package future

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"spotkeeper/internal/model"
	"spotkeeper/internal/timeutil"
)

type fakeStore struct {
	mu    sync.Mutex
	users []model.User
	tasks []model.Task
	off   map[string]bool
}

func (s *fakeStore) UsersForWeekday(int) ([]model.User, error) { return s.users, nil }
func (s *fakeStore) DaysOffOn(string) (map[string]bool, error) { return s.off, nil }
func (s *fakeStore) UpsertTask(t model.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, t)
	return nil
}
func (s *fakeStore) last(login string) (model.Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.tasks) - 1; i >= 0; i-- {
		if s.tasks[i].Login == login {
			return s.tasks[i], true
		}
	}
	return model.Task{}, false
}

type fakePrep struct{ fail string }

func (p fakePrep) Prepare(_ context.Context, u model.User, day string) (model.Job, error) {
	if u.Login == p.fail {
		return model.Job{}, errors.New("boom")
	}
	return model.Job{Login: u.Login, Day: day, Level: u.Level}, nil
}

type fakeRun struct {
	mu    sync.Mutex
	jobs  []model.Job
	times []time.Time
	block chan struct{}
}

func (r *fakeRun) Run(_ context.Context, j model.Job) model.Result {
	r.mu.Lock()
	r.jobs = append(r.jobs, j)
	r.times = append(r.times, time.Now())
	r.mu.Unlock()
	if r.block != nil {
		<-r.block
	}
	return model.Result{Status: model.StatusPaid, SpotID: "s1", Attempts: 2}
}

type fakeNotify struct {
	mu  sync.Mutex
	got []string
}

func (n *fakeNotify) Result(_ context.Context, login, _ string, r model.Result) {
	n.mu.Lock()
	n.got = append(n.got, login+":"+r.Status)
	n.mu.Unlock()
}

func users(names ...string) []model.User {
	var us []model.User
	for _, n := range names {
		us = append(us, model.User{Login: n, Active: true})
	}
	return us
}

func newF(st *fakeStore, p fakePrep, r *fakeRun, n *fakeNotify) *Future {
	return NewFuture(st, p, r, n, FutureConfig{ReleaseTime: "11:00", PrepareLead: 10 * time.Minute,
		AheadDays: 28})
}

// The 11:00 run reserves on the level chosen in the user's settings (monitors ignore it).
func TestFutureKeepsUserLevel(t *testing.T) {
	us := users("a")
	us[0].Level = "nadziemny"
	r := &fakeRun{}
	f := newF(&fakeStore{users: us}, fakePrep{}, r, &fakeNotify{})
	if err := f.RunDay(context.Background(), time.Now().Add(100*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if len(r.jobs) != 1 || r.jobs[0].Level != "nadziemny" {
		t.Fatalf("future job must keep the user's level: %+v", r.jobs)
	}
}

func TestRunDayBasic(t *testing.T) {
	st := &fakeStore{users: users("a", "b")}
	r, n := &fakeRun{}, &fakeNotify{}
	f := newF(st, fakePrep{}, r, n)
	release := time.Now().Add(100 * time.Millisecond)
	if err := f.RunDay(context.Background(), release); err != nil {
		t.Fatal(err)
	}
	if len(r.jobs) != 2 {
		t.Fatalf("runs=%d", len(r.jobs))
	}
	want := timeutil.AddDays(timeutil.Today(release), 28)
	for _, j := range r.jobs {
		if j.Day != want {
			t.Fatalf("day %s want %s", j.Day, want)
		}
	}
	tk, _ := st.last("a")
	if tk.Status != model.StatusPaid || tk.SpotID != "s1" || tk.Attempts != 2 || tk.Kind != model.KindFuture {
		t.Fatalf("task %+v", tk)
	}
	if len(n.got) != 2 {
		t.Fatalf("notify %v", n.got)
	}
}

func TestPrepareFailureIsolated(t *testing.T) {
	st := &fakeStore{users: users("a", "bad", "c")}
	r, n := &fakeRun{}, &fakeNotify{}
	f := newF(st, fakePrep{fail: "bad"}, r, n)
	if err := f.RunDay(context.Background(), time.Now().Add(100*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if len(r.jobs) != 2 {
		t.Fatalf("runs=%d", len(r.jobs))
	}
	tk, ok := st.last("bad")
	if !ok || tk.Status != model.StatusFailed || tk.Detail != "boom" {
		t.Fatalf("task %+v", tk)
	}
	if len(n.got) != 3 {
		t.Fatalf("notify %v", n.got)
	}
}

func TestAllStartConcurrently(t *testing.T) {
	st := &fakeStore{users: users("a", "b", "c")}
	r := &fakeRun{block: make(chan struct{})}
	f := newF(st, fakePrep{}, r, &fakeNotify{})
	done := make(chan struct{})
	go func() { _ = f.RunDay(context.Background(), time.Now().Add(100*time.Millisecond)); close(done) }()
	time.Sleep(250 * time.Millisecond)
	r.mu.Lock()
	n := len(r.times)
	var spread time.Duration
	if n == 3 {
		spread = r.times[2].Sub(r.times[0])
		if d := r.times[1].Sub(r.times[0]); d > spread {
			spread = d
		}
	}
	r.mu.Unlock()
	close(r.block)
	<-done
	if n != 3 || spread > 50*time.Millisecond {
		t.Fatalf("started=%d spread=%v", n, spread)
	}
}

func TestNotBeforeIsRelease(t *testing.T) {
	st := &fakeStore{users: users("a")}
	r := &fakeRun{}
	f := newF(st, fakePrep{}, r, &fakeNotify{})
	release := time.Now().Add(100 * time.Millisecond)
	_ = f.RunDay(context.Background(), release)
	if len(r.jobs) != 1 || !r.jobs[0].NotBefore.Equal(release) {
		t.Fatalf("jobs %+v", r.jobs)
	}
}

// fakeClock drives Loop: Sleep advances the wall clock by the requested time
// (plus extra[i] on the i-th call, a host suspend) and stops the loop after
// stopAfter runs.
type fakeClock struct {
	now    time.Time
	sleeps []time.Duration
	extra  map[int]time.Duration
}

func (c *fakeClock) attach(f *Future, r *fakeRun, stopAfter int) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	f.Now = func() time.Time { return c.now }
	f.Sleep = func(ctx context.Context, d time.Duration) error {
		c.sleeps = append(c.sleeps, d)
		c.now = c.now.Add(d + c.extra[len(c.sleeps)-1])
		if len(r.jobs) >= stopAfter || len(c.sleeps) > 100000 {
			cancel()
			return ctx.Err()
		}
		return nil
	}
	return ctx
}

func TestLoopRunsTodayInsidePrepareWindow(t *testing.T) {
	st := &fakeStore{users: users("a")}
	r := &fakeRun{}
	f := newF(st, fakePrep{}, r, &fakeNotify{})
	c := &fakeClock{now: time.Date(2026, 10, 24, 8, 55, 0, 0, time.UTC)} // 10:55 Warsaw, release 11:00
	_ = f.Loop(c.attach(f, r, 1))
	if len(r.jobs) == 0 || r.jobs[0].Day != "2026-11-21" {
		t.Fatalf("jobs %+v", r.jobs)
	}
}

func TestLoopSleepsThenRuns(t *testing.T) {
	st := &fakeStore{users: users("a")}
	r := &fakeRun{}
	f := newF(st, fakePrep{}, r, &fakeNotify{})
	c := &fakeClock{now: time.Date(2026, 10, 24, 6, 0, 0, 0, time.UTC)} // 08:00 Warsaw
	_ = f.Loop(c.attach(f, r, 1))
	if len(r.jobs) != 1 || r.jobs[0].Day != "2026-11-21" {
		t.Fatalf("jobs %+v", r.jobs)
	}
	// release 09:00Z, lead 10m: 2h50m of waiting, in chunks of at most wakeChunk
	var total time.Duration
	for i, d := range c.sleeps {
		if d > wakeChunk {
			t.Fatalf("sleep %d is %v > %v", i, d, wakeChunk)
		}
		if total += d; total >= 2*time.Hour+50*time.Minute {
			break
		}
	}
	if total != 2*time.Hour+50*time.Minute {
		t.Fatalf("waited %v before preparing", total)
	}
}

// Regression: the host suspended over the release (2026-11-10, 10:48-11:08).
// A single monotonic timer woke hours late and the day was never tried; the
// loop must re-check the wall clock and run the missed release right after
// the resume, logging that it is late.
func TestLoopRunsMissedReleaseAfterSuspend(t *testing.T) {
	st := &fakeStore{users: users("a")}
	r := &fakeRun{}
	f := newF(st, fakePrep{}, r, &fakeNotify{})
	var buf bytes.Buffer
	f.Log = slog.New(slog.NewJSONHandler(&buf, nil))
	start := time.Date(2026, 11, 9, 15, 34, 0, 0, time.UTC) // 16:34 Warsaw, day before
	// suspended for 18h20m during the first wait: resumes 11:08:50 Warsaw
	c := &fakeClock{now: start, extra: map[int]time.Duration{0: 18*time.Hour + 34*time.Minute + 20*time.Second}}
	_ = f.Loop(c.attach(f, r, 1))
	if len(r.jobs) != 1 || r.jobs[0].Day != "2026-12-08" {
		t.Fatalf("jobs %+v", r.jobs)
	}
	if !strings.Contains(buf.String(), `"msg":"future_run_late"`) {
		t.Fatalf("no future_run_late log:\n%s", buf.String())
	}
	if len(c.sleeps) > 2 {
		t.Fatalf("ran only after %d sleeps %v", len(c.sleeps), c.sleeps[:3])
	}
}

func TestFutureLogsRunAndPrepareFailure(t *testing.T) {
	st := &fakeStore{users: users("a", "bad")}
	var buf bytes.Buffer
	f := newF(st, fakePrep{fail: "bad"}, &fakeRun{}, &fakeNotify{})
	f.Log = slog.New(slog.NewJSONHandler(&buf, nil))
	if err := f.RunDay(context.Background(), time.Now().Add(100*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	seen := map[string]map[string]any{}
	for _, ln := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		m := map[string]any{}
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatal(err)
		}
		seen[m["msg"].(string)] = m
	}
	if seen["future_run_start"]["users"].(float64) != 2 {
		t.Fatalf("%v", seen)
	}
	if p := seen["prepare_failed"]; p == nil || p["login"] != "bad" || p["kind"] != "future" || p["level"] != "WARN" {
		t.Fatalf("%v", seen)
	}
	d := seen["future_run_done"]
	if d == nil || d["failed"].(float64) != 1 || d["paid"].(float64) != 1 {
		t.Fatalf("%v", seen)
	}
}

func TestLoopPicksUpNewReleaseTime(t *testing.T) {
	st := &fakeStore{users: users("a")}
	r := &fakeRun{}
	f := newF(st, fakePrep{}, r, &fakeNotify{})
	rel := "11:00"
	f.cfg.ReleaseFunc = func() string { return rel }
	c := &fakeClock{now: time.Date(2026, 10, 24, 6, 0, 0, 0, time.UTC)} // 08:00 Warsaw
	ctx := c.attach(f, r, 2)
	inner := f.Sleep
	f.Sleep = func(ctx context.Context, d time.Duration) error {
		rel = "10:00" // changed while waiting: the first release is already fixed at 11:00
		return inner(ctx, d)
	}
	_ = f.Loop(ctx)
	if len(r.jobs) < 2 {
		t.Fatalf("jobs %d", len(r.jobs))
	}
	if h := r.jobs[0].NotBefore.In(timeutil.Warsaw).Hour(); h != 11 {
		t.Fatalf("first release %v", r.jobs[0].NotBefore)
	}
	if nb := r.jobs[1].NotBefore.In(timeutil.Warsaw); nb.Hour() != 10 || nb.Day() != 25 {
		t.Fatalf("second release %v, want 10:00 Warsaw next day", nb)
	}
}

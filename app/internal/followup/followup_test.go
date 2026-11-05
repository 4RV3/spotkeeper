package followup

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"spotkeeper/internal/model"
)

type fakeAPI struct {
	mu     sync.Mutex
	calls  int
	confAt int // call number from which status is 2 (0 = never)
}

func (f *fakeAPI) PaymentStatus(ctx context.Context, jwt, id string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if jwt != "jwt" || id != "P1" {
		return 0, errors.New("bad args")
	}
	if f.confAt > 0 && f.calls >= f.confAt {
		return 2, nil
	}
	return 1, nil
}

type fakeSess struct{}

func (fakeSess) Session(ctx context.Context, login string) (string, error) { return "jwt", nil }

type fakeStore struct {
	mu    sync.Mutex
	tasks map[string]model.Task
}

func newStore(ts ...model.Task) *fakeStore {
	s := &fakeStore{tasks: map[string]model.Task{}}
	for _, t := range ts {
		s.UpsertTask(t)
	}
	return s
}
func key(l, d, k string) string { return l + "|" + d + "|" + k }
func (s *fakeStore) UpsertTask(t model.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[key(t.Login, t.Day, t.Kind)] = t
	return nil
}
func (s *fakeStore) GetTask(l, d, k string) (model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[key(l, d, k)]
	if !ok {
		return model.Task{}, model.ErrNotFound
	}
	return t, nil
}
func (s *fakeStore) ListTasksByStatus(st string, since time.Time) ([]model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Task
	for _, t := range s.tasks {
		if t.Status == st && !t.UpdatedAt.Before(since) {
			out = append(out, t)
		}
	}
	return out, nil
}

type notif struct {
	mu   sync.Mutex
	got  []model.Result
	days []string
}

func (n *notif) Result(ctx context.Context, login, day string, r model.Result) {
	n.mu.Lock()
	n.got = append(n.got, r)
	n.days = append(n.days, day)
	n.mu.Unlock()
}
func (n *notif) list() []model.Result {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]model.Result(nil), n.got...)
}

func unconfirmedTask(updated time.Time) model.Task {
	return model.Task{Login: "u", Day: "2026-10-26", Kind: model.KindFuture, Status: model.StatusUnconfirmed,
		PaymentID: "P1", ReservationID: "R1", UpdatedAt: updated}
}

func mk(api *fakeAPI, st *fakeStore, n *notif) *Followup {
	f := New(api, fakeSess{}, st, n, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.Delays = []time.Duration{10 * time.Millisecond, 30 * time.Millisecond}
	return f
}

var unc = model.Result{Status: model.StatusUnconfirmed, PaymentID: "P1", ReservationID: "R1"}

func TestConfirmedOnFirstCheck(t *testing.T) {
	api, st, n := &fakeAPI{confAt: 1}, newStore(unconfirmedTask(time.Now())), &notif{}
	f := mk(api, st, n)
	f.Wrap(n).Result(context.Background(), "u", "2026-10-26", unc)
	f.Wait()
	got := n.list()
	if len(got) != 2 || got[0].Status != model.StatusUnconfirmed || got[1].Status != model.StatusPaid {
		t.Fatalf("%+v", got)
	}
	tk, _ := st.GetTask("u", "2026-10-26", model.KindFuture)
	if tk.Status != model.StatusPaid || !strings.HasPrefix(tk.Detail, "płatność potwierdzona po ") {
		t.Fatalf("%+v", tk)
	}
	if api.calls != 1 {
		t.Fatalf("calls %d", api.calls)
	}
}

func TestStillUnconfirmedAfterLastCheck(t *testing.T) {
	api, st, n := &fakeAPI{}, newStore(unconfirmedTask(time.Now())), &notif{}
	f := mk(api, st, n)
	f.Wrap(n).Result(context.Background(), "u", "2026-10-26", unc)
	f.Wait()
	got := n.list()
	if len(got) != 2 || got[1].Status != model.StatusUnconfirmed || !strings.Contains(got[1].Err, "nadal niepotwierdzona") {
		t.Fatalf("%+v", got)
	}
	if api.calls != 2 {
		t.Fatalf("calls %d", api.calls)
	}
	tk, _ := st.GetTask("u", "2026-10-26", model.KindFuture)
	if tk.Status != model.StatusUnconfirmed || !strings.Contains(tk.Detail, "nadal niepotwierdzona") {
		t.Fatalf("%+v", tk)
	}
}

func TestResumeAfterRestart(t *testing.T) {
	// first delay already passed; only the last check remains
	api, n := &fakeAPI{confAt: 1}, &notif{}
	st := newStore(unconfirmedTask(time.Now().Add(-20*time.Millisecond)),
		model.Task{Login: "old", Day: "d", Kind: model.KindPast, Status: model.StatusUnconfirmed, PaymentID: "P1", UpdatedAt: time.Now().Add(-time.Hour)})
	f := mk(api, st, n)
	f.Run(context.Background())
	f.Wait()
	got := n.list()
	if len(got) != 1 || got[0].Status != model.StatusPaid || api.calls != 1 {
		t.Fatalf("%+v calls=%d", got, api.calls)
	}
	// a finished follow-up is not resumed again
	f2 := mk(&fakeAPI{}, st, &notif{})
	f2.Run(context.Background())
	f2.Wait()
	if f2.api.(*fakeAPI).calls != 0 {
		t.Fatal("paid task resumed")
	}
}

func TestResumeStopsOnCtx(t *testing.T) {
	api, st, n := &fakeAPI{}, newStore(unconfirmedTask(time.Now())), &notif{}
	f := mk(api, st, n)
	f.Delays = []time.Duration{time.Hour, 2 * time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	f.Run(ctx)
	cancel()
	f.Wait()
	if len(n.list()) != 0 || api.calls != 0 {
		t.Fatal("ran after cancel")
	}
}

func TestWrapForwardsEverything(t *testing.T) {
	api, st, n := &fakeAPI{}, newStore(), &notif{}
	f := mk(api, st, n)
	w := f.Wrap(n)
	rs := []model.Result{
		{Status: model.StatusPaid, PaymentID: "P1"},
		{Status: model.StatusFailed, Err: "x"},
		{Status: model.StatusUnconfirmed},                  // no payment id: no schedule
		{Status: model.StatusUnconfirmed, PaymentID: "P1"}, // no stored task: no schedule
	}
	for _, r := range rs {
		w.Result(context.Background(), "u", "2026-10-26", r)
	}
	f.Wait()
	got := n.list()
	if len(got) != len(rs) {
		t.Fatalf("%+v", got)
	}
	for i := range rs {
		if got[i] != rs[i] {
			t.Fatalf("%d: %+v != %+v", i, got[i], rs[i])
		}
	}
	if api.calls != 0 {
		t.Fatalf("unexpected checks: %d", api.calls)
	}
}

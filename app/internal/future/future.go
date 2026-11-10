// Package future schedules the daily future reservations.
package future

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"spotkeeper/internal/model"
	"spotkeeper/internal/timeutil"
)

// startLead is how long before the release the per-user engines start. It
// covers the engine's warm-up (engine.WarmLead): the mobile app's booking
// screen calls, sent shortly before the release and bounded by it; the
// reservation calls themselves wait for NotBefore.
const startLead = 4 * time.Second

// FutureStore is the persistence needed by the scheduler.
type FutureStore interface {
	UsersForWeekday(wd int) ([]model.User, error)
	UpsertTask(t model.Task) error
	// DaysOffOn returns the logins that marked day as a day off.
	DaysOffOn(day string) (map[string]bool, error)
}

// Preparer builds a job for one user and day.
type Preparer interface {
	Prepare(ctx context.Context, u model.User, day string) (model.Job, error)
}

// Runner executes one reservation job.
type Runner interface {
	Run(ctx context.Context, job model.Job) model.Result
}

// Notifier reports a result to the user.
type Notifier interface {
	Result(ctx context.Context, login, day string, r model.Result)
}

// FutureConfig configures the scheduler.
type FutureConfig struct {
	ReleaseTime string
	// ReleaseFunc, if set, overrides ReleaseTime and is read on every Loop iteration.
	ReleaseFunc func() string
	PrepareLead time.Duration
	AheadDays   int
}

// Future runs the daily release.
type Future struct {
	st   FutureStore
	prep Preparer
	run  Runner
	n    Notifier
	cfg  FutureConfig

	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
	// Log receives structured events; nil means slog.Default().
	Log *slog.Logger
	// Health, if set, receives the outcome of every run (admin "Stan systemu").
	Health func(err error, detail string)
	// Summary, if set, receives every run that had scheduled users (admin report).
	Summary func(ctx context.Context, s RunSummary)
}

// RunSummary is the outcome of one 11:00 run.
type RunSummary struct {
	Day                                string
	Paid, Unconfirmed, Failed, Skipped int
	// Problems has "login — status: reason" for everyone not paid, sorted.
	Problems []string
}

func (f *Future) releaseTime() string {
	if f.cfg.ReleaseFunc != nil {
		if r := f.cfg.ReleaseFunc(); r != "" {
			return r
		}
	}
	return f.cfg.ReleaseTime
}

func (f *Future) logger() *slog.Logger {
	if f.Log != nil {
		return f.Log
	}
	return slog.Default()
}

// NewFuture builds a Future.
func NewFuture(st FutureStore, prep Preparer, run Runner, n Notifier, cfg FutureConfig) *Future {
	return &Future{st: st, prep: prep, run: run, n: n, cfg: cfg,
		Now: time.Now, Sleep: sleepCtx}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// wakeChunk caps one timer in sleepUntil. Go timers run on the monotonic
// clock, which stops while the host is suspended; re-checking the wall clock
// at least this often bounds how late a wake-up can be after a resume.
const wakeChunk = 30 * time.Second

// sleepUntil sleeps until the wall-clock instant at, in chunks of at most
// wakeChunk, so time the host spends suspended counts toward the wait.
func (f *Future) sleepUntil(ctx context.Context, at time.Time) error {
	at = at.Round(0) // drop any monotonic reading: compare wall clocks only
	for {
		d := at.Sub(f.Now().Round(0))
		if d <= 0 {
			return ctx.Err()
		}
		if err := f.Sleep(ctx, min(d, wakeChunk)); err != nil {
			return err
		}
	}
}

// RunDay prepares and runs all reservations for the release at time release.
func (f *Future) RunDay(ctx context.Context, release time.Time) error {
	day := timeutil.AddDays(timeutil.Today(release), f.cfg.AheadDays)
	all, err := f.st.UsersForWeekday(timeutil.Weekday(day))
	if err != nil {
		return err
	}
	users, problems := f.skipDaysOff(all, day)
	skipped := len(problems)
	var probMu sync.Mutex
	problem := func(login, status, reason string) {
		probMu.Lock()
		problems = append(problems, login+" — "+status+": "+reason)
		probMu.Unlock()
	}
	began := f.Now()
	f.logger().Info("future_run_start", "release", release.Format(time.RFC3339), "day", day, "users", len(users))
	if late := began.Round(0).Sub(release.Round(0)); late > 0 {
		// e.g. the host was suspended over the release: still try, late
		f.logger().Warn("future_run_late", "day", day, "late_ms", late.Milliseconds())
	}
	var paid, unconfirmed, failed, already atomic.Int64

	type prepared struct {
		job model.Job
		ok  bool
	}
	res := make([]prepared, len(users))
	var wg sync.WaitGroup
	for i, u := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job, err := f.prep.Prepare(ctx, u, day)
			if err != nil {
				failed.Add(1)
				problem(u.Login, "nieudana", err.Error())
				f.logger().Warn("prepare_failed", "kind", model.KindFuture, "login", u.Login, "day", day, "err", err.Error())
				_ = f.st.UpsertTask(model.Task{Login: u.Login, Day: day, Kind: model.KindFuture,
					Status: model.StatusFailed, Detail: err.Error()})
				f.n.Result(ctx, u.Login, day, model.Result{Status: model.StatusFailed, Err: err.Error()})
				return
			}
			_ = f.st.UpsertTask(model.Task{Login: u.Login, Day: day, Kind: model.KindFuture,
				Status: model.StatusPending})
			res[i] = prepared{job: job, ok: true}
		}()
	}
	wg.Wait()

	_ = f.sleepUntil(ctx, release.Add(-startLead))
	if ctx.Err() != nil {
		return nil
	}

	for _, p := range res {
		if !p.ok {
			continue
		}
		job := p.job
		job.NotBefore = release
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = f.st.UpsertTask(model.Task{Login: job.Login, Day: day, Kind: model.KindFuture,
				Status: model.StatusRunning})
			r := f.run.Run(ctx, job)
			switch r.Status {
			case model.StatusPaid:
				paid.Add(1)
			case model.StatusUnconfirmed:
				unconfirmed.Add(1)
				problem(job.Login, "niepotwierdzona", r.Err)
			case model.StatusSkipped:
				// already booked by the user (service code 106): not a failure
				already.Add(1)
				problem(job.Login, "pominięto", "masz już rezerwację na ten dzień")
			default:
				failed.Add(1)
				problem(job.Login, "nieudana", r.Err)
			}
			_ = f.st.UpsertTask(model.Task{Login: job.Login, Day: day, Kind: model.KindFuture,
				Status: r.Status, SpotID: r.SpotID, ReservationID: r.ReservationID,
				PaymentID: r.PaymentID, Attempts: r.Attempts, Detail: r.Err})
			f.n.Result(ctx, job.Login, day, r)
		}()
	}
	wg.Wait()
	f.logger().Info("future_run_done", "day", day, "paid", int(paid.Load()), "unconfirmed", int(unconfirmed.Load()),
		"failed", int(failed.Load()), "duration_ms", f.Now().Sub(began).Milliseconds())
	if f.Health != nil {
		detail := fmt.Sprintf("dzień %s: opłacono %d, niepotwierdzone %d, nieudane %d",
			day, paid.Load(), unconfirmed.Load(), failed.Load())
		var err error
		if failed.Load() > 0 {
			err = errors.New(detail)
		}
		f.Health(err, detail)
	}
	if f.Summary != nil && len(all) > 0 {
		sort.Strings(problems)
		f.Summary(ctx, RunSummary{Day: day, Paid: int(paid.Load()), Unconfirmed: int(unconfirmed.Load()),
			Failed: int(failed.Load()), Skipped: skipped + int(already.Load()), Problems: problems})
	}
	return nil
}

// skipDaysOff drops users who marked day as a day off, or who skip public holidays when
// day is one, and records the day as skipped (no notification). It also returns one
// "login — pominięto: reason" line per skipped user.
func (f *Future) skipDaysOff(users []model.User, day string) ([]model.User, []string) {
	off, err := f.st.DaysOffOn(day)
	if err != nil {
		f.logger().Warn("days_off_failed", "day", day, "err", err.Error())
	}
	holiday := timeutil.Holiday(day)
	out := users[:0:0]
	var skipped []string
	for _, u := range users {
		reason := ""
		switch {
		case off[u.Login]:
			reason = "urlop"
		case holiday != "" && u.SkipHolidays:
			reason = "święto: " + holiday
		}
		if reason == "" {
			out = append(out, u)
			continue
		}
		f.logger().Info("future_skipped", "login", u.Login, "day", day, "reason", reason)
		_ = f.st.UpsertTask(model.Task{Login: u.Login, Day: day, Kind: model.KindFuture,
			Status: model.StatusSkipped, Detail: reason})
		skipped = append(skipped, u.Login+" — pominięto: "+reason)
	}
	return out, skipped
}

// Loop runs RunDay every day until ctx is done.
func (f *Future) Loop(ctx context.Context) error {
	var last time.Time
	for ctx.Err() == nil {
		now := f.Now()
		rt := f.releaseTime()
		release, err := timeutil.At(timeutil.Today(now), rt)
		if err != nil {
			return err
		}
		// Today's release is still ahead (possibly inside the prepare window)
		// unless it has passed or was already run.
		if !now.Before(release) || !release.After(last) {
			release, err = timeutil.At(timeutil.AddDays(timeutil.Today(now), 1), rt)
			if err != nil {
				return err
			}
		}
		if err := f.sleepUntil(ctx, release.Add(-f.cfg.PrepareLead)); err != nil {
			return nil
		}
		last = release
		if err := f.RunDay(ctx, release); err != nil {
			return err
		}
	}
	return nil
}

// Package followup re-checks payments that were still unconfirmed when the
// engine gave up, and tells the user how they ended.
package followup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"spotkeeper/internal/model"
)

// PaymentAPI is the service call used to poll a payment.
type PaymentAPI interface {
	PaymentStatus(ctx context.Context, jwt, paymentID string) (int, error)
}

// SessionSource returns a valid JWT for a login.
type SessionSource interface {
	Session(ctx context.Context, login string) (string, error)
}

// TaskStore is the subset of the store used by followup.
type TaskStore interface {
	UpsertTask(model.Task) error
	GetTask(login, day, kind string) (model.Task, error)
	ListTasksByStatus(status string, since time.Time) ([]model.Task, error)
}

// Notifier delivers a result to the user.
type Notifier interface {
	Result(ctx context.Context, login, day string, r model.Result)
}

// resumeWindow bounds how old an unconfirmed task may be to be resumed.
const resumeWindow = 35 * time.Minute

// Followup schedules delayed payment checks.
type Followup struct {
	// Delays are offsets from the unconfirmed result; the last one is final.
	Delays []time.Duration
	// Now is a test hook.
	Now func() time.Time

	api  PaymentAPI
	sess SessionSource
	st   TaskStore
	n    Notifier
	log  *slog.Logger

	mu     sync.Mutex
	ctx    context.Context
	active map[string]bool
	wg     sync.WaitGroup
}

// New builds a Followup with delays {5m, 30m}.
func New(api PaymentAPI, sess SessionSource, st TaskStore, n Notifier, log *slog.Logger) *Followup {
	if log == nil {
		log = slog.Default()
	}
	return &Followup{
		Delays: []time.Duration{5 * time.Minute, 30 * time.Minute},
		Now:    time.Now,
		api:    api, sess: sess, st: st, n: n, log: log,
		ctx:    context.Background(),
		active: map[string]bool{},
	}
}

// Run binds ctx: all timers stop when it ends. It does not block and also
// resumes pending checks from the store (see Resume).
func (f *Followup) Run(ctx context.Context) {
	f.mu.Lock()
	f.ctx = ctx
	f.mu.Unlock()
	f.Resume(ctx)
}

// Wait blocks until every scheduled check goroutine has returned.
func (f *Followup) Wait() { f.wg.Wait() }

// Wrap returns a Notifier forwarding to n that also schedules checks for
// unconfirmed results that carry a payment id. Set Followup's own notifier
// to the unwrapped n.
func (f *Followup) Wrap(n Notifier) Notifier { return wrappedN{f, n} }

type wrappedN struct {
	f *Followup
	n Notifier
}

func (w wrappedN) Result(ctx context.Context, login, day string, r model.Result) {
	w.n.Result(ctx, login, day, r)
	if r.Status != model.StatusUnconfirmed || r.PaymentID == "" {
		return
	}
	for _, kind := range []string{model.KindFuture, model.KindPast} {
		t, err := w.f.st.GetTask(login, day, kind)
		if err == nil {
			// future and monitor tasks can share login+day: pick the one
			// that actually holds this unconfirmed payment.
			if t.Status == model.StatusUnconfirmed && t.PaymentID == r.PaymentID {
				w.f.schedule(login, day, kind, r.PaymentID, w.f.Now())
				return
			}
		} else if !errors.Is(err, model.ErrNotFound) {
			w.f.log.Warn("payment_followup_schedule", "login", login, "day", day, "err", err.Error())
			return
		}
	}
	w.f.log.Warn("payment_followup_schedule", "login", login, "day", day, "err", "task not found")
}

func finalMsg(d time.Duration) string {
	return fmt.Sprintf("płatność nadal niepotwierdzona po %d min — sprawdź w aplikacji serwisu", int(d.Minutes()))
}

// Resume schedules the remaining checks of recent unconfirmed tasks so that
// a restart does not lose them.
func (f *Followup) Resume(ctx context.Context) {
	if len(f.Delays) == 0 {
		return
	}
	now := f.Now()
	ts, err := f.st.ListTasksByStatus(model.StatusUnconfirmed, now.Add(-resumeWindow))
	if err != nil {
		f.log.Warn("payment_followup_resume", "err", err.Error())
		return
	}
	for _, t := range ts {
		if t.PaymentID == "" || t.Detail == finalMsg(f.Delays[len(f.Delays)-1]) {
			continue
		}
		f.schedule(t.Login, t.Day, t.Kind, t.PaymentID, t.UpdatedAt)
	}
}

// schedule starts the check goroutine for one task; delays are offsets from
// base. Overdue delays are skipped except the last, which runs at once.
func (f *Followup) schedule(login, day, kind, paymentID string, base time.Time) {
	key := login + "|" + day + "|" + kind
	f.mu.Lock()
	if f.active[key] || len(f.Delays) == 0 {
		f.mu.Unlock()
		return
	}
	f.active[key] = true
	ctx := f.ctx
	delays := append([]time.Duration(nil), f.Delays...)
	f.wg.Add(1)
	f.mu.Unlock()

	go func() {
		defer f.wg.Done()
		defer func() {
			f.mu.Lock()
			delete(f.active, key)
			f.mu.Unlock()
		}()
		for i, d := range delays {
			last := i == len(delays)-1
			wait := base.Add(d).Sub(f.Now())
			if wait <= 0 && !last {
				continue
			}
			if wait > 0 {
				t := time.NewTimer(wait)
				select {
				case <-t.C:
				case <-ctx.Done():
					t.Stop()
					return
				}
			}
			if f.check(ctx, login, day, kind, paymentID, i+1, d, last) {
				return
			}
		}
	}()
}

// check runs one check; true means the follow-up is finished.
func (f *Followup) check(ctx context.Context, login, day, kind, paymentID string, n int, d time.Duration, last bool) bool {
	if ctx.Err() != nil {
		return true
	}
	task, err := f.st.GetTask(login, day, kind)
	if err != nil || task.Status != model.StatusUnconfirmed {
		f.log.Info("payment_followup", "login", login, "day", day, "check", n, "status", "task_changed", "confirmed", false)
		return true
	}
	status := -1
	jwt, err := f.sess.Session(ctx, login)
	if err == nil {
		status, err = f.api.PaymentStatus(ctx, jwt, paymentID)
	}
	if err != nil {
		f.log.Warn("payment_followup_error", "login", login, "day", day, "check", n, "err", err.Error())
	}
	if ctx.Err() != nil { // shutdown mid-check: record nothing, Resume retries
		return true
	}
	confirmed := err == nil && status == 2
	f.log.Info("payment_followup", "login", login, "day", day, "check", n, "status", status, "confirmed", confirmed)
	if confirmed {
		task.Status = model.StatusPaid
		task.Detail = fmt.Sprintf("płatność potwierdzona po %d min", int(d.Minutes()))
		task.UpdatedAt = f.Now()
		if err := f.st.UpsertTask(task); err != nil {
			f.log.Warn("payment_followup_store", "login", login, "day", day, "err", err.Error())
		}
		f.n.Result(ctx, login, day, model.Result{Status: model.StatusPaid, SpotID: task.SpotID,
			ReservationID: task.ReservationID, PaymentID: paymentID, Attempts: task.Attempts})
		return true
	}
	if !last {
		return false
	}
	msg := finalMsg(d)
	task.Detail = msg
	task.UpdatedAt = f.Now()
	if err := f.st.UpsertTask(task); err != nil {
		f.log.Warn("payment_followup_store", "login", login, "day", day, "err", err.Error())
	}
	f.n.Result(ctx, login, day, model.Result{Status: model.StatusUnconfirmed, SpotID: task.SpotID,
		ReservationID: task.ReservationID, PaymentID: paymentID, Attempts: task.Attempts, Err: msg})
	return true
}

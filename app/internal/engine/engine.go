// Package engine runs one reservation.
package engine

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync/atomic"
	"time"

	"spotkeeper/internal/model"
)

// API is the service API surface the engine needs.
type API interface {
	Offers(ctx context.Context, jwt, parkingID string, start, end time.Time) (model.Offers, error)
	Reserve(ctx context.Context, jwt, spotID string, start, end time.Time, token, vehicleID string) (string, error)
	PaymentAmount(ctx context.Context, jwt, reservationID string) (model.Amount, error)
	StartPayment(ctx context.Context, jwt, orderID string, amount float64, reservationID string) (string, error)
	StartCardPayment(ctx context.Context, jwt, cardID string, amount float64, reservationID, ip string) (string, error)
	PaymentStatus(ctx context.Context, jwt, paymentID string) (int, error)
}

// ContextAPI is the optional part of the API that repeats the mobile app's
// surrounding calls: BookingContext when the booking screen opens (sent
// shortly before the release) and CheckoutContext between picking a spot and
// booking it (sent while the turnstile token is solved). Their errors never
// stop a reservation.
type ContextAPI interface {
	BookingContext(ctx context.Context, jwt, parkingID string, now time.Time) error
	CheckoutContext(ctx context.Context, jwt, parkingID, spotID string, start, end time.Time) error
}

// Tokener mints a solved turnstile token.
type Tokener interface {
	Token(ctx context.Context, turnstileURL, requestID string) (model.Token, error)
}

const (
	defaultAmount  = 15.0
	reserveTimeout = 15 * time.Second
	code106Backoff = 3 * time.Second
	// already106 consecutive 106 answers mean the user already holds a reservation
	// for that time (one alone can be transient, as in the original workflow)
	already106   = 2
	failBackoff  = 500 * time.Millisecond
	tokenBackoff = time.Second
	apiTries     = 5 // max tries for payment calls
	apiRetryWait = time.Second
	// WarmLead is how long before NotBefore BookingContext is sent. The run
	// waits for it at most until warmMargin before NotBefore; calls still in
	// flight then finish in the background (never cancelled at the release,
	// so their pooled connections stay warm). warmTimeout bounds them.
	WarmLead    = 3 * time.Second
	warmMargin  = 300 * time.Millisecond
	warmTimeout = 5 * time.Second
	// checkoutWait is how long Reserve waits, once the token is ready, for a
	// still-running CheckoutContext; checkoutTimeout bounds CheckoutContext.
	checkoutWait    = 300 * time.Millisecond
	checkoutTimeout = 5 * time.Second
	// prePayBudget bounds Payments/amount and Payments/start-payment together
	// (prePayTries each, prePayRetryWait apart), so the card is charged soon
	// after the booking even when they fail or hang.
	prePayBudget    = 5 * time.Second
	prePayTries     = 2
	prePayRetryWait = 500 * time.Millisecond
)

// Engine runs one reservation per Run call.
type Engine struct {
	api    API
	tok    Tokener
	cfg    model.EngineConfig
	claims *Claims

	// Test hooks.
	Sleep func(ctx context.Context, d time.Duration) error
	Now   func() time.Time
	Rand  *rand.Rand
	// Log receives structured events; nil means slog.Default().
	Log *slog.Logger
	// Relogin returns a fresh JWT for login after the session expired
	// (HTTP 401) mid-run; nil disables it. Used at most once per Run.
	Relogin func(ctx context.Context, login string) (string, error)
	// Amount, if set and > 0, overrides cfg.PaymentAmount at each payment.
	Amount func() float64
}

func (e *Engine) logger() *slog.Logger {
	if e.Log != nil {
		return e.Log
	}
	return slog.Default()
}

// runStats collects timing for the result log line.
type runStats struct {
	base       time.Time    // moment the run began (after NotBefore)
	firstResNs atomic.Int64 // ns since base, +1; 0 = none
	minted     atomic.Int64
	relogged   bool // Relogin already used in this run (run goroutine only)
}

// withRelogin calls fn with job.JWT; on a 401 it re-logs in once per run,
// swaps job.JWT and retries fn immediately.
func (e *Engine) withRelogin(ctx context.Context, job *model.Job, st *runStats, fn func(jwt string) error) error {
	err := fn(job.JWT)
	var ae *model.APIError
	if err == nil || e.Relogin == nil || st.relogged || !errors.As(err, &ae) || ae.HTTPStatus != 401 {
		return err
	}
	st.relogged = true
	jwt, rerr := e.Relogin(ctx, job.Login)
	e.logger().Warn("session_relogin", "login", job.Login, "day", job.Day, "ok", rerr == nil && jwt != "")
	if rerr != nil || jwt == "" {
		return err
	}
	job.JWT = jwt
	return fn(job.JWT)
}

func ms(d time.Duration) int64 { return d.Milliseconds() }

func shortErr(err error) string {
	if err == nil {
		return ""
	}
	var ae *model.APIError
	if errors.As(err, &ae) {
		return truncate(ae.Message)
	}
	return truncate(err.Error())
}

func truncate(s string) string {
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

// New builds an Engine.
func New(api API, tok Tokener, cfg model.EngineConfig, claims *Claims) *Engine {
	if claims == nil {
		claims = NewClaims()
	}
	return &Engine{api: api, tok: tok, cfg: cfg, claims: claims, Sleep: sleepCtx, Now: time.Now}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Run executes the job and returns its result. Mirrors the original workflow: every
// attempt queries offers with the user's JWT, takes the spot and the
// turnstile URL from that same response, solves a token for that URL and
// reserves with it.
func (e *Engine) Run(ctx context.Context, job model.Job) model.Result {
	st := &runStats{}
	if cx, ok := e.api.(ContextAPI); ok {
		if wait := job.NotBefore.Add(-WarmLead).Sub(e.Now()); wait > 0 {
			if err := e.Sleep(ctx, wait); err != nil {
				return model.Result{Status: model.StatusCancelled}
			}
		}
		done := make(chan struct{})
		jwt := job.JWT
		go func() {
			defer close(done)
			wc, cancel := context.WithTimeout(ctx, warmTimeout)
			defer cancel()
			if err := cx.BookingContext(wc, jwt, job.ParkingID, e.Now()); err != nil {
				e.logger().Debug("booking_context_error", "login", job.Login, "day", job.Day, "err", shortErr(err))
			}
		}()
		// a late start does not wait at all
		if wait := job.NotBefore.Add(-warmMargin).Sub(e.Now()); wait > 0 {
			t := time.NewTimer(wait)
			select {
			case <-done:
			case <-t.C:
			case <-ctx.Done():
			}
			t.Stop()
		}
	}
	if wait := job.NotBefore.Sub(e.Now()); wait > 0 {
		if err := e.Sleep(ctx, wait); err != nil {
			return model.Result{Status: model.StatusCancelled}
		}
	}
	st.base = e.Now()
	r := e.run(ctx, &job, st)
	first := int64(-1)
	if v := st.firstResNs.Load(); v > 0 {
		first = ms(time.Duration(v - 1))
	}
	lvl := slog.LevelWarn
	if r.Status == model.StatusPaid {
		lvl = slog.LevelInfo
	}
	e.logger().Log(ctx, lvl, "reservation_result", "login", job.Login, "day", job.Day,
		"status", r.Status, "attempts", r.Attempts, "spot", r.SpotID,
		"reservation_id", r.ReservationID, "payment_id", r.PaymentID, "err", truncate(r.Err),
		"first_reserve_ms", first,
		"total_ms", ms(e.Now().Sub(st.base)), "tokens_minted", int(st.minted.Load()))
	return r
}

func (e *Engine) pollInterval() time.Duration {
	if e.cfg.PollInterval <= 0 {
		return 500 * time.Millisecond
	}
	return e.cfg.PollInterval
}

func (e *Engine) budget() time.Duration {
	if e.cfg.Budget <= 0 {
		return 450 * time.Second
	}
	return e.cfg.Budget
}

func (e *Engine) maxAttempts() int {
	if e.cfg.MaxAttempts <= 0 {
		return 10
	}
	return e.cfg.MaxAttempts
}

func (e *Engine) run(ctx context.Context, job *model.Job, st *runStats) model.Result {
	deadline := st.base.Add(e.budget())
	rejected := map[string]bool{} // spots tried in this run or claimed by siblings
	max := e.maxAttempts()
	reserves := 0
	cancelled := func() model.Result {
		return model.Result{Status: model.StatusCancelled, Attempts: reserves}
	}
	// pause sleeps d; false means the run was cancelled.
	pause := func(d time.Duration) bool { return e.Sleep(ctx, d) == nil }
	// runID is the per-execution id in the turnstile requestId
	// ("<run>:<attempt>"); turnstile-service only accepts [A-Za-z0-9_.:-].
	runID := newRunID()
	in106 := 0 // consecutive 106 answers

	for attempt := 1; attempt <= max; attempt++ {
		if ctx.Err() != nil {
			return cancelled()
		}
		if !e.Now().Before(deadline) {
			return model.Result{Status: model.StatusFailed, Err: "budget exhausted", Attempts: reserves}
		}
		var offers model.Offers
		err := e.withRelogin(ctx, job, st, func(jwt string) (err error) {
			offers, err = e.api.Offers(ctx, jwt, job.ParkingID, job.Start, job.End)
			return
		})
		var spot model.Spot
		found := false
		if err == nil {
			for {
				spot, found = SelectSpot(offers.Spots, job.Level, func(id string) bool { return rejected[id] }, e.Rand)
				if !found {
					break
				}
				if e.claims.Claim(job.Day, spot.ID) {
					break
				}
				rejected[spot.ID] = true
			}
		}
		if !found {
			if !pause(e.pollInterval()) {
				return cancelled()
			}
			continue
		}
		rejected[spot.ID] = true

		url := offers.TurnstileURL
		if e.cfg.TurnstilePrefix == "" || !strings.HasPrefix(url, e.cfg.TurnstilePrefix) {
			e.claims.Release(job.Day, spot.ID)
			return model.Result{Status: model.StatusFailed, Attempts: reserves,
				Err: "the service did not issue a contextual verification URL"}
		}
		checkout := e.checkoutContext(ctx, job, spot.ID)
		tok, err := e.tok.Token(ctx, url, fmt.Sprintf("%s:%d", runID, attempt))
		st.minted.Add(1)
		if err != nil {
			e.claims.Release(job.Day, spot.ID)
			if ctx.Err() != nil {
				return cancelled()
			}
			code, retry := "error", false
			var te model.TokenError
			var tp *model.TokenError
			if errors.As(err, &te) {
				code, retry = te.Code, te.Retryable
			} else if errors.As(err, &tp) {
				code, retry = tp.Code, tp.Retryable
			}
			e.logger().Warn("token_error", "login", job.Login, "day", job.Day, "code", code, "retryable", retry)
			if !retry {
				return model.Result{Status: model.StatusFailed, Attempts: reserves,
					Err: "Context verification failed: " + code}
			}
			if !pause(tokenBackoff) {
				return cancelled()
			}
			continue
		}

		e.awaitCheckout(ctx, checkout)
		d0 := e.Now().Sub(st.base)
		if d0 < 0 {
			d0 = 0
		}
		st.firstResNs.CompareAndSwap(0, int64(d0)+1)
		rc, cancel := context.WithTimeout(ctx, reserveTimeout)
		var resID string
		err = e.withRelogin(ctx, job, st, func(jwt string) (err error) {
			resID, err = e.api.Reserve(rc, jwt, spot.ID, job.Start, job.End, tok.Value, job.VehicleID)
			return
		})
		cancel()
		reserves++
		ec := 0
		var ae *model.APIError
		errors.As(err, &ae)
		if ae != nil {
			ec = ae.Code
		}
		e.logger().Info("reservation_attempt", "login", job.Login, "day", job.Day, "attempt", attempt,
			"spot", spot.ID, "ok", err == nil, "err_code", ec, "err", shortErr(err),
			"since_start_ms", ms(e.Now().Sub(st.base)))
		if err == nil {
			return e.pay(ctx, job, st, spot.ID, resID, reserves)
		}
		// the original workflow re-picks randomly among all spots, including one that just
		// failed; free the spot again for ourselves and our siblings.
		e.claims.Release(job.Day, spot.ID)
		delete(rejected, spot.ID)
		if ctx.Err() != nil {
			return cancelled()
		}
		d := failBackoff
		if ae != nil && ae.Code == 106 {
			d = code106Backoff
			if in106++; in106 >= already106 {
				e.logger().Warn("reservation_already_booked", "login", job.Login, "day", job.Day, "attempts", reserves)
				return model.Result{Status: model.StatusSkipped, AlreadyBooked: true, Attempts: reserves,
					Err: "masz już rezerwację na ten dzień (serwis: kod 106, druga rezerwacja w tym samym czasie niedozwolona)"}
			}
		} else {
			in106 = 0
		}
		if !pause(d) {
			return cancelled()
		}
	}
	return model.Result{Status: model.StatusFailed, Attempts: reserves,
		Err: fmt.Sprintf("Reservation failed after %d attempts", max)}
}

// checkoutContext starts the API's CheckoutContext for spot in the
// background; the returned channel closes when it ends (nil without a
// ContextAPI).
func (e *Engine) checkoutContext(ctx context.Context, job *model.Job, spotID string) <-chan struct{} {
	cx, ok := e.api.(ContextAPI)
	if !ok {
		return nil
	}
	done := make(chan struct{})
	jwt := job.JWT
	go func() {
		defer close(done)
		cc, cancel := context.WithTimeout(ctx, checkoutTimeout)
		defer cancel()
		if err := cx.CheckoutContext(cc, jwt, job.ParkingID, spotID, job.Start, job.End); err != nil {
			e.logger().Debug("checkout_context_error", "login", job.Login, "day", job.Day, "err", shortErr(err))
		}
	}()
	return done
}

// awaitCheckout waits up to checkoutWait for a running CheckoutContext, so the
// booking follows it as in the mobile app.
func (e *Engine) awaitCheckout(ctx context.Context, done <-chan struct{}) {
	if done == nil {
		return
	}
	t := time.NewTimer(checkoutWait)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
	case <-ctx.Done():
	}
}

// retry calls fn up to apiTries times, apiRetryWait apart, re-logging in on a 401.
func (e *Engine) retry(ctx context.Context, job *model.Job, st *runStats, fn func(jwt string) error) error {
	return e.retryN(ctx, apiTries, apiRetryWait, job, st, fn)
}

// retryN calls fn up to tries times, wait apart, re-logging in on a 401.
func (e *Engine) retryN(ctx context.Context, tries int, wait time.Duration, job *model.Job, st *runStats, fn func(jwt string) error) error {
	var err error
	for try := 0; try < tries; try++ {
		if try > 0 && e.Sleep(ctx, wait) != nil {
			break
		}
		if err = e.withRelogin(ctx, job, st, fn); err == nil {
			return nil
		}
	}
	return err
}

// pay runs under the parent ctx only: the search budget must not cut a
// payment after a successful reservation. Mirrors the mobile app: the amount
// is read from Payments/amount (the configured amount is the fallback when
// that fails), a payment process is registered with Payments/start-payment,
// the saved card is charged and the status is then checked immediately and
// every PayInterval, PayPolls checks in total.
func (e *Engine) pay(ctx context.Context, job *model.Job, st *runStats, spotID, resID string, attempts int) model.Result {
	res := model.Result{SpotID: spotID, ReservationID: resID, Attempts: attempts, Status: model.StatusUnconfirmed}
	amount := e.cfg.PaymentAmount
	if e.Amount != nil {
		if a := e.Amount(); a > 0 {
			amount = a
		}
	}
	if amount <= 0 {
		amount = defaultAmount
	}
	pre, cancelPre := context.WithTimeout(ctx, prePayBudget)
	defer cancelPre()
	var amt model.Amount
	if err := e.retryN(pre, prePayTries, prePayRetryWait, job, st, func(jwt string) (err error) {
		amt, err = e.api.PaymentAmount(pre, jwt, resID)
		return
	}); err != nil {
		e.logger().Warn("payment_amount_fallback", "login", job.Login, "day", job.Day,
			"amount", amount, "err", shortErr(err))
	} else if amt.Free {
		e.logger().Info("payment_free", "login", job.Login, "day", job.Day, "reservation_id", resID)
		res.Status = model.StatusPaid
		return res
	} else if amt.Total > 0 {
		amount = amt.Total
	}

	orderID := newOrderID()
	if err := e.retryN(pre, prePayTries, prePayRetryWait, job, st, func(jwt string) error {
		_, err := e.api.StartPayment(pre, jwt, orderID, amount, resID)
		return err
	}); err != nil {
		// the card payment below is still tried: an unpaid reservation is lost
		e.logger().Warn("start_payment_error", "login", job.Login, "day", job.Day, "err", shortErr(err))
	}

	var payID string
	if err := e.retry(ctx, job, st, func(jwt string) (err error) {
		payID, err = e.api.StartCardPayment(ctx, jwt, job.CardID, amount, resID, job.IP)
		return
	}); err != nil {
		res.Err = "start payment: " + err.Error()
		return res
	}
	res.PaymentID = payID
	polls := e.cfg.PayPolls
	if polls <= 0 {
		polls = 20
	}
	interval := e.cfg.PayInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	for i := 0; i < polls; i++ {
		var ps int
		if err := e.retry(ctx, job, st, func(jwt string) (err error) {
			ps, err = e.api.PaymentStatus(ctx, jwt, payID)
			return
		}); err != nil {
			res.Err = "payment status: " + err.Error()
			return res
		}
		if ps == 2 {
			res.Status = model.StatusPaid
			return res
		}
		if i < polls-1 {
			if e.Sleep(ctx, interval) != nil {
				res.Err = "cancelled while confirming payment"
				return res
			}
		}
	}
	return res
}

// newOrderID returns a random version-4 UUID as 32 lowercase hex digits, the
// orderId format the mobile app sends to Payments/start-payment.
func newOrderID() string {
	b := make([]byte, 16)
	_, _ = crand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return hex.EncodeToString(b)
}

// newRunID returns a random hex id for one reservation run.
func newRunID() string {
	b := make([]byte, 6)
	_, _ = crand.Read(b)
	return hex.EncodeToString(b)
}

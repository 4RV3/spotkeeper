package engine

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"spotkeeper/internal/model"
)

func noSleep(e *Engine) *Engine {
	e.Sleep = func(ctx context.Context, d time.Duration) error { return ctx.Err() }
	return e
}

func TestPayFlowMatchesMobileApp(t *testing.T) {
	a := &fakeAPI{spots: spots3, status: 2, amount: model.Amount{Total: 15}}
	c := cfg()
	c.PaymentAmount = 99 // the API amount wins
	r := noSleep(newEng(a, &fakeTok{}, c, NewClaims())).Run(context.Background(), job("u"))
	if r.Status != model.StatusPaid {
		t.Fatalf("%+v", r)
	}
	if strings.Join(a.payCalls, ",") != "amount,start,card" {
		t.Fatal(a.payCalls)
	}
	if len(a.starts) != 1 || len(a.amounts) != 1 || a.amounts[0] != 15 {
		t.Fatal(a.starts, a.amounts)
	}
	f := strings.Fields(a.starts[0])
	if !regexp.MustCompile(`^[0-9a-f]{12}4[0-9a-f]{3}[89ab][0-9a-f]{15}$`).MatchString(f[0]) || f[1] != "15" || f[2] != "R1" {
		t.Fatal(a.starts[0])
	}
}

func TestPayAmountFallbackOnError(t *testing.T) {
	a := &fakeAPI{spots: spots3, status: 2, amountErr: errors.New("boom")}
	e := noSleep(newEng(a, &fakeTok{}, cfg(), NewClaims()))
	e.Amount = func() float64 { return 12.5 }
	r := e.Run(context.Background(), job("u"))
	if r.Status != model.StatusPaid || a.amountN != prePayTries || len(a.amounts) != 1 || a.amounts[0] != 12.5 {
		t.Fatalf("%+v n=%d %v", r, a.amountN, a.amounts)
	}
	if !strings.HasSuffix(a.starts[0], " 12.5 R1") {
		t.Fatal(a.starts)
	}
}

func TestPayFreeSkipsPayment(t *testing.T) {
	a := &fakeAPI{spots: spots3, status: 2, amount: model.Amount{Free: true}}
	r := noSleep(newEng(a, &fakeTok{}, cfg(), NewClaims())).Run(context.Background(), job("u"))
	if r.Status != model.StatusPaid || r.PaymentID != "" || len(a.starts) != 0 || len(a.amounts) != 0 {
		t.Fatalf("%+v %v %v", r, a.starts, a.amounts)
	}
}

func TestStartPaymentFailureStillPaysByCard(t *testing.T) {
	a := &fakeAPI{spots: spots3, status: 2, amount: model.Amount{Total: 15}, startErr: errors.New("boom")}
	r := noSleep(newEng(a, &fakeTok{}, cfg(), NewClaims())).Run(context.Background(), job("u"))
	if r.Status != model.StatusPaid || len(a.starts) != prePayTries || len(a.amounts) != 1 {
		t.Fatalf("%+v %d %v", r, len(a.starts), a.amounts)
	}
}

func TestNewOrderIDUnique(t *testing.T) {
	if newOrderID() == newOrderID() {
		t.Fatal("same id")
	}
}

// ctxAPI records the mobile-app context calls next to the reservation.
type ctxAPI struct {
	*fakeAPI
	mu     sync.Mutex
	events []string
}

func (c *ctxAPI) ev(s string) {
	c.mu.Lock()
	c.events = append(c.events, s)
	c.mu.Unlock()
}
func (c *ctxAPI) BookingContext(ctx context.Context, jwt, p string, now time.Time) error {
	c.ev("booking:" + p)
	return errors.New("ignored")
}
func (c *ctxAPI) CheckoutContext(ctx context.Context, jwt, p, spot string, s, e time.Time) error {
	time.Sleep(20 * time.Millisecond) // finishes after the instant token
	c.ev("checkout:" + spot)
	return errors.New("ignored")
}
func (c *ctxAPI) Reserve(ctx context.Context, jwt, spotID string, s, e time.Time, token, v string) (string, error) {
	c.ev("reserve:" + spotID)
	return c.fakeAPI.Reserve(ctx, jwt, spotID, s, e, token, v)
}

func TestContextCallsPrecedeReservation(t *testing.T) {
	a := &ctxAPI{fakeAPI: &fakeAPI{spots: spots3[:1], status: 2}}
	e := newEng(a, &fakeTok{}, cfg(), NewClaims())
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	var sleeps []time.Duration
	e.Now = func() time.Time { return now }
	e.Sleep = func(ctx context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		now = now.Add(d)
		return nil
	}
	j := job("u")
	j.ParkingID = "P"
	j.NotBefore = now.Add(10 * time.Minute)
	r := e.Run(context.Background(), j)
	if r.Status != model.StatusPaid {
		t.Fatalf("%+v", r)
	}
	want := "booking:P,checkout:a,reserve:a"
	if got := strings.Join(a.events, ","); got != want {
		t.Fatalf("events %s, want %s", got, want)
	}
	if len(sleeps) < 2 || sleeps[0] != 10*time.Minute-WarmLead || sleeps[1] != WarmLead {
		t.Fatal(fmt.Sprint(sleeps))
	}
}

// slowWarm blocks BookingContext until its ctx ends and records when the
// reservation started.
type slowWarm struct {
	*ctxAPI
	warmEnd   chan time.Time
	reserveAt time.Time
}

func (s *slowWarm) BookingContext(ctx context.Context, jwt, p string, now time.Time) error {
	<-ctx.Done()
	s.warmEnd <- time.Now()
	return ctx.Err()
}
func (s *slowWarm) Reserve(ctx context.Context, jwt, spotID string, st, en time.Time, token, v string) (string, error) {
	s.mu.Lock()
	s.reserveAt = time.Now()
	s.mu.Unlock()
	return s.fakeAPI.Reserve(ctx, jwt, spotID, st, en, token, v)
}

func TestHangingWarmUpDoesNotDelayOrCancelAtRelease(t *testing.T) {
	a := &slowWarm{ctxAPI: &ctxAPI{fakeAPI: &fakeAPI{spots: spots3[:1], status: 2}}, warmEnd: make(chan time.Time, 1)}
	e := noSleep(newEng(a, &fakeTok{}, cfg(), NewClaims()))
	e.Sleep = func(ctx context.Context, d time.Duration) error { time.Sleep(d); return ctx.Err() }
	j := job("u")
	j.NotBefore = time.Now().Add(500 * time.Millisecond)
	r := e.Run(context.Background(), j)
	if r.Status != model.StatusPaid {
		t.Fatalf("%+v", r)
	}
	a.mu.Lock()
	late := a.reserveAt.Sub(j.NotBefore)
	a.mu.Unlock()
	if late < 0 || late > 200*time.Millisecond {
		t.Fatalf("reserve %v after release", late)
	}
	select {
	case end := <-a.warmEnd:
		t.Fatalf("warm-up cancelled at %v (release %v)", end, j.NotBefore)
	default: // still running past the release, as intended
	}
}

func TestLateStartWarmUpInBackground(t *testing.T) {
	a := &slowWarm{ctxAPI: &ctxAPI{fakeAPI: &fakeAPI{spots: spots3[:1], status: 2}}, warmEnd: make(chan time.Time, 1)}
	e := noSleep(newEng(a, &fakeTok{}, cfg(), NewClaims()))
	j := job("u")
	j.NotBefore = time.Now().Add(-time.Minute)
	t0 := time.Now()
	r := e.Run(context.Background(), j)
	if r.Status != model.StatusPaid || time.Since(t0) > time.Second {
		t.Fatalf("%+v took %v", r, time.Since(t0))
	}
}

// hangCheckout never finishes CheckoutContext before its ctx ends.
type hangCheckout struct{ *ctxAPI }

func (h *hangCheckout) CheckoutContext(ctx context.Context, jwt, p, spot string, s, e time.Time) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestHangingCheckoutDelaysReserveAtMostCheckoutWait(t *testing.T) {
	a := &hangCheckout{&ctxAPI{fakeAPI: &fakeAPI{spots: spots3[:1], status: 2}}}
	e := noSleep(newEng(a, &fakeTok{}, cfg(), NewClaims()))
	t0 := time.Now()
	r := e.Run(context.Background(), job("u"))
	if r.Status != model.StatusPaid || time.Since(t0) > checkoutWait+300*time.Millisecond {
		t.Fatalf("%+v took %v", r, time.Since(t0))
	}
}

// hangPrePay blocks Payments/amount and start-payment until their ctx ends.
type hangPrePay struct{ *fakeAPI }

func (h *hangPrePay) PaymentAmount(ctx context.Context, jwt, r string) (model.Amount, error) {
	<-ctx.Done()
	return model.Amount{}, ctx.Err()
}
func (h *hangPrePay) StartPayment(ctx context.Context, jwt, o string, a float64, r string) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

func TestHangingPrePayStepsStillChargeCardWithinBudget(t *testing.T) {
	a := &hangPrePay{&fakeAPI{spots: spots3, status: 2}}
	e := newEng(a, &fakeTok{}, cfg(), NewClaims())
	e.Sleep = func(ctx context.Context, d time.Duration) error {
		select {
		case <-time.After(d):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	e.Amount = func() float64 { return 15 }
	t0 := time.Now()
	r := e.Run(context.Background(), job("u"))
	if r.Status != model.StatusPaid || len(a.amounts) != 1 || a.amounts[0] != 15 || time.Since(t0) > prePayBudget+time.Second {
		t.Fatalf("%+v %v took %v", r, a.amounts, time.Since(t0))
	}
}

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"spotkeeper/internal/model"
)

const testURL = "https://app.example.com/turnstile?v=5#sid=x"

type fakeAPI struct {
	mu        sync.Mutex
	spots     []model.Spot
	reserveFn func(n int, spotID, token string) (string, error)
	reserves  []string
	tokens    []string
	reserveAt []time.Time
	status    int
	amountErr error
	amount    model.Amount
	amountN   int
	amounts   []float64
	startErr  error
	starts    []string // "orderID amount reservationID" per StartPayment call
	payCalls  []string // payment call order: amount, start, card
}

func (f *fakeAPI) Offers(ctx context.Context, jwt, p string, s, e time.Time) (model.Offers, error) {
	return model.Offers{Spots: f.spots, TurnstileURL: testURL}, nil
}
func (f *fakeAPI) Reserve(ctx context.Context, jwt, spotID string, s, e time.Time, token, v string) (string, error) {
	f.mu.Lock()
	n := len(f.reserves)
	f.reserves = append(f.reserves, spotID)
	f.tokens = append(f.tokens, token)
	f.reserveAt = append(f.reserveAt, time.Now())
	fn := f.reserveFn
	f.mu.Unlock()
	if fn != nil {
		return fn(n, spotID, token)
	}
	return "R1", nil
}
func (f *fakeAPI) PaymentAmount(ctx context.Context, jwt, r string) (model.Amount, error) {
	f.mu.Lock()
	f.amountN++
	f.payCalls = append(f.payCalls, "amount")
	f.mu.Unlock()
	return f.amount, f.amountErr
}
func (f *fakeAPI) StartPayment(ctx context.Context, jwt, orderID string, a float64, r string) (string, error) {
	f.mu.Lock()
	f.starts = append(f.starts, fmt.Sprintf("%s %v %s", orderID, a, r))
	f.payCalls = append(f.payCalls, "start")
	f.mu.Unlock()
	return "S1", f.startErr
}
func (f *fakeAPI) StartCardPayment(ctx context.Context, jwt, c string, a float64, r, ip string) (string, error) {
	f.mu.Lock()
	f.amounts = append(f.amounts, a)
	f.payCalls = append(f.payCalls, "card")
	f.mu.Unlock()
	return "P1", nil
}
func (f *fakeAPI) PaymentStatus(ctx context.Context, jwt, p string) (int, error) {
	return f.status, nil
}

type fakeTok struct {
	mu    sync.Mutex
	n     int
	times []time.Time
	ids   []string
}

func (f *fakeTok) Token(ctx context.Context, u, id string) (model.Token, error) {
	f.mu.Lock()
	f.n++
	n := f.n
	f.times = append(f.times, time.Now())
	f.ids = append(f.ids, id)
	f.mu.Unlock()
	return model.Token{Value: fmt.Sprintf("tok%d", n), ObservedAt: time.Now()}, nil
}

func cfg() model.EngineConfig {
	return model.EngineConfig{MaxAttempts: 10, PayPolls: 3, TurnstilePrefix: "https://app.example.com/turnstile?",
		PollInterval: 5 * time.Millisecond, Budget: 2 * time.Second, PayInterval: time.Millisecond}
}

var spots3 = []model.Spot{{ID: "a", Level: "-1"}, {ID: "b", Level: "-1"}, {ID: "c", Level: "-1"}}

func job(login string) model.Job {
	return model.Job{Login: login, Day: "2026-10-26", JWT: "j", CardID: "c"}
}

func newEng(a API, t Tokener, c model.EngineConfig, cl *Claims) *Engine {
	e := New(a, t, c, cl)
	e.Rand = rand.New(rand.NewPCG(1, 2))
	return e
}

func TestPaid(t *testing.T) {
	a := &fakeAPI{spots: spots3, status: 2}
	r := newEng(a, &fakeTok{}, cfg(), NewClaims()).Run(context.Background(), job("u"))
	if r.Status != model.StatusPaid || r.Attempts != 1 || r.PaymentID != "P1" || r.ReservationID != "R1" {
		t.Fatalf("%+v", r)
	}
}

func TestTokenUsedOnce(t *testing.T) {
	a := &fakeAPI{spots: spots3, status: 2}
	a.reserveFn = func(n int, _, _ string) (string, error) {
		if n < 2 {
			return "", &model.APIError{Code: 5}
		}
		return "R1", nil
	}
	c := cfg()
	c.MaxAttempts = 10
	e := newEng(a, &fakeTok{}, c, NewClaims())
	e.Sleep = func(ctx context.Context, d time.Duration) error {
		if d == failBackoff {
			d = time.Millisecond
		}
		return sleepCtx(ctx, d)
	}
	r := e.Run(context.Background(), job("u"))
	if r.Status != model.StatusPaid || r.Attempts != 3 {
		t.Fatalf("%+v", r)
	}
	seen := map[string]bool{}
	for _, tk := range a.tokens {
		if seen[tk] {
			t.Fatalf("token reused: %v", a.tokens)
		}
		seen[tk] = true
	}
	// the original workflow re-picks randomly among all spots, a failed one included: the
	// failed spot must be claimable again (3 attempts over 3 spots, with a
	// fixed seed, hit c twice).
	if len(a.reserves) != 3 {
		t.Fatalf("reserves: %v", a.reserves)
	}
}

func TestFailedSpotReleasedForRetry(t *testing.T) {
	a := &scriptAPI{fakeAPI: fakeAPI{spots: []model.Spot{{ID: "only"}}, status: 2}}
	a.reserveFn = func(n int, _, _ string) (string, error) {
		if n == 0 {
			return "", &model.APIError{Code: 5}
		}
		return "R1", nil
	}
	cl := NewClaims()
	e := newEng(a, &urlTok{api: a}, cfg(), cl)
	recSleep(e)
	r := e.Run(context.Background(), job("u"))
	if r.Status != model.StatusPaid || r.Attempts != 2 || len(a.reserves) != 2 || a.reserves[1] != "only" {
		t.Fatalf("%+v %v", r, a.reserves)
	}
}

type flakyPay struct {
	*scriptAPI
	startFail, statusFail int
	starts, statuses      int
}

func (f *flakyPay) StartCardPayment(ctx context.Context, jwt, c string, a float64, r, ip string) (string, error) {
	f.starts++
	if f.starts <= f.startFail {
		return "", errors.New("boom")
	}
	return f.scriptAPI.StartCardPayment(ctx, jwt, c, a, r, ip)
}

func (f *flakyPay) PaymentStatus(ctx context.Context, jwt, p string) (int, error) {
	f.statuses++
	if f.statuses <= f.statusFail {
		return 0, errors.New("boom")
	}
	return f.scriptAPI.PaymentStatus(ctx, jwt, p)
}

func TestPaymentCallsRetried5xLike(t *testing.T) {
	// retry maxTries 5, 1 s apart, on payment start and status.
	s := &scriptAPI{fakeAPI: fakeAPI{spots: spots3, status: 2}}
	f := &flakyPay{scriptAPI: s, startFail: 4, statusFail: 4}
	e := newEng(f, &urlTok{api: s}, cfg(), NewClaims())
	ds := recSleep(e)
	r := e.Run(context.Background(), job("u"))
	if r.Status != model.StatusPaid || f.starts != 5 || f.statuses != 5 {
		t.Fatalf("%+v starts=%d statuses=%d", r, f.starts, f.statuses)
	}
	n := 0
	for _, d := range *ds {
		if d == time.Second {
			n++
		}
	}
	if n != 8 {
		t.Fatalf("1s retry waits: %d (%v)", n, *ds)
	}
	g := &flakyPay{scriptAPI: &scriptAPI{fakeAPI: fakeAPI{spots: spots3, status: 2}}, startFail: 5}
	e = newEng(g, &urlTok{api: g.scriptAPI}, cfg(), NewClaims())
	recSleep(e)
	r = e.Run(context.Background(), job("u"))
	if r.Status != model.StatusUnconfirmed || g.starts != 5 {
		t.Fatalf("%+v starts=%d", r, g.starts)
	}
}

func TestCode106Backoff(t *testing.T) {
	a := &fakeAPI{spots: spots3, status: 2}
	a.reserveFn = func(n int, _, _ string) (string, error) {
		if n == 0 {
			return "", &model.APIError{Code: 106}
		}
		return "R1", nil
	}
	e := newEng(a, &fakeTok{}, cfg(), NewClaims())
	var mu sync.Mutex
	var sleeps []time.Duration
	e.Sleep = func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		sleeps = append(sleeps, d)
		mu.Unlock()
		if d >= time.Second {
			return nil
		}
		return sleepCtx(ctx, d)
	}
	r := e.Run(context.Background(), job("u"))
	if r.Status != model.StatusPaid || r.Attempts != 2 {
		t.Fatalf("%+v", r)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, d := range sleeps {
		if d == 3*time.Second {
			return
		}
	}
	t.Fatalf("no 3s sleep: %v", sleeps)
}

func TestSiblingsNoCollision(t *testing.T) {
	a := &fakeAPI{spots: []model.Spot{{ID: "a"}, {ID: "b"}}, status: 2}
	cl := NewClaims()
	var wg sync.WaitGroup
	res := make([]model.Result, 2)
	for i := range res {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res[i] = New(a, &fakeTok{}, cfg(), cl).Run(context.Background(), job(fmt.Sprint("u", i)))
		}()
	}
	wg.Wait()
	if res[0].Status != model.StatusPaid || res[1].Status != model.StatusPaid || res[0].SpotID == res[1].SpotID {
		t.Fatalf("%+v", res)
	}
}

func TestCancel(t *testing.T) {
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(30 * time.Millisecond); cancel() }()
	r := newEng(&fakeAPI{}, &fakeTok{}, cfg(), NewClaims()).Run(ctx, job("u"))
	if r.Status != model.StatusCancelled {
		t.Fatalf("%+v", r)
	}
	time.Sleep(20 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutine leak: %d -> %d", before, after)
	}
}

type countAPI struct {
	fakeAPI
	n atomic.Int64
}

func (c *countAPI) Offers(ctx context.Context, j, p string, s, e time.Time) (model.Offers, error) {
	c.n.Add(1)
	return model.Offers{}, nil
}

func TestZeroPollIntervalNoSpin(t *testing.T) {
	c := cfg()
	c.PollInterval = 0
	c.Budget = 100 * time.Millisecond
	c.MaxAttempts = 1000
	a := &countAPI{}
	r := newEng(a, &fakeTok{}, c, NewClaims()).Run(context.Background(), job("u"))
	if r.Status != model.StatusFailed || a.n.Load() > 100 {
		t.Fatalf("%+v offers=%d", r, a.n.Load())
	}
}

func TestUnconfirmed(t *testing.T) {
	a := &fakeAPI{spots: spots3, status: 1}
	r := newEng(a, &fakeTok{}, cfg(), NewClaims()).Run(context.Background(), job("u"))
	if r.Status != model.StatusUnconfirmed || r.ReservationID != "R1" {
		t.Fatalf("%+v", r)
	}
}

func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, ln := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if ln == "" {
			continue
		}
		m := map[string]any{}
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatalf("bad json %q", ln)
		}
		out = append(out, m)
	}
	return out
}

func TestLogsReservationResult(t *testing.T) {
	a := &fakeAPI{spots: spots3, status: 2}
	var buf bytes.Buffer
	e := newEng(a, &fakeTok{}, cfg(), NewClaims())
	e.Log = slog.New(slog.NewJSONHandler(&buf, nil))
	e.Run(context.Background(), job("u"))
	n := 0
	for _, m := range logLines(t, &buf) {
		if m["msg"] != "reservation_result" {
			continue
		}
		n++
		if m["status"] != model.StatusPaid || m["attempts"].(float64) != 1 ||
			m["first_reserve_ms"].(float64) < 0 || m["tokens_minted"].(float64) < 1 || m["level"] != "INFO" {
			t.Fatalf("%v", m)
		}
	}
	if n != 1 {
		t.Fatalf("results=%d", n)
	}
	for _, tk := range a.tokens {
		if strings.Contains(buf.String(), tk) {
			t.Fatal("token leaked")
		}
	}
	if strings.Contains(buf.String(), "turnstile?") {
		t.Fatal("url leaked")
	}
}

func TestLogsAttemptOnFailure(t *testing.T) {
	a := &fakeAPI{spots: spots3, status: 2}
	a.reserveFn = func(n int, _, _ string) (string, error) {
		if n == 0 {
			return "", &model.APIError{Code: 106, Message: "busy"}
		}
		return "R1", nil
	}
	var buf bytes.Buffer
	e := newEng(a, &fakeTok{}, cfg(), NewClaims())
	e.Sleep = func(context.Context, time.Duration) error { return nil }
	e.Log = slog.New(slog.NewJSONHandler(&buf, nil))
	e.Run(context.Background(), job("u"))
	var at []map[string]any
	for _, m := range logLines(t, &buf) {
		if m["msg"] == "reservation_attempt" {
			at = append(at, m)
		}
	}
	if len(at) != 2 || at[0]["ok"] != false || at[0]["err_code"].(float64) != 106 || at[1]["ok"] != true {
		t.Fatalf("%v", at)
	}
}

// scriptAPI returns distinct turnstile URLs per Offers call and records the call order.
type scriptAPI struct {
	fakeAPI
	evMu    sync.Mutex
	events  []string
	offerN  int
	noSpots bool
	url     string
	payN    int
}

func (a *scriptAPI) Offers(ctx context.Context, jwt, p string, s, e time.Time) (model.Offers, error) {
	a.evMu.Lock()
	defer a.evMu.Unlock()
	a.offerN++
	a.events = append(a.events, fmt.Sprintf("offers%d", a.offerN))
	if a.noSpots {
		return model.Offers{}, nil
	}
	u := a.url
	if u == "" {
		u = fmt.Sprintf("%s&n=%d", testURL, a.offerN)
	}
	return model.Offers{Spots: a.spots, TurnstileURL: u}, nil
}

func (a *scriptAPI) Reserve(ctx context.Context, jwt, spotID string, s, e time.Time, token, v string) (string, error) {
	a.evMu.Lock()
	a.events = append(a.events, "reserve:"+token)
	a.evMu.Unlock()
	return a.fakeAPI.Reserve(ctx, jwt, spotID, s, e, token, v)
}

func (a *scriptAPI) PaymentStatus(ctx context.Context, jwt, p string) (int, error) {
	a.evMu.Lock()
	a.payN++
	a.evMu.Unlock()
	return a.fakeAPI.PaymentStatus(ctx, jwt, p)
}

type urlTok struct {
	api *scriptAPI
	err error
	mu  sync.Mutex
	got []string
	at  []time.Time
}

func (t *urlTok) Token(ctx context.Context, u, id string) (model.Token, error) {
	t.mu.Lock()
	t.at = append(t.at, time.Now())
	t.got = append(t.got, u)
	n := len(t.got)
	t.mu.Unlock()
	t.api.evMu.Lock()
	t.api.events = append(t.api.events, "token:"+u)
	t.api.evMu.Unlock()
	if t.err != nil {
		return model.Token{}, t.err
	}
	return model.Token{Value: fmt.Sprintf("tok%d", n), ObservedAt: time.Now()}, nil
}

func recSleep(e *Engine) *[]time.Duration {
	var mu sync.Mutex
	var ds []time.Duration
	e.Sleep = func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		ds = append(ds, d)
		mu.Unlock()
		return ctx.Err()
	}
	return &ds
}

func TestNoTokenBeforeNotBefore(t *testing.T) {
	a := &scriptAPI{fakeAPI: fakeAPI{spots: spots3, status: 2}}
	tk := &urlTok{api: a}
	j := job("u")
	j.NotBefore = time.Now().Add(150 * time.Millisecond)
	r := newEng(a, tk, cfg(), NewClaims()).Run(context.Background(), j)
	if r.Status != model.StatusPaid {
		t.Fatalf("%+v", r)
	}
	if tk.at[0].Before(j.NotBefore) || a.reserveAt[0].Before(j.NotBefore) {
		t.Fatal("token or reserve before NotBefore")
	}
}

func TestTokenUsesURLFromSameOffersResponse(t *testing.T) {
	a := &scriptAPI{fakeAPI: fakeAPI{spots: spots3, status: 2}}
	a.reserveFn = func(n int, _, _ string) (string, error) {
		if n < 2 {
			return "", &model.APIError{Code: 5}
		}
		return "R1", nil
	}
	tk := &urlTok{api: a}
	e := newEng(a, tk, cfg(), NewClaims())
	recSleep(e)
	r := e.Run(context.Background(), job("u"))
	if r.Status != model.StatusPaid || r.Attempts != 3 {
		t.Fatalf("%+v", r)
	}
	want := []string{"offers1", "token:" + testURL + "&n=1", "reserve:tok1",
		"offers2", "token:" + testURL + "&n=2", "reserve:tok2",
		"offers3", "token:" + testURL + "&n=3", "reserve:tok3"}
	if fmt.Sprint(a.events) != fmt.Sprint(want) {
		t.Fatalf("events %v", a.events)
	}
}

func TestEmptyPollsCountAsAttempts(t *testing.T) {
	a := &scriptAPI{fakeAPI: fakeAPI{}, noSpots: true}
	e := newEng(a, &urlTok{api: a}, cfg(), NewClaims())
	recSleep(e)
	r := e.Run(context.Background(), job("u"))
	if r.Status != model.StatusFailed || r.Err != "Reservation failed after 10 attempts" || a.offerN != 10 {
		t.Fatalf("%+v offers=%d", r, a.offerN)
	}
}

func TestNonRetryableTokenStops(t *testing.T) {
	a := &scriptAPI{fakeAPI: fakeAPI{spots: spots3}}
	tk := &urlTok{api: a, err: model.TokenError{Code: "blocked"}}
	r := newEng(a, tk, cfg(), NewClaims()).Run(context.Background(), job("u"))
	if r.Status != model.StatusFailed || r.Err != "Context verification failed: blocked" || a.offerN != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestRetryableTokenWaits1s(t *testing.T) {
	a := &scriptAPI{fakeAPI: fakeAPI{spots: spots3}}
	tk := &urlTok{api: a, err: &model.TokenError{Code: "busy", Retryable: true}}
	c := cfg()
	c.MaxAttempts = 3
	e := newEng(a, tk, c, NewClaims())
	ds := recSleep(e)
	r := e.Run(context.Background(), job("u"))
	if r.Status != model.StatusFailed || len(*ds) != 3 || (*ds)[0] != time.Second || a.offerN != 3 {
		t.Fatalf("%+v %v", r, *ds)
	}
}

func TestCode106Waits3s(t *testing.T) {
	a := &scriptAPI{fakeAPI: fakeAPI{spots: spots3, status: 2}}
	a.reserveFn = func(n int, _, _ string) (string, error) {
		if n == 0 {
			return "", &model.APIError{Code: 106}
		}
		return "R1", nil
	}
	e := newEng(a, &urlTok{api: a}, cfg(), NewClaims())
	ds := recSleep(e)
	r := e.Run(context.Background(), job("u"))
	if r.Status != model.StatusPaid || len(*ds) < 1 || (*ds)[0] != 3*time.Second {
		t.Fatalf("%+v %v", r, *ds)
	}
}

func TestOtherReserveErrorWaits500ms(t *testing.T) {
	a := &scriptAPI{fakeAPI: fakeAPI{spots: spots3, status: 2}}
	a.reserveFn = func(n int, _, _ string) (string, error) {
		if n == 0 {
			return "", &model.APIError{Code: 5}
		}
		return "R1", nil
	}
	e := newEng(a, &urlTok{api: a}, cfg(), NewClaims())
	ds := recSleep(e)
	e.Run(context.Background(), job("u"))
	if len(*ds) < 1 || (*ds)[0] != 500*time.Millisecond {
		t.Fatalf("%v", *ds)
	}
}

func TestMissingTurnstileURLFails(t *testing.T) {
	a := &scriptAPI{fakeAPI: fakeAPI{spots: spots3}, url: "http://elsewhere/x"}
	tk := &urlTok{api: a}
	r := newEng(a, tk, cfg(), NewClaims()).Run(context.Background(), job("u"))
	if r.Status != model.StatusFailed || r.Err != "the service did not issue a contextual verification URL" || len(tk.got) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestPaymentPolls2sUpTo20(t *testing.T) {
	a := &scriptAPI{fakeAPI: fakeAPI{spots: spots3, status: 1}}
	c := cfg()
	c.PayPolls = 20
	c.PayInterval = 2 * time.Second
	e := newEng(a, &urlTok{api: a}, c, NewClaims())
	ds := recSleep(e)
	r := e.Run(context.Background(), job("u"))
	if r.Status != model.StatusUnconfirmed || a.payN != 20 || len(*ds) != 19 {
		t.Fatalf("%+v polls=%d sleeps=%d", r, a.payN, len(*ds))
	}
	for _, d := range *ds {
		if d != 2*time.Second {
			t.Fatal(*ds)
		}
	}
}

func TestPaymentAmount15(t *testing.T) {
	a := &scriptAPI{fakeAPI: fakeAPI{spots: spots3, status: 2}}
	c := cfg()
	c.PaymentAmount = 15
	newEng(a, &urlTok{api: a}, c, NewClaims()).Run(context.Background(), job("u"))
	if len(a.amounts) != 1 || a.amounts[0] != 15 {
		t.Fatal(a.amounts)
	}
}

func TestBudgetExhausted(t *testing.T) {
	a := &scriptAPI{fakeAPI: fakeAPI{}, noSpots: true}
	c := cfg()
	c.MaxAttempts = 1000
	c.Budget = 50 * time.Millisecond
	r := newEng(a, &urlTok{api: a}, c, NewClaims()).Run(context.Background(), job("u"))
	if r.Status != model.StatusFailed || r.Err != "budget exhausted" {
		t.Fatalf("%+v", r)
	}
}

func TestPayUsesAmountFunc(t *testing.T) {
	a := &scriptAPI{fakeAPI: fakeAPI{spots: spots3, status: 2}}
	c := cfg()
	c.PaymentAmount = 15
	e := newEng(a, &urlTok{api: a}, c, NewClaims())
	e.Amount = func() float64 { return 12.5 }
	e.Run(context.Background(), job("u"))
	if len(a.amounts) != 1 || a.amounts[0] != 12.5 {
		t.Fatal(a.amounts)
	}
}

package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"spotkeeper/internal/model"
)

type reloginAPI struct {
	*fakeAPI
	mu2  sync.Mutex
	jwts []string
	n401 int // how many leading Reserve calls return 401
	call int
}

func (r *reloginAPI) Reserve(ctx context.Context, jwt, spotID string, s, e time.Time, token, v string) (string, error) {
	r.mu2.Lock()
	r.jwts = append(r.jwts, jwt)
	r.call++
	c := r.call
	r.mu2.Unlock()
	if c <= r.n401 {
		return "", &model.APIError{HTTPStatus: 401, Message: "expired"}
	}
	return "R1", nil
}

func reloginEng(a API, calls *int) *Engine {
	e := newEng(a, &fakeTok{}, cfg(), NewClaims())
	e.Sleep = func(ctx context.Context, d time.Duration) error { return nil }
	if calls != nil {
		e.Relogin = func(ctx context.Context, login string) (string, error) {
			*calls++
			return "new", nil
		}
	}
	return e
}

func TestReloginOn401(t *testing.T) {
	a := &reloginAPI{fakeAPI: &fakeAPI{spots: spots3, status: 2}, n401: 1}
	calls := 0
	r := reloginEng(a, &calls).Run(context.Background(), job("u"))
	if r.Status != model.StatusPaid || r.Attempts != 1 || calls != 1 {
		t.Fatalf("%+v calls=%d", r, calls)
	}
	if len(a.jwts) != 2 || a.jwts[0] != "j" || a.jwts[1] != "new" {
		t.Fatalf("jwts %v", a.jwts)
	}
}

func TestSecond401Fails(t *testing.T) {
	a := &reloginAPI{fakeAPI: &fakeAPI{spots: spots3, status: 2}, n401: 2}
	calls := 0
	r := reloginEng(a, &calls).Run(context.Background(), job("u"))
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
	// second 401 is a normal failure (counted attempt); the next attempt succeeds
	if r.Status != model.StatusPaid || r.Attempts != 2 {
		t.Fatalf("%+v", r)
	}
	if len(a.jwts) != 3 || a.jwts[1] != "new" || a.jwts[2] != "new" {
		t.Fatalf("jwts %v", a.jwts)
	}
}

func TestNoReloginWhenNil(t *testing.T) {
	a := &reloginAPI{fakeAPI: &fakeAPI{spots: spots3, status: 2}, n401: 1}
	r := reloginEng(a, nil).Run(context.Background(), job("u"))
	if r.Status != model.StatusPaid || r.Attempts != 2 {
		t.Fatalf("%+v", r)
	}
	for _, j := range a.jwts {
		if j != "j" {
			t.Fatalf("jwts %v", a.jwts)
		}
	}
}

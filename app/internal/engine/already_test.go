package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"spotkeeper/internal/model"
)

// Code 106 twice in a row means the user already holds a reservation for that time:
// stop instead of burning all attempts (and tokens).
func TestRepeated106MeansAlreadyBooked(t *testing.T) {
	a := &fakeAPI{spots: spots3, status: 2}
	a.reserveFn = func(int, string, string) (string, error) {
		return "", &model.APIError{Code: 106, Message: "Jednoczesna rezerwacja wielu miejsc"}
	}
	e := newEng(a, &fakeTok{}, cfg(), NewClaims())
	e.Sleep = func(context.Context, time.Duration) error { return nil }
	r := e.Run(context.Background(), job("u"))
	if r.Status != model.StatusSkipped || !r.AlreadyBooked || r.Attempts != 2 || r.Err == "" {
		t.Fatalf("%+v", r)
	}
}

// A single 106 (or 106s split by other errors) keeps the old transient handling.
func TestNonConsecutive106KeepsTrying(t *testing.T) {
	a := &fakeAPI{spots: spots3, status: 2}
	a.reserveFn = func(n int, _, _ string) (string, error) {
		switch n {
		case 0, 2:
			return "", &model.APIError{Code: 106}
		case 1:
			return "", errors.New("timeout")
		}
		return "R1", nil
	}
	e := newEng(a, &fakeTok{}, cfg(), NewClaims())
	e.Sleep = func(context.Context, time.Duration) error { return nil }
	r := e.Run(context.Background(), job("u"))
	if r.Status != model.StatusPaid || r.AlreadyBooked || r.Attempts != 4 {
		t.Fatalf("%+v", r)
	}
}

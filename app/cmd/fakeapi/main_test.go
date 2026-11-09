package main

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"spotkeeper/internal/api"
	"spotkeeper/internal/timeutil"
	"spotkeeper/internal/turnstile"
)

// TestRealClientsAgainstFake drives the production clients through the fake so the demo stays compatible.
func TestRealClientsAgainstFake(t *testing.T) {
	s := httptest.NewServer(newServer(10*time.Millisecond, 2).routes())
	defer s.Close()
	ctx := context.Background()
	hc := api.NewHTTPClient()

	pc := api.New(s.URL+"/pc/api", "4.7.1", hc)
	sess, err := pc.Login(ctx, "jan.kowalski@example.com", "pw")
	if err != nil || sess.Exp.Before(time.Now()) {
		t.Fatalf("login: %+v %v", sess, err)
	}
	vs, err := pc.Vehicles(ctx, sess.JWT, "")
	if err != nil || len(vs) != len(plates) || vs[0].Plate != "WX 1234A" {
		t.Fatalf("vehicles: %+v %v", vs, err)
	}
	day := timeutil.AddDays(timeutil.Today(time.Now()), 3)
	start, end, _ := timeutil.DayWindow(day)
	offers, err := pc.Offers(ctx, sess.JWT, "p", start, end)
	if err != nil || len(offers.Spots) != 6 || offers.TurnstileURL == "" {
		t.Fatalf("offers: %+v %v", offers, err)
	}
	free, err := pc.Timeline(ctx, sess.JWT, "p", start)
	if err != nil || len(free) != 7 || free[0].Day != day || free[0].Free != 2 {
		t.Fatalf("timeline: %+v %v", free, err)
	}

	if err := pc.BookingContext(ctx, sess.JWT, "p", time.Now()); err != nil {
		t.Fatalf("booking context: %v", err)
	}
	if err := pc.CheckoutContext(ctx, sess.JWT, "p", offers.Spots[0].ID, start, end); err != nil {
		t.Fatalf("checkout context: %v", err)
	}
	if v2, err := pc.Vehicles(ctx, sess.JWT, "p"); err != nil || len(v2) != len(plates) {
		t.Fatalf("vehicles v2: %+v %v", v2, err)
	}

	tok, err := turnstile.New(s.URL+"/ts", "https://app.example.com/turnstile?", 2*time.Minute, hc).Token(ctx, offers.TurnstileURL, "r1")
	if err != nil || tok.Value == "" {
		t.Fatalf("token: %+v %v", tok, err)
	}
	res, err := pc.Reserve(ctx, sess.JWT, offers.Spots[0].ID, start, end, tok.Value, vs[0].ID)
	if err != nil || res == "" {
		t.Fatalf("reserve: %q %v", res, err)
	}
	amt, err := pc.PaymentAmount(ctx, sess.JWT, res)
	if err != nil || amt.Total != 15 || amt.Free {
		t.Fatalf("amount: %+v %v", amt, err)
	}
	if _, err := pc.StartPayment(ctx, sess.JWT, "6a496b6a0f9e49528a1709799884f9b1", amt.Total, res); err != nil {
		t.Fatalf("start payment: %v", err)
	}
	pay, err := pc.StartCardPayment(ctx, sess.JWT, "card1", 15, res, "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if st, err := pc.PaymentStatus(ctx, sess.JWT, pay); err != nil || st != 2 {
		t.Fatalf("status: %d %v", st, err)
	}
}

// TestFakeParkingsAndLevels drives the new picker and level calls through the real client.
func TestFakeParkingsAndLevels(t *testing.T) {
	s := httptest.NewServer(newServer(10*time.Millisecond, 2).routes())
	defer s.Close()
	ctx := context.Background()
	pc := api.New(s.URL+"/pc/api", "4.7.1", api.NewHTTPClient())
	ps, err := pc.ParkingsList(ctx, "j")
	if err != nil || len(ps) != 2 || ps[0].Name != "Parking Testowy" || ps[1].Street != "Wzorcowa 22B" {
		t.Fatalf("parkings: %+v %v", ps, err)
	}
	lv, err := pc.ParkingLevels(ctx, "j", ps[0].ID, time.Now())
	if err != nil || len(lv) != 2 {
		t.Fatalf("levels: %v %v", lv, err)
	}
	none := httptest.NewServer(newServer(10*time.Millisecond, 0).routes())
	defer none.Close()
	lv, err = api.New(none.URL+"/pc/api", "4.7.1", api.NewHTTPClient()).ParkingLevels(ctx, "j", ps[0].ID, time.Now())
	if err != nil || len(lv) != 0 {
		t.Fatalf("levels without free spots: %v %v", lv, err)
	}
}

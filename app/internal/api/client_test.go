package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"spotkeeper/internal/model"
)

func newTest(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(srv.URL+"/api", "4.7.1", srv.Client())
}

func jwtWithExp(exp int64) string {
	p := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":` + strconv.FormatInt(exp, 10) + `}`))
	return "h." + p + ".s"
}

func TestEnvelopeFalse200(t *testing.T) {
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"isSuccess":false,"error":{"code":7,"message":"nope"}}`)
	})
	_, err := c.Vehicles(context.Background(), "j")
	var ae *model.APIError
	if !errors.As(err, &ae) || ae.Code != 7 || ae.HTTPStatus != 200 || ae.Message != "nope" {
		t.Fatalf("got %#v", err)
	}
}

func TestReserve400(t *testing.T) {
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		io.WriteString(w, `{"isSuccess":false,"error":{"code":106,"message":"x"}}`)
	})
	_, err := c.Reserve(context.Background(), "j", "s", time.Now(), time.Now(), "tok", "v")
	var ae *model.APIError
	if !errors.As(err, &ae) || ae.HTTPStatus != 400 || ae.Code != 106 {
		t.Fatalf("got %#v", err)
	}
}

func TestLoginExp(t *testing.T) {
	var body map[string]string
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/Sessions/native" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&body)
		io.WriteString(w, `{"isSuccess":true,"result":{"jwt":"`+jwtWithExp(1793000000)+`"}}`)
	})
	s, err := c.Login(context.Background(), "u", "p")
	if err != nil || s.Exp.Unix() != 1793000000 || body["login"] != "u" || body["password"] != "p" {
		t.Fatalf("%v %v %v", s, err, body)
	}
}

func TestHeaders(t *testing.T) {
	n := 0
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		n++
		if r.Header.Get("x-pc-mobile-app-version") != "4.7.1" || r.Header.Get("Accept-Language") != "pl" ||
			r.Header.Get("Accept") != "application/json" {
			t.Errorf("headers %v for %s", r.Header, r.URL.Path)
		}
		if r.URL.Path != "/api/Sessions/native" && r.Header.Get("Authorization") != "Bearer jj" {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		io.WriteString(w, `{"isSuccess":true,"result":{"jwt":"`+jwtWithExp(1)+`","id":"x","status":2,"entries":[],"data":{"totalPrice":1}}}`)
	})
	ctx := context.Background()
	now := time.Now()
	c.Login(ctx, "a", "b")
	c.Vehicles(ctx, "jj")
	c.CreditCards(ctx, "jj")
	c.Offers(ctx, "jj", "P", now, now)
	c.Timeline(ctx, "jj", "P", now)
	c.Reserve(ctx, "jj", "s", now, now, "t", "v")
	c.PaymentAmount(ctx, "jj", "r")
	c.ParkingsList(ctx, "jj")
	c.StartCardPayment(ctx, "jj", "c", 1, "r", "1.1.1.1")
	c.PaymentStatus(ctx, "jj", "p")
	if n != 10 {
		t.Fatalf("calls %d", n)
	}
}

func TestTimelineWarsawDay(t *testing.T) {
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("ParkingId") != "P" || q.Get("Start") != "2026-10-24T08:05:06.007Z" {
			t.Errorf("query %v", q)
		}
		io.WriteString(w, `{"isSuccess":true,"result":{"entries":[{"day":"2026-10-24T22:00:00+00:00","freePlacesCount":3},{"day":"2026-10-25T23:00:00+00:00","freePlacesCount":0}]}}`)
	})
	start := time.Date(2026, 10, 24, 10, 5, 6, 7e6, time.FixedZone("x", 7200))
	d, err := c.Timeline(context.Background(), "j", "P", start)
	if err != nil || len(d) != 2 || d[0] != (model.DayFree{Day: "2026-10-25", Free: 3}) || d[1].Day != "2026-10-26" {
		t.Fatalf("%v %v", d, err)
	}
}

func TestOffersVehiclesCards(t *testing.T) {
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ParkingOffers/parking-offers-within-time-range-for-specyfic-parking":
			q := r.URL.Query()
			if q.Get("Start") != "2026-10-24T22:00:00.000Z" || q.Get("End") != "2026-10-25T23:00:00.000Z" || q.Get("Page") != "1" || q.Get("PageSize") != "1000" {
				t.Errorf("query %v", q)
			}
			io.WriteString(w, `{"isSuccess":true,"result":{"turnstileUrl":"https://app.example.com/turnstile?a=1","entries":[{"id":"s1","level":"-1","isForHandicapped":true,"electricCharger":false,"isMiniEvOnly":true}]}}`)
		case "/api/Vehicles":
			io.WriteString(w, `{"isSuccess":true,"result":[{"id":"v1","licensePlateNumber":"WX1"}]}`)
		case "/api/Payments/credit-cards":
			io.WriteString(w, `{"isSuccess":true,"result":[{"id":"c1","status":1,"clientHash":"h"}]}`)
		}
	})
	ctx := context.Background()
	o, err := c.Offers(ctx, "j", "P", time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC), time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC))
	if err != nil || o.TurnstileURL != "https://app.example.com/turnstile?a=1" || len(o.Spots) != 1 ||
		o.Spots[0] != (model.Spot{ID: "s1", Level: "-1", Handicapped: true, MiniEV: true}) {
		t.Fatalf("%v %v", o, err)
	}
	v, err := c.Vehicles(ctx, "j")
	if err != nil || len(v) != 1 || v[0] != (model.Vehicle{ID: "v1", Plate: "WX1"}) {
		t.Fatalf("%v %v", v, err)
	}
	cc, err := c.CreditCards(ctx, "j")
	if err != nil || len(cc) != 1 || cc[0] != (model.Card{ID: "c1", Status: 1, ClientHash: "h"}) {
		t.Fatalf("%v %v", cc, err)
	}
}

func TestReservePaymentFlow(t *testing.T) {
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		switch r.URL.Path {
		case "/api/ParkingSpots/s1/reservations":
			if b["start"] != "2026-10-24T22:00:00.000Z" || b["end"] != "2026-10-25T23:00:00.000Z" || b["turnstileToken"] != "tok" || b["vehicleId"] != "v1" {
				t.Errorf("reserve body %v", b)
			}
			io.WriteString(w, `{"isSuccess":true,"result":{"id":"r1"}}`)
		case "/api/Payments/amount/r1":
			io.WriteString(w, `{"isSuccess":true,"result":{"data":{"totalPrice":12.5}}}`)
		case "/api/Payments/start-saved-credit-card-payment":
			if b["cardId"] != "c1" || b["amount"] != 12.5 || b["paymentTargetType"] != float64(0) || b["targetObjectId"] != "r1" || b["ipAddress"] != "1.2.3.4" {
				t.Errorf("pay body %v", b)
			}
			io.WriteString(w, `{"isSuccess":true,"result":{"id":"p1"}}`)
		case "/api/Payments/payment-status":
			if r.URL.Query().Get("PaymentId") != "p1" {
				t.Errorf("q %v", r.URL.Query())
			}
			io.WriteString(w, `{"isSuccess":true,"result":{"status":2}}`)
		}
	})
	ctx := context.Background()
	id, err := c.Reserve(ctx, "j", "s1", time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC), time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC), "tok", "v1")
	if err != nil || id != "r1" {
		t.Fatal(id, err)
	}
	amt, err := c.PaymentAmount(ctx, "j", "r1")
	if err != nil || amt != 12.5 {
		t.Fatal(amt, err)
	}
	pid, err := c.StartCardPayment(ctx, "j", "c1", 12.5, "r1", "1.2.3.4")
	if err != nil || pid != "p1" {
		t.Fatal(pid, err)
	}
	st, err := c.PaymentStatus(ctx, "j", "p1")
	if err != nil || st != 2 {
		t.Fatal(st, err)
	}
}

func TestNewHTTPClient(t *testing.T) {
	hc := NewHTTPClient()
	tr := hc.Transport.(*http.Transport)
	if hc.Timeout != 0 || tr.MaxIdleConnsPerHost != 64 || !tr.ForceAttemptHTTP2 {
		t.Fatal("bad client")
	}
}

func TestParkingsList(t *testing.T) {
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ParkingOffers/user-parkings-with-parking-offers-overview" ||
			r.URL.Query().Get("Page") != "1" || r.URL.Query().Get("PageSize") != "1000" {
			t.Errorf("request %s", r.URL)
		}
		io.WriteString(w, `{"isSuccess":true,"result":{"entries":[
{"parkingId":"11111111-1111-1111-1111-111111111111","parkingName":"Parking Testowy","type":1,
 "address":{"city":"Testowo","streetName":"Przykladowa","streetNumber":"7"},"minPrice":10,"maxPrice":15.5,"hidePrice":false},
{"parkingId":"","parkingName":"bez id"},
{"parkingId":"22222222-2222-2222-2222-222222222222","parkingName":"Drugi","address":{"city":"Testowo"}}]}}`)
	})
	ps, err := c.ParkingsList(context.Background(), "j")
	want := []model.Parking{
		{ID: "11111111-1111-1111-1111-111111111111", Name: "Parking Testowy", City: "Testowo", Street: "Przykladowa 7", MinPrice: 10, MaxPrice: 15.5},
		{ID: "22222222-2222-2222-2222-222222222222", Name: "Drugi", City: "Testowo"},
	}
	if err != nil || len(ps) != 2 || ps[0] != want[0] || ps[1] != want[1] {
		t.Fatalf("%+v %v", ps, err)
	}
}

func TestParkingLevels(t *testing.T) {
	var offersQ url.Values
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ParkingOffers/full-day-parking-offers-overview-timeline-for-specyfic-parking":
			io.WriteString(w, `{"isSuccess":true,"result":{"entries":[
{"day":"2026-10-24T22:00:00+00:00","freePlacesCount":2},
{"day":"2026-10-25T23:00:00+00:00","freePlacesCount":5},
{"day":"2026-10-26T23:00:00+00:00","freePlacesCount":0}]}}`)
		default:
			offersQ = r.URL.Query()
			io.WriteString(w, `{"isSuccess":true,"result":{"turnstileUrl":"","entries":[
{"id":"a","level":" poziom B "},{"id":"b","level":"poziom A"},{"id":"c","level":"poziom B"},{"id":"d","level":""},{"id":"e","level":"  "}]}}`)
		}
	})
	lv, err := c.ParkingLevels(context.Background(), "j", "P", time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC))
	if err != nil || len(lv) != 2 || lv[0] != "poziom A" || lv[1] != "poziom B" {
		t.Fatalf("%q %v", lv, err)
	}
	// most free spots: Warsaw 2026-10-26 (CET after the 25th switch), window = that day
	if offersQ.Get("Start") != "2026-10-25T23:00:00.000Z" || offersQ.Get("End") != "2026-10-26T23:00:00.000Z" {
		t.Fatalf("offers query %v", offersQ)
	}
}

func TestParkingLevelsNoFreeSpots(t *testing.T) {
	calls := 0
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		io.WriteString(w, `{"isSuccess":true,"result":{"entries":[{"day":"2026-10-24T22:00:00+00:00","freePlacesCount":0}]}}`)
	})
	lv, err := c.ParkingLevels(context.Background(), "j", "P", time.Now())
	if err != nil || lv == nil || len(lv) != 0 || calls != 1 {
		t.Fatalf("%v %v %d", lv, err, calls)
	}
}

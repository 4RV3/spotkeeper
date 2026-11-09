package api

import (
	"context"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"spotkeeper/internal/model"
)

// The expected strings below are copied from the mobile app's debug log
// (app 4.7.1, 2026-10-23): query and JSON key order must match it exactly.

type wireRec struct {
	mu    sync.Mutex
	reqs  []string // "METHOD path?rawquery"
	body  map[string]string
	agent []string
}

func wireServer(t *testing.T, result string) (*Client, *wireRec) {
	rec := &wireRec{body: map[string]string{}}
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		line := r.Method + " " + strings.TrimPrefix(r.URL.Path, "/api/")
		if r.URL.RawQuery != "" {
			line += "?" + r.URL.RawQuery
		}
		rec.reqs = append(rec.reqs, line)
		if len(b) > 0 {
			rec.body[line] = string(b)
		}
		rec.agent = append(rec.agent, r.Header.Get("User-Agent"))
		rec.mu.Unlock()
		io.WriteString(w, `{"isSuccess":true,"result":`+result+`}`)
	})
	return c, rec
}

var (
	logStart = time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC)
	logEnd   = time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC)
)

const logParking = "59c12250-6ccf-4e9f-96f7-000000000000"

func TestWireOffersQueryOrder(t *testing.T) {
	c, rec := wireServer(t, `{"entries":[]}`)
	c.Offers(context.Background(), "j", logParking, logStart, logEnd)
	want := "GET ParkingOffers/parking-offers-within-time-range-for-specyfic-parking?ParkingId=59c12250-6ccf-4e9f-96f7-000000000000&Start=2026-10-24T22%3A00%3A00.000Z&End=2026-10-25T23%3A00%3A00.000Z&Page=1&PageSize=1000"
	if rec.reqs[0] != want {
		t.Fatalf("got  %s\nwant %s", rec.reqs[0], want)
	}
}

func TestWireTimelineQuery(t *testing.T) {
	c, rec := wireServer(t, `{"entries":[]}`)
	c.Timeline(context.Background(), "j", logParking, time.Date(2026, 10, 23, 10, 15, 44, 431e6, time.UTC))
	want := "GET ParkingOffers/full-day-parking-offers-overview-timeline-for-specyfic-parking?ParkingId=59c12250-6ccf-4e9f-96f7-000000000000&Start=2026-10-23T10%3A15%3A44.431Z"
	if rec.reqs[0] != want {
		t.Fatalf("got  %s\nwant %s", rec.reqs[0], want)
	}
}

func TestWireReserveBody(t *testing.T) {
	c, rec := wireServer(t, `{"id":"r"}`)
	c.Reserve(context.Background(), "j", "59c12250-6ccf-4e9f-96f7-000000000021", logStart, logEnd, "TOK", "0188a550-67df-4ebd-b464-2af6a4d77bf3")
	line := "POST ParkingSpots/59c12250-6ccf-4e9f-96f7-000000000021/reservations"
	want := `{"start":"2026-10-24T22:00:00.000Z","end":"2026-10-25T23:00:00.000Z","turnstileToken":"TOK","vehicleId":"0188a550-67df-4ebd-b464-2af6a4d77bf3"}`
	if rec.reqs[0] != line || rec.body[line] != want {
		t.Fatalf("%v %s", rec.reqs, rec.body[line])
	}
}

func TestWirePaymentBodies(t *testing.T) {
	c, rec := wireServer(t, `{"id":"x","data":{"isFreeOfCharge":false,"price":15,"commisionRate":"0,00%","totalPrice":15}}`)
	ctx := context.Background()
	res := "01a16842-a4a9-4318-83d0-941b882603a9"
	amt, err := c.PaymentAmount(ctx, "j", res)
	if err != nil || amt != (model.Amount{Total: 15}) {
		t.Fatal(amt, err)
	}
	c.StartPayment(ctx, "j", "6a496b6a0f9e49528a1709799884f9b1", 15, res)
	c.StartCardPayment(ctx, "j", "019bfc0b-a2dd-4226-a63a-6fdee023747a", 15, res, "185.135.194.101")
	c.PaymentStatus(ctx, "j", "01a16842-a6ce-4b0b-a2ae-c66d11ce1cc5")
	wantReqs := []string{
		"GET Payments/amount/" + res,
		"POST Payments/start-payment",
		"POST Payments/start-saved-credit-card-payment",
		"GET Payments/payment-status?PaymentId=01a16842-a6ce-4b0b-a2ae-c66d11ce1cc5",
	}
	if strings.Join(rec.reqs, "\n") != strings.Join(wantReqs, "\n") {
		t.Fatal(rec.reqs)
	}
	if got := rec.body[wantReqs[1]]; got != `{"orderId":"6a496b6a0f9e49528a1709799884f9b1","amount":15,"paymentTargetType":0,"targetObjectId":"`+res+`"}` {
		t.Fatal(got)
	}
	if got := rec.body[wantReqs[2]]; got != `{"cardId":"019bfc0b-a2dd-4226-a63a-6fdee023747a","amount":15,"paymentTargetType":0,"targetObjectId":"`+res+`","ipAddress":"185.135.194.101"}` {
		t.Fatal(got)
	}
}

func TestWireFreeAmount(t *testing.T) {
	c, _ := wireServer(t, `{"data":{"isFreeOfCharge":true,"totalPrice":0}}`)
	amt, err := c.PaymentAmount(context.Background(), "j", "r")
	if err != nil || !amt.Free {
		t.Fatal(amt, err)
	}
}

func TestWireBookingContext(t *testing.T) {
	c, rec := wireServer(t, `{"entries":[]}`)
	if err := c.BookingContext(context.Background(), "j", logParking, time.Date(2026, 10, 23, 10, 15, 44, 431e6, time.UTC)); err != nil {
		t.Fatal(err)
	}
	got := append([]string(nil), rec.reqs...)
	sort.Strings(got)
	want := []string{
		"GET ParkingOffers/full-day-parking-offers-overview-timeline-for-specyfic-parking?ParkingId=" + logParking + "&Start=2026-10-23T10%3A15%3A44.431Z",
		"GET Parkings/parking-details?ParkingId=" + logParking,
		"GET Parkings/private-parkings/details?ParkingId=" + logParking,
		"GET Parkings/reservation-policies?ParkingId=" + logParking,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatal(got)
	}
}

func TestWireCheckoutContext(t *testing.T) {
	c, rec := wireServer(t, `{}`)
	spot := "59c12250-6ccf-4e9f-96f7-000000000021"
	if err := c.CheckoutContext(context.Background(), "j", logParking, spot, logStart, logEnd); err != nil {
		t.Fatal(err)
	}
	if rec.reqs[0] != "GET Parkings/parking-cost?ParkingSpotId="+spot+"&Start=2026-10-24T22%3A00%3A00.000Z&End=2026-10-25T23%3A00%3A00.000Z" {
		t.Fatal(rec.reqs[0])
	}
	rest := append([]string(nil), rec.reqs[1:]...)
	sort.Strings(rest)
	want := []string{
		"GET Payments/v2/configurations?parkingId=" + logParking,
		"GET Payments/v2/configurations?parkingId=" + logParking,
		"GET Payments/v2/credit-cards?parkingId=" + logParking,
		"GET Vehicles/v2?parkingId=" + logParking,
	}
	if strings.Join(rest, "\n") != strings.Join(want, "\n") {
		t.Fatal(rest)
	}
}

func TestWireVehiclesCardsV2(t *testing.T) {
	c, rec := wireServer(t, `[]`)
	c.Vehicles(context.Background(), "j", logParking)
	c.CreditCards(context.Background(), "j", logParking)
	if rec.reqs[0] != "GET Vehicles/v2?parkingId="+logParking || rec.reqs[1] != "GET Payments/v2/credit-cards?parkingId="+logParking {
		t.Fatal(rec.reqs)
	}
}

func TestWireUserAgent(t *testing.T) {
	c, rec := wireServer(t, `[]`)
	c.UserAgent = "okhttp/4.12.0"
	c.Vehicles(context.Background(), "j", "")
	if rec.agent[0] != "okhttp/4.12.0" {
		t.Fatal(rec.agent)
	}
}

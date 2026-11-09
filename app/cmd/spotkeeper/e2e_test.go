package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"spotkeeper/internal/config"
	"spotkeeper/internal/model"
	"spotkeeper/internal/timeutil"
)

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func ok(result any) map[string]any {
	return map[string]any{"isSuccess": true, "result": result}
}

func fakeService(t *testing.T) *httptest.Server {
	t.Helper()
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":` + strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10) + `}`))
	jwt := "h." + payload + ".s"
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/api/")
		if ua := r.Header.Get("User-Agent"); !strings.Contains(ua, "Chrome/") {
			t.Errorf("%s sent User-Agent %q, want the Chrome default", p, ua)
		}
		switch {
		case p == "Sessions/native":
			writeJSON(w, ok(map[string]any{"jwt": jwt}))
		case p == "Vehicles" || p == "Vehicles/v2":
			writeJSON(w, ok([]map[string]any{{"id": "veh1", "licensePlateNumber": "WX 12345"}}))
		case p == "Payments/credit-cards" || p == "Payments/v2/credit-cards":
			writeJSON(w, ok([]map[string]any{{"id": "card1", "status": 1, "clientHash": "h"}}))
		case p == "ParkingOffers/user-parkings-with-parking-offers-overview":
			writeJSON(w, ok(map[string]any{"entries": []map[string]any{{
				"parkingId": "11111111-2222-3333-4444-555555555555", "parkingName": "Parking Testowy",
				"address": map[string]any{"city": "Testowo", "streetName": "Probna", "streetNumber": "3"}}}}))
		case strings.HasPrefix(p, "ParkingOffers/full-day-parking-offers-overview-timeline"):
			day := time.Now().UTC().Truncate(24 * time.Hour).Format("2006-01-02T15:04:05+00:00")
			writeJSON(w, ok(map[string]any{"entries": []map[string]any{{"day": day, "freePlacesCount": 2}}}))
		case strings.HasPrefix(p, "ParkingOffers/parking-offers"):
			writeJSON(w, ok(map[string]any{
				"turnstileUrl": "https://app.example.com/turnstile?v=5#sid=x&cd=y&tc=z",
				"entries":      []map[string]any{{"id": "spot1", "level": "podziemny"}},
			}))
		case strings.HasSuffix(p, "/reservations"):
			writeJSON(w, ok(map[string]any{"id": "res1"}))
		case strings.HasPrefix(p, "Payments/amount/"):
			writeJSON(w, ok(map[string]any{"data": map[string]any{"totalPrice": 15.0}}))
		case p == "Payments/start-payment":
			writeJSON(w, ok(map[string]any{"id": "proc1"}))
		case p == "Payments/v2/configurations", p == "Parkings/parking-cost", p == "Parkings/parking-details",
			p == "Parkings/private-parkings/details", p == "Parkings/reservation-policies":
			writeJSON(w, ok(map[string]any{}))
		case p == "Payments/start-saved-credit-card-payment":
			writeJSON(w, ok(map[string]any{"id": "pay1"}))
		case p == "Payments/payment-status":
			writeJSON(w, ok(map[string]any{"status": 2}))
		default:
			http.NotFound(w, r)
		}
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func fakeTurnstile(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RequestID string `json:"requestId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		writeJSON(w, map[string]any{
			"ok": true, "api_version": 2, "request_id": req.RequestID,
			"context_sha256":         strings.Repeat("ab", 32),
			"telemetry_acknowledged": true,
			"token":                  "tok-" + strings.Repeat("x", 30),
			"token_observed_at":      time.Now().UTC().Format(time.RFC3339Nano),
		})
	}))
	t.Cleanup(s.Close)
	return s
}

func envFunc(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func testEnv(t *testing.T, pc, ts string) map[string]string {
	return map[string]string{
		"LISTEN_ADDR":      "127.0.0.1:0",
		"DB_PATH":          filepath.Join(t.TempDir(), "sub", "spotkeeper.db"),
		"API_DOMAIN":       "app.example.com",
		"ACCOUNT_LOGIN":    "jan.kowalski@example.com",
		"ACCOUNT_PASSWORD": "pw",
		"APP_PASSWORD":     "secret123",
		"API_BASE":         pc + "/api",
		"TURNSTILE_URL":    ts,
		"POLL_INTERVAL":    "20ms",
	}
}

func TestE2EFutureReservation(t *testing.T) {
	pc, ts := fakeService(t), fakeTurnstile(t)
	cfg, err := config.Load(envFunc(testEnv(t, pc.URL, ts.URL)))
	if err != nil {
		t.Fatal(err)
	}
	a, err := build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.store.Close()

	release := time.Now().Add(300 * time.Millisecond)
	day := timeutil.AddDays(timeutil.Today(release), 28)
	login := "jan.kowalski@example.com"
	if err := a.store.UpsertUser(model.User{Login: login, Plate: "WX12345", Active: true,
		Schedule: strconv.Itoa(timeutil.Weekday(day))}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := a.future.RunDay(ctx, release); err != nil {
		t.Fatal(err)
	}
	tasks, err := a.store.ListTasks(login, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks: %+v", tasks)
	}
	tk := tasks[0]
	if tk.Status != model.StatusPaid || tk.Day != day || tk.SpotID != "spot1" ||
		tk.ReservationID != "res1" || tk.PaymentID != "pay1" {
		t.Fatalf("task: %+v", tk)
	}
}

func TestGracefulShutdown(t *testing.T) {
	pc, ts := fakeService(t), fakeTurnstile(t)
	env := envFunc(testEnv(t, pc.URL, ts.URL))
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- run(ctx, env, ready) }()

	var addr string
	select {
	case addr = <-ready:
	case err := <-done:
		t.Fatalf("run exited early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("not ready")
	}
	resp, err := http.Get(fmt.Sprintf("http://%s/healthz", addr))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("healthz %d", resp.StatusCode)
	}

	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown too slow")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("slow")
	}
}

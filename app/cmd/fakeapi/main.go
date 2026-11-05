// Command fakeapi is a local stand-in for the reservation service and turnstile-service,
// for clicking through SpotKeeper without real reservations or payments.
//
// Routes: the service API under /pc/api/, turnstile under /ts/v2/token.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	_ "time/tzdata"
)

var requestIDRe = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

// Plates every fake account owns; register with one of them.
var plates = []string{"WX 1234A", "KR 5555B", "DEMO 1"}

type server struct {
	tokenDelay time.Duration
	free       int
	domain     string // host of the verification URLs (API_DOMAIN)

	seq atomic.Int64
}

func newServer(tokenDelay time.Duration, free int) *server {
	return &server{tokenDelay: tokenDelay, free: free, domain: "app.example.com"}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/pc/api/", s.serviceAPI)
	mux.HandleFunc("POST /ts/v2/token", s.token)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") })
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func ok(result any) map[string]any {
	return map[string]any{"isSuccess": true, "statusCode": 200, "result": result}
}

func fakeJWT() string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":` + strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10) + `}`))
	return "h." + payload + ".s"
}

func (s *server) turnstileURL() string {
	return fmt.Sprintf("https://%s/turnstile?v=5#sid=fake%d&cd=demo&tc=demo", s.domain, time.Now().UnixNano())
}

func (s *server) serviceAPI(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/pc/api/")
	switch {
	case p == "Sessions/native":
		writeJSON(w, http.StatusOK, ok(map[string]any{"jwt": fakeJWT()}))
	case p == "Vehicles":
		vs := make([]map[string]any, len(plates))
		for i, pl := range plates {
			vs[i] = map[string]any{"id": fmt.Sprintf("veh%d", i+1), "licensePlateNumber": pl}
		}
		writeJSON(w, http.StatusOK, ok(vs))
	case p == "Payments/credit-cards":
		writeJSON(w, http.StatusOK, ok([]map[string]any{{"id": "card1", "status": 1, "clientHash": "demo"}}))
	case p == "ParkingOffers/user-parkings-with-parking-offers-overview":
		writeJSON(w, http.StatusOK, ok(map[string]any{"entries": fakeParkings}))
	case strings.HasPrefix(p, "ParkingOffers/parking-offers-within-time-range"):
		writeJSON(w, http.StatusOK, ok(map[string]any{"turnstileUrl": s.turnstileURL(), "entries": s.spots()}))
	case strings.HasPrefix(p, "ParkingOffers/full-day-parking-offers-overview-timeline"):
		s.timeline(w, r)
	case strings.HasPrefix(p, "ParkingSpots/") && strings.HasSuffix(p, "/reservations") && r.Method == http.MethodPost:
		writeJSON(w, http.StatusCreated, ok(map[string]any{"id": fmt.Sprintf("res%d", s.seq.Add(1))}))
	case strings.HasPrefix(p, "Payments/amount/"):
		writeJSON(w, http.StatusOK, ok(map[string]any{"data": map[string]any{"totalPrice": 15.0}}))
	case p == "Payments/start-saved-credit-card-payment":
		writeJSON(w, http.StatusCreated, ok(map[string]any{"id": fmt.Sprintf("pay%d", s.seq.Add(1))}))
	case p == "Payments/payment-status":
		writeJSON(w, http.StatusOK, ok(map[string]any{"status": 2}))
	default:
		http.NotFound(w, r)
	}
}

// fakeParkings are the synthetic parkings the picker lists.
var fakeParkings = []map[string]any{
	{"parkingId": "11111111-aaaa-4bbb-8ccc-000000000001", "parkingName": "Parking Testowy", "type": 1,
		"address":  map[string]any{"country": "PL", "city": "Testowo", "streetName": "Probna", "streetNumber": "1"},
		"minPrice": 10, "maxPrice": 15, "hidePrice": false},
	{"parkingId": "11111111-aaaa-4bbb-8ccc-000000000002", "parkingName": "Garaz Przykladowy", "type": 1,
		"address":  map[string]any{"country": "PL", "city": "Przykladowo", "streetName": "Wzorcowa", "streetNumber": "22B"},
		"minPrice": 12, "maxPrice": 18, "hidePrice": false},
}

func (s *server) spots() []map[string]any {
	levels := []string{"podziemny (-1,-2)", "nadziemny (+1,+2,+3)"}
	out := make([]map[string]any, 0, 6)
	for i := 0; i < 6; i++ {
		out = append(out, map[string]any{
			"id": fmt.Sprintf("spot-%03d", rand.IntN(400)), "level": levels[i%2],
			"isForHandicapped": i == 4, "electricCharger": i == 5, "isMiniEvOnly": false,
		})
	}
	return out
}

func (s *server) timeline(w http.ResponseWriter, r *http.Request) {
	start, err := time.Parse(time.RFC3339, r.URL.Query().Get("Start"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"isSuccess": false, "error": map[string]any{"code": 1, "message": "bad Start"}})
		return
	}
	loc, _ := time.LoadLocation("Europe/Warsaw")
	d := start.In(loc)
	d = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
	entries := make([]map[string]any, 7)
	for i := range entries {
		day := d.AddDate(0, 0, i)
		entries[i] = map[string]any{"day": day.UTC().Format("2006-01-02T15:04:05+00:00"), "freePlacesCount": s.free}
	}
	writeJSON(w, http.StatusOK, ok(map[string]any{"entries": entries, "turnstileUrl": s.turnstileURL()}))
}

func (s *server) token(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TurnstileURL string `json:"turnstileUrl"`
		RequestID    string `json:"requestId"`
	}
	// Same input rules as turnstile-service context_api.py.
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !requestIDRe.MatchString(req.RequestID) ||
		!strings.HasPrefix(req.TurnstileURL, "https://"+s.domain+"/turnstile?") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "code": "invalid_request", "error": "A nonempty requestId is required"})
		return
	}
	select {
	case <-time.After(s.tokenDelay):
	case <-r.Context().Done():
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "api_version": 2, "request_id": req.RequestID,
		"context_sha256":         strings.Repeat("ab", 32),
		"telemetry_acknowledged": true,
		"token":                  fmt.Sprintf("fake-token-%d-%s", s.seq.Add(1), strings.Repeat("x", 24)),
		"token_observed_at":      time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	delay, err := time.ParseDuration(envOr("FAKE_TOKEN_DELAY", "2s"))
	if err != nil {
		slog.Error("FAKE_TOKEN_DELAY", "err", err)
		os.Exit(1)
	}
	free, err := strconv.Atoi(envOr("FAKE_FREE_PLACES", "2"))
	if err != nil {
		slog.Error("FAKE_FREE_PLACES", "err", err)
		os.Exit(1)
	}
	addr := envOr("LISTEN_ADDR", ":9000")
	slog.Info("fakeapi listening", "addr", addr, "plates", plates)
	fake := newServer(delay, free)
	fake.domain = envOr("API_DOMAIN", fake.domain)
	srv := &http.Server{Addr: addr, Handler: fake.routes(), ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("serve", "err", err)
		os.Exit(1)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

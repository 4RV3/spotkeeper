package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"spotkeeper/internal/config"
	"spotkeeper/internal/store"
)

const testParking = "11111111-2222-3333-4444-555555555555"

func minimalEnv(t *testing.T) map[string]string {
	pc, ts := fakeService(t), fakeTurnstile(t)
	return testEnv(t, pc.URL, ts.URL)
}

func buildApp(t *testing.T, env map[string]string) *app {
	t.Helper()
	cfg, err := config.Load(envFunc(env))
	if err != nil {
		t.Fatal(err)
	}
	a, err := build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.store.Close() })
	return a
}

func login(t *testing.T, h http.Handler, password string) *http.Cookie {
	t.Helper()
	r := httptest.NewRequest("POST", "/login", strings.NewReader(url.Values{"password": {password}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	for _, c := range w.Result().Cookies() {
		if c.Name == "pc_session" && c.Value != "" {
			return c
		}
	}
	t.Fatalf("no session cookie, status %d", w.Code)
	return nil
}

func TestRunStartsWithMinimalEnv(t *testing.T) {
	env := minimalEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- run(ctx, envFunc(env), ready) }()
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
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown too slow")
	}
}

func TestRunFailsWithShortAppPassword(t *testing.T) {
	env := minimalEnv(t)
	env["APP_PASSWORD"] = "abc"
	err := run(context.Background(), envFunc(env), nil)
	if err == nil || !strings.Contains(err.Error(), "APP_PASSWORD must be at least 8 characters") {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionKeyNextToDB(t *testing.T) {
	env := minimalEnv(t)
	buildApp(t, env)
	if _, err := os.Stat(filepath.Join(filepath.Dir(env["DB_PATH"]), "session.key")); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseFuncFromDB(t *testing.T) {
	a := buildApp(t, minimalEnv(t))
	if a.releaseTime() != "11:00" || a.amount() != 15 || a.parkingID() != "" {
		t.Fatalf("defaults: %q %v %q", a.releaseTime(), a.amount(), a.parkingID())
	}
	if err := a.store.SaveAppSettings(store.AppSettings{ParkingID: testParking, ReleaseTime: "10:45", PaymentAmount: 12.5}); err != nil {
		t.Fatal(err)
	}
	if a.releaseTime() != "10:45" || a.amount() != 12.5 || a.parkingID() != testParking {
		t.Fatalf("after save: %q %v %q", a.releaseTime(), a.amount(), a.parkingID())
	}
}

func TestPasswordChangeRevokesSession(t *testing.T) {
	env := minimalEnv(t)
	a := buildApp(t, env)
	c := login(t, a.handler, "secret123")
	get := func(h http.Handler) int {
		r := httptest.NewRequest("GET", "/", nil)
		r.AddCookie(c)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if code := get(a.handler); code != 200 {
		t.Fatalf("same password: %d", code)
	}
	a.store.Close()
	env["APP_PASSWORD"] = "another-pass"
	b := buildApp(t, env)
	if code := get(b.handler); code != 303 {
		t.Fatalf("after password change: %d", code)
	}
}

func TestCheckWiredEndToEnd(t *testing.T) {
	env := minimalEnv(t)
	a := buildApp(t, env)
	u, _ := a.store.GetUser(a.cfg.Login)
	u.Plate = "WX12345"
	if err := a.store.UpsertUser(u); err != nil {
		t.Fatal(err)
	}
	if err := a.store.SaveAppSettings(store.AppSettings{ParkingID: testParking, ReleaseTime: "11:00", PaymentAmount: 15}); err != nil {
		t.Fatal(err)
	}
	c := login(t, a.handler, "secret123")
	r := httptest.NewRequest("POST", "/check", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	a.handler.ServeHTTP(w, r)
	body := w.Body.String()
	if w.Code != 200 || strings.Count(body, "✓") != 6 || strings.Contains(body, "✗") {
		t.Fatalf("status %d, ok marks %d\n%s", w.Code, strings.Count(body, "✓"), body)
	}
}

func TestPickerAndLevelsWired(t *testing.T) {
	a := buildApp(t, minimalEnv(t))
	c := login(t, a.handler, "secret123")
	get := func(path string) (int, string) {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(c)
		w := httptest.NewRecorder()
		a.handler.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	code, body := get("/")
	if code != 200 || !strings.Contains(body, "Parking Testowy — Testowo, Probna 3") {
		t.Fatalf("picker: %d", code)
	}
	code, body = get("/settings/levels?parking=" + testParking)
	if code != 200 || strings.TrimSpace(body) != `{"levels":["podziemny"]}` {
		t.Fatalf("levels: %d %s", code, body)
	}
}

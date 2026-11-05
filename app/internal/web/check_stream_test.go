package web

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"spotkeeper/internal/check"
)

func TestCheckStreamEmitsRunningThenResult(t *testing.T) {
	e := newDEnv(t)
	e.srv.d.CheckStream = func(_ context.Context, p func(int, string, bool, check.Step)) []check.Step {
		p(0, "Ustawienia", false, check.Step{})
		st := check.Step{Name: "Ustawienia", OK: true, Detail: "OK"}
		p(0, "Ustawienia", true, st)
		p(1, "Logowanie", false, check.Step{})
		bad := check.Step{Name: "Logowanie", Detail: "zle"}
		p(1, "Logowanie", true, bad)
		sk := check.Step{Name: "Parking", Skipped: true, Detail: "Pominięto"}
		p(2, "Parking", true, sk)
		return []check.Step{st, bad, sk}
	}
	e.srv.d.Check = func(context.Context) []check.Step { return nil }
	w := e.do("POST", "/check/stream", nil)
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/x-ndjson") {
		t.Fatal(w.Code, w.Header())
	}
	lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
	want := []string{
		`{"i":0,"name":"Ustawienia","state":"running"}`,
		`{"detail":"OK","i":0,"name":"Ustawienia","state":"ok"}`,
		`{"i":1,"name":"Logowanie","state":"running"}`,
		`{"detail":"zle","i":1,"name":"Logowanie","state":"fail"}`,
		`{"detail":"Pominięto","i":2,"name":"Parking","state":"skipped"}`,
		`{"allOk":false,"done":true}`,
	}
	if len(lines) != len(want) {
		t.Fatalf("%d lines: %q", len(lines), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %s, want %s", i, lines[i], want[i])
		}
	}
	if !w.Flushed {
		t.Error("response was not flushed")
	}
}

func TestCheckStreamRequiresLogin(t *testing.T) {
	e := newDEnv(t)
	ran := false
	e.srv.d.Check = func(context.Context) []check.Step { ran = true; return nil }
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, httptest.NewRequest("POST", "/check/stream", nil))
	if w.Code != 303 || w.Header().Get("Location") != "/login" || ran {
		t.Fatal(w.Code, w.Header(), ran)
	}
}

func TestCheckStreamCSRF(t *testing.T) {
	e := newDEnv(t)
	ran := false
	e.srv.d.Check = func(context.Context) []check.Step { ran = true; return nil }
	r := httptest.NewRequest("POST", "/check/stream", nil)
	r.Header.Set("Origin", "http://evil.example")
	r.AddCookie(e.cookie)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	if w.Code != 403 || ran {
		t.Fatal(w.Code, ran)
	}
}

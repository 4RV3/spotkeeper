package web

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"context"

	"spotkeeper/internal/check"
)

func TestCheckPageRequiresLogin(t *testing.T) {
	e := newDEnv(t)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, httptest.NewRequest("GET", "/check", nil))
	if w.Code != 303 || w.Header().Get("Location") != "/login" {
		t.Fatal(w.Code, w.Header())
	}
	// unavailable page and escaped details when logged in
	if w = e.do("GET", "/check", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "niedostępna") {
		t.Fatal(w.Code)
	}
	e.srv.d.Check = func(context.Context) []check.Step {
		return []check.Step{{Name: "A", OK: true, Detail: "<b>x"}, {Name: "B", Detail: "bad"}, {Name: "C", Skipped: true, Detail: "pominięto"}}
	}
	b := e.do("POST", "/check", url.Values{}).Body.String()
	if !strings.Contains(b, "&lt;b&gt;x") || strings.Contains(b, "<b>x") || !strings.Contains(b, "✓") || !strings.Contains(b, "✗") {
		t.Fatal(b)
	}
}

func TestCheckPostCSRF(t *testing.T) {
	e := newDEnv(t)
	ran := false
	e.srv.d.Check = func(context.Context) []check.Step { ran = true; return nil }
	r := httptest.NewRequest("POST", "/check", nil)
	r.Header.Set("Origin", "http://evil.example")
	r.AddCookie(e.cookie)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	if w.Code != 403 || ran {
		t.Fatal(w.Code, ran)
	}
}

package web

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"spotkeeper/internal/check"
)

func TestHelpPageRenders(t *testing.T) {
	e := newDEnv(t)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, httptest.NewRequest("GET", "/pomoc", nil)) // public: no cookie
	b := w.Body.String()
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	for _, want := range []string{`id="zanim-zaczniesz"`, "Zanim zaczniesz", "numerem rejestracyjnym", "karta płatnicza", `id="faq"`, `id="blad"`, "<details"} {
		if !strings.Contains(strings.ToLower(b), strings.ToLower(want)) {
			t.Errorf("help page lacks %q", want)
		}
	}
	if strings.Contains(b, "Wyloguj") {
		t.Error("public help page must not show account controls")
	}
	if strings.Contains(strings.ToLower(b), "parkcash") {
		t.Error("service brand name in help page")
	}
}

func TestHelpLinkInHeader(t *testing.T) {
	e := newDEnv(t)
	for _, b := range []string{get(e, "/"), get(e, "/pomoc")} {
		if !strings.Contains(b, `<a class="help-link" href="/pomoc">Pomoc</a>`) {
			t.Error("header lacks the Pomoc link")
		}
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, httptest.NewRequest("GET", "/login", nil))
	if !strings.Contains(w.Body.String(), `href="/pomoc#zanim-zaczniesz"`) || !strings.Contains(w.Body.String(), `href="/pomoc"`) {
		t.Error("login page lacks the help links")
	}
}

func TestCheckHelpLinkForFailedStep(t *testing.T) {
	e := newDEnv(t)
	e.srv.d.Check = func(context.Context) []check.Step {
		return []check.Step{{Name: "Pojazd i karta", Detail: "x", Help: "/pomoc#zanim-zaczniesz"}}
	}
	b := e.do("POST", "/check", nil).Body.String()
	if !strings.Contains(b, `href="/pomoc#zanim-zaczniesz"`) {
		t.Fatal("failed step lacks the help link")
	}
}

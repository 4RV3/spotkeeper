package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManifestIsPublic(t *testing.T) {
	e := newDEnv(t)
	for path, ct := range map[string]string{
		"/manifest.webmanifest": "application/manifest+json",
		"/static/icon-192.png":  "image/png", "/static/icon-512.png": "image/png", "/static/icon-180.png": "image/png",
		"/static/icon-512-maskable.png": "image/png", "/static/icon.svg": "image/svg+xml",
	} {
		w := httptest.NewRecorder()
		e.h.ServeHTTP(w, httptest.NewRequest("GET", path, nil)) // no session
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), ct) {
			t.Fatalf("%s: %d %q", path, w.Code, w.Header().Get("Content-Type"))
		}
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, httptest.NewRequest("GET", "/manifest.webmanifest", nil))
	var m struct {
		Name     string `json:"name"`
		StartURL string `json:"start_url"`
		Display  string `json:"display"`
		Icons    []struct{ Sizes, Purpose string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil || m.StartURL != "/" || m.Display != "standalone" || len(m.Icons) < 2 || m.Name != "SpotKeeper" {
		t.Fatalf("manifest %+v %v", m, err)
	}
	b := get(e, "/")
	if !strings.Contains(b, `<link rel="manifest" href="/manifest.webmanifest" crossorigin="use-credentials">`) ||
		!strings.Contains(b, `rel="apple-touch-icon"`) {
		t.Fatal("layout lacks manifest/apple-touch-icon")
	}
}

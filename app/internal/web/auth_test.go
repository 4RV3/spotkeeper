package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func (e *dEnv) post(path string, v url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == "pc_session" {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func (e *dEnv) now(t time.Time) { e.srv.d.Now = func() time.Time { return t } }

func TestHealthzAndRedirect(t *testing.T) {
	e := newDEnv(t)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 200 || w.Body.String() != "ok" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	e.h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 303 || w.Header().Get("Location") != "/login" {
		t.Fatal(w.Code, w.Header())
	}
}

func TestLoginPassword(t *testing.T) {
	e := newDEnv(t)
	w := e.post("/login", url.Values{"password": {"wrong"}})
	if w.Code != 401 || !strings.Contains(w.Body.String(), "Nieprawidłowe hasło.") {
		t.Fatal(w.Code)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "pc_session" {
			t.Fatal("session set on failure")
		}
	}
	w = e.post("/login", url.Values{"password": {"sesame123"}})
	if w.Code != 303 || w.Header().Get("Location") != "/" {
		t.Fatal(w.Code)
	}
	c := sessionCookie(t, w)
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie flags %+v", c)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(c)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Fatal("dashboard after login", rec.Code)
	}
	// a login field is ignored: the session always belongs to Deps.Login
	w = e.post("/login", url.Values{"login": {"someone@else.pl"}, "password": {"sesame123"}})
	if l, _ := e.srv.currentUser(func() *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		r.AddCookie(sessionCookie(t, w))
		return r
	}()); l != testLogin {
		t.Fatal("session login", l)
	}
}

func TestEmptyAppPasswordNeverMatches(t *testing.T) {
	e := newDEnv(t)
	e.srv.d.AppPassword = ""
	if w := e.post("/login", url.Values{"password": {""}}); w.Code != 401 {
		t.Fatal(w.Code)
	}
}

func TestTamperedCookie(t *testing.T) {
	e := newDEnv(t)
	c := sessionCookie(t, e.post("/login", url.Values{"password": {"sesame123"}}))
	s := e.srv
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(c)
	if l, ok := s.currentUser(r); !ok || l != testLogin {
		t.Fatal(l, ok)
	}
	bad := *c
	bad.Value = "x" + c.Value
	r = httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&bad)
	if l, ok := s.currentUser(r); ok || l != "" {
		t.Fatal("tampered accepted")
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	if w.Code != 303 || w.Header().Get("Location") != "/login" {
		t.Fatal(w.Code)
	}
	// a validly signed cookie for another login is not the instance's account
	w = httptest.NewRecorder()
	s.setSession(w, "other@example.com")
	r = httptest.NewRequest("GET", "/", nil)
	r.AddCookie(w.Result().Cookies()[0])
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	if rec.Code != 303 {
		t.Fatal("foreign login accepted", rec.Code)
	}
	// expired
	e.now(time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC).Add(31 * 24 * time.Hour))
	r = httptest.NewRequest("GET", "/", nil)
	r.AddCookie(c)
	if _, ok := s.currentUser(r); ok {
		t.Fatal("expired accepted")
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := newDEnv(t)
	for i := 0; i < maxLoginFails; i++ {
		e.post("/login", url.Values{"password": {"bad"}})
	}
	w := e.post("/login", url.Values{"password": {"sesame123"}})
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal(w.Code)
	}
	e.now(time.Date(2026, 10, 24, 10, 16, 0, 0, time.UTC))
	if w := e.post("/login", url.Values{"password": {"sesame123"}}); w.Code != 303 {
		t.Fatal(w.Code)
	}
}

func TestLoginErrorEscaped(t *testing.T) {
	e := newDEnv(t)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, httptest.NewRequest("GET", "/login", nil))
	if strings.Contains(w.Body.String(), `name="login"`) {
		t.Fatal("login form still asks for a login")
	}
}

func TestOriginAndLogout(t *testing.T) {
	e := newDEnv(t)
	r := httptest.NewRequest("POST", "/logout", nil)
	r.Header.Set("Origin", "http://evil.example")
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	w = e.post("/logout", url.Values{})
	if w.Code != 303 {
		t.Fatal(w.Code)
	}
	cleared := false
	for _, c := range w.Result().Cookies() {
		if c.Name == "pc_session" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("cookie not cleared")
	}
}

func TestSecurityHardening(t *testing.T) {
	e := newDEnv(t)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, httptest.NewRequest("GET", "/login", nil))
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'self'") {
		t.Fatal("no CSP")
	}
	r := httptest.NewRequest("POST", "/login", nil)
	r.Header.Set("Referer", "http://evil.example/x")
	w = httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("referer", w.Code)
	}
	r = httptest.NewRequest("POST", "/login", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w = httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("sec-fetch", w.Code)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		NewServer(Deps{SessionSecret: []byte("x")})
	}()
}

func TestFailMapBounded(t *testing.T) {
	e := newDEnv(t)
	for i := 0; i < maxFailKeys+500; i++ {
		e.srv.recordFail(fmt.Sprint("k", i))
	}
	e.srv.mu.Lock()
	n := len(e.srv.fails)
	e.srv.mu.Unlock()
	if n > maxFailKeys {
		t.Fatal(n)
	}
}

func TestLoginWrongPassword401(t *testing.T) {
	e := newDEnv(t)
	if w := e.post("/login", url.Values{"password": {"nope"}}); w.Code != 401 {
		t.Fatal(w.Code)
	}
}

func TestLoginConstantTime(t *testing.T) {
	if !passwordMatches("abc", "abc") || passwordMatches("abc", "abcd") || passwordMatches("", "x") {
		t.Fatal("passwordMatches wrong")
	}
	src, err := os.ReadFile("auth.go")
	if err != nil || !strings.Contains(string(src), "subtle.ConstantTimeCompare") || strings.Contains(string(src), "password == ") {
		t.Fatal("auth.go must use subtle compare")
	}
}

func TestLoginRateLimit6th(t *testing.T) {
	e := newDEnv(t)
	for i := 0; i < 5; i++ {
		if w := e.post("/login", url.Values{"password": {"bad"}}); w.Code != 401 {
			t.Fatal(i, w.Code)
		}
	}
	w := e.post("/login", url.Values{"password": {"bad"}})
	if w.Code != 429 || !strings.Contains(w.Body.String(), "Zbyt wiele nieudanych prób") {
		t.Fatal(w.Code)
	}
}

func TestLoginSuccessResetsIP(t *testing.T) {
	e := newDEnv(t)
	for i := 0; i < 4; i++ {
		e.post("/login", url.Values{"password": {"bad"}})
	}
	e.post("/login", url.Values{"password": {"sesame123"}})
	for i := 0; i < 5; i++ {
		if w := e.post("/login", url.Values{"password": {"bad"}}); w.Code != 401 {
			t.Fatal(i, w.Code)
		}
	}
}

func TestSessionSurvivesNewServer(t *testing.T) {
	e := newDEnv(t)
	c := sessionCookie(t, e.post("/login", url.Values{"password": {"sesame123"}}))
	if c.MaxAge != 30*24*3600 || c.Path != "/" {
		t.Fatalf("%+v", c)
	}
	d := e.srv.d
	s2 := NewServer(d).(*Server)
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(c)
	if l, ok := s2.currentUser(r); !ok || l != testLogin {
		t.Fatal(l, ok)
	}
}

func TestLogoutClears(t *testing.T) {
	e := newDEnv(t)
	w := e.post("/logout", url.Values{}, e.cookie)
	c := sessionCookie(t, w)
	if c.MaxAge >= 0 || c.Value != "" {
		t.Fatalf("%+v", c)
	}
}

func TestNoUsernameField(t *testing.T) {
	e := newDEnv(t)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, httptest.NewRequest("GET", "/login", nil))
	b := w.Body.String()
	if strings.Contains(b, `name="login"`) || strings.Contains(b, `name="username"`) || strings.Contains(b, "/register") || !strings.Contains(b, `autocomplete="current-password"`) {
		t.Fatal("login form wrong")
	}
}

func TestTamperedCookieRejected(t *testing.T) {
	e := newDEnv(t)
	c := *e.cookie
	i := strings.Index(c.Value, ".")
	c.Value = c.Value[:i] + "." + strings.Repeat("0", len(c.Value)-i-1)
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&c)
	if _, ok := e.srv.currentUser(r); ok {
		t.Fatal("bad sig accepted")
	}
}

func TestExpiredCookieRejected(t *testing.T) {
	e := newDEnv(t)
	e.now(time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC).Add(31 * 24 * time.Hour))
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(e.cookie)
	if _, ok := e.srv.currentUser(r); ok {
		t.Fatal("expired accepted")
	}
}

func TestFailFloodCannotDisableLimiter(t *testing.T) {
	e := newDEnv(t)
	for i := 0; i < maxFailKeys+50; i++ {
		e.srv.recordFail(fmt.Sprint("k", i))
	}
	for i := 0; i < maxLoginFails; i++ {
		e.post("/login", url.Values{"password": {"bad"}})
	}
	if w := e.post("/login", url.Values{"password": {"sesame123"}}); w.Code != 429 {
		t.Fatal(w.Code)
	}
}

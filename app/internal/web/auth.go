package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

const (
	cookieName     = "pc_session"
	sessionTTL     = 30 * 24 * time.Hour
	maxLoginFails  = 5
	loginFailWindw = 15 * time.Minute
)

var dayNames = []string{"Pn", "Wt", "Śr", "Cz", "Pt", "So", "Nd"}

func (s *Server) sign(payload string) string {
	m := hmac.New(sha256.New, s.d.SessionSecret)
	m.Write([]byte(payload))
	return hex.EncodeToString(m.Sum(nil))
}

func (s *Server) setSession(w http.ResponseWriter, login string) {
	exp := s.d.Now().Add(sessionTTL)
	p := base64.RawURLEncoding.EncodeToString([]byte(login + "|" + strconv.FormatInt(exp.Unix(), 10)))
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: p + "." + s.sign(p), Path: "/", Expires: exp, MaxAge: int(sessionTTL.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.d.CookieSecure,
	})
}

func (s *Server) clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.d.CookieSecure,
	})
}

func (s *Server) currentUser(r *http.Request) (string, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return "", false
	}
	p, sig, ok := strings.Cut(c.Value, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(s.sign(p))) {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return "", false
	}
	i := strings.LastIndex(string(raw), "|")
	if i <= 0 {
		return "", false
	}
	exp, err := strconv.ParseInt(string(raw[i+1:]), 10, 64)
	if err != nil || !s.d.Now().Before(time.Unix(exp, 0)) {
		return "", false
	}
	return string(raw[:i]), true
}

// requireUser runs h as the instance's single account when the session is valid.
func (s *Server) requireUser(h func(w http.ResponseWriter, r *http.Request, login string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		login, ok := s.currentUser(r)
		if !ok || login != s.d.Login {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		h(w, r, s.d.Login)
	}
}

type dayOpt struct {
	Value   string
	Name    string
	Checked bool
}

func dayOpts(checked map[string]bool) []dayOpt {
	out := make([]dayOpt, 7)
	for i := range out {
		v := strconv.Itoa(i + 1)
		out[i] = dayOpt{v, dayNames[i], checked[v]}
	}
	return out
}

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, 200, "login.html", nil)
}

// limited reports whether login has too many recent failures.
func (s *Server) limited(login string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.d.Now()
	keep := s.fails[login][:0]
	for _, t := range s.fails[login] {
		if now.Sub(t) < loginFailWindw {
			keep = append(keep, t)
		}
	}
	if len(keep) == 0 {
		delete(s.fails, login)
	} else {
		s.fails[login] = keep
	}
	return len(keep) >= maxLoginFails
}

func remoteHost(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

// clientIP is the real client address. X-Forwarded-For is honoured only when
// the connection comes from a trusted proxy (e.g. a reverse proxy); then
// the right-most address that is not itself a trusted proxy wins, so a client
// cannot spoof it by sending its own header.
func (s *Server) clientIP(r *http.Request) string {
	host := remoteHost(r)
	addr, err := netip.ParseAddr(host)
	if err != nil || !s.trusted(addr) {
		return host
	}
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		if !s.trusted(a) {
			return a.String()
		}
	}
	return host
}

func (s *Server) trusted(a netip.Addr) bool {
	a = a.Unmap()
	for _, p := range s.d.TrustedProxies {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

const (
	maxFailKeys = 10000
	// overflowKey is a shared bucket used once the per-client table is full, so
	// flooding it with distinct keys cannot switch the limiter off.
	overflowKey = "overflow"
)

func (s *Server) recordFail(login string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.fails[login]; !ok && len(s.fails) >= maxFailKeys-1 {
		now := s.d.Now()
		for k, ts := range s.fails {
			if len(ts) == 0 || now.Sub(ts[len(ts)-1]) >= loginFailWindw {
				delete(s.fails, k)
			}
		}
		if len(s.fails) >= maxFailKeys-1 {
			login = overflowKey
			if len(s.fails[login]) >= maxLoginFails {
				return
			}
		}
	}
	s.fails[login] = append(s.fails[login], s.d.Now())
}

func (s *Server) loginPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	password := r.PostFormValue("password")
	key := "login:" + s.clientIP(r)
	if s.limited(key) || s.limited(overflowKey) {
		w.Header().Set("Retry-After", fmt.Sprint(int(loginFailWindw.Seconds())))
		s.render(w, http.StatusTooManyRequests, "login.html", map[string]any{"Error": "Zbyt wiele nieudanych prób. Spróbuj ponownie za 15 minut."})
		return
	}
	// constant time; an empty AppPassword never matches
	if s.d.AppPassword == "" || !passwordMatches(password, s.d.AppPassword) {
		s.recordFail(key)
		s.render(w, http.StatusUnauthorized, "login.html", map[string]any{"Error": "Nieprawidłowe hasło."})
		return
	}
	s.mu.Lock()
	delete(s.fails, key)
	s.mu.Unlock()
	s.setSession(w, s.d.Login)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// passwordMatches hashes both sides first so the compare is constant time
// and the length of the real password does not leak.
func passwordMatches(got, want string) bool {
	a, b := sha256.Sum256([]byte(got)), sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

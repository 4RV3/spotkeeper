package web

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientIPBehindTrustedProxy(t *testing.T) {
	s := &Server{d: Deps{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.1/32")}}}
	cases := []struct {
		name, remote, xff, want string
	}{
		{"direct client, header ignored", "203.0.113.7:5000", "1.2.3.4", "203.0.113.7"},
		{"via trusted proxy", "192.0.2.1:41000", "198.51.100.9", "198.51.100.9"},
		{"spoofed left entry ignored", "192.0.2.1:41000", "6.6.6.6, 198.51.100.9", "198.51.100.9"},
		{"proxy without header", "192.0.2.1:41000", "", "192.0.2.1"},
		{"garbage header", "192.0.2.1:41000", "nonsense", "192.0.2.1"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("POST", "/login", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := s.clientIP(r); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestClientIPNoTrustedProxies(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest("POST", "/login", nil)
	r.RemoteAddr = "192.0.2.1:41000"
	r.Header.Set("X-Forwarded-For", "198.51.100.9")
	if got := s.clientIP(r); got != "192.0.2.1" {
		t.Fatalf("header must be ignored without TRUSTED_PROXIES, got %q", got)
	}
}

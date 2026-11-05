package publicip

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func server(t *testing.T, body string, status int) (*httptest.Server, *int32) {
	t.Helper()
	var n int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&n, 1)
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	return s, &n
}

func TestOK(t *testing.T) {
	s, _ := server(t, " 203.0.113.7\n", 200)
	ip, err := (&Lookup{URL: s.URL, HC: s.Client()}).IP(context.Background())
	if err != nil || ip != "203.0.113.7" {
		t.Fatalf("%q %v", ip, err)
	}
}

func TestCacheAndTTL(t *testing.T) {
	s, n := server(t, "203.0.113.7", 200)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	l := &Lookup{URL: s.URL, HC: s.Client(), TTL: time.Minute, Now: func() time.Time { return now }}
	for i := 0; i < 2; i++ {
		if _, err := l.IP(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if *n != 1 {
		t.Fatalf("cache hit refetched: %d", *n)
	}
	now = now.Add(2 * time.Minute)
	if _, err := l.IP(context.Background()); err != nil || *n != 2 {
		t.Fatalf("no refetch after TTL: %d %v", *n, err)
	}
}

func TestInvalidBody(t *testing.T) {
	s, _ := server(t, "<html>not an ip</html>", 200)
	if ip, err := (&Lookup{URL: s.URL, HC: s.Client()}).IP(context.Background()); err == nil {
		t.Fatalf("got %q", ip)
	}
}

func TestErrorNotCached(t *testing.T) {
	var n int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.WriteHeader(500)
			return
		}
		w.Write([]byte("203.0.113.7"))
	}))
	defer s.Close()
	l := &Lookup{URL: s.URL, HC: s.Client()}
	if _, err := l.IP(context.Background()); err == nil {
		t.Fatal("want error")
	}
	if ip, err := l.IP(context.Background()); err != nil || ip != "203.0.113.7" {
		t.Fatalf("%q %v", ip, err)
	}
}

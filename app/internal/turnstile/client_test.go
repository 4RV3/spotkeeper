package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"spotkeeper/internal/model"
)

const (
	goodPrefix = "https://app.example.com/turnstile?"
	goodURL    = goodPrefix + "x=1"
)

var hash = strings.Repeat("a", 64)

func serve(t *testing.T, status int, mod func(m map[string]any, req map[string]string)) (*Client, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		var req map[string]string
		json.NewDecoder(r.Body).Decode(&req)
		if r.URL.Path != "/v2/token" || r.Method != "POST" || req["turnstileUrl"] != goodURL {
			t.Errorf("bad request %s %s %v", r.Method, r.URL.Path, req)
		}
		m := map[string]any{
			"ok": true, "api_version": 2, "request_id": req["requestId"], "context_sha256": hash,
			"telemetry_acknowledged": true, "token": strings.Repeat("t", 30),
			"token_observed_at": time.Now().UTC().Format(time.RFC3339Nano),
		}
		if mod != nil {
			mod(m, req)
		}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(m)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, goodPrefix, 120*time.Second, srv.Client()), &calls
}

func tokErr(t *testing.T, err error) *model.TokenError {
	t.Helper()
	var te *model.TokenError
	if !errors.As(err, &te) {
		t.Fatalf("want TokenError, got %#v", err)
	}
	return te
}

func TestOK(t *testing.T) {
	c, _ := serve(t, 200, nil)
	tok, err := c.Token(context.Background(), goodURL, "r1")
	if err != nil || tok.Value != strings.Repeat("t", 30) || time.Since(tok.ObservedAt) > 5*time.Second {
		t.Fatalf("%v %v", tok, err)
	}
}

func TestMismatchedRequestID(t *testing.T) {
	c, _ := serve(t, 200, func(m map[string]any, _ map[string]string) { m["request_id"] = "other" })
	_, err := c.Token(context.Background(), goodURL, "r1")
	if te := tokErr(t, err); te.Retryable || te.Code != "invalid_token_response" {
		t.Fatalf("%+v", te)
	}
}

func Test503Retryable(t *testing.T) {
	c, _ := serve(t, 503, func(m map[string]any, _ map[string]string) { m["ok"] = false })
	_, err := c.Token(context.Background(), goodURL, "r1")
	if te := tokErr(t, err); !te.Retryable {
		t.Fatalf("%+v", te)
	}
}

func TestRetryableStatuses(t *testing.T) {
	for _, s := range []int{408, 422, 429, 500} {
		c, _ := serve(t, s, func(m map[string]any, _ map[string]string) { m["ok"] = false; m["code"] = "boom" })
		_, err := c.Token(context.Background(), goodURL, "r1")
		if te := tokErr(t, err); !te.Retryable || te.Code != "boom" {
			t.Fatalf("%d %+v", s, te)
		}
	}
	c, _ := serve(t, 400, func(m map[string]any, _ map[string]string) { m["ok"] = false })
	_, err := c.Token(context.Background(), goodURL, "r1")
	if te := tokErr(t, err); te.Retryable || te.Code != "token_service_failure" {
		t.Fatalf("%+v", te)
	}
}

func TestBadURL(t *testing.T) {
	c, calls := serve(t, 200, nil)
	_, err := c.Token(context.Background(), "https://evil.io/turnstile?x", "r1")
	if te := tokErr(t, err); te.Retryable {
		t.Fatalf("%+v", te)
	}
	if *calls != 0 {
		t.Fatal("HTTP call made")
	}
}

func TestStaleToken(t *testing.T) {
	c, _ := serve(t, 200, func(m map[string]any, _ map[string]string) {
		m["token_observed_at"] = time.Now().Add(-200 * time.Second).UTC().Format(time.RFC3339Nano)
	})
	_, err := c.Token(context.Background(), goodURL, "r1")
	tokErr(t, err)
}

func TestFutureToken(t *testing.T) {
	c, _ := serve(t, 200, func(m map[string]any, _ map[string]string) {
		m["token_observed_at"] = time.Now().Add(30 * time.Second).UTC().Format(time.RFC3339Nano)
	})
	_, err := c.Token(context.Background(), goodURL, "r1")
	tokErr(t, err)
}

func TestInvalidFields(t *testing.T) {
	muts := map[string]func(m map[string]any, _ map[string]string){
		"ok":        func(m map[string]any, _ map[string]string) { m["ok"] = false },
		"version":   func(m map[string]any, _ map[string]string) { m["api_version"] = 1 },
		"hash":      func(m map[string]any, _ map[string]string) { m["context_sha256"] = "zz" },
		"telemetry": func(m map[string]any, _ map[string]string) { m["telemetry_acknowledged"] = false },
		"short":     func(m map[string]any, _ map[string]string) { m["token"] = "short" },
	}
	for name, mod := range muts {
		c, _ := serve(t, 200, mod)
		_, err := c.Token(context.Background(), goodURL, "r1")
		if te := tokErr(t, err); te.Retryable || te.Code != "invalid_token_response" {
			t.Fatal(name, fmt.Sprintf("%+v", te))
		}
	}
}

func TestTransportErrorRetryable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	c := New(url, goodPrefix, time.Minute, http.DefaultClient)
	_, err := c.Token(context.Background(), goodURL, "r1")
	if te := tokErr(t, err); !te.Retryable {
		t.Fatalf("%+v", te)
	}
}

func TestTurnstilePrefixFromDomain(t *testing.T) {
	c, calls := serve(t, 200, nil)
	c.prefix = "https://other.example.org/turnstile?"
	_, err := c.Token(context.Background(), goodURL, "r1")
	if te := tokErr(t, err); te.Retryable || *calls != 0 {
		t.Fatalf("%+v calls=%d", te, *calls)
	}
	c.prefix = ""
	if _, err = c.Token(context.Background(), goodURL, "r1"); err == nil || *calls != 0 {
		t.Fatalf("empty prefix must reject: %v calls=%d", err, *calls)
	}
}

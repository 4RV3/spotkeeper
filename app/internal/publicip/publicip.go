// Package publicip detects the host's public IP address through an HTTP
// service that returns it as plain text.
package publicip

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	defaultTTL = 10 * time.Minute
	timeout    = 5 * time.Second
)

// Lookup fetches the public IP from URL and caches it for TTL. Errors are not
// cached. The zero values of HC, TTL and Now select sensible defaults.
type Lookup struct {
	URL string
	HC  *http.Client
	TTL time.Duration
	Now func() time.Time

	mu      sync.Mutex
	ip      string
	fetched time.Time
}

func (l *Lookup) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

// IP returns the public IP, from cache while it is fresher than TTL.
func (l *Lookup) IP(ctx context.Context) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ttl := l.TTL
	if ttl <= 0 {
		ttl = defaultTTL
	}
	if l.ip != "" && l.now().Sub(l.fetched) < ttl {
		return l.ip, nil
	}
	ip, err := l.fetch(ctx)
	if err != nil {
		return "", err
	}
	l.ip, l.fetched = ip, l.now()
	return ip, nil
}

func (l *Lookup) fetch(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.URL, nil)
	if err != nil {
		return "", fmt.Errorf("publicip: %w", err)
	}
	hc := l.HC
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("publicip: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("publicip: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return "", fmt.Errorf("publicip: %w", err)
	}
	a, err := netip.ParseAddr(strings.TrimSpace(string(body)))
	if err != nil {
		return "", fmt.Errorf("publicip: invalid response: %w", err)
	}
	return a.String(), nil
}

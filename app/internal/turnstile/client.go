// Package turnstile is the client for turnstile-service v2.
package turnstile

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"spotkeeper/internal/model"
)

const (
	requestTimout = 110 * time.Second
)

var hex64 = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Client fetches Cloudflare Turnstile tokens.
type Client struct {
	base   string
	prefix string
	maxAge time.Duration
	hc     *http.Client
}

// New builds a Client; urlPrefix is the only prefix accepted for verification
// URLs (https://<API_DOMAIN>/turnstile?) and maxAge is the largest accepted token age.
func New(baseURL, urlPrefix string, maxAge time.Duration, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{}
	}
	return &Client{base: strings.TrimRight(baseURL, "/"), prefix: urlPrefix, maxAge: maxAge, hc: hc}
}

type response struct {
	OK        bool   `json:"ok"`
	Version   int    `json:"api_version"`
	RequestID string `json:"request_id"`
	Hash      string `json:"context_sha256"`
	Telemetry bool   `json:"telemetry_acknowledged"`
	Token     string `json:"token"`
	Observed  string `json:"token_observed_at"`
	Code      string `json:"code"`
}

// Token requests a token for turnstileURL, correlated by requestID.
func (c *Client) Token(ctx context.Context, turnstileURL, requestID string) (model.Token, error) {
	if c.prefix == "" || !strings.HasPrefix(turnstileURL, c.prefix) {
		return model.Token{}, &model.TokenError{Code: "invalid_turnstile_url"}
	}
	b, _ := json.Marshal(map[string]string{"turnstileUrl": turnstileURL, "requestId": requestID})
	ctx, cancel := context.WithTimeout(ctx, requestTimout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v2/token", bytes.NewReader(b))
	if err != nil {
		return model.Token{}, &model.TokenError{Code: "token_service_failure"}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return model.Token{}, &model.TokenError{Retryable: true, Code: "token_service_failure"}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	status := resp.StatusCode
	var body response
	json.Unmarshal(raw, &body)

	var observed time.Time
	age := time.Duration(1 << 62)
	if t, err := time.Parse(time.RFC3339Nano, body.Observed); err == nil {
		observed = t
		age = time.Since(t)
	}
	ok := status == http.StatusOK && body.OK && body.Version == 2 &&
		body.RequestID == requestID && hex64.MatchString(body.Hash) &&
		body.Telemetry && len(body.Token) > 20 &&
		!observed.IsZero() && age >= -10*time.Second && age <= c.maxAge
	if ok {
		return model.Token{Value: body.Token, ObservedAt: observed}, nil
	}
	code := body.Code
	if code == "" {
		code = "token_service_failure"
		if status == http.StatusOK {
			code = "invalid_token_response"
		}
	}
	retry := status == 408 || status == 422 || status == 429 || status >= 500
	return model.Token{}, &model.TokenError{Retryable: retry, Code: code}
}

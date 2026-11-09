// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// Config is the runtime configuration. Field names are the CamelCase form of the
// environment variables.
type Config struct {
	// Login is the service login (ACCOUNT_LOGIN, lowercased): the single account.
	Login string
	// Password is the service password (ACCOUNT_PASSWORD).
	Password string
	// APIDomain is the host of the reservation service (API_DOMAIN, no scheme or path).
	APIDomain string
	// AppPassword protects the web UI (APP_PASSWORD, at least 8 characters).
	AppPassword string
	// DefaultAppPassword is true when APP_PASSWORD was unset and the default is used.
	DefaultAppPassword bool
	ListenAddr         string
	DBPath             string
	CookieSecure       bool
	// APIBase is the service API base URL: https://<API_DOMAIN>/api unless API_BASE is set.
	APIBase string
	// TurnstilePrefix is the only URL prefix accepted for verification URLs.
	TurnstilePrefix string
	AppVersion      string
	// UserAgent (USER_AGENT) is sent as the User-Agent of every service API
	// call; DefaultUserAgent (Chrome on Android) when unset, never Go's own.
	UserAgent    string
	TurnstileURL string
	PrepareLead  time.Duration
	PollInterval time.Duration
	MaxTokenAge  time.Duration
	// TrustedProxies (TRUSTED_PROXIES: IPs or CIDRs, comma-separated) may set
	// X-Forwarded-For.
	TrustedProxies []netip.Prefix
	// PublicIPURL (PUBLIC_IP_URL) returns the caller's public IP as plain text;
	// "off" disables detection (main maps a set-but-empty
	// variable to "off").
	PublicIPURL string
	// LogLevel is debug|info|warn|error.
	LogLevel string
}

// DefaultUserAgent is the User-Agent sent to the service API when USER_AGENT
// is unset: Chrome on Android (the mobile app's real one is not in its logs).
const DefaultUserAgent = "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Mobile Safari/537.36"

// loader reads variables with defaults and remembers the first parse error.
type loader struct {
	getenv func(string) string
	err    error
}

func (l *loader) fail(key string, err error) {
	if l.err == nil {
		l.err = fmt.Errorf("config: %s: %w", key, err)
	}
}

func (l *loader) str(key, def string) string {
	if v := l.getenv(key); v != "" {
		return v
	}
	return def
}

func (l *loader) boolean(key string, def bool) bool {
	v := l.getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.fail(key, err)
		return def
	}
	return b
}

func (l *loader) duration(key string, def time.Duration) time.Duration {
	v := l.getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		l.fail(key, err)
		return def
	}
	return d
}

const defaultAppPassword = "spotkeeper"

// Placeholders shipped in docker-compose.yml; Load refuses to start with them.
const (
	placeholderLogin    = "twoj@email.pl"
	placeholderPassword = "twoje-haslo"
	placeholderDomain   = "twoja-domena.pl"
)

// hostRe matches a bare DNS host name (letters, digits, dots, hyphens).
var hostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

// Load builds the Config from getenv. API_DOMAIN, ACCOUNT_LOGIN and ACCOUNT_PASSWORD are
// required; APP_PASSWORD defaults to "spotkeeper".
func Load(getenv func(string) string) (Config, error) {
	l := &loader{getenv: getenv}
	cfg := Config{
		APIDomain:    strings.ToLower(strings.TrimSpace(l.str("API_DOMAIN", ""))),
		Login:        strings.ToLower(strings.TrimSpace(l.str("ACCOUNT_LOGIN", ""))),
		Password:     l.str("ACCOUNT_PASSWORD", ""),
		AppPassword:  l.str("APP_PASSWORD", defaultAppPassword),
		ListenAddr:   l.str("LISTEN_ADDR", ":8080"),
		DBPath:       l.str("DB_PATH", "/data/spotkeeper.db"),
		CookieSecure: l.boolean("COOKIE_SECURE", false),
		AppVersion:   l.str("APP_VERSION", "4.7.1"),
		UserAgent:    strings.TrimSpace(l.str("USER_AGENT", DefaultUserAgent)),
		TurnstileURL: l.str("TURNSTILE_URL", "http://turnstile:3099"),
		PrepareLead:  l.duration("PREPARE_LEAD", 10*time.Minute),
		PollInterval: l.duration("POLL_INTERVAL", 500*time.Millisecond),
		MaxTokenAge:  l.duration("MAX_TOKEN_AGE", 30*time.Second),
		PublicIPURL:  l.str("PUBLIC_IP_URL", "https://api.ipify.org"),
		LogLevel:     strings.ToLower(strings.TrimSpace(l.str("LOG_LEVEL", "info"))),
	}
	if strings.EqualFold(cfg.PublicIPURL, "off") {
		cfg.PublicIPURL = ""
	}
	for _, p := range strings.Split(l.str("TRUSTED_PROXIES", ""), ",") {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		if !strings.Contains(p, "/") {
			a, err := netip.ParseAddr(p)
			if err != nil {
				l.fail("TRUSTED_PROXIES", err)
				continue
			}
			p = netip.PrefixFrom(a, a.BitLen()).String()
		}
		pfx, err := netip.ParsePrefix(p)
		if err != nil {
			l.fail("TRUSTED_PROXIES", err)
			continue
		}
		cfg.TrustedProxies = append(cfg.TrustedProxies, pfx.Masked())
	}
	if l.err != nil {
		return Config{}, l.err
	}
	if cfg.APIDomain == "" {
		return Config{}, fmt.Errorf("config: API_DOMAIN is required (host only, e.g. app.example.com) / " +
			"w pliku docker-compose.yml wpisz domenę serwisu rezerwacji (API_DOMAIN)")
	}
	if !hostRe.MatchString(cfg.APIDomain) {
		return Config{}, fmt.Errorf("config: API_DOMAIN must be a host name only, without https:// or a path, got %q", cfg.APIDomain)
	}
	if cfg.APIDomain == placeholderDomain {
		return Config{}, fmt.Errorf("config: API_DOMAIN still holds the placeholder from docker-compose.yml; " +
			"edit it there / " +
			"w pliku docker-compose.yml wpisz domenę serwisu rezerwacji zamiast \"twoja-domena.pl\"")
	}
	if cfg.Login == "" {
		return Config{}, fmt.Errorf("config: ACCOUNT_LOGIN is required")
	}
	if !emailRe.MatchString(cfg.Login) {
		return Config{}, fmt.Errorf("ACCOUNT_LOGIN must be an e-mail address")
	}
	if cfg.Password == "" {
		return Config{}, fmt.Errorf("config: ACCOUNT_PASSWORD is required")
	}
	if cfg.Login == placeholderLogin || cfg.Password == placeholderPassword {
		return Config{}, fmt.Errorf("config: ACCOUNT_LOGIN / ACCOUNT_PASSWORD still hold the placeholders from docker-compose.yml; " +
			"edit them there / " +
			"w pliku docker-compose.yml wpisz swój login i hasło do serwisu zamiast \"twoj@email.pl\" i \"twoje-haslo\"")
	}
	cfg.APIBase = l.str("API_BASE", "https://"+cfg.APIDomain+"/api")
	cfg.TurnstilePrefix = "https://" + cfg.APIDomain + "/turnstile?"
	cfg.DefaultAppPassword = l.getenv("APP_PASSWORD") == ""
	if len(cfg.AppPassword) < 8 {
		return Config{}, fmt.Errorf("APP_PASSWORD must be at least 8 characters")
	}
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return Config{}, fmt.Errorf("config: LOG_LEVEL must be debug|info|warn|error, got %q", cfg.LogLevel)
	}
	return cfg, nil
}

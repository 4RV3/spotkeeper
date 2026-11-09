package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func required() map[string]string {
	return map[string]string{"API_DOMAIN": "app.example.com", "ACCOUNT_LOGIN": "Jan@Example.com", "ACCOUNT_PASSWORD": "pw", "APP_PASSWORD": "secret123"}
}

func TestDefaults(t *testing.T) {
	cfg, err := Load(envOf(required()))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		Login:           "jan@example.com",
		Password:        "pw",
		AppPassword:     "secret123",
		ListenAddr:      ":8080",
		DBPath:          "/data/spotkeeper.db",
		APIDomain:       "app.example.com",
		APIBase:         "https://app.example.com/api",
		TurnstilePrefix: "https://app.example.com/turnstile?",
		AppVersion:      "4.7.1",
		UserAgent:       DefaultUserAgent,
		TurnstileURL:    "http://turnstile:3099",
		PrepareLead:     10 * time.Minute,
		PollInterval:    500 * time.Millisecond,
		MaxTokenAge:     30 * time.Second,
		PublicIPURL:     "https://api.ipify.org",
		LogLevel:        "info",
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("Load defaults = %+v, want %+v", cfg, want)
	}
}

func TestAppPasswordDefault(t *testing.T) {
	for _, set := range []bool{false, true} {
		env := required()
		delete(env, "APP_PASSWORD")
		if set {
			env["APP_PASSWORD"] = ""
		}
		cfg, err := Load(envOf(env))
		if err != nil || cfg.AppPassword != "spotkeeper" || !cfg.DefaultAppPassword {
			t.Fatalf("cfg = %+v, err = %v", cfg, err)
		}
	}
}

func TestAppPasswordTooShort(t *testing.T) {
	env := required()
	env["APP_PASSWORD"] = "abc"
	_, err := Load(envOf(env))
	if err == nil || err.Error() != "APP_PASSWORD must be at least 8 characters" {
		t.Fatalf("err = %v", err)
	}
}

func TestAppPasswordCustom(t *testing.T) {
	env := required()
	env["APP_PASSWORD"] = "12345678"
	cfg, err := Load(envOf(env))
	if err != nil || cfg.AppPassword != "12345678" || cfg.DefaultAppPassword {
		t.Fatalf("cfg = %+v, err = %v", cfg, err)
	}
}

func TestLoadRequiresLogin(t *testing.T) {
	for _, key := range []string{"API_DOMAIN", "ACCOUNT_LOGIN", "ACCOUNT_PASSWORD"} {
		env := required()
		delete(env, key)
		_, err := Load(envOf(env))
		if err == nil || !strings.Contains(err.Error(), key) {
			t.Fatalf("missing %s: err = %v", key, err)
		}
	}
}

func TestNoRemovedEnv(t *testing.T) {
	src, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"TELEGRAM", "LOKI", "INFISICAL", "MONITOR", "ACCESS_CODE", "ADMIN_LOGINS",
		"PARKING_ID", "PAYMENT_AMOUNT", "RELEASE_TIME", "PUSH_SUBJECT", "SESSION_SECRET"} {
		if strings.Contains(string(src), word) {
			t.Errorf("config.go still mentions %s", word)
		}
	}
}

func TestOverride(t *testing.T) {
	env := required()
	env["POLL_INTERVAL"] = "2s"
	env["DB_PATH"] = "x.db"
	env["COOKIE_SECURE"] = "true"
	cfg, err := Load(envOf(env))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PollInterval != 2*time.Second || cfg.DBPath != "x.db" || !cfg.CookieSecure {
		t.Fatalf("got %+v", cfg)
	}
}

func TestBadDuration(t *testing.T) {
	env := required()
	env["POLL_INTERVAL"] = "abc"
	_, err := Load(envOf(env))
	if err == nil || !strings.Contains(err.Error(), "POLL_INTERVAL") {
		t.Fatalf("err = %v", err)
	}
}

func TestLogLevel(t *testing.T) {
	env := required()
	env["LOG_LEVEL"] = "DEBUG"
	cfg, err := Load(envOf(env))
	if err != nil || cfg.LogLevel != "debug" {
		t.Fatalf("got %q %v", cfg.LogLevel, err)
	}
	env["LOG_LEVEL"] = "loud"
	if _, err := Load(envOf(env)); err == nil {
		t.Fatal("want error for bad LOG_LEVEL")
	}
}

func TestTrustedProxies(t *testing.T) {
	env := required()
	env["TRUSTED_PROXIES"] = " 192.0.2.1 , 198.51.100.0/24,2001:db8::1"
	cfg, err := Load(envOf(env))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range cfg.TrustedProxies {
		got = append(got, p.String())
	}
	if !reflect.DeepEqual(got, []string{"192.0.2.1/32", "198.51.100.0/24", "2001:db8::1/128"}) {
		t.Fatalf("got %v", got)
	}
	env["TRUSTED_PROXIES"] = "192.0.2"
	if _, err := Load(envOf(env)); err == nil {
		t.Fatal("want error for bad TRUSTED_PROXIES")
	}
}

func TestLoadRejectsNonEmailLogin(t *testing.T) {
	for _, login := range []string{"jan", "jan@x", "@example.com", "a b@example.com", "a@@example.com"} {
		env := required()
		env["ACCOUNT_LOGIN"] = login
		_, err := Load(envOf(env))
		if err == nil || err.Error() != "ACCOUNT_LOGIN must be an e-mail address" {
			t.Fatalf("%q: err = %v", login, err)
		}
	}
}

func TestPublicIPURL(t *testing.T) {
	c, err := Load(envOf(required()))
	if err != nil || c.PublicIPURL != "https://api.ipify.org" {
		t.Fatalf("%q %v", c.PublicIPURL, err)
	}
	env := required()
	env["PUBLIC_IP_URL"] = "off"
	if c, err = Load(envOf(env)); err != nil || c.PublicIPURL != "" {
		t.Fatalf("%q %v", c.PublicIPURL, err)
	}
}

func TestLoadRejectsPlaceholders(t *testing.T) {
	for key, val := range map[string]string{"ACCOUNT_LOGIN": "twoj@email.pl", "ACCOUNT_PASSWORD": "twoje-haslo", "API_DOMAIN": "twoja-domena.pl"} {
		env := required()
		env[key] = val
		_, err := Load(envOf(env))
		if err == nil || !strings.Contains(err.Error(), "docker-compose.yml") {
			t.Fatalf("%s=%q: err = %v", key, val, err)
		}
	}
}

func TestLoadAPIDomainRequired(t *testing.T) {
	env := required()
	delete(env, "API_DOMAIN")
	_, err := Load(envOf(env))
	if err == nil || !strings.Contains(err.Error(), "API_DOMAIN is required") {
		t.Fatalf("err = %v", err)
	}
	env["API_DOMAIN"] = "  "
	if _, err = Load(envOf(env)); err == nil || !strings.Contains(err.Error(), "API_DOMAIN is required") {
		t.Fatalf("blank: err = %v", err)
	}
}

func TestLoadAPIDomainRejectsURL(t *testing.T) {
	for _, d := range []string{"https://app.example.com", "app.example.com/api", "app.example.com:8443", "a b.example.com", "user@app.example.com", "app.example.com."} {
		env := required()
		env["API_DOMAIN"] = d
		_, err := Load(envOf(env))
		if err == nil || !strings.Contains(err.Error(), "API_DOMAIN must be a host name only") {
			t.Fatalf("%q: err = %v", d, err)
		}
	}
}

func TestAPIBaseDerived(t *testing.T) {
	cfg, err := Load(envOf(required()))
	if err != nil || cfg.APIBase != "https://app.example.com/api" {
		t.Fatalf("derived: %q %v", cfg.APIBase, err)
	}
	env := required()
	env["API_BASE"] = "http://localhost:9000/api"
	cfg, err = Load(envOf(env))
	if err != nil || cfg.APIBase != "http://localhost:9000/api" {
		t.Fatalf("override: %q %v", cfg.APIBase, err)
	}
}

func TestTurnstilePrefixFromDomain(t *testing.T) {
	env := required()
	env["API_DOMAIN"] = "Svc.Example.org"
	cfg, err := Load(envOf(env))
	if err != nil || cfg.TurnstilePrefix != "https://svc.example.org/turnstile?" {
		t.Fatalf("%q %v", cfg.TurnstilePrefix, err)
	}
}

func TestUserAgentDefaultIsChromeAndOverridable(t *testing.T) {
	cfg, _ := Load(envOf(required()))
	if !strings.Contains(cfg.UserAgent, "Chrome/") || strings.Contains(cfg.UserAgent, "Go-http-client") {
		t.Fatal(cfg.UserAgent)
	}
	env := required()
	env["USER_AGENT"] = "  okhttp/4.12.0 "
	cfg, _ = Load(envOf(env))
	if cfg.UserAgent != "okhttp/4.12.0" {
		t.Fatal(cfg.UserAgent)
	}
}

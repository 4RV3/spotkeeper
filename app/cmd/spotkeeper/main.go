// Command spotkeeper is the single-user parking reservation service.
package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata"

	"spotkeeper/internal/account"
	"spotkeeper/internal/api"
	"spotkeeper/internal/check"
	"spotkeeper/internal/config"
	"spotkeeper/internal/engine"
	"spotkeeper/internal/followup"
	"spotkeeper/internal/future"
	"spotkeeper/internal/logging"
	"spotkeeper/internal/model"
	"spotkeeper/internal/publicip"
	"spotkeeper/internal/sessionkey"
	"spotkeeper/internal/store"
	"spotkeeper/internal/turnstile"
	"spotkeeper/internal/web"
)

// version is the build's commit, set with -ldflags "-X main.version=..." (see Dockerfile).
var version = ""

// sessionSecret derives the cookie-signing key from the persisted key and the
// app password, so changing APP_PASSWORD revokes every session.
func sessionSecret(key []byte, appPassword string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(appPassword))
	return m.Sum(nil)
}

// app holds the wired components.
type app struct {
	cfg      config.Config
	store    *store.Store
	future   *future.Future
	followup *followup.Followup
	handler  http.Handler
	// settings-backed values the loops read (exposed for tests)
	releaseTime func() string
	amount      func() float64
	parkingID   func() string
}

// logNotifier reports every reservation result to the log.
type logNotifier struct{}

func (logNotifier) Result(_ context.Context, login, day string, r model.Result) {
	slog.Info("reservation_result", "login", login, "day", day, "status", r.Status)
}

// build wires all components from the configuration.
func build(cfg config.Config) (*app, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	// The single account's row must exist: tasks and days off hang on it.
	if _, err := st.GetUser(cfg.Login); errors.Is(err, model.ErrNotFound) {
		err = st.UpsertUser(model.User{Login: cfg.Login, Active: true})
		if err != nil {
			st.Close()
			return nil, fmt.Errorf("create user: %w", err)
		}
	} else if err != nil {
		st.Close()
		return nil, fmt.Errorf("read user: %w", err)
	}
	key, err := sessionkey.LoadOrCreate(filepath.Join(filepath.Dir(cfg.DBPath), "session.key"))
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("session key: %w", err)
	}
	hc := api.NewHTTPClient()
	pc := api.New(cfg.APIBase, cfg.AppVersion, hc)
	tok := turnstile.New(cfg.TurnstileURL, cfg.TurnstilePrefix, cfg.MaxTokenAge, hc)
	prep := account.NewPreparer(pc, account.Static(cfg.Login, cfg.Password), "")
	// GUI settings are read on every use, so edits apply without a restart.
	settings := func() store.AppSettings {
		set, err := st.AppSettings()
		if err != nil {
			slog.Warn("read settings", "err", err.Error())
		}
		return set // defaults are filled in even on error
	}
	parkingID := func() string { return settings().ParkingID }
	prep.ParkingID = parkingID
	var publicIP func(context.Context) (string, error)
	if cfg.PublicIPURL != "" {
		publicIP = (&publicip.Lookup{URL: cfg.PublicIPURL}).IP
		prep.PublicIP = publicIP
	}

	eng := engine.New(pc, tok, model.EngineConfig{
		MaxAttempts:     10,
		PayPolls:        20,
		PollInterval:    cfg.PollInterval,
		Budget:          450 * time.Second,
		PayInterval:     2 * time.Second,
		PaymentAmount:   15, // overridden by eng.Amount
		TurnstilePrefix: cfg.TurnstilePrefix,
	}, engine.NewClaims())
	amount := func() float64 { return settings().PaymentAmount }
	eng.Amount = amount
	// A session that expires mid-run (HTTP 401) gets one fresh login.
	eng.Relogin = func(ctx context.Context, login string) (string, error) {
		prep.Invalidate(login)
		return prep.Session(ctx, login)
	}
	// Unconfirmed payments are re-checked after 5 and 30 min; every result
	// (paid, unconfirmed, failed) is logged.
	var results followup.Notifier = logNotifier{}
	fu := followup.New(pc, prep, st, results, slog.Default())
	results = fu.Wrap(results)

	releaseTime := func() string { return settings().ReleaseTime }
	fut := future.NewFuture(st, prep, eng, results, future.FutureConfig{
		ReleaseFunc: releaseTime,
		PrepareLead: cfg.PrepareLead,
		AheadDays:   28,
	})
	runCheck := func(ctx context.Context, progress func(int, string, bool, check.Step)) []check.Step {
		return check.Run(ctx, check.Deps{
			User:     func() (model.User, error) { return st.GetUser(cfg.Login) },
			Settings: st.AppSettings,
			Prepare:  prep.Prepare,
			PublicIP: publicIP,
			Offers:   pc.Offers,
			Levels:   pc.ParkingLevels,
			Token:    tok.Token,
			Now:      time.Now,
			Progress: progress,
		})
	}
	h := web.NewServer(web.Deps{
		Store:          st,
		Login:          cfg.Login,
		AppPassword:    cfg.AppPassword,
		SessionSecret:  sessionSecret(key, cfg.AppPassword),
		CookieSecure:   cfg.CookieSecure,
		Now:            time.Now,
		AheadDays:      28,
		TrustedProxies: cfg.TrustedProxies,
		Parkings: func(ctx context.Context) ([]model.Parking, error) {
			jwt, err := prep.Session(ctx, cfg.Login)
			if err != nil {
				return nil, err
			}
			return pc.ParkingsList(ctx, jwt)
		},
		Levels: func(ctx context.Context, parkingID string) ([]string, error) {
			jwt, err := prep.Session(ctx, cfg.Login)
			if err != nil {
				return nil, err
			}
			return pc.ParkingLevels(ctx, jwt, parkingID, time.Now())
		},
		Check: func(ctx context.Context) []check.Step { return runCheck(ctx, nil) },
		CheckStream: func(ctx context.Context, p func(int, string, bool, check.Step)) []check.Step {
			return runCheck(ctx, p)
		},
	})
	return &app{cfg: cfg, store: st, future: fut, followup: fu, handler: h,
		releaseTime: releaseTime, amount: amount, parkingID: parkingID}, nil
}

// envGetter is os.Getenv, except that a set-but-empty PUBLIC_IP_URL means
// "disable lookup" (reported as "off") while an unset one keeps the default.
func envGetter(key string) string {
	v, set := os.LookupEnv(key)
	if key == "PUBLIC_IP_URL" && set && v == "" {
		return "off"
	}
	return v
}

// run wires everything, serves until ctx ends, then shuts down gracefully.
// The listen address is sent on ready (if non-nil) once the server accepts connections.
func run(ctx context.Context, getenv func(string) string, ready chan<- string) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	slog.SetDefault(logging.Setup(cfg.LogLevel))
	if cfg.DefaultAppPassword {
		slog.Warn("default_app_password", "hint", "set APP_PASSWORD in docker-compose.yml")
	}
	a, err := build(cfg)
	if err != nil {
		return err
	}
	defer a.store.Close()

	ln, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	srv := &http.Server{Handler: a.handler, ReadHeaderTimeout: 10 * time.Second}

	loopCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	loop := func(name string, f func(context.Context) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := f(loopCtx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("loop stopped", "loop", name, "err", err.Error())
			}
		}()
	}
	a.followup.Run(loopCtx) // before the loops: results must see the bound ctx
	loop("future", a.future.Loop)

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	slog.Info("listening", "addr", ln.Addr().String())
	if ready != nil {
		ready <- ln.Addr().String()
	}

	var result error
	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			result = err
		}
	}
	cancel()
	sctx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer scancel()
	if err := srv.Shutdown(sctx); err != nil && result == nil {
		result = err
	}
	wg.Wait()
	a.followup.Wait()
	return result
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, envGetter, nil); err != nil {
		slog.Error("fatal", "err", err.Error())
		os.Exit(1)
	}
}

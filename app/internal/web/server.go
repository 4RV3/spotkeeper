// Package web serves the UI.
package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"html/template"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"sync"
	"time"

	"spotkeeper/internal/check"
	"spotkeeper/internal/model"
	"spotkeeper/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/app.css static/app.js static/logo.svg static/icon.svg static/icon-180.png static/icon-192.png static/icon-512.png static/icon-512-maskable.png
var staticFS embed.FS

type asset struct {
	body        []byte
	contentType string
	version     string
}

// assets are loaded once; version is a short content hash for cache busting.
var assets = func() map[string]asset {
	out := map[string]asset{}
	for name, ct := range map[string]string{
		"app.css":               "text/css; charset=utf-8",
		"app.js":                "text/javascript; charset=utf-8",
		"logo.svg":              "image/svg+xml",
		"icon.svg":              "image/svg+xml",
		"icon-512-maskable.png": "image/png",
		"icon-180.png":          "image/png",
		"icon-192.png":          "image/png",
		"icon-512.png":          "image/png",
	} {
		b, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			panic(err)
		}
		h := sha256.Sum256(b)
		out[name] = asset{b, ct, hex.EncodeToString(h[:4])}
	}
	return out
}()

func serveStatic(w http.ResponseWriter, r *http.Request) {
	a, ok := assets[r.PathValue("name")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", a.contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(a.body)
}

// Store is the persistence the web layer needs.
type Store interface {
	UpsertUser(model.User) error
	GetUser(login string) (model.User, error)
	ListTasks(login string, limit int) ([]model.Task, error)
	ListTasksInRange(login, from, to string) ([]model.Task, error)
	FirstTaskDay(login string) (string, error)
	AddDayOff(login, day string) error
	RemoveDayOff(login, day string) error
	ListDaysOff(login string) ([]string, error)
	AppSettings() (store.AppSettings, error)
	SaveAppSettings(store.AppSettings) error
}

// Deps are the dependencies of the web server.
type Deps struct {
	Store Store
	// Login is the one service account (lowercase); every session belongs to it.
	Login string
	// AppPassword is the password of the web UI.
	AppPassword   string
	SessionSecret []byte
	CookieSecure  bool
	Now           func() time.Time
	AheadDays     int
	// TrustedProxies may set X-Forwarded-For; empty = use the connection address only.
	TrustedProxies []netip.Prefix
	// Check runs the configuration check behind GET/POST /check.
	Check func(ctx context.Context) []check.Step
	// CheckStream is Check with step progress callbacks (optional; POST /check/stream).
	CheckStream func(ctx context.Context, progress func(i int, name string, done bool, s check.Step)) []check.Step
	// Parkings lists the parkings for the picker (nil: the picker only shows the
	// stored one). Levels lists the spot levels of a parking.
	Parkings func(ctx context.Context) ([]model.Parking, error)
	Levels   func(ctx context.Context, parkingID string) ([]string, error)
}

// Server is the HTTP handler.
type Server struct {
	d     Deps
	mux   *http.ServeMux
	tmpl  map[string]*template.Template
	mu    sync.Mutex
	fails map[string][]time.Time

	cacheMu   sync.Mutex
	parkingsC parkingsCache
	levelsC   map[string]levelsCache
}

// releaseTime is the daily "HH:MM" (Europe/Warsaw) the day AheadDays ahead opens:
// the stored setting, "11:00" when it cannot be read.
func (s *Server) releaseTime() string {
	if a, err := s.d.Store.AppSettings(); err == nil && a.ReleaseTime != "" {
		return a.ReleaseTime
	}
	return "11:00"
}

// dashboardRoutes is set by the dashboard file to register its routes.
var dashboardRoutes func(s *Server, mux *http.ServeMux)

// NewServer builds the handler.
func NewServer(d Deps) http.Handler {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.AheadDays == 0 {
		d.AheadDays = 28
	}
	if len(d.SessionSecret) < 16 {
		panic("web: SessionSecret must be at least 16 bytes")
	}
	s := &Server{d: d, mux: http.NewServeMux(), tmpl: map[string]*template.Template{}, fails: map[string][]time.Time{}}
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		panic(err)
	}
	for _, e := range entries {
		if e.Name() == "layout.html" {
			continue
		}
		s.tmpl[e.Name()] = template.Must(template.New("layout.html").Funcs(funcs).ParseFS(templateFS, "templates/layout.html", path.Join("templates", e.Name())))
	}
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	s.mux.HandleFunc("GET /static/{name}", serveStatic)
	s.mux.HandleFunc("GET /login", s.loginForm)
	s.mux.HandleFunc("POST /login", s.loginPost)
	s.mux.HandleFunc("POST /logout", func(w http.ResponseWriter, r *http.Request) {
		s.clearSession(w)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
	s.pwaRoutes(s.mux)
	s.mux.HandleFunc("GET /pomoc", s.helpPage)
	s.mux.HandleFunc("GET /check", s.requireUser(s.checkPage))
	s.mux.HandleFunc("POST /check", s.requireUser(s.checkPage))
	s.mux.HandleFunc("POST /check/stream", s.requireUser(s.checkStream))
	if dashboardRoutes != nil {
		dashboardRoutes(s, s.mux)
	} else {
		s.mux.HandleFunc("GET /{$}", s.requireUser(func(w http.ResponseWriter, r *http.Request, login string) {
			w.Write([]byte("SpotKeeper: " + login))
		}))
	}
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", csp)
	if s.d.CookieSecure {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
	}
	s.mux.ServeHTTP(w, r)
}

// csp allows only same-origin resources: no inline scripts or styles.
const csp = "default-src 'self'; style-src 'self'; script-src 'self'; " +
	"base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// sameOrigin rejects POSTs whose Origin (or, absent that, Referer or
// Sec-Fetch-Site) shows a cross-site origin.
func (s *Server) sameOrigin(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return true
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		return err == nil && u.Host == r.Host
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		u, err := url.Parse(ref)
		return err == nil && u.Host == r.Host
	}
	if sf := r.Header.Get("Sec-Fetch-Site"); sf == "cross-site" || sf == "same-site" {
		return false
	}
	return true
}

func (s *Server) render(w http.ResponseWriter, status int, page string, data map[string]any) {
	t, ok := s.tmpl[page]
	if !ok {
		http.Error(w, "template missing", http.StatusInternalServerError)
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	data["CSSV"] = assets["app.css"].version
	data["JSV"] = assets["app.js"].version
	data["LogoV"] = assets["logo.svg"].version
	data["IconV"] = assets["icon.svg"].version
	if _, set := data["User"]; !set {
		data["User"] = ""
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout.html", data); err != nil {
		slog.Error("render", "page", page, "err", err)
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

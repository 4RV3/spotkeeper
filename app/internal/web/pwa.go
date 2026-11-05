package web

import "net/http"

// manifest is the PWA manifest: installable, standalone, opens the dashboard.
const manifest = `{
 "name": "SpotKeeper",
 "short_name": "SpotKeeper",
 "description": "Automatyczne rezerwacje miejsc parkingowych.",
 "lang": "pl",
 "start_url": "/",
 "scope": "/",
 "display": "standalone",
 "background_color": "#18181c",
 "theme_color": "#18181c",
 "icons": [
  {"src": "/static/icon-192.png", "sizes": "192x192", "type": "image/png", "purpose": "any"},
  {"src": "/static/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "any"},
  {"src": "/static/icon-512-maskable.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable"}
 ]
}`

func (s *Server) pwaRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /manifest.webmanifest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write([]byte(manifest))
	})
}

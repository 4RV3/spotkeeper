package web

import (
	"strings"
	"testing"
)

func TestNoSerifFont(t *testing.T) {
	css := strings.ToLower(string(assets["app.css"].body))
	for _, bad := range []string{"cinzel", "@font-face", "georgia", "trajan"} {
		if strings.Contains(css, bad) {
			t.Errorf("app.css still references %q", bad)
		}
	}
}

func TestLayoutNoOldBrand(t *testing.T) {
	b, err := templateFS.ReadFile("templates/layout.html")
	if err != nil {
		t.Fatal(err)
	}
	l := strings.ToLower(string(b))
	for _, bad := range []string{"tour", "news"} {
		if strings.Contains(l, bad) {
			t.Errorf("layout contains %q", bad)
		}
	}
	if !strings.Contains(string(b), "Nieoficjalny klient · MIT") || !strings.Contains(string(b), "SpotKeeper") {
		t.Error("footer/wordmark missing")
	}
}

func TestNoFontAssetsAndManifestColors(t *testing.T) {
	for name := range assets {
		if strings.HasPrefix(name, "fonts/") {
			t.Errorf("asset %s should not exist", name)
		}
	}
	if !strings.Contains(manifest, `"background_color": "#18181c"`) || !strings.Contains(manifest, `"theme_color": "#18181c"`) {
		t.Error("manifest colors are not #18181c")
	}
}

func TestThemeToggleRendered(t *testing.T) {
	e := newDEnv(t)
	for _, path := range []string{"/", "/login"} {
		b := get(e, path)
		if !strings.Contains(b, `id="theme-toggle"`) || !strings.Contains(b, `app.js?v=`) || strings.Contains(b, "defer") ||
			!strings.Contains(b, `media="(prefers-color-scheme: light)"`) {
			t.Errorf("%s: theme toggle / blocking script / theme-color metas missing", path)
		}
	}
	if !strings.Contains(string(assets["app.js"].body), "localStorage.getItem('theme')") {
		t.Error("app.js does not restore the saved theme")
	}
	css := string(assets["app.css"].body)
	if !strings.Contains(css, "prefers-color-scheme:light") || !strings.Contains(css, `[data-theme=dark]`) {
		t.Error("css lacks light/dark variants")
	}
}

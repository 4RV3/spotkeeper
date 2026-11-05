package web

import (
	"strings"
	"testing"
)

// Old brand names, split so this file stays free of them.
var oldBrands = []string{"park" + "cash", "fart" + "cash"}

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
	for _, name := range []string{"layout", "dashboard", "login", "check"} {
		tb, err := templateFS.ReadFile("templates/" + name + ".html")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(string(tb)), oldBrands[0]) {
			t.Errorf("%s.html mentions the old brand", name)
		}
	}
	l := strings.ToLower(string(b))
	for _, bad := range []string{oldBrands[1], "tour", "news"} {
		if strings.Contains(l, bad) {
			t.Errorf("layout contains %q", bad)
		}
	}
	if !strings.Contains(string(b), "Nieoficjalny klient · MIT") || !strings.Contains(string(b), "SpotKeeper") {
		t.Error("footer/wordmark missing")
	}
	for _, name := range []string{"app.css", "app.js", "logo.svg", "icon.svg"} {
		s := strings.ToLower(string(assets[name].body))
		if strings.Contains(s, oldBrands[0]) || strings.Contains(s, oldBrands[1]) {
			t.Errorf("%s has old brand", name)
		}
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

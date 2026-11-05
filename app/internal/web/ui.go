package web

import "html/template"

// icons are trusted constant SVG snippets (never built from user data).
var icons = map[string]template.HTML{
	"clock":  `<svg class="ic" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg>`,
	"check":  `<svg class="ic" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="m8 12 3 3 5-6"/></svg>`,
	"cal":    `<svg class="ic" viewBox="0 0 24 24" aria-hidden="true"><rect x="3" y="5" width="18" height="16" rx="2"/><path d="M3 10h18M8 3v4m8-4v4"/></svg>`,
	"moon":   `<svg class="ic" viewBox="0 0 24 24" aria-hidden="true"><path d="M20 14.5A8 8 0 0 1 9.5 4 8 8 0 1 0 20 14.5z"/></svg>`,
	"sun":    `<svg class="ic" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5M17.5 17.5 19 19M5 19l1.5-1.5M17.5 6.5 19 5"/></svg>`,
	"logout": `<svg class="ic" viewBox="0 0 24 24" aria-hidden="true"><path d="M9 4H5a1 1 0 0 0-1 1v14a1 1 0 0 0 1 1h4M16 8l4 4-4 4M20 12H9"/></svg>`,
	"gear":   `<svg class="ic" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="3"/><path d="M12 2v3m0 14v3M2 12h3m14 0h3M4.9 4.9 7 7m10 10 2.1 2.1M4.9 19.1 7 17M17 7l2.1-2.1"/></svg>`,
	"list":   `<svg class="ic" viewBox="0 0 24 24" aria-hidden="true"><path d="M8 6h13M8 12h13M8 18h13M3 6h.5M3 12h.5M3 18h.5"/></svg>`,
}

// marks are the small calendar icons for day markers and task statuses (keyed by the
// badge/pill class); phones show them instead of the text labels.
var marks = map[string]string{
	"auto":        `<path d="M17 2l3 3-3 3"/><path d="M4 11V9a4 4 0 0 1 4-4h12"/><path d="M7 22l-3-3 3-3"/><path d="M20 13v2a4 4 0 0 1-4 4H4"/>`,
	"vac":         `<path d="M3 12a9 9 0 0 1 18 0z"/><path d="M12 12v7a2 2 0 0 1-4 0"/>`,
	"paid":        `<path d="m5 12 5 5 9-10"/>`,
	"running":     `<circle cx="12" cy="12" r="8"/><path d="M12 8v4l2.5 2"/>`,
	"pending":     `<circle cx="12" cy="12" r="8"/><path d="M12 8v4l2.5 2"/>`,
	"unconfirmed": `<path d="M12 4v10M12 19v1"/>`,
	"failed":      `<path d="M6 6l12 12M18 6 6 18"/>`,
	"skipped":     `<path d="m5 6 6 6-6 6M13 6l6 6-6 6"/>`,
	"cancelled":   `<path d="M5 12h14"/>`,
}

var funcs = template.FuncMap{
	"add":  func(a, b int) int { return a + b },
	"icon": func(name string) template.HTML { return icons[name] },
	"mark": func(name string) template.HTML {
		p, ok := marks[name]
		if !ok {
			return ""
		}
		return template.HTML(`<svg class="mk" viewBox="0 0 24 24" aria-hidden="true">` + p + `</svg>`)
	},
}

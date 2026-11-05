package web

import (
	"encoding/json"
	"net/http"

	"spotkeeper/internal/check"
)

// checkPage is the configuration check: GET shows the intro, POST runs it.
func (s *Server) checkPage(w http.ResponseWriter, r *http.Request, login string) {
	data := map[string]any{"User": login, "Title": "Kontrola", "Available": s.d.Check != nil, "StepNames": check.StepNames, "Stream": s.d.CheckStream != nil || s.d.Check != nil}
	if r.Method == http.MethodPost && s.d.Check != nil {
		if !s.sameOrigin(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var steps []check.Step
		steps = s.d.Check(r.Context())
		data["Steps"] = steps
		data["Ran"] = true
		allOK := len(steps) > 0
		for _, st := range steps {
			allOK = allOK && st.OK
		}
		data["AllOK"] = allOK
	}
	s.render(w, http.StatusOK, "check.html", data)
}

// stepState maps a finished step to its stream state.
func stepState(st check.Step) string {
	switch {
	case st.Skipped:
		return "skipped"
	case st.OK:
		return "ok"
	}
	return "fail"
}

// checkStream runs the configuration check and streams its progress as NDJSON:
// {"i":0,"name":"..","state":"running"}, {"i":0,"state":"ok|fail|skipped","detail":".."}
// and finally {"done":true,"allOk":bool}. Each line is flushed at once.
func (s *Server) checkStream(w http.ResponseWriter, r *http.Request, _ string) {
	if !s.sameOrigin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if s.d.Check == nil && s.d.CheckStream == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	fl, _ := w.(http.Flusher)
	enc := json.NewEncoder(w)
	emit := func(v map[string]any) {
		_ = enc.Encode(v)
		if fl != nil {
			fl.Flush()
		}
	}
	progress := func(i int, name string, done bool, st check.Step) {
		if !done {
			emit(map[string]any{"i": i, "name": name, "state": "running"})
			return
		}
		m := map[string]any{"i": i, "name": name, "state": stepState(st), "detail": st.Detail}
		if st.Help != "" {
			m["help"] = st.Help
		}
		emit(m)
	}
	var steps []check.Step
	if s.d.CheckStream != nil {
		steps = s.d.CheckStream(r.Context(), progress)
	} else {
		steps = s.d.Check(r.Context())
		for i, st := range steps {
			progress(i, st.Name, true, st)
		}
	}
	allOK := len(steps) > 0
	for _, st := range steps {
		allOK = allOK && st.OK
	}
	emit(map[string]any{"done": true, "allOk": allOK})
}

// helpPage serves GET /pomoc: the onboarding checklist and FAQ. It is public and
// shows no account data; a valid session only adds the user's header.
func (s *Server) helpPage(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{"Title": "Pomoc", "Wide": true}
	if login, ok := s.currentUser(r); ok && login == s.d.Login {
		data["User"] = login
	}
	s.render(w, http.StatusOK, "help.html", data)
}

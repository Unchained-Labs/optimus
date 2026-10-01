// Package web serves the optimus dashboard in a browser: the same fleet,
// sessions, handoffs and usage as the TUI, plus live interactive terminals for
// every agent, so sessions can be driven from another machine or a phone.
package web

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Unchained-Labs/optimus/internal/app"
	"github.com/Unchained-Labs/optimus/internal/config"
	"github.com/Unchained-Labs/optimus/internal/index"
	"github.com/Unchained-Labs/optimus/internal/model"
	"github.com/Unchained-Labs/optimus/internal/mux"
	"github.com/Unchained-Labs/optimus/internal/providers"
	"github.com/Unchained-Labs/optimus/internal/ratelimits"
	"github.com/Unchained-Labs/optimus/internal/usage"
)

//go:embed static
var staticFS embed.FS

const cookieName = "optimus_token"

type Server struct {
	app   *app.App
	token string

	mu       sync.Mutex
	idx      *index.Index
	limits   []ratelimits.Window
	loadedAt time.Time
}

func New(a *app.App) (*Server, error) {
	tok, err := config.WebToken()
	if err != nil {
		return nil, err
	}
	mux.SweepViews()
	return &Server{app: a, token: tok}, nil
}

// index returns the session index, rescanning when stale or forced.
func (s *Server) index(force bool) (*index.Index, []ratelimits.Window) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if force || s.idx == nil || time.Since(s.loadedAt) > 15*time.Second {
		s.idx = s.app.Index()
		s.limits = ratelimits.Load()
		s.loadedAt = time.Now()
	}
	return s.idx, s.limits
}

func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	static, _ := fs.Sub(staticFS, "static")
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	m.HandleFunc("GET /{$}", s.page)
	m.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))

	api := http.NewServeMux()
	api.HandleFunc("GET /api/state", s.state)
	api.HandleFunc("GET /api/sessions", s.sessions)
	api.HandleFunc("GET /api/sessions/{id}/transcript", s.transcript)
	api.HandleFunc("POST /api/sessions/{id}/resume", s.resume)
	api.HandleFunc("POST /api/sessions/{id}/handoff", s.handoff)
	api.HandleFunc("POST /api/windows", s.launch)
	api.HandleFunc("POST /api/windows/{id}/send", s.send)
	api.HandleFunc("POST /api/windows/{id}/keys", s.keys)
	api.HandleFunc("POST /api/windows/{id}/rename", s.rename)
	api.HandleFunc("DELETE /api/windows/{id}", s.kill)
	api.HandleFunc("GET /api/windows/{id}/screen", s.screen)
	api.HandleFunc("GET /api/usage", s.usage)
	api.HandleFunc("GET /api/term/{id}", s.term)
	m.Handle("/api/", s.auth(api))
	return securityHeaders(m)
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		h.ServeHTTP(w, r)
	})
}

// page serves the app; a ?token= link logs the browser in with a cookie.
func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	if t := r.URL.Query().Get("token"); t != "" {
		if s.validToken(t) {
			http.SetCookie(w, &http.Cookie{Name: cookieName, Value: t, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 90 * 24 * 3600})
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	b, _ := staticFS.ReadFile("static/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}

func (s *Server) validToken(t string) bool {
	return len(t) == len(s.token) && subtle.ConstantTimeCompare([]byte(t), []byte(s.token)) == 1
}

// auth requires the token (cookie or bearer header) and, for anything that
// changes state, a same-origin request.
func (s *Server) auth(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := ""
		if c, err := r.Cookie(cookieName); err == nil {
			tok = c.Value
		}
		if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
			tok = strings.TrimPrefix(a, "Bearer ")
		}
		if !s.validToken(tok) {
			httpErr(w, http.StatusUnauthorized, "missing or wrong token — open the URL printed by `optimus web --url`")
			return
		}
		if r.Method != http.MethodGet && !sameOrigin(r) {
			httpErr(w, http.StatusForbidden, "cross-origin request refused")
			return
		}
		h.ServeHTTP(w, r)
	})
}

func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true // non-browser client holding the token
	}
	u, err := url.Parse(o)
	return err == nil && u.Host == r.Host
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func httpErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return errors.New("bad request body: " + err.Error())
	}
	return nil
}

// --- views -------------------------------------------------------------------

type windowView struct {
	ID        string    `json:"id"`
	Index     int       `json:"index"`
	Name      string    `json:"name"`
	Agent     string    `json:"agent"`
	Cwd       string    `json:"cwd"`
	State     mux.State `json:"state"`
	Activity  time.Time `json:"activity"`
	Created   time.Time `json:"created"`
	SessionID string    `json:"session_id,omitempty"`
	Title     string    `json:"title,omitempty"`
	Cost      float64   `json:"cost"`
}

type sessionView struct {
	Agent     string    `json:"agent"`
	ID        string    `json:"id"`
	ShortID   string    `json:"short_id"`
	Cwd       string    `json:"cwd"`
	Project   string    `json:"project"`
	Title     string    `json:"title"`
	Model     string    `json:"model"`
	Branch    string    `json:"branch,omitempty"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Turns     int       `json:"turns"`
	Tokens    int64     `json:"tokens"`
	Cost      float64   `json:"cost"`
	Live      bool      `json:"live"`
	Status    string    `json:"status,omitempty"`
	Resumable bool      `json:"resumable"`
}

func viewSession(s *model.Session) sessionView {
	resumable := false
	if p := providers.Get(s.Agent); p != nil {
		resumable = p.ResumeArgs(s.ID, providers.LaunchOpts{}) != nil
	}
	return sessionView{s.Agent, s.ID, s.ShortID(), s.Cwd, s.ProjectName(), s.DisplayTitle(), s.Model, s.Branch, s.Start, s.End, s.Messages, s.Usage.Total(), s.Cost, s.Live, s.Status, resumable}
}

type agentView struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	History   bool   `json:"history"`
	Command   string `json:"command"`
}

type projectView struct {
	Cwd      string         `json:"cwd"`
	Name     string         `json:"name"`
	Last     time.Time      `json:"last"`
	Sessions int            `json:"sessions"`
	Agents   map[string]int `json:"agents"`
	Cost7d   float64        `json:"cost_7d"`
	Cost     float64        `json:"cost"`
	Live     int            `json:"live"`
	Exists   bool           `json:"exists"`
}

type windowSummary struct {
	Today  float64             `json:"today"`
	Week   float64             `json:"week"`
	Month  float64             `json:"month"`
	Block  *blockView          `json:"block,omitempty"`
	Limits []ratelimits.Window `json:"limits"`
	Budget []usage.Budget      `json:"budgets"`
}

type blockView struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Cost   float64   `json:"cost"`
	Tokens int64     `json:"tokens"`
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	idx, limits := s.index(r.URL.Query().Get("reload") == "1")
	now := time.Now()
	cfg := s.app.Cfg

	ws, err := mux.List()
	if err != nil {
		httpErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	windows := make([]windowView, 0, len(ws))
	managed := map[string]bool{}
	for _, x := range ws {
		screen, _ := mux.Capture(x.ID, 30)
		v := windowView{ID: x.ID, Index: x.Index, Name: x.Name, Agent: x.Agent, Cwd: x.Cwd, State: mux.Detect(x, screen), Activity: x.Activity, Created: x.Created, SessionID: x.SessionID}
		if sess := idx.ForWindow(x.Agent, x.Cwd, x.SessionID, x.Created); sess != nil {
			v.SessionID, v.Title, v.Cost = sess.ID, sess.DisplayTitle(), sess.Cost
			managed[sess.ID] = true
		}
		windows = append(windows, v)
	}
	outside := []providers.LiveSession{}
	for _, l := range idx.Live {
		if !managed[l.ID] {
			outside = append(outside, l)
		}
	}

	agents := []agentView{}
	for _, p := range providers.All() {
		bin, _ := providers.Command(cfg, p)
		srcs, _ := p.Discover()
		_, generic := p.(providers.Generic)
		agents = append(agents, agentView{Name: p.Name(), Installed: providers.Installed(cfg, p), History: !generic || len(srcs) > 0, Command: bin})
	}

	projects := []projectView{}
	for i, p := range usage.Projects(idx.Sessions, s.app.Prices, now) {
		if i >= 40 {
			break
		}
		st, err := os.Stat(p.Cwd)
		projects = append(projects, projectView{p.Cwd, p.Name, p.Last, p.Sessions, p.Agents, p.Cost7d, p.Cost, p.Live, err == nil && st.IsDir()})
	}

	sum := windowSummary{
		Today:  usage.Total(idx.Sessions, s.app.Prices, usage.Filter{Since: usage.StartOfDay(now)}).Cost,
		Week:   usage.Total(idx.Sessions, s.app.Prices, usage.Filter{Since: usage.StartOfWeek(now)}).Cost,
		Month:  usage.Total(idx.Sessions, s.app.Prices, usage.Filter{Since: usage.StartOfMonth(now)}).Cost,
		Limits: []ratelimits.Window{},
		Budget: usage.Budgets(idx.Sessions, s.app.Prices, cfg, now),
	}
	for _, l := range limits {
		if !l.Stale(now) {
			sum.Limits = append(sum.Limits, l)
		}
	}
	if bl := usage.Blocks(idx.Sessions, s.app.Prices, "claude", cfg.BlockHours, now); len(bl) > 0 && bl[len(bl)-1].Active {
		b := bl[len(bl)-1]
		sum.Block = &blockView{b.Start, b.End, b.Cost, b.Usage.Total()}
	}
	if sum.Budget == nil {
		sum.Budget = []usage.Budget{}
	}

	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	writeJSON(w, map[string]any{
		"windows":               windows,
		"outside":               outside,
		"agents":                agents,
		"default_agent":         cfg.Agent(),
		"claude_remote_control": cfg.ClaudeRemoteControl(),
		"projects":              projects,
		"summary":               sum,
		"home":                  home,
		"cwd":                   cwd,
		"tmux":                  mux.Available(),
		"sessions_total":        len(idx.Sessions),
		"loaded":                idx.Loaded,
	})
}

func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	idx, _ := s.index(q.Get("reload") == "1")
	text := strings.ToLower(strings.TrimSpace(q.Get("q")))
	agent, cwd := q.Get("agent"), q.Get("cwd")
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 200
	}
	out := []sessionView{}
	total := 0.0
	for _, ss := range idx.Sessions {
		if (agent != "" && ss.Agent != agent) || (cwd != "" && ss.Cwd != cwd) {
			continue
		}
		if text != "" && !strings.Contains(strings.ToLower(ss.DisplayTitle()+" "+ss.Cwd+" "+ss.Agent+" "+ss.ID+" "+ss.Model), text) {
			continue
		}
		total += ss.Cost
		if len(out) < limit {
			out = append(out, viewSession(ss))
		}
	}
	writeJSON(w, map[string]any{"sessions": out, "total_cost": total})
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) *model.Session {
	idx, _ := s.index(false)
	ss := idx.Find(r.PathValue("id"))
	if ss == nil {
		httpErr(w, http.StatusNotFound, "no such session")
	}
	return ss
}

type messageView struct {
	Role  string           `json:"role"`
	Text  string           `json:"text"`
	Time  time.Time        `json:"time"`
	Tools []model.ToolCall `json:"tools,omitempty"`
}

func (s *Server) transcript(w http.ResponseWriter, r *http.Request) {
	ss := s.session(w, r)
	if ss == nil {
		return
	}
	msgs, err := providers.Get(ss.Agent).Transcript(ss)
	if err != nil {
		httpErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]messageView, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, messageView{m.Role, m.Text, m.Time, m.Tools})
	}
	writeJSON(w, map[string]any{"session": viewSession(ss), "messages": out})
}

func (s *Server) resume(w http.ResponseWriter, r *http.Request) {
	ss := s.session(w, r)
	if ss == nil {
		return
	}
	id, err := s.app.Resume(ss)
	if err != nil {
		httpErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]string{"window": id})
}

func (s *Server) handoff(w http.ResponseWriter, r *http.Request) {
	ss := s.session(w, r)
	if ss == nil {
		return
	}
	var req struct {
		Target    string `json:"target"` // new:<agent> | win:<id> | file
		Note      string `json:"note"`
		Summarize bool   `json:"summarize"`
		Dir       string `json:"dir"`
	}
	if err := readJSON(r, &req); err != nil {
		httpErr(w, http.StatusBadRequest, err.Error())
		return
	}
	switch {
	case strings.HasPrefix(req.Target, "new:"):
		id, path, err := s.app.HandoffTo(ss, strings.TrimPrefix(req.Target, "new:"), req.Dir, req.Note, req.Summarize)
		if err != nil {
			httpErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, map[string]string{"window": id, "path": path})
	case strings.HasPrefix(req.Target, "win:"):
		path, err := s.app.HandoffInto(ss, strings.TrimPrefix(req.Target, "win:"), req.Note, req.Summarize)
		if err != nil {
			httpErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, map[string]string{"path": path})
	default:
		doc, path, err := s.app.HandoffDoc(ss, req.Note, req.Summarize)
		if err != nil {
			httpErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, map[string]string{"path": path, "document": doc})
	}
}

func (s *Server) launch(w http.ResponseWriter, r *http.Request) {
	var req app.LaunchRequest
	if err := readJSON(r, &req); err != nil {
		httpErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.HasPrefix(req.Dir, "~") {
		home, _ := os.UserHomeDir()
		req.Dir = home + strings.TrimPrefix(req.Dir, "~")
	}
	if req.Dir == "" {
		httpErr(w, http.StatusBadRequest, "pick a project directory")
		return
	}
	if st, err := os.Stat(req.Dir); err != nil || !st.IsDir() {
		httpErr(w, http.StatusBadRequest, "not a directory: "+req.Dir)
		return
	}
	id, err := s.app.LaunchWith(req)
	if err != nil {
		httpErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]string{"window": id})
}

// window resolves the {id} path value to a live optimus window.
func window(w http.ResponseWriter, r *http.Request) (mux.Window, bool) {
	ws, err := mux.List()
	if err == nil {
		for _, x := range ws {
			if x.ID == r.PathValue("id") {
				return x, true
			}
		}
	}
	httpErr(w, http.StatusNotFound, "no such window")
	return mux.Window{}, false
}

func (s *Server) send(w http.ResponseWriter, r *http.Request) {
	win, ok := window(w, r)
	if !ok {
		return
	}
	var req struct {
		Text   string `json:"text"`
		Submit *bool  `json:"submit"`
	}
	if err := readJSON(r, &req); err != nil {
		httpErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := mux.Send(win.ID, req.Text, req.Submit == nil || *req.Submit); err != nil {
		httpErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) keys(w http.ResponseWriter, r *http.Request) {
	win, ok := window(w, r)
	if !ok {
		return
	}
	var req struct {
		Keys []string `json:"keys"`
	}
	if err := readJSON(r, &req); err != nil {
		httpErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := mux.Keys(win.ID, req.Keys...); err != nil {
		httpErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) rename(w http.ResponseWriter, r *http.Request) {
	win, ok := window(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		httpErr(w, http.StatusBadRequest, "name required")
		return
	}
	if err := mux.Rename(win.ID, req.Name); err != nil {
		httpErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) kill(w http.ResponseWriter, r *http.Request) {
	win, ok := window(w, r)
	if !ok {
		return
	}
	if err := mux.Kill(win.ID); err != nil {
		httpErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) screen(w http.ResponseWriter, r *http.Request) {
	win, ok := window(w, r)
	if !ok {
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if n <= 0 || n > 500 {
		n = 60
	}
	out, err := mux.Capture(win.ID, n)
	if err != nil {
		httpErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"screen": out, "state": mux.Detect(win, out)})
}

type usageRow struct {
	Key      string  `json:"key"`
	Cost     float64 `json:"cost"`
	Tokens   int64   `json:"tokens"`
	Sessions int     `json:"sessions"`
	Priced   bool    `json:"priced"`
}

func (s *Server) usage(w http.ResponseWriter, r *http.Request) {
	idx, _ := s.index(false)
	now := time.Now()
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 365 {
		days = 30
	}
	conv := func(rows []usage.Row) []usageRow {
		out := make([]usageRow, 0, len(rows))
		for _, x := range rows {
			out = append(out, usageRow{x.Key, x.Cost, x.Usage.Total(), x.Sessions, s.app.Prices.Known(x.Key)})
		}
		return out
	}
	since := usage.StartOfDay(now).AddDate(0, 0, -(days - 1))
	f := usage.Filter{Since: since}
	byProject := conv(usage.GroupBy(idx.Sessions, s.app.Prices, f, "project"))
	sort.SliceStable(byProject, func(i, j int) bool { return byProject[i].Cost > byProject[j].Cost })
	writeJSON(w, map[string]any{
		"days":       days,
		"daily":      conv(usage.Daily(idx.Sessions, s.app.Prices, usage.Filter{}, days, now)),
		"by_model":   conv(usage.GroupBy(idx.Sessions, s.app.Prices, f, "model")),
		"by_agent":   conv(usage.GroupBy(idx.Sessions, s.app.Prices, f, "agent")),
		"by_project": byProject,
		"total":      usage.Total(idx.Sessions, s.app.Prices, f).Cost,
	})
}

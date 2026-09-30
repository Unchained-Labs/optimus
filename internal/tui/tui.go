// Package tui is the optimus dashboard: live agents, sessions, projects and
// usage in one full-screen view.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/wardn/optimus/internal/app"
	"github.com/wardn/optimus/internal/clip"
	"github.com/wardn/optimus/internal/index"
	"github.com/wardn/optimus/internal/model"
	"github.com/wardn/optimus/internal/mux"
	"github.com/wardn/optimus/internal/providers"
	"github.com/wardn/optimus/internal/ratelimits"
)

type tab int

const (
	tabAgents tab = iota
	tabSessions
	tabProjects
	tabUsage
)

var tabNames = []string{"Agents", "Sessions", "Projects", "Usage"}

type mode int

const (
	modeNormal mode = iota
	modePick        // picker modal
	modeInput       // single-line text input modal
	modeConfirm     // y/n
	modeDetail      // transcript viewer
	modeFilter      // typing into the sessions filter
	modeHelp
)

// pickItem is one row of a picker.
type pickItem struct {
	label, detail string
	value         string
}

type picker struct {
	title    string
	items    []pickItem
	cur      int
	filter   textinput.Model
	filtered []pickItem
	freeText bool // Enter with no match uses the typed text as value
	onPick   func(m *Model, value string) tea.Cmd
}

func (p *picker) refilter() {
	q := strings.ToLower(strings.TrimSpace(p.filter.Value()))
	p.filtered = p.filtered[:0]
	for _, it := range p.items {
		if q == "" || strings.Contains(strings.ToLower(it.label+" "+it.detail), q) {
			p.filtered = append(p.filtered, it)
		}
	}
	if p.cur >= len(p.filtered) {
		p.cur = max(len(p.filtered)-1, 0)
	}
}

type Model struct {
	app  *app.App
	idx  *index.Index
	tab  tab
	mode mode
	w, h int

	// agents tab
	windows []mux.Window
	states  map[string]mux.State
	preview string
	wCur    int
	marked  map[string]bool

	// sessions tab
	sCur        int
	sOff        int
	filter      textinput.Model
	agentFilter string
	cwdFilter   string
	sessions    []*model.Session

	// projects tab
	pCur, pOff int

	// modals
	pick        *picker
	input       textinput.Model
	inputTitle  string
	onInput     func(m *Model, v string) tea.Cmd
	confirmText string
	onConfirm   func(m *Model) tea.Cmd
	detail      viewport.Model
	detailSess  *model.Session

	limits   []ratelimits.Window
	status   string
	statusAt time.Time
	isErr    bool
	loading  bool
	busy     string
}

// --- messages -----------------------------------------------------------------

type tickMsg time.Time
type indexMsg struct {
	idx    *index.Index
	limits []ratelimits.Window
}
type windowsMsg struct {
	ws      []mux.Window
	states  map[string]mux.State
	preview string
	err     error
}
type doneMsg struct {
	text   string
	err    error
	attach string // window to attach to afterwards
	reload bool
}
type attachedMsg struct{ err error }
type transcriptMsg struct {
	s    *model.Session
	msgs []model.Message
	err  error
}

func Run() error {
	m := New()
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

func New() *Model {
	f := textinput.New()
	f.Prompt = "/ "
	f.Placeholder = "filter sessions"
	in := textinput.New()
	in.Prompt = "› "
	m := &Model{app: app.New(), states: map[string]mux.State{}, marked: map[string]bool{}, filter: f, input: in, loading: true, idx: &index.Index{}}
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.loadIndex(), m.pollWindows(), tick())
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) loadIndex() tea.Cmd {
	a := m.app
	return func() tea.Msg { return indexMsg{idx: a.Index(), limits: ratelimits.Load()} }
}

func (m *Model) selectedWindow() *mux.Window {
	if m.wCur >= 0 && m.wCur < len(m.windows) {
		return &m.windows[m.wCur]
	}
	return nil
}

func (m *Model) pollWindows() tea.Cmd {
	sel := ""
	if w := m.selectedWindow(); w != nil {
		sel = w.ID
	}
	lines := max(m.h, 40)
	return func() tea.Msg {
		ws, err := mux.List()
		states := map[string]mux.State{}
		preview := ""
		if sel == "" && len(ws) > 0 {
			sel = ws[0].ID
		}
		for _, w := range ws {
			screen, _ := mux.Capture(w.ID, lines)
			states[w.ID] = mux.Detect(w, screen)
			if w.ID == sel {
				preview = screen
			}
		}
		return windowsMsg{ws: ws, states: states, preview: preview, err: err}
	}
}

func (m *Model) flash(s string, err bool) {
	m.status, m.statusAt, m.isErr = s, time.Now(), err
}

// --- update -------------------------------------------------------------------

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.detail.Width, m.detail.Height = m.w-4, m.h-5
		return m, nil

	case tickMsg:
		cmds := []tea.Cmd{tick(), m.pollWindows()}
		if time.Since(m.idx.Loaded) > 30*time.Second && !m.loading {
			m.loading = true
			cmds = append(cmds, m.loadIndex())
		}
		return m, tea.Batch(cmds...)

	case indexMsg:
		m.idx, m.limits, m.loading = msg.idx, msg.limits, false
		m.applyFilter()
		return m, nil

	case windowsMsg:
		if msg.err == nil {
			// keep the cursor on the same window when the list changes
			var cur string
			if w := m.selectedWindow(); w != nil {
				cur = w.ID
			}
			m.windows, m.states, m.preview = msg.ws, msg.states, msg.preview
			for i, w := range m.windows {
				if w.ID == cur {
					m.wCur = i
				}
			}
			if m.wCur >= len(m.windows) {
				m.wCur = max(len(m.windows)-1, 0)
			}
		}
		return m, nil

	case doneMsg:
		m.busy = ""
		if msg.err != nil {
			m.flash(msg.err.Error(), true)
		} else if msg.text != "" {
			m.flash(msg.text, false)
		}
		var cmds []tea.Cmd
		if msg.reload {
			m.loading = true
			cmds = append(cmds, m.loadIndex())
		}
		cmds = append(cmds, m.pollWindows())
		if msg.attach != "" && msg.err == nil {
			cmds = append(cmds, m.attach(msg.attach))
		}
		return m, tea.Batch(cmds...)

	case attachedMsg:
		if msg.err != nil {
			m.flash("attach: "+msg.err.Error(), true)
		}
		m.loading = true
		return m, tea.Batch(m.loadIndex(), m.pollWindows(), tea.ClearScreen)

	case transcriptMsg:
		m.busy = ""
		if msg.err != nil {
			m.flash(msg.err.Error(), true)
			return m, nil
		}
		m.detailSess = msg.s
		m.detail = viewport.New(m.w-4, m.h-5)
		m.detail.SetContent(renderTranscript(msg.s, msg.msgs, m.w-6))
		m.detail.GotoBottom()
		m.mode = modeDetail
		return m, nil

	case tea.MouseMsg:
		if m.mode == modeDetail {
			var cmd tea.Cmd
			m.detail, cmd = m.detail.Update(msg)
			return m, cmd
		}
		if msg.Action == tea.MouseActionPress {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				m.move(-1)
			case tea.MouseButtonWheelDown:
				m.move(1)
			}
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) attach(id string) tea.Cmd {
	return tea.ExecProcess(mux.AttachCmd(id), func(err error) tea.Msg { return attachedMsg{err} })
}

func (m *Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := k.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.mode {
	case modePick:
		return m.keyPick(k)
	case modeInput:
		switch key {
		case "esc":
			m.mode = modeNormal
			return m, nil
		case "enter":
			m.mode = modeNormal
			v := m.input.Value()
			if m.onInput != nil {
				return m, m.onInput(m, v)
			}
			return m, nil
		case "tab":
			m.input.SetValue(completePath(m.input.Value()))
			m.input.CursorEnd()
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(k)
		return m, cmd
	case modeConfirm:
		m.mode = modeNormal
		if key == "y" || key == "Y" || key == "enter" {
			return m, m.onConfirm(m)
		}
		return m, nil
	case modeHelp:
		m.mode = modeNormal
		return m, nil
	case modeFilter:
		switch key {
		case "esc":
			m.filter.SetValue("")
			m.filter.Blur()
			m.mode = modeNormal
			m.applyFilter()
			return m, nil
		case "enter", "down", "up":
			m.filter.Blur()
			m.mode = modeNormal
			return m, nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(k)
		m.applyFilter()
		return m, cmd
	case modeDetail:
		switch key {
		case "esc", "q", "backspace":
			m.mode = modeNormal
			return m, nil
		case "r":
			return m, m.resume(m.detailSess)
		case "h":
			m.mode = modeNormal
			return m, m.handoffPicker(m.detailSess, false)
		case "H":
			m.mode = modeNormal
			return m, m.handoffPicker(m.detailSess, true)
		case "y":
			return m, m.copyHandoff(m.detailSess)
		}
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(k)
		return m, cmd
	}

	// normal mode: global keys
	switch key {
	case "q":
		return m, tea.Quit
	case "?":
		m.mode = modeHelp
		return m, nil
	case "1", "2", "3", "4":
		m.tab = tab(key[0] - '1')
		return m, nil
	case "tab":
		m.tab = (m.tab + 1) % 4
		return m, nil
	case "shift+tab":
		m.tab = (m.tab + 3) % 4
		return m, nil
	case "R":
		m.loading = true
		m.flash("reloading…", false)
		return m, m.loadIndex()
	case "j", "down":
		m.move(1)
		return m, m.pollWindowsIfAgents()
	case "k", "up":
		m.move(-1)
		return m, m.pollWindowsIfAgents()
	case "g", "home":
		m.move(-1 << 20)
		return m, nil
	case "G", "end":
		m.move(1 << 20)
		return m, nil
	case "pgdown", "ctrl+d":
		m.move(m.listHeight() / 2)
		return m, nil
	case "pgup", "ctrl+u":
		m.move(-m.listHeight() / 2)
		return m, nil
	case "n":
		return m, m.newAgentPicker("")
	}

	switch m.tab {
	case tabAgents:
		return m.keyAgents(key)
	case tabSessions:
		return m.keySessions(key)
	case tabProjects:
		return m.keyProjects(key)
	}
	return m, nil
}

func (m *Model) pollWindowsIfAgents() tea.Cmd {
	if m.tab == tabAgents {
		return m.pollWindows()
	}
	return nil
}

func (m *Model) move(d int) {
	clamp := func(v, n int) int {
		if v >= n {
			v = n - 1
		}
		if v < 0 {
			v = 0
		}
		return v
	}
	switch m.tab {
	case tabAgents:
		m.wCur = clamp(m.wCur+d, len(m.windows))
	case tabSessions:
		m.sCur = clamp(m.sCur+d, len(m.sessions))
	case tabProjects:
		m.pCur = clamp(m.pCur+d, len(m.projects()))
	}
}

// --- agents tab -----------------------------------------------------------------

func (m *Model) keyAgents(key string) (tea.Model, tea.Cmd) {
	w := m.selectedWindow()
	switch key {
	case "enter", "a", "l", "right":
		if w != nil {
			return m, m.attach(w.ID)
		}
	case " ":
		if w != nil {
			m.marked[w.ID] = !m.marked[w.ID]
			if !m.marked[w.ID] {
				delete(m.marked, w.ID)
			}
			m.move(1)
		}
	case "s":
		if w != nil {
			id, name := w.ID, w.Name
			m.askInput("Send a prompt to "+name, "", func(m *Model, v string) tea.Cmd {
				if strings.TrimSpace(v) == "" {
					return nil
				}
				return func() tea.Msg {
					return doneMsg{text: "sent to " + name, err: mux.Send(id, v, true)}
				}
			})
		}
	case "b":
		targets := m.broadcastTargets()
		if len(targets) == 0 {
			m.flash("no running agents", true)
			return m, nil
		}
		label := fmt.Sprintf("Broadcast to %d agents", len(targets))
		if len(m.marked) == 0 {
			label += " (all — mark some with space to narrow)"
		}
		m.askInput(label, "", func(m *Model, v string) tea.Cmd {
			if strings.TrimSpace(v) == "" {
				return nil
			}
			return func() tea.Msg {
				for _, id := range targets {
					if err := mux.Send(id, v, true); err != nil {
						return doneMsg{err: err}
					}
				}
				return doneMsg{text: fmt.Sprintf("broadcast to %d agents", len(targets))}
			}
		})
	case "x", "d":
		if w != nil {
			id, name := w.ID, w.Name
			m.confirm(fmt.Sprintf("Kill %s? (y/N)", name), func(m *Model) tea.Cmd {
				delete(m.marked, id)
				return func() tea.Msg { return doneMsg{text: "killed " + name, err: mux.Kill(id)} }
			})
		}
	case "r":
		if w != nil {
			id := w.ID
			m.askInput("Rename window", w.Name, func(m *Model, v string) tea.Cmd {
				return func() tea.Msg { return doneMsg{err: mux.Rename(id, v)} }
			})
		}
	case "h", "H":
		if w != nil {
			if s := m.sessionForWindow(*w); s != nil {
				return m, m.handoffPicker(s, key == "H")
			}
			m.flash("no transcript known for this window yet (it may not have saved one)", true)
		}
	case "o":
		if w != nil {
			if s := m.sessionForWindow(*w); s != nil {
				return m, m.openDetail(s)
			}
			m.flash("no transcript known for this window yet", true)
		}
	}
	return m, nil
}

func (m *Model) broadcastTargets() []string {
	var ids []string
	for _, w := range m.windows {
		if w.Dead {
			continue
		}
		if len(m.marked) == 0 || m.marked[w.ID] {
			ids = append(ids, w.ID)
		}
	}
	return ids
}

// sessionForWindow finds the transcript of the agent running in a window: by
// the id optimus assigned, else the newest session of that agent in that dir
// that started after the window.
func (m *Model) sessionForWindow(w mux.Window) *model.Session {
	if w.SessionID != "" {
		if s := m.idx.Find(w.SessionID); s != nil {
			return s
		}
	}
	for _, s := range m.idx.Sessions { // newest first
		if s.Agent == w.Agent && s.Cwd == w.Cwd && (w.Created.IsZero() || !s.End.Before(w.Created)) {
			return s
		}
	}
	return nil
}

// --- sessions tab -----------------------------------------------------------------

func (m *Model) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.sessions = m.sessions[:0]
	for _, s := range m.idx.Sessions {
		if m.agentFilter != "" && s.Agent != m.agentFilter {
			continue
		}
		if m.cwdFilter != "" && s.Cwd != m.cwdFilter {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(s.DisplayTitle()+" "+s.Cwd+" "+s.Agent+" "+s.ID+" "+s.Model), q) {
			continue
		}
		m.sessions = append(m.sessions, s)
	}
	if m.sCur >= len(m.sessions) {
		m.sCur = max(len(m.sessions)-1, 0)
	}
}

func (m *Model) selectedSession() *model.Session {
	if m.sCur >= 0 && m.sCur < len(m.sessions) {
		return m.sessions[m.sCur]
	}
	return nil
}

func (m *Model) keySessions(key string) (tea.Model, tea.Cmd) {
	s := m.selectedSession()
	switch key {
	case "/":
		m.mode = modeFilter
		m.filter.Focus()
		return m, textinput.Blink
	case "esc":
		m.filter.SetValue("")
		m.cwdFilter, m.agentFilter = "", ""
		m.applyFilter()
	case "a":
		m.agentFilter = nextAgent(m.agentFilter, m.idx.Sessions)
		m.applyFilter()
	case "enter", "o":
		if s != nil {
			return m, m.openDetail(s)
		}
	case "r":
		if s != nil {
			return m, m.resume(s)
		}
	case "h", "H":
		if s != nil {
			return m, m.handoffPicker(s, key == "H")
		}
	case "y":
		if s != nil {
			return m, m.copyHandoff(s)
		}
	case "p":
		if s != nil {
			m.cwdFilter = s.Cwd
			m.applyFilter()
		}
	}
	return m, nil
}

func nextAgent(cur string, ss []*model.Session) string {
	seen := map[string]bool{}
	var names []string
	for _, s := range ss {
		if !seen[s.Agent] {
			seen[s.Agent] = true
			names = append(names, s.Agent)
		}
	}
	sort.Strings(names)
	names = append([]string{""}, names...)
	for i, n := range names {
		if n == cur {
			return names[(i+1)%len(names)]
		}
	}
	return ""
}

func (m *Model) openDetail(s *model.Session) tea.Cmd {
	m.busy = "loading transcript…"
	return func() tea.Msg {
		p := providers.Get(s.Agent)
		msgs, err := p.Transcript(s)
		return transcriptMsg{s: s, msgs: msgs, err: err}
	}
}

func (m *Model) resume(s *model.Session) tea.Cmd {
	m.mode = modeNormal
	m.busy = "resuming " + s.ShortID() + "…"
	a := m.app
	return func() tea.Msg {
		id, err := a.Resume(s)
		return doneMsg{text: "resumed " + s.ShortID(), err: err, attach: id}
	}
}

func (m *Model) copyHandoff(s *model.Session) tea.Cmd {
	m.busy = "building handoff…"
	a := m.app
	return func() tea.Msg {
		doc, path, err := a.HandoffDoc(s, "", false)
		if err != nil {
			return doneMsg{err: err}
		}
		how, err := clip.Copy(doc)
		return doneMsg{text: fmt.Sprintf("copied handoff (~%dk tokens) via %s · saved %s", len(doc)/4000, how, shortHome(path)), err: err}
	}
}

// handoffPicker asks where to send a session's context.
func (m *Model) handoffPicker(s *model.Session, summarize bool) tea.Cmd {
	var items []pickItem
	for _, p := range providers.All() {
		if providers.Installed(m.app.Cfg, p) && p.Name() != "shell" {
			items = append(items, pickItem{label: "new " + p.Name() + " session", detail: "in " + shortHome(s.Cwd), value: "new:" + p.Name()})
		}
	}
	for _, w := range m.windows {
		if !w.Dead {
			items = append(items, pickItem{label: fmt.Sprintf("into %d:%s", w.Index, w.Name), detail: w.Agent + " · " + shortHome(w.Cwd), value: "win:" + w.ID})
		}
	}
	items = append(items,
		pickItem{label: "copy to clipboard", value: "copy"},
		pickItem{label: "save to file", detail: "prints the path", value: "file"},
	)
	title := "Hand off “" + model.Truncate(s.DisplayTitle(), 40) + "” to…"
	if summarize {
		title += "  (condensed by " + m.app.Cfg.SummarizeWith + " first)"
	}
	a := m.app
	m.openPicker(title, items, false, func(m *Model, v string) tea.Cmd {
		m.busy = "transferring context…"
		if summarize {
			m.busy = "summarizing with " + a.Cfg.SummarizeWith + "… (this takes a moment)"
		}
		return func() tea.Msg {
			switch {
			case strings.HasPrefix(v, "new:"):
				agent := strings.TrimPrefix(v, "new:")
				id, path, err := a.HandoffTo(s, agent, "", "", summarize)
				return doneMsg{text: "started " + agent + " with context from " + shortHome(path), err: err, attach: id}
			case strings.HasPrefix(v, "win:"):
				path, err := a.HandoffInto(s, strings.TrimPrefix(v, "win:"), "", summarize)
				return doneMsg{text: "sent context " + shortHome(path), err: err}
			case v == "copy":
				doc, _, err := a.HandoffDoc(s, "", summarize)
				if err != nil {
					return doneMsg{err: err}
				}
				how, err := clip.Copy(doc)
				return doneMsg{text: "copied handoff via " + how, err: err}
			default:
				_, path, err := a.HandoffDoc(s, "", summarize)
				return doneMsg{text: "saved " + path, err: err}
			}
		}
	})
	return textinput.Blink
}

// --- projects tab ------------------------------------------------------------------

func (m *Model) keyProjects(key string) (tea.Model, tea.Cmd) {
	ps := m.projects()
	if m.pCur >= len(ps) {
		return m, nil
	}
	p := ps[m.pCur]
	switch key {
	case "enter", "o":
		m.cwdFilter = p.Cwd
		m.filter.SetValue("")
		m.applyFilter()
		m.sCur = 0
		m.tab = tabSessions
	case "c":
		return m, m.newAgentPicker(p.Cwd)
	}
	return m, nil
}

// --- new agent ----------------------------------------------------------------------

func (m *Model) newAgentPicker(dir string) tea.Cmd {
	var items []pickItem
	for _, p := range providers.All() {
		if providers.Installed(m.app.Cfg, p) {
			bin, _ := providers.Command(m.app.Cfg, p)
			items = append(items, pickItem{label: p.Name(), detail: bin, value: p.Name()})
		}
	}
	if len(items) == 0 {
		m.flash("no agents installed", true)
		return nil
	}
	if dir == "" {
		switch m.tab {
		case tabProjects:
			if ps := m.projects(); m.pCur < len(ps) {
				dir = ps[m.pCur].Cwd
			}
		}
	}
	m.openPicker("Start which agent?", items, false, func(m *Model, agent string) tea.Cmd {
		if dir != "" {
			return m.launch(agent, dir)
		}
		m.dirPicker(agent)
		return textinput.Blink
	})
	return textinput.Blink
}

func (m *Model) dirPicker(agent string) {
	wd, _ := os.Getwd()
	items := []pickItem{{label: shortHome(wd), detail: "current dir", value: wd}}
	seen := map[string]bool{wd: true}
	for _, p := range m.projects() {
		if p.Cwd == "" || seen[p.Cwd] {
			continue
		}
		if st, err := os.Stat(p.Cwd); err != nil || !st.IsDir() {
			continue
		}
		seen[p.Cwd] = true
		items = append(items, pickItem{label: shortHome(p.Cwd), detail: "last used " + ago(p.Last), value: p.Cwd})
	}
	m.openPicker("Start "+agent+" in… (type a path, Tab completes)", items, true, func(m *Model, dir string) tea.Cmd {
		return m.launch(agent, expandHome(dir))
	})
}

func (m *Model) launch(agent, dir string) tea.Cmd {
	m.busy = "starting " + agent + "…"
	a := m.app
	return func() tea.Msg {
		id, err := a.Launch(agent, dir, "", "")
		return doneMsg{text: "started " + agent + " in " + shortHome(dir), err: err, attach: id}
	}
}

// --- modal helpers ---------------------------------------------------------------------

func (m *Model) openPicker(title string, items []pickItem, freeText bool, onPick func(*Model, string) tea.Cmd) {
	f := textinput.New()
	f.Prompt = "› "
	f.Placeholder = "type to filter"
	if freeText {
		f.Placeholder = "type a path or filter"
	}
	f.Focus()
	m.pick = &picker{title: title, items: items, filter: f, freeText: freeText, onPick: onPick}
	m.pick.refilter()
	m.mode = modePick
}

func (m *Model) keyPick(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.pick
	switch k.String() {
	case "esc":
		m.mode = modeNormal
		return m, nil
	case "up", "ctrl+p", "ctrl+k":
		if p.cur > 0 {
			p.cur--
		}
		return m, nil
	case "down", "ctrl+n", "ctrl+j":
		if p.cur < len(p.filtered)-1 {
			p.cur++
		}
		return m, nil
	case "tab":
		if p.freeText {
			v := p.filter.Value()
			if v == "" && p.cur < len(p.filtered) {
				v = shortHome(p.filtered[p.cur].value) + "/"
			}
			p.filter.SetValue(completePath(v))
			p.filter.CursorEnd()
			p.refilter()
		}
		return m, nil
	case "enter":
		m.mode = modeNormal
		typed := strings.TrimSpace(p.filter.Value())
		if p.freeText && typed != "" && looksLikePath(typed) {
			if st, err := os.Stat(expandHome(typed)); err == nil && st.IsDir() {
				return m, p.onPick(m, expandHome(typed))
			}
		}
		if p.cur < len(p.filtered) {
			return m, p.onPick(m, p.filtered[p.cur].value)
		}
		if p.freeText && typed != "" {
			m.flash("not a directory: "+typed, true)
		}
		return m, nil
	}
	var cmd tea.Cmd
	p.filter, cmd = p.filter.Update(k)
	p.refilter()
	return m, cmd
}

func (m *Model) askInput(title, initial string, fn func(*Model, string) tea.Cmd) {
	m.input.SetValue(initial)
	m.input.CursorEnd()
	m.input.Focus()
	m.inputTitle, m.onInput = title, fn
	m.mode = modeInput
}

func (m *Model) confirm(text string, fn func(*Model) tea.Cmd) {
	m.confirmText, m.onConfirm = text, fn
	m.mode = modeConfirm
}

// --- path helpers -----------------------------------------------------------------------

var home, _ = os.UserHomeDir()

func shortHome(p string) string {
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

func expandHome(p string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}

func looksLikePath(s string) bool {
	return strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~") || strings.HasPrefix(s, ".")
}

// completePath extends a partial directory path as far as it is unambiguous.
func completePath(v string) string {
	if v == "" {
		return v
	}
	full := expandHome(v)
	dir, base := filepath.Split(full)
	if dir == "" {
		dir = "."
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return v
	}
	var matches []string
	for _, e := range ents {
		if e.IsDir() && strings.HasPrefix(e.Name(), base) && (!strings.HasPrefix(e.Name(), ".") || strings.HasPrefix(base, ".")) {
			matches = append(matches, e.Name())
		}
	}
	if len(matches) == 0 {
		return v
	}
	common := matches[0]
	for _, mt := range matches[1:] {
		for !strings.HasPrefix(mt, common) {
			common = common[:len(common)-1]
		}
	}
	out := filepath.Join(dir, common)
	if len(matches) == 1 {
		out += "/"
	}
	if strings.HasPrefix(v, "~") {
		return shortHome(out)
	}
	return out
}

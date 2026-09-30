package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/wardn/optimus/internal/format"
	"github.com/wardn/optimus/internal/model"
	"github.com/wardn/optimus/internal/mux"
	"github.com/wardn/optimus/internal/usage"
)

func ago(t time.Time) string { return format.Ago(t, time.Now()) }

var projCache struct {
	loaded time.Time
	ps     []usage.Project
}

func (m *Model) projects() []usage.Project {
	if !projCache.loaded.Equal(m.idx.Loaded) {
		projCache.ps = usage.Projects(m.idx.Sessions, m.app.Prices, time.Now())
		projCache.loaded = m.idx.Loaded
	}
	return projCache.ps
}

// listHeight is the number of rows available to a tab's body.
func (m *Model) listHeight() int { return max(m.h-4, 1) }

func (m *Model) View() string {
	if m.w == 0 {
		return ""
	}
	if m.mode == modeDetail {
		return m.viewDetail()
	}
	body := ""
	switch m.tab {
	case tabAgents:
		body = m.viewAgents()
	case tabSessions:
		body = m.viewSessions()
	case tabProjects:
		body = m.viewProjects()
	case tabUsage:
		body = m.viewUsage()
	}
	body = fitLines(body, m.w, m.listHeight())
	screen := lipgloss.JoinVertical(lipgloss.Left, m.viewHeader(), "", body, m.viewFooter())

	switch m.mode {
	case modePick:
		return overlay(screen, m.viewPicker(), m.w, m.h)
	case modeInput:
		box := sTitle.Render(m.inputTitle) + "\n\n" + m.input.View() + "\n\n" + sDim.Render("enter send · esc cancel")
		return overlay(screen, sModal.Width(min(m.w-4, 80)).Render(box), m.w, m.h)
	case modeConfirm:
		return overlay(screen, sModal.Render(sWarn.Render(m.confirmText)), m.w, m.h)
	case modeHelp:
		return overlay(screen, m.viewHelp(), m.w, m.h)
	}
	return screen
}

// overlay centers a box over the screen (the screen is replaced; terminals
// don't composite, and a dimmed backdrop keeps context).
func overlay(screen, box string, w, h int) string {
	_ = screen
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box, lipgloss.WithWhitespaceChars(" "))
}

func (m *Model) viewHeader() string {
	var tabs []string
	for i, n := range tabNames {
		label := fmt.Sprintf("%d %s", i+1, n)
		if i == int(tabAgents) && len(m.windows) > 0 {
			label += fmt.Sprintf(" (%d)", len(m.windows))
		}
		if tab(i) == m.tab {
			tabs = append(tabs, sTabOn.Render(label))
		} else {
			tabs = append(tabs, sTab.Render(label))
		}
	}
	left := sLogo.Render("OPTIMUS") + " " + strings.Join(tabs, "")

	now := time.Now()
	var right []string
	today := usage.Total(m.idx.Sessions, m.app.Prices, usage.Filter{Since: usage.StartOfDay(now)})
	right = append(right, sDim.Render("today ")+sMoney.Render(format.Money(today.Cost)))
	bl := usage.Blocks(m.idx.Sessions, m.app.Prices, "claude", m.app.Cfg.BlockHours, now)
	if n := len(bl); n > 0 && bl[n-1].Active {
		right = append(right, sDim.Render("block ")+sMoney.Render(format.Money(bl[n-1].Cost))+sDim.Render(" ↻"+format.Dur(bl[n-1].End.Sub(now))))
	}
	for _, w := range m.limits {
		if w.Stale(now) || w.LimitUSD > 0 {
			continue
		}
		st := lipgloss.NewStyle().Foreground(pctColor(w.UsedPct / 100))
		right = append(right, sDim.Render(w.Agent+" "+w.Name+" ")+st.Render(fmt.Sprintf("%.0f%%", w.UsedPct)))
	}
	if m.loading {
		right = append(right, sDim.Render("indexing…"))
	}
	r := strings.Join(right, sDim.Render(" · "))
	gap := m.w - lipgloss.Width(left) - lipgloss.Width(r) - 1
	if gap < 1 {
		return cell(left, m.w)
	}
	return left + strings.Repeat(" ", gap) + r
}

func (m *Model) viewFooter() string {
	if m.busy != "" {
		return sWarn.Render("⟳ " + m.busy)
	}
	if m.status != "" && time.Since(m.statusAt) < 6*time.Second {
		if m.isErr {
			return cell(sErr.Render("✗ "+m.status), m.w)
		}
		return cell(sMoney.Render("✓ ")+sText.Render(m.status), m.w)
	}
	var keys [][2]string
	switch m.tab {
	case tabAgents:
		keys = [][2]string{{"enter", "attach"}, {"n", "new"}, {"s", "send"}, {"b", "broadcast"}, {"space", "mark"}, {"h", "handoff"}, {"o", "transcript"}, {"r", "rename"}, {"x", "kill"}}
	case tabSessions:
		keys = [][2]string{{"enter", "view"}, {"r", "resume"}, {"h", "handoff"}, {"H", "summarized handoff"}, {"y", "copy ctx"}, {"/", "filter"}, {"a", "agent"}, {"p", "project"}}
	case tabProjects:
		keys = [][2]string{{"enter", "sessions"}, {"c", "new agent here"}}
	case tabUsage:
		keys = [][2]string{{"R", "reload"}}
	}
	keys = append(keys, [2]string{"tab", "switch"}, [2]string{"?", "help"}, [2]string{"q", "quit"})
	var parts []string
	for _, k := range keys {
		parts = append(parts, sKey.Render(k[0])+" "+sDim.Render(k[1]))
	}
	return cell(strings.Join(parts, "  "), m.w)
}

// --- agents -------------------------------------------------------------------------

func stateBadge(s mux.State) string {
	switch s {
	case mux.StateBusy:
		return lipgloss.NewStyle().Foreground(cYellow).Render("● busy ")
	case mux.StateWaiting:
		return lipgloss.NewStyle().Foreground(cRed).Bold(true).Render("◆ input")
	case mux.StateExited:
		return sDim.Render("✗ exit ")
	}
	return lipgloss.NewStyle().Foreground(cGreen).Render("○ idle ")
}

func (m *Model) viewAgents() string {
	h := m.listHeight()
	if len(m.windows) == 0 {
		return m.viewAgentsEmpty()
	}
	leftW := min(max(m.w*2/5, 40), 64)
	rightW := m.w - leftW - 2 // separator + padding

	var rows []string
	for i, w := range m.windows {
		mark := "  "
		if m.marked[w.ID] {
			mark = sKey.Render("▸ ")
		}
		line1 := mark + sDim.Render(fmt.Sprintf("%d ", w.Index)) + sBold.Render(cell(w.Name, leftW-18)) + " " + stateBadge(m.states[w.ID])
		line2 := "    " + agentStyle(w.Agent).Render(w.Agent) + sDim.Render(" · "+shortHome(w.Cwd)+" · "+ago(w.Activity))
		if i == m.wCur {
			line1 = sSel.Render(cell(line1, leftW))
			line2 = sSel.Render(cell(line2, leftW))
		}
		rows = append(rows, cell(line1, leftW), cell(line2, leftW))
	}

	// agents running outside optimus
	managed := map[string]bool{}
	for _, w := range m.windows {
		if w.SessionID != "" {
			managed[w.SessionID] = true
		}
	}
	var ext []string
	for _, l := range m.idx.Live {
		if managed[l.ID] {
			continue
		}
		ext = append(ext, cell("  "+agentStyle(l.Agent).Render(l.Agent)+" "+sDim.Render(l.Status+" · "+shortHome(l.Cwd)), leftW))
	}
	if len(ext) > 0 {
		rows = append(rows, "", sHead.Render("  OUTSIDE OPTIMUS"))
		rows = append(rows, ext...)
	}
	left := fitLines(strings.Join(rows, "\n"), leftW, h)

	// live preview of the selected pane, bottom-aligned like a terminal
	title := ""
	if w := m.selectedWindow(); w != nil {
		title = sTitle.Render(w.Name) + sDim.Render(fmt.Sprintf("  %s · pid %d · %s", w.Command, w.PID, "enter to attach, Alt-q to come back"))
	}
	lines := strings.Split(strings.TrimRight(m.preview, "\n "), "\n")
	ph := h - 2
	if len(lines) > ph {
		lines = lines[len(lines)-ph:]
	}
	for i, l := range lines {
		lines[i] = cell(strings.ReplaceAll(l, "\t", "    "), rightW-2)
	}
	right := cell(title, rightW) + "\n" + sDim.Render(strings.Repeat("─", rightW)) + "\n" + fitLines(strings.Join(lines, "\n"), rightW, ph)
	sep := sDim.Render(strings.Repeat("│\n", h-1) + "│")
	return lipgloss.JoinHorizontal(lipgloss.Top, left, sep, " "+right)
}

func (m *Model) viewAgentsEmpty() string {
	var b strings.Builder
	b.WriteString("\n  " + sBold.Render("No agents running in the optimus multiplexer.") + "\n\n")
	b.WriteString("  Press " + sKey.Render("n") + " to start one (claude, codex, opencode, …). It runs in a private tmux\n")
	b.WriteString("  server, so it keeps going when you quit optimus. Attach with " + sKey.Render("enter") + ", come back with " + sKey.Render("Alt-q") + ".\n\n")
	if len(m.idx.Live) > 0 {
		b.WriteString("  " + sHead.Render("RUNNING OUTSIDE OPTIMUS") + "\n")
		for _, l := range m.idx.Live {
			fmt.Fprintf(&b, "  %s %s %s\n", agentStyle(l.Agent).Render(cell(l.Agent, 8)), cell(l.Status, 6), sDim.Render(shortHome(l.Cwd)))
		}
		b.WriteString("\n  " + sDim.Render("Those can't be attached from here, but you can hand their context to a new agent from the Sessions tab.") + "\n")
	}
	return b.String()
}

// --- sessions ---------------------------------------------------------------------------

func (m *Model) viewSessions() string {
	h := m.listHeight()
	var head []string
	if m.mode == modeFilter || m.filter.Value() != "" {
		head = append(head, m.filter.View())
	}
	var chips []string
	if m.agentFilter != "" {
		chips = append(chips, "agent:"+m.agentFilter)
	}
	if m.cwdFilter != "" {
		chips = append(chips, "project:"+shortHome(m.cwdFilter))
	}
	total := 0.0
	for _, s := range m.sessions {
		total += s.Cost
	}
	info := sDim.Render(fmt.Sprintf("%d sessions · %s", len(m.sessions), format.Money(total)))
	if len(chips) > 0 {
		info += "  " + sKey.Render(strings.Join(chips, "  ")) + sDim.Render("  (esc clears)")
	}
	head = append(head, info)

	cols := []struct {
		name  string
		w     int
		right bool
	}{{"", 2, false}, {"AGENT", 9, false}, {"WHEN", 6, true}, {"PROJECT", 18, false}, {"TURNS", 6, true}, {"TOKENS", 8, true}, {"COST", 9, true}, {"TITLE", 0, false}}
	fixed := 0
	for _, c := range cols {
		fixed += c.w + 1
	}
	cols[len(cols)-1].w = max(m.w-fixed, 10)
	var hdr []string
	for _, c := range cols {
		if c.right {
			hdr = append(hdr, rcell(c.name, c.w))
		} else {
			hdr = append(hdr, cell(c.name, c.w))
		}
	}
	head = append(head, sHead.Render(strings.Join(hdr, " ")))

	rowsH := h - len(head)
	if m.sCur < m.sOff {
		m.sOff = m.sCur
	}
	if m.sCur >= m.sOff+rowsH {
		m.sOff = m.sCur - rowsH + 1
	}
	var rows []string
	for i := m.sOff; i < len(m.sessions) && i < m.sOff+rowsH; i++ {
		s := m.sessions[i]
		live := "  "
		if s.Live {
			live = lipgloss.NewStyle().Foreground(cGreen).Render("● ")
		}
		vals := []string{
			live,
			agentStyle(s.Agent).Render(cell(s.Agent, 9)),
			sDim.Render(rcell(ago(s.End), 6)),
			cell(s.ProjectName(), 18),
			rcell(fmt.Sprint(s.Messages), 6),
			sDim.Render(rcell(format.Tokens(s.Usage.Total()), 8)),
			sMoney.Render(rcell(format.Money(s.Cost), 9)),
			cell(s.DisplayTitle(), cols[7].w),
		}
		line := strings.Join(vals, " ")
		if i == m.sCur {
			line = sSel.Render(cell(line, m.w))
		}
		rows = append(rows, line)
	}
	if len(m.sessions) == 0 {
		if m.loading {
			rows = append(rows, sDim.Render("  indexing sessions…"))
		} else {
			rows = append(rows, sDim.Render("  no sessions match"))
		}
	}
	return strings.Join(append(head, rows...), "\n")
}

// --- projects -------------------------------------------------------------------------

func (m *Model) viewProjects() string {
	ps := m.projects()
	h := m.listHeight() - 1
	pathW := max(m.w-2-22-7-9-22-10-10-7, 10)
	hdr := "  " + cell("PROJECT", 22) + " " + cell("PATH", pathW) + " " + rcell("LAST", 6) + " " + rcell("SESSIONS", 8) + "  " + cell("AGENTS", 22) + rcell("7D", 10) + rcell("TOTAL", 10)
	rows := []string{sHead.Render(hdr)}
	if m.pCur < m.pOff {
		m.pOff = m.pCur
	}
	if m.pCur >= m.pOff+h {
		m.pOff = m.pCur - h + 1
	}
	for i := m.pOff; i < len(ps) && i < m.pOff+h; i++ {
		p := ps[i]
		var ag []string
		for k, v := range p.Agents {
			ag = append(ag, agentStyle(k).Render(fmt.Sprintf("%s×%d", k, v)))
		}
		sort.Strings(ag)
		live := "  "
		if p.Live > 0 {
			live = lipgloss.NewStyle().Foreground(cGreen).Render("● ")
		}
		line := live + sBold.Render(cell(p.Name, 22)) + " " + sDim.Render(cell(shortHome(p.Cwd), pathW)) + " " + rcell(ago(p.Last), 6) + " " + rcell(fmt.Sprint(p.Sessions), 8) + "  " + cell(strings.Join(ag, " "), 22) + sMoney.Render(rcell(format.Money(p.Cost7d), 10)+rcell(format.Money(p.Cost), 10))
		if i == m.pCur {
			line = sSel.Render(cell(line, m.w))
		}
		rows = append(rows, line)
	}
	return strings.Join(rows, "\n")
}

// --- usage ------------------------------------------------------------------------------

func (m *Model) viewUsage() string {
	now := time.Now()
	ss, pr := m.idx.Sessions, m.app.Prices

	card := func(title string, since time.Time) string {
		t := usage.Total(ss, pr, usage.Filter{Since: since})
		return sBox.Width(22).Render(sDim.Render(title) + "\n" + sMoney.Bold(true).Render(format.Money(t.Cost)) + "\n" + sDim.Render(format.Tokens(t.Usage.Total())+" tok · "+fmt.Sprint(t.Sessions)+" sess"))
	}
	cards := lipgloss.JoinHorizontal(lipgloss.Top,
		card("TODAY", usage.StartOfDay(now)), " ",
		card("THIS WEEK", usage.StartOfWeek(now)), " ",
		card("THIS MONTH", usage.StartOfMonth(now)), " ",
		card("ALL TIME", time.Time{}))

	colW := max(min((m.w-4)/2, 70), 40)

	// --- left column: remaining quota, block, budgets
	var L []string
	L = append(L, sTitle.Render("REMAINING"))
	if len(m.limits) == 0 {
		L = append(L, sDim.Render("No quota data yet. Set optimus as Claude Code's"), sDim.Render("status line to capture 5h / 7d limits:"), sKey.Render("  optimus config statusline"))
	}
	for _, w := range m.limits {
		pct := w.UsedPct
		reset := ""
		if !w.ResetsAt.IsZero() {
			reset = "↻ " + format.Dur(w.ResetsAt.Sub(now))
		}
		if w.Stale(now) {
			pct, reset = 0, "reset"
		}
		label := cell(w.Agent+" "+w.Name, 16)
		L = append(L, label+bar(pct/100, colW-36, pctColor(pct/100))+fmt.Sprintf(" %3.0f%% left %3.0f%%  ", pct, 100-pct)+sDim.Render(reset))
	}
	L = append(L, "")

	bl := usage.Blocks(ss, pr, "claude", m.app.Cfg.BlockHours, now)
	L = append(L, sTitle.Render(fmt.Sprintf("CURRENT %dH BLOCK", m.app.Cfg.BlockHours)))
	if n := len(bl); n > 0 && bl[n-1].Active {
		b := bl[n-1]
		el := now.Sub(b.Start)
		frac := el.Seconds() / b.End.Sub(b.Start).Seconds()
		rate := b.Cost / max(el.Hours(), 0.25)
		L = append(L,
			cell("elapsed", 16)+bar(frac, colW-36, cBlue)+sDim.Render(fmt.Sprintf(" %s / %dh", format.Dur(el), m.app.Cfg.BlockHours)),
			cell("spent", 16)+sMoney.Render(format.Money(b.Cost))+sDim.Render("  "+format.Tokens(b.Usage.Total())+" tokens"),
			cell("burn rate", 16)+sText.Render(format.Money(rate)+"/h")+sDim.Render("  → "+format.Money(b.Cost+rate*b.End.Sub(now).Hours())+" projected"),
			cell("resets", 16)+sText.Render(b.End.Local().Format("15:04"))+sDim.Render(" (in "+format.Dur(b.End.Sub(now))+")"))
	} else {
		L = append(L, sDim.Render("no active block — the next message starts one"))
	}
	L = append(L, "")

	L = append(L, sTitle.Render("BUDGETS"))
	bs := usage.Budgets(ss, pr, m.app.Cfg, now)
	if len(bs) == 0 {
		L = append(L, sDim.Render("none set · add \"budgets\": {\"daily_usd\": 50}"), sDim.Render("to ~/.config/optimus/config.json"))
	}
	for _, b := range bs {
		L = append(L, cell(b.Name, 16)+bar(b.Frac(), colW-36, pctColor(b.Frac()))+fmt.Sprintf(" %s / %s", format.Money(b.Spent), format.Money(b.Limit)))
	}

	// --- right column: 14-day chart + breakdowns
	var R []string
	days := usage.Daily(ss, pr, usage.Filter{}, 14, now)
	maxc := 0.0
	for _, d := range days {
		maxc = max(maxc, d.Cost)
	}
	R = append(R, sTitle.Render("LAST 14 DAYS"))
	for _, d := range days {
		t, _ := time.ParseInLocation("2006-01-02", d.Key, time.Local)
		frac := 0.0
		if maxc > 0 {
			frac = d.Cost / maxc
		}
		R = append(R, sDim.Render(t.Format("Mon 02 "))+bar(frac, colW-24, cMauve)+" "+sMoney.Render(rcell(format.Money(d.Cost), 9)))
	}
	R = append(R, "")
	since30 := usage.StartOfDay(now).AddDate(0, 0, -29)
	R = append(R, sTitle.Render("30 DAYS BY MODEL"))
	for i, r := range usage.GroupBy(ss, pr, usage.Filter{Since: since30}, "model") {
		if i >= 6 {
			break
		}
		name := r.Key
		if !pr.Known(name) {
			name += sDim.Render(" (unpriced)")
		}
		R = append(R, cell(name, colW-24)+sDim.Render(rcell(format.Tokens(r.Usage.Total()), 9))+" "+sMoney.Render(rcell(format.Money(r.Cost), 9)))
	}
	R = append(R, "", sTitle.Render("30 DAYS BY AGENT"))
	for _, r := range usage.GroupBy(ss, pr, usage.Filter{Since: since30}, "agent") {
		R = append(R, agentStyle(r.Key).Render(cell(r.Key, colW-24))+sDim.Render(rcell(format.Tokens(r.Usage.Total()), 9))+" "+sMoney.Render(rcell(format.Money(r.Cost), 9)))
	}

	cols := lipgloss.JoinHorizontal(lipgloss.Top, fitLines(strings.Join(L, "\n"), colW, len(L)), "   ", strings.Join(R, "\n"))
	if m.w < 2*40+3 {
		cols = strings.Join(L, "\n") + "\n\n" + strings.Join(R, "\n")
	}
	note := sDim.Render("Costs are API list-price equivalents — on a Claude/ChatGPT plan they show value consumed, not money billed.")
	return cards + "\n" + cols + "\n\n" + note
}

// --- modals -------------------------------------------------------------------------------

func (m *Model) viewPicker() string {
	p := m.pick
	w := min(m.w-6, 90)
	var b strings.Builder
	b.WriteString(sTitle.Render(p.title) + "\n\n")
	b.WriteString(p.filter.View() + "\n\n")
	maxRows := max(min(m.h-12, 16), 3)
	start := 0
	if p.cur >= maxRows {
		start = p.cur - maxRows + 1
	}
	for i := start; i < len(p.filtered) && i < start+maxRows; i++ {
		it := p.filtered[i]
		line := cell(it.label, w/2) + " " + sDim.Render(it.detail)
		if i == p.cur {
			line = sSel.Render(cell(sKey.Render("› ")+line, w-4))
		} else {
			line = "  " + line
		}
		b.WriteString(cell(line, w-4) + "\n")
	}
	if len(p.filtered) == 0 {
		b.WriteString(sDim.Render("  no match") + "\n")
	}
	b.WriteString("\n" + sDim.Render("↑↓ move · enter select · esc cancel"))
	if p.freeText {
		b.WriteString(sDim.Render(" · tab complete path"))
	}
	return sModal.Width(w).Render(b.String())
}

func (m *Model) viewHelp() string {
	sections := []struct {
		title string
		keys  [][2]string
	}{
		{"Everywhere", [][2]string{{"1-4 / tab", "switch view"}, {"j k ↑ ↓", "move"}, {"n", "start a new agent"}, {"R", "rescan sessions"}, {"q", "quit (agents keep running)"}}},
		{"Agents", [][2]string{{"enter", "attach — Alt-q comes back, Alt-←/→ cycles agents"}, {"s", "send a prompt"}, {"space / b", "mark agents / broadcast a prompt"}, {"h / H", "hand this agent's context to another"}, {"o", "open transcript"}, {"r / x", "rename / kill"}}},
		{"Sessions", [][2]string{{"enter", "read transcript"}, {"r", "resume in the multiplexer"}, {"h", "hand off context to a new agent, a running one, clipboard or file"}, {"H", "same, condensed by an agent first"}, {"y", "copy handoff to clipboard"}, {"/ a p esc", "filter text / agent / project / clear"}}},
		{"Projects", [][2]string{{"enter", "sessions of this project"}, {"c", "start an agent here"}}},
	}
	var b strings.Builder
	b.WriteString(sLogo.Render("OPTIMUS") + sDim.Render("  the tmux of AI agents") + "\n")
	for _, s := range sections {
		b.WriteString("\n" + sTitle.Render(s.title) + "\n")
		for _, k := range s.keys {
			b.WriteString("  " + sKey.Render(cell(k[0], 12)) + " " + sText.Render(k[1]) + "\n")
		}
	}
	b.WriteString("\n" + sDim.Render("CLI: optimus help · press any key"))
	return sModal.Render(b.String())
}

// --- transcript ----------------------------------------------------------------------------

func (m *Model) viewDetail() string {
	s := m.detailSess
	head := sLogo.Render("OPTIMUS") + " " + agentStyle(s.Agent).Render(s.Agent) + " " + sBold.Render(model.Truncate(s.DisplayTitle(), m.w-40)) +
		sDim.Render(fmt.Sprintf("  %s · %s · %s", shortHome(s.Cwd), format.Money(s.Cost), s.ShortID()))
	foot := sKey.Render("r") + sDim.Render(" resume  ") + sKey.Render("h") + sDim.Render(" handoff  ") + sKey.Render("H") + sDim.Render(" summarized  ") + sKey.Render("y") + sDim.Render(" copy ctx  ") + sKey.Render("esc") + sDim.Render(" back  ") + sDim.Render(fmt.Sprintf("%3.0f%%", m.detail.ScrollPercent()*100))
	return cell(head, m.w) + "\n" + sDim.Render(strings.Repeat("─", m.w)) + "\n" + lipgloss.NewStyle().PaddingLeft(2).Render(m.detail.View()) + "\n" + cell(foot, m.w)
}

func renderTranscript(s *model.Session, msgs []model.Message, width int) string {
	wrap := lipgloss.NewStyle().Width(max(width, 20))
	var b strings.Builder
	for _, msg := range msgs {
		ts := ""
		if !msg.Time.IsZero() {
			ts = sDim.Render(" " + msg.Time.Local().Format("Jan 02 15:04"))
		}
		switch msg.Role {
		case "user":
			b.WriteString(sUser.Render("▌ you") + ts + "\n")
			b.WriteString(wrap.Render(sText.Render(msg.Text)) + "\n\n")
		default:
			b.WriteString(sAgentMsg.Render("▌ "+s.Agent) + ts + "\n")
			if strings.TrimSpace(msg.Text) != "" {
				b.WriteString(wrap.Render(msg.Text) + "\n")
			}
			for _, t := range msg.Tools {
				icon := "⚙"
				if t.Edit {
					icon = "✎"
				}
				b.WriteString(sDim.Render(model.Truncate(fmt.Sprintf("  %s %s %s", icon, t.Name, t.Target), width)) + "\n")
			}
			b.WriteString("\n")
		}
	}
	if len(msgs) == 0 {
		b.WriteString(sDim.Render("(empty transcript)"))
	}
	return b.String()
}

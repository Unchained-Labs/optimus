// Package cli implements optimus's non-interactive subcommands.
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/wardn/optimus/internal/app"
	"github.com/wardn/optimus/internal/clip"
	"github.com/wardn/optimus/internal/config"
	"github.com/wardn/optimus/internal/format"
	"github.com/wardn/optimus/internal/index"
	"github.com/wardn/optimus/internal/model"
	"github.com/wardn/optimus/internal/mux"
	"github.com/wardn/optimus/internal/providers"
	"github.com/wardn/optimus/internal/ratelimits"
	"github.com/wardn/optimus/internal/usage"
)

var Version = "0.1.0"

const Usage = `optimus — the tmux of AI coding agents

Usage:
  optimus                         open the dashboard (TUI)

 Multiplexer
  optimus new <agent> [dir]       start an agent in the optimus tmux server
        --name N --prompt P --attach
  optimus ps                      list running agents (optimus windows + other live sessions)
  optimus attach [window]         attach to a window (Alt-q to come back)
  optimus peek <window> [-n 40]   print the bottom of a window's screen
  optimus send <window|all> TEXT  type a prompt into one or all agents  (--no-enter)
  optimus kill <window>           stop an agent window
  optimus resume <session>        reopen a past session in the multiplexer (--attach)

 Sessions & context
  optimus ls                      list sessions  (-a agent -p project -n 30 --live --json)
  optimus show <session>          print a transcript  (--tail N)
  optimus handoff <session>       build a context document from a session and
        --to AGENT                  …start AGENT primed with it
        --into WINDOW               …send it into a running window
        --copy | --print            …copy to clipboard / print it (default: save + print path)
        --note TEXT --summarize --dir DIR

 Costs & limits
  optimus usage                   spend report  (--by day|month|model|agent|project|session --since 7d --json)
  optimus blocks                  5-hour usage blocks  (-n 10)
  optimus limits                  quota windows (Claude/Codex) and your budgets
  optimus projects                per-project sessions and spend
  optimus statusline              Claude Code status line command (records rate limits)

 Setup
  optimus agents                  detected agents and where their data lives
  optimus config [init|path|edit] manage ~/.config/optimus/config.json
  optimus reindex                 drop the parse cache and rescan
  optimus version
`

func Run(args []string) int {
	a := app.New()
	if a.CfgErr != nil {
		fmt.Fprintf(os.Stderr, "optimus: config %s: %v (using defaults)\n", config.Path(), a.CfgErr)
	}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "ls", "sessions", "list":
		err = cmdLs(a, rest)
	case "show", "cat":
		err = cmdShow(a, rest)
	case "usage", "cost", "costs":
		err = cmdUsage(a, rest)
	case "blocks":
		err = cmdBlocks(a, rest)
	case "limits", "remaining", "quota":
		err = cmdLimits(a, rest)
	case "projects":
		err = cmdProjects(a, rest)
	case "ps":
		err = cmdPs(a, rest)
	case "new", "spawn", "run":
		err = cmdNew(a, rest)
	case "resume":
		err = cmdResume(a, rest)
	case "attach", "a":
		err = cmdAttach(rest)
	case "send":
		err = cmdSend(rest)
	case "peek":
		err = cmdPeek(rest)
	case "kill":
		err = cmdKill(rest)
	case "handoff", "transfer", "cp":
		err = cmdHandoff(a, rest)
	case "statusline":
		err = cmdStatusline(a)
	case "agents", "doctor":
		err = cmdAgents(a)
	case "config":
		err = cmdConfig(a, rest)
	case "reindex":
		_ = index.Clear()
		idx := a.Index()
		fmt.Printf("indexed %d sessions\n", len(idx.Sessions))
	case "version", "--version", "-v":
		fmt.Println("optimus", Version)
	case "help", "--help", "-h":
		fmt.Print(Usage)
	default:
		fmt.Fprintf(os.Stderr, "optimus: unknown command %q\n\n%s", cmd, Usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "optimus:", err)
		return 1
	}
	return 0
}

// parse lets flags appear before or after positional args.
func parse(fs *flag.FlagSet, args []string) []string {
	fs.SetOutput(io.Discard)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			fmt.Fprintln(os.Stderr, "optimus:", err)
			os.Exit(2)
		}
		if fs.NArg() == 0 {
			return pos
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func table() *tabwriter.Writer { return tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0) }

func printJSON(v any) error {
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", "  ")
	return e.Encode(v)
}

var home, _ = os.UserHomeDir()

func sp(p string) string { return format.ShortPath(p, home) }

// parseSince accepts 7d, 12h, 2w, today, week, month, or a YYYY-MM-DD date.
func parseSince(s string, now time.Time) (time.Time, error) {
	switch s {
	case "", "all":
		return time.Time{}, nil
	case "today":
		return usage.StartOfDay(now), nil
	case "week":
		return usage.StartOfWeek(now), nil
	case "month":
		return usage.StartOfMonth(now), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t, nil
	}
	if len(s) > 1 {
		n, err := strconv.Atoi(s[:len(s)-1])
		if err == nil {
			switch s[len(s)-1] {
			case 'h':
				return now.Add(-time.Duration(n) * time.Hour), nil
			case 'd':
				return usage.StartOfDay(now).AddDate(0, 0, -(n - 1)), nil
			case 'w':
				return usage.StartOfDay(now).AddDate(0, 0, -7*n+1), nil
			case 'm':
				return usage.StartOfDay(now).AddDate(0, -n, 1), nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("bad --since %q (try 7d, 12h, 2w, today, week, month, 2026-09-01)", s)
}

func findSession(idx *index.Index, q string) (*model.Session, error) {
	if q == "" {
		return nil, fmt.Errorf("missing session id")
	}
	if q == "last" || q == "latest" {
		if len(idx.Sessions) == 0 {
			return nil, fmt.Errorf("no sessions")
		}
		return idx.Sessions[0], nil
	}
	s := idx.Find(q)
	if s == nil {
		return nil, fmt.Errorf("no unique session matches %q (use more characters of the id)", q)
	}
	return s, nil
}

// --- sessions ---------------------------------------------------------------

type sessionJSON struct {
	*model.Session
	Cost   float64     `json:"cost_usd"`
	Tokens int64       `json:"tokens"`
	Usage  model.Usage `json:"usage"`
	Live   bool        `json:"live"`
}

func cmdLs(a *app.App, args []string) error {
	fs := flag.NewFlagSet("ls", flag.ExitOnError)
	agent := fs.String("a", "", "agent")
	proj := fs.String("p", "", "project substring")
	n := fs.Int("n", 30, "limit (0 = all)")
	live := fs.Bool("live", false, "only running sessions")
	asJSON := fs.Bool("json", false, "json output")
	pos := parse(fs, args)
	if len(pos) > 0 && *proj == "" {
		*proj = pos[0]
	}
	idx := a.Index()
	var out []*model.Session
	for _, s := range idx.Sessions {
		if (*agent != "" && s.Agent != *agent) || (*proj != "" && !strings.Contains(strings.ToLower(s.Cwd), strings.ToLower(*proj))) || (*live && !s.Live) {
			continue
		}
		out = append(out, s)
		if *n > 0 && len(out) >= *n {
			break
		}
	}
	if *asJSON {
		js := make([]sessionJSON, len(out))
		for i, s := range out {
			js[i] = sessionJSON{s, s.Cost, s.Usage.Total(), s.Usage, s.Live}
		}
		return printJSON(js)
	}
	now := time.Now()
	w := table()
	fmt.Fprintln(w, "ID\tAGENT\tWHEN\tPROJECT\tTURNS\tTOKENS\tCOST\tTITLE")
	for _, s := range out {
		live := ""
		if s.Live {
			live = "● "
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s%s\n", s.ShortID(), s.Agent, format.Ago(s.End, now), s.ProjectName(), s.Messages, format.Tokens(s.Usage.Total()), format.Money(s.Cost), live, model.Truncate(s.DisplayTitle(), 60))
	}
	return w.Flush()
}

func cmdShow(a *app.App, args []string) error {
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	tail := fs.Int("tail", 0, "only the last N messages")
	pos := parse(fs, args)
	idx := a.Index()
	s, err := findSession(idx, first(pos))
	if err != nil {
		return err
	}
	msgs, err := providers.Get(s.Agent).Transcript(s)
	if err != nil {
		return err
	}
	if *tail > 0 && len(msgs) > *tail {
		msgs = msgs[len(msgs)-*tail:]
	}
	fmt.Printf("%s · %s · %s · %s · %s\n\n", s.Agent, s.ID, sp(s.Cwd), format.Money(s.Cost), s.DisplayTitle())
	for _, m := range msgs {
		ts := ""
		if !m.Time.IsZero() {
			ts = m.Time.Local().Format("15:04")
		}
		switch m.Role {
		case "user":
			fmt.Printf("\x1b[1;36m▌ you %s\x1b[0m\n%s\n\n", ts, m.Text)
		default:
			fmt.Printf("\x1b[1;35m▌ %s %s\x1b[0m\n", s.Agent, ts)
			if m.Text != "" {
				fmt.Println(m.Text)
			}
			for _, t := range m.Tools {
				fmt.Printf("\x1b[2m  ⚙ %s %s\x1b[0m\n", t.Name, t.Target)
			}
			fmt.Println()
		}
	}
	return nil
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// --- usage ------------------------------------------------------------------

func cmdUsage(a *app.App, args []string) error {
	fs := flag.NewFlagSet("usage", flag.ExitOnError)
	by := fs.String("by", "day", "day|month|model|agent|project|session")
	since := fs.String("since", "30d", "time window")
	agent := fs.String("a", "", "agent")
	asJSON := fs.Bool("json", false, "json output")
	parse(fs, args)
	now := time.Now()
	st, err := parseSince(*since, now)
	if err != nil {
		return err
	}
	idx := a.Index()
	f := usage.Filter{Since: st, Agent: *agent}
	rows := usage.GroupBy(idx.Sessions, a.Prices, f, *by)
	total := usage.Total(idx.Sessions, a.Prices, f)
	if *asJSON {
		type r struct {
			Key      string      `json:"key"`
			Cost     float64     `json:"cost_usd"`
			Tokens   int64       `json:"tokens"`
			Usage    model.Usage `json:"usage"`
			Sessions int         `json:"sessions"`
		}
		var out []r
		for _, x := range rows {
			out = append(out, r{x.Key, x.Cost, x.Usage.Total(), x.Usage, x.Sessions})
		}
		return printJSON(out)
	}
	maxCost := 0.0
	for _, r := range rows {
		if r.Cost > maxCost {
			maxCost = r.Cost
		}
	}
	w := table()
	fmt.Fprintf(w, "%s\tCOST\t\tINPUT\tOUTPUT\tCACHE R\tCACHE W\tSESSIONS\n", strings.ToUpper(*by))
	for _, r := range rows {
		key := r.Key
		switch *by {
		case "project":
			key = sp(key)
		case "session":
			if s := idx.Find(strings.SplitN(key, "/", 2)[1]); s != nil {
				key = s.Agent + " " + s.ShortID() + " " + model.Truncate(s.DisplayTitle(), 40)
			}
		case "model":
			if !a.Prices.Known(key) {
				key += " (unpriced)"
			}
		}
		frac := 0.0
		if maxCost > 0 {
			frac = r.Cost / maxCost
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\n", key, format.Money(r.Cost), format.Bar(frac, 16), format.Tokens(r.Usage.Input), format.Tokens(r.Usage.Output), format.Tokens(r.Usage.CacheRead), format.Tokens(r.Usage.CacheWrite5m+r.Usage.CacheWrite1h), r.Sessions)
	}
	fmt.Fprintf(w, "TOTAL\t%s\t\t%s\t%s\t%s\t%s\t%d\n", format.Money(total.Cost), format.Tokens(total.Usage.Input), format.Tokens(total.Usage.Output), format.Tokens(total.Usage.CacheRead), format.Tokens(total.Usage.CacheWrite5m+total.Usage.CacheWrite1h), total.Sessions)
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Println("\ncosts are API list-price equivalents; on a subscription they show value used, not money billed.")
	return nil
}

func cmdBlocks(a *app.App, args []string) error {
	fs := flag.NewFlagSet("blocks", flag.ExitOnError)
	n := fs.Int("n", 10, "how many recent blocks")
	agent := fs.String("a", "claude", "agent (\"\" for all)")
	parse(fs, args)
	idx := a.Index()
	now := time.Now()
	bl := usage.Blocks(idx.Sessions, a.Prices, *agent, a.Cfg.BlockHours, now)
	if len(bl) > *n {
		bl = bl[len(bl)-*n:]
	}
	w := table()
	fmt.Fprintln(w, "START\tEND\tCOST\tTOKENS\tSTATUS")
	for _, b := range bl {
		status := ""
		if b.Active {
			status = fmt.Sprintf("● active, resets in %s", format.Dur(b.End.Sub(now)))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", b.Start.Local().Format("Mon 01-02 15:04"), b.End.Local().Format("15:04"), format.Money(b.Cost), format.Tokens(b.Usage.Total()), status)
	}
	return w.Flush()
}

func cmdLimits(a *app.App, args []string) error {
	now := time.Now()
	ws := ratelimits.Load()
	fmt.Println("Quota windows")
	if len(ws) == 0 {
		fmt.Println("  none recorded yet — run `optimus config statusline` to see how to let Claude Code report them")
	}
	w := table()
	for _, x := range ws {
		reset := ""
		if !x.ResetsAt.IsZero() {
			reset = "resets in " + format.Dur(x.ResetsAt.Sub(now))
		}
		pct := x.UsedPct
		note := fmt.Sprintf("seen %s ago", format.Ago(x.Captured, now))
		if x.Stale(now) {
			pct, reset, note = 0, "reset since", note+" (window has reset)"
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t%3.0f%% used\t%s\t%s\n", x.Agent, x.Name, format.Bar(pct/100, 20), pct, reset, note)
	}
	w.Flush()

	idx := a.Index()
	fmt.Println("\nCurrent block")
	bl := usage.Blocks(idx.Sessions, a.Prices, "claude", a.Cfg.BlockHours, now)
	if n := len(bl); n > 0 && bl[n-1].Active {
		b := bl[n-1]
		el := now.Sub(b.Start)
		rate := b.Cost / max(el.Hours(), 0.25)
		fmt.Printf("  claude  started %s  ·  %s spent  ·  %s tokens  ·  resets in %s  ·  burn %s/h\n", b.Start.Local().Format("15:04"), format.Money(b.Cost), format.Tokens(b.Usage.Total()), format.Dur(b.End.Sub(now)), format.Money(rate))
	} else {
		fmt.Println("  no active block")
	}

	fmt.Println("\nBudgets")
	bs := usage.Budgets(idx.Sessions, a.Prices, a.Cfg, now)
	if len(bs) == 0 {
		fmt.Printf("  none set — add \"budgets\": {\"daily_usd\": 50, \"weekly_usd\": 200} to %s\n", config.Path())
	}
	w = table()
	for _, b := range bs {
		fmt.Fprintf(w, "  %s\t%s\t%s / %s\t%s left\n", b.Name, format.Bar(b.Frac(), 20), format.Money(b.Spent), format.Money(b.Limit), format.Money(max(b.Limit-b.Spent, 0)))
	}
	return w.Flush()
}

func cmdProjects(a *app.App, args []string) error {
	fs := flag.NewFlagSet("projects", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "json output")
	parse(fs, args)
	idx := a.Index()
	now := time.Now()
	ps := usage.Projects(idx.Sessions, a.Prices, now)
	if *asJSON {
		return printJSON(ps)
	}
	w := table()
	fmt.Fprintln(w, "PROJECT\tPATH\tLAST\tSESSIONS\tAGENTS\t7D\tTOTAL")
	for _, p := range ps {
		var ag []string
		for k, v := range p.Agents {
			ag = append(ag, fmt.Sprintf("%s:%d", k, v))
		}
		sort.Strings(ag)
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\t%s\n", model.Truncate(p.Name, 28), model.Truncate(sp(p.Cwd), 60), format.Ago(p.Last, now), p.Sessions, strings.Join(ag, " "), format.Money(p.Cost7d), format.Money(p.Cost))
	}
	return w.Flush()
}

// --- multiplexer -------------------------------------------------------------

func cmdPs(a *app.App, args []string) error {
	ws, err := mux.List()
	if err != nil {
		return err
	}
	now := time.Now()
	w := table()
	fmt.Fprintln(w, "WIN\tNAME\tAGENT\tSTATE\tPROJECT\tACTIVE\tUP")
	for _, x := range ws {
		screen, _ := mux.Capture(x.ID, 30)
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s ago\t%s\n", x.Index, x.Name, x.Agent, mux.Detect(x, screen), sp(x.Cwd), format.Ago(x.Activity, now), format.Ago(x.Created, now))
	}
	w.Flush()
	if len(ws) == 0 {
		fmt.Println("(no agents in the optimus multiplexer — start one with `optimus new claude`)")
	}
	var others []providers.LiveSession
	for _, p := range providers.All() {
		if lr, ok := p.(providers.LiveReporter); ok {
			others = append(others, lr.Live()...)
		}
	}
	managed := map[int]bool{}
	for _, x := range ws {
		managed[x.PID] = true
	}
	if len(others) > 0 {
		fmt.Println("\nOther live sessions (outside optimus)")
		w = table()
		for _, l := range others {
			fmt.Fprintf(w, "  %s\t%s\t%s\tpid %d\t%s\n", l.Agent, l.Status, sp(l.Cwd), l.PID, l.ID)
		}
		w.Flush()
	}
	return nil
}

func cmdNew(a *app.App, args []string) error {
	fs := flag.NewFlagSet("new", flag.ExitOnError)
	name := fs.String("name", "", "window name")
	prompt := fs.String("prompt", "", "initial prompt")
	attach := fs.Bool("attach", false, "attach after starting")
	pos := parse(fs, args)
	if len(pos) == 0 {
		return fmt.Errorf("usage: optimus new <agent> [dir]   (agents: %s)", strings.Join(providers.Names(), ", "))
	}
	dir := ""
	if len(pos) > 1 {
		dir = pos[1]
	}
	id, err := a.Launch(pos[0], absDir(dir), *prompt, *name)
	if err != nil {
		return err
	}
	fmt.Printf("started %s in window %s\n", pos[0], id)
	if *attach {
		return mux.AttachCmd(id).Run()
	}
	return nil
}

func absDir(d string) string {
	if d == "" {
		wd, _ := os.Getwd()
		return wd
	}
	if strings.HasPrefix(d, "~/") {
		d = home + d[1:]
	}
	if st, err := os.Stat(d); err == nil && st.IsDir() {
		if abs, err := absPath(d); err == nil {
			return abs
		}
	}
	return d
}

func cmdResume(a *app.App, args []string) error {
	fs := flag.NewFlagSet("resume", flag.ExitOnError)
	attach := fs.Bool("attach", false, "attach after starting")
	pos := parse(fs, args)
	s, err := findSession(a.Index(), first(pos))
	if err != nil {
		return err
	}
	id, err := a.Resume(s)
	if err != nil {
		return err
	}
	fmt.Printf("resumed %s %s in window %s\n", s.Agent, s.ShortID(), id)
	if *attach {
		return mux.AttachCmd(id).Run()
	}
	return nil
}

func cmdAttach(args []string) error {
	if !mux.Running() {
		return fmt.Errorf("no agents running — start one with `optimus new claude`")
	}
	target := ""
	if len(args) > 0 {
		w, err := mux.Resolve(args[0])
		if err != nil {
			return err
		}
		target = w.ID
	} else if ws, _ := mux.List(); len(ws) > 0 {
		target = ws[0].ID
	}
	return mux.AttachCmd(target).Run()
}

func cmdSend(args []string) error {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	noEnter := fs.Bool("no-enter", false, "paste without submitting")
	pos := parse(fs, args)
	if len(pos) < 2 {
		return fmt.Errorf("usage: optimus send <window|all> <text>   (use - to read text from stdin)")
	}
	text := strings.Join(pos[1:], " ")
	if text == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		text = string(b)
	}
	var targets []mux.Window
	if pos[0] == "all" {
		ws, err := mux.List()
		if err != nil {
			return err
		}
		for _, w := range ws {
			if !w.Dead {
				targets = append(targets, w)
			}
		}
	} else {
		w, err := mux.Resolve(pos[0])
		if err != nil {
			return err
		}
		targets = []mux.Window{w}
	}
	for _, w := range targets {
		if err := mux.Send(w.ID, text, !*noEnter); err != nil {
			return err
		}
		fmt.Printf("sent to %d:%s\n", w.Index, w.Name)
	}
	return nil
}

func cmdPeek(args []string) error {
	fs := flag.NewFlagSet("peek", flag.ExitOnError)
	n := fs.Int("n", 40, "lines")
	pos := parse(fs, args)
	w, err := mux.Resolve(first(pos))
	if err != nil {
		return err
	}
	out, err := mux.Capture(w.ID, *n)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(out, "\n "), "\n")
	if len(lines) > *n {
		lines = lines[len(lines)-*n:]
	}
	fmt.Println(strings.Join(lines, "\n"))
	return nil
}

func cmdKill(args []string) error {
	w, err := mux.Resolve(first(args))
	if err != nil {
		return err
	}
	return mux.Kill(w.ID)
}

// --- handoff ----------------------------------------------------------------

func cmdHandoff(a *app.App, args []string) error {
	fs := flag.NewFlagSet("handoff", flag.ExitOnError)
	to := fs.String("to", "", "start this agent with the context")
	into := fs.String("into", "", "send the context into this running window")
	copyIt := fs.Bool("copy", false, "copy the document to the clipboard")
	printIt := fs.Bool("print", false, "print the document")
	note := fs.String("note", "", "extra instructions to include")
	summarize := fs.Bool("summarize", false, "condense with an agent first (costs tokens)")
	dir := fs.String("dir", "", "working dir for --to (default: the session's)")
	pos := parse(fs, args)
	idx := a.Index()
	s, err := findSession(idx, first(pos))
	if err != nil {
		return err
	}
	switch {
	case *to != "":
		id, path, err := a.HandoffTo(s, *to, absDirOrEmpty(*dir), *note, *summarize)
		if err != nil {
			return err
		}
		fmt.Printf("context saved to %s\nstarted %s in window %s — attach with `optimus attach %s`\n", path, *to, id, id)
		return nil
	case *into != "":
		w, err := mux.Resolve(*into)
		if err != nil {
			return err
		}
		path, err := a.HandoffInto(s, w.ID, *note, *summarize)
		if err != nil {
			return err
		}
		fmt.Printf("context saved to %s and sent to %d:%s\n", path, w.Index, w.Name)
		return nil
	}
	doc, path, err := a.HandoffDoc(s, *note, *summarize)
	if err != nil {
		return err
	}
	if *printIt {
		fmt.Print(doc)
		return nil
	}
	if *copyIt {
		how, err := clip.Copy(doc)
		if err != nil {
			return err
		}
		fmt.Printf("copied %d chars (~%d tokens) via %s; also saved to %s\n", len(doc), len(doc)/4, how, path)
		return nil
	}
	fmt.Println(path)
	return nil
}

func absDirOrEmpty(d string) string {
	if d == "" {
		return ""
	}
	return absDir(d)
}

// --- statusline ---------------------------------------------------------------

// cmdStatusline is meant to be Claude Code's statusLine command. It records
// the rate-limit snapshot, then prints a compact line (or delegates to a
// chained status line command).
func cmdStatusline(a *app.App) error {
	payload, _ := io.ReadAll(os.Stdin)
	_ = ratelimits.RecordClaude(payload)
	if a.Cfg.StatuslineChain != "" {
		cmd := exec.Command("sh", "-c", a.Cfg.StatuslineChain)
		cmd.Stdin = strings.NewReader(string(payload))
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		return cmd.Run()
	}
	var v struct {
		Model struct {
			DisplayName string `json:"display_name"`
		} `json:"model"`
		Workspace struct {
			CurrentDir string `json:"current_dir"`
		} `json:"workspace"`
		Cost struct {
			TotalCostUSD float64 `json:"total_cost_usd"`
		} `json:"cost"`
	}
	_ = json.Unmarshal(payload, &v)
	parts := []string{"\x1b[1;31m◆\x1b[0m " + v.Model.DisplayName}
	if v.Workspace.CurrentDir != "" {
		parts = append(parts, model.ProjectName(v.Workspace.CurrentDir))
	}
	if v.Cost.TotalCostUSD > 0 {
		parts = append(parts, "session "+format.Money(v.Cost.TotalCostUSD))
	}
	now := time.Now()
	for _, w := range ratelimits.Load() {
		if w.Agent != "claude" || w.Stale(now) || w.LimitUSD > 0 {
			continue
		}
		col := "32"
		if w.UsedPct >= 80 {
			col = "31"
		} else if w.UsedPct >= 50 {
			col = "33"
		}
		s := fmt.Sprintf("%s \x1b[%sm%s %.0f%%\x1b[0m", w.Name, col, format.Bar(w.UsedPct/100, 5), w.UsedPct)
		if !w.ResetsAt.IsZero() && w.Name == "5h" {
			s += " ↻" + format.Dur(w.ResetsAt.Sub(now))
		}
		parts = append(parts, s)
	}
	fmt.Print(strings.Join(parts, " · "))
	return nil
}

// --- setup --------------------------------------------------------------------

func cmdAgents(a *app.App) error {
	idx := a.Index()
	count := map[string]int{}
	for _, s := range idx.Sessions {
		count[s.Agent]++
	}
	w := table()
	fmt.Fprintln(w, "AGENT\tINSTALLED\tCOMMAND\tSESSIONS\tHISTORY")
	for _, p := range providers.All() {
		bin, _ := providers.Command(a.Cfg, p)
		inst := "no"
		if providers.Installed(a.Cfg, p) {
			inst = "yes"
		}
		hist := "launch only"
		switch p.Name() {
		case "claude":
			hist = "~/.claude/projects"
		case "codex":
			hist = "~/.codex/sessions"
		case "opencode":
			hist = "~/.local/share/opencode/storage"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", p.Name(), inst, bin, count[p.Name()], hist)
	}
	w.Flush()
	fmt.Printf("\ntmux: %v   config: %s\n", mux.Available(), config.Path())
	for _, e := range idx.Errors {
		fmt.Println("warning:", e)
	}
	return nil
}

func cmdConfig(a *app.App, args []string) error {
	sub := first(args)
	switch sub {
	case "", "show":
		b, _ := json.MarshalIndent(a.Cfg, "", "  ")
		fmt.Printf("# %s\n%s\n", config.Path(), b)
	case "path":
		fmt.Println(config.Path())
	case "init":
		if _, err := os.Stat(config.Path()); err == nil {
			return fmt.Errorf("%s already exists", config.Path())
		}
		c := config.Default()
		c.Budgets = config.Budgets{DailyUSD: 0, WeeklyUSD: 0, MonthlyUSD: 0}
		if err := config.Save(c); err != nil {
			return err
		}
		fmt.Println("wrote", config.Path())
	case "edit":
		if _, err := os.Stat(config.Path()); err != nil {
			if err := config.Save(a.Cfg); err != nil {
				return err
			}
		}
		ed := os.Getenv("EDITOR")
		if ed == "" {
			ed = "vi"
		}
		cmd := exec.Command(ed, config.Path())
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	case "statusline":
		self, _ := os.Executable()
		fmt.Printf(`To let optimus see your Claude plan's remaining 5h / 7d quota, set it as
Claude Code's status line in ~/.claude/settings.json:

  "statusLine": { "type": "command", "command": "%s statusline" }

If you already have a status line command, keep it by chaining it in
%s:

  "statusline_chain": "<your existing command>"
`, self, config.Path())
	default:
		return fmt.Errorf("usage: optimus config [show|path|init|edit|statusline]")
	}
	return nil
}

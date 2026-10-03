// Package app wires config, index, multiplexer and handoff together for both
// the CLI and the TUI.
package app

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Unchained-Labs/optimus/internal/config"
	"github.com/Unchained-Labs/optimus/internal/handoff"
	"github.com/Unchained-Labs/optimus/internal/index"
	"github.com/Unchained-Labs/optimus/internal/model"
	"github.com/Unchained-Labs/optimus/internal/mux"
	"github.com/Unchained-Labs/optimus/internal/pricing"
	"github.com/Unchained-Labs/optimus/internal/providers"
	"github.com/Unchained-Labs/optimus/internal/worktree"
)

type App struct {
	Cfg    config.Config
	Prices *pricing.Table
	CfgErr error
}

func New() *App {
	cfg, err := config.Load()
	return &App{Cfg: cfg, Prices: pricing.New(cfg.Pricing), CfgErr: err}
}

func (a *App) Index() *index.Index { return index.Load(a.Prices) }

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (a *App) provider(agent string) (providers.Provider, string, []string, error) {
	p := providers.Get(agent)
	if p == nil {
		return nil, "", nil, fmt.Errorf("unknown agent %q (known: %v)", agent, providers.Names())
	}
	bin, extra := providers.Command(a.Cfg, p)
	if !providers.Installed(a.Cfg, p) {
		return nil, "", nil, fmt.Errorf("%s is not installed (looked for %q on PATH; override with agents.%s.command in %s)", agent, bin, agent, config.Path())
	}
	return p, bin, extra, nil
}

// LaunchRequest describes a new session. Zero values mean "use the config".
type LaunchRequest struct {
	Agent         string `json:"agent"`
	Dir           string `json:"dir"`
	Prompt        string `json:"prompt"`
	Name          string `json:"name"`
	RemoteControl *bool  `json:"remote_control,omitempty"`
	// Worktree runs the agent in its own git worktree and branch.
	Worktree bool `json:"worktree,omitempty"`
}

func (a *App) opts(name string, rc *bool) providers.LaunchOpts {
	o := providers.LaunchOpts{Name: name, RemoteControl: a.Cfg.ClaudeRemoteControl()}
	if self, err := os.Executable(); err == nil {
		o.Hooks = self
	}
	if rc != nil {
		o.RemoteControl = *rc
	}
	return o
}

// Launch starts a new agent session in the multiplexer.
func (a *App) Launch(agent, dir, prompt, name string) (string, error) {
	return a.LaunchWith(LaunchRequest{Agent: agent, Dir: dir, Prompt: prompt, Name: name})
}

func (a *App) LaunchWith(r LaunchRequest) (string, error) {
	agent, dir, prompt, name := r.Agent, r.Dir, r.Prompt, r.Name
	if agent == "" {
		agent = a.Cfg.Agent()
	}
	p, bin, extra, err := a.provider(agent)
	if err != nil {
		return "", err
	}
	if dir == "" {
		dir, _ = os.Getwd()
	}
	sid := ""
	if p.PresetID() {
		sid = newUUID()
	}
	argPrompt := prompt
	if !providers.AcceptsPrompt(p) {
		argPrompt = ""
	}
	if name == "" {
		name = agent + "-" + model.ProjectName(dir)
	}
	name = mux.Sanitize(name)
	var tree *worktree.Tree
	if r.Worktree {
		label := firstWords(prompt, 4) // the branch says what the task is
		if label == "" {
			label = strings.TrimPrefix(r.Name, agent+"-")
		}
		t, err := worktree.Create(dir, agent, label)
		if err != nil {
			return "", err
		}
		tree, dir = &t, t.Path
	}
	argv := append(append([]string{bin}, extra...), p.NewArgs(argPrompt, sid, a.opts(name, r.RemoteControl))...)
	id, err := mux.Spawn(name, agent, dir, sid, argv)
	if err != nil {
		if tree != nil {
			_ = tree.Remove(true)
		}
		return "", err
	}
	if tree != nil {
		for k, v := range map[string]string{"@optimus_worktree": tree.Path, "@optimus_repo": tree.Repo, "@optimus_base": tree.Base, "@optimus_branch": tree.Branch} {
			mux.SetWindowOption(id, k, v)
		}
	}
	a.EnsureWeb()
	if prompt != "" && argPrompt == "" {
		// agent can't take a prompt on argv: type it once its UI is up
		time.Sleep(3 * time.Second)
		if err := mux.Send(id, prompt, true); err != nil {
			return id, err
		}
	}
	return id, nil
}

func firstWords(s string, n int) string {
	f := strings.Fields(s)
	if len(f) > n {
		f = f[:n]
	}
	return strings.Join(f, " ")
}

// Tree returns the worktree a window's agent works in, if any.
func Tree(w mux.Window) (worktree.Tree, bool) {
	if w.Worktree == "" {
		return worktree.Tree{}, false
	}
	return worktree.Tree{Repo: w.Repo, Path: w.Worktree, Base: w.Base, Branch: w.Branch}, true
}

// Fanout starts the same task in several agents, each in its own worktree of
// dir's repository, so their results can be compared and the best merged.
func (a *App) Fanout(agents []string, dir, prompt string) ([]string, error) {
	if _, ok := worktree.Root(dir); !ok {
		return nil, fmt.Errorf("fan-out needs a git repository: %s isn't one", dir)
	}
	var ids []string
	for _, ag := range agents {
		name := ag + "-" + mux.Sanitize(firstWords(prompt, 3))
		id, err := a.LaunchWith(LaunchRequest{Agent: ag, Dir: dir, Prompt: prompt, Name: name, Worktree: true})
		if err != nil {
			return ids, fmt.Errorf("%s: %w", ag, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// Resume reopens an existing session in the multiplexer, or returns the
// window it is already running in.
func (a *App) Resume(s *model.Session) (string, error) {
	if ws, _ := mux.List(); ws != nil {
		for _, w := range ws {
			if w.SessionID == s.ID && !w.Dead {
				return w.ID, nil
			}
		}
	}
	p, bin, extra, err := a.provider(s.Agent)
	if err != nil {
		return "", err
	}
	name := mux.Sanitize(s.Agent + "-" + s.ShortID())
	ra := p.ResumeArgs(s.ID, a.opts(name, nil))
	if ra == nil {
		return "", fmt.Errorf("%s does not support resuming sessions", s.Agent)
	}
	argv := append(append([]string{bin}, extra...), ra...)
	dir := s.Cwd
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		dir, _ = os.Getwd()
	}
	id, err := mux.Spawn(name, s.Agent, dir, s.ID, argv)
	if err == nil {
		a.EnsureWeb()
	}
	return id, err
}

// WebRunning reports whether the web dashboard answers on its address.
func (a *App) WebRunning() bool {
	c := http.Client{Timeout: 400 * time.Millisecond}
	resp, err := c.Get("http://" + a.Cfg.WebAddr() + "/healthz")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// WebURL is the dashboard address including the access token.
func (a *App) WebURL() string {
	tok, _ := config.WebToken()
	host := a.Cfg.WebAddr()
	if strings.HasPrefix(host, "0.0.0.0:") || strings.HasPrefix(host, ":") {
		host = "127.0.0.1:" + host[strings.LastIndex(host, ":")+1:]
	}
	return "http://" + host + "/?token=" + tok
}

// EnsureWeb starts the web dashboard in the background when autostart is on
// and it isn't already running. It returns whether the dashboard is up.
func (a *App) EnsureWeb() bool {
	if a.WebRunning() {
		return true
	}
	if !a.Cfg.WebAutostart() || !mux.Available() {
		return false
	}
	self, err := os.Executable()
	if err != nil {
		return false
	}
	home, _ := os.UserHomeDir()
	if err := mux.StartService("web", home, []string{self, "web", "--addr", a.Cfg.WebAddr()}); err != nil {
		return false
	}
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		if a.WebRunning() {
			return true
		}
	}
	return false
}

// HandoffDoc builds (and optionally condenses) a handoff document for a
// session and saves it to disk.
func (a *App) HandoffDoc(s *model.Session, note string, summarize bool) (doc, path string, err error) {
	p := providers.Get(s.Agent)
	if p == nil {
		return "", "", errors.New("unknown agent " + s.Agent)
	}
	msgs, err := p.Transcript(s)
	if err != nil {
		return "", "", err
	}
	doc = handoff.Build(s, msgs, handoff.Options{MaxTokens: a.Cfg.HandoffTokens, Note: note})
	if summarize {
		sp := providers.Get(a.Cfg.SummarizeWith)
		if sp == nil {
			return "", "", fmt.Errorf("summarize_with agent %q unknown", a.Cfg.SummarizeWith)
		}
		bin, _ := providers.Command(a.Cfg, sp)
		sum, err := handoff.Summarize(bin, sp.Name(), doc)
		if err != nil {
			return "", "", err
		}
		doc = fmt.Sprintf("# Handoff: %s\n\nCondensed by optimus from %s session `%s` in `%s`.\n\n%s\n", s.DisplayTitle(), s.Agent, s.ID, s.Cwd, sum)
	}
	path, err = handoff.Save(s, doc)
	return doc, path, err
}

// HandoffTo launches a new agent in the session's project, primed with the
// handoff document.
func (a *App) HandoffTo(s *model.Session, agent, dir, note string, summarize bool) (string, string, error) {
	_, path, err := a.HandoffDoc(s, note, summarize)
	if err != nil {
		return "", "", err
	}
	if dir == "" {
		dir = s.Cwd
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		dir, _ = os.Getwd()
	}
	id, err := a.Launch(agent, dir, handoff.Kickoff(path), agent+"-from-"+s.ShortID())
	return id, path, err
}

// HandoffInto sends the handoff to an already-running optimus window.
func (a *App) HandoffInto(s *model.Session, windowID, note string, summarize bool) (string, error) {
	_, path, err := a.HandoffDoc(s, note, summarize)
	if err != nil {
		return "", err
	}
	return path, mux.Send(windowID, handoff.Kickoff(path), true)
}

// Package app wires config, index, multiplexer and handoff together for both
// the CLI and the TUI.
package app

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/wardn/optimus/internal/config"
	"github.com/wardn/optimus/internal/handoff"
	"github.com/wardn/optimus/internal/index"
	"github.com/wardn/optimus/internal/model"
	"github.com/wardn/optimus/internal/mux"
	"github.com/wardn/optimus/internal/pricing"
	"github.com/wardn/optimus/internal/providers"
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

// Launch starts a new agent session in the multiplexer.
func (a *App) Launch(agent, dir, prompt, name string) (string, error) {
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
	argv := append(append([]string{bin}, extra...), p.NewArgs(argPrompt, sid)...)
	if name == "" {
		name = agent + "-" + model.ProjectName(dir)
	}
	id, err := mux.Spawn(name, agent, dir, sid, argv)
	if err != nil {
		return "", err
	}
	if prompt != "" && argPrompt == "" {
		// agent can't take a prompt on argv: type it once its UI is up
		time.Sleep(3 * time.Second)
		if err := mux.Send(id, prompt, true); err != nil {
			return id, err
		}
	}
	return id, nil
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
	ra := p.ResumeArgs(s.ID)
	if ra == nil {
		return "", fmt.Errorf("%s does not support resuming sessions", s.Agent)
	}
	argv := append(append([]string{bin}, extra...), ra...)
	dir := s.Cwd
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		dir, _ = os.Getwd()
	}
	return mux.Spawn(s.Agent+"-"+s.ShortID(), s.Agent, dir, s.ID, argv)
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

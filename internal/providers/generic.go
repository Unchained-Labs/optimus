package providers

import (
	"os"

	"github.com/wardn/optimus/internal/model"
)

// Generic is a launch-only agent: optimus can run it in the multiplexer and
// hand context to it, but doesn't read its history.
type Generic struct {
	name, bin  string
	promptFlag string // "" = positional prompt, "-" = no initial prompt support
	resume     []string
}

func init() {
	register(Generic{name: "gemini", bin: "gemini", promptFlag: "-i", resume: []string{"--resume"}})
	register(Generic{name: "cursor-agent", bin: "cursor-agent", resume: []string{"--resume"}})
	register(Generic{name: "aider", bin: "aider", promptFlag: "-"})
	register(Generic{name: "amp", bin: "amp", promptFlag: "-"})
	register(Generic{name: "crush", bin: "crush", promptFlag: "-"})
	register(Generic{name: "goose", bin: "goose", promptFlag: "-"})
	register(Generic{name: "shell", bin: "", promptFlag: "-"})
}

func (g Generic) Name() string { return g.name }
func (g Generic) Binary() string {
	if g.bin == "" {
		return defaultShell()
	}
	return g.bin
}
func (g Generic) PresetID() bool                                   { return false }
func (Generic) Discover() ([]Source, error)                        { return nil, nil }
func (Generic) Parse(Source) (*model.Session, error)               { return nil, nil }
func (Generic) Transcript(*model.Session) ([]model.Message, error) { return nil, nil }

func (g Generic) NewArgs(prompt, _ string) []string {
	if prompt == "" || g.promptFlag == "-" {
		return nil
	}
	if g.promptFlag == "" {
		return []string{prompt}
	}
	return []string{g.promptFlag, prompt}
}

// AcceptsPrompt reports whether an initial prompt can be passed on the
// command line; otherwise it must be typed into the pane after launch.
func AcceptsPrompt(p Provider) bool {
	if g, ok := p.(Generic); ok {
		return g.promptFlag != "-"
	}
	return true
}

func (g Generic) ResumeArgs(id string) []string {
	if len(g.resume) == 0 {
		return nil
	}
	return append(append([]string{}, g.resume...), id)
}

func defaultShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "sh"
}

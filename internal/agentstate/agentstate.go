// Package agentstate records what each agent is doing, as reported by the
// agent itself through hooks (`optimus hook claude|codex`), and combines that
// with the screen heuristic into one state per window.
//
// Hooks run inside the agent's tmux pane, so they key their record by
// $TMUX_PANE; the multiplexer lists each window's pane id.
package agentstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Unchained-Labs/optimus/internal/config"
	"github.com/Unchained-Labs/optimus/internal/mux"
)

// Record is the last lifecycle event an agent reported.
type Record struct {
	Pane      string    `json:"pane"`
	Agent     string    `json:"agent"`
	SessionID string    `json:"session_id,omitempty"`
	State     mux.State `json:"state"`
	Message   string    `json:"message,omitempty"`
	Event     string    `json:"event"`
	At        time.Time `json:"at"`
	// Full means the agent reports its whole lifecycle (busy, input, idle),
	// so the record is authoritative. Otherwise it only marks some events
	// (e.g. Codex's turn-complete) and the screen decides the rest.
	Full bool `json:"full"`
}

func dir() string { return filepath.Join(config.StateDir(), "agents") }

func fileFor(pane string) string {
	return filepath.Join(dir(), strings.TrimPrefix(pane, "%")+".json")
}

// Write stores a record for its pane.
func Write(r Record) error {
	if r.Pane == "" {
		return nil
	}
	if err := os.MkdirAll(dir(), 0o755); err != nil {
		return err
	}
	if r.At.IsZero() {
		r.At = time.Now()
	}
	b, _ := json.Marshal(r)
	tmp := fileFor(r.Pane) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, fileFor(r.Pane))
}

// Read returns the record for a pane, if any.
func Read(pane string) (Record, bool) {
	var r Record
	if pane == "" {
		return r, false
	}
	b, err := os.ReadFile(fileFor(pane))
	if err != nil || json.Unmarshal(b, &r) != nil {
		return r, false
	}
	return r, true
}

// Forget removes records of panes that no longer exist.
func Forget(alive map[string]bool) {
	files, _ := filepath.Glob(filepath.Join(dir(), "*.json"))
	for _, f := range files {
		if !alive["%"+strings.TrimSuffix(filepath.Base(f), ".json")] {
			_ = os.Remove(f)
		}
	}
}

// Info is a window's resolved state.
type Info struct {
	State   mux.State
	Message string    // e.g. "Claude needs your permission to use Bash"
	Since   time.Time // when the state was reported (zero for screen-only)
	Source  string    // "hook" or "screen"
}

// staleBusy is how long a hook-reported "busy" is trusted when the screen
// disagrees (an interrupted turn doesn't fire a Stop hook).
const staleBusy = 45 * time.Second

// Resolve decides a window's state from its hook record and its screen.
func Resolve(w mux.Window, screen string) Info {
	scr := mux.Detect(w, screen)
	if w.Dead {
		return Info{State: mux.StateExited, Source: "screen"}
	}
	r, ok := Read(w.Pane)
	if w.External || !ok || r.At.Before(w.Created.Add(-time.Second)) { // pane ids of other tmux servers mean nothing here
		return Info{State: scr, Message: screenPrompt(scr, screen), Source: "screen"}
	}
	if !r.Full {
		// partial reporters: the screen wins while it shows activity
		if scr == mux.StateIdle && r.State != "" {
			return Info{State: r.State, Message: r.Message, Since: r.At, Source: "hook"}
		}
		return Info{State: scr, Message: screenPrompt(scr, screen), Source: "screen"}
	}
	switch {
	case r.State == mux.StateBusy && scr != mux.StateBusy && time.Since(r.At) > staleBusy:
		return Info{State: scr, Message: screenPrompt(scr, screen), Source: "screen"}
	case r.State == mux.StateIdle && scr == mux.StateWaiting:
		// a prompt appeared that no hook announced (e.g. a menu)
		return Info{State: scr, Message: screenPrompt(scr, screen), Source: "screen"}
	}
	msg := r.Message
	if msg == "" && r.State == mux.StateWaiting {
		msg = screenPrompt(r.State, screen)
	}
	return Info{State: r.State, Message: msg, Since: r.At, Source: "hook"}
}

// screenPrompt pulls the question an agent is asking from the bottom of its
// screen, for display next to "needs input".
func screenPrompt(st mux.State, screen string) string {
	if st != mux.StateWaiting {
		return ""
	}
	lines := strings.Split(strings.TrimRight(screen, "\n "), "\n")
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-14; i-- {
		l := strings.TrimSpace(strings.Trim(lines[i], "│╭╮╰╯─ "))
		low := strings.ToLower(l)
		if strings.Contains(low, "?") && !strings.HasPrefix(low, "❯") && !strings.HasPrefix(low, "›") && len(l) > 6 {
			return l
		}
	}
	return ""
}

// Prompt returns the last lines of the screen, trimmed, for showing an
// agent's pending question without attaching to it.
func Prompt(screen string, n int) []string {
	lines := strings.Split(strings.TrimRight(screen, "\n "), "\n")
	var out []string
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		l := strings.TrimRight(lines[i], " ")
		if strings.TrimSpace(l) == "" && len(out) == 0 {
			continue
		}
		out = append([]string{l}, out...)
	}
	return out
}

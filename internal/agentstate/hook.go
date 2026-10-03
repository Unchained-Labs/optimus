package agentstate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Unchained-Labs/optimus/internal/mux"
)

// FromClaude maps a Claude Code hook payload (JSON on the hook's stdin) to a
// record. ok is false for events that don't change the state.
func FromClaude(payload []byte) (Record, bool) {
	var p struct {
		Event     string          `json:"hook_event_name"`
		SessionID string          `json:"session_id"`
		Message   string          `json:"message"`
		Tool      string          `json:"tool_name"`
		Cwd       string          `json:"cwd"`
		ToolInput json.RawMessage `json:"tool_input"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return Record{}, false
	}
	r := Record{Agent: "claude", SessionID: p.SessionID, Event: p.Event, Full: true}
	switch p.Event {
	case "UserPromptSubmit", "PreToolUse", "PostToolUse", "SubagentStart":
		r.State = mux.StateBusy
	case "PermissionRequest":
		r.State = mux.StateWaiting
		r.Message = "wants to use " + p.Tool + toolSummary(p.ToolInput, p.Cwd)
	case "Notification":
		low := strings.ToLower(p.Message)
		switch {
		case strings.Contains(low, "permission"), strings.Contains(low, "approve"), strings.Contains(low, "needs your"):
			r.State = mux.StateWaiting
		case strings.Contains(low, "waiting for your input"), strings.Contains(low, "idle"):
			r.State = mux.StateIdle
		default:
			return r, false
		}
		r.Message = p.Message
	case "Stop":
		r.State = mux.StateIdle
		r.Message = "finished"
	case "SessionEnd":
		r.State = mux.StateExited
	default:
		return r, false
	}
	return r, true
}

// FromCodex maps the JSON Codex passes to its `notify` program (as the last
// argument). Codex only reports turn completion, so the record is partial.
func FromCodex(payload []byte) (Record, bool) {
	var p struct {
		Type    string `json:"type"`
		Last    string `json:"last-assistant-message"`
		Session string `json:"thread-id"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return Record{}, false
	}
	switch p.Type {
	case "agent-turn-complete":
		msg := "finished"
		if s := strings.TrimSpace(p.Last); s != "" {
			msg = "finished: " + firstLine(s, 80)
		}
		return Record{Agent: "codex", SessionID: p.Session, Event: p.Type, State: mux.StateIdle, Message: msg}, true
	case "approval-requested":
		return Record{Agent: "codex", SessionID: p.Session, Event: p.Type, State: mux.StateWaiting, Message: "needs approval"}, true
	}
	return Record{}, false
}

func toolSummary(input json.RawMessage, cwd string) string {
	var in map[string]any
	if json.Unmarshal(input, &in) != nil {
		return ""
	}
	for _, k := range []string{"command", "file_path", "path", "url", "pattern"} {
		if v, ok := in[k].(string); ok && v != "" {
			if cwd != "" {
				v = strings.TrimPrefix(v, strings.TrimSuffix(cwd, "/")+"/")
			}
			return ": " + firstLine(v, 70)
		}
	}
	return ""
}

func firstLine(s string, n int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// ClaudeSettings is the --settings JSON that makes a Claude session report
// its lifecycle to optimus. Hooks from settings files are merged, so the
// user's own hooks keep running.
func ClaudeSettings(optimusBin string) string {
	cmd := fmt.Sprintf("%s hook claude", shellQuote(optimusBin))
	entry := func(matcher bool) []map[string]any {
		h := map[string]any{"hooks": []map[string]any{{"type": "command", "command": cmd, "timeout": 5}}}
		if matcher {
			h["matcher"] = "*"
		}
		return []map[string]any{h}
	}
	hooks := map[string]any{
		"UserPromptSubmit":  entry(false),
		"PreToolUse":        entry(true),
		"PermissionRequest": entry(true),
		"Notification":      entry(false),
		"Stop":              entry(false),
		"SessionEnd":        entry(false),
	}
	b, _ := json.Marshal(map[string]any{"hooks": hooks})
	return string(b)
}

// CodexNotify is the `-c notify=…` override that makes Codex report turn
// completion to optimus.
func CodexNotify(optimusBin string) string {
	b, _ := json.Marshal([]string{optimusBin, "hook", "codex"})
	return "notify=" + string(b)
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

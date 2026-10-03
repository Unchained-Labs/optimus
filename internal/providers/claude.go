package providers

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Unchained-Labs/optimus/internal/agentstate"
	"github.com/Unchained-Labs/optimus/internal/config"
	"github.com/Unchained-Labs/optimus/internal/model"
)

// Claude reads Claude Code transcripts from ~/.claude/projects/<slug>/<id>.jsonl
// (plus <id>/subagents/*.jsonl for sub-agent usage).
type Claude struct{}

func init() { register(Claude{}) }

func (Claude) Name() string   { return "claude" }
func (Claude) Binary() string { return "claude" }
func (Claude) PresetID() bool { return true }

func claudeRoot() string {
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return v
	}
	return filepath.Join(config.Home(), ".claude")
}

func (Claude) Discover() ([]Source, error) {
	files, err := filepath.Glob(filepath.Join(claudeRoot(), "projects", "*", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	var out []Source
	for _, f := range files {
		key, _, ok := fileKey(f)
		if !ok {
			continue
		}
		id := strings.TrimSuffix(filepath.Base(f), ".jsonl")
		subs, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(f, ".jsonl"), "subagents", "*.jsonl"))
		for _, s := range subs {
			if k, _, ok := fileKey(s); ok {
				key += "|" + k
			}
		}
		out = append(out, Source{Agent: "claude", ID: id, Path: f, Key: key})
	}
	return out, nil
}

type claudeUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreation            *struct {
		Eph5m int64 `json:"ephemeral_5m_input_tokens"`
		Eph1h int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
	OutputDetails *struct {
		Thinking int64 `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

type claudeLine struct {
	Type        string `json:"type"`
	Timestamp   string `json:"timestamp"`
	Cwd         string `json:"cwd"`
	GitBranch   string `json:"gitBranch"`
	IsSidechain bool   `json:"isSidechain"`
	IsMeta      bool   `json:"isMeta"`
	AiTitle     string `json:"aiTitle"`
	CustomTitle string `json:"customTitle"`
	Summary     string `json:"summary"`
	LastPrompt  string `json:"lastPrompt"`
	RequestID   string `json:"requestId"`
	Message     *struct {
		ID      string          `json:"id"`
		Role    string          `json:"role"`
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
		Usage   *claudeUsage    `json:"usage"`
	} `json:"message"`
}

type claudeBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func (u *claudeUsage) toUsage() model.Usage {
	out := model.Usage{
		Input:     u.InputTokens,
		Output:    u.OutputTokens,
		CacheRead: u.CacheReadInputTokens,
	}
	if u.CacheCreation != nil && (u.CacheCreation.Eph5m+u.CacheCreation.Eph1h) > 0 {
		out.CacheWrite5m = u.CacheCreation.Eph5m
		out.CacheWrite1h = u.CacheCreation.Eph1h
	} else {
		out.CacheWrite5m = u.CacheCreationInputTokens
	}
	if u.OutputDetails != nil {
		out.Reasoning = u.OutputDetails.Thinking
	}
	return out
}

// userText returns the human-authored text of a user message, or "" when the
// message is a tool result or harness noise.
func claudeUserText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if raw[0] == '"' {
		if json.Unmarshal(raw, &s) != nil {
			return ""
		}
	} else {
		var blocks []claudeBlock
		if json.Unmarshal(raw, &blocks) != nil {
			return ""
		}
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && !isNoise(b.Text) {
				parts = append(parts, b.Text)
			}
		}
		s = strings.Join(parts, "\n")
	}
	if name, ok := commandName(s); ok {
		return name
	}
	if isNoise(s) {
		return ""
	}
	return strings.TrimSpace(s)
}

func (c Claude) Parse(src Source) (*model.Session, error) {
	s := &model.Session{Agent: "claude", ID: src.ID, Path: src.Path}
	bk := bucketer{}
	seen := map[string]bool{}
	var aiTitle, customTitle, summary string

	addUsage := func(l *claudeLine, t time.Time) {
		m := l.Message
		if m == nil || m.Usage == nil || m.Model == "" || m.Model == "<synthetic>" {
			return
		}
		key := m.ID + ":" + l.RequestID
		if m.ID != "" && seen[key] {
			return
		}
		seen[key] = true
		bk.add(t, m.Model, m.Usage.toUsage())
	}

	err := eachLine(src.Path, func(b []byte) {
		var l claudeLine
		if json.Unmarshal(b, &l) != nil {
			return
		}
		switch l.Type {
		case "ai-title":
			aiTitle = l.AiTitle
			return
		case "custom-title":
			customTitle = l.CustomTitle
			return
		case "summary":
			if summary == "" {
				summary = l.Summary
			}
			return
		case "last-prompt":
			if l.LastPrompt != "" {
				s.LastPrompt = l.LastPrompt
			}
			return
		case "user", "assistant":
		default:
			return
		}
		t := parseTime(l.Timestamp)
		if !t.IsZero() {
			if s.Start.IsZero() || t.Before(s.Start) {
				s.Start = t
			}
			if t.After(s.End) {
				s.End = t
			}
		}
		if s.Cwd == "" && l.Cwd != "" {
			s.Cwd = l.Cwd
		}
		if l.GitBranch != "" && l.GitBranch != "HEAD" {
			s.Branch = l.GitBranch
		}
		if l.Type == "assistant" {
			addUsage(&l, t)
			return
		}
		if l.IsSidechain || l.IsMeta || l.Message == nil {
			return
		}
		if txt := claudeUserText(l.Message.Content); txt != "" {
			s.Messages++
			if !strings.HasPrefix(txt, "/") {
				if s.FirstPrompt == "" {
					s.FirstPrompt = model.Truncate(txt, 400)
				}
				s.LastPrompt = model.Truncate(txt, 400)
			}
		}
	})
	if err != nil {
		return nil, err
	}

	// sub-agent transcripts: usage only
	subs, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(src.Path, ".jsonl"), "subagents", "*.jsonl"))
	for _, f := range subs {
		_ = eachLine(f, func(b []byte) {
			if !bytes.Contains(b, []byte(`"usage"`)) {
				return
			}
			var l claudeLine
			if json.Unmarshal(b, &l) != nil || l.Type != "assistant" {
				return
			}
			addUsage(&l, parseTime(l.Timestamp))
		})
	}

	s.Buckets = bk.list()
	s.Model = dominantModel(s.Buckets)
	switch {
	case customTitle != "":
		s.Title = customTitle
	case aiTitle != "":
		s.Title = aiTitle
	case summary != "":
		s.Title = summary
	}
	if s.Start.IsZero() && s.FirstPrompt == "" && len(s.Buckets) == 0 {
		return nil, nil // empty shell file
	}
	return s, nil
}

func (Claude) Transcript(s *model.Session) ([]model.Message, error) {
	var out []model.Message
	lastID := ""
	err := eachLine(s.Path, func(b []byte) {
		var l claudeLine
		if json.Unmarshal(b, &l) != nil || l.Message == nil || l.IsSidechain {
			return
		}
		t := parseTime(l.Timestamp)
		switch l.Type {
		case "user":
			if l.IsMeta {
				return
			}
			if txt := claudeUserText(l.Message.Content); txt != "" {
				out = append(out, model.Message{Role: "user", Text: txt, Time: t})
				lastID = ""
			}
		case "assistant":
			var blocks []claudeBlock
			if json.Unmarshal(l.Message.Content, &blocks) != nil {
				return
			}
			// Claude Code writes one line per content block of a response;
			// lines sharing a message id belong together.
			if n := len(out); n == 0 || l.Message.ID == "" || l.Message.ID != lastID || out[n-1].Role != "assistant" {
				out = append(out, model.Message{Role: "assistant", Time: t})
			}
			lastID = l.Message.ID
			msg := &out[len(out)-1]
			for _, bl := range blocks {
				switch bl.Type {
				case "text":
					if strings.TrimSpace(bl.Text) != "" {
						if msg.Text != "" {
							msg.Text += "\n"
						}
						msg.Text += bl.Text
					}
				case "tool_use":
					msg.Tools = append(msg.Tools, toolCall(bl.Name, bl.Input))
				}
			}
		}
	})
	// drop assistant entries that ended up empty (thinking-only responses)
	kept := out[:0]
	for _, m := range out {
		if m.Role == "user" || m.Text != "" || len(m.Tools) > 0 {
			kept = append(kept, m)
		}
	}
	return kept, err
}

// toolCall summarizes a tool invocation from its JSON input.
func toolCall(name string, input json.RawMessage) model.ToolCall {
	var in map[string]any
	_ = json.Unmarshal(input, &in)
	tc := model.ToolCall{Name: name}
	str := func(k string) string {
		if v, ok := in[k].(string); ok {
			return v
		}
		return ""
	}
	for _, k := range []string{"file_path", "notebook_path", "path", "filePath"} {
		if v := str(k); v != "" {
			tc.Target = v
			break
		}
	}
	if tc.Target == "" {
		for _, k := range []string{"command", "cmd", "pattern", "url", "query", "description", "prompt"} {
			if v := str(k); v != "" {
				tc.Target = model.Truncate(model.OneLine(v), 120)
				break
			}
		}
	}
	switch strings.ToLower(name) {
	case "edit", "write", "multiedit", "notebookedit", "str_replace_based_edit_tool", "apply_patch", "patch", "create_file", "edit_file", "write_file":
		tc.Edit = true
	}
	return tc
}

// claudeOpts maps launch options to flags. Values are attached with "=" so an
// optional-value flag like --remote-control never swallows the prompt.
func claudeOpts(o LaunchOpts) []string {
	var a []string
	if o.Hooks != "" {
		a = append(a, "--settings", agentstate.ClaudeSettings(o.Hooks))
	}
	if o.Name != "" {
		a = append(a, "--name="+o.Name)
	}
	if o.RemoteControl {
		if o.Name != "" {
			a = append(a, "--remote-control="+o.Name)
		} else {
			a = append(a, "--remote-control")
		}
	}
	return a
}

func (Claude) NewArgs(prompt, sessionID string, o LaunchOpts) []string {
	var a []string
	if sessionID != "" {
		a = append(a, "--session-id", sessionID)
	}
	a = append(a, claudeOpts(o)...)
	if prompt != "" {
		if o.RemoteControl && o.Name == "" {
			a = append(a, "--") // keep the prompt out of --remote-control's optional value
		}
		a = append(a, prompt)
	}
	return a
}

func (Claude) ResumeArgs(id string, o LaunchOpts) []string {
	return append([]string{"--resume", id}, claudeOpts(o)...)
}

// Live reads ~/.claude/sessions/<pid>.json, which Claude Code keeps for each
// running interactive session.
func (Claude) Live() []LiveSession {
	files, _ := filepath.Glob(filepath.Join(claudeRoot(), "sessions", "*.json"))
	var out []LiveSession
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var v struct {
			PID       int    `json:"pid"`
			SessionID string `json:"sessionId"`
			Cwd       string `json:"cwd"`
			Name      string `json:"name"`
			Status    string `json:"status"`
		}
		if json.Unmarshal(b, &v) != nil || !pidAlive(v.PID) {
			continue
		}
		out = append(out, LiveSession{Agent: "claude", ID: v.SessionID, PID: v.PID, Cwd: v.Cwd, Name: v.Name, Status: v.Status})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

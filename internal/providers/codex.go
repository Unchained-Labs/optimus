package providers

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/wardn/optimus/internal/config"
	"github.com/wardn/optimus/internal/model"
)

// Codex reads OpenAI Codex CLI rollouts from
// ~/.codex/sessions/YYYY/MM/DD/rollout-<ts>-<uuid>.jsonl.
type Codex struct{}

func init() { register(Codex{}) }

func (Codex) Name() string   { return "codex" }
func (Codex) Binary() string { return "codex" }
func (Codex) PresetID() bool { return false }

func codexRoot() string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return v
	}
	return filepath.Join(config.Home(), ".codex")
}

var uuidRe = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

func (Codex) Discover() ([]Source, error) {
	var out []Source
	for _, dir := range []string{"sessions", "archived_sessions"} {
		root := filepath.Join(codexRoot(), dir)
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".jsonl") {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			id := uuidRe.FindString(filepath.Base(p))
			if id == "" {
				id = strings.TrimSuffix(filepath.Base(p), ".jsonl")
			}
			out = append(out, Source{Agent: "codex", ID: id, Path: p, Key: keyOf(info)})
			return nil
		})
	}
	return out, nil
}

// codexLine covers both the current envelope ({timestamp,type,payload}) and
// the older flat format where each line is the item itself.
type codexLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexItem struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Cwd       string          `json:"cwd"`
	Model     string          `json:"model"`
	Role      string          `json:"role"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
	Input     string          `json:"input"`
	Message   string          `json:"message"`
	Content   []codexContent  `json:"content"`
	Info      *codexTokenInfo `json:"info"`
	Git       *struct {
		Branch string `json:"branch"`
	} `json:"git"`
	RateLimits json.RawMessage `json:"rate_limits"`
}

type codexContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type codexTokens struct {
	Input     int64 `json:"input_tokens"`
	Cached    int64 `json:"cached_input_tokens"`
	Output    int64 `json:"output_tokens"`
	Reasoning int64 `json:"reasoning_output_tokens"`
}

type codexTokenInfo struct {
	Total *codexTokens `json:"total_token_usage"`
	Last  *codexTokens `json:"last_token_usage"`
}

func (l *codexLine) item() (codexItem, time.Time) {
	var it codexItem
	if len(l.Payload) > 0 {
		_ = json.Unmarshal(l.Payload, &it)
		if l.Type == "session_meta" {
			it.Type = "session_meta"
		} else if l.Type == "turn_context" {
			it.Type = "turn_context"
		}
	}
	return it, parseTime(l.Timestamp)
}

func codexText(c []codexContent) string {
	var parts []string
	for _, x := range c {
		if (x.Type == "input_text" || x.Type == "output_text" || x.Type == "text") && strings.TrimSpace(x.Text) != "" {
			parts = append(parts, x.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func (Codex) scan(path string, fn func(it codexItem, t time.Time)) error {
	return eachLine(path, func(b []byte) {
		var l codexLine
		if json.Unmarshal(b, &l) != nil {
			return
		}
		if len(l.Payload) == 0 {
			// legacy flat line
			var it codexItem
			if json.Unmarshal(b, &it) != nil {
				return
			}
			if it.Type == "" && it.ID != "" {
				it.Type = "session_meta"
			}
			fn(it, parseTime(l.Timestamp))
			return
		}
		it, t := l.item()
		fn(it, t)
	})
}

func (c Codex) Parse(src Source) (*model.Session, error) {
	s := &model.Session{Agent: "codex", ID: src.ID, Path: src.Path}
	bk := bucketer{}
	curModel := "gpt-5"
	var prev codexTokens
	var lastT time.Time
	err := c.scan(src.Path, func(it codexItem, t time.Time) {
		if !t.IsZero() {
			lastT = t
			if s.Start.IsZero() {
				s.Start = t
			}
			s.End = t
		}
		switch it.Type {
		case "session_meta":
			if it.ID != "" {
				s.ID = it.ID
			}
			if it.Cwd != "" {
				s.Cwd = it.Cwd
			}
			if it.Git != nil && it.Git.Branch != "" {
				s.Branch = it.Git.Branch
			}
		case "turn_context":
			if it.Model != "" {
				curModel = it.Model
			}
			if s.Cwd == "" && it.Cwd != "" {
				s.Cwd = it.Cwd
			}
		case "message":
			if it.Role != "user" {
				return
			}
			if txt := codexText(it.Content); !isNoise(txt) {
				s.Messages++
				if s.FirstPrompt == "" {
					s.FirstPrompt = model.Truncate(txt, 400)
				}
				s.LastPrompt = model.Truncate(txt, 400)
			}
		case "token_count":
			if it.Info == nil {
				return
			}
			var d codexTokens
			switch {
			case it.Info.Total != nil:
				cur := *it.Info.Total
				d = codexTokens{cur.Input - prev.Input, cur.Cached - prev.Cached, cur.Output - prev.Output, cur.Reasoning - prev.Reasoning}
				if d.Input < 0 || d.Output < 0 {
					d = codexTokens{}
					if it.Info.Last != nil {
						d = *it.Info.Last
					}
				}
				prev = cur
			case it.Info.Last != nil:
				d = *it.Info.Last
			}
			if d.Input == 0 && d.Output == 0 {
				return
			}
			when := t
			if when.IsZero() {
				when = lastT
			}
			bk.add(when, curModel, model.Usage{
				Input:     max(d.Input-d.Cached, 0),
				CacheRead: d.Cached,
				Output:    d.Output,
				Reasoning: d.Reasoning,
			})
		}
	})
	if err != nil {
		return nil, err
	}
	s.Buckets = bk.list()
	s.Model = dominantModel(s.Buckets)
	if s.Model == "" {
		s.Model = curModel
	}
	if s.Start.IsZero() && s.Messages == 0 {
		if _, mt, ok := fileKey(src.Path); ok {
			s.Start, s.End = mt, mt
		}
	}
	return s, nil
}

func (c Codex) Transcript(s *model.Session) ([]model.Message, error) {
	var out []model.Message
	err := c.scan(s.Path, func(it codexItem, t time.Time) {
		switch it.Type {
		case "message":
			txt := codexText(it.Content)
			if it.Role == "user" && isNoise(txt) || txt == "" {
				return
			}
			role := it.Role
			if role != "user" && role != "assistant" {
				return
			}
			out = append(out, model.Message{Role: role, Text: txt, Time: t})
		case "function_call", "custom_tool_call", "local_shell_call":
			tc := codexToolCall(it)
			if n := len(out); n > 0 && out[n-1].Role == "assistant" {
				out[n-1].Tools = append(out[n-1].Tools, tc)
			} else {
				out = append(out, model.Message{Role: "assistant", Time: t, Tools: []model.ToolCall{tc}})
			}
		}
	})
	return out, err
}

var patchFileRe = regexp.MustCompile(`\*\*\* (?:Update|Add|Delete) File: (.+)`)

func codexToolCall(it codexItem) model.ToolCall {
	name := it.Name
	if name == "" {
		name = it.Type
	}
	raw := it.Arguments
	if raw == "" {
		raw = it.Input
	}
	if m := patchFileRe.FindStringSubmatch(raw); m != nil {
		return model.ToolCall{Name: "apply_patch", Target: strings.TrimSpace(m[1]), Edit: true}
	}
	var args map[string]any
	if json.Unmarshal([]byte(raw), &args) == nil {
		if cmd, ok := args["command"].([]any); ok {
			var parts []string
			for _, p := range cmd {
				if s, ok := p.(string); ok {
					parts = append(parts, s)
				}
			}
			// ["bash","-lc","<script>"] → the script
			if len(parts) == 3 && (parts[1] == "-lc" || parts[1] == "-c") {
				parts = parts[2:]
			}
			return model.ToolCall{Name: name, Target: model.Truncate(model.OneLine(strings.Join(parts, " ")), 120)}
		}
		b, _ := json.Marshal(args)
		return toolCall(name, b)
	}
	return model.ToolCall{Name: name}
}

func (Codex) NewArgs(prompt, _ string) []string {
	if prompt == "" {
		return nil
	}
	return []string{prompt}
}

func (Codex) ResumeArgs(id string) []string { return []string{"resume", id} }

// LatestRateLimits returns the rate_limits object from the most recent
// token_count event of the newest rollout, if any.
func (c Codex) LatestRateLimits() (json.RawMessage, time.Time) {
	srcs, _ := c.Discover()
	var newest string
	var nt time.Time
	for _, s := range srcs {
		if _, mt, ok := fileKey(s.Path); ok && mt.After(nt) {
			newest, nt = s.Path, mt
		}
	}
	if newest == "" {
		return nil, time.Time{}
	}
	var rl json.RawMessage
	var at time.Time
	_ = c.scan(newest, func(it codexItem, t time.Time) {
		if it.Type == "token_count" && len(it.RateLimits) > 0 && string(it.RateLimits) != "null" {
			rl, at = it.RateLimits, t
		}
	})
	return rl, at
}

package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Unchained-Labs/optimus/internal/config"
	"github.com/Unchained-Labs/optimus/internal/model"
)

// OpenCode reads sst/opencode's file storage:
//
//	storage/session/<project>/<ses_id>.json
//	storage/message/<ses_id>/<msg_id>.json
//	storage/part/<msg_id>/<prt_id>.json
type OpenCode struct{}

func init() { register(OpenCode{}) }

func (OpenCode) Name() string   { return "opencode" }
func (OpenCode) Binary() string { return "opencode" }
func (OpenCode) PresetID() bool { return false }

func opencodeRoot() string {
	if v := os.Getenv("OPENCODE_DATA_DIR"); v != "" {
		return v
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		base = filepath.Join(config.Home(), ".local", "share")
	}
	return filepath.Join(base, "opencode", "storage")
}

func (OpenCode) Discover() ([]Source, error) {
	root := opencodeRoot()
	files, _ := filepath.Glob(filepath.Join(root, "session", "*", "*.json"))
	var out []Source
	for _, f := range files {
		key, _, ok := fileKey(f)
		if !ok {
			continue
		}
		id := strings.TrimSuffix(filepath.Base(f), ".json")
		// the message dir's mtime moves whenever a message is added
		if k, _, ok := fileKey(filepath.Join(root, "message", id)); ok {
			key += "|" + k
		}
		out = append(out, Source{Agent: "opencode", ID: id, Path: f, Key: key})
	}
	return out, nil
}

type ocSession struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Directory string `json:"directory"`
	ParentID  string `json:"parentID"`
	Time      struct {
		Created int64 `json:"created"`
		Updated int64 `json:"updated"`
	} `json:"time"`
}

type ocMessage struct {
	ID         string  `json:"id"`
	Role       string  `json:"role"`
	ModelID    string  `json:"modelID"`
	ProviderID string  `json:"providerID"`
	Cost       float64 `json:"cost"`
	Tokens     *struct {
		Input     int64 `json:"input"`
		Output    int64 `json:"output"`
		Reasoning int64 `json:"reasoning"`
		Cache     struct {
			Read  int64 `json:"read"`
			Write int64 `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
	Time struct {
		Created int64 `json:"created"`
	} `json:"time"`
	Path *struct {
		Cwd string `json:"cwd"`
	} `json:"path"`
}

type ocPart struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	Synthetic bool   `json:"synthetic"`
	Tool      string `json:"tool"`
	State     *struct {
		Input json.RawMessage `json:"input"`
	} `json:"state"`
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func (OpenCode) messages(id string) []ocMessage {
	files, _ := filepath.Glob(filepath.Join(opencodeRoot(), "message", id, "*.json"))
	var out []ocMessage
	for _, f := range files {
		var m ocMessage
		if readJSON(f, &m) == nil {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Time.Created != out[j].Time.Created {
			return out[i].Time.Created < out[j].Time.Created
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (OpenCode) parts(msgID string) []ocPart {
	files, _ := filepath.Glob(filepath.Join(opencodeRoot(), "part", msgID, "*.json"))
	sort.Strings(files)
	var out []ocPart
	for _, f := range files {
		var p ocPart
		if readJSON(f, &p) == nil {
			out = append(out, p)
		}
	}
	return out
}

func ocText(parts []ocPart) string {
	var s []string
	for _, p := range parts {
		if p.Type == "text" && !p.Synthetic && strings.TrimSpace(p.Text) != "" {
			s = append(s, p.Text)
		}
	}
	return strings.TrimSpace(strings.Join(s, "\n"))
}

func (o OpenCode) Parse(src Source) (*model.Session, error) {
	var ss ocSession
	if err := readJSON(src.Path, &ss); err != nil {
		return nil, err
	}
	s := &model.Session{
		Agent: "opencode", ID: ss.ID, Path: src.Path, Cwd: ss.Directory, Title: ss.Title,
		Start: msTime(ss.Time.Created), End: msTime(ss.Time.Updated),
	}
	if s.ID == "" {
		s.ID = src.ID
	}
	if ss.ParentID != "" && s.Title != "" {
		s.Title = "↳ " + s.Title
	}
	bk := bucketer{}
	for _, m := range o.messages(s.ID) {
		t := msTime(m.Time.Created)
		if t.After(s.End) {
			s.End = t
		}
		switch m.Role {
		case "user":
			if txt := ocText(o.parts(m.ID)); txt != "" {
				s.Messages++
				if s.FirstPrompt == "" {
					s.FirstPrompt = model.Truncate(txt, 400)
				}
				s.LastPrompt = model.Truncate(txt, 400)
			}
		case "assistant":
			if m.Tokens == nil {
				continue
			}
			if s.Cwd == "" && m.Path != nil {
				s.Cwd = m.Path.Cwd
			}
			bk.add(t, m.ModelID, model.Usage{
				Input:        m.Tokens.Input,
				Output:       m.Tokens.Output + m.Tokens.Reasoning,
				Reasoning:    m.Tokens.Reasoning,
				CacheRead:    m.Tokens.Cache.Read,
				CacheWrite5m: m.Tokens.Cache.Write,
				SourceCost:   m.Cost,
			})
		}
	}
	s.Buckets = bk.list()
	s.Model = dominantModel(s.Buckets)
	return s, nil
}

func (o OpenCode) Transcript(s *model.Session) ([]model.Message, error) {
	var out []model.Message
	for _, m := range o.messages(s.ID) {
		parts := o.parts(m.ID)
		msg := model.Message{Role: m.Role, Text: ocText(parts), Time: msTime(m.Time.Created)}
		for _, p := range parts {
			if p.Type == "tool" {
				var in json.RawMessage
				if p.State != nil {
					in = p.State.Input
				}
				msg.Tools = append(msg.Tools, toolCall(p.Tool, in))
			}
		}
		if msg.Text == "" && len(msg.Tools) == 0 {
			continue
		}
		out = append(out, msg)
	}
	return out, nil
}

func (OpenCode) NewArgs(prompt, _ string, _ LaunchOpts) []string {
	if prompt == "" {
		return nil
	}
	return []string{"--prompt", prompt}
}

func (OpenCode) ResumeArgs(id string, _ LaunchOpts) []string { return []string{"--session", id} }

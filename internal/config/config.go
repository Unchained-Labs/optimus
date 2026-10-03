// Package config resolves optimus paths and the user's config file.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Budgets struct {
	DailyUSD   float64 `json:"daily_usd,omitempty"`
	WeeklyUSD  float64 `json:"weekly_usd,omitempty"`
	MonthlyUSD float64 `json:"monthly_usd,omitempty"`
	BlockUSD   float64 `json:"block_usd,omitempty"`
}

// Price is USD per million tokens. Zero cache fields fall back to the
// standard multipliers of Input.
type Price struct {
	Input        float64 `json:"input"`
	Output       float64 `json:"output"`
	CacheRead    float64 `json:"cache_read,omitempty"`
	CacheWrite5m float64 `json:"cache_write_5m,omitempty"`
	CacheWrite1h float64 `json:"cache_write_1h,omitempty"`
}

type Agent struct {
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"` // extra args for every launch
}

// Remote controls how sessions can be driven from elsewhere: Claude Code's
// own Remote Control, and the optimus web dashboard.
type Remote struct {
	// ClaudeRemoteControl starts every Claude session optimus launches with
	// --remote-control (default on).
	ClaudeRemoteControl *bool `json:"claude_remote_control,omitempty"`
	// WebAutostart starts the web dashboard whenever optimus launches or
	// resumes a session (default on).
	WebAutostart *bool `json:"web_autostart,omitempty"`
	// Addr is where the web dashboard listens (default 127.0.0.1:7777).
	Addr string `json:"addr,omitempty"`
}

// Notifications decides when and how optimus tells you an agent needs you.
type Notifications struct {
	Desktop  *bool  `json:"desktop,omitempty"`   // notify-send / macOS notification (default on)
	OnInput  *bool  `json:"on_input,omitempty"`  // agent waits for an answer (default on)
	OnFinish *bool  `json:"on_finish,omitempty"` // agent finished its turn (default on)
	Ntfy     string `json:"ntfy,omitempty"`      // ntfy topic URL for phone push, e.g. https://ntfy.sh/my-secret-topic
}

type Config struct {
	DefaultAgent string `json:"default_agent,omitempty"`
	// Attach: how to show an agent when optimus runs inside your own tmux:
	// "auto" (default: popup on tmux 3.2+), "popup" or "nested".
	Attach        string           `json:"attach,omitempty"`
	Notifications Notifications    `json:"notifications"`
	Remote        Remote           `json:"remote"`
	Budgets       Budgets          `json:"budgets"`
	BlockHours    int              `json:"block_hours,omitempty"`
	Pricing       map[string]Price `json:"pricing,omitempty"` // keyed by model-id prefix
	Agents        map[string]Agent `json:"agents,omitempty"`
	HandoffTokens int              `json:"handoff_max_tokens,omitempty"`
	// SuggestPct: when an agent's quota window reaches this %, suggest
	// continuing its running sessions in another agent (default 85, -1 off).
	SuggestPct      int               `json:"handoff_suggest_pct,omitempty"`
	SuggestTo       map[string]string `json:"handoff_suggest_to,omitempty"` // e.g. {"claude":"codex"}
	SummarizeWith   string            `json:"summarize_with,omitempty"`     // agent used by `handoff --summarize`
	StatuslineChain string            `json:"statusline_chain,omitempty"`
}

func Default() Config {
	return Config{BlockHours: 5, HandoffTokens: 20000, SummarizeWith: "claude", DefaultAgent: "claude"}
}

func (c Config) ClaudeRemoteControl() bool {
	return c.Remote.ClaudeRemoteControl == nil || *c.Remote.ClaudeRemoteControl
}

func (c Config) WebAutostart() bool {
	return c.Remote.WebAutostart == nil || *c.Remote.WebAutostart
}

func (c Config) WebAddr() string {
	if c.Remote.Addr != "" {
		return c.Remote.Addr
	}
	return "127.0.0.1:7777"
}

func (c Config) SuggestThreshold() float64 {
	if c.SuggestPct == 0 {
		return 85
	}
	return float64(c.SuggestPct)
}

func (c Config) Agent() string {
	if c.DefaultAgent != "" {
		return c.DefaultAgent
	}
	return "claude"
}

func Bool(b bool) *bool { return &b }

func on(b *bool) bool { return b == nil || *b }

func (n Notifications) DesktopOn() bool { return on(n.Desktop) }
func (n Notifications) InputOn() bool   { return on(n.OnInput) }
func (n Notifications) FinishOn() bool  { return on(n.OnFinish) }

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

func xdg(env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return filepath.Join(home(), fallback)
}

func Dir() string {
	if v := os.Getenv("OPTIMUS_HOME"); v != "" {
		return v
	}
	return filepath.Join(xdg("XDG_CONFIG_HOME", ".config"), "optimus")
}

func StateDir() string {
	if v := os.Getenv("OPTIMUS_HOME"); v != "" {
		return filepath.Join(v, "state")
	}
	return filepath.Join(xdg("XDG_STATE_HOME", ".local/state"), "optimus")
}

func CacheDir() string {
	if v := os.Getenv("OPTIMUS_HOME"); v != "" {
		return filepath.Join(v, "cache")
	}
	return filepath.Join(xdg("XDG_CACHE_HOME", ".cache"), "optimus")
}

// DataDir holds data optimus creates for you, such as agent worktrees.
func DataDir() string {
	if v := os.Getenv("OPTIMUS_HOME"); v != "" {
		return filepath.Join(v, "data")
	}
	return filepath.Join(xdg("XDG_DATA_HOME", ".local/share"), "optimus")
}

func Path() string          { return filepath.Join(Dir(), "config.json") }
func WebTokenFile() string  { return filepath.Join(StateDir(), "web-token") }
func HandoffDir() string    { return filepath.Join(StateDir(), "handoffs") }
func RateLimitFile() string { return filepath.Join(StateDir(), "ratelimits.json") }

// Home returns the user's home directory (overridable for tests).
func Home() string {
	if v := os.Getenv("OPTIMUS_AGENT_HOME"); v != "" {
		return v
	}
	return home()
}

func Load() (Config, error) {
	c := Default()
	b, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Default(), err
	}
	if c.BlockHours <= 0 {
		c.BlockHours = 5
	}
	if c.HandoffTokens <= 0 {
		c.HandoffTokens = 20000
	}
	return c, nil
}

func Save(c Config) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(Path(), append(b, '\n'), 0o644)
}

// WebToken returns the web dashboard's access token, creating it on first use.
func WebToken() (string, error) {
	if b, err := os.ReadFile(WebTokenFile()); err == nil && len(b) >= 32 {
		return string(b[:32]), nil
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(buf)
	if err := os.MkdirAll(StateDir(), 0o700); err != nil {
		return "", err
	}
	return tok, os.WriteFile(WebTokenFile(), []byte(tok), 0o600)
}

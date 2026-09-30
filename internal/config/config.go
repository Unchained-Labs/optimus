// Package config resolves optimus paths and the user's config file.
package config

import (
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

type Config struct {
	Budgets         Budgets          `json:"budgets"`
	BlockHours      int              `json:"block_hours,omitempty"`
	Pricing         map[string]Price `json:"pricing,omitempty"` // keyed by model-id prefix
	Agents          map[string]Agent `json:"agents,omitempty"`
	HandoffTokens   int              `json:"handoff_max_tokens,omitempty"`
	SummarizeWith   string           `json:"summarize_with,omitempty"` // agent used by `handoff --summarize`
	StatuslineChain string           `json:"statusline_chain,omitempty"`
}

func Default() Config {
	return Config{BlockHours: 5, HandoffTokens: 20000, SummarizeWith: "claude"}
}

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

func Path() string          { return filepath.Join(Dir(), "config.json") }
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

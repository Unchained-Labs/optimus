package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Unchained-Labs/optimus/internal/config"
)

// settable maps `optimus config set` keys to setters.
var settable = map[string]func(c *config.Config, v string) error{
	"budgets.daily_usd":            floatSetter(func(c *config.Config) *float64 { return &c.Budgets.DailyUSD }),
	"budgets.weekly_usd":           floatSetter(func(c *config.Config) *float64 { return &c.Budgets.WeeklyUSD }),
	"budgets.monthly_usd":          floatSetter(func(c *config.Config) *float64 { return &c.Budgets.MonthlyUSD }),
	"budgets.block_usd":            floatSetter(func(c *config.Config) *float64 { return &c.Budgets.BlockUSD }),
	"block_hours":                  intSetter(func(c *config.Config) *int { return &c.BlockHours }),
	"handoff_max_tokens":           intSetter(func(c *config.Config) *int { return &c.HandoffTokens }),
	"summarize_with":               func(c *config.Config, v string) error { c.SummarizeWith = v; return nil },
	"statusline_chain":             func(c *config.Config, v string) error { c.StatuslineChain = v; return nil },
	"default_agent":                func(c *config.Config, v string) error { c.DefaultAgent = v; return nil },
	"remote.addr":                  func(c *config.Config, v string) error { c.Remote.Addr = v; return nil },
	"remote.claude_remote_control": boolSetter(func(c *config.Config) **bool { return &c.Remote.ClaudeRemoteControl }),
	"remote.web_autostart":         boolSetter(func(c *config.Config) **bool { return &c.Remote.WebAutostart }),
	"budgets.session_usd":          floatSetter(func(c *config.Config) *float64 { return &c.Budgets.SessionUSD }),
	"notifications.desktop":        boolSetter(func(c *config.Config) **bool { return &c.Notifications.Desktop }),
	"notifications.on_input":       boolSetter(func(c *config.Config) **bool { return &c.Notifications.OnInput }),
	"notifications.on_finish":      boolSetter(func(c *config.Config) **bool { return &c.Notifications.OnFinish }),
	"notifications.ntfy":           func(c *config.Config, v string) error { c.Notifications.Ntfy = v; return nil },
	"handoff_suggest_pct": func(c *config.Config, v string) error {
		n, err := strconv.Atoi(v)
		if err != nil || n == 0 || n > 100 {
			return fmt.Errorf("%q: use 1–100, or -1 to turn suggestions off", v)
		}
		c.SuggestPct = n
		return nil
	},
	"attach": func(c *config.Config, v string) error {
		if v != "auto" && v != "popup" && v != "nested" {
			return fmt.Errorf("%q: use auto, popup or nested", v)
		}
		c.Attach = v
		return nil
	},
}

func settableKeys() []string {
	var ks []string
	for k := range settable {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func floatSetter(field func(*config.Config) *float64) func(*config.Config, string) error {
	return func(c *config.Config, v string) error {
		f, err := strconv.ParseFloat(strings.TrimPrefix(v, "$"), 64)
		if err != nil || f < 0 {
			return fmt.Errorf("%q is not a non-negative number", v)
		}
		*field(c) = f
		return nil
	}
}

func boolSetter(field func(*config.Config) **bool) func(*config.Config, string) error {
	return func(c *config.Config, v string) error {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("%q is not true/false", v)
		}
		*field(c) = config.Bool(b)
		return nil
	}
}

func intSetter(field func(*config.Config) *int) func(*config.Config, string) error {
	return func(c *config.Config, v string) error {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return fmt.Errorf("%q is not a positive integer", v)
		}
		*field(c) = n
		return nil
	}
}

func configSet(cfg config.Config, key, value string) error {
	set, ok := settable[key]
	if !ok {
		return fmt.Errorf("unknown key %q (keys: %s)", key, strings.Join(settableKeys(), ", "))
	}
	if err := set(&cfg, value); err != nil {
		return err
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Printf("%s = %s\n", key, value)
	return nil
}

func claudeSettingsPath() string {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		dir = filepath.Join(config.Home(), ".claude")
	}
	return filepath.Join(dir, "settings.json")
}

// installStatusline points Claude Code's statusLine at `optimus statusline`.
// An existing status line command is kept by chaining it through optimus,
// and the original settings file is backed up first.
func installStatusline(cfg config.Config) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if abs, err := filepath.EvalSymlinks(self); err == nil {
		self = abs
	}
	want := self + " statusline"

	path := claudeSettingsPath()
	settings := map[string]any{}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	default:
		if err := json.Unmarshal(raw, &settings); err != nil {
			return fmt.Errorf("%s is not valid JSON (%v); not touching it", path, err)
		}
	}

	if sl, ok := settings["statusLine"].(map[string]any); ok {
		cur, _ := sl["command"].(string)
		if cur == want {
			fmt.Println("already installed:", path)
			return nil
		}
		if cur != "" && !strings.HasSuffix(strings.TrimSpace(cur), "optimus statusline") {
			cfg.StatuslineChain = cur
			if err := config.Save(cfg); err != nil {
				return err
			}
			fmt.Printf("kept your existing status line (%s) by chaining it through optimus\n", cur)
		}
	}

	if len(raw) > 0 {
		backup := path + ".bak-optimus"
		if err := os.WriteFile(backup, raw, 0o600); err != nil {
			return err
		}
		fmt.Println("backed up", path, "→", backup)
	}
	settings["statusLine"] = map[string]any{"type": "command", "command": want}
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Println("Claude Code status line →", want)
	return nil
}

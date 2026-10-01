package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wardn/optimus/internal/config"
)

func TestInstallStatuslineKeepsExisting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPTIMUS_HOME", filepath.Join(dir, "optimus"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(dir, "claude"))
	settings := filepath.Join(dir, "claude", "settings.json")
	os.MkdirAll(filepath.Dir(settings), 0o755)
	os.WriteFile(settings, []byte(`{"model":"opus","statusLine":{"type":"command","command":"my-line.sh"}}`), 0o644)

	if err := installStatusline(config.Default()); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	b, _ := os.ReadFile(settings)
	json.Unmarshal(b, &got)
	if got["model"] != "opus" {
		t.Errorf("other settings lost: %s", b)
	}
	cmd := got["statusLine"].(map[string]any)["command"].(string)
	if filepath.Base(cmd) == "my-line.sh" || len(cmd) < len(" statusline") || cmd[len(cmd)-len(" statusline"):] != " statusline" {
		t.Errorf("statusLine command = %q", cmd)
	}
	if bak, _ := os.ReadFile(settings + ".bak-optimus"); len(bak) == 0 {
		t.Error("no backup written")
	}
	cfg, _ := config.Load()
	if cfg.StatuslineChain != "my-line.sh" {
		t.Errorf("chain = %q", cfg.StatuslineChain)
	}
	// second run is a no-op and must not chain optimus to itself
	if err := installStatusline(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg2, _ := config.Load(); cfg2.StatuslineChain != "my-line.sh" {
		t.Errorf("chain changed on rerun: %q", cfg2.StatuslineChain)
	}
}

func TestConfigSet(t *testing.T) {
	t.Setenv("OPTIMUS_HOME", t.TempDir())
	if err := configSet(config.Default(), "budgets.daily_usd", "$42.5"); err != nil {
		t.Fatal(err)
	}
	if c, _ := config.Load(); c.Budgets.DailyUSD != 42.5 {
		t.Errorf("daily = %v", c.Budgets.DailyUSD)
	}
	if configSet(config.Default(), "budgets.daily_usd", "-1") == nil || configSet(config.Default(), "nope", "1") == nil {
		t.Error("bad input accepted")
	}
}

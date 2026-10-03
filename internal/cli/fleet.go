package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Unchained-Labs/optimus/internal/agentstate"
	"github.com/Unchained-Labs/optimus/internal/app"
	"github.com/Unchained-Labs/optimus/internal/config"
	"github.com/Unchained-Labs/optimus/internal/format"
	"github.com/Unchained-Labs/optimus/internal/mux"
	"github.com/Unchained-Labs/optimus/internal/usage"
)

// Fleet is a cheap summary of the running agents, meant for status lines
// that refresh often: it reads recorded states (set by the notifier and the
// agents' hooks) instead of capturing screens, and caches today's spend.
type Fleet struct {
	Agents  int     `json:"agents"`
	Waiting int     `json:"waiting"`
	Busy    int     `json:"busy"`
	Today   float64 `json:"today_usd"`
}

func fleetSummary(a *app.App) Fleet {
	var f Fleet
	ws, _ := mux.List()
	for _, w := range ws {
		if w.Dead {
			continue
		}
		f.Agents++
		st := mux.State(w.State)
		if r, ok := agentstate.Read(w.Pane); ok && r.Full && !r.At.Before(w.Created) {
			st = r.State
		}
		switch st {
		case mux.StateWaiting:
			f.Waiting++
		case mux.StateBusy:
			f.Busy++
		}
	}
	f.Today = todaySpend(a)
	return f
}

type spendCache struct {
	At    time.Time `json:"at"`
	Day   string    `json:"day"`
	Today float64   `json:"today"`
}

// todaySpend caches today's cost for a minute: indexing every session on each
// status line refresh would be wasteful.
func todaySpend(a *app.App) float64 {
	path := filepath.Join(config.CacheDir(), "today.json")
	now := time.Now()
	var c spendCache
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &c) == nil &&
		c.Day == now.Format("2006-01-02") && now.Sub(c.At) < time.Minute {
		return c.Today
	}
	idx := a.Index()
	c = spendCache{At: now, Day: now.Format("2006-01-02"), Today: usage.Total(idx.Sessions, a.Prices, usage.Filter{Since: usage.StartOfDay(now)}).Cost}
	if b, err := json.Marshal(c); err == nil {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, b, 0o644)
	}
	return c.Today
}

func (f Fleet) String() string {
	var parts []string
	if f.Agents > 0 {
		s := fmt.Sprintf("⧉ %d agent", f.Agents)
		if f.Agents > 1 {
			s += "s"
		}
		parts = append(parts, s)
	}
	if f.Waiting > 0 {
		parts = append(parts, fmt.Sprintf("◆ %d waiting", f.Waiting))
	}
	if f.Busy > 0 {
		parts = append(parts, fmt.Sprintf("● %d busy", f.Busy))
	}
	parts = append(parts, format.Money(f.Today)+" today")
	return strings.Join(parts, " · ")
}

// cmdFleet prints the summary: plain, --json, or --tmux (for the optimus
// multiplexer's status bar).
func cmdFleet(a *app.App, args []string) error {
	fs := flag.NewFlagSet("fleet", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "json output")
	tmuxFmt := fs.Bool("tmux", false, "tmux status-bar format")
	parse(fs, args)
	f := fleetSummary(a)
	switch {
	case *asJSON:
		return printJSON(f)
	case *tmuxFmt:
		s := f.String()
		if f.Waiting > 0 {
			s = strings.Replace(s, fmt.Sprintf("◆ %d waiting", f.Waiting), fmt.Sprintf("#[fg=#f38ba8,bold]◆ %d waiting#[fg=#a6adc8]", f.Waiting), 1)
		}
		fmt.Print(s)
	default:
		fmt.Println(f.String())
	}
	return nil
}

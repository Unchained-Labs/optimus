// Package ratelimits tracks subscription quota windows ("how much is left").
//
// Claude Code hands rate-limit data to status line commands, so `optimus
// statusline` records each snapshot it receives. Codex writes rate limits into
// its rollouts, which optimus reads directly.
package ratelimits

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Unchained-Labs/optimus/internal/config"
	"github.com/Unchained-Labs/optimus/internal/providers"
)

type Window struct {
	Agent    string    `json:"agent"`
	Name     string    `json:"name"`
	UsedPct  float64   `json:"used_pct"`
	ResetsAt time.Time `json:"resets_at,omitempty"`
	Captured time.Time `json:"captured"`
	// Spend-limit style windows carry dollars instead of a percentage.
	UsedUSD  float64 `json:"used_usd,omitempty"`
	LimitUSD float64 `json:"limit_usd,omitempty"`
}

// Stale reports whether the window has reset since it was captured, so the
// percentage no longer applies.
func (w Window) Stale(now time.Time) bool {
	return !w.ResetsAt.IsZero() && now.After(w.ResetsAt)
}

type snapshot struct {
	Captured   time.Time       `json:"captured"`
	RateLimits json.RawMessage `json:"rate_limits"`
}

// RecordClaude stores the rate_limits object of a status line payload.
func RecordClaude(payload []byte) error {
	var v struct {
		RateLimits json.RawMessage `json:"rate_limits"`
	}
	if json.Unmarshal(payload, &v) != nil || len(v.RateLimits) == 0 || string(v.RateLimits) == "null" {
		return nil
	}
	b, _ := json.Marshal(snapshot{Captured: time.Now(), RateLimits: v.RateLimits})
	if err := os.MkdirAll(filepath.Dir(config.RateLimitFile()), 0o755); err != nil {
		return err
	}
	tmp := config.RateLimitFile() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, config.RateLimitFile())
}

// Load returns every known quota window across agents.
func Load() []Window {
	var out []Window
	if b, err := os.ReadFile(config.RateLimitFile()); err == nil {
		var s snapshot
		if json.Unmarshal(b, &s) == nil {
			out = append(out, parse("claude", s.RateLimits, s.Captured)...)
		}
	}
	if raw, at := (providers.Codex{}).LatestRateLimits(); len(raw) > 0 {
		out = append(out, parse("codex", raw, at)...)
	}
	return out
}

func parse(agent string, raw json.RawMessage, captured time.Time) []Window {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []Window
	for _, k := range keys {
		var o map[string]any
		if json.Unmarshal(m[k], &o) != nil || o == nil {
			continue
		}
		w := Window{Agent: agent, Name: prettyName(k, o), Captured: captured}
		pct, hasPct := num(o, "used_percentage", "used_percent", "utilization", "percent_used")
		usd, hasUSD := num(o, "used_usd")
		lim, _ := num(o, "limit_usd")
		if !hasPct && !hasUSD {
			continue
		}
		if hasPct && pct <= 1 && strings.Contains(k, "utilization") {
			pct *= 100
		}
		w.UsedPct = pct
		if hasUSD {
			w.UsedUSD, w.LimitUSD = usd, lim
			if !hasPct && lim > 0 {
				w.UsedPct = usd / lim * 100
			}
		}
		w.ResetsAt = resetTime(o, captured)
		out = append(out, w)
	}
	return out
}

func prettyName(k string, o map[string]any) string {
	switch k {
	case "five_hour":
		return "5h"
	case "seven_day":
		return "7d"
	case "seven_day_opus":
		return "7d opus"
	case "seven_day_sonnet":
		return "7d sonnet"
	case "spend_limit":
		if p, ok := o["period"].(string); ok && p != "" {
			return "spend/" + p
		}
		return "spend"
	}
	if mins, ok := num(o, "window_minutes"); ok && mins > 0 {
		switch {
		case int(mins)%(60*24) == 0:
			return strconv.Itoa(int(mins)/(60*24)) + "d"
		case int(mins)%60 == 0:
			return strconv.Itoa(int(mins)/60) + "h"
		}
		return strconv.Itoa(int(mins)) + "m"
	}
	return strings.ReplaceAll(k, "_", " ")
}

func num(o map[string]any, keys ...string) (float64, bool) {
	for _, k := range keys {
		switch v := o[k].(type) {
		case float64:
			return v, true
		case string:
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return f, true
			}
		}
	}
	return 0, false
}

func resetTime(o map[string]any, captured time.Time) time.Time {
	for _, k := range []string{"resets_at", "reset_at", "resetsAt"} {
		switch v := o[k].(type) {
		case float64:
			if v > 1e12 {
				return time.UnixMilli(int64(v))
			}
			return time.Unix(int64(v), 0)
		case string:
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return t
			}
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return time.Unix(int64(f), 0)
			}
		}
	}
	if s, ok := num(o, "resets_in_seconds", "reset_after_seconds"); ok && !captured.IsZero() {
		return captured.Add(time.Duration(s) * time.Second)
	}
	return time.Time{}
}

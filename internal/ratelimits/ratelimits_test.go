package ratelimits

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseClaudeAndCodex(t *testing.T) {
	now := time.Unix(1790000000, 0)
	claude := parse("claude", json.RawMessage(`{"five_hour":{"used_percentage":42.5,"resets_at":1790003600},"seven_day":{"used_percentage":10,"resets_at":"2026-10-03T00:00:00Z"},"spend_limit":{"used_usd":271.4,"limit_usd":500,"period":"month"}}`), now)
	if len(claude) != 3 {
		t.Fatalf("got %+v", claude)
	}
	byName := map[string]Window{}
	for _, w := range claude {
		byName[w.Name] = w
	}
	if w := byName["5h"]; w.UsedPct != 42.5 || !w.ResetsAt.Equal(time.Unix(1790003600, 0)) {
		t.Errorf("5h: %+v", w)
	}
	if w := byName["7d"]; w.ResetsAt.IsZero() {
		t.Errorf("7d: %+v", w)
	}
	if w := byName["spend/month"]; w.LimitUSD != 500 || int(w.UsedPct) != 54 {
		t.Errorf("spend: %+v", w)
	}
	codex := parse("codex", json.RawMessage(`{"primary":{"used_percent":12.5,"window_minutes":300,"resets_in_seconds":600},"secondary":{"used_percent":3,"window_minutes":10080}}`), now)
	if len(codex) != 2 || codex[0].Name != "5h" || !codex[0].ResetsAt.Equal(now.Add(10*time.Minute)) || codex[1].Name != "7d" {
		t.Errorf("codex: %+v", codex)
	}
	if !(Window{ResetsAt: now}).Stale(now.Add(time.Second)) {
		t.Error("window past its reset should be stale")
	}
}

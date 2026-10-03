package app

import (
	"testing"
	"time"

	"github.com/Unchained-Labs/optimus/internal/config"
	"github.com/Unchained-Labs/optimus/internal/index"
	"github.com/Unchained-Labs/optimus/internal/model"
	"github.com/Unchained-Labs/optimus/internal/mux"
	"github.com/Unchained-Labs/optimus/internal/ratelimits"
)

func TestSuggestions(t *testing.T) {
	a := &App{Cfg: config.Default()}
	a.Cfg.Agents = map[string]config.Agent{"codex": {Command: "sh"}} // "installed"
	now := time.Now()
	idx := &index.Index{Sessions: []*model.Session{{Agent: "claude", ID: "s1", Cwd: "/w"}}}
	ws := []mux.Window{{ID: "@1", Agent: "claude", Cwd: "/w", SessionID: "s1"}, {ID: "@2", Agent: "codex", Cwd: "/w"}}
	high := []ratelimits.Window{{Agent: "claude", Name: "5h", UsedPct: 91, ResetsAt: now.Add(time.Hour)}}

	got := a.Suggestions(idx, ws, high)
	if len(got) != 1 || got[0].Window.ID != "@1" || got[0].To != "codex" || got[0].Quota.UsedPct != 91 {
		t.Fatalf("got %+v", got)
	}
	low := []ratelimits.Window{{Agent: "claude", Name: "5h", UsedPct: 40, ResetsAt: now.Add(time.Hour)}}
	if s := a.Suggestions(idx, ws, low); len(s) != 0 {
		t.Errorf("below threshold: %+v", s)
	}
	stale := []ratelimits.Window{{Agent: "claude", Name: "5h", UsedPct: 99, ResetsAt: now.Add(-time.Minute)}}
	if s := a.Suggestions(idx, ws, stale); len(s) != 0 {
		t.Errorf("a window that already reset must not suggest: %+v", s)
	}
	a.Cfg.SuggestPct = -1
	if s := a.Suggestions(idx, ws, high); len(s) != 0 {
		t.Errorf("disabled: %+v", s)
	}
}

func TestSpendEvents(t *testing.T) {
	a := &App{Cfg: config.Default()}
	idx := &index.Index{Sessions: []*model.Session{
		{Agent: "claude", ID: "big", Cwd: "/a", Cost: 47},
		{Agent: "claude", ID: "small", Cwd: "/b", Cost: 3},
	}}
	ws := []mux.Window{{ID: "@1", Name: "api", Agent: "claude", Cwd: "/a", SessionID: "big"}, {ID: "@2", Name: "web", Agent: "claude", Cwd: "/b", SessionID: "small"}}
	ev := a.spendEvents(idx, ws, 20)
	if len(ev) != 1 || ev[0].Window != "@1" || ev[0].Key != "spend/big/2" || ev[0].Title != "api has cost $47.00" {
		t.Fatalf("events: %+v", ev)
	}
}

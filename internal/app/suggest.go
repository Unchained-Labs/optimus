package app

import (
	"fmt"
	"time"

	"github.com/Unchained-Labs/optimus/internal/index"
	"github.com/Unchained-Labs/optimus/internal/model"
	"github.com/Unchained-Labs/optimus/internal/mux"
	"github.com/Unchained-Labs/optimus/internal/notify"
	"github.com/Unchained-Labs/optimus/internal/providers"
	"github.com/Unchained-Labs/optimus/internal/ratelimits"
)

// Suggestion: an agent is close to its quota, so a running session could
// continue in another agent with a handoff.
type Suggestion struct {
	Window  mux.Window
	Session *model.Session
	From    string
	To      string
	Quota   ratelimits.Window
}

var defaultSuggestTo = map[string]string{"claude": "codex", "codex": "claude"}

// SuggestTarget is the agent a session of `from` should continue in.
func (a *App) SuggestTarget(from string) string {
	to := defaultSuggestTo[from]
	if v, ok := a.Cfg.SuggestTo[from]; ok {
		to = v
	}
	if to == "" || to == from {
		return ""
	}
	if p := providers.Get(to); p == nil || !providers.Installed(a.Cfg, p) {
		return ""
	}
	return to
}

// Suggestions lists running sessions whose agent has a quota window at or
// above the configured threshold.
func (a *App) Suggestions(idx *index.Index, ws []mux.Window, limits []ratelimits.Window) []Suggestion {
	pct := a.Cfg.SuggestThreshold()
	if pct < 0 {
		return nil
	}
	now := time.Now()
	worst := map[string]ratelimits.Window{}
	for _, l := range limits {
		if l.Stale(now) || l.LimitUSD > 0 || l.UsedPct < pct {
			continue
		}
		if cur, ok := worst[l.Agent]; !ok || l.UsedPct > cur.UsedPct {
			worst[l.Agent] = l
		}
	}
	if len(worst) == 0 {
		return nil
	}
	var out []Suggestion
	for _, w := range ws {
		q, ok := worst[w.Agent]
		if !ok || w.Dead {
			continue
		}
		to := a.SuggestTarget(w.Agent)
		s := idx.ForWindow(w.Agent, w.Cwd, w.SessionID, w.Created)
		if to == "" || s == nil {
			continue
		}
		out = append(out, Suggestion{Window: w, Session: s, From: w.Agent, To: to, Quota: q})
	}
	return out
}

// SuggestionAdvisories returns the notifier hook that announces quota-driven
// handoff suggestions (once per session per quota window).
func (a *App) SuggestionAdvisories() func() []notify.Event {
	return func() []notify.Event {
		ws, err := mux.List()
		if err != nil || len(ws) == 0 {
			return nil
		}
		var out []notify.Event
		for _, s := range a.Suggestions(a.Index(), ws, ratelimits.Load()) {
			out = append(out, notify.Event{
				Title:  fmt.Sprintf("%s %s quota at %.0f%%", s.From, s.Quota.Name, s.Quota.UsedPct),
				Body:   fmt.Sprintf("continue %s in %s? press C in optimus, or: optimus handoff %s --to %s", s.Window.Name, s.To, s.Session.ShortID(), s.To),
				Window: s.Window.ID,
				Key:    s.Window.ID + "/" + s.Quota.Name + "/" + s.Quota.ResetsAt.Format(time.RFC3339),
			})
		}
		return out
	}
}

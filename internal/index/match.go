package index

import (
	"time"

	"github.com/Unchained-Labs/optimus/internal/model"
)

// ForWindow finds the transcript of the agent running in a multiplexer window:
// by the session id optimus assigned (kept current by the agent's hooks), else
// a session of that agent in that directory active since the window started.
//
// A window that knows its session id never falls back: before its first
// message an agent's transcript doesn't exist yet, and guessing then picked
// another session in the same folder, so two windows showed one session.
//
// Agents that can't be given an id (Codex until its first turn reports one,
// opencode always) are guessed: the first session that started after the
// window did is the window's own. A session that started earlier belongs to
// another window or terminal, however recently it was active — resumed
// sessions are always tagged with their id.
func (idx *Index) ForWindow(agent, cwd, sessionID string, created time.Time) *model.Session {
	if sessionID != "" {
		return idx.Find(sessionID)
	}
	var hit *model.Session
	// @optimus_created has whole seconds and is set just after the spawn
	since := created.Add(-2 * time.Second)
	for _, s := range idx.Sessions { // newest first
		if s.Agent != agent || s.Cwd != cwd {
			continue
		}
		if created.IsZero() { // a window from before optimus recorded it
			return s
		}
		if !s.Start.Before(since) && (hit == nil || s.Start.Before(hit.Start)) {
			hit = s
		}
	}
	return hit
}

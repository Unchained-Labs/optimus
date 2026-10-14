package index

import (
	"time"

	"github.com/Unchained-Labs/optimus/internal/model"
)

// ForWindow finds the transcript of the agent running in a multiplexer window:
// by the session id optimus assigned (kept current by the agent's hooks), else
// the newest session of that agent in that directory that was active after
// the window started.
//
// A window that knows its session id never falls back: before its first
// message an agent's transcript doesn't exist yet, and guessing then picked
// another session in the same folder, so two windows showed one session.
func (idx *Index) ForWindow(agent, cwd, sessionID string, created time.Time) *model.Session {
	if sessionID != "" {
		return idx.Find(sessionID)
	}
	for _, s := range idx.Sessions { // newest first
		if s.Agent == agent && s.Cwd == cwd && (created.IsZero() || !s.End.Before(created)) {
			return s
		}
	}
	return nil
}

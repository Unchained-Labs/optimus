package index

import (
	"time"

	"github.com/Unchained-Labs/optimus/internal/model"
)

// ForWindow finds the transcript of the agent running in a multiplexer window:
// by the session id optimus assigned, else the newest session of that agent in
// that directory that was active after the window started.
func (idx *Index) ForWindow(agent, cwd, sessionID string, created time.Time) *model.Session {
	if sessionID != "" {
		if s := idx.Find(sessionID); s != nil {
			return s
		}
	}
	for _, s := range idx.Sessions { // newest first
		if s.Agent == agent && s.Cwd == cwd && (created.IsZero() || !s.End.Before(created)) {
			return s
		}
	}
	return nil
}

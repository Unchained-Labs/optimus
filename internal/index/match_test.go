package index

import (
	"testing"
	"time"

	"github.com/Unchained-Labs/optimus/internal/model"
)

func TestForWindowNeverBorrowsAnotherSession(t *testing.T) {
	now := time.Now()
	idx := &Index{Sessions: []*model.Session{
		{Agent: "claude", ID: "11111111-older-window", Cwd: "/w", End: now},
	}}
	// a fresh window whose transcript isn't written yet
	if s := idx.ForWindow("claude", "/w", "22222222-new-window", now.Add(-time.Minute)); s != nil {
		t.Fatalf("borrowed %s for a window with its own id", s.ID)
	}
	if s := idx.ForWindow("claude", "/w", "11111111-older-window", now.Add(-time.Hour)); s == nil {
		t.Fatal("lost the window's own session")
	}
	// no id at all: the folder guess still applies
	if s := idx.ForWindow("claude", "/w", "", now.Add(-time.Hour)); s == nil {
		t.Fatal("no fallback without an id")
	}
}

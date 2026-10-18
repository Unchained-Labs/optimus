package index

import (
	"testing"
	"time"

	"github.com/Unchained-Labs/optimus/internal/model"
)

func TestForWindowNeverBorrowsAnotherSession(t *testing.T) {
	now := time.Now()
	idx := &Index{Sessions: []*model.Session{
		{Agent: "claude", ID: "11111111-older-window", Cwd: "/w", Start: now.Add(-30 * time.Minute), End: now},
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

// Codex and opencode windows have no id until the agent reports one: two
// windows in one folder must each get their own session, not the newest.
func TestForWindowGuessesTheWindowsOwnSession(t *testing.T) {
	t0 := time.Now().Add(-time.Hour).Truncate(time.Second)
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	a := &model.Session{Agent: "codex", ID: "a", Cwd: "/w", Start: at(0).Add(time.Second), End: at(50)}
	b := &model.Session{Agent: "codex", ID: "b", Cwd: "/w", Start: at(5).Add(time.Second), End: at(55)}
	old := &model.Session{Agent: "codex", ID: "old", Cwd: "/w", Start: at(-600), End: at(30)}
	idx := &Index{Sessions: []*model.Session{b, a, old}} // newest first
	cases := []struct {
		name    string
		created time.Time
		want    string
	}{
		{"first window", at(0), "a"},
		{"second window", at(5), "b"},
		{"fresh window beside busy ones", at(20), ""},
		{"window newer than every session", at(58), ""},
		{"window without a creation time", time.Time{}, "b"},
	}
	for _, c := range cases {
		got := ""
		if s := idx.ForWindow("codex", "/w", "", c.created); s != nil {
			got = s.ID
		}
		if got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

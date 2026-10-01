package handoff

import (
	"strings"
	"testing"
	"time"

	"github.com/Unchained-Labs/optimus/internal/model"
)

func TestBuild(t *testing.T) {
	s := &model.Session{Agent: "claude", ID: "abc", Cwd: "/w", Title: "Fix login", Messages: 3}
	t0 := time.Now()
	msgs := []model.Message{
		{Role: "user", Text: "/model", Time: t0},
		{Role: "user", Text: "fix the login bug", Time: t0},
		{Role: "assistant", Text: "Looking.", Tools: []model.ToolCall{{Name: "Read", Target: "/w/auth.go"}}},
		{Role: "assistant", Tools: []model.ToolCall{{Name: "Edit", Target: "/w/login.go", Edit: true}, {Name: "Bash", Target: "go test ./..."}}},
		{Role: "user", Text: "now add a test"},
		{Role: "assistant", Text: strings.Repeat("long answer ", 5000)},
	}
	doc := Build(s, msgs, Options{MaxTokens: 3000, Note: "be brief"})
	for _, want := range []string{"# Handoff: Fix login", "## Original request\n\n> fix the login bug", "`/w/login.go`", "`/w/auth.go`", "go test ./...", "> now add a test", "be brief", "chars truncated"} {
		if !strings.Contains(doc, want) {
			t.Errorf("missing %q", want)
		}
	}
	if len(doc) > 3000*charsPerToken+500 {
		t.Errorf("doc over budget: %d chars", len(doc))
	}
	if strings.Count(doc, "**Assistant:**") != 2 {
		t.Errorf("consecutive assistant turns should merge:\n%s", doc)
	}
}

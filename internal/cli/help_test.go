package cli

import (
	"strings"
	"testing"
)

func TestCommandHelp(t *testing.T) {
	cases := map[string][]string{
		"handoff":  {"optimus handoff <session>", "--into WINDOW", "--edit"},
		"takeover": {"optimus takeover", "--force"},
		"config":   {"optimus config [init", "optimus config set", "optimus config statusline"},
		"codex":    {"optimus claude|codex"},
		"sessions": {"optimus ls "},
	}
	for cmd, want := range cases {
		h := commandHelp(cmd)
		for _, w := range want {
			if !strings.Contains(h, w) {
				t.Errorf("commandHelp(%q) lacks %q:\n%s", cmd, w, h)
			}
		}
	}
	if h := commandHelp("handoff"); strings.Contains(h, "optimus usage") {
		t.Errorf("handoff help runs into the next section:\n%s", h)
	}
	for _, cmd := range []string{"", "nope", "hook"} {
		if h := commandHelp(cmd); h != "" {
			t.Errorf("commandHelp(%q) = %q, want empty", cmd, h)
		}
	}
}

// Every alias must lead to a documented command, or `optimus <alias> -h`
// prints nothing useful.
func TestAliasesAreDocumented(t *testing.T) {
	for alias, cmd := range aliases {
		if commandHelp(cmd) == "" {
			t.Errorf("alias %q → %q, which Usage doesn't document", alias, cmd)
		}
	}
}

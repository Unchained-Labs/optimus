package mux

import (
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	cases := map[string]State{
		"● Update(src/a.ts)\n✳ Implementing… (3s · esc to interrupt)":              StateBusy,
		"Allow command?  pnpm test\n  › 1. Yes   2. Always   3. No":                StateWaiting,
		" Do you want to make this edit to login.ts?\n ❯ 1. Yes\n   2. No":         StateWaiting,
		"● Done. All tests pass.\n\n> \n  ⏵⏵ auto mode on · bypass permissions on": StateIdle,
	}
	for screen, want := range cases {
		if got := Detect(Window{}, screen); got != want {
			t.Errorf("%q: got %s want %s", screen, got, want)
		}
	}
	if Detect(Window{Dead: true}, "") != StateExited {
		t.Error("dead pane should be exited")
	}
}

func TestIndependent(t *testing.T) {
	got := independent([]string{"claude", "--resume", "x"})
	if got[0] != "env" || got[len(got)-3] != "claude" {
		t.Fatalf("%v", got)
	}
	joined := " " + strings.Join(got, " ") + " "
	for _, v := range []string{"CLAUDECODE", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_SESSION_ID"} {
		if !strings.Contains(joined, " -u "+v+" ") {
			t.Errorf("%s not removed", v)
		}
	}
	if strings.Contains(joined, "CLAUDE_CONFIG_DIR") {
		t.Error("user configuration must pass through")
	}
}

func TestScoped(t *testing.T) {
	defer func(f func() bool) { scopesAvailable = f }(scopesAvailable)
	scopesAvailable = func() bool { return false }
	if got := scoped([]string{"claude"}); len(got) != 1 || got[0] != "claude" {
		t.Fatalf("no systemd: got %v", got)
	}
	scopesAvailable = func() bool { return true }
	got := scoped([]string{"claude", "--resume", "x"})
	if got[0] != "systemd-run" || got[len(got)-4] != "--" || got[len(got)-3] != "claude" {
		t.Fatalf("got %v", got)
	}
	if a, b := scoped([]string{"x"}), scoped([]string{"x"}); strings.Join(a, " ") == strings.Join(b, " ") {
		t.Error("each agent needs its own unit name")
	}
}

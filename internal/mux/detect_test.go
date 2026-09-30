package mux

import "testing"

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

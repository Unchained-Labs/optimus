package agentstate

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Unchained-Labs/optimus/internal/mux"
)

func TestFromClaude(t *testing.T) {
	cases := []struct {
		payload string
		state   mux.State
		msg     string
		ok      bool
	}{
		{`{"hook_event_name":"UserPromptSubmit","session_id":"s1"}`, mux.StateBusy, "", true},
		{`{"hook_event_name":"PreToolUse","tool_name":"Bash"}`, mux.StateBusy, "", true},
		{`{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"npm run migrate"}}`, mux.StateWaiting, "wants to use Bash: npm run migrate", true},
		{`{"hook_event_name":"PermissionRequest","tool_name":"Write","cwd":"/w/app","tool_input":{"file_path":"/w/app/hello.txt"}}`, mux.StateWaiting, "wants to use Write: hello.txt", true},
		{`{"hook_event_name":"Notification","message":"Claude needs your permission to use Bash"}`, mux.StateWaiting, "Claude needs your permission to use Bash", true},
		{`{"hook_event_name":"Notification","message":"Claude is waiting for your input"}`, mux.StateIdle, "Claude is waiting for your input", true},
		{`{"hook_event_name":"Stop"}`, mux.StateIdle, "finished", true},
		{`{"hook_event_name":"SessionEnd"}`, mux.StateExited, "", true},
		{`{"hook_event_name":"PreCompact"}`, "", "", false},
		{`not json`, "", "", false},
	}
	for _, c := range cases {
		r, ok := FromClaude([]byte(c.payload))
		if ok != c.ok || r.State != c.state || r.Message != c.msg {
			t.Errorf("%s → %v %q %q", c.payload, ok, r.State, r.Message)
		}
		if ok && !r.Full {
			t.Errorf("claude records must be authoritative")
		}
	}
}

func TestFromCodex(t *testing.T) {
	r, ok := FromCodex([]byte(`{"type":"agent-turn-complete","last-assistant-message":"All 12 tests pass.\nDetails…"}`))
	if !ok || r.State != mux.StateIdle || r.Message != "finished: All 12 tests pass." || r.Full {
		t.Errorf("%+v %v", r, ok)
	}
}

func TestClaudeSettingsShape(t *testing.T) {
	var v struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Type, Command string
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(ClaudeSettings("/opt/it's/optimus")), &v); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []string{"UserPromptSubmit", "PreToolUse", "PermissionRequest", "Notification", "Stop", "SessionEnd"} {
		h := v.Hooks[ev]
		if len(h) != 1 || len(h[0].Hooks) != 1 || h[0].Hooks[0].Type != "command" {
			t.Fatalf("%s: %+v", ev, h)
		}
		if got := h[0].Hooks[0].Command; got != `'/opt/it'\''s/optimus' hook claude` {
			t.Errorf("command not shell-quoted: %s", got)
		}
	}
	if v.Hooks["PreToolUse"][0].Matcher != "*" {
		t.Error("tool hooks need a matcher")
	}
}

func TestResolve(t *testing.T) {
	t.Setenv("OPTIMUS_HOME", t.TempDir())
	w := mux.Window{ID: "@1", Pane: "%7", Created: time.Now().Add(-time.Hour)}
	idleScreen := "> \n  ? for shortcuts"
	busyScreen := "✻ Working… (3s · esc to interrupt)"

	if got := Resolve(w, busyScreen); got.State != mux.StateBusy || got.Source != "screen" {
		t.Errorf("no record should fall back to the screen: %+v", got)
	}
	Write(Record{Pane: "%7", Agent: "claude", State: mux.StateWaiting, Message: "wants to use Bash: ls", Full: true})
	if got := Resolve(w, idleScreen); got.State != mux.StateWaiting || got.Message != "wants to use Bash: ls" || got.Source != "hook" {
		t.Errorf("hook should win: %+v", got)
	}
	// a stale "busy" with a calm screen (interrupted turn) yields to the screen
	Write(Record{Pane: "%7", Agent: "claude", State: mux.StateBusy, Full: true, At: time.Now().Add(-2 * time.Minute)})
	if got := Resolve(w, idleScreen); got.State != mux.StateIdle {
		t.Errorf("stale busy: %+v", got)
	}
	// a record from before this window existed (pane id reuse) is ignored
	Write(Record{Pane: "%7", State: mux.StateWaiting, Full: true, At: time.Now().Add(-2 * time.Hour)})
	if got := Resolve(w, idleScreen); got.Source != "screen" {
		t.Errorf("old record should be ignored: %+v", got)
	}
	// partial reporter (codex): screen activity wins, calm screen shows the hook
	Write(Record{Pane: "%7", Agent: "codex", State: mux.StateIdle, Message: "finished: done"})
	if got := Resolve(w, busyScreen); got.State != mux.StateBusy {
		t.Errorf("partial + busy screen: %+v", got)
	}
	if got := Resolve(w, idleScreen); got.Message != "finished: done" {
		t.Errorf("partial + idle screen: %+v", got)
	}
	Forget(map[string]bool{"%9": true})
	if _, ok := Read("%7"); ok {
		t.Error("Forget should drop records of dead panes")
	}
}

func TestPrompt(t *testing.T) {
	got := Prompt("a\nb\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n\n\n", 3)
	if strings.Join(got, "|") != " Do you want to proceed?| ❯ 1. Yes|   2. No" {
		t.Errorf("%q", got)
	}
}

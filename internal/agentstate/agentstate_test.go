package agentstate

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
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
		{`{"hook_event_name":"SessionEnd","reason":"prompt_input_exit"}`, mux.StateExited, "", true},
		{`{"hook_event_name":"SessionEnd","reason":"clear","session_id":"old"}`, "", "", false},
		{`{"hook_event_name":"SessionStart","source":"clear","session_id":"new"}`, mux.StateIdle, "", true},
		{`{"hook_event_name":"SessionStart","source":"resume","session_id":"other"}`, mux.StateIdle, "", true},
		{`{"hook_event_name":"SessionStart","source":"compact","session_id":"same"}`, "", "", false},
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
	for _, ev := range []string{"UserPromptSubmit", "PreToolUse", "PermissionRequest", "Notification", "Stop", "SessionStart", "SessionEnd"} {
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
	// /clear ends the session but not the process: a live pane isn't exited
	Write(Record{Pane: "%7", Agent: "claude", State: mux.StateExited, Full: true})
	if got := Resolve(w, idleScreen); got.State != mux.StateIdle {
		t.Errorf("a live pane reported exited: %+v", got)
	}
	if got := Resolve(mux.Window{ID: "@1", Pane: "%7", Dead: true}, idleScreen); got.State != mux.StateExited {
		t.Errorf("a dead pane is exited: %+v", got)
	}
	Forget(map[string]bool{"%9": true}, time.Now())
	if _, ok := Read("%7"); ok {
		t.Error("Forget should drop records of dead panes")
	}
}

func TestForgetKeepsRecordsNewerThanTheListing(t *testing.T) {
	t.Setenv("OPTIMUS_HOME", t.TempDir())
	listed := time.Now()
	time.Sleep(10 * time.Millisecond)
	// the first hook of a window opened after the panes were listed
	Write(Record{Pane: "%3", Agent: "claude", State: mux.StateBusy, Full: true})
	Forget(map[string]bool{}, listed)
	if _, ok := Read("%3"); !ok {
		t.Fatal("a record written after the listing was dropped")
	}
	Forget(map[string]bool{}, time.Now().Add(time.Second))
	if _, ok := Read("%3"); ok {
		t.Error("the next round should drop it")
	}
}

func TestConcurrentWritesNeverTear(t *testing.T) {
	t.Setenv("OPTIMUS_HOME", t.TempDir())
	// parallel tool calls run their hooks at the same time
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if err := Write(Record{Pane: "%4", Agent: "claude", State: mux.StateBusy, Message: strings.Repeat("x", 2000+i), Full: true}); err != nil {
					t.Error(err)
				}
				if _, ok := Read("%4"); !ok {
					t.Error("read a torn record")
				}
			}
		}(i)
	}
	wg.Wait()
	if left, _ := filepath.Glob(filepath.Join(dir(), ".rec-*")); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func TestPrompt(t *testing.T) {
	got := Prompt("a\nb\n Do you want to proceed?\n ❯ 1. Yes\n   2. No\n\n\n", 3)
	if strings.Join(got, "|") != " Do you want to proceed?| ❯ 1. Yes|   2. No" {
		t.Errorf("%q", got)
	}
}

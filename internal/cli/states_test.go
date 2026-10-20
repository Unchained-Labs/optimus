package cli

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/Unchained-Labs/optimus/internal/agentstate"
	"github.com/Unchained-Labs/optimus/internal/mux"
)

// /clear in a window: Claude ends the old session and starts a new one in the
// same process. The window must follow the new session at once and must not
// read as exited.
func TestHookFollowsClear(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	tmp := t.TempDir()
	t.Setenv("TMUX_TMPDIR", sockDir(t))
	t.Setenv("OPTIMUS_HOME", tmp)
	t.Setenv("OPTIMUS_NO_SCOPE", "1")
	prev := mux.Socket
	mux.Socket = fmt.Sprintf("optimus-hook-%d", time.Now().UnixNano())
	defer func() { _ = exec.Command("tmux", "-L", mux.Socket, "kill-server").Run(); mux.Socket = prev }()
	if _, err := mux.Spawn("c", "claude", tmp, "old", []string{"sleep", "30"}); err != nil {
		t.Fatal(err)
	}
	ws, _ := mux.List()
	if len(ws) != 1 {
		t.Fatalf("windows: %+v", ws)
	}
	t.Setenv("TMUX_PANE", ws[0].Pane)
	hook := func(payload string) {
		r, w, _ := os.Pipe()
		w.WriteString(payload)
		w.Close()
		stdin := os.Stdin
		os.Stdin = r
		defer func() { os.Stdin = stdin }()
		if err := cmdHook([]string{"claude"}); err != nil {
			t.Fatal(err)
		}
	}
	hook(`{"hook_event_name":"UserPromptSubmit","session_id":"old"}`)
	hook(`{"hook_event_name":"Stop","session_id":"old"}`)
	hook(`{"hook_event_name":"SessionEnd","reason":"clear","session_id":"old"}`)
	hook(`{"hook_event_name":"SessionStart","source":"clear","session_id":"new"}`)
	ws, _ = mux.List()
	if ws[0].SessionID != "new" {
		t.Errorf("window still tagged %q after /clear", ws[0].SessionID)
	}
	if st := agentstate.Resolve(ws[0], "> ").State; st != mux.StateIdle {
		t.Errorf("state after /clear: %s", st)
	}
}

// sockDir is a short folder for the test's tmux socket: macOS's per-user
// TMPDIR plus t.TempDir() overflows the 104-byte limit on socket paths.
func sockDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "otm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

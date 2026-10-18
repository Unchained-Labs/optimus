package mux

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// The pane runs with the tmux server's environment: systemd-run must be told
// where the user manager is, or it fails and the agent never starts.
func TestScopedCarriesTheRuntimeDir(t *testing.T) {
	prev := scopesAvailable
	scopesAvailable = func() bool { return true }
	defer func() { scopesAvailable = prev }()
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/4242")
	got := strings.Join(scoped([]string{"claude", "--resume", "x"}), " ")
	if !strings.HasPrefix(got, "env XDG_RUNTIME_DIR=/run/user/4242 systemd-run --user --scope ") || !strings.HasSuffix(got, " -- claude --resume x") {
		t.Errorf("argv: %s", got)
	}
}

// Two launches on a server that isn't running yet both try to create it.
func TestSpawnAtOnceOnAColdServer(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	tmp := t.TempDir()
	t.Setenv("TMUX_TMPDIR", tmp)
	t.Setenv("OPTIMUS_HOME", tmp)
	t.Setenv("OPTIMUS_NO_SCOPE", "1")
	prev := Socket
	Socket = fmt.Sprintf("optimus-cold-%d", time.Now().UnixNano())
	defer func() { _ = exec.Command("tmux", "-L", Socket, "kill-server").Run(); Socket = prev }()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := Spawn(fmt.Sprintf("a%d", i), "shell", tmp, "", []string{"sleep", "30"}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if ws, _ := List(); len(ws) != 4 {
		t.Errorf("%d windows, want 4", len(ws))
	}
}

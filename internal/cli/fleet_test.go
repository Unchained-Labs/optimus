package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Unchained-Labs/optimus/internal/agentstate"
	"github.com/Unchained-Labs/optimus/internal/app"
	"github.com/Unchained-Labs/optimus/internal/mux"
)

func TestFleetSummary(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	dir := t.TempDir()
	t.Setenv("OPTIMUS_HOME", dir+"/o")
	t.Setenv("OPTIMUS_AGENT_HOME", dir+"/home")
	t.Setenv("CLAUDE_CONFIG_DIR", dir+"/home/.claude")
	sock := fmt.Sprintf("optimus-fleet-%d", time.Now().UnixNano())
	mux.Socket = sock
	t.Cleanup(func() {
		exec.Command("tmux", "-L", sock, "kill-server").Run()
		os.Remove(filepath.Join(tmuxDir(), sock)) // tmux can leave the socket file behind
	})

	a, b := mustSpawn(t, "one"), mustSpawn(t, "two")
	_ = a
	w, _ := mux.Resolve(b)
	agentstate.Write(agentstate.Record{Pane: w.Pane, Agent: "claude", State: mux.StateWaiting, Full: true})

	f := fleetSummary(app.New())
	if f.Agents != 2 || f.Waiting != 1 {
		t.Fatalf("fleet: %+v", f)
	}
	if s := f.String(); !strings.Contains(s, "⧉ 2 agents") || !strings.Contains(s, "◆ 1 waiting") || !strings.HasSuffix(s, " today") {
		t.Errorf("summary: %q", s)
	}
}

func mustSpawn(t *testing.T, name string) string {
	t.Helper()
	id, err := mux.Spawn(name, "shell", t.TempDir(), "", []string{"sh", "-c", "sleep 60"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func tmuxDir() string {
	if d := os.Getenv("TMUX_TMPDIR"); d != "" {
		return filepath.Join(d, fmt.Sprintf("tmux-%d", os.Getuid()))
	}
	return fmt.Sprintf("/tmp/tmux-%d", os.Getuid())
}

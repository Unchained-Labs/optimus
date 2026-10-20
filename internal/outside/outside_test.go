package outside

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Unchained-Labs/optimus/internal/index"
	"github.com/Unchained-Labs/optimus/internal/model"
	"github.com/Unchained-Labs/optimus/internal/mux"
)

func TestAgentOf(t *testing.T) {
	cases := map[string]proc{
		"claude":   {comm: "claude"},
		"codex":    {comm: "node", args: []string{"node", "/usr/lib/node_modules/@openai/codex/bin/codex.js"}},
		"opencode": {comm: "opencode"},
		"":         {comm: "zsh", args: []string{"zsh"}},
	}
	for want, p := range cases {
		if got := agentOf(p); got != want {
			t.Errorf("%+v: got %q want %q", p, got, want)
		}
	}
}

func TestSessionFor(t *testing.T) {
	now := time.Now()
	idx := &index.Index{Sessions: []*model.Session{
		{Agent: "codex", ID: "new", Cwd: "/w", End: now},
		{Agent: "codex", ID: "old", Cwd: "/w", End: now.Add(-3 * time.Hour)},
	}}
	if s := sessionFor(idx, Agent{Agent: "codex", Cwd: "/w", Started: now.Add(-time.Hour)}, nil); s == nil || s.ID != "new" {
		t.Errorf("got %+v", s)
	}
	if s := sessionFor(idx, Agent{Agent: "codex", Cwd: "/other", Started: now.Add(-time.Hour)}, nil); s != nil {
		t.Errorf("other folder must not match: %+v", s)
	}
}

func TestSessionForSkipsClaimed(t *testing.T) {
	now := time.Now()
	idx := &index.Index{Sessions: []*model.Session{
		{Agent: "claude", ID: "new", Cwd: "/w", End: now},
		{Agent: "claude", ID: "older", Cwd: "/w", End: now.Add(-time.Minute)},
	}}
	a := Agent{Agent: "claude", Cwd: "/w", Started: now.Add(-time.Hour)}
	if s := sessionFor(idx, a, map[string]bool{"new": true}); s == nil || s.ID != "older" {
		t.Errorf("a second agent in the folder got %+v, want the session nobody writes", s)
	}
	if s := sessionFor(idx, a, map[string]bool{"new": true, "older": true}); s != nil {
		t.Errorf("every session is taken, got %+v", s)
	}
}

func TestSessionArg(t *testing.T) {
	id := "71f6caa6-872e-4a8f-b1ed-a9431efd72dc"
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"claude", "--resume", id}, id},
		{[]string{"claude", "-r", id, "--name=x"}, id},
		{[]string{"claude", "--session-id=" + id}, id},
		{[]string{"codex", "resume", id}, id},
		{[]string{"claude", "-r", "fix the build"}, ""},
		{[]string{"claude", "--resume"}, ""},
		{[]string{"claude"}, ""},
	}
	for _, c := range cases {
		if got := sessionArg(c.args); got != c.want {
			t.Errorf("%v: got %q want %q", c.args, got, c.want)
		}
	}
}

// TestScanFindsAgentsInYourTmux runs a fake agent in a "personal" tmux server
// and another inside an optimus server, in an isolated tmux directory.
func TestScanFindsAgentsInYourTmux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("process scan test is linux-only")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	tmp := t.TempDir()
	t.Setenv("TMUX_TMPDIR", tmp)
	t.Setenv("OPTIMUS_HOME", tmp+"/o")
	sleep, _ := exec.LookPath("sleep")
	bin := filepath.Join(tmp, "bin")
	os.MkdirAll(bin, 0o755)
	agent := filepath.Join(bin, "aider") // a "coding agent" that just sleeps
	if b, err := os.ReadFile(sleep); err != nil || os.WriteFile(agent, b, 0o755) != nil {
		t.Skip("can't copy sleep")
	}
	mine := filepath.Join(tmp, fmt.Sprintf("tmux-%d", os.Getuid()), "personal")
	os.MkdirAll(filepath.Dir(mine), 0o700)
	if out, err := exec.Command("tmux", "-S", mine, "new-session", "-d", "-s", "work", agent, "120").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	defer exec.Command("tmux", "-S", mine, "kill-server").Run()
	mux.Socket = fmt.Sprintf("optimus-scan-%d", time.Now().UnixNano())
	if _, err := mux.Spawn("inside", "aider", tmp, "", []string{agent, "120"}); err != nil {
		t.Fatal(err)
	}
	defer exec.Command("tmux", "-L", mux.Socket, "kill-server").Run()
	time.Sleep(300 * time.Millisecond)

	cacheAt = time.Time{}
	var mineFound, optimusFound int
	for _, a := range Scan(nil) {
		if a.Agent != "aider" {
			continue
		}
		if a.Pane != nil && a.Pane.Socket == "personal" && a.Pane.Session == "work" {
			mineFound++
		} else {
			optimusFound++
		}
	}
	if mineFound != 1 || optimusFound != 0 {
		t.Fatalf("personal=%d (want 1), inside optimus=%d (want 0)", mineFound, optimusFound)
	}
	ws := Windows(Scan(nil))
	if len(ws) != 1 || !ws[0].External || !mux.IsExternal(ws[0].ID) {
		t.Fatalf("linked windows: %+v", ws)
	}
	if out, err := mux.Capture(ws[0].ID, 5); err != nil {
		t.Errorf("capture through the linked id: %v %q", err, out)
	}
	if err := mux.Kill(ws[0].ID); err == nil {
		t.Error("optimus must refuse to stop an agent in your own tmux")
	}
}

// A take-over stops the agent and resumes its session within the 2 s the
// scan is cached: the stopped agent must not still claim the session, or the
// resume refuses ("already running in a terminal tab").
func TestScanDropsAgentsThatExited(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs /proc")
	}
	mux.Socket = fmt.Sprintf("optimus-none-%d", time.Now().UnixNano()) // no server
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	gone := exec.Command("true")
	if err := gone.Run(); err != nil {
		t.Skip(err)
	}
	me := os.Getpid()
	cacheMu.Lock()
	cached, cacheAt = []Agent{
		{PID: gone.Process.Pid, Agent: "claude", SessionID: "stopped"},
		{PID: me, Agent: "claude", SessionID: "reused", Started: startTime(me).Add(-time.Hour)}, // pid now held by another program
		{PID: me, Agent: "claude", SessionID: "running", Started: startTime(me)},
	}, time.Now()
	cacheMu.Unlock()
	defer func() { cacheMu.Lock(); cached, cacheAt = nil, time.Time{}; cacheMu.Unlock() }()
	var got []string
	for _, a := range Scan(nil) {
		got = append(got, a.SessionID)
	}
	if len(got) != 1 || got[0] != "running" {
		t.Fatalf("got %v, want only the agent still running", got)
	}
}

func TestClaudeRegistryIgnoresReusedPids(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs /proc")
	}
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	os.MkdirAll(filepath.Join(dir, "sessions"), 0o755)
	me := os.Getpid()
	st, _ := os.ReadFile(fmt.Sprintf("/proc/%d/stat", me))
	f := strings.Fields(string(st)[strings.LastIndexByte(string(st), ')')+1:])
	write := func(procStart string) {
		os.WriteFile(filepath.Join(dir, "sessions", fmt.Sprintf("%d.json", me)), []byte(fmt.Sprintf(`{"pid":%d,"sessionId":"s1","cwd":"/w","procStart":%q}`, me, procStart)), 0o644)
	}
	write(f[19])
	if l, ok := claudeLive()[me]; !ok || l.SessionID != "s1" {
		t.Fatalf("own entry not read: %+v", l)
	}
	write("1") // left by a killed Claude whose pid is now ours
	if l, ok := claudeLive()[me]; ok {
		t.Fatalf("stale entry of a reused pid was trusted: %+v", l)
	}
}

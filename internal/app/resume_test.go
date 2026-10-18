package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Unchained-Labs/optimus/internal/config"
	"github.com/Unchained-Labs/optimus/internal/model"
	"github.com/Unchained-Labs/optimus/internal/mux"
	"github.com/Unchained-Labs/optimus/internal/worktree"
)

// testApp resumes "opencode" sessions with a stand-in that just sleeps, on a
// tmux server of its own.
func testApp(t *testing.T) *App {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	tmp := t.TempDir()
	t.Setenv("OPTIMUS_HOME", filepath.Join(tmp, "home"))
	t.Setenv("TMUX_TMPDIR", sockDir(t))
	t.Setenv("OPTIMUS_NO_SCOPE", "1")
	bin := filepath.Join(tmp, "fake-agent")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	prev := mux.Socket
	mux.Socket = fmt.Sprintf("optimus-resume-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", mux.Socket, "kill-server").Run()
		mux.Socket = prev
	})
	off := false
	cfg := config.Default()
	cfg.Remote.WebAutostart, cfg.Remote.Addr = &off, "127.0.0.1:1"
	cfg.Agents = map[string]config.Agent{"opencode": {Command: bin}}
	return &App{Cfg: cfg}
}

func TestResumeTwiceAtOnceStartsOneAgent(t *testing.T) {
	a := testApp(t)
	s := &model.Session{Agent: "opencode", ID: "ses_double", Cwd: t.TempDir()}
	var wg sync.WaitGroup
	ids := make([]string, 4)
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id, err := a.Resume(s)
			if err != nil {
				t.Error(err)
			}
			ids[i] = id
		}(i)
	}
	wg.Wait()
	ws, _ := mux.List()
	if len(ws) != 1 {
		t.Fatalf("%d windows for one session (ids %v)", len(ws), ids)
	}
	for _, id := range ids {
		if id != ws[0].ID {
			t.Errorf("resume returned %s, the session runs in %s", id, ws[0].ID)
		}
	}
}

func TestResumeInADeletedFolderRecreatesIt(t *testing.T) {
	a := testApp(t)
	gone := filepath.Join(t.TempDir(), "discarded", "tree")
	id, err := a.Resume(&model.Session{Agent: "opencode", ID: "ses_gone", Cwd: gone})
	if err != nil {
		t.Fatal(err)
	}
	w, err := mux.Resolve(id)
	if err != nil {
		t.Fatal(err)
	}
	if w.Cwd != gone {
		t.Errorf("resumed in %q, want the session's own folder %q", w.Cwd, gone)
	}
}

func TestResumeReattachesItsWorktree(t *testing.T) {
	a := testApp(t)
	for k, v := range map[string]string{"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.com", "GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.com"} {
		t.Setenv(k, v)
	}
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	tree, err := worktree.Create(repo, "opencode", "fix it")
	if err != nil {
		t.Fatal(err)
	}
	// the agent exited: its window, the only record of the tree, is gone
	id, err := a.Resume(&model.Session{Agent: "opencode", ID: "ses_tree", Cwd: tree.Path})
	if err != nil {
		t.Fatal(err)
	}
	w, _ := mux.Resolve(id)
	got, ok := Tree(w)
	if !ok || got.Branch != tree.Branch || got.Base != tree.Base || !samePath(got.Repo, tree.Repo) {
		t.Errorf("resumed window's tree %+v, want %+v", got, tree)
	}
}

func samePath(a, b string) bool {
	ra, _ := filepath.EvalSymlinks(a)
	rb, _ := filepath.EvalSymlinks(b)
	return ra == rb
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

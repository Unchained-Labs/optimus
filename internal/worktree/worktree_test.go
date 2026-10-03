package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repo(t *testing.T) string {
	t.Helper()
	t.Setenv("OPTIMUS_HOME", t.TempDir())
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-qm", "a").Run()
	return dir
}

func TestLifecycle(t *testing.T) {
	dir := repo(t)
	tr, err := Create(filepath.Join(dir), "codex", "Fix the Login bug!")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tr.Branch, "optimus/codex-fix-the-login-bug-") || !strings.HasPrefix(tr.Path, os.Getenv("OPTIMUS_HOME")) {
		t.Errorf("tree: %+v", tr)
	}
	if s, _ := tr.Stats(); !s.Empty() {
		t.Errorf("fresh tree should be empty: %+v", s)
	}
	// agent edits a file and adds a new one
	os.WriteFile(filepath.Join(tr.Path, "a.txt"), []byte("one\ntwo\n"), 0o644)
	os.WriteFile(filepath.Join(tr.Path, "b.txt"), []byte("new\n"), 0o644)
	s, _ := tr.Stats()
	if s.Files != 1 || s.Insertions != 1 || s.Untracked != 1 || s.String() != "+1 −0 · 2 files" {
		t.Errorf("stats: %+v %q", s, s)
	}
	if d, _ := tr.Diff(); !strings.Contains(d, "+two") || !strings.Contains(d, "b.txt") {
		t.Errorf("diff misses changes:\n%s", d)
	}
	if err := tr.Remove(false); err == nil {
		t.Error("removing unmerged work must need force")
	}
	if err := tr.Merge(false, "x"); err == nil {
		t.Error("uncommitted work must not merge without commit")
	}
	if err := tr.Merge(true, "optimus: codex work"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(b) != "new\n" {
		t.Error("merge did not land in the main checkout")
	}
	if err := tr.Remove(false); err != nil {
		t.Fatalf("merged tree should remove cleanly: %v", err)
	}
	if _, err := os.Stat(tr.Path); !os.IsNotExist(err) {
		t.Error("worktree dir still exists")
	}
}

func TestNotARepo(t *testing.T) {
	t.Setenv("OPTIMUS_HOME", t.TempDir())
	if _, err := Create(t.TempDir(), "claude", "x"); err == nil || !strings.Contains(err.Error(), "not inside a git repository") {
		t.Errorf("err = %v", err)
	}
}

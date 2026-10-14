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

func TestCopyIncluded(t *testing.T) {
	dir := repo(t)
	write := func(rel, s string) {
		p := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", ".env*\nnode_modules/\ncerts/\n")
	write(IncludeFile, "# local config the agents need\n.env\n.env.local\n**/.env\ncerts/\nnotes.txt\n")
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-qm", "ignores").Run()
	write(".env", "SECRET=1\n")
	write(".env.local", "LOCAL=1\n")
	write("web/.env", "WEB=1\n")
	write("certs/dev/key.pem", "key\n")
	write("node_modules/x/index.js", "big\n") // ignored, but not listed
	write("notes.txt", "wip\n")               // listed, but not ignored: someone's work, not config
	os.Symlink(".env", filepath.Join(dir, ".env.link"))

	tr, err := Create(dir, "claude", "x")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(tr.Copied, " ")
	for _, want := range []string{".env", ".env.local", "web/.env", "certs/dev/key.pem"} {
		if !strings.Contains(" "+got+" ", " "+want+" ") {
			t.Errorf("%s not copied (copied: %s)", want, got)
		}
	}
	for _, not := range []string{"node_modules/x/index.js", "notes.txt", ".env.link"} {
		if _, err := os.Lstat(filepath.Join(tr.Path, not)); err == nil {
			t.Errorf("%s must not be copied", not)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(tr.Path, "web/.env")); string(b) != "WEB=1\n" {
		t.Errorf("web/.env = %q", b)
	}
	if st, _ := os.Stat(filepath.Join(tr.Path, ".env")); st == nil || st.Mode().Perm() != 0o600 {
		t.Errorf(".env must keep its permissions: %v", st)
	}
	if s, _ := tr.Stats(); !s.Empty() {
		t.Errorf("copied ignored files must not count as the agent's changes: %+v", s)
	}
	// running it again keeps what's there
	os.WriteFile(filepath.Join(tr.Path, ".env"), []byte("EDITED\n"), 0o600)
	if again, err := tr.CopyIncluded(); err != nil || len(again) != 0 {
		t.Errorf("second copy: %v %v", again, err)
	}
	if b, _ := os.ReadFile(filepath.Join(tr.Path, ".env")); string(b) != "EDITED\n" {
		t.Error("an existing file was overwritten")
	}
}

func TestCopyIncludedSymlink(t *testing.T) {
	dir := repo(t)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("local/\n"), 0o644)
	os.WriteFile(filepath.Join(dir, IncludeFile), []byte("local/\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "local"), 0o755)
	os.Symlink("/etc/hostname", filepath.Join(dir, "local", "link"))
	tr, err := Create(dir, "claude", "x")
	if err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(filepath.Join(tr.Path, "local", "link")); err != nil || target != "/etc/hostname" {
		t.Errorf("symlink not recreated: %q %v", target, err)
	}
}

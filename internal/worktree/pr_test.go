package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareURL(t *testing.T) {
	for _, c := range []struct{ remote, want string }{
		{"git@github.com:acme/api.git", "https://github.com/acme/api/compare/main...optimus/x-1?expand=1"},
		{"https://github.com/acme/api", "https://github.com/acme/api/compare/main...optimus/x-1?expand=1"},
		{"ssh://git@github.com/acme/api.git", "https://github.com/acme/api/compare/main...optimus/x-1?expand=1"},
		{"https://user:tok@github.example.com/acme/api.git", "https://github.example.com/acme/api/compare/main...optimus/x-1?expand=1"},
		{"git@gitlab.com:grp/sub/api.git", "https://gitlab.com/grp/sub/api/-/merge_requests/new?merge_request%5Bsource_branch%5D=optimus%2Fx-1&merge_request%5Btarget_branch%5D=main"},
		{"git@bitbucket.org:acme/api.git", "https://bitbucket.org/acme/api/pull-requests/new?source=optimus%2Fx-1&dest=main"},
		{"/srv/git/api.git", ""},
		{"work-alias:acme/api.git", ""},
		{"https://git.example.com/acme/api.git", ""},
	} {
		if got := compareURL(c.remote, "main", "optimus/x-1"); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.remote, got, c.want)
		}
	}
}

// published returns a repository with a bare origin, and a worktree of it.
func published(t *testing.T) (dir, origin string, tr Tree) {
	dir = repo(t)
	origin = filepath.Join(t.TempDir(), "origin.git")
	for _, args := range [][]string{{"init", "-q", "--bare", origin}, {"-C", dir, "remote", "add", "origin", origin}, {"-C", dir, "push", "-q", "-u", "origin", "main"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	tr, err := Create(dir, "claude", "add rate limiting")
	if err != nil {
		t.Fatal(err)
	}
	return dir, origin, tr
}

func TestPullRequestWithoutGH(t *testing.T) {
	ghBin = func() string { return "" }
	t.Cleanup(func() { ghBin = func() string { p, _ := exec.LookPath("gh"); return p } })
	_, origin, tr := published(t)
	if _, err := tr.PullRequest("x", PROptions{}); err == nil || !strings.Contains(err.Error(), "nothing to publish") {
		t.Errorf("a branch without work must not be published: %v", err)
	}
	os.WriteFile(filepath.Join(tr.Path, "limit.go"), []byte("package x\n"), 0o644)
	pr, err := tr.PullRequest("optimus: rate limiting", PROptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pr.Opened || pr.Base != "main" || pr.Remote != "origin" || pr.URL != "" || !strings.Contains(pr.Message, "gh") {
		t.Errorf("pr: %+v", pr)
	}
	if out, err := exec.Command("git", "-C", origin, "log", "--format=%s", tr.Branch).Output(); err != nil || !strings.HasPrefix(string(out), "optimus: rate limiting") {
		t.Errorf("branch not pushed with the pending work committed: %q %v", out, err)
	}
}

func TestPullRequestWithGH(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(bin, "args")
	// a fake gh: the first create succeeds, the next one finds the existing PR
	script := `#!/bin/sh
echo "$@" >> ` + log + `
case "$1 $2" in
"pr create") if [ -e ` + log + `.done ]; then echo 'a pull request for branch "b" into branch "main" already exists:'; echo https://github.com/acme/api/pull/7; exit 1; fi
  touch ` + log + `.done; echo 'Creating pull request'; echo https://github.com/acme/api/pull/7 ;;
"pr view") echo https://github.com/acme/api/pull/7 ;;
esac
`
	os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755)
	ghBin = func() string { return filepath.Join(bin, "gh") }
	t.Cleanup(func() { ghBin = func() string { p, _ := exec.LookPath("gh"); return p } })
	_, _, tr := published(t)
	os.WriteFile(filepath.Join(tr.Path, "limit.go"), []byte("package x\n"), 0o644)
	pr, err := tr.PullRequest("wip", PROptions{Title: "Add rate limiting", Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	if !pr.Opened || pr.URL != "https://github.com/acme/api/pull/7" {
		t.Errorf("pr: %+v", pr)
	}
	args, _ := os.ReadFile(log)
	if a := string(args); !strings.Contains(a, "--head "+tr.Branch+" --base main --title Add rate limiting --body - Add rate limiting") || !strings.Contains(a, "--draft") {
		t.Errorf("gh args: %s", a)
	}
	// more work later: pushing again returns the same pull request
	os.WriteFile(filepath.Join(tr.Path, "limit_test.go"), []byte("package x\n"), 0o644)
	if pr, err := tr.PullRequest("tests", PROptions{Interactive: true}); err != nil || !pr.Opened || pr.URL != "https://github.com/acme/api/pull/7" {
		t.Errorf("existing pr: %+v %v", pr, err)
	}
}

func TestPullRequestNeedsRemote(t *testing.T) {
	dir := repo(t)
	tr, err := Create(dir, "codex", "x")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(tr.Path, "n.txt"), []byte("n\n"), 0o644)
	if _, err := tr.PullRequest("x", PROptions{}); err == nil || !strings.Contains(err.Error(), "no git remote") {
		t.Errorf("err = %v", err)
	}
}

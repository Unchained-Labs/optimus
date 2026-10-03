// Package worktree gives each agent its own git worktree and branch, so
// agents working in the same repository don't trample each other's changes,
// and lets you review, merge or discard what each one did.
package worktree

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Unchained-Labs/optimus/internal/config"
)

// Tree is an agent's worktree.
type Tree struct {
	Repo   string // main checkout (where merges land)
	Path   string // the worktree directory the agent works in
	Branch string // optimus/<agent>-<label>-<id>
	Base   string // commit the branch started from
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return string(out), errors.New("git " + args[0] + ": " + msg)
	}
	return string(out), nil
}

// Root returns the top-level directory of the repository containing dir.
func Root(dir string) (string, bool) {
	out, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(out), true
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string, n int) string {
	s = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > n {
		s = strings.TrimRight(s[:n], "-")
	}
	return s
}

// Create adds a worktree on a new branch from the repository's current HEAD.
func Create(dir, agent, label string) (Tree, error) {
	root, ok := Root(dir)
	if !ok {
		return Tree{}, fmt.Errorf("%s is not inside a git repository", dir)
	}
	base, err := git(root, "rev-parse", "HEAD")
	if err != nil {
		return Tree{}, fmt.Errorf("the repository has no commits yet: %w", err)
	}
	var b [2]byte
	_, _ = rand.Read(b[:])
	name := agent
	if l := slug(label, 24); l != "" {
		name += "-" + l
	}
	name = fmt.Sprintf("%s-%x", name, b)
	t := Tree{
		Repo:   root,
		Branch: "optimus/" + name,
		Base:   strings.TrimSpace(base),
		Path:   filepath.Join(config.DataDir(), "worktrees", filepath.Base(root), name),
	}
	if err := os.MkdirAll(filepath.Dir(t.Path), 0o755); err != nil {
		return Tree{}, err
	}
	if _, err := git(root, "worktree", "add", "-b", t.Branch, t.Path, t.Base); err != nil {
		return Tree{}, err
	}
	return t, nil
}

// Stats summarizes what an agent changed relative to its base: committed and
// uncommitted changes to tracked files, plus new untracked files.
type Stats struct {
	Files      int `json:"files"`
	Insertions int `json:"insertions"`
	Deletions  int `json:"deletions"`
	Commits    int `json:"commits"`
	Untracked  int `json:"untracked"`
}

func (s Stats) Empty() bool { return s.Files == 0 && s.Untracked == 0 && s.Commits == 0 }

func (s Stats) String() string {
	if s.Empty() {
		return "no changes"
	}
	parts := []string{fmt.Sprintf("+%d −%d", s.Insertions, s.Deletions), plural(s.Files+s.Untracked, "file")}
	if s.Commits > 0 {
		parts = append(parts, plural(s.Commits, "commit"))
	}
	return strings.Join(parts, " · ")
}

func plural(n int, w string) string {
	if n == 1 {
		return "1 " + w
	}
	return strconv.Itoa(n) + " " + w + "s"
}

var shortstatRe = regexp.MustCompile(`(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?`)

func (t Tree) Stats() (Stats, error) {
	var s Stats
	out, err := git(t.Path, "diff", "--shortstat", t.Base)
	if err != nil {
		return s, err
	}
	if m := shortstatRe.FindStringSubmatch(out); m != nil {
		s.Files, _ = strconv.Atoi(m[1])
		s.Insertions, _ = strconv.Atoi(m[2])
		s.Deletions, _ = strconv.Atoi(m[3])
	}
	if out, err := git(t.Path, "ls-files", "--others", "--exclude-standard"); err == nil {
		s.Untracked = len(strings.Fields(out))
	}
	if out, err := git(t.Path, "rev-list", "--count", t.Base+"..HEAD"); err == nil {
		s.Commits, _ = strconv.Atoi(strings.TrimSpace(out))
	}
	return s, nil
}

// Diff is everything the agent changed since its base, new files included.
func (t Tree) Diff() (string, error) {
	out, err := git(t.Path, "diff", "--stat", "--patch", t.Base)
	if err != nil {
		return "", err
	}
	if un, err := git(t.Path, "ls-files", "--others", "--exclude-standard"); err == nil && strings.TrimSpace(un) != "" {
		for _, f := range strings.Fields(un) {
			d, _ := exec.Command("git", "-C", t.Path, "diff", "--no-index", "--", "/dev/null", f).Output()
			out += string(d)
		}
	}
	return out, nil
}

// Dirty reports uncommitted changes (tracked or untracked) in dir.
func Dirty(dir string) bool {
	out, err := git(dir, "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) != ""
}

// Merge brings the agent's work into the branch checked out in the main
// repository. With commit, uncommitted changes in the worktree are
// committed first; otherwise they block the merge.
func (t Tree) Merge(commit bool, message string) error {
	if Dirty(t.Path) {
		if !commit {
			return errors.New("the agent's worktree has uncommitted changes — commit them first (or merge with commit)")
		}
		if _, err := git(t.Path, "add", "-A"); err != nil {
			return err
		}
		if _, err := git(t.Path, "commit", "-m", message); err != nil {
			return err
		}
	}
	if Dirty(t.Repo) {
		return fmt.Errorf("%s has uncommitted changes; commit or stash them before merging", t.Repo)
	}
	if out, err := git(t.Path, "rev-list", "--count", t.Base+"..HEAD"); err == nil && strings.TrimSpace(out) == "0" {
		return errors.New("nothing to merge: the agent made no changes")
	}
	_, err := git(t.Repo, "merge", "--no-ff", "-m", "Merge "+t.Branch, t.Branch)
	return err
}

// Remove deletes the worktree and its branch. Unless force, it refuses to
// drop work that isn't merged.
func (t Tree) Remove(force bool) error {
	if !force {
		if Dirty(t.Path) {
			return errors.New("the worktree has uncommitted changes (use force to discard them)")
		}
		if out, err := git(t.Repo, "branch", "--no-merged", "HEAD", "--list", t.Branch); err == nil && strings.TrimSpace(out) != "" {
			return errors.New("the branch has unmerged commits (merge it, or use force to discard them)")
		}
	}
	args := []string{"worktree", "remove", t.Path}
	if force {
		args = append(args, "--force")
	}
	if _, err := git(t.Repo, args...); err != nil {
		return err
	}
	del := "-d"
	if force {
		del = "-D"
	}
	_, err := git(t.Repo, "branch", del, t.Branch)
	return err
}

// Package worktree gives each agent its own git worktree and branch, so
// agents working in the same repository don't trample each other's changes,
// and lets you review, merge or discard what each one did.
package worktree

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
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

	Copied []string // files copied from the main checkout on creation (.worktreeinclude)
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
	// best effort: a missing .env makes the agent's build fail, which it
	// reports and you fix; refusing to start the agent over it would not help
	t.Copied, _ = t.CopyIncluded()
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

// Of recovers the worktree optimus made at path, for an agent whose window
// is gone (it exited, or the tmux server restarted): the window carried the
// tree, so resuming its session would otherwise run in the worktree with no
// way to diff, merge or discard it. Base is where the branch meets the main
// checkout's HEAD — the commit it started from unless the main branch was
// reset behind it.
func Of(path string) (Tree, bool) {
	root := filepath.Join(config.DataDir(), "worktrees") + string(filepath.Separator)
	if !strings.HasPrefix(filepath.Clean(path)+string(filepath.Separator), root) {
		return Tree{}, false
	}
	top, ok := Root(path)
	if !ok || !samePath(top, path) {
		return Tree{}, false
	}
	br, err := git(path, "symbolic-ref", "--short", "HEAD")
	if err != nil || !strings.HasPrefix(strings.TrimSpace(br), "optimus/") {
		return Tree{}, false
	}
	common, err := git(path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return Tree{}, false
	}
	t := Tree{Path: path, Branch: strings.TrimSpace(br), Repo: filepath.Dir(strings.TrimSpace(common))}
	base, err := git(t.Repo, "merge-base", "HEAD", t.Branch)
	if err != nil {
		return Tree{}, false
	}
	t.Base = strings.TrimSpace(base)
	return t, true
}

func samePath(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

// IncludeFile names the file listing gitignored files a new worktree needs
// (.env, local config, certificates), in .gitignore syntax. It's the same
// file Claude Code and Conductor read, so a repository set up for them works
// here unchanged.
const IncludeFile = ".worktreeinclude"

// CopyIncluded copies into the worktree every file of the main checkout that
// matches a pattern in .worktreeinclude and is ignored by git: a fresh
// checkout has no .env, so without them the agent's first build or test run
// fails. Tracked files are never copied (the checkout has them), files
// already in the worktree are kept, and one file that can't be copied
// doesn't stop the rest. It returns the paths copied, relative to the root.
func (t Tree) CopyIncluded() ([]string, error) {
	inc := filepath.Join(t.Repo, IncludeFile)
	if _, err := os.Stat(inc); err != nil {
		return nil, nil
	}
	out, err := git(t.Repo, "ls-files", "-z", "--others", "--ignored", "--exclude-from="+inc)
	if err != nil || out == "" {
		return nil, err
	}
	// keep only what git ignores: an untracked file that isn't ignored is
	// work in progress in the main checkout, not local configuration
	ci := exec.Command("git", "-C", t.Repo, "check-ignore", "-z", "--stdin")
	ci.Stdin = strings.NewReader(out)
	ign, _ := ci.Output() // exit 1 means none is ignored
	var copied []string
	var errs []error
	for _, rel := range strings.Split(string(ign), "\x00") {
		if rel == "" {
			continue
		}
		dst := filepath.Join(t.Path, rel)
		if _, err := os.Lstat(dst); err == nil {
			continue
		}
		if err := copyFile(filepath.Join(t.Repo, rel), dst); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", rel, err))
			continue
		}
		copied = append(copied, rel)
	}
	return copied, errors.Join(errs...)
}

// copyFile copies a regular file with its permissions, or recreates a
// symlink as a symlink.
func copyFile(src, dst string) error {
	st, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	if !st.Mode().IsRegular() {
		return nil // sockets, fifos: nothing to carry over
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, st.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

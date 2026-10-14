package worktree

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// PR is the outcome of publishing an agent's branch for review.
type PR struct {
	URL     string `json:"url"`            // the pull request, or the page that opens one
	Opened  bool   `json:"opened"`         // URL is a pull request (not a page to open one)
	Branch  string `json:"branch"`         // the agent's branch, pushed
	Base    string `json:"base"`           // the branch the pull request targets
	Remote  string `json:"remote"`         // where it was pushed
	Message string `json:"note,omitempty"` // why no pull request was opened, if none was
}

// ghBin finds the GitHub CLI; tests point it at a fake.
var ghBin = func() string { p, _ := exec.LookPath("gh"); return p }

// PROptions shape a pull request.
type PROptions struct {
	Title string // empty: gh takes title and body from the commits
	Draft bool
	// Interactive lets git and gh ask for a password or passphrase on the
	// terminal. Without it (the web dashboard: nobody is at its terminal)
	// they fail instead of waiting forever, and give up after a few minutes.
	Interactive bool
}

const publishTimeout = 3 * time.Minute

func (o PROptions) command(dir, name string, args ...string) (*exec.Cmd, context.CancelFunc) {
	if o.Interactive {
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		return cmd, func() {}
	}
	ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GH_PROMPT_DISABLED=1")
	// no controlling terminal: ssh can't prompt for a passphrase on the tty
	// of the tmux window the dashboard runs in
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd, cancel
}

func (o PROptions) run(dir, name string, args ...string) ([]byte, error) {
	cmd, cancel := o.command(dir, name, args...)
	defer cancel()
	return cmd.CombinedOutput()
}

// PullRequest pushes the agent's branch and opens a pull request against the
// branch checked out in the main repository: the branch Merge would land in,
// so "review it on GitHub" and "merge it here" mean the same thing. Pending
// changes are committed first, with the title as message if there is one. With the GitHub CLI (gh) the pull
// request is opened, or the existing one returned; without it, or when gh
// can't, the URL is the forge's page to open one (GitHub, GitLab, Bitbucket).
func (t Tree) PullRequest(message string, o PROptions) (PR, error) {
	pr := PR{Branch: t.Branch}
	if o.Title != "" {
		message = o.Title // the pending work is what the title describes
	}
	if Dirty(t.Path) {
		if _, err := git(t.Path, "add", "-A"); err != nil {
			return pr, err
		}
		if _, err := git(t.Path, "commit", "-m", message); err != nil {
			return pr, err
		}
	}
	if out, err := git(t.Path, "rev-list", "--count", t.Base+"..HEAD"); err == nil && strings.TrimSpace(out) == "0" {
		return pr, errors.New("nothing to publish: the agent made no changes")
	}
	pr.Base, pr.Remote = target(t.Repo)
	if pr.Remote == "" {
		return pr, fmt.Errorf("%s has no git remote to push to", t.Repo)
	}
	if pr.Base == "" {
		return pr, fmt.Errorf("can't tell which branch the pull request should target: %s has no branch checked out and %s has no default branch", t.Repo, pr.Remote)
	}
	// push from the main checkout: a relative remote path is relative to it
	if out, err := o.run(t.Repo, "git", "push", "-u", pr.Remote, t.Branch); err != nil {
		return pr, fmt.Errorf("git push: %s", strings.TrimSpace(string(out)))
	}
	remoteURL, _ := git(t.Repo, "remote", "get-url", pr.Remote)
	pr.URL = compareURL(strings.TrimSpace(remoteURL), pr.Base, t.Branch)

	gh := ghBin()
	if gh == "" {
		pr.Message = "pushed; install the GitHub CLI (gh) to open pull requests directly"
		return pr, nil
	}
	args := []string{"pr", "create", "--head", t.Branch, "--base", pr.Base}
	if o.Title != "" {
		body, _ := git(t.Path, "log", "--reverse", "--format=- %s", t.Base+"..HEAD")
		args = append(args, "--title", o.Title, "--body", body)
	} else {
		args = append(args, "--fill")
	}
	if o.Draft {
		args = append(args, "--draft")
	}
	out, err := o.run(t.Path, gh, args...)
	if err != nil && strings.Contains(string(out), "already exists") {
		// pushing more work to a branch with a pull request updates it
		out, err = o.run(t.Path, gh, "pr", "view", t.Branch, "--json", "url", "--jq", ".url")
	}
	if u := lastURL(string(out)); err == nil && u != "" {
		pr.URL, pr.Opened = u, true
		return pr, nil
	}
	pr.Message = "pushed, but gh couldn't open the pull request: " + firstLine(string(out))
	return pr, nil
}

// target is the branch a pull request should go to and the remote to push
// to: the main checkout's branch and its upstream remote, falling back to
// origin (or the only remote) and its default branch.
func target(repo string) (base, remote string) {
	if out, err := git(repo, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		base = strings.TrimSpace(out)
	}
	if base != "" {
		if out, err := git(repo, "config", "--get", "branch."+base+".remote"); err == nil {
			remote = strings.TrimSpace(out)
		}
	}
	if remote == "" {
		out, _ := git(repo, "remote")
		rs := strings.Fields(out)
		for _, r := range rs {
			if r == "origin" {
				remote = r
			}
		}
		if remote == "" && len(rs) > 0 {
			remote = rs[0]
		}
	}
	if base == "" && remote != "" {
		if out, err := git(repo, "symbolic-ref", "--short", "-q", "refs/remotes/"+remote+"/HEAD"); err == nil {
			base = strings.TrimPrefix(strings.TrimSpace(out), remote+"/")
		}
	}
	return base, remote
}

var scpRe = regexp.MustCompile(`^(?:[^@/]+@)?([^:/]+):(.+)$`) // git@github.com:owner/repo.git

// compareURL is the forge's page for opening a pull request from branch into
// base, or "" for a remote that isn't a known forge.
func compareURL(remote, base, branch string) string {
	host, path := "", ""
	if u, err := url.Parse(remote); err == nil && u.Scheme != "" && u.Host != "" {
		host, path = u.Hostname(), u.Path
	} else if m := scpRe.FindStringSubmatch(remote); m != nil {
		host, path = m[1], m[2]
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	if !strings.Contains(host, ".") || path == "" {
		return "" // an ssh alias or a local path: no web page to point at
	}
	web := "https://" + host + "/" + path
	q := url.QueryEscape
	switch {
	case strings.Contains(host, "gitlab"):
		return web + "/-/merge_requests/new?merge_request%5Bsource_branch%5D=" + q(branch) + "&merge_request%5Btarget_branch%5D=" + q(base)
	case strings.Contains(host, "bitbucket"):
		return web + "/pull-requests/new?source=" + q(branch) + "&dest=" + q(base)
	case strings.Contains(host, "github"):
		return web + "/compare/" + base + "..." + branch + "?expand=1"
	}
	return ""
}

var urlRe = regexp.MustCompile(`https?://\S+`)

func lastURL(s string) string {
	m := urlRe.FindAllString(s, -1)
	if len(m) == 0 {
		return ""
	}
	return m[len(m)-1]
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

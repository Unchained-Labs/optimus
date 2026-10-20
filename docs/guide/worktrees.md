# Parallel agents & worktrees

Two agents editing the same checkout will overwrite each other's work. optimus can give each agent its **own git worktree on its own branch**, show what each one changed, and merge the one you want.

## Start an agent in its own worktree

=== "Shell"

    ```sh
    optimus codex ~/dev/api -w -p "fix the flaky login test"
    optimus new claude ~/dev/api --worktree
    ```

=== "Terminal dashboard"

    ++n++ or ++shift+n++ in a git repository asks *Run in its own git worktree and branch?*. Answer ++y++.

=== "Browser"

    **＋ New session** → tick **Own git worktree & branch**.

The worktree is created from the repository's current `HEAD`, under `~/.local/share/optimus/worktrees/<repo>/`, on a branch named after the agent and the task, e.g. `optimus/codex-fix-the-flaky-login-3f2a`.

## Bring your `.env` along

A worktree is a fresh checkout, so files git ignores (`.env`, local config, development certificates) aren't in it, and the agent's first build or test run fails without them. List them in a **`.worktreeinclude`** file at the repository root, in `.gitignore` syntax:

```gitignore
# .worktreeinclude: gitignored files every new worktree needs
.env
.env.local
**/.env
config/secrets.json
certs/
```

When optimus creates a worktree (`--worktree`, fan-out, both dashboards), it copies every file of your main checkout that matches a pattern **and** is ignored by git. Tracked files are never copied (the checkout has them), and neither are untracked files git doesn't ignore: those are work in progress, not configuration. Permissions are kept and symlinks stay symlinks. It's the same file Claude Code and Conductor read, so a repository set up for them works unchanged. Commit `.worktreeinclude` itself; it holds patterns, never secrets.

For setup that's more than copying (installing dependencies, generating code), use git's own `post-checkout` hook: `git worktree add` runs it in the new worktree.

## Fan out: one task, several agents

Give the same task to several agents at once, each in its own worktree, then keep the best result:

=== "Shell"

    ```sh
    optimus fanout --agents claude,codex "add rate limiting to the public API"
    ```

=== "Terminal dashboard"

    ++shift+f++, choose the agents (`claude,codex`), then the task.

=== "Browser"

    **＋ New session** → tick **Fan out**, click several agents, write the task, **Fan out**.

## Compare, review, merge, discard

```sh
optimus compare
```

```text
WIN  NAME                AGENT   STATE  CHANGES                    BRANCH
1    claude-add-rate     claude  idle   +84 −12 · 5 files · 1 commit   optimus/claude-add-rate-limiting-to-9c1e
2    codex-add-rate      codex   busy   +61 −3 · 3 files               optimus/codex-add-rate-limiting-to-07ab
```

The same numbers appear next to each agent in both dashboards (`⎇ +84 −12 · 5 files`).

| Action | Shell | Terminal dashboard | Browser |
|---|---|---|---|
| review the diff (new files included) | `optimus wt diff 1` | ++shift+d++ | **Diff** |
| merge into the repository's checked-out branch | `optimus wt merge 1 --commit` | ++shift+m++ | **Merge** |
| push the branch and open a pull request | `optimus wt pr 1` | | **PR** |
| stop the agent and delete its worktree and branch | `optimus wt rm 2 --force` | ++shift+x++ | **Discard** |

Merging commits the agent's pending changes (with `--commit` / in the dashboards), then merges its branch with `--no-ff` into whatever is checked out in your main repository. It refuses if your main checkout has uncommitted changes. Removing without `--force` refuses to drop unmerged or uncommitted work.

## Open a pull request instead

When you'd rather review on GitHub (or from your phone) than merge locally, publish the agent's branch:

```sh
optimus wt pr 1                                  # title and body from the commits
optimus wt pr 1 --title "Add rate limiting" --draft
```

```text
pushed optimus/claude-add-rate-limiting-to-9c1e to origin
pull request into main: https://github.com/acme/api/pull/42
```

It commits the agent's pending changes (with the title as message, if you gave one), pushes the branch to the remote of your main checkout's branch (or `origin`), and opens a pull request into the branch checked out there: the same branch **Merge** would land in. With the [GitHub CLI](https://cli.github.com) (`gh`) installed and logged in, the pull request is opened for you, and running it again after more work pushes the new commits and returns the same pull request. Without `gh`, or for GitLab and Bitbucket, it prints the page that opens one.

In the browser, **PR** asks for an optional title and opens the pull request in a new tab. The dashboard has no terminal to type a password on, so git and gh must already be able to push without asking (a credential helper, an SSH agent, `gh auth login`); otherwise they fail with their own message instead of waiting.

Once the pull request is merged on the forge, `optimus wt rm 1 --force` stops the agent and removes its worktree and local branch (`--force`, because the merge happened elsewhere, so your local branch doesn't know about it).

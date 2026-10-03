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
| stop the agent and delete its worktree and branch | `optimus wt rm 2 --force` | ++shift+x++ | **Discard** |

Merging commits the agent's pending changes (with `--commit` / in the dashboards), then merges its branch with `--no-ff` into whatever is checked out in your main repository. It refuses if your main checkout has uncommitted changes. Removing without `--force` refuses to drop unmerged or uncommitted work.

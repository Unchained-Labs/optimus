# CLI reference

`optimus` with no arguments opens the terminal dashboard. Everything else is a subcommand. This page is generated from the binary's own help on every docs build.

```text
--8<-- "reference/cli-usage.txt"
```

## Agent shortcuts

Any agent name is also a command. It starts that agent in the multiplexer and attaches:

```sh
optimus claude                      # here
optimus codex ~/dev/web             # in a folder
optimus opencode . -p "fix the build"
optimus gemini -d                   # start detached
```

## Times and filters

`--since` accepts `7d`, `12h`, `2w`, `3m`, `today`, `week`, `month`, `all` or a date (`2026-09-01`).
Session arguments accept a full id, a unique prefix (`3f2a`) or `last`.
Window arguments accept the tmux id (`@3`), the index (`3`) or the name.

## JSON output

`ls`, `usage` and `projects` accept `--json`, for scripts and status bars:

```sh
optimus usage --by agent --since today --json | jq '.[] | "\(.key) \(.cost_usd)"'
```

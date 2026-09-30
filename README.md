# optimus

**The tmux of AI coding agents.** One terminal dashboard to run, watch and switch between Claude Code, Codex, opencode and friends; see what they cost and how much quota is left; and carry context from one session to another, even across agents.

```
 OPTIMUS  1 Agents (3)  2 Sessions  3 Projects  4 Usage        today $9.02 · block $9.02 ↻1h27m · claude 5h 62%
  1 claude-api        ● busy   │ claude-api  claude · pid 41233 · enter to attach, Alt-q to come back
    claude · ~/dev/api · 4s    │──────────────────────────────────────────────────────────────
  2 codex-web         ◆ input  │ ● Editing src/routes/login.ts…
    codex · ~/dev/web · 1m     │   Do you want to make this edit? ❯ 1. Yes  2. No
  3 opencode-infra    ○ idle   │
```

## What it does

| | |
|---|---|
| **Multiplexer** | Starts agents in a private tmux server (`tmux -L optimus`), so they keep running after you quit. Live preview of every pane, with a busy / waiting-for-input / idle badge. Attach with `enter`, return with `Alt-q`, cycle agents with `Alt-←/→`. Send a prompt to one agent, or broadcast it to several. |
| **Sessions** | Every past session of every agent, newest first, searchable, with turns, tokens and cost. Read transcripts, resume any session into the multiplexer. |
| **Context transfer** | `h` on any session builds a handoff document (original request, files changed and read, recent commands, the latest conversation within a token budget, and where it left off). You can start a new agent with it (any agent: Claude to Codex works), paste it into a running agent, copy it, or save it. `H` first condenses it with an agent. |
| **Projects** | Sessions grouped by working directory, with the agents used, 7-day and total spend. Start an agent in any project. |
| **Usage & remaining** | Today / week / month / all-time spend, a 14-day chart, breakdowns by model and agent, the current 5-hour block with burn rate and projection, real **5h / 7d quota** windows for Claude and Codex plans, and your own budgets. |

## Agents

| Agent | Runs in multiplexer | History, costs, handoff | Resume |
|---|---|---|---|
| Claude Code | ✓ | ✓ `~/.claude/projects` (incl. sub-agents) | ✓ |
| Codex CLI | ✓ | ✓ `~/.codex/sessions` | ✓ |
| opencode | ✓ | ✓ `~/.local/share/opencode/storage` | ✓ |
| Gemini CLI, cursor-agent | ✓ | receive handoffs | ✓ |
| aider, amp, crush, goose, shell | ✓ | receive handoffs | – |

Run `optimus agents` to see what's detected. Override a binary or add default args in the config file.

## Install

Requires Go 1.25+ and tmux.

```sh
make install          # → ~/.local/bin/optimus
optimus               # open the dashboard
```

### Show real remaining quota (Claude plans)

Claude Code gives rate-limit data (5h and 7d windows) only to status line commands. Make optimus your status line and it records every snapshot:

```jsonc
// ~/.claude/settings.json
"statusLine": { "type": "command", "command": "optimus statusline" }
```

That also gives you a compact status line: `◆ Opus 5.5 · api · session $4.50 · 5h ███░░ 62% ↻1h23m · 7d █░░░░ 18%`. Already have a status line? Keep it by setting `"statusline_chain": "<your command>"` in the optimus config. Optimus still records the data and prints your line. Codex quota is read straight from its session logs, so it needs no setup.

## CLI

Everything in the dashboard is also scriptable:

```sh
optimus new claude ~/dev/api --attach           # start an agent (Alt-q to detach)
optimus ps                                      # running agents + state
optimus send all "run the tests and report"     # broadcast a prompt
optimus peek 2 -n 30                            # look at an agent's screen
optimus ls -p api                               # sessions of a project
optimus show last --tail 20                     # read a transcript
optimus handoff 3f2a --to codex                 # continue a Claude session in Codex
optimus handoff 3f2a --into 2                   # feed it to a running agent
optimus handoff 3f2a --copy --note "focus on the auth bug"
optimus usage --by model --since 30d            # spend report (also --json)
optimus blocks                                  # 5-hour usage blocks
optimus limits                                  # quota windows + budgets
optimus projects
```

`optimus help` lists everything.

## Config

`~/.config/optimus/config.json` (`optimus config init` / `optimus config edit`):

```json
{
  "budgets": { "daily_usd": 50, "weekly_usd": 250, "monthly_usd": 800, "block_usd": 30 },
  "block_hours": 5,
  "handoff_max_tokens": 20000,
  "summarize_with": "claude",
  "agents": {
    "claude": { "args": ["--model", "opus"] },
    "codex":  { "command": "/opt/codex/bin/codex" }
  },
  "pricing": {
    "my-local-model": { "input": 0, "output": 0 }
  }
}
```

**About costs:** prices are API list rates per million tokens (cache reads and writes included, sub-agents counted). On a Claude Max or ChatGPT plan they show the *value* you consumed, not what you were billed. opencode's own reported cost is used for models optimus has no price for. Add or override prices under `pricing`: keys are model-id prefixes.

## How it works

- **Index**: each provider parses its agent's transcripts into sessions with per-hour, per-model token buckets. Results are cached in `~/.cache/optimus/index.gob`, keyed by file size and mtime, so only changed sessions are re-read. `optimus reindex` rebuilds the cache.
- **Multiplexer**: a dedicated tmux server with its own config (`~/.config/optimus/tmux.conf`, generated). Windows carry `@optimus_agent`, `@optimus_cwd` and `@optimus_session` options. Claude sessions get a pre-assigned `--session-id`, so optimus always knows which transcript belongs to which window. The agent state badge is a heuristic based on the bottom of the pane.
- **Handoff**: documents are saved in `~/.local/state/optimus/handoffs/`. The receiving agent gets a one-line prompt telling it to read the file. That avoids argv and paste-size limits and works with every agent.

## Layout

```
cmd/optimus          entry point
internal/providers   claude, codex, opencode parsers + launch-only agents
internal/index       discovery, parse cache, pricing of sessions
internal/usage       aggregation, 5h blocks, budgets, projects
internal/ratelimits  quota windows (Claude status line, Codex rollouts)
internal/mux         tmux backend
internal/handoff     context documents
internal/tui         Bubble Tea dashboard
internal/cli         subcommands
```

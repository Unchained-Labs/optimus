# Agents

| Agent | Binary | History | Launch flags optimus uses | Resume |
|---|---|---|---|---|
| Claude Code | `claude` | `~/.claude/projects` (+ sub-agents) | `--session-id`, `--name`, `--remote-control`, first prompt | `--resume <id>` |
| Codex CLI | `codex` | `~/.codex/sessions` | first prompt | `codex resume <id>` |
| opencode | `opencode` | `~/.local/share/opencode/storage` | `--prompt` | `--session <id>` |
| Gemini CLI | `gemini` | – | `-i <prompt>` | `--resume` |
| cursor-agent | `cursor-agent` | – | first prompt | `--resume <id>` |
| aider, amp, crush, goose | as named | – | prompt typed in after start | – |
| shell | `$SHELL` | – | – | – |

`optimus agents` shows what's installed and how many sessions were found for each.

## Pointing at another binary

```json
{ "agents": { "claude": { "command": "/opt/claude/bin/claude", "args": ["--model", "sonnet"] } } }
```

## How sessions map to windows

Claude sessions started by optimus get a pre-assigned `--session-id`, so their window, transcript and cost are linked exactly. For other agents, optimus matches the newest session of that agent in the window's folder that was active after the window opened.

## Adding an agent

Most agents need a single file in `internal/providers/`. See [Contributing → Adding an agent](../project/contributing.md#adding-an-agent).

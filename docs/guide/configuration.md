# Configuration

`~/.config/optimus/config.json`. Every key is optional.

```sh
optimus config            # show the effective config
optimus config edit       # open in $EDITOR
optimus config set KEY VALUE
```

```json
{
  "default_agent": "claude",
  "remote": {
    "claude_remote_control": true,
    "web_autostart": true,
    "addr": "127.0.0.1:7777"
  },
  "notifications": { "desktop": true, "on_input": true, "on_finish": true, "ntfy": "" },
  "budgets": { "daily_usd": 50, "weekly_usd": 250, "monthly_usd": 800, "block_usd": 30 },
  "block_hours": 5,
  "handoff_max_tokens": 20000,
  "summarize_with": "claude",
  "statusline_chain": "~/bin/my-statusline.sh",
  "agents": {
    "claude": { "args": ["--model", "opus"] },
    "codex": { "command": "/opt/codex/bin/codex" }
  },
  "pricing": {
    "my-local-model": { "input": 0, "output": 0 },
    "claude-opus-5-5": { "input": 4, "output": 20, "cache_read": 0.2 }
  }
}
```

## Keys

| Key | Default | Set with `config set` | Meaning |
|---|---|:-:|---|
| `default_agent` | `claude` | ✓ | agent for `optimus new`, ++shift+n++ and quick start |
| `remote.claude_remote_control` | `true` | ✓ | start Claude sessions with `--remote-control` |
| `remote.web_autostart` | `true` | ✓ | start the web dashboard with the first session |
| `remote.addr` | `127.0.0.1:7777` | ✓ | web dashboard address |
| `notifications.desktop` | `true` | ✓ | desktop notifications |
| `notifications.on_input` / `on_finish` | `true` | ✓ | notify when an agent needs input / finishes a turn |
| `notifications.ntfy` | – | ✓ | ntfy topic URL for phone push |
| `budgets.daily_usd` · `weekly_usd` · `monthly_usd` · `block_usd` | – | ✓ | spend limits shown as bars |
| `block_hours` | `5` | ✓ | usage-block length |
| `handoff_max_tokens` | `20000` | ✓ | handoff document budget |
| `handoff_suggest_pct` | `85` | ✓ | suggest moving sessions to another agent at this quota % (`-1` off) |
| `handoff_suggest_to.<agent>` | `claude→codex`, `codex→claude` | | where to suggest continuing |
| `summarize_with` | `claude` | ✓ | agent used by `--summarize` / ++shift+h++ |
| `statusline_chain` | – | ✓ | your previous status line, run behind optimus |
| `agents.<name>.command` | the agent's usual binary | | use a different binary |
| `agents.<name>.args` | – | | extra arguments on every launch |
| `pricing.<model-prefix>` | built in | | USD per million tokens: `input`, `output`, `cache_read`, `cache_write_5m`, `cache_write_1h` |

## Environment

| Variable | Effect |
|---|---|
| `OPTIMUS_HOME` | put config, state and cache under one directory |
| `OPTIMUS_SOCKET` | use another tmux server (e.g. for tests or demos) |
| `CLAUDE_CONFIG_DIR`, `CODEX_HOME`, `OPENCODE_DATA_DIR` | where each agent keeps its data |
| `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME` | respected for optimus's own files |

## Files

| Path | Contents |
|---|---|
| `~/.config/optimus/config.json` | configuration |
| `~/.config/optimus/tmux.conf` | generated config for optimus's tmux server |
| `~/.local/state/optimus/web-token` | web dashboard access token (0600). Delete it to rotate. |
| `~/.local/state/optimus/ratelimits.json` | last Claude quota snapshot |
| `~/.local/state/optimus/handoffs/` | handoff documents |
| `~/.cache/optimus/index.gob` | session parse cache, safe to delete |

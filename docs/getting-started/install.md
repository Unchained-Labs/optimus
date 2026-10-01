# Install

## One line

```sh
curl -fsSL https://raw.githubusercontent.com/Unchained-Labs/optimus/main/install.sh | sh
```

The installer is a short wizard:

1. **Checks your system:** Linux or macOS, amd64 or arm64, and tmux (it prints the right install command if tmux is missing).
2. **Asks where to install:** default `~/.local/bin`.
3. **Gets the binary:** downloads the release for your platform and verifies its SHA-256 checksum, or builds from source with Go if there's no release for it.
4. **Offers to add the folder to your `PATH`.**
5. **Detects your agents** and asks which one is your default.
6. **Offers to connect the Claude Code status line,** so optimus can show your real 5h / 7d quota.
7. **Asks for optional budgets.**
8. **Confirms remote control:** the web dashboard and Claude Remote Control for new sessions.
9. **Indexes your existing sessions** and shows the last week of spend.

!!! tip "Nothing changes behind your back"
    Without a terminal (CI, provisioning), the installer only edits your shell config with `--yes`, and only touches Claude Code's settings with `--statusline`. Claude Code's `settings.json` is backed up before any change.

## Options

| Flag | Env | Meaning |
|---|---|---|
| `-y`, `--yes` | `OPTIMUS_YES=1` | accept defaults, no questions |
| `--bin-dir DIR` | `OPTIMUS_BIN_DIR` | install location |
| `--version TAG` | `OPTIMUS_VERSION` | a specific release (default: latest) |
| `--from-source` | | build with Go instead of downloading |
| `--statusline` | | set up the Claude Code status line without asking |
| `--no-setup` | | install the binary only |
| `--repo OWNER/NAME` | `OPTIMUS_REPO` | install from a fork |

```sh
# unattended, e.g. in a dotfiles script
curl -fsSL https://raw.githubusercontent.com/Unchained-Labs/optimus/main/install.sh | sh -s -- --yes --statusline
```

## From source

```sh
git clone https://github.com/Unchained-Labs/optimus
cd optimus
make install        # → ~/.local/bin/optimus
```

## Requirements

| | |
|---|---|
| OS | Linux or macOS |
| tmux | 3.0+, needed for the multiplexer. Sessions and usage work without it. |
| Go | 1.25+, only when building from source |
| Agents | any of Claude Code, Codex, opencode, Gemini CLI, cursor-agent, aider, amp, crush, goose |

## Uninstall

```sh
rm ~/.local/bin/optimus
rm -rf ~/.config/optimus ~/.local/state/optimus ~/.cache/optimus
tmux -L optimus kill-server     # stops agents started by optimus
```

If you set up the status line, restore `~/.claude/settings.json.bak-optimus`.

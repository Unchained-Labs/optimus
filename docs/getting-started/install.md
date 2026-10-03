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

## Where it goes

optimus is a single self-contained binary of about 3.5 MB, with no runtime, libraries or `/opt` bundle. It goes in a `bin` folder on your `PATH`:

| Situation | Location |
|---|---|
| default | `~/.local/bin/optimus` (just for you, no sudo) |
| `/usr/local/bin` is writable and `~/.local/bin` doesn't exist | `/usr/local/bin/optimus` |
| interactive run | the wizard asks, with one of the above pre-filled |
| `--bin-dir DIR` | wherever you say |

If that folder isn't on your `PATH`, the wizard offers to add it to `~/.zshrc`, `~/.bashrc`, fish or `~/.profile`. Open a new shell (or run `hash -r`) and `optimus` works from anywhere.

### Machine-wide

```sh
curl -fsSL https://raw.githubusercontent.com/Unchained-Labs/optimus/main/install.sh | sudo sh -s -- --bin-dir /usr/local/bin --no-setup
```

Keep `--no-setup` with `sudo`, so the setup questions don't configure root's account. Each user's config is created on first run.

## Binary or source?

The installer **downloads the prebuilt binary** for your platform (Linux or macOS, amd64 or arm64) from the [latest release](https://github.com/Unchained-Labs/optimus/releases) and checks its SHA-256 against the release's `checksums.txt`. It only builds from source, which needs Go 1.25+, when:

- you pass `--from-source`,
- no binary exists for your platform, or
- you run `sh install.sh` from inside a git checkout (it builds that checkout).

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

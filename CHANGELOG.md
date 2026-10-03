# Changelog

All notable changes to this project are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Reliable agent states from the agents themselves: Claude Code sessions get lifecycle hooks via `--settings`, Codex gets a `notify` program; *needs input* now shows the actual question ("wants to use Write: hello.txt").
- Notifications when an agent needs input or finishes: desktop, multiplexer status line, browser, and optional phone push via ntfy (`optimus watch`, `notifications.*` config).
- Answer an agent without attaching: `!` in the terminal dashboard shows its question and forwards the next key; `optimus answer <window> <key>`.
- One git worktree per agent (`--worktree`, `-w`, or the launch prompt), with changes shown per agent, and diff / merge / discard from the CLI (`optimus wt`), both dashboards and the API.
- Fan-out: the same task to several agents, each in its own worktree (`optimus fanout`, `F`, browser), and `optimus compare`.
- Preview and edit a handoff before it's sent (`--edit`, "✎ preview & edit first" in the TUI, "Preview & edit" in the browser).
- Quota-aware handoff: when an agent's quota window passes `handoff_suggest_pct` (default 85%), optimus suggests continuing its running sessions in another agent (notification, `C` in the TUI, a banner in the browser).
- Phone: `optimus web --url` prints a QR code of the login link (Tailscale addresses first); a 📱 button shows it in the dashboard.
- The web dashboard is an installable app (PWA: manifest, icons, service worker) — Add to Home Screen over HTTPS, e.g. with `tailscale serve`.
- Fleet summary everywhere: `optimus fleet` (`--json`, `--tmux`), appended to the Claude Code status line and shown in the multiplexer's status bar.
- Automated sessions (`claude -p`, Agent SDK, `codex exec`, cron jobs) are detected and hidden from session lists by default (`ls --all`, `z`, browser checkbox); they still count in costs.
- Full-text search inside transcripts: `optimus search`, `S` in the Sessions view, "inside transcripts" in the browser, with the matching passage.
- Command palette: `Ctrl-K` / `:` in the terminal dashboard and `Ctrl/Cmd-K` in the browser — agents, recent sessions, launches per project and every action, matched by words in any order.
- Jump to the next agent waiting for you: `Alt-n` anywhere in the multiplexer, `i` in the dashboards, `optimus next`.

### Fixed
- Claude's workspace-trust dialog was shown as busy; it's now *needs input*.

## [0.1.0] - 2026-10-03

First public release.

### Added
- Terminal dashboard (`optimus`) with Agents, Sessions, Projects and Usage views.
- Agent multiplexer on a private tmux server: start, attach (Alt-q back), watch, broadcast, rename and stop agents; busy / needs-input / idle detection.
- Session history, costs and transcripts for Claude Code (including sub-agents), Codex CLI and opencode; launch support for Gemini CLI, cursor-agent, aider, amp, crush, goose and a plain shell.
- Context handoff between sessions and across agents: to a new agent, into a running one, to the clipboard or a file, optionally condensed by an agent.
- Usage: daily/weekly/monthly spend, 5-hour blocks with burn rate, budgets, per model/agent/project breakdowns, and real 5h/7d quota for Claude (via `optimus statusline`) and Codex.
- Browser dashboard (`optimus web`): live interactive terminals over websockets, message box with broadcast, phone-friendly key buttons, sessions, handoffs and usage; token auth.
- Remote control by default: launching a session brings up the web dashboard; Claude sessions start with `--remote-control`.
- One-step launch: `optimus claude|codex|… [dir]`, `optimus new` with a default agent, `N` in the TUI, New session dialog on the web.
- One-line installer with a setup wizard, release builds for Linux and macOS (amd64/arm64), CI on Linux and macOS.
- Documentation site on GitHub Pages.

### Fixed
- Agents not listed (and the web terminal returning 404) on tmux 3.3+, which escapes control characters in format output.

[Unreleased]: https://github.com/Unchained-Labs/optimus/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Unchained-Labs/optimus/releases/tag/v0.1.0

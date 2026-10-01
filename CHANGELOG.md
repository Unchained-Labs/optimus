# Changelog

All notable changes to this project are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Fixed
- Agents not listed (and the web terminal returning 404) on tmux 3.3+, which escapes control characters in format output.

## [0.1.0]

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

[Unreleased]: https://github.com/Unchained-Labs/optimus/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Unchained-Labs/optimus/releases/tag/v0.1.0

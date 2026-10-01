# Security Policy

optimus can type into your coding agents, and its web dashboard exposes that over HTTP, so we take security reports seriously.

## Supported versions

Security fixes go into the latest release. Please upgrade before reporting:

```sh
curl -fsSL https://raw.githubusercontent.com/Unchained-Labs/optimus/main/install.sh | sh
```

## Reporting a vulnerability

**Please do not open a public issue.** Report privately through GitHub's [private vulnerability reporting](https://github.com/Unchained-Labs/optimus/security/advisories/new) with:

- a description of the issue and its impact,
- steps to reproduce (a proof of concept if you have one),
- the optimus version (`optimus version`) and OS.

You can expect an acknowledgement within 3 working days and a status update within 10. We'll credit you in the release notes unless you'd rather stay anonymous.

## Security model

Worth knowing when you assess a report:

- **Web dashboard**: listens on `127.0.0.1:7777` by default. Every API call and terminal requires the access token stored in `~/.local/state/optimus/web-token` (file mode 0600), sent as an `HttpOnly`, `SameSite=Strict` cookie or a bearer header. State-changing requests and websocket upgrades must be same-origin. Anyone who has the token can type into your agents — treat it like an SSH key. Delete the file to rotate it.
- **Network exposure**: binding to a non-loopback address (`--addr 0.0.0.0:7777`) is opt-in and prints a warning. Use a private network (Tailscale, WireGuard); there is no built-in TLS.
- **Remote keys**: the key buttons only accept a fixed allow-list of tmux key names; free text goes through tmux paste buffers, never a shell.
- **Agent data**: optimus reads agent histories under your home directory and never sends them anywhere. Handoff documents are written to `~/.local/state/optimus/handoffs/` and only passed to agents you choose.
- **Config edits**: `optimus config statusline --install` backs up `~/.claude/settings.json` before changing it. The installer never edits your files unattended unless you pass `--yes` / `--statusline`.

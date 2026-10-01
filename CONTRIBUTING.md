# Contributing to optimus

Thanks for helping make optimus better. Bug reports, agent integrations, docs and code are all welcome.

## Before you start

- **Bugs**: search [existing issues](https://github.com/Unchained-Labs/optimus/issues) first, then open one with the bug template. Include `optimus version`, `optimus agents` output, your OS and `tmux -V`.
- **Features**: open an issue to discuss the idea before writing a large change, so we can agree on the shape first.
- **Security issues**: don't open a public issue — see [SECURITY.md](https://github.com/Unchained-Labs/optimus/blob/main/SECURITY.md).

## Development setup

You need Go (version in `go.mod`), tmux, and optionally `shellcheck`.

```sh
git clone https://github.com/Unchained-Labs/optimus && cd optimus
make build          # → bin/optimus
make test           # go test ./...
make lint           # gofmt, go vet, shellcheck
./bin/optimus       # run the TUI against your real sessions (read-only)
```

Optimus only *reads* agent histories, but it does start real agents when you press `n`. To try the UI without touching your own sessions or spending tokens, use the synthetic environment the demo is recorded with:

```sh
DEMO_HOME=$PWD/demo/.home bash -c 'source demo/setup.sh && exec bash'
optimus          # TUI on fake sessions and mock agents
optimus web      # same, in the browser
```

It swaps `$HOME` for a seeded fake one and runs on a separate tmux socket (`OPTIMUS_SOCKET=optimus-demo`).

## Project layout

| Path | What lives there |
|---|---|
| `internal/providers` | One file per agent: where its sessions live, how to parse them, how to launch/resume it |
| `internal/index` | Discovery, parse cache, matching windows to transcripts |
| `internal/usage`, `internal/pricing` | Aggregation, 5-hour blocks, budgets, model prices |
| `internal/mux` | The tmux backend (windows, capture, send, browser views) |
| `internal/handoff` | Context documents for moving work between sessions |
| `internal/tui` | Bubble Tea terminal dashboard |
| `internal/web` | Browser dashboard: JSON API, auth, websocket terminals, static UI |
| `internal/cli` | Subcommands |
| `demo/` | Demo recording: VHS tape, browser recorder, synthetic data, mock agents |

## Adding an agent

1. Create `internal/providers/<agent>.go` implementing `Provider` (see `codex.go` for a compact example). Launch-only agents can be a one-line `Generic` registration in `generic.go`.
2. Parse usage into hourly `Bucket`s; never double count streamed or repeated usage events.
3. Add a test with a small synthetic fixture in `providers_test.go` — never commit real transcripts.
4. Add model prices to `internal/pricing` if the agent uses models optimus doesn't know.
5. Update the agent table in the README.

## Pull requests

- Keep changes focused; one topic per PR.
- `make test lint` must pass. CI runs the same checks on Linux and macOS, plus the installer.
- Add or update tests for behaviour changes. UI changes: include a screenshot or a short clip.
- Use clear commit messages in the imperative ("Add Gemini session parser").
- By contributing you agree your work is released under the [MIT License](https://github.com/Unchained-Labs/optimus/blob/main/LICENSE).

## Code of Conduct

This project follows the [Contributor Covenant](https://github.com/Unchained-Labs/optimus/blob/main/CODE_OF_CONDUCT.md). By participating you agree to uphold it.

# FAQ

??? question "Does optimus replace tmux or my terminal?"
    No. It runs its own tmux server (`tmux -L optimus`) next to yours, and works fine from inside an existing tmux session. Your own tmux config and sessions are untouched.

??? question "Does it send my code or transcripts anywhere?"
    No. optimus only reads agent histories from your disk. The only network traffic is the web dashboard you start yourself and, with `--summarize`, the agent you choose to condense a handoff.

??? question "Why do the costs differ from my bill?"
    On a Claude or ChatGPT plan you aren't billed per token. optimus shows what the same traffic would cost at API list prices, a measure of how much you're getting out of the plan. Override prices under `pricing` in the [config](guide/configuration.md).

??? question "Can I use it with agents I started outside optimus?"
    Yes. Their history, cost and transcripts appear like any other session. Agents running in your own tmux are linked in place (state, live terminal, messages, notifications); agents in a plain terminal tab can be taken over, which stops them there and resumes the same session inside optimus. See [Agents outside optimus](guide/outside.md).

??? question "All my agents disappeared at once. Why?"
    Usually the out-of-memory killer. It ends a whole cgroup, and agents used to share one with the tmux server. On Linux with a systemd user session, optimus now starts each agent in its own `optimus-agent-*.scope`, so an OOM kill ends only the agent that grew too big. The kernel log (`journalctl -k`) records each kill. (`OPTIMUS_NO_SCOPE=1` turns scopes off.)

??? question "How do I see the options of one command?"
    `optimus <command> -h` (or `optimus help <command>`) prints its usage and flags; `optimus help` lists every command.

??? question "Is it safe to open the web dashboard from my phone?"
    Over a private network, yes. Use Tailscale, WireGuard or an SSH tunnel and keep the token private. Anyone with the token can type into your agents, so never expose the port to the internet. See [Security](reference/security.md).

??? question "Does the installer compile anything?"
    No. It downloads the prebuilt, checksum-verified binary for your platform. It only builds from source with `--from-source`, when no binary exists for your platform, or when run from a git checkout. See [Install](getting-started/install.md).

??? question "Where is the binary installed, and how do I uninstall?"
    `~/.local/bin/optimus` by default (or `/usr/local/bin` when that's writable and `~/.local/bin` doesn't exist). Pass `--bin-dir` to choose. To uninstall, see [Install → Uninstall](getting-started/install.md#uninstall).

??? question "Which agents are supported?"
    Claude Code, Codex and opencode with full history, cost and transcripts; Gemini CLI and cursor-agent with resume; aider, amp, crush, goose and any shell as launch-only. See [Agents](reference/agents.md), or [add one](project/contributing.md#adding-an-agent).

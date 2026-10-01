---
title: The tmux of AI coding agents
hide:
  - navigation
  - toc
---

<div class="op-hero" markdown>

<span class="op-badge">OPTIMUS</span>

# The tmux of *AI coding agents*

<p class="lead">Run a fleet of Claude Code, Codex, opencode and other agent sessions side by side. Drive them from your terminal, your browser or your phone, see what they cost and how much quota is left, and move context between sessions, even across agents.</p>

[Get started](getting-started/install.md){ .md-button .md-button--primary }
[See it in action](#demo){ .md-button }
[GitHub](https://github.com/Unchained-Labs/optimus){ .md-button }

<div class="op-install" markdown>

```sh
curl -fsSL https://raw.githubusercontent.com/Unchained-Labs/optimus/main/install.sh | sh
```

</div>

<div class="op-shot">
<img src="images/web-fleet.png" alt="optimus web dashboard: a fleet of agents with a live terminal">
</div>

</div>

## Everything about your agents, in one place

<div class="grid cards" markdown>

-   :material-view-dashboard-variant:{ .lg .middle } __One fleet, every agent__

    ---

    Start Claude Code, Codex, opencode, Gemini, cursor-agent, aider and more in any project. Each one runs in optimus's own tmux server and keeps running after you close the UI.

    [:octicons-arrow-right-24: Terminal dashboard](guide/terminal.md)

-   :material-cellphone-link:{ .lg .middle } __Terminal, browser, phone__

    ---

    The same fleet in a keyboard-driven terminal UI and a web dashboard with live interactive terminals and one-tap keys for answering prompts from your phone.

    [:octicons-arrow-right-24: Browser & phone](guide/web.md)

-   :material-transfer:{ .lg .middle } __Context that travels__

    ---

    Hand a session's goal, changed files, commands and recent conversation to a new agent of any kind, or to one that's already running. Claude → Codex in one keystroke.

    [:octicons-arrow-right-24: Sessions & handoff](guide/sessions.md)

-   :material-chart-timeline-variant:{ .lg .middle } __Costs & quota__

    ---

    Daily and weekly spend, the current 5-hour block with burn rate, real Claude and Codex 5h / 7d quota, and budgets, so limits stop being a surprise.

    [:octicons-arrow-right-24: Costs & quota](guide/usage.md)

-   :material-remote:{ .lg .middle } __Remote control by default__

    ---

    New sessions bring up the web dashboard, and Claude sessions start with Claude Code's own Remote Control, so they appear in the Claude apps too.

    [:octicons-arrow-right-24: Configuration](guide/configuration.md)

-   :material-console:{ .lg .middle } __Scriptable__

    ---

    Everything is a command: `optimus claude`, `optimus send all "…"`, `optimus handoff 3f2a --to codex`, `optimus usage --json`.

    [:octicons-arrow-right-24: CLI reference](reference/cli.md)

</div>

## Demo { #demo }

<div class="op-shot">
<video src="assets/demo/optimus.mp4" poster="images/tui-agents.png" controls muted playsinline preload="none"></video>
</div>

Recorded with mock agents on synthetic data. No real sessions and no tokens were used.

## Supported agents

| Agent | Run in optimus | History, cost, transcripts | Resume | Remote control |
|---|:-:|:-:|:-:|:-:|
| Claude Code | :material-check: | :material-check: incl. sub-agents | :material-check: | web + Claude apps |
| Codex CLI | :material-check: | :material-check: incl. quota | :material-check: | web |
| opencode | :material-check: | :material-check: | :material-check: | web |
| Gemini CLI, cursor-agent | :material-check: | receives handoffs | :material-check: | web |
| aider, amp, crush, goose, any shell | :material-check: | receives handoffs | – | web |

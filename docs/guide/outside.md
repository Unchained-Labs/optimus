# Agents outside optimus

Agents you started before optimus, or in another terminal, show up in optimus too. What optimus can do with each depends on where it runs:

| Where it runs | What optimus does |
|---|---|
| **your own tmux** (any tmux server that isn't optimus's) | **links it in place**: it appears in the fleet marked ↗ *your tmux*, with its state, notifications, live preview and browser terminal; you can send it messages and answer its prompts. optimus doesn't stop or rename it. |
| **a terminal tab** (no tmux) | its terminal belongs to that tab, so it can't be shown live; optimus lists it and can **take it over**. |
| **a container** (e.g. a dev container) | listed for information only: its session lives inside the container. |

```sh
optimus ps          # the "Outside optimus" section says which is which
```

## Take over

Taking over moves an agent into optimus without losing the conversation:

1. optimus asks the process to exit (SIGTERM; it never force-kills),
2. waits for it to finish writing its session,
3. resumes the **same session** inside optimus (`claude --resume`, `codex resume`, …) with hooks, notifications, the browser terminal and Claude Remote Control.

| Where | How |
|---|---|
| terminal dashboard | ++shift+t++: the selected linked agent, or pick one from *Outside optimus* |
| browser | **Take over** in the *Outside optimus* list, or on a linked agent's pane |
| shell | `optimus takeover <pid or session id>` (`--attach` to jump in) |

If the agent is in the middle of a turn, optimus refuses unless you confirm (`--force`), because stopping it would cut that turn short. The terminal tab it ran in simply shows that the agent exited.

!!! note
    optimus recognizes Claude Code, Codex, opencode, Gemini CLI, cursor-agent, aider, amp, crush and goose by their process. Resuming needs an agent with resumable sessions (Claude Code, Codex, opencode) and a session optimus can identify; Claude Code always says which one it is.

## Launching from inside an agent

If you run `optimus` from inside a Claude Code session (for example from its shell tool), the agents optimus starts are still independent sessions: optimus removes the variables that would make Claude Code treat them as its children, which would turn their transcripts off.

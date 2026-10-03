# Attention & notifications

With several agents running, the question is always *which one needs me now?* optimus answers it in three ways: reliable states, notifications, and a key that jumps to the next agent waiting for you.

## Reliable states from the agents themselves

Agents started by optimus report their own lifecycle, so *needs input* doesn't depend on reading the screen:

| Agent | How | What it reports |
|---|---|---|
| Claude Code | hooks passed with `--settings` on launch (your `settings.json` isn't touched; your own hooks keep running) | prompt submitted / tool use → **busy** · permission request → **needs input**, with the tool and target ("wants to use Write: hello.txt") · turn finished → **idle** · session ended |
| Codex | `-c notify=…` on launch | turn finished → **idle**, with the first line of its answer |
| others | screen heuristic | busy / input / idle from the bottom of the screen |

When there's no report yet (for example Claude's workspace-trust dialog, which comes before any hook), or a reported *busy* goes stale because a turn was interrupted, optimus falls back to the screen.

`optimus ps` shows each agent's state and what it's asking.

## Notifications

When an agent **needs input** or **finishes a turn**, optimus:

- shows a desktop notification (`notify-send` on Linux, Notification Center on macOS),
- flashes a message in the status line of the optimus multiplexer (and marks the window with ◆),
- notifies the browser dashboard (tab title count, in-page toast, and a system notification once you click 🔔 over HTTPS or localhost),
- optionally pushes to your phone through [ntfy](https://ntfy.sh).

The notifier runs inside the web dashboard and the terminal dashboard, and as `optimus watch` if you want it on its own. A lock makes sure only one runs, so nothing is notified twice.

```sh
optimus config set notifications.on_finish false   # only tell me when I'm needed
optimus config set notifications.desktop false     # no desktop popups
optimus config set notifications.ntfy https://ntfy.sh/<a-long-secret-topic>
```

!!! warning "ntfy topics are public by name"
    Anyone who knows an ntfy.sh topic can read it, and the notification body includes the agent's question (a command or file name). Use a long random topic, or a self-hosted ntfy server with access control.

## Jump to the next agent that needs you

| Where | Key |
|---|---|
| anywhere inside the multiplexer, even while attached to an agent | ++alt+n++ |
| terminal dashboard | ++i++ |
| browser | ++i++ or the red **◆ waiting** button |
| shell | `optimus next` lists them, longest waiting first |

Pressing it again cycles through all waiting agents. Answer, ++alt+n++, answer: that's the whole loop.

## Answer without attaching

Most interruptions are a single keystroke: approve a command, pick option 1. You don't need to attach for that:

| Where | How |
|---|---|
| terminal dashboard | select the agent (++i++ jumps to the next waiting one), press ++exclamation++. A box shows its question; the next key (++1++ ++2++ ++3++ ++y++ ++n++ ++enter++ ++tab++) goes straight to the agent, ++up++ / ++down++ move through its menu, ++esc++ closes. |
| browser / phone | the key buttons under the terminal (Esc, ↵, ↑ ↓, Tab, 1 2 3, y n, ^C) |
| shell | `optimus answer 2 1`, `optimus answer web-login y`, `optimus answer 3 Down Enter` |

The full loop is: ++i++, ++exclamation++, ++1++, ++i++ … with no attaching anywhere.

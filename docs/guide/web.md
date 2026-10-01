# Browser & phone

```sh
optimus web --open
```

![Web dashboard](../images/web-fleet.png)

The web dashboard drives the same agents as the terminal UI:

- **Fleet:** every running agent with its state, plus a **live, interactive terminal** for the one you select. Type straight into it, or use the message box (++enter++ sends, ++shift+enter++ adds a new line; tick **all** to broadcast).
- **Keys:** one-tap buttons for Esc, ↵, arrows, Tab, 1/2/3, y/n and ^C, for answering an agent's questions without a keyboard.
- **Sessions:** search, filter by agent or project, read transcripts, **Resume** and **Handoff**.
- **Usage:** spend, quota, the current block, budgets, a 30-day chart, and breakdowns by model, agent and project.
- **＋ New session** (or ++n++): agent, project, first message, name, and Claude Remote Control. **Quick start** in the sidebar starts your default agent in a recent project with one click.

<div class="op-row" markdown>
![New session](../images/web-new-session.png)
![Handoff](../images/web-handoff.png)
</div>

## On your phone

<div class="op-row" markdown>
<div class="op-phone" markdown>![Phone: fleet](../images/mobile-fleet.png)</div>
<div class="op-phone" markdown>![Phone: usage](../images/mobile-usage.png)</div>
</div>

The layout adapts to small screens: agents become a swipeable row, the terminal fills the screen, and navigation moves to the bottom.

To reach it from your phone, put both devices on a private network and listen on that interface:

```sh
optimus web --addr 100.101.102.103:7777   # your machine's Tailscale IP
optimus web --url                          # open this link on the phone once
```

!!! danger "Don't expose it to the internet"
    Anyone with the access token can type into your agents. Use Tailscale, WireGuard or an SSH tunnel. `--addr 0.0.0.0:7777` works on a trusted LAN and prints a warning. There is no built-in TLS.

## Always on

By default, starting or resuming a session also starts the dashboard in the background (`remote.web_autostart`), so remote control is ready whenever an agent is running. Manage it with:

```sh
optimus web --bg       # start in the background now
optimus web --url      # print the login link
optimus web --stop     # stop the background dashboard
```

## Claude Remote Control

Claude sessions started or resumed by optimus also get Claude Code's own `--remote-control=<name>`, so they appear in the Claude desktop, web and mobile apps. Turn it off globally with `optimus config set remote.claude_remote_control false`, or per session in the **New session** dialog.

## How the terminals work

Each browser terminal is a private tmux session *grouped* with the agents' session: it shares the windows but has its own current window and size. It's attached through a pty and streamed to [xterm.js](https://xtermjs.org) over a websocket. Watching an agent from your phone doesn't move the window you're looking at on your laptop, and closing the tab removes the view, never the agent.

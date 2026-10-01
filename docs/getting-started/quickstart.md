# Quick start

## 1. Open the dashboard

```sh
optimus
```

You land on **Agents**. Your existing sessions are already indexed: press ++2++ for **Sessions**, ++4++ for **Usage**.

## 2. Start an agent

=== "One command"

    ```sh
    optimus claude            # Claude Code, in the current folder, attached
    optimus codex ~/dev/web   # any agent, any folder
    ```

=== "From the dashboard"

    Press ++shift+n++ to start your default agent in the project you're looking at, or ++n++ to pick the agent and folder.

=== "From the browser"

    `optimus web --open`, then **＋ New session**: choose the agent and project, and optionally a first message.

You're now inside the agent. Press ++alt+q++ to go back to the dashboard. The agent keeps running.

## 3. Watch and steer the fleet

| | |
|---|---|
| ++enter++ | attach to the selected agent |
| ++s++ | send it a prompt without attaching |
| ++space++ then ++b++ | mark agents, broadcast one prompt to them |
| ++alt+left++ / ++alt+right++ | while attached: switch agents |

Agents show **busy**, **input** (waiting for you) or **idle**, so you can see which one needs attention.

## 4. Take it to your phone

```sh
optimus web --url        # login link with your access token
```

Open it in a browser. To use it from your phone, listen on a private network such as Tailscale: `optimus web --addr 100.x.y.z:7777`. See [Browser & phone](../guide/web.md).

## 5. Move context between agents

In **Sessions**, select a session and press ++h++, then choose **new codex session**. Codex starts in the same project with a document describing what was done and where it left off. See [Sessions & handoff](../guide/sessions.md).

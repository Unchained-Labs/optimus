#!/usr/bin/env python3
"""A stand-in coding agent for the optimus demo recording.

It looks and behaves roughly like an agent TUI, enough to show off optimus's
live previews, state badges, prompts and handoffs, without calling any model.

usage: fake-agent.py FLAVOR [args...]
  FLAVOR   claude | codex | opencode
  OPTIMUS_DEMO_SCENARIO  busy | approval | idle   (default: by project dir)
  a trailing positional (or --prompt X) is treated as the first prompt
"""
import os
import random
import select
import sys
import termios
import time

FLAVOR = sys.argv[1]
ARGS = sys.argv[2:]
SCENARIO = os.environ.get("OPTIMUS_DEMO_SCENARIO") or {"api": "busy", "web": "approval"}.get(os.path.basename(os.getcwd()), "idle")
CWD = os.getcwd()
PROJ = os.path.basename(CWD)
HOME = os.path.expanduser("~")

R = "\x1b[0m"
B = "\x1b[1m"
DIM = "\x1b[2m"
ACC = {"claude": "\x1b[38;5;209m", "codex": "\x1b[38;5;114m", "opencode": "\x1b[38;5;111m"}[FLAVOR]
GRN = "\x1b[38;5;114m"
YEL = "\x1b[38;5;221m"
SPIN = {"claude": "✢✳✶✻✽✻✶✳", "codex": "◐◓◑◒", "opencode": "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"}[FLAVOR]


def out(s=""):
    sys.stdout.write(s + "\n")
    sys.stdout.flush()


def short(p):
    return p.replace(HOME, "~")


def header():
    if FLAVOR == "claude":
        out(f"{ACC}╭──────────────────────────────────────────────╮{R}")
        out(f"{ACC}│{R} {ACC}✻{R} {B}Claude Code{R} {DIM}(demo mock){R}                     {ACC}│{R}")
        out(f"{ACC}│{R}   {DIM}cwd: {short(CWD):<39}{R}{ACC}│{R}")
        out(f"{ACC}╰──────────────────────────────────────────────╯{R}")
    elif FLAVOR == "codex":
        out(f"{B}>_ Codex{R} {DIM}(demo mock) · model: gpt-5-codex{R}")
        out(f"{DIM}   directory: {short(CWD)}{R}")
    else:
        out(f"{ACC}{B}█▀▀ █▀█ █▀▀ █▄ █ █▀▀ █▀█ █▀▄ █▀▀{R}")
        out(f"{ACC}{B}█▄▄ █▄█ █▀  █ ▀█ █▄▄ █▄█ █▄▀ ██▄{R}  {DIM}demo mock · {short(CWD)}{R}")
    out()


def user_line(text):
    out(f"{B}>{R} {text}")
    out()


def tool(name, target):
    if FLAVOR == "claude":
        out(f"{GRN}●{R} {B}{name}{R}({target})")
        out(f"  {DIM}⎿  {random.choice(['Read 118 lines', 'Updated with 12 additions', 'Done', 'Found 4 matches'])}{R}")
    elif FLAVOR == "codex":
        out(f"{DIM}•{R} {'Ran' if name == 'Bash' else 'Edited' if name in ('Edit', 'Update') else 'Explored'} {B}{target}{R}")
    else:
        out(f"{ACC}┃{R} {name.lower():<6} {DIM}{target}{R}")


def say(text):
    prefix = {"claude": f"{ACC}●{R} ", "codex": "", "opencode": f"{ACC}┃{R} "}[FLAVOR]
    for line in text.split("\n"):
        out(prefix + line)
    out()


def spinner_line(i, label, secs):
    c = SPIN[i % len(SPIN)]
    if FLAVOR == "codex":
        return f"{ACC}{c}{R} {B}Working{R} {DIM}({secs}s • esc to interrupt){R}"
    if FLAVOR == "claude":
        return f"{ACC}{c} {label}…{R} {DIM}({secs}s · esc to interrupt){R}"
    return f"{ACC}{c}{R} {label}… {DIM}esc to interrupt{R}"


def work(steps, label="Working", pace=1.1):
    """Show a spinner at the bottom while tool steps scroll by."""
    t0 = time.time()
    i = 0
    for name, target in steps:
        end = time.time() + pace
        while time.time() < end:
            sys.stdout.write("\r\x1b[K" + spinner_line(i, label, int(time.time() - t0)))
            sys.stdout.flush()
            poll_input(0.12)
            i += 1
        sys.stdout.write("\r\x1b[K")
        tool(name, target)
    sys.stdout.write("\r\x1b[K")
    sys.stdout.flush()


PENDING = []
_buf = b""


def poll_input(timeout):
    """Collect complete input lines without blocking the animation."""
    global _buf
    r, _, _ = select.select([sys.stdin], [], [], timeout)
    if r:
        chunk = os.read(sys.stdin.fileno(), 4096)
        if not chunk:
            time.sleep(timeout)
            return
        _buf += chunk
        while b"\n" in _buf:
            line, _buf = _buf.split(b"\n", 1)
            line = line.decode(errors="replace").strip()
            if line:
                PENDING.append(line)


def next_line():
    while not PENDING:
        poll_input(1.0)
    return PENDING.pop(0)


def prompt():
    sys.stdout.write(f"{ACC}{B}›{R} ")
    sys.stdout.flush()


PROJECT_STEPS = {
    "api": [("Read", "src/middleware/rateLimit.ts"), ("Bash", "npm test -- --runInBand"), ("Read", "test/rateLimit.test.ts")],
    "web": [("Bash", "pnpm test"), ("Read", "app/login/page.tsx")],
    "infra": [("Bash", "terraform validate"), ("Read", "terraform/redis.tf")],
    "ml-pipeline": [("Bash", "python -m pytest -q"), ("Read", "pipeline/features.py")],
}
TEST_RESULTS = {
    "api": "All 142 tests pass (3.8s). The new rate-limit suite covers\nburst, refill and per-key isolation.",
    "web": "118 passed, 1 failed: `login › passkey fallback` expects the old\nbutton label. One-line fix in app/login/page.tsx — want me to apply it?",
    "infra": "`terraform validate` is clean and the policy tests pass (24/24).",
    "ml-pipeline": "63 passed in 9.2s. No failures; the new schema check is covered.",
}


def answer(text):
    lower = text.lower()
    steps = PROJECT_STEPS.get(PROJ, [("Read", "README.md")])
    if "handoff" in lower and ".md" in lower:
        path = next((w for w in text.split() if w.endswith(".md")), "handoff.md")
        work([("Read", short(path)), ("Read", "src/middleware/rateLimit.ts"), ("Bash", "git log --oneline -5")], "Reading context")
        say("Picked up the handoff from the Claude session “Rate limiting for the\n"
            "public API”. Where it stands:\n"
            " • token-bucket middleware per API key, Redis-backed, 429 + Retry-After\n"
            " • X-RateLimit-* headers documented in docs/api.md\n"
            " • last ask: the flaky burst test — fixed with the fake clock\n"
            "Next I'll add the per-plan limits you mentioned. Starting now.")
        return
    if "test" in lower:
        work(steps, "Running tests")
        say(TEST_RESULTS.get(PROJ, "All tests pass."))
        return
    work(steps[:2], "Thinking")
    say("Done. " + random.choice(["Changes are staged — nothing pushed yet.", "Let me know if you want a PR."]))


def main():
    header()
    first = None
    if "--prompt" in ARGS:
        first = ARGS[ARGS.index("--prompt") + 1]
    elif ARGS and not ARGS[-1].startswith("-") and (len(ARGS) < 2 or ARGS[-2] != "--session-id"):
        first = ARGS[-1]

    if first:
        user_line(first)
        answer(first)
    elif SCENARIO == "busy":
        user_line("add per-plan limits: free 60/min, pro 600/min, enterprise custom")
        tool("Read", "src/middleware/rateLimit.ts")
        tool("Read", "src/routes/keys.ts")
        loop = [("Edit", "src/middleware/rateLimit.ts"), ("Read", "src/plans.ts"), ("Edit", "src/plans.ts"),
                ("Bash", "npm test -- rateLimit"), ("Edit", "test/rateLimit.test.ts"), ("Grep", "planTier")]
        work(loop * 4, "Implementing", pace=1.6)
        say("Per-plan limits are in: plans.ts maps tier → bucket size, enterprise\nreads its limit from the key record. 9 new tests, all green.")
    elif SCENARIO == "approval":
        user_line("fix the passkey fallback test and run the e2e suite")
        tool("Edit", "app/login/page.tsx")
        tool("Bash", "pnpm test login")
        out()
        out(f"{YEL}{B}Allow command?{R}  {B}pnpm exec playwright test --project=chromium{R}")
        out(f"  {B}› 1. Yes{R}   2. Always   3. No, and tell Codex what to do differently")
        out()
        user_line(next_line())
        work([("Bash", "pnpm exec playwright test --project=chromium")], "Running e2e")
        say("E2E: 37 passed, 0 failed.")
    else:
        user_line({"infra": "plan the redis upgrade for staging", "ml-pipeline": "retrain with the fixed features"}.get(PROJ, "review the last change"))
        tool("Read", PROJECT_STEPS.get(PROJ, [("Read", "README.md")])[-1][1])
        say({"infra": "Plan: 1 to change, 0 to destroy. Staging redis goes 7.0 → 7.2\nwith a rolling restart; no downtime expected.",
             "ml-pipeline": "Retrained: AUC 0.905 on the holdout (was 0.84). Model saved to\nartifacts/churn-xgb-v14."}.get(PROJ, "Looks good."))

    while True:
        if not PENDING:
            prompt()
        line = next_line()
        sys.stdout.write("\r\x1b[K")
        user_line(line)
        answer(line)


# echo is ours to draw: typed/pasted text shows up as a "> prompt" line
fd = sys.stdin.fileno()
saved = termios.tcgetattr(fd)
attrs = termios.tcgetattr(fd)
attrs[3] &= ~termios.ECHO
termios.tcsetattr(fd, termios.TCSANOW, attrs)
try:
    main()
except KeyboardInterrupt:
    pass
finally:
    termios.tcsetattr(fd, termios.TCSANOW, saved)

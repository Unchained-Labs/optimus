#!/usr/bin/env python3
"""Build a synthetic $HOME for recording the optimus demo.

Writes fake Claude Code / Codex / opencode histories (timestamps relative to
now), an optimus config pointing every agent at demo/fake-agent.py, a quota
snapshot, and a few project directories. Nothing here touches the real home.

usage: seed.py DEMO_HOME
"""
import json
import os
import random
import sys
import uuid
from datetime import datetime, timedelta, timezone

random.seed(7)
HOME = os.path.abspath(sys.argv[1])
HERE = os.path.dirname(os.path.abspath(__file__))
NOW = datetime.now(timezone.utc)


def iso(t):
    return t.strftime("%Y-%m-%dT%H:%M:%S.") + f"{t.microsecond // 1000:03d}Z"


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(text)


PROJECTS = {
    "api": ["src/server.ts", "src/middleware/rateLimit.ts", "src/routes/keys.ts", "test/rateLimit.test.ts", "docs/api.md"],
    "web": ["app/login/page.tsx", "app/dashboard/page.tsx", "components/Chart.tsx", "lib/auth.ts"],
    "infra": ["terraform/main.tf", "terraform/redis.tf", "k8s/api-deployment.yaml", ".github/workflows/deploy.yml"],
    "ml-pipeline": ["pipeline/train.py", "pipeline/features.py", "configs/xgb.yaml", "notebooks/eval.ipynb"],
    "docs": ["guides/quickstart.md", "guides/auth.md", "mkdocs.yml"],
}
for p, files in PROJECTS.items():
    for f in files:
        write(os.path.join(HOME, "dev", p, f), "// demo\n")

TOOLS = {
    "api": [("Read", "src/server.ts"), ("Grep", "rateLimit"), ("Edit", "src/middleware/rateLimit.ts"), ("Bash", "npm test"), ("Edit", "src/routes/keys.ts"), ("Read", "docs/api.md")],
    "web": [("Read", "app/login/page.tsx"), ("Edit", "app/login/page.tsx"), ("Bash", "pnpm lint"), ("Edit", "components/Chart.tsx"), ("Bash", "pnpm test")],
    "infra": [("Read", "terraform/main.tf"), ("Edit", "terraform/redis.tf"), ("Bash", "terraform plan"), ("Edit", "k8s/api-deployment.yaml")],
    "ml-pipeline": [("Read", "pipeline/train.py"), ("Edit", "pipeline/features.py"), ("Bash", "python -m pytest -q"), ("Bash", "python pipeline/train.py --config configs/xgb.yaml")],
    "docs": [("Read", "guides/quickstart.md"), ("Edit", "guides/auth.md"), ("Bash", "mkdocs build")],
}

# (agent, project, title, turns [(user, assistant)], ended ago, duration, model, steps)
H = timedelta(hours=1)
D = timedelta(days=1)
SESSIONS = [
    ("claude", "api", "Rate limiting for the public API", [
        ("add rate limiting to the public API — 100 req/min per API key, backed by redis",
         "I'll add a token-bucket middleware keyed by API key, store buckets in Redis with a TTL, and return 429 with a Retry-After header when a key runs dry."),
        ("also expose the remaining quota in X-RateLimit-* headers and document it",
         "Done. Every response now carries X-RateLimit-Limit / Remaining / Reset, the middleware has 14 new tests, and docs/api.md has a Rate limits section."),
        ("the burst test is flaky on CI — can you look?",
         "The test relied on wall-clock time. I switched it to the fake clock the rest of the suite uses; 50 consecutive runs pass locally."),
    ], 25 * 60, 2.2 * H, "claude-opus-5-5", 70),
    ("claude", "ml-pipeline", "Feature drift in the churn model", [
        ("the churn model's AUC dropped from 0.91 to 0.84 last week — find out why",
         "Two features drifted: `days_since_login` changed units (hours → days) in the upstream export, and `plan_tier` gained a new value the encoder maps to 'unknown'."),
        ("fix the unit bug and retrain",
         "Fixed the conversion in features.py, added a schema check that fails on unit changes, and retrained: AUC is back to 0.905 on the holdout."),
    ], 2.4 * H, 1.5 * H, "claude-fable-5-1", 40),
    ("codex", "web", "Login page redesign", [
        ("rebuild the login page with the new design tokens and add passkey support",
         "Rebuilt app/login with the token set, added WebAuthn passkey registration and sign-in, and kept password login as a fallback."),
    ], 3.1 * H, 1.2 * H, "gpt-5-codex", 45),
    ("opencode", "infra", "Redis cluster for rate limiter", [
        ("provision a managed redis for the api rate limiter, staging and prod",
         "Added terraform/redis.tf with one cluster per environment, wired the connection string into the api deployment as a secret, and `terraform plan` is clean."),
    ], 50 * 60, 1.0 * H, "claude-sonnet-5-5", 30),
    ("claude", "docs", "Auth guide rewrite", [
        ("rewrite the auth guide around API keys and the new rate limits",
         "Rewrote guides/auth.md: key creation, rotation, scopes, and a rate-limit section with header examples."),
    ], 1 * D + 3 * H, 0.8 * H, "claude-sonnet-5-5", 25),
    ("claude", "api", "API key rotation endpoint", [
        ("add an endpoint to rotate API keys without downtime",
         "POST /keys/:id/rotate now issues a new key and keeps the old one valid for a 24h grace period."),
    ], 1 * D + 6 * H, 1.8 * H, "claude-opus-5-5", 60),
    ("codex", "web", "Dashboard charts perf", [
        ("the dashboard charts stutter with 10k points — make them fast",
         "Switched the chart to canvas rendering with LTTB downsampling; 10k points now render in 18 ms."),
    ], 2 * D + 2 * H, 1.4 * H, "gpt-5-codex", 50),
    ("claude", "ml-pipeline", "XGBoost hyperparameter sweep", [
        ("run a sweep over depth and learning rate and summarize",
         "Best config: depth 6, lr 0.05, 800 rounds (AUC 0.912). Results table is in notebooks/eval.ipynb."),
    ], 3 * D + 1 * H, 3.5 * H, "claude-opus-5-5", 90),
    ("claude", "infra", "CI deploy pipeline", [
        ("add a github actions workflow that deploys the api on tag",
         "Added .github/workflows/deploy.yml: build, push image, and a helm upgrade gated on the staging smoke test."),
    ], 4 * D, 1.1 * H, "claude-sonnet-5-5", 35),
    ("opencode", "api", "OpenAPI spec cleanup", [
        ("make the openapi spec match the actual routes",
         "Regenerated the spec from route definitions and fixed 9 mismatches; CI now fails when they drift."),
    ], 5 * D + 4 * H, 0.9 * H, "claude-sonnet-5-5", 28),
    ("claude", "web", "Auth session bug", [
        ("users get logged out after 5 minutes on safari",
         "Safari dropped the cookie because SameSite=None was set without Secure on the preview domain; fixed in lib/auth.ts."),
    ], 6 * D + 2 * H, 0.7 * H, "claude-opus-5-5", 30),
    ("codex", "ml-pipeline", "Feature store backfill", [
        ("backfill the feature store for the last 90 days",
         "Backfilled 90 days in 12 partitions; row counts match the warehouse within 0.01%."),
    ], 8 * D, 2.5 * H, "gpt-5-codex", 60),
    ("claude", "api", "Postgres connection pooling", [
        ("we hit too many connections under load, add pooling",
         "Moved to a shared pg pool (max 20) with pgbouncer in transaction mode; the load test holds 2k rps."),
    ], 9 * D + 5 * H, 2.0 * H, "claude-opus-5", 75),
    ("claude", "docs", "Quickstart refresh", [
        ("update the quickstart for the v2 SDK",
         "Quickstart now uses the v2 client, and every snippet is tested in CI."),
    ], 11 * D, 0.6 * H, "claude-sonnet-5-5", 20),
    ("claude", "ml-pipeline", "Model monitoring alerts", [
        ("alert when prediction drift exceeds a threshold",
         "Added a daily PSI job per feature with a Slack alert above 0.2."),
    ], 12 * D + 3 * H, 1.6 * H, "claude-fable-5-1", 45),
]


def claude_usage(model, i):
    big = model.startswith("claude-fable") or model.startswith("claude-opus")
    return {
        "input_tokens": random.randint(2, 40),
        "output_tokens": random.randint(300, 2600 if big else 1800),
        "cache_read_input_tokens": random.randint(40_000, 160_000),
        "cache_creation_input_tokens": (cw := random.randint(800, 9000)),
        "cache_creation": {"ephemeral_5m_input_tokens": 0, "ephemeral_1h_input_tokens": cw},
    }


def spread(start, end, n):
    return [start + (end - start) * (i + 1) / (n + 1) for i in range(n)]


def claude_session(proj, title, turns, end, dur, model, steps):
    sid = str(uuid.uuid4())
    cwd = os.path.join(HOME, "dev", proj)
    start = end - dur
    lines = []
    times = spread(start, end, len(turns) * (steps // len(turns) + 2))
    ti = 0
    def nt():
        nonlocal ti
        t = times[min(ti, len(times) - 1)]
        ti += 1
        return iso(t)
    parent = None
    for ui, (user, answer) in enumerate(turns):
        u = str(uuid.uuid4())
        lines.append({"type": "user", "uuid": u, "parentUuid": parent, "timestamp": nt(), "cwd": cwd, "gitBranch": "main", "sessionId": sid,
                      "message": {"role": "user", "content": user}})
        parent = u
        for k in range(steps // len(turns)):
            tool, target = random.choice(TOOLS[proj])
            key = "command" if tool == "Bash" else ("pattern" if tool == "Grep" else "file_path")
            val = target if tool in ("Bash", "Grep") else os.path.join(cwd, target)
            mid = "msg_" + uuid.uuid4().hex[:20]
            lines.append({"type": "assistant", "timestamp": nt(), "requestId": "req_" + uuid.uuid4().hex[:16], "cwd": cwd, "sessionId": sid,
                          "message": {"id": mid, "role": "assistant", "model": model,
                                      "content": [{"type": "tool_use", "id": "t" + mid, "name": tool, "input": {key: val}}],
                                      "usage": claude_usage(model, k)}})
        mid = "msg_" + uuid.uuid4().hex[:20]
        lines.append({"type": "assistant", "timestamp": nt(), "requestId": "req_" + uuid.uuid4().hex[:16], "cwd": cwd, "sessionId": sid,
                      "message": {"id": mid, "role": "assistant", "model": model, "content": [{"type": "text", "text": answer}], "usage": claude_usage(model, 0)}})
    lines.append({"type": "ai-title", "aiTitle": title, "sessionId": sid})
    slug = cwd.replace("/", "-").replace(".", "-")
    write(os.path.join(HOME, ".claude", "projects", slug, sid + ".jsonl"), "\n".join(json.dumps(l) for l in lines) + "\n")


def codex_session(proj, title, turns, end, dur, model, steps):
    sid = str(uuid.uuid4())
    cwd = os.path.join(HOME, "dev", proj)
    start = end - dur
    ev = [("session_meta", {"id": sid, "cwd": cwd, "timestamp": iso(start), "git": {"branch": "main"}}, start),
          ("turn_context", {"cwd": cwd, "model": model}, start)]
    times = spread(start, end, steps + 4)
    tot = {"input_tokens": 0, "cached_input_tokens": 0, "output_tokens": 0, "reasoning_output_tokens": 0}
    i = 0
    for user, answer in turns:
        ev.append(("response_item", {"type": "message", "role": "user", "content": [{"type": "input_text", "text": user}]}, times[i])); i += 1
        for k in range(steps // len(turns)):
            tool, target = random.choice(TOOLS[proj])
            t = times[min(i, len(times) - 1)]; i += 1
            if tool == "Edit":
                ev.append(("response_item", {"type": "custom_tool_call", "name": "apply_patch", "input": f"*** Begin Patch\n*** Update File: {target}\n@@"}, t))
            else:
                cmd = target if tool == "Bash" else f"sed -n 1,120p {target}"
                ev.append(("response_item", {"type": "function_call", "name": "shell", "arguments": json.dumps({"command": ["bash", "-lc", cmd]})}, t))
            inp = random.randint(30_000, 90_000)
            tot["input_tokens"] += inp
            tot["cached_input_tokens"] += int(inp * 0.85)
            tot["output_tokens"] += random.randint(300, 2000)
            ev.append(("event_msg", {"type": "token_count", "info": {"total_token_usage": dict(tot)},
                                     "rate_limits": {"primary": {"used_percent": 23.0, "window_minutes": 300, "resets_in_seconds": 14400},
                                                     "secondary": {"used_percent": 41.0, "window_minutes": 10080, "resets_in_seconds": 320000}}}, t))
        ev.append(("response_item", {"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": answer}]}, times[min(i, len(times) - 1)])); i += 1
    lt = start.astimezone()
    path = os.path.join(HOME, ".codex", "sessions", lt.strftime("%Y"), lt.strftime("%m"), lt.strftime("%d"),
                        f"rollout-{lt.strftime('%Y-%m-%dT%H-%M-%S')}-{sid}.jsonl")
    write(path, "\n".join(json.dumps({"timestamp": iso(t), "type": typ, "payload": p}) for typ, p, t in ev) + "\n")
    t_mtime = end.timestamp()
    os.utime(path, (t_mtime, t_mtime))


def opencode_session(proj, title, turns, end, dur, model, steps):
    sid = "ses_" + uuid.uuid4().hex[:24]
    cwd = os.path.join(HOME, "dev", proj)
    root = os.path.join(HOME, ".local", "share", "opencode", "storage")
    start = end - dur
    ms = lambda t: int(t.timestamp() * 1000)
    write(os.path.join(root, "session", "proj_" + proj, sid + ".json"),
          json.dumps({"id": sid, "title": title, "directory": cwd, "time": {"created": ms(start), "updated": ms(end)}}))
    times = spread(start, end, steps + 4)
    i = 0
    def msg(role, parts, t, tokens=None):
        mid = "msg_" + uuid.uuid4().hex[:24]
        m = {"id": mid, "sessionID": sid, "role": role, "time": {"created": ms(t)}}
        if tokens:
            m.update({"modelID": model, "providerID": "anthropic", "tokens": tokens, "cost": 0})
        write(os.path.join(root, "message", sid, mid + ".json"), json.dumps(m))
        for j, p in enumerate(parts):
            write(os.path.join(root, "part", mid, f"prt_{j:03d}.json"), json.dumps(p))
    for user, answer in turns:
        msg("user", [{"type": "text", "text": user}], times[i]); i += 1
        for k in range(steps // len(turns)):
            tool, target = random.choice(TOOLS[proj])
            tok = {"input": random.randint(5, 50), "output": random.randint(300, 1500), "reasoning": 0,
                   "cache": {"read": random.randint(30_000, 100_000), "write": random.randint(500, 5000)}}
            msg("assistant", [{"type": "tool", "tool": tool.lower(), "state": {"input": {"filePath" if tool != "Bash" else "command": os.path.join(cwd, target) if tool != "Bash" else target}}}],
                times[min(i, len(times) - 1)], tok); i += 1
        msg("assistant", [{"type": "text", "text": answer}], times[min(i, len(times) - 1)],
            {"input": 10, "output": 600, "reasoning": 0, "cache": {"read": 50_000, "write": 1000}}); i += 1


for agent, proj, title, turns, ago, dur, model, steps in SESSIONS:
    end = NOW - (timedelta(seconds=ago) if isinstance(ago, (int, float)) else ago)
    {"claude": claude_session, "codex": codex_session, "opencode": opencode_session}[agent](proj, title, turns, end, dur, model, steps)

# a Claude Code session running outside optimus (pid 1 is always alive)
write(os.path.join(HOME, ".claude", "sessions", "1.json"),
      json.dumps({"pid": 1, "sessionId": str(uuid.uuid4()), "cwd": os.path.join(HOME, "dev", "docs"), "status": "idle", "kind": "interactive"}))

# optimus config: every agent is the mock, budgets set
agent = os.path.join(HERE, "fake-agent.py")
cfg = {
    "budgets": {"daily_usd": 40, "weekly_usd": 250, "monthly_usd": 900},
    "agents": {name: {"command": os.path.join(HOME, ".bin", name)} for name in ("claude", "codex", "opencode")},
}
write(os.path.join(HOME, ".config", "optimus", "config.json"), json.dumps(cfg, indent=2))
for name in ("claude", "codex", "opencode"):
    p = os.path.join(HOME, ".bin", name)
    write(p, f"#!/bin/sh\nexec python3 {agent} {name} \"$@\"\n")
    os.chmod(p, 0o755)

# Claude plan quota as recorded by `optimus statusline`
now = int(NOW.timestamp())
write(os.path.join(HOME, ".local", "state", "optimus", "ratelimits.json"), json.dumps({
    "captured": iso(NOW - timedelta(minutes=2)),
    "rate_limits": {"five_hour": {"used_percentage": 47, "resets_at": now + 2 * 3600 + 14 * 60},
                    "seven_day": {"used_percentage": 31, "resets_at": now + 4 * 86400}},
}))
print("seeded", HOME)

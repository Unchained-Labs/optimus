#!/usr/bin/env bash
# Prepare a synthetic environment for recording the demo. Source it:
#   DEMO_HOME=/some/tmp/dir source demo/setup.sh
# It swaps $HOME for a seeded fake one and uses a separate tmux server, so the
# recording never shows (or touches) your real sessions and agents.
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEMO_HOME="${DEMO_HOME:?set DEMO_HOME to an empty scratch dir}"

export OPTIMUS_SOCKET=optimus-demo
tmux -L "$OPTIMUS_SOCKET" kill-server 2>/dev/null
rm -rf "$DEMO_HOME" && mkdir -p "$DEMO_HOME"
python3 "$REPO/demo/seed.py" "$DEMO_HOME" >/dev/null

export HOME="$DEMO_HOME"
unset XDG_CONFIG_HOME XDG_STATE_HOME XDG_CACHE_HOME XDG_DATA_HOME CLAUDE_CONFIG_DIR CODEX_HOME OPENCODE_DATA_DIR OPTIMUS_HOME TMUX
export PATH="$REPO/bin:$PATH"
export PS1='\[\e[1;38;5;211m\]❯\[\e[0m\] '
cd "$HOME/dev/api"

# agents already running when the demo starts
optimus new claude   ~/dev/api         --name api-ratelimits >/dev/null
optimus new codex    ~/dev/web         --name web-login      >/dev/null
optimus new opencode ~/dev/infra       --name infra-redis    >/dev/null
optimus new claude   ~/dev/ml-pipeline --name ml-churn       >/dev/null

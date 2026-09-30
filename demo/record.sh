#!/usr/bin/env bash
# Render the demo video (demo/optimus.mp4 + .gif) with charmbracelet/vhs.
# Needs vhs, ttyd, ffmpeg, tmux and python3 on PATH.
set -euo pipefail
cd "$(dirname "$0")/.."
make build >/dev/null
export DEMO_HOME="${DEMO_HOME:-$PWD/demo/.home}"
vhs demo/demo.tape
tmux -L optimus-demo kill-server 2>/dev/null || true
echo "wrote demo/optimus.mp4 demo/optimus.gif"

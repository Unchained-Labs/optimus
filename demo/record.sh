#!/usr/bin/env bash
# Render the optimus demo: terminal (VHS) + browser + phone (headless Chrome),
# joined with title cards into demo/optimus.mp4 and demo/optimus.gif, plus the
# README screenshots in docs/images/.
#
# Needs: vhs, ttyd, ffmpeg, tmux, python3, go, and Chrome/Chromium (CHROME=path
# to pick one; otherwise rod downloads its own).
# Everything runs against a synthetic $HOME with mock agents (demo/seed.py,
# demo/fake-agent.py) on a separate tmux socket: no real sessions, no tokens.
set -euo pipefail
cd "$(dirname "$0")/.."
REPO=$PWD
BUILD=$REPO/demo/build
export DEMO_HOME="${DEMO_HOME:-$REPO/demo/.home}"
W=1600 H=920
FONT_BOLD="${FONT_BOLD:-$(fc-match -f '%{file}' 'JetBrainsMono Nerd Font:style=Bold' 2>/dev/null || fc-match -f '%{file}' 'monospace:bold')}"
FONT="${FONT:-$(fc-match -f '%{file}' 'JetBrainsMono Nerd Font:style=Regular' 2>/dev/null || fc-match -f '%{file}' 'monospace')}"
BG=0x1e1e2e

rm -rf "$BUILD" && mkdir -p "$BUILD" docs/images
make build >/dev/null
cleanup() { tmux -L optimus-demo kill-server 2>/dev/null || true; }
trap cleanup EXIT

echo "▸ terminal"
vhs demo/terminal.tape >/dev/null
# README screenshots from the terminal recording (timestamps follow the tape)
for shot in agents:1.5 sessions:31 projects:66 usage:76; do
  ffmpeg -loglevel error -y -ss "${shot#*:}" -i "$BUILD/terminal.mp4" -frames:v 1 "docs/images/tui-${shot%%:*}.png"
done

echo "▸ browser"
cleanup
# keep Go's caches outside the fake $HOME the demo runs in
GOMODCACHE=$(go env GOMODCACHE) GOCACHE=$(go env GOCACHE)
export GOMODCACHE GOCACHE
(
  # shellcheck source=/dev/null
  source demo/setup.sh
  optimus web --bg >/dev/null
  url=$(optimus web --url | head -1 | tr -d ' ')
  cd "$REPO/demo/browser" && go run . -url "$url" -out "$BUILD" ${CHROME:+-chrome "$CHROME"}
)
cp "$BUILD"/shots/*.png docs/images/

enc=(-c:v libx264 -preset slow -crf 20 -pix_fmt yuv420p -r 30)

# frames → video, keeping the real timing of each frame
ffmpeg -loglevel error -y -f concat -safe 0 -i "$BUILD/desktop/frames.txt" -vf "scale=$W:$H,fps=30" "${enc[@]}" "$BUILD/web.mp4"
# phone: framed on the left half, caption on the right
ffmpeg -loglevel error -y -f concat -safe 0 -i "$BUILD/mobile/frames.txt" -f lavfi -i "color=c=$BG:s=${W}x${H}:r=30" -filter_complex "
  [0:v]scale=-2:840,pad=iw+14:ih+14:7:7:color=0x45475a[ph];
  [1:v][ph]overlay=200:(H-h)/2:shortest=1,
  drawtext=fontfile=$FONT_BOLD:text='Drive it from your phone':fontcolor=0xcdd6f4:fontsize=54:x=760:y=330,
  drawtext=fontfile=$FONT:text='Live terminals, one-tap answers, broadcasts,':fontcolor=0x9399b2:fontsize=30:x=760:y=420,
  drawtext=fontfile=$FONT:text='sessions and spend - over your own network.':fontcolor=0x9399b2:fontsize=30:x=760:y=465,fps=30" "${enc[@]}" "$BUILD/phone.mp4"

# title cards: card NAME SECONDS "BIG" "small"
card() {
  ffmpeg -loglevel error -y -f lavfi -i "color=c=$BG:s=${W}x${H}:d=$2:r=30" -vf "
    drawtext=fontfile=$FONT_BOLD:text='$3':fontcolor=0xf38ba8:fontsize=76:x=(w-text_w)/2:y=(h-text_h)/2-40,
    drawtext=fontfile=$FONT:text='$4':fontcolor=0x9399b2:fontsize=32:x=(w-text_w)/2:y=(h/2)+50,
    fade=t=in:st=0:d=0.35,fade=t=out:st=$(echo "$2 - 0.35" | bc):d=0.35" "${enc[@]}" "$BUILD/card-$1.mp4"
}
card intro 2.6 "OPTIMUS" "the tmux of AI coding agents"
card terminal 1.8 "In your terminal" "every agent, its state, its cost - one keystroke away"
card browser 1.8 "In your browser" "the same fleet, live and interactive"
card outro 3.2 "curl -fsSL …/install.sh | sh" "Claude Code · Codex · opencode · Gemini · cursor-agent · aider"

# VHS output may differ slightly in size; normalise every segment
ffmpeg -loglevel error -y -i "$BUILD/terminal.mp4" -vf "scale=$W:$H:force_original_aspect_ratio=decrease,pad=$W:$H:(ow-iw)/2:(oh-ih)/2:color=$BG,fps=30" "${enc[@]}" "$BUILD/terminal-n.mp4"

parts=(card-intro card-terminal terminal-n card-browser web phone card-outro)
inputs=() filter=""
for i in "${!parts[@]}"; do inputs+=(-i "$BUILD/${parts[$i]}.mp4"); filter+="[$i:v]"; done
filter+="concat=n=${#parts[@]}:v=1:a=0[v]"
ffmpeg -loglevel error -y "${inputs[@]}" -filter_complex "$filter" -map "[v]" "${enc[@]}" -movflags +faststart demo/optimus.mp4

# README GIF: smaller and faster
ffmpeg -loglevel error -y -i demo/optimus.mp4 -vf "fps=10,scale=960:-1:flags=lanczos,setpts=0.8*PTS,split[a][b];[a]palettegen=max_colors=128:stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=4:diff_mode=rectangle" demo/optimus.gif

echo "✓ demo/optimus.mp4 ($(du -h demo/optimus.mp4 | cut -f1)), demo/optimus.gif ($(du -h demo/optimus.gif | cut -f1)), docs/images/"

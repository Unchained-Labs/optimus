#!/bin/sh
# optimus installer — the tmux of AI coding agents
#
#   curl -fsSL https://raw.githubusercontent.com/Unchained-Labs/optimus/main/install.sh | sh
#
# or from a checkout:  sh install.sh
#
# Options (also settable as env vars):
#   -y, --yes            accept defaults, no questions (OPTIMUS_YES=1)
#   --bin-dir DIR        where to put the binary (OPTIMUS_BIN_DIR, default ~/.local/bin)
#   --version TAG        release to install (OPTIMUS_VERSION, default latest)
#   --from-source        build with Go instead of downloading a release
#   --repo OWNER/NAME    GitHub repo (OPTIMUS_REPO, default Unchained-Labs/optimus)
#   --statusline         set up the Claude Code status line without asking
#   --no-setup           only install the binary; skip the setup questions
set -eu

REPO="${OPTIMUS_REPO:-Unchained-Labs/optimus}"
BIN_DIR="${OPTIMUS_BIN_DIR:-}"
VERSION="${OPTIMUS_VERSION:-latest}"
YES="${OPTIMUS_YES:-0}"
FROM_SOURCE=0
SETUP=1
STATUSLINE=0

while [ $# -gt 0 ]; do
  case "$1" in
    -y|--yes) YES=1 ;;
    --bin-dir) BIN_DIR="$2"; shift ;;
    --bin-dir=*) BIN_DIR="${1#*=}" ;;
    --version) VERSION="$2"; shift ;;
    --version=*) VERSION="${1#*=}" ;;
    --repo) REPO="$2"; shift ;;
    --repo=*) REPO="${1#*=}" ;;
    --from-source) FROM_SOURCE=1 ;;
    --statusline) STATUSLINE=1 ;;
    --no-setup) SETUP=0 ;;
    -h|--help) sed -n '2,18p' "$0" 2>/dev/null | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) printf 'unknown option: %s\n' "$1" >&2; exit 2 ;;
  esac
  shift
done

# --- terminal helpers ---------------------------------------------------------

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  B=$(printf '\033[1m'); D=$(printf '\033[2m'); R=$(printf '\033[0m')
  PINK=$(printf '\033[38;5;211m'); GRN=$(printf '\033[38;5;114m'); YEL=$(printf '\033[38;5;221m'); RED=$(printf '\033[38;5;203m'); BLU=$(printf '\033[38;5;111m')
else
  B=''; D=''; R=''; PINK=''; GRN=''; YEL=''; RED=''; BLU=''
fi

# Questions are read from the terminal even when the script itself arrives on
# stdin through `curl | sh`.
TTY=''
if [ "$YES" != 1 ] && [ -r /dev/tty ] && (exec </dev/tty) 2>/dev/null; then
  TTY=/dev/tty
fi

say()  { printf '%s\n' "$*"; }
step() { printf '\n%s%s◆%s %s%s%s\n' "$B" "$PINK" "$R" "$B" "$*" "$R"; }
ok()   { printf '  %s✓%s %s\n' "$GRN" "$R" "$*"; }
warn() { printf '  %s!%s %s\n' "$YEL" "$R" "$*"; }
info() { printf '  %s%s%s\n' "$D" "$*" "$R"; }
die()  { printf '\n  %s✗ %s%s\n\n' "$RED" "$*" "$R" >&2; exit 1; }

# ask "question" default → $REPLY
ask() {
  if [ -z "$TTY" ]; then REPLY="$2"; return; fi
  if [ -n "$2" ]; then
    printf '  %s?%s %s %s[%s]%s ' "$BLU" "$R" "$1" "$D" "$2" "$R"
  else
    printf '  %s?%s %s ' "$BLU" "$R" "$1"
  fi
  IFS= read -r REPLY <"$TTY" || REPLY=''
  [ -n "$REPLY" ] || REPLY="$2"
}

# confirm "question" y|n → exit status. Without a terminal only --yes accepts
# the default; otherwise nothing that edits your files happens unasked.
confirm() {
  if [ -z "$TTY" ]; then [ "$YES" = 1 ] && [ "$2" = y ]; return; fi
  if [ "$2" = y ]; then hint='Y/n'; else hint='y/N'; fi
  printf '  %s?%s %s %s(%s)%s ' "$BLU" "$R" "$1" "$D" "$hint" "$R"
  IFS= read -r ans <"$TTY" || ans=''
  [ -n "$ans" ] || ans="$2"
  case "$ans" in [Yy]*) return 0 ;; *) return 1 ;; esac
}

have() { command -v "$1" >/dev/null 2>&1; }

# shellcheck disable=SC2088 # matching a literal "~/" typed by the user
expand() { case "$1" in "~") printf '%s' "$HOME" ;; "~/"*) printf '%s/%s' "$HOME" "${1#\~/}" ;; *) printf '%s' "$1" ;; esac; }

fetch() { # url dest
  if have curl; then curl -fsSL --retry 2 -o "$2" "$1"
  elif have wget; then wget -q -O "$2" "$1"
  else return 1; fi
}

TMP=$(mktemp -d 2>/dev/null || mktemp -d -t optimus)
trap 'rm -rf "$TMP"' EXIT INT TERM

# --- banner ---------------------------------------------------------------------

printf '\n%s' "$PINK$B"
cat <<'EOF'
   ___  ____ _____ ___ __  __ _   _ ____
  / _ \|  _ \_   _|_ _|  \/  | | | / ___|
 | | | | |_) || |  | || |\/| | | | \___ \
 | |_| |  __/ | |  | || |  | | |_| |___) |
  \___/|_|    |_| |___|_|  |_|\___/|____/
EOF
printf '%s  %sthe tmux of AI coding agents%s\n' "$R" "$D" "$R"

# --- 1. system --------------------------------------------------------------------

step "Checking your system"
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in linux|darwin) ;; *) die "unsupported OS: $OS (optimus runs on Linux and macOS)" ;; esac
ARCH=$(uname -m)
case "$ARCH" in x86_64|amd64) ARCH=amd64 ;; aarch64|arm64) ARCH=arm64 ;; *) die "unsupported CPU: $ARCH" ;; esac
ok "$OS/$ARCH"

tmux_hint() {
  if [ "$OS" = darwin ]; then echo "brew install tmux"
  elif have apt-get; then echo "sudo apt-get install -y tmux"
  elif have dnf; then echo "sudo dnf install -y tmux"
  elif have pacman; then echo "sudo pacman -S tmux"
  elif have apk; then echo "sudo apk add tmux"
  else echo "install tmux with your package manager"; fi
}
if have tmux; then
  ok "$(tmux -V)"
else
  warn "tmux not found — the agent multiplexer needs it (sessions/usage work without)"
  info "install it with: $(tmux_hint)"
fi

# --- 2. where ------------------------------------------------------------------------

step "Install location"
if [ -z "$BIN_DIR" ]; then
  if [ -w /usr/local/bin ] && ! [ -d "$HOME/.local/bin" ]; then def=/usr/local/bin; else def="$HOME/.local/bin"; fi
  ask "Install optimus into" "$def"
  BIN_DIR=$REPLY
fi
BIN_DIR=$(expand "$BIN_DIR")
mkdir -p "$BIN_DIR" 2>/dev/null || die "cannot create $BIN_DIR"
[ -w "$BIN_DIR" ] || die "$BIN_DIR is not writable (choose another with --bin-dir)"
ok "$BIN_DIR"

# --- 3. get the binary ----------------------------------------------------------------

# A checkout next to this script means we build that checkout.
SRC=''
case "$0" in
  */install.sh|install.sh)
    d=''
    if cd "$(dirname "$0")" 2>/dev/null; then d=$(pwd); cd - >/dev/null; fi
    if [ -n "$d" ] && [ -f "$d/go.mod" ] && grep -q 'module github.com/Unchained-Labs/optimus' "$d/go.mod"; then SRC=$d; FROM_SOURCE=1; fi ;;
esac

build_from() { # dir
  have go || die "Go is required to build from source (https://go.dev/dl) — or install a release binary instead"
  ver=$(git -C "$1" describe --tags --always --dirty 2>/dev/null || echo dev)
  info "go build ($(go version | awk '{print $3}'), version $ver)…"
  (cd "$1" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/Unchained-Labs/optimus/internal/cli.Version=$ver" -o "$TMP/optimus" ./cmd/optimus) \
    || die "build failed"
}

step "Getting optimus"
if [ "$FROM_SOURCE" = 1 ]; then
  if [ -z "$SRC" ]; then
    have git || die "git is required to build from source"
    info "cloning github.com/$REPO…"
    set -- # reuse the positional args as an argv for git
    [ "$VERSION" = latest ] || set -- --branch "$VERSION"
    git clone -q --depth 1 "$@" "https://github.com/$REPO.git" "$TMP/src" || die "clone failed"
    SRC="$TMP/src"
  fi
  build_from "$SRC"
  ok "built from ${SRC}"
else
  asset="optimus_${OS}_${ARCH}.tar.gz"
  if [ -n "${OPTIMUS_DOWNLOAD_BASE:-}" ]; then base=$OPTIMUS_DOWNLOAD_BASE # mirrors, tests
  elif [ "$VERSION" = latest ]; then base="https://github.com/$REPO/releases/latest/download"
  else base="https://github.com/$REPO/releases/download/$VERSION"; fi
  info "downloading $asset ($VERSION)…"
  if fetch "$base/$asset" "$TMP/$asset"; then
    if fetch "$base/checksums.txt" "$TMP/checksums.txt"; then
      want=$(grep " $asset\$" "$TMP/checksums.txt" | awk '{print $1}')
      if have sha256sum; then got=$(sha256sum "$TMP/$asset" | awk '{print $1}'); else got=$(shasum -a 256 "$TMP/$asset" | awk '{print $1}'); fi
      if [ -z "$want" ] || [ "$want" != "$got" ]; then die "checksum mismatch for $asset"; fi
      ok "checksum verified"
    fi
    tar -xzf "$TMP/$asset" -C "$TMP" optimus || die "could not unpack $asset"
    ok "release binary"
  else
    warn "no release binary available — building from source instead"
    have git || die "git is required to build from source"
    git clone -q --depth 1 "https://github.com/$REPO.git" "$TMP/src" || die "clone failed (is github.com/$REPO reachable?)"
    build_from "$TMP/src"
    ok "built from github.com/$REPO"
  fi
fi

chmod +x "$TMP/optimus"
mv -f "$TMP/optimus" "$BIN_DIR/optimus"
OPT="$BIN_DIR/optimus"
ok "installed $("$OPT" version) → $OPT"

# --- 4. PATH ------------------------------------------------------------------------------

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    step "PATH"
    shell=$(basename "${SHELL:-sh}")
    case "$shell" in
      zsh)  rc="$HOME/.zshrc";  line="export PATH=\"$BIN_DIR:\$PATH\"" ;;
      bash) rc="$HOME/.bashrc"; line="export PATH=\"$BIN_DIR:\$PATH\"" ;;
      fish) rc="$HOME/.config/fish/config.fish"; line="fish_add_path $BIN_DIR" ;;
      *)    rc="$HOME/.profile"; line="export PATH=\"$BIN_DIR:\$PATH\"" ;;
    esac
    warn "$BIN_DIR is not on your PATH"
    if confirm "Add it to $rc?" y; then
      mkdir -p "$(dirname "$rc")"
      printf '\n# added by the optimus installer\n%s\n' "$line" >>"$rc"
      ok "updated $rc (open a new terminal, or: $line)"
    else
      info "add this to your shell config: $line"
    fi
    ;;
esac

# --- 5. setup wizard --------------------------------------------------------------------------

if [ "$SETUP" = 1 ]; then
  step "Your agents"
  found=0; first=''
  for a in claude codex opencode gemini cursor-agent aider amp crush goose; do
    if have "$a"; then ok "$a"; found=$((found + 1)); [ -n "$first" ] || first=$a; fi
  done
  [ "$found" -gt 0 ] || warn "no coding agents found on PATH yet (claude, codex, opencode, …)"
  if [ "$found" -gt 0 ]; then
    ask "Default agent for new sessions (optimus new, N in the dashboard)" "$first"
    if "$OPT" config set default_agent "$REPLY" >/dev/null 2>&1; then ok "default agent: $REPLY"; fi
  fi

  if have claude || [ -d "$HOME/.claude" ]; then
    step "Remaining quota"
    info "Claude Code only shares your plan's 5h / 7d usage with its status line."
    info "optimus can be that status line (an existing one keeps working, chained)."
    if [ "$STATUSLINE" = 1 ] || { [ -n "$TTY" ] && confirm "Set up the Claude Code status line?" y; }; then
      "$OPT" config statusline --install 2>&1 | sed 's/^/    /'
    else
      info "later: optimus config statusline --install"
    fi
  fi

  step "Budgets"
  info "Optional spend alerts, in API-equivalent dollars. Leave blank to skip."
  for period in daily weekly monthly; do
    ask "$period budget (USD)" ""
    if [ -n "$REPLY" ]; then
      if "$OPT" config set "budgets.${period}_usd" "$REPLY" >/dev/null 2>&1; then
        ok "$period: \$${REPLY#\$}"
      else
        warn "ignored \"$REPLY\" (not a number)"
      fi
    fi
  done

  step "Remote control"
  info "Every session optimus starts also opens the web dashboard (live terminals,"
  info "works from your phone) and Claude sessions get Claude's own Remote Control."
  if [ -z "$TTY" ] || confirm "Keep both on?" y; then
    ok "on — dashboard: optimus web --url"
  else
    "$OPT" config set remote.web_autostart false >/dev/null
    "$OPT" config set remote.claude_remote_control false >/dev/null
    ok "off — start the dashboard any time with: optimus web"
  fi

  step "Indexing your sessions"
  n=$("$OPT" reindex 2>/dev/null | awk '{print $2}')
  ok "${n:-0} sessions indexed"
  if [ "${n:-0}" -gt 0 ] 2>/dev/null; then
    "$OPT" usage --by agent --since 7d 2>/dev/null | sed -n '1,6p' | sed 's/^/    /'
  fi
fi

# --- done ---------------------------------------------------------------------------------------

printf '\n%s%s✓ optimus is ready.%s\n\n' "$B" "$GRN" "$R"
say "  ${B}optimus${R}                    open the dashboard"
say "  ${B}optimus claude${R}             start an agent here (Alt-q to come back)"
say "  ${B}optimus web --open${R}         the same fleet in your browser / phone"
say "  ${B}optimus usage${R}              what you spent"
say "  ${B}optimus help${R}               everything else"
say ""

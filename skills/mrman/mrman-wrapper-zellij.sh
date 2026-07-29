#!/usr/bin/env bash
# Launch mrman in a zellij pane and block until the reviewer exits.
#
# zellij has no `wait-for` primitive like tmux, so the spawned command
# signals completion by writing to a FIFO this script reads.
set -e -u -o pipefail

MRMAN_PANE_DIRECTION="${MRMAN_PANE_DIRECTION:-stacked}"  # down | right | stacked
ZELLIJ_BIN="${ZELLIJ_BIN:-zellij}"

# Accept the tmux wrapper's vocabulary so the two are interchangeable.
case "${MRMAN_PANE_POSITION:-}" in
  top | bottom) MRMAN_PANE_DIRECTION="down" ;;
  left | right) MRMAN_PANE_DIRECTION="right" ;;
  stacked) MRMAN_PANE_DIRECTION="stacked" ;;
esac

RED=$'\033[0;31m'
GREEN=$'\033[0;32m'
YELLOW=$'\033[1;33m'
NC=$'\033[0m'

log_info() { echo "${GREEN}[mrman]${NC} $*"; }
log_warn() { echo "${YELLOW}[mrman]${NC} $*"; }
log_error() { echo "${RED}[mrman]${NC} $*" >&2; }

usage() {
  cat <<EOF
Usage: $(basename "$0") [directory]

Launch mrman in a zellij pane to review changes.

Arguments:
  directory    Repository to review (default: the current directory)

Environment:
  MRMAN_PANE_DIRECTION  down, right or stacked (default: stacked)
  ZELLIJ_BIN            Path to the zellij executable

Examples:
  $(basename "$0")
  $(basename "$0") ~/project
  MRMAN_PANE_DIRECTION=right $(basename "$0")
EOF
}

require_mrman() {
  if ! command -v mrman >/dev/null 2>&1; then
    log_error "mrman not found on PATH. Install it first."
    return 1
  fi
}

require_repo() {
  local dir="$1"
  if git -C "$dir" rev-parse --git-dir >/dev/null 2>&1; then
    return 0
  fi
  if command -v jj >/dev/null 2>&1 && jj -R "$dir" root >/dev/null 2>&1; then
    return 0
  fi
  log_error "Not a git or jj repository: $dir"
  return 1
}

launch_pane() {
  local target_dir="$1"

  case "$MRMAN_PANE_DIRECTION" in
    down | right | stacked) ;;
    *)
      log_warn "Unknown MRMAN_PANE_DIRECTION '$MRMAN_PANE_DIRECTION'; using 'stacked'"
      MRMAN_PANE_DIRECTION="stacked"
      ;;
  esac

  log_info "Launching mrman in a $MRMAN_PANE_DIRECTION pane"
  log_info "Directory: $target_dir"

  local fifo session_log
  fifo=$(mktemp -u "${TMPDIR:-/tmp}/mrman-fifo.XXXXXX")
  mkfifo "$fifo"
  session_log=$(mktemp "${TMPDIR:-/tmp}/mrman-session.XXXXXX")

  local zellij_args=(--close-on-exit --name mrman --cwd "$target_dir")
  if [[ "$MRMAN_PANE_DIRECTION" == "stacked" ]]; then
    zellij_args+=(--stacked)
  else
    zellij_args+=(--direction "$MRMAN_PANE_DIRECTION")
  fi

  # mrman announces `mrman-session: <slug>` on stderr; tee it so the agent
  # can reattach without a `review list` round trip.
  zellij_args+=(-- sh -c "mrman 2>'$session_log'; echo done >'$fifo'")

  "$ZELLIJ_BIN" run "${zellij_args[@]}"

  log_info "mrman is running — waiting for it to exit"
  read -r _ <"$fifo"
  rm -f "$fifo"
  log_info "mrman finished"

  local slug
  slug=$(grep -o 'mrman-session: .*' "$session_log" 2>/dev/null | tail -1 | cut -d' ' -f2- || true)
  cat "$session_log" >&2 || true
  rm -f "$session_log"

  if [[ -n "$slug" ]]; then
    echo
    echo "mrman-session: $slug"
    echo "Read the review with: mrman review comments --session '$slug'"
  else
    log_warn "Could not read the session slug; find it with:"
    log_warn "  mrman review list --repo '$target_dir'"
  fi
}

main() {
  if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    usage
    exit 0
  fi
  require_mrman || exit 1

  local target_dir="${1:-.}"
  target_dir=$(cd "$target_dir" && pwd)
  require_repo "$target_dir" || exit 1

  if [[ -z "${ZELLIJ:-}" ]]; then
    log_error "Not running inside zellij."
    echo
    echo "To review alongside an agent, start the agent inside zellij:"
    echo "  1. Exit the current agent session."
    echo "  2. Run 'zellij', then start the agent inside it."
    echo "  3. Ask for the review again."
    exit 1
  fi

  if ! command -v "$ZELLIJ_BIN" >/dev/null 2>&1 && [[ ! -x "$ZELLIJ_BIN" ]]; then
    log_error "zellij not found on PATH (set ZELLIJ_BIN to override)"
    exit 1
  fi

  if pgrep -x mrman >/dev/null 2>&1; then
    log_warn "mrman is already running (zellij has no pane list to check against)"
    exit 0
  fi

  launch_pane "$target_dir"
}

main "$@"

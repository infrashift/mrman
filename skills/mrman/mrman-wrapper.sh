#!/usr/bin/env bash
# Launch mrman in a tmux split pane and block until the reviewer exits.
#
# The agent that runs this keeps working while the human reviews; the
# session slug mrman announces on stderr is how it reattaches afterwards.
set -e -u -o pipefail

MRMAN_PANE_POSITION="${MRMAN_PANE_POSITION:-top}"  # top | bottom
MRMAN_PANE_SIZE="${MRMAN_PANE_SIZE:-80}"           # percent of the window

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

Launch mrman in a tmux split pane to review changes.

Arguments:
  directory    Repository to review (default: the current directory)

Environment:
  MRMAN_PANE_POSITION   top or bottom (default: top)
  MRMAN_PANE_SIZE       Pane size as a percentage (default: 80)

Examples:
  $(basename "$0")
  $(basename "$0") ~/project
  MRMAN_PANE_SIZE=70 $(basename "$0")
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
  # mrman also reviews jj repos and, with --file, no repo at all; this
  # wrapper only claims to handle the ordinary case.
  if git -C "$dir" rev-parse --git-dir >/dev/null 2>&1; then
    return 0
  fi
  if command -v jj >/dev/null 2>&1 && jj -R "$dir" root >/dev/null 2>&1; then
    return 0
  fi
  log_error "Not a git or jj repository: $dir"
  return 1
}

already_running() {
  tmux list-panes -a -F '#{pane_current_command}' 2>/dev/null | grep -qx 'mrman'
}

launch_pane() {
  local target_dir="$1"

  # Size in lines rather than percent: tmux rejects a percentage when it
  # has no TTY, which is exactly the case when an agent runs this.
  local window_height pane_lines
  window_height=$(tmux display-message -p '#{window_height}')
  pane_lines=$(( window_height * MRMAN_PANE_SIZE / 100 ))

  local split_args=()
  [[ "$MRMAN_PANE_POSITION" == "top" ]] && split_args+=(-b)
  split_args+=(-l "$pane_lines" -c "$target_dir")

  log_info "Launching mrman in the $MRMAN_PANE_POSITION pane (${pane_lines} lines)"
  log_info "Directory: $target_dir"

  local wait_channel="mrman-$$"
  local session_log
  session_log=$(mktemp "${TMPDIR:-/tmp}/mrman-session.XXXXXX")

  # mrman announces `mrman-session: <slug>` on stderr; capturing it saves
  # the agent a `review list` round trip.
  local pane_id
  pane_id=$(tmux split-window -d -P -F '#{pane_id}' "${split_args[@]}" \
    "cd '$target_dir' && mrman 2> >(tee '$session_log' >&2); tmux wait-for -S '$wait_channel'")

  tmux select-pane -t "$pane_id"
  log_info "mrman is running in pane $pane_id — waiting for it to exit"
  tmux wait-for "$wait_channel"
  log_info "mrman finished"

  local slug
  slug=$(grep -o 'mrman-session: .*' "$session_log" 2>/dev/null | tail -1 | cut -d' ' -f2- || true)
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

  if [[ -z "${TMUX:-}" ]]; then
    log_error "Not running inside tmux."
    echo
    echo "To review alongside an agent, start the agent inside tmux:"
    echo "  1. Exit the current agent session."
    echo "  2. Start tmux, then start the agent inside it."
    echo "  3. Ask for the review again."
    exit 1
  fi

  if already_running; then
    log_warn "mrman is already running in another pane (Ctrl-b then an arrow key)"
    exit 0
  fi

  launch_pane "$target_dir"
}

main "$@"

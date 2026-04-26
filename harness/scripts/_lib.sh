#!/usr/bin/env bash
# _lib.sh — shared helpers for avangarde harness scripts

# ── Dependency check ──────────────────────────────────────────────────────────
_MISSING=()
command -v gum    &>/dev/null || _MISSING+=("gum       → brew install gum")
command -v claude &>/dev/null || _MISSING+=("claude    → npm install -g @anthropic-ai/claude-code")
command -v git    &>/dev/null || _MISSING+=("git       → brew install git")

if [[ ${#_MISSING[@]} -gt 0 ]]; then
  echo "ERROR: missing required dependencies:" >&2
  for dep in "${_MISSING[@]}"; do echo "  • $dep" >&2; done
  exit 1
fi

# ── Timeout command (macOS coreutils uses gtimeout) ───────────────────────────
if command -v gtimeout &>/dev/null; then
  TIMEOUT_CMD="gtimeout"
elif command -v timeout &>/dev/null; then
  TIMEOUT_CMD="timeout"
else
  TIMEOUT_CMD=""
  echo "WARNING: no timeout command found (install with: brew install coreutils). Sensors will not be time-limited." >&2
fi
export TIMEOUT_CMD

# ── Colors (used in plain echo output, not prompts) ───────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
RESET='\033[0m'

# ── Print helpers ──────────────────────────────────────────────────────────────
info()    { gum style --foreground 51  "→ $*"; }
success() { gum style --foreground 82  "✓ $*"; }
warn()    { gum style --foreground 214 "! $*"; }
error()   { gum style --foreground 196 "✗ $*" >&2; }
header()  { echo; gum style --bold --foreground 63 "$*"; }
divider() { gum style --foreground 63 "────────────────────────────────────────"; }

# debug — only prints when HARNESS_VERBOSE=1
debug() {
  [[ "${HARNESS_VERBOSE:-0}" == "1" ]] || return 0
  gum style --foreground 240 --italic "  [debug] $*"
}

# ── User prompts ───────────────────────────────────────────────────────────────

# ask <var_name> <prompt> [default]
ask() {
  local var="$1"
  local prompt="$2"
  local default="${3:-}"

  info "$prompt"
  local value
  value=$(gum write \
    --placeholder="${default:-...}" \
    --value="$default" \
    --width 80 \
    --height 3 \
    --char-limit 0)

  if [[ -z "$value" && -n "$default" ]]; then
    value="$default"
  fi

  printf -v "$var" '%s' "$value"
  debug "ask[$var] captured $(echo -n "$value" | wc -c | tr -d ' ') chars: $(echo "$value" | head -1 | cut -c1-60)"
}

# ask_multiline <var_name> <prompt> [initial_value]
ask_multiline() {
  local var="$1"
  local prompt="$2"
  local initial="${3:-}"

  info "$prompt"
  local value
  value=$(gum write \
    --placeholder="..." \
    --value="$initial" \
    --width 80 \
    --height 15 \
    --char-limit 0)

  printf -v "$var" '%s' "$value"
  debug "ask_multiline[$var] captured $(echo -n "$value" | wc -c | tr -d ' ') chars, $(echo "$value" | wc -l | tr -d ' ') lines"
  if [[ "${HARNESS_VERBOSE:-0}" == "1" ]]; then
    echo "$value" | while IFS= read -r line; do
      debug "  | $line"
    done
  fi
}

# _flush_tty — switch terminal to raw non-blocking, drain all pending input, restore
_flush_tty() {
  local saved
  saved=$(stty -g < /dev/tty 2>/dev/null) || return 0
  # raw + min 0 time 0: each char available immediately, reads return at once if empty
  stty -icanon min 0 time 0 < /dev/tty 2>/dev/null || return 0
  while IFS= read -r -t 0 _ < /dev/tty 2>/dev/null; do :; done
  stty "$saved" < /dev/tty 2>/dev/null || stty sane < /dev/tty 2>/dev/null || true
}

# yes_no <prompt> — returns 0 for yes, 1 for no
yes_no() {
  local prompt="$1"
  local answer

  while true; do
    gum style --bold "  $prompt"
    gum style --foreground 240 "  [y] Yes   [n] No"
    # Flush AFTER all display is done — clears any newlines from preceding echo/gum calls
    _flush_tty
    IFS= read -r answer < /dev/tty
    debug "yes_no[\"$prompt\"] raw input: '${answer}'"
    case "$(echo "$answer" | tr '[:upper:]' '[:lower:]')" in
      y|yes) debug "yes_no[\"$prompt\"] → yes"; return 0 ;;
      n|no)  debug "yes_no[\"$prompt\"] → no";  return 1 ;;
      *)     warn "Please type y or n" ;;
    esac
  done
}

# confirm_or_edit <var_name> <label>
# Shows proposed value, asks accept or edit.
confirm_or_edit() {
  local var="$1"
  local label="$2"
  local current="${!var}"

  divider
  info "Proposed ${label}:"
  echo "$current"
  divider

  if yes_no "Accept this ${label}?"; then
    return 0
  else
    ask_multiline "$var" "Edit ${label}" "$current"
    debug "confirm_or_edit[$var] edited to $(echo -n "${!var}" | wc -c | tr -d ' ') chars"
  fi
}

# ask_choice <var_name> <prompt> <option1> <option2> ...
# Presents an interactive list. Sets var to chosen value.
ask_choice() {
  local var="$1"
  local prompt="$2"
  shift 2
  local value
  value=$(gum choose --header "  $prompt" "$@")
  printf -v "$var" '%s' "$value"
}

# ── Filesystem helpers ─────────────────────────────────────────────────────────
ensure_dir() { mkdir -p "$1"; }
harness_root() { echo "harness"; }
tasks_file() { echo "harness/loops/tasks.md"; }

# ── Git helpers ────────────────────────────────────────────────────────────────

require_git_repo() {
  if ! git rev-parse --git-dir &>/dev/null; then
    error "Not in a git repository. Run 'git init' first."
    exit 1
  fi
}

current_branch() { git rev-parse --abbrev-ref HEAD; }

slugify() {
  echo "$1" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/-/g' | sed 's/-\+/-/g' | sed 's/^-\|-$//g' | cut -c1-50 | sed 's/-$//'
}

# ── Task list helpers ──────────────────────────────────────────────────────────

next_unchecked_task() {
  local tf
  tf="$(tasks_file)"
  if [[ ! -f "$tf" ]]; then echo ""; return; fi
  grep -m1 '^\[ \]' "$tf" | sed 's/^\[ \] *//'
}

mark_task_done() {
  local task="$1"
  local tf
  tf="$(tasks_file)"
  local escaped
  escaped=$(printf '%s\n' "$task" | sed 's/[[\.*^$()+?{}|]/\\&/g')
  sed -i.bak "s/^\[ \] *${escaped}/[x] ${task}/" "$tf" && rm -f "${tf}.bak"
}

# mark_task_done_by_slug <slug>
# Finds the [ ] task whose slugified name matches <slug> and marks it [x].
# More reliable than exact-name matching when case/spacing may differ.
mark_task_done_by_slug() {
  local target_slug="$1"
  local tf
  tf="$(tasks_file)"
  [[ ! -f "$tf" ]] && return 1

  local found=0
  local tmpfile
  tmpfile=$(mktemp)
  while IFS= read -r line; do
    if [[ "$line" =~ ^\[\ \]\  ]] && [[ $found -eq 0 ]]; then
      local task_text="${line#\[ \] }"
      local line_slug
      line_slug=$(slugify "$task_text")
      if [[ "$line_slug" == "$target_slug" ]]; then
        echo "[x] ${task_text}" >> "$tmpfile"
        found=1
        continue
      fi
    fi
    echo "$line" >> "$tmpfile"
  done < "$tf"
  mv "$tmpfile" "$tf"
  return $(( found == 0 ? 1 : 0 ))
}

# ── Script location ────────────────────────────────────────────────────────────
scripts_dir() {
  if [[ -d "harness/scripts" ]]; then
    echo "harness/scripts"
  else
    echo "$(cd "$(dirname "${BASH_SOURCE[1]}")" && pwd)"
  fi
}

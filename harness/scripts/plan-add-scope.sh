#!/usr/bin/env bash
# plan-add-scope.sh — Planning and scope management
#
# Usage:
#   plan-add-scope.sh --mode=plan    # Create/refresh phased task list
#   plan-add-scope.sh --mode=scope   # Add a new task mid-project

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

# ── Parse args ─────────────────────────────────────────────────────────────────
MODE=""
for arg in "$@"; do
  case "$arg" in
    --mode=*) MODE="${arg#--mode=}" ;;
  esac
done

if [[ -z "$MODE" ]]; then
  error "Usage: plan-add-scope.sh --mode=plan|scope"
  exit 1
fi

require_git_repo

TASKS_FILE="$(tasks_file)"
VISION_FILE="specs/product/vision.md"

# ── MODE: plan ─────────────────────────────────────────────────────────────────
if [[ "$MODE" == "plan" ]]; then
  header "Avangarde Harness — Planning"
  divider

  if [[ ! -f "$VISION_FILE" ]]; then
    error "No vision file found at ${VISION_FILE}. Run 'avangardespec init' first."
    exit 1
  fi

  info "Reading project vision..."
  VISION=$(cat "$VISION_FILE")

  info "Asking AI to propose a phased task list..."
  RAW_PLAN=$("$SCRIPT_DIR/claude.sh" \
    "Based on this project vision, propose a phased task list.
Format exactly like this:
## Phase 1 — <name>
[ ] <task>
[ ] <task>

## Phase 2 — <name> (requires Phase 1)
[ ] <task>
[ ] <task>

Include 2-4 phases. Each task is one atomic unit of work (one branch, one PR)." \
    "$VISION_FILE")

  divider
  echo "$RAW_PLAN"
  divider

  # Walk through each phase
  APPROVED_PLAN=""
  CURRENT_PHASE=""
  CURRENT_TASKS=""

  # Parse RAW_PLAN into an array of phases to avoid stdin conflict with gum prompts
  declare -a PHASE_HEADERS=()
  declare -a PHASE_BODIES=()
  _cur_header=""
  _cur_body=""
  while IFS= read -r line; do
    if [[ "$line" =~ ^##\  ]]; then
      if [[ -n "$_cur_header" ]]; then
        PHASE_HEADERS+=("$_cur_header")
        PHASE_BODIES+=("$_cur_body")
      fi
      _cur_header="$line"
      _cur_body=""
    else
      _cur_body+="$line"$'\n'
    fi
  done <<< "$RAW_PLAN"
  # Push final phase
  if [[ -n "$_cur_header" ]]; then
    PHASE_HEADERS+=("$_cur_header")
    PHASE_BODIES+=("$_cur_body")
  fi

  # Debug: show parsed arrays
  if [[ "${HARNESS_VERBOSE:-0}" == "1" ]]; then
    echo "  [debug] parsed ${#PHASE_HEADERS[@]} phases:" >&2
    for i in "${!PHASE_HEADERS[@]}"; do
      echo "  [debug]   phase $i header: ${PHASE_HEADERS[$i]}" >&2
      echo "  [debug]   phase $i body lines: $(echo "${PHASE_BODIES[$i]}" | wc -l | tr -d ' ')" >&2
    done
  fi

  # Review each phase — stdin is now free for gum
  for i in "${!PHASE_HEADERS[@]}"; do
    echo
    info "Phase $((i+1)) of ${#PHASE_HEADERS[@]}: ${PHASE_HEADERS[$i]}"
    echo "${PHASE_BODIES[$i]}"
    echo "I AM HERE"
    divider
    if yes_no "Accept this phase?"; then
      APPROVED_PLAN+="${PHASE_HEADERS[$i]}"$'\n'"${PHASE_BODIES[$i]}"$'\n\n'
    else
      ask_multiline EDITED_TASKS "Enter your version of the tasks for this phase (one [ ] task per line)"
      APPROVED_PLAN+="${PHASE_HEADERS[$i]}"$'\n'"${EDITED_TASKS}"$'\n\n'
    fi
  done

  ensure_dir "harness/loops"
  cat > "$TASKS_FILE" <<TASKS_EOF
# Task List

${APPROVED_PLAN}
TASKS_EOF

  success "Task list written to ${TASKS_FILE}"
  echo
  info "Next step: run 'avangardespec pre-task' to start the first task."

# ── MODE: scope ────────────────────────────────────────────────────────────────
elif [[ "$MODE" == "scope" ]]; then
  header "Avangarde Harness — Add Scope"
  divider

  if [[ ! -f "$TASKS_FILE" ]]; then
    error "No task list found at ${TASKS_FILE}. Run 'avangardespec plan' first."
    exit 1
  fi

  ask WHAT_TODO   "What needs to be done?"
  ask WHEN_TODO   "When? (now / next-phase / backlog)" "next-phase"

  CURRENT_TASKS=$(cat "$TASKS_FILE")

  info "Asking AI to assess placement..."
  PLACEMENT=$("$SCRIPT_DIR/claude.sh" \
    "A new scope item needs to be placed in the task list.
New item: ${WHAT_TODO}
Timing: ${WHEN_TODO}
Current task list:
${CURRENT_TASKS}

Assess:
- Dependencies: what existing tasks does this depend on?
- Suggested phase: which phase should it go in?
- Flag: is this a hotfix (urgent, unplanned), backlog, or normal task?
- Proposed task line: exactly '[ ] <task description>'

Format as key: value pairs.")

  divider
  echo "$PLACEMENT"
  divider

  # Extract the proposed task line
  PROPOSED_TASK=$(echo "$PLACEMENT" | grep -i 'proposed task' | sed 's/.*: *//')
  if [[ -z "$PROPOSED_TASK" ]]; then
    PROPOSED_TASK="[ ] ${WHAT_TODO}"
  fi

  info "Proposed task: ${PROPOSED_TASK}"

  ask FINAL_TASK "Task line to insert (edit if needed)" "$PROPOSED_TASK"

  # Ask which phase to insert into
  echo
  info "Current phases:"
  grep '^## ' "$TASKS_FILE" | nl -ba
  ask TARGET_PHASE "Insert into which phase? (paste the phase header or leave blank to append)"

  if [[ -z "$TARGET_PHASE" ]]; then
    # Append to end
    echo "$FINAL_TASK" >> "$TASKS_FILE"
  else
    # Insert after the matching phase header
    # Use python for reliable multiline sed
    python3 - <<PYEOF
import re, sys

target = """${TARGET_PHASE}"""
new_task = """${FINAL_TASK}"""
path = "${TASKS_FILE}"

with open(path, 'r') as f:
    content = f.read()

lines = content.split('\n')
result = []
inserted = False
for line in lines:
    result.append(line)
    if not inserted and line.strip() == target.strip():
        # Insert after next blank line or next task
        result.append(new_task)
        inserted = True

if not inserted:
    result.append(new_task)

with open(path, 'w') as f:
    f.write('\n'.join(result))
print("Inserted.")
PYEOF
  fi

  success "Task added to ${TASKS_FILE}"

else
  error "Unknown mode: ${MODE}. Use --mode=plan or --mode=scope"
  exit 1
fi

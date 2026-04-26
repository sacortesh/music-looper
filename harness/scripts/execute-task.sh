#!/usr/bin/env bash
# execute-task.sh — Ralph loop: AI coding + sensor check cycle

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

require_git_repo

SENSOR_FILE="harness/sensors/check.sh"
SPECS_DIR="specs/features"

# ── Find current task ──────────────────────────────────────────────────────────
header "Avangarde Harness — Task Loop"
divider

CURRENT_BRANCH=$(current_branch)

if [[ "$CURRENT_BRANCH" =~ ^task/ ]]; then
  SLUG="${CURRENT_BRANCH#task/}"
  SPEC_FILE="${SPECS_DIR}/${SLUG}.md"
else
  warn "Current branch is not a task branch: ${CURRENT_BRANCH}"
  info "Run 'avangardespec pre-task' first to create a task branch."
  TASK=$(next_unchecked_task)
  if [[ -z "$TASK" ]]; then
    error "No unchecked tasks found. Run 'avangardespec plan' to set up tasks."
    exit 1
  fi
  SLUG=$(slugify "$TASK")
  SPEC_FILE="${SPECS_DIR}/${SLUG}.md"
fi

if [[ ! -f "$SPEC_FILE" ]]; then
  error "Spec file not found: ${SPEC_FILE}"
  info "Run 'avangardespec pre-task' to generate the spec."
  exit 1
fi

if [[ ! -f "$SENSOR_FILE" ]]; then
  error "Sensor file not found: ${SENSOR_FILE}"
  info "Run 'avangardespec pre-task' first."
  exit 1
fi

# ── Read context ───────────────────────────────────────────────────────────────
SPEC_CONTENT=$(cat "$SPEC_FILE")
ARCH_CONTENT=""
[[ -f "harness/guides/architecture.md" ]] && ARCH_CONTENT=$(cat "harness/guides/architecture.md")
CODING_CONTENT=""
[[ -f "harness/guides/coding-rules.md" ]] && CODING_CONTENT=$(cat "harness/guides/coding-rules.md")

# Extract manual requirements from spec (between ## Manual Requirements and next ## or end)
MANUAL_REQS=$(awk '/^## Manual Requirements/{found=1; next} found && /^## /{exit} found{print}' "$SPEC_FILE" | grep -v '^[[:space:]]*$' || true)

info "Task spec: ${SPEC_FILE}"
info "Branch:    ${CURRENT_BRANCH}"
echo

# ── Surface manual blockers ────────────────────────────────────────────────────
MANUAL_UNCHECKED_NOW=$(echo "$MANUAL_REQS" | grep '^\- \[ \]' || true)

if [[ -n "$MANUAL_REQS" ]]; then
  echo
  gum style --bold --foreground 214 --border double --border-foreground 214 --padding "1 3" \
    "⚠  MANUAL SETUP REQUIRED — AI cannot do these"
  echo
  gum style --foreground 214 "Complete these steps before or during coding."
  gum style --foreground 240 "Use Space to select any you have already done, Enter to continue."
  echo
  echo "$MANUAL_REQS"
  echo

  if [[ -n "$MANUAL_UNCHECKED_NOW" ]]; then
    declare -a UNCHECKED_NOW_ITEMS=()
    while IFS= read -r line; do
      [[ -z "$line" ]] && continue
      UNCHECKED_NOW_ITEMS+=("${line#- \[ \] }")
    done <<< "$MANUAL_UNCHECKED_NOW"

    DONE_NOW=$(gum choose --no-limit \
      --header "  Mark any items you have already completed (Space = toggle, Enter = confirm):" \
      "${UNCHECKED_NOW_ITEMS[@]}" < /dev/tty) || true

    if [[ -n "$DONE_NOW" ]]; then
      while IFS= read -r done_item; do
        [[ -z "$done_item" ]] && continue
        escaped=$(printf '%s\n' "$done_item" | sed 's/[[\.*^$()+?{}|]/\\&/g')
        sed -i.bak "s/^- \[ \] *${escaped}/- [x] ${done_item}/" "$SPEC_FILE" && rm -f "${SPEC_FILE}.bak"
      done <<< "$DONE_NOW"
      success "Marked done. Remaining items will be verified at closure."
    else
      info "No items marked done yet. Remember to complete them before closure."
    fi
  fi
  echo
fi

# ── Build base context prompt ──────────────────────────────────────────────────
# Written to a temp file so claude can receive it without quoting issues
build_prompt() {
  local extra="${1:-}"
  cat <<PROMPT
You are implementing a task inside the Avangarde Harness. Read everything below carefully before writing any code.

## Task Spec
${SPEC_CONTENT}

## Architecture Rules
${ARCH_CONTENT}

## Coding Rules
${CODING_CONTENT}

${extra}

Rules:
- Implement only what the BDD spec describes. Nothing more.
- Follow every architecture and coding rule above.
- Do not modify files outside this task's scope.
- When done, say so — the harness will run sensors automatically.
- Review sensors in harness/sensors/check.sh: if a sensor only duplicates what a unit test already covers and that unit test is already part of the sensor suite, remove the redundant sensor or refactor it into the existing unit test. Only keep sensors that check structural concerns (process exit codes, file existence, CLI contracts) or things that are genuinely out of unit-test scope.
- Check that a .gitignore exists at the repo root. Detect the project stack from files present (e.g. package.json, go.mod, Cargo.toml, pyproject.toml, etc.) and ensure the .gitignore covers the standard ignored patterns for that stack (build artifacts, dependency directories, env files, editor noise, OS files). Add any missing entries — do not remove existing ones.
PROMPT
}

# ── Invoke Claude agent ────────────────────────────────────────────────────────
invoke_agent() {
  local prompt="$1"
  local prompt_file
  prompt_file=$(mktemp /tmp/harness-prompt-XXXXXX.txt)
  echo "$prompt" > "$prompt_file"

  header "Claude agent — implement task"
  divider
  info "Claude will open with the task context pre-loaded."
  info "When you are done coding, exit Claude (/exit or Escape)."
  info "Sensors will run automatically after you exit."
  divider
  echo

  # || true: claude can exit non-zero (/exit, Ctrl+C) — don't abort the loop
  claude "$(cat "$prompt_file")" || true

  rm -f "$prompt_file"
  echo
  info "Claude session ended. Running sensors..."
  echo
}

# ── Ralph loop ─────────────────────────────────────────────────────────────────
ITERATION=0
LAST_FAILURE=""
SAME_FAILURE_COUNT=0
MAX_SAME_FAILURES=2

while true; do
  ((ITERATION++)) || true
  header "Loop iteration ${ITERATION}"
  divider

  # ── Invoke agent ─────────────────────────────────────────────────────────────
  if [[ $ITERATION -eq 1 ]]; then
    PROMPT=$(build_prompt "## Instruction
Implement the task described in the spec above from scratch.")
  else
    PROMPT=$(build_prompt "## Sensor Failures to Fix
The following sensors are failing. Fix the code so all sensors pass.

${SENSOR_OUTPUT}")
  fi

  invoke_agent "$PROMPT"
  echo

  # ── Run sensors ──────────────────────────────────────────────────────────────
  info "Running sensors..."
  echo
  SENSOR_TMPFILE=$(mktemp)
  SENSOR_EXIT=0
  set +e
  bash "$SENSOR_FILE" 2>&1 | tee "$SENSOR_TMPFILE"
  SENSOR_EXIT=${PIPESTATUS[0]}
  set -e
  SENSOR_OUTPUT=$(cat "$SENSOR_TMPFILE")
  rm -f "$SENSOR_TMPFILE"
  echo

  if [[ $SENSOR_EXIT -eq 0 ]]; then
    # ── Sensors PASS ────────────────────────────────────────────────────────────
    success "All sensors pass!"
    echo

    if yes_no "Commit current changes?"; then
      bash "$SCRIPT_DIR/commit.sh"
    fi

    echo
    if yes_no "Continue the loop?"; then
      info "Continuing..."
    else
      echo
      if yes_no "Run closure now?"; then
        exec bash "$SCRIPT_DIR/closure-task.sh"
      else
        success "Exiting. Run 'avangardespec close' when ready."
        break
      fi
    fi

  else
    # ── Sensors FAIL ────────────────────────────────────────────────────────────
    error "Sensors failed (exit code: ${SENSOR_EXIT})"
    echo

    FAILURE_SUMMARY=$(echo "$SENSOR_OUTPUT" | grep -E '✗|TIMEOUT' | head -5 || true)

    if echo "$SENSOR_OUTPUT" | grep -q 'TIMEOUT'; then
      echo
      warn "One or more sensors timed out — command may be blocking or interactive."
      warn "Edit harness/sensors/check.sh and fix the flagged run_check lines."
      echo
    fi

    if [[ "$FAILURE_SUMMARY" == "$LAST_FAILURE" ]]; then
      ((SAME_FAILURE_COUNT++)) || true
    else
      SAME_FAILURE_COUNT=1
      LAST_FAILURE="$FAILURE_SUMMARY"
    fi

    if [[ $SAME_FAILURE_COUNT -ge $MAX_SAME_FAILURES ]]; then
      echo
      warn "The same failure has occurred ${SAME_FAILURE_COUNT} times in a row."
      warn "This suggests a guide, spec, or sensor issue — not just a code issue."
      echo
      info "Options:"
      echo "  1) Update harness/guides/ to clarify the rule the AI keeps breaking"
      echo "  2) Update the spec in ${SPEC_FILE} to clarify expected behavior"
      echo "  3) Update harness/sensors/check.sh if the check is wrong"
      echo "  4) Continue anyway"
      echo
      ask CHOICE "Choice (1/2/3/4)" "4"

      case "$CHOICE" in
        1)
          "${EDITOR:-nano}" "harness/guides/architecture.md"
          ;;
        2)
          "${EDITOR:-nano}" "$SPEC_FILE"
          SPEC_CONTENT=$(cat "$SPEC_FILE")
          ;;
        3)
          "${EDITOR:-nano}" "$SENSOR_FILE"
          ;;
        4)
          warn "Continuing..."
          ;;
      esac

      SAME_FAILURE_COUNT=0
      LAST_FAILURE=""
    fi
  fi
done

echo

#!/usr/bin/env bash
# execute-pre-task.sh — Pre-task phase wizard
# Picks next task, generates BDD spec, checks sensors, creates branch.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

require_git_repo

TASKS_FILE="$(tasks_file)"
SENSOR_FILE="harness/sensors/check.sh"
SPECS_DIR="specs/features"

# ── Guard ──────────────────────────────────────────────────────────────────────
if [[ ! -f "$TASKS_FILE" ]]; then
  error "No task list found at ${TASKS_FILE}. Run 'avangardespec plan' first."
  exit 1
fi

# ── Pick task ──────────────────────────────────────────────────────────────────
header "Avangarde Harness — Pre-Task"
divider

SUGGESTED=$(next_unchecked_task)

if [[ -z "$SUGGESTED" ]]; then
  success "All tasks are complete! Nothing left to do."
  exit 0
fi

info "Next task: ${SUGGESTED}"
echo
if ! yes_no "Proceed with this task?"; then
  ask CUSTOM_TASK "Enter the task you want to work on"
  TASK="$CUSTOM_TASK"
else
  TASK="$SUGGESTED"
fi

SLUG=$(slugify "$TASK")
BRANCH="task/${SLUG}"
SPEC_FILE="${SPECS_DIR}/${SLUG}.md"

# ── BDD generation ─────────────────────────────────────────────────────────────
header "Generating BDD acceptance criteria..."

VISION_CONTEXT=""
[[ -f "specs/product/vision.md" ]] && VISION_CONTEXT=$(cat "specs/product/vision.md")

BDD=$("$SCRIPT_DIR/claude.sh" \
  "Generate BDD acceptance criteria for this task: '${TASK}'
Format:
Feature: <name>

  Scenario: <scenario name>
    Given <precondition>
    When <action>
    Then <expected outcome>

Write 2-4 scenarios. Be specific. No prose." \
  <(echo "$VISION_CONTEXT"))

confirm_or_edit BDD "BDD acceptance criteria"

# ── Manual requirements detection ──────────────────────────────────────────────
header "Detecting manual setup requirements..."

MANUAL_REQS=$("$SCRIPT_DIR/claude.sh" \
  "For this task: '${TASK}'

Identify ANY manual steps a human must do that code CANNOT do automatically.
Examples: create .env file, register API key, provision database, create cloud account, configure DNS, set up OAuth app, obtain credentials.

If there are manual requirements, list them as actionable steps with:
- what needs to be done
- where to do it (URL, file path, service name)
- what value/output to capture

If there are NO manual requirements, reply with exactly: none

Format (if any):
- [ ] <step>: <where/how> → capture: <what to save>

No prose. Steps only." \
  <(echo "$VISION_CONTEXT"))

if [[ "$MANUAL_REQS" != "none" && -n "$MANUAL_REQS" ]]; then
  echo
  gum style --bold --foreground 214 "  ⚠  MANUAL SETUP REQUIRED — AI cannot do this automatically"
  divider
  echo "$MANUAL_REQS"
  divider
  echo
  confirm_or_edit MANUAL_REQS "manual requirements"
else
  MANUAL_REQS=""
  success "No manual setup required for this task."
fi

# ── Sensor check ───────────────────────────────────────────────────────────────
header "Checking sensors..."

ensure_dir "harness/sensors"
if [[ ! -f "$SENSOR_FILE" ]]; then
  warn "No check.sh found. Creating empty sensor file."
  cat > "$SENSOR_FILE" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
PASS=0; FAIL=0
run_check() {
  local name="$1"; shift
  if "$@" &>/dev/null; then echo "  ✓ ${name}"; ((PASS++)) || true
  else                       echo "  ✗ ${name}"; ((FAIL++)) || true
  fi
}
echo "Running sensors..."
# Add checks below
echo ""
echo "Results: ${PASS} passed, ${FAIL} failed"
[[ $FAIL -eq 0 ]]
EOF
  chmod +x "$SENSOR_FILE"
fi

# Infer task type for sensor suggestions
TASK_TYPE_PROMPT="Task: '${TASK}'. What type is this? Choose from: api, ui, db, config, docs, test, infra, other. One word answer."
TASK_TYPE=$("$SCRIPT_DIR/claude.sh" "$TASK_TYPE_PROMPT")
TASK_TYPE=$(echo "$TASK_TYPE" | tr -d '[:space:]' | tr '[:upper:]' '[:lower:]')

SENSOR_CONTENT=$(cat "$SENSOR_FILE")

info "Task type detected: ${TASK_TYPE}"

# Ask AI for sensor lines — ask for run_check lines only after a clear marker
SENSOR_CHECK=$("$SCRIPT_DIR/claude.sh" \
  "Current check.sh content:
${SENSOR_CONTENT}

Task type: ${TASK_TYPE}
Task: ${TASK}

Does check.sh have run_check lines that validate this task type?
Reply with exactly this format:
covered: yes|no
missing: <what is missing or 'nothing'>
---SENSOR_LINES---
<zero or more run_check lines to add, or leave blank if covered>")

divider
echo "$SENSOR_CHECK"
divider

COVERED=$(echo "$SENSOR_CHECK" | grep -i '^covered:' | sed 's/covered: *//' | tr '[:upper:]' '[:lower:]' || true)

# Extract everything after the ---SENSOR_LINES--- marker
PROPOSED=$(echo "$SENSOR_CHECK" | awk '/^---SENSOR_LINES---/{found=1; next} found{print}' | grep -v '^[[:space:]]*$' || true)

if [[ "$COVERED" == "no" && -n "$PROPOSED" ]]; then
  warn "Sensors don't cover this task type. Auto-adding..."
  echo "$PROPOSED"
  echo
  # Append after the marker line if it exists, otherwise just append at end
  if grep -q 'Add checks below' "$SENSOR_FILE"; then
    # Insert after the marker using a temp file — avoids sed special char issues
    TMPFILE=$(mktemp)
    while IFS= read -r line; do
      echo "$line" >> "$TMPFILE"
      if [[ "$line" == *"Add checks below"* ]]; then
        echo "$PROPOSED" >> "$TMPFILE"
      fi
    done < "$SENSOR_FILE"
    mv "$TMPFILE" "$SENSOR_FILE"
    chmod +x "$SENSOR_FILE"
  else
    {
      echo ""
      echo "# Added for ${TASK_TYPE} tasks"
      echo "$PROPOSED"
    } >> "$SENSOR_FILE"
  fi
  success "Sensors added to check.sh."
else
  success "Sensors already cover this task type."
fi

# ── Create spec file ───────────────────────────────────────────────────────────
header "Creating spec file..."

ensure_dir "$SPECS_DIR"

if [[ -f "$SPEC_FILE" ]]; then
  warn "Spec file already exists: ${SPEC_FILE}"
  if ! yes_no "Overwrite?"; then
    info "Keeping existing spec."
  else
    rm "$SPEC_FILE"
  fi
fi

if [[ ! -f "$SPEC_FILE" ]]; then
  MANUAL_SECTION=""
  if [[ -n "$MANUAL_REQS" ]]; then
    MANUAL_SECTION="## Manual Requirements

<!-- Complete ALL items below before or during this task. AI cannot do these. -->
${MANUAL_REQS}

---"
  fi

  cat > "$SPEC_FILE" <<SPEC_EOF
# Task Spec: ${TASK}

Slug: ${SLUG}
Branch: ${BRANCH}
Created: $(date +%Y-%m-%d)

---

## BDD Acceptance Criteria

${BDD}

---

${MANUAL_SECTION}

## Notes

<!-- Human notes appended here during execute-task.sh iterations -->
SPEC_EOF
  success "Created ${SPEC_FILE}"
fi

# ── Create branch ──────────────────────────────────────────────────────────────
header "Creating branch..."

# Ensure there is at least one commit so branches work.
# Also commit any untracked harness scaffold so it doesn't pollute task diffs.
if ! git rev-parse HEAD &>/dev/null; then
  info "No commits yet — committing harness scaffold as initial commit..."
  git add harness/ specs/ CLAUDE.md 2>/dev/null || true
  git commit --allow-empty -m "chore: init harness scaffold"
else
  # Already have commits — commit any un-committed harness files before branching
  git add harness/ specs/ CLAUDE.md 2>/dev/null || true
  if ! git diff --cached --quiet; then
    info "Committing untracked harness files before branching..."
    git commit -m "chore: update harness scaffold"
  fi
fi

CURRENT=$(current_branch)
if [[ "$CURRENT" == "$BRANCH" ]]; then
  info "Already on branch ${BRANCH}."
else
  BASE_BRANCH="main"
  [[ -f "harness/.config" ]] && source "harness/.config"

  if git show-ref --verify --quiet "refs/heads/${BRANCH}"; then
    warn "Branch ${BRANCH} already exists."
    if yes_no "Switch to it?"; then
      git checkout "$BRANCH"
    else
      info "Staying on ${CURRENT}."
    fi
  else
    git checkout -b "$BRANCH"
    success "Created and switched to branch: ${BRANCH}"
  fi
fi

# ── Final approval ─────────────────────────────────────────────────────────────
echo
header "Pre-task summary"
divider
info "Task:   ${TASK}"
info "Branch: ${BRANCH}"
info "Spec:   ${SPEC_FILE}"
divider
echo
info "BDD saved to spec:"
echo "$BDD"
echo
if yes_no "Everything looks good — ready to start coding?"; then
  echo
  success "Run 'avangardespec task' to start the Ralph loop."
else
  warn "Go back and adjust the spec or sensors before coding."
  info "Edit the spec at: ${SPEC_FILE}"
  info "Edit sensors at:  ${SENSOR_FILE}"
fi
echo

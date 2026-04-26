#!/usr/bin/env bash
# closure-task.sh — Closure phase wizard
# Validates behavior against BDD, commits, marks task done, merges branch.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

require_git_repo

SENSOR_FILE="harness/sensors/check.sh"
SPECS_DIR="specs/features"
SPECS_DONE_DIR="specs/features/done"
TASKS_FILE="$(tasks_file)"

# ── Find current task ──────────────────────────────────────────────────────────
header "Avangarde Harness — Task Closure"
divider

CURRENT_BRANCH=$(current_branch)

if [[ ! "$CURRENT_BRANCH" =~ ^task/ ]]; then
  error "Current branch is not a task branch: ${CURRENT_BRANCH}"
  info "Switch to a task branch before running 'avangardespec close'."
  exit 1
fi

SLUG="${CURRENT_BRANCH#task/}"
SPEC_FILE="${SPECS_DIR}/${SLUG}.md"

if [[ ! -f "$SPEC_FILE" ]]; then
  error "Spec file not found: ${SPEC_FILE}"
  exit 1
fi

# ── Verify sensors pass ────────────────────────────────────────────────────────
info "Verifying all sensors pass before closure..."
echo

if [[ -f "$SENSOR_FILE" ]]; then
  if ! bash "$SENSOR_FILE"; then
    error "Sensors are not all passing. Fix failures before closing the task."
    info "Run 'avangardespec task' to continue the loop."
    exit 1
  fi
else
  warn "No sensor file found. Skipping sensor check."
fi

echo

# ── Manual requirements gate ──────────────────────────────────────────────────
MANUAL_REQS=$(awk '/^## Manual Requirements/{found=1; next} found && /^## /{exit} found{print}' "$SPEC_FILE" | grep -v '^[[:space:]]*$' || true)
MANUAL_UNCHECKED=$(echo "$MANUAL_REQS" | grep '^\- \[ \]' || true)

if [[ -n "$MANUAL_REQS" ]]; then
  echo
  gum style --bold --foreground 214 --border double --border-foreground 214 --padding "1 3" \
    "⚠  MANUAL REQUIREMENTS — Verify before closing"
  echo

  if [[ -n "$MANUAL_UNCHECKED" ]]; then
    gum style --bold --foreground 214 "The following steps must be done by you (not by AI)."
    gum style --foreground 240 "Use Space to select the ones you have completed, Enter to confirm."
    echo

    # Build array of unchecked item labels (strip the "- [ ] " prefix)
    declare -a UNCHECKED_ITEMS=()
    while IFS= read -r line; do
      [[ -z "$line" ]] && continue
      UNCHECKED_ITEMS+=("${line#- \[ \] }")
    done <<< "$MANUAL_UNCHECKED"

    # Interactive multi-select: user picks which items are done
    DONE_SELECTION=$(gum choose --no-limit \
      --header "  Select completed items (Space = toggle, Enter = confirm):" \
      "${UNCHECKED_ITEMS[@]}" < /dev/tty) || true

    # Mark selected items [x] in the spec file
    if [[ -n "$DONE_SELECTION" ]]; then
      while IFS= read -r done_item; do
        [[ -z "$done_item" ]] && continue
        # Escape for sed
        escaped=$(printf '%s\n' "$done_item" | sed 's/[[\.*^$()+?{}|]/\\&/g')
        sed -i.bak "s/^- \[ \] *${escaped}/- [x] ${done_item}/" "$SPEC_FILE" && rm -f "${SPEC_FILE}.bak"
      done <<< "$DONE_SELECTION"
    fi

    # Re-check for remaining unchecked items
    STILL_UNCHECKED=$(awk '/^## Manual Requirements/{found=1; next} found && /^## /{exit} found{print}' "$SPEC_FILE" | grep '^\- \[ \]' || true)

    if [[ -n "$STILL_UNCHECKED" ]]; then
      echo
      gum style --bold --foreground 196 "  These items are still incomplete:"
      echo "$STILL_UNCHECKED" | while IFS= read -r line; do
        gum style --foreground 196 "    $line"
      done
      echo
      warn "Complete all manual requirements before closing."
      info "Re-run 'avangardespec close' when ready."
      exit 0
    fi

    success "All manual requirements marked complete."
  else
    success "All manual requirements already complete."
  fi
  echo
fi

# ── BDD vs implementation diff ────────────────────────────────────────────────
header "Validating behavior against BDD..."

SPEC_CONTENT=$(cat "$SPEC_FILE")

# Build implementation summary from git diff against base branch
BASE_BRANCH="main"
[[ -f "harness/.config" ]] && source "harness/.config"

IMPL_DIFF=$(git diff "${BASE_BRANCH}...HEAD" --stat 2>/dev/null || git diff --stat HEAD~1 2>/dev/null || echo "(diff unavailable)")
IMPL_FILES=$(git diff "${BASE_BRANCH}...HEAD" --name-only 2>/dev/null || git diff --name-only HEAD~1 2>/dev/null || echo "")

MATCH_SUMMARY=$("$SCRIPT_DIR/claude.sh" \
  "Review whether the implementation matches the BDD spec.

BDD spec:
${SPEC_CONTENT}

Files changed in this task:
${IMPL_FILES}

Diff summary:
${IMPL_DIFF}

Produce:
- match: yes / partial / no
- covered_scenarios: which BDD scenarios appear implemented
- missing_scenarios: which BDD scenarios are NOT covered (if any)
- recommendation: accept / return to loop")

divider
echo "$MATCH_SUMMARY"
divider
echo

# ── Human approval ─────────────────────────────────────────────────────────────
if yes_no "Does the behavior match the intent? (OK to close)"; then

  # ── Final commit ─────────────────────────────────────────────────────────────
  UNCOMMITTED=$(git diff --name-only; git diff --cached --name-only; git ls-files --others --exclude-standard | grep -v '^$' || true)
  if [[ -n "$UNCOMMITTED" ]]; then
    info "You have uncommitted changes. Committing before closure..."
    bash "$SCRIPT_DIR/commit.sh"
  fi

  # ── Mark task done ────────────────────────────────────────────────────────────
  info "Marking task done in ${TASKS_FILE}..."
  if [[ -f "$TASKS_FILE" ]]; then
    if mark_task_done_by_slug "$SLUG"; then
      success "Marked task done (slug: ${SLUG})"
    else
      # Fallback: try matching by spec title
      TASK_NAME=$(head -2 "$SPEC_FILE" | grep '^# Task Spec:' | sed 's/# Task Spec: *//' || true)
      if [[ -n "$TASK_NAME" ]]; then
        mark_task_done "$TASK_NAME" || true
        warn "Slug match failed — used title match: ${TASK_NAME}"
      else
        warn "Could not find matching task in ${TASKS_FILE} — mark it manually."
      fi
    fi
  else
    warn "Tasks file not found; skipping mark."
  fi

  # ── Archive spec ──────────────────────────────────────────────────────────────
  ensure_dir "$SPECS_DONE_DIR"
  ARCHIVE_NAME="${SPECS_DONE_DIR}/${SLUG}-$(date +%Y%m%d).md"
  mv "$SPEC_FILE" "$ARCHIVE_NAME"
  success "Archived spec to ${ARCHIVE_NAME}"

  # ── Merge branch ──────────────────────────────────────────────────────────────
  info "Merging ${CURRENT_BRANCH} into ${BASE_BRANCH}..."
  # Create base branch if it doesn't exist yet (first task on a new repo)
  if ! git show-ref --verify --quiet "refs/heads/${BASE_BRANCH}"; then
    info "Base branch '${BASE_BRANCH}' does not exist — creating it from current HEAD..."
    git checkout -b "$BASE_BRANCH"
    git checkout "$CURRENT_BRANCH"
  fi
  git checkout "$BASE_BRANCH"
  git merge --no-ff "$CURRENT_BRANCH" -m "merge: complete task/${SLUG}"
  success "Merged ${CURRENT_BRANCH} into ${BASE_BRANCH}"

  # ── Log progress ──────────────────────────────────────────────────────────────
  PROGRESS_FILE="harness/loops/progress.md"
  if [[ -f "$PROGRESS_FILE" ]]; then
    echo "| $(date +%Y-%m-%d) | ${TASK_NAME} | Done |" >> "$PROGRESS_FILE"
  fi

  # ── Done ──────────────────────────────────────────────────────────────────────
  echo
  header "Task closed successfully."
  divider
  info "Task:    ${TASK_NAME}"
  info "Branch:  ${CURRENT_BRANCH} (merged)"
  info "Archive: ${ARCHIVE_NAME}"
  divider
  info "Next step: run 'avangardespec pre-task' to start the next task."
  echo

else
  # ── Rejection: return to loop ─────────────────────────────────────────────────
  echo
  warn "Task rejected. Returning to the loop."
  echo

  ask REJECTION_NOTES "What needs to be fixed? (these notes will be added to the spec)"

  if [[ -n "$REJECTION_NOTES" ]]; then
    echo "" >> "$SPEC_FILE"
    echo "## Rejection Notes — $(date +%Y-%m-%d)" >> "$SPEC_FILE"
    echo "" >> "$SPEC_FILE"
    echo "$REJECTION_NOTES" >> "$SPEC_FILE"
    success "Notes added to spec."
  fi

  info "Run 'avangardespec task' to continue the loop with updated notes."
fi

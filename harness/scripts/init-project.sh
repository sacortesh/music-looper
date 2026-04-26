#!/usr/bin/env bash
# init-project.sh — Constitution phase wizard
# Run once per project to scaffold the harness and generate CLAUDE.md

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

# ── Flags ──────────────────────────────────────────────────────────────────────
SYNC_ONLY=0
for arg in "$@"; do
  case "$arg" in
    --sync-scripts) SYNC_ONLY=1 ;;
  esac
done

# ── Sync-only mode ─────────────────────────────────────────────────────────────
if [[ "$SYNC_ONLY" == "1" ]]; then
  require_git_repo
  if [[ ! -d "harness/scripts" ]]; then
    error "No harness/scripts/ directory found. Run 'avangardespec init' first."
    exit 1
  fi
  header "Syncing harness scripts..."
  for script in "$SCRIPT_DIR"/*.sh "$SCRIPT_DIR/avangardespec"; do
    [[ -f "$script" ]] || continue
    dest="harness/scripts/$(basename "$script")"
    cp "$script" "$dest"
    chmod +x "$dest"
    success "Synced $dest"
  done
  echo
  success "Scripts up to date."
  exit 0
fi

# ── Guards ─────────────────────────────────────────────────────────────────────
require_git_repo

if [[ -d "harness" ]]; then
  warn "A harness/ directory already exists in this project."
  if ! yes_no "Re-initialize? (existing files will not be overwritten)"; then
    info "Aborted."
    exit 0
  fi
fi

# ── Wizard prompts ─────────────────────────────────────────────────────────────
header "Avangarde Harness — Project Initialization"
divider
info "Answer a few questions to seed the project constitution."
echo

ask WHAT_BUILDING "What are you building?"
ask TECH_STACK    "Tech stack? (leave blank for AI suggestion)"
ask CONSTRAINTS   "Any constraints or non-goals? (leave blank to skip)"
ask BASE_BRANCH   "Default base branch for merges" "main"

echo

# ── AI proposals ──────────────────────────────────────────────────────────────
header "Generating constitution with AI..."

CONTEXT_PROMPT="Project description: ${WHAT_BUILDING}"
[[ -n "$TECH_STACK" ]]   && CONTEXT_PROMPT+=$'\nTech stack: '"${TECH_STACK}"
[[ -n "$CONSTRAINTS" ]]  && CONTEXT_PROMPT+=$'\nConstraints / non-goals: '"${CONSTRAINTS}"

TECH_STACK_FINAL="$TECH_STACK"
if [[ -z "$TECH_STACK" ]]; then
  info "Asking AI to suggest a tech stack..."
  TECH_STACK_FINAL=$("$SCRIPT_DIR/claude.sh" \
    "Suggest a minimal, appropriate tech stack for this project.
Rules:
- Match the stack to the project type (CLI tool, API, web app, library, script, etc.)
- Do NOT default to web frameworks (Next.js, React, Vercel) unless the project is explicitly a web app
- Prefer the simplest runtime that fits (e.g. plain Node.js for a CLI, not a framework)
- No cloud hosting suggestions unless the project clearly needs deployment
Format: 'key: value' pairs. Max 6 lines." \
    <(echo "$CONTEXT_PROMPT"))
  confirm_or_edit TECH_STACK_FINAL "tech stack"
fi

info "Generating vision statement..."
VISION=$("$SCRIPT_DIR/claude.sh" \
  "Write a concise product vision for this project. 3-5 bullet points. What it is, who it serves, what outcome it delivers." \
  <(echo "$CONTEXT_PROMPT"))
confirm_or_edit VISION "vision"

info "Generating data model outline..."
DATA_MODEL=$("$SCRIPT_DIR/claude.sh" \
  "List the core data entities and their key fields for this project. Format: entity: field1, field2, ..." \
  <(echo "$CONTEXT_PROMPT"))
confirm_or_edit DATA_MODEL "data model"

info "Generating architecture rules..."
ARCH_RULES=$("$SCRIPT_DIR/claude.sh" \
  "List 5-8 architecture rules for this project. Short, imperative sentences. e.g. 'Separate concerns: one file per responsibility.'" \
  <(echo "$CONTEXT_PROMPT"))
confirm_or_edit ARCH_RULES "architecture rules"

info "Generating coding rules..."
CODING_RULES=$("$SCRIPT_DIR/claude.sh" \
  "List file-type specific coding rules for this project. Format: '.ext: rule'. Cover the main file types in the stack." \
  <(echo "$CONTEXT_PROMPT"))
confirm_or_edit CODING_RULES "coding rules"

info "Generating branching strategy..."
BRANCH_STRATEGY=$("$SCRIPT_DIR/claude.sh" \
  "Propose a simple branching strategy for this project. Format: 'key: value' pairs. Include: branch naming, merge target, and hotfix approach." \
  <(echo "$CONTEXT_PROMPT"))
confirm_or_edit BRANCH_STRATEGY "branching strategy"

# ── Scaffold project structure ─────────────────────────────────────────────────
header "Scaffolding project structure..."

ensure_dir "harness/guides"
ensure_dir "harness/sensors"
ensure_dir "harness/loops"
ensure_dir "harness/scripts"
ensure_dir "specs/product"
ensure_dir "specs/features/done"

# ── Write guide files ──────────────────────────────────────────────────────────
write_if_absent() {
  local path="$1"
  local content="$2"
  if [[ ! -f "$path" ]]; then
    echo "$content" > "$path"
    success "Created $path"
  else
    info "Skipped $path (already exists)"
  fi
}

write_if_absent "specs/product/vision.md" "# Vision

${VISION}

---

## Tech Stack

${TECH_STACK_FINAL}

---

## Data Model

${DATA_MODEL}

---

## Constraints and Non-Goals

${CONSTRAINTS:-None specified.}
"

write_if_absent "harness/guides/architecture.md" "# Architecture Rules

${ARCH_RULES}

---

## Branching Strategy

${BRANCH_STRATEGY}

Base branch: ${BASE_BRANCH}
"

write_if_absent "harness/guides/coding-rules.md" "# Coding Rules

${CODING_RULES}
"

write_if_absent "harness/guides/AGENTS.md" "# Agent Instructions

You are working inside the Avangarde Harness.

Before every coding session:
1. Read \`harness/loops/tasks.md\` — identify the current task and branch.
2. Read \`harness/guides/architecture.md\` — respect every rule listed.
3. Read \`harness/guides/coding-rules.md\` — apply file-type rules.
4. Read the spec file in \`specs/features/\` for the current task.

During coding:
- Stay within the scope of the current task.
- After every change, run \`harness/sensors/check.sh\`.
- If the same sensor fails twice, stop and surface the issue to the human.

Never:
- Modify files outside the current task scope without explicit approval.
- Skip or comment out sensor checks.
- Introduce code that is not covered by the current BDD spec.
"

# Seed check.sh only if it doesn't exist
if [[ ! -f "harness/sensors/check.sh" ]]; then
  cat > "harness/sensors/check.sh" <<'SENSOR_EOF'
#!/usr/bin/env bash
# check.sh — harness sensor aggregator
# Add checks below as new task types are encountered.
# Each check should exit 0 on pass, non-zero on fail.

set -euo pipefail

PASS=0
FAIL=0

TIMEOUT_CMD="${TIMEOUT_CMD:-}"
run_check() {
  local name="\$1"
  shift
  local exit_code=0
  if [[ -n "\$TIMEOUT_CMD" ]]; then
    "\$TIMEOUT_CMD" 10s "\$@" </dev/null &>/dev/null || exit_code=\$?
  else
    "\$@" </dev/null &>/dev/null || exit_code=\$?
  fi
  if [[ \$exit_code -eq 124 ]]; then
    echo "  ✗ \${name}  [TIMEOUT — command is blocking or interactive]"
    ((FAIL++)) || true
  elif [[ \$exit_code -eq 0 ]]; then
    echo "  ✓ \${name}"
    ((PASS++)) || true
  else
    echo "  ✗ \${name}  [exit \$exit_code]"
    ((FAIL++)) || true
  fi
}

echo "Running sensors..."

# ── Add sensors below this line ────────────────────────────────────────────────
# Example: run_check "Unit tests" npm test
# Example: run_check "Lint" npm run lint
# Example: run_check "Typecheck" npx tsc --noEmit

# ── End sensors ────────────────────────────────────────────────────────────────

echo ""
echo "Results: ${PASS} passed, ${FAIL} failed"
[[ $FAIL -eq 0 ]]
SENSOR_EOF
  chmod +x "harness/sensors/check.sh"
  success "Created harness/sensors/check.sh"
fi

# Seed tasks.md if it doesn't exist
if [[ ! -f "harness/loops/tasks.md" ]]; then
  cat > "harness/loops/tasks.md" <<TASKS_EOF
# Task List

Run \`avangardespec plan\` to generate the phased task list.

TASKS_EOF
  success "Created harness/loops/tasks.md"
fi

# Seed progress.md
write_if_absent "harness/loops/progress.md" "# Progress Log

| Date | Task | Outcome |
|------|------|---------|
"

# ── Copy harness scripts into project ─────────────────────────────────────────
info "Installing harness scripts into harness/scripts/..."
for script in "$SCRIPT_DIR"/*.sh "$SCRIPT_DIR/avangardespec"; do
  [[ -f "$script" ]] || continue
  dest="harness/scripts/$(basename "$script")"
  if [[ ! -f "$dest" ]]; then
    cp "$script" "$dest"
    chmod +x "$dest"
    success "Installed $dest"
  else
    info "Skipped $dest (already exists)"
  fi
done

# ── Generate CLAUDE.md ─────────────────────────────────────────────────────────
if [[ ! -f "CLAUDE.md" ]]; then
  cat > "CLAUDE.md" <<CLAUDE_EOF
# Harness Instructions

This project uses the Avangarde Harness. Before doing anything:

1. Read \`harness/loops/tasks.md\` — find the current task and branch.
2. Read \`harness/guides/\` — architecture rules, coding rules, AGENTS.md.
3. Do not code outside the current task scope.
4. Run \`harness/sensors/check.sh\` after every change.

## Available commands

Use these to progress through the harness phases:

\`\`\`
avangardespec pre-task   → before coding a new task (BDD spec + branch)
avangardespec task       → start the Ralph loop (AI coding + sensors)
avangardespec close      → close task after sensors pass
avangardespec scope      → add a new task or feature mid-project
\`\`\`

## Current base branch: ${BASE_BRANCH}

## Project guides

- \`harness/guides/AGENTS.md\`       — agent behavior rules
- \`harness/guides/architecture.md\` — architecture decisions
- \`harness/guides/coding-rules.md\` — file-type coding rules
- \`specs/product/vision.md\`        — product vision and data model
CLAUDE_EOF
  success "Created CLAUDE.md"
else
  info "Skipped CLAUDE.md (already exists)"
fi

# ── Store base branch in harness config ───────────────────────────────────────
echo "BASE_BRANCH=${BASE_BRANCH}" > "harness/.config"
success "Saved harness config"

# ── Commit scaffold so task branches start clean ───────────────────────────────
if git rev-parse --git-dir &>/dev/null; then
  info "Committing harness scaffold..."
  git add harness/ specs/ CLAUDE.md 2>/dev/null || true
  # Only commit if there is something staged
  if ! git diff --cached --quiet; then
    git commit -m "chore: init harness scaffold"
    success "Scaffold committed."
  else
    info "Nothing to commit (scaffold already tracked)."
  fi
fi

# ── Done ───────────────────────────────────────────────────────────────────────
echo
header "Harness initialized successfully."
divider
info "Next step: run 'avangardespec plan' to create the task list."
echo

#!/usr/bin/env bash
# adopt-project.sh — Retrofit an existing project into the Avangarde Harness.
# Scans docs and code, extracts vision, assembles a task list with done/not-done.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/_lib.sh"

require_git_repo

# ── Guard ──────────────────────────────────────────────────────────────────────
if [[ -d "harness" ]]; then
  warn "A harness/ directory already exists."
  if ! yes_no "Re-adopt? (existing harness files will not be overwritten)"; then
    info "Aborted."
    exit 0
  fi
fi

header "Avangarde Harness — Adopt Existing Project"
divider
info "This wizard will analyse your project and build a harness around it."
echo

# ── Step 1: Quick human context ────────────────────────────────────────────────
header "Step 1 — What is this project?"
ask PROJECT_DESC "Describe the project in a few sentences (what it does, who it's for)"
ask WHAT_MISSING "What do you think is still missing for this to be considered done?"
ask BASE_BRANCH  "Base branch" "main"
echo

# ── Step 2: Scan markdown docs ────────────────────────────────────────────────
header "Step 2 — Scanning documentation..."
divider

# Find all .md files, prioritise likely-important ones
ALL_MDS=$(find . \
  -not -path '*/node_modules/*' \
  -not -path '*/.git/*' \
  -not -path '*/harness/*' \
  -name '*.md' | sort)

if [[ -z "$ALL_MDS" ]]; then
  warn "No markdown files found."
  DOC_CONTENT=""
else
  info "Found markdown files:"
  echo "$ALL_MDS" | while read -r f; do echo "  $f"; done
  echo

  # Let user exclude noise
  if yes_no "Exclude any of these from analysis?"; then
    ask EXCLUDED "Enter filenames to exclude (space-separated, or leave blank)"
  else
    EXCLUDED=""
  fi

  # Read content of relevant docs (cap each file at 100 lines to stay within context)
  DOC_CONTENT=""
  while IFS= read -r f; do
    [[ -z "$f" ]] && continue
    # Skip excluded
    skip=0
    for ex in $EXCLUDED; do
      [[ "$f" == *"$ex"* ]] && skip=1 && break
    done
    [[ $skip -eq 1 ]] && continue

    DOC_CONTENT+="
=== $f ===
$(head -100 "$f" 2>/dev/null || true)
"
  done <<< "$ALL_MDS"
fi

# ── Step 3: Scan code structure ────────────────────────────────────────────────
header "Step 3 — Scanning code structure..."
divider

# Build a file tree (no content, just paths) excluding noise dirs
FILE_TREE=$(find . \
  -not -path '*/node_modules/*' \
  -not -path '*/.git/*' \
  -not -path '*/harness/*' \
  -not -path '*/.next/*' \
  -not -path '*/dist/*' \
  -not -path '*/build/*' \
  -not -path '*/__pycache__/*' \
  -not -name '*.md' \
  -type f | sort | head -200)

info "Found $(echo "$FILE_TREE" | wc -l | tr -d ' ') source files."
echo

# Read key files that are likely to reveal implemented features
# (entry points, main modules, package manifests, config files)
KEY_PATTERNS=("package.json" "pyproject.toml" "go.mod" "Cargo.toml" "Makefile"
              "index.*" "main.*" "app.*" "cli.*" "server.*" "routes.*")

KEY_FILE_CONTENT=""
for pattern in "${KEY_PATTERNS[@]}"; do
  while IFS= read -r f; do
    [[ -z "$f" ]] && continue
    KEY_FILE_CONTENT+="
=== $f ===
$(head -60 "$f" 2>/dev/null || true)
"
  done < <(find . \
    -not -path '*/node_modules/*' \
    -not -path '*/.git/*' \
    -not -path '*/harness/*' \
    -name "$pattern" -type f 2>/dev/null | head -3)
done

# ── Step 4: Extract vision via AI ─────────────────────────────────────────────
header "Step 4 — Extracting vision from docs and code..."

VISION_RAW=$("$SCRIPT_DIR/claude.sh" \
  "Analyse this project and extract:
- vision: what the project does and who it's for (2-4 bullets)
- tech_stack: key: value pairs for language, framework, infra
- data_model: core entities and fields (if any)
- stated_goals: goals or features explicitly mentioned in the docs

Human context: ${PROJECT_DESC}

Docs:
${DOC_CONTENT:-none found}

File tree:
${FILE_TREE}

Key files:
${KEY_FILE_CONTENT:-none found}")

confirm_or_edit VISION_RAW "extracted vision"

# ── Step 5: Determine what is done vs missing ─────────────────────────────────
header "Step 5 — Analysing what is done and what is missing..."

TASK_ANALYSIS=$("$SCRIPT_DIR/claude.sh" \
  "Based on the project vision, docs, and code structure below, produce a task list.

For each task:
- Mark [x] if the code clearly implements it
- Mark [ ] if it is missing, incomplete, or only partially done

Format exactly like this — no other text:
## Phase 1 — <name>
[x] <completed task>
[ ] <missing task>

## Phase 2 — <name>
[ ] <missing task>

Use 2-4 phases. Base phases on natural project layers (foundation, core features, polish, etc).

Vision:
${VISION_RAW}

Human says is missing: ${WHAT_MISSING}

File tree:
${FILE_TREE}

Key files:
${KEY_FILE_CONTENT:-none}")

divider
echo "$TASK_ANALYSIS"
divider
echo

# ── Step 6: Walk phases for human approval ─────────────────────────────────────
header "Step 6 — Review proposed task list"

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
done <<< "$TASK_ANALYSIS"
if [[ -n "$_cur_header" ]]; then
  PHASE_HEADERS+=("$_cur_header")
  PHASE_BODIES+=("$_cur_body")
fi

if [[ "${HARNESS_VERBOSE:-0}" == "1" ]]; then
  echo "  [debug] parsed ${#PHASE_HEADERS[@]} phases" >&2
fi

APPROVED_PLAN=""
for i in "${!PHASE_HEADERS[@]}"; do
  echo
  info "Phase $((i+1)) of ${#PHASE_HEADERS[@]}: ${PHASE_HEADERS[$i]}"
  echo "${PHASE_BODIES[$i]}"
  divider
  if yes_no "Accept this phase?"; then
    APPROVED_PLAN+="${PHASE_HEADERS[$i]}"$'\n'"${PHASE_BODIES[$i]}"$'\n\n'
  else
    ask_multiline EDITED_TASKS "Edit tasks for this phase (one [ ] or [x] task per line)"
    APPROVED_PLAN+="${PHASE_HEADERS[$i]}"$'\n'"${EDITED_TASKS}"$'\n\n'
  fi
done

# ── Step 7: Scaffold harness ───────────────────────────────────────────────────
header "Step 7 — Scaffolding harness..."

ensure_dir "harness/guides"
ensure_dir "harness/sensors"
ensure_dir "harness/loops"
ensure_dir "harness/scripts"
ensure_dir "specs/product"
ensure_dir "specs/features/done"

write_if_absent() {
  local path="$1" content="$2"
  if [[ ! -f "$path" ]]; then
    echo "$content" > "$path"
    success "Created $path"
  else
    info "Skipped $path (already exists)"
  fi
}

write_if_absent "specs/product/vision.md" "# Vision

${VISION_RAW}

---

## Human Notes

What is missing for closure: ${WHAT_MISSING}
"

write_if_absent "harness/guides/AGENTS.md" "# Agent Instructions

You are working inside the Avangarde Harness on an existing project.

Before every coding session:
1. Read \`harness/loops/tasks.md\` — find the current task and branch.
2. Read \`harness/guides/architecture.md\` — respect every rule listed.
3. Read \`harness/guides/coding-rules.md\` — apply file-type rules.
4. Read the spec file in \`specs/features/\` for the current task.

During coding:
- Stay within the scope of the current task.
- After every change, run \`harness/sensors/check.sh\`.
- If the same sensor fails twice, stop and surface the issue to the human.
"

write_if_absent "harness/guides/architecture.md" "# Architecture Rules

<!-- Populated from project analysis — edit to reflect actual rules -->
${VISION_RAW}
"

write_if_absent "harness/guides/coding-rules.md" "# Coding Rules

<!-- Add file-type specific rules here as you discover them -->
"

if [[ ! -f "harness/sensors/check.sh" ]]; then
  cat > "harness/sensors/check.sh" <<'SENSOR_EOF'
#!/usr/bin/env bash
# check.sh — harness sensor aggregator
set -euo pipefail
PASS=0; FAIL=0
TIMEOUT_CMD="${TIMEOUT_CMD:-}"
run_check() {
  local name="$1"; shift
  local exit_code=0
  if [[ -n "$TIMEOUT_CMD" ]]; then
    "$TIMEOUT_CMD" 10s "$@" </dev/null &>/dev/null || exit_code=$?
  else
    "$@" </dev/null &>/dev/null || exit_code=$?
  fi
  if [[ $exit_code -eq 124 ]]; then
    echo "  ✗ ${name}  [TIMEOUT — command is blocking]"
    ((FAIL++)) || true
  elif [[ $exit_code -eq 0 ]]; then
    echo "  ✓ ${name}"; ((PASS++)) || true
  else
    echo "  ✗ ${name}  [exit $exit_code]"; ((FAIL++)) || true
  fi
}
echo "Running sensors..."
# ── Add checks below ──────────────────────────────────────────────────────────

# ── End sensors ───────────────────────────────────────────────────────────────
echo ""
echo "Results: ${PASS} passed, ${FAIL} failed"
[[ $FAIL -eq 0 ]]
SENSOR_EOF
  chmod +x "harness/sensors/check.sh"
  success "Created harness/sensors/check.sh"
fi

write_if_absent "harness/loops/tasks.md" "# Task List

${APPROVED_PLAN}
"

write_if_absent "harness/loops/progress.md" "# Progress Log

| Date | Task | Outcome |
|------|------|---------|
"

# Copy harness scripts
info "Installing harness scripts..."
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

echo "BASE_BRANCH=${BASE_BRANCH}" > "harness/.config"

write_if_absent "CLAUDE.md" "# Harness Instructions

This project uses the Avangarde Harness. Before doing anything:

1. Read \`harness/loops/tasks.md\` — find the current task and branch.
2. Read \`harness/guides/\` — architecture rules, coding rules, AGENTS.md.
3. Do not code outside the current task scope.
4. Run \`harness/sensors/check.sh\` after every change.

## Available commands

\`\`\`
avangardespec pre-task   → before coding a new task (BDD spec + branch)
avangardespec task       → start the Ralph loop (AI coding + sensors)
avangardespec close      → close task after sensors pass
avangardespec scope      → add a new task or feature mid-project
\`\`\`

## Base branch: ${BASE_BRANCH}
"

# Commit scaffold
git add harness/ specs/ CLAUDE.md 2>/dev/null || true
if ! git diff --cached --quiet; then
  git commit -m "chore: adopt project into harness"
  success "Scaffold committed."
fi

# ── Done ───────────────────────────────────────────────────────────────────────
echo
header "Project adopted successfully."
divider
DONE_COUNT=$(echo "$APPROVED_PLAN" | grep -c '^\[x\]' || true)
TODO_COUNT=$(echo "$APPROVED_PLAN" | grep -c '^\[ \]' || true)
info "Tasks done:    ${DONE_COUNT}"
info "Tasks to do:   ${TODO_COUNT}"
divider
info "Next: run 'avangardespec pre-task' to start the first open task."
echo

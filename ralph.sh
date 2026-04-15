#!/bin/bash
# ralph.sh — AI-assisted development loop
#
# Usage:
#   ./ralph.sh [mode] [max_iterations] ["feature description"]
#
# Modes:
#   build  (default) — fully autonomous: implements one task per iteration
#   plan             — planning only: updates PRD.md with tasks/features
#   teach            — guided mode: Claude teaches, you implement
#
# Examples:
#   ./ralph.sh                                    # build, unlimited iterations
#   ./ralph.sh build 5                            # build, up to 5 tasks
#   ./ralph.sh plan 1 "add user authentication"   # plan a new feature
#   ./ralph.sh teach                              # start a teaching session
#
# Project files (auto-created on first run):
#   PRD.md      — Product Requirements Doc with phases & tasks
#   progress.md — Tracks completed tasks and test counts
#   lessons.md  — Records decisions, feedback, learnings (read every iteration)
#   bdd/        — Gherkin integration tests, one .feature file per feature

# jq stream filters
STREAM_TEXT='select(.type == "assistant").message.content[]? | select(.type == "text").text // empty'
FINAL_RESULT='select(.type == "result").result // empty'

# ---------------------------------------------------------------------------
# Parse arguments
# ---------------------------------------------------------------------------
MODE="${1:-build}"
if [[ "$MODE" != "plan" && "$MODE" != "build" && "$MODE" != "teach" ]]; then
    echo "Usage: ./ralph.sh [build|plan|teach] [max_iterations] [\"feature description\"]"
    echo ""
    echo "  build  (default)  autonomous implementation, one task at a time"
    echo "  plan              plan new features/tasks into PRD.md"
    echo "  teach             guided mode: Claude teaches, you implement"
    exit 1
fi

MAX_ITERATIONS=${2:-0}
FEATURE_DESC="${3:-}"

# ---------------------------------------------------------------------------
# Init: ensure project files exist, create them if not
# ---------------------------------------------------------------------------
init_project() {
    local created=0

    if [ ! -f "PRD.md" ]; then
        echo ""
        echo "┌──────────────────────────────────────────┐"
        echo "│  No PRD.md found — let's start fresh.    │"
        echo "└──────────────────────────────────────────┘"
        echo ""
        printf "What do you want to build? > "
        read -r PROJECT_DESC
        echo ""

        cat > PRD.md <<EOF
# PRD: ${PROJECT_DESC}

## Overview
${PROJECT_DESC}

## Phases & Tasks

> This PRD was just created. Run \`./ralph.sh plan 1 "<feature>"\` to flesh it out,
> or run \`./ralph.sh build\` and Claude will plan before implementing.

EOF
        echo "  Created PRD.md"
        created=1
    fi

    if [ ! -f "progress.md" ]; then
        cat > progress.md <<EOF
# Progress

## Completed Tasks
_Nothing completed yet._

## Test Counts
| Type | Count |
|------|-------|
| Unit |   0   |
| BDD  |   0   |
EOF
        echo "  Created progress.md"
        created=1
    fi

    if [ ! -f "lessons.md" ]; then
        cat > lessons.md <<EOF
# Lessons & Decisions

> Claude reads this at the start of every iteration.
> It records key decisions, user feedback, and technical learnings accumulated
> across sessions so context is never lost.

## Decisions
_No decisions recorded yet._

## Feedback
_No feedback recorded yet._

## Learnings
_No learnings recorded yet._
EOF
        echo "  Created lessons.md"
        created=1
    fi

    [ "$created" -eq 1 ] && echo ""
}

# ---------------------------------------------------------------------------
# Git helpers (gracefully no-op when not in a git repo)
# ---------------------------------------------------------------------------
IS_GIT_REPO=false
CURRENT_BRANCH=""
if git rev-parse --is-inside-work-tree &>/dev/null 2>&1; then
    IS_GIT_REPO=true
    CURRENT_BRANCH=$(git branch --show-current)
fi

push_changes() {
    if [ "$IS_GIT_REPO" = true ] && [ -n "$CURRENT_BRANCH" ]; then
        git push origin "$CURRENT_BRANCH" 2>/dev/null || \
        git push -u origin "$CURRENT_BRANCH" 2>/dev/null || \
        echo "  (skipping push — no remote configured)"
    fi
}

# ---------------------------------------------------------------------------
# Run init
# ---------------------------------------------------------------------------
init_project

# ---------------------------------------------------------------------------
# Header
# ---------------------------------------------------------------------------
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Mode:    $MODE"
[ "$IS_GIT_REPO" = true ] && echo "  Branch:  $CURRENT_BRANCH"
[ "$MAX_ITERATIONS" -gt 0 ] 2>/dev/null && echo "  Max:     $MAX_ITERATIONS iterations"
[ -n "$FEATURE_DESC" ] && echo "  Feature: $FEATURE_DESC"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# ---------------------------------------------------------------------------
# TEACH mode — interactive session, Claude guides, user implements
# ---------------------------------------------------------------------------
if [ "$MODE" = "teach" ]; then
    claude --permission-mode acceptEdits \
"@PRD.md @progress.md @lessons.md

You are a patient senior developer in a teaching session with a junior developer.

At the start of each task:
1. Read PRD.md, progress.md, and lessons.md for full context.
2. Identify the next incomplete task.
3. Explain clearly what needs to be done and why — as if teaching.
   Give hints, patterns to follow, and pointers to relevant files.
   Do NOT write the code yourself; guide the user to write it.
4. Ask the user to implement it and come back when done.

When the user says they are done:
5. Run \`git diff\` (or inspect changed files) to review the implementation.
6. Give detailed feedback:
   - What was done well
   - What could be improved or refactored
   - Any technical debt introduced
   - Topics the user should study to level up
7. For backend or script code: check whether a Gherkin feature file exists in bdd/.
   If not, guide the user to create bdd/<feature-name>.feature with 1–3 minimal
   scenarios that a person can verify manually.
8. Commit the changes with a clear message.
9. Update progress.md: mark the task done, update test counts.
10. Append any key decisions or learnings from this session to lessons.md.
11. Ask the user if they want to continue to the next task.

WE ONLY DO ONE TASK AT A TIME."
    exit 0
fi

# ---------------------------------------------------------------------------
# BDD / testing instructions injected into build & plan prompts
# ---------------------------------------------------------------------------
BDD_INSTRUCTIONS='Testing strategy:
- Write unit tests only where they provide clear value (skip trivial boilerplate).
- For any backend or script feature: create or update bdd/<feature-name>.feature
  with Gherkin scenarios. Keep it minimal (1–3 scenarios) and manually verifiable.
  Example:
    Feature: User login
      Scenario: Successful login with valid credentials
        Given a registered user "alice" with password "secret"
        When she submits the login form with correct credentials
        Then she is redirected to the dashboard and sees a welcome message'

# ---------------------------------------------------------------------------
# Build / Plan prompts
# ---------------------------------------------------------------------------
if [ "$MODE" = "build" ]; then
    PROMPT="@PRD.md @progress.md @lessons.md
1. Read PRD.md, progress.md, and lessons.md for full context including past decisions.
2. Find the next incomplete task — the first one not yet marked done in progress.md.
   If PRD.md has no tasks yet, add a first set of concrete tasks before implementing.
3. Implement that single task with clean, minimal code.
4. ${BDD_INSTRUCTIONS}
5. Commit your changes with a descriptive message.
6. Update progress.md: mark the task done, update test counts.
7. If you made a significant decision or learned something worth preserving,
   append it to lessons.md under the appropriate section.
ONLY DO ONE TASK PER ITERATION. Stop after committing and updating progress."

elif [ "$MODE" = "plan" ]; then
    FEATURE_LINE=""
    if [ -n "$FEATURE_DESC" ]; then
        FEATURE_LINE="
NEW FEATURE REQUEST: \"$FEATURE_DESC\"
Plan the concrete tasks needed to implement this feature."
    fi

    PROMPT="@PRD.md @progress.md @lessons.md
1. Read PRD.md, progress.md, and lessons.md for context.
2. Review what has been built so far vs. what remains.${FEATURE_LINE}
3. Add well-structured phases and tasks to PRD.md.
   Tasks must be: small, concrete, actionable, and ordered by dependency.
4. Commit the updated PRD.md with a clear message.
5. Update progress.md if structural changes affect completed work.
ONLY PLAN — do not implement any code. Stop after committing."
fi

# ---------------------------------------------------------------------------
# Main loop (build / plan)
# ---------------------------------------------------------------------------
ITERATION=0

while true; do
    if [ "$MAX_ITERATIONS" -gt 0 ] 2>/dev/null && [ "$ITERATION" -ge "$MAX_ITERATIONS" ]; then
        echo "Reached max iterations: $MAX_ITERATIONS"
        break
    fi

    TMPFILE=$(mktemp)
    trap "rm -f '$TMPFILE'" EXIT

    claude -p "$PROMPT" \
        --dangerously-skip-permissions \
        --output-format=stream-json \
        --model opus \
        --verbose \
    | grep --line-buffered '^{' \
    | tee "$TMPFILE" \
    | jq --unbuffered -rj "$STREAM_TEXT"

    echo ""

    push_changes

    ITERATION=$((ITERATION + 1))
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "  Loop $ITERATION done"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    # Exit after a single-shot plan run
    if [ "$MAX_ITERATIONS" -eq 1 ] 2>/dev/null; then
        break
    fi
done
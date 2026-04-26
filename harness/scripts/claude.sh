#!/usr/bin/env bash
# claude.sh — thin wrapper around the Claude CLI
#
# Usage:
#   ./claude.sh "<prompt>" [context_file]
#
# Returns a short, structured response on stdout.
# Set HARNESS_VERBOSE=1 to print full prompt and response to stderr.

set -euo pipefail

PROMPT="${1:-}"
CONTEXT_FILE="${2:-}"

if [[ -z "$PROMPT" ]]; then
  echo "Usage: claude.sh \"<prompt>\" [context_file]" >&2
  exit 1
fi

SYSTEM_PROMPT='You are a structured assistant inside a development harness.
Rules:
- No prose introductions or conclusions
- No markdown headers (##, ###)
- Use bullet lists or "key: value" pairs only
- Max 20 lines total
- Be specific and actionable
- Never refuse to answer on topic'

# Build the full prompt, optionally prepending context
FULL_PROMPT="$PROMPT"
if [[ -n "$CONTEXT_FILE" && -r "$CONTEXT_FILE" ]]; then
  CONTEXT=$(cat "$CONTEXT_FILE")
  FULL_PROMPT="Context:
${CONTEXT}

Task:
${PROMPT}"
fi

# ── Verbose: log what we're sending ───────────────────────────────────────────
if [[ "${HARNESS_VERBOSE:-0}" == "1" ]]; then
  {
    echo ""
    echo "┌─ claude.sh ── SYSTEM PROMPT ─────────────────────────────────────"
    echo "$SYSTEM_PROMPT"
    echo "├─ FULL PROMPT ────────────────────────────────────────────────────"
    echo "$FULL_PROMPT"
    echo "└──────────────────────────────────────────────────────────────────"
    echo ""
  } >&2
fi

# ── Invoke Claude CLI ──────────────────────────────────────────────────────────
if ! command -v claude &>/dev/null; then
  echo "ERROR: 'claude' CLI not found. Install it with: npm install -g @anthropic-ai/claude-code" >&2
  exit 1
fi

RESPONSE=$(claude --print \
                  --system-prompt "$SYSTEM_PROMPT" \
                  "$FULL_PROMPT")

# ── Verbose: log the response ──────────────────────────────────────────────────
if [[ "${HARNESS_VERBOSE:-0}" == "1" ]]; then
  {
    echo "┌─ claude.sh ── RESPONSE ───────────────────────────────────────────"
    echo "$RESPONSE"
    echo "└──────────────────────────────────────────────────────────────────"
    echo ""
  } >&2
fi

echo "$RESPONSE"

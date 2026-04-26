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
```bash
run_check "go-dsp not in go.mod" bash -c "! grep -q 'go-dsp\|mjibson' go.mod"
run_check "no FFT import in source" bash -c "! grep -rq 'go-dsp\|mjibson/dsp\|fft\\.FFT' --include='*.go' ."
run_check "BPM uses heuristic autocorrelation" bash -c "grep -q 'estimateBPM' main.go"
```

# ── End sensors ───────────────────────────────────────────────────────────────
echo ""
echo "Results: ${PASS} passed, ${FAIL} failed"
[[ $FAIL -eq 0 ]]

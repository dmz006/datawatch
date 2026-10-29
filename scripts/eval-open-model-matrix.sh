# Run with: bash scripts/eval-open-model-matrix.sh (no shebang: repo lint forbids literal system-binary paths).
# Open-model matrix eval driver (4 pi-prompt roles x 4 task types = 16 cells).
#
# Fixture:  testdata/eval/open-model-matrix/matrix.json (specs/verifiers from
#           testdata/eval/model-tiering)
# State:    .datawatch/eval/open-model-matrix/<run-id>/state.json (gitignored)
# Results:  docs/testing/runs/<YYYY-MM-DD>-open-model-matrix/results.{json,md}
#
# All logic lives in scripts/eval_open_model_matrix.py (stdlib only, run with
# `python3 -I`). This wrapper owns dispatch and timestamped phase lines.
# Daemon: DW_BASE (default https://127.0.0.1:8443), DW_BASE_TOKEN,
# DW_BASE_INSECURE (default 1).
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PY_MOD="$REPO/scripts/eval_open_model_matrix.py"
SELF="scripts/eval-open-model-matrix.sh"
PY="$(command -v python3)" || { echo "python3 not found" >&2; exit 1; }

ts() { date '+%H:%M:%S'; }
phase() { echo "[$(ts)] $*"; }
py() { "$PY" -I "$PY_MOD" "$@"; }

usage() {
  cat <<EOF
Usage: $SELF <subcommand> [options]

  preflight   --run-id ID   llm test each role; used_model must equal expected model exactly
  setup       --run-id ID   seed 16 cell dirs from each task type's baseline
  create-prd  --run-id ID   ONE scratch Automaton, 1 story, 16 tasks (refuses unless preflight passed)
  run         --run-id ID [--timeout S] [--poll S]   approve + run, poll until terminal
  collect     --run-id ID   per-cell model_identity / verification_integrity / artifact_check / model_result
  report      --run-id ID   write results.json + results.md
  cleanup     --run-id ID [--offline]   cancel scratch Automaton, remove cell dirs
  self-test                 offline verdict-classification checks
EOF
}

[ $# -ge 1 ] || { usage; exit 2; }
cmd="$1"; shift
case "$cmd" in
  preflight|setup|create-prd|run|collect|report|cleanup|self-test)
    phase "start $cmd"
    T=$(date +%s)
    rc=0
    py "$cmd" "$@" || rc=$?
    phase "done $cmd rc=$rc ($(( $(date +%s) - T ))s)"
    exit "$rc"
    ;;
  -h|--help|help) usage ;;
  *) usage; exit 2 ;;
esac

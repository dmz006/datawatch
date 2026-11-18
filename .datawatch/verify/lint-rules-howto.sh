#!/usr/bin/env bash
# .datawatch/verify/lint-rules-howto.sh
# Verifies the howto documents all four new decompose-lint rules, in the source doc and in the
# embedded copy after make sync-docs, and that the howto coverage test still passes.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

# Doc change: sync the embedded docs copy before checking it.
make sync-docs

for FOR_ID in artefact_files_allowed final_gate_artefact_allowed script_invokes_known_flag check_matches_spec_text; do
  grep -q "${FOR_ID}" docs/howto/automaton-decompose-lint.md \
    || { echo "FAIL: ${FOR_ID} missing from docs/howto/automaton-decompose-lint.md"; exit 1; }
done
echo 'ok: all four rule ids documented in the howto source'

EMBED_HOWTO=$(find internal/server/web -path '*howto/automaton-decompose-lint.md' | sort | head -n 1)
[ -n "${EMBED_HOWTO}" ] || { echo "FAIL: embedded docs copy of the howto not found after make sync-docs"; exit 1; }
for FOR_ID in artefact_files_allowed final_gate_artefact_allowed script_invokes_known_flag check_matches_spec_text; do
  grep -q "${FOR_ID}" "${EMBED_HOWTO}" \
    || { echo "FAIL: ${FOR_ID} missing from the embedded howto (${EMBED_HOWTO})"; exit 1; }
done
echo 'ok: embedded copy documents all four rule ids'

# The howto coverage gate is the shell script (there is no Go package in scripts/):
bash scripts/check-howto-coverage.sh
# The docs-index gate lives in internal/docsindex (internal/server/web holds JS assets):
go test ./internal/docsindex/ -count=1

echo 'PASS: howto documents the four new rules on every doc surface'

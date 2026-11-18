#!/usr/bin/env bash
# .datawatch/verify/decompose-lint-rules-final-gate.sh - final gate for the four new decompose-lint
# rules: cumulative file set vs the merge base, gofmt on the story's Go files only, build, every
# rule's fake tests plus the full decompose-lint regression set, config tests, and the howto docs on
# source and embedded surfaces.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

ALLOWED_RE='^(internal/config/decompose_lint\.go|internal/config/decompose_lint_test\.go|internal/autonomous/decompose_lint\.go|internal/autonomous/decompose_lint_test\.go|internal/autonomous/decompose_lint_rules\.go|internal/autonomous/decompose_lint_text\.go|internal/autonomous/decompose_lint_verify_script\.go|internal/autonomous/decompose_lint_unexecutable\.go|internal/autonomous/decompose_lint_artefact_test\.go|internal/autonomous/decompose_lint_cumulative_test\.go|internal/autonomous/decompose_lint_script_help\.go|internal/autonomous/decompose_lint_script_help_test\.go|internal/autonomous/decompose_lint_spec_text\.go|internal/autonomous/decompose_lint_spec_text_test\.go|internal/autonomous/decompose_guidance_test\.go|internal/autonomous/rules_context_test\.go|docs/howto/README\.md|\.datawatch/verify/(lint-artefact-rule|lint-cumulative-rerun-rule|lint-script-flag-rule|lint-spec-heading-rule|lint-rules-howto|decompose-lint-rules-final-gate)\.sh|docs/howto/automaton-decompose-lint\.md|internal/server/web/docs/.*|docs/plans/drafts/.*|docs/plans/parity-issues/.*|docs/plans/reports/.*|CHANGELOG\.md|\.datawatch/reports/final_gate_decompose_lint_rules\.txt)$'

BASE=$(git merge-base HEAD main 2>/dev/null || git merge-base HEAD master 2>/dev/null || git rev-parse HEAD)
CHANGED=$({ git diff --name-only "${BASE}" 2>/dev/null; git ls-files --others --exclude-standard; } | sort -u)
UNEXPECTED=$(grep -vE "${ALLOWED_RE}" <<<"${CHANGED}" || true)
if [ -n "${UNEXPECTED}" ]; then
  echo 'FAIL: changed files outside the cumulative allowed list:'
  echo "${UNEXPECTED}"
  exit 1
fi
echo 'ok: every changed file is in the cumulative allowed list (merge-base comparison)'

GOFMT_FILES='internal/config/decompose_lint.go internal/config/decompose_lint_test.go internal/autonomous/decompose_lint.go internal/autonomous/decompose_lint_rules.go internal/autonomous/decompose_lint_verify_script.go internal/autonomous/decompose_lint_artefact_test.go internal/autonomous/decompose_lint_cumulative_test.go internal/autonomous/decompose_lint_script_help.go internal/autonomous/decompose_lint_script_help_test.go internal/autonomous/decompose_lint_spec_text.go internal/autonomous/decompose_lint_spec_text_test.go'
UNFMT=$(gofmt -l ${GOFMT_FILES})
if [ -n "${UNFMT}" ]; then echo "FAIL: gofmt needed on: ${UNFMT}"; exit 1; fi
echo "ok: gofmt clean on the story's Go files"

go build ./...
echo 'ok: build'

go test ./internal/config/ -run 'DecomposeLint' -count=1
echo 'ok: config round-trip and match tests'

go test ./internal/autonomous/ -run 'ArtefactFiles|FinalGateArtefact|ScriptInvokesKnownFlag|CheckMatchesSpecText' -count=1
echo "ok: the four new rules' fake tests"

go test ./internal/autonomous/ -run 'DecomposeLint' -count=1
echo 'ok: decompose-lint regression set'

# Doc surfaces: sync the embedded docs copy, then check source and embedded.
make sync-docs
for FOR_ID in artefact_files_allowed final_gate_artefact_allowed script_invokes_known_flag check_matches_spec_text; do
  grep -q "${FOR_ID}" docs/howto/automaton-decompose-lint.md \
    || { echo "FAIL: ${FOR_ID} missing from the howto source"; exit 1; }
done
EMBED_HOWTO=$(find internal/server/web -path '*howto/automaton-decompose-lint.md' | sort | head -n 1)
[ -n "${EMBED_HOWTO}" ] || { echo "FAIL: embedded howto not found after make sync-docs"; exit 1; }
for FOR_ID in artefact_files_allowed final_gate_artefact_allowed script_invokes_known_flag check_matches_spec_text; do
  grep -q "${FOR_ID}" "${EMBED_HOWTO}" \
    || { echo "FAIL: ${FOR_ID} missing from the embedded howto"; exit 1; }
done
echo 'ok: howto documents all four rules on source and embedded surfaces'

# The howto coverage gate is the shell script (there is no Go package in scripts/):
bash scripts/check-howto-coverage.sh
# The docs-index gate lives in internal/docsindex (internal/server/web holds JS assets):
go test ./internal/docsindex/ -count=1
echo 'ok: howto coverage and docs-index surfaces still pass'

echo 'Verification summary: cumulative file set, gofmt, build, config tests, four rule fake-test sets, decompose-lint regression set, howto source and embedded copy, howto coverage and docs index all passed'
echo '## Final answer: pass'

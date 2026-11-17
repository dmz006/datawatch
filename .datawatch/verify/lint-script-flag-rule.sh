#!/usr/bin/env bash
# .datawatch/verify/lint-script-flag-rule.sh
# Verifies the script_invokes_known_flag rule: cumulative file set vs the merge base, gofmt on
# this story's Go files only, build, fake-binary flag tests, regression set.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

# Cumulative allowed-file list: every story of this Automaton may touch these, plus the
# standard tasks' generated artefacts (changelog draft, parity draft, gate report) -
# never one task's own file list only.
ALLOWED_RE='^(internal/config/decompose_lint\.go|internal/config/decompose_lint_test\.go|internal/autonomous/decompose_lint\.go|internal/autonomous/decompose_lint_test\.go|internal/autonomous/decompose_lint_rules\.go|internal/autonomous/decompose_lint_text\.go|internal/autonomous/decompose_lint_verify_script\.go|internal/autonomous/decompose_lint_unexecutable\.go|internal/autonomous/decompose_lint_artefact_test\.go|internal/autonomous/decompose_lint_cumulative_test\.go|internal/autonomous/decompose_lint_script_help\.go|internal/autonomous/decompose_lint_script_help_test\.go|internal/autonomous/decompose_lint_spec_text\.go|internal/autonomous/decompose_lint_spec_text_test\.go|\.datawatch/verify/(lint-artefact-rule|lint-cumulative-rerun-rule|lint-script-flag-rule|lint-spec-heading-rule|lint-rules-howto|decompose-lint-rules-final-gate)\.sh|docs/howto/automaton-decompose-lint\.md|internal/server/web/docs/.*|docs/plans/drafts/.*|docs/plans/parity-issues/.*|docs/plans/reports/.*|CHANGELOG\.md|\.datawatch/reports/final_gate_decompose_lint_rules\.txt)$'

BASE=$(git merge-base HEAD main 2>/dev/null || git merge-base HEAD master 2>/dev/null || git rev-parse HEAD)
CHANGED=$({ git diff --name-only "${BASE}" 2>/dev/null; git ls-files --others --exclude-standard; } | sort -u)
UNEXPECTED=$(grep -vE "${ALLOWED_RE}" <<<"${CHANGED}" || true)
if [ -n "${UNEXPECTED}" ]; then
  echo 'FAIL: changed files outside the cumulative allowed list:'
  echo "${UNEXPECTED}"
  exit 1
fi
echo 'ok: every changed file is in the cumulative allowed list (merge-base comparison)'

# gofmt on this story's Go files only - never a whole package.
GOFILES='internal/config/decompose_lint.go internal/config/decompose_lint_test.go internal/autonomous/decompose_lint.go internal/autonomous/decompose_lint_rules.go internal/autonomous/decompose_lint_verify_script.go internal/autonomous/decompose_lint_artefact_test.go internal/autonomous/decompose_lint_cumulative_test.go internal/autonomous/decompose_lint_script_help.go internal/autonomous/decompose_lint_script_help_test.go'
UNFMT=$(gofmt -l ${GOFILES})
if [ -n "${UNFMT}" ]; then echo "FAIL: gofmt needed on: ${UNFMT}"; exit 1; fi
echo 'ok: gofmt clean on the touched Go files'

go build ./internal/config/ ./internal/autonomous/
echo 'ok: build'

go test ./internal/config/ -run 'DecomposeLint' -count=1
echo 'ok: config round-trip and match tests'

go test ./internal/autonomous/ -run 'ScriptInvokesKnownFlag' -count=1
echo 'ok: script_invokes_known_flag fake-binary flag tests'

go test ./internal/autonomous/ -run 'DecomposeLint' -count=1
echo 'ok: decompose-lint regression set'

echo 'PASS: script_invokes_known_flag rule verified'

#!/usr/bin/env bash
# Verification: the security_scan_extensions setting is plumbed end to end (config, manager, main copy, REST, MCP, PWA).
set -euo pipefail
if [ -n "${TMUX:-}" ] || [ -n "${TMUX_TMPDIR:-}" ]; then
  exec env -u TMUX -u TMUX_TMPDIR "$0" "$@"
fi
cd "$(git rev-parse --show-toplevel)"
fail=0
bad() { echo "FAIL: $*"; fail=1; }
has() { grep -Eq -- "$2" "$1" 2>/dev/null || bad "$1 lacks: $2"; }
BASE="$(git merge-base HEAD main)"
CH="$(mktemp)"; trap 'rm -f "$CH"' EXIT
{ git diff --name-only "$BASE"; git ls-files --others --exclude-standard; } | sort -u > "$CH"
allowed="internal/config/config.go
internal/autonomous/manager.go
cmd/datawatch/main.go
internal/server/api.go
internal/mcp/autonomous.go
internal/server/web/app.js
internal/session/manager.go
internal/session/manager_precommit_scan_test.go
internal/autonomous/security_precommit.go
internal/autonomous/models.go
internal/autonomous/security_precommit_test.go
docs/config-reference.yaml
docs/api/autonomous.md
internal/server/web/docs/config-reference.yaml
internal/server/web/docs/api/autonomous.md
.datawatch/verify/security-scan-extensions-config.sh
.datawatch/verify/security-scan-precommit-wire.sh
.datawatch/verify/security-scan-docs.sh
.datawatch/verify/security-scan-wire-final-gate.sh"
while IFS= read -r f; do
  [ -n "$f" ] || continue
  case "$f" in
    .datawatch/*|.decompose-output-*.json|*.log|docs/plans/drafts/*|docs/plans/parity-issues/*|docs/plans/reports/*) ;;
    *) grep -Fxq -- "$f" <<< "$allowed" || bad "file outside the allowed set: $f" ;;
  esac
done < "$CH"
gofmt_touched() {
  local gof; gof="$(grep '\.go$' "$CH" | while read -r f; do [ -f "$f" ] && echo "$f"; done || true)"
  [ -z "$gof" ] || { local out; out="$(echo "$gof" | xargs gofmt -l)"; [ -z "$out" ] || bad "gofmt: $out"; }
}

has internal/config/config.go 'security_scan_extensions'
has internal/server/api.go 'security_scan_extensions'
has internal/autonomous/manager.go 'SecurityScanExtensions'
has internal/mcp/autonomous.go 'security_scan_extensions'
has internal/server/web/app.js 'security_scan_extensions'
go build ./internal/config/ ./internal/autonomous/ ./internal/server/ ./internal/mcp/ ./cmd/datawatch/ || bad "go build"
gofmt_touched

[ "$fail" -eq 0 ] || { echo "security-scan-extensions-config.sh: FAIL"; exit 1; }
echo "security-scan-extensions-config.sh: PASS"

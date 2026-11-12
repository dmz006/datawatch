#!/usr/bin/env bash
# TS-819 — lifecycle outbound webhook sink and peer forwarding smoke
# tags: surface:api feature:lifecycle-webhook
# The smoke script does the work against the runner's sandbox daemon and uses
# its own throwaway receiver. It is not parallel:ok: it enables the webhook
# sink on the shared sandbox for its run and restores the original values.
# See scripts/release-smoke-lifecycle-webhook.sh for the eight steps.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-819"
story_preflight "surface:api feature:lifecycle-webhook" || return 0

smoke="$REPO_ROOT/scripts/release-smoke-lifecycle-webhook.sh"
if [[ ! -f "$smoke" ]]; then
  ko "smoke script missing: $smoke"
  return 0
fi

out=$(DATAWATCH_BASE="$TEST_TLS" DATAWATCH_TOKEN="$TEST_TOKEN" bash "$smoke" 2>&1)
rc=$?
save_evidence "$CURRENT_STORY" "lifecycle-webhook-smoke.log" "$out"
echo "$out" | sed 's/^/    /'

case "$rc" in
  0) ok "lifecycle webhook smoke passed against $TEST_TLS";;
  2) skip "sandbox daemon unreachable at $TEST_TLS";;
  *) ko "lifecycle webhook smoke failed (rc=$rc)";;
esac
return 0

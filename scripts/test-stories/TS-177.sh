#!/usr/bin/env bash
# TS-177 — Cleanup K8s namespace
# tags: surface:k8s feature:bootstrap
# legacy fn: t14_ts177_cleanup_namespace
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-177"
story_preflight "surface:k8s feature:bootstrap" || return 0

_story_ts_177() {
  if ! t14_check_cluster; then skip "kubectl --context=$K8S_CONTEXT cluster unreachable"; return; fi
  local out
  out=$(kubectl --context="$K8S_CONTEXT" delete namespace "$K8S_NAMESPACE" --timeout=60s --ignore-not-found=true 2>&1 || echo "failed")
  save_evidence TS-177 "cleanup.txt" "$out"
  ok "K8s namespace $K8S_NAMESPACE deletion attempted: $out"
}

RESULT=fail
_story_ts_177
: "${RESULT:=fail}"
unset -f _story_ts_177

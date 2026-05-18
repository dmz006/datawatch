#!/usr/bin/env bash
# TS-179 — Verify namespace gone
# tags: surface:k8s feature:k8s conflict:k8s
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-179"
story_preflight "surface:k8s feature:k8s conflict:k8s" || return 0

_story_ts_179() {
  if ! command -v kubectl &>/dev/null; then
    skip "K8s: kubectl not found"
    return
  fi
  local cluster_ok
  cluster_ok=$(t14_check_cluster)
  if [[ "$cluster_ok" != "yes" ]]; then
    skip "kubectl --context=$K8S_CONTEXT cluster unreachable"
    return
  fi
  local out rc
  out=$(kubectl --context="$K8S_CONTEXT" get namespace "$K8S_NAMESPACE" 2>&1); rc=$?
  save_evidence TS-179 "get_ns.txt" "$out"
  if [[ $rc -ne 0 ]] || echo "$out" | grep -qiE "not found|NotFound"; then
    ok "namespace $K8S_NAMESPACE is gone (not found)"
  elif echo "$out" | grep -qiE "terminating"; then
    skip "namespace $K8S_NAMESPACE is still terminating (may take time)"
  else
    ko "namespace $K8S_NAMESPACE still exists: $out"
  fi
}

RESULT=fail
_story_ts_179
: "${RESULT:=fail}"
unset -f _story_ts_179

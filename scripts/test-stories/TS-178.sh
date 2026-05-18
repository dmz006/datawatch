#!/usr/bin/env bash
# TS-178 — kubectl delete namespace datawatch-e2e
# tags: surface:k8s feature:k8s conflict:k8s
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-178"
story_preflight "surface:k8s feature:k8s conflict:k8s" || return 0

_story_ts_178() {
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
  local out
  out=$(kubectl --context="$K8S_CONTEXT" delete namespace "$K8S_NAMESPACE" \
    --ignore-not-found=true 2>&1 || echo "failed")
  save_evidence TS-178 "delete_ns.txt" "$out"
  if echo "$out" | grep -qE "deleted|not found|NotFound"; then
    ok "kubectl delete namespace $K8S_NAMESPACE: completed (deleted or not found)"
  elif echo "$out" | grep -qiE "terminating"; then
    ok "kubectl delete namespace $K8S_NAMESPACE: namespace is terminating"
  else
    ko "kubectl delete namespace $K8S_NAMESPACE: unexpected output: $out"
  fi
}

RESULT=fail
_story_ts_178
: "${RESULT:=fail}"
unset -f _story_ts_178

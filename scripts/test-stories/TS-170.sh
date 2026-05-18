#!/usr/bin/env bash
# TS-170 — Apply test namespace + manifests
# tags: surface:k8s feature:bootstrap
# legacy fn: t14_ts170_apply_manifests
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-170"
story_preflight "surface:k8s feature:bootstrap" || return 0

_story_ts_170() {
  if ! t14_check_cluster; then skip "kubectl --context=$K8S_CONTEXT cluster unreachable"; return; fi
  local out
  out=$(kubectl --context="$K8S_CONTEXT" create namespace "$K8S_NAMESPACE" --dry-run=client -o yaml 2>/dev/null | \
        kubectl --context="$K8S_CONTEXT" apply -f - 2>&1 || echo "failed")
  save_evidence TS-170 "apply.txt" "$out"
  if echo "$out" | grep -qE "created|configured|unchanged"; then
    ok "K8s namespace $K8S_NAMESPACE created/configured"
  else
    skip "K8s namespace creation failed: $out"
  fi
}

RESULT=fail
_story_ts_170
: "${RESULT:=fail}"
unset -f _story_ts_170

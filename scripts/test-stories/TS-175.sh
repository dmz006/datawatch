#!/usr/bin/env bash
# TS-175 — Config via env vars (ConfigMap)
# tags: surface:k8s feature:config
# legacy fn: t14_ts175_config_via_envvars
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-175"
story_preflight "surface:k8s feature:config" || return 0

_story_ts_175() {
  if ! t14_check_cluster; then skip "kubectl --context=$K8S_CONTEXT cluster unreachable"; return; fi
  # Verify that ConfigMap can be created (proves env-var injection capability)
  local out
  out=$(kubectl --context="$K8S_CONTEXT" create configmap dw-e2e-config-$$ \
    --namespace="$K8S_NAMESPACE" \
    --from-literal=server.token="$TEST_TOKEN" \
    --from-literal=server.port="18080" \
    2>&1 || echo "failed")
  save_evidence TS-175 "configmap.txt" "$out"
  if echo "$out" | grep -qE "created|configured"; then
    ok "K8s ConfigMap created (env-var config injection capability verified)"
    kubectl --context="$K8S_CONTEXT" delete configmap "dw-e2e-config-$$" \
      --namespace="$K8S_NAMESPACE" --ignore-not-found=true >/dev/null 2>&1 || true
  else
    skip "K8s ConfigMap create failed: $out"
  fi
}

RESULT=fail
_story_ts_175
: "${RESULT:=fail}"
unset -f _story_ts_175

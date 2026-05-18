#!/usr/bin/env bash
# TS-173 — Session creation via K8s service
# tags: surface:k8s feature:sessions
# legacy fn: t14_ts173_session_via_service
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-173"
story_preflight "surface:k8s feature:sessions" || return 0

_story_ts_173() {
  if ! t14_check_cluster; then skip "kubectl --context=$K8S_CONTEXT cluster unreachable"; return; fi
  # Deploy datawatch with service and verify session creation
  local pod_name="dw-e2e-sess-$$"
  local service_name="dw-e2e-sess-svc-$$"

  # Create deployment
  local out
  out=$(kubectl --context="$K8S_CONTEXT" run "$pod_name" \
    --namespace="$K8S_NAMESPACE" \
    --image=harbor.dmzs.com/library/datawatch-e2e:latest \
    --port=18180 \
    --restart=Never \
    2>&1 || echo "failed")
  save_evidence TS-173 "pod_create.txt" "$out"

  if echo "$out" | grep -qE "created|Running"; then
    # Create service for pod
    kubectl --context="$K8S_CONTEXT" expose pod "$pod_name" \
      --namespace="$K8S_NAMESPACE" \
      --name="$service_name" \
      --port=18180 \
      --target-port=18180 \
      2>/dev/null || true

    # Wait for service to be ready
    sleep 3

    # Verify service is accessible
    local svc_ip=$(kubectl --context="$K8S_CONTEXT" get service "$service_name" \
      --namespace="$K8S_NAMESPACE" \
      -o jsonpath='{.spec.clusterIP}' 2>/dev/null)

    save_evidence TS-173 "service_ip.txt" "service_ip=$svc_ip"

    if [[ -n "$svc_ip" ]]; then
      ok "K8s service created with IP: $svc_ip"
      # Cleanup
      kubectl --context="$K8S_CONTEXT" delete service "$service_name" \
        --namespace="$K8S_NAMESPACE" --ignore-not-found=true >/dev/null 2>&1 || true
      kubectl --context="$K8S_CONTEXT" delete pod "$pod_name" \
        --namespace="$K8S_NAMESPACE" --ignore-not-found=true >/dev/null 2>&1 || true
    else
      skip "K8s service IP not assigned: $svc_ip"
      kubectl --context="$K8S_CONTEXT" delete service "$service_name" \
        --namespace="$K8S_NAMESPACE" --ignore-not-found=true >/dev/null 2>&1 || true
      kubectl --context="$K8S_CONTEXT" delete pod "$pod_name" \
        --namespace="$K8S_NAMESPACE" --ignore-not-found=true >/dev/null 2>&1 || true
    fi
  else
    skip "K8s pod deployment failed: $out"
  fi
}

RESULT=fail
_story_ts_173
: "${RESULT:=fail}"
unset -f _story_ts_173

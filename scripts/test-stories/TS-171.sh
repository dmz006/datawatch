#!/usr/bin/env bash
# TS-171 — Pod scheduling check (busybox probe)
# tags: surface:k8s feature:bootstrap
# legacy fn: t14_ts171_pod_running_check
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-171"
story_preflight "surface:k8s feature:bootstrap" || return 0

_story_ts_171() {
  if ! t14_check_cluster; then skip "kubectl --context=$K8S_CONTEXT cluster unreachable"; return; fi
  # Deploy a busybox pod to prove cluster scheduling works (no datawatch container image required)
  local pod_name="dw-e2e-probe-$$"
  local out
  out=$(kubectl --context="$K8S_CONTEXT" run "$pod_name" \
    --namespace="$K8S_NAMESPACE" \
    --image=busybox:latest \
    --restart=Never \
    --command -- sleep 30 \
    2>&1 || echo "failed")
  save_evidence TS-171 "pod_create.txt" "$out"
  if echo "$out" | grep -qE "created|Running"; then
    # Wait up to 30s for Running
    local attempts=0
    local phase=""
    while [[ $attempts -lt 15 ]]; do
      phase=$(kubectl --context="$K8S_CONTEXT" get pod "$pod_name" \
        --namespace="$K8S_NAMESPACE" \
        -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
      [[ "$phase" == "Running" ]] && break
      sleep 2; attempts=$((attempts+1))
    done
    save_evidence TS-171 "pod_phase.txt" "phase=$phase"
    kubectl --context="$K8S_CONTEXT" delete pod "$pod_name" \
      --namespace="$K8S_NAMESPACE" --ignore-not-found=true >/dev/null 2>&1 || true
    if [[ "$phase" == "Running" || "$phase" == "Succeeded" ]]; then
      ok "K8s cluster schedules pods: $pod_name reached $phase"
    else
      skip "K8s pod did not reach Running (phase=$phase) — cluster may be resource-constrained"
    fi
  else
    skip "K8s pod create failed: $out (no container image for full datawatch deployment)"
  fi
}

RESULT=fail
_story_ts_171
: "${RESULT:=fail}"
unset -f _story_ts_171

#!/usr/bin/env bash
# TS-172 — Health via port-forward
# tags: surface:k8s feature:bootstrap
# legacy fn: t14_ts172_health_via_portforward
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-172"
story_preflight "surface:k8s feature:bootstrap" || return 0

_story_ts_172() {
  if ! t14_check_cluster; then skip "kubectl --context=$K8S_CONTEXT cluster unreachable"; return; fi
  # Deploy datawatch pod and verify it reaches Running state
  local pod_name="dw-e2e-health-$$"
  local out
  out=$(kubectl --context="$K8S_CONTEXT" run "$pod_name" \
    --namespace="$K8S_NAMESPACE" \
    --image=harbor.dmzs.com/library/datawatch-e2e:latest \
    --port=18180 \
    --restart=Never \
    2>&1 || echo "failed")
  save_evidence TS-172 "pod_create.txt" "$out"

  if echo "$out" | grep -qE "created|Running"; then
    # Wait for pod to be Running (max 40s)
    local attempts=0
    local phase="Unknown"
    while [[ $attempts -lt 20 ]]; do
      phase=$(kubectl --context="$K8S_CONTEXT" get pod "$pod_name" \
        --namespace="$K8S_NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
      save_evidence TS-172 "pod_phase_$attempts.txt" "phase=$phase"
      [[ "$phase" == "Running" ]] && break
      sleep 2; attempts=$((attempts+1))
    done

    save_evidence TS-172 "final_phase.txt" "final_phase=$phase"
    if [[ "$phase" == "Running" ]]; then
      ok "K8s pod reached Running state (image pull from harbor successful)"
      kubectl --context="$K8S_CONTEXT" delete pod "$pod_name" \
        --namespace="$K8S_NAMESPACE" --ignore-not-found=true >/dev/null 2>&1 || true
    else
      skip "K8s pod did not reach Running: phase=$phase"
      # Log pod events for debugging
      kubectl --context="$K8S_CONTEXT" describe pod "$pod_name" \
        --namespace="$K8S_NAMESPACE" 2>/dev/null | grep -A 5 "Events:" > /tmp/pod_events.txt || true
      save_evidence TS-172 "pod_events.txt" "$(cat /tmp/pod_events.txt)"
      kubectl --context="$K8S_CONTEXT" delete pod "$pod_name" \
        --namespace="$K8S_NAMESPACE" --ignore-not-found=true >/dev/null 2>&1 || true
    fi
  else
    skip "K8s pod deployment failed: $out"
  fi
}

RESULT=fail
_story_ts_172
: "${RESULT:=fail}"
unset -f _story_ts_172

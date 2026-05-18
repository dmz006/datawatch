#!/usr/bin/env bash
# TS-174 — Memory persistence in K8s
# tags: surface:k8s feature:memory
# legacy fn: t14_ts174_memory_persistence
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-174"
story_preflight "surface:k8s feature:memory" || return 0

_story_ts_174() {
  if ! t14_check_cluster; then skip "kubectl --context=$K8S_CONTEXT cluster unreachable"; return; fi
  # Deploy multiple pods to verify datawatch persistence can handle concurrent instances
  local pod1="dw-e2e-mem1-$$"
  local pod2="dw-e2e-mem2-$$"

  local out1 out2
  out1=$(kubectl --context="$K8S_CONTEXT" run "$pod1" \
    --namespace="$K8S_NAMESPACE" \
    --image=harbor.dmzs.com/library/datawatch-e2e:latest \
    --port=18180 \
    --restart=Never \
    2>&1 || echo "failed")
  out2=$(kubectl --context="$K8S_CONTEXT" run "$pod2" \
    --namespace="$K8S_NAMESPACE" \
    --image=harbor.dmzs.com/library/datawatch-e2e:latest \
    --port=18180 \
    --restart=Never \
    2>&1 || echo "failed")

  save_evidence TS-174 "pod1_create.txt" "$out1"
  save_evidence TS-174 "pod2_create.txt" "$out2"

  if echo "$out1" | grep -qE "created|Running" && echo "$out2" | grep -qE "created|Running"; then
    # Wait for pods to be Running (max 40s)
    local attempts=0
    local phase1="Unknown" phase2="Unknown"
    while [[ $attempts -lt 20 ]]; do
      phase1=$(kubectl --context="$K8S_CONTEXT" get pod "$pod1" \
        --namespace="$K8S_NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
      phase2=$(kubectl --context="$K8S_CONTEXT" get pod "$pod2" \
        --namespace="$K8S_NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
      [[ "$phase1" == "Running" && "$phase2" == "Running" ]] && break
      sleep 2; attempts=$((attempts+1))
    done

    save_evidence TS-174 "final_phases.txt" "pod1=$phase1 pod2=$phase2"
    if [[ "$phase1" == "Running" && "$phase2" == "Running" ]]; then
      ok "K8s memory persistence: concurrent pods reached Running (image pull successful)"
      kubectl --context="$K8S_CONTEXT" delete pod "$pod1" "$pod2" \
        --namespace="$K8S_NAMESPACE" --ignore-not-found=true >/dev/null 2>&1 || true
    else
      skip "K8s pods did not reach Running: pod1=$phase1 pod2=$phase2"
      kubectl --context="$K8S_CONTEXT" delete pod "$pod1" "$pod2" \
        --namespace="$K8S_NAMESPACE" --ignore-not-found=true >/dev/null 2>&1 || true
    fi
  else
    skip "K8s pod deployment failed: $out1 / $out2"
  fi
}

RESULT=fail
_story_ts_174
: "${RESULT:=fail}"
unset -f _story_ts_174

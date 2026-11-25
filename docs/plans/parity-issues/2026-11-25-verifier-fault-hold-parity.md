# datawatch-app parity draft: held task when the verifier gives no verdict

Draft only; no issue has been filed.

## What changed in the daemon
- A task can now be held (`review_needed`) with `failure_reason.source` =
  `verifier-fault-hold`: the verifier could not give a verdict. The worker's
  commit is kept and no failure is counted.
- Tasks carry `verifier_fault_retries`, and `infra_retries` entries can have the
  class `verifier_fault`.
- New alert title: "Automaton task held: no verdict from the verifier".
- The hold's operator question offers retry or cancel; its default is `hold`.

## What the mobile client needs
- Show the same "Held for the operator" badge and cause line for
  `verifier-fault-hold` as for `infra-retry-hold`.
- No new action, setting or endpoint.

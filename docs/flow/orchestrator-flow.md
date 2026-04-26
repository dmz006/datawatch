# PRD-DAG orchestrator flow

End-to-end view of a PRD-DAG run from operator intent to final verdict.

```
                 ┌────────────────────┐
                 │ Operator (any ch.) │
                 │  REST/MCP/CLI/comm │
                 └─────────┬──────────┘
                           │ POST /api/orchestrator/graphs
                           │ {title, prd_ids, deps}
                           ▼
┌───────────────────────────────────────────────────────────────┐
│                    orchestrator.Runner                        │
│                                                               │
│  Plan():  PRD → PRD node; (PRD × guardrail) → guardrail node │
│           deps become node-level DependsOn                    │
│                                                               │
│  Run():   Kahn topo-sort → dispatch each ready node           │
└──────────┬──────────────────────────────────┬─────────────────┘
           │                                  │
           │ Kind=prd                         │ Kind=guardrail
           ▼                                  ▼
┌──────────────────────┐          ┌────────────────────────────┐
│ PRDRunFn             │          │ GuardrailFn                │
│  (REST loopback to   │          │  v1 stub: pass + summary   │
│  /api/autonomous/    │          │  OR                        │
│  prds/{id}/run)      │          │  subprocess plugin hook    │
│                      │          │  OR                        │
│ autonomous           │          │  validator-per-name        │
│ Manager.Run:         │          │                            │
│  topo-sort tasks →   │          │ returns Verdict:           │
│  spawn each as       │          │  outcome: pass|warn|block  │
│  ephemeral worker    │          │  severity: info|low|…|crit │
│  via session.        │          │                            │
│  Manager.Start →     │          │  summary, issues[]         │
│  verify → auto-fix   │          │                            │
└──────────┬───────────┘          └────────────┬───────────────┘
           │ summary                           │ Verdict
           ▼                                   ▼
      ┌────────────────────────────────────────────┐
      │          Node state persisted              │
      │   <data_dir>/orchestrator/graphs.jsonl     │
      │                                            │
      │   if Verdict.outcome == "block":           │
      │      node.status = blocked                 │
      │      graph.status = blocked                │
      │      all dependent nodes → cancelled       │
      │   else:                                    │
      │      node.status = completed               │
      │      runner advances to next ready node    │
      └────────────────────────────────────────────┘
                           │
                           ▼
         ┌──────────────────────────────────────┐
         │ Graph.Status: completed | blocked    │
         │ GET /api/orchestrator/graphs/{id}    │
         │   → full node tree + all verdicts    │
         │ GET /api/orchestrator/verdicts       │
         │   → flat verdict log across graphs   │
         └──────────────────────────────────────┘
```

## Key properties

- **Fire-and-forget run** — `POST .../run` returns immediately; runner
  continues in the background. Operators poll `GET .../graphs/{id}`.
- **Block dominates** — any guardrail returning `block` halts the
  graph. Dependents are cancelled, not failed (fixable by re-running
  after intervention).
- **Verdict provenance** — every Verdict stamps `VerdictAt` and
  optionally `ValidatorID` (session ID of the validator worker for
  validator-based guardrails). The full verdict log is append-only
  via the JSONL store rewrite semantics.
- **Reuse all the way down** — PRD execution is the autonomous
  package (`internal/autonomous`) reused verbatim; each task inside
  a PRD is still a `pipeline.Task` running under an ephemeral
  worker spawn. The orchestrator adds the *outer* DAG + guardrail
  overlay, not a new session primitive.

## Audit + cost

Every PRD node and guardrail node is a regular session, so cost
accounting and audit entries roll up automatically. Per-graph cost
is the sum of the session costs for its PRD + guardrail worker
sessions — no new infrastructure needed.

See also: [`docs/flow/agent-spawn-flow.md`](agent-spawn-flow.md)
(worker spawn), [`docs/api/autonomous.md`](../api/autonomous.md)
(PRD internals), [`docs/api/orchestrator.md`](../api/orchestrator.md)
(operator contract).

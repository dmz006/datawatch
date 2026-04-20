# BL117 orchestrator flow (v4.0.0)

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
│  prds/{id}/run)      │          │  BL33 plugin on_guardrail  │
│                      │          │  OR (v4.0.x):              │
│ BL24 autonomous      │          │  BL103 validator-per-name  │
│ Manager.Run:         │          │                            │
│  topo-sort tasks →   │          │ returns Verdict:           │
│  spawn each as F10   │          │  outcome: pass|warn|block  │
│  worker via session. │          │  severity: info|low|…|crit │
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
  BL103-based guardrails). The full verdict log is append-only via
  the JSONL store rewrite semantics.
- **Reuse all the way down** — PRD execution is BL24
  (`internal/autonomous`) reused verbatim; each task inside a PRD is
  still a `pipeline.Task` running under F10 spawn. BL117 adds the
  *outer* DAG + guardrail overlay, not a new session primitive.

## Audit + cost

Every PRD node and guardrail node is a regular session, so BL6 cost
accounting and BL9 audit entries roll up automatically. Per-graph
cost is the sum of the session costs for its PRD + guardrail worker
sessions — no new infrastructure needed.

See also: `docs/flow/f10-agent-spawn-flow.md` (worker spawn),
`docs/api/autonomous.md` (BL24 PRD internals),
`docs/api/orchestrator.md` (operator contract).

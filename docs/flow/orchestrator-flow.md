# PRD-DAG orchestrator flow

End-to-end view of a PRD-DAG run from operator intent to final verdict.

```mermaid
flowchart TD
    Op["Operator<br/>REST · MCP · CLI · comm"]
    Op -->|POST /api/orchestrator/graphs<br/>{title, prd_ids, deps}| Runner

    subgraph Runner["orchestrator.Runner"]
        Plan["Plan(): PRD → PRD node<br/>(PRD × guardrail) → guardrail node<br/>deps → node.DependsOn"]
        Run["Run(): Kahn topo-sort →<br/>dispatch each ready node"]
        Plan --> Run
    end

    Run -->|"Kind = prd"| PRDFn[PRDRunFn]
    Run -->|"Kind = guardrail"| GFn[GuardrailFn]

    subgraph PRDPath["PRD execution"]
        PRDFn --> Loop["REST loopback<br/>POST /api/autonomous/prds/{id}/run"]
        Loop --> Mgr["autonomous.Manager.Run:<br/>topo-sort tasks →<br/>spawn each as ephemeral worker<br/>via session.Manager.Start →<br/>verify → auto-fix"]
        Mgr --> Sum[summary]
    end

    subgraph GPath["Guardrail evaluation"]
        GFn --> Choose{Strategy?}
        Choose -->|stub| Stub[pass + summary]
        Choose -->|plugin| Plug[subprocess plugin hook]
        Choose -->|validator| Val[validator-per-name]
        Stub --> V["Verdict<br/>outcome: pass · warn · block<br/>severity: info … crit<br/>summary, issues[]"]
        Plug --> V
        Val --> V
    end

    Sum --> Persist["Node state persisted<br/>&lt;data_dir&gt;/orchestrator/graphs.jsonl"]
    V --> Persist

    Persist --> Decide{"Verdict<br/>outcome"}
    Decide -->|block| Blocked["node.status = blocked<br/>graph.status = blocked<br/>dependents → cancelled"]
    Decide -->|pass / warn| Advance["node.status = completed<br/>runner advances to next ready node"]

    Blocked --> Final["Graph.Status: completed · blocked<br/>GET /api/orchestrator/graphs/{id} → tree + verdicts<br/>GET /api/orchestrator/verdicts → flat verdict log"]
    Advance --> Final
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

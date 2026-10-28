# Automation triage flow

How a failed or blocked Automaton gets its triage record filled, refreshed,
alerted once, and cleared, and how the operator reads the same record on
every surface — all driven by the coordinator's poll loop, with no new
endpoints in the write path.

## The poll-time triage pass

`Coordinator.idleLoop` ticks (1 minute default); each tick runs the idle
check, the budget check, then `triagePass()` over **every** Automaton in the
store, regardless of Automation membership.

```mermaid
flowchart TD
    T[idleLoop tick] --> IP[triagePass: one walk over m.store.ListPRDs]
    IP --> S{PRD status}
    S -- failed / blocked --> M{member of an Automation?}
    M -- yes, and boot not Ready --> SKIP[skip this tick:<br/>boot recovery may still replay the failure]
    M -- no / yes and Ready --> F[triageFresh: build the record the pass would store<br/>(identity, failed-task block, end reason, retry budgets)]
    F --> U{record unchanged AND no alert due?}
    U -- yes --> SKIP2[skip: the stored record is not rewritten every poll]
    U -- no --> A{alert due? threshold > 0 AND<br/>AlertedAt unset AND now >= FirstSeenAt + threshold}
    A -- yes --> STAMP[stamp AlertedAt on the fresh record]
    A -- no --> SAVE
    STAMP --> SAVE[Manager.SaveTriageRecord: keeps stored FirstSeenAt,<br/>AlertedAt and Notes across refreshes]
    SAVE --> DUE{alert due?}
    DUE -- yes --> QUE[queue one 'warn' alert, body = triageAlertBody(fresh)]
    DUE -- no --> NEXT
    S -- anything else --> C{stored triage record?}
    C -- yes --> CLR[Manager.ClearTriageRecord: the card clears with the record]
    C -- no --> NEXT[next PRD]
    QUE --> NEXT
    NEXT --> FIN[after the walk: every queued alert goes through<br/>the Manager alert sink (comm channel), one per episode]
```

Key properties the walk enforces:

- **One record per stuck Automaton.** A `failed` Automaton wins over
  `blocked` when both apply. A healthy Automaton gets no record.
- **Alert once per episode.** The alert fires when the stored record's
  `FirstSeenAt` is at least `triage_alert_minutes` old and `AlertedAt` is
  still unset; stamping `AlertedAt` before saving is what keeps a reload
  from re-alerting, and recovering then failing again starts a new
  episode (new `FirstSeenAt`) that alerts again.
- **`0` = off.** `EffectiveTriageAlertMinutes()` returns the setting
  (unset → the `DefaultAutomationTriageAlertMinutes` default of 30); `0`
  silences the alert while the record still fills, refreshes and clears.
- **Reads are pure.** The pass never starts, retries or edits anything;
  the retry budgets it shows are read from the resolved budget snapshot —
  shown, not spent.

## Reading the same record, on every surface

The record is stored on the Automaton (`PRD.Triage`) and served — newest
first, beside the effective `alert_minutes` — by one read that shares the
`autonomous:read` capability with the sibling autonomous reads:

```mermaid
sequenceDiagram
    participant Op as Operator (PWA / CLI / API / peer)
    participant R as Server (handleAutonomousTriage)
    participant M as Manager (triageReader)
    participant C as MCP (autonomous_triage)
    participant K as CLI (datawatch autonomous triage)

    Op->>R: GET /api/autonomous/triage [automation_id / prd_id / status=failed|blocked]
    R->>R: 503 if the autonomous manager is absent; 405 on any other method
    R->>R: 403 without capability autonomous:read (federation policy)
    R->>M: ListTriageRecords() + TriageAlertMinutes()
    M-->>R: stored records + the effective threshold
    R->>R: status filter (failed|blocked; anything else 400, no guessing);<br/>exact-match automation_id / prd_id filters; empty match = empty list, never 404
    R-->>Op: 200 {"records":[…newest first…], "alert_minutes":30}
    C->>R: the MCP tool proxies the same GET with the supplied filters
    K->>R: the CLI sends the same query params and renders the body<br/>through the shared formatter (autonomous triage text)
```

The **PWA** card in the Automaton detail view renders the same record data
(`triageCardHtml`), and the comm-channel **alert body** is produced by the
same `TriageRecord` formatter (`triageAlertBody` delegates to it) — so the
record shows the same standing wherever the operator reads it, including on
the alert itself. Android/iPhone read the same REST contract; the app-side
card is tracked in
[2026-10-26-autonomous-triage-mobile-parity-note.md](../plans/2026-10-26-autonomous-triage-mobile-parity-note.md).

Related: [automata-capacity-flow.md](automata-capacity-flow.md) (why the
Automaton got stuck in the first place),
[howto/automation-tab.md](../howto/automation-tab.md) (the operator
procedure), the alert bound is `autonomous.automation.triage_alert_minutes`
(`../config-reference.yaml`).

# datawatch v5.22.0 — release notes

**Date:** 2026-04-26
**Spans:** v5.21.0 → v5.22.0
**Closed:** Observability fill-in + arrow-buttons layout fix

## What's new

### Observability — LoopStatus surfaces BL191 Q4 + Q6 counters

Per AGENT.md § Monitoring & Observability rule (*"every new feature
MUST include monitoring and observability support"*), v5.9.0
(recursion) and v5.10.0 (per-task/story guardrails) shipped without
visible counters. The audit caught it.

`LoopStatus` (returned from `GET /api/autonomous/status` + the
chat-channel `autonomous status` verb + the `autonomous_status` MCP
tool) now carries:

```go
ChildPRDsTotal int            // count of PRDs with non-empty ParentPRDID (BL191 Q4)
MaxDepthSeen   int            // max(PRD.Depth) across the store (BL191 Q4)
BlockedPRDs    int            // count of PRDs in PRDBlocked status (BL191 Q6)
VerdictCounts  map[string]int // outcome → count rollup ("pass"/"warn"/"block")
                              // across every Story.Verdicts + Task.Verdicts (BL191 Q6)
```

Sample response:

```json
{
  "running": true,
  "active_prds": 2,
  "queued_tasks": 0,
  "running_tasks": 1,
  "child_prds_total": 5,
  "max_depth_seen": 3,
  "blocked_prds": 1,
  "verdict_counts": {"pass": 12, "warn": 2, "block": 1}
}
```

Operators on the existing `/api/autonomous/status` polling loop get
the new counters automatically. PWA + chat + MCP surfaces all
forward the same JSON shape so no per-surface render change is
needed beyond what's there.

4 new unit tests (`internal/autonomous/observability_test.go`)
cover: child + max-depth counters, blocked-PRD count,
verdict-rollup correctness, nil-when-empty.

### CI fix — `containers` workflow agent-base FROM resolution

Operator report: *"GitHub runners are still failing, removed failed jobs and fix"*.

Every tag push since v5.21.0 had the `containers` workflow's
`build-agents` stage failing with:

```
ERROR: failed to solve: ghcr.io/dmz006/datawatch/agent-base:VERSION: not found
```

Root cause: Stage 1 (`build-base`) pushes agent-base to
`ghcr.io/dmz006/datawatch-agent-base` (hyphen — the GHCR convention
matching every other image). Stage 2 (`build-agents`) sets
`REGISTRY=ghcr.io/dmz006/datawatch` and the agent-* Dockerfiles do
`FROM ${REGISTRY}/agent-base:${BASE_TAG}`, resolving to
`ghcr.io/dmz006/datawatch/agent-base:VERSION` (slash) — a path that
doesn't exist on GHCR.

Fix: Stage 1 also tags agent-base under the slash-namespaced path
`ghcr.io/dmz006/datawatch/agent-base:VERSION`. The hyphen tag stays
primary (operator-facing pulls); the slash tag is internal plumbing
the agent-* layer chain pulls from. Conditional in the matrix
ensures only `agent-base` gets the dual tag.

### Edit-PRD modal button fix

Operator report: *"can't edit and not [any] other buttons work"*.

The v5.19.0 Edit modal had `onclick="submitPRDEdit(${JSON.stringify(id)})"`
which produced `onclick="submitPRDEdit("foo")"` — embedded
double-quotes inside a double-quoted attribute broke the handler.
Now uses `escHtml(JSON.stringify(id))` so the inner quotes become
`&quot;` entities and the attribute parses correctly.

### UX fix — tmux arrow buttons right-justified

Operator report: *"The arrow buttons are under the saved commands
drop-down and not right justified."*

v5.19.0 restored the arrow buttons (regression of the v5.2.0
behavior) but placed them BEFORE the saved-commands dropdown. With
the flex layout's wrap behavior, that put arrows on top and
dropdown on the next line. The intent (per the v5.2.0 commit) is
arrows to the RIGHT of the dropdown, hugging the row's right edge.

Layout is now:

```
[📃 Response] [Commands… ▾] [custom-cmd wrap] ─── spacer ─── [↑] [↓] [←] [→]
```

The arrow group uses `margin-left: auto` to push to the right of the
flex container.

## Tests

1390 passed in 58 packages (1382 → 1390; 4 new for observability +
0 net for the JS-only layout fix).

## Known follow-ups (operator-reported, deferred to next release)

- **Autonomous tab auto-refresh on changes** — operator: *"I shouldn't
  have to refresh the autonomous tab, it should refresh on changes"*.
  Currently the PWA polls / requires manual Refresh button. Deferred
  because it requires plumbing a `MsgPRDUpdate` WS broadcast on every
  Manager.SavePRD path — not contained enough for this release. v5.23.0.

## Other follow-ups

Per the audit doc — the remaining items are patch-class:

- **datawatch-app#10** — catch-up issue bundling every PWA-visible
  change since v5.3.0 (the previous datawatch-app#9 covered through
  v5.2.0).
- **container parent-full retag** — daemon-behavior changes since
  v5.0.1 (BL180 procfs+cross-host+eBPF, BL191 Q1+Q4+Q6, BL202 PWA,
  channel fix, full CRUD, observability fill-in) need a parent-full
  rebuild + push to GHCR.
- **gosec HIGH-severity review** — the audit's gosec run flagged
  297 issues (mostly pre-existing G104 globally suppressed); HIGH
  findings need a per-finding decision.

## Upgrade path

```bash
datawatch update                                # check + install
datawatch restart                               # apply

# Verify the new counters:
curl -sk https://localhost:8443/api/autonomous/status | jq '
  {child_prds_total, max_depth_seen, blocked_prds, verdict_counts}'
```

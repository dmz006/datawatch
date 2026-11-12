# Quality gate scope

**Date:** 2026-10-23
**Version:** no bump — this doc proposes a setting that ships in the current release line
**Status:** Proposed — awaiting operator confirmation of the smoke set (new scopes stay
off; built-in default remains `full`)
**Scope:** `internal/pipeline/quality.go` (gate engine), `internal/autonomous/` (executor
and PRD records), `internal/config/config.go` (setting keys) — code to follow in a later
pass; this doc is the proposal only.

## Problem

Today every task the autonomous executor completes runs the whole configured test
command (`go test ./...`, typically with `go vet`), in the per-task quality gate
(`internal/autonomous/executor.go`, engine in `internal/pipeline/quality.go`). On this
repository the full suite takes well over two minutes of wall time, so the gate is a
main reason tasks queue for verification: every executor task pays the full-suite cost
whether or not it touched anything near the failing packages, and a single task in a
large Automaton blocks the queue behind it. At the same time the full suite is
disproportionately slow in a few packages (measured below), so the cost of every-task
verification is inflated by exactly the tests that most tasks do not affect.

## Design

Three scope values for the per-task quality gate:

- `full` — run the configured test command exactly as today. The current behaviour,
  and the safe fallback.
- `touched-plus-smoke` — run `go test` and `go vet` over the union of
  (a) the touched packages: the Go packages the task changed, plus the packages that
  import a changed package one level up; and
  (b) the built-in smoke set below (the confirmed list of core package patterns).
- `touched` — part (a) only, without the smoke set.

Setting and override order (first match wins, then the built-in default):

1. Per-Automaton override: `quality_gates.scope` (new field of the existing
   per-PRD `quality_gates` block).
2. Global: `autonomous.default_quality_gates.scope` (new field of the existing
   `default_quality_gates` block in the daemon config).
3. Built-in default: `full`.

Touched-package computation: take the task diff since the task base commit, map each
changed file to the Go package that contains it (its directory, where that resolves to
a package), and add the packages that import any changed package one level up. `go test`
and `go vet` run only over that package set (plus the smoke set when the scope is
`touched-plus-smoke`); no other test command runs at task scope.

Bounds that keep the escape hatches honest:

- The final gate task of an Automaton always runs the full suite, regardless of the
  scoped value, so each Automaton ends on a whole-repo proof.
- The merge process is unchanged: it still runs the full suite before anything ships.
- The gate result records which packages ran and why (touched, importer-of a touched
  package, smoke set, or full) so the operator can audit exactly what a given result
  covered.

Cost: the smoke set below runs in roughly 40 s of wall time (one parallel run, the
slowest package in the set dominates) versus 134 s for the full suite on this
repository's current code — measured 2026-10-23, this worktree.

## Proposed smoke set

```
./internal/autonomous/...
./internal/config
./internal/pipeline
./internal/server/...
./internal/mcp/...
./internal/observer/...
./internal/audit
./internal/metrics
```

## Rationale

Criterion: keep the packages whose breakage is most expensive and cheapest to test.
Every pattern below resolves to at least one package via `go list` (verified 2026-10-23,
this worktree; times are `go test -count=1` wall clock for the listed pattern).

| Pattern | Measured s | Why breakage is expensive | Why it is cheap |
|---|---|---|---|
| `./internal/autonomous/...` | 13.5 | The executor is the machine under test; a broken executor silently mis-runs every later task and Automaton, and its bugs need the most expensive manual re-run to find | The core package's test time is 13.5 s; `autonomous/scan` is ~0 s |
| `./internal/config` | 1.9 | A config-parsing regression changes daemon startup and every operator's live config interpretation at once | 1.9 s |
| `./internal/pipeline` | 3.2 | This is the gate engine and shared pipeline logic; its bugs corrupt the very verification this feature adds | 3.2 s |
| `./internal/server/...` | 38.8 | The REST/WS surface every client — PWA, apps, federation peers — parses; contract regressions are the most operator-visible kind of breakage | ~39 s is the slowest line here, still 1/3 of the full suite; `multiserver` is ~0 s |
| `./internal/mcp/...` | 1.7 | MCP tool contracts are consumed by external agent harnesses where breakage appears as far-off, hard-to-attribute agent failures | 1.7 s; `search` is ~0 s |
| `./internal/observer/...` | 1.0 | Event envelopes feed operator-dashboards and federation observers; a shape break hides problems from sight | 1.0 s; `ebpf` is ~0 s |
| `./internal/audit` | ~0 | Audit trail recording is a compliance surface; a break silently drops the record | Instant |
| `./internal/metrics` | ~0 | Stats storage backs capacity and status surfaces an operator watches | Instant |

Measured and deliberately excluded (test time not small relative to value as a
*per-task, every-task* cost; each is still covered by the touched-package set whenever
a task touches it, and by the full suite at Automaton end and before merge):

| Package | Measured s | Note |
|---|---|---|
| `internal/session` | 127.7–130.0 | Highest value in the repo (session manager), but the second-slowest package; paying it every task recreates the queueing problem this feature removes |
| `internal/router` | 65.9 | Slow in proportion to how rarely a scoped task touches its neighbourhood |
| `cmd/datawatch` | 52.6 | Entry-point wiring; a break there almost always co-occurs with a touched package in the set above |
| `internal/messaging/backends/email` | 133.6 | Slowest package in the suite; it is the tail of the full-suite run and the main reason the full suite is slow, which is exactly why it does not belong in a per-task set |

## Operator confirmation

The new scopes (`touched-plus-smoke`, `touched`) and this smoke list ship as setting
and data only. The new scope stays off — the built-in default remains `full` — until
the operator confirms the list above. Confirming changes nothing about runtime
behaviour by itself; it authorises a later default flip. Flipping the built-in default
later involves a one-line change of the default constant (to the confirmed scope value,
with this list as the built-in smoke set) plus a release note; it requires no further
design and no operator config edits unless an Automaton or daemon already overrides
`scope`.

**DECISION (operator):** `[ ] CONFIRM — the proposed smoke set above is adopted as the
built-in list.` — or — `[ ] EDIT — same list with changes: _____.` — or —
`[ ] REJECT — keep `full` as the only scope; rationale: _____.`

## Parity surface

- **REST:** included. The scoped value is a new field inside the existing autonomous
  config payload (`quality_gates.scope` on the PRD record,
  `autonomous.default_quality_gates.scope` in the daemon config) and appears in the
  autonomous config GET/PUT responses; additive enum value on an existing field.
- **YAML/config:** included. New keys `autonomous.default_quality_gates.scope`
  and, per Automaton, `quality_gates.scope` on the PRD spec; documented in
  `docs/config-reference.yaml` when the code lands.
- **CLI:** excluded. No new or changed `datawatch` subcommand; the general config
  edit path already reaches the new keys, and gate results live in task/PRD records,
  not in a new CLI output.
- **MCP:** excluded. No MCP tool exposes or is affected by the gate scope; the
  tool contract for memory and research tools is unchanged.
- **comm channel:** excluded. No new delivery or notification format; gate-failure
  notifications keep the existing shape, with the package list recorded in the gate
  result text (task/PRD record surface), not in a comm-channel message type.
- **PWA:** excluded. The PWA settings and Automata views render the existing config
  fields; no PWA-specific affordance is added for this value, and the implementation
  pass must verify the additive field does not break form rendering (additive JSON, no
  layout change expected).
- **Android:** excluded. Additive config value that no app code parses today; the
  apps are unaffected, so no app change or issue is required for this phase.
- **iPhone:** excluded. Same reason as Android: the change is an additive config/REST
  value with no app-side consumer.

If a later phase adds a scope selector to any client UI, the REST enum change then
becomes a mobile-consumed contract change and the mobile surfaces move to
"included" at that time.

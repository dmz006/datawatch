# OpenRig Adoption Plan

- **Date:** 2026-09-24
- **Version:** v8.34.0 (datawatch at time of planning; read from `cmd/datawatch/main.go`)
- **Type:** research / planning document — **no code is changed by this plan's own work**
- **Basis:** `artifacts/research/openrig/gap-matrix.md`, the research concept inventory recorded in
  `artifacts/research/openrig/concept-inventory.md`, and openrig.dev itself. No prior planning
  documents were used as a source.

## What we learned from openrig.dev

OpenRig models the multi-agent world as a small set of durable, named, versioned artifacts:

- **Rig / pod / seat topology.** A *rig* is a declarative, diffable YAML topology (RigSpec 0.2) of
  *pods* and *seats*; a seat is a durable role that outlives any session occupying it, with the
  seat's name, edges, and inbound work unchanged when the occupant is replaced
  ([taxonomy](https://openrig.dev/specs/taxonomy), [RigSpec](https://openrig.dev/specs/rigspec)).
- **Typed edges.** Five edge kinds; only `delegates_to` and `spawned_by` constrain launch order
  at boot and restore today — the rest are informational
  ([edge types](https://openrig.dev/specs/edge-types)).
- **Named profiles over a declared resource pool.** An AgentSpec declares a resource pool
  (skills, guidance, subagents, hooks, runtime resources); named profiles select a subset and never
  inject undeclared resources ([AgentSpec](https://openrig.dev/specs/agentspec)).
- **Owned work with enforced closure reasons.** A queue item cannot be marked done without a
  closure reason from a fixed set, enforced inside the database transaction ("hot-potato closure");
  a handoff closes the source row and opens the successor's row in one transaction
  ([coordination](https://openrig.dev/docs/coordination)).
- **Durable workflows with step projection.** When a step owner closes their row, one daemon
  transaction closes it and projects the next step's row — "the scribe, not the gate"
  ([workflows](https://openrig.dev/docs/workflows)).
- **Portable bundles with integrity checksums.** A `.rigbundle` carries the rig spec plus the
  vendored blueprints, a manifest with per-file SHA-256 + sibling digests, `plan` / `inspect` /
  `install` verbs, and unpack safety ([RigBundle](https://openrig.dev/specs/rigbundle)).
- **On-demand context packs.** Operator-authored file bundles with a manifest, assembled on demand
  into one paste-ready bundle; token accounting reports overage rather than truncating
  ([context](https://openrig.dev/docs/context)).
- **Daemon-native watchdog jobs.** Jobs persist across daemon restarts; policies cover reminders,
  artifact gates, workflow keep-alive, idle queue-row gates, and context-usage thresholds, and can
  be the one live wake on a parked queue row ([workflows](https://openrig.dev/docs/workflows)).
- **Honest continuity.** Handover, agent images, restore packets, and per-seat outcome reporting
  (resumed / rebuilt / fresh / waiting / failed); the system explicitly "does not promise recovery"
  ([continuity](https://openrig.dev/docs/continuity)).

## Scope

Subsystems that would be affected **if the recommendations are implemented** (repo-relative paths):

- Durable role work queue — `internal/session/queue.go` (closure reasons, transactional handoff)
- Project profile + ephemeral worker profile system — `internal/profile/project.go`,
  `internal/agents/spawn.go` (durable seat identity)
- Durable scheduler, alert rules, eBPF observer — `internal/session/schedule.go`,
  `internal/alertrules/`, `internal/observer/` (condition-based watchdog policies)
- Autonomous planning engine + PRD-DAG orchestrator — `internal/autonomous/executor.go`,
   `internal/orchestrator/runner.go`, `internal/orchestrator/models.go` (close-and-project,
   topology file)
- Template store + skills resource store — `internal/autonomous/templates.go`,
  `internal/autonomous/models.go`, `internal/skills/store.go` (portable bundle envelope)
- Discussion scopes and memory scopes are **not** in scope — the gap matrix
  classifies datawatch as
  equivalent or stronger there (`internal/session/discussion_subs.go`, `internal/memory/scopes.go`)
  and they are deliberately **not** adopted-from-OpenRig.

**This plan is research only.** No source files are created, modified, or deleted by this
document's own work; no configuration changes and no new endpoints are introduced. The phases
below are a forward-looking adoption list, not work performed here.

## Phases

### Phase 1 — Enforced closure reasons + transactional handoff
- **Goal:** adopt the hot-potato closure rule: a fixed closure-reason set enforced on close, plus a
  single-transaction "close this row + open the successor's row" primitive.
- **Extends:** the durable role work queue — `internal/session/queue.go`.
- **Operator can afterwards:** close work only with a declared reason (handed off, blocked on,
  denied, canceled, no-follow-on, escalation); hand work to a successor atomically; query
  "which rows are handed off with no successor" as a first-class class; no work falls on the floor
  silently across the queue, planning-engine tasks, and crash-restart flows.
- **Success criteria:** closing a row without a reason is rejected at the store level; the
  handoff primitive closes the source row and opens the target row in one write; existing
  unqualified close/complete call sites keep working (empty reason treated as a compatibility
  sentinel, never as "done").
- **Depends on:** no earlier phase.
- **Status:** Planned

### Phase 2 — Durable seat identity
- **Goal:** one stable seat id linking a seat definition (the project profile), its current
  occupant (session / worker), and its successor.
- **Extends:** the project profile + the ephemeral worker profile system —
  `internal/profile/project.go`, `internal/agents/spawn.go`, plus a new lightweight seat registry
  store listing seats, current occupants, inbound queue rows, and handover state.
- **Operator can afterwards:** address a seat stably across restarts and occupant replacements
  ("restart seat X"); keep a seat's inbound work bound to the seat rather than to a session name;
  report honestly which occupant currently holds which seat.
- **Success criteria:** every spawned worker carries an optional seat id; the seat registry shows
  definition / occupant / successors consistently; existing session-only flows are unchanged
  (seat id is an additive foreign key, not a replacement).
- **Depends on:** no earlier phase (independent of Phase 1; the two are the P0 pair).
- **Status:** Planned

### Phase 3 — Condition-based watchdog jobs
- **Goal:** a small policy registry of daemon-native watchdog policies — at minimum an
  idle-gate on
  queue rows, an artifact-readiness gate, and a context-usage threshold per seat — reusing the
  durable scheduler as host and the observer / alert-rule substrate for conditions; a parked row
  may carry a live watchdog as its one wake.
- **Extends:** the durable scheduler + alert-rule evaluator — `internal/session/schedule.go`,
  `internal/alertrules/`, with conditions reading the eBPF observer (
  `internal/observer/`) and queue-lease signals from `internal/session/queue.go`.
- **Operator can afterwards:** self-heal the "work stalled, no one noticed" class — reclaim idle
  rows, gate progress on declared artifacts, act before a seat hits its context wall; inspect each
  policy's evaluation history with the honest caveat that unrecorded quiet periods are by design.
- **Success criteria:** the three policy types exist with per-job evaluation history; jobs survive
  daemon restarts; an idle-row gate can reclaim a stalled claimed row; policy evaluation is
  recorded only for meaningful evaluations.
- **Depends on:** Phase 2 (seats are the watchdog target); benefits from Phase 1 for the
  one-live-wake link on parked rows.
- **Status:** Planned

### Phase 4 — Atomic close-and-project (durable step projection)
- **Goal:** "the scribe, not the gate": when a step owner closes their row, one transaction
  closes the current frontier row and projects the next task's row, with acceptance staying with
  the next stage.
- **Extends:** the autonomous planning engine's execution loop + the PRD-DAG orchestrator runner —
  `internal/autonomous/executor.go`, `internal/orchestrator/runner.go`.
- **Operator can afterwards:** ask "which task is the current frontier and who owns it?" for any
  in-flight plan and get a durable answer backed by a queue row, not in-memory executor state;
  resume a run that died between steps without replaying completed work.
- **Success criteria:** close-and-project is a single atomic write; the frontier is queryable as
  open queue rows; existing in-flight runs are unaffected (new runs opt in).
- **Depends on:** Phase 1 (the handoff primitive it calls); Phase 2 for owning seats.
- **Status:** Planned

### Phase 5 — Declarative seat-level topology file (the "rig" artifact)
- **Goal:** a datawatch-native named, versioned, importable topology spec: pods, seats, and typed
  edges (launch-order-relevant kinds first, informational kinds later), optionally with a
  topology-scoped culture file.
- **Extends:** the PRD-DAG orchestrator's graph model + the project profile —
  `internal/orchestrator/models.go`, `internal/profile/project.go`, with the loader persisting
  into the existing graph store (`internal/orchestrator/store.go`).
- **Operator can afterwards:** express a multi-agent setup as one diffable, versionable file;
  import, validate, and diff it; derive execution (phases 1–4 machinery) from the file as source
  of truth; share the topology artifact between machines.
- **Success criteria:** the schema loads, validates, and persists as a first-class artifact;
  launch-order-relevant edges drive boot order; the informational edge kinds render but do not
  affect behavior (matching OpenRig's own staging).
- **Depends on:** Phase 2 (seats are the nodes the file names); builds on Phase 4 so the executor
  is derived from the file.
- **Status:** Planned

### Phase 6 — Portable bundle + integrity envelope
- **Goal:** a one-file portable rig: topology spec + project/cluster profile pair + synced skills
  tree + queue rows + culture file, packed as a compressed archive with a manifest, per-file
  SHA-256 integrity digests, a sibling digest, unpack safety, and `plan` / `inspect` / `install`
  verbs layered on the existing template substitution.
- **Extends:** the template store + the skills resource store —
  `internal/autonomous/templates.go`,
  `internal/autonomous/models.go`, `internal/skills/store.go`, with a new bundle reader/writer
  package.
- **Operator can afterwards:** ship a working multi-agent setup as one archive; verify it against
  the manifest (corruption in transit, tampered contents) before installing; plan-preview,
  inspect, and install on a new machine.
- **Success criteria:** the archive contains no external references; sibling digest and per-file
  hashes both verify; unpack rejects symlinks / absolute / traversal paths; `plan` and `inspect`
  make no mutations; datawatch keeps its stronger `{{var}}` parameterization as the bundle's
  substitution machinery.
- **Depends on:** Phase 2 (seats in the envelope) and Phase 5 (the topology file being bundled).
- **Status:** Planned

## Parity surface

Full parity-surface set enumerated per the project rule: **REST, MCP, CLI, comm channel,
YAML/config, PWA, Android, iPhone/iOS.**

This plan is research only, so in its current form **every surface of every phase is excluded —
research document, no operator-facing surface in this PRD**; that reason applies to all eight
surfaces for this document as written. When a phase is later promoted to active work, the drafted
per-surface intent is below:

- **Phase 1 (closure + handoff):** ships on REST, MCP, CLI (the queue verbs' existing surfaces).
  Excluded: comm channel — closure reasons are operator query semantics, not push notifications;
  YAML/config — no new configuration surface, the reason set is data-level; PWA, Android,
  iPhone/iOS — read surfaces for closed-rows queries are a mobile parity follow-up, declared in a
  later operator-visible release per the parity rule.
- **Phase 2 (seat identity):** ships on REST, MCP, CLI (seat listing / address verbs). Excluded:
  comm channel — no new notify path; YAML/config — the seat id rides the existing
  profile surface;
  PWA, Android, iPhone/iOS — seat status is developer-console material first; mobile views follow
  when the PWA shows seats.
- **Phase 3 (watchdog jobs):** ships on REST, MCP, CLI (register / list / show / stop) plus
  YAML/config for default policy settings. Excluded: comm channel — alert delivery reuses the
  existing alert pipeline rather than a new channel surface; PWA, Android, iPhone/iOS — policy
  management is console-first; the operator already receives alerts on the existing surfaces.
- **Phase 4 (close-and-project):** ships on MCP + CLI (the planning-engine's existing control
  verbs) plus REST read surfaces for the queryable frontier. Excluded: comm channel — no operator
  message changes; YAML/config — none; PWA, Android, iPhone/iOS — progress is already visible
  through existing run-status surfaces.
- **Phase 5 (topology file):** ships on CLI + YAML/config (the file itself is the config surface)
  plus REST import/export. Excluded: MCP — the loader is operator-driven, not agent-driven;
  comm channel — none; PWA, Android, iPhone/iOS — a file-based authored artifact;
  viewing rendered
  topology is a PWA enhancement candidate, not a launch surface.
- **Phase 6 (bundle + integrity):** ships on CLI (plan / inspect / install) plus
  YAML/config-adjacent
  import and REST for bundle upload where the operator uploads from the console. Excluded: MCP —
  bundling is an operator packaging act, not an in-session capability; comm channel — none;
  PWA, Android, iPhone/iOS — out of scope for a transfer-file workflow; mobile consumption would
  re-verify the same digests before install.

## Tracker hook

Per the project-tracking rules, if this plan is later promoted to active work it must be
registered in `docs/plans/README.md` (the single source of truth for bugs, plans, and backlog),
moved from backlog into the planned section with a link to this document, and given a new,
never-reused tracker entry per the no-reuse rule. This research document deliberately carries no
tracker number and does not edit the tracker in its own work.

# Plan: Cross-Session Communication Rule

- **Date**: 2026-10-09
- **Version at planning**: v8.78.2
- **Status**: Shipped this pass.

## Context

Operator-requested (carried over from the prior part of this
session): future datawatch sessions may run in containers or on
different hosts entirely, and Claude Code's own native cross-session
messaging (`SendMessage`/`ListAgents`) only works between sessions
sharing one Claude Code install on one machine, and is invisible to
datawatch's own audit trail. The ask: a policy, mirroring the
existing Memory Use Rule, steering spawned sessions toward
datawatch's own audited memory/discussion/reply tools for anything
that should be audited or might cross a host/container boundary —
injected into session guardrails the same way the Memory Use Rule
and RTK instructions already are, not just written as a standalone
doc.

## Investigation

The injection mechanism (`internal/session/tracker.go`'s
`WriteSessionGuardrails`, called from `internal/session/manager.go`)
already does exactly this for two other concerns:

- `memoryInstructions()` — the Memory Use Rule, gated on
  `GuardrailsOptions.MemoryEnabled`.
- `rtkInstructions()` — RTK token-savings guidance, gated on
  `GuardrailsOptions.RTKEnabled`.

Both are appended into an **existing** project's `CLAUDE.md`/
`AGENT.md` only if not already present, on every claude-code session
spawn into that project directory. A fresh project gets the full
template (including a once-per-write refresh of the `# Session
Guardrails` header block — a separately-fixed bug, see
`guardrails_refresh_test.go`).

**A real pre-existing bug found while wiring `MemoryEnabled`**:
`cfg.Memory.SessionAwareness` is documented ("injects memory
instructions into session guardrails, default true") and fully
exposed over REST/PWA, but `manager.go`'s `guardrailOpts.MemoryEnabled
= m.cfg.Memory.Enabled` never actually consulted it — an operator who
turned memory on but explicitly disabled `session_awareness`
(expecting the guardrails injection specifically to stop) saw no
effect. Fixed in the same commit as this feature, since it's the
exact line being extended:
`m.cfg.Memory.Enabled && m.cfg.Memory.IsSessionAwareness()`.

## What shipped

- `internal/config.CrossSessionConfig{Enabled *bool}` — a tri-state
  pointer-bool defaulting to **true**, deliberately mirroring
  `Memory.SessionAwareness`'s shape (an instruction-injection toggle)
  rather than `Memory.Enabled`/`RTK.Enabled`'s shape (a subsystem
  toggle, default false) — there's no separate subsystem here to turn
  on or off, only a choice of whether the guidance text is injected.
- `GuardrailsOptions.CrossSessionEnabled` + `crossSessionCommunicationRule()`
  (`internal/session/tracker.go`) — appended into an existing
  CLAUDE.md/AGENT.md the same way `rtkInstructions()` is, gated on the
  new flag, idempotent (checked via the
  `<!-- cross-session-communication-rule -->` marker comment, same
  pattern RTK's own `<!-- rtk-instructions -->` marker uses).
- Policy content names 5 real, existing tools (confirmed by reading
  their actual MCP descriptions, not assumed): `memory_remember`/
  `memory_recall` (durable facts), `memory_discussion_write`/
  `memory_discussion_recall` (a shared, cross-host discussion scope
  synced to registered peers via WAL), `discussion_subscribe` (push
  delivery of new discussion entries instead of polling),
  `reply_to_parent` (reply to the spawning session), `memory_handoff`
  (task-to-task handoff within a story). Explicitly not an absolute
  ban — ephemeral same-host coordination nobody needs to audit is
  called out as still fine without going through datawatch.
- Applies to **claude-code sessions only** — same scope as the Memory
  Use Rule and RTK instructions; other backends write `AGENT.md` with
  no native cross-session-messaging concept to steer away from.

## Parity surface

- **REST**: `GET`/`PUT /api/config`'s `cross_session.enabled` (added
  to `handleGetConfig`'s map and `applyConfigPatch`'s switch,
  identical shape to every other boolean config field).
- **MCP**: no new tool — rides the existing generic `config_set`/
  `get_config` tools, same as every other simple config toggle.
- **CLI**: no new subcommand — the generic `datawatch config set
  cross_session.enabled=false` already works through the same REST
  path.
- **Comm channel**: excluded — same reasoning as GH#201 Phase 1's
  audit-log decision; a policy toggle isn't a chat-channel concern.
- **YAML**: `cross_session.enabled` (`docs/config-reference.yaml`).
- **PWA**: new "Cross-Session Communication Rule" settings card
  (`GENERAL_CONFIG_FIELDS` in `app.js`), 1 toggle, 1 new locale key
  (`settings_cross_session_enabled`) × 5 bundles.
- **Android/iPhone**: excluded — this governs text injected into a
  claude-code session's own project files on the daemon's host
  filesystem; neither mobile app has an equivalent surface.

## Verification

- `internal/session/gh_cross_session_policy_test.go` (4 tests): the
  policy is appended when enabled and a CLAUDE.md already exists, not
  duplicated on a second session's spawn into the same project, not
  added when disabled, and `CrossSessionConfig.IsEnabled()`'s
  tri-state default.
- `internal/server/applyconfigpatch_b38_test.go`: `cross_session.enabled`
  round-trips through `applyConfigPatch`.
- `node --test internal/server/web/*.test.js`: 182/182, no regression
  from the new PWA settings card.
- Full repo suite + `gosec` (exact CI command) + `release-smoke.sh`
  all re-run before shipping, per this session's standing practice.
- New howto with a diagram: `docs/howto/cross-session-policy.md`.

## Release checklist (AGENT.md Section A/C)

- [x] A2 — `go test ./...`: 3346 passed, 0 failed
- [x] A3 — `version: v8.79.0 (both files)`
- [x] A4 — `changelog: added`
- [x] A6 — `backlog: BL401 added to docs/plans/README.md`
- [x] A7 — `id-check: clean`
- [x] A8 — `leak-check: clean`
- [x] A9 — `node-check: 182/182 passed`
- [x] A10 — `make-build: ok`
- [x] C2 — `gosec: clean` (exact CI command: live=63, baseline=63)
- [x] C3 — `smoke: 185 passed, 0 failed, 35 skipped`
- [ ] A11 — `ci: <pending — check after push>`

## Out of scope / explicitly deferred

- Not a technical enforcement mechanism — this is guidance text in
  the LLM's own context, same as the Memory Use Rule. A session that
  ignores it and uses native `SendMessage` anyway is a
  model-following-instructions question, not a bug this plan fixes.
- Not retroactive — disabling the flag stops *future* sessions from
  adding the section if missing; it does not remove the section from
  a project's `CLAUDE.md` that an earlier session already wrote.
- The `ScheduleStore.Update()` bug found independently during this
  session (a REST schedule edit silently no-ops for spawn-type
  schedules because it only writes the legacy `Command` field, never
  `DeferredSession.Task`, which is what the fire path actually reads)
  is unrelated to this feature and was flagged, not fixed, in this
  pass — found while making an unrelated, approved production
  schedule change for a peer session's integration work.

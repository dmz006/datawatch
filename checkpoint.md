# Checkpoint — docs set for "Make a daemon restart safe" (planning restart policy)

Time: 2026-11-09 02:09
Status: **COMPLETE**

## What I verified (source of truth = code in this tree)

- Three values `reattach` / `restart` / `draft`, default `reattach`,
  normalize/valid in `internal/config/planning_restart_policy.go`; unset or
  unrecognised reads as `reattach`; invalid values refused on every write
  path (REST `PUT /api/config`, CLI `autonomous config-set` client-side
  pre-send, MCP `autonomous_config_set`, comm `configure`, PWA select) —
  confirmed in `internal/server/restart_policy.go`,
  `cmd/datawatch/cli_autanning_restart_policy.go`,
  `internal/mcp/planning_restart_policy_config.go`,
  `cmd/datawatch/cli_autonomous.go`, `internal/server/web/app.js:18272`.
- **Boot-resume path in this build does NOT read the setting**
  (`internal/autonomous/api.go` still resets the PRD to `draft` and kills the
  decompose session; `grep -rn PlanningRestartPolicy internal/autonomous/` is
  empty). So the "post-change behaviour" is DECIDED (operator decision
  2026-11-08, `docs/plans/restart-matrix-findings.md`) but not yet in this
  build — every docs description of it must carry that caveat. It does.
- `GET /api/config` reports the value (api.go:6593); `datawatch autonomous
  config-get` does not show it (consistent with how-to's Checking section).
- PWA select + localized help exist on all five locales
  (`settings_planning_restart_policy*`), help text links the how-to and
  carries the "until the build reads it, every value acts like draft" caveat.
- How-to `docs/howto/restart-with-work-running.md` is referenced from
  `scripts/check-howto-coverage.sh` (LLM-translation list) and
  `scripts/check-locale-keys.sh` (known-key allowlist), and from
  autonomous.go MCP description, locales, and operations.md.

## Edits made (commit cd7d08c, 4 files +10/−7)

1. `docs/howto/restart-with-work-running.md`
   - Web UI row: was "Not yet on a Settings card" → now describes the PWA
     select (Automata tab, *Autonomous Automata planning* card) + locale keys.
   - Current status: added pointer to the *Still to review (operator)* notes
     in operations.md and the explicit operator-verification instruction.
   - Write-path list now includes the PWA select.
2. `docs/datawatch-definitions.md` (Planning restart behavior entry)
   - Names the setting `autonomous.planning_restart_policy`, all three values,
     the draft-fallback caveat, links the how-to; keeps existing links
     (restart-matrix-findings, autonomous-recovery, operations).
3. `docs/howto/autonomous-recovery.md`
   - Third-case paragraph: names the setting + values, links
     restart-with-work-running.md for Current status and every surface.
4. `docs/operations.md` (Planning restart values bullet)
   - Lists all five write surfaces incl. the PWA select; adds the
     "until the build reads it" caveat pointing at Still to review.

## Gates

- `scripts/check-howto-coverage.sh` → PASS
- `scripts/check-docs-sync.sh` → in sync (mirror populated, idempotent)
- Manual link integrity on all 4 edited files → no broken links
- `docs/config-reference.yaml` block re-checked — consistent, no change needed

## Flagged for operator review (kept/added in docs, NOT code)

- The build in use must be confirmed to carry the restart-safe planning
  boot path; until then all values read as `draft` (re-queue with
  `datawatch autonomous prd-plan <id>`). This is the "Still to review
  (operator)" section and the how-to's Current status.
- Release smoke restart-matrix run with `planning` skipped until the
  build ships (operations.md).

DATAWATCH_COMPLETE: docs set updated and committed (cd7d08c): PWA surface documented as live on all four doc surfaces, build-status caveat + operator-review flags consistent with the code in this tree; coverage/docs-sync/link gates pass.

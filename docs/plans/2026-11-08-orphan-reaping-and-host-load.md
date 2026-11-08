# Orphan reaping and host load — plan

- **Date:** 2026-11-08
- **Status:** Done — all eight phases complete on this branch; the release version is
taken at merge time (see Version), and `CHANGELOG.md` [Unreleased] is the record
until then.
- **Version:** Taken at merge time. This feature branch deliberately does not change the
  version in `cmd/datawatch/main.go` or `internal/server/api.go`; the merge helper names
  the release version when the branch reaches main, and `CHANGELOG.md` [Unreleased] is
  the record until then.
- **Defect addressed (found 2026-11-04):** after a worker task was cancelled, the
  processes the worker had started — test runs, servers, helper scripts — kept running
  and piled up on the host.

## Scope

Files and packages touched by this work, repository-relative:

- `internal/autonomous/orphan_reaper.go` — evidence-based orphan finder and reaper
  (`/proc` working directory / executable / script folder, ancestry, session-id
  environment; exact-pid signals, evidence re-checked before each signal).
- `cmd/datawatch/descendant_processes.go` — the existing recorded-descendant helper the
  reaper reuses: record pids + start times while the worker runs, end only recorded pids.
- `cmd/datawatch/main.go` — wiring the reap into every session end (finished, cancelled,
  failed, daemon stopping) and once-per-minute host load check tick loop.
- `internal/autonomous/host_load.go` — the host load check core
  (`autonomous.host_load_alert`: `mode`, `multiple`, `minutes`), including the
  read-only `Status()` snapshot.
- `internal/autonomous/manager.go`, `internal/autonomous/models.go` — status
  propagation so the check's state rides the existing status and stats surfaces.
- `internal/server`, `internal/mcp`, `internal/stats/collector.go`,
  `internal/metrics/prometheus.go` — the check's config on REST, MCP, CLI, comm channel
  and YAML, its read-only state on `GET /api/stats`, `GET /api/autonomous/status`, the
  `autonomous_status` MCP tool and the `datawatch_host_load_*` Prometheus gauges.
- `internal/server/web` (Settings → General → Autonomous) and
  `internal/server/web/locales/*.json` — three new setting labels in five locales.
- The bounded load step rule: the built-in `bounded_load_step` task template, the rules
  block appended to the decompose prompt, and the warn-severity `unbounded_load_step`
  decompose-lint finding.
- Docs: `docs/howto/automata-orphan-reaping.md`, `docs/howto/host-load-check.md`,
  `docs/datawatch-definitions.md`, `docs/operations.md`, `README.md`, `CHANGELOG.md`,
  `docs/plans/host-load-status-surfaces.md`, and the parity issue drafts under
  `docs/plans/parity-issues/`.

## Phases

1. **Session-end reap.** Evidence-based orphan finder and reaper on top of the existing
   recorded-descendant signalling; wired into every session end. The reap never fails or
   blocks the session end; a reap that ended anything writes one `session_end_reap`
   audit entry (plus a task decision when the session was bound to a task), and nothing
   is written when it finds nothing.
   — Status: **Done** (version taken at merge time).

2. **Host load check core.** Once-per-minute watch of the 1-minute load average; when it
   stays above `multiple` (default 1.5) times the core count for `minutes` (default 10)
   consecutive minutes, one warning alert per episode naming the busiest process groups
   and whether each belongs to a running or an ended session's worktree or scratch
   folder, by the same evidence rule — never by name. Modes `off`, `alert` (default)
   and `reap` (also ends the leftover processes of ended sessions the evidence proves;
   a running session is never touched).
   — Status: **Done** (version taken at merge time).

3. **Config surfaces.** `autonomous.host_load_alert` round-trips through YAML, REST,
   MCP, the CLI, the comm channel and the PWA General settings; reads live, so a change
   needs no restart; bad values refused on every write surface, naming the key.
   — Status: **Done** (version taken at merge time).

4. **Bounded load step rule.** A task that asks to pin the CPU must bound it (niced,
   capped at a quarter of the cores, hard-timed, self-cleaning, dead when its task is
   dead) — stated once in three surfaces: the `bounded_load_step` task template, the
   decompose-prompt rules block, and the `unbounded_load_step` lint finding.
   — Status: **Done** (version taken at merge time).

5. **Monitoring surfaces.** The check's read-only state exposed on the existing
   monitoring surfaces — `GET /api/stats` (and its WS broadcast),
   `GET /api/autonomous/status`, the `autonomous_status` MCP tool, and Prometheus —
   per `docs/plans/host-load-status-surfaces.md`.
   **Monitoring surfaces that do not apply (as recorded by that task):** the CLI (no
   existing `datawatch <noun> status`-style surface prints host load; a new CLI command
   would be a new surface, out of scope — the alert remains the one proactive
   notification), the non-API web UI (the new JSON fields are additive and show up where
   the existing endpoints are already rendered; no template or JS change), and
   session alerts / chat broadcast (the check's existing alert path is unchanged; no new
   per-session alert or WS frame).
   — Status: **Done** (version taken at merge time).

6. **Docs set.** Operator how-tos, definitions, operations sections, README lines, the
   `CHANGELOG.md` [Unreleased] entry with its Reuse audit, and this plan.
   — Status: **Done** (version taken at merge time).

7. **Parity issue drafts.** `docs/plans/parity-issues/2026-11-08-session-end-reap-parity.md`,
   `docs/plans/parity-issues/host-load-check.md`,
   `docs/plans/parity-issues/session-end-descendant-reap.md` — each with its
   `Parity surface` section.
   — Status: **Done** (drafts only; not filed, per their Filing records).

8. **Lints and test hygiene.** Four lint findings in the new reaper and load tests fixed.
   — Status: **Done** (version taken at merge time).

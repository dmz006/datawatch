# datawatch v5.26.13 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.12 → v5.26.13
**Patch release** (no binaries — operator directive).
**Closed:** Shell hidden from autonomous LLM list + cancel/delete kills spawned worker sessions + smoke aligned with the exclusion.

## What's new

### Shell backend hidden from autonomous LLM dropdowns

Operator: *"Shell should be excluded from llm list for automation."*

The `shell` backend is a session backend (raw bash) but isn't an LLM — autonomous PRDs running through it have nothing to "decide" or "plan". v5.26.13 filters it out of the per-PRD / per-task LLM override dropdowns via a new `NON_LLM_BACKENDS` set in `renderBackendSelect`. Existing PRDs that already have `backend: shell` still render the value via the fall-through "(not configured)" option so the assignment doesn't drop silently — but new picks can't choose it.

### Cancel + delete now kill the spawned worker sessions

Operator: *"If running prd is stopped, does it properly stop session, same for deleting."*

**Pre-v5.26.13 answer: no.** When a PRD ran, the executor spawned a tmux session per task and stored the `SessionID` on the task. When the operator cancelled or hard-deleted that PRD, the session pointers were dropped from state but the tmux sessions kept running — accumulating `autonomous:*` orphans that eventually hit the `session.max_sessions` cap and blocked new spawns with `500 max sessions`. End-to-end smoke caught this when the lifecycle test couldn't spawn its workers.

v5.26.13: the REST DELETE handler in `internal/server/autonomous.go` now walks `SessionIDsForPRD()` **before** mutating PRD state (since hard-delete cascades and we'd lose the pointers afterwards) and best-effort calls `s.manager.Kill(sessionID)` for each. Response payload includes `killed_sessions: N` so the operator sees how many were reaped. Same path for both bare-DELETE (cancel → flips status) and DELETE?hard=true (remove + descendants).

### `release-smoke.sh §7b` switched off `shell`

The lifecycle test was using `backend: shell` to avoid burning LLM tokens — but per the new exclusion rule, smoke shouldn't be running through a backend the autonomous UI now hides. Section 7b now picks the first available LLM (`ollama` → `openwebui` → `opencode` → `claude-code` priority order); skips if none. ollama is local + free so on operator hosts with ollama wired (most), the smoke still runs without cost.

### One-shot orphan cleanup (manual run, this session only)

The accumulated `autonomous:*` orphans on the operator's daemon (9 sessions across prior PRD runs) were cleaned via `POST /api/sessions/kill` per session ID. v5.26.13's auto-cleanup-on-delete prevents future accumulation.

## Configuration parity

No new config knob.

## Tests

- 1397 Go unit tests passing.
- **Functional smoke: 29 PASS / 0 FAIL / 2 SKIP** via `scripts/release-smoke.sh`.
- Smoke §7b now exercises the LLM worker spawn path (vs. shell pre-v5.26.13).

## Known follow-ups

- **Cascade-delete session kill across child PRDs** — current implementation kills the direct PRD's task sessions; if SpawnPRD-spawned descendants have their own running sessions, those need to be killed too. Add to smoke + impl as follow-up. (Low risk in practice since v5.26.8 cascade-delete already refuses if any descendant is `running`, so descendants' sessions should already be terminated by the time hard-delete walks them.)
- v6.0 packaging items unchanged.

## Upgrade path

```bash
git pull
datawatch restart
# Try stopping a running PRD — response now includes
# killed_sessions: N. Tmux session list no longer accumulates
# orphan autonomous:* names.
```

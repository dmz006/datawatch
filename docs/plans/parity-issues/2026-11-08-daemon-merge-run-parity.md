# [sync] feat(merge): daemon-run merge pipeline, per-repo merge lock and run history (Part of finishing review and merge by the daemon)

**Filed 2026-11-09 as dmz006/datawatch-app#421, one consolidated issue for the merge lock, validator and merge run drafts** (shipped in v9.118.0).

Parent work: "Part of finishing review and merge by the daemon (Phase 4 of the autonomy
handover plan)", operator-approved 2026-11-07. This draft is a planning note for the
datawatch-app parity issue. It is a file only: nothing has been filed on any tracker.

## Affected area

The daemon-side merge pipeline that ships a finished Automaton's work branches into the
main branch: the per-repository merge lock, the bounded merge-run history, the run entry
point `POST /api/autonomous/merge/run`, and the read model `GET /api/autonomous/merge`
that reports held locks and recorded runs.

Code involved:

- `internal/autonomous/merge_lock.go` and `internal/autonomous/merge_lock_test.go` — the
  persisted per-repository lock store (`merge-locks.json`), with acquire, release, status
  and list.
- `internal/autonomous/merge_lock_status_test.go` — checks how a held lock is rendered
  and that no lock key appears when nothing is held.
- `internal/mergepipeline/run.go` and `internal/mergepipeline/run_test.go` — the Runner
  that resolves a pipeline, takes the lock, runs steps, runs post-ship hooks and releases.
- `internal/autonomous/merge_pipeline_run.go` — the run records (bounded to 200 entries,
  newest first, persisted to `merge_pipeline_runs.json`).
- `internal/server/merge_run.go` — the HTTP handler for `POST /api/autonomous/merge/run`.
- `internal/autonomous/merge_status.go` and `internal/autonomous/merge_text.go` — the
  merge status read model and the text renderers shared by CLI, MCP and comm channel.

## Current behavior

- **Starting a run is REST-only.** `POST /api/autonomous/merge/run` takes a title, a list
  of branches, an optional Automaton ID and an optional project name, and runs the merge
  synchronously. The MCP tool `autonomous_merge` is read-only; its description points the
  caller at the REST endpoint. In this review I found no CLI verb, comm-channel verb or
  PWA control that starts a run. (These were checked by search of the code, not by a live
  test.)
- **Two concurrency guards exist and they are separate.** The handler takes an in-process
  mutex and answers `409 a merge is already running` when it is held. The Runner then takes
  the persisted per-repository lock. A second run against the same repository is refused
  by the lock with a message that names the holding run and Automaton. The in-process
  mutex does not cover a second daemon or a restarted daemon; only the persisted lock does.
- **The lock survives restarts.** A held lock is written to `merge-locks.json`, so a
  restarted daemon still shows it held. A resumed run takes its own lock back. Locks with
  no run and no Automaton are not reported as held.
- **If the lock store cannot be opened, the run is refused.** The handler's fallback locker
  refuses every acquire, so the run returns a failure instead of running without a lock.
- **Run history is bounded.** The last 200 runs are kept, and `GET /api/autonomous/merge`
  returns them as `pipeline_runs`. Each run carries the pipeline source in effect and one
  entry per step with a start, an end and a result (ok, failed, skipped or running).
- **Post-ship hook results** are recorded in the run record. A failed hook leaves the ship
  as it was and leaves one triage note when the run belongs to an Automaton.
- **Surfaces that show the state.** The merge state (including held locks and recent runs)
  is rendered by `GET /api/autonomous/merge`, the `autonomous_merge` MCP tool, the
  `datawatch autonomous merge` command and the `autonomous merge` chat verb, through the
  shared renderer in `merge_text.go`.

## Expected behavior

- Every surface that can show a merge state can also start a merge run, or the gap is
  recorded as an explicit, reason-logged exclusion in the feature's `Parity surface`
  section. Today the start action exists only on REST.
- A merge run that is started from any surface shows the same run record and the same
  held-lock state as `GET /api/autonomous/merge`.
- The concurrency rule is stated once and holds across daemon restarts: two runs never ship
  into the same repository. The in-process guard is treated as a fast path only, with the
  persisted lock as the authority.
- A refused run (lock held, lock store unavailable, no base repository) tells the caller
  why in the same words on every surface. Whether a refused run leaves a run record behind
  is not yet verified and should be checked before the issue is filed.
- The PWA Automation view shows the held lock and the recent runs next to the merge badge
  and the safe-to-delete card, with the same wording the CLI and comm channel use.

## Parity surface

| Surface | Status | Note |
|---|---|---|
| REST | Included | `GET /api/autonomous/merge` (state and runs); `POST /api/autonomous/merge/run` (start). |
| MCP | Partly included | `autonomous_merge` reads the state and runs. No MCP tool starts a run. |
| CLI | Partly included | `datawatch autonomous merge` reads the state. No CLI verb starts a run. |
| comm channel | Partly included | `autonomous merge` reads the state. No chat verb starts a run. |
| YAML/config | Included | The merge pipeline config, including `post_ship`, is readable and writable through the existing config surfaces. |
| PWA | Partly included | The Automation view shows the merge badge and the safe-to-delete card. No start control and no lock or run panel were found. To be confirmed in the PWA code before filing. |
| Android | Not verified | Not checked in this draft. Capability parity is required by the Mobile-Parity Rule; the datawatch-app issue must state its own status. |
| iPhone | Not verified | Not checked in this draft. Same as Android. |

## Suggested next step

Write the datawatch-app parity issue from this draft after the PWA check above is done. The
issue should ask for three things, in this order:

1. A start action for a merge run on the PWA, wired to `POST /api/autonomous/merge/run`,
   with the same refusal wording the REST handler returns.
2. A held-lock and recent-runs panel on the PWA Automation view, reading the same fields as
   `GET /api/autonomous/merge`.
3. Android and iPhone rows in the same parity table, each marked included, partly included,
   or excluded with a reason.

On the datawatch side, the next step is to decide whether a start verb belongs on the CLI,
the MCP surface and the comm channel. That is an operator decision about the scope of the
daemon's merge authority, so it is not decided here. Until it is decided, the parity table
above records those three surfaces as partly included with the reason stated.

Verification still to do before this issue is filed: a live check of the PWA Automation view
for a start control and a lock panel, and a live check of the lock refusal on a sandbox
daemon, run with `env -u TMUX -u TMUX_TMPDIR`. Unit tests in the files listed above cover
the lock store and the Runner, but they do not confirm the live behavior.

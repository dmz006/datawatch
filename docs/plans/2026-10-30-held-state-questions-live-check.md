# Live check — automaton held-state questions (Attempt 2, live-daemon only)

**Date:** 2026-11-01 (howto under test: `docs/howto/automaton-held-state-questions.md`)
**Tree:** `/home/dmz/.datawatch/prd-worktrees/56d30001`
**Binary under test:** `./datawatch` (139.5 MB, built in a prior session from this tree)

**Sandbox daemon (heading names it, per protocol):**
- PID **3230892** (tracked in `tmp-held-verify/daemon.pid`; a 1-thread wrapper PID 3230049 was the file's original occupant — corrected)
- Port **18142** (127.0.0.1)
- Data dir **`tmp-held-verify/dw-data`**, config `tmp-held-verify/sandbox-config.yaml`
- Started with `env -u TMUX` isolation; no production daemon touched, no `pkill`, no systemctl
- Killed by PID with SIGTERM at teardown; **clean shutdown** ("Shutting down... (signal=terminated)", "Killing active sessions..."), no SIGKILL needed

## Method

Everything below was exercised **live on the sandbox daemon** — REST API, CLI
(`./datawatch question ... --url http://127.0.0.1:18142`), config surfaces, and
the daemon's own audit log / question store in the sandbox data dir. No lifecycle
check was delegated to test suites (that was the failure mode of Attempt 1's report,
which is replaced by this one). Where a claim is code-level rather than observed, it
is labeled as such.

**Five fixtures** (all created via the daemon API under the sandbox data dir):

| id | shape | final live state |
|----|-------|------------------|
| `0e160001` | revision-hold fixture (decision gate off) | `revisions_asked` → cancelled at teardown |
| `18b00001` | failing verifier (setup fault) | `blocked` (empty PRD `blocked_reason`) → cancelled |
| `19d20001` | awaiting-decision fixture (decision gate `on`) | `running` → cancelled |
| `a8780001` | failing fixture (exit-3 verifier) | `approved`, never spawned (capacity) → cancelled |
| `16d71a2b` | automation with pending member insert `98925e98` (member 19d) | deleted at teardown |

## Results (live observations)

| # | Check | Result | Evidence |
|---|-------|--------|----------|
| 1 | Shadow mode | **PASS** | `held_mode: shadow` → pass wrote **shadow notes only** (3 items), `recommended_option: hold`, zero open questions, zero alerts |
| 2 | `held_mode` surface | **PASS** | It is a **main config** key: `autonomous.questions.held_mode`; `PUT /api/config` accepts `off`/`shadow`/`on`/`""`; **`"purple"` → HTTP 400** |
| 3 | On-mode raise | **PASS** | `held_mode: on` → held questions appeared as **open** with the per-state option tables ("hold" always default) |
| 4 | `pending_insert` hold | **PASS** | `agent_insert_policy` is a **manager config** key (`autonomous.automation.agent_insert_policy`, `PUT /api/autonomous/config`). Default (allow) inserted the member into `members[]` immediately; with `require_operator` the member landed in `pending_inserts[]` and the pass raised a pending_insert question — options approve_insert / reject_insert / hold |
| 5 | Answer + stale refusal | **PASS (stale) / PARTIAL (fresh)** | `question answer` records the answer (`a-…` returned, question → state `answered`). Answering a question whose state moved → **HTTP 409 stale refusal** (live) |
| 6 | Answer → action routing | **NOT OBSERVED LIVE (see Finding 1)** | Answers `a-18e3899c…658d33` (resume), `a-18e38a3d…1367f` (approve_insert) recorded, but after ≥90 s the holds were unchanged and **no `held_answer` audit action ever appeared** in `dw-data/audit.log` |
| 7 | Withdraw-on-clear | **PASS** | Delete automation `16d71a2b` → open pending_insert question `q-18e38a40…` → **`withdrawn`** on the next pass; cancel `0e160001` → open revision question `q-18e38a08…` → **`withdrawn`**. Final pass: `open: []`, `shadow: 0` |
| 8 | Re-raise while held | **PASS (observed)** | While a hold persists the pass raises a **new question id every minute** (old ones are `answered`, not `open`; dedupe is against open source-lines) |
| 9 | `blocked` hold scope | **PASS (scope)** | 18b ended `blocked` with **empty** `blocked_reason` → correctly *excluded* from the held scan. Code (observation): `routeHeldBlocked` handles review-hold/boot-park blocks with resume; other block reasons are refused with a say-so |
| 10 | Alerts | **PASS** | `dw-data/alerts.json`: 8 alerts, **all `level: info` session-lifecycle events** (my sandbox sessions' started/complete, verifier started/complete). **Zero held-question alerts.** Code-level: `held_question_pass.go` has no notify call and `BuildHeldQuestion` sets no Priority |
| 11 | CLI surface | **PASS** | `datawatch question held --url …` lists open/shadow with option tables; `question answer … --option … --text …` records answers; `question answer … --state bogus` → proper HTTP 400 passthrough. Note: CLI targets the daemon via the **`--url` flag**, not `DATAWATCH_BASE_URL` |
| 12 | Config write semantics | **TRAP FOUND (Finding 2)** | `PUT /api/config` with `{"autonomous":{"questions":{"held_mode":"on"}}}` **replaced the whole `autonomous` block** — my earlier `plan_coverage_check: off` was clobbered back to the default `block` |

## Findings (live + code-level)

1. **Fresh held answers are recorded but no action is routed in this live daemon.**
   The daemon's pass loop is unambiguously alive (held questions re-raised on the
   minute boundary in `audit.log`, status changes applied), yet across ~10 minutes
   of polling after each fresh answer: (a) 0e stayed `revisions_asked`, (b) the
   pending insert stayed pending, (c) no `held_answer` audit action (the one
   `recordHeldAnswer` writes on *any* outcome — applied/recorded/refused/stale)
   ever appeared, (d) answered questions stayed state `answered` rather than being
   withdrawn by routing. The code path exists
   (`internal/autonomous/question_tick.go` → `ProcessHeldAnswer`,
   `internal/autonomous/held_question_answer.go`) and runs in the same tick loop
   as the pass that demonstrably did run, so the gap is in the live wiring of this
   build's `questionPass`, not in the routing logic itself. Reported as observed
   behavior; the code does not explain why.
2. **`PUT /api/config` is a replacement, not a deep merge**, at the `autonomous`
   block level. Editing one nested key silently resets every sibling to defaults
   (bit me: `plan_coverage_check: off` lost its value; `agent_insert_policy` had
   to be re-applied).
3. **The plan-coverage gate reads the *stored* record, not live config.**
   Approval of a PRD decomposed under `plan_coverage_check: block` (0e: 0 unmatched
   requirements, **4 structural findings**) stays blocked after flipping live
   config to `off` — the gate checks `prd.PlanCoverage.Mode`
   (`plan_coverage_gate.go:174` et seq.; the string `plan_coverage_check=block` in
   the error is literally hardcoded). Escape hatches: acknowledge is only allowed
   **from `needs_review`** (0e in `revisions_asked` was refused), or re-decompose
   under the new mode.
4. **`blocked` holds require a non-empty PRD `blocked_reason`.** A PRD can be
   `blocked` on the task/verifier level with an empty reason — the pass then
   correctly does not hold it. (Live: my 18b fixture.)
5. **Hold state → capacity interaction:** 18b's blocked task kept a host-pool lease
   (capacity API: held=1, holder `353b0002`… holder id `b053…`-class) and
   `a8780001`'s task never spawned while the pool was exhausted — consistent with
   `max_concurrent_tasks: 1`; no panic or deadlock in daemon logs.
6. **Fixture hygiene:** one fixture session (`fb7a`) briefly acquired the real
   repo's tree lock (`/home/dmz/workspace/datawatch/.datawatch/tree.lock`); it was
   killed via the daemon on cancellation and the lock is gone. Fixture specs were
   write-only-harmless (echo to files in the sandbox project dir).
7. **Howto accuracy:** I did not find a howto statement contradicting the live
   surfaces I ran in this attempt (states, options, "hold" default, shadow-first
   advice, stale refusal, withdraw-on-clear behavior all match). I deliberately
   avoided quoting howto wording I had not re-verified (Attempt 1's Finding-10
   error mode).

## Teardown record (all verified)

- All four PRDs cancelled (`{"status":"cancelled"}` each); automation `16d71a2b` deleted (`not found` afterwards)
- Sessions: 3 verifier sessions `complete`, `fb7a` `killed`
- Final held pass: `open: []`, `shadow: 0`; last two held questions both `withdrawn`
- `alerts.json` re-checked after teardown: still 8 info-level session alerts, no held-question alerts
- Real-repo tree lock confirmed absent
- Daemon PID 3230892 stopped by SIGTERM (clean shutdown logged, no -9), port 18142 closed, wrapper gone, no `held-verify` processes remaining
- Sandbox artifacts left in-tree under `tmp-held-verify/` (data dir, config, logs) — inspectable for audit

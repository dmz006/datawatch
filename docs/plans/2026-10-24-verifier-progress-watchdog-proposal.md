# Verifier progress watchdog: count the verifier's own liveness, not just pane silence

Date: 2026-10-24
Status: implemented, pending final gate

Confirmed: yes

## Summary

On 2026-10-24 a print-mode pi verifier (task 52970007, BL590 PRD) wrote nothing to its pane until it finished; the no-progress watchdog killed both verification sessions after ten minutes of silence, and the task's final gate failed with it — even though a verification on a GPU shared by three engines legitimately takes longer than that. The proposed direction is to let the watchdog accept signals that the verifier is genuinely still working (a live runner process, a held capacity slot, a model-server load signal) and/or raise the limit, so silence alone never ends a verifier that the system can prove is still running.

## Current behavior

- The end condition: `VerifierWatchdog.Observe(now, outputPresent, marker)` exceeds the limit only while neither the output file exists nor the pane marker has changed; the error text is built by `VerifierWatchdogMessage` (internal/autonomous/verifier_watchdog.go:36, internal/autonomous/verifier_watchdog.go:58).
- Wiring: the watchdog is built only for sessions named with `autonomous:verify` (verifierSessionNamePrefix, cmd/datawatch/session_judgment.go:211), its clock starts at `spawnedAt` (cmd/datawatch/session_judgment.go:332, cmd/datawatch/session_judgment.go:374), and it is observed on each poll with a false `outputPresent` and `sessionPaneMarker`, a SHA-256 of the last 50 lines of pane output (cmd/datawatch/session_judgment.go:411, cmd/datawatch/session_judgment.go:216).
- Slot ordering: the capacity slot is acquired before the verifier session is spawned — `verifierCapacityAdmit` runs at cmd/datawatch/main.go:5659 and the session spawn at cmd/datawatch/main.go:5687; the admit does a blocking `led.Acquire` bounded by the capacity wait duration (cmd/datawatch/capacity_wiring.go:594, cmd/datawatch/capacity_wiring.go:612). Because the watchdog clock starts only once the session's wait loop begins (cmd/datawatch/session_judgment.go:332, after the spawn), **slot-wait time does not count toward the limit today** — only post-spawn silence does.
- What the ledger can tell the watchdog: `Ledger.Held(holder)` reports whether a holder has a lease and `Ledger.LeaseOf(holder)` the lease itself (internal/capacity/ledger.go:517, internal/capacity/ledger.go:528); `Snapshot()` exposes per-pool `Held` counts and the `Waiting` list with `Since` timestamps (internal/capacity/ledger.go:92, internal/capacity/ledger.go:110, internal/capacity/ledger.go:73, internal/capacity/ledger.go:600) — so it can distinguish "the verifier is holding a slot" from "some holder is still waiting for one".
- Process liveness: `hasLiveChildProcess(tmuxSession)` already reports whether a runner process (claude, pi, opencode) is alive under a session's panes, and is what the daemon uses for its idle-stall detector (cmd/datawatch/session_idle_probe.go:23, cmd/datawatch/session_idle_probe.go:29).
- Model-server probes: the existing Ollama status probes report which models are loaded/running (VRAM size, expiry) — `FetchOllamaStats` with `RunningModels` (internal/stats/ollama.go:19, internal/stats/ollama.go:23) and the observer tap that polls `/api/ps` for the loaded-model list (internal/observer/ollama_tap.go:28). **None reports an active request per session.** They can instead report model-loaded/VRAM state per host, and the capacity layer already caches GPU utilisation per node (cmd/datawatch/capacity_wiring.go, `gpuNodeBackpressure`).
- Today's rule, docs/operations.md:534: "A verifier session that shows neither an output file nor any change in its pane output for `autonomous.verifier_no_progress_timeout_seconds` (default 600, ten minutes; unset or non-positive uses the default) is ended instead of waiting out the long verifier timeout." docs/implementation.md:1444 adds that the default is 600 seconds (`autonomous.verifier_no_progress_timeout_seconds`) and that a trip "classifies as infra like the timeout".

## Decisions for the operator

**Operator answers, 2026-10-24 (recorded by the supervisor; these replace the proposals below where they differ):**

1. Progress: a live runner process or a held capacity slot for the verifier's holder counts as progress (options B plus C). Option D is deferred until a per-session request probe exists.
2. Limit: keep the single setting `autonomous.verifier_no_progress_timeout_seconds` and raise its default to 900 seconds.
3. Slot wait: time spent waiting for a capacity slot is excluded from the limit, and the docs say so explicitly.
4. Hung verifier: hand off to the fallback verifier backend at once, under the existing infra escalation threshold (option C). This differs from the proposed option A; the operator chose it.

The proposals and options below are kept for reference.

All answers below are proposals, not decisions.

1. **What counts as progress for a print-mode verifier?**
   - Option A — keep only "output file or pane change": simplest, but misreads a silent-but-working print run as hung, which is exactly the 2026-10-24 incident.
   - Option B — also count a live runner process (`hasLiveChildProcess`): cheap, already daemon-side, true for print-mode pi; but a wedged runner that never exits would keep it alive, so the long verifier timeout remains the real backstop.
   - Option C — also count a held capacity slot (ledger `Held` for the verifier's holder): proves the task holds a real compute claim; but the slot is held even while the model is idle, so it is a weaker liveness signal than B.
   - Option D — also count an active model-server request for the session: strongest signal, but no existing probe reports per-session requests (see Current behavior), so this needs new plumbing.
   - Proposed: B plus C — treat a live runner process or a held capacity slot for the verifier's holder as progress; defer D until a per-session request probe exists.

2. **The new time limit: value, and one setting or two?**
   - Option A — one raised value for everything: simplest config surface; conflates "waiting for work to start" with "work running slowly".
   - Option B — one value plus a separate waiting value: more precise, but adds a second key and a second parity surface set for a distinction that the current code already handles (slot-wait time does not count today, see Current behavior).
   - The current default is 600 seconds (`autonomous.verifier_no_progress_timeout_seconds`; docs/operations.md:534, docs/implementation.md:1444).
   - Proposed: keep one setting (`autonomous.verifier_no_progress_timeout_seconds`) and raise its default to 900 (fifteen minutes), now that "progress" also means the process/slot signals above.

3. **Does time spent waiting for a capacity slot count toward the limit?**
   - Option A — count it: simpler mental model, but punishes a verifier that is merely queued behind two sibling engines on a shared GPU for over ten minutes.
   - Option B — exclude it (today's de facto behavior, since the clock starts only after spawn at cmd/datawatch/session_judgment.go:332): the limit measures the run, not the queue; queue waits are already covered by the capacity wait bounds and the 30-minute long-wait alert.
   - Proposed: exclude slot-wait time from the limit, and say so explicitly in the docs rather than relying on the spawn-ordering accident.

4. **What happens when a verifier is judged hung?**
   - Option A — end as an infra failure exactly as today: no code change to the failure path; retries and the existing infra-escalation threshold already drive fallback.
   - Option B — retry once before classifying: adds one re-run per misfire, but doubles GPU time spent on an already-suspect session.
   - Option C — hand off to the fallback verifier backend immediately under the existing infra escalation threshold: faster recovery, but risks burning the fallback on a transient slow run that a retry would have passed.
   - Option D — hold the task and notify the operator: safest on failures, but stalls the PRD unattended in an automated pipeline.
   - Proposed: keep A — end and classify as infra as today; the existing infra retry and escalation-to-fallback machinery handles recovery.

## Parity surface

Only the meaning of the existing `autonomous.verifier_no_progress_timeout_seconds` key changes (its default value and what counts as progress); no new config key is proposed.

- REST: excluded — the key is already served; only its evaluation semantics change.
- MCP: excluded — `autonomous_config_set/get` already expose the key; no key added.
- CLI: excluded — the flag/config already reads the same key.
- Comm channel: excluded — the config already flows through the existing channel.
- YAML/config: excluded — the key already exists; only the default and documented meaning change.
- PWA: excluded — the settings card already edits the key (label `settings_verifier_no_progress_timeout`, per docs/implementation.md:1444).
- Android: excluded — config parity follows the existing key; nothing new to ship.
- iPhone: excluded — same as Android; existing key only.

## Phases

1. This proposal — operator confirms the four decisions above. Status: Done — confirmed on 2026-10-24, recorded above.
2. Implement the confirmed rule: extend the watchdog's progress signals (runner process, held slot) in the one-shot runner's wait loop, apply the confirmed default, and unit tests. Status: Done — decision logic and signal wiring implemented and unit-tested.
3. Live smoke: run a print-mode verification on a shared-GPU engine and confirm a legitimately silent run survives while a genuinely dead one is still ended. Status: Pending — no live run recorded yet.
4. Final gate and docs: re-verify the docs/operations.md and docs/implementation.md paragraphs, and the gate task. Status: In progress — docs updated to the confirmed rule; final gate pending live smoke.

## Status

Confirmed by the operator on 2026-10-24: the four decisions are recorded above and the Confirmed line reads yes. The implementation (progress signals, confirmed 900-second default, slot-wait exclusion, fallback-backend escalation) has landed; the live smoke is still pending and the final gate will close out the remaining docs.

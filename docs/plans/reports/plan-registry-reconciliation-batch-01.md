# Plan registry reconciliation — batch 01 (entries 1 to 20 of 118)

Entries 1 to 20 of [plan-registry-reconciliation-list.md](plan-registry-reconciliation-list.md),
checked against the registry in `docs/plans/README.md`. Class a means the
subject of the file already has a registry row (the row is named); class b
means a real plan or shipped feature with no registry row (a proposed row is
given, in the registry's "description | status" shape with the pipe escaped);
class c means the file is not a plan (the reason is given). "Row N" refers to
the registry's own N row, whose first column is that number.

Operator review: the two class-`b` proposed rows in this batch
(`2026-10-13-ci-watch-live-verification.md`,
`2026-10-14-verification-llm-override-live.md`) are still not in the registry
(checked 2026-11-11); accept or reject each proposal before it is added to
`docs/plans/README.md`.

| file | class | evidence | proposed row |
|---|---|---|---|
| 2026-10-13-ci-watch-live-verification.md | b | Records the ci-watch completion guardrail (how-to `docs/howto/ci-watch-guardrail.md`): guardrail config, live verification against a real GitHub Actions run on 2026-10-13, release v9.0.63. No registry row covers this guardrail (row 193 is the 2026-10-24 Automation coordinator CI watch; row 8's `ci_watch.go` note is the tree-lock own-commit pin) | CI-watch completion guardrail: an Automaton's completion is gated on the CI result of its own commit (how-to `docs/howto/ci-watch-guardrail.md`, live-verified 2026-10-13) \| done, shipped in v9.0.63 |
| 2026-10-14-cli-self-update-dialog-recovery.md | c | Investigation only — its own status line: "Status: investigation only … No non-test Go code was changed": root-cause analysis of re-pasted task text in OneShot claude-code sessions (the heartbeat re-delivery, not the self-update screen); no plan, no code change, no registry row | |
| 2026-10-14-verification-llm-override-live.md | b | Live end-to-end proof (story TS-796) of the per-task, per-story and per-Automaton verification LLM override, shipped in the v9.0.67 to v9.0.73 line: overridden task judged by the override backend (`verifier_source: "task"`), sibling by the fallback. No registry row names this feature (row 63 BL482 is LLM overrides on a blocked run; row 168 BL585 is the later flexible verifier routing) | Per-task, per-story and per-Automaton verification LLM override: the verifier is chosen per task, story or Automaton instead of a single global setting (live end-to-end proof 2026-10-14) \| done, shipped in v9.0.73 |
| 2026-10-20-bl463-mcp-drop-findings.md | a | The connection-drop findings and mitigations behind row 5 (BL463, MCP connection reliability, session isolation, error-mode parity; shipped v9.5.29 with connection-drop mitigations and transport observability) | |
| 2026-10-20-oneshot-startup-dialog-dismiss.md | a | TDD-red design for dismissing OneShot claude-code startup dialogs; row 34 (BL450, startup-dialog auto-dismiss for one-shot tasks) is done, shipped 9.5.36, and its record `b0390001` is this file's own worktree | |
| 2026-10-20-plan-coverage-gate.md | a | The deterministic spec-to-plan coverage gate; row 78 (BL497, spec-to-plan coverage check after every decompose, deterministic, no LLM) is completed and shipped in v9.5.32 | |
| 2026-10-20-tag-browsing-tile-design.md | a | The tag-based browsing and tile redesign design (v9.5.28, ten design questions); row 36 (BL461, tag-based browsing and tile redesign design) is shipped 9.19.0 and answers those questions | |
| 2026-10-20-voice-long-recording-rootcause.md | a | Root-cause analysis of voice transcription timing out on long recordings; row 33 (BL448) is done and shipped in an earlier 9.5.x (flat 15 s upload ReadTimeout) | |
| 2026-10-21-datawatch-app-lifecycle-subscriptions-note.md | a | App-side parity/localization note for the MCP resource-subscription feature; row 11 (BL427 Phase 3, resource subscriptions) is shipped | |
| 2026-10-21-pwa-connection-stability.md | a | Research input for the unstable-on-5G-over-Tailscale PWA; row 130 (BL549, P0 PWA stability) is done, shipped 9.5.62 (jittered reconnect backoff, update banner instead of reload, incremental resync) | |
| 2026-10-22-observer-mobile-fit.md | a | The five-phase Observer mobile-fit plan, all phases Done; row 153 (BL572, Observer tab System Statistics mobile fit) is shipped 9.14.2 with the mobile-fit checker and guard stories | |
| 2026-10-23-datawatch-app-gate-scope-note.md | a | App-side note for the quality-gate scope feature; row 271 (registry audit, record `fdc50001`) is done, merged and shipped in 9.47.0 | |
| 2026-10-23-datawatch-app-notify-events-note.md | a | App-side note for the per-Automaton `notify_events` feature; row 178 (BL596) is shipped 9.16.0 (per-Automaton over profile over global, five-kind default set) | |
| 2026-10-23-datawatch-app-tags-note.md | a | App-side note for nested tags and tag browsing; rows 35 (BL393 nested tags, shipped 9.5.105) and 36 (BL461 tag-based browsing design, shipped 9.19.0) cover it | |
| 2026-10-23-lifecycle-outbound-webhook-and-forwarding-proposal.md | a | The webhook-sink and federation-forwarding proposal; row 23 (BL427 Phase 5, webhook sink and federation forwarding) is shipped 9.17.0 | |
| 2026-10-23-quality-gate-scope.md | a | The quality-gate scope proposal (touched packages plus a smoke set, setting in YAML with all surfaces); row 271 (registry audit, record `fdc50001`) is done, shipped 9.47.0 | |
| 2026-10-23-session-store-write-amplification.md | a | The seven-phase plan for the session-store write amplification; row 164 (BL580, P0 daemon CPU peg and slow API calls from whole-file store rewrites) is done, shipped 9.5.121 | |
| 2026-10-24-datawatch-app-capacity-node-usage-note.md | a | App-side localization note (12 new locale keys) for the capacity node-usage feature; row 187 (BL604, live usage per compute node) is shipped 9.13.0 | |
| 2026-10-25-live-held-task-review.md | a | The fix-and-review plan for held `review_needed` tasks that cannot be resolved while the Automaton runs; row 200 (BL613, P1) is shipped 9.17.4 (five-second executor-slot wait and its error removed) | |
| 2026-10-25-restart-during-launch.md | a | The restart-during-launch fix and restart-matrix state; row 183 (BL600, P0) is shipped 9.16.1 (adopt only agent-started sessions, start-confirm bound on adopted sessions, the restart-during-launch matrix state) | |

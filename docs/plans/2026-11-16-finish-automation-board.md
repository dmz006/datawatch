# Finish-automation board

Generated 2026-11-16 20:18 from the daemon's own data (merge state and the Automata list) by `python3 scripts/automation-board.py`. It is the one place that lists everything still unfinished; the supervisor regenerates it at each full tick.

## Rules that keep it organized

1. An Automaton is finished only when its work is on main (landing `landed`) or the operator has recorded `kept` or `dropped` with a reason. Archived is not finished.
2. Before filing a new defect or Automaton, run the overlap check; if an Automaton already describes it, land or finish that one instead.
3. Every merge goes through the chain scripts and is recorded shipped only when its branch is on main.
4. The board is regenerated at each hourly tick and any change is committed with the registry row.

## A. Finished work with commits that never reached main (46)

State `pending` = commits ahead of main, not merged. `needs_look` = diverged or odd, a person must look. The recommended action is the default; keep and drop need the operator's reason.

| State | Commits | Id | Title | Archived | Recommended |
|---|---:|---|---|---|---|
| pending | 111 | `72bc0001` | Installable packs for standard tasks and Automaton templates | yes | merge through the chain |
| pending | 104 | `7bd50001` | Git hosts: settings, readiness, GitLab, release, CI watch and tags | no | merge through the chain |
| pending | 49 | `72d10109` | Pi stall reporting, fast failure and node preflight | yes | merge through the chain |
| pending | 44 | `72d1017e` | Finalise session records whose process is gone and make every session  | yes | merge through the chain |
| pending | 40 | `31e40001` | Format all Go files with gofmt | no | merge through the chain |
| pending | 40 | `90d60001` | Deploy after ship: a configured post-ship build, install, restart and  | yes | merge through the chain |
| pending | 37 | `1d470001` | Compact header: server and project/plan scope icons with modals | yes | merge through the chain |
| pending | 36 | `9f830001` | Automation parallel chains, concurrency limit, decision-wait and stage | yes | merge through the chain |
| pending | 33 | `13f00001` | Send an empty system_tags array on summary rows and legacy list record | yes | merge through the chain |
| pending | 32 | `f79e0001` | Verification-script pre-flight and fixed-date decompose lint | no | merge through the chain |
| pending | 31 | `d0460001` | Lane fill frees lanes held by idle Automata | yes | merge through the chain |
| pending | 29 | `3a640001` | Per-LLM and per-model usage hold and burn guard | yes | merge through the chain |
| pending | 29 | `3e5c0001` | Fast Projects and Automata pages | no | merge through the chain |
| pending | 28 | `b20f0001` | Verifier cap above 1 requires a measured headroom check | yes | merge through the chain |
| pending | 26 | `18980001` | Actionable Automata Inbox | no | merge through the chain |
| pending | 23 | `6eac0001` | Sessions toolbar fixes: closable Filters panel and icon view buttons | yes | merge through the chain |
| pending | 23 | `e5700001` | Core automation stability 2: archive only merged work | yes | merge through the chain |
| pending | 21 | `2b340001` | Route each task to a lane at admission | yes | merge through the chain |
| pending | 20 | `41050001` | Core automation stability 3: planner avoids foreseeable task and check | yes | merge through the chain |
| pending | 20 | `8b7a0001` | Core automation stability 1: automatic gate repair | yes | merge through the chain |
| pending | 18 | `6e980001` | Automata Resources, Progress and costs, Housekeeping in datawatch styl | yes | merge through the chain |
| pending | 16 | `4e190001` | Deflake round five: await question raise notifications before temp dir | yes | merge through the chain |
| pending | 16 | `70900001` | Configuration parity audit and CI gate | no | merge through the chain |
| pending | 16 | `71660001` | Plan status roll-up and plan rename/edit | yes | merge through the chain |
| pending | 15 | `a1a30001` | Trace retention follows the session | yes | merge through the chain |
| pending | 14 | `14310001` | Stale browser state cannot break the Sessions view-mode buttons | no | merge through the chain |
| pending | 13 | `26720001` | Stop the tracker writing RTK instructions into rules files | yes | merge through the chain |
| pending | 13 | `4f450001` | Archive-based trivy scan for the agent-base build and scan gate | yes | merge through the chain |
| pending | 12 | `33160001` | Consistent phone page layout across all views | yes | merge through the chain |
| pending | 4 | `d5060001` | Make the internal/autonomous race-detector run pass | no | merge through the chain |
| pending | 1 | `718a0001` | Measured headroom check for a verifier cap above 1 | yes | merge through the chain |
| pending | 1 | `d2f10001` | Shadow merge and comparison for daemon-run Automaton merges | yes | merge through the chain |
| needs_look | 16 | `e2120001` | e2120001 | yes | review the diff, then merge or drop |
| needs_look | 14 | `a8500001` | Flag undeclared file changes in Automata tasks: per-task visible warni | yes | review the diff, then merge or drop |
| needs_look | 8 | `91dc0001` | Fix the default PRD quality gate so it runs, validates its baseline an | yes | review the diff, then merge or drop |
| needs_look | 8 | `928f0001` | Voice transcription long-recording failure: root-cause fix + streaming | yes | review the diff, then merge or drop |
| needs_look | 8 | `bb590001` | Automatic spec-to-plan coverage check after every decompose | yes | review the diff, then merge or drop |
| needs_look | 7 | `06120001` | Sonnet plan review before approval | yes | review the diff, then merge or drop |
| needs_look | 6 | `0631deb1` | Runs leave no debris: clean tests, harness self-cleanup, safe auto-com | yes | review the diff, then merge or drop |
| needs_look | 5 | `4ce40001` | Refuse LLM settings changes while a plan is running | yes | review the diff, then merge or drop |
| needs_look | 5 | `a354e98a` | Decompose: resolve every depends_on entry at store time and warn about | yes | review the diff, then merge or drop |
| needs_look | 5 | `cece0001` | Fix PWA /api/schedules request flood | yes | review the diff, then merge or drop |
| merged | 4 | `6b3b0001` | Treat every -prompt backend as one-shot for hand-started sessions | yes | review the diff, then merge or drop |
| needs_look | 4 | `c2b60001` | Fix long-recording voice transcription and design streaming long-form  | yes | review the diff, then merge or drop |
| needs_look | 4 | `f56e0001` | Close the documentation gaps for the shipped lifecycle-event notificat | yes | review the diff, then merge or drop |
| needs_look | 2 | `dc6c597b` | Ship v9.1.9: autonomous task session-name prefix closeout | yes | review the diff, then merge or drop |

## B. Failed or blocked with work left (10)

| Id | Status | Tasks left | Title | Why |
|---|---|---:|---|---|
| `38b69678` | blocked | 35 | [eval] model-tiering 20261015-r1 | last activity was 552764 seconds ago (153h32m44s), older tha |
| `71f80001` | blocked | 27 | Merge scripts in the repository with shadow pipeline compariso | task 6a880002 needs operator review |
| `9a1a8666` | blocked | 16 | [eval] open-model-matrix 20261015-r7 |  |
| `3be9c0e5` | blocked | 16 | [eval] open-model-matrix 20261015-r10 |  |
| `d5060001` | blocked | 7 | Make the internal/autonomous race-detector run pass | task 1ff40003 needs operator review |
| `70900001` | blocked | 4 | Configuration parity audit and CI gate |  |
| `049f4713` | blocked | 3 | BL433 — PWA performance audit under load (design/audit pass) |  |
| `f6a38c29` | blocked | 2 | BL422 — Claude model/effort tiering eval for dev-Automaton tas | last activity was 575902 seconds ago (159h58m22s), older tha |
| `fea73c82` | blocked | 1 | BL437 — Recurring, hardware-aware LLM model/effort evaluation  | last activity was 570131 seconds ago (158h22m11s), older tha |
| `9f8f0001` | blocked | 0 | Move Automata configuration into Settings and replace the usag | guardrail rules-review returned block (text-only judge) - ju |

## C. Waiting for review, approval or planning (18)

| Id | Status | Tasks | Title |
|---|---|---:|---|
| `12480001` | running | 22 |  |
| `5c0c0001` | running | 17 | Remove the CPU burner and fix intermittent tests at their root |
| `d1130001` | running | 15 | Free orphaned pi-pool slots held by judges and verifiers |
| `3b170001` | needs_review | 9 | Research: pack workflow graphs — grounded answers, first step, |
| `316c41e1` | needs_review | 8 | Register the individual-builder harness research and its enhan |
| `87620001` | running | 7 | Make the full release smoke pass on main |
| `2fbf0001` | needs_review | 7 | Authentication and multi-user design proposal |
| `36729dd7` | needs_review | 7 | Register the openrig.dev adoption research as a held item |
| `2c9a27e2` | needs_review | 7 | Personal agents with personality and decision-making (held pla |
| `3e500001` | running | 3 |  |
| `ff540001` | needs_review | 0 | Fix the shutdowncause child process scan flake in the full tes |
| `a7f20001` | needs_review | 0 | Fix open high and critical CodeQL alerts |
| `f31d0001` | draft | 0 | Fix the shadows: plan size and version, changelog source, stor |
| `f79e0001` | needs_review | 0 | Verification-script pre-flight and fixed-date decompose lint |
| `31e40001` | needs_review | 0 | Format all Go files with gofmt |
| `18980001` | needs_review | 0 | Actionable Automata Inbox |
| `7bd50001` | needs_review | 0 | Git hosts: settings, readiness, GitLab, release, CI watch and  |
| `14310001` | needs_review | 0 | Stale browser state cannot break the Sessions view-mode button |

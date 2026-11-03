# datawatch-app parity backfill: audit of 2026-10-27

Operator, 2026-10-27, quoted: "Parity is required, no design there, it is a core rule" and "Have all past 9.0 until now changes had parity app issues filed?"

## Result

Between v9.0.0 and v9.23.1 there are 68 releases whose diff touches the PWA (`internal/server/web/app.js`, `index.html`, `locales`, `style.css`). Only 10 of them are named by version in a datawatch-app issue; 58 are not. 41 of those 58 added locale keys (719 keys in total). A version-string match is strict, so some of them may be covered by differently worded issues (for example tag browsing is covered by issues 273 to 275, lifecycle sinks by 265); the backfill must read the existing issues before filing.

## How it got missed

- AGENT.md has the Mobile-Parity Rule and a checklist box, but nothing enforced it: no gate in CI, no check in decompose validation, no step in the supervisor merge procedure, and the final judge could only note the parity issue as unverifiable in the tree.
- It was carried only by individual Automaton specs. Of 105 stored Automata whose tasks touch the web UI, 46 mention datawatch-app anywhere in their specs; 59 do not. A mention in a spec is not proof that an issue was filed, and some specs tell the worker not to push or create external issues, so a worker could not file it.
- The release plan had the parity issues as part of the final close-out (BL496) and only "for every new setting", narrower than the rule (strings, layout, behaviour, API shape, affordances), so layout and behaviour changes had no owner until then.
- From 9.5.x on the build produced a release every few minutes; with no per-merge step the issues were filed by hand when someone remembered (1 to 3 a day).

## Fix going forward

The supervisor files a datawatch-app issue at every merge that changes the PWA (supervisor rules, merge procedure step 6, 2026-10-27). BL635 adds a decompose check, and a task for the parity issue should be part of any Automaton that touches the web UI.

## Releases to backfill (no datawatch-app issue names the version)

| Release | New en.json keys | Title |
|---|---|---|
| v9.0.3 | 0 | fix: orphaned-tmux PWA card not federation-aware + real test leak |
| v9.0.5 | 0 | chore(release): suppress 5 no-fix-available CVEs blocking v9.0.3's agent-base scan |
| v9.0.62 | 24 | fix(autonomous): recover a blocked task in place — editable spec while blocked, system-binary-aware scope lint |
| v9.0.63 | 2 | feat(autonomous): ci-watch completion guardrail — an Automaton waits for the CI of the commit it pushed |
| v9.0.75 | 8 | feat(autonomous): foreign-file-drift detection — flag a task whose commit swept in someone else's uncommitted  |
| v9.5.32 | 29 | autonomous: a deterministic, no-LLM plan requirement coverage gate at decompose time, with an operator acknowl |
| v9.5.33 | 0 | Retried tasks keep their first-attempt pre-task SHA |
| v9.5.40 | 0 | Git is now opt-in for sessions |
| v9.5.47 | 2 | Daemon restart no longer revives stale running Automata |
| v9.5.52 | 13 | Sonnet plan review before approval |
| v9.5.54 | 3 | Undeclared-file warning on completed tasks |
| v9.5.57 | 0 | PWA terminal no longer stalls while a session is live |
| v9.5.59 | 0 | PWA shows the datawatch eye during full-page refreshes |
| v9.5.62 | 0 | PWA stays connected on cellular: stable reconnect, safe reload, incremental resync |
| v9.5.65 | 1 | Automation coordinator: the queue advances itself |
| v9.5.67 | 2 | Boot-resume counts live workers; parked Automata can be resumed |
| v9.5.68 | 8 | Inference-cap gap closure: plan-tier caps, verifier lease hand-off, decompose admission |
| v9.5.70 | 2 | Idle claude-code workers are nudged to finish before they are treated as stalled |
| v9.5.71 | 59 | Lifecycle subscriptions: update, test-fire, pending-events queue and PWA tab |
| v9.5.73 | 0 | The PWA terminal no longer throws when xterm is used after dispose |
| v9.5.75 | 13 | Result panel for scheduled jobs: MCP, CLI and comm-channel parity |
| v9.5.82 | 5 | Pinned models on GPU nodes are protected from silent eviction |
| v9.5.88 | 2 | Reactive usage back-off for rate-limited backends |
| v9.5.90 | 20 | Per-LLM dollar budget with a windowed ledger |
| v9.5.92 | 1 | Automation model-tier defaults for execution, verification and planning |
| v9.5.96 | 2 | Claude start-up confirmation dialogs answered or failed fast |
| v9.5.97 | 146 | Automation tab in the web UI |
| v9.5.99 | 6 | The quality gate runs through a shell and compares by test name |
| v9.5.101 | 2 | Task delivery waits for slow-starting Claude sessions |
| v9.5.103 | 8 | Federated daemons detect a shared compute node or billing account by hashed fingerprint |
| v9.5.105 | 32 | Automata tags: a nested tag tree to organise Automata |
| v9.5.106 | 7 | Automation close-out: import-graph, parity audit and deprecation of the old graph tools |
| v9.5.107 | 10 | Role tags on LLM entries and session priority in admission |
| v9.5.108 | 2 | The capacity view names what is running and waiting |
| v9.5.109 | 11 | Fast web UI loading with summary lists and loading states |
| v9.5.112 | 23 | Optional local-model decision assistant for Automations |
| v9.5.114 | 6 | Capacity rate ceilings and a non-blocking admission query |
| v9.5.117 | 3 | Verifier watchdog, fallback verifier and a lease reaper that spares live verifiers |
| v9.5.121 | 8 | Per-record session and PRD stores |
| v9.6.1 | 0 | Paged Automata list and small reconnect refresh |
| v9.7.0 | 0 | Automation is the first sub-tab of the Automata tab |
| v9.8.0 | 80 | Progress dashboard for the Automata |
| v9.9.0 | 0 | Bounded websocket session list with per-session updates |
| v9.10.3 | 0 | Status cards only on the Automation sub-tab |
| v9.12.0 | 40 | Failure escalation policy for repeated task failures |
| v9.13.0 | 12 | Compute node usage on the capacity card |
| v9.14.0 | 0 | imap_mcp channel config parity |
| v9.14.2 | 0 | Observer tab fits phone, tablet and desktop |
| v9.15.0 | 1 | Pi workers and judges balanced across the GPU nodes |
| v9.16.0 | 15 | Per-Automaton notify events |
| v9.16.2 | 0 | Shared navigation mobile fit |
| v9.17.0 | 38 | Lifecycle events to a webhook or a federation peer |
| v9.18.0 | 4 | Judge routing policy and time limit |
| v9.19.0 | 66 | Tag browsing and the shared tile for Automata |
| v9.20.0 | 2 | Project directories must lie under an allowed root |
| v9.21.1 | 0 | Open CodeQL alerts are fixed in code: command injection, path containment, reflected output and more |
| v9.21.2 | 1 | A restart keeps each pi worker on the node it was placed on |
| v9.21.3 | 0 | The workspace root is an allowed project directory by default |

## Method (rerun)

List `v9.*` tags in version order; for each consecutive pair run `git diff --name-only` over the PWA files above; fetch all datawatch-app issues with `gh issue list -R dmz006/datawatch-app --state all --limit 500 --json number,title,body`; a release is covered when an issue title or body contains its version; new keys are the keys in `locales/en.json` at the tag that are absent at the previous tag.

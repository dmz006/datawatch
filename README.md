# datawatch

<p align="center"><img src="internal/server/web/icon-512.svg" width="180" alt="datawatch logo"/></p>

**A distributed control plane for orchestrating AI work — recursive, episodic, secure, and structured across hosts, clusters, and channels.**

[![License: Polyform NC](https://img.shields.io/badge/license-Polyform%20NC%201.0-blue)](LICENSE)
[![Go version](https://img.shields.io/badge/go-1.24%2B-00ADD8)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20WSL2-lightgrey)](docs/setup.md)
[![Release](https://img.shields.io/badge/release-v9.0.46-success)](CHANGELOG.md)

`datawatch` is a single-binary control plane that runs, remembers, plans, attests, and **debates** AI work — local sessions, ephemeral container workers, persistent memory, and the messaging fabric that ties them together — under one operator with one set of lifecycle, audit, and security guarantees.

It started as a daemon that bridged Signal/Telegram to AI coding sessions running in tmux. It's now a full compute abstraction layer with capability-based federation access control, multi-mode compute-node routing, a multi-server proxy surface, and full PAI-parity personal AI infrastructure — structured identity, multi-phase reasoning, rubric-based grading, and multi-persona debate — all mirrored across 7 interchangeable surfaces.

<p align="center"><img src="docs/tour.gif" width="300" alt="datawatch web UI tour"/></p>

---

## 📱 [Android — phone, auto, and wearables unified](https://github.com/dmz006/datawatch-app)

**datawatch is now on your wrist, dashboard, and pocket.** Compose Multiplatform with full 7-surface parity across Android, Android Auto, and Wear OS — same capabilities, native to each form factor.

- **Android Auto** — hands-free voice commands via "Hey Google, ask DataWatch for status," live session/vitals monitoring on the infotainment display, quick-decision buttons, urgent push notifications. Three distraction-safe screens.
- **Wear OS** — Automata queue dashboard, **private local voice** (audio goes straight to your own server, transcribed by your own Whisper instance — no Google, no cloud STT), tile shortcuts, ambient progress, health-data cross-reference.
- **Android phone** — full session and Automata orchestration parity with desktop (REST/MCP/CLI), discussion scopes, file uploads, offline queue with auto-sync, bidirectional push replies.

> [![Join the beta on Google Play](https://img.shields.io/badge/Google%20Play-Join%20Beta-4285F4?style=for-the-badge&logo=google-play&logoColor=white)](https://groups.google.com/g/datawatch-testers)
>
> **Android is in Google Play closed testing, currently pending Google's app review.** Google requires 12 testers opted in for 14 continuous days before production access. **The opt-in/install links below may not load the app yet** if the review hasn't cleared — if a link 404s or shows nothing to install, the app isn't visible to new testers yet; try again later or watch `dmz006/datawatch-app`'s CHANGELOG for the review-cleared announcement.
>
> 1. [Join the tester group](https://groups.google.com/g/datawatch-testers) with the same Google account your phone uses for Play
> 2. [Opt in on Google Play](https://play.google.com/apps/testing/com.dmzs.datawatchclient)
> 3. [Install from Google Play](https://play.google.com/store/apps/details?id=com.dmzs.datawatchclient) (the listing appears after you opt in, once Google's review clears) and stay opted in for 14 days — **we need 12 testers.**
>
> **Wear OS** is in the same closed test. Once you've opted in, open Play Store on the watch and search **datawatch**, or install it from play.google.com in a browser and pick the watch. Android Auto is part of the phone app.
>
> **iOS:** [public TestFlight beta](https://testflight.apple.com/join/qta87V4Q) — install
> TestFlight from the App Store, open the link on your iPhone/iPad, tap
> **Accept**, then **Install**. Feedback: open an issue in
> [`dmz006/datawatch-app`](https://github.com/dmz006/datawatch-app).

---

## 🎉 Community skills + plugins registry

**[`dmz006/datawatch-community`](https://github.com/dmz006/datawatch-community)** is the official hub for sharing datawatch Skills and Plugins — autonomous session patterns, multi-agent topologies, inter-agent proposal pipelines, and more, contributed by operators and installable in one command:

```bash
datawatch skills registry connect https://github.com/dmz006/datawatch-community
datawatch skills sync community
```

Browse the catalog in-app (Settings → Skills → Registry) or on GitHub. To contribute: fork, add your Skill or Plugin directory, open a PR — see [`CONTRIBUTING.md`](https://github.com/dmz006/datawatch-community/blob/main/CONTRIBUTING.md).

---

## Current release

**Incoming (not yet released):** the operator-question channel step of the 2026-10-25 decision-channel plan — an Automaton can ask the operator a question over REST with numbered options and a default, you answer and it is recorded immutably with its verbatim quote (a changed mind supersedes, never edits), an answer that looks like a secret is refused `422` with the two safe routes named, every state change is an audit event, and the whole channel is off by default behind `autonomous.questions.enabled`, which the PWA Settings toggle (in all five locales), MCP `config_set`, `datawatch config set` and comm `configure` all set without a restart. The mobile queue view and answer sheet are tracked in datawatch-app#283.

**Current release: v9.29.0 (2026-10-28).** The usage and cost calculator models the session meter and the shared weekly ceiling from operator readings, with advice for three usage policies on every surface (REST, CLI, comm channel, MCP, PWA) in v9.29.0; the prior release was **v9.28.7**: The CI test timeout is raised to 12 minutes. **v9.28.6**: Opening a record store removes only stale temporary files, so a concurrent save is never lost; this was the cause of a flaky CI watch test. **v9.28.5**: The MCP debug transport escapes declared stream bytes and sets nosniff, and PWA dictation ids come from a page counter instead of Math.random; subprocess sessions keep running the caller's command line through a shell. **v9.28.4**: go-jose is updated to 4.1.6, which fixes a HIGH denial of service. **v9.28.3**: The daemon cleanup audit test no longer times out on a slow CI runner. **v9.28.2**: Placement and adoption tests use the live configuration shape, with a live story, a smoke section and a post-deploy node check. **v9.28.1**: Test, story, smoke and sandbox scripts record the daemons they start and end them and their sessions on normal exit, failure and interrupt. **v9.28.0**: The session header no longer shows the time counter or the eye; a Tags dropdown in the page header holds Watched only, tag filters and, for the open session, Watch this session and Edit tags. **v9.27.0**: A triage record for failed or blocked Automata, a poll-time pass in the Automation coordinator, REST and MCP read, a CLI command, a chat verb, an alert and a PWA card. **v9.26.0**: A question store with REST, MCP and CLI access and a settings switch lets an Automaton raise a question and the operator answer it, off by default. **v9.25.3**: A verifier or judge request that waits for a full backend is re-evaluated by the policy and moves to a backend with a free slot. **v9.25.2**: Fixes for the open CodeQL findings verified with a local CodeQL scan: HTML-escaped untyped markup bodies, nosniff headers, path containment and fixed argument vectors. **v9.25.1**: A relaunched, retried or resumed task is admitted through the normal one-engine-per-node placement, and a node over its limit is reported with its lease holders. **v9.25.0**: Plan coverage now flags a write task that declares no files, with a read-only marker and gate exemptions, and the planning prompts ask for files on every write task. **v9.24.0**: The task scope lint no longer flags literal pattern text in a task spec and has an off, warn or enforce setting. **v9.23.2**: The final judge now follows the judge policy chain and the size-aware rule unless a task or Automaton explicitly pins its guardrail backend. **v9.23.1**: Failed and slow voice transcriptions are logged with the real cause, long recordings are split into chunks, and a failed recording stays in the page with a retry. **v9.23.0**: Scope warnings, guardrail warnings and session start and clear events are opt-in lifecycle event kinds for subscriptions, off unless listed. **v9.22.0**: A diff above a changed-line threshold (default 400, 0 = off) tries Claude first for the final judge, with the routing rule shown in the decision log. **v9.21.3**: Session start, PRDs with an explicit project directory and the test runner accept directories under the workspace root as well as the PRD worktree base. **v9.21.2**: Surviving pi workers keep their compute node across a daemon restart, so one GPU no longer sits idle after a deploy. **v9.21.1**: Fixes for the open CodeQL findings: fixed argument vectors, one checked path-containment helper, safer response writing, bounded allocation and certificate check, and test-only alerts dismissed with reasons. **v9.21.0**: A startup warning, an acknowledge setting and a per-listener posture in health and diagnose for listeners reachable without authentication. **v9.20.0**: Project directories from requests are contained to the allowed roots; the CodeQL alerts are triaged. **v9.19.6**: A pi backend reads its candidate nodes from its own LLM entry, least loaded first. **v9.19.5**: The compute_nodes list now decides where local-model work is placed, least loaded node first. **v9.19.4**: Adopted pi sessions take a node lease again and waiting work is re-placed when the node list changes. **v9.19.3**: Changing the supervisor watch script no longer needs a version bump. **v9.19.2**: A cheap supervisor watch script for the 20 minute check. **v9.19.1**: A queued pi task moves to a node that has a free engine. **v9.19.0**: Automata tag browsing with a shared tile, an attention group and a federated tag registry. **v9.18.0**: Judges follow the verifier routing policy with a time limit and one attempt per backend. **v9.17.6** (2026-10-23): pi-prompt workers take a node lease and are placed on the least loaded healthy node. **v9.17.5** (2026-10-23): Launched and adopted workers share one start tracker. **v9.17.4** (2026-10-23): A held task can be retried or cancelled while its Automaton is still running other tasks. **v9.17.3** (2026-10-23): A worker is refused, with an alert, when its project directory is not a registered worktree. **v9.17.2** (2026-10-23): Local-model pi-prompt workers running in a node-hosted runner are recognised as alive. **v9.17.1** (2026-10-23): Idle claude-code workers at the empty prompt are nudged after their turn. **v9.17.0** (2026-10-23): Automaton lifecycle events can go to a signed webhook or a federation peer (both ship disabled). **v9.16.2** (2026-10-23): Shared navigation and cross-tab elements fit phone screens. **v9.16.1** (2026-10-23): A restart during a session launch no longer leaves a dead worker adopted as alive. **v9.16.0** (2026-10-23): Each Automaton can now choose which lifecycle events notify, overriding the profile and the global default. **v9.15.0** (2026-10-23): Workers and judges now run on the compute node whose slot they hold, so both GPUs are used, one engine per GPU. **v9.14.3** (2026-10-23): The verifier no-progress watchdog now counts a live runner or a held slot as progress and defaults to 900 seconds. **v9.14.2** (2026-10-23): The Observer tab now fits narrow screens, with a mobile-fit checker and guard stories. **v9.14.1** (2026-10-23): Idle claude-code workers at an empty prompt are now nudged and retried; only real work children count. **v9.14.0** (2026-10-23): The imap_mcp channel can now be configured from the REST config, the config patch and the PWA settings page. **v9.13.0** (2026-10-23): The capacity card now shows loaded model, memory, slots and GPU use for each compute node. **v9.12.0** (2026-10-23): Tasks that fail repeatedly now escalate through a configurable worker policy and then wait for operator review with a summary of every attempt. **v9.11.1** (2026-10-23): The session start detector measures from the last evidence of life, so long silent first actions and restarts no longer end healthy workers. **v9.11.0** (2026-10-23): The Automation coordinator now plans lane fill each poll in shadow mode, recording what it would start and starting nothing. **v9.10.4** (2026-10-23): Guardrail and plan-review judges now take capacity leases like verifiers. **v9.10.3** (2026-10-23): The Progress and capacity cards now show only on the Automation sub-tab. **v9.10.2** (2026-10-23): The stall detector now applies only to the opencode family and matches the provider error line, so Claude and pi tasks that mention network errors are no longer ended. **v9.10.1** (2026-10-23): A daemon restart matrix runs the in-flight scenarios in a sandbox and is part of the release smoke. **v9.10.0** (2026-10-23): Plugin widget data endpoints now require the plugin:read and plugin:write federation capabilities (peers must be granted them), previously merged from a finished branch. **v9.9.1** (2026-10-23): The orphan sweep compares session ids in one normalised form, so live Claude workers are no longer ended after five minutes. **v9.9.0** (2026-10-23): The websocket sends only the default view of the session list and one session at a time on a change, so the PWA stays responsive on a slow link. **v9.8.2** (2026-10-23): The internal tracker id check now runs as part of the datawatch development lint and release workflow. **v9.8.1** (2026-10-23): Each session end records who ended it and why in the timeline and the journal, with a guard test against unattributed ends. **v9.8.0** (2026-10-23): The Automata tab shows progress and an estimate of when the rest will be done, and the same numbers are on REST, MCP, CLI and comm. **v9.7.0** (2026-10-23): Automation moved into the Automata tab as its first sub-tab, opening there by default, and the separate Automation page is gone. **v9.6.1** (2026-10-23): The Automata tab loads a paged summary list and the reconnect refresh is one small request, instead of downloading the full lists. **v9.6.0** (2026-10-23): The MCP SSE endpoint can advertise a public URL, so it works behind a reverse proxy (contributed by Cairn Viktor). **v9.5.126** (2026-10-23): A direct-mode PRD with no declared write directories no longer commits automatically, and the other two PRD commit-scope rules are verified with tests. **v9.5.125** (2026-10-23): A restart during verification keeps a worker that had already finished and runs only the verification again. **v9.5.124** (2026-10-23): Verifier backend selection is a policy: prefer-free, balance or fixed over an ordered backend list, with a stronger verifier on retries and a cap on Claude verifier sessions. **v9.5.123** (2026-10-23): The server tests that start sessions use a fake multiplexer and an isolated environment, so a busy host no longer breaks them. **v9.5.122** (2026-10-23): Only the harness commits, after validation, through a per-session commit guard, and automatic commits are refused outside a project. **v9.5.121** (2026-10-23): Sessions and PRDs are stored as individual records written by a coalescing background writer, so the daemon no longer rewrites whole files on every change. **v9.5.120** (2026-10-23): The web UI load scenarios join the PWA runner and release smoke, and a new guide explains list endpoint paging and caching. **v9.5.119** (2026-10-23): The daemon records the signal and sender behind every shutdown and names the previous stop cause at the next start. **v9.5.118** (2026-10-23): A daemon restart no longer leaves a duplicate worker beside the adopted one, and a periodic sweep ends orphaned automation sessions. **v9.5.117** (2026-10-23): A hung verifier is ended after 10 minutes, repeated infra failures fall back to another verifier, and live verifier leases are no longer reaped. **v9.5.116** (2026-10-23): Capacity admission no longer waits on the network or the session store, and a stuck ledger reports itself. **v9.5.115** (2026-10-23): The session list legacy-shape test no longer fails sometimes under load. **v9.5.114** (2026-10-23): Capacity pools can carry request and token rate ceilings, and admission can be queried without waiting. **v9.5.113** (2026-10-23): The release scan is unblocked for one Brotli finding in the agent image. **v9.5.112** (2026-10-23): Automations can use an optional local-model decision assistant at four hook points, with a hard guard and a shadow mode. **v9.5.111** (2026-10-23): The session input audit test no longer fails on temp-directory cleanup: every late writer is stopped before the directory is removed. **v9.5.110** (2026-10-23): Capacity admission no longer deadlocks on the shared-node check. **v9.5.109** (2026-10-23): The web UI loads fast: summary list endpoints with paging and ETag, loading and failed states on every tab, and an Automation empty state. **v9.5.108** (2026-10-21): The capacity view shows PRD and task titles with the short id instead of raw ids. **v9.5.107** (2026-10-21): LLM entries can carry role tags with a contention check, and session priority now reaches capacity admission. **v9.5.106** (2026-10-21): The Automation feature is closed out: import-graph converts an old orchestrator graph, the old graph tools are deprecated, and every Automation operation exists on every surface. **v9.5.105** (2026-10-21): Automata can be organised in a nested tag tree with an include sub-tags switch. **v9.5.104** (2026-10-21): The launch-failure retry test no longer flakes on loaded runners, and a data race in the executor failure path is fixed. **v9.5.103** (2026-10-21): Federated daemons can detect a shared compute node or billing account by hashed fingerprint, with operator confirmation. **v9.5.102** (2026-10-21): The session input audit test no longer flakes on temp-directory cleanup or under the race detector, and the session store races behind it are fixed. **v9.5.101** (2026-10-21): Task delivery now waits up to a configurable three-minute budget for a slow-starting Claude session instead of failing early. **v9.5.100** (2026-10-21): The test suites no longer leave a 500 MB throwaway home behind each run. **v9.5.99** (2026-10-21): The PRD quality gate now runs compound commands through a shell, marks an unusable baseline invalid, and compares by failing test name. **v9.5.98** (2026-10-21): Waiting tasks now say why and for how long, and admission and budget events are audited. **v9.5.97** (2026-10-21): The web UI has an Automation tab: pipeline strip with forks, joins and nesting, controls, and a create and edit form. **v9.5.96** (2026-10-21): A Claude session stuck at a start-up confirmation dialog is now answered or failed fast, and a silent start is retried. **v9.5.95** (2026-10-21): The release scan is no longer blocked by two OpenJDK findings in the signal-cli image (traced unreachable and suppressed). **v9.5.94** (2026-10-21): An Automation or member can carry a spending limit over a window; the coordinator gates on it, pauses and auto-resumes at a known reset, and blocks at an unknown one. **v9.5.93** (2026-10-21): Every -prompt backend now records hand-started sessions as one-shot, so the validation gate, reaper and retention cover them. **v9.5.92** (2026-10-21): An Automation can set default model settings for its members, with an automation verifier source, executor_source on run records, and a separate optional planning pair. **v9.5.91** (2026-10-21): Fixes the release pipeline failure of the previous version, where go.mod listed a directly imported dependency as indirect. **v9.5.90** (2026-10-21): An LLM can carry a dollar budget over a rolling or weekly window; admission waits when the window is full, with an opt-in labelled allocation estimate and opt-in peer counting. **v9.5.89** (2026-10-21): The rate-limit pattern staleness flag now also changes when the claude command line tool version changes, and the version is shown on every status surface. **v9.5.88** (2026-10-21): A rate-limited backend sets a back-off flag that autonomous dispatch waits on, clearing at the parsed reset time or a configurable maximum, with a pattern-drift staleness flag. **v9.5.87** (2026-10-21): A running Automation can be paused, resumed, cancelled, skipped, and have members inserted, removed or reordered on every interface, with a required cancel choice and an agent insert policy. **v9.5.86** (2026-10-21): The pi launch command prints the completion line only when pi exits zero, prints a distinct failure line with the exit status otherwise, and the session shows the failure. **v9.5.85** (2026-10-21): An Automation can fork into parallel branches, join them with a per-join failure rule, and contain other Automations; the coordinator tracks every active branch and survives restarts. **v9.5.84** (2026-10-21): The MCP SSE listener retries a failed bind with backoff for up to mcp.sse_bind_timeout and reports its state in the health output. **v9.5.83** (2026-10-21): Session snapshots and attempt snapshots stage through a guard that skips large files and cache directories, refuses over-limit trees, and cleans interrupted temporary packs. **v9.5.82** (2026-10-21): A compute node can be pinned to one model: collisions warn at startup, requests for another model are refused unless swapping is allowed, and a pin keeper re-pins an evicted model. **v9.5.81** (2026-10-21): A quality-gate regression is re-run once before it is reported and names the failing tests, and the DNS backend tests no longer rely on a rebound port or fixed sleeps. **v9.5.80** (2026-10-21): A claude-code worker whose task was never typed in is now retried with a fresh session or re-sent its task, instead of idling for the whole task. **v9.5.79** (2026-10-21): Test setups use per-test temp directories and the pi backend cleans its task, output and status scratch files when a session ends. **v9.5.78** (2026-10-21): The flaky WebSocket output-scope unsubscribe test now polls the real condition instead of racing the hub. **v9.5.77** (2026-10-21): The boot-resume age gate no longer parks an Automaton whose worker is still running on a task labelled verifying. **v9.5.76** (2026-10-21): Phase 2 first tranche of the container CVE reachability review records reachability traces for 12 accepted container CVE risks, with removal recommendations for the operator. **v9.5.75** (2026-10-21): Scheduled jobs and integrations can post structured results into a per-schedule history, readable over REST, MCP, CLI and the comm channel, with a result panel in the PWA. **v9.5.74** (2026-10-21): The needs-input detector no longer flips a live adopted session on the echoed task instruction, and its timing is injectable so the session tests are deterministic. **v9.5.73** (2026-10-21): The PWA terminal guards every fit, resize and scroll call against a disposed or hidden terminal, and the service-worker-disabled console line is a single quiet info message. **v9.5.72** (2026-10-21): The wrapped tail of the typed launch command is no longer read as a worker completion, so live claude-code workers survive a daemon restart. **v9.5.71** (2026-10-21): Lifecycle event subscriptions can now be edited and test-fired, a pollable pending-events queue serves generic MCP clients, and the PWA Automaton detail has a Subscriptions tab. **v9.5.70** (2026-10-21): An Automaton worker that goes quiet after finishing its turn without a completion marker is nudged once, and only treated as stalled if it stays silent after the nudge. **v9.5.69** (2026-10-21): The OpenAPI summaries no longer show internal tracker tags, so the internal-ref audit passes on main. **v9.5.68** (2026-10-21): Inference admission is now capacity-aware across host, node and per-LLM pools, with plan-tier caps, a verifier that keeps the task slot, and workers that park on external conditions. **v9.5.67** (2026-10-21): The boot-resume age gate no longer parks an Automaton whose worker is still alive, and a parked Automaton can be resumed from the API, MCP, CLI, chat and PWA. **v9.5.66** (2026-10-21): A daemon restart no longer kills in-flight claude-code PRD workers: after a restart the echoed task text is not mistaken for a completion line, and a live worker with a delivered task is adopted. **v9.5.65** (2026-10-21): The Automation coordinator advances a queue of PRDs on its own with a compare-and-set guard, reconciles after a daemon restart, and adds queued-versus-running status with an idle alert. **v9.5.64** (2026-10-21): Lifecycle-event subscriptions are now stored and managed through REST, MCP, the CLI and the comm channel, with a session-push adapter that can send input to a session. **v9.5.63** (2026-10-21): PRD workers no longer load parent-directory CLAUDE.md files that belong to other sessions: each claude-code launch passes an exclude list, so workers stop stalling on foreign guardrails and task lines. **v9.5.62** (2026-10-21): The PWA on cellular and Tailscale no longer reconnect-storms or reloads under you: reconnect uses jittered backoff with a stability window, a version change shows an update banner instead of reloading while you are active, and resync after a reconnect is one request. **v9.5.61** (2026-10-21): Automatic task and pre-session commits now use the PRD task title as the commit subject instead of the worker prompt text. **v9.5.60** (2026-10-21): A claude-code worker session no longer dies seconds after launch: task delivery is guarded against the completion detector reading the typed task text, and a session that ends before delivery is retried as a launch failure. **v9.5.59** (2026-10-21): The PWA no longer cuts to a white screen when it reloads itself: the animated datawatch eye fills the screen for a short minimum window during refresh transitions. **v9.5.58** (2026-10-21): A new PRD worktree now gets its git-ignored docs index file, so task verification scripts that build the server no longer fail on a missing embed. **v9.5.57** (2026-10-21): The PWA session terminal no longer stalls: the newest screen frame is always drawn, a Jump to live button appears when scrolled up, and a silent view re-subscribes after 30 seconds. **v9.5.56** (2026-10-21): A daemon restart no longer throws away a task whose worker session survived: boot-resume adopts the live in-progress session, or verifies a session that finished while the daemon was down, and only resets a task whose session is gone. **v9.5.55** (2026-10-21): The boot-resume age gate now also covers failed PRDs, so a daemon restart parks old failed PRDs instead of re-launching them. **v9.5.54** (2026-10-21): A task that changes files outside its declared and generated files is now flagged with a visible warning and alert, with an off/warn/block setting on every surface. **v9.5.53** (2026-10-21): The boot-resume gate and API-executor tests drain their executors before temp-dir cleanup, so they no longer flake in CI. **v9.5.52** (2026-10-21): A plan can now be reviewed by a second model before approval (autonomous.plan_review_backend and plan_review_mode), with a re-run action on every surface and settings in the PWA. **v9.5.51** (2026-10-21): The plan-coverage gate no longer reports false unresolved_file findings for Go identifiers, prose lists and runtime files, while genuine missing paths still fail it. **v9.5.50** (2026-10-21): The `agent-aider` image pins certifi above the fixed release with a guard check, and the unfixed libcups2 finding in `parent-full` is an accepted risk with an expiry. **v9.5.49** (2026-10-21): MCP sessions no longer drop mid-conversation and MCP errors now match REST (session isolation is clearer too). **v9.5.48** (2026-10-21): The file-drift guardrail now blocks on pre-existing drift by default and only warns about the worker's own edits, and approving a drift-blocked task works on a blocked PRD. **v9.5.47** (2026-10-21): A daemon restart no longer revives stale running Automata: a new setting, autonomous.resume_max_age_seconds (default 3600, 0 = never, -1 = no limit), parks them with a plain reason instead. **v9.5.46** (2026-10-21): A one-shot pi task that exits silently without a completion or verdict is now detected after the idle limit and retried instead of stalling the PRD. **v9.5.45** (2026-10-21): A decompose whose final save fails now rolls back to draft with a retryable failure instead of getting stuck. **v9.5.44** (2026-10-21): Autonomous settings changed at runtime (verifier, guardrail, planner, stall bound, diff truncation) now apply to live Automata at once, with no daemon restart. **v9.5.43** (2026-10-21): The autonomous executor is free of data races under the race detector, one PRD can no longer be started twice at once, and the two remaining flaky executor tests are deterministic. **v9.5.42** (2026-10-21): The `internal/session` screen-capture tests wait on real conditions instead of sleeping, so they stop flaking under CI load. **v9.5.41** (2026-10-21): The `agent-base` image's `gh` binary is rebuilt with a fixed Go standard library, and a new check fails the build if any Go pin drops below the CVE floor. **v9.5.40** (2026-10-21): Sessions touch git only when you ask: a new `git` field (and `session.git_default`, default off) controls the snapshot and completion commit, which reverses the old always-commit default for one-shot sessions. **v9.5.39** (2026-10-21): The `agent-base` container scan no longer fails the release on the unfixed util-linux CVE-2026-53615. **v9.5.38** (2026-10-21): Automaton guardrail judges now always run on a model you explicitly configured (the PRD or task pin first, then the global guardrail, verification and planning models), and `pi`'s built-in default is never used; when nothing resolves, the judge is not launched and the failure names the keys to set. See [LLM backends](docs/llm-backends.md#judge-and-planning-model-resolution-automata). **v9.5.37** (2026-10-21): A plan whose file-changing tasks lack verification scripts is now sent back to the planner with the offending task names. **v9.5.36** (2026-10-21): One-shot and PRD worker sessions no longer stall on a Claude Code start-up dialog. **v9.5.35** (2026-10-21): PRD worker, verifier and guardrail sessions can no longer reach the production tmux server, the Go toolchain pin moves to 1.26.10 for GO-2026-6809, the autonomous tests no longer flake under load, and a judge that returns issues as objects no longer blocks a PRD. **v9.5.33** (2026-10-20): The PWA no longer floods the daemon with schedule requests when a session view recovers, retried tasks keep their first-attempt pre-task SHA, voice recordings longer than 30 seconds transcribe, and a model that cannot run on a PRD's backend is refused. **v9.5.32** opencode sessions on an Ollama
compute node now start when the model is given without its `ollama/`
prefix. The session gets the Ollama provider settings instead of exiting
at launch. **v9.1.3** fixes `llm test` and ask calls for opencode LLM
entries whose model carries the `ollama/` prefix (they returned 404).

**v9.0.87** The session Status tab now
shows Automaton task detail (current focus, a formatted Automaton card,
verification results, files and base commit, recent activity and cost)
from datawatch's own task data, with no hook payload needed. The Stats tab
now gets a per-session process envelope for running tmux sessions.

**v9.0.86** `autonomous.max_concurrent_tasks`
can now be set daemon-wide (YAML, Settings, REST, MCP, comm, CLI), and an
Automaton without its own setting follows it. The default stays 1 (one task
at a time). `autonomous.max_parallel_tasks`, which never had any effect, is
deprecated: still accepted but ignored, with a warning. It will be removed
in a later release.

**v9.0.85** Automata now publish internal
lifecycle events (Automaton and task status changes, worker session start)
through an in-process event dispatcher. The PWA's live Automata refresh now
runs on top of it, with no visible change. This is groundwork for upcoming
notifications.

**v9.0.84** The three session repos that
grew before v9.0.83 have been shrunk with `scripts/shrink-session-repo.sh`
(31 GB → 3.3 MB for the largest). Evidence is in [Testing](docs/testing.md).

**v9.0.83** Session-tracking folders no
longer keep the live `output.log` in their git history, and the daemon
turns off git's automatic garbage collection in those repos, so a large
session repo can no longer trigger a repack that exhausts host memory
and takes the daemon down. `scripts/shrink-session-repo.sh` shrinks repos
that grew before the fix. See [Session Tracking](docs/session-tracking.md#repository-size--maintenance)
and [v9.0.83](CHANGELOG.md).

**v9.0.82** brought smaller payloads for mobile
and web clients on busy daemons, each one requested by the client: a compact session list
(`GET /api/sessions?view=summary`), gzip for `/api/*` JSON, paged alerts
with a cheap unread-count endpoint and an `alerts_read` WebSocket event,
and a WebSocket `hello` handshake that switches a client to per-session
delta updates and to terminal output only for the sessions it has open.
`GET /api/info` now lists `capabilities` so clients can detect each
feature, and six new `server.*` settings tune the limits and defaults.
See the [low-bandwidth client flow](docs/api/mobile-surface.md#low-bandwidth-client-flow)
and [v9.0.82](CHANGELOG.md).

**v9.0.9–v9.0.75** are a patch
chain on top of the v9.0.0 major release: a plugin extension framework
(manifest-declared widgets, now rendering live on the dashboard and in
session detail — plugins can push or pull their own stat tiles,
tables, lists, sparklines, status badges and markdown cards), and a
pass of fixes to Automata's own verification and guardrail checks so a
configured tool-capable backend actually gets real file/tool access
instead of silently falling back to a plain text-only check. v9.0.62
lets a blocked Automaton's failed task be edited and retried in place,
stops the scope check from blocking specs that merely name an installed
system tool, and makes cancel/delete report exactly which sessions it
stopped. v9.0.63 adds an opt-in **ci-watch** completion check: an
Automaton that pushed a commit waits for that commit's GitHub Actions
runs before it counts as done, and a failing run either blocks it for
an operator decision, gets a corrective task, or is re-run when CI
itself broke — see
[ci-watch-guardrail.md](docs/howto/ci-watch-guardrail.md). v9.0.64
clarifies the ci-watch MCP tool descriptions, and v9.0.65 fixes the
agent container image's security scan by rebuilding its bundled GitHub
CLI with a patched Go and `golang.org/x/net`. v9.0.66 stops one-shot
claude-code workers from getting their task re-pasted on every channel
heartbeat. v9.0.73 lets you choose which LLM verifies a task, a story
or a whole Automaton, overriding the global verifier (task > story >
Automaton > global), from REST, MCP, the CLI, chat and the PWA. Each
verification result records which backend, model and level judged it
— see
[autonomous-planning.md](docs/howto/autonomous-planning.md). v9.0.74
makes an autonomous claude-code task actually run at the effort level
you chose. v9.0.75 flags an Automaton task whose commit swept in
somebody else's uncommitted work (a file that was already modified or
untracked before the task started and was never in its planned files).
It warns by default and can be set to block — see
[file-drift-guardrail.md](docs/howto/file-drift-guardrail.md). None of
these change v9.0.0's actual feature set, described below.

An active Automation (a chain of Automata with next-step edges) now starts
its next Automaton on its own when the current one finishes, and a draft one is
activated from REST, MCP, the CLI or chat — see
[automation-queue-advance.md](docs/howto/automation-queue-advance.md). The
Automation tab in the web app lists, draws and steers them — see
[automation-tab.md](docs/howto/automation-tab.md).

Lane fill keeps the execution lanes busy without a supervisor tick. On every
poll the daemon checks the GPU and Claude lanes against the queue and decides
what each idle lane should start. It runs in shadow mode by default, which
records the decisions and starts nothing, so they can be checked against what
the operator did first; switching to on starts the work through the normal
admission path. An idle lane with eligible work raises an alert. Each Automaton
can prefer a GPU lane, a Claude lane or either. It never merges and never holds
GPU work because of Claude usage limits — see
[automation-lane-fill.md](docs/howto/automation-lane-fill.md).

- **Usage calculator** — type a reading of your weekly provider allowance and get advice on whether you are within budget and what each lane count costs; it only advises and never stops a lane. See [usage-calculator.md](docs/howto/usage-calculator.md).

**v9.0.0 — Memory Lifecycle Complete.** The
first major version since the memory-scope/lifecycle work landed:
per-session and shared memory scopes, seeding/harvest/archive across
PRDs, and Automata memory-stats reporting are now considered stable,
not in-progress. Bundled with this release: the bulk of a multi-week
security hardening sweep (session-scoped credentials replacing the
admin token, SSRF/path-traversal/prototype-pollution/reflected-XSS
fixes, bearer-token leakage and federation-proxy origin-isolation
closures), full HTTP
access/audit logging with SIEM/CEF forwarding, and the Cross-Session
Communication Rule guardrail for spawned sessions. A handful of larger
hardening items (TLS fingerprint pinning, cosign/sigstore release
signing, Helm `securityContext` hardening, an egress allowlist, and
real sandbox isolation for session workers) are deliberately sequenced
as post-9.0.0 follow-on work — each is fully designed, not yet built.
Full detail: [CHANGELOG.md](CHANGELOG.md).

- **[v8.74.0](CHANGELOG.md)** — Added an HTTP access / WebSocket
  lifecycle / auth-failure log — see "Audit & Access Logging" in
  [operations.md](docs/operations.md).
- **[v8.72.0](CHANGELOG.md)** — Three-finger swipe now opens a real
  server-picker overlay (server list + an explicit "Add server" action)
  instead of scrolling to and pulsing the always-visible toolbar bar —
  works even with zero remote servers configured, which the old approach
  never did.
- **[v8.67.0](CHANGELOG.md)** — Cross-host comm-channel `send` and the
  CLI's `--server` flag were both blind to any federation peer added after
  daemon startup (a frozen snapshot from boot, never refreshed from the
  live server registry) — fixed.
- **[v8.62.0](CHANGELOG.md)** — Native ACME/Let's Encrypt HTTP-01,
  live-verified end-to-end against a real public host and a real Let's
  Encrypt directory — 4 real bugs found and fixed in that run.
- **[v8.39.25](CHANGELOG.md)** — Security Design A3: every spawned session
  gets its own scoped credential (not the admin token), closing the last of
  the SEC-002–SEC-014 security hardening sweep (11 real findings fixed:
  SSRF, path traversal, prototype pollution, reflected XSS, bearer-token
  leakage, an origin-isolation gap in the federation-peer PWA proxy). Full
  writeup: [docs/plans/2026-10-03-bl394-security-findings-review.md](docs/plans/2026-10-03-bl394-security-findings-review.md).

### v8.63.0–v8.73.1 highlights

- claude-code and ollama sessions now report real token usage/cost
  (`Session.TokensIn/TokensOut/EstCostUSD` had been zero for every session
  on every backend). The PWA shows the animated datawatch eye for
  content-loading states across 123 cards/panels/sheets, matching the apps
  (GH#186), and council live-run logs render markdown instead of truncated
  plain text (GH#181).
- Native ACME/Let's Encrypt feature-complete: HTTP-01, DNS-01 via
  Cloudflare (wildcards / port-80-unavailable), zero-downtime hot-swap
  renewal, and native APNs push dispatch straight to registered iOS
  devices. See [docs/howto/letsencrypt-acme.md](docs/howto/letsencrypt-acme.md).
- Several real security/config-sync bugs found and fixed: the LLM
  registry leaking a literal API key in clear text (GH#179), a config/
  manager desync where `PUT /api/autonomous/config` and a council LLM
  change didn't take effect until a daemon restart, APNs push dispatch
  mixing up sandbox/production hosts per-device.
- A help-link anchor mismatch fixed across most PWA cards (GH#189), 3
  missing help-link pages, a Community Plugins browse/install card
  (GH#191), and PRD scratch artifacts (`CHECKPOINT.md`,
  `.decompose-output-*.json`) relocated out of the shared project
  repository into a managed per-PRD scratch directory.

### v8.45.0–v8.61.1 highlights (PWA parity sweep, Phases 2–3, now complete)

- Repair-deps button, council-run markdown rendering, memory-scope browser + promote, voice-reply quick commands, "memory promote to" wizard field, watch sessions/automata, terminal search + copy, skeleton loading shimmer, mute-session notifications, splash status line + replay.
- PRD `permission_mode` editor, a persisted session Chrome-enabled badge, pause/resume for a running Automaton (new paused status + cooperative executor drain), Observer Backend Health / Envelopes / quick add-memory cards, an About-screen subsystem reload + MCP channel/tools status cards, a Config Viewer + safe raw editor, and a hand-rolled SVG dependency-graph card for an Automaton's stories/tasks.

See [CHANGELOG.md](CHANGELOG.md) for the complete patch-by-patch history.

### Release eras

- **v9.x** — Memory-lifecycle-complete era: the subprocess scope isolation / lifecycle management / PRD-integration memory trilogy, full Android/iOS parity sweeps, and federation live-store consistency fixes.
- **v8.x** — Federation era: capability-based access control (50 capabilities, 14 built-in groups) gating every REST endpoint and MCP tool, multi-mode Compute Node routing (direct / docker-network / cross-peer proxy), channel routing, federated file service, discussion-scoped shared memory, full operational data encryption, and the Android/Wear/Auto app.
- **v7.x** — Compute abstraction era: Compute Node registry + LLM Registry with automatic failover dispatch, the Ollama Marketplace, Claude Code hooks + live status board, systematic capability enforcement across 110+ endpoints.
- **v6.x and earlier** — PAI-parity era: operator identity, Algorithm Mode's 7-phase reasoning harness, the Evals framework, Council Mode's multi-persona debate, Skill Registries, the native Secrets Manager, Tailscale mesh, and the original Signal-bridge core.

Full detail for any version: [CHANGELOG.md](CHANGELOG.md).

---

## Why a control plane and not a bot

The same profile that drives a chat-spawned session can drive a Kubernetes-deployed worker in a remote cluster, a child agent of an existing worker, a scheduled cron job, a webhook reaction, or a cross-host fan-out — and the operator only ever interacts with one surface: the daemon's REST API. **Every feature is mirrored verbatim across 7 surfaces:** REST, MCP, CLI, Comm channels (Signal/Telegram/Matrix/Slack/Discord/etc.), PWA, mobile (Compose Multiplatform), and YAML on disk.

That uniformity is the whole point. Read once, write once, audit once.

---

## What it does

➡ **[docs/architecture-overview.md](docs/architecture-overview.md)** has the one-screen Mermaid map of every interface, subsystem, and data path. The summary below is intentionally short — each area links to its full howto.

### Orchestration & autonomy

The **Automata PRD-DAG orchestrator** decomposes a goal into a dependency graph of stories and tasks, executes them with quality gates and git-diff-grounded verification, and supports guardrails, rubric-based grading, and structural mid-run editing. Writes are serialized per project tree: at most one task writer per tree at a time (same-tree tasks queue behind an advisory lock with a visible wait reason), while work on different trees — including per-Automaton worktrees — runs in parallel. **Algorithm Mode** is PAI's 7-phase structured-thinking harness (Observe → Orient → Decide → Act → Measure → Learn → Improve) as a per-session state machine. **Council Mode** runs multi-persona structured debate — 6 default personas, each able to use its own LLM backend — with real-time SSE streaming. The **Evals framework** replaces binary pass/fail verification with rubric-based grading.Admission is capacity-aware across host, node and per-LLM pools: an LLM entry's cap resolves from its explicit limit, then its subscription **plan tier** (mapped by `autonomous.plan_tier_max_inflight`), then a conservative default — the verifier keeps the task's own slot (hand-off, never a second one), planning waits behind workers on a saturated pool, and a worker parked on an external condition (CI, a deploy) returns its slot and re-enters the queue on the way back. **Algorithm Mode** is PAI's 7-phase structured-thinking harness (Observe → Orient → Decide → Act → Measure → Learn → Improve) as a per-session state machine. **Council Mode** runs multi-persona structured debate — 6 default personas, each able to use its own LLM backend — with real-time SSE streaming. The **Evals framework** replaces binary pass/fail verification with rubric-based grading.
**Lifecycle event subscriptions** turn the Automaton's event stream into rules you can act on ahead of time: *when this event fires (status change, worker session start, PRD update), deliver it there* — a comm channel, a session (text typed in, bridge-preferred with `SendInput` fallback), a push topic, the alert store, or a pollable pending-events queue for generic MCP clients. Subscriptions can be edited and test-fired, and the PWA's Automaton detail view has a Subscriptions tab to manage them. One-shot subscriptions and the `--task <id> --on session_started --send "…"` shorthand replace the watch-and-inject polling scripts; everything persists across restarts and applies without a daemon restart. Two more sinks send events out of the daemon: a signed HTTPS **webhook** (to an allow-listed host, with the signing secret held in the vault) and **peer forwarding** to a registered federation peer. Both ship disabled until the operator turns them on.
**Autonomous triage** keeps a record for every Automaton that ends failed or blocked: which one it is, when it was first seen in that state, the failed task and its error, the session end reason, and the four retry budgets — and raises one triage alert per episode once the record has sat for `autonomous.automation.triage_alert_minutes` (default 30, 0 = off). Records are read on every surface: REST `GET /api/autonomous/triage`, MCP `autonomous_triage`, CLI `datawatch autonomous triage [--automation <id>] [--prd <id>] [--status failed|blocked]`, comm `autonomous triage [automation-id]`, and a triage card in the PWA's Automaton detail view (wording bundled with the other five locales).
→ [docs/api/autonomous.md](docs/api/autonomous.md) · [docs/api/orchestrator.md](docs/api/orchestrator.md) · [docs/howto/automaton-lifecycle-subscriptions.md](docs/howto/automaton-lifecycle-subscriptions.md) · [docs/howto/automaton-lifecycle-webhooks.md](docs/howto/automaton-lifecycle-webhooks.md) · [docs/flow/lifecycle-subscription-flow.md](docs/flow/lifecycle-subscription-flow.md)

### Compute & LLM routing

Register any host, GPU box, Kubernetes cluster, or remote datawatch peer as a **Compute Node** with declared capacity, RBAC, and scheduling priority. The **LLM Registry** maps named LLM entries to an ordered failover list of nodes; every consumer (sessions, Council, Automata, `/api/ask`) routes through one dispatcher. Three routing modes — direct, docker-managed, or proxied through another datawatch peer. The **Ollama Marketplace** is a browseable, hardware-fit-checked model catalog shipped embedded in the daemon.
→ [docs/howto/compute-nodes.md](docs/howto/compute-nodes.md) · [docs/howto/llm-registry.md](docs/howto/llm-registry.md) · [docs/howto/ollama-marketplace.md](docs/howto/ollama-marketplace.md)

### Memory & intelligence

Vector-indexed episodic memory (SQLite or PostgreSQL+pgvector) with a 5-scope hierarchy — persona-global, persona-in-project, project-shared, session-local, and federated **discussion scopes** shared across peers via an append-only WAL. A temporal knowledge graph with validity-windowed triples, a 6-axis spatial "memory palace" schema, and a 4-layer wake-up stack that auto-injects operator identity and relevant context into every spawned session.
→ [docs/memory.md](docs/memory.md) · [docs/howto/discussion-scopes.md](docs/howto/discussion-scopes.md)

### Federation & multi-instance

**Capability-based access control** — 50 capabilities, 14 built-in groups — gates every REST endpoint and MCP tool for federated peers; no admin-or-nothing. Peers can proxy inference, aggregate sessions, route inbound channel messages to specific peers, and share a federated file service. A remote peer's own PWA can be viewed inline from your dashboard, served from an isolated origin so a compromised peer can never read your session's credentials.
→ [docs/howto/channel-routing.md](docs/howto/channel-routing.md) · [docs/howto/file-service.md](docs/howto/file-service.md)

### Security

Full operational data encryption at rest (XChaCha20-Poly1305) covering config, memory, logs, and every JSON store. A centralized native secrets manager (plus optional KeePass/1Password/Vault backends) with `${secret:name}` references everywhere a credential is needed. Bearer token auth, dual-port TLS, full audit logging, and — as of the current release — a fully reviewed and hardened CodeQL/Dependabot posture.
→ [docs/encryption.md](docs/encryption.md) · [docs/plans/2026-10-03-bl394-security-findings-review.md](docs/plans/2026-10-03-bl394-security-findings-review.md)

### Interfaces & extensibility

A manifest-driven **plugin framework** (hot-reload, declared comm verbs / CLI subcommands / MCP tools / mobile cards) and **Skill Registries** synced from git, including the community hub. **Docs-as-MCP-Interface** makes 22 curated howtos searchable and executable through MCP with a hybrid vector+BM25 index. Claude Code hooks auto-install at session spawn and drive a live status board. An MCP server exposes 60+ tools to Cursor, Claude Desktop, and VS Code.
→ [docs/skills.md](docs/skills.md) · [docs/mcp.md](docs/mcp.md) · [docs/howto/claude-hooks.md](docs/howto/claude-hooks.md) · [docs/howto/docs-as-mcp.md](docs/howto/docs-as-mcp.md)

### Also included

Multi-channel messaging (Signal, Telegram, Discord, Slack, Matrix, Twilio, webhooks, DNS, voice via Whisper) · pluggable LLM backends (claude-code, aider, pi, goose, gemini, opencode, ollama, openwebui, shell) · Docker/Kubernetes container workers with PQC bootstrap · auto rate-limit recovery · eBPF per-process network monitoring + Prometheus `/metrics` · Tailscale mesh for the PWA and agent pods.

See the [Documentation index](#documentation-index) below for everything else.

---

## Installation

### Linux (one-liner)

```bash
curl -fsSL https://raw.githubusercontent.com/dmz006/datawatch/main/install/install.sh | bash
```

Installs to `~/.local/bin` for non-root users, `/usr/local/bin` for root. Includes systemd service.

### From source

```bash
git clone https://github.com/dmz006/datawatch
cd datawatch
go build -o bin/datawatch ./cmd/datawatch
sudo mv bin/datawatch /usr/local/bin/
```

### Update an existing install

```bash
datawatch update && datawatch restart
```

Update is version-string aware. Tmux sessions survive daemon restarts.

---

## Quick start

Three stages, each one a complete, working state on its own — stop after
stage 2 if that's all you need today.

### 1. Install

See [Installation](#installation) above, then initialize + start:

```bash
datawatch config init
datawatch start
curl -ks https://localhost:8443/api/health   # {"status":"ok",...}
```

### 2. Minimal setup — one backend, one session

The fastest path from a fresh install to a real AI session: no messaging
backend, no LLM registry, no MCP server required yet. Pick whichever
coding CLI you already have on your machine — [opencode](https://opencode.ai)
or [Claude Code](https://docs.anthropic.com/en/docs/claude-code):

```bash
# opencode
datawatch config set llm.backends.opencode.enabled true
datawatch config set llm.backends.opencode.path "$(which opencode)"

# — or — claude-code
datawatch config set llm.backends.claude_code.enabled true
datawatch config set llm.backends.claude_code.path "$(which claude)"

datawatch reload
datawatch backends list
#  → opencode | claude-code   ENABLED   reachable

datawatch sessions start --llm opencode --task "Hello, what model are you?" --project-dir /tmp
# (swap --llm claude-code if that's the one you configured)
```

Open `https://localhost:8443` in a browser to watch the session stream
live, or `datawatch sessions tail <id>` from the CLI. This is a
complete, working datawatch install — everything past this point is
additive.

### 3. Full datawatch setup — LLM registry + MCP

Layer on the rest once the minimal path works:

```bash
# Configure your operator identity
datawatch identity configure
# or open the PWA and click the 🤖 robot icon in the header

# Register more backends/hardware in the LLM Registry (ordered
# failover list, not just the one backend from stage 2)
datawatch llm list
datawatch compute node list
datawatch compute pull-model datawatch-ollama llama3.1:8b
datawatch sessions start --llm ollama --model llama3.1:8b --task "Hello"

# Point an MCP host (Claude Desktop / Cursor / VS Code — or a
# datawatch-spawned claude-code/opencode session from stage 2, which
# already has this same MCP server wired in) at the daemon:
#   {"datawatch": {"command": "datawatch", "args": ["mcp"], "env": {"DATAWATCH_TOKEN": "<bearer>"}}}

# (Optional) add a messaging backend
datawatch setup telegram    # or discord / slack / signal
```

Once that MCP host is connected, it can configure datawatch through
plain prompts instead of CLI flags or hand-edited YAML — including
finding out what to do first. Example, verbatim:

> **You:** "What's configured and what isn't? Set up something that's
> missing."
>
> **Claude:** calls `alert_rule_list`, `compute_node_list`,
> `federation_peer_list` — sees two compute nodes and a federation peer,
> but zero alert rules. Calls `docs_search("alert rule")` to confirm the
> schema, then `alert_rule_create` to add one (e.g. GPU usage on your
> RTX node above 90%, cooldown 5 minutes). No restart needed.

Same pattern for anything else: ask what's there, let it read the docs
it needs, let it call the tool. See
docs/howto/setup-and-install.md's MCP section for the fuller
prompt → tool-call reference.

Send `help` in a configured messaging channel to see the command
reference, or see
[docs/howto/setup-and-install.md](docs/howto/setup-and-install.md) for
the full walkthrough of all three stages, and
[docs/howto/chat-and-llm-quickstart.md](docs/howto/chat-and-llm-quickstart.md)
for the most-common chat × backend pairings.

---

## Surface quick reference

Every datawatch feature is reachable from all of these surfaces:

| Surface | Example |
|---|---|
| REST | `curl https://localhost:8443/api/llms` |
| MCP | `llm_list` / `compute_node_list` (via Claude Code / Cursor / VS Code) |
| CLI | `datawatch llm list` / `datawatch compute node list` |
| Comm | `llm list` / `compute node list` (sent in Signal / Telegram / Matrix / etc.) |
| PWA | Settings → Compute → LLM Configuration / Compute Nodes |
| Mobile | Mirrored via Compose Multiplatform app (`dmz006/datawatch-app`) |
| YAML | `~/.datawatch/datawatch.yaml` `compute:` + `llm:` blocks |

The mobile parity rule: every operator-visible PWA change files an issue against `dmz006/datawatch-app` so the Compose pipeline mirrors it.

See [docs/commands.md](docs/commands.md) for the full command reference.

---

## Architecture

➡ **[docs/architecture-overview.md](docs/architecture-overview.md)** — one-screen Mermaid diagram of every interface, subsystem, and data path, with planned features called out.

For deeper drill-downs: [docs/architecture.md](docs/architecture.md) (package list, component diagram, session state machine) · [docs/data-flow.md](docs/data-flow.md) (per-feature sequence diagrams) · [docs/plans/README.md](docs/plans/README.md) (open and planned features tracker).

---

## Documentation index

Full documentation lives in [docs/](docs/) — see [docs/README.md](docs/README.md) for a complete index with all flow diagrams.

| Area | Start here |
|---|---|
| Getting started | [docs/setup.md](docs/setup.md) · [docs/commands.md](docs/commands.md) · [docs/pwa-setup.md](docs/pwa-setup.md) |
| Compute + LLM | [docs/howto/compute-nodes.md](docs/howto/compute-nodes.md) · [docs/howto/llm-registry.md](docs/howto/llm-registry.md) · [docs/howto/ollama-marketplace.md](docs/howto/ollama-marketplace.md) |
| Sessions + hooks | [docs/howto/sessions-deep-dive.md](docs/howto/sessions-deep-dive.md) · [docs/howto/claude-hooks.md](docs/howto/claude-hooks.md) |
| Backends | [docs/llm-backends.md](docs/llm-backends.md) · [docs/messaging-backends.md](docs/messaging-backends.md) |
| Interfaces | [docs/mcp.md](docs/mcp.md) · [docs/howto/mcp-tools.md](docs/howto/mcp-tools.md) · [docs/howto/docs-as-mcp.md](docs/howto/docs-as-mcp.md) · [internal/server/web/openapi.yaml](internal/server/web/openapi.yaml) |
| Autonomous + plugins | [docs/api/autonomous.md](docs/api/autonomous.md) · [docs/howto/automation-tab.md](docs/howto/automation-tab.md) · [docs/howto/automata-progress.md](docs/howto/automata-progress.md) · [docs/howto/automation-lane-fill.md](docs/howto/automation-lane-fill.md) · [docs/howto/automata-tags.md](docs/howto/automata-tags.md) · [docs/howto/automata-tag-registry.md](docs/howto/automata-tag-registry.md) · [docs/flow/tag-registry-sync-flow.md](docs/flow/tag-registry-sync-flow.md) · [docs/api/orchestrator.md](docs/api/orchestrator.md) · [docs/api/plugins.md](docs/api/plugins.md) · [docs/skills.md](docs/skills.md) · [docs/howto/result-panel.md](docs/howto/result-panel.md) |
| Comms + federation | [docs/howto/channel-routing.md](docs/howto/channel-routing.md) · [docs/howto/file-service.md](docs/howto/file-service.md) · [docs/howto/discussion-scopes.md](docs/howto/discussion-scopes.md) · [docs/howto/comm-channels.md](docs/howto/comm-channels.md) |
| Memory & intelligence | [docs/memory.md](docs/memory.md) · [docs/memory-usage-guide.md](docs/memory-usage-guide.md) |
| Operations & security | [docs/operations.md](docs/operations.md) · [docs/config-reference.yaml](docs/config-reference.yaml) · [docs/encryption.md](docs/encryption.md) · [docs/multi-session.md](docs/multi-session.md) · [docs/uninstall.md](docs/uninstall.md) |
| Community | [`dmz006/datawatch-community`](https://github.com/dmz006/datawatch-community) |
| Source attribution | [docs/plan-attribution.md](docs/plan-attribution.md) |

---

## Prerequisites

| Dependency | Version | Notes |
|---|---|---|
| [signal-cli](https://github.com/AsamK/signal-cli) | ≥ 0.13 | Optional — Signal protocol bridge |
| Java | ≥ 17 | Optional — required by signal-cli |
| [tmux](https://github.com/tmux/tmux) | Any recent | Session management |
| [ollama](https://ollama.com) | Any recent | Optional — local LLM inference |
| [claude CLI](https://docs.anthropic.com/en/docs/claude-code) | Latest | Optional — claude-code backend |
| [Tailscale](https://tailscale.com) | Any | Optional — for PWA + mesh |
| Go | 1.24+ | Only required for building from source |

---

## License

Polyform Noncommercial 1.0.0. See [LICENSE](LICENSE).

Commercial licensing inquiries: open an issue.

---

## Acknowledgements

Special thanks to **[Daniel Keys Moran](https://en.wikipedia.org/wiki/Daniel_Keys_Moran)** and his novel
**[The Long Run](https://www.amazon.com/Long-Run-Daniel-Keys-Moran/dp/1939888336)** — the story of Trent
the Uncatchable, a thief and hacker operating under the eye of an all-seeing AI surveillance network, sparked a
decades-long obsession with the intersection of technology, autonomy, and the systems that watch over us.
That spirit lives somewhere in this project.

> *"The DataWatch sees everything."*

If you haven't read it: [buy it on Amazon](https://www.amazon.com/Long-Run-Daniel-Keys-Moran/dp/1939888336)
(Kindle edition also available), or borrow it from the
[Internet Archive](https://archive.org/details/longruntaleofcon0000mora).
Daniel has also historically offered copies by email request via his
[blog](https://danielkeysmoran.blogspot.com).

### Additional Acknowledgements

Datawatch's design also borrows heavily from three projects, with full attribution in [docs/plan-attribution.md](docs/plan-attribution.md):

- **[HackingDave/nightwire](https://github.com/HackingDave/nightwire)** — Signal-driven AI coding bot. Episodic memory + Signal-as-control-plane shape.
- **[milla-jovovich/mempalace](https://github.com/milla-jovovich/mempalace)** — Memory palace metaphor, 4-layer wake-up stack, full 6-axis spatial schema, conversation mining, repair self-check.
- **[danielmiessler/Personal_AI_Infrastructure (PAI)](https://github.com/danielmiessler/Personal_AI_Infrastructure)** — Identity / Telos, Algorithm Mode 7-phase, Skills, Evals, Council, ISA generalization.

---

## Contributing

Issues + PRs welcome. Read [AGENT.md](AGENT.md) for the operating rules — every commit follows the documented Pre-Execution / Versioning / Documentation / Mobile-Parity / Secrets-Store rules.

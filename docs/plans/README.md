# Plans, Bugs & Backlog

Single source of truth for all datawatch project tracking.

---

# Rules

**All operating rules live in [`/AGENT.md`](../../AGENT.md).** This file
holds the project-tracking data only — bugs, plans, backlog items.
Cross-reference highlights:

- Pre-execution + scope — [`AGENT.md` § Pre-Execution Rule, § Scope Constraints](../../AGENT.md)
- Code quality + security — [`AGENT.md` § Code Quality Rules, § Security Rules](../../AGENT.md)
- Testing + audit + memory — [`AGENT.md` § Testing Tracker Rules, § Audit Logging Rule, § Memory Use Rule](../../AGENT.md)

## Folder hygiene rule (operator-directed 2026-05-05)

`docs/plans/` only holds **active** material:

- Dated plan docs (`YYYY-MM-DD-*.md`) **≤ 7 days old**.
- Release notes for the **current minor version line only** (e.g. `RELEASE-NOTES-v6.11.*.md`).

Anything older auto-archives to:

- `historical-plans/` — for stale dated plans.
- `historical-releasenotes/` — for off-minor release notes.

Enforced by `scripts/tidy-plans.sh`. `scripts/release-smoke.sh` runs
`tidy-plans.sh --check` first and aborts the release if the folder needs
tidying. Run `scripts/tidy-plans.sh` (no flags) to perform the moves
and commit before re-running smoke.
- Git + versioning + release cadence — [`AGENT.md` § Git Discipline, § Versioning, § Release vs Patch Discipline](../../AGENT.md) (incl. binary-build cadence: only minor/major releases ship binaries)
- Documentation + tracker discipline — [`AGENT.md` § Documentation Rules, § Project Tracking](../../AGENT.md) (includes "no internal IDs in user-facing UI", "README marquee must reflect current release", "backlog refactor each release", "feature completeness audit", "container maintenance audit")
- Plan attribution — [`docs/plan-attribution.md`](../plan-attribution.md)

If you find a rule that applies to operating behavior duplicated in this file,
move it to AGENT.md and replace it with a cross-reference. AGENT.md is the
single source of truth.

## Current state — 2026-10-13

Latest release: **v9.0.82** (2026-10-14) on `main` — BL432 (GH#242), server payload optimization for mobile/PWA clients: compact session list (`view=summary`), gzip for `/api/*` JSON, alerts paging + `/api/alerts/count` + `alerts_read` WS event, WS `hello` with delta session list and subscribed-only output, `/api/info` capabilities, six new `server.*` settings (feature commits v9.0.76–v9.0.81). Companion: BL433 (GH#243, PWA audit). Previous: **v9.0.75** — BL425 foreign-file-drift detection; **v9.0.66** — BL429. v9.0.29–v9.0.74 history is in `CHANGELOG.md`.

Previous: **v9.0.28** (2026-10-11) on `main`; **v9.0.29** open on `datawatch#205` pending operator merge (BL404, the Stage 2 shakedown of the new PRD-driven workflow — see the Roadmap section below for the full B116-B121/BL413 list of what that shakedown found and fixed). Previous: **v9.0.23**. feat(autonomous): BL407 Phase 5 — config/parity surface. `PRD.Git.AutoPR`/`BaseBranch` are now operator-settable after PRD creation (new `set_git` REST action, `autonomous_prd_set_git` MCP tool, `prd-set-git` CLI command, and a new Git section on the PWA's PRD Settings modal — Mobile-Parity issue `datawatch-app#249`), and a new daemon-wide `autonomous.default_auto_pr` config starts every future PRD's `Git.AutoPR` pre-set (applied once at creation time, not resolved at use-time, since `AutoPR` is a plain bool with no pointer override — a resolve-at-use-time default couldn't tell "operator explicitly turned it back off" from "never touched"). `WorktreeBaseRepo`/`WorktreeDir`/`RulesFile`/`ContextFile`/`UpstreamRepos` were confirmed already fully config-round-trippable with zero new code. Caught two real gaps live while shipping it: the new MCP tool needed its own `federation.MCPToolCap` entry or it 404s from `POST /api/mcp/call` despite being properly registered, and the new PWA hint text first said "PRD" and left "automaton" untranslated in 4 locale bundles — both caught by this repo's own guard tests before shipping. **Active work:** the v10.0.0 self-build arc — see [§ Roadmap](#roadmap-self-build-arc--v1000-locked-2026-10-10). Stage 1 (BL406+BL407) and Stage 2 (BL404, the real shakedown of the new PRD-driven workflow) are both done — see the Roadmap section for the full B116-B121/BL413 list the shakedown surfaced and fixed same-day. **Stage 3 — BL403** (Plugin Extension Surfaces, `docs/plans/2026-10-09-plugin-extension-surfaces.md`) is now in progress, run PRD-driven per the roadmap's own design, starting with Phase 1 (Plugin Manifest v3). Plans folder refactored 2026-10-10: 11 completed plan docs archived to `historical-plans/`, BL381 closed as shipped, and unscheduled work called out in the Roadmap's "Not yet placed" list.

Previous: **v9.0.7** (2026-10-09). fix(daemon): `datawatch restart`/`stop`/`start` now detect and delegate to an active/enabled `datawatch.service` systemd --user unit instead of raw PID signaling + self-daemonizing; the daemon now falls back to the dedicated `~/.datawatch/tmux` socket when `TMUX_TMPDIR` is unset at boot; `ResumeMonitors` self-heals a `StateFailed` session back to `StateRunning` at boot when its tmux pane is actually still alive. Full history for every release between this and the v8.39.1 entry below is in `CHANGELOG.md` (this section was 8 days / ~400 versions stale until this backlog refactor — see the "Gotcha" note in AGENT.md's Project Tracking Rule: this header is meant to be refreshed every release, not just major ones).

**Headline jump since the previous entry below:** v9.0.0 "Memory Lifecycle Complete" major release (2026-10-07, memory trilogy BL385/386/387 + every v8.x release since formally cut as a milestone); v9.0.1–v9.0.7 patch chain fixing the Dockerfile `GO_VERSION` drift that silently dropped 7 of 9 container images from the v9.0.0 release, a `datawatch-channel` sibling-binary self-update gap stale since 2026-05-12, and the systemd/tmux/boot-self-heal fixes above. Between v8.39.1 and v9.0.0: the full BL394/BL395/security-remediation walkthrough, BL396 PWA parity sweep, BL397 ACME+APNs, BL398 CVE suppression review, BL399 GH#201 audit-logging completeness (5 phases), BL400/BL401/BL402/BL403 filed.

Previous: **v8.39.1** (2026-10-02). fix(session): `last_summary_long` is now populated on every terminal-state transition (kill, subprocess exit, daemon-restart orphan cleanup) via `Manager.triggerSummarize`, not just the `waiting_input` path — closes GH#164.

Previous: **v8.39.0** (2026-10-02). feat(websearch): multi-provider search registry (BL391) — named provider registry (SearXNG + Brave Search API, tried in priority order), internal SQLite result cache to reduce paid-API usage, per-provider usage tracking (total/today/week/month + cache hits + daily series + history), full 6-surface parity (REST/CLI/MCP/comm-channel/YAML/PWA). Closes GH#165 (confirmed Bing-via-SearXNG anti-scraping degradation) by letting a Brave provider run alongside or in front of SearXNG. Brave API key stored in the secrets vault, never plaintext.

Previous: **v8.38.0** (2026-09-30). feat(council): per-persona LLM backend/model assignment (BL390 Phase 1) — Settings gets a backend-pool multi-select (sourced from the existing LLM registry) and a cascading per-persona Backend → Model picker; `Persona.Model` existed since Council Mode shipped but was never actually passed to the inference call, always using the council's one shared `LLMRef` — now wired end-to-end, with the same backend/model pairing-cascade reasoning the per-task/per-story autonomous PRD overrides use. Persona + synthesis calls now admit through the same node:/llm: capacity ledger autonomous PRD tasks use. Found and fixed along the way: the Council persona list and "Edit persona" modal had been silently broken (always empty / "persona not found") since `/api/council/personas` was changed to a bare array in a prior release — four call sites still read a `.personas` property that no longer existed.

Previous: **v8.37.4** (2026-09-30). feat(pwa): expand/collapse button on the PRD markdown/file viewer — operator-requested, since the PWA itself can be maximized on a desktop browser but the viewer stayed capped at 860px. Auto-reverts if the window shrinks back down to where expanded/normal would look identical. Verified with a Playwright-driven browser test (no existing JS test harness in this repo to extend).

Previous: **v8.37.3** (2026-09-30). fix(session): opening a session on a differently-sized device (PWA vs. Android Auto) could flip it from `waiting_input` to `running` with no input sent. Root cause: every client sends `resize_term` unconditionally on open; a redundant tmux resize still makes the TUI repaint on a fresh SIGWINCH, and `StartScreenCapture`'s poll read that repaint as backend activity. `ResizeTmux` now checks the tmux window's current size first (new `TmuxAPI.WindowSize`) and skips the resize when it already matches.

Previous: **v8.37.2** (2026-09-30). fix(autonomous): a per-task `set_task_llm` backend override didn't clear the inherited model, pairing a stale model string from the old backend with the new one — found live diagnosing why PRD `8baa807b`'s task `0c56044c` kept stalling on `opencode`/`ollama/qwen3.8:27b`; overriding to `claude-code` still passed the old Ollama model string through, which claude-code doesn't recognize. Extracted `resolveTaskBackendModel`: model now only inherits from the same level backend was resolved from.

Previous: **v8.37.1** (2026-09-30). fix(pwa): the `session_state` WS handler v8.37.0 shipped passed the wrong payload shape to `updateSession` — `msg.data` instead of `msg.data.session` — which would have corrupted `state.sessions` once deployed. Caught in review before wider rollout, not a live incident.

Previous: **v8.37.0** (2026-09-30). feat(ws): `session_state` single-session WS broadcast for mobile/external clients (GH#162). `MsgSessionState` was defined in the WS protocol from the start but never actually broadcast — every session-list update went out as the full list even when only one session changed. Investigated GH#162's proposed subscribe-channel design and found the underlying capability (full-list push on connect + on every change) already existed unconditionally on `/ws`; this closes the one genuine scoped gap instead — `Hub.BroadcastSessionState` now fires alongside the existing full-list broadcast, additive not a replacement. GH#162 closed; cross-repo client adoption tracked in `datawatch-app#204`.

Previous: **v8.36.14** (2026-09-30). fix(test): `TestExecutor_WorkerStallRespectsRetryBudget` (and any test using the shared `runToTerminal` helper) intermittently failed CI with a `TempDir RemoveAll` race against the executor goroutine's own still-running teardown tail; added a short settle period after observing terminal state.

Previous: **v8.36.13** (2026-09-30). fix(session): a recurring `spawn`-type schedule could get permanently stuck skipping every fire after a daemon restart orphaned its subprocess session at `StateRunning` forever. `ResumeMonitors` now marks an orphaned subprocess session `StateFailed` at boot and fires the terminal-state callbacks the live process would have.

Previous: **v8.36.12** (2026-09-30). fix(test): 5 `internal/server` tests spawned real tmux sessions via `handleStartSession` and never cleaned them up, leaking one orphan per run — 197 accumulated since May. Shared `cleanupStartedSession` test helper added; all 197 orphans cleaned up.

Previous: **v8.36.11** (2026-09-30). fix(pwa): `autonomous.verification_backends` (v8.36.9) had no PWA Settings field — a Configuration Accessibility Rule gap (YAML/REST/MCP/comm-channel/CLI were covered, PWA wasn't). Added as a comma-separated field on the Autonomous settings card.

Previous: **v8.36.10** (2026-09-30). fix(autonomous): a verifier/guardrail `/api/ask` call that itself failed (timeout, unreachable node) was indistinguishable from the model replying with malformed text, both reported as the same misleading "unparseable response". New `askFailureReason` helper checks HTTP status/error field first.

Previous: **v8.36.9** (2026-09-30). feat(autonomous): `autonomous.verification_backends` — an ordered list of LLM registry names the verifier load-balances across by free node capacity, so a cloud-backed PRD's verifier no longer contends with unrelated local GPU work for the same capacity slot. Also fixed: verifier capacity requests never set `PRDID` (capacity card couldn't show a PRD blocked on it); `autonomous_config_set` silently reset every unmentioned field instead of merging.

Previous: **v8.36.8** (2026-09-30). fix(autonomous): verifier/guardrail/scan-grader/orchestrator-guardrail LLM responses wrapped in prose, a markdown fence, or a `<think>` preamble were treated as an outright unparseable failure. New shared `extractJSON` helper strips a fence or falls back to the first/last brace span.

Previous: **v8.36.7** (2026-09-30). fix(autonomous): a worker's checkpoint `git add -A` could reach the operator's home directory when its project dir had never been `git init`'d on its own and git silently resolved to a stray ancestor `.git`. `ensureProjectDirOwnGitRepo` now runs before every task spawn; 7 affected project dirs migrated to their own repos as a one-time cleanup. Also: CI release-asset upload retry window widened for a GitHub read-path propagation lag.

Previous: **v8.36.6** (2026-09-30). fix(autonomous): a PRD that reached `completed` with a story still `cancelled` inside it had no recovery path. `force=true reset_task` now accepts `PRDCompleted` and reopens a `StoryCancelled` story. New `repair_depends_on` endpoint re-resolves v8.36.5's title→ID `depends_on` fix against PRDs planned before it shipped. New howto: `docs/howto/autonomous-recovery.md`.

Previous: **v8.36.5** (2026-09-30). fix(autonomous): task `depends_on` never actually worked — the decompose LLM can only reference a sibling by title, but every consumer compared against IDs, so dependency ordering was never real; `SetStories` now resolves titles to IDs once assigned. A cancelled dependency was also treated as satisfied rather than unmet in the sequential executor path. Also: the one-shot stall-detector's bare `"> "` pattern falsely matched opencode's own normal status line, killing every opencode session almost immediately — scoped the kill+retry to claude-code.

Previous: **v8.36.3** (2026-09-30). fix(autonomous): a deliberately cancelled story no longer rolls the whole PRD up to `PRDFailed` — only `TaskFailed` does now; cancellation is an operator choice, not a failure.

Previous: **v8.36.2** (2026-09-30). fix(ci): v8.36.1's `agent-gemini` CVE fix didn't actually work — the vulnerable `brace-expansion`/`undici` versions come from npm's own bundled deps, not `gemini-cli`. Now overwrites npm's nested copies with the patched packages directly.

Previous: **v8.36.1** (2026-09-30). fix(autonomous): a one-shot claude-code session could hang forever on Claude Code's own interactive disambiguation menu when a retry redelivered an already-partially-done task spec with no "this is a retry" context. Three fixes: one-shot specs now state no human is present; `reset_task` folds prior error/verification detail into the retry hint; wait loop kills+retries a one-shot session stuck at `waiting_input` for ~9s. Also: `gemini-cli` bumped for CVE fixes.

Previous: **v8.36.0** (2026-09-30). feat(capacity): interactive session starts and the verifier's own `/api/ask` call now admit through the same capacity ledger autonomous tasks use, instead of contending unmanaged. Session delete gets the same keep/purge/archive memory-strategy picker PRD hard-delete already had. `/api/capacity?prd_id=` scopes the pool list to one PRD; every registered compute node now always appears even unconfigured. Also fixed: a data race between boot-resume and a live `Run()` could kill an in-flight task's session as a false orphan; `PurgeScope`/`ArchiveScope` dropped the session ID for session-local scopes, so purging one session's memories actually purged the whole project's.

Previous: **v8.35.1** (2026-09-30). fix(memory): PRD hard-delete's `memory_strategy` was silently ignored when sent by the Android client (REST only read it from query params, Android sends JSON body — now falls back to body). Also: PWA retry-button contrast fix; CI OpenSSL DTLS CVE suppression (no DTLS usage).

Previous: **v8.35.0** (2026-09-30). feat(autonomous): structural Automaton editing (add/remove story or task) without a full re-decompose, on REST/MCP/CLI/comm/PWA. A cancelled Automaton is now editable in place (edit spec, structural edits, `reset_task`) instead of requiring destructive `reset_to_draft`. Halt-on-story-failure is now the default instead of continuing into unrelated later stories. Also: PRD file viewer renders GFM tables/Mermaid; animated splash screens ported from datawatch-app.

Previous: **v8.33.30** (2026-09-16). feat(files): Inline markdown viewer for `files_touched` file chips (B106). Clicking a file chip now opens an in-app modal instead of a new browser tab. Markdown files render with `renderChatMarkdown` (headings, code blocks, lists, etc.). Other text/code files render as monospace `<pre>`. Modal header has filename, ⬇ Download button, and Close. Backdrop click dismisses. Binary files retain download-only behavior.

Previous: **v8.33.29** (2026-09-16). fix(pwa): Automata list now shows completed automata by default; filter bar gains completed/cancelled/rejected/archived badges (B105). `completed` added to `_AUTOMATA_ACTIVE_STATUSES` so it appears in the default view without toggling ⏱. Filter bar badge list extended with `completed`, `cancelled`, `rejected`, `archived` (latter three with a visual separator + reduced opacity). Badge filter bypasses the history gate so clicking `archived` shows archived items directly without enabling the history toggle. ⏱ button tooltip updated to describe what it adds (cancelled/rejected/archived) rather than just saying "History".

Previous: **v8.33.28** (2026-09-16). fix(pwa/autonomous): PRD list view story/task status now matches detail view (B104). Three fixes: (1) `_taskStatusIcon` now maps `running`/`verifying`/`running_tests` to correct icons (`▶`/`⟳`/`🧪`) instead of falling through to `○`; (2) `renderDetailStoriesTree` story status uses same `hasActiveTasks` override as `_renderStatusGraphs` — won't show ✓ when tasks are actively running; (3) `loadAutomataPanel`/`_automataRenderCards` now save + restore `<details open>` state so expanded "Stories & tasks" cards stay open across WS-triggered refreshes.

Previous: **v8.33.27** (2026-09-16). fix(pwa/autonomous): two fixes — (B103) Automata detail view now live-updates without flicker: `prd_update` WS events patch only changed task rows (via `data-task-id`), story status pills, and the pre-isolated status-graph/active-session slots; expanded task state and scroll position are preserved. (B102) `files_touched` now records both uncommitted tracked-file edits (`git diff HEAD`) and brand-new untracked files (`git ls-files --others`) instead of only committed changes (`HEAD~1..HEAD`); for non-git project dirs falls back to `find-newer` since session start time using `TouchedFiles(sess.CreatedAt)`.

Previous: **v8.33.26** (2026-09-16). fix(pwa): Restart/Stop buttons in session header now have correct height and 6px gap between them (B101). `#actionBtns` span changed from inline to `inline-flex; align-items:center; gap:6px` — previously the span's inline context caused the buttons to render at a different height than the other pill buttons in the `.meta` flex row, and no gap between Stop and Restart.

Previous: **v8.33.25** (2026-09-16). fix(pwa/autonomous): three Automata detail-view bugs — (B99) `ResetTask` now resets story status from `completed`/`failed` to `pending` when resetting a task within that story; progress card dot no longer shows ✓ for a story when any of its tasks are actively running; PRD tab switching (Overview/Stories/Decisions) now swaps tab content in-place without re-fetching the PRD, eliminating the loading flash on every tab click.

Previous: **v8.33.24** (2026-09-16). fix(autonomous): `Run()` is now idempotent — if an executor goroutine is already live it returns without cancelling it (B98). Previously clicking Run in the PWA while a task was mid-spawn would cancel the in-flight HTTP request, marking the task failed with "context canceled", then the new goroutine would see all tasks terminal and immediately fail the PRD. Now `Run()` checks `runCancels[id]`; if an entry exists the live goroutine will pick up any newly-pending tasks on its next poll cycle.

Previous: **v8.33.23** (2026-09-16). fix(autonomous): `reset_task` now works on PRDFailed PRDs (B97) — previously refused with "not running"; now accepts PRDFailed and auto-restores PRDRunning so partially-complete PRDs can be recovered without discarding all completed task work.

Previous: **v8.33.22** (2026-09-16). feat(files): files-as-links (BL374) + fix auto_fix_retries default (B96). New `GET /api/files/download?path=<abs>&inline=1` endpoint serves task output files; 6 tests. PWA: `files_touched` chips in task/story cards replaced with clickable `⬇ filename` links. B96: `config.DefaultConfig()` now initializes `Autonomous.AutoFixRetries: 1` (was 0 due to missing struct init — caused single verifier parse failure to kill whole PRD with no retry).

Previous: **v8.33.18** (2026-09-16). fix(sessions): suppress spurious needs-input alerts after task delivery (B92). `MarkInputSent()` method + `inputSentAt` map in Manager; 30s post-input-sent suppression window in both `MarkWaitingInput` and `tryTransitionToWaiting` codepaths blocks the brief prompt flash that opencode shows between Enter-pressed and esc-interrupt.

Previous: **v8.33.17** (2026-09-16). fix(sessions): eliminate false-positive `DATAWATCH_COMPLETE:` detection in opencode TUI executor sessions (B91). Three-layer defense: (1) `MarkTaskDelivered` moved before `SendInput`; (2) 5-minute task-echo suppression in `StartScreenCapture` and `monitorOutput`; (3) `strings.Contains(l, "<one-sentence summary")` placeholder guard in visible and scrollback scans. Also: mcp/search config-file fallback for web_search.{url,engine} when DATAWATCH_WEB_SEARCH_URL env absent.

Previous: **v8.33.8** (2026-09-15). fix(autonomous): executor goroutines re-launched at daemon startup for PRDs in `running` state (`resumeRunningPRDs()` in `SetExecutors`); `reset_task` handler now calls `Run(id)` after reset to guarantee executor is alive. Fixes Automata stuck in "running" with no sessions starting after daemon restart or after task reset on a restarted daemon.

Previous: **v8.33.7** (2026-09-15). fix(daemon): in-place update now auto-restarts the daemon (detached shell child polls for PID exit then calls `datawatch start`); no service manager required.

Previous: **v8.33.21** (2026-09-16). fix(pwa): image attachment success toast (B94) — `showToast` added to upload success path; B90 README docs corrected (fixed v8.33.2, not open).

Previous: **v8.33.20** (2026-09-16). fix(pwa/autonomous): progress bars show 0% despite completed tasks; story status `"running"` injected by LLM decomposer now normalized to `StoryPending` in `SetStories` (B95). `_renderStatusGraphs` adds `'completed'` to `terminalTaskStates`; handles `'running'` story status as active.

Previous: **v8.33.4** (2026-09-15). docs: remove internal IDs from user-facing howtos; add exec_steps to automata-memory-workflow howto. Pre-release smoke found 9 BL### leaks in docs/datawatch-definitions.md + docs/howto/automata-memory-workflow.md + docs/howto/prd-memory-workflow.md — replaced with plain version strings. prd-memory-workflow.md lacked exec_steps front-matter (howto-coverage rule) — added exec_steps for seed/harvest/get steps; corrected title to "Automata Memory Workflow". smoke: 94 headers, 169 passed, 0 failed, 30 skipped.

Previous: **v8.33.3** (2026-09-15). feat(server): per-guardrail block approval endpoint (GH#153) — REST `POST /api/sessions/{id}/guardrail/{name}/approve`, MCP `session_guardrail_approve`, comm `session guardrail-approve`. 7 tests. ZAP GH#154 triage — 110002/110004 confirmed false positives, already IGNORE'd in rules.tsv. Both issues closed.

Previous: **v8.33.2** (2026-09-15). fix(autonomous): boot-time stuck-task reconciliation (B90) — `reconcileStuckTasks()` at Manager startup resets stuck `TaskInProgress`/`TaskVerifying`/`TaskRunningTests` tasks to `TaskPending` when session is no longer alive (kills orphaned session first); `SetSessionAliveFn` callback wired in main.go; 6 regression tests. Depends on `AutoFixRetries` to re-queue. All 2626 tests passing.

Previous: **v8.33.1** (2026-09-15). fix(autonomous): SSE stall watchdog + opencode chunk/header timeouts (B89) — "SSE read timed out" added to stall patterns; patterns + matcher extracted to `sse_stall.go`; new `opencode.ollama_chunk_timeout_sec`/`opencode.ollama_header_timeout_sec` config fields (default 1200s/900s) written to opencode.json.

Previous: **v8.33.0** (2026-09-15, retagged). feat(memory): Automata Memory Integration Phase 3 (BL387) — auto-report on PRDCompleted (async goroutine, memoryReportFn callback, PRD.MemoryReport field); memory-scope PWA dashboard tile (per-scope bar chart from /api/memory/stats). BL387 complete; all 3 phases across BL385-387 form complete memory lifecycle. CI fixes included in retag: gitleaks advisory (5d05a9c5), goreleaser dirty go.mod (58d7e3a7 — removed go mod tidy before hook, added verify step).

Previous: **v8.32.0** (2026-09-15). feat(memory): Automata Memory Integration Phase 2 (BL387) — decomposer prompt enriched with project-shared context via memoryContextFn callback; cross-Automaton prd-shared seeding at first run via MemorySeedConfig.FromPRDs + memoryCrossSeedFn; REST/MCP/AutonomousAPI interface all updated with from_prds param. 8 new tests.

Previous: **v8.31.0** (2026-09-15). feat(memory): Automata Memory Integration Phase 1 (BL387) — verifier findings written to prd-shared on task failure (role=verifier-finding, prefix=[verifier-finding]), child Automaton prd-shared inheritance (up to 50 entries seeded parent→child before decompose). Both controlled by existing memory_seed.enabled flag. Manager gains memoryVerifierFn + memoryScopeSeedFn callbacks with SetMemoryVerifierFn/SetMemoryScopeSeedFn setters.

Previous: **v8.30.0** (2026-09-15). feat(memory): Autonomous Automata Memory Lifecycle Management (BL386) — warm-start seeding, harvest-on-completion, archive-on-delete, archive-import, memory handoff MCP tool, Automaton memory report REST+MCP, scope inventory REST+MCP, scoped TTL sweep, UTC timezone fix for Prune/PruneByRole/SweepStale. All 5 phases complete.

Previous: **v8.29.0** (2026-09-15). feat(memory): Subprocess Memory Scope Isolation — extends the memory scope hierarchy from 4 to 6 layers, adding `prd-shared` and `story-shared` scopes. MCP subprocess memory tools now route through scoped API automatically; `memory_sweep_stale`/`memory_import` blocked in subprocess mode. New REST save/delete endpoints. Executor injects `--caller-prd-id`/`--caller-story-id` into Goose MCP args.

Previous: **v8.28.9** (2026-09-15). fix(autonomous): Automata comm-channel parity gap B88 — adds `cancel-story`, `cancel-task`, `reset-to-draft`, `set-quality-gates`, `scan-results` to `internal/router/sx2_parity.go`. All five operations were present on REST, MCP, and PWA but missing from the comm-channel router (BL382 regression). Also updates help string.

Previous: **v8.28.8** (2026-09-15). fix(autonomous): verifier now detects pre-existing untracked files overwritten by a task (B87). When `task.FilesPlanned` overlaps `pre_task_untracked_files`, the verifier reads current file content (up to 8 KB) and includes it as `<overwritten_files>` context so the LLM can verify the spec was satisfied. The hard "no output" early-return now only fires when diff, new-untracked, AND overwritten-files are all empty.

Previous: **v8.28.0** (2026-09-14). feat(autonomous): per-story LLM config (BL381) — Story struct gains Backend/Effort/Model fields; executor resolves per-task → per-story → per-PRD → global; new `set_story_llm` on 5 surfaces (REST, MCP `autonomous_prd_set_story_llm`, CLI `prd-set-story-llm`, comm channel `autonomous set-story-llm`, API). Allows Story A to run on claude-opus while Story B uses llama3 concurrently.\n\nPrevious: **v8.27.11** (2026-09-14). fix(pwa): image attachments now work end-to-end — `[image:path]` converted to `@path` (Claude Code file-reference notation) when no vision backend is configured, allowing Claude Code sessions to read and process images directly. Chain of PWA attachment fixes v8.27.6–v8.27.11: Android label fix, preview position, re-render survival, multi-file, 403 upload fix, channel-mode send, upload-blocking guard, and vision passthrough.

Previous: **v8.27.4** (2026-09-14). fix(autonomous): sequential executor re-runs terminal tasks on restart — the sequential path in `Manager.Run()` now skips tasks already in a terminal state (`completed`/`failed`/`cancelled`) when the executor goroutine is restarted. Previously, a re-`/run` would re-dispatch all tasks in topological order including already-finished work, and failed tasks would not populate `failedIDs`, causing their dependents to run instead of being skipped. Also: `qwen3.8:27b` replaced with `qwen3:8b` as default PRD model to avoid provider header timeouts.

Previous: **v8.27.3** (2026-09-14). fix(autonomous): add "provider response headers" to SSE stall watchdog patterns — `autonomousSSEStallPatterns` now includes `"provider response headers"` so the built-in watchdog detects and kills opencode sessions where the ollama backend never sent HTTP response headers (cold-start overload on large models like `qwen3.8:27b`).

Previous: **v8.27.2** (2026-09-13). fix(autonomous): verifier detects newly created untracked files — `git diff <pre-task-SHA>..HEAD` empty no longer causes hard fail when the task's only output is new untracked markdown/code files. Pre-task untracked file list captured at spawn time via `git ls-files --others --exclude-standard`; post-task comparison surfaces truly new files as `<new_files>` section in the verifier prompt.

Previous: **v8.25.11** (2026-09-13). fix(autonomous): enforce strict contract on all guardrail and verifier LLM responses — autonomous PRD guardrail, orchestrator guardrail, and scan grader all now default to `block`/`fail` on unparseable LLM response instead of `warn`, preventing false-positive PRD advancement.

Previous: **v8.25.8** (2026-09-13). fix(compute): scheme normalization moved to storage layer — `Node.Validate()` now prepends `http://` to bare `host:port` addresses before persisting, covering all write paths (REST API, MCP, CLI, PWA, Android). `EnsureFromStatsPeer` also normalizes RemoteAddr on stats-peer push. Belt-and-suspenders guards added to the three `n.Address` read sites in api.go. 4 new registry normalization tests.

Previous: **v8.25.7** (2026-09-13). fix(autonomous): defensive http:// scheme guard for compute node Ollama URLs — scheme-less node address (e.g. `127.0.0.1:53106`) no longer produces unparseable `baseURL` in opencode.json. Guard in both api.go and lsp.go. `"cannot be parsed as a URL"` added to SSE stall patterns for auto-kill. New PRD E2E test suite (6 tests: full lifecycle, approval gate, spawn error, multi-story, retry exhaustion, dependency order).

Previous: **v8.25.6** (2026-09-13). fix(autonomous): memory checkpointing appended to every task spec — CHECKPOINT.md (file checkpoint, survives daemon restarts) + memory_remember MCP call (survives session restarts), both written every 5–10 min by the worker.

Previous: **v8.25.5** (2026-09-13). fix(autonomous): SSE stall detection in autonomousVerify wait loop — scrollback scanned every ~30 s for opencode SSE timeout/connection patterns; stalled session killed and executor retry fires automatically. Background PRD watchdog goroutine (60 s tick) kills stalled task sessions on running PRDs. `Manager.CapturePaneScrollback` public helper added.

Previous: **v8.25.4** (2026-09-12). feat(ui): Observatory peer resources panel (live GPU/CPU/mem per peer with sparklines), collapsible channel bridge/diagnostics sections, session stats compute node rich card (GPU util/temp/power/VRAM + ollama CPU/RSS, 5 s live refresh), Automata Progress tab compute GPU/CPU card (5 s live refresh). 15 new i18n locale keys across en/de/es/fr/ja. All locale guard tests pass. datawatch-app#168 filed.

Previous: **v8.25.3** (2026-09-12). feat(observer): nvidia-smi and tegrastats GPU probes for Shape B peers. `SMIProbe` (nvidia-smi CSV) and `TegraStatsProbe` (tegrastats, handles both classic Jetson GR3D_FREQ and NVIDIA Thor gpu@T/VDD_GPU formats) now populate `snap.GPU[]` — temp, memory, power, util% — in the observer peer StatsResponse. `SetGPUFn` hook added to Collector; tegrastats takes priority on Jetson/Tegra hosts.

 Previous: **v8.25.0** (2026-09-12). feat(automata/pwa): automata UI improvements — spec expand (show full / collapse), status graphs (per-story progress bars + async CPU%/RSS from observer), cancel button always visible in toolbar, "View Sessions" in ⋯ Edit dropdown. Fixes: task click in list "Stories & tasks" panel no longer collapses it; expand button in Stories tab works on first click.

 Next: **v10.0.0** via the self-build arc (see § Roadmap). The harness-impl planning docs (`docs/plans/harness-impl/`) previously listed here as "v-next" are **not yet placed** in that roadmap — see its "Not yet placed" list.

 | Bucket | Count | Notes |
|---|---|---|
| Open bugs | 0 | B111/B112 — fixed by BL406 Phase 5, v9.0.17. B113 — fixed by BL406 Phase 0. B114 (`PRD.ClusterProfile` dispatch never resolves) — fixed by BL407 Phase 0, v9.0.18. B115 (zero e2e cookbook coverage added since the v9.0.0 tag) — fixed by backfilling TS-787-TS-795, 2026-10-10. B116 (`PUT /api/config`'s autonomous.* propagation clobbered live-only no-omitempty bool defaults, e.g. `auto_approve_children` true→false on an unrelated write) — fixed v9.0.24. B117 (PRD creation rejected the exact empty-project_dir case BL407 Phase 1's worktree auto-assignment is designed for) — fixed v9.0.25. B118 (session-based decompose needs project_dir too, but worktree auto-creation only fired at Run() time; also added orphan-worktree cleanup on Reject/Archive) — fixed v9.0.26. B119 (`autonomous.execution_backend` had zero config surface despite existing as a live field) — fixed v9.0.26. B120 (an explicit clear-to-zero on a touched `autonomous.*` field, e.g. `planning_model: ""`, was itself omitted from B116's own scoped propagation, found live immediately after v9.0.26 shipped) — fixed v9.0.27. B121 (`autonomous.rules_file`/`context_file` had full config plumbing but zero consumers — BL406's own "decomposer reads it" claim was never implemented despite all 6 phases marked Done) — fixed v9.0.28 (decomposer half; scanner half filed as BL413). B122 (B121's own large-prompt injection exposed a tmux send-keys timing race — a ~160KB/20-chunk payload corrupted the receiving TUI's input rendering and the trailing Enter never registered) — fixed v9.0.29. None open |
| Open features | 7 | BL241 Matrix (design interview needed); BL365 + BL408 + BL395 security remediation track (roadmap Stage 3 #7); BL390 Phase 2/3 (folded into BL405 Phase 8a); BL393 nested tags (excluded until authorized); BL394 + BL392 (substantially done, residue cross-repo). BL389 shipped v9.0.0; BL381 shipped v8.28.0/v8.28.2 |
| Active backlog | 0 | BL387 complete (v8.31–33.0 ✅) |
| Pending backlog | 5 | BL402, BL403, BL404, BL405 (v10.0.0 roadmap; BL406 shipped v9.0.17, BL407 shipped v9.0.23) + BL398 Phase 2; BL400 deferred (open questions). BL335 shipped v8.62.x via BL397 Phase 4 |
| Active (in-progress) | 0 | — |
| Deferred | 0 | — |
| Awaiting operator action | 0 | — |
| Open GH issues | 3 | GH#4 — mobile parity tracking (meta, perpetual); GH#196 — `security/accepted-risks.yml` daily watch; GH#204 — result panel for scheduled jobs (= BL402) |
| Recently closed | GH#162 ✅ v8.37.0 (session_state WS broadcast; client adoption tracked datawatch-app#204); B106 ✅ v8.33.30; B105 ✅ v8.33.29; B104 ✅ v8.33.28; B103 ✅ v8.33.27; B102 ✅ v8.33.27; B101 ✅ v8.33.26; B99 ✅ v8.33.25; B98 ✅ v8.33.24; B97 ✅ v8.33.23; BL374 ✅ v8.33.22; B96 ✅ v8.33.22; B94 ✅ v8.33.21; B95 ✅ v8.33.20; B93 ✅ v8.33.19; B92 ✅ v8.33.18; B91 ✅ v8.33.17; GH#153 ✅ v8.33.3; GH#154 ✅ v8.33.3; B90 ✅ v8.33.2; B89 ✅ v8.33.1; BL387 ✅ v8.33.0; BL386 ✅ v8.30.0; B88 ✅ v8.28.9; B87 ✅ v8.28.8; BL381 ✅ v8.28.0; B86 ✅ v8.27.4; B85 ✅ v8.27.3; B84 ✅ v8.27.2; B83 ✅ v8.27.1; BL382 ✅ v8.27.0; BL380 ✅ v8.27.0; B82 ✅ v8.25.12; B81 ✅ v8.25.11; B79+B80 ✅ v8.25.10; BL377 ✅ v8.25.6; BL376 ✅ v8.25.5; BL375 ✅ v8.25.4; BL373 ✅ v8.25.0; BL374 ✅ v8.33.22; B78 ✅ v8.23.0; BL372 ✅ v8.22.0; BL327–BL334 ✅ v8.2.0–v8.6.0; BL336 ✅ v8.9.17–v8.9.19; BL337 ✅ v8.9.20; BL338 ✅ v8.9.21; BL339 ✅ v8.9.22; BL340 ✅ v8.9.23; BL341 ✅ v8.9.24; BL342+BL343 ✅ v8.9.25; BL347 ✅ v8.10.0; BL353 ✅ v8.10.4; BL354 ✅ v8.10.5; BL355 ✅ v8.10.6; BL356 ✅ v8.10.7; BL357 ✅ v8.10.8; BL358 ✅ v8.10.9; BL359 ✅ v8.10.10; BL360 ✅ v8.10.11; BL361 ✅ v8.10.12; BL362 ✅ v8.10.17; BL319 ✅ v8.13.0; GH#117 ✅ v8.13.1; GH#120 ✅ v8.13.0; GH#125 ✅ v8.9.25 (already existed); GH#128 ✅ v8.13.2; GH#129 ✅ v8.13.4; BL368 ✅ v8.15.0; BL366 ✅ v8.16.0; BL367 ✅ v8.17.0; BL369 ✅ v8.18.0; B55+B56 ✅ v8.19.9; B57 ✅ v8.19.10; BL371 ✅ v8.20.0; B58 ✅ v8.20.1; B59+B60+B61+B62+B63 ✅ v8.20.2–v8.20.3; B64+B65 ✅ v8.20.4; B66 ✅ v8.20.5; B67 ✅ v8.20.6; B68 ✅ v8.20.7; B69+B70+B71 ✅ v8.20.8; B72+B73+B74+B75+B76 ✅ v8.20.9–v8.20.11; B77 ✅ v8.21.0; B79+B80 ✅ v8.25.10; B81 ✅ v8.25.11 | badge/chip, async decompose, push, channel routing, file service, discussion scopes, operational encryption; anti-clobber typing hold + queue; line-printing renderer lock; update self-update archive priority + channel co-update; container builder CVE fix; imap-mcp email command channel; MCP session name resolution + permission_mode; compute migrate CLI + federation peer health alerts; session lineage + cascade kill + reply_to_parent; recurring named schedules; name-addressed session ops; claude_alive zombie detection; exit hooks; work queue; discussion push/subscribe; restart_session; result store; list_sessions filters; channel bridge diagnostics; extra_mcp_servers injection; alert dock fix; FCM payload enrichment; schedule spawn overlap guard + run history; downloadChannelBinary version fix; vision input system; verifier git-diff grounding; autonomous PRD quality gates; prompt injection hardening; PRD backend filter; PRD Approve/Reject restore; autonomous task backend resolution; PRD split planning vs execution backend; cancelled PRD restart; session wait/cleanup; wizard create modal execution backend + dir validation; Plan/Run prominent toolbar buttons; image attachment file picker; Start Planning feedback + detail view progress; Edit menu stacking context fix; Start Planning button disables in-place + Cancel warning; opencode one-shot task delivery via state-change handler; prd_id/task_id on sessions; verifying progress + glyphs; active session filter fix; generalized TUI task delivery (goose/aider/all backends); session-based PRD decomposer with codebase access; DATAWATCH_COMPLETE: detection fixed for one-shot sessions (v8.21.1–v8.21.4: visible scan limit, firstTick skip, daemon-restart recovery, creation-time screen capture) |
| Frozen / external | 7 items | BL281–BL285 (Vault follow-ups) · F7 · S14c · mobile parity GH#4 |
| GH issues closed/triaged | GH#52 ✅ (BL316), GH#63 ✅ (BL317), GH#77→BL328 ✅, GH#75→BL329 ✅, GH#76→BL330 ✅, GH#72→BL331 ✅, GH#68+69→BL332 ✅, GH#70→BL333 ✅, GH#78 ✅ v8.8.0 (PWA E2E Phase 0+1), GH#91–GH#101 ✅ v8.8.0 (security/dashboard/observer/docs sprint), GH#117 ✅ v8.13.1 (FCM payload), GH#118 ✅ v8.13.0 (extra_mcp_servers), GH#120 ✅ v8.13.0 (alert dock), GH#125 ✅ v8.9.25 (compute migrate already existed), GH#128 ✅ v8.13.2 (schedule spawn), GH#129 ✅ v8.13.4 (downloadChannelBinary version), GH#152 ✅ v8.27.0 (BL382 cancel_story/cancel_task/requeue_task) | |

v8.14.0 shipped 2026-08-29 — BL363 testing gap closure: 22 unit tests for T2/T3 (`providerKeyEnvVar`, `shellQuote`, `gooseEnvPrefix` all branches, all setters on both backends); config-reference.yaml updated with provider/model/api_key_ref/channel_enabled; Settings UI Goose card added to LLM_CONFIG_FIELDS; llm-backends.md goose section rewritten for T2/T3; minor version bump (8.13.x→8.14.0) retroactive for new LLM backend. BL363 ✅ closed.
v8.13.39 shipped 2026-08-29 — BL363 T4: agent-goose container finalized — `ENV GOOSE_CLI_THEME=plain` + `/home/datawatch/.config/goose/` pre-created in Dockerfile.
v8.13.38 shipped 2026-08-29 — BL363 T3: Goose MCP channel integration — `GooseConfig.ChannelEnabled`; `GOOSE_MCP__DATAWATCH__*` env vars injected at launch; `datawatch mcp --caller-session-id` flag; `mcp.Server.SetCallerSessionID`; config API (`goose.channel_enabled`); manager injects on both new-session and restart paths.
v8.13.37 shipped 2026-08-29 — BL363 T2: Goose provider/model/API-key injection — `GooseConfig` gains `provider`, `model`, `api_key_ref`; `SetProvider`/`SetModel`/`SetAPIKey` setters on both backends; manager calls setters from config before launch; `providerKeyEnvVar` maps provider→env-var; config API (`goose.provider`, `goose.model`, `goose.api_key_ref`) wired.
v8.13.36 shipped 2026-08-29 — BL363 T1: Goose backend fully functional — init() registration, binary fallback resolution, interactive TUI (`goose session`), Nameable (`--name`), Resumable (`goose session resume`), goose-prompt one-shot, version normalization, 11 unit tests.
v8.13.35 shipped 2026-08-29 — OpenCode scrollback fix: `DisableAlternateScreen` called on tmux window after session creation for opencode/opencode-acp backends; TUI output now lands on main screen instead of alternate buffer.
v8.13.34 shipped 2026-08-28 — OpenCode new-session model picker sends `?node=<compute_node>` (BL364); Ollama provider block corrected to openai-compatible shape OpenCode actually recognizes.
v8.13.33 shipped 2026-08-12 — CVE-2026-58050 (libssh2-1 HIGH, no fix in bookworm) added to trivyignore; SSH server attack surface, operator-configured remotes only.
v8.13.32 shipped 2026-08-12 — rtk curl retry 10×30s (5-min window) + --retry-all-errors; 5×10s was exhausted by GitHub releases sustained 503.
v8.13.31 shipped 2026-08-12 — Trivy pin v0.70.0→v0.73.0; v0.70.0 never published, install.sh exited non-zero blocking SARIF in build-base+build-agents.
v8.13.30 shipped 2026-08-12 — Dockerfile.agent-base: rtk curl download adds --retry 5 --retry-delay 10 (arm64 build got HTTP 503 from GitHub releases).
v8.13.29 shipped 2026-08-12 — 26 Dependabot CVEs resolved: channel npm overrides updated (hono 4.12.19→4.13.1, @hono/node-server 2.0.3→2.1.0, fast-uri 3.1.2→4.1.2, ip-address 10.2.0→10.5.0, qs 6.15.0→6.15.3, body-parser 2.2.2→2.3.0).
v8.13.28 shipped 2026-08-08 — ZAP rule 120000 IGNORE (localStorage is intentional SPA design; XSS mitigated by nonce CSP); closed GH#145-147.
v8.13.27 shipped 2026-08-08 — gh release upload --repo flag (fixes "release not found" in CI); 8 new CVE triages: CVE-2026-15308 (python HTML DoS), CVE-2026-14257+CVE-2026-69152 (brace-expansion ReDoS), CVE-2026-69192 (ip-address), CVE-2026-69244 (aiohttp), CVE-2026-59939 (httplib2), GHSA-6v7p-g79w-8964 (MessagePack), CVE-2025-47273 (setuptools).
v8.13.26 shipped 2026-08-08 — CVE-2026-8458 (curl HIGH, unauthorized connection reuse) added to trivyignore; attach-tarball if-guard runs even when build-base partial failure.
v8.13.25 shipped 2026-08-08 — ZAP rule 10024 IGNORE (session_id URL param is datawatch session filter, not HTTP auth token); closed 15 stale ZAP issues GH#130–144.
v8.13.24 shipped 2026-07-22 — CVE-2026-47063 (openjdk HIGH) added to trivyignore; attach-tarball no longer depends on build-agents (tarball now uploads even when agent containers fail).
v8.13.23 shipped 2026-07-22 — goreleaser retry binary (curl installer + dynamic rate-limit sleep); attach-tarball depends on goreleaser (prevents "release not found" race).
v8.13.22 shipped 2026-07-22 — agent-goose bump to 1.43.0 + aaif-goose URL; CVE-2026-41254 suppressed in parent-full.
v8.13.21 shipped 2026-07-22 — goreleaser + attach-tarball retry on rate-limit; replaces softprops/action-gh-release with gh release upload loop.
v8.13.20 shipped 2026-07-21 — soupsieve>=2.8.4 in agent-aider venv (CVE-2026-49476 + CVE-2026-49477 DoS, fixed 2.8.4).
v8.13.19 shipped 2026-07-21 — continue-on-error on daemon restart step so advisory ZAP section is fully non-blocking.
v8.13.18 shipped 2026-07-21 — daemon restart: kill -9 + lsof port-wait + rm -rf state dir before restart (old SIGTERM left port bound).
v8.13.17 shipped 2026-07-21 — .trivyignore: 7 libglib2.0-0 CVEs (CRITICAL+HIGH, fix_deferred) in parent-full (transitive via signal-cli).
v8.13.16 shipped 2026-07-21 — daemon restart timeout 30s→60s to match initial startup budget; add error logging on timeout.
v8.13.15 shipped 2026-07-21 — .trivyignore: 5 agent-gemini CVEs (libpython3.11 + 4 npm transitive: brace-expansion, sigstore, tar×2).
v8.13.14 shipped 2026-07-21 — ZAP: daemon health-check+restart before advisory passes; continue-on-error on passes 3-5.
v8.13.13 shipped 2026-07-21 — .trivyignore: GHSA-hrxh-6v49-42gf (grpc HIGH in /usr/bin/gh, not in datawatch module graph).
v8.13.12 shipped 2026-07-21 — ZAP rules.tsv: 100000+90022 WARN→IGNORE (WARN causes WARN-NEW exit-2 in no-baseline runs).
v8.13.11 shipped 2026-07-21 — ZAP CI: rm -f cleanup between passes (Docker UID PermissionError); distinct artifact_name per pass.
v8.13.10 shipped 2026-07-21 — ZAP rules.tsv: 4 new WARN-NEW findings added (100000 server error, 10106 HTTP-only, 2 private IP, 90022 app error) — all expected in unauthenticated CI scan.

v8.13.9 shipped 2026-07-21 — .trivyignore: CVE-2026-39822 (Go os.Root symlink following) suppressed pending golang:1.26.5-bookworm Docker Hub publish. datawatch does not call os.Root (confirmed by source search; govulncheck passes).

v8.13.8 shipped 2026-07-21 — Dockerfile.stats-cluster + Dockerfile.validator: ARG GO_VERSION=1.25→1.26 to satisfy go.mod go 1.26.5 minimum with GOTOOLCHAIN=local.

v8.13.7 shipped 2026-07-21 — Dockerfile.agent-base: ARG GO_VERSION=1.25.12→1.26.5 (golang:1.25.12 not on Docker Hub; builder resolved to 1.26.4; 1.26.5 fixes CVE-2026-39822). go.mod: go 1.25.12→1.26.5. .trivyignore: 23 new Debian bookworm CVEs triaged (all affected/fix_deferred).

v8.13.6 shipped 2026-07-21 — Dockerfile.agent-base: ARG GO_VERSION=1.25.11→1.25.12. go.mod `go` directive now 1.25.12 + GOTOOLCHAIN=local in Docker → builder stage go mod download failed; fixed by matching version.

v8.13.5 shipped 2026-07-21 — Security: golang.org/x/text v0.37.0→v0.39.0 (GO-2026-5970 infinite loop on invalid input in norm.Form). go directive 1.25.11→1.25.12 (GO-2026-5856 crypto/tls ECH privacy leak). Cascading x/golang upgrades (crypto v0.53.0, net v0.56.0, sys v0.46.0).

v8.13.4 shipped 2026-07-21 — GH#129 fix: `downloadChannelBinary(dataDir, targetVersion string)` — `datawatch update` and auto-updater now pass `latest` (resolved target version) instead of compile-time `Version` global; startup self-heal and `setup channel` continue to pass `Version` (correct — downloads matching running binary).

v8.13.3 shipped 2026-06-28 — LLM-optional memory system: `Recall`/`RecallAll`/`RecallInNamespaces`/`RetrieveContext` now fall back to keyword/LIKE search when the embedding LLM is unavailable (Similarity=0 on fallback results). New `TextSearchableBackend` optional interface implemented by SQLite `Store` (`SearchByText`, `SearchAllByText`, `SearchInNamespacesByText`, `ListUnembedded`). `SaveOutputChunks` stores chunks without a vector instead of skipping when Ollama is offline. New `LazyReembed(batchSize)` on `Retriever` re-embeds un-vectorized memories when LLM comes back online. Summarizer `Summarize`/`SummarizeDual` return stub "Summary unavailable — LLM offline." instead of propagating errors. 234 tests.

v8.13.2 shipped 2026-06-27 — GH#128 schedule spawn overlap guard + run history: `ScheduledCommand` gains `active_spawn_id`, `last_fire_at`, `last_fire_result`, `fire_count`. Scheduler skips new cron fire when prior spawn still running (not complete/failed/killed) via `SkipFire()`. `RecordFire()` persists run history after each spawn. `schedule list` shows FIRES/LAST-FIRE/LAST-RESULT columns. CLI `schedule spawn` gains `--shell <cmd>` (subprocess alias) and `--path <dir>` (alias for --dir). 401 tests.

v8.13.1 shipped 2026-06-27 — GH#117 FCM push payload enrichment: `session_id`, `session_name`, `last_response` added to `session_state_changed` Extras in state-change handler and `NeedsInputHandler` waiting_input push events. Mobile clients can now correlate push events to sessions without fetching the session list.

v8.13.0 shipped 2026-06-27 — BL319/GH#118 `session.extra_mcp_servers` config: injects additional MCP servers into `.mcp.json` on every spawn alongside the built-in datawatch bridge. Non-claude-code: via `WriteProjectMCPConfig`. Claude-code: via `InjectExtrasIntoMCPConfig`. GH#120 alert dock fix: `handleAlert` early return removed (dock was missing alerts in session-detail view); four `navigate('session', …)` → `navigate('session-detail', …)` typo fixes in alerts view.

v8.12.9 shipped 2026-06-27 — `Manager.SendInput` now applies `scheduleSettleMs` to ALL send sources (was schedule-only since v8.0.x). Root cause: MCP/CLI/API sends used hardcoded 120ms settle which is too short for React Ink TUI two-step literal+Enter. `POST /api/sessions/send` dedicated REST endpoint (bypasses text-command parsing). CLI `session send` uses new endpoint first, falls back to `/api/command` for older daemons. Lint fix: `strings.TrimSuffix` not `TrimRight` (SA1024). CVE-2026-55199 added to .trivyignore.

v8.12.x shipped 2026-06-xx — subprocess spawn mode: `--subprocess` flag on `schedule spawn` runs task via `bash -c`; exit code = completion (0=complete, non-zero=failed), no TUI, no DATAWATCH_COMPLETE: scraping. Completion pattern matching in TUI cursor-overwrite output (CR-split, non-ASCII rune strip, 5s task-echo suppression). Various one-shot channel delivery fixes.

v8.11.x shipped 2026-06-xx — `schedule spawn` command introduced: `datawatch schedule spawn --cron "…" --task "…" --dir "…" --backend claude-code`. Ephemeral one-shot sessions from a cron schedule, independent of any running session. `POST /api/schedules` API. `DeferredSession` struct with OneShot, Ephemeral, Subprocess flags.

v8.10.20 shipped 2026-06-13 — patch: sanitizeForSpeech post-processor (strips file paths, URLs, error codes, code-heavy lines). ollamaChatSystemPrompt + dualSummaryPrompt rewritten to explicitly ban file names, identifiers, and technical jargon. 95 summarizer tests, 2352 total.

v8.10.19 shipped 2026-06-13 — patch: Ollama summary overwrite bug (`GetLastResponse` for `waiting_input` sessions now returns stored summary when `SummaryGeneratedAt` is set, instead of re-capturing raw tmux content). Android Auto TTS formatting: `cleanShort` strips numbered lists, backticks, and bold/italic markdown pairs. Tests added for `stripMarkdownPair` and new `cleanShort` cases.

v8.10.18 shipped 2026-06-12 — patch: `↳ child of [X]` display bug. `strings.Index` (first `-`) → `strings.LastIndex` (last `-`) so the 4-char hex short ID is shown, not the hostname. Fixed in comm router session list and CLI sessions table.

v8.10.17 shipped 2026-06-12 — BL362: MCP channel bridge diagnostics. `GET /api/channel/diagnostics` returns bridge kind/path, global port config, per-session `ChannelSessionDiag` (port, `bridge_alive`, `probe_error`), and actionable hints. `channel_diagnostics` MCP tool. `datawatch channel diagnostics [--json]` CLI. `channel diagnostics` comm router trigger. PWA diagnostics card. `docs/config-reference.yaml` env var docs. `docs/datawatch-definitions.md` definition section. Full 7-surface parity.

v8.10.12 shipped 2026-06-11 — BL361: `list_sessions` structured filters. `SessionFilter` (name glob, state, backend, alive). `ListSessionsFiltered` AND-logic. `GET /api/sessions` gains `?name/state/backend/alive` query params. MCP `list_sessions` gains 5 params including `format=json`. CLI `session list` gains `--name/--state/--backend/--alive/--json`. 6 tests.

v8.10.11 shipped 2026-06-11 — BL360: Structured agent result store. `ResultStore` (put/get/list/delete/expire, optional TTL, file-backed). 4-surface parity: REST + MCP + CLI + comm. 6 tests.

v8.10.10 shipped 2026-06-11 — BL359: `restart_session` from any state. Kill live tmux + relaunch, preserve ID/Name/lineage, optional task override. 5-surface parity: REST + MCP + CLI + comm + PWA. 5 tests.

v8.10.9 shipped 2026-06-11 — BL358: Discussion push/subscribe. `DiscussionSubStore` (JSON-persisted). `handleDiscussionWrite` fires `dispatchDiscussionEntry` after each WAL write, routing content to subscribed sessions. `memory_discussion_wal` gains `after_seq`/`block`/`timeout` long-poll params. 4-surface parity: MCP (`discussion_subscribe/unsubscribe/subscriptions`) + REST (`/api/discussion-subs`) + CLI (`discussion-sub`) + comm channel. 5 tests. Also includes v8.10.4–8 (BL353–357): recurring named schedules, name-addressed session ops, `claude_alive` zombie detection, session exit hooks, durable role-based work queue.

v8.10.0 shipped 2026-06-10 — BL347: Session lineage. `parent_id` + `KillChildren` fields on Session. `caller_session_id` + `kill_children` on `start_session` MCP tool. New `session_children` and `reply_to_parent` MCP tools. REST `GET /api/sessions/{id}/children` + `?tree=1` forest view. Channel `new:` command accepts `parent=<id>` and `kill_children=true` tokens. `list` command shows `↳ child of [<id>]` for children. 17 new tests across 3 packages. Full QA gate: 38/38 PASS. Minor version release.

v8.9.25 shipped 2026-06-10 — BL342+BL343: ComputeNode CLI catch-22 fix + federation peer health alerting. `compute migrate` CLI command exposes existing migration REST endpoint. `compute node add/update` rejects deprecated kinds at CLI boundary. Background peer-health goroutine fires system alerts on peer state transitions. ZAP 10096 (Timestamp Disclosure) suppressed in `.zap/rules.tsv`. Closes GH#125. Silences recurring ZAP issues #109, #115, #116, #121, #122, #123, #124, #126.

v8.9.24 shipped 2026-06-09 — BL341: MCP session name resolution + permission_mode for autonomous handoff. `resolveSession` helper accepts session names in addition to hex IDs (with active-preference tiebreak). `start_session` tool gains `permission_mode` parameter. 12 new targeted tests.

v8.9.23 shipped 2026-06-07 — BL340: imap-mcp email command channel. New `imap_mcp` messaging backend connects to a running imap-mcp instance via SSE event stream (receive) and REST send endpoint. Acts only on `inbound.command` events that passed imap-mcp's trust gates. imap-mcp v0.2.0 adds `GET /api/events` SSE and `POST /api/accounts/{account}/messages/send`. Closes GH#127.

v8.9.22 shipped 2026-06-07 — Container CVE fix: agent-base Dockerfile pinned to `golang:1.25.11-bookworm` + `GOTOOLCHAIN=local`, resolving CVE-2026-42504 (HIGH, stdlib MIME header DoS) that blocked all agent container image builds since v8.9.17.

v8.9.21 shipped 2026-06-07 — Self-update reliability: `installPrebuiltBinary` now tries goreleaser archive first (not bare binary), uses `runtime.GOOS/GOARCH`, and updates `datawatch-channel` alongside self. `downloadChannelBinary` fixed to extract from goreleaser archive (was always trying non-existent bare binary). Both `datawatch update` CLI and auto-updater now call `downloadChannelBinary` after each update.

v8.9.20 shipped 2026-06-07 — Line-printing renderer lock: `ensureClaudeTUISetting` writes `"tui":"default"` into `<dataDir>/.claude/settings.json` at `NewManager` startup, pinning claude-code to line-printing mode against Anthropic's `tengu_pewter_brook` alt-screen server-side flag.

v8.9.19 shipped 2026-06-06 — BL336 regression fix: `WaitTypingIdle` switched from TTY `mtime` (updated by AI output) to `lastRawInputAt` (updated only by operator xterm keystrokes via `SendRawKeys`); session-to-session MCP sends now pass through immediately. 4 unit tests added.

v8.9.18 shipped 2026-06-06 — BL336 (anti-clobber typing detection + queue, v8.9.17–v8.9.18): agent messages to active sessions wait for 30 s of TTY idle before injecting; multiple messages queue in arrival order, first waits for idle, remainder flush immediately after. Go bridge port-7433 duplicate bridge fix (v8.9.15–v8.9.16): `WriteProjectMCPConfig` skipped for claude-code backends; stale `.mcp.json` `"datawatch"` entries removed on session start. JS bridge memory parity (v8.9.13–v8.9.14): `memory_remember/recall/list/forget/stats` + `callParent` helper. Go 1.25.11 (CVE fix: `net/textproto` GO-2026-5039, `crypto/x509` GO-2026-5037).

v8.9.4 shipped 2026-05-30 — dual-summary AI pipeline (v8.9.0): single LLM call → short (`last_response`) + long (`last_summary_long`) summaries, model-aware context, envelope expand on session cards. Summarizer model selector with quality hints (v8.9.1): `session.summarizer.model` config + PWA datalist from all compute nodes. Robust dual-summary parser (v8.9.2): `<think>` tag stripping, multi-format marker fallback, paragraph-split last resort; CLI `session send` Enter fix (SendKeysWithSettle). `last_response` spacing fix (v8.9.3–v8.9.4): ANSI cursor sequences → space; `CaptureResponse` switched to `tmux capture-pane -p -S -200` (rendered terminal, correct word spacing). GH#111, #112, #114 closed. All five v8.9.x tags pushed.

v8.8.6 shipped 2026-05-27 — ZAP CI fix (direct binary launch replaces KIND+Docker+Helm), fix remaining [10027] suspicious comments (`deprecated`→`superseded`/`legacy`, `Debug Console`→`Diagnostic Console`), tagline updated "AI Session Monitor"→"AI Orchestration" across splash/About/CLI banners. iOS parity standard extended: AGENT.md Mobile-Parity Rule updated, `docs/parity-status.md` created with PWA|Android|iOS table, BL335 (APNs push) filed for next minor. GH#105/#106 (ZAP issues) closed; GH#107 (iOS parity) docs addressed; GH#108 (tagline) closed.

v8.8.3 shipped 2026-05-26 — `--chrome` flag for Claude Chrome integration. Opt-in `*bool` pointer pattern across all 7 surfaces (REST, PWA checkbox, CLI, MCP `start_session`, comm `chrome=true`, claudecode.Backend.SetChrome, session.StartOptions.Chrome). `session_chrome` locale key in all 5 bundles. datawatch-app issue filed (Mobile-Parity + Localization Rule).

v8.8.0–v8.8.2 shipped 2026-05-26 — security headers (CSP/X-Frame-Options/SRI), dashboard responsiveness (heatmap narrow mode, burn-rate strip, orbital fallback, EKG/Smoke mobile), Observer eBPF network card, docs search, OpenCode provider key management, Channel Routing 6-surface parity (BL331 gap closure), Automata Type Registry 6-surface parity, PWA e2e reform Phase 0+1. Docs: Android beta join link, Wear OS private local voice correction.

v8.7.x (unreleased) — BL331 6-surface parity gap closure: Channel Routing (channel_pattern→peer_name routing) was missing PWA card, MCP tools, comm channel command, and CLI subcommand. All 4 surfaces added; 4 new locale keys in 5 bundles; unit tests for all new code.

v8.6.0 shipped 2026-05-19 (committed; publish pending) — closes BL334 T43g (servers.json, skills.json, compute/nodes.json, inference/llms.json encryption) and T43h (daemon-app.log DWLOG1 encryption + `datawatch security logs` CLI). Append-mode fix for `daemon-app.log` (logs survive restarts). TS-065 E2E assertion loosened (skill_load returns markdown string, not dict).

v8.5.0 shipped 2026-05-19 — closes BL334 T43a–T43f: channel_routing.json, discussion WAL (per-line ENC:), participants.json, startup migration, security REST endpoints + CLI + secure-wipe.

v8.4.0 shipped 2026-05-19 — closes BL332 (discussion scopes: WAL-backed federated memory, participant fan-out, conflict detect + resolve, MCP + CLI + PWA surfaces).

v8.3.0 shipped 2026-05-19 — closes BL331 (channel-address federation router, `comms-channel-agent` group), BL333 (federated file service).

v8.2.0 shipped 2026-05-19 — closes BL327 (badge/chip multi-select), BL328 (async PRD decompose + SSE), BL329 (identity POST alias), BL330 (UnifiedPush).

v8.1.0 shipped 2026-05-19 — closes BL324 (community skills + plugins registry), BL325 (plugin install from registry: `POST /api/plugins/install`, `GET /api/plugins/browse`, CLI `datawatch plugins install/browse-registry`, MCP `plugin_install`/`plugin_browse_registry`, community registry seeded first by default), BL326 (mic recording overlay with animated waveform, Cancel/Send buttons), S14b (per-pod alert rules: `internal/alertrules/` package with YAML store + observer-driven evaluator, 8 REST endpoints, 8 MCP tools, CLI `datawatch alert-rules`, wired into daemon). Community registry (`dmz006/datawatch-community`) pre-seeded as first registry in all listing surfaces.

v8.0.0 shipped 2026-05-19 — major release closing BL316 (CBAC package + federation peers + 50-capability sweep across all REST/MCP handlers), BL317 (MCP SSE federated auth + per-tool CBAC gates), BL318–BL322 (compute node routing modes: direct/docker-network/datawatch-proxy + DockerLifecycle + ProxyRouter), BL321 (gemini-api + opencode-api adapters), BL322 (7-surface parity for routing/adapters), BL323 (E2E test infrastructure + 626 test stories). Plans post-v7-routing.md and post-v7-llm-kinds.md archived to historical-plans after delivery.

v6.6.0 shipped 2026-05-04 — minor cut closing BL252 (PWA i18n full coverage across 7 phases) and BL246 (Automata UX overhaul — 4-tab detail view, persistent header toolbar exposing every PRD API verb, split Edit Spec + Settings modals, hidden-by-default per-card checkboxes with Select-mode toggle). Also collects BL247/BL249/BL250 from the v6.5.x patch series. v6.5.0 (2026-05-04) landed BL243 Phase 1 (Tailscale sidecar + headscale client + 7-surface parity); Phases 2+3 followed in v6.5.1+v6.5.2+v6.5.3. BL251 (agent auth/settings injection) shipped v6.5.4. BL241 Matrix still needs design interview before implementation. BL253 closed via v6.5.1 (eBPF setup false-positive, GH#37).

## Roadmap: self-build arc → v10.0.0 (locked 2026-10-10)

Decided through iterative design discussion 2026-10-10, starting from
BL405's single-thread backlog-execution idea and expanding into using
datawatch's own PRD/Automata engine to work through its own backlog.
**This is the authoritative execution order** — individual BL entries
below carry the design detail; this section carries the sequencing.
Update this section's stage markers as each stage completes; don't let
it go stale the way BL365's "Phase 2 next" line did (corrected
2026-10-10, see BL365 below).

**Stage 1 — build traditionally (not via PRD; bootstrapping constraint,
not a preference). Done — both items shipped.**
1. **BL406** — Configurable Project-Rules Enforcement. First, because
   everything after it benefits from being checked by it. Shipped
   v9.0.17.
2. **BL407** — PRD Git Workflow (branch-per-PRD + auto-PR on
   completion). Second; independent of BL406, no reason to parallelize
   given the one-thread-at-a-time constraint. Shipped v9.0.23 (Phase 6
   docs closure, 2026-10-11, is docs-only — no version bump).

**Stage 2 — one small, low-risk item, run for real through the new
PRD-driven path as a deliberate shakedown. Done — real run complete,
PR open pending operator merge.**
3. **BL404** — imap_mcp parity gaps. Smallest, most self-contained real
   item left; whatever BL406/BL407 need tuning on gets found here
   cheaply, per the operator's "real-world testing to validate/tune"
   instruction, before trusting the workflow with something bigger.
   **Done 2026-10-11**: real PRD, decomposed/approved/run via the new
   worktree + auto-PR workflow, backend `claude-code`. PR:
   [`datawatch#205`](https://github.com/dmz006/datawatch/pull/205)
   (open, awaiting operator merge — not auto-merged, per the plan's
   own "open for review" design). The shakedown found and fixed real
   gaps in the new workflow itself, each shipped same-day:
   **B116** (config-write propagation clobbered live-only bool
   defaults, v9.0.24), **B117** (PRD creation blocked worktree-mode
   PRDs with no work-source error, v9.0.25), **B118** (decompose
   needed worktree auto-creation too, not just `Run()`, plus
   orphan-worktree cleanup, v9.0.26), **B119**
   (`autonomous.execution_backend` had zero config surface, v9.0.26),
   **B120** (an explicit clear-to-zero on a touched `autonomous.*`
   field never reached the live manager, v9.0.27), **B121**
   (`autonomous.rules_file`/`context_file` had full config plumbing
   but zero consumers — BL406's own "decomposer reads it" claim was
   never implemented despite all 6 phases marked Done, v9.0.28). Also
   found: the per-task LLM verifier is unreliable in both directions
   (an `ok:true` task's `issues` list contained 3 factually false
   complaints, confirmed by reading the real diff) — filed as
   **BL413** (needs a genuinely new diff-aware rule type, not
   improvised). A manual audit against AGENT.md's own documentation
   checklist (closing the gap BL413 will eventually automate) found 3
   more real misses the task specs never required — CHANGELOG,
   testing-tracker, version bump — fixed by pushing directly to the
   same PR branch (v9.0.29), not a new PR.

**Stage 3 — full backlog, PRD-driven, one item at a time, confidence
established. In progress — starting with BL403.**
4. **BL403** — Plugin Extension Surfaces. Before BL405: its Phase 3/4
   widget `Kind` taxonomy is what BL405 Phase 10 consumes. **Phases 1,
   2, 3 and 4 shipped** (v9.0.31–46, PRs #206/#213/#221/#228). Phase 2's
   merge is what surfaced BL416 below. Phase 3 (widget manifest schema,
   per-widget pull scheduler with hung-pull isolation, plugin-scoped
   push auth + endpoint, disk-snapshotted latest-value store) shipped
   2026-10-12 — see B123 above for a verifier-reliability finding
   surfaced reviewing this phase's own run (no code defect; the
   implementation was correct). Phase 4 (generic PWA widget renderer —
   dashboard cards + session detail, first operator-visible surface of
   this plan) shipped 2026-10-12, PR #228, `datawatch-app#252` filed
   for Mobile-Parity. **Next: Phase 5** (federation capability gating
   for widget read/write).
5. **BL416** — Tooled, Deterministic PRD Guardrails (filed 2026-10-12,
   plan: `docs/plans/2026-10-12-tooled-deterministic-guardrails.md`).
   **Phases 0-4 shipped same day** (v9.0.34–39, PRs #214/#216/#217/
   #218/#219): AGENT.md `make lint` gate; schema-only
   `VerificationScript`/`GuardrailBackend`/`GuardrailModel`/
   `FilesGenerated` fields; decompose now instructs tooled planning
   sessions to write real per-task verification scripts (Reuse-and-
   Expand checked against `scripts/` + rules files first, ecosystem-
   convention fallback with none); guardrails now actually execute
   that script once per unit — a clean pass never calls an LLM, a
   failure's judgment is grounded in the real captured output, not a
   guess — and honor per-task/automaton guardrail backend/model
   overrides; full REST/MCP/CLI/comm parity for the new overrides,
   PWA shows planned/touched/generated files as three distinct
   groups, Mobile-Parity issue filed (`datawatch-app#251`). Phase 5
   (pure docs/cross-reference work) is the only piece left, lower
   priority, deferred — not blocking Stage 3's return to BL403.
6. **BL402** — GH#204 result panel for scheduled jobs/integrations.
   5 phases, zero open design questions, placed right after BL403 since
   its new "opt-in dashboard card" is a natural second user of BL403's
   generic widget `Kind` taxonomy once it exists.
7. **BL405** — LLM Enhancements. Phase 8c needs explicit operator
   go-ahead before that specific sub-phase runs (already flagged in the
   plan) — rest is clear to queue.
8. **Security remediation track** (BL365 + BL408 + BL395, one coherent
   program, not three separate items — see BL395's entry for the full
   **Design A → C → B → D** order), plus **BL398** (`.trivyignore` CVE
   review's one remaining real piece — a per-entry reachability trace
   for the ~69 CAVEATed suppressions, not part of the formal SEC-###/
   HLLM-### track but thematically paired here). Fully decided, zero
   open design questions (confirmed 2026-10-10 against the 2026-09-22
   walkthrough log) — implementation only. Placed after BL403/BL402/
   BL405 so this large, already-fully-sequenced body of work isn't
   interleaved against their phases.
9. **BL409** — File/function naming audit across all of datawatch
   (filed 2026-10-10, operator-raised after catching this session's own
   `bl406_*.go` filenames — a pattern confirmed pervasive and
   pre-existing across the whole codebase: `bl221_scan.go`,
   `bl369_injection_test.go`, `gh201_phase2_hopchain_test.go`, and many
   more). New AGENT.md Code Quality Rule (added 2026-10-10): name
   files/functions/types for what they do, never for the tracker ID
   that created them — IDs get collided/renumbered/reused (see this
   session's own B89–B97 and BL366/BL369 findings) and tell a future
   reader nothing. **Deliberately placed last in Stage 3**, after
   BL403/BL402/BL405/the security track: this is a cross-cutting rename
   sweep touching source files across the whole repo, so it benefits
   from everything else having already landed (less churn to re-audit,
   lower risk of colliding with in-flight phase work). **Scope, to
   settle when this item is reached, not now**: rename only source
   files/Go identifiers (functions/types) — explicitly does NOT touch
   `CHANGELOG.md`, `docs/plans/README.md`'s historical BL entries, or
   `docs/testing-tracker.md`'s past entries, all three of which are the
   existing rule's own named "reference/archaeology material where
   tracker IDs ARE expected and useful," not a gap to close. Needs real
   tooling care (e.g. `gopls rename`, not blind `sed`) given the
   codebase's size — no plan doc written yet.
10. **BL410** — Failure Observability & Conditional Auto-Retry (filed
   2026-10-10, operator-raised while reviewing BL407 Phase 0). Every
   PRD-lifecycle failure point — task execution, guardrail/gate
   verdicts, story rollups, `autonomous_prd_decompose` itself — should
   log a structured reason with evidence (not a bare string), stay
   queryable/trackable over time, and support auto-retry policy
   conditioned on failure *type* (not just a fixed count), with an eye
   toward skills improving from observed failure patterns. Existing
   partial infrastructure to build on, not replace: `Task.Error` (bare
   string, `internal/autonomous/models.go:432`),
   `GuardrailVerdict{Outcome,Severity,Summary,Issues}` (:507),
   `VerificationResult{OK,Severity,Summary,Issues,DiffSHA}` (:518), the
   generic `Decision{At,Kind,Actor,Note}` log every PRD already keeps,
   and a fixed-count retry knob (`Config.AutoFixRetries`,
   `Task.RetryCount`). Also found live while scoping this: `Learning`
   (`models.go:552`, `{ID,TaskID,PRDID,Category,Text,CreatedAt}`) and
   the `autonomous_learnings`/`memory_learnings` read-side tooling
   exist, but **nothing in the codebase ever constructs a `Learning`**
   (grep confirms zero non-test writers) — dead code in the same shape
   as B111/B112, and exactly the mechanism this ask wants revived: a
   structured place to record "why did this fail" that accumulates
   over time for skills/future decomposition to consult.

   **Designed and scoped 2026-10-12**, triggered by BL418's own live
   validation incident (a guardrail session timeout and a genuine
   AGENT.md finding both produced an identical `GuardrailVerdict{Outcome:"block"}`,
   requiring the same manual operator intervention). Plan:
   `/home/dmz/.claude/plans/staged-orbiting-forest.md` (local plan file,
   not yet moved to `docs/plans/`). Every architectural decision
   (4-category taxonomy, content auto-fix in scope not deferred, retry
   always happens in place on the same PRD — never a new PRD, 3-tier
   `RetryProfile` precedence controlling both numbers and behavior,
   infra-retry escalation after 2 same-config attempts, a deliberately
   low global retry ceiling of 5 with per-PRD/profile override for
   genuinely iterative work like CVE remediation, configurable
   auto-fix-skips-approval defaulting to skip) was asked of and answered
   by the operator directly, not picked by the planner.

   **Phase 0 shipped v9.0.50** — `FailureReason` type + `Category`
   field on `GuardrailVerdict`/`VerificationResult`, stamped `"infra"`
   at every known mechanism-failed-before-judgment-ran collapse point
   in `autonomousGuardrail`/`autonomousVerify` (`cmd/datawatch/main.go`).
   Pure observability, zero behavior change.

   **Phase 1 shipped v9.0.51** — bounded, escalating `infra`-category
   auto-retry wired into `runPerTaskGuardrails`/`runPerStoryGuardrails`/
   `runPRDCompletionRulesReview` via a new `runGuardrailWithInfraRetry`
   wrapper; 3-tier retry-policy resolution (explicit PRD > named
   `RetryProfile` > global `Config`, mirroring `resolveGuardrails`
   exactly); full `RetryProfile` CRUD (REST/MCP, mirrors
   `GuardrailProfile`) and per-Automaton override
   (`set_retry_policy`/`per_automaton_retry_policy_set`); new PWA Retry
   Profiles card. Caught and fixed one real bug while wiring this: the
   originally-proposed infra budget (2) equaled the escalation
   threshold (2), making escalation structurally unreachable — raised
   the default to 4.

   **Phase 2 shipped v9.0.52** — content auto-fix. Task-level: a
   content-category block now retries the SAME task via its existing
   `executeOne` attempt loop, fed in through the existing `RetryHint`
   field — zero new spawn machinery. Story/PRD-level: no single task
   owns a story-wide or PRD-wide finding, so a new
   `synthesizeCorrectiveWork` helper appends corrective work (a new
   task into the same story, or a new story+task) built from the
   verdict's own `Issues` — always onto the SAME `PRD.ID`, never a new
   one, per the operator's explicit "it should retry itself" decision
   — then `Run()` recurses to execute it and re-reach the same
   completion check, fully unattended. Both paths bounded by Phase 1's
   own `content`-category budget/ceiling. `resolveAutoFixSkipApproval`
   now does real work (default: skip approval entirely; off routes
   through the existing `StoryAwaitingApproval` gate). No new config/
   REST/MCP/PWA surface — reuses Phase 1's fields verbatim.

   **Phase 3 shipped v9.0.53 — BL410's last planned phase.** New
   `recordFailureLearning` finally calls `store.AddLearning` (the exact
   dead code BL411 flagged — fully plumbed read side via
   `autonomous_learnings`/`memory_learnings`, zero writers anywhere).
   Fires once, only when a unit's retry budget is genuinely exhausted
   — not on every attempt, so a transient failure that self-heals
   writes zero rows. Hooked at every terminal-failure point: `executeOne`'s
   final `TaskFailed`, task/story `TaskBlocked`/block, both PRD-level
   completion checks. Found and fixed two real bugs while wiring this:
   (1) a new `lastFailureCategory` tracker was needed because relying
   on `vr` directly at the exhaustion point would have silently
   mislabeled a stall/quality-gate-regression-caused exhaustion as
   content (those paths `continue` without ever touching `vr`); (2) a
   nil-pointer panic in several pre-existing tests that construct a
   bare `&Manager{}` without `NewManager`, fixed with an `m.store ==
   nil` guard. No new config/REST/MCP/PWA surface.

   **Known gaps, carried forward across all three phases, not silently
   dropped**: `Manager.Decompose`'s own infra/scope retry was scoped
   into Phase 1 originally but never wired (`PRD.LastDecomposeFailure`
   exists, nothing uses it); `Task.FailureReason`/`Story.FailureReason`
   remain unpopulated (the same info already lives in `Verdicts`); no
   dedicated test for `AutoFixSkipApproval=false`'s end-to-end path or
   for a stall-specifically-causing-exhaustion Learning row's
   `Category`. **BL410 is now fully shipped per its approved design**
   (Phases 0-3); any further work (the decompose-retry gap, a
   dedicated `RetryProfile` PWA create/edit form beyond the current
   list+delete card) is a new, separate decision, not an open phase.

   **Remediation pass, 2026-10-13** (after an operator audit found the
   "fully shipped" claim above was incomplete): `RetryProfile` CRUD and
   per-Automaton retry-policy overrides had REST/MCP/PWA parity from
   Phase 1 but no CLI and no comm-channel verbs — fixed (`datawatch
   retry profile ...` / `datawatch retry automaton set ...` in
   `cmd/datawatch/cli_retry.go`, mirroring `cli_guardrail.go`; `retry
   profile ...` / `retry automaton set` comm verbs in
   `internal/router/retry_profiles.go`, mirroring `guardrails.go`).
   `docs/howto/retry-policy.md` rewritten with the real CLI/comm
   sections plus "What happens on a retry" (Phase 2 mechanics) and
   "When a retry budget is finally exhausted" (Phase 3 Learning
   writer), both previously undocumented. `docs/flow/guardrail-flow.md`
   gained a "Retry, escalation, and content auto-fix" section — it had
   zero mentions of any of this despite three releases of real
   behavior layered on the flow it documents. Mobile-parity tracking
   issue filed per the Mobile-Parity Rule (datawatch-app#256).
   `release-smoke.sh` section 69 added — MCP/REST/CLI wiring coverage
   for `RetryProfile` CRUD + per-Automaton retry policy, closing what
   had been the largest remaining gap (zero smoke/E2E coverage of any
   kind). `Manager.Decompose`/`decomposeStreamingCore` wired to
   `retryDecomposeFailure` — the planning LLM call, `ParsePlanning`,
   and `validateVerificationScriptCoverage` now retry with the prior
   failure fed back as a hint (same single-use lifecycle as
   `Task.PriorAttemptHint`) before rolling back to `PRDDraft`, bounded
   by the same `infra`/`scope` budgets as everything else — closing
   the original "why was manager.decompose not wired in?" finding.
   `Task.FailureReason`/`Story.FailureReason` now populated at every
   point `recordFailureLearning` already fires (same data, structured
   instead of a bare string, additive alongside `Task.Error`).
   **Still open**: a dedicated `TS-###` behavioral E2E story (a real
   PRD driven through an actual infra-retry escalation, content-fix
   synthesis, and genuine budget-exhaustion Learning write end to
   end) — the smoke coverage above is wiring/round-trip, not that.
11. **BL411** — Pre-v10.0.0 Dead Code Audit (filed 2026-10-10, same
   conversation as BL410). This session alone found four instances of
   "fully-plumbed but never actually consulted" code in
   `internal/autonomous`: B111 (`GuidedMode` bool, fixed), B112
   (scope-drift rule, fixed), `Learning` above (unused), and
   `Story.ExecutionProfile` (`models.go:391` — settable via
   `SetStoryProfile` but never read when `executor.go` builds a task's
   `SpawnRequest`; only `PRD.ProjectProfile` is consulted, found while
   researching BL407). Scope: a systematic sweep for this exact pattern
   (exported field/type with full write-side plumbing — REST/MCP/CLI/
   comm/PWA — and zero or near-zero read-side consultation) across the
   whole codebase, not just `internal/autonomous`. Natural timing
   pairing with **BL409** (both are whole-repo hygiene sweeps best run
   late, once everything else in the arc has landed) but a separate
   number — different pattern (naming vs. liveness), raised separately.
   `Story.ExecutionProfile` itself is a concrete, already-scoped fix
   that doesn't need the full systematic sweep designed first — worth
   landing standalone whenever reached. No plan doc written yet.
12. **BL412** — Self-Config/Self-Modify Permission-Gate Generalization
   (filed 2026-10-11, operator direction after `mcp.allow_self_config`
   — the BL110/v2.4.5 gate meant to control whether an in-process AI
   can tune its own daemon config — turned out to only guard one
   specific MCP tool (generic `config_set`) plus a few BL220 tools
   (detection timing, DNS channel, proxy aggregation), not `PUT
   /api/config`, not the CLI, not `autonomous_config_set`). Operator
   wants the underlying concept generalized across **all** mutating
   surfaces — REST, MCP, CLI — for proper LLM sandboxing, modeled on
   how federation already does capability/peer-scoped access control.
   Default posture: **enabled by default** (encourage LLM-driven
   configuration out of the box), with the hardening/restriction path
   clearly documented for operators who want it tighter — the inverse
   of today's single narrow gate's closed-by-default framing. No plan
   doc yet; needs a design pass (inventory every mutation-capable
   surface, pick one consistent gating mechanism, decide the default)
   before queueing a position in the roadmap.
13. **BL413** — Diff-Aware Project-Rules (structured Phase-Completion-
   Checklist enforcement for autonomous PRD runs) (filed 2026-10-11,
   operator challenge: "we agreed nothing ships without passing all
   rule checklists — is that actually enforced for autonomous runs?").
   Audited live against BL404's real run: no mechanism generates or
   gates a PRD on AGENT.md's "Phase Completion Checklist"/"General
   documentation checklist" (CHANGELOG, `docs/testing-tracker.md`,
   Mobile-Parity, version bump, etc.). The project-rules engine
   (`internal/autonomous/scan`) already has 3 fully generic rule types
   (`content`/`consistency`/`presence`) that work against any file
   path with zero new code — confirmed live by configuring a real
   `version-sync` consistency rule (`cmd/datawatch/main.go` vs.
   `internal/server/api.go`) and independently proving it both passes
   clean against this repo and catches a deliberate mismatch, no PRD
   involved. But `presence` only checks "does a file matching this
   glob exist anywhere," never "was it touched in *this* diff" — so it
   can't express "if any `.go` file changed, `CHANGELOG.md` must also
   be in `files_touched`," which is what most of AGENT.md's checklist
   items actually need. Needs a new diff-aware rule type (or a
   `files_touched`-conditioned variant of `presence`) consulting the
   same `pre_task_sha`/`files_touched` data the executor already
   tracks per task. No plan doc yet — needs a design pass (what the
   new rule type's pattern/config shape is, which checklist items are
   safely automatable vs. need operator judgment e.g. "docs-only skips
   version bump"). Related, not duplicate: **BL406**'s own plan
   claimed ("the decomposer/task-spec generation and the new scanner
   both read whichever rules/context path is configured") was found
   live to have never been implemented despite all 6 phases marked
   Done — `autonomous.rules_file`/`context_file` had full config
   read/write plumbing but zero consumers anywhere in the codebase.
   Fixed immediately (2026-10-11, same session, not deferred): new
   `internal/autonomous/rules_context.go` injects the resolved
   rules/context file content into the decompose prompt for both
   `Decompose()`/`DecomposeStreaming()`. BL413 is the *scanner* half
   of that same original promise — a genuinely new rule-type design,
   correctly deferred rather than improvised.
14. **BL414** — PRD-Decision Cost Rollup + Token Usage Observability &
   Guardrails (filed 2026-10-11, operator-raised reviewing BL403/BL404's
   real runs: "should I see token spend on PRD decisions history?").
   Found live: `autonomous.Decision.CostUSD`
   (`internal/autonomous/models.go:57`) is a real schema field with
   zero writers anywhere in the codebase — a `decompose`/`run` Decision
   never gets the cost of the LLM call(s) it actually triggered. Real
   cost tracking *does* exist, just one level down and not rolled up:
   `internal/session`'s `EstCostUSD` (per-session running total,
   `cost_summary`/`cost_usage` MCP + `/api/cost/*` REST) is live and
   accurate for claude-code specifically (B98 fixed it — scrapes the
   real Claude Code JSONL transcript every 10s instead of the never-
   called manual-only path B98 found). A PRD's `decompose`/task-run
   Decisions have no link back to the session(s)/cost they spent.
   **Operator direction, not yet designed**: this needs to become a
   real iteration — where token usage is *reported* (PRD Decisions
   rollup is one surface, not the only one), a dashboard/observability
   story (so spend is visible without querying `cost_summary` by hand),
   and **token guardrails** (budget ceilings, alerts, maybe hard stops)
   tied to the same data. **Explicitly not a fresh design from zero**:
   `docs/plans/2026-10-10-llm-enhancements.md` (BL405) already has
   deep, adjacent work in this exact space — Phase 4 (per-call usage
   record store, richer than the session-level aggregate this gap
   would roll up from), Phase 5 (local-compute $/GPU-hour cost model),
   Phase 9 (alerting hooks already named `cost_burn_rate`/
   `token_budget_remaining`/`model_error_rate` — literally the
   "guardrail" signals being asked for here), and Phase 10 (dashboard
   cross-reference into BL403's widget surface). BL414 should be
   scoped as the PRD-lifecycle-specific slice (Decision rollup +
   whatever's missing after BL405 ships) and sequenced alongside or
   right after BL405, not as a parallel/competing design — needs an
   operator discussion pass (same shape as BL412/BL413) before a plan
   doc gets written: which of BL405's phases already cover this vs.
   what's genuinely a new gap. See also BL428 (item 29), which
   recommends absorbing this item's budget-guardrail half.
15. **BL415** — `dependency-review` required check can't pass under the
    Actions-generated `GITHUB_TOKEN` (filed 2026-10-11, found while
    merging PR #206/#212/#213). Symptom: `actions/dependency-review-
    action` fails instantly with "Dependency review is not supported on
    this repository. Please ensure that Dependency graph is enabled,"
    even after re-pinning a confirmed-dangling SHA to a valid current
    release. Ruled out as a repo-settings gap: the repo is public (GHAS
    free), `gh api repos/dmz006/datawatch` confirms
    `dependabot_security_updates`/`secret_scanning`/
    `secret_scanning_push_protection` all enabled, a direct
    `PATCH .../security_and_analysis` attempt to force-enable Advanced
    Security returns "Advanced security is always available for public
    repos," and `gh api repos/.../dependency-graph/compare/<base>...
    <head>` succeeds and returns real data **when called with a
    personal OAuth token** — the same compare call the action itself
    makes apparently fails under the ephemeral Actions `GITHUB_TOKEN`
    specifically, despite `permissions: contents: read` being granted
    in the workflow. Removed `dependency-review/review` from the
    `main-protection` ruleset's required checks (2026-10-11) so PRDs
    can actually merge — the other 5 required checks
    (`CI / test + lint`, `security-scan / govulncheck`,
    `security-scan / gosec`, `secret-scan / gitleaks`,
    `docs-sync / check`) are unaffected and still gate every merge.
    **Deferred to after v10.0.0** — needs either a GitHub support
    ticket or someone willing to bisect the Actions-token-vs-PAT
    discrepancy directly; not worth more self-build time right now.
16. **BL417** — `internal/autonomous`'s test suite has recurring,
    CI-load-only flakiness (filed 2026-10-12, found across BL416
    Phases 1-4's own PRs). *Six* different test names have now failed
    on required CI's full `go test ./...` this session —
    `TestExecutor_CancelledDependencyFailsDependent`,
    `TestRun_OpensPRAfterWorktreePRDCompletesWithAutoPR` (both already
    fixed: a too-tight deadline bumped, a real git-push race from
    reusing a pre-seeded fixture remote), `TestExecutorResume_DoubleRunSafe`,
    `TestExecutor_WorkerStallRespectsRetryBudget` (PR #222 — a
    pre-existing fixture PRD's record leaking state into a shared-ID
    collision, same package, same symptom), `TestCheckScript_Fixtures/valid`
    (PR #227, `internal/plugins` — exit 141/SIGPIPE, a broken shell
    pipe under CI resource contention, same CI-load pattern in a
    different package), and `TestScope_ExecutorBlocksOutOfScopeTaskWithoutSpawning`
    (PR #230, `internal/autonomous` — "PRD did not reach a terminal
    state"; reproduced 5/5 locally at 0.07s each, only fails under
    CI's full-suite load) — none yet investigated past confirming a
    rerun always passes. Pattern: never reproduces locally, no matter
    the `-count`; only shows up under CI's full-suite parallel load.
    **Deferred, not blocking Stage 3** — each occurrence so far has
    been resolved by a rerun; worth a dedicated pass (audit for shared
    global state / excessive goroutine contention under default
    `-parallel`, or just reduce CI's parallelism for these packages)
    rather than continuing to whack-a-mole individual test names as
    they turn up.
17. **BL418** — PRD-completion rules review (judgment, not scripted).
    Filed and shipped same day, 2026-10-12, v9.0.47. Found auditing a
    completed Automaton (BL403 Phase 4) by hand: several process-
    compliance items (version collision, backlog tracker never
    updated, stale README release line, missing CHANGELOG subsection)
    had silently never happened, and nothing in the pipeline would
    ever have caught any of it — guardrails only judge a unit against
    its own narrow spec; the existing deterministic `prd_complete`
    project-rules scan only matches a short list of hand-written regex
    rules. Operator-directed design correction mid-discussion: build
    this as a judgment review reusing the existing tool-capable
    guardrail mechanism (B124) and the existing rules-file pointer
    (`rulesContextSuffix`, generalized to completion time), NOT as a
    scripted checklist — a scripted check would hardcode this
    project's own specific rules, which breaks for any other
    operator's repo and rule set. See `internal/autonomous/rules_review.go`'s
    own doc comment for the full design. 10 new tests; `rtk go test ./...`
    3817 passed, `make lint` 0 issues, `gosec` 0 new findings. **Mobile-Parity:
    N/A** — a real, small follow-up noted in CHANGELOG: the PWA's
    existing verdict/blocked-count aggregation doesn't include the new
    PRD-level `Verdicts` field yet, not fixed since nothing asked for
    it to render yet.

    **Configurability follow-up, same day, v9.0.48**: shipped
    unconditionally-on in v9.0.47 — flagged live immediately ("these
    validation tools should be selectable and configurable by the
    operator"), since every other guardrail in this codebase is
    operator opt-in. Fixed by extending `resolveGuardrails` with a
    third `"prd"` level and gating `runPRDCompletionRulesReview` on
    `"rules-review"` appearing in it — new `Config.PerPRDGuardrails`/
    `PRD.PerPRDGuardrails`, off by default, same precedence chain
    (explicit > profile > global) as `PerTaskGuardrails`/
    `PerStoryGuardrails`. Full REST/MCP/config/comm parity mirrored
    from those two fields exactly. Still owes a genuine live-PRD
    validation run (opt in on a real PRD, confirm the review actually
    fires and records a verdict) — not yet performed as of v9.0.48.

18. **BL419** — `kind-smoke` CI job was silently testing nothing +
    production docs-index build bug (filed + shipped 2026-10-13, same
    day). Not roadmap work — surfaced directly from an operator
    question ("why am I getting so many PR failure notices") about
    this session's own BL410 remediation PRs, then expanded
    considerably once actually tested rather than assumed fixed. Root
    causes, each confirmed live before being called fixed:
    1. `kind-smoke.yaml` never ran `make sync-docs` on the host
       checkout before invoking `release-smoke.sh`, so its own
       static-lint section failed on every PR regardless of content —
       fixed with a `sync-docs` step mirroring `ci.yaml`'s identical one.
    2. Once that was fixed, `DW_BASE` — the env var the job sets
       intending to point `release-smoke.sh` at the daemon already
       running inside the kind cluster — turned out to be dead
       configuration the script never read; it always spun up its own
       separate local daemon, so the job never actually exercised the
       deployed chart/image at all. Built real external-daemon mode
       into `release-smoke.sh` (`DW_BASE`/`DW_BASE_TOKEN`/
       `DW_BASE_INSECURE`), then — at the operator's explicit direction
       after asking "did you actually test with an E2E test, not just
       smoke" — the identical mechanism into `run-tests.sh`, replacing
       an operator-facing doc section this session had itself just
       written inaccurately (claimed `--no-daemon`/`TEST_BASE` already
       supported this; testing it live proved it didn't — `TEST_PORT`/
       `TEST_TLS_PORT` are only honored when the given port is
       currently free, which an already-running daemon's never is).
    3. Fixing the CLI's TLS trust for external-daemon mode
       (`daemonClient()`, `DW_BASE_INSECURE`) initially regressed the
       script's own default local-daemon mode (12 CLI checks failed) —
       caught by re-running the full suite rather than assuming the
       fix was safe; corrected by gating on `cfgPath == ""` specifically
       instead of `serverURL != ""` alone.
    4. Running real external-daemon-mode smoke for the first time ever
       surfaced two genuine, previously-latent bugs no prior run could
       have hit (every prior run only ever exercised a local test
       daemon with memory hardcoded on): a hard `FAIL` where a memory-
       disabled daemon should `SKIP` (`release-smoke.sh`'s mempalace
       section, `release-smoke-wakeup.sh`'s skip/fail conflation), and
       — the most consequential finding — **every production container
       image built from `Dockerfile.agent-base` has shipped with an
       empty docs-search index**, because the Dockerfile only ever
       stubbed a `{}` placeholder to satisfy the `//go:embed` directive
       and `docs/` was excluded from the build context entirely via
       `.dockerignore`. Fixed (`make sync-docs docs-index` for real,
       `docs/` un-ignored) and verified with an actual `docker build` +
       extracted binary: 3399 chunks / 186 paths indexed, 59 real howto
       refs returned via `docs_list_howtos` where it previously
       returned 0.
    New howto `docs/howto/testing-against-a-running-daemon.md`
    documents the one shared `DW_BASE` mechanism both scripts now use,
    including an explicit correction of its own first-draft inaccuracy
    about `run-tests.sh`.

    **Root-caused and fixed, v9.0.60**: the `opencode-acp` state-
    detection timeout was root-caused — `Dockerfile.agent-base`
    deliberately ships with no LLM agent binary at all (by design),
    so `/api/sessions/start` still returned a real session id (the
    daemon doesn't check binary availability before creating the
    tmux pane), the pane's command failed to exec, and the watcher
    correctly never saw a real prompt — reported as a false "watcher
    regression?" against an environment that structurally could never
    pass it. Fixed by probing `/api/backends`' own `available` flag
    before attempting the session. This was `kind-smoke`'s exact last
    remaining failure (150 pass/1 fail/49 skip on the real CI run
    right before this fix).

    **Actually root-caused, v9.0.61**: the v9.0.60 fix correctly let
    the check run (opencode genuinely is available there), which
    surfaced the REAL remaining issue: `state=complete after 40s`,
    not the `state=running` symptom this check guards against. The
    idle task legitimately completed outright (`Running → Complete`
    directly, by design — confirmed via
    `TestBL266_ACP_SessionCompletedTransitionsToComplete`) without
    ever needing `WaitingInput`. Accepted `complete` as a pass
    alongside `waiting_input` — reaching any real terminal state
    (not stuck forever in `Running`) is itself proof the watcher
    works. **Confirmed**: PR #241 landed fully green (15/15 checks,
    including `kind-smoke` and `review`), merged into `main`
    (`fedef963`), production daemon redeployed to v9.0.61 via
    `make install` (the plain `go build` in the repo root does not
    touch the installed binary systemd actually runs — caught live
    when a first `restart` silently kept serving the stale v9.0.53).

19. **BL420** — session-tracker git repo growth OOM-kills the whole
    daemon (filed + mitigated 2026-10-13, same day). Not roadmap work
    — surfaced while starting the first PRD-driven dev session under
    the new "build the backlog in PRDs" operating model: the daemon
    OOM-killed itself twice in ~90 minutes (`systemd`: "107.4G memory
    peak" / "106.9G memory peak", `Failed with result 'oom-kill'`,
    restart counter climbing — a crash loop, since each restart
    re-triggers the same commit on the same bloated repo). Root cause,
    confirmed live: `internal/session/tracker.go`'s commit path
    (`git add -A` then `git commit --allow-empty` on every tracked
    session event, ~line 598-609) has no exclusion for `output.log`
    (the live, ever-growing claude-code output stream) and no repo-
    size management at init (~line 32-42). For a long-lived session
    this produces many large loose blobs; git's default auto-gc
    threshold eventually trips mid-commit, and the resulting
    `git repack -d -l` -> `git pack-objects --all --reflog
    --unpacked --incremental` has to delta-compress those blobs — an
    operation whose memory cost scales with blob size and isn't
    capped by default, consuming all available RAM+swap and OOM-
    killing the entire `datawatch.service` cgroup (confirmed via
    `/proc/<pid>/cgroup`), not just the git subprocess. Worst offender:
    `~/.datawatch/sessions/johnnyjohnny-ab22/` at 30GB/.git (3488
    commits) — a long-lived session never cleaned up; also found
    `johnnyjohnny-9245` (this session) at 8.7GB and `johnnyjohnny-ddeb`
    at 815MB, out of 322 session repos total, all with git's implicit
    default gc threshold still in effect.
    **Immediate stopgap applied directly** (the daemon needed to
    survive long enough to run the real fix as a PRD): killed the
    runaway `pack-objects`/`repack` tree, confirmed no leaked temp
    pack file, set `gc.auto=0` across all 322 session repos (non-
    destructive — stops the implicit re-trigger, doesn't touch
    history), and dropped `autonomous.max_parallel_tasks` to 1 per
    the operator's new single-task-at-a-time constraint.
    **Real fix tracked as a dev PRD** (first one run under the new
    "all datawatch dev work goes through PRDs" directive — see
    `feedback_dev_work_via_prds` in memory): stop tracking/rotate
    `output.log` out of the commit path, bound any future repo
    maintenance by memory, and shrink the existing bloated repos
    outside the daemon's own cgroup so a retry can't repeat today's
    OOM. Uses `claude-code` for decompose/execution/verification per
    the operator's explicit instruction for dev PRDs.
    **Shipped v9.0.83 (2026-10-14)**: tracker fix (`54954c5f`, in
    binaries since v9.0.62), shrink script, repro harness, docs; the
    live daemon was redeployed and observed (see `docs/testing.md`).
    **What the code fix does**: `output.log` / `.enc` / `.pipe` stay on
    disk but untracked and in `.git/info/exclude` (legacy repos get
    `git rm --cached` on first commit after restart), and tracker repos
    set `gc.auto=0`, `gc.autoDetach=false`, `pack.windowMemory=64m`,
    `pack.threads=1`, `core.bigFileThreshold=16m` at init and resume, so
    a daemon commit can never start a repack.
    **Repro harness** (`scripts/repro-tracker-git-growth.sh`, 120
    commits × 512 KB): legacy `.git` 2.83 GB vs fixed 2.4 MB (~1,177×),
    max commit RSS 65 MB vs 6 MB, auto-gc fired only in legacy —
    `RESULT: PASS`.
    **Live remediation done 2026-10-14 (v9.0.84)** with
    `scripts/shrink-session-repo.sh --allow-active` (all three were
    `waiting_input` but idle: no commit in the last 2 min, no
    `index.lock`), each in its own `systemd-run --user` scope
    (`MemoryMax=4G`, no swap), after a `--all-hygiene` sweep made
    `gc.auto=0` + excludes durable on all 547 session repos:

    | Repo | Commits | `.git` before | `.git` after | Peak memory (scope `memory.peak`) | Run time |
    |------|---------|---------------|--------------|-----------------------------------|----------|
    | `ddeb` | 660 | 822 MB | 884 KB | 32.9 MiB | 7 s |
    | `ab22` | 3642 | 31 GB | 3.3 MB | 274.3 MiB | 38 s |
    | `9245` | 2121 | 8.7 GB | 2.1 MB | 102.1 MiB | 20 s |

    All three: `fsck` clean, 0 commits touch `output.log`, timeline.md /
    conversation.md commit counts unchanged, working-tree `output.log`
    same size and inode. Daemon untouched throughout (same MainPID,
    `NRestarts=0`, memory 623–649 MiB, 0 OOM lines; host available
    memory never below 94 GB). Disk used 622 GB → 583 GB.
    **BL420 closed.**
20. **BL421** — PRD lifecycle can't recover a blocked/cancelled task's
    spec without a full re-decompose (filed + shipped 2026-10-13, same
    day — **shipped v9.0.62**).
    Not roadmap work — surfaced live while trying to fix BL420's own
    reproduction-harness task, which `decompose` had written with a
    literal `/usr/bin/time` path in its spec prose; the scope-linter
    flags any absolute-path-looking string against the PRD's allowed
    dirs regardless of whether it's a real read/write claim or just a
    prose mention of an already-installed system binary, so the task
    blocked immediately on spawn. `autonomous_prd_edit_task` only
    accepts PRD status `needs_review`/`revisions_asked`/`cancelled`,
    and the only path back from `blocked` is `cancel` ->
    `reset_to_draft` -> `decompose`, which (per its own tool
    description) regenerates the whole story/task tree — risking the
    bookkeeping record of already-completed sibling tasks, even though
    the actual code commit is safe in git regardless. Also confirmed
    `autonomous_prd_cancel` silently killed one ephemeral session
    (`killed_sessions:1`, no ID given) — verified directly it wasn't
    `ab22`/`9245`/`ddeb` before proceeding, but the response should
    name what it killed rather than make the operator verify by hand.
    Scope for the fix: (1) allow `autonomous_prd_edit_task` from
    `blocked` status too, so a blocked task's spec can be corrected
    and `reset_task`'d in place with no cancel/decompose round trip;
    (2) teach the scope-linter to distinguish "this task needs to
    read/write under `/x/y`" from "this task invokes the already-
    installed binary at `/x/y`" — at minimum whitelist standard system
    bin dirs (`/usr/bin`, `/bin`, `/usr/local/bin`) for read/execute
    references; (3) update decompose's own prompt/guidance to never
    hardcode an absolute system-binary path in a task spec — reference
    commands by name, resolved via `$PATH`/`command -v`; (4)
    `autonomous_prd_cancel`'s response should name the killed session
    ID(s), not just a count. Uses `claude-code` for decompose/
    execution/verification per the standing dev-PRD directive. BL420
    itself is paused (not abandoned — task 1's `tracker.go` fix
    already shipped/committed; task 2's spec is already corrected and
    waiting) until this ships and the daemon's redeployed.
21. **BL441** — PRD-completion content-auto-fix never fires; PRD sits
    silently `running` forever (filed 2026-10-14, live incident on PRD
    `5d599cef`/BL440). Confirmed via daemon logs, not inferred: a
    `rules-review` guardrail session (`659e`) finished at 21:20:32 with
    a genuine `content`-category block (real issues: version-bump
    discipline violated across a multi-commit feature, missing commit
    tokens, Section C release-smoke/gosec/dep-audit skipped, a
    live-smoke gap for new REST/PWA surface). From that point on,
    nothing happened — no `synthesizeCorrectiveWork` call, no new
    story/task appended, no transition to `blocked`, no retry. All of
    BL440's real tasks were already `completed`; the PRD just sat at
    `status: running` indefinitely with zero active sessions. Operator
    caught it live and asked for the matching code fix to be tracked,
    not just worked around by hand. Per BL410 Phases 0-3 (shipped,
    `docs/plans/README.md`'s own BL410 entry above), a PRD-level
    content-category block is exactly the case `synthesizeCorrectiveWork`
    was built for — task-level content auto-fix reuses the existing
    `RetryHint` loop and is a different code path, not confirmed broken
    by this incident; the gap looks specific to the PRD-completion
    rollup path (`runPRDCompletionRulesReview`/
    `runPRDCompletionProjectRulesCheck` in `internal/autonomous`). Scope
    for the fix (not yet designed): (1) reproduce directly — a fake
    PRD-completion guardrail returning a `content` block with real
    `Issues`, assert `synthesizeCorrectiveWork` is actually invoked; (2)
    if the call site is simply missing, wire it in following BL410's
    own design; (3) regardless of root cause, add a watchdog/timeout so
    a PRD can never again sit silently `running` with zero active
    tasks/sessions indefinitely — either auto-retry the completion
    check after N minutes or transition to `blocked` so the operator
    (or BL431's future decision-assistant) sees it needs attention.
    Explicitly named by the operator as a live worked example for
    BL431's decision-assistant hook points — see that item's "Local-
    model decision assistant" section.

    **Shipped, v9.0.62** (all four scope items): (1) `EditTaskSpec`
    accepts a `blocked` PRD for its failed/blocked task only, completed
    task specs immutable in every status — edit -> `reset_task` -> rerun
    verified live; (2) scope linter skips names directly inside
    `/bin`,`/sbin`,`/usr/bin`,`/usr/sbin`,`/usr/local/bin`,
    `/opt/homebrew/bin` unless a write verb precedes them (`/etc/passwd`
    etc. still flagged); (3) both planning prompts steer to command
    names via `$PATH`/`command -v`; (4) cancel/delete returns additive
    `killed_session_ids`. Note: a single-story PRD whose only task fails
    rolls up to `failed`, not `blocked`, and `failed` still isn't an
    editable status — out of this item's scope, not changed.

    **Known residual gap, found live 2026-10-13 running BL422's own
    spec-edit (not yet fixed in code):** the scope-linter's binary-
    mention whitelist (shipped item (2) above) only skips a path naming
    a *specific binary inside* an exec dir (e.g. `/usr/bin/time`) — it
    does not skip a bare mention of the exec *directory itself* with no
    filename after it (e.g. "don't hardcode `/usr/bin` or `/bin`
    paths"), which is exactly how item (3)'s own new guidance tends to
    get phrased. Worked around in BL422's task spec by rewriting the
    sentence to drop the literal path entirely rather than fixing
    `isBinaryMention` itself. BL421 is already shipped/closed, so this
    isn't being reopened for it — tracked here so it isn't lost, to be
    picked up as a small follow-up (a one-line widening of
    `isBinaryMention` in `internal/autonomous/scope.go`) whenever that
    file is next touched, or on its own if it starts recurring.
21. **BL422** — Claude model/effort tiering eval for dev-PRD cost,
    covering all three LLM touchpoints (filed 2026-10-13, expanded same
    day after operator review, prep only — NOT started). Not roadmap
    work — surfaced from an operator question while running BL420:
    every dev PRD under the "all dev work via PRDs" directive currently
    pins `claude-code` at a single model/effort for decompose, every
    task's execution, and verification alike, even though task
    difficulty within one PRD varies a lot (e.g. BL420's task 1 —
    reading/understanding a concurrency-sensitive Go file and writing a
    correct fix — versus task 2, a mostly-mechanical shell script).
    `autonomous_prd_set_task_llm`/`_set_story_llm`/`_set_llm` already
    exist for per-unit execution/decompose overrides; this PRD is just
    the actual test harness + results, not a new mechanism — except for
    verification, which has **no** per-task/per-PRD override today
    (`autonomous_config_set`'s `verification_backend`/`verification_effort`
    are global-only), so that touchpoint's test deliberately exercises a
    transient, guarded global-config flip rather than a real override.
    Goal: build a small, repeatable eval PRD that tests all **three**
    LLM touchpoints a dev PRD uses — execution, decompose/planning, and
    verification — across a full model/effort grid: 4 Claude models
    (opus, sonnet, haiku, fable) × 3 effort levels (low, medium, high;
    xhigh/max deliberately excluded to bound scope) = 12 tiers.
    Execution and decompose each run the full 12-tier grid (execution
    against 4 representative task types — read-heavy/analysis,
    mostly-mechanical scripting, doc-only edits, verification/review —
    using BL420's real fixed commits as fixtures; decompose scored
    against BL420's own real spec/structure as ground truth via an
    objective rubric, cheap since it never executes a task).
    Verification runs only a deliberately reduced tier set (e.g.
    `default,haiku-low,opus-high`), via a strict capture → set → run-one
    → restore → independently-reverify pattern guarded by bash traps
    (EXIT/ERR/INT) so a crash mid-run can never leave global config
    changed — the only part of this PRD permitted to touch live global
    config, and only transiently/reversibly. Records pass/fail + wall
    time + cost per tier per touchpoint, and reports — separately for
    each touchpoint — where a cheaper tier is safe to default to for
    dev-PRD work vs. where Sonnet (or higher) should stay required.
    Explicitly scoped to Claude model tiers only for now — **not** a
    Claude-vs-open-model comparison. A separate future rerun comparing
    against Qwen/other open models via the existing Ollama/OpenWebUI
    backends is a known follow-up the operator wants eventually, but is
    out of scope here; note it as a forward pointer only, don't build it
    now. **Explicitly NOT approved or run yet** — prep only, decompose +
    review, hold for a later go-ahead.

**BL443** — `goose`/`aider` are declared, recognized LLM registry kinds
    (session-spawn code already special-cases both — `internal/session/
    manager.go:5507,5530`) but have **no inference adapter implemented**:
    `llm_test` on a freshly-registered `goose-johnnyjohnny`/
    `aider-johnnyjohnny` entry fails with `no adapter for kind "goose"`/
    `"aider"` (registered adapters today: ollama, openwebui, opencode,
    claude, gemini-api, opencode-api — confirmed live 2026-10-14 while
    setting up BL437's eval drivers). This blocks the lightweight
    ask/verify/probe path for both backends; **not yet confirmed** whether
    it also blocks real autonomous-task session execution (a separate,
    already-existing TUI session-spawn code path) — that needs a live task
    run to test, not yet done. Also found live: `llm_add` on the literal
    name `goose` returns `HTTP 409 llm already exists`, but `llm_get`/
    `llm_list` show no such entry anywhere (`llm_add goose-johnnyjohnny`
    worked fine) — a real name-collision/stale-index bug, not reproduced
    for `aider`. Scope for the fix (not yet designed): (1) implement a real
    adapter for `goose`/`aider` kinds (or confirm their task-execution path
    doesn't need one and narrow `llm_test`'s error to say so instead of a
    bare 502); (2) reproduce and fix the `goose`-name 409-vs-404
    inconsistency. Both are currently registered as `goose-johnnyjohnny`/
    `aider-johnnyjohnny` (BL437/eval-driver tags) pending this fix.

    **Item (1) shipped same day, v9.0.88.** Root cause turned out to be
    exactly the misleading-message case named above, not a missing
    adapter — `goose`/`aider`/`claude-code`/etc. are intentionally
    session-backend kinds (#46) with no request/response ask path by
    design; `llm_test` just didn't check that before falling through to
    `dispatcher.Call`'s generic 502. Fixed in `handleLLMTest`
    (`internal/server/inference.go`): now returns a clear `400` naming
    the kind and explaining it has no ask/probe adapter by design,
    pointing at running a real task as the actual verification path.
    Tests: `internal/server/bl443_llm_test_session_backend_test.go`
    (unit only — **not yet validated live** against a rebuilt/restarted
    daemon). **Still open**: (2) the `goose`-name 409-vs-404 registry
    inconsistency (not reproduced for `aider`), and whether the real
    autonomous-task session-spawn path for `goose`/`aider` (separate from
    this ask-adapter fix) actually works end-to-end — needs a live task
    run through one of them to confirm, not yet done.

    **Live goose probe run 2026-10-14** (manual `start_session`, not the
    real autonomous one-shot path — see caveat below): confirms the
    mechanics genuinely work. A real `goose session` launched against
    `ollama qwen3:1.7b`, read a file, ran real shell commands, and
    committed to its own git repo. Two real friction points found along
    the way, not blocking: (a) a transient `.git/index.lock` race on the
    first commit attempt (self-resolved on retry — cause not isolated,
    possibly goose's own background indexing colliding with its explicit
    shell-run git commands); (b) goose auto-added its own init files
    (`.mcp.json`, a skills dir) into the task's working directory and
    staged them, worth a hygiene check later. The smallest model
    (`qwen3:1.7b`) did not retry on its own after the git error — it
    explained the problem correctly in prose but only wrote
    `DATAWATCH_COMPLETE` into a todo-list item instead of outputting it,
    needing one explicit nudge to actually finish. **Caveat, not yet
    closed**: `start_session` is an interactive launch, not
    `OneShot=true` — the session's state stayed `waiting_input` even
    after the real `DATAWATCH_COMPLETE:` line was printed, confirming
    the automatic one-shot task-delivery-and-completion-detection path
    (`isTUITask` in `cmd/datawatch/main.go`) was never actually exercised
    by this probe. That's the one piece still unconfirmed for real
    autonomous PRD task execution through goose/aider — needs a real
    task spawned through the autonomous system itself, not a manual
    probe, to close out.
    **Critical finding 2026-10-14, changes BL437's viability for these two
    drivers**: datawatch currently has **no real model-selection control**
    over either backend. `goose`'s injection path exists
    (`internal/session/manager.go:1972,3492`, `SetProvider`/`SetModel`
    from `m.cfg.Goose.Provider`/`.Model`) but the **global** config it
    reads from is empty/unset (`goose.model: "", goose.provider: ""`,
    confirmed live via `config get goose`) — the injection is a no-op
    today, so goose runs on whatever its own local
    `~/.config/goose/config.yaml` happens to default to, independent of
    datawatch. `aider` has **no injection code of any kind** anywhere in
    `manager.go` — its only special-casing is a usage-tracking goroutine
    (line 3536), nothing about model selection at all. Setting a `model`
    field on the per-LLM registry entry (`goose-johnnyjohnny`/
    `aider-johnnyjohnny`, via `llm_update`) does **not** feed this path —
    confirmed by reading the actual injection code, which reads the
    global `Goose` config block, not the per-registry-entry field. This
    means BL437's test matrix cannot currently produce valid comparative
    data for goose/aider cells specifically: a cell claiming to test
    model X may actually run against whatever goose/aider's own local
    config defaults to, regardless of the cell's own spec. **Scope for
    the fix (not yet designed)**: (1) wire the global `goose.model`/
    `.provider` config to actually be set per-task (likely via the same
    per-task-LLM-override mechanism other backends use, not a single
    global value, since different cells need different models); (2) add
    equivalent model-selection injection for `aider` from scratch — it
    has none today; (3) re-verify goose's injection path actually works
    once wired, live, not assumed. **This blocks BL437's goose/aider rows
    specifically until fixed — Claude-backend rows are unaffected.**
    **Forward pointer:** **BL437** (item 31) extends this eval into a recurring, hardware-aware monthly sweep.

**BL444** — MCP SSE listener bind failure at boot is never retried and
invisible in `/api/health` (filed 2026-10-14, external, trusted tester
Cairn-works, GH#210). If `mcp.sse_host` names an address not yet assigned
when the daemon starts (e.g. a Tailscale IP before `tailscaled` is up
after reboot), the SSE listener fails once, permanently, with no retry —
confirmed live on a real instance (`bind: cannot assign requested
address`). Checked for existing coverage first (operator asked to be
sure before filing): no generic listen-retry helper exists anywhere in
the codebase (the only "retry" config found, `mcp.max_retries`, is for
per-session channel bridges — unrelated). Confirmed in code that
`/api/health`'s `mcp` subsystem status (`internal/server/api.go:2440`)
is a static wiring check (`mcpDocsFunc != nil`), blind to whether the
listener actually bound — so this failure mode is real and currently
invisible, exactly as reported. **Half of GH#210 (the "visible in
health" part) overlaps with SEC-002's already-decided "per-listener auth
flag in health/diagnose" — fold that half in there, not here.** This
item is scoped to just the genuinely new half: retry the SSE bind with
backoff (mirroring how the daemon already retries elsewhere), and surface
a real bind-failure flag in `/api/health`'s `mcp` subsystem distinct from
"disabled". No plan doc yet.

**BL445** — review and merge GH#208 (external PR, Cairn-works), `fix(mcp):
add mcp.public_url` (closes GH#207). Verified the premise is real, not
duplicated: `internal/mcp/server.go` had zero references to any
public-URL override before this PR, and the existing `server.public_url`
field serves an unrelated purpose (K8s worker→parent callback
resolution, not MCP SSE endpoint advertisement) — confirmed by reading
its own doc comment and every call site. The MCP SSE server runs on a
genuinely separate listener (`mcp.sse_host`/`sse_port`, own TLS flag),
so it structurally needs its own override, not a reuse of
`server.public_url`. PR mirrors the existing pattern exactly (235
additions / 9 deletions across config, server, tests, all 5 PWA
locales). No CI reported yet on the PR branch — run the full suite and a
real reverse-proxy smoke test before merging, don't merge on diff-read
alone.

**BL446** — federation as a sibling-agent mail transport: read-back
delivery receipts, change-wins per-seat read state, writer provenance,
wake-on-arrival, per-seat paging of large stores, multi-aspect identity
handoff (filed 2026-10-14, external, trusted tester Cairn-works, GH#211,
large feature request — 6 distinct asks, the submitter's own identity-
handoff half alone cites "13 principles, 12 implementation
requirements"). Checked for existing coverage first: BL357 (durable
role-based work queue) and BL358 (discussion push/subscribe) are real
building blocks already shipped, but neither implements verified
read-back receipts, change-wins merge semantics, or writer provenance —
confirmed by reading both features' actual shipped scope. Genuine,
substantial new design surface, not a duplicate. Scope: a design pass
first (not implementation) explicitly building on BL357/BL358 rather
than inventing a parallel messaging layer, deferred to post-v10.0.0 given
its size — do not let this compete with the v10.0.0 arc's own sequencing.
No plan doc yet.

**Tracker note (2026-10-15):** BL447–BL450 were filed and run as
autonomous PRDs but never registered in this tracker. The gap was found
while debugging BL451. The entries below were reconstructed from git
history, CHANGELOG.md and plan docs. Anything the repo doesn't record is
marked as unconfirmed.

**BL447**: per-task `ProjectDir` override plus `set_task_project_dir`.
A task can name its own project directory, which takes precedence over
the Automaton's; an empty value inherits the Automaton's. Write dirs are
scoped to the task, per-task sites are rewired to the resolved task
directory, and the setter is exposed on REST, MCP, CLI, comm channels and
the PWA (filed 2026-10-14). **Shipped v9.1.0.** Commits `2e7934d8`,
`025f6b4e`, `32ae4b6b`. No dedicated plan doc. See CHANGELOG v9.1.0.

**BL448**: unconfirmed. No commit, CHANGELOG entry or plan doc in the
repo references BL448. It was filed as an autonomous PRD (see tracker
note), but its scope and status cannot be confirmed from the repo. Fill
this in from the PRD record.

**BL449**: open-model eval matrix (BL437 S5). A 16-cell matrix of open
models run through the Automaton system, reusing the model-tiering fixtures.
It includes a driver script (`scripts/eval_open_model_matrix.py`:
preflight/setup/create-prd/run/collect/report/self-test/cleanup) and
fixtures in `testdata/eval/open-model-matrix/` (filed 2026-10-15). Plan:
[2026-10-15-bl449-open-model-matrix.md](2026-10-15-bl449-open-model-matrix.md).
**In progress:** the PRD (`9e7bad8e`) was unstuck after the BL451 race and
is `running` again. Commits: `3acd24c1`, `ce9d7b94` (fixtures + driver);
`fe6ab76d` (preflight `llm_test` timeout raised to 240 s); **v9.1.3**
`f9eb5970` (opencode ask adapter strips `ollama/`; table test added later);
**v9.1.4** `f0c80d2e` (session start normalises the opencode model for
Ollama nodes).

Runs, all under `docs/testing/runs/2026-10-15-open-model-matrix/`, each
overwriting the last:

- r1 `bfafeb30`: preflight failed, `gpt-oss:120b` timeout.
- r3 `84ab59e4`: preflight failed, `granite4.2:30b` cold-load timeout.
- r4 `2c65e850`: preflight passed, but the run was invalid because every
  opencode worker died with `ProviderModelNotFoundError`.
- r5 `6cf10468`: preflight failed with an `ollama/` prefix 404, which led
  to v9.1.3.
- r6 `78dfd70f`: preflight 4/4 OK, but `run_verdict =
  invalid-infrastructure`. The driver pinned the bare model, so
  `opencode.json` had no `provider.ollama` block. No cell did model work,
  which led to v9.1.4.
- r8 `3382fcda` (Automaton `d07570dd`, on v9.1.4): preflight 4/4 OK, but
  invalid for scoring. 16/16 cells are suspect because session records
  were evicted before `collect`. fast/mid workers still died 3–7 s after
  spawn. cpu ran and wrote placeholder identities. gpt-oss:120b ran but
  passed no verifier. A false-positive file-drift block on
  decompose-validate-review needed a `reset_task`.

**Still open:**

- a valid run on v9.1.4+ (spec steps 3–5 have no valid data yet);
- an opencode-path preflight;
- the one-shot wrapper must not emit `DATAWATCH_COMPLETE` after a failed
  `opencode run`;
- `collect` must resolve sessions by id;
- a unit test for the v9.1.4 normalisation;
- the node-a fast/mid 3–7 s opencode deaths, which survive v9.1.4 (r8);
- the file-drift guard must not block on spec-required commits (r8).

Scope note: `b7f59ed6` (Claude-driver per-task ProjectDir) is a BL447
follow-up from the same session, outside BL449's spec, and needs operator
acceptance. See the plan's "Scope notes".

**BL450**: unconfirmed. No commit, CHANGELOG entry or plan doc in the
repo references BL450. It was filed as an autonomous PRD (see tracker
note), but its scope and status cannot be confirmed from the repo.

**BL451**: completion-rollup lost-update race. A `reset_task` landed
while the PRD-completion ci-watch check was in flight. About ten seconds
later the rollup wrote `completed` over the reset and discarded it,
leaving BL449's PRD terminal with a pending task (filed + fixed
2026-10-15). Fix: add a `PRD.Rev` optimistic-concurrency field and
`Store.CommitPRDIfRev`. Every terminal rollup status now commits only if
the revision is unchanged, the status is still the one the rollup saw,
and all tasks are still terminal, all checked under the store lock. A
stale commit records `rollup_aborted_stale` and changes nothing.
Completion side effects fire only after the commit succeeds. Follow-up:
`reset_task force=true` on a pending task in a terminal PRD reopens the
PRD (`recover_pending_task`). **Shipped v9.1.1 (race fix) and v9.1.2
(recovery).** Phase 3, unsticking BL449 live, is done: PRD `9e7bad8e` is
`running` again. Commits `fd7bccbb`, `9b76fb4d`, `8d5c20ab`. Plan:
[2026-10-15-bl451-rollup-race.md](2026-10-15-bl451-rollup-race.md).
22. **BL423** — an Automaton reports `completed` while the CI run for
    the commit it pushed is failing (filed + shipped 2026-10-13, same
    day — **shipped v9.0.63**). Not roadmap work — found live right
    after BL421: the v9.0.62 release work rolled up to `completed`, and
    only afterwards did the tag's own `Release` workflow
    (run 38484929685, https://github.com/dmz006/datawatch/actions/runs/38484929685)
    fail in `containers / agent-base` on container vulnerability /
    `golang.org/x/net` / Go stdlib scan findings. Nothing in the
    Automaton noticed; the operator had to find the red run by hand.
    Root cause: both completion checks (the deterministic project-rules
    check and BL418's `rules-review`) only judge the local diff — no
    completion path ever looks at the remote CI result for the commit
    the Automaton itself pushed, so "completed" meant "local work
    finished", not "CI agreed".
    **Shipped, v9.0.63:** new opt-in `ci-watch` PRD-level guardrail
    (`internal/autonomous/ci_watch.go`), resolved through the existing
    `per_prd_guardrails` precedence (PRD list > guardrail profile >
    global), runs after `rules-review` in the completion rollup. If the
    final HEAD is on a github.com origin's remote-tracking ref and `gh`
    is on `$PATH`, it polls that commit's runs up to
    `autonomous.ci_watch_timeout_seconds` (default 1500); otherwise it
    records `skipped`. All green → `pass`; CI-machinery failures
    (cancelled/timed_out/startup_failure/stale) → re-run via the infra
    retry budget, then `failed_infra`; real failures → judged by the
    existing `GuardrailFn` against the project's rules/context file and
    the real logs, mapped like `rules-review` (`policy` →
    `failed_policy` + Blocked, never auto-fixed; content →
    `failed_content` + BL410 Phase 2 corrective work on the same PRD);
    timeout → `pending`, Automaton completes, one follow-up re-check.
    Every outcome sets `PRD.CIWatch` (`ci_watch` on the API record) and
    appends a `ci_watch` decision; PWA detail card; full config parity
    for the timeout key; howto `docs/howto/ci-watch-guardrail.md`.
    Known limitation: worktree/auto-PR Automata push after completion,
    so ci-watch skips them (documented, push path unchanged).
    **Verification evidence:**
    `docs/plans/2026-10-13-ci-watch-live-verification.md` (template-fit
    finding, release record) plus `internal/autonomous/ci_watch_test.go`.
    **Out of scope** (explicitly not done here): fixing the specific
    container vulnerability / `golang.org/x/net` / Go stdlib findings
    that triggered this (security/dependency failures stay operator
    decisions); non-GitHub CI providers; backfilling `ci_watch` onto
    existing Automata; CI watching independent of an Automaton's
    completion; building a new template system (templates can't carry
    guardrail settings today — see the verification doc's "Template
    fit" section).
23. **BL424** — Comprehensive operator guide to integrating a repo with
    the automated-PRD system end to end (filed 2026-10-13, prep only —
    NOT started). Not roadmap work — broader than any one capability's
    own how-to: covers the rules/context file convention, the full
    guardrail catalog and precedence, quality gates/retry policy/failure
    taxonomy, templates, task-spec/verification-script authoring, the
    full PRD/Story/Task lifecycle and recovery paths, and an MCP-
    discoverability spot-check (can a model learn and use all of this
    from the tool schemas alone). **Explicitly sequenced last** —
    deliberately held until after the rest of this roadmap (through
    v10.0.0) and the in-flight model-tiering eval (BL422) and ci-watch
    (BL423, shipped) land, so it documents the system's final shape
    rather than a snapshot that goes stale with the next change.
24. **BL425** — Structural guardrail: detect a task's own commit
    sweeping in files that were already dirty/untracked in the project
    tree before that task's worker session started (filed 2026-10-13,
    shipped 2026-10-14 — **shipped v9.0.75**). Not roadmap work — found
    live on 2026-10-13 running BL423 in direct (non-worktree) mode in
    the same tree an operator was hand-editing: the Automaton's
    `git add -A` swept an unrelated uncommitted `docs/plans/README.md`
    edit into its own commit, under its own message, and verification
    passed because the diff was real content. The guidance fix shipped
    separately and directly in AGENT.md (Git Discipline: stage only the
    task's own files with targeted `git add <path>`, no more
    `git add -A` example); this item is the code-level backstop.
    **Shipped, v9.0.75** (implemented across the post-v9.0.74 commits,
    released in v9.0.75): the executor's existing pre-task
    `PreTaskSHA`/`PreTaskUntrackedFiles` capture is widened with
    `PreTaskDirtyFiles` (staged + unstaged tracked changes); after the
    worker session and its verifier finish, a deterministic, no-LLM
    check (`internal/autonomous/file_drift.go`) flags every file that a
    commit in `<pre-task SHA>..HEAD` touched, that was already
    dirty/untracked beforehand, and that is not in the task's planned
    `files`. Findings land in `Task.ForeignFileDrift`
    (`foreign_file_drift` on the API record), a `file_drift_detected`
    decision, an `autonomous_file_drift_detected` audit event and a PWA
    warning chip on the task. Warn-only by default; opt-in
    `block_on_file_drift` (Automaton > guardrail profile > global
    `autonomous.block_on_file_drift`) sets the task Blocked with
    `foreign_file_drift: <files>`. Full config parity (REST, MCP, CLI,
    comm `configure`, PWA, YAML); worktree-mode Automata are a
    deliberate no-op (zero git calls); howto
    `docs/howto/file-drift-guardrail.md`; release-smoke section 72.
    **Verification evidence:**
    `docs/plans/2026-10-14-file-drift-live-verification.md` (live
    warn-only, strict and worktree runs against the real daemon) plus
    `internal/autonomous/file_drift_test.go`.
    **Out of scope** (explicitly not done here): any automatic git
    surgery (no revert/reset/amend/un-stage, never a history rewrite);
    attributing a drifted file to the session or person that edited
    it; excluding the daemon's own tooling files written into a
    direct-mode project directory (they are reported as drift today);
    changing how workers invoke `git add`; checking worktree/auto-PR
    Automata. See also BL428 (item 29), the prevention-side tree lock.
25. **BL426** — the blocking `containers / agent-base` Trivy scan fails
    in every Release run since v9.0.62 (filed + shipped 2026-10-13, same
    day — **shipped v9.0.65**). Not roadmap work — the container finding
    BL423 explicitly left out of scope, failing in run 38484929685
    (v9.0.62) and run 38495887540 (v9.0.63). Root cause: all four HIGH Go
    findings (stdlib v1.27.1, `golang.org/x/net` v0.58.0) are in the
    `/usr/bin/gh` gobinary Trivy target — the GitHub CLI from the
    cli.github.com apt repo — not go.mod / `GO_VERSION` drift: the
    datawatch binaries have 0 findings, and stdlib 1.27.1 is newer than
    this repo's own Go, which builds with `GOTOOLCHAIN=local`.
    **Shipped, v9.0.65:** no gh release newer than v2.102.0 exists, so a
    new `ghbuilder` stage in `Dockerfile.agent-base` builds the released
    v2.102.0 tag (commit-checked) on Go 1.27.2 with x/net v0.60.0, and a
    `go version -m` guard fails the build on any other versions; the
    three remaining `openssh-client` HIGH CVEs (CVE-2026-106553/106555/
    106583, `fix_deferred`, postponed for bookworm) are accepted in
    `security/accepted-risks.yml` + `.trivyignore`. Local Trivy 0.73.0:
    `usr/bin/gh` 4 → 0. Also the first live real-content validation of
    the ci-watch completion guardrail (BL423): it was enabled on this
    Automaton via `per_automaton_guardrails_set` with
    per_prd_guardrails `["ci-watch"]`, so this Automaton's own release
    push is judged by the CI run it triggers.
    **Verification evidence:**
    `docs/plans/2026-10-14-bl426-agent-base-trivy.md` (Trivy Target
    tables before/after, `go version -m`, fix options considered).
    **Revert path:** once a cli/cli release ships with Go ≥ 1.27.2 and
    x/net ≥ v0.60.0, go back to a pinned package install.

26. **BL429** — one-shot Claude Code sessions re-pasted their whole task
    while the CLI's own self-update dialog ("✔ Update installed ·
    /restart to apply") was showing (filed + shipped 2026-10-14 —
    **shipped v9.0.66**). Split out of **BL428**, the broader
    concurrency/budget design pass. Incident sessions **348b** (~80
    `[Pasted text #N]` chips over ~40 min) and **258d** (timeline:
    `source:channel-task` re-sent after every idle transition).
    **Confirmed root cause:** the update notice was incidental. It is a
    passive footer, not a modal. The channel bridge re-POSTs
    `/api/channel/ready` every 60 s, and `handleChannelReady` re-typed a
    OneShot session's full task on every heartbeat that found it idle in
    `waiting_input`. Each re-delivery also re-armed the task-echo
    completion suppression. **Shipped, v9.0.66:** delivery is claimed
    once per session through a persisted `task_delivered_at`
    (`ClaimTaskDelivery`; released on send failure or timeout, cleared
    on `Restart`). The footer is recognised by `selfUpdatePatterns`,
    logged as `[self-update]` (debounced), never classified as a prompt
    or rate limit, and never sent a key. **Live-validated** on a sandbox
    daemon: 4 ready POSTs (3 at the 60 s cadence and 1 after a daemon
    restart) gave 1 delivery, and nothing but the task reached the pane.
    **Open, operator decision:** the non-claude TUI one-shot path's
    in-memory guard re-delivers after a daemon restart, which was seen
    live on a shell-backend session. The optional bridge
    `"heartbeat": true` flag and a separate TUI source label are not
    done. **Evidence:**
    `docs/plans/2026-10-14-cli-self-update-dialog-recovery.md` (root
    cause, call sites, pre-fix test run, live validation).

27. **BL430** — per-task / per-story / per-Automaton verification LLM
    override (filed 2026-10-14 — **shipped v9.0.72**). Split out of the
    **BL428** concurrency/budget design pass (same as the self-update-
    dialog fix, BL429) so it is ready before **BL422**'s model-tiering
    eval results are applied to real dev Automata. BL422 keeps its own
    transient global capture/set/restore mechanism for its own testing;
    this is the clean per-unit mechanism for applying its results
    afterward. New `verification_backend` / `verification_model` /
    `verification_effort` on Task, Story and Automaton; precedence task
    → story → Automaton → global `autonomous.verification_*`, and an
    explicit per-unit backend bypasses the `verification_backends`
    capacity pool. Each verification result records `verifier_backend`
    / `verifier_model` / `verifier_effort` / `verifier_source`
    (task|story|prd|global|pool). Surfaces: REST
    `set_task_verification_llm` / `set_story_verification_llm` /
    `set_verification_llm`, matching MCP tools, CLI
    `prd-set-*-verification-llm`, comm verbs, PWA Verification LLM rows
    (v9.0.67–v9.0.71; docs + release-smoke section 71 in v9.0.72).
    **Pre-existing gap, left as-is:** the global
    `autonomous.verification_effort` is still not consumed by the verify
    path — only a per-unit effort reaches a session-routed verifier.
    **Live-validated** on a sandbox daemon (E2E story TS-796): the
    overridden task recorded `verifier_backend` `ollama-localhost` /
    `verifier_source` `task`, and its sibling recorded `global`.
    **Evidence:** `docs/plans/2026-10-14-verification-llm-override-live.md`.

28. **BL427** — Automaton lifecycle event notifications (push, not poll)
    (filed 2026-10-13). **Status: Phase 1 shipped in v9.0.85** — event
    type, emit at every PRD/task status and session write site,
    multi-subscriber in-process dispatcher (`Manager.Subscribe`), WS
    `prd_update` broadcast migrated onto it with an unchanged frame shape
    (live WS smoke in `docs/testing.md`). **Phases 2–5 still Planned**
    (built-in sinks, persisted subscriptions + REST/MCP/CLI/comm, pending
    events + PWA UI, webhook/federation). Not roadmap work — surfaced live on
    2026-10-13/14: the operator had to hand-write a watch-and-inject script
    to follow BL422's eval Automaton, and the BL431 overnight queue stall
    went unnoticed for 5+ hours. **Gap, confirmed by direct code search:**
    no outbound notification fires on any Automaton status transition
    (blocked/completed/needs_review/failed, scope warnings, task-level
    start/verify/complete). The `PRDCompleted`/`PRDFailed`/`PRDBlocked`
    branches in `Manager.Run` only save and return; the one existing hook
    (`SetOnPRDUpdate`) is a single-slot PWA WS refresh that the executor
    itself never calls, and `Store.SaveTask` has no hook at all. Every
    surface has to poll `autonomous_status`/`autonomous_prd_get`.
    **Existing pieces to reuse:** the Decision log
    (`internal/autonomous/models.go`) as the event vocabulary and audit
    record; the messaging/webhook backends
    (`internal/messaging/backends/webhook/backend.go`, inbound-only today,
    so an outbound sink is new work) for external delivery; and the
    `discussion_subscribe` push precedent
    (`internal/mcp/bl358_discussion_subscribe.go`) for delivering to
    subscribed AI sessions. **Ruled out:** `schedule_add` — purely
    time/input-triggered, it would only move the polling loop to another
    clock and has no notion of Automaton state; `alertrules` — scoped to
    observer metrics (cpu/mem/gpu/rss/net) evaluated on a 30 s poll, no
    Automaton awareness. **Sequencing:** after **BL422** (item 21, the
    model/effort tiering eval) — BL422's results define the "recommended
    tier" that a model-tiering notification event (tier-mismatch) would be
    compared against, and feed v10 roadmap planning; that event kind is a
    separate follow-up filed only once BL422 lands. BL422–BL426 are
    unaffected. **Design doc:**
    [2026-10-13-prd-lifecycle-events.md](2026-10-13-prd-lifecycle-events.md)
    (event source, transitions + precedence, transports, noise control,
    phases, parity surface, operator open questions).

29. **BL428** — Concurrency control & usage-budget design (design pass)
   (filed 2026-10-13/14, operator-raised after a live incident).
   **Status: Phase 1 shipped in v9.0.86** (deprecation of
   `max_parallel_tasks` — accepted-and-ignored with a startup warning,
   `config_deprecated_key` audit entry and `deprecated_fields` note on
   config writes — plus global `autonomous.max_concurrent_tasks`
   plumbing across YAML/REST/MCP/comm/CLI/PWA; mobile parity
   datawatch-app#261). **Phases 2–6 remain Planned**, each as a separate
   follow-up Automaton: tree write lock, inference-cap gap closure,
   reactive usage back-off, dollar-window budget, and the
   visibility/audit/parity close-out. The final removal of
   `max_parallel_tasks` from the code is deliberately deferred one
   release. Original design-pass finding: `autonomous.max_parallel_tasks: 1` on the
   running daemon is dead config — `MaxParallelTasks` is plumbed through
   YAML/REST/MCP/PWA but has zero executor consumers, and its "per-PRD
   worker cap (default 3)" descriptions are false. The real knob,
   `max_concurrent_tasks` (BL370), only bounds fan-out *within one PRD's
   own run* (and its global default has no YAML field). Nothing gates
   across PRDs: `API.Run` gives every PRD its own executor goroutine, and
   the capacity ledger's `llm:claude-code` pool is unlimited because
   `max_inflight` defaults to 0. Result on 2026-10-13: **BL423**
   (`9f311580`) and **BL426** (`ff67e611`), plus a scratch probe PRD
   created by BL423's own verification task, had 3 tasks `in_progress`
   against one working tree at once — BL426's verifier saw BL423's
   commits in its diff and BL426's release collided on a version number.
   The doc designs **two separate mechanisms**, keyed differently: (3a) a
   per-canonical-tree **write lock** for tree writers (task execution,
   `verification_script`, `scan_fix`), with worktree-mode Automata keyed
   on their own worktree; and (3b) **inference caps** reusing the existing
   `llm:<name>`/`node:<name>` ledger pools (closing gaps: decompose not
   admitted, verify lease stacking, long waits holding slots). Plus
   **usage-budget throttling**: a `plan_tier`-derived `max_inflight`
   default, a daemon-wide reactive back-off on a real `StateRateLimited`
   signal (today nothing in `internal/autonomous` reacts to rate limits),
   and a dollar-window `llm.<name>.budget` ceiling; and deprecates
   `max_parallel_tasks` (deprecate and remove, not repurpose). Worked
   example: BL426's final task was told to poll a Release run with a
   ~90-minute bound and was killed as a stalled worker within about a
   minute — an LLM session is the wrong vehicle for a long CI wait, so
   IDLE-EXTERNAL-WAIT becomes its own resource class that releases its
   inference slot and keeps the tree lock only while it holds unpushed
   work. **Cross-references:** **BL414** — the doc recommends BL428
   *absorbs* BL414's budget-guardrail half (ceilings, hard stops) and
   BL414 narrows to cost observability and Decision `CostUSD` rollup,
   rather than BL414 growing a second "allocation mode" (open question
   1, operator to confirm). **BL422** — Claude only for now, but the
   backend/model pair is a first-class key dimension everywhere, so
   BL422's open-model forward pointer (Qwen etc. via Ollama/OpenWebUI)
   slots in later as extra pool limits with no structural change.
   **BL425** — BL425's foreign-file-drift check is the after-the-fact
   *detector*; 3a is *prevention* between datawatch writers; the two are
   complementary and neither replaces the other (humans editing the tree
   remain detect-only). **BL427** — both are infrastructure design
   passes on the PRD execution engine, but they share no mechanism (the
   doc lists BL427's push notifications as out of scope). Recommendation
   for the operator, not a decision: review the two docs together in one
   sitting (they touch the same executor paths and both feed BL431), but
   sequence their implementation independently as separate PRDs, in the
   already-confirmed addendum order below (BL427 → BL428 → BL431), with
   neither blocking the other's review. **Design doc:**
   [2026-10-14-concurrency-and-budget-design.md](2026-10-14-concurrency-and-budget-design.md)
   (resource classes, phase-level concurrency matrix, tree lock,
   inference caps, usage budget, worked examples, config keys, 6 planned
   implementation phases, parity surface, 9 operator open questions).
   **Forward pointer:** the §3b/§3c/§4d/§4e cost and capacity bounds also apply to **BL437**'s recurring model-eval sweep (item 31).

30. **BL440** — Status-tab session detail for Automaton task sessions
   (filed + **✅ closed 2026-10-14, shipped v9.0.87**). The session
   Status sub-tab showed "—" for Current focus, a raw JSON dump for the
   Sprint card, and Tests/Git placeholders waiting on hook payload fields
   that nothing produces; the Stats sub-tab never had a per-session
   process envelope because `observer.RegisterSessionRoot` had no
   production caller. **Shipped:** additive `automaton` block on
   `GET /api/sessions/{id}/status` built from stored task fields
   (`9ccd6983`); PWA Status cards — focus, formatted Automaton card,
   verification as Tests, verifier identity, git/file state, Activity,
   Cost (`ab319bf8`); observer session roots registered each stats tick
   via `SyncSessionRoots` (`22382029`); Stats backend fallback
   `claude-code → claude` and corrected copy (`98e233a9`). Mobile
   parity: datawatch-app#262. **Evidence + resolution:**
   [2026-10-14-status-tab-session-detail-gap.md](2026-10-14-status-tab-session-detail-gap.md).
   **Deferred follow-ups (open, from the doc's Resolution section):**
   - (a) Why hook auto-install isn't taking effect on this host — check
     `session.auto_install_hooks` and `[hookinstall]` log lines before
     any code change (operator first).
   - (b) Confirm whether `$TOOL_NAME` is empty under current Claude Code;
     if so the TodoWrite parse should read `tool_name` from stdin.
   - (c) The "hooks alive" header overstates hook installation — it
     should distinguish hook-script events from daemon-synthesized ones.
   - (d) Subprocess sessions could be rooted at the subprocess PID
     (needs the PID plumbed out of `runSubprocess`).
   - (e) Host claude processes may not match the observer `claude`
     signature (only `backend:claude-docker` seen live) — not
     investigated.
   - (f) Live check after the daemon runs v9.0.87: `observer_envelopes`
     should list `kind: session` entries for running tmux sessions.
   - (g) Surface per-session tokens/cost on the Stats tab before building
     token rate / latency / model state (still unbuilt).
   - (h) Optional: re-fetch the Status tab on a `prd_update` whose PRD id
     matches the session's Automaton.
   - Council / Skills / Tracker / closed-task summary cards remain unbuilt
     with no backlog item; `session.hook_script_dir` is dead config to
     clean up later.

31. **BL437** — Recurring, hardware-aware LLM model/effort evaluation (design pass)
   (filed 2026-10-14, operator-named, pre-v10.0.0 priority). **Status: Queued**;
   design doc done (`needs_review`), no implementation started. The design pass
   answers six asks: (1) model discovery beyond `llm_list`, so candidates are
   found rather than only what is already registered; (2) hardware-aware results,
   so advice is conditioned on the measured hardware class; (3) a monthly recurring
   trigger rather than an event trigger, with defined failure behavior; (4) all
   models and all efforts, swept as a matrix rather than a full cross product;
   (5) goose and datawatch-default as backends under test alongside claude-code;
   (6) results feeding **BL428**'s `plan_tier` defaults and **BL431**'s model
   defaults, with operator confirmation as the recommended default rather than
   auto-apply. **Cross-references:** **BL422** (item 21) — generalizes its fixtures
   and driver rather than replacing them. **BL259** (eval framework) — the design
   recommends migrating onto it; its `llm_rubric` grader always fails today, so
   grading stays deterministic until BL259's LLM-grading follow-up lands. **BL428**
   (item 29) — cost and capacity bounds in §3b/§3c/§4e and tier defaults in §4d.
   **BL431** — sections 6 and 11. **Design doc:**
   [2026-10-14-recurring-model-eval-design.md](2026-10-14-recurring-model-eval-design.md).

**Active execution order (operator-confirmed, last updated 2026-10-14 ~14:05;
ordered by feature dependency, not filing date or chronology — supersedes
the three separate dated addenda this replaced).** Items run one at a time
against this tree (no cross-PRD write serialization exists yet — BL428
§3a — so this order is enforced by hand/by watcher scripts, not by the
daemon).

| # | Item | Status | Why here |
|---|---|---|---|
| 1 | **BL432** — mobile/PWA payload optimization | Shipped v9.0.82 | Triaged 2026-10-14 from GH#242; unrelated bugfix, ran first because it was ready first. |
| 2 | **BL433** — PWA performance audit | In progress — code inventory committed ([2026-10-14-pwa-performance-audit.md](2026-10-14-pwa-performance-audit.md)); Method + Measurements next | Companion to BL432 (GH#243), audit-only. |
| 3 | **BL434** — full-screen eye splash + input-draft preservation fix | Queued (re-decomposing) | PWA visual + a real data-loss bug on the same reload sites. |
| 4 | **BL435** — unmodified-retry executor fix | Queued | Generalizes a one-off task-spec workaround used to recover BL431 into a real fix for every datawatch install. |
| 5 | **BL431** (`c187962a`) — Automation design doc | One task left (self-registration) | Design doc itself is done and committed; only its own backlog-registration task remains. |
| 6 | **BL420** — session-tracker OOM fix | **Shipped v9.0.83** (tracker code in binaries since v9.0.62; v9.0.83 documents it, ships `scripts/shrink-session-repo.sh` + `scripts/repro-tracker-git-growth.sh`, redeployed + 15-min observation in `docs/testing.md`). The bloated repos were shrunk with the script on 2026-10-14 (v9.0.84): ab22 31 GB → 3.3 MB, 9245 8.7 GB → 2.1 MB, ddeb 822 MB → 884 KB. | **Operator-confirmed 2026-10-14 ~14:05: moved back ahead of BL427/428/431.** Not "regular work" under the operator's own ordering principle — it's crash-prevention for the daemon that is about to run the heaviest, most concurrent automated work yet (row 7). Doing the riskiest implementation stretch without this fix in place first is the wrong order. |
| 7 | **BL427 → BL428 → BL431 implementation** | BL427 Phase 1 (event type + in-process dispatcher, WS `prd_update` migrated onto it) shipped in v9.0.85; BL428 Phase 1 (`max_parallel_tasks` deprecation + global `max_concurrent_tasks`) shipped in v9.0.86; BL427 Phases 2–5, BL428 Phases 2–6 and BL431 not started | Optimize the Automaton/PRD execution system itself before more regular feature/bugfix work, so the rest of the roadmap runs through it rather than being hand-sequenced. Dependency order: event dispatch → concurrency/budget mechanics → Automation coordinator built on top of both. Known trade-off, noted not litigated: BL428 §4d / BL431 §6-§11's model-tier design cite BL422's results as an input, so that part proceeds on placeholder assumptions until BL422 runs afterward (row 8). |
| 8 | **BL422** — Claude model/effort tiering eval | Queued, resumes after #7 | Its results get retrofitted into whatever BL428/431 implemented with placeholder tier defaults (see row 7's trade-off note). |
| — | **BL436** — per-turn chunked session capture (replaces tmux-scrollback reliance) | Design doc done, `needs_review` | Filed 2026-10-14, not yet slotted into the numbered order above — docs-only, low collision risk, could run opportunistically. |
| — | **BL437** — recurring hardware-aware model/effort eval ([design doc](2026-10-14-recurring-model-eval-design.md)) | Queued, pre-v10.0.0 priority (operator-named 2026-10-14) | Generalizes BL422 into a recurring monthly sweep over all models, efforts and hardware classes. Sequenced after BL422's results are available (row 8), so it is not slotted into the numbered order above yet. |
| — | **BL440** — Status-tab session detail for Automaton task sessions | **Shipped v9.0.87** (follow-ups (a)–(h) open in item 30) | Surfaced live 2026-10-14 while watching Automaton task sessions; display-only, ran outside the numbered order. |
| — | **BL447**: per-task `ProjectDir` override / `set_task_project_dir` | **Shipped v9.1.0** | Autonomous PRD; missed the tracker, registered 2026-10-15. |
| — | **BL448**: scope unconfirmed in repo | Unconfirmed | Autonomous PRD; missed the tracker, and the repo doesn't record its scope. |
| — | **BL449**: open-model eval matrix (BL437 S5) | **In progress**: PRD `9e7bad8e` running again after the BL451 unstick. v9.1.3 (ask-adapter `ollama/` strip) and v9.1.4 (session-start model normalisation) shipped. Runs r1–r8 produced no valid data: r6 was `invalid-infrastructure` (provider block missing for the bare model pin), and r8 on v9.1.4 was invalid for scoring (fast/mid workers die in 3–7 s; session records evicted before collect). | Plan: [2026-10-15-bl449-open-model-matrix.md](2026-10-15-bl449-open-model-matrix.md). Next: a valid run on v9.1.4+. |
| — | **BL450**: scope unconfirmed in repo | Unconfirmed | Autonomous PRD; missed the tracker, and the repo doesn't record its scope. |
| — | **BL451**: completion-rollup lost-update race (`PRD.Rev`) | **Shipped v9.1.1 + v9.1.2** (BL449 PRD unstuck, `running` again) | Found live while running BL449. |

BL424 (item 23 above) is explicitly excluded from this table and stays
sequenced last, right before the v10.0.0 cutover, per its own note above —
unaffected by anything in this table.

**Federation-aware shared-resource coordination** (operator-raised
2026-10-14, additive to BL428/431's existing scope, not a reordering): BL428
gains §3c (shared compute nodes across federated peers) and §4e (shared
cloud/billing accounts across federated peers), each via a keyed-hash
fingerprint exchange — peers detect a shared resource without disclosing
the underlying secret or hardware identifier, an operator confirms before
any capacity pool is merged, and a live cross-daemon lease/back-off
mechanism coordinates while sessions actually run on a confirmed-shared
resource. BL431 gains a matching forward pointer and open question.

**Outside the queue, handled separately, not blocking Stage 3:**
- **BL432** — server payload optimization for mobile/PWA clients
  (GH#242, filed + shipped 2026-10-14 — **✅ Closed in v9.0.82**;
  feature commits v9.0.76–v9.0.81). The datawatch-app Android client,
  profiled against a busy daemon (484 sessions, 500 alerts), moved
  33–52 MB/min idle on the Sessions tab: 3 MB `GET /api/sessions` and
  WS `sessions` frames (~7/min), 185 KB `/api/alerts` polled every 5 s,
  ~1000 `output` frames/min with no terminal open, no gzip on API JSON.
  **Shipped:** `GET /api/sessions?view=summary` (+ `since=`, `states=`,
  ended-session cap, `X-Datawatch-Sessions-Omitted`); gzip middleware
  for `/api/*` JSON; alerts `limit`/`unread`/`before` paging,
  `GET /api/alerts/count`, `alerts_read` WS event; WS `hello` →
  `hello_ack` with `session_list: delta` (`session_state` +
  `session_removed`), `session_view: summary`, `output: subscribed` and
  a new `unsubscribe` message; `/api/info` `capabilities`; new settings
  `server.session_summary_terminal_limit`,
  `server.session_summary_text_chars`, `server.api_gzip`,
  `server.api_gzip_min_bytes`, `server.ws_session_list_default`,
  `server.ws_output_scope_default` (full config parity); MCP/CLI
  options; client flow in `docs/api/mobile-surface.md`. All opt-in —
  defaults keep the old frames. Client adoption is tracked in
  datawatch-app (Mobile-Parity). Evidence: `docs/testing.md`
  "v9.0.82 — Server payload optimization for mobile clients".
  **Follow-up found in live validation (not fixed — operator
  decision):** the daemon still sends the full `sessions` frame
  (3.55 MB on this daemon) on WebSocket connect, before it reads
  `hello`, so a delta client pays it once per connect. Options: accept
  the hello options as WS URL query params (`/ws?session_list=delta`),
  or hold the on-connect list briefly for a `hello`. Only
  `server.ws_session_list_default: delta` avoids it today, which the
  PWA does not support yet (see BL433).
- **BL433** — PWA refresh/connection performance audit under load
  (GH#243), companion to BL432. **Open — code inventory done:** [2026-10-14-pwa-performance-audit.md](2026-10-14-pwa-performance-audit.md) (`needs_review`). Same method as the app audit
  (read-only proxy, idle-per-tab bytes/min). Once the audit lands, the
  PWA may adopt the same opt-ins BL432 shipped — `hello` with
  `session_list: delta` / `session_view: summary` /
  `output: subscribed`, the summary list and alert counts — at which
  point `server.ws_output_scope_default: subscribed` becomes safe to
  recommend for web-only deployments.
- ~~**BL381**~~ — status-verified 2026-10-10: shipped v8.28.0 + v8.28.2
  (PWA picker). Moved to Completed. No slot needed.
- **BL393** (nested tags) — stays excluded until explicitly authorized
  for implementation.
- **BL241** (Matrix) — stays excluded pending its own design interview
  (10 unanswered decision points).
- **BL400** (Alerts: conditions, filtering, SIEM parity) — design-
  options doc only, explicitly deferred; 3 unanswered questions (where
  filtering logic should live, whether alerts get their own CEF mirror
  or feed the existing audit pipeline, whether a dedupe/rate-limit
  window is worth adding) before it can be queued.
- **BL394** (Dependabot/CodeQL review) — substantially done; residue is
  deliberately-accepted security postures (not bugs), a `§3g` SQL-LIMIT
  question, and a `datawatch-app`#208 handoff already tracked there —
  no in-repo implementation work left to queue.
- **BL392** (cross-repo GitHub hardening audit) — substantially done;
  residue is two cross-repo issues already filed
  (`datawatch-app`#206/#207) — no in-repo implementation work left to
  queue, follow-up lives in the other repo.

**Not yet placed in the roadmap (found during the 2026-10-10 plans
refactor — each needs an operator decision: queue it, defer it
explicitly, or drop it):**
- **Harness implementation — Eval Sweep / Eval-DAG / Red-team
  validator** (`docs/plans/harness-impl/`, research in
  `docs/plans/harness-research/`). Draft specs dated 2026-09-14/16, no
  BL number, zero code in the tree (no `EvalSweep`/`RunSet`/red-team
  types anywhere under `internal/`). Only referenced by a stale "Next
  (v-next)" line in Current state. Biggest unscheduled body of work.
- **Settings-tab federation residue** — the v8.73.15 pass ended
  **Partial**: three judgment calls deferred (`loadServers`/
  `loadServersList`, `loadTailscaleConfig`, `fileServiceUpload`) and the
  inventory flagged as possibly non-exhaustive
  ([historical-plans/2026-10-08-settings-tab-federation.md](historical-plans/2026-10-08-settings-tab-federation.md)).
  No BL number.
- **BL397 B1 — delegated-subzone DNS-01** — "deferred, not declined" in
  the ACME plan; everything else in BL397 shipped.
- **GH#196** (`security/accepted-risks.yml` daily watch) — open issue,
  no BL entry; likely the same body of work as BL398 Phase 2 and could
  ride that slot.
- **Frozen items** (BL281–BL285 Vault follow-ups, F7, S14c) and
  **BL190** (howto screenshot density, cosmetic) — intentionally
  parked; listed here only so "the roadmap covers the whole backlog" is
  literally true.

**Active plan docs (`docs/plans/*.md`) → roadmap slot** (everything
else is in `historical-plans/`):

| Plan doc | BL | Roadmap slot |
|---|---|---|
| `2026-10-10-configurable-project-rules-enforcement.md` | BL406 (+B111/B112/B113) | Stage 1 #1 — **done**, all 6 phases shipped (v9.0.11–v9.0.17) |
| `2026-10-10-prd-git-workflow.md` | BL407 (+B114) | Stage 1 #2 — shipped v9.0.23 (Phases 0-6 all done) |
| _(no plan doc yet — ran directly as a PRD)_ | BL404 | Stage 2 #3 — done, PR `datawatch#205` open pending merge |
| `2026-10-09-plugin-extension-surfaces.md` | BL403 | Stage 3 #4 — in progress; Phases 1-4 shipped (v9.0.31–46), Phase 5 (federation capability gating) next |
| `2026-10-09-gh204-result-panel.md` | BL402 (GH#204) | Stage 3 #5 |
| `2026-10-10-llm-enhancements.md` | BL405 (absorbs BL390 P2/P3) | Stage 3 #6 |
| `2026-10-04-f2-session-worker-isolation.md` | BL395 (security track) | Stage 3 #7 |
| `2026-10-08-bl398-trivyignore-cve-review.md` | BL398 Phase 2 only | Stage 3 #7 (paired) |
| `2026-10-09-alerts-conditions-and-filtering.md` | BL400 | Outside the queue — 3 open questions |
| `harness-impl/`, `harness-research/` | — | **Unplaced** (see above) |

**Sequencing mechanism**: BL357's existing role-based work queue
(`queue push/claim/complete/fail`) — one queue item per Stage-3 PRD, a
single claimer works through them one at a time, only claims the next
after the current PRD's quality gates *and* BL406's new project-rules
gate pass. No new global-concurrency mechanism.

**Release cadence**: interim work ships as ordinary patch/minor versions
as each PRD completes (mirroring how v9.0.0 absorbed the prior
security-refresh work); the major cut to **v10.0.0** happens once Stage
3's queue is empty.

**Phase Completion Checklist, applies to this whole arc**: every phase
of every plan in this arc (BL406, BL407, BL403, BL402, BL405, BL398,
and the security track) must satisfy AGENT.md's Phase Completion
Checklist (Planning Rules §5, added 2026-10-10) — not just functional
code + unit tests, but the full documentation/Mobile-Parity/version-
bump checklist. BL406, BL405, and BL407's plans already carry an
explicit cross-reference per phase; BL404 (no plan doc written yet)
must include it from the start when written. This is also the
mechanism a PRD — human or Claude-driven — uses to know a phase is
actually done vs. what's still missing, per the operator's own framing.

## Unclassified

_(empty — drop new operator-filed items here; the backlog refactor each release pulls them into BL### entries below.)_

---

#### v9.0.0 — Memory Lifecycle Complete (major release milestone) ✅ Executed 2026-10-07

**Operator-filed 2026-09-15. Status corrected 2026-10-07**: this entry had sat at "Status: in-progress"/"Status: planned" (below, for BL386/BL387) since 2026-09-16 — stale leftover, never updated after BL385/386/387 all actually shipped (BL385 v8.29.0, BL386 v8.30.0, BL387 v8.31.0–v8.33.0; see the Completed Backlog table above, which already tracked this correctly). The referenced `docs/plans/2026-09-15-v9-memory-lifecycle-major.md` does not exist on disk. The operator's 2026-10-07 instruction to execute the major release is the current authorization; see `docs/plans/historical-plans/2026-10-07-v9.0.0-major-release.md` for what v9.0.0 actually ships (the memory trilogy, already-shipped since September, plus every v8.x release since — this milestone had simply never been formally cut).

**E2E status (2026-09-16, historical)**: Full suite ran: 551 passed, 18 failed, 39 skipped. All 18 failures fixed (T45 sprint TS-680–694). T47 sprint added TS-696–705. Re-verified clean as part of the 2026-10-07 release pass — see the new plan doc for current numbers.

#### BL366 — Autonomous verifier: git-diff grounding

**Operator-filed 2026-08-30. Nightwire review (v1.x independent verification concept).**

**Current state:** BL25 independent verification is fully implemented and wired (`internal/autonomous/executor.go`, `cmd/datawatch/main.go:3715`). The verifier already uses a configurable backend (`verification_backend`, `verification_model`) via `/api/ask` loopback — already LLM-agnostic and works with any compute node (Ollama, OpenAI-compatible, Anthropic, Gemini, etc.).

**The gap:** The verifier prompt only receives `task.Spec` (what the task was supposed to do) — it cannot see the actual code change. This means the verifier answers "was this task plausibly completed?" based on description alone, not evidence. A task that produces no output or wrong output passes verification as long as the spec sounds complete.

**Proposed:** After the worker session completes, capture `git diff HEAD~1` (or `git diff <pre-task-sha>`) in the project directory and inject it into the verifier prompt alongside the task spec:

```
Task spec:
<user_data>
{{task.Spec}}
</user_data>

Git diff (actual code change):
<diff>
{{gitDiff}}
</diff>

Verify that the diff plausibly implements the spec. Reply STRICT JSON:
{"ok": <bool>, "severity": "...", "summary": "...", "issues": [...]}
```

The `SpawnResult` should return the pre-task git SHA so the executor can produce a clean diff (vs. relying on HEAD~1, which breaks if the worker made multiple commits). Add `PreTaskSHA string` to `SpawnResult`.

**Diff truncation:** Cap at 8 KB before sending to verifier (configurable: `autonomous.verifier_diff_max_bytes`); truncation is noted in the prompt. Large diffs get a summary pass first.

**Security:** The diff content comes from the local git repo — not user input — so it does not need `<user_data>` wrapping. Only `task.Spec` needs the data-boundary tag (see BL369).

**Surfaces:** Only `cmd/datawatch/main.go` (the `autonomousVerify` and `SpawnResult` structs) + `internal/autonomous/executor.go` (`SpawnResult.PreTaskSHA`). No API surface change needed.

---

#### BL333 — Federated File Service (peer-to-peer file transfer + PWA upload) 🔵 v8.3.0

**Operator-filed 2026-05-19. Target v8.3.0.**

Per-peer and per-discussion isolated file storage. Global `file_service.root` with subdirectory isolation: `local/` (operator), `peers/<name>/` (each peer's private directory), `discussions/<id>/` (shared between discussion participants only). No enumeration leak — paths outside boundary return 404. REST CRUD + transfer + peer storage metadata; MCP tools; CLI; Comm; PWA file upload (long-missing); mobile file picker. Enables workspace sync without rsync config. Community plugin templates for NFS/git/rsync patterns filed separately. Sprint T43, TS-701–TS-724. Full plan: `docs/plans/2026-05-19-v820-sprint.md`.

---

#### BL332 — Discussion Scopes (federated shared memory) 🔵 v8.4.0

**Operator-filed 2026-05-19. Merges GH#68 + GH#69. Target v8.4.0.**

New `discussion/<id>` memory scope type — peer-equal (not hierarchical). Push-on-write sync to all participants; delivery queue for offline peers. Conflict surface + resolution API. Hard memory isolation: discussion scope boundary = access boundary, enforced at API layer, no enumeration of outside scopes, no leaks. Throttles (60 ops/min per peer default), 100ms write-batching, per-discussion write lock. Loop prevention: `origin_peer + origin_wal_seq` unique identity, `propagated_by[]` hop path. Participants: intra-instance (local sessions, containers, sub-nodes) + federation peers. WAL provides history. 3 sprints: T42a (foundation), T42b (sync protocol), T42c (conflict + 7-surface). Sprint T42, TS-719–TS-750. Full plan: `docs/plans/2026-05-19-v820-sprint.md`.

---

#### BL331 — Channel-Address Federation via Comms 🔵 v8.3.0

**Operator-filed 2026-05-19. From GH#72. Target v8.3.0.**

Federation through comms channels. Channel bridge becomes federation-peer-aware: `channel_identity[]` on federation peer config (type: matrix_dm/matrix_room/slack_dm/slack_room/signal/discord_dm/discord_server). 1:1 DMs need no address token; group rooms require prefix. New 14th built-in capability group `comms-channel-agent`. Global comms routing config + per-peer override: handler=automata|plugin|mailbox. `owner_peer` on Session + PRD; own-only scope default; `view_all` grantable. Structured commands with catch-all → Automata. Federation must be pre-established. Sprint T41, TS-683–TS-700. Full plan: `docs/plans/2026-05-19-v820-sprint.md`.

---

#### BL330 — UnifiedPush endpoints 🔵 v8.2.0

**Operator-filed 2026-05-19. From GH#76. Target v8.2.0.**

`GET /.well-known/unifiedpush`, `POST /api/push/register`, `DELETE /api/push/unregister`, `POST /api/push/notify`. Unblocks Android T10 push stories (TS-306+). Sprint T40d, TS-665–TS-673.

---

#### BL329 — /api/identity endpoints 🔵 v8.2.0

**Operator-filed 2026-05-19. From GH#75. Target v8.2.0.**

`GET/PUT/POST /api/identity` — role, current_focus, context_notes. Persisted to `identity.json`. Unblocks Android TS-286, New User Arc TS-410. Sprint T40c, TS-658–TS-664.

---

#### BL328 — PRD decompose async (SSE + polling) 🔵 v8.2.0

**Operator-filed 2026-05-19. From GH#77. Target v8.2.0.**

`POST /decompose` → 202 + background goroutine (stories persisted per-story to DB). `GET /decompose/stream` SSE primary (resumable via Last-Event-ID). `GET /decompose/status` polling utility API. Exponential backoff on disconnect; low-power pause; ConnectivityManager on Android; visibilitychange on PWA; best-effort Tailscale VPN reconnect. Unblocks Android T13 TS-232–241. Reference implementation for streaming-sse-pattern. Sprint T40b, TS-645–TS-657.

---

#### BL327 — Settings badge multi-select UI 🔵 v8.2.0

**Operator-filed 2026-05-19. Target v8.2.0.**

Replace all 14 comma-separated list text inputs throughout the PWA with badge/chip multi-select. Three component variants: freeform (type to add), known-set (dropdown from API), known-set ordered (draggable, for fallback chain). Replaces v5.26.8 `csvExpandButtonHTML` pattern in settings context. Sprint T40a, TS-637–TS-644.

---




### v6.12.0 batch — closed 2026-05-05

- ✅ `datawatch-definitions.md` central docs system + ? help icon in PWA header (Sessions / Session detail / Automata / Observer / Settings deep links to GitHub-rendered version until daemon serves /docs/ locally)
- ✅ Sessions list: clickable affordances on done/killed cards stay full-opacity (`.card-actions / .drag-handle / .last-response-link` opacity:1 inside greyed cards)
- ✅ Settings → About: dropped Branding/Splash card; "System documentation & diagrams" now links to the central definitions doc
- ✅ Observer audit log default = 5 entries (selector keeps 5/20/50/100)
- ✅ README cleanup: stripped BL refs, restored Daniel Keys Moran acknowledgement verbatim, "Additional Acknowledgements" header above the upstream-attribution list, kept "new in vX.Y.Z" annotations on feature subheaders
- ✅ Federated peer stale badge (Option A): cog badge clickable → navigates to Observer → Federated Peers and flashes the stale row; per-peer health dot already conveyed status

### Deferred to v6.12.x patches (tracked sub-tasks)

- ✅ **BL268** — `datawatch-definitions.md` populated end-to-end (Sessions / Automata / Observer / Settings × 7 sub-tabs / Documentation index); only stray `TODO` placeholder fixed v6.13.7 (start-session link → sessions-deep-dive). Closed v6.13.7.
- ✅ **BL269** — `openAutomataHowto()` opens `/diagrams.html#docs/datawatch-definitions.md#automata` (`app.js:10449`). Closed v6.12.1.
- ✅ **BL270** — `.select-bar-fixed { bottom: var(--nav-h); }` in `style.css:1317` puts the bar above the bottom nav, parity with Sessions. Closed v6.12.1; horizontal-scroll variant added v6.12.4.
- ✅ **BL271** — `wizard-grid-2col` (`style.css:752` v6.12.4) + mobile-first `wizard-mobile` overhaul (`style.css:770` v6.13.1) + Start-from-template strip first (`app.js:10247`). Closed v6.13.1.
- ✅ **BL272** — `.settings-section > .settings-row` 8 px 14 px inset normalized + 6 px inter-card gap (`style.css:2844-2866`). Closed v6.12.1 (v6.12.4 gap retune).
- ✅ **BL273** — `/docs/` is served by the FileServer (`internal/server/server.go:459` `//go:embed web` + `web/docs/` mirror), and every `?`/`docs` link in the PWA goes through `defsLink()`/`docsLink()` (`app.js:4690-4713`) targeting `/diagrams.html#docs/...` — no GitHub round-trip. Closed v6.13.7 (verified; was effectively shipped earlier).
- ✅ **BL274 — Docs-as-MCP-interface** (operator-filed 2026-05-05; **closed 2026-05-08 in v6.21.0**). 6 sprints, design captured in `docs/plans/2026-05-07-bl274-docs-as-mcp-plan.md`. Shipped: hybrid index (vector primary + BM25 fallback), 4 MCP tools (`docs_search` / `docs_read` / `docs_list_howtos` / `docs_apply`), 22/22 curated howtos with hand-authored `exec_steps` front-matter, plan-then-execute with approval-token round-trip + per-step risk gate, in-process MCP dispatcher (`internal/mcp.Server.Invoke`), LLM-translation fallback (Ollama/OpenWebUI) for non-curated howtos, fsnotify-driven plugin + skill auto-indexer with pending-trust queue, all-opt-in trust model, 7-surface parity (REST + MCP + CLI + comm + PWA + locale × 5), 3 new AGENT.md rules + 3 CI lint scripts wired into release-smoke, new `docs/howto/docs-as-mcp.md`, `datawatch-definitions.md` update. Hard constraint enforced: no GPU required; every Ollama-using path has a tested fallback. Six minor releases: v6.16.0 → v6.17.0 → v6.18.0 → v6.19.0 → v6.20.0 → v6.21.0; one critical patch v6.18.1 (chunker frontmatter fix). 1847+ tests pass; smoke 100%+; mobile-parity issues filed at every release.

_Historical Unclassified items shipped + tracked elsewhere:_ Directory-selector "create folder" (v4.0.1), Aperant integration review (skipped — see [`docs/plan-attribution.md`](../plan-attribution.md) "Researched and skipped"), datawatch-observer / BL171–BL173 (✅ all three shapes shipped — see Recently closed).

_2026-05-02 operator-filed items promoted directly to BL218–BL221. 2026-05-03 v6.1 refactor: raw operator notes promoted to BL239–BL243. 2026-05-03 v6.2 refactor: BL239/BL240/BL221 closed; BL245 promoted from unclassified. 2026-05-04 v6.5.0 refactor: raw operator UX notes promoted to BL246–BL250; GH#32 incorporated as BL252; GH#4 referenced in Frozen/External; BL251 added from pre-session research. GH#37 promoted to BL253. 2026-05-11 v7.0.0 refactor: BL292/293/295/296 promoted to Active backlog; BL294 moved to Open Bugs; BL297/298 moved to Recently closed; BL299/BL300 filed._

---




## Open Bugs

> **B111** — `PRD.GuidedMode` is a dead flag (found 2026-10-10 while
> grounding the BL405 single-thread-workflow design against existing
> executor primitives). Full REST/MCP/CLI/comm plumbing exists to *set*
> it (`autonomous_prd_set_guided_mode`, `set_guided_mode` REST action,
> `queue guided-mode` comm verb) and it persists correctly on `PRD`
> (`internal/autonomous/models.go:184`), but **nothing in
> `internal/autonomous/executor.go` or anywhere else ever reads it** —
> grep for `GuidedMode` outside `manager.go`/`api.go`/the MCP-CLI-REST
> plumbing files returns zero hits. AGENT.md's own Decomposer
> Scope-Drift Rule (BL384) documents it as load-bearing: *"Guided mode
> pauses before each task runs and requires operator approval, giving a
> checkpoint before scope drift executes"* — step 1 of BL384's own
> mitigation checklist. Any PRD run with `guided_mode: true` today,
> including ones following BL384's own documented procedure, gets no
> pause at all. **Fixed in BL406 Phase 5 (v9.0.17, shipped 2026-10-10)**
> via `PRD.GuidedModeSource` (pluggable gate source —
> `operator`/`council`/`guardrail_auto`; only `operator` has real
> behavior today, `council` falls back to `operator` with a logged
> reason since BL405 Phase 8's `CouncilProfile` doesn't exist yet) —
> `internal/autonomous/guided_mode.go`. No longer open.

> **B112** — AGENT.md's BL384 "PRD scan rule — scope drift detector" is
> also dead (found 2026-10-10, same investigation as B111).
> `internal/autonomous/scan/scan.go` is a real, working scan framework,
> but its only three `Scanner` implementations (`sast.go`, `secrets.go`,
> `deps.go`) are security-only — zero hits anywhere for `scope-drift`/
> `ScopeDrift`. BL384's mitigation stack was confirmed 2-for-3 dead
> (B111 + this); only its mitigation #2 (manually patching each task spec
> before approval) had ever actually worked. **Fixed in BL406 Phase 5
> (v9.0.17, shipped 2026-10-10)** as a new built-in `scope_drift`
> `ProjectRule` type (`internal/autonomous/scan/project_rules.go`) —
> opt-in, not a `DefaultConfig()` default (unlike the parity-inheritance
> rule), since a normal non-doc-only PRD saying "Implement X" is
> expected work, not drift. No longer open.

> **B113** — `internal/autonomous`'s scan config (`scan.Config` —
> SAST/secrets/deps toggles) has **zero YAML persistence and is never
> wired at daemon startup** (found 2026-10-10 while starting BL406 Phase
> 0, tracing how to add new fields to this same struct).
> `cmd/datawatch/main.go:3961`'s `amgrCfg := autonomouspkg.Config{...}`
> struct literal never sets `Scan:` — it's always the Go zero-value
> (`scan.Config{}`, every scanner **disabled**), contradicting
> `autonomous.DefaultConfig()`'s own stated intent (`Scan:
> scan.DefaultConfig()`, every scanner **enabled**) — that default is
> never actually reached at startup because `amgrCfg` is built as a bare
> literal, not from `DefaultConfig()` with overrides. `SetScanConfig` is
> only ever called from one place, the REST handler
> (`internal/server/autonomous.go:2021`) — purely in-memory; an operator
> who explicitly enables scanning via the API loses it on the next daemon
> restart, silently, with no error. `internal/config.AutonomousConfig`
> (the YAML-facing mirror struct) has no `Scan` field at all — only an
> unrelated, similarly-named `SecurityScan bool` (a different, older,
> single-flag toggle). **Fixed as part of BL406 Phase 0 (shipped
> 2026-10-10, live-verified)**, since the new `RulesFile`/`ContextFile`/
> `UpstreamRepos`/`ProjectRules` fields ride the exact same wiring and
> would have inherited the identical bug otherwise. Live smoke (real
> sandbox daemon, `kill -9` + process restart) caught a **second** real
> bug before it shipped: the first version of the PUT-handler fix mutated
> `s.cfg.Autonomous.Scan` directly from the request body, but that's a
> separate copy from the Manager's own config and never received the
> startup default — saving it produced a YAML block missing every
> untouched field to `omitempty`. Fixed by round-tripping through JSON
> from the Manager's own current config instead, and by syncing the
> startup default onto `cfg.Autonomous.Scan` too so the two copies can't
> diverge in the first place. Full suite green (3369 Go tests, 83
> packages; 182 JS tests). No longer open — safe to move to Completed
> Bugs at the next backlog refactor pass.

> **B114** — `PRD.ClusterProfile` dispatch is not functionally wired,
> independent of git (found 2026-10-10 while planning BL407). The
> `autonomousSpawn` closure's cluster branch
> (`cmd/datawatch/main.go:4452-4480`) returns
> `SpawnResult{SessionID: "agent:" + out.ID}` — a synthetic string never
> registered in `session.Manager` — so the executor's own verify loop
> polls `mgr.GetSession(task.SessionID)` for an ID that can never
> resolve; it spins until context cancellation rather than ever
> completing or failing. Separately, `docker_driver.go:178-179`'s own
> comment confirms task execution *inside* the container isn't wired
> yet either ("Task, for now, is visible to the worker via env...
> Actually invoking the task lives in the session start flow (Sprint
> 6+)"). `PostSessionPRHook` has never been exercised end-to-end against
> a real docker/k8s spawn in production — only against test mocks — and
> its session-binding mechanism (`SetAgentBinding`) is a manual operator
> action on a pre-existing local session, never something the
> `/api/agents` spawn path or the autonomous executor does
> automatically. **Fixed in BL407 Phase 0 (v9.0.18, shipped
> 2026-10-10)**: `autonomousSpawn`'s cluster branch registers a real
> virtual `session.Session` (`agents.VirtualSessionFullID`, mirroring
> the existing council-virtual pattern); `agents.Manager.Spawn` mints
> a per-agent result-report token (the bootstrap token is single-use,
> already burned by completion time); new pre-auth
> `POST /api/agents/report` lets the worker report its outcome with
> that token, which flips the virtual session to a terminal state
> directly (no polling bridge). The worker now actually runs `Task` as
> a local one-shot session once it's cloned its repo, using
> Backend/Effort/Model/PermissionMode threaded through from the
> original dispatch request. No longer open.

> **B115** — zero `docs/testing/v9.0.0/cookbook.md` (TS-###) e2e stories
> were added for any of the 21 releases between the v9.0.0 tag
> (2026-10-09) and v9.0.21 (2026-10-10) — confirmed by `git log
> --diff-filter=A -- scripts/test-stories/` showing the last story file
> (TS-786) was added 2026-10-08, before the tag, and a grep of all 692
> story scripts for `BL406`/`BL407`/`B11[0-4]`/the v9.0.1-21 version
> strings returning zero matches. The per-phase AGENT.md checklist's own
> "unit tests + live/smoke test + go test ./... green" bar was met on
> every one of those releases, but none of them got the deeper,
> REST-driven regression coverage this cookbook is for — a real
> systemic gap, not any one phase's individual oversight. Fixed by
> backfilling 9 new stories (TS-787–TS-795) for the subset that's
> safely REST-testable without a live LLM/real GitHub write/real
> docker-cluster spin-up: BL406 Phases 0/1/2/3/5 (config round-trips,
> the ProjectRulesScanner content rule, the default parity-inheritance
> rule, the PWA guardrail dropdown, the opt-in scope_drift rule,
> `set_guided_mode`'s live B111 fix), v9.0.3's federation-aware PWA
> orphan card, v9.0.16's `notify_exclude`, and B113's scan-config
> round-trip. Two (parity-inheritance, scope_drift) turned out to need
> `conflict:llm` after all — a fresh PRD's `add_story`/`add_task` are
> rejected while status is `draft`, and the only REST-reachable way to
> `needs_review` is a real decompose; found live while writing them,
> fixed by decomposing for real just to cross that gate and then using
> our own controlled story/task text for the actual assertion.
> Explicitly NOT backfilled, documented instead in
> `docs/testing/v9.0.0/cookbook.md`'s own gaps table: BL406 Phase 4
> (upstream issue-filing — would file a real GitHub issue against a
> real repo if run automatically), BL407 Phase 0 (real cluster-dispatch
> terminal-state regression — needs a built worker container image,
> infra this harness doesn't have yet), BL407 Phases 1-3 (PRD.Git
> fields aren't REST/MCP/CLI-exposed at all yet — that's Phase 5's own
> job), and the handful of pure build/release/docs/YAML-only v9.0.x
> patches that have no REST surface to test. No longer open.

> **B116** — `PUT /api/config`'s BL369 `autonomous.*` propagation
> (`internal/server/api.go`) marshaled the *entire* `s.cfg.Autonomous`
> struct and merged it onto the live `autonomous.Manager` config on
> every `autonomous.*` write, not just the key actually touched. Four
> bool fields are deliberately marked "no `omitempty`"
> (`AutoApproveChildren`, `PerStoryApproval`, `ContinueOnStoryFailure`,
> `InjectionGuard`/`BlockOnInjection` — a prior fix so an explicit
> `false` isn't silently dropped), so they're *always* present in that
> full-struct marshal. Since the YAML-facing config defaults to Go's
> zero value (`false`) while the live manager's own default for
> `AutoApproveChildren` is `true` (set once at daemon construction,
> `internal/autonomous/manager.go:203`), any unrelated `autonomous.*`
> write silently clobbered a live-only `true` back to `false`. Found
> live 2026-10-11 setting `autonomous.worktree_base_repo` ahead of a
> PRD run — `auto_approve_children` flipped false, confirmed via the
> audit log showing only `worktree_base_repo` as the intended change.
> **Fixed v9.0.24**: scoped the propagated JSON to only the top-level
> field(s) the specific `PUT` touched, read from the already-converted
> config struct rather than raw patch values (so array/CSV fields still
> convert correctly); a nested key like
> `autonomous.default_quality_gates.enabled` forwards the whole
> `default_quality_gates` object, not a literal dotted key. 3 new
> tests in `internal/server/autonomous_config_propagation_test.go`. No
> longer open.

> **B117** — `POST /api/autonomous/prds` (every surface proxies through
> one handler: REST, MCP's `autonomous_prd_create`, CLI, comm, PWA)
> hard-rejected a PRD with no `project_dir`/`project_profile` as "project_dir
> or project_profile required" — a v5.26.19 check that predates, and was
> never updated for, BL407 Phase 1's worktree auto-assignment (Decision 2):
> when `project_dir` is empty and `autonomous.worktree_base_repo` is
> configured, `Manager.Run()` creates a dedicated git worktree and
> backfills `ProjectDir` itself at run time. As written, no PRD could ever
> reach that path — creation failed before `Run()` got a chance. Found live
> 2026-10-11 creating BL404's PRD, the deliberate first real shakedown of
> worktree mode. **Fixed v9.0.25**: loosened the creation-time check to
> also allow an empty `project_dir`/`project_profile` when `cluster_profile`
> is unset *and* `autonomous.worktree_base_repo` is configured; every other
> case (no worktree config, or `cluster_profile` alone) is unchanged. 4 new
> tests in `internal/server/autonomous_prd_create_worktree_test.go`. No
> longer open.

> **B118** — session-based decompose (any coding-agent planning
> backend — opencode, claude-code, goose, aider) needs a real
> `project_dir` to root the planning session in, exactly like `Run()`
> does — but BL407 Phase 1's worktree auto-creation only ever fired at
> `Run()` time, which happens *after* decompose in the PRD lifecycle. A
> worktree-mode PRD could therefore never successfully decompose at
> all. Found live 2026-10-11 creating BL404's own PRD, the deliberate
> first real shakedown of worktree mode. **Fixed v9.0.26**: extracted
> the shared gating logic into `ensureWorktreeIfConfigured`
> (`internal/autonomous/worktree.go`) and called it from
> `Decompose()`/`DecomposeStreaming()` too, not just `Run()` — also
> closes a drifted-duplicate risk between the two decompose entry
> points already flagged as a prior bug source in their own code
> comments. Also added orphan-worktree cleanup, found missing in the
> same investigation: a worktree created at decompose time for a PRD
> that's then rejected (before ever running) or archived (the
> universal "done with this forever" backstop, covering a
> cancelled/blocked/failed PRD's worktree once the operator is done
> inspecting it) previously had no cleanup path at all — only a
> *successfully completed* PRD's worktree was ever removed. New
> `cleanupOrphanedWorktree` (`internal/autonomous/git_completion.go`),
> called from `Reject()` and `Archive()`, deliberately non-forced (a
> worktree with any uncommitted content is left in place for
> inspection, same spirit as Decision 5). 6 new tests in new
> `internal/autonomous/worktree_decompose_test.go`. No longer open.

> **B119** — `autonomous.execution_backend` (the daemon-wide fallback
> backend for a task/PRD with no explicit `Backend`, BL203) existed as
> a live `internal/autonomous.Config` field with zero config surface
> anywhere — not YAML, not `PUT /api/config`, not the daemon-startup
> bridge. Found live 2026-10-11 while making Claude the default backend
> for real PRD-driven development work. **Fixed v9.0.26**: wired the
> standard 3 layers (YAML mirror in `config.AutonomousConfig`,
> `applyConfigPatch` case + `GET /api/config` read-side, daemon-startup
> bridge in `cmd/datawatch/main.go`) — same minimal pattern as every
> other `autonomous.*` field added this cycle. 1 new test in
> `internal/server/autonomous_config_patch_test.go`. MCP/CLI/PWA-specific
> surface beyond the shared `PUT /api/config` path (which already
> covers CLI/comm for free) intentionally not done in this pass —
> worth its own accessibility-sweep item later, same shape as BL404
> itself. No longer open.

> **B120** — found live 2026-10-11 immediately after v9.0.26's own B116
> fix landed, while clearing `autonomous.planning_model` back to `""`
> after switching `planning_backend` to `claude-code`: the `PUT
> /api/config` call reported success and persisted the empty string to
> YAML correctly, but the live `autonomous.Manager` kept serving the
> stale value forever. Root cause: B116's scoped-propagation fix reads
> each touched field's current value from a marshal of the whole
> `autonomous.Config` struct — a field whose value happens to be its
> zero value is, by its own `omitempty` tag's design, omitted from that
> marshal entirely, so an explicit "clear this back to empty" never
> reached the scoped partial JSON. **Fixed v9.0.27**: new
> `autonomousFieldJSONByTag` (`internal/server/api.go`), a small
> reflection-based fallback that encodes a touched field's zero value
> directly (bypassing `omitempty`) when it's missing from the normal
> marshal. 1 new test in
> `internal/server/autonomous_config_propagation_test.go`. No longer
> open.

> **B121** — `autonomous.rules_file`/`context_file` (BL406) had full
> REST/MCP/CLI/comm read-write config plumbing but zero consumers
> anywhere in the codebase — confirmed via a full-repo grep. BL406's
> own Context section explicitly promised "the decomposer/task-spec
> generation and the new scanner both read whichever path is
> configured," and every one of that plan's 6 phases was marked Done,
> but the actual consumption was never built. Found live 2026-10-11
> auditing BL404's real run after an operator challenge ("is AGENT.md/
> DATAWATCH-CONTEXT.md actually enforced for autonomous PRDs?").
> **Fixed v9.0.28** (decomposer half): new
> `internal/autonomous/rules_context.go` resolves `RulesFile` (default
> `AGENT.md`, always applied) and `ContextFile` (default `CONTEXT.md`,
> only when present) relative to each PRD's own `project_dir`. The
> scanner half (BL406's other promised consumer) needs a genuinely new
> rule type — filed as **BL413**, not improvised.
> **Revised same day, v9.0.30** (operator-raised: "do we need to
> directly inject agent and context, or just point to the files?"):
> the v9.0.28 fix inlined the full file content (~160KB combined on
> this repo) into the planning prompt — the direct trigger for
> **B122**'s tmux chunked-paste corruption, and B122's own timing fix
> alone wasn't sufficient (confirmed live: a second, larger inlined
> prompt still corrupted claude-code's input rendering at ~26 chunks).
> A session-based decompose backend is already running *in*
> `project_dir` with its own file-reading tools, so there's no need to
> pre-digest file content into the prompt at all. Now emits a short
> pointer ("read AGENT.md and DATAWATCH-CONTEXT.md...") instead —
> eliminates the oversized-payload problem at its root rather than
> just tuning timing around it. Confirmed in the same investigation
> that decompose already reuses the exact same one-shot session
> delivery path (`POST /api/sessions/start`, `one_shot: true`) every
> other one-shot task uses, per the Reuse-and-Expand rule — not a
> bespoke code path needing its own fix. Tests rewritten for the new
> pointer-only behavior. No longer open.

> **B122** — B121's own large-prompt injection (full `AGENT.md`/
> `DATAWATCH-CONTEXT.md` content into every decompose call, ~160KB)
> exposed a pre-existing timing race in `internal/session/tmux.go`'s
> `SendKeysWithSettle`: a flat 10ms inter-chunk delay and flat 120ms
> trailing settle before `Enter`, never exercised beyond one chunk
> before today since every prior decompose prompt was a few KB. Found
> live kicking off BL403 Phase 1 as a real PRD: the receiving TUI
> (claude-code) corrupted its own input rendering across ~20 back-to-
> back chunk writes (observed: ~13 unexpanded `[Pasted text #N]`
> placeholders stuck on the input line) and the trailing `Enter` never
> registered as "submit" — the session sat forever instead of
> running. This chunking path had zero prior test coverage. **Fixed
> v9.0.29**: `interChunkDelay` 10ms → 30ms, and a new
> `settleForChunks` helper scales the trailing settle with chunk count
> (capped at 2s). A single-chunk payload — the overwhelming common
> case before today — is completely unaffected. 7 new tests in new
> `internal/session/tmux_large_payload_test.go`, including an exact
> reproduction of the real failure's scale. No longer open.

> **B123** — local-model verifier gave an incorrect completeness
> critique despite marking the task `ok: true`. Found live reviewing
> BL403 Phase 3's own PRD run (task 95cad9cf, the pull scheduler,
> `internal/plugins/widget_scheduler.go`): the verification record said
> `"ok":true` but listed issues — "The Reconcile method is incomplete
> and missing critical logic", "intervalFor function implementation is
> missing", "Worker management and tickers are partially implemented",
> "Test coverage for interval respect and hung pulls is incomplete".
> Independently read the real diff: `Reconcile()` is fully implemented
> (stop/start workers by key, `sameAs` diff against the desired set);
> there is no function literally named `intervalFor` but there IS an
> `s.intervalFor` field used exactly as the task spec asked for (a test
> override hook for sub-second intervals) — the verifier apparently
> expected a free function by that name and didn't recognize the field
> form; `widget_scheduler_test.go` has all 6 required test properties
> plus 4 extra (10 test functions total), including
> `TestWidgetScheduler_IntervalRespected` and
> `TestWidgetScheduler_HungPullIsolation` by name. This PRD's
> `verification_backend` is `ollama-datawatch` (a small local model) —
> the autonomous config's own comment elsewhere in this doc already
> notes guardrail/verifier judgment quality varies by backend size.
> Nothing was fixed in the code (it was already correct); this is a
> process finding about verifier reliability, not a code defect — filed
> per the operator's standing instruction to report PRD/process defects
> rather than silently patch them. The "should verification_backend
> default to a stronger model" question this closed with is answered,
> in part, by **B124** below: pointing verification at a stronger model
> is necessary but not sufficient on its own — see B124 for why the
> verifier never had real tool access regardless of which model backed
> it, and operator direction (2026-10-12) to configure `claude-code` as
> the verifier now that it can actually use tools.

> **B124** — the PRD verifier and per-task/per-story guardrail never
> actually had tool access, for ANY configured backend — found while
> investigating B123 and operator-raised ("the verifier should be able
> to also run tools so it needs to be either opencode or goose or
> claude[-code]"). Root cause had two independent layers, both found
> live 2026-10-12:
> 1. Decompose never actually produced a `verification_script` for any
>    task — confirmed by checking both PRDs that had run since BL416
>    Phase 2 shipped (BL403 Phase 3, 6 tasks; BL403 Phase 4, 10 tasks):
>    zero out of 16 ever got one. Root cause: `PlanningPromptSession`
>    (`internal/autonomous/decompose.go`) told the model its JSON "must
>    follow this schema exactly" while that very schema never listed
>    `verification_script`/`files_generated` — the BL416 instruction
>    asking for them was appended afterward, inside what reads as the
>    "Feature request" section, directly contradicting the "exactly"
>    contract shown first. `TestDecompose_VerificationScriptRoundTrips`
>    (BL416 Phase 2's own test) faked the LLM response entirely, so it
>    proved only that the PARSER can read the field — never that the
>    real production template asks for it consistently, which is
>    exactly why this shipped uncaught.
> 2. Independent of (1): even a perfectly compliant decompose response
>    was being judged by a verifier/guardrail with no tool access at
>    all, for any backend name. Both `autonomousVerify` and
>    `autonomousGuardrail` (`cmd/datawatch/main.go`) were hardwired
>    through `/api/ask`, which has adapters only for `claude` (cloud
>    API), `ollama` and `openwebui` — all text-only. Pointing
>    `verification_backend` at `claude-code`/`opencode`/`goose` didn't
>    error (confirmed one such attempt silently no-op'd, zero live
>    damage) but any real attempt would have resolved to
>    `inferenceDisp.Call`'s "no adapter for kind" error — the backend
>    name was never actually reachable via this path. The pre-existing
>    stopgap (`resolveVerifierBackendModel`'s own doc comment:
>    "session-only kinds ... fall through to the ollama default") was
>    already explicit that this was a known, deliberate limitation —
>    just never followed up with the real fix.
> **Fixed v9.0.41**: (1) `PlanningPromptSession`'s schema now lists both
> fields on the story and task examples; a new kick-back gate
> (`validateVerificationScriptCoverage`,
> `internal/autonomous/verification_script_coverage.go`) rejects a
> decompose response outright (rolls the PRD back to `draft`, same as a
> parse error) when it plans real files but names zero scripts, for any
> tooled PRD. (2) Both `autonomousVerify` and `autonomousGuardrail` now
> spawn a real one-shot tool-using session
> (`cmd/datawatch/session_judgment.go`'s `runOneShotJSONSession`,
> extracted from decompose's own proven session-spawn mechanism) when
> the resolved backend has real tool access and a project directory
> exists to run it in — same judgment prompt, real file/build/test
> access instead of text alone. Tool-access detection is fully
> data-driven, per operator direction ("it shouldn't be hard-coded...
> tie to a definition..."): a new `llm.ToolCapable` interface
> (`internal/llm/backend.go`) lets any backend declare
> `HasToolAccess() bool`; claude-code, opencode (all 3 variants), goose
> (both variants), aider, gemini and shell all implement it; ollama/
> openwebui do not. Whichever LLM is configured for
> `autonomous.verification_backend` (daemon-wide, or any per-PRD/
> per-task/per-story override) is checked against this interface at the
> moment it resolves — adding a future tool-capable backend needs no
> change to this routing logic, only `HasToolAccess() bool { return
> true }` on the new backend. 15 new/updated tests across
> `internal/autonomous` and `cmd/datawatch` (see
> `docs/testing-tracker.md`'s B124 section); `go test ./...` (3801
> passed) and `make lint` (0 issues) both green; `gosec` reinstalled to
> match the local Go toolchain (the same stale-binary class of problem
> noted earlier this session for `golangci-lint`) and re-run scoped to
> every touched package — 0 new findings.
> **Follow-up found immediately on deploy (v9.0.42)**: after rebuilding
> and reinstalling the daemon for real — a separate discovery made
> deploying this very fix: the running `systemctl` service had been
> stuck on a stale v9.0.30 binary from the day before, silently
> predating BL416 Phase 2 (v9.0.37) entirely, for the whole time this
> session believed it was testing "live" decompose/verify behavior.
> Both root causes above are still correct and were confirmed by
> reading source, not by trusting that stale "live" evidence — but it
> means the earlier claim of having confirmed them live needed this
> correction, made to the operator at the time — and configuring
> `autonomous.verification_backend=claude-code` for real still didn't
> route a verify call through a session: `autonomous.
> verification_backends` (the capacity-candidate list, still
> `["ollama-johnnyjohnny","ollama-datawatch"]`) is tried first and
> matched before the single `verification_backend` is ever consulted,
> and attempting to clear it had no effect. Root cause:
> `handleAutonomousConfigSet` (`internal/mcp/autonomous.go`) only
> included `verification_backends` in its PUT body when
> `len(arr) > 0`, so an explicit empty array and "didn't mention it"
> were indistinguishable and the clear silently never happened. Fixed
> v9.0.42: presence-aware, matching this same handler's existing
> pattern for bool fields.
> **Second follow-up found immediately validating v9.0.42 (v9.0.43)**:
> the clear still didn't survive a restart — `handleAutonomousConfig`'s
> own PUT handler re-syncs the persisted config via
> `json.Marshal(manager config)` + `json.Unmarshal(onto
> config.AutonomousConfig)`, and `omitempty` elided the empty slice
> from that intermediate marshal, so the unmarshal left the saved
> `config.yaml` holding its previous value — reverted on the very next
> boot. Same class of bug `Enabled` was already fixed for (GH#180,
> `Enabled` has no `omitempty` for exactly this reason);
> `VerificationBackends` never got the same treatment. Fixed: removed
> `omitempty` from `VerificationBackends` on both structs; new
> regression test using the real `autonomous.Config` type (the
> existing GH#180 test fixture's fake `Config()` returns a plain
> `map[string]any`, which has no `omitempty` tags at all and could
> never have caught this class of bug). No longer open.

> **B125** — `amgrCfg` (the config the `autonomousVerify`/
> `autonomousGuardrail` closures actually read — `cmd/datawatch/main.go`)
> is built ONCE at daemon boot from the loaded YAML and never refreshed.
> `autonomous_config_set` correctly updates the `Manager`'s own live
> config (so `GET /api/autonomous/config` reflects a change immediately)
> and correctly persists it to disk (once B124's two follow-ups above
> were fixed) — but the already-running verify/guardrail closures keep
> reading the stale snapshot captured at process start, so a runtime
> config change to `verification_backend`/`verification_backends`/
> `verification_model`/etc. has NO EFFECT until the daemon is fully
> restarted. Found live validating B124 end-to-end: configuring
> `verification_backend=claude-code` had no observable effect on a real
> verify call until a restart, even after both the MCP and
> handleAutonomousConfig bugs above were fixed. **Not fixed** — this is
> a real architectural limitation (every closure in the giant `main()`
> setup function that captures `amgrCfg` by value would need to read
> live from `amgr.Config()` on every call instead, a change that
> touches a large swath of `cmd/datawatch/main.go`, not a one-line fix)
> and is reported per the operator's standing instruction rather than
> silently patched under time pressure. Likely affects other
> `autonomous.*` fields read via `amgrCfg.*` the same way, not just the
> two touched here — worth a dedicated audit, not a point fix. Open.

> **B124 root cause, found validating the three fixes above (v9.0.44)**:
> even with `verification_backend=claude-code` correctly live (B125
> worked around with a restart) and `verification_backends` correctly
> empty (v9.0.42/43), a real verify call still used `ollama` — confirmed
> live: `"verifier: ask call failed: LLM unavailable: ollama HTTP 404:
> model 'qwen3.8:27b' not found"`. Root cause: `resolveAskBackend`
> (`cmd/datawatch/main.go`) was written for its one original caller
> (`decomposeFn`'s ask-only path, which only checks whether an error
> came back) and deliberately returned `("", "", err)` — empty kind —
> for a non-ask-compatible backend like `claude-code`. Both the verify
> and guardrail call sites discard that error, so all they ever
> actually received was an empty string; `isInferenceSessionKind("")`
> is always false, so `needsSessionJudgment` always concluded "no
> session needed," regardless of what was actually configured — with
> no error and no log line, for the entire time B124 was "shipped."
> Also explains why v9.0.41's own validation looked plausible at first:
> nothing about this failure mode is visible without reading the real
> error text closely. Fixed: `resolveAskBackend` now returns the real
> resolved kind alongside the error — extracted to a package-level
> `resolveAskCompatibleKind` specifically so this contract is
> independently testable (the inline closure never was, which is
> exactly why a unit test using an idealized fake resolver passed while
> the real closure's bug shipped anyway).
> **Final follow-up, found live confirming the fix actually worked
> end to end (v9.0.45)**: the spawn itself now logged correctly
> (`[autonomous:verify:...] spawned ... backend=claude-code`), proving
> the routing fix worked — but the spawned session hung for its full
> 10-minute timeout. Its pane: *"There's an issue with the selected
> model (qwen3.8:27b). It may not exist or you may not have access to
> it."* Caught only because the operator flagged the session was
> taking unusually long and told the assistant to check it directly —
> a real process on the self-check discipline this session otherwise
> tried to hold itself to: "a session spawned" was wrongly treated as
> "the fix is confirmed working" without checking that session's own
> live output first. Root cause: `verification_model`/the merged
> guardrail-model fallback is an ask-compatible (Ollama/OpenWebUI)
> model name, applied unconditionally even for a session call where
> it's meaningless. Fixed: a session call now uses the session
> backend's own registry-default model instead (a genuine per-unit
> `GuardrailModel` override for guardrail still carries through). New
> regression test reproduces the exact live failure. No longer open.

> **B126** — found as a side effect debugging B124's root cause, NOT
> fixed, reported per standing instruction: a task's `AutoFixRetries`
> retry can end up with `PreTaskSHA` equal to its OWN prior attempt's
> commit SHA, rather than the commit that existed before any attempt
> started. When that happens, `taskProducedNoOutput`'s diff check
> (`git diff PreTaskSHA..HEAD`) is empty by construction even though
> the task's deliverable genuinely exists and was correctly committed —
> the verifier reports "no changes detected... task produced no
> output" on a task that did real, correct work, and the PRD fails.
> Reproduced live twice during B124 validation (throwaway PRDs
> `2732a375` and `f583fcde`): in both cases the worktree's git log
> showed the expected file correctly committed, yet `pre_task_sha` on
> the task record equaled that exact commit. Root cause not yet
> isolated (likely in whatever resets `PreTaskSHA` for a retry attempt)
> — worth a dedicated investigation, not a point fix guessed at under
> time pressure. Open.

> **B127** — found live via BL418's own first real validation run
> (PRD `7fe31a99`, BL403 Phase 5, 2026-10-12): `resolveGuardrailBackendModel`
> (BL416 Phase 1) baked the flat ask-only `autonomous.verification_model`
> into its returned model whenever no task/PRD guardrail override
> existed, contradicting its own doc comment ("the empty-global case...
> remains the caller's responsibility"). Every guardrail call site
> assigns this straight onto `GuardrailInvocation.GuardrailModel`, and
> B124's session-routing closure trusts any non-empty value there as a
> genuine per-unit override — exactly the bug class B124 already fixed
> for verify (`resolveVerifierBackendModel`), left unfixed for
> guardrails because nothing had exercised a non-ask-compatible
> backend + empty task/PRD override + non-empty global model in
> production until `rules-review`'s first live fire did. Real PRD hit
> it directly: a claude-code guardrail session rejected `qwen3.8:27b`
> outright and hung for its full 10-minute timeout, blocking the PRD.
> Fixed v9.0.49: removed the global-model fallback from
> `resolveGuardrailBackendModel`'s default branch; backend still falls
> through to global, model does not. No longer open.

_(B125 and B126 above are the only open bugs as of 2026-10-12.
Everything else listed in this section going back to the 2026-10-09/10
backlog refactor has been closed and moved to the Completed Bugs table.
Historical: B22 fixed in v2.4.3 · B23/24 in v2.4.4 · B25 in v2.4.5 · B31
in v3.0.1 · B30 in v3.1.0 — see Completed section.)_

## Open Features

#### BL241 — Matrix.org communication channel (filed 2026-05-03, awaiting design interview)

Add Matrix as a communication channel. Matrix is extensive and has multiple integration options (rooms, encrypted DMs, federation, bots, bridges). Requires a design interview with the operator to choose the approach before implementation.

**Design doc (in flight):** [`2026-05-04-bl241-matrix-design.md`](historical-plans/2026-05-04-bl241-matrix-design.md) — full discussion: 10 decision points (DP1–DP10), 3 architecture-shape diagrams, per-surface parity matrix, 3 candidate phasing plans, consolidated questions for operator in §11. **No decisions made yet.**

**References:** https://spec.matrix.org/latest/ · https://github.com/mautrix/go (`maunium.net/go/mautrix v0.22.0` already in `go.sum`)
**Status:** Open — design discussion in flight (see design doc); operator answers in §11 of design doc drive the implementation plan.


#### BL365 — Core security assessment: daemon, code & features (filed 2026-08-28, plan ready)

Assessment-only (no fixes in scope) security review of the datawatch daemon across every
channel — CLI, REST API, WebSocket, PWA, MCP, channel-bridge comms, DNS covert channel,
agent bootstrap, federation, containers, eBPF observer. Loopback + Tailscale-only threat
model (operator-confirmed); messaging treated as operator-only (untrusted-ingress deferred
§10 F-1). Sandbox-only dynamic testing per the E2E process (2nd instance, non-default ports,
temp data dir, dummy secrets, full teardown) — the production daemon is never touched.
Static: gosec/govulncheck/Trivy re-triage vs `docs/security-review.md`, gitleaks, plus a
local-model (Ollama) read-only code review of the high-risk files. Findings register in the
plan (`SEC-###`) for later GH-issue conversion on operator request.

**Plan doc:** [`2026-08-28-security-assessment-core.md`](2026-08-28-security-assessment-core.md)
**Status:** In progress — Phase 0 ✅ (inventory + sandbox harness) · Phase 1 ✅ (gosec/govulncheck/trivy/gitleaks + local-model review; findings registered, SEC-001…024 with gaps). **Phase 2's "authn/authz sandbox tests" is superseded, not pending** (corrected 2026-10-10, reviewed while prioritizing the BL403/404/405/406/407 arc): the 2026-09-22 resumed walkthrough (`docs/plans/historical-plans/2026-09-22-security-remediation-activation.md` §7) already decided every one of SEC-001–025 explicitly, re-validated against v8.39.16, and BL395 (below) confirmed all decisions still hold and added the F-2 isolation design on top. **Remediation (v8.39.15):** SEC-004 fixed; SEC-021's XSS half fixed (file-service root-fallback half deferred, see SEC-021 row in the walkthrough). Everything else SEC-001–025 is a *decided, not-yet-implemented* item — see BL395 for the real master implementation order (Design **A → C → B → D**), not a design gap. Full writeup: `docs/plans/historical-plans/2026-10-03-bl394-security-findings-review.md` §7.

**External confirmation, 2026-10-14 (GH#209, trusted tester Cairn-works):** live evidence
on a real v9.0.10 instance that default `server.host: 0.0.0.0` + `server.token: ""`
serves the full REST/WebSocket API — including the `shell` backend — unauthenticated
to every interface, with zero warning anywhere. This does **not** reopen SEC-001 (which
explicitly decided an empty token stays a legitimate operator choice — not re-litigated
here) — it's real-world confirmation for exactly the remediation SEC-002 already
proposed but never implemented: *"a startup warning and a per-listener auth flag in
health/diagnose."* Treat GH#209 as the forcing function to actually implement that half
of SEC-002 before/around the v10.0.0 line, not as a new finding. Operator response
posted on the issue 2026-10-14: internal assessment already covers part of this,
update to follow once remediated.

#### BL408 — Hostile-LLM assessment: prompt injection, overreach, escape (filed 2026-08-28)

> Tracking-ID collision resolved 2026-10-10 (found while reviewing the security
> backlog for the BL403/404/405/406/407 prioritization pass): this entry was
> labeled `BL366` on filing, but `BL366` was already taken ("Autonomous verifier:
> git-diff grounding," closed v8.16.0). Re-labeled `BL366-hllm` pending renumber at
> the time; the renumber was never completed, and its own suggested fallback
> (`BL369`) was *also* already taken by then ("Prompt injection hardening in
> autonomous executor," v8.18.0) — this collision sat unresolved for over a month.
> Assigned its real, permanent number now: **BL408**.

Assessment-only companion to BL365, treating datawatch-spawned LLM sessions as potentially
hostile/compromised. Three classes: T1 prompt-injection data exfiltration (secrets, memory,
identity; tested on ≥2 models incl. the weakest local), T2 confused-deputy overreach
(cross-session/agent reads, config/schedule abuse, file escape), T3 container/host escape +
credential reuse for lateral movement (messaging APIs, Tailscale, self-update). Phase 0
builds a privilege manifest (env / MCP tools / fs / net / memory surface per session type).
Reuses BL365's sandbox harness, evidence bar, and findings register (`HLLM-###`).

**Plan doc:** [`2026-08-28-security-assessment-hostile-llm.md`](2026-08-28-security-assessment-hostile-llm.md)
**Status:** ✅ **ASSESSED 2026-09-01** (v8.18.x). Phase 0 recon + T1/T2/T3 + C controls audit
complete. **9 findings registered (HLLM-001…009):** 2 **CRIT** (HLLM-001 admin token in 0644
`.mcp.json`; HLLM-002 session is full-admin principal / uniform 361-tool surface unscoped),
6 **HIGH** (audit not LLM-attributable; unscoped secrets+memory+sessions; `allow_self_config`
defeatable via REST; exfil control absent / refused-by-model is a mask not a control;
peer-federation + Tailscale-ACL from a session; self-propagation/supply-chain from inside),
1 **MEDIUM** (local sessions run as daemon uid, no container — operator's architectural call).
All confirmed with live sandbox repro (v8.18.1 build, canary sink, throwaway token); T3.1
container-escape deferred (no worker image). **Operator triage → issue conversion is the next
step** (per the plan's §7 DoD item 6). Root-cause fixes: per-session scoped credential +
capability set (fixes HLLM-002 → cascades HLLM-004/005/007/008), LLM actor in audit log
(HLLM-003), datawatch-side content boundary (HLLM-006), isolate local session execution
(HLLM-009, F-2).

**Remediation (v8.39.15):** HLLM-001's file-permission exposure vector fixed — `.mcp.json`
(all 4 writers) now `0600` with a self-healing `Chmod` for any pre-existing `0644` file, so
another local uid/process can no longer read the token out of it. **The deeper root cause —
the token being the full, unscoped admin credential at all, shared with HLLM-002's "session
is a full-admin principal" finding — is NOT fixed**, deliberately: per-session scoped
credentials is a real design project (cascades HLLM-004/005/007/008 per the root-cause note
above), not something to rush ahead of a release. HLLM-002 and its 6 downstream HIGH findings
remain fully open. Full writeup: `docs/plans/historical-plans/2026-10-03-bl394-security-findings-review.md` §7.

#### BL395 — F-2 LLM-session worker isolation + resumed security-remediation walkthrough (filed 2026-10-04)

Resumed the paused 2026-09-22 security-remediation-activation walkthrough
(`docs/plans/historical-plans/2026-09-22-security-remediation-activation.md`,
paused at SEC-015) and validated every remaining item (SEC-015–024,
HLLM-001–009) against current code (v8.39.16) before deciding anything —
all 17 confirmed still real and unfixed, with Tier 1's two partial fixes
(HLLM-001's file-permission half, SEC-021's XSS-render half) confirmed
still holding. Decisions made this pass: SEC-015 fails closed on an
unpinned HTTPS parent (opt-out env var); SEC-016 gets a dedicated
`POST /api/auth/rotate-token` endpoint with a 60s grace window, not a
fix-in-place on `PUT /api/config`; SEC-023's new crypto envelope uses
Argon2id t=4/m=512MiB/p=4; SEC-024's Helm chart requires a non-empty
`apiToken` (render-error otherwise) even though SEC-001 left the bare
daemon's empty-token posture alone — a k8s pod is cluster-reachable by
default in a way a loopback-bound daemon isn't; HLLM-006's egress
allowlist defaults to loopback-only with named one-flag categories
(Tailscale mesh / federation peers / compute nodes) rather than raw
host/port entries; HLLM-008 gets a show-diff-then-confirm step on
plugin/skill installs *in addition to* making the verbs admin-only.
SEC-017/018/019/020/021(remainder)/022 batched as approved-to-implement
per their already-specified, no-real-tradeoff fix directions.

**F-2** (HLLM-009 / Design B2.b, previously filed-not-designed) is now
fully designed: plan doc
[`2026-10-04-f2-session-worker-isolation.md`](../2026-10-04-f2-session-worker-isolation.md).
Harden the existing `runc` worker path immediately (cap-drop, non-root
enforcement, read-only-root with an explicit writable-path allowlist,
a Docker-side resource-limit bug fixed along the way); add a pluggable
per-`ClusterProfile` `sandbox_runtime` (`runc`/`runsc`/`kata`), gVisor
and Kata landing together rather than phased, gated behind a runtime-
availability capability probe; move PQC bootstrap keys out of the
container/pod spec entirely into the bootstrap HTTP response body
(closing the `docker inspect`/`kubectl get pod -o yaml` exposure
structurally, not just practically); generate a default-deny
NetworkPolicy per worker as a second, stronger enforcement point for
HLLM-006's egress control. SEC-024's Helm `securityContext` work lands
in the same PR as F-2's k8s-driver hardening. Also found and corrected
along the way: `docs/howto/container-workers.md` mischaracterized a
container worker as "a smaller-surface companion to the full daemon" —
it is the same full daemon binary with the same ~390-tool MCP catalog,
just reached over the Tailscale mesh instead of loopback; confinement
has to happen at the container/pod boundary, it cannot be assumed away.

**Status:** Design complete (this plan doc), code not started. Remaining
HLLM items (001–002, 004–005, 007) close automatically once Design
A2/A3 (per-session scoped token + capability opt-out) land — no separate
decision needed for those; HLLM-003 sequences after A3 exists for the
same reason. Implementation order unchanged from the master index:
A → C → B → D, with this F-2 doc's hardening phase (F2.1) and SEC-024
able to land early since neither depends on A.

#### BL394 — Dependabot + Code Scanning findings review (filed 2026-10-03)

Operator-raised: Dependabot/CodeQL findings "many look like false
positives" — review, clean up, and discuss what's actually needed to fix.
Line-by-line review of all 107 open `datawatch` alerts (not 100 — an
earlier pagination hiccup undercounted) found two systemic reasons the
raw count overstated the real signal (gosec's `#nosec` doesn't register
with CodeQL; a dataflow alert's reported line is the sink, not the
source), but also real, confirmed vulnerabilities: a capability-mismatch
path-traversal bug (`api_smoke_progress.go`, gated by a read-sounding cap
but reachable to write/delete arbitrary `.json` paths), a skill-registry
sync path traversal (`skills/manager.go`, trusts unvalidated frontmatter
`name` from any connected registry), a council-persona create path
traversal, an SSRF in mobile push-endpoint registration, a CRLF/email-
header-injection bug, a client-side prototype-pollution bug and a
reflected XSS in the PWA, and 15 total call sites of one CLI setup-wizard
bug that echoes existing secrets as the visible default when re-running
`datawatch setup`. 74 of the 107 were confirmed false positives and
dismissed on CodeQL with a documented reason each; `datawatch-app`'s 11
CodeQL + 5 Dependabot findings were handed off there directly (filed as
`datawatch-app`#208) per the node's scope split — this agent's focus stays
`datawatch` core.

Push.go's SSRF fix (§3e) is the first item actually shipped from this
review, in **v8.39.3** — coordinated with the `datawatch-app` agent first
(same node, discussion WAL) because a naive "block all private IPs" fix
would have broken real self-hosted ntfy/Gotify push. Full `go test ./...`
run caught one real regression (a nil-cfg panic in an existing test
helper) before it shipped.

**v8.39.4** fixed the other two confirmed-real path-traversal findings
(§3b): a council persona's `name` and a skill registry's own advertised
`SKILL.md` frontmatter `name` were both joined into a filesystem path
with no validation — for skills, meaning any git registry an operator
connects to (not just `datawatch-community`) could ship a malicious
manifest and have it exploited on sync. New shared `internal/pathsafe`
package fixes both. The third instance from the same finding
(`api_smoke_progress.go`'s `run_id`) is deliberately not folded in —
it also needs a separate capability-model decision first.

**v8.39.5** fixed the 4th path-traversal-shaped finding, a standalone
one with no shared root cause with the other three: the generic
webhook's `image_url` field passed an arbitrary local path straight to
`os.ReadFile` with zero scoping, reachable via a `POST /task` whose
bearer token is itself optional — not just overwrite/delete like the
other three, an actual file-content-disclosure primitive. Fixed with a
new `webhook.image_dir` config field (empty disables the feature by
default) and the same `filepath.Clean`-based scoping idiom already used
elsewhere in this codebase. 14 new tests, including the real traversal
attack run end-to-end through the actual HTTP handler.

**v8.39.6** fixed the `cliPrompt` secret-echo finding (§3d) — and
corrected a counting error this doc's own §3d made earlier ("2 confirmed
real" should have been "1 alert, 2 source flows in one message"). The
2 CodeQL-flagged call sites (SMTP password, OpenWebUI API key) turned
out to be 13 once every `cliPrompt` call site was actually grepped —
every bot token, bearer token, API key, and shared secret in the setup
wizard had the same bug. New `cliPromptSecret` fixes all 13; the ~45
genuinely non-secret fields keep the original `cliPrompt` unchanged.

**v8.39.7** is a correction, not a new finding closed: the email-
injection finding (§3f) was originally called "CONFIRMED REAL," but live
verification while fixing it (timing a real `smtp.SendMail` call against
a non-routable address) showed Go's own stdlib already rejects a
CRLF-laced header value in ~1 microsecond, before dialing at all — this
was never a live exploitable gap in this code, and the original write-up
also named the wrong field (`message`, the body, when it would have been
`to`/`from`, the header values, if it had been exploitable). Hardened
anyway with an explicit check, kept as genuine defense in depth against
a plausible future change, not because today's code needed it.

**v8.39.8** fixed the client-side prototype-pollution finding (§3h): the
PWA's `hook_update` WS handler read a plain `{}` object by a server-sent
`session_id` — on a plain object, `obj["__proto__"]` doesn't return
`undefined` for a missing key like every other key does, it returns the
real shared `Object.prototype`, and the code then wrote ordinary
properties onto whatever that returned, polluting it for the whole page
when `session_id === "__proto__"`. Traced reachability: gated by
`CapConfigWrite` (not a broadly-distributed read-only preset), but for
anyone at that tier this is a real privilege *escalation* — from "can
write daemon config" to "can run JS in the operator's own browser."
Fixed with a 3-value guard (`__proto__`/`constructor`/`prototype`). No
JS test framework exists for this PWA, so a standalone Node script
(`internal/server/web/app_security_test.js`) loads the real `app.js` in
a stubbed browser environment and exercises the actual `handleMessage`
function — validated the test itself catches the bug by temporarily
reverting the fix and confirming it fails first.

**v8.39.9** fixed the last confirmed-real finding from this review: the
embedded docs viewer reflected an attacker-controlled URL hash into
`innerHTML` unescaped — a crafted link runs arbitrary script in the
operator's own session, no capability token needed, just a click.
Found 3 *more* unescaped sites sharing the same tainted value while
implementing the fix, beyond the 2 originally identified — one inside
an `href="..."` attribute, where the exploitable character is a
literal `"` rather than `<`, so this file's pre-existing `<`-only
escaping convention wouldn't have closed that one anyway. Fixed all 5
with a proper `escHtml` helper; another standalone Node test
(`diagrams_security_test.js`) confirms the payload lands escaped, not
verbatim, validated the same way (revert-and-confirm-it-fails) as the
prototype-pollution test.

**v8.39.10** closed out the rest of what this review had left open or
mischaracterized. Two findings the doc had flagged as "not
independently re-verified" turned out to be real: `evals.LoadSuite`'s
`?suite=` and `layers_recursive.L0ForAgent`'s `?agent_id=` both take a
query-string value straight into a `filepath.Join` — query params are
never touched by `http.ServeMux`'s path-cleaning, which is exactly why
these two didn't get the same free pass as their path-segment-sourced
siblings earlier in the review. Both now go through the existing
`pathsafe.ValidateRecordName`. Re-reading §3h's app.js escaping findings
surfaced a sharper version of a bug this doc had previously called
"cosmetic": `escHtml` already turns `'` into `&#39;`, so a schedule-
entry onclick handler's trailing `.replace(/'/g,"\\'")` was a no-op —
the simplest quote-breakout payload worked, not just a backslash-
collision edge case — because the browser HTML-decodes an attribute
value *before* compiling it as JS. New `escJsAttr` fixes that plus a
second real site (`renderChanRow`'s onclick). Also fixed: two more
unescaped `innerHTML` interpolations (status-board test counts), the
`channel/index.ts` stack-trace-exposure hygiene gap, and the 2
`missing-workflow-permissions` workflow gaps (§3i). Dismissed 10 more
CodeQL alerts, all the same shape — a fix from an earlier version in
this list left a validation guard one call frame above the generic
helper function CodeQL's own sink location points at (`copyDir`/
`copyFile`, `push.go`'s dialer, `email/backend.go`'s header line) — plus
2 accepted-risk command-injection dismissals (`manager.go`'s subprocess
backend, gated by a capability that already means "run arbitrary
tasks"; `git.go`'s push URL, traced to the operator's own project
config). Separately, confirmed one new finding as real but
architectural and deliberately did **not** fix it: `proxy.go`'s
`handleRemotePWA` serves a remote federation peer's own PWA content
under the local daemon's own origin with no CSP/sandboxing — a
malicious peer's JS would run with the local daemon's session. Also
DRY'd up the two hand-rolled JS test scripts into one shared stub
module and migrated all three JS regression tests to Node's built-in
`node --test` runner after comparing it directly against the hand-rolled
approach.

**v8.39.11** closed the one item v8.39.10 had deliberately deferred:
`api_smoke_progress.go`'s capability-model decision. Fixed both halves
together — the path traversal (`pathsafe.ValidateRecordName` on
`run_id`/`id`, every write path) and the actual capability mismatch
(new `CapAnalyticsWrite` + a narrow `smoke-reporter` builtin preset,
split off of `CapAnalyticsRead`, which was handed to three presets
explicitly documented as read-only). The split mattered because the
write path also serves `#54`'s cross-instance smoke-forwarding — one
federation peer POSTing results to another over a bearer token checked
against this same capability system — so a naive "just add
`CapAnalyticsWrite` to `full-control`" would have silently 403'd any
operator's already-working forwarding setup; documented as an
operator-action-required change instead. Also in v8.39.11: bumped the
4 stale Dependabot override floors in `channel/package.json` (§2,
`npm audit` now 0 vulnerabilities), and fixed a `js/double-escaping`
bug CodeQL found in v8.39.10's own new `app-escaping.test.js` helper
(order-dependent entity decoding could double-unescape a crafted
payload inside the test's own browser-decode simulation — caught while
re-checking this review's current alert state, not from the original
pass).

The `proxy.go` same-origin federation-peer-proxy risk (alert #545)
turned out more severe on a closer look — the PWA's auth token lives in
`localStorage`, not a cookie, so a compromised peer's proxied JS can
read it directly rather than merely "riding" an ambient session.

**v8.39.12** implemented the fix: a second, independent origin
(`server.proxy_sandbox_port`, new config field, default `8444`, full
accessibility — YAML/REST/CLI/MCP/comm-channel/Web UI per `AGENT.md`'s
Configuration Accessibility Rule) serving only `/remote/`+`/api/proxy/`,
with its own CSP whose `frame-ancestors` names the real main origin
instead of `'self'`, computed per-request so it's correct regardless of
which hostname reaches the daemon. `/remote/` on the main origin now
only 301-redirects there. Decided, after discussion, to start with a
port (not a per-peer subdomain, which needs real DNS/ingress) but
designed the one function any caller uses to resolve "which origin does
peer X's content use" so a subdomain scheme is a drop-in upgrade later,
not a rewrite. Iframe embedding was deliberately not built in this pass
— the existing UI already opens the remote PWA in a new tab, which a
genuinely separate origin already fully isolates via the browser's own
same-origin policy with no iframe/sandbox needed; the sandbox CSP
already allows embedding later if the UI changes to want it.

Live-testing the fix (not just reading the diff) found and fixed a
second, independent bug: `handleRemotePWA` was forwarding the proxied
remote peer's own response headers verbatim, including its own
Content-Security-Policy — landing a second, conflicting CSP on the same
response (multiple CSP headers combine as an AND per directive, so this
could have silently weakened or broken the new, correct policy
depending on what a given remote peer's own CSP said). Fixed by
stripping every known security-header name from the forwarded response.
Both fixes verified against a real running daemon with `curl`
(redirect, single correct CSP header, narrow route surface, config
round-trip), not just unit tests — full writeup in the plan doc's §6.

**v8.39.13** then built the iframe-embed UX v8.39.12 deliberately
deferred — the "PWA" link now opens the remote peer's dashboard inline
(a modal, same conventions as the existing file viewer) instead of only
a new tab. Driving a real headless Chrome against it (not `curl`, which
can't evaluate CSP/iframes/JS at all) found four more real bugs, one at
a time, each fixed before moving to the next: (1) the main dashboard's
own CSP had no `frame-src` and would have silently blocked the iframe
outright regardless of the sandbox origin's own policy; (2) the sandbox
origin's now-empty localStorage (the whole point of v8.39.12's fix)
left the embedded dashboard's own API calls with no token to send —
fixed with a new short-lived (1-hour), single-peer-scoped proxy token,
built only after checking in on the design with the operator first,
since injecting the real admin token instead would have undone the
origin split entirely; (3) static asset tag-loads (css/js under
`/remote/`) needed the same `?token=` treatment as the top-level page
load, since a `<link>`/`<script src>` can no more carry a header than a
navigation can; (4) a pre-existing, unrelated bug — `handleRemotePWA`
forwarding the browser's own `Accept-Encoding` header upstream — was
silently corrupting every gzip-compressed script/stylesheet it proxied
for any real browser (not `curl`, which doesn't send that header by
default), and has existed since before this entire review began. Final
verification included a full-page screenshot of the actual proxied
dashboard rendering real session/alert/config data inside the embedded
viewer. Full writeup, bug by bug, in the plan doc's new §6a.

**v8.39.15** closed out the remaining BL394 loose ends (dismissed the 3
CodeQL alerts that didn't auto-resolve after their fixes landed, since
CodeQL's interprocedural tracking doesn't connect a guard one call-frame
up from its sink — same pattern as the other post-fix dismissals in this
review; investigated §3g's SQL-`LIMIT` caveat and confirmed it's still
present but deliberately not fixed, since a naive `LIMIT` would silently
degrade search ranking quality rather than just bounding resource use —
a real design question, not a quick patch) **and then widened scope to
the two other open security-assessment threads** (BL365, Hostile-LLM —
see their own entries above) that turned out to contain real, still-live
findings of their own: SEC-004 (GitHub webhook, zero HMAC verification),
SEC-021 (docs-viewer stored XSS via unsanitized `marked.parse()`), and
HLLM-001's file-permission exposure (admin token in a world-readable
`.mcp.json`). Full writeup: plan doc's new §7.

**Plan doc:** [`2026-10-03-bl394-security-findings-review.md`](historical-plans/2026-10-03-bl394-security-findings-review.md)
**Status:** 84 false positives/accepted-risk dismissed on CodeQL, plus 3 more in v8.39.15 (87 total). 14 confirmed-real findings fixed across BL394/BL365/Hostile-LLM (push.go SSRF v8.39.3; council/skills path traversal v8.39.4; webhook image-read v8.39.5; cliPrompt secret echo v8.39.6; prototype pollution v8.39.8; reflected XSS v8.39.9; evals/layers_recursive query-param traversal + app.js escJsAttr + channel.js stack-trace + workflow permissions, all v8.39.10; api_smoke_progress.go path traversal + capability split, v8.39.11; proxy.go same-origin risk, v8.39.12 + its iframe-embed follow-up, v8.39.13; GitHub webhook HMAC + docs-viewer XSS sanitization + .mcp.json permissions, v8.39.15). The §2 Dependabot recommendation is also done (v8.39.11). 1 corrected from a mischaracterization and hardened as defense-in-depth rather than closing a live gap (email CRLF, v8.39.7). Deliberately deferred, documented, not silently dropped: `/api/health`'s cosmetic 401-when-proxied gap (§6a); the file-service root-fallback half of SEC-021 (§7); SEC-001/002/003/006/007/008's default-posture questions and HLLM-002's per-session-credential-scoping redesign (both real, both explicitly out of "quick fix" scope — see BL365/Hostile-LLM entries above); the §3g SQL-`LIMIT` memory-growth question. This review's remaining "needs review, not fully checked" item (`datawatch-app`'s own 11 CodeQL findings, handed off as `datawatch-app`#208) stays open and un-triaged here.

---

#### BL393 — Nested tags for Automata/PRD organization (filed 2026-10-03)

Operator-raised: Automata/PRD needs a folder or grouping mechanism to
organize multiple unrelated projects/efforts — today's list is one flat
stream with only status/type filter badges, no visual sectioning. Planning
session surfaced three real design forks (grouping source, terminology,
nesting) via direct operator decision rather than unilateral design choice
(explicitly requested: "do not make decisions, stop and ask me"). Decided:
**tags** (not "folder"), **both** explicit assignment and a suggested
default derived from the existing BL27 project-alias registry, a **managed
registry** of first-class Tag objects (`id`/`name`/`parent_id`/`color` —
not bare strings, so renaming propagates everywhere), **nested from the
start** (a tree, not flat), and **multiple tags per Automaton** with
multi-section browse behavior (standard label semantics).

Plan explicitly avoids repeating a real bug found in the closest existing
precedent: the `Type` registry (BL221) is in-memory only — grep confirms no
persistence file exists, so operator-registered custom types are silently
lost on every daemon restart. The new Tag registry is designed to persist
from day one (`tags.jsonl`, same JSONL pattern as the existing
`guardrail_profiles.jsonl`). Full design covers data model, storage, REST/
MCP/CLI/comm-channel/PWA surfaces, and four genuinely open implementation
questions (tag deletion semantics, parent-tag browse inheritance, suggested-
tag auto-create) flagged for the operator rather than silently decided.

**Plan doc:** [`2026-10-03-bl393-automata-tags.md`](historical-plans/2026-10-03-bl393-automata-tags.md)
**Status:** plan only — no code written, no settings changed. Not yet authorized for implementation.

---

#### BL392 — Cross-repo GitHub configuration hardening audit (filed 2026-10-03)

Operator-raised: compare `datawatch`'s repo-level GitHub protections (branch/tag
rulesets, CODEOWNERS, required checks, Dependabot, Actions permissions) against
`datawatch-app` and `datawatch-community`, and build a hardening plan for the
latter two. Audit found the premise only partly held — all three repos already
share matching `main-protection`/`tag-protection` rulesets from the same setup
date, and `datawatch-app` is actually *stricter* on PR review requirements than
`datawatch` (1 required approval + thread resolution vs. `datawatch`'s 0, which
instead relies on 6 required CI checks). The real gap is `datawatch-community`:
zero CI, zero required checks, no `SECURITY.md`, no `CODEOWNERS` — notable since
it's the one repo built specifically to accept third-party-authored submissions.
`datawatch` itself also has two gaps (no CODEOWNERS, Dependabot security updates
disabled) flagged but left out of scope since the ask was about the other two.

Same-day follow-up (operator spotted `datawatch`'s own Dependabot-alerts/code-
scanning split in the Settings UI): implemented and verified the additive
monitoring toggles across all three repos — CodeQL default-setup (datawatch:
go/javascript/python/etc.; datawatch-app: actions + java-kotlin, see below;
datawatch-community: no supported language, correctly skipped), private
vulnerability reporting (datawatch-app, datawatch-community), and Dependabot
security updates (datawatch, the one repo missing it). Real finding along the
way: datawatch-app's first CodeQL run failed `Analyze (swift)` — Autobuild
can't find an Xcode project/workspace in this Kotlin-Multiplatform iOS setup
(genuine build-tooling limitation, not a flake); fixed by dropping `swift`
from the language list (actions + java-kotlin both analyze cleanly). Proper
Swift coverage needs a custom `codeql.yml` with an explicit build step — not
attempted, left as a follow-up.

Same-day Phase 1 (operator authorized directly: "go ahead and start Phase 1"):
`datawatch-community` now has `.github/workflows/validate.yml` +
`scripts/validate_registry.py` — lightweight schema validation for every
skill/plugin submission (required manifest fields, name/category match
directory, plugin `entry` script exists and is executable), wired in as a
required status check on the `main-protection` ruleset, plus `SECURITY.md`
(scoped to reporting malicious/vulnerable submissions, not daemon CVEs) and
`CODEOWNERS` (`* @dmz006`). Running the validator against the real repo
before trusting it as a required check surfaced 3 genuine pre-existing
violations, all fixed: a skill with no frontmatter at all
(`image-reviewer`), two plugins' `run.sh` not executable in the git tree
(`workspace-git-sync`, `workspace-rsync-sync`), and one plugin mis-filed
under `skills/` using a non-standard `skill.yaml` instead of a real plugin
`manifest.yaml` (`workspace-nfs-mount`, moved to `plugins/ops/`).
`required_approving_review_count` stays 0 — manual operator review remains
the real safety model; CI is a second line of defense, not a replacement.

Same-day Phase 2 (operator authorized directly: "go ahead and start Phase 2"):
flipped `require_code_owner_review` to `true` on `datawatch-app`'s
`main-protection` ruleset — its `CODEOWNERS` file already existed and
listed real protected paths, it just wasn't being enforced. No other
changes; this repo's review strictness was already ahead of `datawatch`'s
own baseline and the plan explicitly said not to weaken it to "match."

Same-day cleanup of the last two §3c baseline gaps: added `datawatch`'s own
missing `CODEOWNERS` (parse-verified clean via the `codeowners/errors`
API; deliberately did *not* flip `require_code_owner_review` here — this
repo's 0-approval/6-required-CI-checks review model is intentional, unlike
`datawatch-app`). Also answered the operator's question of whether the
Phase 2 CODEOWNERS flip needs any `datawatch-app` CI changes: no —
`require_code_owner_review` is a native GitHub merge gate, unrelated to
Actions workflows. But comparing the two repos' actual security workflows
surfaced a real, separate parity gap: `datawatch` has a PR-time
`dependency-review-action` gate (fails a PR on a newly-introduced
HIGH-severity CVE or GPL/AGPL dependency) that `datawatch-app` lacks —
its own security scan is cron-scheduled, not PR-gated. Filed
[`datawatch-app`#206](https://github.com/dmz006/datawatch-app/issues/206)
rather than implementing cross-repo; flagged the Gradle dependency-graph
submission caveat up front so whoever picks it up doesn't ship a gate that
silently reviews nothing.

Also filed [`datawatch-app`#207`](https://github.com/dmz006/datawatch-app/issues/207)
for the Swift CodeQL gap (§7), with a worked custom-workflow draft reusing
`ios-build.yml`'s own build steps (the real reason default-setup's
autobuild fails: the Xcode project is generated at build time by
`xcodegen`, not committed) — not implemented directly, since getting it
green needs real CI iteration against this repo's Xcode toolchain.

**Plan doc:** [`2026-10-03-cross-repo-github-hardening.md`](historical-plans/2026-10-03-cross-repo-github-hardening.md) (§7 CodeQL follow-up, §8 Phase 1, §9 Phase 2, §10 datawatch CODEOWNERS + both filed issues)
**Status:** everything within direct reach is done — all three phases from §4, plus `datawatch`'s own CODEOWNERS gap. The two remaining items are real cross-repo implementation work, now tracked as `datawatch-app`#206 (dependency-review-action) and #207 (Swift CodeQL).

---


#### BL390 — Council multi-backend persona assignment + capacity integration + PRD gate wiring (filed 2026-09-30)

Operator-raised gap: Council persona settings have no LLM→model picker; `Persona.Model`
exists but is dead code (every call always uses the one orchestrator-wide `LLMRef`); council
calls don't participate in the node:/llm: capacity ledger autonomous PRD tasks use; and
`backend: "council"` in the PRD-creation wizard is a UI-only option with zero dispatch behind
it — selecting it does not work today. Three-phase plan: (1) cascading LLM→model dropdown,
multi-backend pool sourced from the existing LLM registry, per-persona backend/model
assignment, capacity-ledger integration for parallelism; (2) named council profiles —
multiple saved persona/backend-pool/engagement-mode (parallel vs round-robin) configurations
instead of one global config; (3) wire a council profile into the PRD verifier gate as a
real, working alternative to the existing single-LLM `/api/ask` verification path, plus a
per-PRD council-profile override field (the Automata-page entry point).

**Plan doc:** [`historical-plans/2026-09-30-council-llm-model-assignment.md`](historical-plans/2026-09-30-council-llm-model-assignment.md)
**Status:** Phase 1 done, shipped v8.38.0. Phase 2 (named council profiles) and Phase 3 (wire `backend: "council"` into real PRD verify-gate execution) planned — **now Phase 8 sub-phase 8a of BL405** (see below); implement as designed here, don't re-decide. BL405 adds a true pipeline-handoff engagement mode (8b) and extends profile selection beyond the verify-gate (8c, needs explicit operator go-ahead — revisits this plan's own "not in scope: council as execution backend" line).

---


## Pending backlog

> **BL406** — Configurable Project-Rules Enforcement: a fourth,
> data-driven scan category (`project-rules`, alongside sast/secrets/
> deps) that checks task/story/PRD content against operator-configured
> project rules — parity-surface inheritance, version-sync, CHANGELOG
> presence, and upstream-issue filing — plus real fixes for B111/B112
> (filed 2026-10-10, found while grounding BL405's single-thread
> backlog-execution workflow design). Plan:
> `docs/plans/2026-10-10-configurable-project-rules-enforcement.md`. 6
> phases, not started. Designed to generalize past this repo: rules/
> context-file paths and upstream-repo targets are operator-configured,
> never hardcoded. Recommended as the **first** thing built in the
> BL403/404/405/406/407 arc — everything after it benefits from being
> checked by it, and building it via today's manual process (not PRD)
> avoids the bootstrapping problem of using a not-yet-built safety net to
> build itself.

> **BL407** — PRD Git Workflow: branch-per-PRD + auto-PR on completion.
> Full plan: `docs/plans/2026-10-10-prd-git-workflow.md` (written
> 2026-10-10, 7 phases 0-6, not started). Two rounds of live
> investigation while planning found the problem is bigger than "add
> git wiring": `PRD.ClusterProfile` dispatch isn't functionally wired
> today independent of git (synthetic session ID that never resolves in
> the verify loop, container task-invocation still a TODO) — filed as
> **B114** (see Open Bugs), fixed as the plan's own Phase 0 rather than
> deferred, per operator decision to build both local-worktree and
> container-dispatch git modes in this arc together. Local git worktree
> (`~/.datawatch/prd-worktrees/<prd-id>/`) is the isolation mechanism for
> self-build/dogfooding work; container dispatch reuses the
> already-isolated `ClusterProfile` path. One PR per PRD at completion
> (not one per task), reusing `git.Provider.OpenPR` and
> `session.ProjectGit`'s existing per-task commit mechanism — no new
> git abstraction. Interim session telemetry during a run
> (`docs/plans/2026-10-04-f2-session-worker-isolation.md` §11) is
> cross-referenced, explicitly out of scope. **Closed 2026-10-11**: all
> 7 phases (0-6) shipped, latest v9.0.23 (Phase 6 was docs-only closure,
> no version bump); Stage 1 of the self-build roadmap is now complete.

> **BL405** — LLM Enhancements: role-aware scheduling, federated
> capacity, council-as-reusable-profile, usage/cost tracking (filed
> 2026-10-10, iterated from `llm-research/docs/07-datawatch-
> improvements.md`'s Part A/B proposals through live design discussion).
> Plan: `docs/plans/2026-10-10-llm-enhancements.md`. 11 phases (0-10), not
> started. Absorbs/supersedes B3 (node/model dashboards → new top-level
> Scheduler surface) and B5 (model scorecard → a `CouncilProfile` fanning
> one prompt across backends); B1's session-level aggregate is already
> fixed (B109) — this plan builds the richer per-call record. Phase 8
> implements BL390 Phase 2/3 (named council profiles, never started) and
> extends it; Phase 8c needs explicit operator go-ahead since it revisits
> BL390 Phase 3's own "not in scope" line. Also clarifies a session
> memory: the broader PWA/all-surfaces multi-hop architecture pass is
> still correctly deferred/unscheduled, but
> `internal/federation/hopchain.go` (GH#201 Phase 2, 2026-10-09) already
> ships a narrow, nesting-aware hop-chain for LLM-proxy attribution — so
> Phase 7 here (giving capacity/scheduling a federation presence) rides
> existing plumbing rather than building multi-hop from scratch.

> **BL404** — `imap_mcp` messaging channel has zero REST/MCP/CLI/comm/
> PWA config exposure (filed 2026-10-10, found while fixing the B110
> outbound-bounce bug). The entire `imap_mcp.*` config block
> (`enabled`/`url`/`account`/`subject_prefix`/`token`, now also `to`)
> is YAML-only — `applyConfigPatch` (`internal/server/api.go`) has zero
> `imap_mcp.*` cases, so it's unreachable via `PUT /api/config`/
> `config_set`/`configure`/PWA Settings, violating the Configuration
> Accessibility Rule's "every config channel" bar. Also:
> `docs/messaging-backends.md` has no `imap_mcp` section at all. Not
> fixed as part of B110 — that was a focused bounce-bug fix, not a
> retroactive full-parity pass for a block that's been exposed this way
> since BL340 (v8.9.23). Not started.

> **BL403** — Plugin Extension Surfaces: dashboard/session widgets,
> service-kind plugins with optional lifecycle supervision, federation
> & audit (operator-directed 2026-10-09, starting from "can imap-mcp
> get a dashboard status card?"). Plan:
> `docs/plans/2026-10-09-plugin-extension-surfaces.md`. 10 phases, not
> started. Investigation found the real gap is deeper than one card:
> plugin manifests have a `Mode: "long-lived"` field nothing in the
> codebase ever reads (dead field — every plugin invocation today is a
> synchronous oneshot subprocess call); no dashboard-card-content
> extension point exists at all (only layout/span for a fixed closed
> set of 11 built-in card IDs); session detail has no widget-slot
> architecture whatsoever. Operator decisions, collected directly this
> session: Plugin Manifest extends v2.1 → v3 (not a parallel system);
> new `kind: automation|service`, with service-kind split into a
> required `connection` block (works fully standalone) and an optional
> `lifecycle` block the plugin's own manifest declares support for
> (operator can't force a mode the author didn't build); widget
> transport is push+pull, chosen per-widget; render contract is 5 fixed
> templates + one markdown kind (no raw HTML); dashboard AND session
> widgets both from day one; new `CapPluginRead`/`CapPluginWrite`
> federation capabilities; `datawatch-app` issue filed at plan-approval
> time, not deferred — the fixed-template contract is chosen
> specifically to make native Android/iOS rendering possible. An
> explicit, later, non-blocking phase (9) migrates datawatch's own
> simplest built-in cards (memory-scope, websearch-usage, guardrails,
> smoke) onto the new generic renderer as a dogfood validation step;
> the genuinely bespoke interactive cards (orbital, gantt, ekg, heatmap,
> tree) are explicitly excluded from that migration, not deferred by
> oversight — and comes *after* imap-mcp (Phase 9), the first real
> adopter and the actual goal driving Phases 1-7, not an example
> tacked on at the end.

> **BL402** — GH#204: result panel for scheduled jobs and integrations
> (filed by a peer session at the operator's request, 2026-10-09).
> Plan: `docs/plans/2026-10-09-gh204-result-panel.md`. 5 phases, not
> started. Investigation before planning corrected two of the issue's
> own assumptions: subprocess-mode sessions get *zero* environment
> injection today (not "already gets DATAWATCH_SESSION_ID" as assumed
> — confirmed by reading `runSubprocess()`), and the generic BL360
> result store is the wrong fit for schedule-keyed history (no
> per-caller scoping, no count-based retention) — a new dedicated
> store was decided instead. Operator decisions: reuse the existing
> Design-A3 `SessionTokenStore` rather than a second token system;
> general mechanism for any session type, not subprocess-only; new
> dashboard card is opt-in (not default); retention configurable,
> default 20. Explicit commitment in the plan itself: Phase 3/4's PWA
> changes get a `datawatch-app` issue filed in the same
> session/commit that ships them — not after, per today's own earlier
> compliance miss (see app#241).

> **BL400** — Alerts: conditions, filtering, and SIEM parity (filed
> 2026-10-09, operator-raised while reviewing GH#201 Phase 5's
> `create_alert` docs: "alerts on every event is really just a log
> event queue" — confirmed real, not hypothetical: `internal/alerts`
> has zero SIEM/CEF integration of its own, and `create_alert` has no
> condition/filter/priority, unlike the separate, pre-existing
> alert-rules system). Plan:
> `docs/plans/2026-10-09-alerts-conditions-and-filtering.md`. Design-
> options doc only, explicitly deferred — no implementation scheduled.
> Three open questions for whenever it's picked up: where filtering
> logic should live (extend alert rules vs. a new smaller policy
> concept vs. just add category/priority fields), whether alerts
> get their own CEF mirror or feed into the existing audit-log
> pipeline, and whether a dedupe/rate-limit window is worth adding.

> **BL398** — Full review of suppressed container-image CVEs (filed
> 2026-10-08, operator-requested immediately after SEC-026/v8.73.26's
> CVE-2026-19445 suppression: "set up a plan to review all suppressed cve
> to see if they impact datawatch, good for documentation and
> mitigation"). Plan: `docs/plans/2026-10-08-bl398-trivyignore-cve-review.md`.
> `.trivyignore` has 71 CVE/GHSA IDs across ~26 package-group blocks,
> accumulated opportunistically release-by-release; only the 3 most
> recent entries carry an honest CAVEAT admitting the actual code path
> was never independently traced, and `docs/security-review.md`'s
> container-scanning section is stale (documents only the original
> v8.0.0-era 19 exceptions). 5 phases: reconcile ≥4 blocks where the
> prose describes more CVEs than are actually suppressed; a fresh
> per-image Trivy rescan baseline; re-verify every un-CAVEATed block's
> rationale against the real container contents instead of re-asserting
> it; a mitigation pass looking for packages to delete outright (rsync's
> 14-CVE block and gawk are the top candidates) rather than suppress
> forever; and bringing `docs/security-review.md` back in sync.
> **Phases 0-1 shipped (v8.73.30-32):** `security/accepted-risks.yml`
> structured registry (70 entries, real per-ID severity/images/dates,
> generates `.trivyignore` + `docs/security-review.md`'s table —
> reconciliation and doc-sync done this way instead of hand-editing both);
> `scripts/check_accepted_risks.py` blocking CI lint; daily watch extended
> with fix-published/expiry/dismissed-alert-gap reporting
> (`scripts/dismissed_alerts_watch.py` found 87/87 dismissed code-scanning
> alerts unregistered); GHSA-regex fix in `image-refresh.yaml`; gitleaks
> now blocking; CVE-2026-19445 flipped from untraced to traced. Adopted
> jointly with `datawatch-app` as a shared standard, tracked on GH#197
> (**closed 2026-10-08** — standard fully implemented and operating on
> both repos).
> **The 87/87 dismissed-alerts gap is now closed (v8.73.37):** all 87
> migrated into the registry as `kind: code-scanning` entries (158 total
> now), each using the alert's own real GitHub `dismissed_comment` as its
> `impact.analysis` rather than new analysis — these were already reviewed
> and justified during BL394, just never mirrored into the structured
> registry. `dismissed_alerts_watch.py` now reports 0/87 unregistered.
> **A real flaw in the daily recheck's own automation was found and fixed
> this session (v8.73.36):** `recheck-ignored-cves` treated "suppressed
> CVE ID absent from a fresh scan" as proof of a real fix. Confirmed
> unsound live — Trivy's vulnerability DB relabels CVE IDs for an
> unchanged package several times a day (`zlib1g` same version, two
> different IDs hours apart) — and it auto-opened two PRs (#199, #200,
> both closed without merging) that would have deleted still-valid
> suppressions on that basis, including the just-traced `CVE-2026-19445`.
> Replaced with `scripts/compute_stale_risks.py`, which anchors staleness
> to the installed package version instead of ID presence. Documented in
> `docs/howto/container-image-refresh.md`; shared with `datawatch-app`
> (their #209) since the schema is shared, though their own automation
> checks real published package versions already and doesn't have this
> failure mode.
> **Phase 2 (re-verify the ~69 migrated *container*-CVE entries that are
> still prose-only, not independently traced) remains open and unstarted
> — the one genuinely unfinished piece of BL398.** It's a real, sizable
> body of work (an independent reachability trace per entry, the way
> CVE-2026-19445's was done), not something to rush through inline, and
> the CVE-ID-churn finding above makes the bar for "verified" here
> explicitly about reachability against real container contents, not
> about matching IDs against a scan.
> **Phase 3 (removal-over-suppression) — decided, not pursued:** the
> operator reviewed removing rsync (the 14-CVE block) and gawk, and
> declined both — rsync is relied on, gawk removal wasn't judged
> worthwhile on its own. This is a closed decision, not a dropped task.

## Open backlog (deferred / awaiting operator action)

**Quick map:** items where I can keep working sit in **Active work** below. Items where I'm blocked on an operator decision sit in **Awaiting operator action** with a structured "what's needed + recommendation" per item. Items shipped recently sit in **Recently closed** for one release cycle; long-term / external items sit in **Frozen / External**.

### Open backlog — deferred (filed, no decision needed, pick up when ready)


### Active work (no decision needed — keep iterating)

> **BL316** — Cross-host session federation + CBAC GH#52 (v7.3.0). **S1 shipped 2026-05-18**: capability-based access control (CBAC) package (`internal/federation/`) — 50 individual cap strings, 13 builtin groups, `Resolve()`/`Check()` functions, `GroupStore` with full CRUD + persistence. REST API: `GET/POST/PUT/DELETE /api/federation/peers{/name}`, `POST /api/federation/peers/{name}/test`, `GET/POST/PUT/DELETE /api/federation/groups{/name}`, `GET /api/federation/groups/builtins`. `fedAuthMiddleware` accepts peer tokens alongside admin token. Capability enforcement wired at: REST (sessions list/write/kill/input, MCP call, start session), WebSocket (command/new-session), and via `fedCap()` helper callable from all handlers. Multiserver Entry extended with `Capabilities []string` + `GetByToken()`. New peer defaults to `federation-peer` group. 12 new integration tests pass. **S2 ✅ shipped v8.67.0 (2026-10-07)**: audited before touching — confirmed `RemoteDispatcher.servers` was a frozen `cfg.Servers` snapshot from daemon startup, never refreshed from the live `multiserver.Store`, so cross-host comm-channel `send` and the CLI's `--server` flag couldn't see any peer added after startup (`federation peer add`, PWA panel, `POST /api/servers`) until a restart. Fixed via `RemoteDispatcher.SetStore()` + `effectiveServers()` (reads the live store on every call); CLI `--server` now routes through the local daemon's existing `/api/proxy/<name>/...` passthrough instead of resolving the remote directly from its own stale config snapshot. 7 new tests, full details in `docs/testing-tracker.md`. Plan doc (backfilled 2026-10-07 per the compliance-audit rule check): `docs/plans/historical-plans/2026-10-07-bl316-s2-bl317-federation-dispatch.md`.






| ID | Item | Status |
|----|------|--------|
| **BL226** | **Service-level alert stream + System tab** — `source:"system"` field, `AddSystem`/`EmitSystem` global, 4 instrumentation sites, REST/MCP/CLI/Comm/PWA System tab. | ✅ **v6.0.9** |
| **BL228** | **Scheduled commands + security scanners** — `schedule add/list/cancel` across 6 surfaces; security scanners in language Dockerfiles (`govulncheck`, `bandit`, `pip-audit`, `eslint-plugin-security`, `cargo-audit`, `brakeman`). | ✅ **v6.0.6** |
| **BL241** | **Matrix.org communication channel** — design interview required; mautrix-go likely approach. See Open Features. | Open — design; v6.2+ |
| **BL248** | **Rate-limit detection overrides saved commands** — `StateRateLimited` guard in `tryTransitionToWaiting()`. | ✅ Closed v6.5.1 |
| **BL249** | **Session auto-reconnect after daemon restart** — reconnect handler fetches `/api/sessions` and patches each record. | ✅ Closed v6.5.1 |
| **BL250** | **Session state refresh after popup dismiss** — `dismissNeedsInputBanner` fetches `/api/sessions` after dismiss. | ✅ Closed v6.5.1 |
| **BL253** | **eBPF setup false-positive** (GH#37) — kernel ≥5.8 enforcement, `cap_sys_resource`, rlimit probe + unprivileged_bpf_disabled check. | ✅ Closed v6.5.1 |
| **BL268** | **`datawatch-definitions.md` end-to-end population** — all sections + TODO placeholders resolved. | ✅ Closed v6.13.7 |
| **BL269** | **`openAutomataHowto()` opens definitions doc** — links to `/diagrams.html#docs/datawatch-definitions.md#automata`. | ✅ Closed v6.12.1 |
| **BL270** | **Select-bar-fixed above bottom nav** — `.select-bar-fixed { bottom: var(--nav-h) }` + horizontal-scroll variant. | ✅ Closed v6.12.4 |
| **BL271** | **wizard-grid-2col + wizard-mobile + Start-from-template strip first** — mobile-first wizard overhaul. | ✅ Closed v6.13.1 |
| **BL272** | **Settings section padding normalization** — `8px 14px` inset + 6px inter-card gap. | ✅ Closed v6.12.4 |
| **BL273** | **`/docs/` served by FileServer** — every `?`/`docs` link routes through `defsLink()`/`docsLink()`. | ✅ Closed v6.13.7 |
| **BL274** | **Docs-as-MCP-interface** — hybrid index, 4 MCP tools, 22 curated howtos with exec_steps, plan-then-execute, 7-surface parity. | ✅ Closed v6.21.0 |
| **BL277** | **Remove yellow "Input Required" popup** — all banner HTML/CSS/JS removed; `.input-bar.needs-input` border kept. | ✅ Closed v6.13.9 |
| **BL278** | **Light mode / dark mode toggle** — `[data-theme="light"]` palette, FOUC-safe bootstrap, `localStorage['cs_theme']`. | ✅ Closed v6.13.11 |
| **BL279** | **Embedded docs viewer UX** — tighter spacing + 48-doc See-also footer sweep. | ✅ Closed v6.14.0 |
| **BL287** | **PWA mic input regression** — toasts at every voice state + defensive state.voice.chunks access. | ✅ Closed v6.20.0 |
| **BL288** | **Settings → About card padding** — `.settings-section .settings-row padding: 6px 14px`. | ✅ Closed v6.19.0 |
| **BL289** | **Document Ollama use + no-GPU fallback tests** — 7 features enumerated, per-path fallback tests. | ✅ Closed v6.22.2 |
| **BL290** | **`datawatch-stats --help` double-dash flag form** — custom usage printer + typo fix. | ✅ Closed v6.19.0 |
| **BL291** | **Observer settings findable in PWA** — new Federated Observer card in Settings → General. | ✅ Closed v6.20.0 |
| **BL297** | **Council "Add Persona" wizard** — SQLite drafts, LLM one-shot + edit + re-interview, 7-surface parity. | ✅ Closed v6.22.3 |
| **BL298** | **Toast / error UX** — `showError()` helper (16px, no auto-dismiss, ✕ button); ~15 app error paths converted. | ✅ Closed v6.22.3 |
| BL190 | **Howto screenshot density** — 22 shots across 8 howtos; below the 15-20-per-howto target. | Iterative cosmetic; pick up only if an operator hits a recipe gap. |


#### BL241 — Matrix.org channel: design interview needed

**What's needed:** Operator-driven design session to choose the Matrix integration approach.

**Options:**
1. **mautrix-go bridge** — proven Go library (matrix-org/mautrix-go), handles federation, encrypted DMs, bridging to other networks. Actively maintained.
2. **Native Matrix Client-Server API** — implement via `net/http`; more control, more effort.
3. **go-coap / go-libp2p** — lower-level matrix-org protocols; better suited to IoT/P2P than general chat.

**Recommendation:** Option 1 (mautrix-go) — most complete Go Matrix SDK, covers rooms/DMs/bots/federation, straightforward to integrate alongside existing comm-channel backends.

---

### Historical changelog archive (v4.7.0 – v8.9.23 era)

_Corrected 2026-10-09/10 backlog refactor: this subsection's own header used to claim "sticky for one release cycle, then archived" — but everything below is many months and ~400 versions old (v4.7.0 through v8.9.23). It was never actually archived out despite the header's own description. Left in place for its richer historical detail (not re-deleted/re-condensed), but mislabeled no longer — this is a permanent historical record, duplicating terser one-line summaries of the same items in the Completed Backlog table below. Treat the Completed Backlog table as canonical for "is this done"; treat this as archaeology._

**v8.9.23 (2026-06-07):** BL340 — imap-mcp email command channel. New `imap_mcp` messaging.Backend (`internal/messaging/backends/imapmcp/`) connects datawatch to a running imap-mcp server. Receive: subscribes to `GET /api/events` SSE stream, acts only on `inbound.command` events (trust boundary in imap-mcp — allowlist/DKIM/DMARC/HMAC/replay-nonce already evaluated). Send: `POST /api/accounts/{account}/messages/send`. imap-mcp v0.2.0 delivers both endpoints (SSE fan-out from in-process bus, SMTP send via existing smtp package). Config: `imap_mcp.enabled`, `url`, `account`, `subject_prefix`. Wired into `GET /api/channels`, comm status table, config defaults. 9 unit tests. Closes GH#127.

**v8.9.22 (2026-06-07):** BL339 — Container builder CVE fix. `Dockerfile.agent-base` `ARG GO_VERSION` pinned from floating `1.25` (resolved to `1.26.3`) to exact `1.25.11`; `ENV GOTOOLCHAIN=local` added to builder stage. Resolves `CVE-2026-42504` (HIGH, stdlib MIME header DoS) that blocked agent-base Trivy scan and skipped all agent container builds (agent-claude, agent-opencode, agent-aider, agent-gemini, agent-goose, parent-full, stats-cluster tarball) since v8.9.17. `.trivyignore` review date updated.

**v8.9.21 (2026-06-07):** BL338 — Self-update archive priority + channel binary co-update. `installPrebuiltBinary` now downloads the goreleaser archive first (correct primary format since v8.x), uses `runtime.GOOS/GOARCH` (no Go toolchain required), and updates `datawatch-channel` sibling alongside the main binary. `downloadChannelBinary` fixed to extract from goreleaser archive (was always trying non-existent bare binary `datawatch-channel-linux-amd64`). Both `datawatch update` CLI and auto-updater now call `downloadChannelBinary` after each successful main-binary update so `<dataDir>/channel/datawatch-channel` stays in sync. Legacy bare-binary fallback retained for pre-goreleaser releases.

**v8.9.20 (2026-06-07):** BL337 — Line-printing renderer lock. `ensureClaudeTUISetting` writes `"tui":"default"` into the datawatch-scoped `settings.json` (`<dataDir>/.claude/settings.json`) at `NewManager` startup, merging with existing settings, idempotent. Pins claude-code to the pre-flag line-printing renderer so tmux scrollback and `pipe-pane` capture are not disrupted by Anthropic's server-side `tengu_pewter_brook` flag (~Jun 2–6 2026) which flips claude to alternate-screen mode (`\e[?1049h`).

**v8.9.17–v8.9.19 (2026-06-05–2026-06-06):** BL336 — Anti-clobber typing detection + queue. Agent channel sends to an active session now wait for 30 s of operator keystroke idle (`lastRawInputAt` from `SendRawKeys`, polled at 300 ms) before injecting, so agent messages don't clobber mid-keystroke operator input in xterm. Multiple messages that arrive while the operator is typing queue up (per-session buffered channel, cap 256, single drain goroutine); first message in each burst waits for idle, remainder flush immediately after in arrival order. Queue closes cleanly on session delete. v8.9.19 fixed regression where session-to-session sends were also held (TTY mtime watches AI output, not user keystrokes; switched to `lastRawInputAt`). `TmuxAPI.PaneTTY`, `Manager.WaitTypingIdle`, `Manager.QueueChannelSend` added. Also in this window: Go bridge port-7433 duplicate-bridge fix (v8.9.15–v8.9.16), JS bridge memory parity (v8.9.13–v8.9.14), Go 1.25.11 CVE fix.

**v7.2.3 (2026-05-18):** BL315 — Fullscreen PWA mode. Header gains a ⛶ toggle button (Fullscreen API, works in any browser) and a ⬇ Install button that appears when the browser fires `beforeinstallprompt` and disappears after install. pwa-setup.md updated. TS-149 added to T11.

**v7.2.2 (2026-05-18):** BL314 — PWA voice button gate. Session detail mic button now hidden when `whisper.enabled` is false (consistent with generic mic factory which already gated on `state._whisperEnabled`). Redundant `_dashCheckEnabled()` call removed from `_dashUpdateStatBar()` (boot-time check already handles both nav buttons). GH#62 closed — dashboard nav was already gated by BL313; GH#61 closed. TS-147/TS-148 added to T11 PWA sprint. Issue triage workflow added (external issues auto-labeled `needs-owner-review`). Makefile docs-index target seeds `{}` placeholder on fresh clone (closes GH#65).

**v7.0.0-alpha.39 (2026-05-11):** Full v7.0 backlog sweep — BL292 (mic auto-attach confirmed comprehensive), BL293 (card button matrix documented per-state), BL294 (session registry flock sidecar — race fix), BL295 (Council InferenceFn wired to LLM dispatcher), BL296 (4 new personas + 7-surface CRUD: REST/MCP/CLI/comm/PWA/YAML), BL299 (narrow-screen 2-row header + card wrapping), BL300 (alert dock auto-open closed — gate on dock.expanded). All open pre-7.0 items closed; BL241 remains open pending design interview.

**v7.0.0-alpha.38 (2026-05-11):** LLM CLI parity (models list/add/remove, in-use, refresh, reassign, force-delete). Observer fix for compute-node detail when monitoring_endpoint not set. Automata PWA modals use showConfirmModal instead of browser confirm(). Rule-violation audit batch: version sync (api.go → 7.0.0-alpha.38), CommFirehose REST+PWA+CLI parity (llm_ref/max_parallel/comm_firehose on /api/council/config), locale fixes (prd_* keys + council_cfg_* × 5 bundles), README rewrite, BL299+BL300 filed.

**v7.0.0 alpha.1–alpha.37e (2026-05-08–2026-05-11):** ComputeNode registry (add/edit/delete/monitor/observer-peer integration), LLM Registry+dispatcher (ollama/openwebui/opencode/claude adapters, ordered failover, enabled-models, in-use bindings, reassign, force-delete), Council wired to real LLMs per-persona, SSE live updates for LLM/compute panels, Scoped memory with namespace isolation, Ollama Marketplace (model discovery/pull/progress), Claude Code hooks + Status board (hook-event stream, per-session state display), Alert dock pill-only (no auto-open), showConfirmModal replacing browser confirm().

**v6.22.3 (2026-05-08):** BL297 — Council "Add Persona" wizard (SQLite drafts, LLM-assisted one-shot + edit + re-interview, 7-surface parity). BL298 — Toast/error UX (showError() with 16px font, no auto-dismiss, ✕ button; ~15 app error paths converted).

**v6.6.0 (2026-05-04):** BL246 fully closed (items 1, 5, 6 — tabbed detail view with Overview/Stories/Decisions/Scan, persistent header toolbar exposing every PRD API verb as a button, split Edit Spec / Settings modals, hidden-by-default per-card checkboxes with select-mode toggle); BL252 closed (Phases 6 + 7 collected — header nav titles, FAB titles, terminal/voice states, status indicators, update progress, memory tools, audit/analytics empty states, Signal device link states, KG queries, toast messages — 69 keys this cut, ~190 total across 7 phases). Smoke: 91/0/6.

**v6.5.7 (2026-05-04):** BL252 Phase 5 — Settings panel i18n (auth, servers, communications, About, dynamic update strings — 24 keys).

**v6.5.6 (2026-05-04):** BL252 Phases 3+4 — PRD lifecycle strip + CRUD modals + stories/tasks tree + Stats card section headings + Alerts empty states (70 keys).

**v6.5.5 (2026-05-04):** BL252 Phases 1+2 — sessions list, session detail toolbar, chat role labels, Mermaid renderer, schedule-input popup, timeline panel, new-session form, channel help (53 keys).

**v6.5.4 (2026-05-04):** BL251 — Agent auth/settings injection. `AgentSettings` struct on `ProjectProfile` (`claude_auth_key_secret`, `opencode_ollama_url`, `opencode_model`); spawn-time secret resolution + env injection; 7-surface parity (REST `PATCH /api/profiles/projects/{name}/agent-settings`, MCP `profile_set_agent_settings`, CLI, comm, PWA editor form, locale, YAML).

**v6.5.3 (2026-05-04):** BL243 Phase 3 — ACL policy generator + push with existing-node awareness. `internal/tailscale/acl.go` `GenerateACLPolicy()` + `GenerateAndPushACL()` with tag-owner declarations, agent-mesh rules, allowed-peer ingress, catch-all preserve rule. New `POST /api/tailscale/acl/generate` + `POST /api/tailscale/acl/push` (empty body auto-generates). MCP `tailscale_acl_generate`. CLI `datawatch tailscale acl-generate/acl-push`. Comm + PWA + locale parity.

**v6.5.2 (2026-05-04):** BL243 Phase 2 — OAuth device-flow activation via comm channel; headscale pre-auth key generation with 7-surface parity.

**v6.5.1 (2026-05-04):** BL247 (Settings tab & card reorganization) + BL248 (rate-limit detection guards saved commands) + BL249 (session auto-reconnect after daemon restart) + BL250 (session state refresh after Input Required popup dismiss) + BL253 (eBPF setup false-positive, GH#37) + BL246 partial (items 2/3/4/7 — Launch Automation FAB, stale help-text replacement, "…" dropdown right-anchored, workspace label clarified, Skills "coming soon" removed).

**v6.5.0 (2026-05-04):** BL243 Phase 1 — Tailscale k8s sidecar mesh, headscale client, 7-surface parity, ${secret:name} integration. Hotfix: JS template literal syntax error (`${secret:name}` inside backtick string in app.js) broke PWA load entirely. Smoke: 91/0/6.

**v6.4.x (2026-05-03):** BL242 all phases — AES-256-GCM secrets store, KeePass, 1Password, config `${secret:name}` refs, spawn-time env injection, scoping, plugin env injection, agent runtime token.

**v6.3.0 (2026-05-03):** BL244 Plugin Manifest v2.1 — comm routing, CLI subcommands, mobile declarations, session injection.

**v6.2.x (2026-05-03):** BL221 Automata redesign (all phases), BL239/BL240, BL245 schedule date fix.

**v6.1 batch (2026-05-03):** BL218/BL219/BL226/BL228 + BL230–BL238 all shipped and closed in v6.0.6–v6.0.9; collected into v6.1.0 minor.

| ID | Closed in | What |
|----|-----------|------|
| BL228 — Scheduled commands + security scanners | v6.0.6 | `schedule add/list/cancel` across 6 surfaces (REST + MCP + CLI + Comm + PWA + YAML). Security scanner tools added to language Dockerfiles (`govulncheck`, `bandit`+`pip-audit`, `eslint-plugin-security`, `cargo-audit`, `brakeman`+`bundler-audit`). |
| BL218 — Channel session-start hygiene | v6.0.7 | SHA-256 hash for `EnsureExtracted` staleness check; `SweepUserScopeMCPConfig` rewrites `~/.mcp.json` on every pre-launch; pre-launch log line emitted per bridge wiring. |
| BL219 — LLM tooling artifact lifecycle | v6.0.8 | `BackendArtifacts` registry; `EnsureIgnored` appends patterns to `.gitignore` idempotently on session start; `CleanupArtifacts` removes ephemeral files on session end. YAML `session.gitignore_check_on_start` / `session.gitignore_artifacts` / `session.cleanup_artifacts_on_end` + full 6-surface parity. |
| BL226 — Service-level alert stream + System tab | v6.0.9 | `Source` field on `Alert`; `AddSystem`/`SetGlobal`/`EmitSystem` global; instrumented pipeline task failure, executor panic, eBPF probe init, plugin Fanout. REST `?source=system`, MCP `source` param, CLI `--system`, Comm `alerts system`, PWA System tab with red unread badge. |
| BL230–BL238 — PWA audit batch | v6.0.2–v6.0.3 | 9 bugs from 2026-05-02 PWA audit: analytics field mismatch, nested-key rendering, internal version string, sprint ID in UI, duplicate language card, branding location, select dropdowns, docs chips, nav restructure (Plugins/Routing/Orchestrator → Settings sub-tabs). |

**Audit (v5.27.0, 2026-04-28):** spot-checked the new entries by grepping current source for the specific files / functions / config keys each entry claims. All verified present. Pre-v5.0 entries audited in the v5.0.5 sweep are kept inline below for cross-reference but rolled-up; assume true unless flagged.

| ID | Closed in | What |
|----|-----------|------|
| BL222 — Settings/General claude-code field duplication | v5.28.8 | Removed `skip_permissions`, `channel_enabled`, `claude_auto_accept_disclaimer`, `permission_mode` from General (they stayed in LLM → claude-code exclusively). Moved `session.default_effort` to LLM → claude-code as well. |
| BL223 — RTK upgrade card raw JS visible as text | v5.28.8 | Replaced `onclick+JSON.stringify` inside HTML attribute strings (caused double-quote breakage) with `data-cmd` attribute + `addEventListener` after innerHTML assignment. |
| BL224 — `orchestrator-flow.md` Mermaid parse failure | v5.28.8 | `V[…issues[]]` had a literal `]` inside an unquoted bracket label; `Decide{Verdict<br/>outcome}` and others had unquoted `<br/>` HTML. Quoted all affected labels. |
| BL225 — `prd-phase3-phase4-flow.md` Mermaid parse failure | v5.28.8 | `G[story._conflictSet[file] = …]` and `L[render ⚠ …<br/>…]` had unquoted `[`/`]` and `<br/>`. Quoted both labels. |
| BL227 — terminal undersized after session completes | v5.28.8 | The 3-dot "generating…" indicator occupies vertical space; its removal on session completion freed height but xterm wasn't notified. Added `requestAnimationFrame(() => { fitAddon.fit(); send('resize_term', …) })` to `refreshGeneratingIndicator()`. |
| BL214 UX fix — language picker promoted + whisper.language tracks PWA locale | v5.28.3 | Operator-asked UX fix on top of v5.28.0/.1 i18n foundation: (1) language picker moved to top of Settings → About (the datawatch identity card), Settings → General → Language kept synced for discoverability; (2) PWA UI language now the default app language — `setLocaleOverride()` syncs `whisper.language` via PUT /api/config when picking a concrete locale (Auto leaves whisper alone); (3) `whisper.language` form field removed from the PWA Whisper card and replaced with a read-only "tracks PWA language" indicator. New `readonly` config-form field type. Configuration parity preserved — `whisper.language` still settable via YAML / REST / MCP / CLI / chat for power-users who need a different transcription language than UI language. Mobile parity at datawatch-app#40 (language picker placement + whisper sync) + #41 (BL208 #30 PRD card style audit gap caught during the same UI-change → mobile-parity audit). |
| BL173-followup — cluster→parent push verified end-to-end in testing cluster | v5.28.2 | **BL173-followup CLOSED.** Verified end-to-end in the operator's testing cluster (`kubectl context: testing`, 3-node Ubuntu 22.04 cluster on 10.8.2.0/24). Deployed `ghcr.io/dmz006/datawatch-parent-full:latest` v5.28.1 as a Deployment in `bl173-verify` ns with seeded config (token + `observer.peers.allow_register=true`) via initContainer + ClusterIP Service. Ran a separate `curlimages/curl` peer Pod that hit `parent.bl173-verify.svc.cluster.local:8080`: `[1] register peer prod-pod-test → token Aqw-…`, `[2] push snapshot → status:ok`, `[3] /api/observer/envelopes/all-peers → by_peer includes prod-pod-test envelope`, `[4] DELETE → status:ok`. Real cluster pod-network topology: peer pod → ClusterIP Service → parent pod cross-node. The dev-workstation pod-overlay gap that originally blocked this is resolved by deploying parent in-cluster (which is the production topology anyway). Runbook in `docs/howto/federated-observer.md` carries forward as the operator-side prod-cluster check; the BL173-followup item itself is done. |
| BL214 wave-2 + BL173-followup runbook | v5.28.1 | **BL214 wave-2** — i18n string-coverage extension. Wired through `t()`: confirm-modal Yes/No buttons (`showConfirmModal`), session dialog titles (delete/stop-session via `dialog_*` Android keys), batch-delete count `%1$d` placeholder, alerts-tab loading + empty state, Autonomous-tab `templates` filter label + New-PRD FAB title. 4 new universal keys (`action_yes`/`action_no`/`common_loading`/`common_no_alerts`) added to all 5 locale bundles + filed at datawatch-app#39 per the v5.28.0 Localization Rule. `TestLocales_CommonNavKeysPresent` parity guard extended. **BL173-followup** — cluster→parent push handler verified end-to-end (peer `bl173-verify` round-tripped: register → push → aggregator includes peer → cleanup). New "Production-cluster reachability check (BL173-followup)" runbook in `docs/howto/federated-observer.md` with the exact pod-side curl + cleanup commands so the operator's production-cluster verification is one-shot when convenient. Failure-mode triage documented (connection error = network gap; 401/403 = auth/token plumbing). |
| BL214 — PWA i18n foundation (DE/ES/FR/JA) | v5.28.0 | **BL214** (datawatch#32) — PWA i18n foundation with translations sourced 1:1 from the datawatch-app Compose Multiplatform Android client (`composeApp/src/androidMain/res/values{,-de,-es,-fr,-ja}/strings.xml`). 5 locale bundles (~240 keys each) embedded in the binary at `internal/server/web/locales/`. Zero-dep `window._i18n` + `t(key, vars)` helper with Android-style `%1$s`/`%1$d` placeholders. `applyI18nDOM` sweeps `data-i18n="<key>"` (with `data-i18n-attr`/`data-i18n-html` variants). Auto-detection: `navigator.language` strip-to-base → fallback to `en`. Settings → General → Language picker (Auto / EN / DE / ES / FR / JA) persisted in localStorage; reload applies. Initial coverage: bottom nav (Sessions/Autonomous/Alerts/Settings) + Settings tabs (Monitor/General/Comms/LLM/About). 3 new tests in `internal/server/v5280_locales_test.go` (presence + ≥90% EN-parity + required-key guards). Iterative string-coverage expansion across the remaining ~9700 lines of `app.js` continues in v5.28.x patches. |
| BL216 — MCP channel bridge introspection (full parity) + BL109 stale-`.mcp.json` fix | v5.27.10 | **BL216** — operator question "ring-laptop has stale `~/.mcp.json` pointing at node + channel.js but daemon log says Go bridge; which is actually used?" answered through every parity surface. New `GET /api/channel/info` returns `{kind, path, ready, hint, node_path, node_modules, stale_mcp_json}`. New `channel_info` MCP tool. New `datawatch channel info` + `datawatch channel cleanup-stale-mcp-json` CLI subcommands (with `--dry-run`/`--json`). New chat `channel info` command. New PWA Monitor → MCP channel bridge panel with kind badge (Go ✓ / JS ⚠) + stale-mcp-json warnings. Per-session register-time daemon log line `[channel] session <id> registered with <kind> bridge at <path>`. **BL109 fix** — `WriteProjectMCPConfig` now writes `Command: <go-bridge>, Args: []` when `BridgePath()` is set, instead of hardcoding `node + channel.js` (which produced stale files on Go-bridge hosts since v5.4.0). 11 new tests across `internal/channel/v52710_bridge_test.go`, `internal/server/v52710_channel_info_test.go`, `internal/router/v52710_channel_info_test.go`. datawatch-app#38 tracks mobile mirror. |
| BL213 + BL212 follow-up — Signal device-linking API + JS channel memory parity | v5.27.9 | **BL213** (datawatch#31) — three Signal device-linking endpoints completed for the mobile companion. **(1)** `GET /api/link/qr` aliased to the existing SSE QR-pair stream (mobile expects the `qr` path name). **(2)** `GET /api/link/status` upgraded from placeholder to real impl: shells out to `signal-cli -a <account> listDevices` and returns the parsed device list `[{id, name, created, last_seen}, ...]` via new `parseListDevicesOutput` helper. **(3)** `DELETE /api/link/{deviceId}` invokes `signal-cli removeDevice -d <id>` with guardrails: rejects non-DELETE (405), missing/non-numeric id (400), device id 1 (primary, 400), and missing `signal.account_number` config (503). 7 new tests in `v5279_link_test.go`. **BL212 follow-up** (datawatch#29) — v5.27.7 added memory tools to the Go bridge but left the JS fallback at `reply`-only. Operator caught this: ring-laptop / storage testing instances still hit the JS path via `~/.mcp.json` pointing at `node ~/.datawatch/channel/channel.js`. v5.27.9 mirrors `memory_remember` / `memory_recall` / `memory_list` / `memory_forget` / `memory_stats` into `internal/channel/embed/channel.js` with a new `callParent` helper that returns the parent's response body (legacy `postToDatawatch` stays for fire-and-forget paths). HTTP errors surface as MCP errors (no silent empty results). 3 new Go snapshot tests in `internal/channel/v5279_channeljs_test.go`. |
| BL208 #30 + BL210 — PRD card style alignment + daemon-MCP gap closures | v5.27.8 | **BL208 #30** (datawatch#30) — PRD card harmonised with Sessions card style. New `.prd-card` CSS class shares the bg2 / system-radius / 12px-14px padding shape; status drives the 4px left-border colour via `.prd-card-status-{draft,decomposing,needs_review,…}` modifiers. Redundant "PRDs" sub-header on the Autonomous tab dropped (operator: tab label is enough). `renderPRDRow` switched off inline border/padding to the class. `.prd-row` alias kept for the v5.26.6 `scrollToPRD` selector. **BL210** — 11 new MCP tools close the daemon-MCP coverage gaps the operator audit flagged: `memory_wal` / `memory_test_embedder` / `memory_wakeup` (operator-priority memory gaps), `claude_models` / `claude_efforts` / `claude_permission_modes` (v5.27.5 LLM listing endpoints), `rtk_version` / `rtk_check` / `rtk_update` / `rtk_discover` (RTK quartet), `daemon_logs`. All forward to existing `/api/*` paths via `proxyJSON`. Bodies in `internal/mcp/v5278_gap_closures.go`; registration inlined into `mcp.New()` alongside the other memory tool block. Remaining BL210 gaps (filters CRUD, backends listing, federation aggregated, devices register, files browser, three sessions sub-endpoints) deferred to v5.28.x — none are operator-priority. |
| BL208 #26 + #27 + BL209 + BL212 — PWA UI parity + config-driven quick commands + channel.js memory tools | v5.27.7 | **BL208 #26** — Running badge pulse (CSS @keyframes 0.55→1.0 over 700ms) + 3-dot generating indicator below the terminal output (each dot fades over 600ms with 200ms stagger). Pure-CSS, prefers-reduced-motion respected. JS hook on state transitions injects/removes the indicator div. **BL208 #27** — scroll-mode button glyph swapped `↕` → `📜` to match Android's TerminalToolbar. **BL209** (datawatch#28) — new `GET /api/quick_commands` endpoint serving an operator-editable list (config: `session.quick_commands`); falls back to a 15-entry baseline (`yes`/`no`/`continue`/`skip`/`/exit` + `Esc`/`Tab`/`Enter`/arrows/`PgUp`/`PgDn`/`Ctrl-b`). Mobile + PWA migration off hardcoded lists tracked at datawatch-app#31. **BL212** (datawatch#29) — `cmd/datawatch-channel/main.go` (Go bridge spawned per claude-code session) gains `memory_remember`/`memory_recall`/`memory_list`/`memory_forget`/`memory_stats` MCP tools. Each forwards to the parent's `/api/memory/*` endpoints via a new `callParent` helper. New `urlQueryEscape` keeps the bridge stdlib-only (no `net/url` dep). 11 new Go tests. New smoke section §7x. **BL208 #30** (PRD card style alignment) deferred to v5.27.8 — bigger restyle of `renderPRDRow` + Sessions card harmonisation. |
| BL211 + BL215 — scrollback state-detection + rate-limit miss hotfix | v5.27.6 | **(BL211)** New `CapturePaneLiveTail()` method on TmuxAPI that always reads the live pane bottom regardless of tmux copy-mode. State detection at `manager.go:1489` switched off `CapturePaneVisible` (which captures scrolled view in copy-mode for PWA display) onto the live tail. Operator scenario fixed: scrolling up no longer pins state detection on stale content while claude finishes its turn. **(BL215)** Per-line rate-limit length gate raised 200 → 1024 chars at `manager.go:3791` because modern claude rate-limit dialogs are paragraph-length with context (operator hit one on 2026-04-30 that datawatch missed; line was ~600 chars containing "5-hour limit reached"). The +60min fallback for no-reset-time messages was already correct (line 3837-3840) — BL215 only had to fix the upstream miss. 8 new Go tests covering both fixes; full sweep 1508/1508. PWA `CapturePaneVisible` keeps the operator-friendly scroll behaviour for the display channel — surgical fix, no UX regression. |
| BL207 — claude permission_mode + model + effort surfaces (plan-mode for PRDs) | v5.27.5 | Three new claude-code per-session options surfaced through every parity surface. **(1)** New REST endpoints `GET /api/llm/claude/{models,efforts,permission_modes}` return hardcoded lists (Anthropic /v1/models query frozen as BL206 per operator decision). **(2)** New `session.permission_mode` config field (`plan` / `acceptEdits` / `auto` / `bypassPermissions` / `dontAsk` / `default`) — when set, claude-code launches with `--permission-mode <value>` and `--dangerously-skip-permissions` is suppressed (explicit operator mode wins). **(3)** Per-session overrides via `POST /api/sessions/start` body (`permission_mode`, `model`, `claude_effort`). **(4)** `PRD.PermissionMode` + `Task.PermissionMode` so PRDs can run a single design-only step (`plan`) inside an otherwise execute-the-plan PRD; executor resolves task → PRD → global. **(5)** PWA New Session modal gains a claude-only options block (Permission mode / Model / Effort dropdowns) populated from the new endpoints; visible only when backend=claude-code. **(6)** Settings → LLM → claude-code panel + Settings → General → Sessions both gain a `permission_mode` text field. New AGENT.md rule: every major release refreshes the hardcoded alias list against Anthropic's current set. 10 new Go tests. New smoke section §7w. datawatch-app sync issues to follow for the mobile companion. |
| BL205 — GET /api/update/check + modern rate-limit patterns | v5.27.4 | **(1) datawatch#25** — new read-only `GET /api/update/check` endpoint so mobile + PWA clients can implement "check → confirm → install" UX without firing the install on the first call. POST /api/update keeps its existing atomic check+install behaviour. PWA `checkForUpdate()` migrated off direct api.github.com calls onto the daemon endpoint (one source of truth, no CORS, goes through daemon auth). 6 new Go tests cover the happy path + method enforcement + side-effect guard. **(2) Operator-reported rate-limit regression** — `rateLimitPatterns` extended with the modern claude-code phrasings (`limit reached`, `weekly usage limit`, `hit weekly limit`, `5-hour limit`, `opus/sonnet limit reached`) that the legacy "You've hit your limit" pattern no longer catches. Both the auto-schedule resume + the alert filter pick up the new phrasings. 4 new Go tests in `internal/session/v5274_ratelimit_modern_test.go`. New smoke section §7v. |
| Hotfix — chat-channel reload wire-up + claudeDisclaimerResponse refactor | v5.27.3 | v5.27.2 wired `SetReloadFn` on the production comm router but missed the `testRouter` that backs `POST /api/test/message`. The smoke test surfaced "Reload not wired by this build" via the chat surface; fixed by wiring symmetrically. Plus `claudeDisclaimerResponse` extracted as a pure helper for unit-testability (4 new test cases). 9 new Go tests + 5 new smoke checks (§7u). Process note: the test+doc commit on top of v5.27.2 included a code change that should have been its own patch — recorded in `docs/plans/RELEASE-NOTES-v5.27.3.md` as a lesson for the next release window. |
| BL204 — subsystem hot-reload + claude auto-accept disclaimer | v5.27.2 | Two operator-asked items closed in one patch. **(1) Subsystem reload** — new `POST /api/reload?subsystem=<name>` endpoint + `Server.RegisterReloader` API + named reloaders for `config` / `filters` / `memory`. Replaces the all-or-nothing daemon restart for hot-reloadable subsystems. Full parity: CLI `datawatch reload [subsystem]`, MCP `reload` tool with `subsystem` arg, chat `reload [subsystem]` command, REST endpoint, PWA Settings → General → Auto-restart on config save (existing toggle now flagged in docs as "subsystem-reload aware"). docs/operations.md updated with the "Why most config changes don't trigger a restart" explainer. **(2) Claude auto-accept disclaimer** — new `session.claude_auto_accept_disclaimer` config flag (default false). When on + backend is `claude-code`, the existing FilterEngine `DetectPrompt` hook auto-sends `1\n` for "trust this folder" / "Quick safety check" and `\n` for "Loading development channels" after a 750ms debounce. Full parity: YAML `session.claude_auto_accept_disclaimer`, REST `PUT /api/config`, MCP `config_set`, CLI `datawatch config set`, comm `configure`, PWA Settings → LLM → claude-code → "Auto-accept startup disclaimer". 2 new chat-parser tests (1471 total). |
| Bug fix — xterm refit + input rebind on prompt cycle | v5.27.1 | Operator-reported: submitting a follow-up prompt resized xterm wrong + dropped the tmux input element's Enter handler, forcing exit/re-enter to recover. Cause: `refreshNeedsInputBanner` (the state-driven banner toggle) was patching slot innerHTML without the immediate `requestAnimationFrame → fitAddon.fit() → resize_term` sync that v5.26.44 added to the explicit Dismiss path. Fixed by comparing before/after banner HTML and running the same fit sequence on any change; rebinds the Enter handler when missing via a `_dwEnterBound` flag on the input element. |
| Mempalace alignment minor — full configuration parity | v5.27.0 | Bundled the v5.26.70 + v5.26.72 mempalace alignment work behind full configuration parity per the project rule: every feature reachable from REST + MCP + CLI + comm channels + PWA. New PWA **Memory Maintenance** section under Settings → Monitor → Memory mirrors the new tools (`sweep_stale`, `spellcheck`, `extract_facts`, `schema_version`) and links to `docs/memory.md` + `RELEASE-NOTES-v5.27.0.md`. Earlier v6.0.0 draft backed out before publish so the v6.0 cut moment stays under operator control. 1469 unit tests (+5 router parsing); smoke 72/0/4. [datawatch-app#21](https://github.com/dmz006/datawatch-app/issues/21) filed for mobile mirror. |
| stdio MCP probe + L4-L5 wake-up REST + GHCR cleanup | v5.26.71 | Three closed in one bundle: (1) `scripts/release-smoke-stdio-mcp.sh` spawns `datawatch mcp` as a subprocess, sends JSON-RPC initialize + tools/list + tools/call(memory_recall), validates each response — required fixing a nil-reader segfault in `ServeStdio` and registering memory tools always-on so they surface in `tools/list`. (2) New `GET /api/memory/wakeup` REST endpoint composes the L0+L1+L4+L5 bundle on demand; `release-smoke-wakeup.sh` probes 3 shapes. (3) `.github/workflows/ghcr-cleanup.yaml` runs weekly + workflow_dispatch, deletes versions from closed minor lines while keeping latest patch + `latest`; uses `GITHUB_TOKEN` with `packages: write` — no PAT. |
| Mempalace QW bundle + ZAP active scan + PRD spacing | v5.26.70 | Five mempalace quick-win Go-native ports: auto-tag on save (`room_detector.go`), memory pinning (column + REST `POST /api/memory/pin` + L1 boost), conversation-window stitching (`conversation_window.go`), query sanitizer (`query_sanitizer.go` — 10 OWASP-LLM01 patterns redacted before embedder), repair self-check (`repair.go`). ZAP workflow gets two new active-scan passes (PWA full + API full with `-t`). PWA `renderStory` / `renderTask` filter empty segments + fold `✎ files` button inline when no files planned. |
| docs/testing.md ↔ smoke coverage audit | v5.26.66-69 | docs/testing.md ↔ smoke coverage audit (#41) closed via §7n KG add+query / §7o spatial-filter / §7p entity detection / §7q per-backend send / §7r stdio MCP / §7s wake-up L4/L5 prerequisite check sections in `release-smoke.sh`. Six new smoke sections cover the gaps `docs/testing.md` flagged. |
| PRD-flow Phase 4 — file association | v5.26.64–67 | `FilesPlanned` decomposer prompt extension + per-task `FilesTouched` post-session diff hook (`ProjectGit.DiffNames` capped at 50 paths) + `RecordTaskFilesTouched` + PWA file-edit modal + ⚠ conflict markers when two pending stories plan the same file. JSON-tag rename `files_planned` → `files`. [datawatch-app#19](https://github.com/dmz006/datawatch-app/issues/19) filed for mobile mirror. |
| PRD-flow Phase 3 — per-story execution profile + approval gate | v5.26.60–63 | Per-story state machine (`pending → awaiting_approval → pending → in_progress → completed`), per-story `ExecutionProfile` override (most-specific wins), `Approve` / `Reject` per story with `RejectedReason` rendered inline. Unified Profile dropdown in the New Session modal. [datawatch-app#18](https://github.com/dmz006/datawatch-app/issues/18) + [#20](https://github.com/dmz006/datawatch-app/issues/20) filed. |
| Settings card-section docs chips | v5.26.0 | Operator-reported: every Settings card needs a docs chip; complex settings should link to howto. `settingsSectionHeader(key, title, docsPath)` already supported the docs arg but no caller passed one. v5.26.0 threads `sec.docs` through all three field arrays (COMMS / LLM / GENERAL). Complex sections (autonomous / orchestrator / voice / pipelines / memory / sessions / RTK) point at the relevant howto; simpler ones (web server / MCP server / plugins / datawatch / auto-update) point at architecture doc. Pure-PWA change; existing `Show inline doc links` toggle still hides all chips when off. README marquee → v5.26.0. |
| Settings card-section docs chips | v5.26.0 | Operator-reported: every Settings card needs a docs chip; complex settings should link to howto. `settingsSectionHeader(key, title, docsPath)` already supported the docs arg but no caller passed one. v5.26.0 threads `sec.docs` through all three field arrays (COMMS / LLM / GENERAL). Complex sections (autonomous / orchestrator / voice / pipelines / memory / sessions / RTK) point at the relevant howto; simpler ones (web server / MCP server / plugins / datawatch / auto-update) point at architecture doc. Pure-PWA change; existing `Show inline doc links` toggle still hides all chips when off. README marquee → v5.26.0. |
| Diagrams page restructure + retention refinement | v5.25.0 | Operator-reported: diagrams.html sidebar dropped Plans group (operator-internal; already gitignored from embedded viewer since v5.3.0) + added top-level How-tos group with all 13 walkthroughs + extended Subsystems with mcp.md/cursor-mcp.md + extended API with observer.md/memory.md/sessions.md/devices.md/voice.md. Asset retention rule refined: keep-set = every major + latest minor + latest patch on latest minor (was just majors). `scripts/delete-past-minor-assets.sh` rewritten with the new logic. AGENT.md § Release-discipline rules updated. |
| Autonomous tab WS auto-refresh + dropdown narrowing | v5.24.0 | Operator v5.22.0 carry-over: PWA Autonomous tab required manual Refresh after every CLI/chat/REST mutation. New `MsgPRDUpdate` WS message + `Manager.SetOnPRDUpdate(PRDUpdateFn)` indirection + `Manager.EmitPRDUpdate(id)` trampoline. Every `*API` mutating method (Create/Decompose/Run/Cancel/Approve/Reject/RequestRevision/EditTaskSpec/InstantiateTemplate/SetTaskLLM/SetPRDLLM/DeletePRD/EditPRDFields) emits after save; trailing emit fires inside the Run goroutine when the executor walk finishes (terminal states reach the PWA). main.go binds the callback to `HTTPServer.BroadcastPRDUpdate`. PWA debounces 250 ms so a Run that flips many tasks per second reloads the panel once at the end. 4 new unit tests. Plus operator-reported tmux-bar fit: saved-commands dropdown `max-width: 200px → 130px` so the [📄] [Commands ▾] [arrows] row fits on one line on a 480px PWA card. |
| Asset retention + 4 operator-reported PWA bugs | v5.23.0 | (1) Settings → Comms bind interface fields were rendering empty (objects-as-strings bug) and were single-select; now multi-select with connected-interface auto-protect (prevents self-disconnect). (2) Session-detail channel/acp mode-badge dropped — the output-tab system conveys it. tmux mode-badge stays (no tab in tmux-only mode). (3) Response button icon-only — 📄 alone, no text, fits v5.22.0 right-justified arrow row. (4) Two new AGENT.md release-discipline rules: embedded docs must be current at binary build time (always go through make cross / make build, never `go build` directly); asset retention — only major releases keep binaries indefinitely, past minor/patch get assets pruned on next release. New `scripts/delete-past-minor-assets.sh` helper. Ran the script against 105 past-minor releases — deleted 477 binary attachments. Operator's config-save question answered in release notes (already efficient: PUT /api/config + applyConfigPatch updates in-memory + YAML, no restart unless key is in RESTART_FIELDS set). |
| Observability fill-in + arrow-buttons layout (audit #3) | v5.22.0 | LoopStatus surfaces BL191 Q4 + Q6 counters: `ChildPRDsTotal`, `MaxDepthSeen`, `BlockedPRDs`, `VerdictCounts` (pass/warn/block rollup across every Story.Verdicts + Task.Verdicts). Operators on `/api/autonomous/status` polling loops get the new fields automatically; PWA / MCP / chat all forward the same JSON. 4 new unit tests. Operator-reported: arrow buttons (Up/Down/Left/Right) now right-justified next to the saved-commands dropdown via `margin-left:auto` in the flex container — v5.19.0 had restored them but placed them BEFORE the dropdown which let flex-wrap put them on the next line. README marquee → v5.22.0. |
| Observer + whisper config-parity sweep (audit #2) | v5.21.0 | Same pattern as v5.17.0 (autonomous) but for observer + the missing whisper HTTP-shape keys. `internal/config.ObserverConfig` gained `ConnCorrelator` (BL293) + `Peers` (BL172) — pre-v5.21.0 these lived only on `observer.Config` so YAML/REST couldn't reach them. New `ObserverPeersConfig` struct. main.go bridge updated. `applyConfigPatch` gained 20 new cases (observer scalars × 5, pointer-bools × 6, federation × 5, peers × 4, ollama_tap × 1, whisper HTTP × 3). 6 new unit tests covering the round-trip matrix. README marquee → v5.21.0. |
| Documentation alignment sweep (audit #1) | v5.20.0 | Pure-docs release closing the audit's documentation drift findings. `docs/mcp.md` bumped from "41 tools" to "100+ tools" with family breakdown + live-list pointer. `docs/cursor-mcp.md` tools table extended beyond the v3-era 5 entries. `docs/api/autonomous.md` documents every endpoint added since v5.2.0 (approve/reject/request_revision/edit_task/instantiate/set_llm/set_task_llm/children/PATCH/DELETE?hard=true). `docs/api/observer.md` documents the cross-host endpoint + every observer MCP tool + CLI subcommand. `openapi.yaml` updated with the four newly-shipped paths. README marquee → v5.20.0. No code changes. |
| Operator-blocking CRUD + UX cleanup + audit gap | v5.19.0 | (1) Autonomous full CRUD finally lands: `Store.DeletePRD` (recursion-aware — descendants spawned via SpawnPRD removed too) + `Store.UpdatePRDFields` (title + spec edit on non-running PRDs) + Manager wrappers + `DELETE /api/autonomous/prds/{id}?hard=true` + `PATCH /api/autonomous/prds/{id}` + CLI `datawatch autonomous prd-delete` / `prd-edit` + PWA Edit + Delete buttons on every PRD card with confirm dialog. (2) PWA whisper test-button rendered as empty `<input type="button">` because `loadGeneralConfig` fell through to the generic input path; mirrored the comms render path's button branch. (3) PWA `loadSavedCmdsQuick` was overwriting `#savedCmdsQuick.innerHTML`, blowing away the Response button + tmux arrow group (regression of v5.2.0 BL191 work); restored. (4) Session-detail had a duplicate "Input Required" label inline above the tmux input box (operator: top-of-page badge already conveys it); removed. (5) RTK config section was duplicated in Settings → General + LLM (operator: should only be in LLM); removed from General. (6) README.md marquee bumped from v5.7.0 → v5.19.0 (was 12 releases stale across this session and the previous one). 8 new unit tests in `internal/autonomous/crud_test.go` (delete-from-map, recursive-descendant-cleanup, not-found-error, refuses-running, update-title, update-spec, update-refuses-running, edit-appends-decision). |
| MCP channel one-way bug | v5.18.0 | Operator report: MCP channel "not working in Claude" — investigated and traced to the HTTP→HTTPS redirect blocking the bridge's `notifyReady()` POST. The bridge follows the 307 redirect, hits the daemon's self-signed TLS cert, fails verify, and the daemon never learns the bridge's listening port. Result: `claude mcp list` shows ✓ Connected (stdio handshake works) but the daemon's push path has nowhere to send → reply tool works one-way only. Fix: redirect handler bypasses for loopback requests to `/api/channel/*`. New `isLoopbackRemote` helper + 1 unit test. Verified end-to-end: daemon log now shows `[channel] ready for session <id>` after every bridge spawn. |
| Operator-surface bridge for BL191 Q4 + Q6 config knobs | v5.17.0 | Polish-pass finding: v5.9.0 (`max_recursion_depth`, `auto_approve_children`) + v5.10.0 (`per_task_guardrails`, `per_story_guardrails`) shipped the runtime feature but the operator-facing surface was incomplete — YAML load dropped them, `PUT /api/config` silently no-op'd, PWA Settings → Autonomous didn't expose them, main.go translation didn't copy them. v5.17.0 closes the bridge: `internal/config/AutonomousConfig` gained the four fields; `cmd/datawatch/main.go` copies them with fallback to package defaults; `applyConfigPatch` handles them with both JSON-array and CSV-string accepting paths (new `splitCSV` helper); PWA gained four field entries. 2 new unit tests (1357 total). |
| PWA viz for shipped data-model work | v5.16.0 | Three contained PWA additions (`internal/server/web/app.js` only) that surface data the daemon was already producing through `/api/autonomous/prds/...` and `/api/observer/envelopes/all-peers` but the PWA wasn't rendering. (1) BL191 Q4 — PRD cards show `↗ parent <id>` + `depth N` badges; new **Children (lazy)** disclosure renders the genealogy tree; per-task `↳ spawn` + `→ child <id>` affordances. (2) BL191 Q6 — color-coded inline verdict badges on every story + task with hover tooltips for severity/summary/issues. (3) BL180 cross-host — "↔ Cross-host view" button on Federated peers, opens a modal that walks `/api/observer/envelopes/all-peers` and tags cross-peer caller rows with `🔗 cross`. New `cross-host-modal` recipe added to `scripts/howto-shoot.mjs` + screenshot inlined into federated-observer howto. |
| BL190 density expansion | v5.15.0 | Recipe map 11 → 19; 22 PNGs total. New recipes cover mobile viewports (sessions/autonomous/settings-monitor/session-detail), Settings deep-scrolls (general → autonomous block, general → auto-update, comms → Signal, LLM → Ollama, LLM → Episodic Memory), the autonomous-prd-expanded toggle (with seeded fixrich PRD carrying 1 story + 3 tasks + 3 decisions + 1 verdict), the diagrams-flow content view, and the header-search filter chips. Inline coverage extended across 8 howtos with multi-shot sequences. Seed-fixtures script enriched with the `fixrich` PRD so the expanded screenshot shows real story+task+decisions content rather than "no stories yet". |
| BL190 expand-and-fill | v5.14.0 | Recipe map grew from 6 to 11 (`settings-monitor`, `settings-about`, `alerts-tab`, `autonomous-new-prd-modal`, `session-detail` added). 11 screenshots committed under `docs/howto/screenshots/`. Inline coverage extended from 4 to 13 howtos — every walkthrough now has at least one PNG: setup-and-install + container-workers + cross-agent-memory + federated-observer + daemon-operations get `settings-monitor`; daemon-operations + setup-and-install get `settings-about`; daemon-operations gets `alerts-tab`; autonomous-review-approve gets `autonomous-landing` + `autonomous-new-prd-modal`; chat-and-llm-quickstart + pipeline-chaining get `session-detail`; comm-channels gets `settings-comms`; prd-dag-orchestrator gets `autonomous-landing`. Per-howto density (1-3 PNGs each) is below the original 15-20-per-howto target; the pipeline is in place for further iterative expansion. |
| BL180 Phase 2 eBPF kprobes (resume) | v5.13.0 | Per the BL292 commit roadmap: new `tcp_connect` (outbound) + `inet_csk_accept` (inbound) kprobes feeding a `conn_attribution` BPF_MAP_TYPE_LRU_HASH (key = sock pointer, value = {pid, ts_ns}). LRU eviction bounds memory; new userspace `realLinuxKprobeProbe.ReadConnAttribution()` iterates the map and `PruneConnAttribution(olderThanNs)` walks + deletes stale entries for freshness. Loader attempts attach on the new probes; failure is non-fatal (existing partial-mode fallback keeps byte counters live). bpf2go regenerated cleanly under clang 20.1.8 with both committed `vmlinux_amd64` + `vmlinux_arm64` headers; new `.o` artifacts updated in tree. 3 new unit tests cover the nil-safe iterator + post-Close idempotence + ConnAttribution row shape; real attach is gated by CAP_BPF and validated via the operator's Thor smoke-test (BL180 design Q6). |
| BL180 Phase 2 cross-host federation correlation | v5.12.0 | New `Envelope.ListenAddrs []ListenAddr` + `Envelope.OutboundEdges []OutboundEdge` fields. The local correlator now records LISTEN-state addrs on backend envelopes + ESTABLISHED outbound conns that miss a local listener (cross-host candidates). New `observer.CorrelateAcrossPeers(byPeer map[string][]Envelope, localPeerName string)` joins outbound edges from one peer to listen addrs on another, producing CallerAttribution rows with `<peer>:<envelope-id>` prefix on the matched server envelope. Reachable as `GET /api/observer/envelopes/all-peers` + `observer_envelopes_all_peers` MCP tool + `datawatch observer envelopes-all-peers` CLI. 7 new unit tests cover happy path, wildcard listener, same-peer-not-matched, single-peer no-op, sort-order, unmatched-edge, local-peer-prefix-suppression. Operator's Q5c "don't close until cross-host works" satisfied. |
| BL190 howto screenshot capture pipeline (first cut) | v5.11.0 | Operator removed the chrome MCP plugin (memory issue); new capture path goes through puppeteer-core in `/tmp/puppet` driving `/usr/bin/google-chrome` headless. New `scripts/howto-shoot.mjs` (recipe-driven; 6 recipes ship: sessions-landing, autonomous-landing, settings-llm, settings-comms, settings-voice, diagrams-landing) + `scripts/howto-seed-fixtures.sh` (idempotent; wipes `fixture: true` JSONL rows + re-seeds PRDs across every status pill, one orchestrator graph + guardrail node, one pipeline with before/after gates). 6 screenshots committed under `docs/howto/screenshots/` (excluded from embedded daemon binary via `_embed_skip.txt`); inlined into chat-and-llm-quickstart, autonomous-planning, voice-input, mcp-tools. Recipe map intentionally minimal — extend to ~15-20 shots × 13 howtos in iterative cuts. |
| BL191 Q6 — guardrails-at-all-levels | v5.10.0 | Per-story + per-task guardrails. New `Story.Verdicts` + `Task.Verdicts` slices, `Config.PerTaskGuardrails` + `Config.PerStoryGuardrails` (defaults empty = opt-in). New `GuardrailFn` indirection on `Manager` wired in `cmd/datawatch/main.go` to a `/api/ask` loopback (same path the BL25 verifier uses; `verification_backend` / `verification_effort` apply). After every task verifies green, per-task guardrails fire; a `block` verdict marks the task blocked and halts the PRD with status=blocked. Per-story guardrails fire after all tasks in a story complete. Permissive parse — unparseable guardrail output becomes a `warn` so best-effort runs still progress. 6 new unit tests (no-config no-op, all-pass, task-block-halts-PRD with second-task-untouched assertion, story-fire-after-all-tasks-done, story-block-halts, no-fn-wired silent no-op). |
| BL191 Q4 — recursive child-PRDs | v5.9.0 | Option (a) shortcut from the design doc: `Task.SpawnPRD` flag turns a parent task spec into a child PRD spec; `recurseChildPRD` in `internal/autonomous/executor.go` walks Decompose → (auto-)Approve → Run inline; child outcome rolls up onto the parent task. New `PRD.ParentPRDID/ParentTaskID/Depth` fields + `Store.CreatePRDWithParent` + `Store.ListChildPRDs`. New `Config.MaxRecursionDepth` (default 5; 0 disables) + `Config.AutoApproveChildren` (default true — false leaves the parent task `blocked` waiting for operator approval). Full parity: REST `GET /api/autonomous/prds/{id}/children` + MCP `autonomous_prd_children` + CLI `datawatch autonomous prd-children` + chat verb `autonomous children <id>` + YAML `autonomous.{max_recursion_depth,auto_approve_children}`. 5 new unit tests cover the recursion matrix. |
| BL201 voice/whisper backend inheritance | v5.8.0 | Daemon-side: new `inheritWhisperEndpoint` helper in `cmd/datawatch/main.go` fills `whisper.endpoint`/`whisper.api_key` from `cfg.OpenWebUI.URL/APIKey` when backend=openwebui, or from `cfg.Ollama.Host + /v1` when backend=ollama. Explicit values always win. New `ollama` case in `internal/transcribe/factory.go` routes through the OpenAI-compat client. PWA Settings already hides endpoint/api_key fields so the resolution is the single source of truth. 8 new unit tests cover the inheritance matrix (openwebui inherit, explicit override, ollama with/without trailing slash, whisper no-op, openai pass-through, case-insensitive backend trim). |
| BL200 howto coverage expansion | v5.7.0 | 13 walkthroughs total — original 6 refreshed + 7 new (`setup-and-install`, `chat-and-llm-quickstart`, `autonomous-review-approve`, `voice-input`, `comm-channels`, `federated-observer`, `mcp-tools`). MCP-tool surface table verified against `internal/mcp/*.go` `NewTool(...)` registry; federated-observer.md uses real `peer register` + `peer delete` + `--token-file` flags. PWA screenshot rebuild stays under BL190 follow-up. |
| `datawatch reload` CLI parity | v5.7.0 | Added the missing CLI subcommand for hot config reload — BL17 already had SIGHUP + `POST /api/reload` + the MCP `reload` tool. Closes a configuration-parity gap; lets every howto recommend `datawatch reload` after `datawatch config set` instead of the SIGHUP/curl dance. |
| Two-place version sync (api.go ↔ main.go) | v5.7.0 | `internal/server/api.go` was stuck at `Version = "5.0.3"` while `cmd/datawatch/main.go` marched through 5.0.x → 5.6.1 (LDFLAGS injection masked the runtime impact, but the AGENT.md "must be updated together" rule was being violated). Both files re-synced to 5.7.0; pre-commit version-check note in AGENT.md § Versioning called out as a recurring failure mode. |
| BL180 Phase 2 (procfs cut) | v5.1.0 | Per-caller envelope attribution: new `Callers []CallerAttribution` field + `internal/observer/conn_correlator.go` (procfs-based join of `/proc/<pid>/net/tcp` connections with the listen-port → envelope map). 9 unit tests cover the parser, scope filter, end-to-end join, and Phase 1 caller preservation. Existing `Caller`/`CallerKind` derived as the loudest entry for back-compat. eBPF kprobe layer + cross-host correlation remain open per operator answers (see Active work). |
| Session toolbar toggle | v5.1.0 | Removed the `toggleTermToolbar` affordance + state + `term-toolbar-hidden` CSS rules; the term-toolbar now always renders (operator confirmed the layout reads cleanly at every viewport). Filed [datawatch-app#8](https://github.com/dmz006/datawatch-app/issues/8) so the mobile shell drops the matching toggle. |
| BL178 reopen | v5.1.0 | Operator on v5.0.5: the session-list response icon was opening to text from "weeks ago". Daemon-side `GetLastResponse` only returned the stored `Session.LastResponse`, which is captured on running→waiting_input transitions. For long-lived running sessions, the stored value can stay stale until the next transition. Fix: when the session is `running` or `waiting_input`, `GetLastResponse` re-captures from the live tmux pane on every read and persists if changed; terminated sessions keep their last-word stored value. |
| Session-list history button | v5.1.0 | Renamed the "Show / Hide history (N)" toggle to just "History (N)" per operator — keeps the count, drops the verb churn. |
| Session-list FAB position | v5.1.0 | Two operator-reported bugs: (a) on Chrome desktop the FAB sat outside the centered 480px PWA card because it was anchored to the viewport's right edge; fix scoped a `right: calc(50vw - 240px + 16px)` override into the `@media (min-width:600px)` block so the FAB tracks the card. (b) On phone the FAB sat on top of the bottom nav because `bottom` was `64px` while `--nav-h` is `60px` (4px gap → visual overlap on Chrome mobile); fix uses `calc(var(--nav-h) + 16px + safe-area)` for a proper 16px clearance. |
| BL191 Q1 + Q2 + Q3 | v5.2.0 | Autonomous PRD lifecycle first cut: Q1 review/approve gate (status machine: draft → decomposing → needs_review → approved → running → complete + revisions_asked / rejected / cancelled) wired across REST / CLI / chat / MCP; Q3 per-PRD `Decisions []Decision` audit timeline appended on every transition; Q2 templates via `IsTemplate` flag + `InstantiateTemplate` with `{{var}}` substitution. 9 new lifecycle tests cover each transition + the gate on Run. PWA full CRUD (Q5) + recursion (Q4) + guardrails-at-all-levels (Q6) deferred to v5.3.0. |
| Settings → About — datawatch-app link | v5.2.0 | Added a GitHub link to the mobile-app repo + a placeholder note for the Play Store link once the app publishes. |
| Settings → About — orphaned-tmux clear | v5.2.0 | Moved the orphaned-tmux count + "Kill all orphaned" affordance from Settings → Monitor → System Statistics to Settings → About since it's an operator/maintenance affordance, not a live metric. Auto-refresh after kill. |
| Settings → General — Voice Input backend dropdown | v5.2.0 | Backend select extended to expose `whisper / openai / openai_compat / openwebui / ollama` (operator wants existing-LLM-backend reuse). Endpoint+key-from-LLM-config inheritance is queued as task #282. |
| PWA generic select + button field renderers | v5.2.0 | `internal/server/web/app.js` — added `select` + `button` field types so future config blocks can wire dropdowns + action buttons declaratively. |
| Session detail — tmux arrow buttons | v5.2.0 | Operator note 2026-04-26: four buttons (↑↓←→) added next to the saved-commands quick row, sending the corresponding tmux escape sequence (`\x1b[A` etc.) via the existing `send_input` WS event. Mobile alignment in [datawatch-app#9](https://github.com/dmz006/datawatch-app/issues/9). |
| datawatch-app catch-up issue (#9) | v5.2.0 | Operator directive 2026-04-26: every PWA-visible change in v5.1.0 + v5.2.0 batched into [datawatch-app#9](https://github.com/dmz006/datawatch-app/issues/9) so the mobile shell can stay aligned: toolbar removal, history rename, FAB position (desktop + phone), BL178 reopen, BL198 drawer fix, About-tab additions, voice-input dropdown, BL191 PRD lifecycle surfaces, arrow keys. |
| Embedded docs — drop plans/ + add back button | v5.3.0 | Operator note 2026-04-26: `docs/plans/` should not ship inside the daemon binary (operator-internal). Added `plans` to `docs/_embed_skip.txt` so `make sync-docs` skips it. Also added a "← PWA" back button to the diagrams.html header so operators can return without browser back. |
| BL203 flexible LLM selection (backend + parity surfaces) | v5.4.0 | Operator directive: per-task and per-PRD worker LLM overrides with most-specific-wins fallthrough to stage default and global. Backend (`SetPRDLLM` / `SetTaskLLM` on Manager + executor uses resolved values) + REST (`set_llm`, `set_task_llm`) + CLI (`prd-set-llm`, `prd-set-task-llm`) + chat verbs + MCP tools all shipped. PWA dropdowns follow in the next cut. |
| BL288 stale node+channel.js MCP cleanup | v5.4.0 | Operator on v5.3.0 saw `/usr/bin/node ~/.datawatch/channel/channel.js` spawn for new sessions even though `[channel] using native Go bridge` was logged. Root cause: leftover `datawatch` (unsuffixed) entry in `claude mcp list` from before the Go-bridge migration (project-scope `.mcp.json`). Added `channel.CleanupStaleJSRegistrations()` that scans all scopes on daemon start and removes any `datawatch*` entry pointing at `node + channel.js`. |
| BL289 internal-ID leak scrub + voice test button + voice howto | v5.4.0 | Operator note: v5.3.0 voice-input label leaked `[task #282]` into operator UI. Removed; the only such leak in the PWA today. Plus a working **Test transcription endpoint** button wired to `POST /api/voice/test` (1 KB silent WAV through the configured backend), which force-disables `whisper.enabled` on failure so a broken backend doesn't keep firing. New `docs/howto/voice-input.md` covers all five backends (whisper local, openai, openai_compat, openwebui, ollama) with the inheritance rules. |
| BL291 since-v4 memory-leak audit | v5.5.0 | Operator on v5.4.0 hitting OOM. Daemon RSS itself was small (60 MB) but every behavior added since v4.0 was audited for unbounded growth / leaked descriptors / per-tick churn. Four fixes: (1) `session.GetLastResponse` 2-second TTL cache + bounded eviction (BL178 reopen v5.1.0 was re-capturing entire encrypted logs on every read); (2) `autonomous.PRD.Decisions` capped at 200 most-recent via `trimDecisions()` in `Store.SavePRD` (BL191 v5.2.0 appended without bound); (3) `observer.CorrelateCallers` short-circuits when no `Kind=Backend` envelope is present (BL180 Phase 2 v5.1.0 was opening /proc per tracked PID per tick regardless of scope); (4) PWA `state.lastResponse` map bounded to 128 entries with FIFO drop. |
| BL202 PWA full CRUD second cut | v5.5.0 | Replaced the v5.3.0 `prompt()` chains with proper modals carrying backend / effort / model dropdowns (live from `/api/backends`). PRD-create + per-task edit + new per-PRD "LLM" action all wired to v5.4.0 `set_llm` / `set_task_llm`. Per-PRD + per-task LLM badges render the current override inline. |
| BL292 leak audit pass 2 | v5.6.0 | Two real leaks found beyond BL291: (1) `session.Manager.promptOscillation` slice grew on every running↔waiting flip with no cap AND the map entry was never deleted on session removal — capped at 100 per session + cleanup on removal; (2) `autonomous.Store.AddLearning` was append-only with the rewrite-everything persist pattern — capped at 1000 most-recent (older learnings already mirrored into episodic memory + KG). Also dropped `promptFirstSeen` / `promptLastNotify` / `lastResponseCache` entries on session removal (same lifecycle gap). BL180 Phase 2 eBPF kprobe work backed out cleanly mid-edit (never compiled successfully); will resume in a separate cycle with `BPF_MAP_TYPE_LRU_HASH` + userspace TTL pruner. |
| BL187 | v5.0.4 (real fix) | First closed v4.8.12 as "no code change needed" — HTML was clean. Operator reopened on v5.0.3: still seeing the old "New" tab, no FAB. Root cause was `internal/server/web/sw.js`: app-shell was cache-first with a static `CACHE_NAME='datawatch-v2'`, so installed PWAs kept serving the pre-BL187 cached `index.html` / `app.js` even after every daemon upgrade. Fix: app-shell switched to network-first w/ cache fallback (offline still works) + `CACHE_NAME` bumped to `datawatch-v5` so existing installs invalidate cleanly on next activate. |
| BL194 | v4.8.11 | "MCP tools" link added to `/diagrams.html` header alongside the existing "API spec" link. |
| BL178 | v4.8.10 | `showResponseViewer` always fetches the live response; cached value shown first as "(updating…)" then patched in place. |
| BL190 | v4.9.3 | How-to suite complete: 6 docs (autonomous-planning, cross-agent-memory, prd-dag-orchestrator, container-workers, pipeline-chaining, daemon-operations) with per-channel reachability matrix on every walkthrough. PWA screenshots deferred to operator. |
| BL197 partial | v4.9.2 | Chat-channel autonomous PRD parity: `autonomous {status, list, get, decompose, run, cancel, learnings, create}` + `prd` alias. PWA PRD-lifecycle UI deferred to BL191 (naturally part of the same design conversation). |
| BL180 Phase 1 | v4.9.1 | Observer ollama runtime tap: per-loaded-model envelopes from `/api/ps` with Caller + CallerKind + GPUMemBytes. New `Envelope.Caller`/`CallerKind` fields for the Phase 2 eBPF correlation. |
| BL189 | v4.9.0 | Whisper backend factory: `whisper.backend = whisper | openai | openai_compat`. Local Python venv default (unchanged); operators can route to OpenWebUI / faster-whisper-server / cloud OpenAI / etc. via the new HTTP backend. Tests cover multipart shape, HTTP errors, anon auth, factory routing. |
| BL185 | v4.8.23 | Rate-limit parser extended to accept `"resets <time>"` (no "at") — the newer claude format. The auto-detect + auto-select-1 + schedule-resume pipeline was already wired since BL30; only the parser needed the new marker. |
| BL177 | v4.8.22 | eBPF arm64 artifacts: per-arch vmlinux.h tree + dual `//go:generate`; both arch `.{go,o}` committed; cross-build verified. |
| BL195 | v4.8.22 | Public container distribution: `.github/workflows/containers.yaml` matrix-pushes 8 images to GHCR on every tag; stats-cluster also `docker save`'d as a release asset. `make containers-push` / `containers-tarball` for local mirror. |
| BL177 longer-term | v5.0.2 | CI drift-check `.github/workflows/ebpf-gen-drift.yaml` — fails when committed eBPF artifacts drift from `netprobe.bpf.c`. |
| BL173 task 1 | v5.0.1 | eBPF kprobe attach loader wired (`loader_linux.go`): `loadNetprobeObjects` + four kprobes; partial attach non-fatal; BTF preloaded so no CAP_SYS_PTRACE. Unblocks BL180 Phase 2 structurally. |
| BL184 secondary | v5.0.1 | opencode-acp `Thinking... (reason)` renders as a visible italic line instead of an empty `<details>` wrapper. |
| BL184 | v4.8.20 | opencode-acp recognition lag: `markChannelReadyIfDetected` runs unconditionally on every output + chat_message WS event. (Thinking-message UX deferred.) |
| BL181 | v4.8.21 | eBPF BTF discovery via `/sys/kernel/btf/vmlinux` (no more CAP_SYS_PTRACE / /proc/self/mem requirement). Test verifies the path. |
| BL192 | v4.8.19 | Doc-coverage audit: docs/api/{voice,devices,sessions}.md added; architecture-overview rows point at the new operator references. |
| BL175 | v4.8.18 | docs duplication strategy: `docs/_embed_skip.txt` + `scripts/check-docs-sync.sh` + `hooks/pre-commit-docs-sync` + `.github/workflows/docs-sync.yaml` CI guard. Hybrid of (a) keep-rsync + (c) skip-manifest. |
| BL199 | v4.8.18 | `/diagrams.html` header — dropped "back to web UI" link; API spec + MCP tools now open in the current browser tab. |
| BL198 | v5.0.5 (real fix) | First closed v4.8.18 with `transform: translateX(-100%)` + `visibility:hidden` + `pointer-events:none` on the mobile aside-collapsed state. Operator reopened on v5.0.4: still saw a 1px strip on desktop collapse, and the docs/diagram pane went blank when collapsed on mobile. Two distinct bugs: (a) **desktop** — the 1px `border-right` on the aside leaked at x=-1 because `box-sizing:border-box` + grid col 0 didn't suppress it; fix added `border-right:none; width:0; visibility:hidden; overflow:hidden` on `.body.aside-collapsed aside`. (b) **mobile** — the desktop rule `.body.aside-collapsed { grid-template-columns: 0px 1fr }` won by specificity even inside the mobile media query. With aside `position:fixed` and out of grid flow, auto-placement put `main` into the 0px first cell so it rendered at ~28 px (just its padding) — the "blank screen" the operator reported. Fix added `.body.aside-collapsed { grid-template-columns: 1fr }` inside the mobile media query so the layout stays single-column when collapsed. Both verified via puppeteer at desktop-open / desktop-collapsed / mobile-default / mobile-open. |
| BL196 | v4.8.17 | Binary size: HTTP gzip middleware + `make cross` rebuilt with `-trimpath -s -w` and opt-in UPX pack. |
| BL193 | v4.8.15 | Full doc-comparison audit (llm-backends, api-mcp-mapping, messaging-backends, architecture-overview, data-flow) — internal IDs swept, tables cross-checked against code. |
| BL176 | v4.8.9 | RTK update string sweep: PWA chip, OpenAPI description, chat help all show the install.sh one-liner. |
| BL188 | v4.8.9 | Attribution guide refreshed — nightwire credit expanded, Aperant noted under "Researched and skipped", operator-action note for BL117/BL33/F10/BL173 follow-ups. |
| BL182 | v4.8.8 | "Input Required" yellow popup now patches in place from the WebSocket state-change event — no more back-out/re-enter. |
| BL183 | v4.8.8 | Orphan-cleanup affordance always visible in Settings → Monitor → System Statistics (was hidden when count was zero). |
| BL186 | v4.8.8 | CLI long-help + setup epilogue swept of internal IDs (Shape A/B/C → operator-language). |
| Release-discipline rules | v4.8.8 | Two new rules: README marquee must reflect current release; backlog refactor each release. |
| Settings → Show inline doc links | v4.8.7 | Per-browser localStorage toggle in Settings → General; inline `docs` chip next to high-value section headers (Proxy Resilience, LLM Configuration, Communication Configuration, System Statistics) deep-links into `/diagrams.html#docs/...`. Honors the toggle. |
| Proxy-flow recursive variant | v4.8.7 | New mermaid flow + loop-prevention notes added to `docs/flow/proxy-flow.md`. |
| BL179 | v4.8.6 | Search-icon to header bar (left of daemon-status light); in-card duplicate removed. |
| `/diagrams.html` UX | v4.8.5 | Collapsible sidebar, mobile responsive, marked.js renders prose for files without mermaid blocks. |
| Diagram + flow refactor | v4.8.3 / v4.8.4 | Renamed flow files (orchestrator-flow / observer-flow / agent-spawn-flow); cleaned BL/F/Sprint/Shape from titles + body + diagrams; added Mermaid flowcharts so they render in `/diagrams.html`. |
| PWA internal-ID sweep | v4.8.1 / v4.8.2 | eBPF noop msg, federated peers card empty-state, profile placeholder, Cluster nodes subscript. |
| S14a foundation | v4.8.0 | `observer.federation.parent_url` + push-with-chain loop prevention + `Envelope.Source` attribution. **Remaining (v4.8.x):** root-side envelope rewriter, PWA Cluster filter pill, `observer_primary_list` MCP alias. |
| S13 follow — orchestrator integration | v4.7.2 | Per-node `observer_summary` join across local + peers in `GET /api/orchestrator/graphs/{id}`. |
| B44 | v4.7.1 | PWA sessions search-icon toggle (mobile parity). |
| BL173-followup verification | 2026-04-25 | Shape C image build + push + dry-run + harbor push validated. |
| BL174 verification | 2026-04-25 | Image-size deltas captured. agent-opencode -50 MB; agent-claude +6 MB; stats-cluster 11 MB. |
| Plugins list shows datawatch-observer | Verified live v4.8.2 | `/api/plugins` returns `native[]` correctly; bug was a v4.7.x snapshot. |

## Frozen Features

| # | Description | Status | Notes |
|---|-------------|--------|-------|
| F7  | libsignal — replace signal-cli with native Go | 🧊 frozen 2026-04-20 | Signal-cli is working and stable; 3–6 mo rewrite deferred until there's a concrete operational need. Plan kept at [2026-03-29-libsignal.md](2026-03-29-libsignal.md). |


## Frozen / external

| ID | Item | Notes |
|----|------|-------|
| BL206 (frozen 2026-04-29) | **Anthropic `/v1/models` query for live claude model list.** Operator decision 2026-04-29: don't query the API. v5.27.5 ships hardcoded alias list (`sonnet`/`opus`/`haiku` + recent full names). Revisit when Anthropic ships a new alias that operators want surfaced before the next datawatch release picks it up — that's the only forcing function for the API integration. | Defer; aliases stay in code-controlled list. |
| BL174 stretch | Distroless / alpine spike for agent-base — would shrink ~250 MB further. Defer until image-size telemetry shows headroom worth chasing. | Defer. |
| S14b | Per-pod alert rules + observer-driven autoscaling. Depends on S14a so federated envelopes can be alert subjects. | Target v4.9.0. |
| S14c | ROCm + Intel level_zero scrapers in Shape C. Needs hardware to validate. | Target v5.0.0. |
| Mobile parity | datawatch-app Compose Multiplatform follow-ups tracked in [GH#4 (this repo — umbrella)](https://github.com/dmz006/datawatch/issues/4) + datawatch-app issues: [#2](https://github.com/dmz006/datawatch-app/issues/2) federated peers · [#3](https://github.com/dmz006/datawatch-app/issues/3) cluster nodes · [#4](https://github.com/dmz006/datawatch-app/issues/4) eBPF status · [#5](https://github.com/dmz006/datawatch-app/issues/5) native plugins · [#6](https://github.com/dmz006/datawatch-app/issues/6) Agents filter pill · [#7](https://github.com/dmz006/datawatch-app/issues/7) per-node observer_summary badge. | External repo; GH#4 is the cross-repo tracking umbrella. |
| Future sprint S14+ | Cross-cluster federation tree, per-pod alert routing, observer-driven autoscaling, ROCm / Intel level_zero. | Not yet specced. |

---

## Backlog — Sprint Plan

**v4.0.0 shipped 2026-04-20. Every planned S1–S8 backlog item is now landed.** The backlog remaining is operational tail (BL85 RTK auto-update, BL166 helm-tool re-add), long-running / frozen items, and v4.0.x follow-ups (web UI cards, BL103-per-guardrail wiring, etc.).

### Shipped timeline (v3.0.0 → v4.0.0)

| Release | Sprint | Items | Release notes |
|---------|--------|-------|---------------|
| v3.0.0 | F10 landing | 25 items (F10 platform, validator, peer broker, orchestrator bridge, mobile surface, memory federation) | [notes](RELEASE-NOTES-v3.0.0.md) |
| v3.1.0 | Test infra + B30 | 3 items (BL89/90/91, B30 fix) | [notes](RELEASE-NOTES-v3.1.0.md) |
| v3.2.0 | Intelligence core | 2 items (BL28 gates, BL39 cycle detection) | [notes](RELEASE-NOTES-v3.2.0.md) |
| v3.3.0 | Observability | 3 items (BL10/11/12) | [notes](RELEASE-NOTES-v3.3.0.md) |
| v3.4.0 / v3.4.1 | Operations + Windows fix | 4 items (BL17/22/37/87) + windows cross-build | [notes](RELEASE-NOTES-v3.4.0.md) |
| v3.5.0 | S1 — quick wins + UI | 5 items (BL1/34/35/41 + F14) | [notes](RELEASE-NOTES-v3.5.0.md) |
| v3.6.0 | S2 — sessions productivity | 6 items (BL5/26/27/29/30/40) | [notes](RELEASE-NOTES-v3.6.0.md) |
| v3.7.0 / v3.7.1 | S3 — cost + audit | 3 items (BL6/86/9) + cost-rates hotfix | [notes](RELEASE-NOTES-v3.7.0.md) |
| v3.7.2 | Sx — parity backfill | 20 MCP tools + 9 CLI commands (v3.5–v3.7 endpoints) | CHANGELOG |
| v3.7.3 | Sx2 — comm + mobile parity | comm router + mobile surface doc | CHANGELOG |
| v3.8.0 | S4 — messaging + UI | 4 items (BL15/31/42/69) | CHANGELOG |
| v3.9.0 | S5 — backends + chat UI | 4 items (BL20/78/79/72) | CHANGELOG |
| v3.10.0 | S6 — autonomous | 2 items (BL24/BL25) | [design](2026-04-20-bl24-autonomous-decomposition.md) · [usage](../api/autonomous.md) |
| v3.11.0 | S7 — plugin framework | 1 item (BL33) | [design](2026-04-20-bl33-plugin-framework.md) · [usage](../api/plugins.md) |
| **v4.0.0** | **S8 — PRD-DAG orchestrator** | **1 item (BL117) + cumulative release notes** | [design](2026-04-20-bl117-prd-dag-orchestrator.md) · [usage](../api/orchestrator.md) · [v3.0→v4.0 cumulative](RELEASE-NOTES-v4.0.0.md) |

Frozen / dropped: F13/BL19 (dropped), BL38 (dropped), BL45 (frozen), BL7 + BL8 (multi-user — frozen). F7 (libsignal) stays open as long-running.

### v4.0.1 — shipped 2026-04-20 (follow-up patch)

Every item flagged as a v4.0.x follow-up in v4.0.0 landed in v4.0.1, plus BL85, BL166, the directory-picker ergonomic, Aperant review, and the F7 freeze:

| Item | Status |
|---|---|
| Web UI Settings cards for autonomous / plugins / orchestrator | ✅ shipped — 14 new fields under General tab (7 autonomous, 3 plugins, 4 orchestrator) |
| BL117 real GuardrailFn (per-guardrail system prompt via `/api/ask`) | ✅ shipped — replaces the v1 stub; unparseable/unreachable → `warn`, doesn't halt the graph |
| Autonomous executor → `session.Manager.Start` wiring | ✅ shipped — `SpawnFn` loopback to `/api/sessions/start`, `VerifyFn` via `/api/ask`, fires async from `POST .../run` |
| Plugin hot-reload via fsnotify | ✅ shipped — `Registry.Watch(ctx)`, 500 ms debounce, wired at startup when `plugins.enabled` |
| `internal/server/web/openapi.yaml` resync | ✅ shipped — regenerated from `docs/api/openapi.yaml` |
| **BL85** — RTK auto-update REST surface | ✅ shipped — `GET /api/rtk/version`, `POST /api/rtk/check`, `POST /api/rtk/update`; background checker was already wired |
| **BL166** — tools-ops helm re-add | ✅ shipped — get.helm.sh reachable; installed from tarball with TARGETARCH |
| Directory-picker "create folder" | ✅ shipped — `POST /api/files` with `{path, name}`; root-path clamp enforced |
| Aperant integration review | ✅ skipped — AGPL-3.0 + Electron desktop app; sits on same claude-code layer; no headless API. Skip per 2026-04-20 research. |
| F7 libsignal | 🧊 frozen — deferred until a concrete need surfaces |

---

### Sprint S1 — Quick wins + UI diff → v3.5.0 — **shipped**

Five low-to-medium-risk items shipped in v3.5.0.

| ID | Item | Status |
|----|------|--------|
| BL1  | IPv6 listener support               | ✅ shipped — IPv6-safe `joinHostPort` at every bind site; `[::]:port` enables dual-stack |
| BL34 | Read-only ask mode                  | ✅ shipped — `POST /api/ask` (Ollama + OpenWebUI backends, no session, no tmux) |
| BL35 | Project summary command             | ✅ shipped — `GET /api/project/summary?dir=` (git status + commits + per-project session stats) |
| BL41 | Effort levels per task              | ✅ shipped — `Session.Effort` (quick/normal/thorough); REST + config + reload + UI parity |
| F14  | Live cell DOM diffing               | ✅ shipped — `tryUpdateSessionsInPlace()` per-card diff before falling back to full render |

### Sprint S2 — Sessions productivity → v3.6.0 — **shipped**

Six items shipped in v3.6.0.

| ID | Item | Status |
|----|------|--------|
| BL5  | Session templates                   | ✅ shipped — `/api/templates` CRUD + `template:` start field |
| BL26 | Recurring schedules                 | ✅ shipped — `recur_every_seconds` + `recur_until` on ScheduledCommand |
| BL27 | Project management                  | ✅ shipped — `/api/projects` CRUD + `project:` start field |
| BL29 | Git checkpoints + rollback          | ✅ shipped — `datawatch-pre/post-{id}` tags + `POST /api/sessions/{id}/rollback` |
| BL30 | Rate-limit cooldown                 | ✅ shipped — `/api/cooldown` (G/P/D) + `session.rate_limit_global_pause` opt-in |
| BL40 | Stale task recovery                 | ✅ shipped — `/api/sessions/stale` + `session.stale_timeout_seconds` |

### Sprint S3 — Cost + observability tail → v3.7.0 — **shipped**

Three items shipped in v3.7.0.

| ID | Item | Status |
|----|------|--------|
| BL6  | Cost tracking                       | ✅ shipped — `Session.tokens_in/out/est_cost_usd` + `/api/cost` + `/api/cost/usage` + per-backend rate table |
| BL86 | Remote GPU/system stats agent       | ✅ shipped — `cmd/datawatch-agent/` (linux-amd64/arm64) — `GET /stats` returns GPU+CPU+memory+disk JSON |
| BL9  | Audit log                           | ✅ shipped — append-only JSONL at `<data_dir>/audit.log` + `GET /api/audit` with filters |

### Sprint Sx — Parity backfill → v3.7.2 — **shipped**

**Audit finding 2026-04-20.** Endpoints shipped in v3.5.0–v3.7.0
had REST + YAML surfaces but were missing MCP / CLI parity, plus
end-to-end functional testing through a running daemon. v3.7.2
addresses the gap:

- **20 MCP tools** in `internal/mcp/sx_parity.go` (REST loopback proxies)
- **9 CLI subcommands** in `cmd/datawatch/cli_sx_parity.go`
- **Functional smoke** verified against a live daemon on port 18080;
  every endpoint returns valid JSON, POST/DELETE round-trips persist,
  cost-rate override applied to live `Manager` correctly.

**Sx2 → v3.7.3 (shipped 2026-04-20):**
- Comm router commands `cost`, `stale`, `audit`,
  `cooldown` (status/set/clear), and a generic `rest <METHOD> <PATH>
  [json]` passthrough that reaches every other Sx endpoint from chat.
- Mobile API surface documented at `docs/api/mobile-surface.md` —
  inventory of every v3.5–v3.7 endpoint plus use-case mapping for
  the `datawatch-app` paired client.

Full parity (REST + YAML + MCP + CLI + comm + mobile + web) for
v3.5–v3.7 is now achieved. S4 can start clean.

| Endpoint | Sprint shipped | Gaps |
|---|---|---|
| `/api/ask` (BL34) | S1 | MCP, comm, CLI |
| `/api/project/summary` (BL35) | S1 | MCP, comm, CLI |
| `/api/templates` (BL5) | S2 | MCP, comm, CLI, UI |
| `/api/projects` (BL27) | S2 | MCP, comm, CLI, UI |
| Recurring schedule fields (BL26) | S2 | MCP/comm/CLI for setting `recur_every_seconds` |
| `/api/sessions/{id}/rollback` (BL29) | S2 | MCP, comm, CLI |
| `/api/cooldown` (BL30) | S2 | MCP, comm, CLI |
| `/api/sessions/stale` (BL40) | S2 | MCP, comm, CLI |
| `/api/cost`, `/api/cost/usage`, `/api/cost/rates` (BL6) | S3 / v3.7.1 | MCP, comm, CLI |
| `/api/audit` (BL9) | S3 | MCP, comm, CLI |
| `datawatch-agent` (BL86) | S3 | parent integration (config, polling adapter, dashboard surface) |

Plus: **functional testing** for each — start a daemon, exercise the
endpoint via every channel, confirm round-trip works, then teardown.

This sprint MUST complete before S4 starts so we don't compound the
gap. Estimate ~2-3 days.

### Sprint S4 — Messaging + UI polish → v3.8.0 — **shipped**

| ID | Item | Status |
|----|------|--------|
| BL15 | Rich previews in alerts             | ✅ shipped — `messaging.FormatAlert` (Telegram MD escaping, Signal mono, Slack/Discord passthrough) + opt-in `session.alerts_rich_format` |
| BL31 | Device targeting (`@device` routing) | ✅ shipped — `session.device_aliases` config + `/api/device-aliases` CRUD |
| BL69 | Splash screen — custom logo         | ✅ shipped — `session.splash_logo_path/tagline` + `GET /api/splash/{logo,info}` |
| BL42 | Quick-response assistant            | ✅ shipped — `POST /api/assist` with dedicated assistant_* config |

Full parity for each: REST + YAML + MCP tool + CLI subcommand + comm + mobile (REST is mobile-friendly).

### Sprint S5 — Backends + chat UI → v3.9.0 — **shipped**

| ID | Item | Status |
|----|------|--------|
| BL20 | Backend auto-selection (routing rules) | ✅ shipped — `session.routing_rules` + `/api/routing-rules` + `/api/routing-rules/test` + MCP/CLI parity |
| BL78 | Chat UI: Gemini chat mode           | ✅ documented (config recipe at `docs/api/chat-mode-backends.md`) — `gemini.output_mode: chat` |
| BL79 | Chat UI: Aider/Goose chat mode      | ✅ documented — same `output_mode: chat` recipe for Aider + Goose |
| BL72 | OpenCode memory hooks               | ✅ documented — opencode chat-mode reuses BL65 memory hook path |

### Sprint S6 — Intelligence → v3.10.0 ✅ SHIPPED 2026-04-20

Design doc: [`2026-04-20-bl24-autonomous-decomposition.md`](2026-04-20-bl24-autonomous-decomposition.md) — maps every nightwire component to a datawatch primitive. Operator doc: [`../api/autonomous.md`](../api/autonomous.md).

| ID | Item | Status |
|----|------|--------|
| BL24 | Autonomous task decomposition       | ✅ shipped — `internal/autonomous/` package (models, JSONL store, decompose prompt+parser, security scanner, manager, executor with topo-sort + auto-fix retry), REST `/api/autonomous/*` + 10 MCP tools + `datawatch autonomous` CLI + comm via `rest` passthrough + `autonomous.*` YAML |
| BL25 | Independent verification            | ✅ shipped — `VerifyFn` indirection in executor; BL103 validator agent wiring deferred to v3.10.x patch |

### Sprint S7 — Extensibility → v3.11.0 ✅ SHIPPED 2026-04-20

Design doc: [`2026-04-20-bl33-plugin-framework.md`](2026-04-20-bl33-plugin-framework.md) — rejects `.so` / Lua; selects subprocess + JSON-RPC over stdio. Operator doc: [`../api/plugins.md`](../api/plugins.md).

| ID | Item | Status |
|----|------|--------|
| BL33 | Plugin framework                    | ✅ shipped — `internal/plugins/` subprocess driver, manifest discovery, 4 hooks, fan-out chaining, timeout/error stats; REST `/api/plugins/*` + 6 MCP tools + `datawatch plugins` CLI + comm via `rest` + `plugins.*` YAML. Disabled by default. |

### Sprint S8 — PRD-DAG orchestrator → **v4.0.0** ✅ SHIPPED 2026-04-20

Design doc: [`2026-04-20-bl117-prd-dag-orchestrator.md`](2026-04-20-bl117-prd-dag-orchestrator.md). Operator doc: [`../api/orchestrator.md`](../api/orchestrator.md). **Cumulative release notes** covering every shipped item since v3.0.0: [`RELEASE-NOTES-v4.0.0.md`](RELEASE-NOTES-v4.0.0.md).

| ID | Item | Status |
|----|------|--------|
| BL117 | PRD-driven DAG orchestrator + guardrail sub-agents | ✅ shipped — `internal/orchestrator/` package (Graph/Node/Verdict, JSONL store, Runner with Kahn topo-sort and verdict aggregation). 4 guardrail types (rules/security/release-readiness/docs-diagrams-architecture) with v1 stub GuardrailFn; plugin `on_guardrail` hook available for real guardrails. REST `/api/orchestrator/*` + 9 MCP tools + `datawatch orchestrator` CLI + comm via `rest` + `orchestrator.*` YAML. |

---

### Sprint summary

| Sprint | Items | Releases | Effort | Status |
|--------|-------|----------|--------|--------|
| S1 | 5 (4 quick wins + F14 DOM diff) | v3.5.0  | 1 day    | ✅ shipped |
| S2 | 6 sessions/productivity         | v3.6.0  | 1 week   | ✅ shipped |
| S3 | 3 cost + obs tail (+ new binary)| v3.7.0  | 1 week   | ✅ shipped (REST/YAML only — Sx gates full parity) |
| Sx | Parity backfill for v3.5–v3.7   | v3.7.2  | 2-3 days | ✅ shipped — MCP (20 tools) + CLI (9 commands) + functional smoke verified |
| Sx2| Comm + mobile parity            | v3.7.3  | 0.5 day  | ✅ shipped — router commands + mobile API surface doc |
| S4 | 4 messaging + UI polish         | v3.8.0  | 3 days   | ✅ shipped |
| S5 | 4 backends + chat UI            | v3.9.0  | 3 days   | ✅ shipped |
| S6 | 2 intelligence (BL24 + BL25)    | v3.10.0 | 2 weeks  | ✅ shipped — [design](2026-04-20-bl24-autonomous-decomposition.md) · [usage](../api/autonomous.md) |
| S7 | 1 plugin framework (BL33)       | v3.11.0 | 3 days   | ✅ shipped — [design](2026-04-20-bl33-plugin-framework.md) · [usage](../api/plugins.md) |
| S8 | 1 PRD-DAG orchestrator (BL117)  | **v4.0.0** | 2-3 weeks | ✅ shipped — [design](2026-04-20-bl117-prd-dag-orchestrator.md) · [usage](../api/orchestrator.md) · [v3.0→v4.0 release notes](RELEASE-NOTES-v4.0.0.md) |

---

### Per-category snapshot (cross-reference)

Quick reference. The sprint plan above is the source of truth — these tables only group items by domain so plans are easy to find.

| Category | Active items | Sprint(s) |
|---|---|---|
| **Sessions** | BL117 future (all S2/S3 sessions items shipped) | S8 |
| **Intelligence** | _(complete — BL24, BL25 shipped in v3.10.0)_ | — |
| **Observability** | _(complete — all shipped)_ | — |
| **Collaboration** | _(BL9 shipped; BL7 + BL8 frozen)_ | — |
| **Messaging** | _(complete — BL15, BL31 shipped)_ | — |
| **Backends & UI** | _(complete — BL20 shipped, BL78/BL79 documented)_ | — |
| **Memory & Security** | _(complete — BL72 documented)_ | — |
| **Extensibility** | _(complete — BL33 shipped in v3.11.0)_ | — |

Per-item plans live in [`2026-04-11-backlog-plans.md`](2026-04-11-backlog-plans.md). Quick-effort items are flagged with ⚡ in the sprint tables above.

> **Already shipped:** Operations (v3.4.0: BL17/22/37/87), Observability core (v3.3.0: BL10/11/12), Intelligence core (v3.2.0: BL28/39), Testing infrastructure (v3.1.0: BL89/90/91), and 25 items in v3.0.0 (BL92–BL116). See per-version release notes for the full shipped list.

---

## Completed

### Bugs Fixed

| # | Description | Fixed |
|---|-------------|-------|
| B1 | xterm.js crashes and slow load (20s → 32ms) | v2.3.0 |
| B2 | Claude Code prompt detection false positives | v2.3.1 |
| B3 | LLM session reconnect on daemon restart | v2.2.9 |
| B4 | Input bar sometimes disappears in session detail | v2.3.8 |
| B5 | Session history controls off-screen on mobile | v2.3.8 |
| B6 | Function parity gaps across API/MCP/CLI/comm | v2.4.1 |
| B7 | Code test coverage 11.2% → 14.5% (318 tests, pure-logic ceiling reached) | v2.4.1 |
| B20 | RTK update available not showing in Monitor page stats card | v2.4.1 |
| B21 | Monitor Infrastructure card shows wrong protocol and bad formatting | v2.4.1 |
| B22 | Daemon crashes from unrecovered panics in background goroutines | v2.4.3 |
| B23 | Silent daemon death — remaining goroutine recovery, BPF map purge, crash log | v2.4.4 |
| B24 | Update check shows downgrade as "update available" (semver compare in UI/router/auto-updater) | v2.4.4 |
| B25 | Trust prompt invisible — MCP spinner hides what user needs to do (full prompt context in card + Input Required banner with key tip) | v2.4.5 |
| B31 | In-app upgrade reports success but doesn't replace binary — asset name mismatch between updater and release (pre-existing since v2.x; surfaced on v3.0.0 upgrade) | v3.0.1 |
| B30 | Scheduled command lands in prompt but requires a 2nd Enter to activate (claude-code TUI phase-4 race) | v3.1.0 |
| B32 | Tmux/scheduled command executes with a blank line, operator has to press Enter again to submit — trailing `\n` in the payload was interpreted by TUIs as multi-line input so the explicit Enter just added another blank | v4.0.2 |
| B33 | PWA "Input Required" yellow card stays visible after sending a reply; only disappears on session reconnect — added auto-dismiss on send + manual X button; re-appears on next distinct prompt | v4.0.2 |
| B34 | Most tmux commands still required a second Enter to submit even after the B32 trim — bracketed-paste TUIs fold a single-call `Enter` into the paste event; `SendKeys` now always uses the two-step push-then-Enter pattern with a 120 ms settle | v4.0.4 |
| B35 | Diagram viewer on the Settings → About tab showed "Failed to load docs/architecture.md: Failed to fetch" on first open — the service worker was serving a stale v1 cache that hadn't seen the new `/docs/` path. Bumped cache name to `datawatch-v2` and made `/docs/*` + `/diagrams.html` network-first. | v4.0.7 |
| B36 | PWA user-facing strings listed internal ticket IDs (e.g. "Autonomous PRD decomposition (BL24+BL25)", "Plugin framework (BL33)", "Default effort (BL41)"). Stripped the parenthetical ticket refs; added a project rule that forbids BL/F/B/S numbers in any operator-facing surface (web, mobile, comm, CLI user output). | v4.0.7 |
| B37 | Auto-install RTK manual-install suggestion pointed at the old release-asset URL; the operator-preferred upstream path is `curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh | sh`. Updated the CLI fallback message and `docs/rtk-integration.md`. | v4.0.7 |
| B38 | PWA + mobile Settings saves for the Autonomous / Plugins / Orchestrator sections silently no-op'd — `applyConfigPatch` had no case branches for `autonomous.*`, `plugins.*`, `orchestrator.*`, so unknown keys fell through the switch while the handler still returned 200. Added case-branches for all 17 keys plus a `default:` that logs unknown keys to stderr so future schema drift surfaces instead of silently dropping. Closes [issue #19](https://github.com/dmz006/datawatch/issues/19). | v4.0.8 |
| B45 | PWA session-list "☑ All (N)" select-all used unfiltered `state.sessions` — counter showed all done sessions (e.g. 1930) regardless of active filter; clicking selected them all. Fixed: `renderSessionsView` caches `state._visibleDone` (filtered done sessions); select bar and `selectAllInactive()` use that cache. Filter changes (`setBackendFilter`, `setSessionStateChip`, text input, clear) now call `state.selectedSessions.clear()`. | v8.19.2 |
| B46 | Claude workspace trust dialog auto-accept sent literal "1" + Enter, which confirmed the pre-selected "No, exit" option. Fixed: detect trust dialog from log stream (`pipe-pane`) and send `tmux send-keys Down` + 100 ms + `tmux send-keys Enter` to navigate to "Yes, I trust the folder". Startup deadline extended 20 s → 45 s. | v8.19.2 |
| B47 | Subprocess/virtual sessions (schedule-spawn, council, agent — `TmuxSession == ""`) force-failed on daemon restart: `ResumeMonitors` called `SessionExists("")` which always returns false, transitioning them to `StateFailed`. Added the `TmuxSession == ""` guard already present in `ReconcileLiveSessions` and `probeClaudeAlive`. | v8.19.2 |
| B48 | PWA image attachment sent `[image:<path>]` as literal text to Claude Code — vision subsystem only ran for inbound channel messages (Signal/Telegram), not for REST `POST /api/sessions/send` or WebSocket `send_input`. Added `Server.expandImageTags()`: regex-matches `[image:<path>]`, reads file, calls `visioner.Describe()`, substitutes `[image: <description>]` in-place. Both REST handler and `executeCommand` CmdSend call it. If vision disabled or file unreadable, tag passes through unchanged. | v8.19.3 |
| B49 | `file_service_root` defaulted to `$HOME` when the config field was empty, exposing the entire home directory via the file browser API. Live config updated to `~/.datawatch/files`. | v8.19.3 |
| B50 | `expandImageTags()` discarded the source file path after vision description — LLMs with file-access tools lost the ability to reference the original file. Replacement format changed from `[image: <desc>]` to `[image: <desc> | path: <path>]`. Fallback (vision disabled / unreadable) unchanged. | v8.19.4 |
| B51 | `expandImageTags()` read arbitrary files via `os.ReadFile(imgPath)` where `imgPath` came directly from user-supplied message text, with no boundary check (gosec G304). Added `checkPathTraversal(root, imgPath)` validation before the read — only paths within `fileServiceRoot()` are processed. Unit tests for `expandImageTags` now configure `FileServiceRoot` on the test server so temp files in `t.TempDir()` pass the traversal guard; previously all tests creating files outside home dir would silently pass for the wrong reason. Two failing tests in `v5272_claude_disclaimer_test.go` fixed (expected `"1\n"`, should be `"trust-dialog"` per v8.19.2 change). | v8.19.5 |
| B52 | `expandImageTags` tests used unchecked `f.Write()` + `f.Close()` return values, failing golangci-lint errcheck. Replaced with `os.WriteFile` and a `writeTempImage` helper. | v8.19.6 |
| B53 | Container Trivy scan blocked by 7 new HIGH CVEs in Debian bookworm base packages (no upstream fix): CVE-2026-16742 (libsystemd0/libudev1), CVE-2026-63382 + CVE-2026-63385 (libevent-core), CVE-2026-76642 + CVE-2026-78408/78409/78410 (util-linux family). Added to `.trivyignore` following existing suppression rationale — none of these packages are reachable via datawatch APIs at container runtime. | v8.19.7 |
| B54 | PWA session card "What's it doing?" button crashed with `Failed to execute 'json' on 'Response': Unexpected end of JSON input` when the session had no new output (or a delta thinner than 5 lines) since the last summary. Root cause: the handler returned `http.Error(..., http.StatusNoContent)` — RFC 7231 §3.3 forbids a body on 204 and Go dropped the message silently, so the PWA's `r.json()` threw. The two no-op branches now return 200 + a JSON contract `{no_change:true, current_status:"no change since last refresh"}`; the `apiFetch` helper resolves 204/205 to `null` as a defense-in-depth guard for any future 204 endpoint; the card shows a localized "no change" chip. Regression tests lock in the 200/JSON contract. | v8.19.8 |
| B55 | PRD backend picker showed all LLM backends including session-only kinds (opencode, claude-code, aider, gemini, goose) that cannot support headless planning via `/api/ask`. Selecting one caused decompose to silently return 202, stream an SSE error, and stop — leaving the PRD stuck with no operator-visible failure message. Fixed by adding `HEADLESS_PLANNING_KINDS = Set(['ollama','openwebui'])` and a `planningOnly` parameter on `renderBackendSelect`; all four PRD-level selectors pass `true`. Per-task selectors remain unfiltered. | v8.19.9 |
| B56 | PRD detail view showed no Approve or Reject buttons when a PRD reached `needs_review` state. Root cause: `_renderDetailHeader` was restructured in a prior release (GATE alpha.36) and only the "Request Revision" button was preserved for the `needs_review`/`revisions_asked` toolbar. Approve (green, calls `prdAction` → POST `/approve`) and Reject (red, calls `prdActionPrompt` → `reject`) are restored. Lifecycle strip hint also corrected: was "Planning… review will become available shortly" even after planning completed; now shows "Plan ready — review the stories below and Approve / Reject / Revise" when `status === 'needs_review'`. | v8.19.9 |
| B57 | Autonomous PRD task sessions ignored the configured LLM backend and silently ran with `claude-code`. Root cause: the executor sends `backend: "<named-llm>"` (e.g. `"ollama-datawatch"`) to `/api/sessions/start`. The session manager's `llm.Get()` only knows fixed kind strings (`"ollama"`, `"openwebui"`, `"claude-code"`, …) and returned `"unknown"` for named inference-registry entries, causing the manager to fall through to its compile-time default (`claude-code`). Fixed in `handleStartSession`: when `req.LLM` is unset but `req.Backend` matches a named inference-registry entry, the entry's `Kind` string replaces `req.Backend` before the manager sees it. | v8.19.10 |
| B59 | Autonomous task sessions accumulated and were never cleaned up after PRD completion. Root cause: `autonomousVerify` in main.go fired immediately after `autonomousSpawn` without waiting for the worker session to finish. The verifier saw no git diff (session hadn't run yet), returned `ok:true` by default, and the session kept running in the background indefinitely. After a multi-task PRD completed, all spawned sessions remained alive until manually killed. Fix: added a 3-second poll loop at the start of `autonomousVerify` that blocks until the session reaches `StateComplete`, `StateFailed`, or `StateKilled` (or context cancels), followed by a best-effort `mgr.Kill()` to tear down the tmux pane. | v8.20.2 |
| B62 | After creating an automaton (draft state), there was no obvious button to start the planning phase. The `▶ Plan` button existed only inside the compact lifecycle strip (rendered at `10px` font and `2px 6px` padding via `.lifecycle-compact`) and blended in with the grayed-out Review/Approve/Run/Done step labels. Operators had no clear affordance — canceling looked equally unclear. Fixed by adding explicit `planBtn` (▶ Start Planning / ▶ Re-plan) and `runBtn` (▶ Run) primary-action buttons directly to the header toolbar for `draft`/`revisions_asked` and `approved` states respectively, styled identically to the Approve/Cancel buttons (blue background, white text, bold weight). | v8.20.3 |
| B63 | Image attachment (📷 button in session input bar) did not open the file picker on some mobile browsers. Root cause: `<input type="file" style="display:none;">` — `display:none` prevents `.click()` from triggering the file dialog in certain mobile browser engines (known WebKit/Blink behavior on iOS/Android PWA). Changed to `position:fixed;top:-200px;left:-200px;width:1px;height:1px;opacity:0` which hides visually while remaining click-triggerable. | v8.20.3 |
| B61 | Launch Automaton wizard (the primary "Start Automata" flow) had two bugs: (1) the execution backend dropdown used `planningOnly=true`, showing only ollama/openwebui instead of all task-capable backends; (2) no planning backend field was present; (3) submitting with no directory selected sent `project_dir=undefined` to the server, returning a cryptic 400 "project_dir or project_profile required" instead of a client-side toast. The B60 fix only patched `openPRDCreateModal`, which is reachable via a different entry point — the main wizard path (`_wizardSubmit`) was untouched. Fix: removed `planningOnly=true` from `wizardBackend` select; added a `wizardDecompositionProfile` planning-only field below execution; added client-side guard that shows "Enter a project directory" toast before submitting; mirrored the `set_llm` call pattern from `openPRDCreateModal` so `decomposition_profile` is saved post-create. | v8.20.2 |
| B60 | New Automaton create modal showed only ollama/openwebui backends in the execution backend dropdown. Root cause: `renderBackendSelect('prdNewBackend', ..., true)` had `planningOnly=true` which filters to `HEADLESS_PLANNING_KINDS`. Users couldn't select opencode, goose, or claude-code as the task execution backend at PRD creation time. Fixed by removing `planningOnly=true` from the execution backend field and adding a separate "Planning backend (decompose)" picker with `planningOnly=true`. The `decomposition_profile` from the new field is included in the `set_llm` POST on PRD creation. | v8.20.2 |
| B72 | Opencode one-shot task sessions never signalled completion. The `send_input` task delivery path did not include the `DATAWATCH_COMPLETE:` convention, so the manager's pane-watcher never saw a completion marker. The autonomous executor's `autonomousVerify` poll loop waited on `StateComplete`/`StateFailed`/`StateKilled` indefinitely — the session sat in `waiting_input` forever. Fix: append `DATAWATCH_COMPLETE: <summary>` instruction to the task text before delivering via `send_input` for opencode one-shot sessions. | v8.20.9 |
| B73 | "No active session record" warning persisted even when the task session existed in the session list. Root cause: the active-session filter in `_loadPRDActiveSessionCard` only checked `s.prd_id === prd.id`. Sessions started before v8.20.8 have no `prd_id` field even though their session ID is stored in `task.session_id` in the PRD data. Fix: build a `Set` of known session IDs from `prd.story[].tasks[].session_id` and also check `taskSessionIds.has(fid)` in the filter. | v8.20.9 |
| B74 | Session Status tab showed no Sprint/Automata context for PRD task sessions ("no sprint data — hook payload sprint=… expected"). Root cause: the sprint card on the board is only populated by external hook payloads (`sprint=…`) from Claude Code's hook scripts. Opencode does not emit these. Fix: `handleSessionStatus` in `hook_events.go` now synthesizes sprint data from the linked PRD when `sess.PRDID != ""` and the board has no existing sprint. Fields set: `sprint_id`, `title`, `status`, `automata`, `task_id`. | v8.20.9 |
| B75 | One-shot PRD task delivery only worked for opencode; other TUI backends (goose, aider, …) assigned as the PRD task executor would start a session, idle at their prompt, and never receive the task. Root cause: the `SetStateChangeHandler` delivery block in `cmd/datawatch/main.go` was guarded by `BackendFamily == "opencode"`. Fix: changed to a deny-list condition: all backends except `claude-code` (channel bridge), `opencode-acp` (ACP API), and `subprocess` (shell task wrapping) receive the task via `send_input`. Renamed `opencodeTaskDelivered` sync.Map → `tuiTaskDelivered`. | v8.20.10 |
| B85 | Watchdog blind to "provider response headers" timeouts. When the ollama backend accepted the TCP connection but never sent HTTP response headers (cold-start overload on large models), opencode sessions spun on the activity indicator indefinitely. `autonomousSSEStallPatterns` did not include this error string, so the built-in watchdog goroutine never detected or killed these sessions. Fix: added `"provider response headers"` to the stall pattern list in `cmd/datawatch/main.go`. | v8.27.3 |
| B86 | GH#179 — `GET /api/llms`, `GET /api/llms/{name}`, and the `llm_list`/`llm_get` MCP tools returned a literal `api_key_ref` (a non-`${secret:name}` API key) in clear text. Fixed by adding `inference.LLM.Redacted()`/`RedactedList()` (mirrors the existing SEC-014 `multiserver.Entry.Redacted()` pattern one-for-one): clears a literal key, sets `api_key_ref_present`/`api_key_ref_prefix`; a `${secret:name}` reference is left as-is since it names a secret rather than containing one. Redacting the GET response created a second latent bug caught in the same pass: `PUT /api/llms/{name}` previously decoded onto a zero-valued struct, so a client that fetched the now-redacted GET and PUT it back on an unrelated field edit (the PWA edit form, and the `llm_add_model`/`llm_remove_model` MCP tools, which both GET-then-PUT-back) would have silently wiped the stored key. Fixed by changing the PUT handler to decode onto a copy of the existing entry (matching the pre-existing `fedPeerUpdate` merge pattern), and by making the PWA edit form omit `api_key_ref` from the outgoing body when the field is left blank and a key was already present. | v8.63.1 |
| B87 | Docs-viewer "?" links on iOS opened the right page but never scrolled to the in-page section (reported by datawatch-app, 2026-10-07). Root cause: iOS (Foundation URL / `SFSafariViewController`) percent-encodes the SECOND `#` in a link like `#docs/memory.md#configuration`, so `location.hash` arrives as `#docs/memory.md%23configuration`. `openFromHash()` (`internal/server/web/diagrams.js`) already decodes before splitting (why the page opens), but the scroll-to-anchor step in `rewriteRelativeMdLinks()` searched the raw, still-encoded hash for a second `#`, found none, and silently stayed at the top. Fixed by decoding first, matching `openFromHash`'s own order. New tests `internal/server/web/diagrams-anchor-scroll.test.js`. | v8.63.3 |
| B88 | GH#183 — APNs push dispatch only had one daemon-wide `push.apns.sandbox` toggle, not the per-device `apns_environment` the issue itself asked for ("persist it on the Device record... pick the host from the device's apns_environment"). A debug/TestFlight build registered while the global flag was set for production (or vice versa) would 400 BadDeviceToken. Found while reviewing the existing APNs implementation against GH#183's own spec, not reported separately. Fixed end-to-end: `devices.Device.ApnsEnvironment` field (validated on `Register`); `POST /api/devices/register` accepts and persists `apns_environment`; `apns.Dispatcher.baseURLFor()` resolves the correct host from the device's own environment, falling back to the configured default only when a device never reported one; both `Send` call sites (`POST /api/push/apns/test`, the real alert-fire fan-out) thread the device's environment through. | v8.63.4 |
| B89 | GH#180 — two config/manager sync gaps, same shape as B86 (GH#179): `PUT /api/autonomous/config` only updated the autonomous manager's own in-memory Config, never `s.cfg.Autonomous`/config.yaml, so `GET /api/config` (every mobile client's gate for the Automata tab) never saw the change. Separately, `PUT /api/config {"council.llm_ref":...}` + `POST /api/reload` updated `s.cfg.Council.*` and config.yaml correctly but never reached the live `council.Orchestrator` — `LLMRef`/`Backends`/`MaxParallel` were plain struct fields set once at daemon startup (`runStart`) and never revisited, so the council kept resolving the startup LLM until an actual restart. Fixed: `handleAutonomousConfig`'s PUT case now marshals the manager's post-merge Config back onto `s.cfg.Autonomous` and persists; new `council.Orchestrator.SetLLMConfig()` (mutex-guarded) + a `reload()` diff-and-apply block for the three council fields. | v8.63.5 |
| B90 | GH#174 — MCP channel bridges (`cmd/datawatch-channel`) announced their listening port to the daemon exactly once at startup, guarded by a one-shot atomic. After a daemon restart, the daemon forgot every session's `ChannelReady`/`ChannelPort` (daemon-side state), and since the bridge never spoke again, "Waiting for MCP channel…" never cleared on any client. Confirmed live by the reporter: 2,435 sessions, 0 with `bridge_alive`. Fixed with a 60s self-healing re-announce (`sendReady`/`readyHeartbeat`) — `handleChannelReady` already overwrites the same fields idempotently each call, so repeating it is safe regardless of root cause. Deployment note: this is the separate `datawatch-channel` binary, not the daemon — already-running bridge subprocesses inside existing sessions only pick up the fix on their next (re)spawn. The issue's third suspected cause (possible duplicate bridge spawns per session) is owned by Claude Code's own MCP client lifecycle, not this repo — left open for separate investigation. | v8.63.6 |
| B91 | GH#173 — `datawatch config generate` wrote the four `detection.*_patterns` keys as the literal string `"[]"` (a Go string passed to `yamlVal`, which quotes anything containing `[]`) instead of a real empty YAML list, so the generated config failed to load at all (`cannot unmarshal !!str`). Fixed: pass a real `[]string{}`, add a `[]string` case to `yamlVal` that renders a proper flow sequence. Also hardened the side effect the issue flagged: `start --foreground --config <path>` with a parse error was silently falling back to `config.DefaultConfig()`'s (production) data dir for the pre-flight PID-lock check instead of erroring out — manually verified via `daemon.pid`'s mtime that this no longer happens. | v8.63.7 |
| B92 | GH#181 — council replies are markdown per the operator's 2026-10-05 three-platform decision; the PWA's completed-run viewer (`councilViewRun`) already rendered them that way, but the live in-progress run log (`councilOpenLiveWatch`'s SSE `persona_response`/`run_completed` handlers) still truncated to plain text at 600/400 chars — that earlier fix never reached the live-streaming path. Fixed with a new `appendMarkdown()` helper reusing `_renderMarkdownFileInto` verbatim (same sanitization path, no new code), rendering each reply as a collapsible markdown block matching the completed-run viewer and the apps. Also closed **GH#184** while triaging this batch — already fixed in v8.60.1, issue just never closed; the one other occurrence of its bug pattern is in dead code (`_loadPRDSessionResources`, unconditional early `return`). | v8.63.8 |
| B93 | GH#186 — operator parity request (2026-10-06): animated-eye content-loading state across the PWA, matching the apps' `DatawatchLoadingContent`/`CardSkeleton`. New shared `loadingEyeBlock(label, size)` primitive + a global `MutationObserver` that auto-starts every instance (mirrors the existing `autoAttachMics` pattern — one observer, not ~100 manual wiring calls). Applied across 123 call sites (Observer/Settings/Dashboard/Alerts/Automata/Session-detail/folder-browsers); a handful of spots left untouched where there was no existing loading-placeholder to mechanically swap (peer-snapshot dialog row, Signal-linking QR wait, Daemon-ops interfaces panel — synchronous, no fetch). **Not visually verified** — no browser access in this environment; confirmed via `node --check`, the full existing JS suite, and a manual diff review for HTML-nesting correctness instead. | v8.63.9 |
| B94 | GH#186 follow-up — a live browser spot-check by the datawatch-app peer (headless Chromium, response delays held open, canvas pixel-diffed to confirm the animation genuinely runs) found the Dashboard showed no loading state at all: `#dashCardGrid` rendered completely empty until `GET /api/dashboard/layout` resolved, because only individual card bodies (built later) ever contained a loading eye, not the grid container itself. Fixed by giving the grid an immediate top-level loading block, matching the already-correct Automata list/detail pattern. Also confirmed via code review (not live) that the issue's "detail Graph tab" item needs no fix — the dependency-graph section renders synchronously inside the single PRD-detail fetch, which already shows the eye. | v8.63.10 |
| B95 | B98 — cost/token accounting reported zero for every session on every backend; `AddUsage` was only ever called from the manual `POST /api/cost/usage` endpoint, never automatically. Researched every backend datawatch supports (confirmed interactive and one-shot/PRD sessions share one spawn code path, not divergent modes) before writing code. Fixed for claude-code (polls its own deterministic-path JSONL transcript every 10s, deliberately excludes cache tokens since `CostRate` has no cache-aware rate and folding them in would wildly overstate cost) and ollama's session-backend chat mode (wires its already-decoded-and-discarded `prompt_eval_count`/`eval_count` into `AddUsage`). Not yet fixed: opencode/goose (mechanisms confirmed, need one live-verification pass each), OpenWebUI session-backend streaming (uncertain without a live instance), aider/gemini (not installed in this environment), and the separate one-shot inference-adapter path (Council/ask/verifier calls aren't session-attributed, so fixing it wouldn't move this bug's own symptom — tracked as its own follow-up). | v8.63.11 |
| B95 (cont.) | B98 follow-up — finished opencode and goose after live-verifying each mechanism first. opencode assigns its own unpredictable `ses_...` ID at launch; resolved once per session by matching `opencode session list --format json`'s `directory`+`created` fields, then polls `opencode export <id>` (skipping its non-JSON prefix line, tracking the delta of its cumulative `info.tokens.{input,output}` since the last poll — reasoning tokens deliberately excluded, same double-count caution as claude-code's cache-token exclusion). goose's `--name` is only passed at launch when `Session.Name` is non-empty; when set, `goose session export --name <name> --format json` works directly as a lookup key (live-verified end to end with a real `goose run` session) with no need to resolve goose's own separate date-based internal ID — tracks the delta of its cumulative `accumulated_usage.{input_tokens,output_tokens}`; a no-op (not a guess) when `Name` is empty. Also found and fixed a real wiring gap in the v8.63.11 fix itself: `ollama.SetUsageFn` was added but never called from `cmd/datawatch/main.go`, so ollama chat-mode sessions were still silently at zero. | v8.63.12 |
| B95 (cont. 2) | B98 continued once more on the operator's explicit follow-up to install missing binaries and use the already-configured OpenWebUI. Installed `aider` (`uv tool install --python 3.12 aider-chat` — this machine's only Python, 3.14, has no released `aider-chat` wheel yet; `uv` fetched an isolated 3.12 toolchain, no sudo/system changes) and `@google/gemini-cli` (plain `npm install -g`), and stood up a real local OpenWebUI (Docker, `ghcr.io/open-webui/open-webui:main`, routed to the existing local Ollama) to live-verify the two backends left open in the row above. OpenWebUI: confirmed `/api/chat/completions`' final SSE chunk carries a `usage` object; extended the existing chunk-decode in `conversation.go`'s `sendAndStream` (same code already scanning every chunk, just wasn't keeping this field) and wired a new `SetUsageFn`, mirroring ollama's shape. aider: no export/API exists at all, but it prints its own `Tokens: N sent, M received.` line straight into the tmux pane log the session monitor already tails (live-verified via a real `pipe-pane` capture of a live run) — new `internal/session/aider_usage.go` parses that line, including correctly reversing aider's own `format_tokens()` k-abbreviation above 1,000/10,000 tokens (confirmed from aider's source, not guessed). gemini: binary now installed and its CLI researched (`-o json`/`stream-json`, real `usageMetadata` fields confirmed in the package), but left unimplemented — needs either real Google Cloud credentials to live-verify, or a decision to rearchitect `gemini`'s `Launch` to be Go-mediated like `openwebui`'s (trading the current plain-text transcript for structured output), which is the operator's call, not a unilateral one. | v8.63.13 |
| B95 (cont. 3) | B98 finished for gemini — the operator chose the Go-mediated rewrite over leaving it open. `gemini.Launch` now runs `gemini -p <task> --output-format json` via Go instead of a raw shell command, parses the result, and re-prints only the response text to the pane (same `printf`-per-line mechanism as `openwebui`'s backend). Live end-to-end testing (real unauthenticated CLI run through the new code path in a real tmux pane) caught a genuine bug a code-read alone would have missed: gemini writes its error JSON to **stderr**, not stdout — the first version only read stdout, so every error fell through to a raw-JSON-dump fallback; fixed to try stdout then fall back to stderr, re-verified clean. `extractUsage` only reads a flat top-level token-count pair (several name variants tried) and deliberately does not recurse into a nested per-model breakdown confirmed to coexist with the same aggregate totals elsewhere in gemini-cli's own source — avoiding a double-count guess; the success-path fields themselves are confirmed from gemini-cli's source but not yet live-exercised (no Google Cloud credentials in this environment). Separately: the operator pointed out a real OpenWebUI instance already exists on the `datawatch` compute node (`http://datawatch:3000`) — the throwaway local Docker container from the row above was redundant and torn down; `openwebui.url`/`enabled` now point at the real instance via `datawatch config set`, pending a real API key from that instance's admin (self-signup is disabled there). | v8.63.14 |
| B96 | Found wiring the real OpenWebUI API key: `openwebui.api_key: "${secret:name}"` never worked at all — `openwebui.NewInteractive(...)` is constructed during early backend registration in `cmd/datawatch/main.go`, hundreds of lines before `secretsStore` exists or `secrets.ResolveConfig` runs (which itself depends on `httpServer`/`agentMgr` already existing, so moving it earlier isn't small), so the backend's `apiKey` field permanently held the literal, unresolved `"${secret:name}"` string. Live-verified the real symptom first (a genuine 401 from the real `datawatch:3000` instance using a key confirmed working via direct `curl`) before diagnosing the ordering bug. Fixed with a new `openwebui.SetAPIKey(key)`, called right after `ResolveConfig` succeeds, re-applying the resolved value to the already-registered backend (same package-level-setter shape as `SetChatEmitter`/`SetUsageFn`). Live re-verified end to end: a real session now gets a real response and reports real `tokens_in`/`tokens_out` against the real instance. | v8.63.15 |
| B97 | Operator-reported: `.decompose-output-*.json` / `CHECKPOINT.md` land directly in a PRD's shared `project_dir` with no lifecycle — polluting `git status`, never cleaned up, nothing stopping two PRDs sharing a project_dir from treading on each other's scratch state. Traced (while investigating stray `Task`/`Write` debris files in this very repo) to datawatch's own decompose/checkpoint-protocol code, not any LLM backend. `.decompose-output-*.json`: read exactly once by datawatch's own code — now relocated to `<data_dir>/autonomous/scratch/<prd_id>/` and deleted from `project_dir` right after that read. `CHECKPOINT.md`: a *retry* worker session needs to read it to resume, and that session is *also* sandboxed to `project_dir` — can't just relocate it without breaking resumability. Fixed by having datawatch itself (unsandboxed) read it after each attempt, relocate to the scratch dir, and fold its content directly into the next attempt's retry hint as literal prompt text — no file read needed by the next session at all. Both patterns also auto-gitignored (new `tooling.EnsureIgnoredPatterns`, extracted from BL219's per-backend mechanism but deliberately kept out of `BackendArtifacts` itself, which `QueryAllStatus`'s PWA card assumes is exclusively real backends — a test pins that boundary) as defense-in-depth for the crash-mid-task window. | v8.66.1 |
| B84 | Executor continues past failed tasks; dependent tasks still launched; story/PRD never roll up on failure. Three root causes: (1) `executeOne` sets `TaskFailed` and returns `m.store.SaveTask(t)` which is `nil`, so the sequential loop's `if err != nil` never fires — next task is dispatched regardless. (2) Neither sequential nor concurrent path tracks which tasks failed, so tasks whose `DependsOn` includes a failed task are still launched. (3) `storyAllTasksDone` and the final PRD rollup only count `TaskCompleted`; failed/cancelled tasks leave stories and PRDs in `in_progress` forever. Fix: sequential path adds `failedIDs` map, re-reads authoritative task status after each `executeOne`, marks dependents `TaskFailed` before launch; concurrent path adds `autoFailDeps()` helper called after each result; `storyAllTasksDone` changed to use `isTaskTerminal` (completed/failed/cancelled); PRD rollup sets new `PRDFailed` status when all tasks terminal but ≥1 failed. | v8.27.2 |
| B83 | Automata watchdog blind to `verifying`/`running_tests` tasks (SSE stall not detected) + insecure spawned watchdog schedule. Built-in watchdog goroutine only checked `in_progress` tasks; tasks stuck in `verifying` or `running_tests` with a timed-out SSE stream were never killed. A cron schedule also spawned opencode sessions with a shell-based watchdog prompt containing three security violations: `curl` to daemon API, `tmux capture-pane` cross-session terminal read, and writes to `/home/dmz/.datawatch/` outside workspace. Fix: extend watchdog task-state filter to cover all three active states; delete schedule. | v8.27.1 |
| B82 | Verifier hard-failed tasks that produced only new untracked files. Root cause: v8.25.10 added a hard fail when `git diff <pre-task-SHA>..HEAD` returns empty — correct for tasks that wrote nothing, but also fires when the task created brand-new files that are not yet tracked by git (e.g. new markdown specs). `git diff` only shows changes to tracked files; untracked new files are invisible to it. Fix: capture `git ls-files --others --exclude-standard` at spawn time into `Task.PreTaskUntrackedFiles`; post-task, diff the two untracked lists — files not seen pre-task are "new files"; include them in the verifier prompt as `<new_files>` section; only hard-fail if both diff and new-file list are empty. | v8.25.12 |
| B81 | Guardrail unparseable LLM responses advanced PRD. Both the autonomous PRD guardrail (`cmd/datawatch/main.go` line ~4233) and orchestrator guardrail (~4624) defaulted to `Outcome: "warn"` when the LLM returned malformed JSON — a "warn" still allows PRD advancement. Root cause: the permissive parse was intentional in the original implementation (BL191 Q6 note: "best-effort runs still progress") but creates the same false-positive path as the verifier bugs B79/B80. Fix: changed both defaults and empty-outcome fallbacks to `"block"` (severity `"medium"`). Scan grader parse failure also fixed: returns `"fail"` instead of `"warn"`. | v8.25.11 |
| B80 | Autonomous verifier empty diff passed to LLM with "verify on spec alone" fallback, allowing tasks that produced no output to pass verification. Root cause: when `PreTaskSHA` was captured but `git diff` returned an empty string (task ran but wrote nothing), `autonomousVerify` fell through to a verifier prompt with "If no diff is present, verify on spec alone" instruction. LLMs reliably approved tasks on spec alone. Fix: empty diff when `PreTaskSHA` is set is now a hard verification failure (`OK: false`, `Summary: "verifier: no changes detected (diff empty against pre-task SHA); task produced no output"`), triggering `AutoFixRetries`. | v8.25.10 |
| B79 | Autonomous verifier unparseable LLM response defaulted to `OK: true`. Root cause: `autonomousVerify` in `cmd/datawatch/main.go` initialized `VerificationResult{OK: true, ...}` then attempted `json.Unmarshal` into it — if the LLM returned prose or malformed JSON, the default was preserved and the task was falsely marked verified. Fix: changed default to `OK: false` so any parse failure is a verification failure, enabling `AutoFixRetries` to trigger a re-run. | v8.25.10 |
| B76 | `DATAWATCH_COMPLETE:` not detected on one-shot TUI sessions when a progress animation rendered after the completion line. Root cause: `CapturePaneLiveTail` uses `tmux capture-pane` with no `-S` flag, so only the visible pane area is captured. When opencode's ⬝■ spinner ran after outputting `DATAWATCH_COMPLETE:`, the completion line was pushed into the scrollback buffer above the visible area before the 3-second watcher fired. The session stayed in `waiting_input` indefinitely. Fix: added `CapturePaneScrollback(session, lines)` to `TmuxAPI` and both completion-check sites now fall back to a 50-line scrollback scan when visible check misses the marker and `OneShot=true`. | v8.20.11 |
| B69 | Automaton detail view showed "No active session record" even when an autonomous task session was actively running. Root cause: the PWA filter `s.prd_id === prd.id` never matched because sessions were started without `prd_id` — the autonomous spawn POST body (`cmd/datawatch/main.go`) did not include `prd_id`/`task_id`, `StartOptions` (`internal/session/manager.go`) had no corresponding fields, and the stored `Session` struct (`internal/session/store.go`) had no JSON fields. Fixed by adding `PRDID`/`TaskID` to all three layers and propagating them through the start API (`internal/server/api.go`). | v8.20.8 |
| B70 | All automaton stories showed "0/N tasks · 0%" progress even when a task session was actively running. Root cause: the progress counter in `_renderStoryReadOnlyExtras` only counted `completed` tasks; `verifying` (active session running) and `running_tests` contributed zero. Fixed by adding an `active` count of tasks with status `verifying` or `running_tests` and appending "⟳ N active" to the progress label when `active > 0`. | v8.20.8 |
| B71 | Task status glyphs were missing for `verifying`, `running_tests`, and `blocked` states. The glyph map in `_renderStoryReadOnlyExtras` only covered `completed`, `failed`, `running`, and `pending`. Added ⟳ (verifying), 🧪 (running_tests), ⛔ (blocked) to the map; added CSS `status-verifying` (spinning accent color via `@keyframes spin`), `status-running_tests` (accent2 color), and `status-blocked` (error color). | v8.20.8 |
| B58 | Cancelled PRDs had no restart path. When an operator cancelled a running PRD (e.g. to fix the backend configuration), the detail view only showed "Clone to Template" in the Edit menu — Settings was hidden and there was no way to reset and re-run. Root cause: `_renderDetailHeader` in app.js only showed Settings/Edit Spec when `editable` was true, which excluded `cancelled`. Fix: added `cancelled` to the `editable` check; added `↺ Reset to Draft` toolbar button for `cancelled` state; added `ResetToDraft` manager method (`cancelled → draft`, clears stories/tasks, preserves spec + settings); `POST /api/autonomous/prds/{id}/reset_to_draft` endpoint; `autonomous_prd_reset_to_draft` MCP tool. | v8.20.1 |
| B108 | Test runs wrote into the operator's real `$HOME` — unisolated test `$HOME` (`go test ./internal/mcp`) and an E2E daemon on 18080 both rewrote real dotfiles (`~/CLAUDE.md`, `~/.mcp.json`). Root cause: `session.default_project_dir` was only applied in the REST start handler, so the manager fell back to `os.UserHomeDir()`. Fixed: manager honours `default_project_dir`; `TestMain` HOME isolation added in mcp/server/session packages; E2E template sets `default_project_dir` under `TEST_DATA`. | v8.73.24 |
| B107 | The shared default tmux server died unexpectedly (no OOM/segfault/kill-server found), killing the `datawatch`/`datawatch-app` sessions living on it. Fixed by moving sessions onto a dedicated `datawatch-tmux.service` (`TMUX_TMPDIR=~/.datawatch/tmux`, `Restart=always`, own cgroup) via `install.sh` user mode; attach hints are now socket-aware. Known residual gap at the time: sessions still need a manual `datawatch session restart` if that dedicated server itself ever crashes — auto-resume wasn't implemented. | v8.73.23 |

**⚠️ Bug-ID collision, found during the 2026-10-09/10 backlog refactor:** `B89` through `B97` were each independently assigned to two unrelated fixes — a v8.33.x-era batch (autonomous executor/verifier hardening) and a later v8.63.x-era batch (GH#173–186 fixes), in direct violation of the "never reuse a bug number" rule. Both sets of rows are listed below under the same numbers; there is no way to retroactively renumber one set without breaking its own commit-message/CHANGELOG cross-references, so this is documented rather than silently fixed. **The next new bug must use B109 or higher — do not reuse any number in this range.**

| B89 | Autonomous task stalled ~2h on an undetected Ollama "thinking" pause (`SSE read timed out`, missing from the stall-pattern list); added the pattern plus `opencode.ollama_chunk_timeout_sec`/`ollama_header_timeout_sec` config to raise OpenCode's own request timeout past real reasoning-pause durations. This entry's own original note said "not committed, pending operator's own commit" — corrected during this refactor: `git log` confirms it shipped as commit `5285fc89`. ⚠️ See the ID-collision note above — a different B89 (GH#180 config/manager sync gaps) also exists in this table. | v8.33.1 |
| B90 | Autonomous task verify wait-loop didn't survive a daemon restart, leaving tasks stuck `verifying`/`in_progress`/`running_tests` forever with no reconciliation. Fixed by `reconcileStuckTasks()` at Manager boot, which resets stuck tasks to pending (after killing any orphaned session) so `AutoFixRetries` picks them back up. ⚠️ ID collision — see note above. | v8.33.2 |
| B91 | The literal `DATAWATCH_COMPLETE: <one-sentence summary...>` placeholder text in the task-delivery instruction, visible on-screen for the whole session, falsely triggered completion detection and killed sessions at T+31-33s. Three-layer fix: `MarkTaskDelivered` moved before `SendInput` so task-echo suppression is active during input rendering; suppression window widened 30s→5min; explicit placeholder-text filter added to both the visible-scan and scrollback-scan loops. ⚠️ ID collision — see note above. | v8.33.17 |
| B92 | A transient prompt-pattern flash immediately after `send_input` fired spurious needs-input push alerts on every parallel PRD task start (1-2 spurious alerts per task on a 6-task PRD). Fixed with a 30-second post-input-sent suppression window that skips the `onNeedsInput` callback without affecting the real pre-delivery needs-input alert. ⚠️ ID collision — see note above. | v8.33.18 |
| B93 | Daemon restart could spawn a duplicate session for a task already `TaskVerifying` with a live session, since `executeOne` had no check for an already-alive session before respawning. Executor now detects the live session and verifies against it directly instead of spawning a duplicate; the orphaned duplicate is killed immediately. ⚠️ ID collision — see note above. | v8.33.19 |
| B94 | Selecting an image for attachment in the Automata command window gave no visible confirmation that the upload succeeded. Added a `showToast('Image attached — will be sent with your next message', 'success')` call to the existing upload success handler. ⚠️ ID collision — see note above. | v8.33.21 |
| B95 | Decomposer-produced stories could persist with `status="running"` (a valid `PRDStatus` but not a valid `StoryStatus`) when the LLM's decompose JSON included that value verbatim, permanently stalling the story since the executor's transition guard never fires for a non-pending initial state. `SetStories` now forces every freshly-set story to `StoryPending` regardless of LLM output. ⚠️ ID collision — see note above; this is a different bug than the 4 other B95/B95(cont.) rows above, which cover the cost/token-accounting bug (B98). | v8.33.20 |
| B96 | `config.DefaultConfig()` left `Autonomous.AutoFixRetries` at the Go zero value (0) instead of the intended default of 1, so verifier parse failures (a transient "unparseable response", not a real quality failure) were never retried and immediately cascaded to `PRDFailed`. Fixed by setting the correct default. ⚠️ ID collision — see note above. | v8.33.22 |
| B97 | `ResetTask` refused to resume a PRD stuck in `PRDFailed` after one task failed, requiring a destructive `reset_to_draft` (discarding all completed work) to recover. Fixed: `ResetTask` now also accepts `PRDFailed` and auto-restores `PRDRunning` so the executor picks the PRD back up without requiring a second operator step. ⚠️ ID collision — see note above. | v8.33.23 |
| B110 | `imap_mcp` outbound email bounced 5× in ~2h (SMTP 5.1.1 user unknown), relayed via `imap-mcp-79`. Root cause: the generic comm-channel Router always sends as `b.Send(r.groupID, text)`; for `imap_mcp`, `groupID` is just the literal label `"imap_mcp"` passed at `newRouter()` construction (there is no "group" concept for email), so every outbound send was addressed to that literal string. Fixed with a new `imap_mcp.to` config field the `Backend` uses to override whatever recipient it's called with — deliberately NOT fixed by repointing `groupID` itself, since `groupID` also drives *inbound* message matching (`router.go`'s `handleMessage`: `if msg.GroupID != r.groupID { return }`), and an inbound SSE event's `GroupID` comes from imap-mcp's own account identifier, not an email address; repointing it would have silently broken inbound matching while fixing the bounce. | v9.0.10 |
| B109 | `cost_summary` reported 4.3B output tokens / $64,532.70 for `claude-code` — not plausible. Root cause: the 4 usage trackers (claude-code/aider line-count based, opencode/goose cumulative-total based) kept their "already counted" checkpoint only in an in-memory `sync.Map`, never persisted; every daemon restart reset it to zero, so the next scan re-read/re-diffed each session's entire historical usage and re-added it on top of the already-persisted total via `AddUsage` (additive) — a session surviving N restarts had its real usage multiplied by roughly N+1. Fixed by persisting the checkpoint on the session record (`UsageLinesRead`/`UsageLastIn`/`UsageLastOut`) and seeding each tracker from it; a `resetPreFixUsageIfCorrupted` self-heal discards a session's stored totals when it shows usage with no checkpoint at all (a signature impossible under the fixed code), so its tracker's own next scan recomputes the true total fresh instead of being manually patched. First use of **B109** — this fix is also what surfaced the B89-B97 collision noted above; new bugs must use B109 or higher going forward, this number included, never B89-108 again. | v9.0.8 |

### Features & Backlog Completed

| ID | Item | Version |
|----|------|---------|
| BL23 | Episodic memory (SQLite + embeddings) | v1.3.0 |
| BL32 | Semantic search across sessions | v1.3.0 |
| BL36 | Task learnings capture | v1.3.0 |
| BL44 | Memory: auto-retrieve on session start | v1.4.0 |
| BL46 | Memory: export/import | v1.4.0 |
| BL48 | Memory: browser enhancements | v1.4.0 |
| BL50 | Memory: embedding cache | v1.4.0 |
| BL52 | Memory: session output auto-index | v1.4.0 |
| BL62 | Memory: write-ahead log | v1.4.0 |
| BL63 | Memory: deduplication | v1.4.0 |
| BL55 | Memory: spatial organization (wings/rooms/halls) | v1.5.0 |
| BL56 | Memory: 4-layer wake-up stack | v1.5.0 |
| BL57 | Memory: temporal knowledge graph | v1.5.0 |
| BL58 | Memory: verbatim storage mode | v1.5.0 |
| BL60 | Memory: entity detection | v1.5.0 |
| BL68 | Memory: hybrid content encryption | v1.5.1 |
| BL70 | Memory: key rotation and management | v1.5.1 |
| BL54 | Memory: REST API enhancements | v1.6.0 |
| BL61 | Memory: MCP KG tools | v1.6.0 |
| BL47 | Memory: retention policies | v2.0.0 |
| BL49 | Memory: cross-project search | v2.0.0 |
| BL51 | Memory: batch reindexing | v2.0.0 |
| BL53 | Memory: learning quality scoring | v2.0.0 |
| BL59 | Memory: conversation mining | v2.0.0 |
| BL64 | Memory: cross-project tunnels | v2.0.0 |
| BL65 | Memory: Claude Code auto-save hook | v2.0.0 |
| BL66 | Memory: pre-compact hook | v2.0.0 |
| BL67 | Memory: mempalace import | v2.0.0 |
| BL43 | Memory: PostgreSQL+pgvector backend | v2.0.2 |
| BL73 | Rich chat UI (bubbles, avatars, markdown) | v2.1.3 |
| BL77 | Chat UI: Ollama native chat mode | v2.2.0 |
| BL80 | Chat UI: image/diagram rendering | v2.2.0 |
| BL81 | Chat UI: thinking/reasoning overlay | v2.2.0 |
| BL82 | Chat UI: conversation threads | v2.2.0 |
| BL83 | OpenCode-ACP rich chat interface | v2.3.1 |
| BL84 | Tmux history scrolling | v2.3.4 |
| BL85 | RTK auto-update check | v2.3.5 |
| BL88 | `POST /api/memory/save` endpoint | v2.3.8 |
| F4  | Channel parity (threaded conversations)         | v1.0.2 |
| F8  | Health check endpoint                           | v1.0.2 |
| F9  | Fallback chains                                 | v1.0.2 |
| F11 | Voice input (Whisper)                           | v1.1.0 |
| F12 | Prometheus metrics                              | v1.0.2 |
| F15 | Session chaining — pipeline DAG executor        | v2.4.0 |
| F10 | Ephemeral container-spawned agents              | v3.0.0 |
| F17 | Mobile device registry (`POST /api/devices/register`) — closes GH [#1](https://github.com/dmz006/datawatch/issues/1) | v3.0.0 |
| F18 | Voice transcription (`POST /api/voice/transcribe`) — closes GH [#2](https://github.com/dmz006/datawatch/issues/2) | v3.0.0 |
| F19 | Federation fan-out (`GET /api/federation/sessions`) — closes GH [#3](https://github.com/dmz006/datawatch/issues/3) | v3.0.0 |
| BL89 | Mock session manager for unit tests (TmuxAPI interface + FakeTmux) | v3.1.0 |
| BL90 | httptest server for API endpoint tests | v3.1.0 |
| BL91 | MCP tool handler tests (direct handler invocation) | v3.1.0 |
| BL28 | Quality gates (test baseline + regression detection wired into Executor) | v3.2.0 |
| BL39 | Circular dependency detection (NewPipeline rejects cycles, DFS path output) | v3.2.0 |
| BL10 | Session diffing — git shortstat captured into Session.DiffSummary on completion | v3.3.0 |
| BL11 | Anomaly detection — stuck-loop / long-input-wait / duration-outlier helpers | v3.3.0 |
| BL12 | Historical analytics — `GET /api/analytics?range=Nd` day-bucket aggregation | v3.3.0 |
| BL17 | Hot config reload — SIGHUP + `POST /api/reload` re-applies hot-reloadable subset | v3.4.0 |
| BL22 | RTK auto-install — `datawatch setup rtk` downloads platform binary into ~/.local/bin | v3.4.0 |
| BL37 | System diagnostics — `GET /api/diagnose` health checks (tmux, sessions, disk, goroutines) | v3.4.0 |
| BL87 | `datawatch config edit` — visudo-style safe editor with validate-on-save loop | v3.4.0 |
| BL1  | IPv6 listener support — every bind via `net.JoinHostPort`; `[::]:port` dual-stack | v3.5.0 |
| BL34 | Read-only ask mode — `POST /api/ask` (Ollama + OpenWebUI), no session/tmux | v3.5.0 |
| BL35 | Project summary — `GET /api/project/summary?dir=` git + per-project session stats | v3.5.0 |
| BL41 | Effort levels per task — `Session.Effort` (quick/normal/thorough), full config parity | v3.5.0 |
| F14  | Live cell DOM diffing — `tryUpdateSessionsInPlace()` per-card diff path | v3.5.0 |
| BL5  | Session templates — `/api/templates` CRUD + `template:` start field | v3.6.0 |
| BL26 | Recurring schedules — `recur_every_seconds` + `recur_until` on ScheduledCommand | v3.6.0 |
| BL27 | Project management — `/api/projects` CRUD + `project:` start field | v3.6.0 |
| BL29 | Git checkpoints + rollback — pre/post tags + `POST /api/sessions/{id}/rollback` | v3.6.0 |
| BL30 | Rate-limit cooldown — `/api/cooldown` + opt-in `rate_limit_global_pause` | v3.6.0 |
| BL40 | Stale task recovery — `/api/sessions/stale` + configurable threshold | v3.6.0 |
| BL6  | Cost tracking — Session.tokens_in/out/est_cost_usd + `/api/cost` + per-backend rates | v3.7.0 |
| BL86 | Remote GPU/system stats agent — `cmd/datawatch-agent/` standalone binary | v3.7.0 |
| BL9  | Operator audit log — append-only JSONL + `/api/audit` filtered query | v3.7.0 |
| BL15 | Rich previews in alerts — `messaging.FormatAlert` Telegram/Signal/Slack/Discord formatting | v3.8.0 |
| BL31 | Device targeting (`@device` routing) — `session.device_aliases` + `/api/device-aliases` CRUD | v3.8.0 |
| BL69 | Splash screen — custom logo path/tagline + `GET /api/splash/{logo,info}` | v3.8.0 |
| BL42 | Quick-response assistant — `POST /api/assist` with dedicated assistant_* config | v3.8.0 |
| BL20 | Backend auto-selection (routing rules) — `/api/routing-rules` + test endpoint | v3.9.0 |
| BL24 | Autonomous task decomposition — `internal/autonomous/` full package, REST + MCP + CLI + comm | v3.10.0 |
| BL25 | Independent verification — VerifyFn indirection; BL103 real guardrail wiring | v3.10.0 |
| BL33 | Plugin framework — subprocess driver, manifest discovery, 4 hooks, full 7-surface parity | v3.11.0 |
| BL117 | PRD-DAG orchestrator + guardrail sub-agents — `internal/orchestrator/`, Kahn topo-sort, 9 MCP tools | v4.0.0 |
| BL85 | RTK auto-update REST surface — GET /api/rtk/version, POST /api/rtk/check, POST /api/rtk/update | v4.0.1 |
| BL166 | tools-ops helm re-add — get.helm.sh TARGETARCH tarball install | v4.0.1 |
| BL103 | BL117 real GuardrailFn — per-guardrail system prompt via /api/ask, unparseable→warn | v4.0.1 |
| BL180 | eBPF observer — Phase 1 Ollama tap, Phase 2 procfs caller attribution + cross-host + kprobes + BTF | v4.9.1–v5.13.0 |
| BL190 | Howto screenshot capture pipeline — 22 PNGs across 8 howtos, puppeteer-core recipe map | v5.11.0–v5.15.0 |
| BL191 | PRD-flow Phases 1–6 — review/approve gate, templates, Q4 recursive child-PRDs, Q6 per-story+task guardrails | v5.2.0–v5.10.0 |
| BL178 | Session last-response live re-capture for running sessions | v4.8.10 |
| BL184 | opencode-acp: recognition lag fix + thinking-message italic render | v4.8.20 |
| BL196 | Binary size: HTTP gzip middleware + -trimpath -s -w + UPX | v4.8.17 |
| BL200 | Howto coverage expansion — 13 walkthroughs total | v5.7.0 |
| BL201 | Voice/whisper backend inheritance — openwebui/ollama URL/key auto-inherit | v5.8.0 |
| BL202 | PWA full CRUD second cut — proper modals + LLM dropdowns | v5.5.0 |
| BL203 | Flexible LLM selection — per-task + per-PRD worker LLM overrides, 7-surface parity | v5.4.0 |
| BL204 | Subsystem hot-reload (POST /api/reload?subsystem=) + claude auto-accept disclaimer flag | v5.27.2 |
| BL205 | GET /api/update/check read-only endpoint + modern rate-limit patterns | v5.27.4 |
| BL207 | Claude permission_mode + model + effort per-session surfaces | v5.27.5 |
| BL208 | PWA running-badge pulse, 3-dot indicator, scroll-mode glyph swap, PRD card style | v5.27.7–v5.27.8 |
| BL209 | GET /api/quick_commands config-driven quick commands | v5.27.7 |
| BL210 | MCP coverage parity audit — all gaps closed (filter_list, backends_list, session_set_state, etc.) | v6.0.4 |
| BL211 | CapturePaneLiveTail() for state detection — scrollback no longer pins detection | v5.27.6 |
| BL212 | channel.js JS fallback memory tools parity (memory_remember/recall/list/forget/stats) | v5.27.9 |
| BL213 | Signal device-linking API (/api/link/qr, /api/link/status, DELETE /api/link/{id}) | v5.27.9 |
| BL214 | PWA i18n foundation DE/ES/FR/JA — 5 locale bundles, t() helper, data-i18n DOM sweep | v5.28.0–v5.28.3 |
| BL215 | Rate-limit length gate raised 200→1024 chars | v5.27.6 |
| BL216 | MCP channel bridge introspection (GET /api/channel/info) + BL109 stale-.mcp.json fix | v5.27.10 |
| BL218 | Channel session-start hygiene — SHA-256 EnsureExtracted, user-scope ~/.mcp.json sweep | v6.0.7 |
| BL219 | LLM tooling artifact lifecycle — BackendArtifacts registry, EnsureIgnored, CleanupArtifacts | v6.0.8 |
| BL220 | Configuration Accessibility Rule full audit — G1–G24 gap closures across 6 surfaces | v5.28.10 |
| BL221 | Automata redesign Phases 1–5 — launch wizard, template store, scan framework, Guided Mode, skills | v6.2.0 |
| BL226 | Service-level alert stream + System tab — source:"system" field, 4 instrumentation sites | v6.0.9 |
| BL228 | Scheduled commands + security scanners — schedule add/list/cancel + language Dockerfiles | v6.0.6 |
| BL239 | Bottom nav bar width — justify-content:space-around + flex:1 at 480px breakpoint | v6.2.0 |
| BL240 | Rate-limit auto-schedule recovery — 6 new patterns, 1024→2048 char gate | v6.2.0 |
| BL242 | Secrets manager — AES-256-GCM store, KeePass, 1Password, scoping, plugin injection, agent token | v6.4.7 |
| BL243 | Tailscale k8s sidecar — headscale client, sidecar injection, OAuth device flow, ACL generator | v6.5.3 |
| BL244 | Plugin Manifest v2.1 — comm routing, CLI subcommands, mobile declarations, session injection | v6.3.0 |
| BL245 | Schedule date display bug — _fmtScheduleTime() year<2000 check | v6.2.1 |
| BL246 | Automata tab UX overhaul — 7 items (sub-tabs, FAB, help text, menu, filters, workflow, launch form) | v6.6.0 |
| BL247 | Settings tab reorganization — card migrations + Observer↔Monitor unification | v6.7.3 |
| BL248 | Rate-limit detection overrides saved commands — StateRateLimited guard | v6.5.1 |
| BL249 | Session auto-reconnect after daemon restart | v6.5.1 |
| BL250 | Session state refresh after Input Required popup dismiss | v6.5.1 |
| BL251 | Agent auth/settings injection — AgentSettings, spawn-time secret resolution, 7-surface | v6.5.4 |
| BL252 | PWA i18n full coverage — 7 phases, ~190 keys, 5 locale bundles | v6.6.0 |
| BL253 | eBPF setup false-positive — kernel ≥5.8, cap_sys_resource, rlimit probe check | v6.5.1 |
| BL254 | Secrets-Store Rule retroactive sweep — 26 credential fields audited, zero retroactive targets | v6.11.3 |
| BL255 | Skill Registries + PAI default — 10 REST endpoints, 13 MCP tools, 7-surface parity | v6.7.0 |
| BL257 | Identity/Telos layer + Identity Wizard — internal/identity, 7-surface CRUD, L0 injection | v6.8.1 |
| BL258 | Algorithm Mode — 7-phase session harness, operator-driven phase advance, 7-surface | v6.9.0 |
| BL259 | Evals Framework — 4 grader types, Suite/Run YAML, 7-surface parity | v6.10.1 |
| BL260 | Council Mode multi-agent debate — 6 personas, debate/quick modes, 7-surface | v6.11.0 |
| BL261 | Settings → Automata tab card padding — Pipeline/Orchestrator/Skills cards | v6.7.7 |
| BL262 | Claude "out of extra usage" rate-limit detection — new trigger phrases | v6.11.3 |
| BL263 | Re-establish tmux pipe-pane bridge after daemon restart — RepipeOutput on ResumeMonitors | v6.11.9 |
| BL267 | HashiCorp Vault / OpenBao backend — VaultStore, KV v2 + static-token auth | v6.15.0 |
| BL268 | datawatch-definitions.md end-to-end population — all sections + TODO placeholders | v6.13.7 |
| BL269 | openAutomataHowto() opens definitions doc at /diagrams.html#automata | v6.12.1 |
| BL270 | Select-bar-fixed above bottom nav — bottom:var(--nav-h) + horizontal-scroll variant | v6.12.4 |
| BL271 | wizard-grid-2col + wizard-mobile overhaul + Start-from-template strip first | v6.13.1 |
| BL272 | Settings section padding normalization — 8px 14px inset + 6px inter-card gap | v6.12.4 |
| BL273 | /docs/ served by FileServer — defsLink()/docsLink() route all ?/docs links | v6.13.7 |
| BL274 | Docs-as-MCP-interface — hybrid index, 4 MCP tools, 22 curated howtos, plan-then-execute | v6.21.0 |
| BL277 | Remove yellow "Input Required" popup — all banner HTML/CSS/JS removed | v6.13.9 |
| BL278 | Light/dark mode toggle — [data-theme="light"] palette, FOUC-safe, localStorage | v6.13.11 |
| BL279 | Embedded docs viewer UX — tighter spacing + 48-doc See-also footer sweep | v6.14.0 |
| BL287 | PWA mic input regression — toasts at every voice state + defensive state.voice.chunks | v6.20.0 |
| BL288 | Settings → About card padding — .settings-section .settings-row 6px 14px | v6.19.0 |
| BL289 | Document Ollama use + no-GPU fallback tests — 7 features, per-path fallback | v6.22.2 |
| BL290 | datawatch-stats --help double-dash flag form — custom usage printer + typo fix | v6.19.0 |
| BL291 | Observer settings findable in PWA — Federated Observer card in Settings → General | v6.20.0 |
| BL297 | Council "Add Persona" wizard — SQLite drafts, LLM one-shot + edit + re-interview, 7-surface | v6.22.3 |
| BL298 | Toast / error UX — showError() helper, no auto-dismiss, ✕ button; ~15 app paths converted | v6.22.3 |
| BL336 | Anti-clobber typing detection + queue — 30 s TTY idle wait, per-session send queue, arrival-order drain | v8.9.18 |
| BL292 | Mic auto-attach on every large text input confirmed comprehensive — `autoAttachMics()` + MutationObserver | v7.0.0-alpha.39 |
| BL293 | Automata card button matrix documented per-state in `renderPRDActions()` for all 9 PRD states | v7.0.0-alpha.39 |
| BL294 | Session registry race fix — exclusive flock sidecar (`sessions.json.lock`) around `Store.persist()` | v7.0.0-alpha.39 |
| BL295 | Council `InferenceFn` wired to LLM dispatcher; stub fallback strings removed; `ErrNoInference` added | v7.0.0-alpha.39 |
| BL296 | 4 new council personas (platform-engineer, network-engineer, data-architect, privacy) + 7-surface CRUD | v7.0.0-alpha.39 |
| BL299 | Narrow-screen header 2-row layout — title row 1, icons row 2, card/PRD/Council headers wrap | v7.0.0-alpha.39 |
| BL300 | Alert dock auto-open closed — `renderAlertDock()` gated on `dock.expanded` | v7.0.0-alpha.39 |
| BL364 | OpenCode new-session model picker now sends `?node=<compute_node>`; Ollama provider block corrected to the OpenAI-compatible shape OpenCode actually recognizes | v8.13.34 |
| BL371 | Autonomous PRD: split planning vs. execution backend — `DecompositionProfile` wired end-to-end | v8.20.0 |
| BL370 | Autonomous PRD: `max_concurrent_tasks` config — goroutine-pool fan-out for independent tasks | v8.26.0 |
| BL378 | GPU load wired for remote compute hosts (NVML probe) + local system GPU in session/PRD cards | v8.26.0 |
| BL379 | Observer tab per-system stats card (local + federated peers + datawatch-stats hosts) | v8.26.0 |
| BL380 | CPU/GPU/memory stats card in PRD status view for active sessions | v8.26.0 |
| BL382 | Automata lifecycle: `cancel_story`/`cancel_task`/`requeue_task` (REST+MCP+PWA+comm), active session resource bars — corrected during the 2026-10-09/10 refactor from a stale "Pending, targeting v8.28.0" status; confirmed shipped via `git log` | v8.27.0 |
| BL389 | Capacity-aware Automata admission and queueing (3 phases) | v9.0.0 |
| BL381 | Per-story LLM config (`Story.Backend/Effort/Model`, task → story → PRD → global resolution) — REST/MCP/CLI/comm/fedCap v8.28.0, PWA story-editor picker v8.28.2; concurrent cross-story execution was already provided by BL370. Status-verified and closed during the 2026-10-10 plans refactor (had sat at a stale "Pending" in Open Features) | v8.28.0 / v8.28.2 |
| BL391 | Multi-provider web search registry + usage tracking + caching (SearXNG + Brave, SQLite result cache) | v8.39.0 |
| BL315 | PWA window-expand + install prompt — `.pwa-expanded` CSS toggle, `beforeinstallprompt`/`appinstalled` wiring, ⬇ Install header button | v8.66.2 |
| BL317 | Multi-server PWA — server picker on 5 nav views, real aggregation fan-out, per-row server attribution, Dashboard/Observer fan-out, TS-387–396 re-verified | v8.69.2 |
| BL318 | OpenCode provider API keys confirmed never touching `opencode.json` in plaintext (GH#95), re-verified against the current tree | v8.8.0 |
| BL319 | `extra_mcp_servers` config — inject additional MCP servers into every spawned session (GH#118) | v8.13.0 |
| BL396 | PWA parity adoption sweep — GH#172/176/177/178/181/182 batch (25-item D59–D83 list + brand casing + Automata pause/resume + council markdown + per-story/GPU resource cards), phased zero-design → API-wiring → new-surface | v8.39.27–v8.61.0 |
| BL397 | Native ACME/Let's Encrypt subsystem + APNs push (folds in BL335 as its own Phase 4) | v8.62.x |
| BL335 | APNs push notification support for iOS (GH#107/#158) — folded into BL397 Phase 4 rather than shipped standalone | v8.62.x |
| BL399 | GH#201 access/audit logging completeness — 5 phases: HTTP/WS/auth-failure access log, federation-hop actor attribution (HMAC hop chain), `ParentAgentID` on chained agents, ~40-site state-changing-action sweep, `create_alert` REST+MCP | v8.78.1 |
| BL401 | Cross-Session Communication Rule — CLAUDE.md policy injection steering spawned sessions toward datawatch's own audited memory/discussion tools instead of native cross-session messaging | v8.78.2 |
| BL432 | Server payload optimization for mobile/PWA clients (GH#242) — compact session list, API gzip, alerts paging/count + `alerts_read`, WS `hello` delta list + subscribed output, `/api/info` capabilities | v9.0.76–v9.0.82 |
| BL344 | Alert click navigates to session detail — "Go to session →" button | v8.10.3 |
| BL346 | Push `session_state_changed` lifecycle events via `PublishToTopic` | v8.10.3 |
| BL348 | PWA session tree view — "Tree" toggle in Sessions toolbar | v8.10.3 |
| BL349 | `get_my_session_id` — MCP tool + REST + CLI + comm | v8.10.3 |
| BL350 | Orphan lineage listing — `GET /api/sessions/orphaned` + `?orphan_*` filters | v8.10.3 |
| BL351 | Kill-children recursive cascade — `kill_children_recursive` flag | v8.10.3 |
| BL352 | Federation lineage parity — `GET /api/sessions/aggregated?parent_id=` | v8.10.3 |
| BL353 | Recurring named schedules — `ScheduledCommand` gains `session_name` | v8.10.4 |
| BL354 | Name-addressed session operations — `resolveSession` tie-breaking fix | v8.10.5 |
| BL355 | Session crash/zombie detection — `ClaudeAlive *bool` added to `Session` | v8.10.6 |
| BL356 | Session crash/exit hooks — `ExitHookEntry` store (`exit_hooks.json`) | v8.10.7 |
| BL357 | Durable role-based work queue — `QueueItem` (id, role, payload, state) | v8.10.8 |
| BL358 | Discussion push / subscribe — `DiscussionSubStore` (JSON-persisted) | v8.10.9 |
| BL359 | `restart_session` from any state — `RestartSession(ctx, id, task)` | v8.10.10 |
| BL360 | Structured agent result store — `ResultEntry` (name, payload, expires) | v8.10.11 |
| BL361 | `list_sessions` structured filters — `SessionFilter` struct | v8.10.12 |
| BL387 | PRD memory integration — verifier memory, child inheritance, decomposer enrichment, cross-PRD seeding, auto-report, PWA tile (6 features) | v8.33.0 |
| BL386 | Memory lifecycle management — warm-start seeding, harvest, archive-on-delete, handoff, PRD report | v8.30.0 |
| BL385 | Subprocess memory scope isolation — session-local writes, scoped reads | v8.29.0 |
| BL384 | Decomposer scope-drift hardening — qwen/ollama ignoring PRD-level doc-only constraints, documented + guarded | v8.28.3 |
| BL383 | PWA session elapsed clock alongside session-active indicator | v8.28.3 |
| BL369 | Prompt injection hardening in autonomous executor | v8.18.0 |
| BL368 | Vision input system — image attachments across comms, skills, sessions, MCP (4 phases) | v8.15.0 |
| BL367 | Autonomous PRD quality gates (BL28 parity for autonomous executor) | v8.17.0 |
| BL324 | Community Skills + Plugins registry (GitHub-hosted, categorized, user-contributed) | v8.1.0 |
| BL325 | External community registry — discovery UX, plugin install | v8.1.0 |
| BL326 | Mic recording popup — animation + cancel/send controls | v8.1.0 |

### Promoted to Features

Per the no-reuse rule, the original BL numbers stay reserved. Status reflects the current state of the parent F-feature.

| BL  | Promoted to | Status |
|-----|-------------|--------|
| BL2 | F14 (Live cell DOM diffing) | Open (F14 still in Open Features) |
| BL3 | F10 (Ephemeral container-spawned agents) | Shipped in v3.0.0 |
| BL4 | F15 (Session chaining — pipeline DAG executor) | Shipped in v2.4.0 |

### Dropped / Frozen

Numbers stay reserved (per the rule above) and are never reused.

| ID | Decision | Date | Reason |
|----|----------|------|--------|
| F13 | Dropped | 2026-04-19 | Copilot/Cline/Windsurf backends — operator decided not to support |
| BL19 | Dropped (with F13) | 2026-04-19 | Original BL that was promoted to F13 |
| BL38 | Dropped | 2026-04-19 | Message content privacy — operator decided not to pursue |
| BL45 | Frozen | 2026-04-19 | ChromaDB/Pinecone/Weaviate backends — operator unsure if needed; revisit if pgvector hits a limit |
| BL7  | Frozen | 2026-04-19 | Multi-user access control — single-operator use stays the supported model for now; no work scheduled |
| BL8  | Frozen | 2026-04-19 | Session sharing (time-limited links) — depends on BL7's auth model; frozen with BL7 |


See [testing.md](../testing.md) for test results and pre-release checklists.

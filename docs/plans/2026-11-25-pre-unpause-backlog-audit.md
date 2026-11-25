# Backlog registry audit before unpause

- **Date:** 2026-11-25
- **Version:** v9.187.17 (version of main when this record was written)
- **Scope:** `docs/plans/` only: this record, and status corrections in [README.md](README.md). No code, configuration, version or changelog change.
- **Phases:** 1. audit of every backlog item and tracker row (Done). 2. second pass over the weaker "delivered" verdicts (Done). 3. Automata filed for missing work (Done). 4. results recorded here (Done). The missing work itself is not built by this plan.
- **Status:** Done as a record. 299 requests are not delivered; two of them have no record and await the operator.

## Summary

1059 entries were judged: backlog items (BL), features (F), bugs (B) and tracker rows.

| Verdict | Count |
|---|---|
| delivered | 548 |
| partial | 230 |
| not delivered | 24 |
| deferred | 42 |
| unclear | 3 |
| superseded | 10 |
| dropped | 4 |
| not a request (log and ship-note rows) | 198 |
| **total** | **1059** |

The 299 requests that are not delivered (partial, not delivered, deferred, unclear) by release: 10.0: 264, 11.0: 22, not set: 4, post-10.0: 9.

**Two frozen requests have no record and await the operator:** BL45 (the ChromaDB, Pinecone and Weaviate memory backends) and F7 (a native Go replacement for signal-cli). A later review also found that native PostgreSQL vector search was requested and never built, which may meet the condition for revisiting BL45; whether to build or drop it is the operator's choice.

## How items were checked

1. **Code reading.** Each item was judged against main by reading code and documents. A changelog line, a "done" status, a shipped mark or a commit subject counted as a lead, not as proof. A request with several parts was partial if any part was missing. Most verdicts were made at v9.187.13; some later checks read v9.187.14.
2. **Second pass.** All 284 "delivered" verdicts of medium or low confidence were checked again by trying to disprove them. 224 held, 58 were disproved and are now partial (about one in five), and 2 could not be settled and are still counted as delivered. The 322 high-confidence "delivered" verdicts have no second-pass mark in the final results.
3. **Filing.** Each missing part was given to an Automaton (new ones were filed, others were already open) or was already being built on a branch, except for the two frozen requests below. When this record was written each Automaton in the table was read from the daemon: none read as cancelled, and most were held or draft. Being held by an Automaton does not mean the work is approved, planned or running.

## What was not run

- No Go test suite, build, release smoke, browser story or live behaviour check was run to reach the verdicts. One review job ran four web test files, and a few read-only status and configuration reads were made.
- "Delivered" therefore means the behaviour or document was found on main, not that it was exercised. Many delivered verdicts are of medium confidence: not every part or surface was traced.
- "Not delivered" and "no record covers it" rest partly on searches that found nothing, so work built under another name could be missed.
- Main moved while the audit ran and after it. Verdicts were not judged again against later merges.

## Not delivered

One line per item. The wording is shortened from the audit's one-sentence request, and the verdict covers the whole request, so a partial item is mostly built in some cases.

**Ids.** BL, F and B are tracker numbers. ROWn is tracker row n where the row has no backlog number, including rows now in [the November archive](roadmap-archive-2026-11.md). SEC-n names an entry in the tracker's later sections by its line number at audit time; it is not a security finding number, and the same work can also appear under its BL number with another verdict.

**Release marks.** ‡ the second pass or the build tracking left no release on the item, so the release shown is that of the Automaton holding it. § the item's audit release differs from the release of the Automaton holding it; the operator should confirm which is right. "not set" means neither names one.

**Notes.** 1. For BL448 the audit names an Automaton that was cancelled as superseded on the day of the audit. The open recovery Automaton `2baa0001` names the same branch and covers recovering its two research documents; it treats the branch's code as replaced by main's. 2. The audit also names two Automata for ROW550 that were cancelled as superseded the same day; only the open ones are listed.

| Item | What was asked | Verdict | Release | Held by |
|---|---|---|---|---|
| BL4 | Chain sessions into pipelines with conditional branching (promoted to F15). | partial | 10.0 | `d80b0001` |
| BL5 | Reusable session templates bundling directory, backend, env and auto-git. | partial | 10.0 ‡ | `6eba0001` |
| BL7 | Multi-user role-based access control with per-user channel bindings and whisper language. | deferred | 11.0 ‡ | `2fbf0001` |
| BL8 | Time-limited session sharing links for teammates. | deferred | 11.0 ‡ | `2fbf0001` |
| BL9 | Append-only, exportable audit log of who started, killed or sent input to sessions. | partial | 10.0 | `d15d0001` |
| BL10 | Put an automatic git diff summary (+47/-12, 3 files) in completion alerts. | partial | 10.0 | `c74e0001` |
| BL11 | Flag stuck loops, unusual CPU or memory use and long input waits. | not delivered | 10.0 | `c74e0001` |
| BL12 | Historical trend charts in the web UI: sessions per day, duration by backend, failure rates. | partial | 10.0 | `25040001` |
| BL15 | Show syntax-highlighted code snippets or terminal screenshots in alerts. | partial | 10.0 | `c74e0001` |
| BL20 | Automatically route a new session to the best backend by task type, load or rules. | partial | 10.0 | `c1c00001` |
| BL26 | Cron-style scheduled prompts with natural-language recurrence, per-project scope and quiet runs that notify … | partial | 10.0 | `c1c00001` |
| BL27 | Register, select and switch between project directories from chat channels. | partial | 10.0 | `c1c00001` |
| BL30 | On a subscription rate-limit cap, pause all operations, notify once and auto-resume. | partial | 10.0 | `5adb0001` |
| BL31 | Route a command to a specific machine with an @device prefix, using device aliases. | partial | 10.0 | `c1c00001` |
| BL33 | Plugin framework auto-discovering plugins that register commands, message matchers and help text. | partial | 10.0 | `c1c00001` |
| BL34 | A read-only 'ask: &lt;question>' chat command that answers without creating a session. | partial | 10.0 | `c1c00001` |
| BL35 | A 'summary' chat command giving project structure, technologies, dependencies and recent git changes. | partial | 10.0 | `c1c00001` |
| BL36 | Automatically capture key learnings after each completed session or task, searchable with 'learnings'. | partial | 10.0 | `c33a0001` |
| BL37 | A 'diagnose' chat command checking messaging backends, web sockets, tmux, disk and LLM backends. | partial | 10.0 | `c1c00001` |
| BL40 | On daemon restart, resume or fail in-progress sessions and notify. | partial | 10.0 ‡ | `dec30001` |
| BL42 | A cheap secondary assistant LLM for general questions, usable from chat. | partial | 10.0 | `c1c00001` |
| BL43 | A PostgreSQL+pgvector memory backend. | partial | 10.0 ‡ | `c33a0001` |
| BL44 | Automatically retrieve relevant memories when a session starts. | partial | 10.0 ‡ | `89920001` |
| BL45 | ChromaDB, Pinecone and Weaviate memory backends. | deferred | not set | **no record; awaits the operator** |
| BL46 | Export and import memories. | partial | 10.0 ‡ | `c33a0001` |
| BL47 | Per-role memory retention policies applied automatically. | not delivered | 10.0 | `c33a0001` |
| BL48 | Memory browser filters by role, project and date, with inline editing and bulk delete. | partial | 10.0 | `c33a0001` |
| BL51 | Batch re-embed all memories after a model change, including a memory_reindex MCP tool. | partial | 10.0 | `c33a0001` |
| BL53 | Rate the quality of learnings (1-5). | not delivered | 10.0 | `c33a0001` |
| BL54 | Expand the memory REST API with knowledge-graph endpoints, filters and pagination. | partial | 10.0 | `c33a0001` |
| BL55 | Organise memories into wings, rooms and halls with filtered recall. | partial | 10.0 | `c33a0001` |
| BL56 | A four-layer wake-up context loaded at session start. | partial | 10.0 ‡ | `89920001` |
| BL58 | A verbatim storage mode that stores raw exchanges instead of summaries. | partial | 10.0 | `c33a0001` |
| BL59 | Mine Claude, ChatGPT or Slack conversation exports into memory. | not delivered | 10.0 | `cf1f0001` |
| BL60 | Detect entities in memories and populate the knowledge graph automatically. | not delivered | 10.0 | `cf1f0001` |
| BL67 | Import memories from mempalace (ChromaDB). | not delivered | 10.0 | `cf1f0001` |
| BL69 | Splash screen with 24h throttle, version badge and a custom logo. | partial | 10.0 | `25040001` |
| BL70 | Memory key management: generate, rotate, back up, fingerprint, import and export keys. | partial | 10.0 | `56d10001` |
| BL72 | OpenCode auto-save memory hooks that detect its transcript format and mine it on completion. | partial | 10.0 | `cf1f0001` |
| BL78 | Wire the Gemini backend to the conversation manager and rich chat. | not delivered | 10.0 | `df5f0001` |
| BL79 | Parse Aider and Goose terminal output into user/assistant turns shown in chat. | not delivered | 10.0 | `df5f0001` |
| BL80 | Render image URLs and mermaid/PlantUML diagrams inline in chat bubbles. | partial | 10.0 | `df5f0001` |
| BL82 | Collapse conversation history into threads in chat. | partial | 10.0 ‡ | `25040001` |
| BL87 | 'datawatch config edit': visudo-style safe editor, including encrypted configs. | partial | 10.0 | `28dc0001` |
| BL90 | httptest tests for the REST API endpoints. | partial | 10.0 ‡ | `e10f0001` |
| BL91 | Tests for the MCP tool handlers. | partial | 10.0 ‡ | `e10f0001` |
| BL96 | Extend the wake-up stack so child agents inherit parent context (L4), see siblings (L5) and have own identity. | partial | 10.0 | `cf1f0001` |
| BL97 | Per-agent diaries recording what each ephemeral worker did. | not delivered | 10.0 | `cf1f0001` |
| BL98 | Detect contradicting knowledge-graph facts, flag them in UI and MCP. | not delivered | 10.0 | `cf1f0001` |
| BL99 | Closets and drawers: search summaries and drill into verbatim originals. | not delivered | 10.0 | `cf1f0001` |
| BL100 | A worker-side memory client so shared and sync-back workers use the parent's memory. | not delivered | 10.0 | `d80b0001` |
| BL102 | Let workers send alerts through the parent's chat channels. | partial | 10.0 | `d80b0001` |
| BL103 | A validator agent that automatically checks a worker's session when it ends (auto_validate). The id was later … | partial | 10.0 | `a91c0001` |
| BL104 | Peer-to-peer messaging between agents over REST, including the worker-side send helper. | partial | 10.0 | `d80b0001` |
| BL105 | Run pipeline steps that name a project and cluster profile as spawned container agents. | not delivered | 10.0 | `d80b0001` |
| BL106 | Apply the on_crash policy when a running worker fails or stops without reporting a result. | partial | 10.0 | `a91c0001` |
| BL107 | Query the agent audit trail over REST, MCP, chat and a web settings card. | partial | 10.0 | `d15d0001` |
| BL108 | Run the idle reaper and bump agent activity from all real activity paths. | partial | 10.0 | `a91c0001` |
| BL111 | Resolve cluster-profile credential references through the pluggable secrets provider. | not delivered | 10.0 | `d15d0001` |
| BL113 | Self-managing platform bootstrap for host and cluster installs. The id was later reused for a daemon-clone … | partial | 10.0 ‡ | `63e10001` |
| BL115 | Run and document the full functional test suite before the F10 release. | partial | 10.0 | `a91c0001` |
| BL116 | Badge sessions that have scheduled commands, in the web list and chat list. | partial | 10.0 | `c74e0001` |
| BL170 | Audit every shipped feature so each has an operator doc, an entry in both openapi files, MCP, CLI, comm … | partial | 10.0 | `a2f90001` |
| BL171 | Build datawatch-observer shape A: in-process unified stats, process tree and per-session/backend envelopes. | partial | 10.0 ‡ | `4a470001` |
| BL173 | Build datawatch-observer shape C: a cluster container with Kubernetes node metrics. | partial | 10.0 ‡ | `4a470001` |
| BL174 | Replace the Node channel.js with a native Go MCP channel server and slim the claude container onto a minimal … | deferred | 10.0 | `945e0001` |
| BL180 | Attribute shared ollama load back to the calling LLM using TCP flow correlation (procfs, cross-host, eBPF … | partial | 10.0 | `4a470001` |
| BL184 | Fix opencode-acp chat-mode recognition lag, and show thinking messages as soon as they arrive, collapsible. | partial | 10.0 | `df5f0001` |
| BL186 | Remove internal tracker and sprint ids from shipped runtime strings: log lines, warnings, setup output and … | partial | 10.0 | `9a6f0001` |
| BL190 | Write the how-to suite and capture PWA screenshots for it, to a planned density of about 15-20 per how-to. | deferred | 10.0 | `a2f90001` |
| BL209 | Serve quick commands from config so the PWA and Android stop hardcoding the list. | partial | 10.0 | `25040001` |
| BL218 | Keep the per-session channel bridge fresh: hash check, user-scope config sweep and a pre-launch log line. | partial | 10.0 ‡ | `945e0001` |
| BL219 | Manage each backend's tooling files: ignore-file hygiene, cleanup on session end, and cross-backend cleanup … | partial | 10.0 | fix built on branch `prep/bundle2-A`, merged to main after the verdict; not re-judged |
| BL220 | Audit every config option across all surfaces and close the gaps. | partial | 10.0 ‡ | `6eba0001` |
| BL221 | Build the Automata redesign Phases 1-5: wizard, templates, scan framework with an always-on git-history … | partial | 10.0 | `f4af0001` |
| BL224 | Fix the orchestrator flow diagram not rendering. | partial | 10.0 ‡ | `a2f90001` |
| BL225 | Fix the Automaton phase 3-4 flow diagram not rendering. | partial | 10.0 ‡ | `a2f90001` |
| BL226 | Send service failures (eBPF, memory backend, plugins, pipeline jobs) to the alert stream and show them on a … | partial | 10.0 | `c74e0001` |
| BL241 | Add Matrix.org as a full communication channel (finalized plan: cleartext base, E2EE, SAS verification and … | deferred | 11.0 | `82630001` |
| BL243 | Add the Tailscale sidecar: headscale client and sidecar injection (phase 1), OAuth device-flow activation via … | partial | 10.0 (Automaton: 11.0) § | `2a2e0001` |
| BL251 | Inject agent auth and settings at spawn for claude-code and opencode containers: API key from the secret … | partial | 10.0 | `d80b0001` |
| BL252 | Localize every remaining PWA screen so all UI strings go through t()/data-i18n. | partial | 10.0 | `9a6f0001` |
| BL257 | Add the identity/Telos layer (hot-reloaded, injected at session start and into Automaton planning) with an … | partial | 10.0 | `f4af0001` |
| BL258 | Add Algorithm Mode: a per-session option whose 7 phase boundaries are detected and gate the session to … | partial | 10.0 | `f4af0001` |
| BL259 | Add an evals framework with four graders including a working LLM rubric, run on session completion and as an … | partial | 10.0 | `f4af0001` |
| BL260 | Add Council Mode with personas and debate/quick modes, usable as an orchestrator guardrail and as a … | partial | 10.0 | `f4af0001` |
| BL263 | After a daemon restart, reconnect the tmux output pipe for every session that survived. | partial | 10.0 | `1a5a0001` |
| BL274 | Expose the docs over MCP so an AI can search, read, and plan or execute how-tos with approval. | partial | 10.0 ‡ | `d15d0001` |
| BL278 | Add a light/dark theme toggle with both themes meeting WCAG AA contrast. | partial | 10.0 ‡ | `25040001` |
| BL281 | Add Vault AppRole and Kubernetes authentication. | deferred | 10.0 (Automaton: 11.0) § | `2a2e0001` |
| BL282 | Add a Vault read cache with TTL and an invalidation command. | deferred | 10.0 | `de6a0009` |
| BL283 | Add per-actor Vault tokens with Vault-side scope enforcement. | deferred | 10.0 (Automaton: 11.0) § | `2a2e0001` |
| BL284 | Better secrets management: move secrets between backends, cross-backend search, rotation-age and expiry … | deferred | 10.0 | `de6a0009` |
| BL285 | Vault failure alerting and stability: alerts, retry/backoff, optional grace cache, health cadence and SLO … | partial | 10.0 | `de6a0009` |
| BL289 | Two requests under one id: scrub a leaked task id and add a voice test button and how-to (v5.4.0); document … | partial | 10.0 ‡ | `f4af0001` |
| BL292 | Three meanings: second leak audit (v5.6.0), eBPF kprobe attribution roadmap, and mic auto-attach on large … | partial | 10.0 | `4a470001` |
| BL295 | Wire real LLM inference into Council Mode instead of stub output. | partial | 10.0 | `f4af0001` |
| BL299 | Two meanings: narrow-screen (&lt;420px) header layout for Automata, session, Council run and LLM/ComputeNode … | partial | 10.0 | `25040001` |
| BL316 | Cross-host session federation (GH#52): instances auto-join, aggregate sessions, route input cross-host, with … | partial | 10.0 | `9ce30001` |
| BL318 | Three meanings: manage OpenCode provider API keys (GH#95); compute-node routing modes (v8.0.0) … | partial | 10.0 ‡ | `df5f0001` |
| BL322 | Expose the compute routing modes and new adapter kinds on all seven surfaces. | partial | 10.0 | `9ce30001` |
| BL325 | Community registry discovery UX, plugin signing/verification with a require-signing option, and a plugin … | partial | 10.0 | `c1f90001` |
| BL327 | Replace the 14 comma-separated settings inputs with chip multi-selects, with known-value dropdowns where a … | partial | 10.0 | `25040001` |
| BL328 | Make Automaton planning async with SSE stream, resume, polling fallback and PWA reconnect handling. | partial | 10.0 | `25040001` |
| BL331 | Federation over chat channels: identify a sending peer by channel_identity, route its messages to an … | partial | 10.0 | `15140001` |
| BL332 | Shared discussion memory scope between peers with push sync, offline delivery queue, conflict handling … | partial | 10.0 | `15140001` |
| BL333 | Federated file service with per-peer and per-discussion isolation, quotas, peer transfer, storage metadata … | partial | 10.0 | `15140001` |
| BL334 | Encrypt the remaining operational data files and the application log when --secure is on. | partial | 11.0 ‡ | `ca9e0001` |
| BL335 | Send native APNs push notifications to the iOS client (GH#107). | partial | 10.0 ‡ | `c74e0001` |
| BL340 | Add an imap-mcp email command channel backend. | partial | 10.0 ‡ | `c1c00001` |
| BL352 | Resolve parent/child lineage across federated peers. | partial | 10.0 | `9ce30001` |
| BL353 | Recurring cron schedules addressed to a session by name that survive restarts of that session. | partial | 10.0 ‡ | `89920001` |
| BL354 | Make session operations addressable by name, preferring the older session during a handoff. | partial | 10.0 ‡ | `6eba0001` |
| BL358 | Push new discussion entries to subscribed sessions and support long-poll on the discussion log. | partial | 10.0 | `9ce30001` |
| BL363 | Integrate the Goose backend fully (launch/resume, model injection, MCP channel, agent container) with tests. | partial | 10.0 ‡ | `63e10001` |
| BL366 | Ground the autonomous verifier in the git diff (a hostile-LLM assessment first filed under this id was … | partial | 10.0 | `f4af0001` |
| BL367 | Add per-Automaton quality gates that fail tasks introducing new test failures. | partial | 10.0 ‡ | `25040001` |
| BL368 | Accept image input across chat channels, sessions, MCP, skills, memory and council. | partial | 10.0 | `f4af0001` |
| BL369 | Harden autonomous prompts against injection: data boundaries, input scanner, and an untrusted-origin marker … | partial | 10.0 | `15140001` |
| BL373 | Improve the Automata PWA: fix task click and expand bugs, truncate spec, status graphs with compute stats … | partial | 10.0 ‡ | `4a470001` |
| BL378 | Show GPU load for remote compute hosts and the local system in session and Automaton cards. | partial | 10.0 ‡ | `4a470001` |
| BL382 | Add cancel story, cancel task and requeue for the Automaton lifecycle (GH#149-152). | partial | 10.0 ‡ | `c74e0001` |
| BL385 | Isolate subprocess memory: session-local writes and scoped reads. | partial | 10.0 ‡ | `c33a0001` |
| BL386 | Memory lifecycle for Automata: warm-start seeding, harvest, archive on delete, handoff, report. | partial | 10.0 ‡ | `cf1f0001` |
| BL387 | Integrate memory into Automata: verifier findings, child inheritance, planner enrichment, cross-Automaton … | partial | 10.0 ‡ | `cf1f0001` |
| BL388 | Reserved follow-up: memory tagging beyond role, an auto-promote policy engine, and per-scope memory quotas. | deferred | 10.0 | `cf1f0001` |
| BL390 | Council per-persona backend/model assignment with capacity; named council profiles; make backend "council" a … | partial | 10.0 | `f4af0001` |
| BL392 | Audit and harden GitHub configuration across datawatch and datawatch-app. | partial | 10.0 | `c1f90001` |
| BL393 | Add nested tags for organizing Automata. | partial | 10.0 ‡ | `86dc0001` |
| BL394 | Review all Dependabot and code-scanning findings, dismiss false positives and fix the real ones. | partial | 10.0 | `c33a0001` |
| BL395 | Implement the decided security fixes from the resumed walkthrough (SEC-015..024, HLLM-001..009), including … | partial | 10.0 | `945e0001` |
| BL396 | PWA parity adoption sweep of the Android/iOS GitHub issue batch (GH#172, 176, 177, 178, 181, 182) plus APNs. | partial | 10.0 ‡ | `c74e0001` |
| BL397 | Build native ACME / Lets Encrypt support (HTTP-01, DNS-01, hot swap) with APNs folded in. | partial | 10.0 | `22250001` |
| BL398 | Review every suppressed container-image CVE for real reachability and mitigate or document it (phases 0-4). | partial | 10.0 | `c1f90001` |
| BL400 | Tie alerts to filtered conditions and make every alert reach the SIEM. | deferred | 10.0 (Automaton: 11.0) § | `31280001` |
| BL403 | Plugin extension surfaces: manifest v3, supervised service plugins, widgets, federation gate, audit, all … | partial | 10.0 | `86dc0001` |
| BL405 | LLM enhancements phases 0-10: role tags, priority, rate pools, preemption, usage records, local cost … | partial | 10.0 | `27a90001` |
| BL407 | Give each Automaton its own git branch and an automatic PR on completion, including cluster mode. | partial | 10.0 ‡ | `63e10001` |
| BL408 | Assess spawned LLM sessions as hostile (data exfiltration, confused deputy, container escape) and triage the … | partial | 10.0 | `945e0001` |
| BL409 | Audit file and function names across the codebase and rename tracker-ID names to what they do. | not delivered | 10.0 | `7e460001` |
| BL410 | Classify Automaton failures with structured reasons and bounded conditional auto-retry, content auto-fix and … | partial | 10.0 | `e10f0001` |
| BL411 | Run a dead-code audit (fully plumbed but never read fields and paths) before v10.0.0. | not delivered | 10.0 | `7c960001` |
| BL413 | Enforce diff-aware project rules (for example tests must arrive with changed code) on autonomous runs and … | partial | 10.0 | `8bf20001` |
| BL414 | Roll LLM cost into Automaton decision records, report token spend, and add spend guardrails. | partial | 10.0 | `152e0001` |
| BL415 | Make the dependency-review required check able to pass under the Actions token. | deferred | post-10.0 | `3f1d0001` |
| BL416 | Give guardrails real tooled verification scripts with overrides, file provenance and docs (phases 0-5). | partial | 10.0 | `a2f90001` |
| BL417 | Fix the CI-load-only test flakiness in internal/autonomous (and the plugins check-script test) with a … | partial | 10.0 | `d5060001` |
| BL418 | Add a judgment-based rules review at Automaton completion, configurable by the operator. | partial | 10.0 | `22250001` |
| BL422 | Run a Claude model/effort tiering eval for dev-Automaton tasks, then add the tier-mismatch lifecycle event if … | deferred | post-10.0 | `f6a38c29` |
| BL424 | Write the operator guide for integrating a repository with Automata end to end, after the rest of the roadmap. | not delivered | 10.0 | `7f3e0001` |
| BL428 | Design and build concurrency control and usage budgets (tree write lock, inference caps, back-off, dollar … | partial | 10.0 | `5adb0001` |
| BL429 | Deliver a one-shot Claude Code task once instead of re-pasting it on every heartbeat. | partial | 10.0 | `df5f0001` |
| BL430 | Per-task, per-story and per-Automaton verification LLM override. | partial | 10.0 | `30060001` |
| BL431 | Build Automations: multi-Automaton orchestration (store, coordinator, fork/join, controls, budgets, parity … | partial | 10.0 ‡ | `4a470001` |
| BL433 | Audit PWA refresh and connection performance under load (GH#243), then adopt the low-bandwidth options. | partial | 10.0 | `049f4713` |
| BL434 | Show the datawatch-eye splash on every full-screen refresh or transition in the PWA. | partial | 10.0 | `25040001` |
| BL436 | Capture session history as per-turn chunks so it no longer depends on tmux scrollback (filed as a design pass … | partial | 10.0 | `df5f0001` |
| BL437 | Design (and later run) a recurring, hardware-aware model and effort eval. | deferred | post-10.0 | `53ee0001` |
| BL438 | Research Hermes Agent and what datawatch could learn from it. | not delivered | 11.0 | `b5ce0001` |
| BL439 | Research whether datawatch can be infrastructure for Hermes Agent. | deferred | 11.0 | `79890001` |
| BL440 | Make the session Status tab show Automaton task detail (the filing also listed open follow-ups a-h). | partial | 10.0 | `4a470001` |
| BL443 | Fix goose and aider LLM entries: inference adapters (or a clear reason none is needed) and the 'goose' name … | partial | 10.0 | `df5f0001` |
| BL446 | Design federation as a sibling-agent mail transport (GH#211), design first, then possibly build. | deferred | 11.0 | `b2110001` |
| BL448 | Stop long voice recordings failing, and design streaming long-form voice input. | partial | 10.0 | `2baa0001` (note 1) |
| BL449 | Open-model eval matrix runner and its runs. | deferred | post-10.0 | `53ee0001` |
| BL453 | Research whether open models can decompose plans effectively. | deferred | 11.0 | `b5ce0001` |
| BL456 | Design personal agents with personality and decision-making for a community user. | deferred | 11.0 | `2c9a27e2` |
| BL459 | Deliver the community skills catalog research and build the catalog's proposed skills. | not delivered | 10.0 | `ca590001` |
| BL460 | Implement the unaddressed recommendations from the open-LLM research. | deferred | 11.0 | `404726a8` |
| BL467 | MCP channel bridge registration should be owner-bound, not first-come (design, then fix). | partial | 10.0 | `2fbf0001` |
| BL469 | Worktree mode by default, direct mode as opt-in, and never two Automata on one direct tree. | partial | 10.0 ‡ | `30060001` |
| BL471 | Decide and build archive versus purge pruning of finished worktrees. | deferred | post-10.0 | `3f1d0001` |
| BL472 | Usage and cost calculator, including every other consumer of tracked models reporting into it. | partial | 10.0 | `152e0001` |
| BL473 | Build the Automation coordinator to the supervisor rules (checks, triage, tree admission, after-done … | partial | 10.0 ‡ | `27a90001` |
| BL475 | Workers must not commit themselves, and non-project folders must never be auto-committed. | partial | not set | fix built on branch `prep/stab-G5`, not merged when this was written |
| BL481 | Compact MCP/CLI replies for Automaton tools with a detail level on prd_get. | partial | 10.0 ‡ | `152e0001` |
| BL483 | Make every automation-run option configurable per project, including minimum model context and the model-swap … | partial | 10.0 | `27a90001` |
| BL487 | Runs must leave no debris: tests never write into the repo, the harness removes only its own files, unknown … | partial | 10.0 | `99e10001` |
| BL492 | Make pi workers observable and stall-resistant: JSON-mode launch, log progress reader, log heartbeat, loop … | partial | 10.0 | `df5f0001` |
| BL494 | Two uses of the id: (a) other model users must not evict the pinned build model (shipped 9.5.82); (b) a … | partial | 10.0 | `27a90001` |
| BL496 | Release close-out for 9.6.0 and 10.0.0: notes, testing tracker, app parity issues, how-tos, build/push/tag … | not delivered | 10.0 | `80660001` |
| BL498 | Sonnet plan review before approval and a verifier split for smoke and final-gate tasks, as settings. | partial | 10.0 ‡ | `152e0001` |
| BL502 | Drift guardrail must tell in-run from foreign edits, add drift_mode, allow task approval on a blocked … | partial | not set | fix built on branch `prep/stab-G2`, merged to main after the verdict; not re-judged |
| BL508 | Warn when a worker edits files outside the Automaton's declared files. | partial | 10.0 ‡ | `30060001` |
| BL528 | make test-e2e-pwa must run. | partial | 10.0 ‡ | `948b0001` |
| BL541 | Port the finished plan-review gate from its old branch onto main. | partial | not set | fix built on branch `prep/stab-G2`, merged to main after the verdict; not re-judged |
| BL549 | Make the web app stable on 5G over Tailscale (jittered reconnect, no reload while typing, light resync). | partial | 10.0 | `22250001` |
| BL568 | Exchange resource fingerprints between federated peers to confirm shared accounts or compute nodes. | partial | 10.0 | `22250001` |
| BL585 | Make the verifier backend a configurable policy on every config surface with parity tests. | partial | 10.0 | `2baa0001` |
| BL592 | Fix the shared navigation bar on phones, report other-tab findings, and add the scope to the regression suite. | partial | 10.0 | `2baa0001`, `9cdb0001` |
| BL598 | Provide MCP resource subscriptions for lifecycle events with an extensive regression suite. | partial | 10.0 | `9ce30001` |
| BL609 | Move the supervisor duties into the Automation coordinator (steps 1 to 11). | partial | 10.0 | `71f80001`, `27210001` |
| BL610 | Provide a structured operator decision channel on all surfaces, with the agent decision protocol asking … | partial | 10.0 | `8bf20001` |
| BL620 | Protect rules files from workers and the daemon, with git identities, branch protection and a memory … | partial | 10.0 | `8bf20001` |
| BL628 | Treat a worker whose only life sign is a hung child as stalled, and keep release builds compiling for Windows. | partial | 10.0 | `30060001` |
| BL636 | Clean up the session header and add the Tags dropdown. | partial | 10.0 | `a2f90001` |
| BL637 | File the missing datawatch-app parity issues for all PWA changes since v9.0.0 as a final stage. | deferred | 10.0 | `04b20001` |
| BL644 | Let an Automation member stand for work with no Automaton yet. | partial | 10.0 ‡ | `86dc0001` |
| BL649 | Re-point the open-model matrix at pi workers and produce one valid run with a redacted report. | partial | 10.0 (Automaton: post-10.0) § | `53ee0001` |
| BL651 | Stream voice input to the server and show text as it is spoken. | deferred | 11.0 | `7e640001` |
| F4 | Channel feature parity: threading, rich format, buttons, file upload. | partial | 10.0 ‡ | `c1c00001` |
| F6 | RTK integration phases: analytics, discover, setup wizard. | partial | 10.0 ‡ | `4a470001` |
| F7 | Replace signal-cli with a native Go libsignal. | deferred | post-10.0 | **no record; awaits the operator** |
| F11 | Voice input via Whisper (per-user language deferred to BL7). | partial | post-10.0 | `2fbf0001` |
| F15 | Session chaining: pipelines with conditional branching, later reworded to a DAG with API, MCP, web UI and … | partial | 10.0 | `d80b0001` |
| B1 | Reused id: (a) OpenWebUI interactive mode instead of curl script; (b) xterm crash and ~20s load; (c) … | partial | 10.0 | `152e0001` |
| B2 | Reused id: (a) terminal scroll leaking out of session detail; (b) Claude Code prompt-detection false … | partial | 10.0 | `945e0001` |
| B3 | Reused id: (a) state badges not updating during work; (b) LLM session reconnect after daemon restart; (c) … | partial | 10.0 | `27a90001` |
| B5 | Reused id: (a) opencode exit to shell not marking session complete; (b) history select controls off-screen on … | partial | 10.0 | `f4af0001` |
| B7 | Reused id: (a) back button hard to tap on mobile; (b) code test coverage at 11.2 percent, target 60 percent … | unclear | 10.0 | `e10f0001` |
| B22 | Reused id: (a) LLM filter buttons do not fit; (b) daemon crashes on unrecovered goroutine panics. | partial | 10.0 ‡ | `dec30001` |
| B95 | Reused id: (a) token/cost accounting zero for every backend (rows note follow-ups for other backends and the … | partial | 10.0 | `152e0001` |
| B107 | Shared tmux server died and took sessions with it (row notes automatic session resume after a … | partial | 10.0 | `dec30001` |
| B111 | GuidedMode flag was never read; make it a real gate (the fix added operator, council and guardrail_auto … | partial | 10.0 | `f4af0001` |
| B115 | Backfill missing e2e stories for v9.0.x (cluster-dispatch story deferred as its own follow-up). | partial | 10.0 | `a91c0001` |
| B121 | rules_file/context_file had no consumers: the decomposer and the scanner must use them. | partial | 10.0 | `8bf20001` |
| B122 | Large tmux sends corrupted input (commit also notes Cancel did not kill the planning session, left unfixed). | partial | 10.0 | `e1650001` |
| B123 | Verifier recorded ok:true with a false completeness critique; open question: use a stronger verifier by … | partial | 10.0 | `30060001` |
| ROW9a | Close the open phase-completion gaps since v9.0.87 (verifier model fix, lifecycle sinks docs/app issue/live … | partial | 10.0 | `22250001` |
| ROW17 | Build the per-LLM dollar-window budget with shared-billing detection, full parity, a live check and a … | partial | 10.0 | `22250001` |
| ROW18 | Build the Automation BudgetPolicy with parity, a live smoke and a datawatch-app parity issue. | partial | 10.0 | `22250001` |
| ROW20 | Build the Automation tab in the web UI (Phase 6) with a live browser smoke. | partial | 10.0 | `e10f0001` |
| ROW188 | Balance pi workers across compute nodes; operator decision: show the node beside each holder AND a pool … | partial | 10.0 | `86dc0001` |
| ROW224 | With an empty write scope the harness should stage nothing and skip the commit (the one behaviour of the … | deferred | 10.0 | `79dd0001` |
| ROW251 | Redraw the header Tags icon; a second sentence of the operator message ("The tab icon ...") was cut off. | partial | 10.0 | `25040001` |
| ROW257 | Raise an operator question automatically for every held Automaton state. | partial | 10.0 | `e3a30001` |
| ROW259 | Give every web card and settings section added since 9.0.0 a ? link and definition, with a test that fails on … | not delivered | 10.0 | `d6970001`, `8a3e0001` |
| ROW263 | Make datawatch do the things the supervisor was doing by hand (inventory of 15 items). | partial | 10.0 | `79dd0001` |
| ROW264 | Remove operator-machine identifiers from the public repos and add a whole-tree leak check that also checks … | partial | 10.0 | `d15d0001` |
| ROW268 | Make the merge pipeline per-project configuration. | partial | 10.0 ‡ | `79dd0001` |
| ROW293 | Restructure 10.0.0 work: bugs first, a continuous maintenance plane, three stages, and an integration guide … | partial | 10.0 | `7f3e0001` |
| ROW295 | Ship Automation checks and ship steps; follow-ups: browser story, monitoring wiring if needed, translations … | partial | 10.0 | `9a6f0001` |
| ROW296 | Honor explicit retry backend and resolve secret refs in LLM keys; follow-up: raise statement coverage of the … | unclear | 10.0 | `e10f0001` |
| ROW297 | Fix the stats installer; follow-ups: tests for --archive and the legacy fallback, and non-tautological … | partial | 10.0 | `e10f0001` |
| ROW299 | Add per-provider billing profiles; follow-ups: browser story, monitoring wiring, translations, cost per … | partial | 10.0 | `e10f0001` |
| ROW306 | Stop federated peer selection reloading to the primary; the new browser story TS-845 was not yet run. | partial | 10.0 | `f1200001` |
| ROW307 | Operator direction: batch related bugs with a tiny-fix lane, generate task wrapping, and list the new work … | partial | 10.0 ‡ | `30060001` |
| ROW311 | Fix federated peer selection; the browser story TS-846 was not run. | partial | 10.0 | `f1200001` |
| ROW328 | Add agent tracing covering all supported LLM backends in 10.0. | partial | 10.0 ‡ | `4a470001` |
| ROW331 | Find and fix every unlocked write that crashed the daemon in SavePRD. | partial | 10.0 ‡ | `dec30001` |
| ROW333 | Ship configurable decompose lint; known gap: no stats counters for lint runs. | partial | 10.0 | `e10f0001` |
| ROW335 | Secret scan follow-up; known gaps: no Prometheus counter or web Monitor card, and the smoke push checks are … | partial | 10.0 | `e10f0001` |
| ROW339 | Crash fix with known gaps: two unlocked readers, a slow race run and a safe-delete cache test that flakes … | partial | 10.0 | `d5060001` |
| ROW340 | Operator asks: fix the Automaton tab strip (scroll and taps), the Alerts help mark, the peer badge … | partial | 10.0 | `152e0001` |
| ROW346 | Ship per-node verifier slots; reviewer items left open: smoke section, locale-guard line, README. | partial | 10.0 | `e10f0001` |
| ROW347 | Ship queued requeue; open gaps: no web settings toggle and two toasts hard-coded in English. | partial | 10.0 | `9a6f0001` |
| ROW348 | Ship classification verdicts; gaps: no Prometheus counters or Monitor card for the classifier. | partial | 10.0 | `e10f0001` |
| ROW349 | Ship planning routing; gaps: live-daemon validation of the new routes (and commit trailers, now history). | partial | 10.0 | `9a6f0001` |
| ROW353 | Plan and build the daemon supervisor loop in six phases. | partial | 10.0 ‡ | `79dd0001` |
| ROW354 | Ship the supervisor loop in the daemon. | partial | 10.0 ‡ | `79dd0001` |
| ROW360 | Fix the phone tab strip that did not scroll and whose taps did nothing. | partial | 10.0 | `25040001` |
| ROW362 | Build federated peer placement from its design once the operator confirms it. | deferred | 10.0 | `9ce30001` |
| ROW364 | Re-file the measured headroom check for a verifier cap above 1, with the five review findings. | partial | 10.0 ‡ | `5adb0001` |
| ROW374 | Audit every config option against every surface in CI as a gate and file the gaps. | partial | 10.0 | `41940001`, `750d0001` |
| ROW380 | Fix two gate defects: restore the whole verify folder (with lib.sh) into task worktrees or fail as an … | partial | 10.0 | `0d0e0001` |
| ROW382 | Stop hung pi live-check tasks: a 30-minute no-activity watchdog counted as infra failure, a spec hint to … | partial | 10.0 | `30060001` |
| ROW383 | Finding for the operator: the small default model fills GPU memory and the verifier minimum blocks by 44 MB. | unclear | 10.0 | `27a90001` |
| ROW390 | Verify and file open GitHub issues, including the PWA refresh and connection audit (#243). | partial | 10.0 | `049f4713` |
| ROW391 | Make the stale-CVE cleanup workflow reuse one branch and pull request and close it when nothing is left. | not delivered | 10.0 | `c1f90001` |
| ROW392 | Capture time, tools and details for charts and timelines, plus a per-LLM billing mode. | partial | 10.0 | `99e10001` |
| ROW396 | Fix two doc gaps in the existing docs pass: say the burn-guard target is the ceiling less safety_points, and … | not delivered | 10.0 | `a2f90001` |
| ROW398 | Make every datawatch session on every backend aware it runs on datawatch. | partial | 10.0 ‡ | `6eba0001` |
| ROW410 | Release the capacity lease on a hand kill and show whether a live pi worker is working or stuck. | partial | 10.0 | `af960001`, `99e10001`, `97760001` |
| ROW411 | Stabilise the planning requeue table test and release the lease when a session ends by any route. | partial | 10.0 | `af960001`, `99e10001` |
| ROW413 | Fix the read-only check, and two findings: ignored report files are unreadable by later tasks, and a Claude … | partial | 10.0 | `30060001` |
| ROW419 | Track imap-mcp as a service plugin, including a manifest copy in the community repo. | partial | 10.0 | `a2f90001` |
| ROW420 | Ship the platform context delivery fix; leftovers: migration sweep scope, doc wording, operations entry and a … | partial | 10.0 | `a2f90001` |
| ROW421 | Tag everything; leftover: the web-bundled OpenAPI file lacks the tag sweep route. | partial | 10.0 | `492f0001` |
| ROW426 | Record operator approvals and file six defects: CI watch waits for CI that never runs, task branch not merged … | partial | 10.0 | `e1650001` |
| ROW427 | Finish review and merge by the daemon (Phase 4): approval modes, release size, merge lock, shadow comparison … | partial | 10.0 | `79dd0001` |
| ROW431 | Orphan reaping; follow-up: scope reaper path evidence to owned worktree folders so it cannot end a sibling … | partial | 10.0 | `57010001`, `99e10001` |
| ROW434 | Narrow the CodeQL gate triggers; open: fix the how-to paragraph inserted between a sentence and its table. | partial | 10.0 | `a2f90001` |
| ROW454 | Keep the plans folder tidy automatically by a daily Automation; the schedule and alert still had to be set. | partial | 10.0 | `a2f90001` |
| ROW459 | Release push and tag for Automata under the approval modes; the row says the ship path does not call it and a … | partial | 10.0 | `79dd0001` |
| ROW462 | Ship the Projects tab; not in place: weekly share, branch retention and major trigger have no reader, plan … | partial | 10.0 | `86dc0001` |
| ROW465 | Show each standard task real mode; not in place: a runner column on the Templates sub-tab and a per-project … | partial | 10.0 | `86dc0001` |
| ROW469 | Ship the verifier run cap; open: no live cap-ended run, and two how-to notes await operator confirmation … | partial | 10.0 | `22250001` |
| ROW494 | Use one Trivy pin and a pinned helm install; not covered: helm archive checksum and a version guard for the … | partial | 10.0 | `c1f90001` |
| ROW496 | Reconcile plan files with the registry; three proposed rows await an operator decision. | partial | 10.0 | `a2f90001` |
| ROW501 | Design compute pools; no build until the operator answers nine points. | deferred | 10.0 | `27a90001` |
| ROW524 | Ship plan records; owed: a live check after deploy, and an open slot placement choice. | partial | 10.0 | `9a6f0001` |
| ROW550 | Board of finished Automata whose commits never reached main (52), and stop the loop archiving before landing. | partial | 10.0 | `79100001`, `2baa0001` (note 2) |
| ROW553 | Show each pack workflow as a read-only graph, later editable, after the research note is accepted. | deferred | 10.0 | `9d190001` |
| ROW554 | Have the daemon do the merging: shadow first, exit criteria, then promote patch releases. | partial | 10.0 | `71f80001`, `27210001` |
| ROW560 | File every bug, gap and missing document found while supervising as an Automaton (eleven filed). | partial | 10.0 | `8a3e0001`, `3fa40001`, `c8f50001`, `09c30001`, `13cf0001`, `929d0001`, `e81e0001`, `08170001`, `32100001`, `526f0001`, `64650001` |
| SEC-476 | Make MCP channel bridge registration owner-bound instead of first come (design, BL467). | not delivered | 10.0 | `2fbf0001` |
| SEC-480 | Make dependency-review a required check (BL415). | deferred | post-10.0 | `3f1d0001` |
| SEC-481 | Build and run the open-model 16-cell matrix (BL449). | deferred | post-10.0 | `53ee0001` |
| SEC-503 | Harness research proposals awaiting prioritization (BL457). | deferred | 11.0 | `316c41e1`, `0fb4e302` |
| SEC-504 | openrig adoption plan awaiting prioritization (BL458). | deferred | 11.0 | `36729dd7` |
| SEC-510 | Implement the unaddressed open-LLM research recommendations (BL460). | deferred | 11.0 | `404726a8` |
| SEC-516 | Personal agents with personality and decision-making (BL456). | deferred | 11.0 | `2c9a27e2` |
| SEC-522 | Federation as a sibling-agent mail transport (BL446). | deferred | 11.0 | `b2110001` |
| SEC-523 | Research whether open models can decompose Automata (BL453). | deferred | 11.0 | `b5ce0001` |
| SEC-524 | Research Hermes Agent (BL438). | deferred | 11.0 | `b5ce0001` |
| SEC-525 | Research datawatch as infrastructure for Hermes Agent (BL439). | deferred | 11.0 | `79890001` |
| SEC-526 | Alerts conditions, filtering and SIEM parity (BL400). | deferred | 11.0 | `31280001` |
| SEC-527 | Matrix.org communication channel (BL241). | deferred | 11.0 | `82630001` |
| SEC-528 | Streaming voice input that shows text while speaking (BL651). | deferred | 11.0 | `7e640001` |

## Delivered, superseded, dropped and not a request

### Delivered (548)

BL1, BL2, BL3, BL6, BL13, BL14, BL16, BL17, BL18, BL21, BL22, BL23,
BL24, BL25, BL28, BL29, BL32, BL39, BL49, BL50, BL52, BL57, BL61, BL62,
BL63, BL64, BL65, BL66, BL68, BL71, BL73, BL77, BL81, BL83, BL84, BL85,
BL88, BL89, BL92, BL93, BL94, BL95, BL101, BL109, BL110, BL112, BL114, BL166,
BL172, BL175, BL176, BL177, BL178, BL179, BL181, BL183, BL185, BL187, BL188, BL189,
BL191, BL192, BL193, BL194, BL195, BL196, BL197, BL198, BL199, BL200, BL201, BL202,
BL203, BL204, BL205, BL207, BL208, BL210, BL211, BL212, BL213, BL215, BL216, BL217,
BL222, BL223, BL227, BL228, BL230, BL231, BL232, BL233, BL234, BL235, BL236, BL237,
BL238, BL239, BL240, BL242, BL244, BL245, BL246, BL247, BL248, BL249, BL253, BL254,
BL255, BL261, BL262, BL267, BL268, BL269, BL270, BL271, BL272, BL273, BL275, BL277,
BL279, BL287, BL288, BL290, BL291, BL293, BL294, BL296, BL297, BL298, BL300, BL312,
BL313, BL314, BL315, BL317, BL319, BL321, BL323, BL324, BL326, BL329, BL330, BL336,
BL337, BL338, BL339, BL341, BL342, BL343, BL344, BL346, BL347, BL348, BL349, BL350,
BL351, BL355, BL356, BL357, BL359, BL360, BL361, BL362, BL364, BL365, BL370, BL371,
BL372, BL374, BL375, BL376, BL377, BL379, BL380, BL381, BL383, BL384, BL389, BL391,
BL399, BL401, BL402, BL404, BL406, BL412, BL419, BL420, BL421, BL423, BL425, BL426,
BL427, BL432, BL435, BL441, BL442, BL444, BL445, BL447, BL450, BL451, BL452, BL454,
BL455, BL457, BL458, BL461, BL462, BL463, BL464, BL465, BL466, BL468, BL470, BL474,
BL476, BL477, BL478, BL479, BL480, BL482, BL484, BL485, BL486, BL488, BL489, BL490,
BL491, BL493, BL495, BL497, BL499, BL500, BL501, BL503, BL504, BL505, BL506, BL507,
BL509, BL510, BL511, BL512, BL513, BL514, BL515, BL516, BL517, BL518, BL519, BL520,
BL521, BL522, BL523, BL524, BL525, BL526, BL527, BL529, BL530, BL531, BL532, BL533,
BL534, BL535, BL536, BL537, BL538, BL539, BL542, BL543, BL544, BL545, BL546, BL547,
BL548, BL550, BL551, BL552, BL553, BL554, BL555, BL556, BL557, BL558, BL559, BL560,
BL561, BL562, BL563, BL564, BL565, BL566, BL567, BL569, BL570, BL571, BL572, BL573,
BL574, BL575, BL576, BL577, BL578, BL579, BL580, BL581, BL583, BL584, BL586, BL587,
BL588, BL589, BL590, BL591, BL594, BL595, BL596, BL597, BL599, BL600, BL601, BL602,
BL603, BL604, BL605, BL606, BL607, BL608, BL611, BL612, BL613, BL614, BL615, BL616,
BL617, BL618, BL619, BL621, BL622, BL623, BL624, BL625, BL626, BL627, BL630, BL631,
BL632, BL633, BL634, BL635, BL638, BL639, BL640, BL641, BL643, BL645, BL646, BL647,
BL648, BL650, F1, F2, F3, F5, F8, F9, F10, F12, F14, F16,
F17, F18, F19, B4, B6, B8, B9, B10, B12, B13, B14, B15,
B16, B17, B18, B19, B20, B21, B23, B24, B25, B26, B27, B30,
B31, B32, B34, B35, B36, B37, B38, B39, B44, B45, B46, B47,
B48, B49, B50, B51, B52, B53, B54, B56, B57, B58, B59, B60,
B61, B62, B63, B64, B65, B66, B67, B68, B69, B70, B71, B72,
B73, B74, B75, B76, B77, B78, B79, B80, B81, B82, B83, B84,
B85, B86, B87, B88, B89, B90, B91, B92, B93, B94, B96, B97,
B98, B99, B101, B102, B103, B104, B105, B106, B108, B109, B110, B112,
B113, B114, B116, B117, B118, B119, B120, B124, B125, B126, B127, B128,
B129, B130, ROW13, ROW15, ROW21, ROW55, ROW77, ROW198, ROW240, ROW248, ROW250, ROW255,
ROW256, ROW258, ROW261, ROW262, ROW265, ROW266, ROW267, ROW269, ROW270, ROW272, ROW273, ROW274,
ROW275, ROW276, ROW278, ROW279, ROW282, ROW283, ROW284, ROW300, ROW305, ROW336, ROW338, ROW341,
ROW350, ROW363, ROW366, ROW367, ROW371, ROW372, ROW373, ROW375, ROW376, ROW377, ROW385, ROW387,
ROW388, ROW394, ROW395, ROW399, ROW401, ROW405, ROW407, ROW416, ROW418, ROW428, ROW447, ROW541,
ROW542, ROW543, SEC-474, SEC-475, SEC-489, SEC-490, SEC-491, SEC-492

### Superseded (10)

BL41, BL86, BL117, BL182, BL214, BL250, BL629, B11, B33, B55

### Dropped (4)

BL19, BL38, BL206, F13

### Not a request (198)

ROW65, ROW239, ROW271, ROW277, ROW280, ROW281, ROW285, ROW286, ROW287, ROW288, ROW289, ROW290,
ROW291, ROW292, ROW294, ROW298, ROW301, ROW302, ROW303, ROW304, ROW308, ROW309, ROW310, ROW312,
ROW314, ROW315, ROW316, ROW317, ROW318, ROW319, ROW320, ROW321, ROW322, ROW323, ROW324, ROW325,
ROW326, ROW327, ROW329, ROW330, ROW332, ROW334, ROW337, ROW342, ROW343, ROW344, ROW345, ROW351,
ROW352, ROW355, ROW356, ROW357, ROW358, ROW359, ROW361, ROW365, ROW368, ROW369, ROW370, ROW378,
ROW381, ROW384, ROW386, ROW389, ROW393, ROW397, ROW400, ROW402, ROW403, ROW404, ROW406, ROW408,
ROW409, ROW415, ROW417, ROW422, ROW423, ROW424, ROW425, ROW429, ROW430, ROW432, ROW433, ROW435,
ROW436, ROW437, ROW438, ROW439, ROW440, ROW441, ROW442, ROW443, ROW444, ROW445, ROW446, ROW448,
ROW449, ROW450, ROW451, ROW452, ROW453, ROW455, ROW456, ROW457, ROW458, ROW460, ROW461, ROW463,
ROW464, ROW466, ROW467, ROW468, ROW470, ROW471, ROW472, ROW473, ROW474, ROW475, ROW476, ROW477,
ROW478, ROW479, ROW480, ROW481, ROW482, ROW483, ROW484, ROW485, ROW486, ROW487, ROW488, ROW489,
ROW490, ROW491, ROW492, ROW493, ROW495, ROW497, ROW498, ROW499, ROW500, ROW502, ROW503, ROW504,
ROW505, ROW506, ROW507, ROW508, ROW509, ROW510, ROW511, ROW512, ROW513, ROW514, ROW515, ROW516,
ROW517, ROW518, ROW519, ROW520, ROW521, ROW522, ROW523, ROW525, ROW526, ROW527, ROW528, ROW529,
ROW530, ROW531, ROW532, ROW533, ROW534, ROW535, ROW536, ROW537, ROW538, ROW539, ROW540, ROW544,
ROW545, ROW546, ROW547, ROW548, ROW549, ROW551, ROW552, ROW555, ROW556, ROW557, ROW558, ROW559,
SEC-472, SEC-487, SEC-501, SEC-508, SEC-514, SEC-520

## Parity surface

This record changes documentation only, so every surface is excluded for the same reason: no behaviour, setting or interface changes.

| Surface | Included | Reason |
|---|---|---|
| REST | No | documentation only |
| MCP | No | documentation only |
| CLI | No | documentation only |
| comm channel | No | documentation only |
| YAML/config | No | documentation only |
| PWA | No | documentation only; `docs/plans/` is not in the embedded docs viewer |
| Android | No | documentation only |
| iPhone | No | documentation only |

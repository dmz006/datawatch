# How-to Guides

Practical walkthroughs for the most common datawatch workflows. Each
guide follows the same structure:

A. Intent of howto is to be detailed. Provide instructions, guide thorugh all available channel options (web, pwa, api, mcp, cli, etc).
B. Provide enough details that any channel being used can follow through configuration, get it working, and use it for it's function.
C. Remember this will be used by both people and other llm through all datawatch channels.
D. Include simple diagrams if neded or links to details architecture or other diagrams.
F. Remember howto are public docs, follow rules for public docs (ie no BL, etc).

1. **Base requirements** — what you need installed/configured first.
2. **Setup** — minimum steps to get the feature working.
3. **Walkthrough** — a real example from start to finish, with the
   commands you run and the output you should see.
4. **ALL Options** - use all channels: cli, api, mcp, pwa, comms channels, etc...

If you're just trying to get datawatch running for the first time,
start at [`docs/setup.md`](../setup.md). The how-to guides assume
you already have a daemon you can talk to.

## Index

| Guide | What it covers |
|-------|----------------|
| [Setup + install](setup-and-install.md) | First-time install end-to-end: binary, daemon start, config, smoke-test, where logs land |
| [Chat + LLM quickstart](chat-and-llm-quickstart.md) | Most-common chat channel + LLM backend pairings (signal/telegram + claude/ollama) |
| [Comm channels](comm-channels.md) | All 11 messaging backends — Signal, Telegram, Discord, Slack, Matrix, Ntfy, Email, Twilio, GitHub/Generic webhooks, DNS |
| [MCP tools](mcp-tools.md) | Wire datawatch into Claude Code / Cursor / any MCP host; the live tool surface |
| [Voice input](voice-input.md) | Transcription backends (whisper / openai / openai_compat / openwebui / ollama) and chat-channel voice notes |
| [Autonomous planning](autonomous-planning.md) | Submit a free-form spec, watch it decompose into stories + tasks, run them with verification |
| [Autonomous review + approve](autonomous-review-approve.md) | The Automaton lifecycle gate: review the decomposition, approve / reject / request-revision before run |
| [Automata lifecycle subscriptions](automaton-lifecycle-subscriptions.md) | Register "when this Automaton event fires → deliver it there" — comm / session / topic / alert / mcp_queue sinks, one-shot watch-and-inject, TTL, coalesce, test-fire, PWA Subscriptions tab, polling queue for generic MCP clients |
| [Automata lifecycle webhooks and peer forwarding](automaton-lifecycle-webhooks.md) | Send lifecycle events out of the daemon: signed HTTPS webhooks with a host allow-list and vault-held signing secret, and forwarding to a federation peer. Both ship disabled; covers enabling on every surface, the receiver's verification, retries, audit records, SSRF rules and troubleshooting |
| [Operator question notifications](operator-questions-notifications.md) | Operator question channel: turning it on (and the three notification toggles), the WebSocket message on raise, the messaging-group line and the high-priority alert, subscribing to `question.answered` / `question.withdrawn` on every surface, polling `GET /api/questions/{id}` for state + answer summary, and the opt-in single `DATAWATCH_ANSWER:` session line |
| [Worker questions and blocking tasks](operator-questions-workers.md) | A worker session asks the operator a design question with a one-line `DATAWATCH_QUESTION:` marker: turning on the channel and `autonomous.questions.worker_marker`, the marker's fields and caps, when a worker should block and when it should keep working, how a blocked task waits (`await_decision`), how the coordinator tick reads and resumes it, answering from the web UI, CLI, REST, MCP and chat, and what is not covered yet |
| [Operator decisions on an Automaton](automaton-operator-decisions.md) | What an operator decision is, how one is raised on the question channel, reading the decision line on REST, MCP, the CLI, chat and the web UI, how the gate holds a task whose spec names an open decision and what the waiting reason says, shadow mode against on, and the two config keys with exact `PUT /api/config` bodies |
| [Operator question answering](operator-questions-answering.md) | Answer an open operator question from every surface — the PWA Automation-tab queue (search, answer sheet with options + your words, history with the answer log), the CLI `question` verbs, the MCP question tools, the chat channel (plus the bare-reply rule), and REST; the immutable record (supersede, never edit), questions that never expire, the secret-looking-answer refusal with its two safe routes, and withdrawing an open question |
| [Autonomous recovery](autonomous-recovery.md) | Recover a stuck/failed task, a cancelled-and-never-restarted story, or a completed Automaton with a skipped dependency; repair_depends_on for pre-v8.36.5 Automata |
| [Container image refresh](container-image-refresh.md) | Daily pipeline that rebuilds, scans and promotes the published container images so OS/base fixes reach released versions |
| [Automata capacity](automata-capacity.md) | Wait for free sessions / node / LLM slots instead of failing; fair queue, priority, reserved interactive slots |
| [Automata progress](automata-progress.md) | How far along the Automata are and roughly when the rest will be done: counts by kind, rates per active day, known versus estimated work left, a completion range, the daily trend; set an Automaton kind, backlog markers, the waits file |
| [Automata tags](automata-tags.md) | Organise Automata in a nested tag tree: assign several tags, browse with an include-sub-tags switch, bulk tag; deletes of used tags are refused |
| [Automata DAG orchestrator](automata-orchestrator.md) | Compose multiple Automata into a graph with guardrails (rules, security, release-readiness, docs integrity) |
| [Automation queue advance](automation-queue-advance.md) | Chain Automatons in an Automation: activate, automatic advance on completion, blocked/idle alerts, restart behaviour |
| [Lane fill](automation-lane-fill.md) | Keep GPU and Claude lanes busy: shadow mode first, reading the status on every surface, lane preferences, the day-long shadow comparison, switching to on, the idle alert, restart behaviour |
| [Automaton worktree cleanup](automaton-worktree-cleanup.md) | Removing finished Automaton branches, worktrees and daemon temp files: the safety checks, off / report / on, retention and the per-pass limit, the disk alert, the status on every surface, and moving from report to on |
| [Automation view](automation-tab.md) | The Automation view, the first sub-tab of Automata: list and filters, pipeline strip (branches, forks, joins, nested), create and edit form, pause / resume / cancel / skip / insert / remove controls |
| [Automation decision assistant](automation-decision-assistant.md) | The optional local-model decision assistant: enable per Automation or member, shadow mode first, hook points, thresholds, what it can never do, escalation |
| [List endpoints + paging](list-endpoints-and-paging.md) | Why list endpoints are small: `view=summary`, `limit`/`cursor` paging, ETag / 304, gzip, and the web UI's loading / cached / stale / failed states |
| [How the Automata tab loads](automata-tab-loading.md) | The paged 50-row summary, Load more, loading / cached / stale / failed states, opening a row for the full record, children, what a reconnect does, and troubleshooting a slow tab over a phone or Tailscale link |
| [Guardrail Library](guardrail-library.md) | Named guardrail registry, profiles, per-Automaton overrides, skill-declared guardrails, scan unification |
| [Retry Policy](retry-policy.md) | Failure categories (infra/content/scope/policy), bounded auto-retry budgets, content auto-fix, retry profiles |
| [Failure escalation](failure-escalation.md) | A failing task moves up a worker ladder, is held for review at a failure count, and is retried, changed or cancelled by the operator; override per Automaton and task |
| [Judge routing and time limit](judge-policy.md) | The judge role (defaults to the verifier routing policy), its three modes, running a judge on a free Claude lane, the 30-minute time limit, one attempt per backend, the timeout warnings and the needs-review hold with its attempt summary |
| [File Drift Guardrail](file-drift-guardrail.md) | Foreign-file drift for direct-mode Automata: a task whose commit swept in someone else's pre-existing uncommitted edit **blocks by default** (`autonomous.drift_mode: block`; set `warn` to flag-only) — the task's own in-run edits only ever warn — worktree mode skipped |
| [CI Watch Guardrail](ci-watch-guardrail.md) | Opt-in completion guardrail: wait for the pushed commit's GitHub Actions runs; on failure an LLM judge applies your project's rules (operator decision / auto-fix / CI re-run) |
| [Project + Cluster Profiles](profiles.md) | Operator walkthrough for both profile sets — REST/MCP/CLI/comm CRUD, the unified Automaton profile dropdown, common multi-cluster patterns, troubleshooting |
| [Container workers](container-workers.md) | Configure project + cluster profiles, spawn ephemeral container workers, monitor + collect attestations |
| [Pipeline + session chaining](pipeline-chaining.md) | Chain tasks into DAG pipelines with before/after gates; combine with manual sessions |
| [Cross-agent memory](cross-agent-memory.md) | Use episodic memory + the knowledge graph across builds, tests, and federated peers |
| [Federated observer](federated-observer.md) | Multi-host stats + envelope tree across primary + Shape A/B/C peers |
| [Daemon operations](daemon-operations.md) | Day-two operator workflow: start / stop / restart / upgrade / diagnose / reload / logs |
| [Testing against a running daemon](testing-against-a-running-daemon.md) | Point the functional-smoke and E2E test scripts at a daemon you already have running (CI-deployed, sandbox, dev) instead of a fresh throwaway one — `DW_BASE` / `TEST_BASE` reference |
| [Navigation at phone widths](navigation-mobile-fit.md) | The phone-width check of the PWA bottom bar: what it measures, `make test-e2e-shared-nav`, where the report goes, how to read a finding, and what the bar shows now (ellipsis labels, full names on hover, 40 px minimum tap targets) |
| [Sessions deep-dive](sessions-deep-dive.md) | Session anatomy, lifecycle, daemon-restart resume path, debugging when state goes wrong |
| [Claude Code hooks](claude-hooks.md) | Auto-install, manual setup, structured hook payload schema, TodoWrite integration |
| [Session Telemetry](session-telemetry.md) | Structured task telemetry from hook events: tasks[], guardrail verdicts, persist-on-stop |
| [Dashboard](dashboard.md) | /dashboard Mission Control: session constellation, EKG waveform, sprint pipeline, expand mode |
| [Channel state engine](channel-state-engine.md) | Why a session is in its current state; signals + diagnostic walkthrough |
| [Identity + Telos](identity-and-telos.md) | One-time operator self-description; injected into every session's wake-up L0 layer. Also covers the automatic identity backups, the empty-write refusal, Reset, and restoring an earlier identity from the PWA, CLI, MCP, REST or chat |
| [Algorithm Mode](algorithm-mode.md) | 7-phase structured-thinking harness (Observe → Improve) with per-phase capture |
| [Evals](evals.md) | Rubric-based grading suites: capability + regression modes, 4 grader types |
| [Council Mode](council-mode.md) | Multi-persona structured debate; 12 default personas; quick + debate modes |
| [Compute Nodes](compute-nodes.md) | ComputeNode registry — kinds (ollama/openai-compat), capacity, scheduling priority, observer peer, model pull, CLI + PWA + REST + MCP + comm |
| [Ollama Marketplace](ollama-marketplace.md) | Browse, pull, and delete Ollama models on a Compute Node from the PWA, CLI, REST, or MCP — no manual `ollama pull` needed |
| [Secrets Manager](secrets-manager.md) | Native + KeePass + 1Password backends; `${secret:name}` references; per-secret scopes |
| [LLM Registry](llm-registry.md) | Named LLM definitions, per-node model pairs, enabled/disable toggle, block-on-delete + reassign, all surfaces |
| [Tailscale Mesh](tailscale-mesh.md) | Headscale + commercial Tailscale; agent worker mesh sidecar; ACL generator |
| [Push notifications](push-notifications.md) | ntfy-compatible SSE endpoint + UnifiedPush discovery; self-hosted; mobile alerts for session events |
| [Push setup — registration API](push-setup.md) | Register/unregister/notify endpoints; Android UnifiedPush integration; CLI `datawatch push` |
| [Async PRD decompose](decompose-async.md) | Async decompose: 202 Accepted + SSE story stream + polling fallback + Last-Event-ID resume |
| [Alert Rules](alert-rules.md) | Create and manage threshold-based alert rules; firings log; PWA + Android + CLI/REST/MCP |
| [Result Panel](result-panel.md) | Scheduled jobs post structured results (status, metrics, tables, history); Status tab panel + Integration status dashboard card; CLI / REST / MCP / comm read surfaces |
| [Native ACME / Let's Encrypt](letsencrypt-acme.md) | Daemon obtains + auto-renews its own browser-trusted TLS cert; HTTP-01, staging-first, no DNS management; PWA + CLI/REST/MCP/comm |
| [Alerts + Notifications](alerts-and-notifications.md) | Alert dock (header badge + slide-out panel), Alerts tab, CLI / REST / MCP / comm surfaces, mobile push |
| [Channel Routing](channel-routing.md) | Route inbound channel messages (Telegram, Signal, …) to federation peers via pattern rules; owner_peer attribution on sessions + PRDs |
| [File Service](file-service.md) | Upload, list, and delete files across federation peers; peers/ and discussions/ subdirs; config:read/write caps |
| [Discussion Scopes](discussion-scopes.md) | Per-discussion memory namespaces with WAL, federated sync, conflict detection, and 60 ops/min throttle |

> **Looking for something else?** [`docs/setup.md`](../setup.md) has
> first-time install. [`docs/api/`](../api/) has REST/MCP/CLI
> reference. [`docs/architecture-overview.md`](../architecture-overview.md)
> explains how the pieces fit together. [`docs/flow/`](../flow/) has
> sequence + flowchart diagrams.

## Conventions

- Every command shown is the operator-canonical form (full flags, no
  abbreviations).
- Output blocks show what you should see — `…` marks elided lines,
  `<vars>` mark substituted values.
- Where a feature is reachable through multiple channels (REST / MCP
  / CLI / chat), each guide picks one and notes the others at the
  bottom.

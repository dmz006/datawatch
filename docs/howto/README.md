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
| [Plan coverage findings filters](plan-coverage-filters.md) | When the coverage gate reports noise: the two on-by-default filters (created-file findings, scoped requirements), the four settings, the suppressed counts on every read surface, and the acknowledgement that still covers the genuine findings |
| [Generated artefacts](generated-artefacts.md) | What an Automaton's change record holds and the five drafts generated from it (gate report, changelog entry, parity issue, tracker rows, registry row): formats, tracked paths, editing without being overwritten, template overrides, the seven settings, the combined documentation task, review, and adopting the scheme in a new repository |
| [Decompose lint, standard tasks and guidance](automaton-decompose-lint.md) | The quality check over a fresh decomposition: the nine-rule built-in pack (six `block`, three `warn`), `off` / `shadow` / `enforce` and the retry budget, operator rules and disables, the six standard tasks (model-session mode) with slots and a per-project switch, the guidance precedence, the stored record and the dry run, and reading config + findings + the standard-task list on REST, MCP, CLI, chat and the PWA |
| [Standard tasks](automata-standard-tasks.md) | What a standard task is and the six built-in ones the decomposer appends to a story, turning them on or off globally and per project with `autonomous.task_templates.enabled` and `autonomous.task_templates.projects`, the mode setting (not yet available), telling a standard task apart from an Automaton template, and reading the list on YAML, CLI, the Templates tab, REST, MCP and the comm channel |
| [Automata lifecycle subscriptions](automaton-lifecycle-subscriptions.md) | Register "when this Automaton event fires → deliver it there" — comm / session / topic / alert / mcp_queue sinks, one-shot watch-and-inject, TTL, coalesce, test-fire, PWA Subscriptions tab, polling queue for generic MCP clients, and the opt-in supervisor event kinds (stuck running, merge ready, all tasks done, capacity wait, burn-guard rung, judge verdicts, final gate) that wake an event-driven supervisor |
| [The event-driven supervisor](automaton-event-driven-supervisor.md) | Wake the supervisor on events instead of polling: the four `supervisor_loop.wake.*` settings and the fallback check, choosing kinds in the picker, subscribing to a peer, adding an event kind or sink through the extension registration, MCP resource subscriptions (`mcp.resource_subscriptions`), what is and is not forwarded across a peer, and every setting on the config file, REST, MCP, CLI, messaging channel and web settings |
| [Automata lifecycle webhooks and peer forwarding](automaton-lifecycle-webhooks.md) | Send lifecycle events out of the daemon: signed HTTPS webhooks with a host allow-list and vault-held signing secret, and forwarding to a federation peer. Both ship disabled; covers enabling on every surface, the receiver's verification, retries, audit records, SSRF rules and troubleshooting |
| [Operator question notifications](operator-questions-notifications.md) | Operator question channel: turning it on (and the three notification toggles), the WebSocket message on raise, the messaging-group line and the high-priority alert, subscribing to `question.answered` / `question.withdrawn` on every surface, polling `GET /api/questions/{id}` for state + answer summary, and the opt-in single `DATAWATCH_ANSWER:` session line |
| [Worker questions and blocking tasks](operator-questions-workers.md) | A worker session asks the operator a design question with a one-line `DATAWATCH_QUESTION:` marker: turning on the channel and `autonomous.questions.worker_marker`, the marker's fields and caps, when a worker should block and when it should keep working, how a blocked task waits (`await_decision`), how the coordinator tick reads and resumes it, answering from the web UI, CLI, REST, MCP and chat, and what is not covered yet |
| [Operator decisions on an Automaton](automaton-operator-decisions.md) | What an operator decision is, how one is raised on the question channel, reading the decision line on REST, MCP, the CLI, chat and the web UI, how the gate holds a task whose spec names an open decision and what the waiting reason says, shadow mode against on, and the two config keys with exact `PUT /api/config` bodies |
| [Decision records](automaton-decision-records.md) | What a decision record is and why the daemon builds the queue question from it, creating one from REST, MCP, the CLI, chat or a file in the watched folder, the configurable file layout, the clarity rules, edits and withdrawals, answers, the document status line rewrite, follow-ups and every `autonomous.decisions.*` key |
| [Operator question answering](operator-questions-answering.md) | Answer an open operator question from every surface — the PWA Automation-tab queue (search, answer sheet with options + your words, history with the answer log), the CLI `question` verbs, the MCP question tools, the chat channel (plus the bare-reply rule), and REST; the immutable record (supersede, never edit), questions that never expire, the secret-looking-answer refusal with its two safe routes, and withdrawing an open question |
| [Held-state questions](automaton-held-state-questions.md) | The daemon raises its own operator question when an Automaton sits in a held state — the five held states and their options (hold is always the default), off / shadow / on, the two config keys, reading the report on REST, MCP, CLI, chat and the PWA card, answering from any surface, auto-withdrawal and stale-answer refusal, the shadow dogfood before switching to on |
| [Autonomous recovery](autonomous-recovery.md) | Recover a stuck/failed task, a cancelled-and-never-restarted story, or a completed Automaton with a skipped dependency; repair_depends_on for pre-v8.36.5 Automata |
| [Autonomous snapshot staging](autonomous.md) | The scoped automatic commits the daemon makes for task work, the `autonomous.snapshot_never_stage` patterns (and their built-in defaults) that keep paths out of staging, and the task's `skipped_snapshot_paths` skip record (path + reason) |
| [The shipped status](automata-shipped-status.md) | The shipped state for Automata and Automations — which is independent of the status: record it (CLI, REST, MCP, comm), the shipped bucket in the counts and lists, the shipped dashboard card, backfill from the merge history, and the five autonomous.shipped settings |
| [Container image refresh](container-image-refresh.md) | Daily pipeline that rebuilds, scans and promotes the published container images so OS/base fixes reach released versions |
| [CodeQL local scan](codeql-local-scan.md) | Run the CodeQL default suite on your machine without uploading, compare with GitHub alerts, check bundle pin drift, optional pre-push and pre-ship steps |
| [Automata defect queue](automata-defect-queue.md) | Group related defects into one batch Automaton (one story per defect, shared gate, documentation, changelog and parity-or-locale task), run tiny test/docs/script fixes in a one-task lane that refuses operator-visible changes, read the intake decision log, split a failed story out, and every `autonomous.defect_queue.*` setting with the REST, MCP, CLI and comm commands |
| [Automata capacity](automata-capacity.md) | Wait for free sessions / node / LLM slots instead of failing; fair queue, priority, reserved interactive slots |
| [Automata completion review auto-fix](automata-completion-review-autofix.md) | What happens when the completion review blocks: fix task created and run, or held in needs_review with the findings; where to see it, how to fix it, and `autonomous.auto_fix_retries` |
| [Autonomous priority by kind](autonomous-priority-by-kind.md) | Where an Automaton lines up in the capacity queue: the shipped kind → priority table and its defaults, explicit pins and `priority_source` (explicit / kind / default), setting and clearing the table on CLI, REST, MCP, the web app and the comm channel, and the defect intake Automation that new kind-bug filings are offered to |
| [Automata progress](automata-progress.md) | How far along the Automata are and roughly when the rest will be done: counts by kind, rates per active day, known versus estimated work left, a completion range, the daily trend; set an Automaton kind, backlog markers, the waits file |
| [Automata tags](automata-tags.md) | Organise Automata in a nested tag tree: assign several tags, browse with an include-sub-tags switch, bulk tag; deletes of used tags are refused |
| [Automata DAG orchestrator](automata-orchestrator.md) | Compose multiple Automata into a graph with guardrails (rules, security, release-readiness, docs integrity) |
| [Automation queue advance](automation-queue-advance.md) | Chain Automatons in an Automation: activate, automatic advance on completion, blocked/idle alerts, restart behaviour |
| [Lane fill](automation-lane-fill.md) | Keep GPU and Claude lanes busy: shadow mode first, reading the status on every surface, lane preferences, the day-long shadow comparison, switching to on, the idle alert, restart behaviour |
| [Automaton worktree cleanup](automaton-worktree-cleanup.md) | Removing finished Automaton branches, worktrees and daemon temp files: the safety checks, off / report / on, retention and the per-pass limit, the disk alert, the status on every surface, and moving from report to on |
| [Automaton uncommitted-work check](automaton-uncommitted-work.md) | The two uncommitted-work checks (per task, and before an Automaton is reported finished), the hold / warn / off policies and the task_files / worktree scopes, the commit / discard / retry answers and what each never touches, the daemon's own unedited generated documents not counting at the finished check, files outside a task's declared files, and every surface where it shows |
| [Automation view](automation-tab.md) | The Automation view, the first sub-tab of Automata: list and filters, pipeline strip (branches, forks, joins, nested), create and edit form, pause / resume / cancel / skip / insert / remove controls |
| [Automation decision assistant](automation-decision-assistant.md) | The optional local-model decision assistant: enable per Automation or member, shadow mode first, hook points, thresholds, what it can never do, escalation |
| [List endpoints + paging](list-endpoints-and-paging.md) | Why list endpoints are small: `view=summary`, `limit`/`cursor` paging, ETag / 304, gzip, and the web UI's loading / cached / stale / failed states |
| [Host load check](host-load-check.md) | The once-per-minute load-average watch: the sustained-breach episode and its one alert naming the busiest process groups, the off / alert / reap modes with defaults, reading an alert, what reap may and may not end and what an operator still reviews, and every surface that sets it |
| [Orphan reaping and host load](automata-orphan-reaping.md) | Session-end reaping of the processes a session left behind (evidence by /proc, never by name, survivors counted, session_end_reap audit entry and task decision), the host_load_alert modes off / alert / reap with multiple and minutes defaults, the bounded load step rule, and reading it from every surface |
| [How the Automata tab loads](automata-tab-loading.md) | The paged 50-row summary, Load more, loading / cached / stale / failed states, opening a row for the full record, children, what a reconnect does, and troubleshooting a slow tab over a phone or Tailscale link |
| [Guardrail Library](guardrail-library.md) | Named guardrail registry, profiles, per-Automaton overrides, skill-declared guardrails, scan unification |
| [Retry Policy](retry-policy.md) | Failure categories (infra/content/scope/policy), bounded auto-retry budgets, content auto-fix, retry profiles |
| [Projects tab](projects-tab.md) | Pick a project and read its page: arcs with priority and state, the Automata and sessions under each arc, progress and cost in points, Gantt, history, effective policy and events; set the record (owner, target, lane defaults, arcs, weekly share, retention) from REST, MCP, the CLI and the comm channel |
| [Project automation policy](project-automation-policy.md) | The per-project tier between an Automaton and the daemon: the settings in each group, what stays daemon-wide, clearing a key back to inheritance, and setting and reading it from the panel, YAML, CLI, MCP, REST and comm |
| [Infrastructure retry](infrastructure-retry.md) | A task that fails because the machinery around it failed (full temp quota, worker killed before delivery, daemon restart) is retried with back-off and not counted as a failure; held with the cause when the retries run out; dependents recover on their own |
| [Merge pipeline](merge-pipeline.md) | Define a project's merge pipeline in a repository file or as a daemon record, see which one wins and why, choose the conflict policy and model, who approves a release before it ships, and how a model's proposal is tested before it is used |
| [Merge shadow](merge-shadow.md) | The `autonomous.merge_shadow` settings that power the shadow merge plan: what they enable, their defaults and validation, every way to set and read them, and where the shadow state is read (REST, MCP, CLI, comm channel, PWA card — all read-only) |
| [Release size and milestones](release-size-and-milestones.md) | Set how far each merge moves the version: the release size by Automaton kind, the major release trigger (manual, milestone or never), the release cadence and the version scheme, per project in Settings > Automation > Merge pipeline |
| [Cost report](cost-report.md) | See what the work cost per Automaton, Automation, backend and week from the data already kept, read the settings, and open it from the web app, CLI, MCP, REST or chat. |
| [Automata automatic tags](automata-auto-tags.md) | The automation, stage and kind tags every Automaton keeps up to date by itself: the templates and kind labels with their defaults, off daemon-wide or for one Automation, renaming a stage so the tag follows, why the system tags cannot be renamed, and filtering progress and the session list by them |
| [Onboarding report](onboarding-report.md) | Read what is configured, what is missing and what to try next from the PWA, CLI, MCP or chat; what each check means; how to switch checks off. Part of the tutorial and onboarding material |
| [Major release requirements](major-release-requirements.md) | The operator checklist for the upcoming major release: the minimum Ollama version per capability and the `autonomous.requirements.min_ollama.<capability>` override, the minimum backends rule, model-pull consent with the publisher allow-list, and reading the requirements report in onboarding, the CLI and the web |
| [Parallel tasks](automaton-parallel-tasks.md) | Run the tasks of one Automaton at the same time: flag a task read only, a worktree per task, the merge conflict policy, and reading the merge state and the speedup |
| [Model request progress](model-request-progress.md) | Is a worker or verifier request hung or slow? The node's running-models signal and optional GPU activity, the four states and the summary wording, the alert and non-blocking question at the threshold (the choice is only recorded), every key and surface, and the limits |
| [Model routing](model-routing.md) | Read which models each compute node serves on every surface, the pin-time refusal when no node serves a pinned model (or a warning, in warn mode), pinning a task or story to a node, the verifier following its worker's node, and the six `autonomous.model_routing` keys |
| [Failure escalation](failure-escalation.md) | A failing task moves up a worker ladder, is held for review at a failure count, and is retried, changed or cancelled by the operator; the per-kind first-attempt worker, retry resume (a failed attempt's retry may continue the previous session with the failure summary), and their cost keys; override per Automaton and task |
| [Worker session profiles](worker-session-profiles.md) | Worker and verifier sessions will start small (with the sessions' own start work), with context and tools chosen for the job: the four built-in profiles, the per-role defaults, giving one task the tools it needs, deterministic starting context and the measured starting context in the cost report and stats, and what the operator reviews |
| [Judge routing and time limit](judge-policy.md) | The judge role (defaults to the verifier routing policy), its three modes, running a judge on a free Claude lane, the 30-minute time limit, one attempt per backend, the timeout warnings and the needs-review hold with its attempt summary |
| [Verifier run cap](automaton-verifier-run-cap.md) | The total wall-clock limit on one verifier session (`autonomous.verifier_max_run_seconds`, default 2700 s, `0` = off), how it differs from the no-progress timeout, where to set it, and its current enforcement status |
| [Quality gate scope](quality-gate-scope.md) | Steer how much of the test suite a gate task runs: `full` / `touched-plus-smoke` / `touched`, per-Automaton override, the smoke set, `scope_plan`, final-gate and fallback rules |
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
| [Operator console](dashboard-operator-console.md) | The dashboard's one-glance “what needs my attention” summary — questions waiting, held & blocked, lanes, usage, CI & release; every switch, every surface (YAML, CLI, PWA, REST, MCP, chat), and what the console never does |
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
| [Secret scan push gate](secret-scan-push-gate.md) | Pre-push secret scan of about-to-be-pushed commits: off/shadow (default)/enforce, detector + allow-list config, what a refusal looks like, where findings are recorded, clearing a false positive; secrets are never printed |
| [Release push and tag](release-push-and-tag.md) | The off-by-default unattended release publication: the three settings with defaults, the secret-scan refusal rule, the audit entry, the CI window, and the read surfaces |
| [Agent tracing](agent-tracing.md) | What agents did: tool calls, model requests, state changes, capability and permission decisions, loop warnings and errors — modes (off/summary/full), the four `tracing.*` settings, coverage level per backend, reading the timeline and roll-ups, redaction before storage, retention, every channel (PWA, CLI, REST, MCP, chat); OpenTelemetry export is a follow-up |
| [Async PRD decompose](decompose-async.md) | Async decompose: 202 Accepted + SSE story stream + polling fallback + Last-Event-ID resume |
| [Alert Rules](alert-rules.md) | Create and manage threshold-based alert rules; firings log; PWA + Android + CLI/REST/MCP |
| [Result Panel](result-panel.md) | Scheduled jobs post structured results (status, metrics, tables, history); Status tab panel + Integration status dashboard card; CLI / REST / MCP / comm read surfaces |
| [Native ACME / Let's Encrypt](letsencrypt-acme.md) | Daemon obtains + auto-renews its own browser-trusted TLS cert; HTTP-01, staging-first, no DNS management; PWA + CLI/REST/MCP/comm |
| [Alerts + Notifications](alerts-and-notifications.md) | Alert dock (header badge + slide-out panel), Alerts tab (meta row with the round `?` help link), CLI / REST / MCP / comm surfaces, mobile push |
| [Channel Routing](channel-routing.md) | Route inbound channel messages (Telegram, Signal, …) to federation peers via pattern rules; owner_peer attribution on sessions + PRDs |
| [File Service](file-service.md) | Upload, list, and delete files across federation peers; peers/ and discussions/ subdirs; config:read/write caps |
| [Self-config capability](self-config-capability.md) | What the builtin `self-config` group is, how `mcp.allow_self_config` maps onto it, the enabled default, the documented hardening (close the gate, remove it from the session default, a narrower custom group, read the audit, grant one session the group), the never-gated audit view from every surface, and bootstrap protection |
| [Discussion Scopes](discussion-scopes.md) | Per-discussion memory namespaces with WAL, federated sync, conflict detection, and 60 ops/min throttle |
| [Classification Verdicts](classification-verdicts.md) | Judge verification by option probabilities: enable classify mode, allowlist a publisher, probe and calibrate, read the route and score vector, failure behaviour |
| [Plugin services (imap-mcp)](imap-mcp-service.md) | Run imap-mcp standalone, attach datawatch or let it manage the process, mint and rotate the service and push tokens, read the status card, act on each of the four events, and the troubleshooting table |
| [Plans folder tidy](plans-folder-tidy.md) | The folder rules for `docs/plans/` (what stays, what moves, the age cutoff, references, the filed-only rule, unclassified files), the daily Automation run, its alert, run stamp and daily report, the 24-hour release smoke window, and every setting: three environment variables, the `tidy-plans.sh` flags and the Automation import file |

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

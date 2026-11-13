# datawatch Documentation

Complete reference for configuring, operating, and extending datawatch.

---

## Getting Started

| Document | Description |
|---|---|
| [setup.md](setup.md) | Full installation and initial configuration guide |
| [commands.md](commands.md) | Complete command reference (messaging and CLI) |
| [pwa-setup.md](pwa-setup.md) | Progressive Web App setup via Tailscale |
| [howto/](howto/README.md) | Practical walkthroughs — autonomous planning, PRD-DAG, container workers, pipeline chaining, cross-agent memory, Automata lifecycle subscriptions (incl. PWA tab and polling queue), and the outbound webhook and peer-forwarding sinks (`howto/automaton-lifecycle-webhooks.md`, disabled until enabled), operator question notifications and the return path (`howto/operator-questions-notifications.md`), answering an open operator question from every surface (`howto/operator-questions-answering.md`), and operator decisions and the decision gate (`howto/automaton-operator-decisions.md`), decision records (`howto/automaton-decision-records.md`), and worker questions and blocking tasks (`howto/operator-questions-workers.md`) |

## Backends

| Document | Description |
|---|---|
| [backends.md](backends.md) | Overview of all LLM and messaging backends |
| [llm-backends.md](llm-backends.md) | Detailed guide for every LLM backend (claude-code, aider, goose, gemini, opencode, ollama, openwebui, shell) |
| [messaging-backends.md](messaging-backends.md) | Detailed guide for every messaging backend (Signal, Telegram, Matrix, Discord, Slack, Twilio, ntfy, email, GitHub webhook, generic webhook) |
| [backend-state.md](backend-state.md) | How each backend family's session state is decided — what counts as working, when waiting_input vs stalled is shown, the default idle timeout per family, and the liveness window and idle-timeout config keys |

## Interfaces

| Document | Description |
|---|---|
| [mcp.md](mcp.md) | MCP (Model Context Protocol) server — Cursor, Claude Desktop, VS Code, remote AI agents |
| [cursor-mcp.md](cursor-mcp.md) | Quick-start for Cursor and Claude Desktop MCP integration |
| [pwa-setup.md](pwa-setup.md) | Browser PWA interface over Tailscale |
| [howto/generated-artefacts.md](howto/generated-artefacts.md) | Generated artefacts: the change record an Automaton keeps and the gate report, changelog draft, parity draft, tracker rows and registry row drawn from it, the seven switches, template overrides, edit protection, reading and regenerating from every surface |
| [howto/mcp-prompts.md](howto/mcp-prompts.md) | 10 MCP prompt slash commands — live context injection for every operator workflow (v7.1.0) |
| [howto/automata-completion-review-autofix.md](howto/automata-completion-review-autofix.md) | Completion review auto-fix: the three outcomes, where the findings show, adding a fix task, and `autonomous.auto_fix_retries` |
| [howto/compute-placement-ask.md](howto/compute-placement-ask.md) | Compute placement ask: the missing-model policy, headroom settings, and approving or denying a placement ask from every surface |
| [howto/automata-progress.md](howto/automata-progress.md) | Automata progress dashboard: counts by kind, rates per active day, known versus estimated work left, completion range, daily trend |
| [howto/projects-tab.md](howto/projects-tab.md) | The Projects sub-tab of the Automata view: pick a project, read its arcs, Automata, progress, history and policy across its page sub-tabs (Overview, Arcs, Gantt, Unassigned, History, Policy, Events), and set its record (arcs, priority, state, target, lane defaults, weekly share, retention) from REST, MCP, the CLI and the comm channel |
| [howto/plan-records.md](howto/plan-records.md) | Plan records: registry rows and findings kept by datawatch, how to name, list, change and import them from REST, MCP, the CLI, the comm channel and the Projects page, and the write-through setting |
| [howto/automata-defect-queue.md](howto/automata-defect-queue.md) | Defect queue: group related defects into one batch Automaton, run tiny test/docs/script fixes in a one-task lane, read the intake decision log, split a failed story out, and every `autonomous.defect_queue.*` setting with the REST, MCP, CLI and comm commands |
| [howto/automata-planning-routing.md](howto/automata-planning-routing.md) | Automata planning routing: rules and first match, match fields, risk classes and path globs, retry ladders and escalation, shadow mode and would-route counts, the scorecard and counts-only gate report, the example rules (not active), access from every surface |
| [howto/automata-task-routing.md](howto/automata-task-routing.md) | Automata task routing: task types and how a task is classified, rows and profiles, what always wins over the table, validation and the premium-model gate, the recorded routing result, switching, dry run, export and import, access from REST, MCP, CLI, chat and the web app |
| [howto/automata-timeline.md](howto/automata-timeline.md) | Automata timeline: the Gantt-style interval chart across all projects — every task as a bar with start and end, blocked and waiting intervals with their reason, the today marker and the projected finish, narrowed by project, Automaton, stage, kind or tag |
| [howto/automata-tags.md](howto/automata-tags.md) | Nested tags for Automata and sessions: the tag tree, subtree counts, browsing with match ANY or ALL, primary tag, the attention group, session tags (own and inherited), settings |
| [howto/automata-tag-registry.md](howto/automata-tag-registry.md) | One tag registry across federated daemons: the keeper, peers, stale copies, pending sync and merge notes |
| [howto/automata-checks-and-ship.md](howto/automata-checks-and-ship.md) | Automata checks and ship steps: the four checks, push, tag and CI watch, the off / shadow / on modes, starting in shadow, reading the status from every surface |
| [howto/automata-rules-and-context.md](howto/automata-rules-and-context.md) | Automata rules and context: rules file, context files, setting up a new repository, per-project overrides, the planning record, the rules digest, the per-task rules checklist and its gap finding, troubleshooting |
| [howto/automation-queue-advance.md](howto/automation-queue-advance.md) | Automation (chained Automatons): activate, automatic advance, idle alert |
| [howto/usage-calculator.md](howto/usage-calculator.md) | Usage calculator: within or over budget for the week, what each lane count costs, the three policies, typed readings, federation, the three PWA variants |
| [howto/classification-verdicts.md](howto/classification-verdicts.md) | Classification verdicts: classify mode versus generate mode, the publisher allowlist, probe and calibration status, reading a verdict, low-confidence review, fail-closed behaviour, the `classifier.*` tunables |
| [flow/classification-verdict-flow.md](flow/classification-verdict-flow.md) | Classify-mode verdict flow: allowlist gate, calibration, probe, abstain rules, low-confidence hold |
| [howto/burn-guard.md](howto/burn-guard.md) | Burn guard: the ladder of admission changes when the weekly burn runs over the pace line, its settings, reading an alert, stepping back, the effort ceiling, the Claude retry limit |
| [howto/host-load-check.md](howto/host-load-check.md) | Host load check: the once-per-minute load-average watch, the sustained-breach episode and its one alert naming the busiest process groups, the off / alert / reap modes with defaults, reading an alert, what reap may and may not end, and every surface that sets it |
| [howto/automata-orphan-reaping.md](howto/automata-orphan-reaping.md) | Orphan reaping and host load: session-end reaping of processes a session left behind (evidence by /proc, never by name), the host_load_alert modes off / alert / reap with multiple and minutes, the bounded load step rule, and reading it from every surface |
| [howto/cost-report.md](howto/cost-report.md) | Cost report: spend per Automaton, Automation, backend and week from the existing data, its settings, and reading it from the web app, CLI, MCP, REST and chat |
| [howto/automata-auto-tags.md](howto/automata-auto-tags.md) | The automatic tags: the automation, stage and kind tags the daemon keeps on every Automaton, the templates and kind labels, off daemon-wide or per Automation, the stage rename that moves the tag, the system-tag lock, and the tag filters on progress and the session list |
| [howto/dashboard-operator-console.md](howto/dashboard-operator-console.md) | Operator console: the dashboard's one-glance “what needs my attention” summary (questions waiting, held & blocked, lanes, usage, CI & release), its seven switches with defaults, and every read and set surface |
| [howto/automation-lane-fill.md](howto/automation-lane-fill.md) | Lane fill: keeping GPU and Claude lanes busy, the shadow mode comparison, lane preferences, the idle alert and restart behaviour |
| [howto/lane-utilization.md](howto/lane-utilization.md) | Lane utilization: busy and idle share per lane, queue wait, backend mix, the idle-while-eligible flag, lane fill mode and plan usage in points and tokens, and reading it from REST, MCP, CLI, chat, the dashboard card and the Gantt view |
| [howto/automation-requirements.md](howto/automation-requirements.md) | Automation requirements: the Ollama minimums against every node, the execution/planning/verification backends, the missing lines, node backend versions, and every read surface |
| [howto/model-pull-consent.md](howto/model-pull-consent.md) | Model pull consent: the complete-policy set (models with allowed/note, publishers, the default-deny rule), every surface, and the pitfalls |
| [howto/automation-repo-ci-watch.md](howto/automation-repo-ci-watch.md) | Repo CI watch: the head of `main` and the newest release tags across every workflow, the classifications, the once-per-episode alert, the budgeted infra rerun / hung cancel / superseded deletion, why security runs are record-only, the shadow-mode comparison, all read surfaces |
| [howto/automation-security-failure.md](howto/automation-security-failure.md) | Security failure report: what the stop means, shadow and on modes and the config key, reading the report on REST, MCP, CLI, chat and the PWA card, the four recommendations, and accepting a risk through the registry (the daemon never writes a suppression) |
| [howto/automation-supervisor-watch.md](howto/automation-supervisor-watch.md) | Supervisor watch: the fourteen findings in plain words, shadow mode first, the comparison with the supervisor script and the one-day agreement before retiring it, the alert time, the triage note, all read surfaces |
| [howto/supervisor-loop.md](howto/supervisor-loop.md) | Supervisor loop: what each mechanical step and model job does, the settings and defaults, all five ways to change them, where to see jobs and runs, reading a run record, tuning a job's model, style and retries, and running the model eval |
| [howto/automaton-event-driven-supervisor.md](howto/automaton-event-driven-supervisor.md) | Event-driven supervisor: wake settings and the fallback check, the event picker, subscribing to a peer, extension registration of event kinds and sinks, MCP resource subscriptions, what crosses a peer, and each setting on every surface |
| [howto/automaton-search-before-create.md](howto/automaton-search-before-create.md) | Search before create: the overlap warning, how to read each kind, the off / shadow / on modes, comparing shadow records with what the supervisor did, the threshold, and the five ways to read warnings |
| [howto/plan-coverage-filters.md](howto/plan-coverage-filters.md) | Plan coverage findings filters: the two on-by-default filters (files the plan creates, scope/constraint-scoped requirements), the four settings with defaults, the suppressed counts and the filters pinned into every stored record, the acknowledgement that still covers the genuine findings, and how to tell a suppression from a real gap |
| [howto/automaton-decompose-lint.md](howto/automaton-decompose-lint.md) | Decompose lint and the standard tasks the decomposer adds to each story: the ten-rule built-in pack (six block, four warn), `off` / `shadow` / `enforce` and the retry budget, operator rules and disables, the six standard tasks with slots and a per-project switch, guidance precedence, and reading the config, the findings and the standard-task list on REST, MCP, CLI, chat and the PWA |
| [howto/automata-standard-tasks.md](howto/automata-standard-tasks.md) | Standard tasks: what the decomposer appends to a story, the six built-in ones, the global and per-project switches under `autonomous.task_templates`, the per-standard-task mode setting (`autonomous.task_templates.mode`: `llm`, `daemon` or `off`), how a standard task differs from an Automaton template, and reading the list on every surface |
| [howto/automaton-daemon-task-modes.md](howto/automaton-daemon-task-modes.md) | Daemon task modes: the llm / daemon / off mode per standard task and which templates the daemon can run, the script gate time cap, the project gate command, and what a refused write looks like on the Settings page, over MCP and on REST |
| [howto/self-config-capability.md](howto/self-config-capability.md) | Self-config capability: what the builtin `self-config` group is, the `mcp.allow_self_config` alias and its three effects, the enabled default with bootstrap protection, the documented hardening (close the gate, remove it from the session-default, a narrower custom group, read the audit, one granted daemon), the audit file with refused attempts recorded, and the setting on every surface, plus the separate `mcp.allow_sensitive_writes` switch for the sensitive write tool class |
| [howto/automaton-worktree-cleanup.md](howto/automaton-worktree-cleanup.md) | Automaton worktree cleanup: what the worktrees and temp-file passes remove and what they never touch, the off / report / on modes, retention and the per-pass limit, the disk alert, reading the status on every surface, and moving from report to on |
| [howto/automaton-uncommitted-work.md](howto/automaton-uncommitted-work.md) | Automaton uncommitted-work check: why a task is complete only when its work is committed, what happens to each task and before an Automaton is reported finished, the hold / warn / off policies and the task_files / worktree scopes, the commit / discard / retry answers and what each never touches, the daemon's own unedited generated documents no longer counting at the finished check, files outside a task's declared files, reading the state on every surface, and a worked example |
| [howto/automaton-branch-refresh.md](howto/automaton-branch-refresh.md) | Automaton branch refresh: the between-task merge of the base branch into an Automaton branch, the two `autonomous.branch_refresh_*` settings and the per-Automaton override, when a refresh runs, what a conflict does and the three answers to its question, resetting a stopped Automaton, and reading the state and the numbers on every surface |
| [howto/automaton-pending-items.md](howto/automaton-pending-items.md) | Automaton pending items: work that has no Automaton yet and how an Automation holds at it, adding an item and putting it in an Automation, attaching an Automaton so the member converts in place and the Automation resumes, the hold and skip policies and auto-resume, reading the state on every surface, an optional file registry read from a table you keep (id pattern, columns, done words, rows that disappear), and a worked example |
| [howto/automation-tab.md](howto/automation-tab.md) | Automation tab: list, pipeline strip, create and edit form, member controls |
| [howto/automata-tab-loading.md](howto/automata-tab-loading.md) | Automata tab loading: paged summary, Load more, loading / cached / stale / failed states, reconnect |
| [howto/automata-planning-failures.md](howto/automata-planning-failures.md) | Planning failures: the spec check on create and revise (what it flags and why, on REST, MCP, CLI, chat and the web form), what the daemon rewords by itself and what stays a failure, where a planning failure is shown, the one alert and the Fix-the-wording question, the planning housekeeping group and its modes, and troubleshooting |
| [howto/automata-events.md](howto/automata-events.md) | The Events tab: the one stream of watch, decisions, retries, checks, cleanup and errors — what an error entry and a quiet entry mean, the next action for a common error, filtering, search and paging on every surface, and where the moved cards went |
| [howto/automation-tab.md](howto/automation-tab.md) | Automation view (first sub-tab of Automata): list, pipeline strip, create and edit form, member controls |
| [howto/infrastructure-retry.md](howto/infrastructure-retry.md) | Infrastructure retry: the four transient failure classes, back-off, the held task and its question, dependents that recover, every key and surface |
| [howto/merge-pipeline.md](howto/merge-pipeline.md) | Merge pipeline: a file in the repository or a daemon record, which wins, conflict policy and model, who approves a release before it ships, how a model proposal is tested before use, and the shadow merge plan (Phase 4: the daemon plans the merge and records it, never applies it; after the merge lands the shadow comparison grades each difference and records a defect candidate per difference) |
| [howto/merge-shadow.md](howto/merge-shadow.md) | Merge shadow: what the settings enable (the finish-time plan, the throwaway trial worktree, `applied` stuck at `false`), the defaults and their validation, every way the three `autonomous.merge_shadow.*` keys are set, where the shadow state is read (REST route, MCP tool, CLI, comm verb, the PWA card — all read-only) and what you still review |
| [howto/release-size-and-milestones.md](howto/release-size-and-milestones.md) | Release size and milestones: the four layers that decide how far a merge moves the version, the breaking-change flag, the milestone Automation, the kind map and major trigger, cadence and version scheme, a worked story from an idea to a major release, and every surface that sets them |
| [howto/release-push-and-tag.md](howto/release-push-and-tag.md) | Release push and tag: the off-by-default unattended release publication (push the release commit, then the tag), `autonomous.push_and_tag` / `autonomous.release_path` / `git.release_ci_window_minutes` with defaults, the enforce-mode secret-scan refusal and its credential-source-label-only audit entry, and the three read surfaces (PWA Release section, `datawatch release status`, the `release_push_status` MCP tool) |
| [howto/project-automation-policy.md](howto/project-automation-policy.md) | Project automation policy: the per-project tier between an Automaton and the daemon, the settings in each group, what stays daemon-wide, clearing a key back to inheritance, and every set and read surface |
| [howto/onboarding-report.md](howto/onboarding-report.md) | Onboarding report: what is configured, what is missing, what to try next, on every surface; the checks and how to switch them off |
| [howto/automaton-parallel-tasks.md](howto/automaton-parallel-tasks.md) | Parallel tasks in one Automaton: read-only tasks, a worktree per task, merging back in order, the merge conflict policy, the speedup numbers |
| [howto/model-request-progress.md](howto/model-request-progress.md) | Model request progress: telling a hung model request from a slow one, the four states, the alert and non-blocking question at the threshold, every key and surface, the limits |
| [howto/model-routing.md](howto/model-routing.md) | Model routing: which models each compute node serves, the pin-time refusal or warning, pinning a task or story to a node, the verifier following its worker, every key and surface |
| [howto/automaton-verifier-run-cap.md](howto/automaton-verifier-run-cap.md) | The verifier run cap: the total wall-clock limit on one verifier session (`autonomous.verifier_max_run_seconds`, default 2700 s, `0` = off), how it differs from the no-progress timeout, every read and set surface, and how it is enforced and retried |
| [howto/automaton-protected-paths.md](howto/automaton-protected-paths.md) | Automaton protected paths: the paths an Automaton's tasks must not change without your say-so, the hold / warn / block / off actions and per-Automaton authorised paths, the approve / reject / change-the-spec question and the new-commit restore on reject, the rules judge reading the rules at the base commit, the rules the daemon never rewrites for you, where the settings live, and reading the state on every surface |
| [howto/failed-attempt-cost.md](howto/failed-attempt-cost.md) | Failed-attempt cost: what a failed attempt costs and why, the first-attempt worker by task kind, escalating after n failures, resuming a retry in the same session or starting fresh, the no-retry holds, reading the attempt record, and the attempt-cost settings on every surface |
| [howto/failure-escalation.md](howto/failure-escalation.md) | Failure escalation: worker ladder, failure count, the review-needed hold, the five operator actions and the override order; the per-kind first-attempt worker, retry resume (a failed attempt's retry may continue the previous session with the failure summary), and their cost keys |
| [howto/worker-session-profiles.md](howto/worker-session-profiles.md) | Worker session profiles: worker and verifier sessions will start small (with the sessions' own start work), with context and tools chosen for the job, the four built-in profiles, the per-role defaults, giving one task the tools it needs, deterministic starting context and the measured starting context, what the operator reviews |
| [howto/ci-code-scanning-upload.md](howto/ci-code-scanning-upload.md) | CI Code Scanning upload: why the SARIF report upload never blocks a release, where the blocking checks live, the retry schedule, rules for new uploads, local checks |
| [howto/compute-nodes.md](howto/compute-nodes.md) | Compute nodes: add and configure a node, slots, draining, certificates, and raising verifiers above one with the measured headroom check (the measurement on every surface, the idle-only and pull-allowed-model rules, the refusal messages, the shared age and free-memory settings) |
| [howto/codeql-local-scan.md](howto/codeql-local-scan.md) | CodeQL local scan: run the default suite offline for all four languages, compare with GitHub alerts and dismissals, check bundle pin drift, optional pre-push and pre-ship steps |
| [howto/multi-servers.md](howto/multi-servers.md) | Multi-server PWA — register remote instances, header server dropdown (selecting a host re-renders the current view in place; the chosen entry is remembered per browser and restored on reload), all-servers aggregation (v7.2.0) |
| [howto/imap-mcp-service.md](howto/imap-mcp-service.md) | Plugin services end-to-end with imap-mcp: run standalone, attach or let the daemon manage, mint the service token, rotation, the status card, acting on each event, troubleshooting |
| [howto/vision-image-descriptions.md](howto/vision-image-descriptions.md) | Vision image descriptions: what the vision block does on every surface, enabling it, the per-describe timeout and the @path fallback when a description times out, the trailing-space rule that lets a last-tag fallback go out with a single Enter, setting the timeout from YAML, the web app, REST, MCP, comm and CLI, and choosing a value |
| [api/openapi.yaml](api/openapi.yaml) | OpenAPI 3.0 REST API specification |

## Memory & Intelligence

| Document | Description |
|---|---|
| [memory.md](memory.md) | Episodic memory — architecture, flow diagrams, configuration, MCP tools, REST API, monitoring, encryption |
| [memory-usage-guide.md](memory-usage-guide.md) | Practical examples — saving, searching, KG, Claude integration, OpenWebUI chat, PostgreSQL, API reference |

## Security

| Document | Description |
|---|---|
| [encryption.md](encryption.md) | Encryption at rest — what's encrypted, formats, memory content encryption, export command, env variable |

## Operations

| Document | Description |
|---|---|
| [operations.md](operations.md) | Day-to-day operations, monitoring, and maintenance |
| [multi-session.md](multi-session.md) | Running datawatch across multiple machines |
| [session-tracking.md](session-tracking.md) | Session lifecycle and state machine |
| [rtk-integration.md](rtk-integration.md) | RTK token savings integration — setup, config, stats, supported backends |
| [channel-testing.md](channel-testing.md) | MCP channel testing guide — manual test procedures for bidirectional flow |
| [config-reference.yaml](config-reference.yaml) | Complete annotated config file reference (all fields, defaults, comments) |
| [addons.md](addons.md) | Optional add-ons and integrations |
| [plugins.md](plugins.md) | Plugin services: attach_only vs managed, the manifest, widget kinds and data modes, tokens and rotation, the four events and their “Next” actions, thresholds, and the surfaces | 
| [uninstall.md](uninstall.md) | Manual uninstall instructions for all installation methods |
| [testing-tracker.md](testing-tracker.md) | Interface validation status for all backends |
| [testing.md](testing.md) | Bug test procedures and results log (28 tests, all PASS) |

## Reference

| Document | Description |
|---|---|
| [architecture-overview.md](architecture-overview.md) | Top-level one-screen Mermaid map of every interface, subsystem and data path (incl. planned features) |
| [architecture.md](architecture.md) | System architecture and component diagram |
| [data-flow.md](data-flow.md) | Message and data flow diagrams |
| [agents.md](agents.md) | F10 ephemeral container-spawned agents — REST/MCP/CLI/comm reference, spawn flow, security notes, helm chart pointer |
| [registry-and-secrets.md](registry-and-secrets.md) | Operator setup — point datawatch at *your* registry, K8s cluster, GitHub/GitLab account, and TLS material; covers config knobs across every channel + audit recipes |
| [profiles.md](profiles.md) | Project + Cluster Profile schema, validation, smoke checks (F10 sprints 2-4) |
| [composition-examples.md](composition-examples.md) | Concrete Project + Cluster Profile compositions — agent/lang/tools image pairings, real example configs |
| [container-build.md](container-build.md) | Building the agent-* / lang-* / tools-* images (Dockerfile + Makefile + harbor push flow) |
| [test-coverage.md](test-coverage.md) | Per-package test counts, integration smoke scripts, known thinly-covered areas |
| [plan-attribution.md](plan-attribution.md) | Attribution + comparison to inspiring projects (mempalace, nightwire) — direct features, divergences, what was built because they inspired it |
| [app-flow.md](app-flow.md) | Application state machine |
| [design.md](design.md) | Design goals and philosophy |
| [implementation.md](implementation.md) | Implementation notes and internals |
| [planning.md](planning.md) | Roadmap and feature planning |
| [backends.md](backends.md) | Backend configuration reference table |
| [claude-channel.md](claude-channel.md) | MCP channel server for Claude Code — per-session channels, auto-retry |
| [covert-channels.md](covert-channels.md) | Research notes on DNS tunneling and alternative low-profile channels |
| [future-native-signal.md](future-native-signal.md) | libsignal native integration plan (replacing signal-cli/Java) |
| [plans/README.md](plans/README.md) | **Project tracker** — bugs, plans, backlog, completed items (single source of truth) |
| [plans/](plans/) | Implementation plans (one file per release/feature, YYYY-MM-DD-slug.md) |

## Flow Diagrams

| Document | Description |
|---|---|
| [data-flow.md](data-flow.md) | **Index** — links to all flow diagrams below |
| [flow/system-data-flow.md](flow/system-data-flow.md) | Top-level component interaction (Signal, daemon, tmux, PWA) |
| [flow/new-session-flow.md](flow/new-session-flow.md) | New session sequence (`new:` → tmux → running) |
| [flow/input-required-flow.md](flow/input-required-flow.md) | Input detection → prompt alert → user reply → resume |
| [flow/multi-machine-flow.md](flow/multi-machine-flow.md) | Two hosts sharing one Signal group |
| [flow/multi-source-flow.md](flow/multi-source-flow.md) | Multiple messaging backends + proxy mode variant |
| [flow/websocket-flow.md](flow/websocket-flow.md) | WS connect, subscribe, output push, keepalive |
| [flow/signal-flow.md](flow/signal-flow.md) | Signal message flow (Phone → signal-cli → router) |
| [flow/signal-rpc-flow.md](flow/signal-rpc-flow.md) | signal-cli JSON-RPC protocol detail |
| [flow/persistence-flow.md](flow/persistence-flow.md) | Config, sessions.json, log file write patterns |
| [flow/dns-channel-flow.md](flow/dns-channel-flow.md) | DNS TXT query encoding, HMAC auth, response fragmentation |
| [flow/proxy-flow.md](flow/proxy-flow.md) | Proxy mode — remote routing, session discovery, command forwarding |
| [flow/agent-spawn-flow.md](flow/agent-spawn-flow.md) | F10 agent lifecycle — profile → spawn → bootstrap → clone → terminate → revoke → sweep |
| [flow/lifecycle-subscription-flow.md](flow/lifecycle-subscription-flow.md) | Automata lifecycle event → dispatcher → subscription engine → comm / session (bridge, SendInput) / topic / alert / pending-events queue (`mcp_queue`) sinks; update + test-fire; outbound webhook and peer forwarding paths (both ship disabled) |
| [flow/tag-registry-sync-flow.md](flow/tag-registry-sync-flow.md) | Shared tag registry across federated daemons: keeper and peer roles, the pull loop, forwarded writes, stale copies, pending sync, merge and removed-on-keeper notes |
| [flow/automaton-create-overlap-flow.md](flow/automaton-create-overlap-flow.md) | Create-time overlap check — create, store, search, then the reply (`on`) or the audit record (`shadow`) |
| [flow/automation-triage-flow.md](flow/automation-triage-flow.md) | Poll-time triage pass — fill/refresh/clear the stuck Automaton's triage record, one alert per episode, and the read on every surface |
| [flow/automation-merge-pending-flow.md](flow/automation-merge-pending-flow.md) | Poll-time merge-pending check — the cache keyed by branch and main tips, the git cherry and subject check, the state decision, and the split between shadow recording and the on-mode alert |

---

## Quick Links

- **Install:** `curl -fsSL https://raw.githubusercontent.com/dmz006/datawatch/main/install/install.sh | bash`
- **Source:** [github.com/dmz006/datawatch](https://github.com/dmz006/datawatch)
- **Issues:** [github.com/dmz006/datawatch/issues](https://github.com/dmz006/datawatch/issues)
- **Live endpoints** (when daemon is running):
  - Swagger UI: `http://<host>:8080/api/docs`
  - OpenAPI spec: `http://<host>:8080/api/openapi.yaml`
  - MCP Tools (HTML): `http://<host>:8080/api/mcp/docs`
  - MCP Tools (JSON): `http://<host>:8080/api/mcp/docs` (Accept: application/json)
  - Ollama models: `http://<host>:8080/api/ollama/models`
  - OpenWebUI models: `http://<host>:8080/api/openwebui/models`
  - Health check: `http://<host>:8080/api/health`
  - Liveness probe: `http://<host>:8080/healthz`
  - Readiness probe: `http://<host>:8080/readyz`
  - Prometheus metrics: `http://<host>:8080/metrics`

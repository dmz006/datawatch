# Operator decision channel: proposal

Date: 2026-10-25
Status: in progress. All open points below were decided by the operator on 2026-10-25. Step 1 (config switch, question store, audit actions, REST, config accessibility) is implemented in this branch with its tests, the operator docs, the curl walkthrough, the PWA settings toggle and the locale key in all five bundles; Steps 2-5 follow the order in this document. The mobile surfaces are tracked externally in datawatch-app#283 (filed 2026-10-25, verified open 2026-10-27 — title: "feat(questions): operator question channel — queue view, answer sheet + settings toggle").

Confirmed: yes

Confirmation, 2026-10-25 about 19:55, operator, quoted: "Yes, confirm it and file Step 1". Scope of the confirmation: Step 1 only (store and REST first). Steps 2 to 5 are filed one at a time, and Steps 4 and 5 (worker marker, guardrail text, Decision Interview Protocol) are raised with the operator again before they are filed. Mobile app issues are not filed without the operator's word.

## 1. Request and why now

The operator asked for this in the section titled "Operator decision channel: questions, decisions and design through datawatch (requested 2026-10-25)". That section is on the main branch and is not in this worktree's copy of the automation orchestration design document, because this branch predates it. The text below is the source of truth for this proposal. Nothing was merged, rebased or checked out from main.

The operator's request, verbatim:

> It would be good if supervisor communicated with datawatch in json or something and questions and decisions and design were through datawatch interface and not in claude terminal, stored up things, track input and quotations, add to design

Why now: on 2026-10-24 a blocking multiple-choice question in the supervisor's terminal stopped the 30-minute ticks for 16 hours. The ticks only run while the session is idle, so the lanes sat idle overnight. Decisions given in a terminal are also lost to everything else. They are copied by hand into plan documents, the operator's exact words are paraphrased, and nothing links an answer to the PRD, task or proposal that asked for it.

Requirements, as the request states them (each is addressed in sections 3 to 5):

1. A structured question, raised as data rather than terminal text.
2. A question queue held by the daemon and shown in the Automation view, the mobile apps, the CLI, the comm channels, REST and MCP, with notifications.
3. Answers recorded with the operator's own words, immutable, with a changed mind written as a new answer.
4. A return path that gives the asker the answer as data, so a session that asked does not need to stay idle.
5. Decisions linked to what they decide, including the proposal file's Confirmed line and the PRD decisions list.
6. Design questions as reviewable records, with the accepted design written back to the design document with the quotation.
7. Search and audit, kept across restarts, with the quotation shown wherever a decision is cited.
8. Parity across every surface in the Configuration Accessibility Rule and the Mobile-Parity Rule.

## 2. Existing mechanisms and reuse

The table lists what the repository already has, verified by a read or a grep in this worktree. To reproduce the marker search:

```
env -u TMUX -u TMUX_TMPDIR git grep -n 'DATAWATCH_NEEDS_INPUT' -- internal
```

| What exists | File path | What it does | What is missing |
|---|---|---|---|
| Needs-input marker detection | internal/session/needs_input_echo.go (classifyNeedsInput); internal/session/manager.go (marker list near line 197, processOutputLine near line 7563) | Recognises a standalone DATAWATCH_NEEDS_INPUT: line in session output and drops echoes of the instruction text | Free text only. No id, options, default, deadline or structured payload. The session is not told whether anyone answered. |
| Worker instruction to print the marker | internal/session/tracker.go (guardrail template near line 438) | Tells a session to print DATAWATCH_NEEDS_INPUT when blocked | Plain text. Nothing tells the worker how an answer reaches it. |
| Needs-input notification | internal/session/manager.go (onNeedsInput, near lines 1004 and 5966); internal/router/router.go (HandleNeedsInput, near line 2117); internal/server/server.go (NotifyNeedsInput, near line 1291); internal/server/ws.go (BroadcastNeedsInput, near line 348) | Sends the prompt to the messaging group with a "Reply with: send <id>" line, and broadcasts a needs_input message on the WebSocket hub | Creates no record. The reply is typed text that is not linked to any question. |
| Needs-input alert context | internal/server/hook_events.go (HookContextForAlert, near line 484) | Adds the last tool and last assistant text to needs-input alert bodies | Context only. |
| PWA needs-input handling | internal/server/web/app.js (case 'needs_input', near line 1353) | Shows a needs-input message in the session view | No question queue, answer form or store. |
| Await-external park | internal/autonomous/await_external.go (AwaitExternalMarker, line 30; sub-reason await_external) | Parks a task on an external condition without counting a retry | Nobody is asked anything, and no answer is recorded. |
| PRD decisions log | internal/autonomous/models.go (Decision type, line 50; Decisions field, about line 260); appended at about twenty sites, for example internal/autonomous/automation_advance.go and internal/autonomous/rejudge.go | Append-only log per PRD with at, kind, actor and a free-form note | No question id, options, verbatim quotation or link to the question that prompted it. The note is paraphrase-prone. Shown as the Decisions log in internal/server/web/app.js near line 13210. |
| Decision assistant | internal/autonomous/decision_assistant.go (DecisionAssistant, shadow and live modes, per-hook thresholds); docs/api-mcp-mapping.md | A local model picks among already-allowed options at four Automation hooks. Shadow mode only records. Below threshold it is meant to escalate to the operator. | It never asks the operator, and there is no queue for it to escalate into. Its shadow-then-live pattern is the precedent for the default-answer open point. |
| Lifecycle events | internal/autonomous/lifecycle_event.go (LifecycleEvent, line 77; optional Decision field) | Typed event for each PRD, task and Automation transition | No question or answer kinds. |
| Pending events queue | internal/server/autonomous_events.go (GET /api/autonomous/events, line 49); internal/mcp/autonomous_events_tools.go; internal/router/autonomous_events.go; cmd/datawatch/cli_autonomous_events.go | Named queue read by cursor. Reports dropped when unseen events were evicted. | Bounded, and not a durable record. This is the candidate return path. |
| Subscriptions | internal/server/autonomous_subscriptions.go (routes at internal/server/server.go lines 424 and 425); internal/mcp/autonomous_subscription_tools.go; internal/router/autonomous_subscriptions.go; internal/router/autonomous_subscription_update.go; cmd/datawatch/cli_autonomous_subscriptions.go | Subscribes a channel or queue to lifecycle kinds, scoped to its owner | Lifecycle kinds only. Question kinds would need adding. |
| Session input delivery | internal/mcp/server.go (send_input near line 1727; reply_to_parent near line 1672); cmd/datawatch/main.go (send command near line 10670) | Sends text into a session | Text only. No structured payload and no record of the answer. |
| Discussion push to sessions | internal/session/discussion_subs.go | Stores subscriptions; per its header, new discussion entries are delivered to subscribed sessions through send_input | Memory-discussion scoped and text only. It shows that a store-backed push into a session already exists. |
| Single-shot ask | internal/mcp/sx_parity.go (toolAsk, near line 155); internal/server/ask.go | One-off question to a local LLM | Not an operator question. The name collides, so do not reuse it. |
| Audit log | internal/audit/log.go (Entry, line 23; Write, Read, Prune, EnableCEFMirror); internal/audit/query.go (ParseQuery); internal/audit/cef.go (FormatCEFLine); internal/audit/events.go (action names); internal/server/audit.go (GET /api/audit) | Append-only JSON-lines audit with a CEF mirror, filtered reads and age-based pruning | No question or answer actions. Details is a free map, so the quotation can be stored but is not indexed. No full-text search. |
| Audit Logging Rule | AGENT.md, "Audit Logging Rule" (line 1251) | Every lifecycle event must emit JSON-lines and CEF, with round-trip and escape tests | Applies to every new question event; the proposal relies on it. |
| Federation capabilities | internal/federation/capabilities.go (CapAutonomousRead, CapAutonomousWrite, CapAutonomousRun, CapAuditRead, lines 104 to 135) | Capability checks on REST through fedCap | No capability for answering. The Federation-Parity Rule requires a fedCap guard on every new write endpoint. |
| Autonomous configuration | internal/config/config.go (AutonomousNotifyConfig, line 2949; EffectivePendingEventsMax, line 2963); cmd/datawatch/cli_autonomous.go (config-get and config-set); MCP tools autonomous_config_get and autonomous_config_set | Autonomous settings reachable through YAML, REST, CLI and MCP | No question settings. |
| Automation view | internal/server/web/app.js (renderAutomationView near line 25479; refreshAutomationView near line 26492; renderAutomationStrip near line 26474; Automata sub-tab routing near line 2412) | Automation list, detail and strip inside the Automata tab | No question queue, badge or answer form. The Automation code lives in internal/server/web/app.js; the app-automation-*.js files are tests only. |
| Locale bundles | internal/server/web/locales/en.json, internal/server/web/locales/de.json, internal/server/web/locales/es.json, internal/server/web/locales/fr.json, internal/server/web/locales/ja.json | Every user-facing string is keyed | New keys would be needed for every question surface. |
| Mobile apps | None in this repository. A listing of tracked files finds no Android, iOS, Kotlin or Swift source. | The Mobile App Sync Rule and the Localization Rule point at the external datawatch-app repository. | Nothing to reuse here. The native clients were not inspected. |
| Parity and access-method docs | docs/parity-status.md (PWA, Android and iOS table); docs/operations.md; docs/setup.md; docs/mcp.md; docs/api-mcp-mapping.md | Per-feature parity and access-method tables | No rows for questions. |
| Supervisor rules document | docs/plans/2026-10-17-supervisor-rules-and-lessons.md | Operator and supervisor rules | Not read for this proposal. Step 5 needs a check for terminal-question rules. |
| Registry Confirmed convention | docs/plans/README.md (registry rows gate later work on a "Confirmed: yes" line) | Hand-set gate | No code reads it. A search of internal and cmd finds no reader. The line is set by hand and links to nothing. |
| Decision Making and DIP | AGENT.md, "Decision Making" (line 1641) and "Decision Interview Protocol (DIP)" (line 1658) | Terminal-based interview: context, options, recommendation, question, wait | Terminal only. No queue, and answers are copied into AGENT.md by hand. |

Where nothing exists at all: a question record, an answer record, a question queue, verbatim answer storage, any question or answer REST, MCP, CLI, comm or PWA surface, expiry and default handling, supersession, search over questions, question and answer audit actions, question kinds in subscriptions, a design-question record, any writer for the Confirmed line, and any mobile code in this repository.

## 3. Data model

Names in this section are internal. Any user-facing copy says Automaton or Automata, never PRD.

### Question record

Fields:
- id: stable, time-sortable, never reused.
- kind: decision or design.
- asked_by: the asker's identity, and asker_kind (session, supervisor, operator or system).
- session_id: optional, the session that raised the question.
- context: one of prd, task, proposal, registry_row or none, plus the matching links below.
- prd_id, task_id, proposal_path, registry_row_key: links. Proposal paths are repository-relative. The registry row key must survive renumbering, because row numbers are display numbers.
- text: the question as the asker wrote it.
- options: two or more entries, each with id, label and tradeoffs.
- recommendation: option_id and a one-sentence rationale.
- default: required. Either an option_id, or hold, meaning the asker takes no action. It may never be empty, so no choice is ever silent.
- deadline_at: optional.
- design (design kind only): references to mockups or documents, as repository-relative paths or artifact links, and the accepted option.
- risk: low or normal, used only by the open point on defaults for low-risk questions.
- created_at and state.

States:
- open: waiting for an answer.
- answered: an answer record exists.
- withdrawn: the asker or the operator withdrew it.
- expired: the deadline passed with no answer.

Transitions: open to answered, open to withdrawn, open to expired. Answered, withdrawn and expired are terminal. A changed answer is a new answer record, never a return to open.

### Answer record

Fields:
- id and question_id, plus created_at.
- actor: the operator's identity, or system when a default is applied (only if the open point on defaults allows it).
- surface: pwa, android, ios, cli, rest, mcp, system, or comm with the channel name.
- chosen_option_id: an option id, or empty for free text only.
- quote: the operator's free text, stored exactly as received. No trimming and no normalisation.
- quote_sha256: a hash of the bytes in quote, so later copies can be checked.
- supersedes: the id of the answer this one replaces, or empty.
- source_message_id: the channel's own id for the message, for comm answers.

States: recorded and superseded. An answer is immutable. Superseded is derived: an answer is superseded when a later answer names it in supersedes. The answer file never changes after it is written.

### Links and reverse index

Every answer links to its question, and the question links to its context. The daemon keeps a reverse index, so a PRD, task, proposal file or registry row can list its questions and answers. The PRD decisions list gets a Decision whose kind is operator_answer and whose note holds the answer id; a typed answer_id field is preferable to parsing the note, and it is a small addition to the Decision type.

### Quotation storage

The quote is stored once, in the answer record. Every citation, including the design write-back and the PRD decisions list, renders the stored quote and the answer id. No one retypes the quote, so the verbatim rule holds from entry to citation.

### Storage and persistence

Recommendation: an append-only JSON-lines file in the daemon data directory, with the current view rebuilt by replay on start. The audit log already uses this shape, and the discussion subscriptions use a JSON file, so it needs no new storage technology. Every raise, answer, withdrawal and expiry also writes an audit entry, so the audit export carries the full trail.

## 4. Smallest sequence of PRD-sized steps

Each step lists what it builds, what it depends on, and its parity surface. Section 5 gives the full parity table, and every excluded surface names its reason there.

### Step 1: store and REST first

Builds:
- The question and answer types and the append-only store described in section 3, with the state rules and validation: at least two options, a required default, an answer that names an option or gives text (or both), and no answer to a question that is not open.
- Audit actions question_raised, question_answered, question_withdrawn, question_expired and answer_superseded, each with a CEF signature and the round-trip and CEF tests that the Audit Logging Rule requires.
- REST: POST /api/questions to raise, GET /api/questions to list with filters on state, context, asker and text, GET /api/questions/{id}, POST /api/questions/{id}/answer, POST /api/questions/{id}/withdraw, and GET /api/questions/{id}/answers.
- One feature switch in configuration, default off, round-tripped through PUT and GET /api/config as the Configuration Accessibility Rule requires.
- fedCap guards: reads use CapAutonomousRead and writes use CapAutonomousWrite. A separate answering capability is an open point.

Depends on: nothing.

Likely new files, none of which exist yet and all created by this step: internal/autonomous/questions.go, internal/autonomous/questions_store.go, internal/server/questions.go.

Parity surface: config and REST now. MCP, CLI, comm, PWA and mobile are deferred to Step 3 with the reason recorded in section 5.

### Step 2: notifications and the return path

Builds:
- On raise: a WebSocket message beside needs_input, a messaging-group notification sent through the existing router send path with the question id and numbered options, and an alert through the existing alert path for questions marked high priority.
- On answer, withdrawal or expiry: lifecycle events question.answered, question.withdrawn and question.expired, delivered through the pending events queue and to subscribers. Subscriptions accept the new kinds.
- A polled single-question GET for askers that cannot subscribe.
- An optional JSON message into the asking session, through send_input, when the question asks for it. It is one line: a fixed marker, DATAWATCH_ANSWER:, followed by JSON. It follows the existing marker convention.
- Raising never blocks. The raise call returns at once, so the asker can carry on with other work and the ticks keep running.

Depends on: Step 1.

Parity surface: config (notification toggles), REST (events and single-question GET), MCP and CLI and comm (subscriptions accept the new kinds, with no new command), WebSocket message type (consumed by the PWA in Step 3), and push through the existing device registration. Client code follows in Step 3.

### Step 2 disagreements with the decision items

This section lists every place Step 2's text differs from the decision items in `docs/plans/2026-10-14-automation-orchestration-design.md` (the Operator decision channel section, items 1 to 24). Where they differ, the decisions win and Step 2 must be read and built as corrected here.

1. **`question.expired` is NOT built (items 2 and 4).** Items 2 and 4 are explicit: questions never expire by a clock, and item 4 closes the earlier open point about a deadline and its expiry behaviour outright. Step 2's "On answer, withdrawal or expiry: … question.expired" is therefore wrong: there is no `question.expired` event, no expiry transition into the expired state, and no deadline-driven state change. A question stays open until it is answered, withdrawn or skipped. Step 1's `question_expired` audit action is corrected by the same decision.
2. **The return path default is event plus polled GET, with the session line opt-in per question (item 22).** The default is the lifecycle event on the existing event queue plus the polled GET endpoint, and it needs no change to the asker's session. The JSON line into the asking session (`DATAWATCH_ANSWER:` plus one line of JSON) is an opt-in the question takes, for askers that cannot poll, so answers stay out of the terminal unless asked for. Step 2's present ordering inverts this: it presents the polled single-question GET as the fallback and the session push as the general option, instead of the event plus polled GET being the default and the session line being per-question opt-in.
3. **Notifications fire on ask, answer and skip (item 11).** Item 11 says an open question raises a notification through the existing notification channels when it is asked and when it is answered or skipped, for every open question, with no repeated messages and no expiry text. Step 2 instead alerts only "questions marked high priority" at ask time and says nothing about answering or skipping. "Skip" is also missing from Step 2's event list entirely: it covers answer, withdrawal and expiry, where the decisions provide a skip flow (items 2, 5a, 10 and 11). A skip applies to the one question it is given on, never to the whole Automaton, and wide skipping is available only when that option is enabled in Automation (off by default); the record then shows the answer as a supervisor decision made on the operator's skip, with its reasoning.
4. **Two new fields on the record: `priority` and `return_path`.** Step 2 refers to "questions marked high priority" and to the session push being opt-in "when the question asks for it", but section 3's record names neither. The question record gains a `priority` field (Step 2's alert rule needs a settable level; item 8's per-question level is one the asker may raise and never lower) and a `return_path` field (item 22's per-question opt-in for the session line). Both are recorded on the question so both behaviours are data on the record, not implicit.
5. **The chat reply selection verbs are Step 3, and decided (item 23).** Item 23 closes the reply rule that Step 3 still points at as the open point on comm replies: each notification carries the question id and numbered options; a bare reply is accepted only when exactly one question is open for that channel; with several open it is refused and the list is sent back; free text is accepted with the option so the operator's exact words stay the quotation. The comm verbs that implement that selection (`question list`, `question show`, `question answer`) are built in Step 3. Step 2's only obligation on this point is the notification content: the question id and numbered options.

Every other line of Step 2 agrees with the decisions and stands: the WebSocket message beside needs_input, the subscriptions accepting the new kinds, the polled GET endpoint existing at all (items 2 and 22), the asker continuing other work while a question is open, and "raising never blocks" (items 2 and 12).

### Step 3: the surfaces

Builds:
- A PWA question queue with an answer form that shows options, the recommendation and the default, a free-text box, a search field, and history with quotations. Its placement follows the first open point.
- CLI commands: question list, question show, question answer, question withdraw and question raise.
- MCP tools: question_raise, question_list, question_get, question_answer and question_withdraw.
- Comm verbs: question list, question show and question answer. The reply mapping follows the open point on comm replies.
- Mobile apps: datawatch-app#283 (filed 2026-10-25, verified open) carries the API contract from Steps 1 and 2, following the Mobile App Sync Rule. This repository holds no native code, so nothing is built here.
- Locale keys in all five bundles, with the locale-guard test updated for the high-visibility keys. Every new string says Automaton or Automata.

Depends on: Steps 1 and 2.

Parity surface: every column in section 5. The mobile column is handled through the external repository.

### Step 4: supervisor and worker integration

Builds:
- A structured marker, DATAWATCH_QUESTION:, followed by one line of JSON. The same classifier pattern as the needs-input marker drops echoes of the instruction text. DATAWATCH_NEEDS_INPUT stays for compatibility.
- The session guardrail text in internal/session/tracker.go describes the new marker alongside the old one.
- A worker that asked either continues other work or parks its task, using a sub-reason in the await-external style.
- The supervisor's tick reads open questions and answers through the list endpoint, not the terminal, and never waits on one.
- Linking: each answer appends a Decision to the Automaton, carrying the answer id. A proposal file's Confirmed line is set from an answer by a docs-only change that uses the stored quote. The design write-back records the quote and answer id under the design section. Who makes that edit is an open point.

Depends on: Steps 1 to 3, because the tick needs the store, the return path and the list surface.

Parity surface: REST and MCP for the tick, a config switch for the marker, and the decision link visible in the PWA through Step 3. Comm and mobile need no change here.

Verification example for this step, to be run when it is built:

```
env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous/ -run Question
```

### Step 5: the AGENT.md Decision Interview Protocol change

Builds:
- DIP step 4 changes from a terminal interview question to a question raised through the queue, with id, options, recommendation and default. The terminal is used only when the queue call fails, and that failure is logged.
- DIP step 5 changes from always waiting to waiting only when this item is blocked by the answer. Otherwise the agent continues other work.
- Decision Making step 2 records the answer id and the verbatim quote, not a paraphrase.
- The supervisor rules document is checked for terminal-question rules, and docs/operations.md gets an access-method table for questions.

Depends on: Steps 1 to 4 working in use.

Parity surface: documentation only. Config, REST, MCP, CLI, comm, PWA and mobile are n/a, because the step changes no runtime behaviour.

## 5. Parity by surface

Columns follow the Configuration Accessibility Rule and the Mobile-Parity Rule. "n/a" means no change, and the reason is given.

| Step | Config | REST | MCP | CLI | Comm channel | PWA | Mobile apps (Android, iOS) |
|---|---|---|---|---|---|---|---|
| 1 store and REST | yes: feature switch, default off, round-trip checked | yes: raise, list, get, answer, withdraw, answers | deferred to Step 3: no client yet | deferred to Step 3: no client yet | deferred to Step 3: no client yet | deferred to Step 3: no UI yet | deferred to Step 3: no client yet |
| 2 notifications and return path | yes: notification toggles | yes: single-question GET and events | n/a: subscriptions take the new kinds, no new tool | n/a: subscription command takes the new kinds | yes: notification text; subscription verb takes the new kinds | yes: WebSocket message type; UI in Step 3 | yes: push through the existing device registration; app side in Step 3 |
| 3 surfaces | n/a: uses the Step 1 switch | yes: search parameter added | yes: five question tools | yes: question commands | yes: question verbs | yes: queue tab, badge and answer form | yes: datawatch-app issue; no native code here |
| 4 supervisor and worker | yes: marker enable flag | n/a: uses Step 1 endpoints | yes: question_raise used by the tick | n/a: no operator command change | n/a: no comm change | yes: decision link shown | n/a: no client change |
| 5 DIP and docs | n/a: documentation only | n/a: documentation only | n/a: documentation only | n/a: documentation only | n/a: documentation only | n/a: documentation only | n/a: documentation only |

## 6. Open questions (not decided)

Each entry is undecided. The recommendations are the proposal's pick, not a decision.

### 6.a Where the queue is shown first

Question: should the question queue sit in the Automation view, or in a new top-level tab?

Options:
1. A "Questions" sub-tab inside the Automation view. Trade-off: it sits next to the Automata it concerns, but design questions that belong to no Automaton have nowhere natural to go.
2. A new top-level "Questions" tab with a header badge. Trade-off: it is visible when nothing is running, and it covers proposal and design questions, at the cost of one more tab.
3. A header tray only, with no tab. Trade-off: the least UI, but no place to review history or search.

Recommendation: a new top-level tab with a header badge, because design and proposal questions are not Automaton-scoped and the Automation view name is reserved for the chain object.

Status: **Decided by the operator, 2026-10-25:** the queue is part of Automation and is shown in the Automation view (design document item 1); details are visible there and in notifications (item 11).

### 6.b Deadlines and expiry

Question: may a question carry a deadline, and what happens at expiry?

Options:
1. No deadline. Trade-off: simplest, but a question can wait forever and block its asker.
2. An optional deadline. At expiry the state becomes expired, and the asker receives question.expired. The default is applied only if the asker's own policy names one. Trade-off: needs a policy per asker.
3. A mandatory deadline with the default applied automatically. Trade-off: this is the silent choice the request forbids, unless the application is recorded as a system answer with the quote none.

Recommendation: option 2, because the asker decides what expiry means, and nothing is applied that the asker did not name.

Status: **Decided by the operator, 2026-10-25:** questions never expire; a skip is per question by default and wide only when that option is enabled, and the supervisor then decides and records it as a supervisor decision on the operator's skip (design document items 2, 5, 5a and 10).

### 6.c Who may answer when several operators exist

Question: which identities may answer a question, and which capability do they need?

Options:
1. The single configured operator identity. Trade-off: simple, and matches today's one-operator setup.
2. Any identity with a new answering capability, plus a per-question allow list that defaults to the operator identity. Trade-off: needs a new federation capability and documentation.
3. Restrict answers by surface, such as only chat from a linked operator. Trade-off: weak when a second operator uses another surface.

Recommendation: option 2, with a new answering capability so that a federated peer can read and raise questions but cannot answer unless it is granted that capability.

Status: **Decided by the operator, 2026-10-25:** not multi-operator yet; any operator authorized for the Automation may answer, from any communication channel (design document items 6 and 7).

### 6.d Whether the tick may proceed on a default for low-risk questions

Question: may the supervisor's tick keep working with a default when the question is low-risk?

Options:
1. Never. Each question blocks only its own item, and the tick waits. Trade-off: safe, but the failure from the request can recur on a single item.
2. Low-risk questions that declare a default may proceed on it after the deadline, with the application recorded as a system answer carrying the quote none. Trade-off: needs a risk class and clear audit.
3. The decision assistant decides within thresholds, in shadow mode first. Trade-off: reuses an existing mechanism, but its scope is Automation hooks only.

Recommendation: option 2, off by default and in shadow mode first, reusing the decision assistant's shadow pattern, because it keeps lanes busy without a silent choice.

Status: **Decided by the operator, 2026-10-25:** low-risk defaults are an Automation setting, never for design questions; low risk is a type list plus a per-question level the asker may raise but never lower; reuse of a repeated design answer is tracked as a future option (design document items 3, 8, 9).

### 6.e Retention of answered questions

Question: how long are answered questions kept?

Options:
1. Keep all records indefinitely. Trade-off: the store grows, but records are small and the quotation trail stays complete.
2. Keep records for a fixed period, then compact them to the answer id and hash, with the full text kept only in the audit log. Trade-off: two places hold the text.
3. Move old records to an archive file. Trade-off: an extra file to export and search.

Recommendation: option 1, because the operator's exact words are the point of the record, and any future prune should run only after an export.

Status: **Decided by the operator, 2026-10-25:** answered questions and quotations are kept forever in the audit log (design document item 13); answers are superseded, never edited (item 14).

### 6.f How the Decision Interview Protocol changes

Question: how should AGENT.md's Decision Interview Protocol change?

Options:
1. Replace the terminal question with a queue call entirely. Trade-off: clean, but it fails when the daemon is down.
2. Queue first, with the terminal used only when the queue call fails, and that failure logged. Trade-off: a fallback path to keep.
3. Leave the protocol as it is and add a note. Trade-off: the terminal stall stays possible.

Recommendation: option 2, because it removes the terminal as the normal channel while keeping the agent able to ask when the daemon is unreachable.

Status: **Decided by the operator, 2026-10-25:** the terminal protocol stays and the queue is added beside it; the supervisor copies terminal answers into the queue with the quotation (design document item 15).

### 6.g Where the mobile app work lives

Question: where should the Android and iOS work for questions be done, given that no mobile source is in this repository?

Options:
1. File datawatch-app issues that carry the API contract, following the Mobile App Sync Rule. Trade-off: the client work is tracked outside this repository.
2. Add native code to this repository. Trade-off: contradicts the current layout, which keeps native clients elsewhere.
3. Defer mobile until the REST contract has been stable for a period. Trade-off: mobile lags the PWA and the operator's stated surfaces.

Recommendation: option 1, once the Step 1 and Step 2 contract ships, because it matches how the existing Android and iOS work is already tracked.

Status: **Decided by the operator, 2026-10-25: option 1** ("1").

### 6.h Secrets inside a verbatim quotation

Question: the answer must store the operator's words verbatim, but the Secrets-Store Rule (AGENT.md, around line 1062) forbids storing secrets. What happens when the free text contains a secret?

Options:
1. Store verbatim with no check. Trade-off: breaks the secrets rule.
2. Refuse to store an answer that matches a secret pattern, tell the operator to resend without it, and audit the refusal without the text. Trade-off: the operator must retype, and the refusal must not echo the secret.
3. Store a redacted quote and a hash. Trade-off: breaks the verbatim rule.

Recommendation: option 2, because it keeps every stored quote verbatim and never stores a secret.

Status: **Decided by the operator, 2026-10-25: option 1 of the interview (refuse and ask again)**, with the refusal message encouraging the safe routes ("1, encourage saving secrets on fs and pointing session to it or coordinate with datawatch secrets"): store the secret in the datawatch secrets store and refer to it by `${secret:name}`, or save it on the filesystem and point the session to the file. The asker receives the secret's name, never its value.

### 6.i Who writes the design and Confirmed lines

Question: should the daemon edit repository documents when an answer is accepted, or only record the link?

Options:
1. The daemon edits the proposal file and commits it. Trade-off: fastest, but the daemon writes into the repository.
2. The daemon records the link and the exact text, and a docs-only change applies it through the normal path. Trade-off: one more step.
3. The operator edits the document by hand. Trade-off: back to the manual copying the request complains about.

Recommendation: option 2, because the repository keeps its normal review path and the daemon never writes files on its own.

Status: **Decided by the operator, 2026-10-25: option 2**, with hand editing kept as an option and full editing by the daemon kept as a future idea to examine ("2 but others may want to edit by hand so leave that as an option,  keep full editing as a future idea to examine").

### 6.j The form of the return path

Question: how does the asker receive the answer?

Options:
1. A lifecycle event on the pending events queue, plus a polled GET. Trade-off: no change to the asker's session, and it reuses the existing queue.
2. A JSON line sent into the asking session through send_input. Trade-off: works for sessions that cannot poll, but it is terminal text again.
3. Both, with the session push opt-in per question. Trade-off: more to test.

Recommendation: option 1 as the default, with the session push opt-in per question, because it keeps answers out of the terminal unless asked for.

Status: **Decided by the operator, 2026-10-25: option 1** ("1"): event plus polled GET by default, session push opt-in per question.

### 6.k How a comm reply selects a question

Question: when several questions are open, how does a bare reply in a chat channel say which one it answers?

Options:
1. The reply must name the question id, for example question answer with the id and an option. Trade-off: explicit, but more typing.
2. Each notification carries the id and numbered options, and a bare reply is accepted only when exactly one question is open for that channel; otherwise the reply is refused with the list. Trade-off: needs a per-channel open count.
3. Only option ids are accepted, with no free text from chat. Trade-off: the operator cannot give the exact words that the request requires.

Recommendation: option 2, because a reply can never bind to the wrong question silently, and the exact words are still captured.

Status: **Decided by the operator, 2026-10-25: option 2 of the list as asked** ("1" in the interview): numbered options on each notification, a bare reply accepted only when exactly one question is open for that channel, otherwise refused with the list; free text accepted.

## 7. Addendum 2026-10-25: the question works with every LLM backend (proposal, not confirmed)

The operator added the requirement: "also make sure question feature works with all llm". The routes below are a proposal for the first confirmed step; the operator confirms or changes them.

Classes of backend, read from `internal/llm/backends` and the session manager:

| Class | Backends | How it asks | How it gets the answer |
|---|---|---|---|
| MCP-capable | claude-code, goose (MCP extension), opencode | calls a `question_ask` tool of the datawatch MCP server, with the session id, PRD and task ids it already receives | the tool result carries the answer when it is ready, or the session polls `question_get`; the session may keep working on other things meanwhile |
| Text-only interactive | aider, gemini, ollama chat, openwebui, shell | prints the existing `DATAWATCH_NEEDS_INPUT: <text>` line; the daemon turns that detected line into a queue question automatically (same record, source = marker) | the daemon sends the answer back as text to the session input, as it does for needs-input today |
| One-shot print mode | pi-prompt and the other `-prompt` backends | has no input once started; the run ends with the marker line | the daemon holds that task as "waiting for a decision" (not failed, no retry counted), and the answer is injected into the task's next prompt as context when it resumes |

Common rules for all classes:
- One record and one queue. The source field says how it was raised (tool, marker, supervisor, system).
- A question raised by a worker never counts as a task failure and never consumes a retry.
- The task or PRD that depends on the answer waits; everything else continues (design doc item 12).
- The text a worker prints is untrusted: the marker line is parsed with a length cap, options come only from the tool route or a structured block, and the free text is stored as a quotation of the asker, never executed.

Conformance test: for each class a fake backend raises a question, the test asserts the question appears in the queue with the right links, that other work proceeds while it is open, that a skip and an answer both resume the asker, and that no retry or failure is counted.

Open points for the operator: whether the marker route may carry options (a small structured block after the marker line) or only free text; and whether a one-shot print backend should be allowed to ask at all, or the daemon should ask on its behalf when its task ends needing a decision.

Operator answers to the two open points above, 2026-10-25 (quoted: "1 marker with options, 2 daemon asks on its behalf"):
1. The marker route carries options: a small structured block after the marker line (options, recommendation, risk label), with the same caps and untrusted-text handling as free text.
2. A one-shot print-mode backend does not ask for itself. When its task ends needing a decision, the daemon raises the question on its behalf, names the task and backend as the asker, and holds the task (not failed, no retry counted) until the answer arrives.

## Rules-review verification 2026-10-27

1. PASS — `docs/implementation.md` line 945 documents `autonomous.questions.enabled`.
2. PASS — `docs/parity-status.md` line 85 names datawatch-app#283; `gh issue view 283 -R dmz006/datawatch-app` returns it OPEN.
3. PASS — `settings_automata_questions_enabled` is in `mustHave` (`internal/server/v5280_locales_test.go` line 142) and has real translations in en, es, de, fr and ja (`internal/server/web/locales/*.json` line 2411).
4. PASS — the CHANGELOG entry has a Reuse audit line (21), a Skills hook N/A reason (23) and a Monitoring N/A reason (25).
5. PASS — settable through MCP and CLI; pin tests `TestQuestionsMCPConfigSetTogglesChannel` (`internal/mcp/questions_config_surface_test.go`) and `TestQuestionsCLIConfigSetTogglesChannel` (`cmd/datawatch/questions_config_surface_test.go`).
6. PASS — the status line (line 4) reads "Status: in progress"; "proposal only" no longer appears.
7. FAIL — `git diff main -- internal/server/repair_depends_on_error_test.go` is non-empty: line 58 drops `allowTestTemp(sm)` and the `sm.WithFakeTmux()` block at lines 55-57 is mis-indented. Not a one-line docs fix in this file; needs `git checkout main -- internal/server/repair_depends_on_error_test.go`.
8. PASS — no "PRD" or "Automation" wording in the CHANGELOG entry, the questions rows of `docs/operations.md` and `docs/api/autonomous.md`, or the locale string; operator-facing text says Automaton/Automata.

Commands (env -u TMUX -u TMUX_TMPDIR): gofmt -l FAIL (`internal/server/repair_depends_on_error_test.go`, the same file as check 7; `cmd/datawatch-channel/main.go` and `internal/mcp/transport_debug.go` also listed, not part of the questions work), go build PASS, go vet PASS, go test ./internal/server/ -run 'Locale|Question' PASS.

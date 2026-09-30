# Harness Research — Recurring Usage Patterns (Cross-Builder)

**Date:** 2026-09-19 (initial, 17 builders) · 2026-09-30 (rewritten against the 20-builder `case-studies.md`) · **Status:** Draft
**Input:** `case-studies.md` (20 curated individual builders) · **Companion:** `candidates.md`, `methodology.md`, `synthesis.md`

Method: read all 20 case studies in `case-studies.md` end-to-end and extracted every technique
that **two or more distinct builders arrived at independently**. "Independently" means each
builder's own source describes the technique as their own design choice; wrapper repos that
merely re-implement another builder's loop were already dropped in curation. A technique is a
named pattern only at **≥2 builders**. Techniques seen in exactly one builder are in the
[Appendix](#appendix--single-builder-techniques-not-recurring).

Counts are out of 20. Patterns are **ranked by builder count, highest first**; ties are broken by
how many distinct variations the builders exhibit (more variation = more independent
convergence), then by document order. Builders are cited as `#N Name` and link to their
case-study anchor (index at the bottom). Quoted text is lifted from the case study, not from
outside sources.

## Ranking at a glance

| Rank | Pattern | Slug | Builders |
|---|---|---|---|
| 1 | Durable on-disk / tracker state for cross-session continuity | `durable-state-continuity` | 10 |
| 2 | Behaviour packaged as versioned, human-readable files | `behaviour-as-files` | 10 |
| 3 | Isolated context and workspace per agent or task | `isolated-agent-context` | 9 |
| 4 | Human-in-the-loop approval gate | `human-approval-gate` | 9 |
| 5 | Bounded loops plus cost/token accounting | `bounded-loops-cost-accounting` | 8 |
| 6 | Tiered multi-model routing, escalation and fallback | `tiered-model-routing` | 7 |
| 7 | Recorded, inspectable run artifacts | `recorded-run-artifacts` | 7 |
| 8 | Deterministic guardrails at the model boundary | `boundary-guardrails` | 6 |
| 9 | Scoped memory as context compression, with promotion | `scoped-memory-promotion` | 6 |
| 10 | Local-inference tier for cost and privacy | `local-inference-tier` | 6 |
| 11 | Machine-readable task plan with pass/fail state | `task-plan-passfail` | 5 |
| 12 | Test-gated autonomous loops (backpressure) | `test-gated-loop` | 5 |
| 13 | Independent verifier grades the producer | `independent-verifier` | 4 |
| 14 | Blame the harness, not the model (failure-driven tuning) | `harness-failure-tuning` | 4 |
| 15 | Agent-maintained instruction files | `self-updating-instructions` | 3 |
| 16 | Eval-driven single-variable iteration | `single-variable-eval` | 2 |
| 17 | Self-waking loops with a triage inbox | `self-waking-triage-loop` | 2 |
| 18 | Structured multi-model debate | `structured-debate` | 2 |
| 19 | Operator drives agents from a chat/mobile surface | `remote-operator-surface` | 2 |

### Verification of the expected areas

The brief listed nine areas to look for. Checked against the actual case studies, not assumed:

| Expected area | Result |
|---|---|
| Memory as context compression with scope/promotion | **Confirmed** — pattern 9 (6 builders). |
| Human-in-loop approval gates | **Confirmed** — pattern 4 (9 builders). |
| Disk/plan-file state for cross-session continuity | **Confirmed** — pattern 1 (10 builders) and pattern 11. |
| Eval-driven iteration loops | **Confirmed, narrow** — pattern 16 holds only 2 builders (#12, #13); pattern 14 is the broader failure-driven variant. |
| Guardrail scanning before commit | **Not confirmed as a distinct pattern.** Only #1 Huntley names security scanners/static analysers as veto gates; #6 Niptao and #4 Akhil gate commits on tests/lint/typecheck, not scanners. It survives only as a variation of pattern 12. Secret redaction (#20) happens at *persist* time, not commit time. |
| Cost/token budgeting | **Confirmed** — pattern 5 (8 builders). |
| Multi-model routing and fallback | **Confirmed** — pattern 6 (7 builders). |
| Self-updating agent instructions (AGENT.md loops) | **Confirmed at the threshold** — pattern 15; strictly only #1 and #4 (#14 is a looser skill-generation variant). |
| Test-gated autonomous loops | **Confirmed** — pattern 12 (5 builders). |

Patterns that were *not* on the expected list but recurred: isolated contexts/workspaces (3),
behaviour-as-files (2), recorded run artifacts (7), boundary guardrails (8), local tier (10),
independent verifier (13), harness-failure tuning (14), self-waking triage (17), debate (18),
remote operator surface (19).

**Not covered by any pattern:** RAG/grounding and lineage/observability beyond call recording —
the same gaps `case-studies.md` flags under "Coverage gaps".

---

## 1. Durable on-disk / tracker state for cross-session continuity

**Slug:** `durable-state-continuity` · **Builders: 10** — [#1 Huntley][b1], [#2 ralphctl][b2], [#3 xr0am][b3], [#4 Akhil][b4], [#6 Niptao][b6], [#14 albert-ying][b14], [#16 OpenRig][b16], [#17 Denicola][b17], [#19 pie][b19], [#20 Forcefield][b20]

**Technique.** Anything that must outlive one model context lives outside it — a plan file, a
JSON task list, a tracker ticket, a session JSONL, a topology snapshot — and every new turn,
process or machine re-reads that artifact instead of relying on conversation. The window is
treated as disposable; the file is the truth.

**Why builders converge.** Every one of them hit the same two failures: a context window that
fills mid-task, and a process/machine that dies mid-run. Writing state down solves both with
one mechanism, and — as #4 puts it — "the model doesn't keep the state in its own memory," so
the loop is only as good as what was written to disk.

**Evidence**
- #1: state "lives on disk (specs/, fix_plan.md, AGENT.md, git)"; context is "deliberately disposable"; "reset over compaction."
- #2: "State (sprint, branch, per-task progress) persists so an interrupted run resumes."
- #3: PRD parsed into `tasks.json` with dependency arrays as the shared source of truth between agents.
- #4: `prd.json` pass/fail flags + `progress.txt`; "each iteration in a fresh agent context."
- #6: Linear is "the queue, the state machine… and the memory" — "re-readable on cold restart."
- #14: "24-hour uninterrupted sessions with resume" via `autolab_resume`.
- #16: `rig down --snapshot` → reboot → `rig up <name>` restores per node (resumed / fresh / failed).
- #17: "push-everything-to-GitHub as the crash-recovery net"; harness config versioned via chezmoi so the VM is disposable.
- #19: "resumable JSONL sessions per project"; stateful cron jobs keep notes between runs.
- #20: sessions stored project-locally and resumable; atomic session writes record an interrupted tool as incomplete.

**One-off variations**
- *Fresh context per iteration* (reset instead of compact): #1, #4 (and per-task fresh subagents in #6).
- *State lives in the team's existing tracker*, not a bespoke file: #6.
- *State is a whole-topology snapshot*, not a task list: #16.
- *Durability by remote push*: #17.
- *Atomic write + "incomplete" marker*: #20.
- *Known footgun, shared by two builders*: resume is tied to the project directory — #17 loses history on folder rename; #20 "`--resume` only finds sessions created in the same directory."

---

## 2. Behaviour packaged as versioned, human-readable files

**Slug:** `behaviour-as-files` · **Builders: 10** — [#1 Huntley][b1], [#4 Akhil][b4], [#6 Niptao][b6], [#7 Storer][b7], [#9 Eve][b9], [#10 joacod][b10], [#11 pcollins][b11], [#14 albert-ying][b14], [#16 OpenRig][b16], [#17 Denicola][b17]

**Technique.** Agent behaviour — prompts, skills, roles, team topology, norms — is expressed as
plain files (Markdown, YAML, small extensions) that are edited, diffed and committed, rather
than hard-coded. A new capability is "drop a file in a folder."

**Why builders converge.** It gives a cheap extension surface with no core change, keeps
behaviour reviewable in git, and makes the harness portable across machines and sessions.
#10 turns it into a rule: "keep the core small and boring."

**Evidence**
- #1: `PROMPT.md`, `specs/`, `AGENT.md` — the prompt stack is the versioned artifact.
- #4: a PRD skill generates `prd.md`; patterns fold into `AGENTS.md`/`CLAUDE.md`.
- #6: the whole orchestration is "one skill file" (Markdown procedure) plus a 180-line bridge script.
- #7: tools, LLM-loop strategy and commands are hot-reloadable extensions under an Apache-2.0 SDK.
- #9: 112 agents, 111 commands, 273 skills, each "one markdown file dropped in `.claude/{agents,commands,skills}/`."
- #10: skills are "local markdown workflow packages"; new capabilities are compositions of runs, not core mutations.
- #11: profile = system prompt + tools + defaults bundle over one shared loop.
- #14: YAML character profiles and per-character `SKILL.md` bundles.
- #16: `RigSpec` YAML plus a `CULTURE.md` norms file.
- #17: `AGENTS.md` rules and skills synced across machines.

**One-off variations**
- *Profile as the unit*: prompt+tools+permission class (#11), with a self-imposed cap ("a handful of profiles, not thirty").
- *Team topology as a file* (#16).
- *Executable extension rather than Markdown* (#7).
- *Norms file that sets group culture* (#16).
- *Unsolved question*: when to inject a skill and when to leave the model alone — #10 says so explicitly.

---

## 3. Isolated context and workspace per agent or task

**Slug:** `isolated-agent-context` · **Builders: 9** — [#1 Huntley][b1], [#2 ralphctl][b2], [#3 xr0am][b3], [#6 Niptao][b6], [#7 Storer][b7], [#14 albert-ying][b14], [#16 OpenRig][b16], [#17 Denicola][b17], [#19 pie][b19]

**Technique.** Each parallel or delegated unit of work gets its own context window and, where
code is touched, its own workspace (git worktree, container, VM, tmux pod). Children return a
result, not their transcript.

**Why builders converge.** Two separate problems are solved by one boundary: parallel agents
stomp on each other's files and ports, and one agent's exploration pollutes another's context.
#2 names the failure directly — parallelism "corrupts branches if you let it."

**Evidence**
- #1: "1 subagent for build/test, N for search/write" — constrained fan-out.
- #2: per-task git worktrees, concurrency 1–5.
- #3: up to three parallel agents; optional Docker sandbox per loop.
- #6: "fresh subagent in its own worktree… worktree deleted" per ticket.
- #7: "delegate to a child thread that returns only its result."
- #14: each trainee gets its own context window and provider; one failing does not stop the others.
- #16: pods as bounded-context groups; owner and checker are separate seats.
- #17: each agent in its own worktree with its own dev server on its own tailnet port.
- #19: trigger actions run in a sub-agent; "parent conversation not auto-inherited."

**One-off variations**
- *Worktree* as the isolation unit: #2, #6, #17. *Thread/branch in a conversation tree*: #7. *Pod/seat topology*: #16. *Disposable VM as blast radius*: #17. *Container*: #3.
- *Coordination of isolated peers*: #3 uses a "safe to start" query as the inter-agent lock (see pattern 11).

---

## 4. Human-in-the-loop approval gate

**Slug:** `human-approval-gate` · **Builders: 9** — [#6 Niptao][b6], [#7 Storer][b7], [#10 joacod][b10], [#11 pcollins][b11], [#14 albert-ying][b14], [#16 OpenRig][b16], [#17 Denicola][b17], [#19 pie][b19], [#20 Forcefield][b20]

**Technique.** The autonomous loop is deliberately stopped at a protocol step where a human
must say yes before a consequential action proceeds. The gate is a blocking call, a label
transition, a per-action permission, or a claim step — not a dashboard afterthought.

**Why builders converge.** Every one ships real work with real blast radius, and the one thing
all of them kept is a place where a person can say no. Gates differ in *where* they sit, which
is the source of most variation.

**Evidence**
- #6: "Nothing is built without an explicit human go"; `manual` fences a ticket off entirely.
- #7: "approval gating on tool calls"; allow/deny individual tools.
- #10: "approval boundary as a policy decision recorded on the run."
- #11: "permission is per-action, not per-session" — pre-approved safe tools, always-ask for watched ones.
- #14: `autolab_editorial` blocks for accept / revise / reject; `lab_meeting` pauses for feedback.
- #16: the operator reads both owner and checker output before the next change.
- #17: zero-approval inside the VM, but the human gate moves to PR review: "skim diff → merge."
- #19: findings land in a triage inbox; `/inbox claim <n>` promotes one into a real turn.
- #20: three-state allow/ask/deny evaluated before execution.

**One-off variations**
- *Gate position*: before build (#6), per tool call (#7, #11, #20), at deliverable (#14), at merge (#17), at claim (#19).
- *Three-way decision with a note* rather than binary (#14), plus an AI fallback when the human is absent (`autolab_editor_act`).
- *Gate moved out of the agent into a chat reply* (`go`/`skip`, #6).
- *Gate relaxed on purpose behind a stronger boundary* (#17).

---

## 5. Bounded loops plus cost/token accounting

**Slug:** `bounded-loops-cost-accounting` · **Builders: 8** — [#2 ralphctl][b2], [#4 Akhil][b4], [#5 deepclaude][b5], [#7 Storer][b7], [#9 Eve][b9], [#13 McCabe][b13], [#19 pie][b19], [#20 Forcefield][b20]

**Technique.** Every loop carries a hard cap (iterations, rounds, attempts, messages) and most
builders also surface spend or token use as a first-class readout. Caps stop runaways;
accounting makes routing and prompt decisions legible.

**Why builders converge.** Unbounded agent loops are the largest line item on any bill and the
most common way to burn a night. #9 states the motivation plainly: long agentic loops "can run
up surprisingly fast."

**Evidence**
- #2: `maxAttempts` (default 3) then `blocked`, never `done`.
- #4: the max-iteration cap "is the only hard stop."
- #5: `/_proxy/cost` tracks token use and savings against the Anthropic equivalent.
- #7: per-transaction token/cache use and stop reason; compaction measures the full request before each call.
- #9: a 40-round tool loop as the safety cap.
- #13: per-run token counts — identical counts across "different" runs exposed a wiring bug (see pattern 14).
- #19: honest `/cost` cache accounting; cron "does not backfill missed ticks," and a still-running job skips the next tick.
- #20: session cap of 1000 messages with an observable `[compacted N]` marker; memory capped at 200 entries / 8 KiB.

**One-off variations**
- *Escalate on plateau* rather than just stop (#2).
- *Savings-versus-baseline readout* (#5). *Cache-hit accounting* (#19).
- *Cap as compaction trigger* (#20) versus cap as hard stop (#2, #4, #9).
- *Only hard stop is the cap; everything else relies on the model honouring instructions* — #4 flags this as a weakness.

---

## 6. Tiered multi-model routing, escalation and fallback

**Slug:** `tiered-model-routing` · **Builders: 7** — [#1 Huntley][b1], [#2 ralphctl][b2], [#5 deepclaude][b5], [#9 Eve][b9], [#10 joacod][b10], [#13 McCabe][b13], [#14 albert-ying][b14]

**Technique.** Different steps use different models: a cheap or local model for routine turns,
a frontier model for hard ones, often with a second model as planner, judge or rescuer. The
active model can change mid-run, and some harnesses fall back automatically.

**Why builders converge.** The 80/20 split is real: #5 measured ~80% routine work, and #10
frames planning as "the expensive part, execution is commodity." A single provider is also a
cost line and a single point of failure.

**Evidence**
- #1: "oracle (second model) for planning and rescue" — a compiler error wall is sent to Gemini for a recovery plan.
- #2: 26 cost-tiered presets; "cheap generator behind a top-tier evaluator"; climbs the ladder one rung at a time on a stall, carrying the critique.
- #5: live switch via `/_proxy/mode` between DeepSeek, OpenRouter, Fireworks and Anthropic, without restarting the session.
- #9: an auto-router sends chat to local models and real coding work to a cloud agentic model; context carries across the switch.
- #10: strong model plans, local MLX model executes.
- #13: local Ollama for iteration; the harness is "the seam" to swap in a frontier model and re-run.
- #14: per-role providers (Claude for data, Codex for writing, Opus for code) with automatic fallback to single-agent mode.

**One-off variations**
- *Trigger*: per message (#9), on stall (#2), operator command (#5), per role (#14), planning-versus-execution phase (#10), rescue on error (#1).
- *Known hole*: the router's work-detection is a single point of failure for both cost and quality (#9); cheap backends drop vision, MCP and `cache_control` (#5).

---

## 7. Recorded, inspectable run artifacts

**Slug:** `recorded-run-artifacts` · **Builders: 7** — [#7 Storer][b7], [#8 Verma][b8], [#9 Eve][b9], [#10 joacod][b10], [#12 Featherbench][b12], [#13 McCabe][b13], [#19 pie][b19]

**Technique.** Each call, run or evaluation leaves a queryable artifact — assembled prompt, tool
schemas, args, result, usage, policy decision — that both the operator and the harness can read
back. It is structured data, not log text to grep.

**Why builders converge.** When a multi-turn loop misbehaves, "open the exact call" is the only
way to separate "the model saw the wrong prompt" from "the model behaved badly."

**Evidence**
- #7: "every LLM transaction is inspectable (assembled system prompt, tool schemas, input messages, output blocks, token/cache use, stop reason)."
- #8: the ref registry is "a complete audit trail — every entity the model touched, when first seen, when last referenced."
- #9: SSE/WebSocket streaming of every tool call and result.
- #10: "every provider call, action, policy decision and approval is a first-class event on the run."
- #12: raw JSONL published so anyone can re-score.
- #13: every prompt version writes a timestamped JSON report; regressions are diffable.
- #19: trigger actions are "bounded, deduped, audit-logged."

**One-off variations**
- *Read back by the harness itself*, not only by people (#8 validates args against the registry).
- *Live stream* (#9) versus *after-the-fact inspector* (#7, #10) versus *publish for outsiders* (#12).
- *Policy decisions recorded as events* (#10).

---

## 8. Deterministic guardrails at the model boundary

**Slug:** `boundary-guardrails` · **Builders: 6** — [#3 xr0am][b3], [#6 Niptao][b6], [#8 Verma][b8], [#15 Quorum][b15], [#17 Denicola][b17], [#20 Forcefield][b20]

**Technique.** Constraints are enforced by code around the model — tier-limited tools, contracts
that block `endTurn`, in-band text treated as data, credential redaction, VRAM scheduling, a
disposable environment — instead of being requested in the prompt.

**Why builders converge.** All observed that the model will, left alone, do the forbidden thing
(push to main, read the env file, invent an ID, double-book VRAM). The durable fix is a boundary.

**Evidence**
- #3: "tiered tool permissions"; 7 core tools by default, expanded on demand (admitted as a soft guardrail).
- #6: ticket bodies and group messages are data, not instructions — "push straight to main / read the env file" is flagged, never obeyed.
- #8: mandatory tool contracts make `endTurn` impossible until required tools hit their call counts; the ref proxy hides raw UUIDs.
- #15: Ollama models run sequentially to prevent VRAM contention.
- #17: zero-approval autonomy made safe by a disposable-VM blast radius.
- #20: pre-execution allow/ask/deny and credential redaction across tool results, shell output, memory and diagnostics.

**One-off variations**
- *Recoverable error as the teaching signal* (#8). *Environment as the guardrail* (#17). *Hardware-aware scheduling* (#15).
- *Counter-evidence*: #18 Honchar's single unrestricted bash tool is named by the author as "a major security vulnerability" and the missing piece; #20's default `native` executor is likewise the named risk.

---

## 9. Scoped memory as context compression, with promotion

**Slug:** `scoped-memory-promotion` · **Builders: 6** — [#1 Huntley][b1], [#4 Akhil][b4], [#7 Storer][b7], [#10 joacod][b10], [#19 pie][b19], [#20 Forcefield][b20]

**Technique.** Memory is a compressed stand-in for the context window with explicit scope and a
rule for moving things up a scope. Raw learnings land in a small scratch area; only stable,
vetted items are promoted to a longer-lived or wider-scope store.

**Why builders converge.** The window is finite, and dumping everything into long-term memory
poisons it. Compression and promotion are how they keep recall cheap and trustworthy.

**Evidence**
- #4: every iteration appends to `progress.txt`, then "folds stable patterns into AGENTS.md" — a two-tier scratch → promoted split.
- #1: the agent writes what it learned about building/running back into `AGENT.md`.
- #7: "fold a span of history into a thread; move or copy items between branches" — compression and scope-move are operations on a tree.
- #10: memory modelled as "(storage, recall, ranking, confidence, provenance, approval)," proposed from evidence and recalled with provenance.
- #19: a stateful loop keeps notes between runs and surfaces findings to an inbox; claiming promotes one into a real turn.
- #20: a 1000-message cap with compaction, plus a deliberately small memory (200 entries / 8 KiB).

**One-off variations**
- *Promotion needs approval* (#10); *promotion is a human claim* (#19); *promotion is automatic on stable pattern* (#4).
- *Memory size cap as a feature* (#20).
- *Note*: #6's tracker memory and #16's pod-shared memory are scoped stores but without a promotion rule, so they are counted under pattern 1, not here.

---

## 10. Local-inference tier for cost and privacy

**Slug:** `local-inference-tier` · **Builders: 6** — [#9 Eve][b9], [#10 joacod][b10], [#13 McCabe][b13], [#15 Quorum][b15], [#19 pie][b19], [#20 Forcefield][b20]

**Technique.** A local model (Ollama, MLX, llama.cpp, LM Studio) is a first-class tier, used for
cheap turns, private data or offline iteration rather than as a fallback of last resort.

**Why builders converge.** Two independent reasons: cost (no billing cycle for iteration-heavy
work) and exposure (code and secrets never leave the machine).

**Evidence**
- #9: fine-tuned local persona models avoid per-token cost and provider data exposure.
- #10: local inference framed as "a cost/privacy tier, not a compromise."
- #13: Ollama at ~8–10 s/case and $0 for cost-honest iteration.
- #15: local Ollama models selectable alongside cloud models.
- #19: local DeepSeek V4 with byte-exact KV-prefix caching.
- #20: local providers with no account, cloud or telemetry.

**One-off variations**
- *Hardware scheduling* (#15). *KV-cache reuse* (#19). *Fine-tune behaviour into weights* (#9).
- *Honest limit*: #9 admits the heavy-reasoning tier still delegates to a cloud model.

---

## 11. Machine-readable task plan with pass/fail state

**Slug:** `task-plan-passfail` · **Builders: 5** — [#1 Huntley][b1], [#2 ralphctl][b2], [#3 xr0am][b3], [#4 Akhil][b4], [#6 Niptao][b6]

**Technique.** A free-form request is turned into an explicit task list or graph with per-item
state, and the loop always works the next unfinished item — or the next one whose parents are done.

**Why builders converge.** A prose TODO gives no verifiable "done" and no way for parallel agents
to know what is safe to start. #4 is blunt that the JSON pass/fail layer "is the load-bearing piece."

**Evidence**
- #1: priority-ordered `fix_plan.md`, one task per iteration; regenerate the plan rather than hand-edit it.
- #2: sprint → dependency-ordered task graph in waves of independent tasks.
- #3: `tasks.json` with dependency arrays; agents query "safe to start" work.
- #4: PRD → `prd.json` with pass/fail per story.
- #6: Linear tickets and labels as the queue and state machine.

**One-off variations**
- *Dependency edges* (#2, #3) versus flat ordered list (#1, #4, #6).
- *Complexity-scored subtask expansion* (#3 only — see Appendix).
- *Plan regenerated, never edited* (#1).

---

## 12. Test-gated autonomous loops (backpressure)

**Slug:** `test-gated-loop` · **Builders: 5** — [#1 Huntley][b1], [#2 ralphctl][b2], [#4 Akhil][b4], [#6 Niptao][b6], [#8 Verma][b8]

**Technique.** A unit of work cannot be marked done, committed or shipped until an automated gate
passes — tests, type-check, linter, build, or a contract check. Failure feeds back into the next
attempt.

**Why builders converge.** The model that feels finished is not finished. #2 documents an agent
feeling its context fill and "declaring things done that aren't"; the gate is what catches it.

**Evidence**
- #1: type-checker / build / test as the rejection gate — "the wheel has to turn fast"; wire in anything that can veto, including static analysers and security scanners.
- #2: per-module verify gates (path prefix + command + timeout), baselined first, re-run only where the diff touched.
- #4: "gate (tests/typecheck) → commit → flag complete."
- #6: tests and lint before the PR; a failed build is commented on the ticket and skipped for the rest of the run.
- #8: `endTurn` is refused until mandatory tools have been called the required number of times.

**One-off variations**
- *Diff-scoped gate* (#2). *Unit-scoped tests only per change* (#1). *Contract gate in place of tests* (#8).
- *Scanner gate* appears only in #1, so "guardrail scanning before commit" is not a separate pattern.
- *Weakness*: in #4 the gate relies on the model following `AGENTS.md`, so a model that declares itself done can skip it.

---

## 13. Independent verifier grades the producer

**Slug:** `independent-verifier` · **Builders: 4** — [#2 ralphctl][b2], [#12 Featherbench][b12], [#13 McCabe][b13], [#16 OpenRig][b16]

**Technique.** The model or seat that produces an artifact is not the one that approves it.
A separate evaluator grades against written criteria; specific critique returns to the producer;
retries are capped.

**Why builders converge.** Self-evaluation is unreliable, and each builder found the independent
grader is itself fallible, so it must be measured.

**Evidence**
- #2: "an independent model grades the change against the task's verification criteria"; failure returns the critique; `blocked`, never `done`. Ceiling named: "the evaluator can be wrong too."
- #12: machine checkers assert the floor; a blind judge panel with a published bias matrix grades the ceiling.
- #13: LLM judge validated per axis against hand scores (faithfulness κ=0.57; completeness κ=0.10, retracted).
- #16: `dev-owner` and `dev-check` seats — "trust-but-verify as a topology property, not a prompt."

**One-off variations**
- *Seat in a topology* (#16) versus *model in a pipeline* (#2) versus *judge panel* (#12). *Judge validated against humans* (#13). *Judge bias measured, not eliminated* (#12).

---

## 14. Blame the harness, not the model (failure-driven tuning)

**Slug:** `harness-failure-tuning` · **Builders: 4** — [#1 Huntley][b1], [#8 Verma][b8], [#12 Featherbench][b12], [#13 McCabe][b13]

**Technique.** Observed failures are treated as defects in the prompt, tool or plumbing, not in
the model. The builder adds a rule, tightens a contract or fixes a wiring bug, then re-runs.

**Why builders converge.** The tempting fix — buy a bigger model — did not work for any of them.
Their evidence pointed at the harness each time.

**Evidence**
- #1: "I haven't blamed the tools; instead, I've looked inside" — "signs" added in response to observed misbehaviour, such as assuming a feature is missing after one empty ripgrep.
- #8: "a pattern of failures usually means a tool is confusing or a contract is too loose"; reliability is not something to "buy from the model."
- #12: a "Mistakes I Made Building the Harness" section — "the harness's own plumbing bit him before the model did."
- #13: identical token counts across "different" prompt runs exposed that the version flag was never threaded through.

**One-off variations**
- *Prompt invariant added* (#1). *Contract or tool wording tightened* (#8). *Harness self-test* (#13). *Author's own rubric was the incoherent part* (#13: the completeness judge disagreed because he disagreed with himself).

---

## 15. Agent-maintained instruction files

**Slug:** `self-updating-instructions` · **Builders: 3** (2 strict) — [#1 Huntley][b1], [#4 Akhil][b4], [#14 albert-ying][b14]

**Technique.** The agent writes back into the files that steer it: `AGENT.md`/`AGENTS.md`, prompt
invariants, or new skill files. Later fresh-context iterations inherit what the agent learned.

**Why builders converge.** With fresh context each iteration, instruction files are the only
channel through which lessons can persist.

**Evidence**
- #1: when the agent learns how to build or run the project "it updates AGENT.md itself."
- #4: patterns discovered during an iteration are folded into `AGENTS.md`/`CLAUDE.md`.
- #14 (looser variant): when a role lacks a capability, a cascade ends by auto-generating a `SKILL.md`.

**One-off variations**
- *Self-edit* (#1, #4) versus *self-extend with a new skill* (#14). *Human-authored but synced rule file*: #17's `AGENTS.md` is adjacent, not self-updating.
- *Risk*: no builder describes a review step for these edits.

---

## 16. Eval-driven single-variable iteration

**Slug:** `single-variable-eval` · **Builders: 2** — [#12 Featherbench][b12], [#13 McCabe][b13]

**Technique.** A fixed panel of tasks, prompts, checkers and rubric is pinned in source control so
that only one variable moves between runs. The same panel is re-run at each model release or
prompt version and the delta is attributed to that variable.

**Why builders converge.** Both fell into the same trap: an apparent win that was really a
changed harness or a weak baseline.

**Evidence**
- #12: "one variable changes between runs — the model"; 28 fixed tasks re-run at each release.
- #13: 58-case golden set from a rule baseline; prompts versioned `--prompt=v1|v2|v3`; "swap in a frontier model… re-run the identical golden set."

**One-off variations**
- *Fair baseline first* — a weak regex made the LLM look great, and a fair one erased the edge (#13).
- *Wilson intervals, blind judges, canary controls* (#12). *Refusals counted as failures* (#12).

---

## 17. Self-waking loops with a triage inbox

**Slug:** `self-waking-triage-loop` · **Builders: 2** — [#6 Niptao][b6], [#19 pie][b19]

**Technique.** The loop schedules its own next run and goes quiet when blocked. Findings are not
pushed into the chat; they wait in an inbox or tracker for the human to act on.

**Why builders converge.** Both wanted agent-initiated work that does not interrupt the operator.

**Evidence**
- #6: when every ticket is blocked on a human the loop schedules a wake-up 20–30 minutes out.
- #19: a `--stateful` cron job reports to a triage inbox; the operator claims an item to promote it.

**One-off variations**
- *Wake interval chosen by the agent* (#6) versus *cron with no backfill* (#19).

---

## 18. Structured multi-model debate

**Slug:** `structured-debate` · **Builders: 2** — [#14 albert-ying][b14], [#15 Quorum][b15]

**Technique.** Two or more models or named roles argue a question through a defined protocol and
converge on one output.

**Why builders converge.** A single model's answer has no signal on how wrong it could be.
Forcing a counter-position surfaces blind spots.

**Evidence**
- #15: seven formal methods (Oxford, Advocate, Socratic, Delphi, Tradeoff, Brainstorm, Standard), with early exit on consensus.
- #14: PI ↔ Trainee role alternation, with a human editor above.

**One-off variations**
- *Protocol chosen per question by an AI advisor* (#15). *Role alternation with a human editor* (#14).
- *Limit*: a single-model setup degrades debate to monologue (#15).

---

## 19. Operator drives agents from a chat or mobile surface

**Slug:** `remote-operator-surface` · **Builders: 2** — [#6 Niptao][b6], [#17 Denicola][b17]

**Technique.** The operator directs and reviews agent work from a phone or group chat over a
private network or bot bridge, not from a desk terminal.

**Why builders converge.** The bug or request arrives away from the desk, and the round trip
(ticket → build → preview → merge) is short enough to close from a phone.

**Evidence**
- #6: Telegram group → Linear ticket → isolated build → PR.
- #17: phone → remote session over Tailscale → agent fixes and smoke-tests → tailnet preview URL → merge.

**One-off variations**
- *Bot bridge* (#6) versus *private network plus thin client* (#17).
- *Shared gotcha*: session history and pollers silently lost — #6's `getUpdates` retains only ~24 h; #17 loses history on folder rename.

---

## Appendix — single-builder techniques (not recurring)

Each appeared in exactly one builder. They are concrete and reusable but do not meet the 2+ bar.

| Technique | Builder | Note |
|---|---|---|
| Ref proxy: model-visible `ref_*` aliases for raw UUIDs, with a durable registry | [#8 Verma][b8] | Makes hallucinated IDs structurally impossible. |
| Recoverable, specific error as the retry prompt | [#8 Verma][b8] | Tool errors tell the model exactly what to do next. |
| Complexity-scored subtask expansion (1–10) | [#3 xr0am][b3] | LLM as a prior over workload. |
| "Safe to start" query as an inter-agent lock | [#3 xr0am][b3] | Dependency predicate replaces merge-conflict avoidance. |
| Prompt invariants as numbered "signs" | [#1 Huntley][b1] | Closest to pattern 14 but the numbering scheme is unique. |
| Anti-placeholder and "don't assume absent" invariants | [#1 Huntley][b1] | |
| Spec-first generation with agent-found contradictions | [#1 Huntley][b1] | Spec ambiguity is his stated root failure. |
| Persona fine-tuned into model weights | [#9 Eve][b9] | |
| Mid-task STEER input that injects a correction without stopping | [#9 Eve][b9] | |
| Run as the first-class abstraction; small-core litmus test | [#10 joacod][b10] | New capability = composition of runs. |
| Corpus-grounded style matching over one's own prior outputs | [#11 pcollins][b11] | |
| Anti-sprawl cap on profiles | [#11 pcollins][b11] | |
| Blind judge panel with published bias matrix; Wilson intervals | [#12 Featherbench][b12] | |
| Canary negative controls for security tasks | [#12 Featherbench][b12] | |
| Refusals scored as failures | [#12 Featherbench][b12] | |
| Per-axis human validation of an LLM judge (κ) | [#13 McCabe][b13] | |
| Correlation-ID enrichment as the highest-leverage deterministic gain | [#13 McCabe][b13] | |
| Runtime skill auto-acquisition cascade (marketplace → GitHub → ToolUniverse → generate) | [#14 albert-ying][b14] | |
| CrossRef-verified citations as a hallucination guard | [#14 albert-ying][b14] | |
| AI method advisor that routes over debate protocols | [#15 Quorum][b15] | |
| Synthesizer rotation to avoid vendor bias | [#15 Quorum][b15] | |
| Declarative team YAML with snapshot/restore and adoption of live sessions | [#16 OpenRig][b16] | |
| Portable, SHA-256-checked agent-team bundle | [#16 OpenRig][b16] | |
| Agent-operated migration with receipts and rollback | [#16 OpenRig][b16] | |
| Portless tailnet preview URL forced through `AGENTS.md` | [#17 Denicola][b17] | |
| Bootstrapping via dotfile manager (chezmoi) | [#17 Denicola][b17] | |
| Hand-written harness as a deliberate learning rule | [#18 Honchar][b18] | No LLM-written code so the author understands it. |
| Single universal bash tool (ReAct) | [#18 Honchar][b18] | Author names the security cost. |
| Byte-exact request stream for KV-prefix reuse | [#19 pie][b19] | |
| Session-scoped triggers with audit; cross-agent messaging removed | [#19 pie][b19] | |
| Credential redaction across every persisted surface; `doctor` that never prints secrets | [#20 Forcefield][b20] | Near pattern 8, but only one builder. |
| Atomic session writes (temp + fsync + rename) | [#20 Forcefield][b20] | |

---

*Research documentation only — no code. Ranked by distinct-builder count across the 20 builders in `case-studies.md`.*

[b1]: case-studies.md#1-geoffrey-huntley--the-ralph-wiggum-loop
[b2]: case-studies.md#2-lukas-grigis--ralphctl-generatorevaluator-ralph-harness
[b3]: case-studies.md#3-xr0am--taskmaster-style-parallel-agent-coordination
[b4]: case-studies.md#4-akhil-thetoolnerd--prdjson-ralph-adoption-loop
[b5]: case-studies.md#5-aattaran--deepclaude-cost-router-with-live-model-switching
[b6]: case-studies.md#6-shashank-singla-niptao--telegram--linear-ticket-loop
[b7]: case-studies.md#7-julian-storer--juggler-context-surgeon-agent-workbench
[b8]: case-studies.md#8-nikhil-verma--harness-that-makes-a-small-llm-reliable
[b9]: case-studies.md#9-jeff-green--eve-v2-persona--40-round-local-agentic-loop
[b10]: case-studies.md#10-joacod--nano-harness-runs-as-the-core-abstraction
[b11]: case-studies.md#11-pcollins--pi-sdk-profile-harness-admin-router--writing-agent
[b12]: case-studies.md#12-ed-yau--featherbench--ed-o-meter-one-variable-eval-harness
[b13]: case-studies.md#13-allen-mccabe--llm-triage-eval-the-harness-that-proved-the-llm-lost
[b14]: case-studies.md#14-albert-ying--autonomous-lab-seniorjunior-editorial-loop
[b15]: case-studies.md#15-detrol--quorum-method-driven-multi-model-debate-harness
[b16]: case-studies.md#16-mvschwarz--openrig-yaml-topologies-over-coding-agent-rigs
[b17]: case-studies.md#17-domenic-denicola--disposable-vm--tailnet-agentic-setup
[b18]: case-studies.md#18-vitalii-honchar--elicode-rust-react-learning-harness
[b19]: case-studies.md#19-c4pt0r--pie-rust-local-agent-runtime-with-stateful-loops
[b20]: case-studies.md#20-jehoshuam--forcefield-local-first-single-binary-agent-harness

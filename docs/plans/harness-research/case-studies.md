# Harness Research — Case Studies (Curated Individual Builders)

**Date:** 2026-09-19 · **Status:** Draft · **Companion:** `candidates.md`, `methodology.md`, `synthesis.md`

Curation pass over `candidates.md` (63 raw candidates + 4 aggregators). Selection criteria applied:
(a) a real person or small individual/small team built it — no framework-org repos;
(b) it is harness-shaped: wraps / orchestrates / guards / measures an LLM;
(c) it is documented enough to extract a concrete recurring usage loop.

17 builders selected. Every URL below was re-verified (HTTP 200 or a live page the
candidate's own fetch returned, checked 2026-09-19). Dropped during curation:
Ralph-loop wrapper repos with no independent technique of their own (nitodeco,
yoshpy-dev, pickle-rick-claude, Clancy Wiggum, bgnm2000 orchestrator sample),
TMUX multiplexers that manage but don't constrain the model (claude-squad,
claude-dashboard, TBD), homelab setup posts with no loop to extract (Wade,
pedroalonso, kunalganglani, abhisaha, fr4iser90, Mate), unverified hosts
(narrator.sh timeout — dropped per "verifiable URLs only"; favur.dev 429 —
kept only as a pointer, not promoted), and podcast-episode users of vendor
frameworks who did not build the harness themselves (Alessio Fanelli,
Symphony+Linear). The awesome-lists (rows 64–67) were not mined for new
individuals: their entries resolved to the same vendor repos already in
`examples-catalog.md`.

---

## 1. Geoffrey Huntley — the Ralph Wiggum loop

**Who / URLs**
- Geoffrey Huntley (ghuntley) — https://ghuntley.com/ralph/
- Loop companion post: https://ghuntley.com/loop/
- Specs technique: https://ghuntley.com/specs/

**What the harness does**
A bare-bash fresh-context autonomous coding loop — `while :; do cat PROMPT.md | claude-code; done` — where all durable state lives on disk (specs/, fix_plan.md, AGENT.md, git) and the context window is deliberately disposable. Huntley ran it to build an entire new programming language (compiler + stdlib) without humans writing code. The loop itself is dumb; the chassis is the file system plus a stack of numbered prompt invariants.

**Usage loops**
1. Plan-regen loop: run a planning-mode Ralph that studies specs/ vs. source with up to 500 subagents and re-derives fix_plan.md (priority-ordered TODO list); discard and re-run this plan when it drifts, never hand-edit.
2. Build loop: one task per iteration from fix_plan.md; after the change, run only the tests for that unit; commit + git tag when green.
3. Backpressure loop: type-checker / build / test as the rejection gate; wire in anything that can veto (static analyzers, security scanners) — "the wheel has to turn fast."
4. Self-tuning loop: when the agent learns how to build/run the project, it updates AGENT.md itself; "signs" (prompt invariants like "don't assume it's not implemented") are added in response to observed misbehavior.

**Techniques**
fresh-context re-init per iteration (reset over compaction); disk-as-memory (git + plan files); backpressure gates (unit tests per change, type checkers for dynamic languages); parallel subagent fan-out with constrained parallelism for builds (1 subagent for build/test, N for search/write); spec-first generation (conversation → specs/ files before any code); anti-placeholder invariants; self-improving AGENT.md; oracle (second model) for planning and rescue (e.g. compiling error wall → Gemini plans recovery).

**What datawatch could learn**
Treat the per-loop prompt stack as a versioned artifact with numbered invariants ("999999999. …") and let the agent itself propose invariant additions; BL24's planning prompt today is a monolith with no per-failure-mechanism audit trail. Huntley's "tune the sign, don't blame the model" loop is exactly the missing verifier-feedback → prompt-mutation edge in the PRD executor.

---

## 2. Lukas Grigis — ralphctl (generator/evaluator ralph harness)

**Who / URLs**
- Lukas Grigis — https://lukasgrigis.dev/blog/ralph-loop/
- Harness repo: https://github.com/lukas-grigis/ralphctl (npm: https://www.npmjs.com/package/ralphctl)
- Field reports: https://lukasgrigis.dev/blog/ralphctl-overnight-run/ · https://lukasgrigis.dev/blog/ralphctl-agent-harness/

**What the harness does**
A runnable Ralph harness in Node that decomposes a plain-language sprint into a dependency-ordered task graph (waves of independent tasks) and drives each task through a strict generator→evaluator gate: an independent model grades the change against the task's verification criteria; failure returns the specific critique to the generator; after maxAttempts (default 3) the task flags `blocked`, never `done`. State (sprint, branch, per-task progress) persists so an interrupted run resumes.

**Usage loops**
1. Sprint loop: plain-language sprint → task graph → waves → per-task gen/eval until every task is verified-done or blocked.
2. Referee loop: generator writes; evaluator gate grades against criteria; critique returned verbatim; retry ≤3 then blocked.
3. Budget loop: 20 cost-tiered presets (standard / economic / strong-gate / fast / frontier); cheap generator behind a top-tier evaluator; on a stall the harness climbs the model ladder one rung at a time carrying the critique upward.
4. Verify-scoping loop: each module declares verify gates (pathPrefix + command + timeout); baseline run before the task, then only re-run gates whose path prefix the diff touched — fail fast.

**Techniques**
independent-evaluator LLM judging (model that writes ≠ model that approves); diff-scoped verification gates; token-budget presets with `escalateOnPlateau`; task-graph waves with per-task git worktrees (concurrency 1–5); crash-resume from persisted state; three-provider orchestration (Claude Code verified, Copilot CLI / Codex preview).

**What datawatch could learn**
ralphctl's single most-transferable mechanism is the diff-scoped gate: BL367 quality gates run a fixed `test_command` per PRD; scoping gate commands by a pathPrefix against the actual diff (and baselining before run) is a cheap upgrade that mirrors what ralphctl does per module.

---

## 3. xr0am — Taskmaster-style parallel agent coordination

**Who / URLs**
- xr0am (Orchestrated Code) — https://xr0am.substack.com/p/what-ralph-wiggum-loops-are-missing
- Tooling he shipped with: https://github.com/eyaltoledano/claude-task-master (referenced; his post documents his own usage pattern of PRD → tasks.json → parallel agents)

**What the harness does**
A practitioner who shipped two production products with a persistent-agent pattern months before "Ralph" was named: a PRD parsed into `tasks.json` with explicit dependency arrays, per-task complexity scores (1–10) driving subtask expansion, and up to three Cursor agents running in parallel, each querying which subtasks are safe to start. Tool access is tiered (7 core tools by default, expanded on demand) and the loop can run agents in a Docker sandbox.

**Usage loops**
1. PRD → tasks loop: AI parses the PRD into tasks.json with dependencies already mapped (auth before endpoints, Redis before rate limiting); the agent cannot start a dependent task until its parents complete.
2. Complexity loop: score each task 1–10, then `expand` into subtask counts proportional to score; each subtask carries its own dependency array.
3. Parallel-dispatch loop: multiple agents query the task store for "safe to start" work each iteration — coordination replaces merge-conflict avoidance.
4. Guardrail loop: tiered tool permissions constrain what agents may touch; optional container sandbox per loop.

**Techniques**
machine-readable dependency DAG over freeform markdown; complexity-scored subtask expansion (LLM-as-prior over workload); safe-to-start querying as the inter-agent lock; tool-tier permissioning as a soft guardrail; Docker-sandbox loop execution.

**What datawatch could learn**
Datawatch's BL117 orchestrator wires dependencies between PRDs; xr0am's pattern inverts the granularity — dependencies and "ready-now" queries *inside* a single task pool. A `ready_tasks` MCP surface (tasks + dependency array + safe-to-start predicate) on top of BL357's work queue would give workers collision-free parallelism without an external planner.

---

## 4. Akhil (TheToolNerd) — PRD→JSON ralph adoption loop

**Who / URLs**
- Akhil (The Tool Nerd) — https://www.thetoolnerd.com/p/autonomous-ai-agent-loop-for-building
- Adopted loop: https://github.com/snarktank/ralph (Ryan Carson's implementation, referenced)

**What the harness does**
A documented step-by-step harness for shipping features with the ralph pattern: a one-sentence project description is expanded by a PRD skill into a structured `prd.md`, converted to `prd.json` with per-story pass/fail flags; the ralph script then loops: pick next unfinished item → implement via the coding CLI → run checks → commit and flip the flag → write the learning to `progress.txt` → update AGENTS.md/CLAUDE.md with discovered patterns → repeat, each iteration in a fresh agent context.

**Usage loops**
1. PRD loop: one-sentence idea → structured PRD (LLM-interviewed) → prd.json task list with pass/fail state.
2. Implementation loop: next-failing-item selection → implement → gate (tests/typecheck) → commit → flag complete, max-iteration cap as the global stop.
3. Learning loop: every iteration appends what it learned to progress.txt and folds stable patterns into AGENTS.md so future fresh-context iterations inherit them.

**Techniques**
structured PRD-as-task-list (JSON with pass/fail per item) instead of prose todos; iteration caps as a budget; file-based cross-session memory (progress.txt + AGENTS.md) standing in for context; gate-before-commit acceptance.

**What datawatch could learn**
The `progress.txt` learning scratchpad is the missing "loop-local memory" layer between a task's session transcript and permanent BL386 story-shared memory — a per-PRD rolling scratch that survives within a PRD without any promote call.

---

## 5. aattaran — deepclaude (cost router with live model switching)

**Who / URLs**
- aattaran (DeepClaude) — https://github.com/aattaran/deepclaude

**What the harness does**
Wraps Claude Code's agent loop and swaps which model thinks underneath: a local proxy on `localhost:3200` intercepts the Anthropic API calls and routes them to DeepSeek V4 Pro, OpenRouter, Fireworks, or Anthropic proper — same UX, ~17x cheaper. The brain swap is live: a `/_proxy/mode` endpoint lets you switch backends mid-session from a slash command, and `/_proxy/cost` continuously tracks token usage and savings against the Anthropic equivalent.

**Usage loops**
1. Cost routing loop: run routine turns on the cheapest backend; `--backend anthropic` when the task needs frontier reasoning; the README's own heuristic is 80% routine / 20% hard.
2. Live-switch loop: `/deepseek` → `/anthropic` → `/openrouter` slash commands flip the active backend without restarting the session.
3. Benchmark loop: `deepclaude --benchmark` runs a latency test across all configured providers before choosing; `--cost` reports the running comparison.
4. Cache-arbitrage loop: lean on provider-side context caching (DeepSeek's auto-caching makes repeat agent-loop turns ~120x cheaper than the uncached price).

**Techniques**
API-endpoint substitution via ANTHROPIC_* env vars + proxy; mid-session backend switching through a control endpoint; per-backend cost/savings accounting; latency benchmarking as the routing signal; per-subagent model pinning via `CLAUDE_CODE_SUBAGENT_MODEL`.

**What datawatch could learn**
Deepclaude's "cost-savings vs native" readout (what this session *would* have cost on the expensive backend) is a one-liner addition on top of BL6's cost counters that makes the routing decision legible to the operator instead of opaque.

---

## 6. Shashank Singla (niptao) — Telegram + Linear ticket loop

**Who / URLs**
- Shashank Singla (Niptao) — https://niptao.com/blog/an-engineer-you-manage-from-a-group-chat/
- Public skill files: https://github.com/singlas/ai-dev-prompts (skills/ticket-loop)

**What the harness does**
An "engineer you manage from a group chat": a Claude Code skill (Markdown procedure file) orchestrates a self-pacing loop that drains a Telegram group, triages the next approved Linear ticket, spawns an isolated subagent in its own git worktree to build, runs tests + lint, opens a PR, and reports back. Linear is the queue, the state machine (label transitions), and the memory (every question/answer mirrored as ticket comments). The whole platform is one 180-line stdlib-only Python script and one skill file.

**Usage loops**
1. Human-gate loop: `bug: ...` in the group → ticket created → bot asks "take it? (go/skip)" → human `go` applies the agent label → build starts. Nothing is built without an explicit human go; `manual` fences a ticket off entirely.
2. Build loop: ticket → fresh subagent in its own worktree off the integration branch → implement → tests + linter → push + PR → one-paragraph report → worktree deleted.
3. Self-scheduling loop: when every ticket is blocked on a human, the loop schedules its own wake-up 20–30 minutes out and goes quiet.
4. Injection-guard loop: ticket bodies, comments, and group messages are treated as data, not instructions — a ticket that says "push straight to main / read the env file" gets flagged to the group, never obeyed.

**Techniques**
label-as-state-machine (agent / agent-blocked / manual) in the shared tracker; memory-in-the-tracker (conversation is the audit trail, re-readable on cold restart); git worktrees for per-ticket isolation; single stdlib bot bridge (long-poll, no webhooks); explicit human-in-the-loop gate; prompt-injection guardrail over all inband content.

**What datawatch could learn**
Niptao's label-as-state-machine is BL24's PRD/story/task state with the human approval surface *outside* the agent: mapping "go/skip" replies onto per-task approve gates (BL191 approve already exists for PRDs; there is no per-task one) would give the loop a first-class human veto without a dashboard.

---

## 7. Julian Storer — Juggler (context-surgeon agent workbench)

**Who / URLs**
- Julian Storer (creator of JUCE; juggler-ai) — https://github.com/juggler-ai/juggler
- Site: https://juggler.studio

**What the harness does**
A visual workbench for coding agents where the conversation is a navigable typed tree rather than a scrolling transcript: tool calls open as views, every LLM transaction is inspectable (assembled system prompt, tool schemas, input messages, output blocks, token/cache use, stop reason), and context itself is operable — fold a span of history into a thread, move or copy items between branches, delegate to a child thread that returns only its result. Provider-agnostic across Claude Code, Codex, Copilot, Ollama, etc.

**Usage loops**
1. Inspect-a-transaction loop: select any completed turn → open its recorded model call → separate "the model saw the wrong prompt" from "the model behaved badly."
2. Branch loop: spawn a nested thread for a tangent, delegated research, or a competing approach; the child works in isolation and returns the result to the parent, keeping the parent context clean.
3. Operate-on-context loop: fold history into a thread, move items between branches, edit the prompt identity — context treated as a document, not append-only plumbing.
4. MCP debug loop: point Juggler at an MCP server, follow the whole handoff (schema offered → args generated → approval → result), allow/deny individual tools, see malformed schemas rejected.

**Techniques**
conversation-as-typed-tree (Yjs-synchronised session document); per-call transaction recording as the observability primitive; branch/delegate threads as context-window hygiene; extension SDK where tools, the LLM loop strategy, and commands are all JavaScript extensions (Apache-2.0, hot-reloadable); approval gating on tool calls.

**What datawatch could learn**
Datawatch's session output is text + telemetry; Storer's "open the exact assembled request" is the missing per-call inspector — one queryable artifact per LLM call (prompt + tools + response + usage) that the PWA could render as a drill-down on any session, which is precisely the span-shape that synthesis.md proposes for OpenInference.

---

## 8. Nikhil Verma — harness that makes a small LLM reliable

**Who / URLs**
- Nikhil Verma (@NikhilVerma) — https://dev.to/nikhilverma/building-a-harness-that-makes-a-small-llm-reliable-5ca9
- Companion: UUID-hallucination post — https://dev.to/blog/llms-unreliable-narrators-uuid-hallucination/

**What the harness does**
A multi-turn agent on Haiku 3 whose reliability comes entirely from structural scaffolding, not model size. Two rails do most of the work: a **ref proxy** that makes raw UUIDs invisible to the model (bidirectional token aliasing with a durable session-scoped registry) and **mandatory tool contracts** that make `endTurn` impossible until required tools hit their call counts. The philosophy: assume the model will fumble, and build the catch.

**Usage loops**
1. Ref-proxy loop: outbound UUIDs → `ref_*` nicknames via the registry; inbound tool args validated against the registry; `unresolvedRefs`/`rawUuids` rejected with a *recoverable* structured error telling the model exactly what to do next; model reads it and retries the same turn.
2. Mandatory-contract loop: workflow frontmatter declares required tools + min call counts; every successful call ticks a counter; `endTurn` has a bouncer that returns a violation listing the missing tool; a stop-condition gate and a replay pin (force the next step to call the missing tool) cover the worst case.
3. Audit loop: the ref registry doubles as a complete audit trail — every entity the model touched, when first seen, when last referenced.

**Techniques**
bidirectional token aliasing (model-visible refs ↔ real entity IDs) with a persistent append-only registry; recoverable tool errors as the teaching signal (specific, actionable wording instead of "invalid input"); dual-layer end-gate (wrapper + stop-predicate); schema rewriting to *hint* (pattern/description) while the harness *enforces*; small-model substitution once correctness is structural.

**What datawatch could learn**
Verma's recoverable-error-as-prompt technique is directly applicable to BL367 auto-fix: instead of relaunching a failed task with the raw error, feed a structured "what went wrong + what to do next" tool result back into the session — and his registry-as-audit-trail is a cheap per-session "what did the agent actually touch" record datawatch's telemetry doesn't have.

---

## 9. Jeff Green — Eve V2 (persona + 40-round local agentic loop)

**Who / URLs**
- Jeff Green (jeffgreen311 / S0LF0RG3) — https://dev.to/jeffgreen311/i-built-a-local-claude-code-alternative-with-ollama-heres-how-the-agentic-loop-works-45b1
- Repo: https://github.com/JeffGreen311/eve-agent-v2-unleashed · Models: https://ollama.com/jeffgreen311

**What the harness does**
A self-hosted two-layer agent: a fine-tuned local Qwen4/8B persona model (voice and tool-calling behaviour baked into the weights, not a system prompt) handles conversation and cheap turns, while an auto-router escalates real coding work to a cloud 480B model running a 40-round tool loop (bash, file ops, grep/glob, git, web search, think). A single-HTML-file browser UI streams every token, tool call, and result live; 112 subagent definitions, 111 slash commands, and 273 skills are all markdown files the loader picks up.

**Usage loops**
1. Route loop: every message hits the auto-router — local persona model for chat/reflection, cloud agentic model only when real work is detected; context carries across the switch.
2. 40-round loop: model returns tool calls → execute → feed results back → loop; the canonical arc is write file → bash-run → read error → fix → run again → write tests → write docs, all in one turn.
3. Steer loop: a STEER input injects a mid-task correction into the next loop round without stopping execution; Stop kills the whole loop.
4. Extension loop: new subagent / command / skill = one markdown file dropped in; the routing logic picks it up with no code changes.

**Techniques**
persona fine-tuning into weights instead of prompt (survives long contexts); local/cloud model split with an auto-router; bounded-round agentic loop (40) as the safety cap; SSE token-level streaming of tool calls + results as the observability surface; markdown-as-skill-packaging at scale (112/111/273).

**What datawatch could learn**
Eve's local-chat / cloud-work split with mid-task model switch mirrors datawatch's per-PRD/per-task LLM override, but the trigger is the *router's* work-detection heuristic per message — datawatch's LLM registry routes at session spawn, and a per-step "this step is cheap, demote; this step is hard, promote" would be the missing dynamic tier.

---

## 10. joacod — nano-harness (runs as the core abstraction)

**Who / URLs**
- joacod — https://joacod.com/blog/what-i-learned-building-nano-harness/
- Repo: https://github.com/joacod/nano-harness

**What the harness does**
A local-first Electron desktop app built as a learning project around one deliberate abstraction: the **run** — a bounded attempt to satisfy one user request, carrying messages, provider calls, actions, policy decisions, approvals, events, and persisted state. On top of runs sit provider adapters, a run inspector, approval boundaries, SQLite persistence, and MCP/skills as late-arriving integrations. The design rule: "keep the core small and boring" — spec-driven dev, memory, and skills are compositions of runs, not new core primitives.

**Usage loops**
1. Plan-vs-execute model loop: strong model (GPT-5.5) for planning/architecture/review, cheaper or local model (e.g. Qwen3.6-35B via MLX) for execution work — the plan is the expensive part, execution is commodity.
2. Run-inspection loop: every provider call, action, policy decision and approval is a first-class event on the run, inspectable in the run inspector — not log-scraping.
3. Approval-boundary loop: sensitive actions require approval as a policy decision recorded on the run; the boundary is data, not code.
4. Skill-injection loop: skills are local markdown workflow packages; the open question the harness answers per-turn is *when to inject and when to leave the model alone*.

**Techniques**
run-as-first-class-abstraction (bounded attempt with typed events); provider = adapter; tool call = (input schema, execution boundary, result) contract; memory as (storage, recall, ranking, confidence, provenance, approval); small-core principle (new capabilities are compositions, never core mutations); local inference as a cost/privacy tier, not a compromise.

**What datawatch could learn**
"Is this a run or a core mutation?" is the discipline datawatch's surface area is under pressure to lose — a new MCP tool today often implies a new daemon capability. Joacod's rule (memory proposed-from-evidence and recalled-with-provenance is a *workflow over runs*, not a new layer) is a ready-made litmus test for the skills/guardrail/eval features currently competing for core status.

---

## 11. pcollins — PI-SDK profile harness (admin router + writing agent)

**Who / URLs**
- pcollins (PCollins.tech) — https://www.pcollins.tech/blog/building-my-own-agent-harness

**What the harness does**
A small personal agent harness on the PI SDK: one run loop, a system-prompt loader, a tool registry, and a session store — with the interesting work pushed into **profiles**: bundles of system prompt + tools + defaults that turn the same loop into different agents. Two working recipes: an admin-system router (knows the domain-tool map, enforces cross-domain rules like "fitness tools don't fire in the projects thread," and selectively asks permission) and a writing agent with read access to the blog corpus that drafts from its own stylistic neighbours instead of inventing a generic voice.

**Usage loops**
1. Route loop: intent in the inbound message → domain classification → only that domain's tools are live; cross-domain calls hit the profile's rules and either ask or refuse.
2. Writing loop: draft request → pull closest stylistic neighbours from the existing post corpus (frontmatter shape, tone class) → draft within the operator's actual voice constraints (no em-dashes, no padding).
3. Gate loop: tools the operator has pre-decided safe are delegated without asking; tools they want watched always ask — permission is per-action, not per-session.

**Techniques**
profile-as-(prompt + tools + defaults) bundle over a single shared loop; domain-scoped tool visibility as context segregation; corpus-grounded style matching (retrieval over one's own prior outputs) instead of generic prompting; deliberate anti-sprawl cap ("a handful of profiles, not thirty").

**What datawatch could learn**
Datawatch has per-PRD/per-task LLM and guardrail overrides but no *tool-surface* override per persona/task; pcollins's profile (prompt + tools + permission class) is exactly the shape that's missing for, e.g., a "researcher" task that may read but never write, or a "releaser" that may git-push but never edit.

---

## 12. Ed Yau — Featherbench / Ed-o-meter (one-variable eval harness)

**Who / URLs**
- Ed Yau (ed-is-ai / Kerv) — https://reinvently.co.uk/blog/building-the-ed-o-meter-llm-eval-harness/
- Harness + tasks + raw results: https://github.com/ed-is-ai/featherbench
- Leaderboard: https://reinvently.co.uk/tools/ed-o-meter/ · Results write-up: https://reinvently.co.uk/blog/glm-5-2-fable-5-gpt-5-5-eval-results/

**What the harness does**
A single-file (~1k-line) MIT-licensed Python harness that runs 28 fixed, hand-authored realworld tasks against any model via OpenRouter and grades them. The whole design constraint: **one variable changes between runs — the model**. Tasks, prompts, answer keys, checkers, rubric, routing are all pinned in source control so a score movement is provably the model's. Grading splits the floor from the ceiling: machine checkers assert the objective minimum (constraint respected, fact present, canary absent) and an LLM rubric judges the ceiling — with *every model judging every response blind* and a published judge-bias matrix to measure (not eliminate) judge self-preference.

**Usage loops**
1. Single-variable re-run loop: at every major model release, re-run the identical 28-task panel; because everything else is frozen, any delta is attributed to the model.
2. Floor/ceiling grading loop: per task, the checker asserts the verifiable minimum (author-written answer keys; negative controls like a canary string the model would emit only under a jailbreak); the rubric scores what no regex can (tone, pacing, trade-offs) — one unambiguous thing judged per task.
3. Judge-bias loop: blind multi-model judging panel produces a mean-score bias matrix per judge pair; self-assigned scores are ruled out by construction.
4. Honesty loop: every pass-rate publishes with its Wilson interval as chart whiskers; refusals count as failures (user-visible); raw JSONL is public for re-scoring.

**Techniques**
single-variable eval discipline (Goodhart-proofing via private tasks authored post-release); answer-keyed floor checkers (negation-aware where needed — the vegetarian-recipe false failure); blind judge panel + published bias matrix; canary negative controls for security tasks; width-over-depth trials with honest confidence intervals; one-endpoint routing (OpenRouter, `allow_fallbacks:false`) as the fairness control.

**What datawatch could learn**
BL259 evals run one suite against one backend with a pass/fail gate; Featherbench's one-variable discipline (pin prompts + checkers + rubric in the eval definition so a score delta is attributable) plus the published raw-results re-scoreability would make datawatch's "sweep across backends" (synthesis.md #1) a claim instead of an anecdote — and the judge-bias matrix is the missing self-reference guard on any LLM-as-verifier in BL259/BL24.

---

## 13. Allen McCabe — LLM-triage-eval (the harness that proved the LLM lost)

**Who / URLs**
- Allen McCabe (fissible, dev.to) — https://dev.to/fissible/i-built-an-eval-harness-to-prove-an-llm-worked-it-proved-the-opposite-327m
- Harness: https://github.com/fissible/llm-triage-eval

**What the harness does**
An eval harness for classifying ~4,000 legacy integration failures into a 13-category root-cause taxonomy, built to *prove* an LLM was worth using on the job. What it actually proved: deterministic rules beat the LLM (89.7% vs 87.9% on 58 hand-reviewed cases), three prompt revisions changed nothing, and the model's only defensible remaining role is writing the plain-English incident summary. Final architecture: rules + regex do classification and structured extraction; the LLM summarises; both LLM paths remain in the harness *as measured contenders* so a two-value config change can re-test them later.

**Usage loops**
1. Baseline-first loop: a weak rule-based classifier pre-tags everything → human review turns it into a 58-case golden set → that same rule set becomes the deterministic baseline the LLM must beat (the most consequential design decision in the project).
2. Prompt-version loop: every prompt is versioned (`--prompt=v1|v2|v3`), every run writes a timestamped JSON report; regressions are diffable. The loop caught a harness bug of its own — v2's flag never threaded through, and *identical token counts across runs* was the tell.
3. Judge-validation loop: 15 LLM-judged summaries re-scored by hand → faithfulness agrees (Cohen/κ-quadratic κ=0.57) but completeness doesn't (κ=0.10, retracted from the report) — LLM-as-judge validated per axis before trusting it.
4. Architecture-swap loop: the harness is the seam — swap in a frontier model when released, re-run the identical golden set, decide by data.

**Techniques**
fair-baseline discipline (a weak regex made the model look great; a details-aware one erased the edge); root-cause-over-symptom taxonomy doctrine (coherence of the label, not prompt wording, drove the only real 8-point gain: consistent doctrine lifted *both* the rules and the model); LLM-as-judge with per-axis human validation; correlated-detail enrichment (two-pass scan joining WARN blobs by correlation ID) as the single highest-leverage, entirely deterministic improvement; golden-set-as-regression-target; local-first (Ollama) for cost-honest iteration.

**What datawatch could learn**
McCabe's core finding — that the whole reliability gain came from a coherent evaluation *doctrine* (what counts as correct), not from prompts — is the argument for BL259 suites carrying explicit, versioned rubric statements per case (what "pass" means, per axis) instead of relying on grader-model vibes, and his token-count-equality bug-detection trick is a dead-simple harness self-test datawatch's eval runner could adopt (identical inputs must not produce identical traces).

---

## 14. albert-ying — Autonomous Lab (senior/junior editorial loop)

**Who / URLs**
- albert-ying (Albert / Kejun Ying) — https://github.com/albert-ying/autonomous-lab
- Site: https://autolab.kejunying.com · PyPI: https://pypi.org/project/autonomous-lab/

**What the harness does**
An MCP server that turns any senior-junior workflow into an autonomous loop inside the coding agent you already pay for (Claude Code, Codex CLI, Cursor — zero separate API keys). Two AI personas (PI + Trainee) design, execute, and revise in a loop; the human sits *above* them as editor/decision-maker: accept, request revisions, reject. Runs 24-hour uninterrupted sessions with resume; multi-agent mode gives each role its own context window on the existing CLI subscriptions, with automatic fallback to single-agent.

**Usage loops**
1. Role-alternation loop: `autolab_next` → AI acts as the role (PI or Trainee) → `autolab_record` → `lab_meeting` (pause for human feedback) → repeat; the meeting pause is the human editorial gate.
2. Editorial-gate loop: when work is ready, `autolab_editorial` blocks for the human's decision (accept / revise / reject); `autolab_editor_act` executes an AI fallback decision if the human defers.
3. Skill-acquisition loop: a character needs a capability → tiered cascade (character marketplace → GitHub → ToolUniverse 1000+ tools → auto-generate SKILL.md) → installed without breaking the loop, so research never stalls on a missing skill.
4. Multi-trainee loop: a PI fans out parallel trainees (each own context window, own focus area, own provider — Claude for data, Codex for writing, Opus for code) via asyncio.gather; one failing does not stop the others.

**Techniques**
human-in-the-loop editorial gate baked into the protocol (`lab_meeting`); multi-agent on *subscription* CLIs (cost = $0 beyond existing subscriptions) rather than raw API keys; skill-containers (SKILL.md bundles per character) and runtime skill auto-acquisition; session persistence + `autolab_resume` for 24-hour runs; verified citations via CrossRef as a hallucination guard; YAML character profiles (personality, expertise, goals, tools).

**What datawatch could learn**
Autonomous Lab's "editorial gate" is a stronger shape than datawatch's binary approve/reject at PRD level: a three-way accept / revise-with-note / reject decision that can also be *delegated to an AI fallback* (`editorial_act`) when the human is absent. That delegation-with-fallback is exactly what BL191's prd_approve currently cannot do — approve, but with a note, or auto-approve after N minutes of quiescence.

---

## 15. Detrol — Quorum (method-driven multi-model debate harness)

**Who / URLs**
- Detrol — https://github.com/Detrol/quorum-cli
- Web: https://quorumai.dev · MCP: `quorum-mcp-server`

**What the harness does**
A terminal "AI war room" that takes a question and runs a structured debate across selected models (GPT, Claude, Gemini, Grok, local Ollama) using one of seven **formal methods** — Standard, Oxford (FOR/AGAINST), Advocate (devil's-advocate on the emerging consensus), Socratic (rotating questioner), Delphi (anonymous iterative estimation), Brainstorm (diverge/build/converge), and Tradeoff (criteria-weighted scoring) — then synthesises a consensus answer. Method choice is AI-advised ("Tab" analyses the question and ranks methods with confidence scores).

**Usage loops**
1. Method-selection loop: question in → Tab → AI advisor analyses and recommends method + confidence (Delphi 95% / Tradeoff 70% / Standard 60%) → operator picks → discussion runs.
2. Structured-debate loop: each method phases the exchange (answer → critique → discuss → position → synthesise for Standard; opening/rebuttal/closing for Oxford) instead of freeform round-robin.
3. Consensus-detection loop: the harness watches for convergence (e.g. CONSENSUS REACHED, all agents agreed, N messages) and ends the loop early rather than burning round budget.
4. Local-model VRAM loop: cloud APIs run in parallel; Ollama models auto-run sequentially to prevent VRAM competition (`QUORUM_EXECUTION_MODE=auto`) — the scheduling is automatic, not a config hunt.

**Techniques**
method-as-phased-protocol (consensus-seeking vs devil's-advocate vs anonymous estimation are structurally different, not prompt variants); rotating roles (Socratic questioner, Delphi anonymisation); AI method advisor as a router over *debate protocols*, not models; synthesizer-rotation (first/random/rotate) to avoid synthesis-bias to one vendor; Ollama-sequence scheduling as a hardware-aware guardrail; compact-output-default (synthesis only) to save downstream context when called from an agent.

**What datawatch could learn**
Datawatch's BL260 Council debates models against a fixed persona set; Quorum's insight is that the *method* (how disagreement is structured) is an operator choice that should route on the question type — a binary architecture decision wants Oxford/Tradeoff, a "we may be groupthink-ing" review wants Advocate, a cost-estimate wants Delphi. A `mode` parameter that selects the debate protocol (personas stay constant) is a cheap, high-value addition to council_run.

---

## 16. mvschwarz — OpenRig (YAML topologies over coding-agent rigs)

**Who / URLs**
- mvschwarz (OpenRig) — https://github.com/mvschwarz/openrig
- Site/docs: https://openrig.dev · Blog: https://esoteric.run/blog/why-i-built-openrig

**What the harness does**
A local daemon + CLI + MCP server + TUI that manages the *team* a set of coding agents forms, not the agents themselves: define a RigSpec in YAML (pods, members, edges, continuity policies, culture file), boot it with `rig up` into tmux sessions, discover and adopt existing Claude Code / Codex sessions into a managed rig, snapshot the whole topology on `rig down` and restore by name, and reshape it live (`rig expand`/`shrink`/`launch`/`remove`). Every agent can manage its own topology through 17 MCP tools.

**Usage loops**
1. Topology-boot loop: `rig up first-project` → tmux sessions + harnesses + startup files + readiness checks in one command → `rig ps --nodes --rig …` to verify health.
2. Owner/checker loop: the starter rig separates an `dev-owner` seat from a `dev-check` seat — the owner implements a bounded change, the checker independently reviews the exact candidate, and the operator reads both before the next change (trust-but-verify as a *topology property*, not a prompt).
3. Snapshot/recovery loop: `rig down --snapshot` captures full topology state → reboot → `rig up <name>` restores per-node (resumed / fresh / failed), so a machine restart is not a work loss.
4. Self-management loop: agents themselves call `rig_up`, `rig_send`, `rig_chatroom_send` via MCP — the topology is a shared, mutable, inspectable resource, not a one-time setup.

**Techniques**
declarative agent-team YAML (RigSpec) with pods as bounded-context groups (shared memory + mutual context maintenance); continuity policies as first-class; agent discovery+adoption of pre-existing tmux sessions instead of forcing new ones; snapshot/restore with per-node outcome reporting; culture file (CULTURE.md) setting group norms (exploratory for research rigs, trust-but-verify for implementation rigs); RigBundle (portable, SHA-256-integrity-checked agent-team archive); `secrets-manager` rig — a HashiCorp Vault operated by a specialist agent as a worked example of agent-managed infrastructure.

**What datawatch could learn**
OpenRig's owner/checker seat separation is datawatch's BL25/BL366 verifier pattern lifted from prompt-level to *topology-level*: two sessions, two contexts, explicit edge between them, the checker's job wired into the spec rather than negotiated per prompt. Datawatch's orchestrator graph has PRD+guardrail nodes but no "adversarial-review" rig primitive — a `rig: adversarial-review` starter (like OpenRig ships) would be a near-free template in BL117.

---

## 17. Domenic Denicola — disposable-VM + tailnet agentic setup

**Who / URLs**
- Domenic Denicola — https://domenic.me/agentic-coding-setup/
- Source repo: https://github.com/domenic/domenic.me

**What the harness does**
A stitched-together personal harness whose end state is "frontier models fix production bugs from my phone, on a train": a disposable Linux VM (always-on home desktop, nested-virt enabled) hosts parallel Claude Code + Codex CLI agents, each in its own **git worktree**, each standing up its own dev server exposed via a **Portless** Tailscale-URL; a Tailscale tailnet connects desktop, laptop, and phone to the VM; chezmoi syncs AGENTS.md + skills + harness config across all three machines; and both agents are set to zero-approval yolo-mode *behind* the VM blast radius, with GitHub push-everything as the safety net.

**Usage loops**
1. Mobile-bug-fix loop: notice a bug in a live deployment → phone app → new remote session over Tailscale → agent fixes + smoke-tests with Playwright + hands back a tailnet preview URL → verify from phone → "open a PR" → skim diff → merge → CI/deploy — usually before the train arrives at the station.
2. Parallel-worktree loop: multiple agents on the same project, each in its own worktree (native "worktree box" in the ChatGPT app), own dev server on its own tailnet port — no port collisions, no stomping, isolated node_modules.
3. Preview-server loop: agents do NOT run `npm run dev` directly; an AGENTS.md rule forces them through a `tportless` wrapper that publishes a tailnet HTTPS URL instead of a localhost IP — secure context, private tailnet, collision-free.
4. Config-sync loop: all harness config (AGENTS.md, skills, ~/.claude, ~/.codex) lives in a private dotfiles repo under chezmoi — the VM is disposable, the *shape of the harness* is versioned and portable.

**Techniques**
disposable-VM blast-radius isolation as the enabler for `bypassPermissions`/`danger-full-access` (zero-approval autonomy is a *safety architecture* property, not a risk accepted); Tailscale-as-magic-network (SSH + HTTPS certs + Taildrive) so sessions, dev servers, and file moves all ride one private network; git worktrees as the parallelism primitive (agents do the sync, .env copying, dependency install — the "annoying" part of worktrees is what the agents are good at); push-everything-to-GitHub as the crash-recovery net; Portless-as-secure-preview-proxy as a single-file CLI rule in AGENTS.md; mobile-app-as-thin-client over the tailnet.

**What datawatch could learn**
Domenic's whole stack is datawatch's *environment* problem solved with existing parts — the missing piece in datawatch today is the "preview server for an agent's work" loop (an agent standing up, exposing, and handing back a URL the operator can poke; nothing in BL24's loop does this) and the AGENTS.md-as-distributed-config pattern (datawatch's AgentSettings exists per Profile but has no sync/diff/rollback story — chezmoi + a private repo is the 20-line fix).

---

## Coverage gaps (vs. methodology.md)

Cross-referencing the cohort against the methodology's six target categories:

| Methodology category | Cohort representation |
|---|---|
| **Eval frameworks** | **Covered well** — Featherbench (single-variable, judge-panel), fissible (baseline-vs-LLM, doctrine), ralphctl (gate-as-eval). |
| **Custom RAG pipelines** | **Absent** — zero cohort members build retrieval/grounding harnesses. Reason: the individual-builder ecosystem gravitates toward *coding* agents and *eval* harnesses; RAG is dominated by the vendor frameworks already in `examples-catalog.md` (Haystack, RAGAS, DeepEval). This is datawatch's `docs_search` grounding-metric gap (synthesis.md #3) and it has no individual-builder reference to copy from — the RAG/grounding gap will need to be filled by adopting a vendor pattern (RAGAS claim-decompose → LLM-judge) rather than a practitioner's loop. |
| **Guardrail systems** | **Covered** — verma (structural ref-proxy + mandatory-contract enforcers), niptao (injection guard as data-not-instruction), quorum (VRAM/sequential scheduling as a runtime guardrail). |
| **Agent orchestration** | **Covered heaviest** — openrig (topology), quorum (method), autonomous-lab (role), ralphctl (task-graph), ghuntley (subagents), domenic (worktrees). |
| **Verification & test harnesses** | **Covered** — ralphctl (diff-scoped gates), openrig (owner/checker seat), fissible (harness-caught-harness-bug), ghuntley (backpressure). |
| **Session/state management** | **Covered** — openrig (snapshot/restore), juggler (tree/branch), domenic (worktrees+chezmoi), niptao (memory-in-tracker), autonomous-lab (`autolab_resume`). |

**Net under-represented:** **RAG/grounding** is the one methodology category with zero individual-builder coverage in this cohort, and it is also the one category datawatch's own gap list (synthesis.md #3) flags. Secondarily, **observability/provenance** sits outside methodology.md's six categories entirely and no cohort member builds a tracing/lineage harness — that gap (synthesis.md #2) is only addressed by vendor tools (Phoenix, Langfuse, OpenInference) in `examples-catalog.md`. The cohort confirms where the *individual* harness ecosystem is actively innovating (loops, gates, cost, guardrails, topologies) and where it has not yet reached (RAG, lineage/observability) — the latter two are the categories where datawatch must look to vendor patterns, not practitioner blogs.

---

*No code was written for this pass. One file created: `docs/plans/harness-research/case-studies.md`. Every URL above was status-checked 2026-09-19; the 17 builders listed are the curators' strongest individual harness-builders meeting all three inclusion criteria.*

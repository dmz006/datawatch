# Harness Research — Recurring Usage Patterns (Cross-Builder)

**Date:** 2026-09-19 · **Status:** Draft · **Input:** `case-studies.md` (17 curated builders)
**Companion:** `candidates.md`, `methodology.md`, `synthesis.md`

Method: read the 17 case studies in `case-studies.md` end-to-end, then pulled every
technique that **more than one builder independently arrived at**. A technique only
qualifies for a named pattern if it appears in **2 or more distinct builders**; single
occurrences go to the one-off appendix (last section).

Each pattern lists a count of the 17 builders it is seen in; patterns are ordered from
most to least common across the catalog. Evidence is quoted/paraphrased from the cited
case study, with that study's anchor (`#N — builder`) so it can be re-opened.

---

## 1. Memory as context compression with scope + promote

Builders: **5** (Huntley, Akhil, Niptao, Storer, joacod)

Durable memory is treated as a *stand-in for the context window* rather than a database:
state that must survive beyond one fresh-context turn is written to a small, re-loadable
artifact (a file, a tracker, an editable tree) and the artifact is what the next turn
reads. The "harness" is the rule that decides *what gets written, where it is scoped,
and who is allowed to promote it up*, which is exactly the scope/promote axis datawatch's
BL386 already models. It exists because every builder hit the same wall — a long task
exceeds one context window — and the cheap fix that recurs is "write it down where the
next fresh-context call can find it."

Seen in:
- **#1 — Geoffrey Huntley (Ralph):** "disk-as-memory (git + plan files)"; "when the agent learns how to build/run the project, it updates AGENT.md itself" — the prompt invariants are memory the agent writes back to itself.
- **#4 — Akhil (PRD→JSON):** "file-based cross-session memory (progress.txt + AGENTS.md) standing in for context"; "every iteration appends what it learned to progress.txt and folds stable patterns into AGENTS.md so future fresh-context iterations inherit them."
- **#6 — Niptao (ticket loop):** "memory-in-the-tracker (conversation is the audit trail, re-readable on cold restart)" — the Linear ticket *is* the scoped memory.
- **#7 — Storer (Juggler):** "context itself is operable — fold a span of history into a thread, move or copy items between branches" — compression is a first-class operation on a typed tree, not append-only.
- **#10 — joacod (nano-harness):** "memory as (storage, recall, ranking, confidence, provenance, approval)" — memory is a workflow over runs, never a new core layer.

## 2. Human-in-loop approval gate

Builders: **5** (Niptao, albert-ying, Storer, pcollins, mvschwarz)

The autonomous loop is *deliberately gated* at one or more points where a human say-so
is required before a consequential action (build, ship, mutate state) proceeds. The
gate is a protocol step — a blocking call, a label transition, a per-action prompt, an
operator who reads both outputs — not a dashboard afterthought, so a run cannot quietly
do the thing it was not authorised to do. It recurs because the builders are shipping
real production work with real blast radius, and the one thing they all kept was a
place where a human can say no.

Seen in:
- **#6 — Niptao:** "`manual` fences a ticket off entirely"; "Nothing is built without an explicit human go" — the `go/skip` reply on the group chat is the gate.
- **#14 — albert-ying (Autonomous Lab):** "`autolab_editorial` blocks for the human's decision (accept / revise / reject)"; the `lab_meeting` pause is "the human editorial gate."
- **#7 — Storer:** "approval gating on tool calls"; "allow/deny individual tools" — the loop stops and hands the decision to the operator per-action.
- **#11 — pcollins:** "tools the operator has pre-decided safe are delegated without asking; tools they want watched always ask — permission is per-action, not per-session."
- **#16 — mvschwarz (OpenRig):** "the operator reads both before the next change (trust-but-verify as a topology property)" — a human sits *above* owner/checker.

## 3. Cost-based routing / budget caps

Builders: **5** (ralphctl, deepclaude, McCabe, albert-ying, Eve)

The harness explicitly spends the cheap model on routine turns/steps and the expensive
frontier model only when the step is hard, and it tracks the resulting spend as a first
class signal. This can be a static tier, a live mid-session switch, an escalation ladder
on a stall, or simply "run local to keep it free." It recurs because unbounded frontier
tokens are the single largest number on any harness's bill, and every builder who
measured it found the 80/20 split was real and exploitable.

Seen in:
- **#2 — ralphctl:** "20 cost-tiered presets… cheap generator behind a top-tier evaluator; on a stall the harness climbs the model ladder one rung at a time carrying the critique upward."
- **#5 — aattaran (DeepClaude):** "run routine turns on the cheapest backend; `--backend anthropic` when the task needs frontier reasoning; the README's own heuristic is 80% routine / 20% hard."
- **#13 — McCabe:** "local-first (Ollama) for cost-honest iteration."
- **#14 — albert-ying:** "multi-agent on subscription CLIs (cost = $0 beyond existing subscriptions) rather than raw API keys."
- **#9 — Eve (Jeff Green):** "local persona model for chat/reflection, cloud agentic model only when real work is detected."

## 4. Session lineage + snapshot/rollback

Builders: **4** (ralphctl, albert-ying, mvschwarz, Denicola)

A run is checkpointed (state, branch, topology, or the whole repo) so that a crash, a
restart, or a bad turn can be walked backward instead of lost. The persistence is *typed*
— a branch, a snapshot name, a resume token, a pushed commit — and it is the operator's
(or the harness's) escape hatch when the loop goes wrong. It recurs because long
autonomous runs *will* be interrupted (machine reboots, OOMs, a bad edit) and the only
robust answer is "I can get back to where I was."

Seen in:
- **#2 — ralphctl:** "State (sprint, branch, per-task progress) persists so an interrupted run resumes"; "crash-resume from persisted state."
- **#14 — albert-ying:** "Runs 24-hour uninterrupted sessions with resume"; "`autolab_resume` for 24-hour runs."
- **#16 — mvschwarz:** "`rig down --snapshot` captures full topology state → reboot → `rig up <name>` restores per-node (resumed / fresh / failed), so a machine restart is not a work loss."
- **#17 — Denicola:** "push-everything-to-GitHub as the crash-recovery net" behind zero-approval mode.

## 5. Guardrail chains at the model boundary

Builders: **4** (Verma, xr0am, Quorum, Niptao)

Constraints are enforced *around* the model rather than asked of it: tool permissions
are tiered, an end-of-turn gate can veto, a runtime scheduler sequences local models to
protect VRAM, and in-band text is treated as data not instruction. The model is allowed
to try, and the harness blocks or reformats the request/response so the bad outcome
cannot happen. It recurs because the builders have all observed the model will, left
alone, do the thing it shouldn't (push to main, read the env file, double-book VRAM),
and the durable fix is a boundary, not a plea.

Seen in:
- **#8 — Verma:** "mandatory tool contracts that make `endTurn` impossible until required tools hit their call counts"; "dual-layer end-gate (wrapper + stop-predicate)."
- **#3 — xr0am:** "Tool access is tiered (7 core tools by default, expanded on demand)… optional container sandbox per loop."
- **#15 — Quorum:** "Ollama models auto-run sequentially to prevent VRAM competition (`QUORUM_EXECUTION_MODE=auto`) — the scheduling is automatic" as a "hardware-aware guardrail."
- **#6 — Niptao:** "prompt-injection guardrail over all inband content"; "a ticket that says 'push straight to main / read the env file' gets flagged to the group, never obeyed."

## 6. Verifier inner-loop (a second LLM checks the first)

Builders: **4** (ralphctl, mvschwarz, Featherbench, McCabe)

The model that produces the artefact is *not* the one that signs off on it. A separate
model (or a checker + a rubric) grades the output against explicit criteria, and on
failure the *specific critique* goes back to the generator to retry, capped. It recurs
because self-evaluation is measurably unreliable — a model praising its own code or
summary is the exact failure mode — and the only trustworthy shape is an independent
grader with a written rubric and a bounded retry count.

Seen in:
- **#2 — ralphctl:** "an independent model grades the change against the task's verification criteria; failure returns the specific critique to the generator; after maxAttempts… the task flags `blocked`, never `done`."
- **#16 — mvschwarz:** "the owner implements a bounded change, the checker independently reviews the exact candidate, and the operator reads both" — trust-but-verify "as a topology property, not a prompt."
- **#12 — Featherbench:** "machine checkers assert the objective minimum… and an LLM rubric judges the ceiling."
- **#13 — McCabe:** "15 LLM-judged summaries re-scored by hand → faithfulness agrees… but completeness doesn't… LLM-as-judge validated per axis before trusting it."

## 7. Self-hosted failover / mid-run model swap

Builders: **4** (DeepClaude, Eve, ralphctl, albert-ying)

The active model is swappable *while the loop is running* — a mid-session backend flip,
a per-message router, an escalation on a stall, or an automatic fallback to a cheaper/
local model. The session's context carries across the swap. It recurs because a single
provider/model is both a cost line and a single point of failure, and the builders who
ran long loops wanted the loop to survive the model going down or the budget going out.

Seen in:
- **#5 — aattaran:** "`/_proxy/mode` endpoint lets you switch backends mid-session from a slash command"; "mid-session backend switching through a control endpoint."
- **#9 — Eve:** "a single… auto-router escalates real coding work to a cloud 480B model… context carries across the switch."
- **#2 — ralphctl:** "on a stall the harness climbs the model ladder one rung at a time carrying the critique upward."
- **#14 — albert-ying:** "multi-agent mode… with automatic fallback to single-agent."

## 8. Audit trail of every model call

Builders: **3** (Verma, Storer, joacod)

Every request sent to a model (prompt, tools, messages, usage, stop reason) is recorded
as a queryable artifact, and the registry/event stream doubles as the record of *what
the agent actually touched*. The trail is not a log for humans to grep — it is read back
by the harness (to validate, to resume, to decide) and by the operator (to inspect the
exact assembled request). It recurs because when a multi-turn loop misbehaves, "open the
exact call" is the only way to separate "the model saw the wrong prompt" from "the model
behaved badly."

Seen in:
- **#8 — Verma:** "the ref registry doubles as a complete audit trail — every entity the model touched, when first seen, when last referenced."
- **#7 — Storer:** "every LLM transaction is inspectable (assembled system prompt, tool schemas, input messages, output blocks, token/cache use, stop reason)."
- **#10 — joacod:** "every provider call, action, policy decision and approval is a first-class event on the run, inspectable in the run inspector — not log-scraping."

## 9. Fresh-context-per-iteration + disk-as-state

Builders: **2** (Huntley, Akhil)

Instead of compaction within one growing window, each iteration *starts clean* and
re-derives its task from durable state on disk. The window is deliberately disposable;
the files (plan, flags, learnings) are the memory and the only thing that must be
consistent. It recurs as the Ralph pattern's signature move: it resets over compaction,
which sidesteps context-rot in long runs that a single long window cannot.

Seen in:
- **#1 — Huntley:** "fresh-context re-init per iteration (reset over compaction)"; "the context window is deliberately disposable."
- **#4 — Akhil:** "repeat, each iteration in a fresh agent context."

## 10. Structured task-dependency DAG

Builders: **2** (xr0am, ralphctl)

A free-form ask is first decomposed into a *machine-readable* task graph with explicit
dependency edges, and the scheduler only releases a task once its parents are done. The
DAG is the coordination primitive — it replaces merge-conflict avoidance and "which
task can I start?" with a `safe-to-start`/`ready` predicate. It recurs because two
builders both found that parallel agents are only safe when they agree on what is
already done, and a dependency list is the cheapest way to encode that.

Seen in:
- **#3 — xr0am:** "a PRD parsed into tasks.json with explicit dependency arrays… the agent cannot start a dependent task until its parents complete"; "safe-to-start querying as the inter-agent lock."
- **#2 — ralphctl:** "decomposes a plain-language sprint into a dependency-ordered task graph (waves of independent tasks)."

## 11. Eval-sweep single-variable (before/after)

Builders: **2** (Featherbench, McCabe)

A fixed panel of tasks + prompts + checkers is pinned in source control so that the
*only* thing that varies between runs is the model (or the prompt version), and the
whole point is re-running the identical panel to attribute any score delta. Golden sets
and baselines make a change before/after measurable rather than anecdotal. It recurs
because both builders hit the same trap — a "win" that was really a changed harness or
a lucky prompt — and the fix is to freeze everything else and let only the variable of
interest move.

Seen in:
- **#12 — Featherbench:** "one variable changes between runs — the model… any score movement is provably the model's"; "single-variable re-run loop: at every major model release, re-run the identical 28-task panel."
- **#13 — McCabe:** "a weak rule-based classifier pre-tags everything → human review turns it into a 58-case golden set"; "swap in a frontier model when released, re-run the identical golden set, decide by data."

## 12. Multi-agent debate / council

Builders: **2** (Quorum, albert-ying)

Two or more models (or named roles) *argue* a question through a structured protocol —
answer → critique → discuss → synthesise, or PI↔Trainee role alternation — and converge
on one output, with the method of disagreement chosen deliberately. It recurs because a
single model's answer is only as good as its blind spots, and forcing a structured
counter-position (devil's advocate, a trainee who challenges the PI) surfaces the gaps a
one-shot reply would have buried.

Seen in:
- **#15 — Quorum:** "runs a structured debate across selected models… using one of seven formal methods… then synthesises a consensus answer."
- **#14 — albert-ying:** "Two AI personas (PI + Trainee) design, execute, and revise in a loop"; "a PI fans out parallel trainees (each own context window… own provider)."

---

## Appendix — One-off patterns (single builder, did not reach the 2+ threshold)

Notable techniques that appeared in **exactly one** catalog entry. They are recorded
because they are concrete and reusable, but they do not yet qualify as a *recurring*
cross-builder pattern.

- **Oracle/rescue model** — **#1 Huntley.** "oracle (second model) for planning and rescue (e.g. compiling error wall → Gemini plans recovery)." A specific *trigger*-driven second model, distinct from the tiered failover ladder (see pattern 7).
- **Persona fine-tuning into weights** — **#9 Eve.** "persona fine-tuning into weights instead of prompt (survives long contexts); voice and tool-calling behaviour baked into the weights, not a system prompt."
- **Corpus-grounded style matching** — **#11 pcollins.** "pull closest stylistic neighbours from the existing post corpus… draft within the operator's actual voice constraints (no em-dashes, no padding)" — retrieval over one's own prior outputs instead of generic prompting.
- **Blind judge bias matrix + confidence intervals** — **#12 Featherbench.** "with every model judging every response blind and a published judge-bias matrix"; "every pass-rate publishes with its Wilson interval as chart whiskers."
- **Token-count-equivalence harness bug-detection** — **#13 McCabe.** "v2's flag never threaded through, and identical token counts across runs was the tell." A dead-simple harness self-test.
- **Prompt versioning + replay** — **#13 McCabe.** "every prompt is versioned (`--prompt=v1|v2|v3`), every run writes a timestamped JSON report; regressions are diffable." Prompts are a versioned artifact with reproducible re-runs, not a mutable string.
- **Red-team / adversarial sweeps (canary negative controls)** — **#12 Featherbench.** "negative controls like a canary string the model would emit only under a jailbreak"; also **#8 Verma's** "assume the model will fumble, and build the catch" stance, and **#15 Quorum's** Advocate method (devil's-advocate on the emerging consensus) — each a single builder, not a recurring pattern.
- **Tiered skill auto-acquisition** — **#14 albert-ying.** "tiered cascade (character marketplace → GitHub → ToolUniverse 1000+ tools → auto-generate SKILL.md)… installed without breaking the loop."
- **Complexity-scored subtask expansion** — **#3 xr0am.** "score each task 1–10, then expand into subtask counts proportional to score; each subtask carries its own dependency array" — LLM-as-prior over workload.
- **Recoverable-error-as-teaching-signal** — **#8 Verma.** "`unresolvedRefs`/`rawUuids` rejected with a recoverable structured error telling the model exactly what to do next; model reads it and retries the same turn."
- **Disposable-VM blast-radius + tailnet preview** — **#17 Denicola.** "zero-approval autonomy is a safety-architecture property, not a risk accepted"; a per-agent tailnet HTTPS preview URL handed back to the operator. Distinct from generic sandboxing: the *environment* is the guardrail.
- **AI method advisor over debate protocols** — **#15 Quorum.** "Tab analyses the question and ranks methods with confidence scores" — a router over *how to disagree*, not over which model to call.
- **Run-as-first-class-abstraction / small-core discipline** — **#10 joacod.** "the design rule: keep the core small and boring… memory, skills, spec-driven dev are compositions of runs, not new core primitives." A litmus test for surface area, not a loop.

---

*No code was written for this pass. One file created: `docs/plans/harness-research/usage-patterns.md`. 12 recurring patterns (all 2+ builders) plus one-off techniques, ordered most→least common across the 17-builder catalog in `case-studies.md`.*

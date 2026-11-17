---
docs:
  index: true
  topics: [claude-code, model, sonnet, haiku, opus, fable, daemon default, worker, verifier, judge, planner, decompose, mechanical, premium, explicit model policy]
exec_steps:
  - tool: autonomous_config_get
    description: Read the global planning, execution and verification backend/model keys that sit at the bottom of the model resolution chain
    args: {}
    read_only: true
---

# Explicit model policy: a Claude Code session always starts with a named model

A Claude Code session that nobody gave a model used to start on the backend's
**built-in default** — a choice the daemon neither knows nor records. On an
install where that built-in default is a premium (Opus-class) model, an
autonomous worker, verifier, guardrail judge or planner could spend that
premium model without anyone having named it. The explicit-model policy
removes that case: every autonomous launch path resolves its Claude Code
model **explicitly**, so the model that runs is always one the daemon can
name.

The policy is fixed daemon behavior. There is **no setting** to turn it on or
off and no key that changes the default models; it applies after the whole
existing resolution chain has run.

## The three rules

1. **A model the operator named always passes through unchanged** — whatever
   level named it: a task, story, PRD or Automaton LLM, an Automation or
   global default, a first-attempt pair, a failure-escalation ladder step, a
   burn-guard route, a planner rule, or a judge pin. This includes a premium
   Opus or Fable model: those are permitted only because the operator named
   them, and the policy never changes a name.
2. **Nothing named a model, and the backend is `claude-code`: the daemon
   default applies.** That is `sonnet` for normal work, and `haiku` for the
   mechanical task types (see below). The session starts with that explicit
   model, never with the backend's built-in default.
3. **Every other backend is untouched.** `pi`, `pi-prompt` and `ollama` resolve
   exactly as before (those backends still refuse to start without a model).

## The four launch paths

| Path | When the policy applies | Daemon default |
|---|---|---|
| **Worker** — the task session | After task, story, PRD, Automaton, Automation, global and burn-guard resolution leave the model empty on a `claude-code` backend | `sonnet`; `haiku` when the task's type is a mechanical type |
| **Verifier** — the verification LLM session | After task, story, Automaton, Automation and global (including pool and policy) resolution leave it empty on a `claude-code` backend | `sonnet` |
| **Guardrail judge** — the review call | A judge whose PRD and task levels name no model, on a `claude-code` judge backend | `sonnet` |
| **Planner** — decompose and planning sessions | After the Automaton's planning fields, routing and the global planning model leave it empty on a `claude-code` backend | `sonnet` |

**Mechanical task types.** The eight types the default task-routing table
routes to the mechanical row: `docs_set`, `changelog_entry`,
`parity_issue_draft`, `bounded_load_step`, `version_note`, `locale_strings`,
`formatting_cleanup` and `tests`. Only the worker path uses the mechanical
`haiku` row: judging, verifying and planning are never mechanical work, so
those paths always take `sonnet`.

**The judge changed the most.** Before the policy, a guardrail judge on the
`claude-code` backend with no model named anywhere was a loud failure: the
daemon recorded a non-retried `no guardrail model` block verdict instead of
launching a judge. Now that same unconfigured judge starts on `sonnet`. The
guardrail is judged rather than skipped, and the model it runs on is still an
explicit one. A judge pin that names a model the backend cannot serve is
still a loud failure, exactly as before.

## What does not change

- **Premium models are still opt-in.** `sonnet` and `haiku` are never premium.
  Opus and Fable class models appear in a launch only because the operator
  named them at some level — the policy never emits one.
- **The resolution order is unchanged.** The policy is the last step of the
  chain, and only on the `claude-code` backend, and only when the model is
  still empty.
- **The validators are unchanged.** A model-less first-attempt entry for
  `pi`, `pi-prompt` or `ollama` is still refused; a model-less `claude-code`
  entry was always accepted and now simply resolves to the daemon default at
  launch.
- **Child inheritance and task-routing rules are unchanged.** A child of a
  Sonnet or Haiku parent is still never put on Opus or Fable by inheritance,
  and a routing row still needs `allow_premium_model` to name one.

## Reading it back

- **Worker.** The task's LLM and its attempt records name the worker backend
  and model that ran. A launch that took the daemon default ran on `sonnet`
  (or `haiku` for a mechanical task), visible on the attempt the way any
  other resolved model is.
- **Verifier.** The PRD's `verifier_backend` / `verifier_model` fields show
  the verification LLM the daemon resolved.
- **Judge.** The judge's decision log entry names the backend and model it
  ran with.
- **Planner.** The decompose and planning records name the planner that ran.

## See also

- [Task routing](automata-task-routing.md) — the table that maps task types
  to workers; the shipped default row for code work is `claude-code` /
  `sonnet`.
- [Failure escalation](failure-escalation.md) and
  [First-attempt worker](failed-attempt-cost.md) — the per-kind pairs and
  ladders that sit above the policy in the worker chain.
- [Judge policy](judge-policy.md) — the guardrail judge's routing, time limit
  and hold.
- [Autonomous planning](autonomous-planning.md) and
  [Planning routing](automata-planning-routing.md) — the planner's backend
  and model.

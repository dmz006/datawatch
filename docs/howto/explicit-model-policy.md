---
docs:
  index: true
  topics: [claude-code, model, sonnet, haiku, opus, fable, daemon default, worker, verifier, judge, planner, decompose, mechanical, premium, explicit model policy, refusal, session record, restart]
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
name — and a direct start that still arrives with no model is **refused
outright** instead of being allowed to run on whatever the operator's
account picks, so the model a session ran on is always one the daemon can
name and record.

The policy is fixed daemon behavior. There is **no setting** to turn it on or
off and no key that changes the default models; it applies after the whole
existing resolution chain has run.

## The rules

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
4. **A start on `claude-code` that still arrives with no model is refused,
   never defaulted.** The daemon will not fall back to the operator's
   account default — or upgrade the model on its own — so it rejects the
   launch up front instead of letting one run under an unrecorded model.

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

## A direct start with no model named is refused

The four paths above are the *autonomous* launch paths, and on them a model-
less `claude-code` entry resolves to the daemon default (`sonnet`, or `haiku`
for a mechanical task). A **direct** start — the CLI `session new`, a chat
`new:` command, a REST `POST /api/sessions/start`, an MCP `session_start`, or
a start from the PWA — names no model when the operator gave none, and the
policy covers that case with a refusal instead of a guess:

- **Refused before anything exists.** A `claude-code` start with no explicit
  model is rejected up front: no tracker, no session record and no tmux pane
  are created, and the backend's CLI is never run under the operator's
  account default. The error says so in plain terms and points at the fix:
  name a model (e.g. `"sonnet"` or `"haiku"`) and try again. The message
  lists every place a model can be named: the `datawatch session new --model
  <name>` flag, the `model` field of `POST /api/sessions/start`, the MCP
  `start_session` `model` argument, the chat `new: model=<name>` token, and the
  LLM row (`datawatch llm models add <llm> --model <name>`).
- **A direct start resolves a model before it is refused.** When the start
  request names no model, the daemon looks for one in this order and uses the
  first that is set: (1) the model on the request itself; (2) the model of the
  resolved LLM row (the row's `model`, else the first model it lists); (3) the
  daemon's global autonomous planning model, when the planning backend is
  `claude-code` (or unset). Only when all three are empty is the start
  refused. The chain applies only to `claude-code` starts; every other backend
  is untouched. A model found by (2) or (3) is recorded on the session exactly
  as if the operator had named it.
- **The fix is to name the model.** An alias from the daemon default set or a
  full registry name, given where the surface takes one (the LLM entry / model
  field of the start request, or the backend's model flag). A model named this
  way is exactly the rule-1 case: it passes through unchanged.
- **The backend never invents a model.** The `claude-code` backend passes
  `--model` to the CLI only when a model was set; with none set it would
  launch under the account default — the case the refusal exists to block.
- **The record stays honest.** When a model is named, the persisted session
  record carries **exactly** the value the backend passes to the CLI as
  `--model` — what you see in the session record is what the CLI ran with.
- **A restart keeps the same model.** The backend is a shared object whose
  model field another session may have overwritten, so on restart the daemon
  re-injects the model the session's record carries before relaunching. A
  restarted `claude-code` session keeps passing exactly that `--model` value,
  never the account default.

## What does not change

- **Premium models are still opt-in.** `sonnet` and `haiku` are never premium.
  Opus and Fable class models appear in a launch only because the operator
  named them at some level — the policy never emits one.
- **The resolution order is unchanged.** The policy is the last step of the
  chain, and only on the `claude-code` backend, and only when the model is
  still empty.
- **The validators are unchanged.** A model-less first-attempt entry for
  `pi`, `pi-prompt` or `ollama` is still refused; a model-less `claude-code`
  *configuration entry* was always accepted and, on the autonomous paths, now
  simply resolves to the daemon default at launch. The refusal above applies
  to a *direct start* that still reaches the launch with no model, not to
  configuration entries.
- **Child inheritance and task-routing rules are unchanged.** A child of a
  Sonnet or Haiku parent is still never put on Opus or Fable by inheritance,
  and a routing row still needs `allow_premium_model` to name one.

## Reading it back

- **Session record.** The `Model` field of the persisted session record is the
  value the backend passed to the CLI as `--model` for that launch — for a
  `claude-code` session it is always the model that ran, and a restart
  relaunches with that same recorded model.
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
- [Merge pipeline](merge-pipeline.md) — the merge-conflict model, whose
  unset default is the non-Opus daemon default (`sonnet`).

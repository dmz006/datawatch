---
docs:
  index: true
  topics: [claude-code, model, sonnet, haiku, opus, daemon default, mechanical work, explicit model, refusal, session record]
exec_steps:
  - tool: autonomous_config_get
    description: Read the daemon-wide autonomous config (planner/judge model keys and the global LLM/model default)
    args: {}
    read_only: true
---

# Claude model defaults: which model runs when nobody names one

Every Claude Code command datawatch launches carries an explicit model.
There is no fall-through to your Claude account's own default and no
silent upgrade to a more expensive model: when a launch site does not name
a model, the daemon names one itself, and when no launch site can resolve
a model at all, the launch is refused with a plain reason instead of
starting. This page is the operator view of that policy — what each default
is, where you can override it, and how you verify which model actually ran.

The full policy, including the refusal backstop and how to verify it, is
[Explicit-model policy: never start Claude Code without a model](explicit-model-policy.md).
This page focuses on the defaults you will see in day-to-day operation.

## The defaults at a glance

| Situation | Model | Who chose it |
|---|---|---|
| A Claude Code session starts and nobody named a model | `sonnet` | The datawatch daemon, not your Claude account |
| Autonomous mechanical work (see below) | `haiku` | The datawatch daemon |
| Explicit operator choice — task or story LLM pair, PRD, automation, config line, judge pin, planner field | Whatever you named (e.g. an Opus-class model) | You |
| A Claude Code launch whose model is empty after every resolution layer | No model — the launch is refused before any tracker, session record, or tmux pane is created | The daemon's refusal backstop |

## When the daemon picks `sonnet`

A worker session, verifier session, guardrail judge, or planner run whose
model resolves to nothing at any layer runs on the daemon default, `sonnet`.
That is a datawatch default, not your Claude account's default: the daemon
always passes the model it chose to the CLI explicitly, so the model cannot
come from an account setting, a provider fall-back, or an upgrade the daemon
did not ask for.

The daemon never *upgrades* toward a more expensive model. A premium
(Opus-class) model runs only when you explicitly named it at one of the
operator-level surfaces listed below.

## Mechanical work runs on `haiku`

The autonomous mechanical work the daemon does for itself inside an
Automaton — **documentation sets, changelog entries, parity-issue drafts,
bounded load steps, version notes, locale strings, formatting cleanup, and
test runs** — runs on `haiku`. That keeps the volume work cheap and fast.
Substantive work (code changes, design decisions, verification judgment)
follows the normal `sonnet` default or an explicit choice.

## Naming a model — the operator-level surfaces

A model you name at any of these surfaces wins over the daemon default for
the launch it applies to:

- **Per task, story, PRD or Automaton** — the LLM pair on the task or
  story, the PRD's `backend`/model settings (PWA card, the
  `prd-set-llm` CLI verb, MCP, REST — see
  [Autonomous PRD LLM Configuration](../operations.md#autonomous-prd-llm-configuration)).
- **Automation / global defaults** — the global LLM/model pair and the
  first-attempt and failure-escalation model pairs
  ([failure escalation](failure-escalation.md)), the
  [judge pin](judge-policy.md), and the [planning backend's](automata-planning-routing.md)
  own model field.
- **An explicit per-session choice** — when you start a session for your
  own use, name the model on the LLM you select (LLM registry rows carry a
  model and a model list; see [LLM registry](llm-registry.md)).

| Name a model | Surface |
|---|---|
| CLI | `datawatch session new --model <name>` (and `--effort quick\|normal\|thorough`) |
| MCP | the `start_session` `model` argument (with `effort` and `name`) |
| Chat | `new: model=<name>: <task>` |
| PWA | the model field of the New Session form — name a model there |
| LLM row | `datawatch llm models add <llm> --model <name>` |

To get the `sonnet` / a hand-picked model on a direct start, name it on any
surface above. If you name nothing, the start tries the LLM row's model, then
the daemon's global planning model, and only then refuses — the autonomous
paths end in the daemon `sonnet` default instead.

Nothing in the daemon, a plan, or a task type silently substitutes a
different model for yours: the session record holds exactly the model the
CLI was passed (see [verification](#verification-which-model-actually-ran)).

## Reasoning effort: what each launch runs at

The daemon sets the reasoning level (`--effort low|medium|high|xhigh|max`) of a Claude Code session fresh at every start and restart, never the level an earlier session used.

**No effort named:** when neither the start request nor its worker profile names an effort, Claude Code starts with no `--effort` flag, at start and at restart, and picks its own level. This includes the built-in `session.default_effort` of `normal`.

**Session effort:** a named `quick` starts Claude Code with `--effort low`, `normal` with `--effort medium` and `thorough` with `--effort high` (never higher). If you set `session.default_effort` to `quick` or `thorough`, a session that names none takes it and gets `low` or `high`. Setting it to `normal` is the same as leaving it alone and sends no flag. A level named for the launch wins over the session effort at start and restart: the start's `claude_effort` (the session record keeps it), otherwise the `effort` of its worker profile (a claude level, or `quick`, `normal` or `thorough`, mapped the same way). Name `claude_effort` when you want `xhigh` or `max`.

One side effect belongs to Claude Code, not to datawatch: typing `/effort <level>` inside a running session is saved by Claude Code as your default in `~/.claude/settings.json`. The daemon does not read or write that file. A Claude Code start or restart that passes `--effort` takes precedence over that saved default for the launch. One that passes none runs at the saved default, as do Claude sessions you start yourself, outside datawatch.

## Verification: which model actually ran

The session record carries exactly the value the CLI received as `--model`;
a restart re-uses the same model instead of re-resolving it. Check it
wherever you read sessions:

```sh
# Name the model on the LLM row your Claude Code sessions use, so
# launches resolve to it explicitly.
datawatch llm add claude-code --kind claude-code
datawatch llm models add claude-code --model sonnet

# Read the session record back — the model field is the exact
# value the CLI received as --model — "sonnet" for a daemon
# default launch, "haiku" for mechanical work, or the model you
# named for an explicit one.
curl -s http://localhost:7433/api/sessions | jq '.[] | {id, backend, model}'
```

For autonomous work, the same applies per run: the worker's attempt record,
the verifier's session record, and the judge's decision log all name the
model that ran.

## Failure mode: a start refused with no model

If a Claude Code launch reaches the start with no model set, datawatch
refuses it up front — before any tracker, session record, or tmux pane is
created — with a plain reason that tells you exactly why:

```
claude-code launch refused: no explicit model set — datawatch will not
fall back to the account default or upgrade the model on its own; name a
model (e.g. "sonnet" or "haiku") in any of these places and try again: the
`datawatch session new --model <name>` flag; the `model` field of POST
/api/sessions/start; the MCP `start_session` `model` argument; the chat
`new: model=<name>` token; or the LLM row via
`datawatch llm models add <llm> --model <name>`
```

Before refusing, a direct start falls back through the request's model, then
the LLM row's model, then the daemon's global planning model (when its backend
is `claude-code`); the refusal fires only when all are empty.

**What it means.** Every model-resolution layer for that launch is empty: no
LLM row, task/story/PRD pair, automation or global default, or explicit
session choice named a model, so the daemon declined to invent one (and
declined to use your account default in place of one).

**Next steps.**

1. Name a model (or set one on the LLM row so every start using it resolves one) on the surface closest to the launch — the LLM row
   (as above), the task or PRD LLM pair, or the automation/global default.
2. Re-run the start; the session record will then show the model you named,
   which is exactly what the CLI passed to Claude Code.
3. If the launch is daemon-side (an Automaton worker), check the PRD's
   `backend`/model settings and the global LLM/model pair, then re-queue
   the task — see [Autonomous PRD LLM Configuration](../operations.md#autonomous-prd-llm-configuration)
   and [failure escalation](failure-escalation.md).

See also: [Explicit-model policy](explicit-model-policy.md) ·
[model routing](model-routing.md) · [task routing](automata-task-routing.md) ·
[worker session profiles](worker-session-profiles.md)

---
docs:
  index: true
  topics: [automata, autonomous, planning, routing, planner, scorecard]
exec_steps:
  - tool: planning_routing_get
    description: Show the planning routing config (enabled, shadow, rules, ladders, risk classes) and whether it is valid
    args: {}
    read_only: true
  - tool: planning_routing_test
    description: Route one example input and show which rule, planners and ladder it would get (no model is called, nothing changes)
    args: {kind: "feature", tags: "security", spec_bytes: 4000}
    read_only: true
  - tool: planning_scorecard
    description: Show the counts-only planner scorecard and gate report for the last 7 days
    args: {window: "7d"}
    read_only: true
---

# Automata planning routing: tune planners for other operators

When an Automaton is decomposed, a **planner** (an LLM backend and a model)
drafts its plan. By default every Automaton uses the one global planner
(`autonomous.planning_backend` and `autonomous.planning_model`). **Planning
routing** lets you send different Automata to different planners, for
example a stronger model for security work, and gives a failed plan a
bounded way to recover (touch-up, replan, then a stronger planner).

Planning routing is **off by default** and ships with **no rules**. Nothing
changes for you until you turn it on. All values below are choices for your
installation; other operators will tune them differently.

## What is decided, and what always wins

Routing only picks the planner when nothing more specific has. These win
over routing, in this order:

1. The planning fields set on the Automaton itself (its own planning backend
   and model, set at create time or through the set-LLM controls).
2. Then routing, if it is enabled and a rule matches.
3. Then the global planner, when routing is off or no rule matches.

Routing is a pure lookup. It calls no model and reads no Automaton content
beyond the routing input (kind, tags, project, spec size, touched files).

## Rules: order and first match

`rules` is an ordered list. datawatch walks it top to bottom and the **first
rule that matches wins**; later rules are not looked at. Put specific rules
above general ones. If no rule matches, the global planner is used.

Each rule has:

| Field | Meaning |
|---|---|
| `name` | Unique name. Also usable as an override. |
| `match` | The selector (below). Empty fields match anything. |
| `planners` | Ordered list of `{backend, model}`. The first is the planner; the rest are its escalation targets. At least one is required. |
| `ladder` | Name of a ladder (below). Empty means the `default` ladder. |

### Match fields

Every list field matches when it is empty (a wildcard) or when **any** entry
in it matches. A rule matches only when **all** of its non-empty fields pass.

| Field | Matches when |
|---|---|
| `kind` | the Automaton kind is in the list (`feature`, `bug`, `other`) |
| `tags` | any listed tag is on the Automaton |
| `project` | the project identifier is in the list |
| `risk_class` | the input resolves to any listed risk class |
| `spec_size_min` | the spec is at least this many bytes (0 = no lower bound) |
| `spec_size_max` | the spec is at most this many bytes (0 = no upper bound) |
| `override` | a free-form override tag the rule applies |

An **override** supplied with a routing request is checked before the rules:
a value equal to a rule name picks that rule directly, and a value written
`backend:model` picks that planner directly with the default ladder. Any
other value is ignored and the rules are walked as usual.

## Risk classes and path globs

A **risk class** is a named set of tags and path globs that marks an input as
sensitive. Two are built in:

| Class | Tags | Path globs |
|---|---|---|
| `security` | `security` | `security/**`, `internal/server/auth*` |
| `design` | `design`, `ui` | none |

An input belongs to a class when **any** of the class's tags is on the
Automaton **or** any file its tasks touch matches one of the class's globs.
A glob ending in `/**` matches everything under that folder; any other glob
is matched against the whole repo-relative path. An input can belong to
several classes. A class you define in `classes` with a built-in name
replaces the built-in. A rule's `risk_class` may name a built-in or one of
your own; an unknown name makes the config invalid.

## Ladders and escalation

A **ladder** is the retry budget a rule uses when the plan the planner
returns fails the checks decompose already runs (scope lint, spec coverage
and structure). The ladder spends its budget in this order:

1. The planner makes an initial plan.
2. Up to `touch_up_max` **touch-up** passes: the planner is told which tasks
   were flagged and the findings, and must return the whole plan again with
   only those tasks changed.
3. Up to `replan_max` **replans**: the findings are fed back and the planner
   plans again.
4. After `escalate_after_attempts` failed attempts at one planner, or when
   the budgets run out, the ladder **escalates** to the next planner in the
   rule's list and starts fresh, spending whatever budget is left.

| Ladder key | Default | Meaning |
|---|---|---|
| `touch_up_max` | 1 | most touch-up passes |
| `replan_max` | 1 | most replans |
| `escalate_after_attempts` | 2 | failed attempts at one planner before the next planner |

A count of 0 reads as its default. Counts cannot be negative. When every
planner and both budgets are used up the ladder stops with a failed plan and
the full attempt record, and the Automaton is left for you to review. The
`default` ladder always exists; add more under `ladders` and name them in a
rule's `ladder`.

## Shadow mode: try a rule set without applying it

Set `shadow: true` (with `enabled: true`) to compute and record the routed
decision for every decompose **while the planner stays exactly as it was**.
This lets you see what rules would do before they do it.

Read it from the scorecard's **would-route** counts: one row per planner and
rule with the number of decomposes that would have gone there. Shadow
decisions are never counted as real decomposes, so they do not inflate the
pass counts. A sensible rollout is: write the rules, turn on shadow, let a
typical week of Automata run, read the would-route counts against what you
expect, adjust, then set `shadow: false`. Use **test routing** (below) to
check a single input at any time.

## The scorecard and the counts-only gate report

The **scorecard** groups real decomposes by planner (backend and model) and
risk class. Per row it shows counts only:

| Count | Meaning |
|---|---|
| decomposes | decomposes recorded in the window (one with several classes counts once in each class row) |
| first attempt | passed on the planner's first plan |
| touch-up | passed after a touch-up |
| replan | passed after a replan |
| escalations | decomposes where the ladder used more than one planner |
| errors | decomposes where at least one planner attempt errored |
| mean findings, mean edits | average check findings and average supervisor edits |

Records with no matching class are listed under `unclassified`.

The **gate report** is the same data rendered as plain text for a go or
no-go decision: a totals line (`decomposes`, `first_attempt`, `touch_up`,
`replan`, `escalations`, `errors`, `would_route`), then one line per
planner and class, then one line per would-route row. It carries counts,
planner names, and rule and class names only. It never includes plan text,
finding text, prompts or spec content, so it is safe to paste into a ticket
or a chat.

The window defaults to 7 days. Give a length such as `36h` or `7d`; `0`
means all records.

## The shipped example rules, and why they are not active

Two example rules are provided as starting points. They are **examples
only**. They are shown as commented text in `docs/config-reference.yaml`
and are not in effect, because routing is off and the rule list is empty by
default:

- **security-strong**: matches the `security` risk class or the `security`
  tag, and routes to a stronger planner first, with a local model as the
  escalation target.
- **design-strong**: matches the `design` or `ui` tag, with the same
  planner chain.

They name specific backends and models that other operators may not have,
may not want, or may not be able to afford, so enabling them for you would
be a decision made on your behalf. Copy one, change the backends and models
to ones in your own LLM registry, run it in shadow mode first, and only then
turn it on.

## Access: five methods and MCP

| Method | How |
|---|---|
| YAML | Edit the `autonomous.planning_routing` block in `datawatch.yaml` (see `docs/config-reference.yaml`), then reload. |
| CLI | `datawatch autonomous planning-routing get`, `set --enabled=true --shadow=true`, `set --rules-file rules.json`, `test --kind feature --tags security`, `scorecard --window 7d` |
| Web UI | Settings, Automata section, **Automata planning routing** card: rules table, ladders, risk classes, JSON editor, **Test Automata routing** form, and the scorecard with would-route counts. |
| REST API | `GET` and `PUT /api/autonomous/planning-routing`, `POST /api/autonomous/planning-routing/test`, `GET /api/autonomous/planning-routing/scorecard?window=7d` |
| Comm channel | `autonomous planning-routing show`, `autonomous planning-routing test kind=feature tags=security`, `autonomous planning-routing scorecard 7d` |
| MCP | `planning_routing_get`, `planning_routing_set`, `planning_routing_test`, `planning_scorecard` |

A `PUT`, `set` or `planning_routing_set` change is validated first. A bad
config is rejected and nothing is applied. Validation refuses duplicate rule
names, rules with no planners, an empty backend or model, an unknown ladder
or risk class, a `spec_size_min` above `spec_size_max`, and negative ladder
counts.

## Check it works

1. `planning-routing get` shows `Config: valid`.
2. `planning-routing test` with a security-tagged input names the rule you
   expect and the planners in order. With routing off it reports the global
   planner as a fallback.
3. After a week in shadow, the scorecard's would-route counts match your
   expectation.

See also: [Automata capacity](automata-capacity.md),
[Automata progress](automata-progress.md).

---
docs:
  index: true
  topics: [supervisor, fresh sessions, handover, state record, corrections, rules pack, restart]
exec_params:
  - {name: project, required: true, description: "The project name"}
  - {name: model, required: true, description: "An explicit Sonnet or Haiku model name; Opus and an empty model are refused"}
  - {name: schedules, required: false, default: "watch", description: "Semicolon-separated name[=cron] pairs, for example watch=*/20 * * * *;full=0 * * * *"}
exec_steps:
  - tool: supervisor_setup
    description: Set up the project's supervisor; running it again adds nothing
    args:
      project: "{{params.project}}"
      model: "{{params.model}}"
      schedules: "{{params.schedules}}"
    read_only: false
  - tool: supervisor_state
    description: Read the supervisor's pinned state record
    args:
      action: get
      project: "{{params.project}}"
    read_only: true
  - tool: supervisor_correction
    description: List the corrections recorded for the supervisor
    args:
      action: list
      project: "{{params.project}}"
    read_only: true
---

# Supervisor fresh sessions

A supervisor is a session that watches one project's Automata. A long
session slowly fills its context and starts to forget, so datawatch does not
keep one alive. It starts a **fresh** session on a schedule, hands it
everything it needs, and lets the old one go. This page sets that up, explains
what a fresh session reads, how you correct it, and how to build your own
rules pack.

## Set it up

One call does the whole set-up. It is safe to repeat: a schedule that already
exists is reported with `created=false` and nothing is added.

```bash
datawatch supervisor-setup run --project myproject --model <a Sonnet or Haiku model> \
  --schedule watch --schedule "full=0 * * * *"
```

The same request exists everywhere:

| Surface | How |
| --- | --- |
| REST | `POST /api/supervisor-setup` with `project`, `model`, optional `schedules` |
| MCP | `supervisor_setup` |
| CLI | `datawatch supervisor-setup run` |
| Chat | `autonomous supervisor-setup run project=P model=M schedules=watch,full` |

The project must be registered under `projects:` in the config with its
directory; an unknown name is refused. The project rules pack is read from
`packs/build-supervisor/rules.md` inside that directory, and the project rules
file is the file `autonomous.rules_file` names (`AGENT.md` when unset).

What it does: launches with the supervisor role, resolves the project
directory, checks the model, generates the handover and makes sure one
recurring spawn schedule exists per name. The model must be named. Only a
Sonnet or Haiku model is accepted; Opus and an empty model are refused. With
no schedule given you get one `watch` schedule every twenty minutes.

## The three layers

Every fresh session starts from one handover with three layers, in this order:

1. **Build context.** The profile, the tool servers (datawatch), the project
   directory and the model.
2. **Rules.** Three texts, each optional after the first: the **default build
   supervisor pack** that ships with datawatch, the **project rules pack** you
   write, and the project's own **rules file**. The project texts come after the
   default, so on a conflict they win.
3. **Direction.** Each plan of the project in order, with its goal, its next
   piece of work, and every Automaton with its state. A blocked Automaton shows
   the reason it is blocked.

Corrections and the pinned state record ride along with the handover (see the
next two sections). The handover is assembled at the moment the session starts,
using the daemon's clock, so a fresh session never reads a stale one.

### How big the handover can be

The handover stays under **24,000 characters** for any project. The default
pack is carried whole; a long project rules pack and a long direction layer are
cut with a note saying where to read the rest. The project **rules file** is
not embedded: the handover carries its path and the list of its section
headings, and the session reads the file by path when it needs a rule.

What is typed into the session is shorter still, at most **8,000 characters**.
A handover that fits is delivered as it is. A larger one is written to
`<project>/.datawatch/supervisor/<schedule name>-handover.md` and the session
is given a short prompt that names that file and says to read it first and then
follow it. The file is rewritten each time the schedule fires, so a fresh
session reads current state; a schedule created earlier with a large stored
task is healed the same way at its next fire.

A schedule name is one plain name, because it becomes part of that file name:
a name with a path separator, `..` or a control character is refused. The file
stays inside the project folder: if `.datawatch` or `.datawatch/supervisor` is
a link that leaves the project (or an absolute link), the file is not written.

## The pinned state record

The record is the supervisor's memory between sessions. It is pinned, so it is
part of the wake-up bundle. Each entry has an id and belongs to one section:

| Section | Holds |
| --- | --- |
| `decision` | A standing decision |
| `work` | An open work item, with an owner and a next step |
| `checked` | What was last checked, and when |
| `correction` | A correction you gave |

Every edit says **who** made it and **why**. Writing identical content records
no edit, and removing an entry that is not there does nothing.

```bash
datawatch supervisor-setup state get --project myproject
datawatch supervisor-setup state set --project myproject --id release-order \
  --section decision --text "Docs Automata go last" --who me --why "operator rule"
datawatch supervisor-setup state change --project myproject --id release-order \
  --text "Docs Automata go after code" --who me --why "clarified"
datawatch supervisor-setup state remove --project myproject --id release-order \
  --who me --why "no longer true"
```

REST is `/api/supervisor-setup/state` (GET, POST, PATCH, DELETE), MCP is
`supervisor_state` with `action` of get, set, change or remove, and chat is
`autonomous supervisor-setup state get|set|change|remove`.

## Corrections and their classes

When you correct a supervisor, record it with a **class**. The class decides
where the correction lives:

| Class | Meaning | Where it goes |
| --- | --- | --- |
| `state` | True only while a situation lasts | Stays in the state record; expires when the situation is done |
| `standing_rule` | A durable rule | Promoted to the project rules pack as a reviewed commit; a fresh session reads it from the pack |
| `product_defect` | The correction reveals a defect | Filed as an Automaton on the project plan; its workaround rides the handover until the Automaton lands |

A correction's status is `active`, `expired`, `promoted` or `filed`. Only a
`standing_rule` can be promoted; promoting another class is refused.

```bash
datawatch supervisor-setup correction record --project myproject --id no-friday-merges \
  --class standing_rule --text "Never merge on Friday" --who me --why "release policy"
datawatch supervisor-setup correction promote --project myproject --id no-friday-merges \
  --who me --why "make it permanent"
datawatch supervisor-setup correction list --project myproject
```

REST is `/api/supervisor-setup/corrections` (and `/promote`), MCP is
`supervisor_correction`, and chat is `autonomous supervisor-setup correction`.

### A correction that keeps coming back

If the same correction is recorded more than twice, datawatch raises one
`correction.repeated` finding. Wording is compared after lowercasing and
dropping punctuation and extra spaces, so a respaced repeat counts. The finding
means the rule is not reaching the supervisor: promote it to a `standing_rule`
or fix where it is written, rather than recording it again.

## Restart and the fallback

A running supervisor is replaced when its context grows past the size limit or
its age passes the age limit. The order never changes: start the new session
with the handover, confirm it answers, then end the old one. The old session is
never ended before the new one has answered, so a failure at any step leaves a
working supervisor behind.

The restart is recorded step by step. If the daemon stops halfway, the next
check finds the record and carries on from the recorded step instead of
starting a second session, so repeating a restart is safe. If the new session
never answers, the restart fails and the old session stays; you get an alert
and the next scheduled tick tries again.

Three events also start a supervisor without waiting for the next tick: a
priority stall, a judge verdict, and a spawn schedule that went to the failed
state. For a failed schedule datawatch alerts you and starts a supervisor so
the cadence is not silently lost. If nothing else works, run
`supervisor-setup run` again: it is idempotent and repairs a missing schedule.

## Settings and defaults

Settings are per project; a field you leave out keeps its default.

| Setting | Default | Range |
| --- | --- | --- |
| Tick cadence | 20 minutes | 1 minute to 24 hours |
| Model | a Sonnet model | Sonnet or Haiku only |
| Context size limit | 150000 tokens | 1000 to 1000000 |
| Context age limit | 6 hours | 1 minute to 7 days |
| Wake findings | prd.blocked, task.review_needed, task.failed, task.final_gate_failed | At most a bounded number of kinds |

The same block is in `docs/config-reference.yaml` under `supervisor_fresh_sessions`.
The loop's own event wake and steps are separate; see
[supervisor-loop](supervisor-loop.md) and
[automaton-event-driven-supervisor](automaton-event-driven-supervisor.md).

## Build and install your own pack

The default pack lives in `packs/build-supervisor/` in the datawatch
repository. Copy that folder and edit the copy:

| File | What it is |
| --- | --- |
| `pack.yaml` | The manifest naming the files below |
| `rules.md` | The rules layer of the handover |
| `handover.md.tmpl` | The template the start-up text follows |
| `watch.yaml` | The light cadence pass |
| `window-guard.yaml` | The projection pass for the usage window |
| `tick.yaml` | The full pass |
| `automation/` | One build module per stack: `go.yaml`, `python.yaml`, `docs-site.yaml` |

Each build module names the files that detect the stack, the build, test and
lint commands, and what must pass before a commit. Add a module for another
stack the same way and list it in `pack.yaml`.

To install it, commit your rules to the project: write them as the **project
rules pack**, or let a `standing_rule` correction promote them one rule at a
time. A fresh session reads the default pack first and your pack after it, so
your pack only needs the rules that differ. Run `supervisor-setup run` again
afterwards; the next fresh session carries your pack.

Keep a pack free of tracker ids, version numbers, host names and home paths.
Those go stale, and a supervisor copies what it reads into specs.

## What you must still review

- **Model.** `model` has no safe empty default; set-up refuses to run without a
  named Sonnet or Haiku model. Check it is the one you want paying for every tick.
- **Promoted rules.** A `standing_rule` promotion is a commit to the project
  rules pack. Read that commit; the next fresh session obeys it.
- **Filed defects.** A `product_defect` correction creates an Automaton. Review
  it and its workaround before approving it.
- **Repeat findings.** A `correction.repeated` finding needs a decision from you.
- **Limits.** The context size and age limits decide how often sessions are
  replaced; tune them if restarts come too often or too late.

## See also

- [supervisor-loop](supervisor-loop.md) — the loop's steps, jobs and runs
- [automation-supervisor-watch](automation-supervisor-watch.md) — the daemon-side watch

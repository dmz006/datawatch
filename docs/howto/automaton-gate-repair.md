---
name: automaton-gate-repair
description: >
  The gate repair engine in one place: when the daemon runs it after a gate
  fails, the six failure classes and their safe repairs (plus unknown), report
  versus on, the safe-repair list and hard limits, the repair budget and the
  held question, and reading and running it from every surface.
when_to_use:
  - a final gate failed and you want the daemon's out-of-band pass to work it out
  - you want to read what the gate repair engine found and did
  - you want to turn the engine's mode (off, report, on)
  - a gate repair held question is open and you want to answer it correctly
tags: []
exec_steps:
  - id: read
    tool: autonomous_gate_repair_status
    description: Read the mode in effect, the records newest first (class, what would be done, what was done, the before/after undo pair, the held question) and the repair budget already spent
    required_args: []
    optional_args: []
    args: {}
    read_only: true
  - id: set-mode
    tool: config_set
    description: Set the engine's mode. report is the default and records without changing anything; on applies the safe repair and re-runs the gate once; off stops the engine
    required_args: [key, value]
    optional_args: []
    args:
      key: autonomous.gate_repair.mode
      value: on
    read_only: false
  - id: run-once
    tool: autonomous_gate_repair_run
    description: Run the engine now. No arguments is one pass over every failed gate; both automaton_id and task_id is one gate; exactly one id is refused
    required_args: []
    optional_args: [automaton_id, task_id]
    args: {}
    read_only: false
  - id: answer-hold
    tool: question_answer
    description: Answer the held question a spent budget, an `unknown` class or a refused repair raised. Pick retry (retry the gate now — the question's default option) or cancel (cancel the task: it stays failed and its dependents do not run)
    required_args: [id]
    optional_args: [chosen_option_id, quote]
    args:
      chosen_option_id: retry
    read_only: false
---

# Repairing failed Automaton gate checks — the engine and how to run it

A story's final gate is checked by a script, and the script is a normal
tracked file in the project. Sometimes the gate fails not because the work is
wrong, but because of something the daemon owns around it — a script that a
later story still owns, a docs mirror that is a copy behind the `docs/`
source of truth by the time the gate ran, a script that refused a file the
task declared. The gate repair engine works it out of band, after the
verdict is in: it works out what a gate-check failure is about (and it **never
guesses**), and for the classes it knows it applies one safe repair to the
script and re-runs the gate once, **without changing the gate's verdict and
without inventing a repair**. Everything the engine does
is recorded, and each record keeps its undo — the `before`/`after` text
pair when it edited a script, the `make sync-docs` output when it rebuilt
the mirror.

The engine has its own mode: `off`, `report` (the default) or `on`. It works
out of band: after a gate failure, the daemon classifies the failing output,
and in `on` mode applies the safe repair for that class and re-runs the gate
once. In `report` mode it only reads: it classifies the same way, records
the would-be repair and raises one warning alert, "Gate repair found", per
failed gate until it stops failing — it changes nothing and spends no budget.
With the mode `off` the engine does not run at all. Its result never changes
the gate's verdict: the gate still ended failed in every case.

## The failure classes and their safe repairs

The engine classifies a failing gate in this order, and each class has exactly
one safe repair:

| Class | What failed | The repair the engine applies (in `on` mode) |
|---|---|---|
| `later_story_script` | a script this story's tasks may not edit yet — a later story owns it | the gate run is reset to run again; the script is not touched, nothing is created now, and the gate passes once that later story lands |
| `docs_mirror_stale` | the built-in docs mirror is a copy behind the `docs/` source of truth | `make sync-docs`, re-run the gate. The script is not edited — the record's `before` is empty and its `after` carries the `make sync-docs` output |
| `allowed_list_rejects` | the script's own allowed list refused a file the task declares | widen that script's allowed-file list with the task-declared file that was refused, re-run |
| `tracker_id_css_hex` | one tracker id in a story matched a CSS hex-color constant in another | narrow the id pattern, re-run |
| `pipe_sigpipe` | a script's own pipe closed before a command ran, or a command was missing | stop the broken pipe or call, re-run |
| `restart_killed` | the gate itself was healthy but the daemon restarted and its kill of that process group took the gate with it | re-run it; it was killed, not failed |
| `unknown` | a failure the engine does not know how to classify | nothing: in `on`, one held operator question plus the "Gate repair held" warning alert (the alert fires even when the question can't be raised); in `report`, just a would-be record and the "Gate repair found" alert |

## The budget and the hard limits

The engine repairs **at most one safe fix per class per task**: the repair
spends that task's budget for that class, and the second time the same class
appears the engine does not repair — it holds and asks, exactly as it does
for anything it does not know. A repair that fails does not count: it does
not spend the budget, does not raise a question, and the gate stays failed —
the record says the repair failed. The budget is recorded in a
small ledger under the daemon's data directory, so it survives restarts. The
classes are independent of each other.

And the engine never:

- guesses a repair it does not recognize — `unknown` is a held question, not
  an attempt;
- touches a script outside the Automaton's own `.datawatch/verify` folder;
- edits anything other than that one script, and does more than the
  `make sync-docs` mirror rebuild and the gate-run reset — every record
  keeps its repair text (the `before`/`after` pair for a script edit, the
  `make sync-docs` output for a mirror rebuild), so every change is undoable
  by hand;
- **merge, ship, delete, end or signal, or change a model or backend setting**
  (`restart_killed` and `later_story_script`, the two re-run/reset classes,
  edit no file at all);
- **change what a failed task means** — the operator's failed-gate path, the
  retry budget and the verifier are untouched, and the held question is one
  answer, not the engine's, and never a retry loop.

## The held question the engine asks

One question per failed gate and class: while one is open it is not
raised again, and one gate failure raises at most one question even if more
than one class matches. A repair that fails raises no question at all — it is
recorded as a failed repair with its reason, the gate stays failed, and the
operator path (the retry budget, the verifier) is untouched.

When the budget for a class is spent, the class is `unknown`, or the repair is refused, the engine
stops and asks the operator. The question's wording:

> Task {task} in {automaton} failed its gate: {failure}. The gate repair engine
> held it ({class}). The repair would be: {repair}. Retry the gate now, or
> cancel the task?

- **Retry the gate now** — answer `retry` (this is the question's default option): the task is held, and if it is a failed task the failed run is reset so it runs again, with the dependents' failures reset too.
- **Cancel the task** — answer `cancel`: the task stays failed and the tasks that depend on it do not run.

The held question it raises is the ordinary stored-operator-question path: it is an ordinary stored operator question, read in the usual places for held questions and answered with the usual answer surface, or with `question_answer` picking `chosen_option_id` `retry` or `cancel` — the answer ids in the record are exactly those two.
When question raising is disabled, a hold (a spent budget, an `unknown`
class or a refused repair) is still recorded as held and still raises the
"Gate repair held" warning alert — the hold is visible either way.

## Surfaces you can read and run it from

| Surface | Read | Run |
|---|---|---|
| MCP | `autonomous_gate_repair_status` (the whole object: `mode`, `records` newest first, `budget`, `held_questions`) — read-only, no arguments; `gate_repairs` (plural) is the earlier read-only alias and answers the same object | `autonomous_gate_repair_run` — no arguments is one pass over every failed gate; both `automaton_id` and `task_id` is one gate; exactly one is refused, not a default. It honors the live mode, spends the same budget, writes the same records and decision lines, and answers with the run outcome(s) plus a fresh status |
| CLI | `datawatch autonomous gate-repair status [--json]` — the mode, the records (newest first) and the repair budget already spent | `datawatch autonomous gate-repair run [--automaton <id> --task <id>] [--json]` — one pass, or one failed gate when both ids are given |
| REST | `GET /api/autonomous/gate-repair` — the same JSON, `mode`, `records`, `budget`, `held_questions`; `GET /api/autonomous/gate-repairs` (plural) is the earlier alias | `POST /api/autonomous/gate-repair/run` — empty body: one pass; both `automaton_id` and `task_id`: one gate; exactly one is refused with 400 |
| Chat channel | `autonomous gate-repair status` — a human-readable listing, newest first | `autonomous gate-repair run [automaton=ID task=ID]` — one pass, or one failed gate when both ids are given |
| Web UI | Automation page (housekeeping sub-tab) → **Gate repair** card: the mode in effect (mode select — `report`/`on`; `off` is set in Settings), the recent findings and the spent repair budget | the card's **Run now** button runs one pass |

The engine's finding is also visible as a decision-log line and an alert; an
applied repair raises no alert — the record and decision line are enough.

## Turning the engine on

- **In config**: `autonomous.gate_repair.mode` — `off`, `report` or `on` in
  the daemon's config file (for example `~/.datawatch/config.yaml`). The
  default is `report`, which records without changing anything.
- **From the daemon**: `datawatch config set autonomous.gate_repair.mode <mode>`;
  change it while you watch in the Automation page (housekeeping sub-tab), in the **Gate repair** card (its mode select offers `report` and `on`; `off` is set in Settings).
- **In chat**: `configure autonomous.gate_repair.mode=<mode>`; read it back
  with `autonomous gate-repair status`.
- **As an MCP agent**: `config_set` with `key` `autonomous.gate_repair.mode`
  and `value` one of `off`, `report`, `on`.

The daemon reads the mode live — a change applies to the next failed gate,
no restart needed. A value other than `off`, `report` or `on` is refused on
every surface, naming the key and the valid values, and nothing is written.

## Turning it on for one Automaton

The engine is daemon-level and there is no per-Automaton switch: a run that
names an Automaton (`--automaton` on the CLI, `automaton=ID` in chat, the
two ids on the run tool or the run route) still honors the daemon's mode. The
right way to gate-repair one broken story is to run the engine at `report`,
read its records, turn it to `on`, and re-run that one gate — or to repair
the script by hand and re-run the gate. There is no hidden per-Automaton
override.

## Undoing the repairs it applied

Each repair has its undo recorded. In `on` mode an applied repair records the
edit's `before`/`after` text pair when it touched a script (for the
docs-mirror repair the record's `after` is the `make sync-docs` output), and
the record and the decision-log line both carry how to undo it. Reading the
records never changes anything:

1. **Read where you stand** — `autonomous_gate_repair_status`. Each `records`
   entry says what happened: found, the `would` (what it would do) or the
   repair applied (`did`), the undo — the `before`/`after` text pair when a
   script was edited — and the held question it raised (if any).

2. **Decide from the record.** Each record says whether it applied
   (`did`), what it found, and how to undo it — the `before`/`after` text
   pair. You are not choosing from options, you are deciding from evidence.
   The two things that always go back to the operator, not the engine, are
   the `unknown` failures and any repair that failed — either way, the
   answer is yours, not the engine's.

3. **Turn the mode back, not the script.** If you do not want the engine
   acting, set `autonomous.gate_repair.mode` back to `report` (or `off`) — that
   stops the engine at the source, and every record is still readable.
   Reverting an edit is a manual job for you: the `before`/`after` text pair
   in the record tells you exactly what the script looked like, and the
   change is one file inside the Automaton's own verify folder.

One more: `restart_killed` and `later_story_script` leave no file behind —
one re-runs the gate, the other resets the run — so for those the record's
undo *is* the repair itself, and reading it back is the whole thing.

## Where to see it

| Surface | How |
|---|---|
| REST | `GET /api/autonomous/gate-repair` — `mode`, `budget` (one entry per spent task/class) and `records`, newest first, each with the failure class, what it would do or did, the undo (the `before`/`after` text pair when a script changed) and the held question; the plural `GET /api/autonomous/gate-repairs` is the earlier alias and answers the same object |
| MCP | `autonomous_gate_repair_status` (same object, read-only, no arguments) and `gate_repairs` (the earlier plural alias) |
| CLI | `datawatch autonomous gate-repair status`: the mode, the budget in lines, each record below it; `--json` for the object |
| Comm channel | `autonomous gate-repair status`: the same lines, budget then records |
| Web UI | Automation page (housekeeping sub-tab) → **Gate repair** card: the mode in effect (mode select — `report`/`on`; `off` is set in Settings), the recent findings (cause, repair, before/after), the spent repair budget and a **Run now** button |
| Alerts | in `report`, one "Gate repair found" warning per failed gate until it stops failing; in `on`, one "Gate repair held" warning when the engine holds (a spent budget, an `unknown` class or a refused repair); a successfully applied repair raises no alert |

## See also

- [automation-lane-fill.md](automation-lane-fill.md) — the lane fill the
  gates belong to.
- [datawatch-definitions.md](../datawatch-definitions.md#gate-repair) — the
  term *gate repair*.
- [infrastructure-retry.md](infrastructure-retry.md) — the adjacent budget
  that applies to infra failures the engine does not own.

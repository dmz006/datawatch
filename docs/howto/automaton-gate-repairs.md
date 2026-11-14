---
docs:
  index: true
  topics: [autonomous, automation, gate, gate repair, repair, failure class, budget, held question, verify script, restart, SIGPIPE, docs mirror, allowed list]
exec_steps:
  - tool: autonomous_gate_repair_status
    description: Read the gate repair engine state: the mode in effect, the repair records and the spent budget
    args: {}
    read_only: true
  - tool: autonomous_config_get
    description: Read the autonomous settings, including the gate_repair block
    args: {}
    read_only: true
---

# Gate repairs

The [supervisor loop](supervisor-loop.md) runs each task's final verify script
before a task moves on. When the script fails on a known, fixable cause — a
gate that still points at a later story's script, a stale docs mirror, an
allowed list that now rejects a required file, and the rest — the **gate
repair engine** classifies the failure, applies the safe repair for it once per
(class, task), and resets the failed task and its dependents so the gate runs
again. In `report` mode it does exactly the same classification and records
what it *would* do. In `off` it does nothing.

**Where it stands.** The engine is wired into the script gate task path today:
when the daemon-run gate script fails, the engine works the failure out of
band after the verdict is in — its result never changes the gate's verdict,
so the gate still ended failed in every case. In `report` (the default) it
classifies the failure, records `would` entries and raises an alert for the
failed gate, changing nothing on disk; in `on` it repairs and resets the
failed run. The setting `autonomous.gate_repair.mode` is accepted on every
configuration surface, so a report-mode engine is active from the first
daemon start. Switch the mode to `on` to make it repair; switch it to `off`
to turn the engine off entirely. It can also be run on demand — one pass
over every failed gate it can see, or one failed gate by name — in whatever
mode is in effect, on the run surfaces below.

## The failure classes

| Class | The failure looks like | The repair (`on`) |
|---|---|---|
| `later_story_script` | The gate script is one a later story wrote or replaced, and its files have not landed yet — the script is missing or empty. | Reset only: the failed gate run is reset so the gate runs again; nothing is created now, and the gate passes when that later story lands. |
| `docs_mirror_stale` | The gate failed because the gitignored embedded docs mirror is out of date. | Run the docs sync step in the Automaton worktree, then rerun the gate. |
| `allowed_list_rejects` | An allowed-list block in the gate script rejects a file this task requires. | Widen the allowed list in the gate script with the one minimal addition. |
| `tracker_id_css_hex` | The gate misflags a tracker identifier or a CSS hex value. | Narrow that check in the gate script. |
| `pipe_sigpipe` | A piped command was killed by SIGPIPE and the gate treats that as a failure. | Adjust the pipe handling in the gate script. |
| `restart_killed` | The run was killed by a daemon or host restart, not by a real failure. | Reset the task and its dependents so the gate runs again. |
| `unknown` | None of the known shapes fit. | No repair. One held operator question; no guess. |

**Hard limits.** A gate repair never merges, ships, deletes, ends or signals a
session, never changes a model or backend, and never touches a security
workflow. File edits are limited to the task's own `.datawatch/verify` folder:
one gate script, one minimal documented change per record — a script path
outside that folder is a refusal, not a repair. The one step outside that
folder is the docs-mirror repair itself: `make sync-docs` in the task's work
tree, which only refreshes the gitignored built-in docs mirror.

**The budget.** Each repair happens at most once per (task, failure class);
the budget persists across daemon restarts and is spent only when a repair
succeeds. When the budget for a task and class is spent — or the class is
`unknown` — the engine does not retry: it raises one held operator question
instead, naming what failed, why the repair was not applied and what the
repair would have been, with two answers — **retry the gate now** (resets the
failed task and its dependents through the normal path) or **cancel the
task** (keeps it failed). One question per failed gate and class; while one
is open it is not raised again. So the same failure always ends in one
answer, not a loop.

## Setting the mode

The key is `autonomous.gate_repair.mode`; the value is `off`, `report` or
`on`. An invalid value is refused on every surface with the same message and
nothing is written. A change takes effect from the next failed gate, with no
restart.

| Surface | How |
|---|---|
| Config file | the `autonomous.gate_repair` block in `config.yaml` |
| REST | `PUT /api/config` with `{"autonomous.gate_repair.mode": "on"}` |
| CLI | `datawatch config set autonomous.gate_repair.mode on` |
| MCP | `config_set` with `autonomous.gate_repair.mode` (requires `mcp.allow_self_config=true`) |
| Chat | `configure autonomous.gate_repair.mode=on` |
| Web UI | **Settings → Automata → Gate repair** |

## Reading the engine

One read-only view shows the mode in effect, the repair records and the spent
budget. Each record says what was found (the failure class and the first
line of the gate's captured output), what it would do or did, and how to
undo it; script edits carry the before and after text. Reading it never
repairs anything, raises an alert or changes the mode — the mode changes only
through the config set paths above.

| Surface | How |
|---|---|
| REST | `GET /api/autonomous/gate-repair` |
| MCP | `autonomous_gate_repair_status` (no arguments; any argument is refused) |
| CLI | `datawatch autonomous gate-repair status` (`--json` for the raw body) |
| Chat | `autonomous gate-repair status` |
| Web UI | the Automation page **Gate repair** card shows the mode in effect, the recent findings (cause, repair, and the before/after pair when a script changed) and the spent repair budget, next to a **Run now** button (below) |

The earlier plural read — `GET /api/autonomous/gate-repairs`, the `gate_repairs` MCP tool, `datawatch autonomous gate-repairs` and the chat verb `autonomous gate-repairs` — still answers the same object; the singular surfaces above are the current ones and are the only ones with the run verb.

## Running the engine now (a pass, or one failed gate)

The engine does not only work out a failure after the gate has already
failed: it can also be run on demand. A run is the same engine path as the
out-of-band work after a failure — it honors the live mode (in `report` it
only records what it would do and changes nothing; in `on` it applies the
safe repair and resets the failed run), spends the same budget, writes the
same records, and is bound by the same hard limits — but it is started by
you, over every failed gate it can see (one pass), or pointed at one
Automaton and task to repair that one failed gate now. It answers with the
run outcome(s) and a fresh status read. It never changes the mode and
touches nothing else.

| Surface | How |
|---|---|
| REST | `POST /api/autonomous/gate-repair/run` — an empty body runs one pass; `{"automaton_id": "<id>", "task_id": "<id>"}` repairs that one failed gate |
| MCP | `autonomous_gate_repair_run` — no arguments for one pass; `automaton_id` and `task_id` for one failed gate (exactly one is refused) |
| CLI | `datawatch autonomous gate-repair run` (`--json` for the raw body); one gate: `--automaton <id> --task <id>` |
| Chat | `autonomous gate-repair run` for one pass; `autonomous gate-repair run automaton=<id> task=<id>` for one failed gate |
| Web UI | the Automation page **Gate repair** card → **Run now** (one pass) |

Both ids, or neither: giving exactly one of the automaton and the task is
refused (400 on REST, the usage on the other surfaces). Called by a federated
peer, the run route needs the autonomous run capability (the status read
needs only the autonomous read capability), and both routes answer 503 until
the engine is wired.

## Undoing a repair

Because every record says how to undo what was done, undoing a repair is:
restore the record's `before` text in place of the `after`; if the engine
had also reset the task and its dependents, put the task back to `failed` to
re-open the failed gate. A reset-only class (`restart_killed`,
`later_story_script`) changed no file at all — only that state put-back
applies. Nothing else was touched. A run — on the run surfaces above — is bound by the same limits: it spends the same budget, writes the same records, and changes nothing outside the engine's own repair path.

The budget file and the ledger survive daemon restarts — a class the task
already spent stays spent, so the engine does not re-apply the same repair
after a restart. The mode and the budget are operator-owned: neither a read
nor a run changes them.

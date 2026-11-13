---
docs:
  index: true
  topics: [autonomous, automation, gate, gate repair, repair, failure class, budget, held question, verify script, restart, SIGPIPE, docs mirror, allowed list]
exec_steps:
  - tool: gate_repairs
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
after a gate fails and no human action is needed for it, the engine runs for
that failure. The setting `autonomous.gate_repair.mode` (default `report`) is
accepted on every configuration surface, so a report-mode engine is active
from the first daemon start: it classifies failed gate scripts and records
`would` entries, and it changes nothing on disk. Switch the mode to `on` to
make it repair; switch it to `off` to turn the engine off entirely.

## The failure classes

| Class | The failure looks like | The repair (`on`) |
|---|---|---|
| `later_story_script` | The gate script is one a later story wrote or replaced; it fails on work this task already did. | Rewrite the gate to this story's own verify script. |
| `docs_mirror_stale` | The gate failed because the gitignored embedded docs mirror is out of date. | Run the docs sync step in the Automaton worktree, then rerun the gate. |
| `allowed_list_rejects` | An allowed-list block in the gate script rejects a file this task requires. | Widen the allowed list in the gate script with the one minimal addition. |
| `tracker_id_css_hex` | The gate misflags a tracker identifier or a CSS hex value. | Narrow that check in the gate script. |
| `pipe_sigpipe` | A piped command was killed by SIGPIPE and the gate treats that as a failure. | Adjust the pipe handling in the gate script. |
| `restart_killed` | The run was killed by a daemon or host restart, not by a real failure. | Reset the task and its dependents so the gate runs again. |
| `unknown` | None of the known shapes fit. | No repair. One held operator question; no guess. |

**Hard limits.** A gate repair never merges, ships, deletes, ends or signals a
session, never changes a model or backend, and never touches a security
workflow. File edits are limited to the task's own `.datawatch/verify` folder:
one gate script, one minimal documented change per record.

**The budget.** Each repair happens at most once per (task, failure class);
the budget persists across daemon restarts. When the budget for a task and
class is spent, or the class is `unknown`, the engine does not retry — it
raises one held operator question instead, so the same failure always ends in
one answer, not a loop.

## Setting the mode

The key is `autonomous.gate_repair.mode`; the value is `off`, `report` or
`on`. An invalid value is refused on every surface with the same message and
nothing is written. A change takes effect from the next failed gate, with no
restart.

| Surface | How |
|---|---|
| Config file | `autonomous.gate_repair.mode` in `[autonomous]` |
| REST | `PUT /api/config` with `{"autonomous.gate_repair.mode": "on"}` |
| CLI | `datawatch config set autonomous.gate_repair.mode on` |
| MCP | `config_set` with `autonomous.gate_repair.mode` (requires `mcp.allow_self_config=true`) |
| Chat | `configure autonomous.gate_repair.mode=on` |
| Web UI | **Settings → Automata → Gate repair** |

## Reading the engine

One read-only view shows the mode in effect, the repair records and the spent
budget. Each record says what was found (the failure class and the script),
what it would do or did, and how to undo it; script edits carry the before and
after text. Reading it never repairs anything, raises an alert or changes the
mode — the mode changes only through the config set paths above.

| Surface | How |
|---|---|
| REST | `GET /api/autonomous/gate-repairs` |
| MCP | `gate_repairs` (no arguments; any argument is refused) |
| CLI | `datawatch autonomous gate-repairs` (`--json` for the raw body) |
| Chat | `autonomous gate-repairs` |
| Web UI | **Settings → Automata → Gate repair** (mode), and the records above it when the engine has acted or would act |

Because every record says how to undo what was done, undoing a repair is:
revert the single gate-script edit (the record's `after` back to its
`before`), or re-run the task from the task list. Nothing else was touched.

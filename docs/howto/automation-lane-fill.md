---
docs:
  index: true
  topics: [automation, autonomous, automaton, lanes, lane fill, lane preference, shadow mode, idle alert, scheduling, queue]
exec_steps:
  - tool: autonomous_lane_fill_status
    description: Read the lanes, the queued eligible counts, the pool evidence, the newest decisions and the idle alerts
    args: {limit: 50}
    read_only: true
  - tool: autonomous_prd_set_lane_preference
    description: Set which lane an Automaton may take (gpu, claude or any; empty means any)
    args:
      id: "{{params.automaton_id}}"
      lane_preference: "{{params.lane_preference}}"
    read_only: false
  - tool: autonomous_config_get
    description: Read the autonomous settings, including the lane fill block
    args: {}
    read_only: true
  - tool: autonomous_config_set
    description: Set the lane fill mode (off, shadow or on); it takes effect on the next poll
    args:
      lane_fill_mode: "{{params.mode}}"
    read_only: false
---

# Lane fill

**Lane fill** is the daemon's own check that every execution lane stays busy.
Before it, a person had to notice that a lane was idle and start the next
queued Automaton by hand. Now, on every poll, the planner looks at the lanes
and the queue and decides what each idle lane should take. In the default
**shadow** mode it only records those decisions. In **on** mode it starts the
work it has decided on, through the same start path a person uses.

## Base requirements

- A running daemon with `autonomous.enabled: true`.
- Some Automata in the queue. Approved Automata are started directly. Drafts
  are decomposed first, in the background.
- The Automation coordinator running. It is the same loop that advances
  Automations, so it needs no separate service.

## The lanes

| Lane | What it is | Default count | Backend it runs |
|---|---|---|---|
| GPU lane | A channel on the local model pool. Two machines can share one pool, and which machine serves a given session is not fixed per lane. A GPU lane costs no Claude usage. | 2 | `pi-prompt` by default (`gpu_backend`), with the model in `gpu_model` |
| Claude lane | A channel that runs one Automaton at a time on the Claude backend. | 3 (0 means none) | `claude-code` (`claude_backend`) |

A lane is **occupied** when an Automaton is running on it, or when a session is
using the GPU pool that belongs to no Automaton. A lane is **idle** otherwise.
An idle lane with nothing it can take shows a **blocker**: the reason the lane
must not be called idle or the reason no eligible work fits it.

## The three modes

| Mode | What the planner does | Starts anything? |
|---|---|---|
| `off` | Does not run. Records nothing. | No |
| `shadow` (default) | Runs on every poll. Records every decision it would make, with its inputs and the pool evidence it used. | No |
| `on` | Runs on every poll and carries out the decisions. | Yes |

**Why shadow comes first.** The rules for what counts as eligible and which
lane suits which work are decisions for the operator. Shadow mode lets you check
those rules against what the supervisor actually did, for about a day, before
the daemon acts on them. A decision recorded in shadow mode shows
`executed: false`. Clients must show it as "would start", not "started".

Switching mode takes effect on the next poll. No restart is needed.

## Reading the status

The status shows the same facts on every surface:

- the mode, and when the last poll ran;
- each lane, with its type, whether it is occupied or idle, its occupant (the
  Automaton title and backend) or its blocker;
- the eligible work queued for each lane type (GPU and Claude);
- the pool evidence: how many sessions are using the GPU pool, which models are
  loaded on the pool hosts, or the error if a host could not be read;
- the newest decisions, newest first, each with its action (`start_prd`,
  `decompose`, `decompose_ahead`, `report_merge`, `blocked` or `idle`; the PWA and
  the comm reply show them as plain words), its reason, whether it was executed,
  and any error;
- the recent idle-lane alerts.

| Surface | How |
|---|---|
| REST | `GET /api/autonomous/lane-fill?limit=50` (`limit` is a whole number from 1 to 1000; default 50) |
| MCP | `autonomous_lane_fill_status` with an optional `limit` |
| CLI | `datawatch autonomous lane-fill [--limit N] [--json]` (`--json` prints the REST body unchanged) |
| Comm channel | `autonomous lane-fill [limit]`: one line per lane, the queued counts and the newest five decisions |
| PWA | Automata tab, **Automation** sub-tab: the **Lane fill** card sits beside the capacity card. It reloads on its own every five seconds. |

An example from the CLI:

```bash
datawatch autonomous lane-fill --limit 20
```

## A lane waiting on a verifier

A lane can be occupied without moving. That happens while an Automaton has
running work and every running piece is stalled waiting on a verification
slot: the check that must finish the task is queued, not done, and nothing on
that lane can complete until it runs. That is a **lane waiting on a
verifier**. It is not idle: an idle lane holds no work; a waiting lane holds
work that cannot finish. A lane that has one moving task is still an
occupied and working lane, even if another piece waits.

**How it shows.** The same facts on every surface:

- **REST** (`GET /api/autonomous/lane-fill`): a lane the count rule below
  counts available has `state: "waiting_verifier"` on the lane object; a
  lane the rule keeps busy keeps `state: "occupied"`. Either way the lane
  carries `waiting_on_verifier: true` and the `wait_reason` text, for
  example "CI still running the checks" or, when the wait queue has no text
  of its own, "waiting for a capacity slot to finish verification". A
  decision made on a lane counted available keeps the same `wait_reason`,
  recorded as the basis for counting the lane available.
- **CLI and comm channel**: `datawatch autonomous lane-fill [--limit N]`
  and `autonomous lane-fill [limit]` render the same lines. The lane line
  reads `claude-1: occupied by <id> "<title>" (claude-code), waiting on
  verifier: <reason>`, and a decision that placed work on that lane carries
  the same reason as `lane counted available by the verifier wait: <reason>`.
- **PWA**: the **Lane fill** card shows the same lane objects with the same
  flag and reason.

**The idle alert.** A lane that stands free only because its occupant waits
on a verifier slot raises the ordinary idle-lane alert: after
`idle_alert_minutes` with an eligible Automaton waiting, once per idle
episode, at warning level, and it clears when the lane stops waiting. The
message names the wait instead of calling the lane idle: it states the lane,
the waiting Automaton and its verifier-wait reason from the wait queue, and
the first eligible Automaton that could have started on the lane.
`idle_alert_minutes: 0` turns it off, for this kind of lane as for the rest.

**Counting the lane available: `autonomous.lane_fill.count_verifier_wait`.**
When on (the default, unset means on), a Claude lane whose occupant is
waiting on a verifier slot counts as available: the waiting occupant holds
the lane without spending Claude usage, so a lane with no working occupant
can take the next ready Automaton. Set to `false` and the lane counts busy,
and the planner places nothing on it. Like the other lane fill keys it is
set in the config file, with `datawatch config set
autonomous.lane_fill.count_verifier_wait false`, in chat with `configure
autonomous.lane_fill.count_verifier_wait=false`, or at REST with
`PUT /api/config`; the change applies on the next poll with no restart, and
an explicit `false` is kept. A GPU lane is never freed by this rule:
GPU work costs no Claude usage, so a waiting GPU occupant always counts the
lane occupied.

**Shadow mode starts nothing because of it.** In the default shadow mode
none of this happens: a start placed on a verifier-wait lane is only
recorded, with `executed: false`, and reads as a would-start. In `on` mode
it goes through the same normal start path as every other start
(admission, approval gates, quality gates, stuck-task rules).

## Setting a lane preference

Every Automaton has a **lane preference**:

- `gpu`: it may take only a GPU lane;
- `claude`: it may take only a Claude lane;
- `any`: it may take whichever lane type is idle. This is the default.

The planner honours `gpu` and `claude` exactly. It never reads a spec and
decides that the work is hard or easy, or needs design. Choosing a preference
is a decision for a person, and it is recorded on the Automaton.

| Surface | How |
|---|---|
| REST | `POST /api/autonomous/prds/{id}/set_lane_preference` with `{"lane_preference": "gpu" \| "claude" \| "any"}` |
| MCP | `autonomous_prd_set_lane_preference` with `id` and `lane_preference`; an empty value means `any` |
| CLI | `datawatch autonomous prd-set-lane-preference <id> gpu\|claude\|any` |
| Comm channel | `autonomous set-lane-preference <automaton-id> gpu\|claude\|any` |
| PWA | The **Lane preference** select in the Automaton settings form. An Automaton with nothing stored shows **Any lane**. |

A value other than the three above is refused. The CLI and the comm channel refuse
it before they send anything, and the daemon answers 400 to any other caller. The
preference only affects which lane the planner picks. It does not change the
Automaton's status or its plan.

## Setting the lane fill options

| Key | Default | What it does |
|---|---|---|
| `autonomous.lane_fill.mode` | `shadow` | `off`, `shadow` or `on` |
| `autonomous.lane_fill.gpu_lanes` | `2` | How many GPU lanes exist |
| `autonomous.lane_fill.claude_lanes` | `3` | How many Claude lanes exist; `0` means none |
| `autonomous.lane_fill.idle_alert_minutes` | `30` | How long a lane may sit idle with eligible work waiting before an alert; `0` turns the alert off |
| `autonomous.lane_fill.decompose_ahead_min` | `2` | The minimum number of decomposed Automata kept ready per lane type |
| `autonomous.lane_fill.poll_seconds` | `60` | Seconds between planner polls; a value below 5 is raised to 5 |
| `autonomous.lane_fill.gpu_backend` | `pi-prompt` | The backend the GPU lanes run |
| `autonomous.lane_fill.gpu_model` | empty | The model the GPU lanes run; empty means the backend's own model |
| `autonomous.lane_fill.claude_backend` | `claude-code` | The backend the Claude lanes run |
| `autonomous.lane_fill.count_verifier_wait` | `true` | Whether a lane waiting on a verifier slot counts available; `false` keeps it busy |

Every key is settable at runtime with no restart:

| Channel | How |
|---|---|
| Config file | the `autonomous.lane_fill` block in `config.yaml` |
| CLI | `datawatch config set autonomous.lane_fill.mode shadow` |
| Chat | `configure autonomous.lane_fill.mode=shadow` |
| REST | `PUT /api/config` with the same key |
| MCP | `autonomous_config_set` with `lane_fill_mode` (and `lane_fill_gpu_lanes`, `lane_fill_claude_lanes`, `lane_fill_idle_alert_minutes`, `lane_fill_decompose_ahead_min`, `lane_fill_poll_seconds`, `lane_fill_gpu_backend`, `lane_fill_gpu_model`, `lane_fill_claude_backend`) |
| PWA | Settings → General → Autonomous, the Lane fill fields |

A bad value is refused, and nothing is written. An explicit `0` is kept.

## The day-long shadow comparison

Run shadow mode for about one day while the supervisor still fills lanes by
hand. Then compare:

1. **Start on the default mode.** Confirm the status shows `mode: shadow`. Each
   poll adds decisions; none show `executed: true`.
2. **Read the status at the end of the day.** Use `--limit 1000`, the largest
   the status returns, so the whole day is in view:

   ```bash
   datawatch autonomous lane-fill --limit 1000 --json > lane-fill-day.json
   ```

   Keep the file outside the repository.
3. **Compare each `start` and `decompose ahead` decision** with what the
   supervisor did in the same window: which Automaton went to which lane, and
   when.
4. **List the differences**: decisions the supervisor did not make; supervisor
   starts the planner did not propose; lanes the planner called idle while the
   supervisor had a reason to leave them that way; and any decision that named a
   held or sequenced Automaton.
5. **Check the criteria before switching**:
   - no decision named a held or sequenced Automaton;
   - every difference is explained, and either accepted or fixed;
   - the pool evidence never reported a busy pool as idle, and an unreadable
     host always appeared as an error or a blocker, never as an idle lane;
   - the idle alert fired only when a lane was idle with eligible work waiting;
   - the operator has answered the open questions on switching (below).

If any criterion fails, stay in shadow mode, fix the cause, and run another day.

## Switching to on

Once the criteria pass, set the mode:

```bash
datawatch config set autonomous.lane_fill.mode on
```

The change takes effect on the next poll. To go back, set it to `shadow` or
`off`; no restart is needed either way. `off` stops the planner. Work already
running keeps running.

In `on` mode, each decision goes through the existing start path:

- A **start** sets the Automaton's worker backend to the lane's backend, if the
  Automaton's current status allows that change. It approves the Automaton if
  it still awaits review, using the same approve call a person would make. The
  plan-coverage gate and the plan-review gate still apply to that approval. A
  refused approval is recorded on the decision. Then it runs the
  Automaton through the normal admission path, so capacity limits, quality gates
  and the stuck-task rules apply exactly as they do to a manual start.
- A **decompose** or **decompose ahead** starts a decomposition of a draft in the
  background, with the planning settings the draft already has, so a poll does
  not wait for it. A failed decomposition is recorded on the decision.
- A **report merge** is recorded and nothing else. Lane fill never merges.
- A **blocked** or **idle** decision is recorded only.

A failed start is recorded with its error. It is tried again no sooner than five
minutes later, not on every poll.

Before switching, the operator should have answered these:

- Whether the routing pattern list in the plan (`docs/plans/2026-10-24-lane-fill-shadow-mode.md`)
  is confirmed or amended.
- Whether `on` mode may approve a review-pending Automaton through the approve
  call, as it does now, or may start only Automata that are already approved.
- The default Claude lane count and the default idle alert minutes.
- Whether an Automaton on a backend that is neither the GPU backend nor the
  Claude backend counts toward lane occupancy.

## The idle alert

When a lane has sat idle for longer than `idle_alert_minutes` while eligible
work waits for it, lane fill raises one alert:

- it names the lane, how long the lane has been idle, and the first eligible
  Automaton it could have started;
- it is raised once per idle period, not on every poll;
- it clears when the lane takes work, so a later idle period alerts again;
- it goes through the normal alert path, at warning level, and is listed in the
  status under the idle lane alerts.

`idle_alert_minutes: 0` turns the alert off. The alert is read live, so a change
needs no restart.

## Restart behaviour

- **Decision history survives a restart.** The decisions are kept, up to 1000,
  in a file beside the other autonomous stores.
- **Nothing is decided before boot recovery has finished.** Automata that were
  running before the restart are not read as idle lanes.
- **An Automaton that was started is never started twice.** Lane fill remembers
  the starts it made until the store shows the Automaton as running, so the next
  poll neither starts it again nor reads its lane as idle. An Automaton started
  before the restart already shows as running in the store, so it is left alone.
- **Idle timers begin again.** The idle alert counts from the restart, so a lane
  that was idle before the restart alerts after the full threshold, not earlier.
- **Mode and thresholds are read live.** A changed mode or threshold applies on
  the next poll with no restart.

## What lane fill never does

- **It never merges.** Finished work that needs a merge is reported, and merging
  stays a supervisor or operator act.
- **It never judges design or difficulty.** It starts an Automaton only from its
  lane preference, its status, its priority and its age.
- **It never holds GPU work because of Claude usage limits.** GPU lanes cost no
  Claude usage. Claude usage limits and budget pace are not inputs to the
  planner, so they never keep a GPU lane idle. The existing admission path still
  applies its own rules to a Claude-lane start.
- **It never starts a held Automaton.** A held Automaton is one whose spec has a
  line starting with `HOLD`, says "do not run" or "sequenced last", or is
  "sequenced after" an Automaton that has not completed. Held work is listed
  with its reason and is never silently dropped.
- **It never bypasses the normal start rules.** Lane fill adds no second way to
  start work. Admission, approval gates, quality gates and the stuck-task rules
  all still run.
- **It never moves work onto a lane it cannot run.** A GPU lane takes an
  Automaton only when its backend is already the GPU backend, or when its status
  still allows the backend to change.
- **It never acts in shadow mode.** Shadow mode starts nothing and approves
  nothing.

## See also

- [Automation queue advance](automation-queue-advance.md): how an Automation starts its next Automaton.
- [Automata capacity](automata-capacity.md): the admission rules every start goes through.
- [Automation view](automation-tab.md): the Automation sub-tab where the Lane fill card appears.
- [Automation decision assistant](automation-decision-assistant.md): the other shadow-mode feature, for comparison.

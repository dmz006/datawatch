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

The mode can also be set for one lane kind alone with `gpu_mode` and
`claude_mode`, so GPU lanes can go to `on` while Claude lanes stay in shadow
(see "Switching to on, one lane kind at a time" below). A kind with no mode of
its own takes `mode`. The status shows the mode in effect for each kind under
`modes`.

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
| `autonomous.lane_fill.stale_after_hours` | `72` | How many hours an Automaton may go without any change before lane fill calls it stale and will not start it; `0` turns the rule off |
| `autonomous.lane_fill.host_down_after_minutes` | `10` | How many minutes a pool host may stay unreachable before lane fill treats it as down and judges the lanes from the readable hosts; `0` never treats an unreachable host as down |
| `autonomous.lane_fill.require_confirmed_proposal` | `true` | Whether an implementation Automaton needs a proposal file with a `Confirmed: yes` line; `false` turns the rule off |
| `autonomous.lane_fill.check_main_carries` | `true` | Whether lane fill skips an Automaton whose work the default branch already has; `false` turns the check off |
| `autonomous.lane_fill.require_worktree` | `true` | Whether an Automaton that has a recorded worktree is taken only when that worktree is real and registered; `false` turns the check off |
| `autonomous.lane_fill.lane_counts_from_caps` | `true` | Whether the number of lanes follows the LLM concurrency caps; `false` uses `gpu_lanes` and `claude_lanes` as written |
| `autonomous.lane_fill.gpu_mode` | empty | The mode for GPU lanes only: `off`, `shadow` or `on`; empty inherits `mode` |
| `autonomous.lane_fill.claude_mode` | empty | The mode for Claude lanes only: `off`, `shadow` or `on`; empty inherits `mode` |

The eight keys from `stale_after_hours` down are the eligibility and
per-lane-kind keys. The four switches (`require_confirmed_proposal`,
`check_main_carries`, `require_worktree`, `lane_counts_from_caps`) are on when
the key is absent, and an explicit `false` is kept. An explicit
`stale_after_hours: 0` and an explicit `host_down_after_minutes: 0` are kept as well.

Every key is settable at runtime with no restart:

| Channel | How |
|---|---|
| Config file | the `autonomous.lane_fill` block in `config.yaml` |
| CLI | `datawatch config set autonomous.lane_fill.mode shadow` |
| Chat | `configure autonomous.lane_fill.mode=shadow` |
| REST | `PUT /api/config` with the same key |
| MCP | `autonomous_config_set` with `lane_fill_mode` (and `lane_fill_gpu_lanes`, `lane_fill_claude_lanes`, `lane_fill_idle_alert_minutes`, `lane_fill_decompose_ahead_min`, `lane_fill_poll_seconds`, `lane_fill_gpu_backend`, `lane_fill_gpu_model`, `lane_fill_claude_backend`, `lane_fill_stale_after_hours`, `lane_fill_host_down_after_minutes`, `lane_fill_require_confirmed_proposal`, `lane_fill_check_main_carries`, `lane_fill_require_worktree`, `lane_fill_lane_counts_from_caps`, `lane_fill_gpu_mode`, `lane_fill_claude_mode`) |
| PWA | Settings → General → Autonomous, the Lane fill fields |

A bad value is refused, and nothing is written. An explicit `0` is kept.

## Why lane fill skips an Automaton

Before it hands a lane to a queued Automaton (a draft, an approved one or one
waiting for review), lane fill checks it against seven rules. The first rule
that refuses it gives the **eligibility reason**, a short fixed code with a
sentence of detail. A refused Automaton is never started in any mode, and it
does not count as eligible work: not in the queued counts, not in the idle-lane
alert, and not in the supervisor watch's `idle_lane_eligible_work` finding.
Work that is already running, finished or cancelled is never refused.

The rules run in this order:

| Reason code | What it means | How to clear it |
|---|---|---|
| `held` | The title or spec has a line that starts with `HOLD`, says "do not run" or "sequenced last", or the judge timed out on every backend while the Automaton needed review. | Remove the line from the spec if the hold is over. If the hold is right, leave it: this is the rule working. |
| `sequenced_after_unshipped` | The spec says it is "sequenced after" another Automaton, and that one is not completed, or the reference matches no Automaton at all (an unknown reference counts as unfinished). | Finish the other Automaton, or fix the reference so it names the right one (an 8-character id is the most reliable). |
| `stale` | The Automaton has had no change for longer than `stale_after_hours` (72 by default). Old probes and forgotten drafts end up here. | If it still matters, edit it, which refreshes its last-updated time; if it does not, cancel it. To change the limit, set `stale_after_hours`; `0` turns the rule off. |
| `proposal_not_confirmed` | An implementation Automaton whose spec names a `docs/plans/<name>.md` proposal that can be read, but the file has no line starting `Confirmed: yes` in its first 200 lines. | Read the proposal, and if you agree, add the line `Confirmed: yes`. The file is read again on every poll. |
| `proposal_missing` | An implementation Automaton whose spec names no proposal file, or none of the named files can be read. | Name the proposal file in the spec, or fix its path. A path that is absolute, has a `..` element, or leaves the repository is treated as unreadable. Research, operational and personal Automata are exempt. |
| `main_carries_work` | The default branch already has the work: a merge or a task commit for this Automaton, or its whole branch is already on the default branch. The detail names the commit. | The work has shipped, so cancel the Automaton. If the match is wrong, set `check_main_carries` to `false`. |
| `worktree_invalid` | The Automaton has a recorded worktree that is gone, has no `.git` entry, or is not in the repository's registered worktree list. | Restore the worktree, or clear the stale entry in the repository (`git worktree prune`), then let the Automaton make a new one. An Automaton with no worktree yet is fine: it gets one when it runs. |

Two rules turn their own check on and off: `proposal_not_confirmed` and
`proposal_missing` need `require_confirmed_proposal`, `main_carries_work` needs
`check_main_carries`, `worktree_invalid` needs `require_worktree` and `stale`
needs a non-zero `stale_after_hours`. The `held` and `sequenced_after_unshipped`
rules have no switch: a hold is a person's instruction.

If a check cannot be made (git cannot be read, the filesystem view is not
there), the answer is "unknown" and **never** refuses. The base-branch check
looks at a few Automata per poll, newest first, and remembers each answer until
the default branch moves, so a long queue never stalls a poll.

### Where to read the reasons

A refusal is a `blocked` decision. Its code is `eligibility_reason`; the key is
absent on a decision that is not a refusal.

| Surface | Where it shows |
|---|---|
| REST | `GET /api/autonomous/lane-fill`: `decisions[].eligibility_reason`, with the sentence in `decisions[].reason` |
| MCP | `autonomous_lane_fill_status`: the same object |
| CLI | `datawatch autonomous lane-fill`: a decision line reads `blocked <id>: [stale] <sentence>`, the code in brackets |
| Comm channel | `autonomous lane-fill [limit]`: the same lines, newest five decisions |
| PWA | The **Lane fill** card: each refused Automaton carries a chip with the reason in plain words. Settings → General → Autonomous has the rows for the seven keys. |
| Audit | A change of a lane kind's mode writes a `lane_fill_mode` entry (kind, old mode, new mode). |

### The lane counts rule

How many lanes lane fill plans for is no longer only a number you type. With
`lane_counts_from_caps` on (the default), the count for a lane type is the
**sum of the in-flight caps of the enabled LLM entries that run its backend**.
These are the same caps the capacity pools use, so the lanes and the admission
rules cannot disagree. A Claude backend that admits one session at a time gets
one Claude lane, even if `claude_lanes` is 3.

The configured count is kept when the caps cannot give a better answer:

- an enabled entry on that backend is **unlimited** (cap `0`);
- no enabled entry runs that backend, or no cap source is available;
- the configured count is `0`, which always means no lane of that type;
- `lane_counts_from_caps` is `false`.

The status shows what it did. `lane_counts` gives, for `gpu` and `claude`, the
`configured` count, the `effective` count the last poll used and the `source`
(`llm_caps` or `config`, with a `reason` when the configured count was kept).
The CLI and comm channel print it on one line, for example
`Lane counts: gpu 2 of 2 (config), claude 1 of 3 (LLM caps)`. To get more
Claude lanes, raise the cap on the LLM entry; do not raise `claude_lanes` and
expect it to win.

### Pool evidence and a node with a private certificate

Before a GPU lane is called idle, lane fill reads the pool: which sessions use
it and which models each host has loaded. If a registered compute node serves
its API over https with a certificate your machine does not trust (a private
certificate authority or a self-signed one), the read used to fail and the GPU
lanes stayed blocked. Now the read uses **that node's own TLS settings**, the
ones you set on the compute node:

- `tls_ca_file`: trust this certificate authority file as well as the system
  roots;
- `tls_cert_fingerprint`: trust the node whose certificate has this SHA-256
  fingerprint;
- `tls_insecure_skip_verify`: turn verification off for this node (it cannot
  be combined with the other two).

When the certificate is not trusted, the pool evidence error names the node and
the fix, for example `pool host <node> unreadable (loaded models): ollama
/api/ps: node <node> certificate not trusted: ...: set tls_ca_file or
tls_cert_fingerprint on the node, or tls_insecure_skip_verify to turn
verification off for it`. The address is never shown. It appears in the
status `evidence.error`, the CLI and the PWA card. Set one of the three on the
node (`PUT /api/compute/nodes/{name}`, the `compute_node_update` MCP tool or the
compute node settings in the PWA; the how-to [Compute nodes](compute-nodes.md)
has the certificate options in full), and the next poll reads the pool. A node on plain http, and
the host's own local Ollama, are read as before.

### A degraded host: an unreachable node that does not block the lanes

Lane fill reads the loaded models of every pool host before it calls a GPU lane
idle. One node that cannot be read used to put the blocker `pool evidence
unavailable: ...` on every GPU lane and stop decompose-ahead, even when that
node was switched off and held no work. Now an unreachable node is **degraded**
instead, and the lanes are judged from the hosts that can be read.

**When an unreachable node still blocks.** A failed read blocks every GPU lane,
exactly as before, when any of these holds:

- the node currently holds work: a session lease, an Automaton placement, or a
  running session that names it as its node;
- the node is one of the nodes a lane's work is placed on;
- the node is not yet known to be down, meaning it has been unreachable for
  less than `autonomous.lane_fill.host_down_after_minutes` (default 10). Lane
  fill remembers, for each node, the time of its first failed read in a row, and
  forgets it on the first successful read;
- `host_down_after_minutes` is `0`, which means never treat an unreachable node
  as down, so every failure blocks.

A failed list of sessions, for this machine or for a pool node, always blocks.

**When it is degraded.** Only when none of the above holds: the node holds no
work, no lane is placed on it, and it has been unreachable for at least
`host_down_after_minutes`. The failure is then left out of the pool evidence
error and is reported as a warning that names the node, the error text and the
time the node first failed.

**Where it shows.** The warning is kept apart from the lane blockers on every
surface, and is empty when nothing is degraded:

| Surface | Where |
|---|---|
| REST | `evidence.degraded` in `GET /api/autonomous/lane-fill`, a list of `host`, `error` and `since`, next to `evidence.error` |
| MCP | the same list in the `autonomous_lane_fill_status` result |
| CLI | `datawatch autonomous lane-fill` prints a "Degraded hosts (not blocking the lanes)" block with each node, its error and how long it has been unreachable; `--json` prints the list unchanged |
| Chat | `autonomous lane-fill` prints the same block |
| Web | the **Lane fill** card shows a "Degraded hosts" block under the pool evidence, only while a node is degraded |

**How it clears.** The next read that succeeds removes the node from the list
and forgets when it first failed, so a node that recovers disappears on the
next poll. If it fails again later, the clock starts over. To make an unreachable
node block again right away, set `host_down_after_minutes` to `0`; to wait
longer before ignoring it, raise it. The change applies on the next poll.

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
   - no decision named a held or sequenced Automaton, and every refusal
     (`eligibility_reason`) is one you agree with: a stale, unconfirmed or
     already-shipped Automaton the supervisor would also have left alone;
   - the lane counts line matches the lanes the supervisor treats as real
     (`lane_counts` shows `llm_caps` where the LLM caps should decide);
   - every difference is explained, and either accepted or fixed;
   - the pool evidence never reported a busy pool as idle, and an unreadable
     host always appeared as an error or a blocker, never as an idle lane;
   - the idle alert fired only when a lane was idle with eligible work waiting;
   - the operator has answered the open questions on switching (below).

If any criterion fails, stay in shadow mode, fix the cause, and run another day.

## Switching to on, one lane kind at a time

Lane fill ships with both lane kinds in shadow, and nothing in this feature
starts work on its own until you change a mode. The base `mode` key and the two
per-kind keys, `gpu_mode` and `claude_mode`, decide what each kind does. An
empty per-kind key inherits `mode`, so a fresh install reads shadow for both.

**The procedure.** GPU lanes cost no Claude usage, so they go first.

1. **Leave both kinds on shadow** (`mode: shadow`, `gpu_mode` and `claude_mode`
   empty). Check the status: `modes` shows `gpu: shadow` and `claude: shadow`.
2. **Compare for a full day**, as in the section above: lane fill's decisions
   against the supervisor's own checks. Fix or accept every difference, and
   check that each refusal reason is right.
3. **Set `gpu_mode` to `on`** and leave `claude_mode` alone:

   ```bash
   datawatch config set autonomous.lane_fill.gpu_mode on
   ```

   The status now reads `modes: {gpu: on, claude: shadow}` while `mode` stays
   `shadow`. Only decisions on GPU lanes are carried out; Claude-lane decisions
   are still recorded and not started.
4. **Watch.** Read the status and the decisions for a while (a day is a
   sensible time): every GPU start should be one you would have made, no
   refused Automaton should start, and the lanes should be busy. Each change of
   a kind's mode is also in the audit log as `lane_fill_mode`.
5. **Only then set `claude_mode` to `on`**:

   ```bash
   datawatch config set autonomous.lane_fill.claude_mode on
   ```

   Claude starts spend Claude usage, so look at the usage pace before and after.

Setting the base `mode` to `on` switches both kinds at once (for any kind with
no mode of its own). Use it only after both kinds have proved themselves.

**A kind set to `off`** plans nothing for its lanes: its lanes, its queued
count and its decisions vanish from the status, and it raises no idle alert.
The other kind is not affected.

**Going back.** Set the kind back to `shadow` (or empty, to inherit `mode`).
The change applies on the next poll, with no restart, and stops new starts of
that kind. Work already started keeps running; nothing is cancelled and nothing
is undone. `off` stops the planner for that kind altogether. To stop everything
in one step, set `mode` to `shadow` and clear `gpu_mode` and `claude_mode`.

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
- **It never starts an implementation Automaton whose proposal is not
  confirmed.** The proposal files of an Automaton are the
  `docs/plans/<name>.md` paths named in its spec. The Automaton is eligible only
  when at least one of those files, read from the base repository, has a line
  that starts with `Confirmed: yes` (case and markdown decoration such as bold
  are ignored) within its first 200 lines. A readable file without the line
  is refused as `proposal_not_confirmed`; a spec that names no file, or whose
  named files cannot be read, is refused as `proposal_missing`. A reference that
  is absolute, has a parent-directory element, or reaches outside the
  repository through a symbolic link counts as unreadable and nothing outside
  the repository is read. Research, operational and personal Automata are
  exempt. A file that cannot be read for any other reason does not refuse the
  Automaton. Set `autonomous.lane_fill.require_confirmed_proposal` to `false`
  to turn the rule off.
- **It never starts an Automaton it has refused.** The seven eligibility
  reasons (see "Why lane fill skips an Automaton") apply in every mode, and a
  refused Automaton is not counted as eligible work anywhere, including the
  supervisor watch's idle-lane finding.
- **It never bypasses the normal start rules.** Lane fill adds no second way to
  start work. Admission, approval gates, quality gates and the stuck-task rules
  all still run.
- **It never moves work onto a lane it cannot run.** A GPU lane takes an
  Automaton only when its backend is already the GPU backend, or when its status
  still allows the backend to change.
- **It never acts in shadow mode.** Shadow mode starts nothing and approves
  nothing.

## Housekeeping repair for an idle lane

The housekeeping repairs include a lane fill repair (group `lane_fill`, mode `off`, `report` or `on`, default `report`). When a lane is idle and eligible work waits, and the grace period has passed, it starts the next eligible Automaton through the normal run path. It never changes a model or backend. See [automaton-housekeeping-repairs.md](automaton-housekeeping-repairs.md) for the full state and the points to review before turning the group to `on`.

It refuses, writes one decision-log line (found, done, how to undo) and raises exactly one alert when no eligible work exists, capacity is full, the lane is at its inflight cap, the usage guard is paused, or the only waiting Automata need a backend the lane may not use. In `report` mode it records what it would start and starts nothing.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| Every GPU lane is blocked with `pool evidence unavailable: ...` | A node could not be read and the failure is blocking. The error names the node. If the node holds work, a lane is placed on it, or it has been unreachable for less than `host_down_after_minutes`, it blocks; restore the node, or for a certificate error set the node's own TLS options (see the certificate section above). A node that is switched off and holds no work stops blocking after `host_down_after_minutes` and shows under Degraded hosts instead. If `host_down_after_minutes` is `0` every failure blocks. See the section on a degraded host. |
| A "Degraded hosts" block shows on the card | A node is unreachable but holds no work. The lanes are judged from the other nodes. It clears on the first successful read. |

## See also

- [Automation queue advance](automation-queue-advance.md): how an Automation starts its next Automaton.
- [Automata capacity](automata-capacity.md): the admission rules every start goes through.
- [Compute nodes](compute-nodes.md): the certificate settings a GPU pool node can need.
- [Automation view](automation-tab.md): the Automation sub-tab where the Lane fill card appears.
- [Automation decision assistant](automation-decision-assistant.md): the other shadow-mode feature, for comparison.

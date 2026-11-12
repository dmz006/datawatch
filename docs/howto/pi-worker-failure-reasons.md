---
docs:
  index: true
  topics: [pi, pi-prompt, failure, preflight, probe, unhealthy, compute, node, model-unresponsive, alert, automata, autonomous]
exec_params:
  - {name: node, required: true, description: "Compute node name to inspect (for example gpu-box-1)"}
  - {name: automaton_id, required: false, description: "Automaton ID to inspect; leave empty to read every Automaton with retried or held tasks"}
exec_steps:
  - tool: capacity_status
    description: Read the capacity pools; a node that failed its launch probe shows unhealthy and unhealthy_reason on its pool, and waiting tasks show the reason
    args: {}
    read_only: true
  - tool: compute_node_health
    description: Read the node's declared capacity, drain and maintenance state (the unhealthy mark is not shown here; it is on the capacity pool)
    args: {name: "{{params.node}}"}
    read_only: true
  - tool: autonomous_infra_retries
    description: Read the retries and held tasks, including model-unresponsive requeues, newest first
    args: {automaton: "{{params.automaton_id}}", limit: 50}
    read_only: true
  - tool: get_alerts
    description: Read the alerts, including "Task session failed" and "Compute node model unresponsive"
    args: {}
    read_only: true
---
# How-to: Pi worker failure reasons

A pi worker is a pi-prompt session that an Automaton starts to do one task,
usually on a compute node that serves the model. When the model behind that
worker is slow, stuck or unreachable, the session can sit quiet for a long time.
Waiting out the silence limit costs time and spend and tells you nothing about
why it stalled.

This guide covers what the daemon does instead: it reads the real failure from
the session's output log, fails the attempt at once, sorts a model that never
answered from a task that went wrong, tests the node before it starts a worker,
and alerts you when one node keeps failing. It also covers the unhealthy mark a
failed test leaves on a node, and how it ends.

None of this is a setting. There is nothing to turn on, and the limits below are
fixed.

## Base requirements

- A running daemon with Automata enabled and a task running on the **pi-prompt**
  backend. Sessions on other backends are not affected.
- For the node test and the node alert: the task runs on a **compute node**
  (see [Compute Nodes](compute-nodes.md)). A launch with no compute node, or no
  model, is not tested.
- Read access to the daemon on the surface you use: the web app, the CLI, REST,
  MCP or the chat channel.

## What happens, in order

```
  Automaton task is placed on a compute node
                 |
                 v
     launch probe: one-token chat request to the node's model (20 s)
        |                                   |
     passes                              fails
        |                                   |
  worker starts;                  no session is started;
  node mark cleared               node is marked unhealthy;
        |                         task is requeued, no attempt used
        v
  worker runs ... the output log gets a DATAWATCH_PI_FAILED line
        |
        v
  attempt fails at once, with evidence from the log
        |
        +-- text also says "Request timed out" --> model unresponsive:
        |        requeue after the back-off, no attempt used, at most 3 in a row
        |
        +-- anything else --> ordinary failure path (failure escalation)
```

## The failure reasons

### Evidence from the output log

Every pi-prompt session writes an output log. When pi exits with a failure, the
launch command prints one line that starts with `DATAWATCH_PI_FAILED`, for
example:

```
DATAWATCH_PI_FAILED: pi exited with status 1 (model qwen3.8:27b)
DATAWATCH_PI_FAILED: pi produced no output
```

The daemon checks the log for that line while it waits for the worker. As soon as
the line is there, it:

1. ends the session,
2. raises an error-level alert titled **Task session failed**, naming the
   Automaton and the task, and
3. fails the attempt with the evidence it read: pi's exit status when the line
   carries one, the last lines of output, the runner's processes, and the node
   and model the worker used.

The evidence is stored on the failed attempt as `runner_evidence`, so the review
report and the next attempt both see why the worker stopped. Before this, the
attempt waited for the silence limit and carried no reason.

### Immediate failure

The attempt fails when the failure line appears, not when the silence limit runs
out. Spending stops at that moment. The line is matched at the start of a line,
after terminal colour codes and carriage returns are removed, so a command that
merely mentions the word in the middle of a line does not count. A log that is
missing, unreadable or still quiet is not a failure.

### Model unresponsive, and why it does not use an attempt

When the failure text also shows `Request timed out`, the model never answered.
That is not the task's fault, so it is classed **`model_unresponsive`** and
handled on the infrastructure track:

- The task is requeued after the back-off, and **no attempt is used**: it is not
  counted toward the failure limit, does not climb the worker ladder, and leaves
  `infra_retry_count` alone.
- A task is requeued at most **3 times in a row** for this reason, counted in
  `model_unresponsive_retries` on the task. The count restarts once the task
  gets a fair attempt.
- The reason on the task names the node and the model:
  `model unresponsive on node <node>, model <model>: ...`.
- After the third requeue the failure follows the ordinary failure path.
- It is not one of the `autonomous.infra_retry.categories`, and
  `autonomous.infra_retry.exhausted_action` does not apply to it. The back-off
  delays do come from `autonomous.infra_retry.backoff_seconds`.

The wider infrastructure retry rules are in
[Infrastructure retry](infrastructure-retry.md#a-stalled-pi-worker-and-an-unresponsive-model).

### The alert per node

The daemon counts model-unresponsive failures in a row for each compute node.
When a node reaches **3 in a row**, it raises one error-level alert titled
**Compute node model unresponsive**, with a body like:

```
Compute node gpu-box-1, model qwen3.8:27b: 3 consecutive unresponsive failures.
```

- It is raised once when the count reaches 3, not on every later failure.
- A task that passes on that node resets its count.
- The count is held in memory and starts again from zero after a daemon restart.

## The preflight

Before a pi worker is started on a compute node, the daemon sends the node a
one-token chat completion for the worker's model (`max_tokens` 1) and waits at
most 20 seconds for it. This is the **launch probe**. It finds a stuck or
unreachable model before a session exists and spend begins.

| Result | What happens |
|---|---|
| Passes | The worker starts. Any unhealthy mark on the node is cleared. |
| Fails (connection error, non-2xx status or the 20 seconds run out) | No session is started. The node is marked unhealthy, with the probe error as the reason. The task is deferred through the model-unresponsive requeue above, so the same limits and the same node alert apply. |
| Skipped (no node address, or no model) | The worker starts as before. |

The probe error names the node URL and the model, for example
`preflight http://gpu-box-1:11434/v1 model "qwen3.8:27b": status 503: ...`.

## The unhealthy mark

A node that failed its launch probe carries an **unhealthy mark** with the probe
error as its reason. While it is set:

- **Placement skips the node**, as if it were full. Work that could use another
  node goes there. A task pinned to this node waits.
- The node's pool in the capacity view shows `unhealthy: true` and
  `unhealthy_reason`.
- A waiting task shows the reason. With one candidate node it reads
  `waiting for node <node>: unhealthy: <probe error>`. With several candidates
  the node's entry reads `<node>: <probe error>`.
- The mark is held in memory. It is not a setting and is not written to the node
  registry.

### How the mark ends

- **A later successful probe clears it.** The probe runs at launch, so this
  happens when a launch on that node passes its probe, for example one that was
  already admitted to the node before the mark was set. Because placement skips
  a marked node, new work does not reach it to run a probe.
- **A daemon restart clears it.** The mark is in memory and is gone when the
  daemon starts again. The node is tested again on the next launch that is
  placed on it, so a node that is still broken is marked again by that launch.
- **The drain, undrain and health controls do not clear it.** They manage a
  different state. **Drain** and **undrain** set and clear the operator's
  maintenance drain on the node (see
  [Draining a node for maintenance](compute-nodes.md#draining-a-node-for-maintenance)).
  **Node health** reads the node's declared capacity, drain and maintenance
  state, and it does not show the unhealthy mark. Undraining a node that failed
  its probe leaves the mark in place. Draining a node that has the mark does not
  hide it.

There is no operator control that removes the mark by hand. If the node is
healthy again and you do not want to wait, restart the daemon.

## Walkthrough

A worker on `gpu-box-1` stops answering, and the model server is wedged.

1. A task is placed on `gpu-box-1`. The launch probe times out after 20 seconds.
   No session starts. The node is marked unhealthy and the task is requeued
   after the back-off. No attempt is used.
2. Read the node state. In the capacity output the pool shows:

   ```
   "name": "node:gpu-box-1", "unhealthy": true,
   "unhealthy_reason": "preflight http://gpu-box-1:11434/v1 model \"qwen3.8:27b\": ...deadline exceeded"
   ```

3. Other tasks are placed on other nodes. A task pinned to `gpu-box-1` shows
   `waiting for node gpu-box-1: unhealthy: preflight ...`.
4. If a worker that was already running on the node fails with a failure line
   and `Request timed out`, the attempt fails at once. The count for the node
   goes up. At 3 in a row you receive the **Compute node model unresponsive**
   alert.
5. Fix the model server. Then either wait for a launch on the node to pass its
   probe, or restart the daemon to clear the mark. The next launch placed on the
   node is tested again.

## All options

Everything here is read-only. There is nothing to set.

| Surface | Read the node mark and waiting reasons | Read the failed attempt and the requeues | Read the alerts |
|---|---|---|---|
| **REST** | `GET /api/capacity` (pools: `unhealthy`, `unhealthy_reason`; waiting tasks: the reason) | `GET /api/autonomous/infra-retries`; the Automaton under `GET /api/autonomous/prds/{id}` (each attempt's `runner_evidence`, the task's `model_unresponsive_retries`) | `GET /api/alerts` |
| **MCP** | `capacity_status` | `autonomous_infra_retries`; `autonomous_prd_get` | `get_alerts` |
| **CLI** | `datawatch capacity --json` (the text form lists queued tasks with their reasons) | `datawatch autonomous infra-retries [automaton-id]` | `datawatch alerts` |
| **Comm channel** | `capacity` (the same text as the CLI) | `autonomous infra-retries [automaton-id]` | the alert notices your messaging channels already send |
| **PWA** | the Capacity card and the waiting task's reason in the Automaton detail | the retry badges on the Automaton detail and the Infrastructure retries card | the alert dock and the Alerts tab |

The PWA shows the waiting reason, which carries the unhealthy text, but it has no
separate unhealthy badge on the node pool. Use the REST or MCP capacity output
for the `unhealthy` field.

### The drain, undrain and health controls

They exist on every surface, but they are about the maintenance drain, not the
unhealthy mark:

| Surface | Commands |
|---|---|
| **REST** | `POST /api/compute/nodes/{name}/drain`, `DELETE /api/compute/nodes/{name}/drain`, `GET /api/compute/nodes/{name}/health` |
| **MCP** | `compute_node_drain`, `compute_node_undrain`, `compute_node_health` |
| **CLI** | `datawatch compute node drain <name>`, `datawatch compute node undrain <name>`, `datawatch compute node health <name>` |
| **Comm channel** | `compute node drain <name>`, `compute node undrain <name>`, `compute node health <name>` |
| **PWA** | Settings → Compute, the **Drain** and **Undrain** buttons on the node card |

### Examples

```sh
# See whether a node carries the unhealthy mark
datawatch capacity --json | grep -A3 '"node:gpu-box-1"'

# See the requeues and held tasks
datawatch autonomous infra-retries

# REST
curl -sk -H "Authorization: Bearer $TOKEN" "$BASE/api/capacity"
curl -sk -H "Authorization: Bearer $TOKEN" "$BASE/api/alerts?source=system&limit=20"
```

```
You: capacity
Bot: Queued tasks (1):
       "<automaton title>" / "<task title>": waiting for node gpu-box-1: unhealthy: preflight ...
```

## Troubleshooting

| What you see | What it means | What to do |
|---|---|---|
| **Task session failed** alert, and the task shows `runner_evidence` | The output log held a failure line. The attempt failed at once. | Read the evidence: the exit status, the last output and the node and model. Fix the cause, then reset or retry the task. |
| Task shows `model unresponsive on node <node>, model <model>` | The model never answered; the task is being requeued without using an attempt. | Check the model server on the node. The requeue stops after 3 in a row. |
| **Compute node model unresponsive** alert | 3 unresponsive failures in a row on one node. | Check the node and its model server. Tasks pinned to the node will keep failing until it answers. |
| `waiting for node <node>: unhealthy: ...` | The node failed its launch probe and carries the mark. | Fix the node. See [How the mark ends](#how-the-mark-ends). |
| The node is healthy again but still skipped | The mark stays until a launch on the node passes its probe or the daemon restarts. | Restart the daemon. |
| Undrain did not bring the node back | Undrain clears the maintenance drain only, not the unhealthy mark. | Same as above. |
| A failure line was printed but the attempt kept waiting | The line was not at the start of a line, or the session is not on the pi-prompt backend. | Check the output log and the session's backend. |

## Limits

- **The thresholds are fixed.** The 20-second probe limit, the 3 requeues in a
  row per task and the 3 failures in a row per node are not settings.
- **Only pi-prompt sessions.** Other backends keep their own stall handling.
- **The node count and the unhealthy mark are in memory.** A daemon restart
  clears both.
- **A launch with no compute node, or no model, is not probed.**
- **A throttle from the model provider is not this.** It has its own handling.

## See also

- [Infrastructure retry](infrastructure-retry.md) — the retry track this uses.
- [Compute Nodes](compute-nodes.md) — registering nodes, drain, and how a pi
  worker picks its node.
- [Failure escalation](failure-escalation.md) — the ordinary failure path.
- [Alerts + Notifications](alerts-and-notifications.md) — reading and routing
  alerts.

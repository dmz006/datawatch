---
docs:
  index: true
  topics: [model, routing, compute, node, served, models, pin, verifier, refusal, automata, autonomous]
exec_params:
  - {name: automaton_id, required: false, description: "Automaton ID to pin a task on; leave empty to skip the pin step"}
  - {name: task_id, required: false, description: "Task ID to pin to a node (needed only for the pin step)"}
  - {name: node, required: false, description: "Compute node name to read, or to pin the task to; leave empty to read every node"}
  - {name: model, required: false, description: "Model name to check against the nodes, for example ollama/qwen3:27b"}
exec_steps:
  - tool: compute_served_models
    description: Read which models each compute node serves; with a model, also get the answer a pin would get
    args: {node: "{{params.node}}", model: "{{params.model}}"}
    read_only: true
  - tool: get_config
    description: Read the daemon config; the six model routing keys are under autonomous.model_routing, with defaults filled in
    args: {}
    read_only: true
  - tool: autonomous_prd_set_task_node
    description: Pin one task of an Automaton to a compute node (an empty node clears the pin); a node that does not serve the task's model is refused or warned about
    args:
      prd_id: "{{params.automaton_id}}"
      task_id: "{{params.task_id}}"
      node: "{{params.node}}"
    read_only: false
---
# How-to: Model routing

An Automaton worker that asks for a model runs on a compute node. If no node
serves that model, the task used to fail much later, far from the cause, with a
model-not-found error from the engine. Model routing moves that failure to the
moment you pin the model. It also lets you read what each node serves, pin a
task or a story to one node, and keeps the verification job on the same node as
its worker.

## What it checks

The check applies to a worker or a verifier model that runs on a local engine
(Ollama, for example) reached through a compute node. A hosted backend such as
`claude-code` runs on no compute node, so it is never checked and never refused.

- **Which nodes count.** If the LLM entry that hosts the model lists
  `compute_nodes`, only those nodes are looked at. Otherwise every enabled
  registered node is. A disabled node is listed but never read.
- **How a name matches.** The provider prefix of the model is dropped
  (`ollama/qwen3:27b` is `qwen3:27b` on the node), and a name with no tag equals
  the same name with `:latest`.
- **Where it runs.** When you pin a model on an Automaton, a story or a task, or
  set a verifier model, and no registered node serves it, the pin is refused or,
  in warn mode, applied with a warning. See [Pin-time refusal](#pin-time-refusal).

## Read what each node serves

Each node's served models are read from the node itself (an Ollama or opencode
node at `/api/tags`, an OpenWebUI node at `/api/v1/models`) and kept for
`autonomous.model_routing.served_models_cache_seconds` (300 seconds by default).
A node of another kind has no model list to read, so it shows as unknown. Every
surface shows the same data.

### REST

```sh
# Every node
curl -sk https://localhost:8443/api/compute/served-models

# One node, read from the node now instead of the kept copy
curl -sk "https://localhost:8443/api/compute/served-models?node=gpu-1&refresh=true"

# Every node, plus the answer a pin of this model would get
curl -sk "https://localhost:8443/api/compute/served-models?model=ollama/qwen3:27b"
```

The answer has `cache_seconds`, `refusal_mode` and a `nodes` list. Each node
carries `node`, `kind`, `models`, `known` (false when the list could not be
read, and then `error` says why), `cached`, `age_seconds` and `fetched_at`; a
disabled node carries `disabled: true`. With `model` the answer also has a
`check` object: `applicable`, `refused`, `node_unknown`, `served_by`, `checked`
(one line per node) and `message`. It is produced by the same function a pin
asks, so the two cannot disagree. An unknown `node` is a 404.

### MCP

```
compute_served_models()
compute_served_models(node="gpu-1", model="ollama/qwen3:27b", refresh=true)
```

Read-only. The arguments are `node`, `model`, `backend` and `refresh`, and the
result is the same object as the REST read.

### CLI

```sh
datawatch compute node served-models
datawatch compute node served-models gpu-1 --model ollama/qwen3:27b --refresh
datawatch compute node served-models --json
```

One line per node: name, kind, how many models, whether the list is `fresh` or
`cached, 42s old`, then the models. With `--model` the reply also prints the
check result (`allowed`, `refused` or `does not apply`) and its message.
`--backend` narrows that check to one backend; `--json` prints the answer as it
came.

### Comm channel

```
compute node models
compute node models gpu-1 model=ollama/qwen3:27b refresh
```

A short block per node, then the model check when you gave a model. The words
after `models` are an optional node name, `model=<name>`, `backend=<name>` and
`refresh`, in any order.

### Web app

Open Settings → Compute and expand a node. Each node carries a **Served models**
block with its models, a line such as "Read 42 seconds ago" or "Read just now",
and a Refresh button that reads the node again. A node whose models cannot be
read shows a note saying the unreachable node policy decides how it counts, with
the cause on hover. A disabled node says it is disabled.

## Pin-time refusal

With the default `refusal_mode` of `refuse`, a pin of a model that no registered
node serves is turned down and nothing is stored. The message names the model
and every node that was checked, with the reason for each:

```
no registered node serves model "ollama/qwen3:27b"; nodes checked: gpu-1 (serves 2 models, none is ollama/qwen3:27b), gpu-2 (could not read models: connection refused)
```

The REST call answers 400 with that text as the body; the CLI, the comm channel
and the web app show the same text. Read the list this way:

- `serves 2 models, none is ...` means the node was read and the model is not on
  it. Pull the model onto the node, or pick a model the node has.
- `could not read models: ...` means the node did not answer. Whether such a
  node counts as serving is decided by `unknown_node_policy`. Under the default
  `assume_serves` an unreadable node lets the pin through; under `assume_not` it
  does not, and the message above is what you see when no node can be read.
- `no nodes are registered` appears when there is no node to check at all.

The same check runs when you set a model for the Automaton's worker, for a story,
for a task, and for the verifier at each of those levels. A call that leaves the
model out or clears it is not checked.

Each refusal also raises one alert titled "Automaton model not served by any
node" at error level, unless `alert_on_refusal` is off. The alert names the
Automaton, what was pinned and the same message.

## Warn mode

Set `refusal_mode` to `warn` and a pin whose model no node serves is applied
instead of refused:

```sh
datawatch config set autonomous.model_routing.refusal_mode warn
```

- The pin is stored and the call succeeds (REST answers 200).
- The Automaton gets a `model_not_served_warning` entry in its decisions, with
  the same message a refusal would have carried.
- A warning-level alert is raised, titled "Automaton model not served by any
  node", unless `alert_on_refusal` is off.

Warn mode does not make the worker able to run: the engine will still fail to
find the model when the task starts. Use it when you are about to pull the
model onto the node and want to set the pin first.

A node that is not a registered compute node is refused in **both** modes,
because a node that does not exist cannot be used.

## Pin a task or a story to a node

A task, or every task of a story, can be pinned to one compute node. The pin is
checked against that node's served models for the model the task resolves to. A
task's own pin wins over its story's pin. An empty node clears the pin. A pin
is accepted under the same status rule as changing a task's LLM.

| Surface | Pin a task | Pin a story |
|---|---|---|
| REST | `POST /api/autonomous/prds/{id}/set_task_node` with `{"task_id": "...", "node": "gpu-1"}` | `POST /api/autonomous/prds/{id}/set_story_node` with `{"story_id": "...", "node": "gpu-1"}` |
| MCP | `autonomous_prd_set_task_node` (`prd_id`, `task_id`, `node`) | `autonomous_prd_set_story_node` (`prd_id`, `story_id`, `node`) |
| CLI | `datawatch autonomous prd-set-task-node <automaton-id> --task <task-id> --node gpu-1` | `datawatch autonomous prd-set-story-node <automaton-id> --story <story-id> --node gpu-1` |
| Comm channel | `autonomous set-task-node <automaton-id> <task-id> gpu-1` | `autonomous set-story-node <automaton-id> <story-id> gpu-1` |
| Web app | Pin to node, on the task editor | Pin to node, on the story editor |

Give an empty `node` (CLI `--node ""`), or the word `clear` on the comm channel,
to remove the pin. In the web app choose "No pin (any node)". While a pin is set
the task or story row shows a "Node: gpu-1" badge.

```sh
curl -sk -X POST -H "Content-Type: application/json" \
  -d '{"task_id": "t1", "node": "gpu-1"}' \
  https://localhost:8443/api/autonomous/prds/<automaton-id>/set_task_node
```

### What a refused pin says

A node that does not serve the model:

```
node "gpu-1" does not serve model "ollama/qwen3:27b" (serves 2 models, none is ollama/qwen3:27b)
```

A node that is not registered:

```
cannot pin task t1 to node "gpu-9": compute node is not registered
```

A node that is outside the `compute_nodes` of the LLM entry hosting the model is
refused too, and the message lists the nodes that entry allows. All of these are
400 on REST, never a server error. In warn mode a node that does not serve the
model is applied with a warning, as above; a node that is not registered is not.

A pinned task runs only on its node. If the node cannot be used for that attempt
(it is not a candidate for the worker's backend and model, or it failed), the
task fails with that reason instead of running on another node.

Pinning a task to a node is recorded as a `set_task_node` (or `set_story_node`)
decision on the Automaton.

## The verifier follows the worker

By default the verification job for a task runs on the node its worker ran on.
The worker's node is the task's pin if it has one, otherwise the node its worker
held, otherwise the node recorded on the task. The verifier follows when that
node is one of the verifier's candidate nodes **and** serves the verifier's
model. It then waits for that node to have room instead of moving to another
node that may not have the model.

When the verifier cannot follow (the worker's node is not known, is not a
candidate for the verifier's backend and model, or does not serve the verifier's
model), normal placement applies, and a `verifier_node_not_followed` decision on
the Automaton gives the reason. The follow only matters when the verifier has
two or more candidate nodes; with one, placement is unchanged.

To turn it off, so the verifier is placed like any other job:

```sh
datawatch config set autonomous.model_routing.verifier_follows_worker_node false
```

Do this when the verifier's model lives on different nodes from the worker's, or
when you would rather a verifier ran on any free node than wait for one.

## Configuration

Every key lives under `autonomous.model_routing` and is read each time it is
needed, so a change applies at once with no restart. An unset key reads its
default.

| Key | Default | Allowed values | Effect |
|---|---|---|---|
| `autonomous.model_routing.refusal_mode` | `refuse` | `refuse`, `warn` | What happens when a pinned model is served by no registered node: refuse the pin, or apply it with a warning and an alert. |
| `autonomous.model_routing.served_models_cache_seconds` | `300` | whole number, 0 or more | How long a node's served models are kept before the node is read again. `0` reads the node every time. |
| `autonomous.model_routing.verifier_follows_worker_node` | `true` | `true`, `false` | Run a task's verification job on the node its worker ran on, and wait for that node rather than move. |
| `autonomous.model_routing.unknown_node_policy` | `assume_serves` | `assume_serves`, `assume_not` | How a node whose models cannot be read counts: as serving the model, or as not serving it. |
| `autonomous.model_routing.probe_timeout_seconds` | `8` | whole number, 1 or more | How long one read of a node's models may take before the node counts as unreadable. |
| `autonomous.model_routing.alert_on_refusal` | `true` | `true`, `false` | Raise an alert when a pin is refused (error level) or applied with a warning (warning level). |

A bad value sent through `PUT /api/config`, MCP, the CLI, the comm channel or the
web app is refused and nothing is written: the refusal names the key and the
valid values. A bad value written by hand into `config.yaml` is caught when the
file loads: the daemon refuses to start with an error that names the key and the
valid values (an unknown mode or policy, a negative cache age, or a probe timeout
below 1).

How to set each key on each surface:

| Surface | How |
|---|---|
| **YAML** | `autonomous.model_routing:` in `config.yaml` with any of the six keys |
| **REST** | `PUT /api/config` with `autonomous.model_routing.<key>`; `GET /api/config` shows all six with defaults filled in |
| **MCP** | `autonomous_config_set` with `model_routing_refusal_mode`, `model_routing_served_models_cache_seconds`, `model_routing_verifier_follows_worker_node`, `model_routing_unknown_node_policy`, `model_routing_probe_timeout_seconds`, `model_routing_alert_on_refusal`; or `config_set` with the dotted key; read with `get_config` |
| **CLI** | `datawatch config set autonomous.model_routing.<key> <value>` |
| **Comm channel** | `configure autonomous.model_routing.<key>=<value>` |
| **Web app** | Settings → General → Autonomous, the Model routing fields |

Examples:

```sh
# Read the nodes every time, so a freshly pulled model is seen at once
datawatch config set autonomous.model_routing.served_models_cache_seconds 0

# Treat a node that does not answer as not serving the model
datawatch config set autonomous.model_routing.unknown_node_policy assume_not

# Give slow nodes more time to answer
datawatch config set autonomous.model_routing.probe_timeout_seconds 20
```

```yaml
autonomous:
  model_routing:
    refusal_mode: refuse
    served_models_cache_seconds: 300
    verifier_follows_worker_node: true
    unknown_node_policy: assume_serves
    probe_timeout_seconds: 8
    alert_on_refusal: true
```

```sh
# REST
curl -sk -X PUT -H "Content-Type: application/json" \
  -d '{"autonomous.model_routing.refusal_mode": "warn"}' \
  https://localhost:8443/api/config
```

```
You: configure autonomous.model_routing.served_models_cache_seconds=60
```

## Limits

- **The cache remembers failures too.** A node that did not answer is not read
  again until the cache age passes. After you fix a node, press Refresh, pass
  `refresh=true`, or lower the cache age.
- **A pin check reads the nodes.** With a cache age of `0` every pin reads every
  node it checks, each bounded by `probe_timeout_seconds`.
- **The check is about the name, not the capacity.** A node that serves the model
  may still be busy; the pin check does not look at load.
- **Only local engines are checked.** A backend that runs on no compute node is
  skipped, and the served-models check answers "does not apply".

## See also

- [compute-nodes.md](compute-nodes.md): registering the nodes that are checked
- [infrastructure-retry.md](infrastructure-retry.md): a task that fails because
  the machinery around it failed
- [failure-escalation.md](failure-escalation.md): the worker ladder and its
  review actions
- [../api/autonomous.md](../api/autonomous.md): REST reference

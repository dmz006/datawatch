# [sync] Model routing: node served models read, per-task node pin, refusal when no node serves the model (datawatch next release)

Issue number: 321
Issue URL: https://github.com/dmz006/datawatch-app/issues/321

## Body of the issue

An Automaton worker could be placed on a compute node that does not serve the model the task needs, and the task then failed far from the cause. The daemon now reads which models each compute node serves, lets the operator pin a task or a story to one node, and refuses a pin (or warns, by setting) when the chosen node does not serve the model. The verifier of a task follows the node its worker ran on. This records the exact contract the Android and iPhone apps should render, the settings to expose, and the locale keys for the labels.

### What changed for the operator

- Each compute node shows the models it serves, with how old that reading is and a Refresh button.
- A task and a story can each be pinned to one compute node ("Pin to node"). A task's own pin wins over its story's pin. An empty node clears the pin.
- When the node does not serve the model the task resolves to, the pin is refused with a message that names the model and the nodes checked (default), or applied with a warning (setting `refusal_mode` = `warn`). A node that is not a registered compute node is refused in both modes.
- A task or story row shows a "Node: name" badge while it is pinned.
- Six settings under `autonomous.model_routing` control the behaviour.

### New REST route

`GET /api/compute/served-models` (read-only; needs compute read access). Query parameters, all optional:

| Parameter | Notes |
|---|---|
| `node` | one compute node name; an unknown name is a `404` |
| `model` | a model name; adds the `check` object to the answer |
| `backend` | the LLM backend the model belongs to, used with `model` |
| `refresh` | `true` reads the nodes again instead of using the cache |

Response body:

| Field | Type | Notes |
|---|---|---|
| `cache_seconds` | integer | the cache age in effect |
| `refusal_mode` | string | `refuse` or `warn`, in effect |
| `nodes` | array | one entry per registered node (or the one named), never `null` |
| `check` | object, only when `model` was given | the same answer a pin gets, see below |

Each entry of `nodes[]`:

| Field | Type | Notes |
|---|---|---|
| `node` | string | the compute node name |
| `kind` | string | the node kind |
| `models` | string array | models the node serves; `[]` when none or when not known |
| `known` | boolean | false when the models could not be read; then `error` says why |
| `cached` | boolean | true when the list came from the cache |
| `age_seconds` | integer | age of the reading; 0 for a fresh one |
| `fetched_at` | RFC 3339 time, empty when never read | when the node was read |
| `error` | string | empty when `known` is true |
| `disabled` | boolean, omitted when false | the node is switched off; it is listed but never read |

The optional `check` object:

| Field | Type | Notes |
|---|---|---|
| `model` | string | the model asked about |
| `applicable` | boolean | false when the backend does not run on a compute node |
| `refused` | boolean | true when no node counts as serving the model |
| `node_unknown` | boolean | true when the `node` named is not a registered node |
| `served_by` | string array | nodes that serve the model (or count as serving under the unknown node policy) |
| `checked` | array | per node `{node, state, reason}`; `state` is `serves`, `not_serving` or `unknown` |
| `message` | string | the sentence a refusal would carry |

Example, `GET /api/compute/served-models?model=ollama/qwen3:27b`:

```json
{
  "cache_seconds": 300,
  "refusal_mode": "refuse",
  "nodes": [
    {
      "node": "gpu-1",
      "kind": "ollama",
      "models": ["qwen3:27b", "llama3.1:8b"],
      "known": true,
      "cached": true,
      "age_seconds": 42,
      "fetched_at": "2026-10-31T09:12:03Z",
      "error": ""
    },
    {
      "node": "gpu-2",
      "kind": "ollama",
      "models": [],
      "known": false,
      "cached": false,
      "age_seconds": 0,
      "fetched_at": "2026-10-31T09:13:10Z",
      "error": "connection refused"
    },
    {
      "node": "spare",
      "kind": "ollama",
      "models": [],
      "known": false,
      "cached": false,
      "age_seconds": 0,
      "fetched_at": "",
      "error": "",
      "disabled": true
    }
  ],
  "check": {
    "model": "ollama/qwen3:27b",
    "applicable": true,
    "refused": false,
    "node_unknown": false,
    "served_by": ["gpu-1", "gpu-2"],
    "checked": [
      {"node": "gpu-1", "state": "serves", "reason": "serves qwen3:27b"},
      {"node": "gpu-2", "state": "unknown", "reason": "could not read models: connection refused"}
    ],
    "message": ""
  }
}
```

Errors: `503` when served models are not available on the daemon, `404` for an unknown `node`, `405` for any method but `GET`. Unknown fields should be ignored, as before.

### New Automaton actions

Both are `POST` and need the same write access as the other Automaton actions. Both return the updated Automaton (full view by default, `?view=compact` for the small write view), like the other `/api/autonomous/prds/{id}/...` actions.

`POST /api/autonomous/prds/{id}/set_task_node`

```json
{"task_id": "t1", "node": "gpu-1", "actor": "operator"}
```

`POST /api/autonomous/prds/{id}/set_story_node`

```json
{"story_id": "s1", "node": "gpu-1", "actor": "operator"}
```

| Field | Notes |
|---|---|
| `task_id` / `story_id` | required; a missing one is a `400` |
| `node` | a registered compute node name; empty clears the pin |
| `actor` | optional, defaults to `operator` |

The pin is accepted under the same status rule as changing a task's LLM. A refusal is a `400` (never a `5xx`) whose body is plain text that names the model and the nodes checked, for example:

```text
no registered node serves model "ollama/qwen3:27b"; nodes checked: gpu-1 (serves 2 models, none is ollama/qwen3:27b), gpu-2 (could not read models: connection refused)
```

or, for a pin to one node:

```text
node "gpu-1" does not serve model "ollama/qwen3:27b" (serves 2 models, none is ollama/qwen3:27b)
```

A node that is not registered is also a `400`: `cannot pin task t1 to node "gpu-9": compute node is not registered`. The app should show the body text as the refusal message. In `warn` mode the pin is applied, the answer is `200`, and the Automaton gets a `model_not_served_warning` entry in its decisions list.

### New field: pinned_node

`pinned_node` (string, omitted when empty) on every task and every story in the Automaton JSON: `GET /api/autonomous/prds`, `GET /api/autonomous/prds/{id}`, the answers of the two actions above, and the Automaton update events clients receive over the WebSocket. A task's effective node is its own `pinned_node`, else its story's. An absent field means no pin. Unknown fields should be ignored, so the app stays forward-compatible with older daemons.

### New settings block

`GET` and `PUT /api/config`, under `autonomous.model_routing`. `GET` always returns all six keys with defaults filled in. `PUT` takes dotted keys (`autonomous.model_routing.<key>`) and checks every key before applying any, so one bad value refuses the whole `PUT` with a message naming the key and the valid values.

| Key | Type | Default | Valid values |
|---|---|---|---|
| `autonomous.model_routing.refusal_mode` | string | `refuse` | `refuse` or `warn` |
| `autonomous.model_routing.served_models_cache_seconds` | integer | `300` | whole number from 0; `0` reads from the node every time |
| `autonomous.model_routing.verifier_follows_worker_node` | boolean | `true` | `true`, `false` |
| `autonomous.model_routing.unknown_node_policy` | string | `assume_serves` | `assume_serves` or `assume_not` (how a node whose models cannot be read counts) |
| `autonomous.model_routing.probe_timeout_seconds` | integer | `8` | whole number from 1 |
| `autonomous.model_routing.alert_on_refusal` | boolean | `true` | `true`, `false` |

### MCP, CLI and comm channel

- MCP: new read-only tool `compute_served_models` (arguments `node`, `model`, `backend`, `refresh`; same object as the REST read) and the write tools `autonomous_prd_set_task_node` and `autonomous_prd_set_story_node`.
- CLI and comm channel: a served-models command and a node pin command for a task and for a story; they print the same refusal text as the REST body.

### PWA (reference render)

- **Served models** block on every compute node row: the title, a Refresh button (calls the route with `refresh=true`), the node's models as small tags, and a line "Read just now" or "Read N seconds ago". A node with `known` false shows the "cannot be read" note with the daemon's `error` as a tooltip; a `disabled` node shows the "disabled" note; a node with no models shows the empty text; a failed read shows the error line.
- **Pin to node** select on the task editor and on the story editor: "No pin (any node)" first, then every registered compute node. A node that is pinned but no longer registered stays in the list so the pin can be seen and cleared. Saving posts to the matching action; a refusal shows the daemon's message in the usual error toast.
- **Node badge** "Node: name" on a task or story row while `pinned_node` is set, with the tooltip "Pinned to compute node name".
- **Settings fields** in the Automata settings block: the six keys above.

### New locale keys

The `de`, `es`, `fr` and `ja` PWA bundles carry the English text as a placeholder until the translations arrive. `%1$s` is a positional placeholder, kept as written.

| Key | EN value | Context |
|---|---|---|
| `model_routing_served_models_title` | Served models | block title |
| `model_routing_served_models_empty` | This node serves no models. | block, node with no models |
| `model_routing_served_models_failed` | Could not read the served models: %1$s | block, failed read; `%1$s` is the error |
| `model_routing_refresh` | Refresh | button that reads the node again |
| `model_routing_unknown_node` | The models on this node cannot be read right now, so the unreachable node policy decides how the node counts. | block, `known` false |
| `model_routing_node_disabled` | This node is disabled, so no models are read from it. | block, `disabled` true |
| `model_routing_fresh` | Read just now | block, age line, not cached |
| `model_routing_age` | Read %1$s seconds ago | block, age line; `%1$s` is `age_seconds` |
| `model_routing_pin_node_label` | Pin to node | task and story editor label |
| `model_routing_pin_node_none` | No pin (any node) | pin select, first option |
| `model_routing_pin_node_hint` | Runs the worker on this compute node only. A node that does not serve the model is refused unless the refusal mode is warn. | pin select hint |
| `model_routing_pinned_badge` | Node: %1$s | badge on a pinned task or story; `%1$s` is the node |
| `model_routing_pinned_title` | Pinned to compute node %1$s | badge tooltip |
| `settings_model_routing_refusal_mode` | Model routing refusal mode (refuse or warn, default refuse) | settings label |
| `settings_model_routing_refusal_mode_hint` | Refuse stops a run whose node does not serve its model. Warn lets the run start and raises a warning. | settings hint |
| `settings_model_routing_refusal_refuse` | Refuse the run | select option |
| `settings_model_routing_refusal_warn` | Warn and continue | select option |
| `settings_model_routing_cache_seconds` | Served models cache age (seconds, whole number, 0 reads the node every time, default 300) | settings label |
| `settings_model_routing_cache_seconds_hint` | How long the models a node serves are remembered before the node is read again. 0 reads the node every time. | settings hint |
| `settings_model_routing_verifier_follows` | Verifier follows the worker node (on by default) | settings label |
| `settings_model_routing_verifier_follows_hint` | The verifier of a task runs on the node its worker used, so a model that is only served there is still found. | settings hint |
| `settings_model_routing_unknown_node_policy` | Unreachable node policy (assume_serves or assume_not, default assume_serves) | settings label; the two values stay untranslated |
| `settings_model_routing_unknown_node_policy_hint` | How a node whose models cannot be read counts: as serving the model, or as not serving it. | settings hint |
| `settings_model_routing_policy_assume_serves` | Assume it serves the model | select option |
| `settings_model_routing_policy_assume_not` | Assume it does not serve the model | select option |
| `settings_model_routing_probe_timeout` | Node probe timeout (seconds, whole number, minimum 1, default 8) | settings label |
| `settings_model_routing_probe_timeout_hint` | How long one read of a node's models may take before the node counts as unreachable. | settings hint |
| `settings_model_routing_alert` | Alert when a run is refused for its model (on by default) | settings label |
| `settings_model_routing_alert_hint` | Raises an alert when a task is refused because its node does not serve the model. | settings hint |

Please send translations of these 29 keys into DE, ES, FR and JA through the usual Compose Multiplatform pipeline (use the app's own word for Automaton: de Automat, es autómata, fr automate, ja オートマトン); the PWA bundles will then mirror them in place of the placeholders.

### Acceptance (mobile)

- Each compute node shows the models it serves from `GET /api/compute/served-models`, with the age line ("Read just now" or "Read N seconds ago") and a Refresh button that calls the route with `refresh=true`.
- A node with `known` false shows the "cannot be read" note with `error` as detail; a `disabled` node shows the disabled note; an empty `models` list on a known node shows the empty text; a failed read shows an error line built from `model_routing_served_models_failed`; a `503` hides the block or shows that error line.
- A task and a story each have a "Pin to node" control listing "No pin (any node)" and the registered compute nodes, preselected from `pinned_node`; saving posts to `set_task_node` or `set_story_node`; choosing "No pin" sends an empty `node`.
- A refused pin (`400`) shows the daemon's message, which names the model and the nodes, and leaves the control on the saved value.
- A task or story row shows the "Node: name" badge while `pinned_node` is set, and nothing when it is absent.
- The six settings read their current values from the daemon config and write them back through the config endpoint; an absent field falls back to the default in the table; the two select fields offer the options in the locale table; a refused `PUT` shows the daemon's message.
- Every label, hint, badge and button is localized in EN, DE, ES, FR and JA, with every user-visible string saying Automaton or Automata.
- Unknown or absent new fields render exactly as today, so the app stays forward-compatible with older daemons.

## Filing record

Checked for an open issue first: `gh issue list -R dmz006/datawatch-app --state all --search 'model routing'` on 2026-10-31 returned only unrelated issues, so no earlier issue was commented on.

Filed: https://github.com/dmz006/datawatch-app/issues/321 (2026-10-31, via `gh issue create -R dmz006/datawatch-app --title "<the title above>" --body-file docs/plans/parity-issues/model-routing.md`). Filed exactly once.

---
docs:
  index: true
  topics: [alert-rules, observer, autoscaling]
exec_params: []
exec_steps:
  - tool: alert_rule_list
    description: List all alert rules
    args: {}
    read_only: true
  - tool: alert_rule_firings
    description: View recent rule firings
    args: {}
    read_only: true
---
# How-to: Per-Pod Alert Rules + Autoscaling (S14b)

Per-pod alert rules let you define metric thresholds that datawatch
evaluates continuously against observer envelopes. When a threshold is
crossed you can fire a system alert, scale a compute node up, or scale
it down — automatically, without writing a custom script.

Rules are evaluated every 30 seconds. The last 100 firings are kept in
a ring buffer you can query at any time.

## What it is

Each `AlertRule` has:

- A **condition** — one metric, one comparison operator, one numeric
  threshold.
- An optional **source filter** — restrict evaluation to a specific
  compute node by name. Omit to evaluate across all envelopes.
- A **window** — look-back period in seconds for metric aggregation
  (default 60 s).
- An **action** — what to do when the condition is met: emit a system
  alert, scale the target node up, or scale it down.
- A **cooldown** — minimum seconds between two consecutive firings of
  the same rule (default 300 s). Prevents flapping.

Rules are persisted in `~/.datawatch/alert-rules.yaml` and survive
daemon restarts.

## Base requirements

- `datawatch` daemon running and reachable at `https://<host>:8443`.
- At least one observer peer active and forwarding envelopes (see
  [federated-observer.md](federated-observer.md)).
- For autoscaling actions: at least one compute node registered (see
  [compute-nodes.md](compute-nodes.md)).

## Setup

No additional configuration is required. The alert-rules evaluator
starts automatically with the daemon. Rules you create are active
immediately once enabled.

## Create your first rule

### Via CLI

```sh
# Add a rule that alerts when any node's CPU exceeds 85 % for 60 s.
datawatch alert-rules add \
  --name high-cpu \
  --description "Alert when CPU is sustained above 85%" \
  --metric cpu_pct \
  --operator ">" \
  --threshold 85 \
  --window 60 \
  --action alert \
  --enabled

# Add a rule that scales up a specific node when CPU stays above 90 %.
datawatch alert-rules add \
  --name cpu-autoscale-up \
  --metric cpu_pct \
  --operator ">" \
  --threshold 90 \
  --window 120 \
  --action scale_up \
  --scale-target my-gpu-node \
  --scale-amount 1 \
  --cooldown 600 \
  --enabled

# Add a rule that scales down when CPU drops below 20 %.
datawatch alert-rules add \
  --name cpu-autoscale-down \
  --metric cpu_pct \
  --operator "<" \
  --threshold 20 \
  --window 300 \
  --action scale_down \
  --scale-target my-gpu-node \
  --scale-amount 1 \
  --cooldown 900 \
  --enabled
```

### Via YAML

You can write rules directly to `~/.datawatch/alert-rules.yaml`:

```yaml
# ~/.datawatch/alert-rules.yaml

- name: high-cpu
  description: Alert when CPU is sustained above 85%
  condition:
    metric: cpu_pct
    operator: ">"
    threshold: 85
  window_seconds: 60
  action:
    kind: alert
  enabled: true
  cooldown_seconds: 300

- name: cpu-autoscale-up
  description: Scale up my-gpu-node when CPU stays above 90% for 2 min
  condition:
    metric: cpu_pct
    operator: ">"
    threshold: 90
  source_filter: my-gpu-node
  window_seconds: 120
  action:
    kind: scale_up
    scale_target: my-gpu-node
    scale_amount: 1
  enabled: true
  cooldown_seconds: 600

- name: mem-alert
  description: Alert when RSS exceeds 8 GiB
  condition:
    metric: rss_bytes
    operator: ">="
    threshold: 8589934592   # 8 * 1024^3
  window_seconds: 60
  action:
    kind: alert
  enabled: true
  cooldown_seconds: 300
```

After editing the YAML directly, reload:

```sh
datawatch reload
```

## Metrics reference

| Metric | Unit | Description |
|--------|------|-------------|
| `cpu_pct` | % (0–100) | CPU utilisation across all cores |
| `mem_pct` | % (0–100) | Memory utilisation as a fraction of total |
| `gpu_pct` | % (0–100) | GPU utilisation (first device) |
| `rss_bytes` | bytes | Resident set size |
| `net_rx_bps` | bytes/s | Network ingress rate |
| `net_tx_bps` | bytes/s | Network egress rate |

## Operators reference

| Operator | Meaning |
|----------|---------|
| `>` | metric strictly greater than threshold |
| `<` | metric strictly less than threshold |
| `>=` | metric greater than or equal to threshold |
| `<=` | metric less than or equal to threshold |

## Actions reference

| Kind | Effect | Required extra fields |
|------|--------|-----------------------|
| `alert` | Emits a system alert to the alert dock | — |
| `scale_up` | Adds `scale_amount` container instances to `scale_target` | `scale_target`, `scale_amount` |
| `scale_down` | Removes `scale_amount` container instances from `scale_target` | `scale_target`, `scale_amount` |

`scale_amount` defaults to 1 when omitted. `scale_target` must match
a registered compute node name exactly.

## Cooldown

Cooldown prevents the same rule from re-firing continuously while the
condition remains true. After a rule fires, the evaluator suppresses
further firings of that rule until `cooldown_seconds` have elapsed.

Default cooldown is 300 s (5 min). For autoscaling rules it is
recommended to set a longer cooldown (≥ 600 s) to allow the cluster
to stabilise between scale events.

## Manage rules

```sh
# List all rules.
datawatch alert-rules list
#  → NAME               ENABLED  METRIC    OP   THRESHOLD  ACTION
#    high-cpu           true     cpu_pct   >    85         alert
#    cpu-autoscale-up   true     cpu_pct   >    90         scale_up → my-gpu-node
#    cpu-autoscale-down true     cpu_pct   <    20         scale_down → my-gpu-node
#    mem-alert          false    rss_bytes >=   8589934592 alert

# Get a single rule.
datawatch alert-rules get high-cpu

# Update a rule (any field; unset fields are preserved).
datawatch alert-rules update high-cpu --threshold 90

# Enable / disable a rule.
datawatch alert-rules enable  high-cpu
datawatch alert-rules disable high-cpu

# Delete a rule.
datawatch alert-rules delete high-cpu
```

## View firings

```sh
# Show the last 100 firings (ring buffer).
datawatch alert-rules firings
#  → RULE              ENVELOPE   SOURCE         VALUE    FIRED_AT             ACTION    RESULT
#    high-cpu          env-abc    my-gpu-node    91.2     2026-05-19T10:14:00Z alert     ok
#    cpu-autoscale-up  env-def    my-gpu-node    93.5     2026-05-19T10:12:30Z scale_up  scaled +1
```

Firings are kept in memory only (ring buffer of 100). They are not
persisted across daemon restarts.

## All surfaces

### REST

```sh
export BASE=https://localhost:8443
export TOKEN=<your_bearer_token>

# List all rules.
curl -sk -H "Authorization: Bearer $TOKEN" "$BASE/api/alert-rules"

# Get one rule.
curl -sk -H "Authorization: Bearer $TOKEN" "$BASE/api/alert-rules/high-cpu"

# Create a rule.
curl -sk -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "high-cpu",
    "condition": {"metric": "cpu_pct", "operator": ">", "threshold": 85},
    "window_seconds": 60,
    "action": {"kind": "alert"},
    "enabled": true,
    "cooldown_seconds": 300
  }' \
  "$BASE/api/alert-rules"

# Update a rule.
curl -sk -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"threshold": 90}' \
  "$BASE/api/alert-rules/high-cpu"

# Delete a rule.
curl -sk -X DELETE -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/alert-rules/high-cpu"

# Enable / disable.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/alert-rules/high-cpu/enable"
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  "$BASE/api/alert-rules/high-cpu/disable"

# List recent firings.
curl -sk -H "Authorization: Bearer $TOKEN" "$BASE/api/alert-rules/firings"
```

REST endpoints summary:

```
GET    /api/alert-rules                  list all rules
POST   /api/alert-rules                  create a rule
GET    /api/alert-rules/{name}           get one rule
PUT    /api/alert-rules/{name}           update a rule
DELETE /api/alert-rules/{name}           delete a rule
POST   /api/alert-rules/{name}/enable    enable a rule
POST   /api/alert-rules/{name}/disable   disable a rule
GET    /api/alert-rules/firings          last 100 firings
```

### MCP

| Tool | Description |
|------|-------------|
| `alert_rule_list` | List all alert rules |
| `alert_rule_get` | Get one rule by name |
| `alert_rule_create` | Create a new rule |
| `alert_rule_update` | Update fields on an existing rule |
| `alert_rule_delete` | Delete a rule |
| `alert_rule_enable` | Enable a rule |
| `alert_rule_disable` | Disable a rule |
| `alert_rule_firings` | List the last 100 firings |

Example (Claude Code):

```
alert_rule_list
alert_rule_create name=high-cpu condition.metric=cpu_pct condition.operator=">" condition.threshold=85 action.kind=alert enabled=true
alert_rule_firings
```

### CLI

```
datawatch alert-rules list
datawatch alert-rules get <name>
datawatch alert-rules add   [--name] [--metric] [--operator] [--threshold] [--window] [--action] [--scale-target] [--scale-amount] [--cooldown] [--source-filter] [--enabled]
datawatch alert-rules update <name> [<same flags>]
datawatch alert-rules delete <name>
datawatch alert-rules enable  <name>
datawatch alert-rules disable <name>
datawatch alert-rules firings
```

## Common pitfalls

- **Rule fires immediately on creation.** If the condition is already
  true at the moment you enable the rule it will fire on the next
  30-second evaluation cycle. Set a longer `window_seconds` if you
  want a sustained condition before the first firing.

- **Autoscaling action returns "node not found".** The `scale_target`
  must match the compute node name exactly as shown in
  `datawatch compute-nodes list`. Names are case-sensitive.

- **No firings despite condition being met.** Check that the rule is
  enabled (`datawatch alert-rules get <name>`) and that observer
  envelopes are arriving (`datawatch observer envelopes`). If
  `source_filter` is set, it must match the `Source` field on the
  envelope exactly.

- **Rule fires once then goes quiet.** The cooldown is active. The
  rule will not re-fire until `cooldown_seconds` have elapsed since the
  last firing. Lower `cooldown_seconds` or check the firings log for
  the last fired timestamp.

- **`rss_bytes` threshold is always exceeded.** `rss_bytes` is in
  bytes. 8 GB = `8589934592`. Use `mem_pct` for percentage-based
  thresholds instead.

- **Firings lost after daemon restart.** Firings are in-memory only
  (ring buffer of 100). Only the rules themselves are persisted to
  `~/.datawatch/alert-rules.yaml`.

## Diagram

```
  observer envelopes (every 30 s)
         │
         ▼
  ┌──────────────────────────────────────────┐
  │  AlertRule evaluator                     │
  │                                          │
  │  for each rule (enabled):                │
  │    filter envelopes by source_filter     │
  │    aggregate metric over window_seconds  │
  │    compare against threshold             │
  │    if condition met AND cooldown elapsed:│
  │      fire action                         │
  └──────────────────────────────────────────┘
         │                  │
         ▼                  ▼
  action=alert        action=scale_up/down
         │                  │
         ▼                  ▼
  alerts.Store        ScaleNode stub
  (alert dock,        (compute node
   REST, MCP,          container count)
   mobile push)

  ring buffer (last 100 firings)
  ← GET /api/alert-rules/firings
  ← datawatch alert-rules firings
  ← alert_rule_firings (MCP)
```

---

## See also

- [howto/federated-observer](federated-observer.md) — observer peers and envelope delivery.
- [howto/compute-nodes](compute-nodes.md) — compute node registration; required for `scale_up`/`scale_down` actions.
- [howto/alerts-and-notifications](alerts-and-notifications.md) — the alert dock that receives `action=alert` firings.
- [datawatch-definitions](../datawatch-definitions.md) — glossary of system terms.

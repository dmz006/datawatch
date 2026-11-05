---
docs:
  index: true
  topics: [automation, requirements, ollama, compute, backends]
exec_params:
  - name: format
    description: "text | json"
    required: false
    default: "text"
exec_steps:
  - tool: automation_requirements_report
    description: Show the automation backend requirements report
    args:
      format: "{{params.format}}"
    read_only: true
---
# How-to: Automation requirements

Can this daemon run the automations it is configured to run? The
requirements report answers in four sections: the Ollama minimums against
every node, the backends configured for each workload, and everything that
is unmet. It is a report, not a gate — nothing is blocked on it — but every
read surface shows the same lines, so you fix once and see it on all of
them.

## The requirement minimums

A minimum is a floor on a backend version for a named capability. The
built-in one:

| Capability | Minimum | Why |
|---|---|---|
| `classification_verdict` | **Ollama 0.35.0** | Shipped `systemone`, the verdict primitive the CI-watch / triage classification rides on |

An operator override raises a floor: `autonomous.requirements.min_ollama.classification_verdict: "0.40.0"` in `datawatch.yaml`. The effective minimum is the override when present, otherwise the built-in.

Each registered Compute Node that is an Ollama backend gets a verdict per
capability:

- **meets** — the node reports a version at or above the minimum,
- **fails** — it reports a lower one (the line includes the reported version),
- **unknown** — the node has not reported a version yet.

## The backends

Automation needs a backend for each workload slot:

| Slot | Required |
|---|---|
| **execution** | yes — at least one, automation runs its members on it |
| **planning** | yes — PRD decomposition and planning calls land here |
| **verification** | yes — result verification runs here |
| Claude session pool | reported — the pool of Claude-backed session lanes |

Each configured backend name is listed; every empty slot appears in
**missing** with a human-readable line (`automation: no execution backend
configured (at least one required)` …).

## Reading the report

### CLI

```sh
# Text (default): sections, verdicts, missing lines.
datawatch automation requirements-report

# JSON: the full body — capabilities, nodes, backends, missing[], ok.
datawatch automation requirements-report --format json
```

### MCP

```
automation_requirements_report format=text
automation_requirements_report format=json
```

### Comm channel

```sh
autonomous requirements-report [text|json]
```

### REST

```sh
# Text sections.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/automation/requirements-report

# The full JSON body.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/automation/requirements-report?format=json

# One node's backend version and its per-capability verdicts.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/compute/nodes/gpu-box/versions
```

## Node backend versions

`datawatch compute node versions <name>` (or
`compute_node_backend_versions name=<name>` on MCP, or
`autonomous node-versions <name>` on the comm channel) answers for one
node: which backend it exposes, what version it reports, when the last
probe ran, and which capability minimums that version meets or fails.

## The happy path

```sh
# 1. Where do we stand?
datawatch automation requirements-report
# → Ollama minimums
#     classification_verdict: minimum Ollama 0.35.0
#     gpu-box: Ollama 0.36.1 — meets
#     cpu-box: no reported version — unknown
# → Backends
#     execution:    ollama-gpu
#     planning:     claude-api
#     verification: claude-api
# → Missing
#     (none)

# 2. A node that fails:
datawatch compute node versions cpu-box
# → Ollama 0.3.9 (probed 2026-11-01T10:12:00Z)
#    classification_verdict: minimum Ollama 0.35.0 — fails (0.3.9)

# 3. Fix (upgrade the Ollama on that host, or lower the exposure by
#    removing it from the pool). Recheck:
datawatch automation requirements-report
# → cpu-box: Ollama 0.36.1 — meets
# → Missing: (none)
```

## Where the pieces come from

- **The minimums** — `internal/autonomous` capability minimums (built-in
  floor `0.35.0` for `classification_verdict`) plus the
  `autonomous.requirements.min_ollama` overrides.
- **The node versions** — the Compute Node registry and its per-node
  backend probes (`compute node get <name>` shows the stored node).
- **The backends** — the automation configuration (execution / planning /
  verification slots) and the Claude session pool.
- **The onboarding check** — the onboarding report carries a
  **requirements** check ("The automation backend requirements are met")
  that surfaces the same missing lines with a fix step; on a surface that
  does not provide the requirements source at all, the check reports as
  optional rather than missing.

## Common pitfalls

- **"unknown" is not a failure.** A node that never reported a version is
  not failing a minimum — it is unverified. Upgrade, re-probe, or note it.
- **Raised by override, not by default.** Raising the floor is
  deliberate: `min_ollama` overrides are per-capability and there is one
  built-in (`classification_verdict` at 0.35.0). Setting the key
  `min_ollama: "0.40.0"` at the top level is a config error, not a
  wildcard — overrides are by capability name.
- **The report is not a gate.** A node that fails a minimum can still be
  routed to it; the report names the gap so *you* decide (upgrade,
  reassign, or accept).
- **`ok` vs. empty `missing`.** `ok: true` on the JSON body is exactly
  "missing is empty"; they are two views of the same fact.

---

## See also

- [datawatch-definitions — Automation requirements](../datawatch-definitions.md#automation-requirements)
- [howto/llm-registry](llm-registry.md)
- [howto/model-pull-consent](model-pull-consent.md)
- [howto/compute-nodes](compute-nodes.md)

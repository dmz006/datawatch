---
docs:
  index: true
  topics: [automation, requirements, release, ollama, backends, consent]
exec_params:
  - name: format
    description: "text | json"
    required: false
    default: "text"
exec_steps:
  - tool: automation_requirements_report
    description: Read the automation requirements report (Ollama minimums per capability, node versions, backend minimums, missing)
    args:
      format: "{{params.format}}"
    read_only: true
  - tool: automation_model_pull_consent_get
    description: Read the model-pull consent policy (model list, publisher allow-list, decision rule)
    read_only: true
---
# How-to: Major release requirements (operator checklist)

The upcoming major release (the next patch above `main`) carries a set of
requirements a daemon must meet before it can run the automations it is
configured to run: a **minimum Ollama version per capability**, a **minimum
backend layout** (execution + planning + verification, plus at least one of
Claude or the local model pool), and a **model-pull consent policy** with a
publisher allow-list. Nothing here is invented per installation — every
requirement is *reported*, on every surface, by the same requirements
report. This guide states the requirements and walks you through reading
the report in onboarding, the CLI and the web, so an operator picking up
the release on a fresh box can check the box item by item.

> The minimum versions themselves are defined in the daemon's built-in
> table and surfaced by the report — this guide deliberately does not
> pin a number. Always read the effective minimum for a capability from
> the report; if you override it, the report marks the line as an
> operator override.

## 1. Minimum Ollama version per capability

A *capability* is a named feature that depends on a backend shipping a
specific primitive. Each capability with a shipped minimum carries a floor
on the Ollama version a node must report. The built-in table (keyed by
capability name) ships in the code; the capability present in the upcoming
release is:

| Capability | What it is |
|---|---|
| `classification_verdict` | The classification-based verdict that judges run outcomes through Ollama's decision endpoint — the primitive the CI-watch / triage classification rides on |

The effective minimum for a capability is the **operator override when
present, otherwise the built-in floor**. The report shows one line per
capability with the effective minimum and a marker when it came from an
override.

### Enforcement

- **Enabling on a node below the minimum is refused.** The refusal names
  the node, the version it reports and the version required.
- **A node that later falls below the minimum is flagged.** The capability
  then *fails closed* on that node — its status stays unavailable until a
  recheck finds the node back at or above the minimum.
- **Unknown versions fail closed.** A node that reports a version the
  daemon cannot parse does not satisfy the minimum.

### The operator override key

Per-capability overrides live under one key, named by capability:

```yaml
autonomous:
  requirements:
    min_ollama:
      classification_verdict: "<X.Y.Z>"  # raise this capability's floor
```

- The key is `autonomous.requirements.min_ollama.<capability>` — the
  capability name is part of the key, so there is no single global
  override. A version written at the `min_ollama` level (not under a
  capability name) is a config error, not a wildcard.
- An override **replaces** the built-in floor for that capability.
- An **empty value clears the floor** for that capability entirely (the
  report then shows "no floor" for it).
- Raising a floor is deliberate: use it to make every node meet a newer
  minimum, or clear it where the capability is not needed on old nodes.

## 2. The minimum backends rule

Automation needs a backend for each workload slot, plus at least one of
the two model providers:

| Requirement | Rule |
|---|---|
| **execution** backend | required — at least one; automation runs its members on it |
| **planning** backend | required — PRD decomposition and planning calls land here |
| **verification** backend | required — result verification runs here |
| **Claude** | optional — but at least one of Claude *or* the local model pool is required |
| **local model pool** | optional — at least one of Claude *or* the local model pool is required |

Each unmet requirement appears in the report's **missing** list with a
human-readable line (for example: `automation: no execution backend
configured (at least one required)`, and `automation: neither a Claude
backend nor a local model pool is configured (each optional, at least one
required)`).

## 3. Model pull consent and the publisher allow-list

**Model pull consent** (`autonomous.model_pull_consent`) is the gate on
every model pull a compute node or the marketplace makes. Two kinds of
allow entries, either of which permits a pull:

- **`models[]`** — model-specific entries: `name`, `allowed` (bool) and a
  free-text `note`. A listed model with `allowed: false` is an explicit
  refusal, shown with its note.
- **`publishers[]`** — the **publisher allow-list**: one entry allows
  every model of that publisher.

The rule is default-deny: a pull is allowed when the model is listed with
`allowed: true`, or when the model is unlisted and its publisher is on the
allow-list; **every other pull is refused**, and the refusal tells the
operator exactly which key to set.

```yaml
autonomous:
  model_pull_consent:
    models:
      - name: "gpt-oss:120b"
        allowed: true
      - name: "legacy-tiny"
        allowed: false
        note: "superseded by the pool default"
    publishers:
      - "mistralai"
```

CLI flags for the same policy: `--model <name>` (allow),
`--deny <name>` (record a refusal), `--publisher <name>` (allow-list a
publisher), `--clear` (wipe). A `set` **replaces** the stored policy
completely — compose the whole list in one command. The full surface tour
is in [howto/model-pull-consent](model-pull-consent.md).

## 4. Reading the requirements report

One report, four sections — capabilities with their minimums and nodes,
the backend minimums, and every unmet requirement — and the same body on
every surface.

### In onboarding

The onboarding report carries a check — **"The automation backend
requirements are met"** — that shows the same unmet lines. When
requirements are unmet, the check reports the missing count and the
details, and its fix step is *Meet the automation backend requirements*
with the commands below. On a surface that does not provide the
requirements source at all, the check reports as **optional**, not
missing.

```sh
# CLI: the onboarding report, with the requirements check inside it.
datawatch automation onboarding-report

# The same card is on the PWA Automation tab (the onboarding report card)
# and on MCP / REST via the onboarding report read.
```

### In the CLI

```sh
# The requirements report — text (default) or the full JSON body.
datawatch automation requirements-report
datawatch automation requirements-report --format json
# → per capability: the effective Ollama minimum (built-in or operator
#   override), each node's reported version and its state
#     (enabled, meets the minimum / flagged, fails closed), satisfied?
# → automation backends: execution / planning / verification,
#   claude configured, local model pool configured
# → "All requirements are met." or "N requirement(s) unmet:" + the lines

# One node's backend version and its per-capability verdicts.
datawatch compute node versions <node>

# The current consent policy.
datawatch automation model-consent get
```

### In the web

The PWA setup view (the Automata settings group) carries two cards:

- **Requirements report** — each capability with its effective Ollama
  minimum (marked when it is an operator override), each node with an
  *ok* / *flagged* badge and the version it reports, the backend
  minimums, and the missing list. A refresh button re-reads the report.
- **Model pull consent** — the per-model pull-allowed toggles with notes
  and the publisher allow-list, editable in place.

The onboarding report card on the Automation tab shows the same
requirements check described above.

### The other surfaces

- **MCP** — `automation_requirements_report` (format `text` or `json`),
  `automation_model_pull_consent_get`, `automation_model_pull_consent_set`.
- **Comm channel** — `autonomous requirements-report [text|json]`,
  `autonomous model-pull-consent-get|set`, `autonomous node-versions <node>`.
- **REST** — `GET /api/automation/requirements-report`
  (`?format=text` for the text rendering), `GET/PUT
  /api/automation/model-consent`, `GET /api/compute/nodes/<name>/versions`.

## Walkthrough: checking the box for the release

```sh
# 1. Where do we stand?
datawatch automation requirements-report
# → capabilities: each minimum (built-in or override) and the nodes under it
# → backends:    execution / planning / verification / claude / local pool
# → missing:     the lines you still owe, or "All requirements are met."

# 2. A node below a capability minimum (upgrade Ollama on that host, or
#    lower/clear the floor if the capability is not needed there):
datawatch compute node versions <node>
# → the reported version, the required minimum, the verdict

# 3. Consent: allow the models this box will pull and any publishers.
datawatch automation model-consent set \
  --model <model-name> --publisher <publisher>
datawatch automation model-consent get

# 4. Recheck until the missing list is empty (JSON: ok: true).
datawatch automation requirements-report --format json | jq .ok
# → true
```

## Common pitfalls

- **Do not copy version numbers between boxes.** The effective minimum is
  per capability and per daemon (built-in table plus local overrides).
  Read it from this daemon's report before you judge a node.
- **Overrides are per capability.** There is no single "minimum Ollama"
  key; `min_ollama` holds capability keys. An empty value clears that
  capability's floor — that is a statement, not a typo.
- **A flagged node fails closed, it does not just warn.** Once a node
  falls below a minimum, the capability on that node stays unavailable
  until a recheck finds it back at or above the minimum.
- **The report is a report, not a gate — except at enablement.** Nothing
  in the report blocks your automation, but enabling a capability on a
  node below its minimum is refused outright, and model pulls are gated
  by consent regardless of the report.
- **`set` replaces the consent policy.** Forgetting a model from the
  previous list drops it. Compose the complete list each time.
- **`ok: true` is exactly "missing is empty".** Two views of the same
  fact; if one says fine and the other says otherwise, you are reading
  two different daemons.

---

## See also

- [howto/automation-requirements](automation-requirements.md) — the per-surface tour of the report itself
- [howto/model-pull-consent](model-pull-consent.md) — the consent policy on every surface
- [howto/onboarding-report](onboarding-report.md) — reading the onboarding report, including the requirements check
- [howto/compute-nodes](compute-nodes.md) — the node registry and its backend probes
- [datawatch-definitions — Automation requirements](../datawatch-definitions.md#automation-requirements)

---
docs:
  index: true
  topics: [guardrails, autonomous, automata, security, scan]
exec_params:
  - {name: automaton_id, required: true, description: "Automaton ID to run a guardrail scan on"}
exec_steps:
  - tool: autonomous_prd_scan
    description: Run all registered guardrail scans on the Automaton
    args: {id: "{{params.automaton_id}}"}
  - tool: autonomous_prd_scan_results
    description: Retrieve scan results and verdicts
    args: {id: "{{params.automaton_id}}"}
---
# How-to: Guardrail Library

Datawatch ships a guardrail library — a registry of named checks that
can be attached to Automata, Automaton profiles, or the global
autonomous config. Scans (SAST, secrets, dependency) and skill-declared
guardrails all live in the same registry.

## What is a guardrail?

A guardrail is a named gate that runs at a specified point in an
Automaton's execution (per-task or per-story) and returns a verdict:

| Verdict | Meaning |
|---------|---------|
| `pass`  | All clear — execution continues |
| `warn`  | Issues found but below the configured severity threshold |
| `block` | Execution paused; operator action required |

## Built-in guardrails

| Name | Type | What it checks |
|------|------|---------------|
| `sast-scan` | scan | Static analysis (Semgrep / CodeQL) |
| `secrets-scan` | scan | Secret / credential exposure |
| `deps-scan` | scan | Dependency vulnerabilities |

Skills can also declare additional guardrails via their manifest.

## Priority resolution

For each Automaton the effective guardrail list is resolved in priority
order:

1. **Explicit per-Automaton override** (`per_task_guardrails` /
   `per_story_guardrails` set directly on the Automaton)
2. **Named profile** assigned to the Automaton (`guardrail_profile`)
3. **Global config** (`autonomous.per_task_guardrails` /
   `autonomous.per_story_guardrails`)

## Managing guardrail profiles

A profile is a named, reusable collection of guardrails that you assign
to one or more Automata.

### CLI

```sh
# List the library.
datawatch guardrail library

# Create a profile.
datawatch guardrail profile create --name security-strict \
  --guardrails sast-scan,secrets-scan,deps-scan

# List profiles.
datawatch guardrail profile list

# Get one profile.
datawatch guardrail profile get <id>

# Update a profile.
datawatch guardrail profile update <id> \
  --guardrails sast-scan,deps-scan

# Delete a profile.
datawatch guardrail profile delete <id>
```

### REST

```sh
BASE=https://localhost:8443

# List library.
curl -sk $BASE/api/autonomous/guardrails

# List profiles.
curl -sk $BASE/api/autonomous/guardrail_profiles

# Create.
curl -sk -X POST -H "Content-Type: application/json" \
  -d '{"name":"security-strict","guardrails":["sast-scan","secrets-scan","deps-scan"]}' \
  $BASE/api/autonomous/guardrail_profiles

# Get / Update / Delete.
curl -sk $BASE/api/autonomous/guardrail_profiles/<id>
curl -sk -X PUT  -H "Content-Type: application/json" \
  -d '{"name":"security-strict","guardrails":["sast-scan"]}' \
  $BASE/api/autonomous/guardrail_profiles/<id>
curl -sk -X DELETE $BASE/api/autonomous/guardrail_profiles/<id>
```

### MCP

```
guardrail_library_list        → list all registered guardrails
guardrail_profile_list        → list profiles
guardrail_profile_create      → create a profile (name + guardrails_json)
guardrail_profile_get         → get one profile by id
guardrail_profile_update      → update name / description / guardrails
guardrail_profile_delete      → delete by id
per_automaton_guardrails_set  → set per-Automaton override
```

### Comm channel

```
You: guardrail library
Bot: Guardrail library:
     sast-scan    [scan] Static analysis
     secrets-scan [scan] Secrets detection
     deps-scan    [scan] Dependency vulnerabilities

You: guardrail profile list
Bot: [{"id":"a1b2","name":"security-strict",...}]

You: guardrail profile create security-strict sast-scan,secrets-scan
Bot: {"id":"a1b2","name":"security-strict","guardrails":["sast-scan","secrets-scan"]}

You: guardrail automaton set <id> profile=security-strict
Bot: {"id":"<id>","guardrail_profile":"security-strict"}
```

### PWA

Settings → **Automate** → **Guardrail Library** — lists all registered
guardrails with type badges.

Settings → **Automate** → **Guardrail Profiles** — CRUD for named
profiles. Click **New Profile** to create; × to delete.

### YAML

Global defaults in `~/.datawatch/config.yaml`:

```yaml
autonomous:
  per_task_guardrails:
    - sast-scan
    - secrets-scan
  per_story_guardrails:
    - deps-scan
```

Per-Automaton override stored in the Automaton record (set via any
surface above).

## Assigning a profile to an Automaton

```sh
# Assign a named profile (overrides global; explicit fields beat profile).
datawatch guardrail automaton set <automaton-id> --profile security-strict

# Explicit per-task list (beats the named profile).
datawatch guardrail automaton set <automaton-id> \
  --per-task sast-scan,secrets-scan \
  --per-story deps-scan
```

## Skill-contributed guardrails

Skills can declare guardrails in their `SKILL.md` frontmatter:

```yaml
---
guardrails:
  - name: my-custom-check
    description: "Validates output format"
    type: custom
---
```

The daemon registers these at startup for each loaded skill.

## See also

- [autonomous-planning.md](autonomous-planning.md) — Automaton lifecycle
- [autonomous-review-approve.md](autonomous-review-approve.md) — approval gates
- [api/autonomous.md](../api/autonomous.md) — REST reference

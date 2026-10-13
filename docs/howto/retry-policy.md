---
docs:
  index: true
  topics: [retry, autonomous, automata, guardrails]
---
# How-to: Retry Policy

Datawatch classifies every PRD-lifecycle failure by category and
applies a bounded, configurable retry policy per category, instead of
treating every failure identically.

## Why this exists

A guardrail or verifier call can fail two very different ways:

- **The mechanism itself failed** — a session timed out, crashed, or
  returned garbage. No real judgment ever ran. Retrying is usually the
  right move.
- **The mechanism ran and found a real problem** — a verifier or
  guardrail actually judged the work and found it lacking. That's
  working as designed, not a malfunction.

Before this, both cases produced an identical block outcome and
required the same manual operator intervention. Now they're
classified and handled differently.

## The four categories

| Category | Meaning |
|---|---|
| `infra` | The judging/executing mechanism itself never completed its job (session timeout/crash, unparseable response, a stalled worker). Retrying the identical call against a healthy session would likely produce a different outcome. |
| `content` | The mechanism ran to completion and produced a real judgment — the work itself was found deficient. |
| `scope` | The spec itself violates a structural constraint before any worker/judge acts on content. Retrying the identical spec unchanged reproduces the identical violation. |
| `policy` | An operator-configured gate (e.g. a quality-gate regression) deliberately intervened by design. |

## Default behavior

- `infra`: retried automatically, up to 4 times by default. The first
  2 retries keep your exact configured backend/model; retries beyond
  that fall back to the global default (your config may have been
  what was wrong).
- `content`: auto-corrective retry is enabled by default — a
  content-category block feeds its own findings back as a hint and
  retries, the same way task verification already does. Falls back to
  `auto_fix_retries` when no explicit budget is set.
- `scope`: no auto-retry by default (0) — an unchanged structurally
  invalid spec reproduces the identical violation.
- `policy`: same fallback as `content`.
- A **ceiling** caps total retries across every category for one
  PRD's whole lifecycle — default **5**. If a normal PRD fails more
  than that, the spec probably needs updating, or datawatch itself has
  a bug — not something one more retry fixes. Work that genuinely
  needs many iterations (e.g. a CVE test-and-fix PRD) should raise its
  own ceiling explicitly.

## Priority resolution

Same shape as guardrail resolution ([guardrail-library.md](guardrail-library.md)):

1. **Explicit per-Automaton override** (`retry_policy` set directly on
   the Automaton)
2. **Named retry profile** assigned to the Automaton (`retry_profile`)
3. **Global config** (`autonomous.retry_policy`)

## Managing retry profiles

A retry profile is a named, reusable policy — not just budget numbers,
but behavior: the ceiling, the infra escalation threshold, whether
content auto-fix is enabled, and whether synthesized corrective work
skips approval.

### REST

```sh
BASE=https://localhost:8443

# List profiles.
curl -sk $BASE/api/autonomous/retry_profiles

# Create.
curl -sk -X POST -H "Content-Type: application/json" \
  -d '{"name":"cve-work","description":"many iterations expected","budgets":{"infra":10,"content":10},"ceiling_total":30}' \
  $BASE/api/autonomous/retry_profiles

# Get / Update / Delete.
curl -sk $BASE/api/autonomous/retry_profiles/<id>
curl -sk -X PUT -H "Content-Type: application/json" \
  -d '{"name":"cve-work","budgets":{"infra":5},"ceiling_total":20}' \
  $BASE/api/autonomous/retry_profiles/<id>
curl -sk -X DELETE $BASE/api/autonomous/retry_profiles/<id>
```

### MCP

```
retry_profile_list                 → list profiles
retry_profile_create               → create (name + budgets_json + ceiling_total + ...)
retry_profile_get                  → get one profile by id
retry_profile_update               → update name / budgets / ceiling / behavior
retry_profile_delete               → delete by id
per_automaton_retry_policy_set     → set per-Automaton override
```

### PWA

Settings → **Automate** → **Retry Profiles** — lists all profiles with
their budgets; click **New Profile** to create, × to delete.

### YAML

Global defaults in `~/.datawatch/config.yaml`:

```yaml
autonomous:
  retry_policy:
    infra: 4
    scope: 0
  retry_ceiling_total: 5
  infra_retry_escalate_after: 2
  content_auto_fix_enabled: true
  auto_fix_skip_approval: true
```

Per-Automaton override stored in the Automaton record (set via any
surface above).

## Assigning a profile to an Automaton

```sh
# Assign a named profile (overrides global; explicit fields beat profile).
```

```
per_automaton_retry_policy_set id=<automaton-id> retry_profile=cve-work
```

## See also

- [guardrail-library.md](guardrail-library.md) — the sibling guardrail-resolution mechanism this mirrors
- [autonomous-planning.md](autonomous-planning.md) — Automaton lifecycle
- [api/autonomous.md](../api/autonomous.md) — REST reference

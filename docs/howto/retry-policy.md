---
docs:
  index: true
  topics: [retry, autonomous, automata, guardrails]
exec_params:
  - {name: automaton_id, required: true, description: "Automaton ID to inspect current retry-policy settings for"}
exec_steps:
  - tool: retry_profile_list
    description: List all named retry profiles
    args: {}
  - tool: autonomous_prd_get
    description: Retrieve the Automaton's current retry-policy fields (retry_profile, retry_policy, retry_ceiling_total, content_auto_fix_enabled, auto_fix_skip_approval)
    args: {id: "{{params.automaton_id}}"}
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

## What happens on a retry

- **`infra`**: the exact same guardrail/verifier call is retried, with
  no changes — see "Default behavior" above for how many times and
  with which backend/model.
- **`content`, at task or story level**: the block's own findings
  (`GuardrailVerdict.Issues`) are fed back as a hint, and the existing
  unit (task or story) is respawned with that hint — the same
  mechanism task verification already uses for `RetryHint`, not a new
  one.
- **`content`, at PRD-completion level** (e.g. `rules-review`, the
  deterministic project-rules check): there's no single task to
  respawn, so a new story + task are synthesized and appended **to
  the same Automaton** — never a new one — with a spec built directly
  from the verdict's findings. When `auto_fix_skip_approval` is true
  (the default), this synthesized work bypasses per-story approval
  entirely and runs unattended; when false, it enters
  `awaiting_approval` exactly like normal decompose output. Once it
  completes, the PRD-completion check re-fires automatically.
- **`infra`/`scope`, during decompose itself** (the planning LLM call
  failing, its response failing to parse, or a tooled plan skipping a
  required verification script): the SAME retry budget and categories
  apply here too — decompose is not a separate mechanism. The failed
  attempt's own error text is fed into the next attempt's prompt
  ("Your previous attempt failed: ..."), the same single-use-hint
  lifecycle a task's `RetryHint` already uses. The Automaton stays
  visibly `planning` while this happens; only a genuinely exhausted
  decompose attempt rolls it back to `draft` for an operator to
  revise. `scope`'s default budget of 0 means a verification-script
  violation still fails on the first attempt by default — an
  unchanged spec reproduces the identical violation, so retrying it
  automatically wouldn't help — but `content`-category reasoning
  never applies to decompose at all: there is no judgment verdict at
  this stage, only a mechanism succeeding or failing.

## When a retry budget is finally exhausted

A failure that genuinely never gets fixed — not one that self-heals
on a later retry — writes a `Learning` entry recording the category
and the detail of what kept failing. Retrieve these the same way you'd
retrieve any other learning:

```
autonomous_learnings id=<automaton-id>
memory_learnings
```

A transient failure that succeeds on a later retry writes nothing —
only genuine exhaustion is recorded, so this stays a signal, not noise.

## Managing retry profiles

A retry profile is a named, reusable policy — not just budget numbers,
but behavior: the ceiling, the infra escalation threshold, whether
content auto-fix is enabled, and whether synthesized corrective work
skips approval.

### CLI

```sh
# List profiles.
datawatch retry profile list

# Create.
datawatch retry profile create --name cve-work \
  --description "many iterations expected" \
  --budgets "infra=10,content=10" --ceiling-total 30

# Get one profile.
datawatch retry profile get <id>

# Update a profile.
datawatch retry profile update <id> --budgets "infra=5" --ceiling-total 20

# Delete a profile.
datawatch retry profile delete <id>

# Assign a profile (or explicit overrides) to an Automaton.
datawatch retry automaton set <automaton-id> --profile cve-work
datawatch retry automaton set <automaton-id> \
  --policy "infra=4,content=1" --ceiling-total 10 \
  --content-auto-fix=true --auto-fix-skip-approval=true
```

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

### Comm channel

```
You: retry profile list
Bot: [{"id":"a1b2","name":"cve-work","budgets":{"infra":10,"content":10},"ceiling_total":30}]

You: retry profile create cve-work infra=10,content=10 ceiling=30
Bot: {"id":"a1b2","name":"cve-work","budgets":{"infra":10,"content":10},"ceiling_total":30}

You: retry profile get a1b2
Bot: {"id":"a1b2","name":"cve-work",...}

You: retry profile delete a1b2
Bot: deleted

You: retry automaton set <id> profile=cve-work policy=infra=4 ceiling=10
Bot: {"id":"<id>","retry_profile":"cve-work","retry_policy":{"infra":4},"retry_ceiling_total":10}
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
surface above — see the CLI section for the explicit-vs-profile
precedence rule in practice).

## See also

- [guardrail-library.md](guardrail-library.md) — the sibling guardrail-resolution mechanism this mirrors
- [autonomous-planning.md](autonomous-planning.md) — Automaton lifecycle
- [api/autonomous.md](../api/autonomous.md) — REST reference

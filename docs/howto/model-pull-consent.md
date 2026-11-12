---
docs:
  index: true
  topics: [models, consent, pull, registry, automation]
exec_params:
  - name: model
    description: Allowed model name (repeatable)
    required: false
    default: ""
exec_steps:
  - tool: automation_model_pull_consent_get
    description: Show the current model-pull consent policy
    read_only: true
  - tool: automation_model_pull_consent_set
    description: Replace the model-pull consent policy (models = JSON array of names or {name,allowed,note}; publishers = JSON array of names; or clear=true)
    read_only: false
---
# How-to: Model pull consent

The model registry holds which models datawatch can *see*; **model pull
consent** (`autonomous.model_pull_consent`) decides which of them may be
*pulled*. It is the operator's pull gate: allow a specific model, allow a
publisher wholesale, or refuse a model with the reason written down next to
it.

## The policy

Three pieces, all in `datawatch.yaml` under `autonomous.model_pull_consent`:

- **models[]** — one entry per model: `name`, `allowed` (bool) and
  `note` (free text, shown wherever the refusal is reported).
- **publishers[]** — publisher names allow-listed wholesale.
- **the rule** — default-deny: a model may be pulled when it is listed as
  allowed **or** its publisher is allow-listed. A listed-but-refused model
  is refused with its note; everything else is refused.

The policy is *complete*: a `set` replaces what is stored, it does not
append. Build the whole list in one command.

## Setting it

### CLI

```sh
# Allow two models, record one refusal with a reason, allow-list a publisher.
# The flags compose the complete list that will be stored:
datawatch automation model-consent set \
  --model gpt-oss:120b \
  --model qwen3-coder:30b \
  --deny big-model-65b \
  --publisher mistralai

# Refusal notes are stored as "denied by operator" — the entry with
# allowed:false is the record; add context via the REST/MCP/comm surfaces
# if you need a richer note.

# Wipe the policy.
datawatch automation model-consent set --clear
```

### MCP

```
# Read.
automation_model_pull_consent_get

# Replace (models is a JSON array of names or {name,allowed,note} objects):
automation_model_pull_consent_set \
  models='["gpt-oss:120b", {"name":"big-model-65b","allowed":false,"note":"too large for the pool"}]'

# Allow-list publishers.
automation_model_pull_consent_set publishers='["mistralai"]'

# Wipe.
automation_model_pull_consent_set clear=true
```

### Comm channel

```
autonomous model-pull-consent-get
autonomous model-pull-consent-set model=gpt-oss:120b deny=big-model-65b publisher=mistralai
autonomous model-pull-consent-set clear
```

### REST

```sh
# Current policy: models[] (name/allowed/note), publishers[], decision_rule.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/automation/model-consent | jq .

# Replace the complete policy.
curl -sk -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "models": [
      {"name":"gpt-oss:120b","allowed":true},
      {"name":"big-model-65b","allowed":false,"note":"too large for the pool"}
    ],
    "publishers": ["mistralai"]
  }' $BASE/api/automation/model-consent
```

Every surface enforces the same shape rules (a name appears at most once,
`set` replaces, `--clear` / `clear=true` cannot mix with other tokens) and
the same storage: the config file is the store; the autonomous manager is
synced in-process from it.

## Reading the current policy

```sh
datawatch automation model-consent get
# → Model pull consent
#    rule: default-deny — a model may be pulled when listed as allowed or its publisher is allow-listed
#    models:
#      gpt-oss:120b allowed
#      big-model-65b refused — too large for the pool
#    publishers: mistralai
```

The same body on MCP (`automation_model_pull_consent_get`), comm
(`autonomous model-pull-consent-get`) and REST.

## The happy path

```sh
# 1. Start from the current state.
datawatch automation model-consent get

# 2. Compose the whole policy in one set (it replaces, not appends).
datawatch automation model-consent set \
  --model gpt-oss:120b \
  --deny legacy-tiny --publisher mistralai

# 3. Verify what is stored.
datawatch automation model-consent get
# → models:
#    gpt-oss:120b allowed
#    legacy-tiny refused — denied by operator
# → publishers: mistralai
```

## Common pitfalls

- **`set` replaces.** Forgetting one model from the previous policy and
  running `set` drops it. Compose the *complete* list every time.
- **Refusals carry a note, allowed entries usually don't.** The `note`
  field is meaningful on `allowed: false` entries — it is the recorded
  reason shown to whoever asks "why not?".
- **Publisher allow-lists are by name.** Matching is on the publisher
  string the model carries; there is no wildcard matching.
- **The rule is default-deny.** A model that is neither listed nor
  publisher-listed is refused. Allow explicitly.
- **Stored in config, synced in-process.** Editing `datawatch.yaml` by
  hand while the daemon runs is fine (the manager reads the source of
  truth), but the sanctioned way is `set` on any surface, which validates
  and saves atomically and audits the change.

---

## See also

- [datawatch-definitions — Model pull consent](../datawatch-definitions.md#model-pull-consent)
- [howto/llm-registry](llm-registry.md)
- [howto/automation-requirements](automation-requirements.md)

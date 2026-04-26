# datawatch v5.17.0 — release notes

**Date:** 2026-04-26
**Spans:** v5.16.0 → v5.17.0
**Closed:** Config-surface bridge for v5.9.0 (BL191 Q4) + v5.10.0 (BL191 Q6)

## Why this is its own release

v5.9.0 + v5.10.0 added autonomous-package config knobs:

```yaml
autonomous:
  max_recursion_depth: 5
  auto_approve_children: true
  per_task_guardrails: ["rules"]
  per_story_guardrails: ["security"]
```

The polish pass found that the operator-facing surface for those
knobs was incomplete:

- **YAML load** — `internal/config/AutonomousConfig` didn't have the
  fields, so `~/.datawatch/config.yaml` round-trip dropped them
  silently.
- **REST `PUT /api/config`** — `applyConfigPatch` is hand-cased per
  key; pre-v5.17.0 these keys hit the `default:` branch and silently
  no-op'd. `datawatch config set autonomous.max_recursion_depth 5`
  returned 200 but nothing landed in config.yaml.
- **PWA Settings → General → Autonomous** — the field list under
  `GENERAL_CONFIG_FIELDS.autonomous` didn't include the new keys;
  operators couldn't reach them through Settings.
- **main.go translation** — even when the autonomous Manager was
  constructed, `amgrCfg` only copied the v5.0-era fields; the new
  ones started at zero values.

In other words: the runtime feature shipped in v5.9.0 / v5.10.0 was
only accessible if the operator called the autonomous Manager's
`SetConfig` directly through the dedicated `/api/autonomous/config`
endpoint. That worked, but no other surface did.

v5.17.0 closes the bridge:

## What changed

### `internal/config/AutonomousConfig`

Added four fields with their YAML + JSON tags:

```go
MaxRecursionDepth   int      `yaml:"max_recursion_depth,omitempty"`
AutoApproveChildren bool     `yaml:"auto_approve_children,omitempty"`
PerTaskGuardrails   []string `yaml:"per_task_guardrails,omitempty"`
PerStoryGuardrails  []string `yaml:"per_story_guardrails,omitempty"`
```

### `cmd/datawatch/main.go` autonomous-Manager translation

Copies the four new fields from `cfg.Autonomous` into the
`autonomouspkg.Config` the Manager actually consumes. When the
operator hasn't touched the recursion knobs (both zero), the
package's `DefaultConfig()` defaults (depth=5, auto_approve=true)
fill in.

### `internal/server/api.go.applyConfigPatch`

Four new cases for the new keys. The two list-shaped keys accept
both JSON arrays (`["rules","security"]`) and CSV strings
(`"rules, security"`) so the PWA's text-input shape works without a
new field type. New `splitCSV` helper covers the CSV path with
trim + empty-entry drop.

### PWA Settings → General → Autonomous

Field list extended with four entries:

```js
{ key: 'autonomous.max_recursion_depth', label: '…', type: 'number' }
{ key: 'autonomous.auto_approve_children', label: '…', type: 'toggle' }
{ key: 'autonomous.per_task_guardrails', label: '… (comma-separated)', type: 'text' }
{ key: 'autonomous.per_story_guardrails', label: '… (comma-separated)', type: 'text' }
```

The text inputs round-trip through the existing `String(arr)`
display path → CSV string sent to server → `splitCSV` parse.

### Tests

2 new unit tests in `internal/server/autonomous_config_patch_test.go`:

- `TestApplyConfigPatch_AutonomousRecursionAndGuardrails` —
  verifies all four keys land via PUT /api/config; covers both
  JSON-array and CSV-string paths for the list keys; verifies
  empty CSV clears the slice.
- `TestSplitCSV` — corner cases (empty, whitespace-only, single,
  multi, leading/trailing whitespace, empty entry between commas).

Total daemon test count: **1357 passed** in 58 packages.

## Why v5.17.0 (minor)

This isn't a feature add — the feature shipped in v5.9.0 and
v5.10.0. But every operator-facing surface gained the new keys, so
this is bigger than a "fix" and a minor bump matches the AGENT.md
"new operator-visible surface" rule.

## Upgrade path

```bash
datawatch update                                      # check + install
datawatch restart                                     # apply the new binary

# Verify the bridge:
datawatch config set autonomous.max_recursion_depth 7
datawatch config set autonomous.auto_approve_children true
datawatch config set autonomous.per_task_guardrails "rules,security"
cat ~/.datawatch/config.yaml | grep -A4 autonomous
```

## Known follow-ups (still open)

- BL190 deeper density (failure popups + mid-run progress + verdict
  drill-down panel) — iterative cosmetic.

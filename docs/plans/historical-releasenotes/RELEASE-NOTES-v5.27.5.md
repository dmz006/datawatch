# datawatch v5.27.5 — release notes

**Date:** 2026-04-29
**Patch.** Three operator-asked claude-code options bundled with full configuration parity, plus a new AGENT.md rule for keeping the hardcoded alias list fresh.

## What's new

### 1. `--permission-mode` exposed as a first-class session option (plan mode for PRDs)

Operator-asked: *"can you set plan mode on session starts so we can use it for PRDs?"*

claude-code accepts `--permission-mode <mode>` with values `default | plan | acceptEdits | auto | bypassPermissions | dontAsk`. **`plan`** is the design-without-writing-files mode you want for PRD decomposition + design-review sessions.

datawatch v5.27.5 surfaces it through every parity layer:

| Surface | Invocation |
|---|---|
| YAML | `session.permission_mode: plan` |
| REST (config) | `PUT /api/config { "session.permission_mode": "plan" }` |
| REST (per-session) | `POST /api/sessions/start { …, "permission_mode": "plan" }` |
| MCP | `config_set` with key `session.permission_mode` |
| CLI | `datawatch config set session.permission_mode plan` |
| Comm | `configure session.permission_mode plan` |
| PWA — global | Settings → LLM → claude-code → Permission mode field; Settings → General → Sessions → Claude permission mode |
| PWA — per-session | New Session modal → Claude options → Permission mode dropdown (visible when backend=claude-code) |
| PRD-level | `PRD.PermissionMode` field — applied to every task that doesn't override |
| Task-level | `Task.PermissionMode` — most-specific-wins fallthrough (task → PRD → session default) |

When `permission_mode` is non-empty, the legacy `--dangerously-skip-permissions` shortcut is suppressed — operators who set both implicitly mean the explicit mode (e.g. `plan` for PRD design); silently dropping the skip-permissions flag avoids the conflict claude would otherwise complain about.

### 2. `--model` and `--effort` exposed as first-class session options

Same parity matrix. PWA New Session modal gains Model + Effort dropdowns (when backend=claude-code), populated from the new endpoints below. PRD `Backend` / `Effort` / `Model` fields already existed (BL203, v5.4.0); v5.27.5 ensures the per-session path can also override directly without going through a PRD.

### 3. Hardcoded alias / effort / permission-mode list endpoints

Operator decision 2026-04-29: don't query Anthropic `/v1/models` at runtime ([BL206 frozen](docs/plans/README.md#frozen--external)). The PWA dropdowns need a list source — daemon serves it as static JSON:

| Endpoint | Returns |
|---|---|
| `GET /api/llm/claude/models` | `{aliases: [{value,label,description}], full_names: [{value,label}], source: "hardcoded"}` |
| `GET /api/llm/claude/efforts` | `{levels: [{value,label}], source: "hardcoded"}` — `low\|medium\|high\|xhigh\|max` |
| `GET /api/llm/claude/permission_modes` | `{modes: [{value,label}], source: "hardcoded"}` — includes `plan`, `acceptEdits`, `auto`, `bypassPermissions`, `dontAsk`, `default` |

Default model aliases shipped: `opus` / `sonnet` / `haiku`. Full names: `claude-opus-4-7`, `claude-sonnet-4-6`, `claude-haiku-4-5-20251001`.

### 4. New AGENT.md rule — major-release alias refresh

Added to `AGENT.md` § Major release alias refresh (above § Container maintenance):

> Every major release (X.0.0) must refresh the hardcoded LLM alias / model lists against the upstream provider's current set.

This is the forcing function that keeps the hardcoded list current without paying the cost of the runtime API integration BL206 froze. Mid-cycle minor/patch releases skip the refresh — the operator always has free-text fall-through in the PWA + can pass any full model name on the CLI as escape hatch.

## Tests

```
Go build:  Success
Go test:   1500 passed in 58 packages (+10:
             — 4 cases in claudecode/v5275_perm_mode_test.go
             — 4 cases in server/v5275_claude_endpoints_test.go
             — 2 inline cases in the existing test files)
Smoke:     run after install — §7w covers all three new endpoints
            + permission_mode config round-trip + plan-mode presence
            in the permission_modes list.
```

## datawatch-app sync

Two issues to file (mobile companion):
- `feat(session-start)`: mirror the New Session modal's claude-only options block (Permission mode / Model / Effort dropdowns).
- `feat(prd)`: surface `PRD.PermissionMode` + `Task.PermissionMode` in the mobile PRD editor.

## Backwards compatibility

- All schema changes are additive. `PRD.PermissionMode` + `Task.PermissionMode` carry zero values for pre-v5.27.5 PRDs; behaviour matches "use session default".
- `session.permission_mode` defaults to empty (claude picks its built-in default) — no change for existing deployments.
- Legacy `session.skip_permissions` continues to work when `session.permission_mode` is empty.

## Upgrade path

```bash
git pull
datawatch update && datawatch restart
# Hard-reload the PWA (cache name → datawatch-v5-27-5).
```

No data migration. No new schema. PWA dropdowns populate lazily on first use.

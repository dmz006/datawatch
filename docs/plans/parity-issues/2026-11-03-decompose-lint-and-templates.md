# [sync] feat(decompose-lint): decompose lint, task templates and decompose guidance (datawatch next minor release)

Parent release: datawatch next minor release — this feature shipped in datawatch's next
minor release and is not yet in the datawatch app. Trackable surface: a new **Decompose
lint** Settings card (with task templates and decompose guidance) and a per-Automaton
**decompose-lint findings panel** with a dry-run action, both backed by two new REST
endpoints.

## What changed in datawatch

Decompose lint is a quality check over a fresh decomposition, run at decompose time by a
deterministic rule engine (no LLM):

- **Mode** (`off` | `shadow` | `enforce`, default `shadow`) gates the effect:
  - `off` — the check records nothing; the Automaton stores no record.
  - `shadow` (default) — lints, records what a run would report, never retries.
  - `enforce` — lints, and any `block` finding triggers a planner retry carrying the
    findings + configured guidance as feedback; at most `max_retries` retries.
- **Findings are recorded, never fatal** — a decomposition is never failed because of
  this check, in any mode.
- **Retry budget** `max_retries` — daemon default **2**, clamped to **0-5**;
  0 disables retries even in enforce.
- **Rule pack** — the built-in pack is the floor (7 rules: 5 `block`, 2 `warn`, see the
  config reference), operator rules are the ceiling over it: same-id entries replace the
  built-in in place, new ids append, `disabled_rules` (or `{id, disabled: true}`) drops
  rules. A rule's `match` combines present conditions with AND — `spec_regex` /
  `title_regex` / `files_regex`, the `read_only` tri-state, `verification_script_empty`,
  and `any_of` / `all_of` (recursively, nesting capped). Severity on an active rule is
  required (`block` | `warn`; empty normalizes to `warn`).
- **Task templates** — reusable task definitions appended to the story at decompose time
  (shadow AND enforce; `off` applies nothing), idempotent: a task a previous apply added
  (or one marked `template:<id>`) is detected and never added twice. Slots `{{feature}}`,
  `{{files}}` and `{{files_list}}` are filled from the feature name and the story's task
  files; `depends_on` lists template ids the entry runs after (and the story's other
  tasks). The built-in pack is the floor: `changelog_entry`, `parity_issue_draft`,
  `docs_set`, `final_gate` (read-only), `version_bump_left_to_merge` (a note, never an
  edit) — written to hold two hard properties for every feature name: no blocking
  decompose-lint finding, and no hosting-CLI side effects. `task_templates.enabled`
  defaults to **true**; `projects` is a per-project override map (keyed by directory or
  alias; `{enabled: *bool}`, absent = follow the global).
- **Decompose guidance** — the block the planner's prompt carries: (1) literal text —
  used verbatim; (2) an existing readable file — its contents; (3) a path-like value with
  a missing/unreadable/empty file — the generated block stands in (a dead path is not
  prose to ship); (4) **UNSET (empty, default)** — generated from the active rule pack,
  one line per rule (id, severity, message) in pack order.
- **Surfaced in** the PWA Settings card, the PWA per-Automaton panel, the
  `datawatch autonomous lint ...` CLI verbs, the `autonomous lint ...` comm verbs and
  the four MCP `autonomous_decompose_lint_*` tools.

This issue adds the **Settings card** (Decompose lint — mode, retry budget, task
templates, guidance, active rules) and the **per-Automaton findings panel** (last
recorded pass + dry run) so parity is met when the next minor release lands.

## New REST contract (two endpoints, four verbs)

| verb | endpoint | purpose |
|---|---|---|
| config GET | `GET /api/autonomous/decompose-lint/config` | read the live config object |
| config PUT | `PUT /api/autonomous/decompose-lint/config` | replace the decompose-lint / task-templates / guidance block(s) |
| dry-run POST | `POST /api/autonomous/prds/{id}/decompose-lint` | read-only dry run over the stored plan (nothing is recorded, nothing re-decomposed) |
| findings GET | `GET /api/autonomous/prds/{id}/decompose-lint` | the stored record from the last decomposition (null when none) |

### GET /api/autonomous/decompose-lint/config

Response object: `decompose_lint`, `task_templates`, `decompose_guidance` (each block
as configured — the effective merge of built-in pack + operator rules, minus
`disabled_rules`).

| field | type | default | notes |
|---|---|---|---|
| `decompose_lint.mode` | string | `shadow` | `off` \| `shadow` \| `enforce` (empty resolves to `shadow`; anything else is a config error) |
| `decompose_lint.max_retries` | int | **2** | clamped to **0-5** (cap 5); 0 disables retries even in `enforce` |
| `decompose_lint.rules` | array | (built-in pack when empty) | operator rules: additions + same-id replacements + disables |
| `decompose_lint.disabled_rules` | array of strings | `[]` | ids dropped from the effective set regardless of origin (unknown ids ignored) |
| `task_templates.enabled` | bool | `true` | `false` turns template apply off everywhere (per-project overrides can still force it on/off) |
| `task_templates.templates` | array | (built-in pack when empty) | same id replaces, new id appends; duplicate ids are a 400 |
| `task_templates.projects` | object | `{}` | keyed by project directory or alias → `{enabled: *bool}` (absent = follow the global) |
| `decompose_guidance` | string | `""` | literal text or file path (see precedence above); empty = generated from the active rule pack |

Rule object (`decompose_lint.rules[]`):

| field | type | notes |
|---|---|---|
| `id` | string | required + unique across the config; same-id replaces the built-in in place |
| `severity` | string | `block` \| `warn` (empty normalizes to `warn`; anything else is a config error); required on an active rule |
| `message` | string | required on an active rule (a bare `{id, disabled: true}` entry is the canonical disable — its other fields stay unused and unvalidated) |
| `match` | object | see `match` below |
| `disabled` | bool | `true` disables the rule without touching its shape |

`match` object (present conditions AND together):

| field | type | notes |
|---|---|---|
| `spec_regex` | string | case-insensitive regex, anchored to the task spec |
| `title_regex` | string | case-insensitive regex, anchored to the task title |
| `files_regex` | string | case-insensitive regex, anchored to the task files list |
| `read_only` | bool \| null | tri-state on each task: `true` = must be read-only, `false` = must not be, absent = don't check |
| `verification_script_empty` | bool | `true` = the script must be empty, `false` = must be non-empty |
| `any_of` / `all_of` | array | at least one member / every member (of `match` objects or nested `any_of`/`all_of`); nesting capped (currently 8) |

Template definition (`task_templates.templates[]`):

| field | type | notes |
|---|---|---|
| `id` | string | required + unique, well-shaped (`[a-z0-9_.-]+`); duplicate is a 400 |
| `title` | string | may carry slots |
| `spec` | string | may carry slots: `{{feature}}`, `{{files}}`, `{{files_list}}` |
| `files` | string | plain or comma-separated |
| `kind` | string | free text, e.g. `docs`, `test` |
| `read_only` | bool | default false |
| `depends_on` | array of strings | template ids this one runs after (also after the story's other tasks) |
| `applies_when` | string | free-text condition for when the template is offered |

### PUT /api/autonomous/decompose-lint/config

Body: any of `decompose_lint` (a full `{mode, max_retries, rules, disabled_rules}`),
`task_templates` (a full `{enabled, templates, projects}`) and `decompose_guidance` (a
string); each present key replaces its **whole block**, omitted keys are left untouched.
Every supplied block is validated before any are applied (legal mode; well-shaped
unique rule ids; legal severities; non-empty rule messages; compiling regexes;
well-shaped unique template ids), so a bad value never leaves a half-updated config.

| status | meaning |
|---|---|
| 200 | the new full config object (same shape as GET) |
| 400 | a supplied block failed validation (nothing was applied) |
| 405 | method not allowed |
| 503 | autonomous disabled |

### POST /api/autonomous/prds/{id}/decompose-lint — dry run

Read-only: the same engine the decompose-time hook runs (`EffectiveRules` +
`LintTasks`), reporting what the check **would record right now** in whatever mode is
configured. Never mutates the Automaton, never records a pass, never re-decomposes,
never calls the planner — the report is the current config's view of an existing plan,
not a fresh decomposition. Request body optional (empty).

| field | type | notes |
|---|---|---|
| `prd` | string | the Automaton id the report covers |
| `mode` | string | `off` \| `shadow` \| `enforce` in effect when computed (the report is mode-agnostic — `off` still describes what a run would record) |
| `rule_count` | int | number of active (effective) rules used |
| `findings` | array of finding | in plan order; empty when clean |
| `summary.block` | int | count of `block` findings |
| `summary.warn` | int | count of `warn` findings |

Finding object (shared by the dry-run report and the stored record):

| field | type | notes |
|---|---|---|
| `rule_id` | string | the rule that fired (findings and operator docs reference rules by this id) |
| `severity` | string | `block` \| `warn` |
| `message` | string | the rule's finding text |
| `story` | string | the story the offending task belongs to |
| `task` | string | the offending task (when story-scoped, story stands in; whole-plan = empty) |
| `matched` | string | the matched excerpt, or the asserted field state |

The single-line operator form used in every surface: `[severity] rule_id task: message`
(task, or story, or `-` when neither).

### GET /api/autonomous/prds/{id}/decompose-lint — the stored record

Response: `{prd_id, decompose_lint}` where `decompose_lint` is the stored record or
`null` when no record was captured (`off` run, or decomposed before the first
decomposition — and after deletion).

Record fields:

| field | type | notes |
|---|---|---|
| `mode` | string | `shadow` \| `enforce` in effect when the pass ran (`off` is never stored — off records no record) |
| `attempts` | int | number of lint passes: 1 when no retry ran, or 1 + the retries consumed (at most 1 + `max_retries`) |
| `linted_at` | string (RFC3339) | when the final pass ran |
| `decompose_lint_findings` | array of finding | shadow: every finding of the single pass; enforce: whatever remained when the retry budget was spent (empty when a retry produced a clean plan); empty when clean |

Other statuses: 400 malformed id, 404 Automaton not found, 405 method not allowed,
503 autonomous disabled.

## MCP tool names

| tool | purpose |
|---|---|
| `autonomous_decompose_lint_config_get` | the config GET |
| `autonomous_decompose_lint_config_set` | the config PUT |
| `autonomous_decompose_lint_dry_run` | the PRD dry run (`{prd_id}`) |
| `autonomous_decompose_lint_findings` | the stored record (`{prd_id}`; empty object = `null`, the no-record wording) |

## CLI and chat verbs

CLI: `datawatch autonomous lint <verb>` (one verb each, in this order):

| verb | args | endpoint |
|---|---|---|
| `config` | none | GET config |
| `config-set` | `mode=` (off\|shadow\|enforce), `max_retries=` (int >= 0), `disabled_rules=` (comma list); one or more required | PUT config (over a live-config merge, so the active rule pack is preserved) |
| `dry-run` | `<prd-id>` | POST dry run |
| `findings` | `<prd-id>` | GET the stored record |

Chat (comm channel): `autonomous lint config`, `autonomous lint config-set
<key>=<val> ...`, `autonomous lint dry-run <prd-id>`, `autonomous lint findings
<prd-id>` — same endpoints, one wording.

## Where to look in datawatch

- `internal/autonomous/decompose_lint.go` — the rule engine (effective rules, `LintTasks`,
  the finding summary), shared by every surface.
- `internal/autonomous/decompose_lint_rules.go` — the built-in 7-rule pack (the floor).
- `internal/autonomous/task_templates_defaults.go` — the built-in template pack (the floor).
- `internal/autonomous/decompose_guidance.go` — guidance precedence + the generated block.
- `internal/autonomous/decompose_lint_hook.go` — the decompose-time hook (mode effects,
  replay, the stored record).
- `internal/config/decompose_lint.go` — config types, `Effective*`/`Normalize*`, the cap of 5.
- `internal/server/autonomous_decompose_lint.go` — REST wiring (the two endpoints).
- `internal/router/decompose_lint.go` — the comm-channel verbs.
- `internal/mcp/autonomous_decompose_lint.go` — the four MCP tools.
- `cmd/datawatch/cli_autonomous_lint.go` — the CLI verbs + the shared `key=value` parsing.
- `internal/server/web/app.js` + `internal/server/web/locales/{en,de,es,fr,ja}.json` —
  the PWA Settings card + per-Automaton panel, the five locales.
- Docs: `docs/howto/automaton-decompose-lint.md` (the feature how-to),
  `docs/config-reference.yaml`, `docs/api/openapi.yaml`.

## Acceptance (mobile)

Both Android and iOS:

- **Settings card** "Decompose lint" — mode (`off` \| `shadow` \| `enforce`, default
  `shadow`), `max_retries` (0-5, **default 2**, per `DefaultDecomposeLintMaxRetries`; the
  PWA label and fallback say 2),
  task templates (`enabled`, default true, the active templates, per-project
  overrides), `decompose_guidance` (a string; empty = auto), active rules (the
  effective JSON), saving via the config PUT and surfacing save validation errors.
- **Findings panel** on the Automaton detail view — the last recorded pass (mode,
  attempts, severity chips) plus a **Dry run** action (a report, not a recorded pass).
- The five locales: **EN, DE, ES, FR, JA** with the same label keys.
- No other surfaces in scope (CLI, comm verbs, MCP are desktop-side).

Filing record — draft (2026-11-03): prepared in
`docs/plans/parity-issues/2026-11-03-decompose-lint-and-templates.md`; not yet filed
(no `gh issue create` run at request).

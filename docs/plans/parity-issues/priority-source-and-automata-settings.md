Issue: https://github.com/dmz006/datawatch-app/issues/351

# [sync] Autonomous settings: new REST fields (priority_by_kind, defect_intake_automation), priority_source on PRD/session objects, MCP, CLI, comm, PWA settings fields and 9 locale keys (priority source badge + two settings rows)

## Body of the issue

This daemon commit adds two autonomous settings and a `priority_source` field on
priority-carrying objects. The PWA (and therefore the Android client, which is
the locale source and the mobile parity surface) needs the two new settings
rows, the priority-source rendering on the Automaton / capacity-queue rows,
and the nine new locale keys (with real translations).

### New REST fields on the existing endpoints

`GET` / `PUT /api/config` (under `autonomous`):

| Field | Notes |
|---|---|
| `priority_by_kind` | object (kind → integer priority ≥ 0). Default is the shipped table `{"bug":80,"maintenance":70,"feature":20,"other":0}`. Higher runs first in the capacity queue. `PUT` accepts an object, a `kind=n` text list, or a single integer; a non-number, a fraction, a boolean, or a negative value answers 400 and stores nothing. Empty (`null`, `{}`, empty string) clears the override back to the shipped defaults |
| `defect_intake_automation` | string, empty by default: the Automation that new kind-bug defect filings are offered to. Must be an existing Automation name or empty, otherwise 400 |

Objects that carry a capacity-queue `priority` now also carry `priority_source`:

| Where | Field | Notes |
|---|---|---|
| `GET /api/autonomous/prds/{id}` | `priority_source` | `explicit` (a per-Automaton pin via `POST /api/autonomous/prds/{id}/set_priority`, wins even when 0), `kind` (resolved from `priority_by_kind` via the Automaton's effective kind), or `default` (the 0 fallback). Absent on PRDs without a priority record |
| `GET /api/autonomous/prds/{id}` | `defect_intake` | object `{outcome, automation, message, at}` recording the last kind-bug filing's intake outcome: `offered` \| `not_configured` \| `skipped` \| `error`. Intake never fails or blocks the filing |
| session objects | `priority_source` | same vocabulary, where the session carries a queue priority |
| capacity queue rows | `priority` + `priority_source` | the waiter suffix names its source (explicit / kind) |

Unknown or absent fields stay ignored on the client side, so an un-updated
client keeps working (a `priority` without a `priority_source` renders exactly
as before).

### MCP

- `autonomous_config_set` accepts two new arguments: `priority_by_kind` (the table,
  same grammar as the REST PUT) and `defect_intake_automation` (Automation name
  or empty). A bad table is refused locally before any request.
- `autonomous_config_get` returns both keys; its description names both surfaces
  and their defaults. No new tool is added; the settings travel through the
  existing config tools.
- The per-Automaton pin is existing surface: `autonomous_prd_set_priority`
  (records `priority_source: explicit`) and `autonomous_prd_get` now returns
  `priority_source` alongside `priority`, plus `defect_intake` on the record.

### CLI

- `datawatch autonomous config-set '{"priority_by_kind":{"bug":90}}'` and
  `datawatch autonomous config-set '{"defect_intake_automation":"MyAutomation"}'`
  set the two keys (both accepted by the same apply path as the comm surface);
  an empty value clears.
- `datawatch autonomous config-get` reads both back.
- There is **no CLI subcommand for the per-Automaton pin** — `datawatch session
  priority <id> <n>` sets a *session's* queue priority, not an Automaton's. The
  pin is MCP/REST/web only.

### Comm channel

- `configure autonomous.priority_by_kind=bug=90,maintenance=70` (both key
  spellings accepted) sets the table; `configure autonomous.priority_by_kind=`
  clears it; `configure autonomous.defect_intake_automation=<name>` sets the name;
  a bad table is refused in-line.

### PWA (reference render — what the mobile client should mirror)

- **Automaton row and the capacity-queue waiting rows:** where the object
  carries a `priority_source`, the priority chip reads
  `Priority {priority} · {source}` (localized: `priority_source_badge` with
  `%1$d` = priority, `%2$s` = the localized source label from
  `priority_source_explicit` / `_kind` / `_default`), with the localized
  tooltip (`priority_source_tip`). Without a source the chip renders
  `Priority N` (hidden at 0) — un-updated data must not break the render.
- **Automata settings tab, new rows:**
  - `autonomous.priority_by_kind` — text field, the table rendered and edited as
    `kind=priority` pairs (`bug=80, maintenance=70, feature=20, other=0`),
    placeholder `bug=80, maintenance=70, feature=20, other=0`, empty clears back
    to the shipped defaults. Label + hint: `settings_priority_by_kind` /
    `settings_priority_by_kind_hint`. A write through the field goes to
    `PUT /api/config` under `autonomous.priority_by_kind`.
  - `autonomous.defect_intake_automation` — text field, an Automation name,
    placeholder `my-defect-triage`, empty = off (filings then record
    `not_configured`). Label + hint: `settings_defect_intake_automation` /
    `settings_defect_intake_automation_hint`.

### Settings to add to the mobile app

| Item | Value |
|---|---|
| Config key | `autonomous.priority_by_kind` |
| REST field | `priority_by_kind` under `autonomous` in `GET` / `PUT /api/config` |
| Label | Capacity priority by kind (kind=priority pairs; higher runs first; empty = shipped defaults bug=80, maintenance=70, feature=20, other=0) |
| Type | text (kind=priority list; written as the parsed object) |
| Default (render) | the shipped table above |
| Placement | autonomous settings block, next to the other autonomous settings |

| Item | Value |
|---|---|
| Config key | `autonomous.defect_intake_automation` |
| REST field | `defect_intake_automation` under `autonomous` in `GET` / `PUT /api/config` |
| Label | Defect intake Automation (Automation name for kind-bug defect filings; empty = off) |
| Type | text (Automation name) |
| Default | empty (off) |
| Placement | autonomous settings block, next to the other autonomous settings |

### New locale keys (9)

| Key | EN value | de / es / fr / ja |
|---|---|---|
| `priority_source_badge` | Priority %1$d · %2$s | de/explanation/es/fr already carry the localized form; ja still placeholder | 
| `priority_source_explicit` | explicit | de explizit / es explícito / fr explicite / ja 明示 |
| `priority_source_kind` | kind | de Typ / es tipo / fr type / ja kind |
| `priority_source_default` | default | de Standard / es predeterminado / fr par défaut / ja デフォルト |
| `priority_source_tip` | Capacity-queue priority set by %1$s: explicit = pinned on the Automaton or session by the operator, kind = resolved from the kind table (autonomous.priority_by_kind), default = nothing set | de/es/fr already carry the localized form; ja still the EN placeholder — **translators needed** |
| `settings_priority_by_kind` | Capacity priority by kind (kind=priority pairs; higher runs first; empty = shipped defaults) | **translators needed** |
| `settings_priority_by_kind_hint` | The kind → priority table (autonomous.priority_by_kind); higher values are admitted first in the capacity queue. Shipped defaults: bug=80, maintenance=70, feature=20, other=0. A priority pinned directly on an Automaton always wins. An empty value clears the table back to the shipped defaults. | **translators needed** |
| `settings_defect_intake_automation` | Defect intake Automation (Automation name for kind-bug defect filings; empty = off) | de/es/fr already carry the localized form (keep "Automation" — the chain object — localized: Automatisierung / automatización / automatisation); ja still the EN placeholder — **translators needed, keep オートメーション** |
| `settings_defect_intake_automation_hint` | The Automation that new kind-bug defect filings flow into (autonomous.defect_intake_automation). Empty by default: each installation chooses its own, and a filing without one is recorded, not filed into any Automation. | **translators needed** |

The de/es/fr/ja values above are placeholders until real translations land in
the datawatch-app bundles; the PWA will then mirror them 1:1 (the bundles are
sourced from the app and never free-hand-typed in the datawatch repo).

### Acceptance (mobile)

- The two settings rows render in the autonomous settings block with their
  localized labels, the right placeholders, and the current daemon value
  pre-filled from `GET /api/config`; a write goes through `PUT /api/config`
  under `autonomous.*` and an empty value clears.
- The Automaton row and the capacity-queue waiting row show the priority chip
  with its source (`Q 80 · <localized source>`) when `priority_source` is
  present, and the legacy chip (no source segment) when it is absent, so
  un-upgraded data still renders.
- All nine keys are localized in EN, DE, ES, FR and JA (no English fallback
  leaking into de/es/fr/ja; "Automaton" words in the de/es/fr/ja values keep
  their localized forms).
- Unknown or absent new fields render exactly as today (forward-compatible).

## Filing record

Checked for an open issue first: this file is the draft body for the
datawatch-app issue (no open issue found under a priority-source /
priority_by_kind / defect_intake search).

**Not yet filed:** the auto-fix run for this repo (rules-review findings,
attempt 1) runs under a **no-gh** constraint and therefore could not file to
`dmz006/datawatch-app`. The body above is complete and ready; file it exactly
once with the title of this document, then record the URL here and in the
Filing record of the fix commit.

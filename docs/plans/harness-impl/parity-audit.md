# Parity-Surface Audit — AGENT.md / DATAWATCH-CONTEXT.md / parity-status.md

**Date:** 2026-09-26
**Status:** Audit complete — findings are recommendations only; no rule text has been changed yet.
**Scope of this document:** read-only cross-rule audit of which parity surfaces each rule names today, which surfaces are missing from the canonical full set, and concrete proposed corrections. This document changes no code and does not edit AGENT.md, DATAWATCH-CONTEXT.md, or docs/parity-status.md; its deliverable is the proposed rule wording below.

---

## 1. Scope and method

**Files read in full:**

| File | Lines | Last verified |
|---|---|---|
| `AGENT.md` | 1906 | 2026-09-26 |
| `DATAWATCH-CONTEXT.md` | 644 | 2026-09-26 |
| `docs/parity-status.md` | 98 | 2026-09-26 |

**Method.** Every rule, checklist row, and section mentioning API, endpoint, config,
channel, comms, surface, or an operator-facing client (PWA, Android, iOS/iPhone,
documentation, how-tos, definitions) was enumerated, and the surfaces it names were
compared against the canonical full set (see §2). A surface is *named* if the text
either lists it or refers to a list that contains it. A surface is *missing* if the
rule speaks to multi-surface parity but does not include it in its enumerated set and
does not route it to another rule that does.

**Audit keywords used:** `API`, `endpoint`, `config`, `channel`, `comms`, `surface`,
`parity`, `PWA`, `Android`, `iOS`, `iPhone`, `Web UI`, `operator-facing`, `access
method`, `howto`, `doc(s)`, `definitions`.

---

## 2. Canonical full parity-surface set (as defined by the Mobile-Parity Rule)

`AGENT.md:462–463`:

> `REST`, `MCP`, `CLI`, `comm channel`, `YAML/config`, `PWA`, `Android`, `iPhone/iOS`.

**Audit question for this document.** Do *any* rules name **documentation**,
**datawatch-definitions**, or **how-to docs** as a parity target in the same set?

**Finding (answer before the table, for speed).** **No.** No rule in the three audited
files names documentation, definitions, or how-tos as a parity target inside its
enumerated parity-surface set. Three rules do govern *documentation content* (feature
doc checklist, docs-as-MCP, feature documentation access-methods), but none folds
the documentation surface into the parity enumeration itself. The `Parity surface`
plan-section clause (`AGENT.md:151`) enumerates exactly the eight surfaces above and
no more.

**Audit finding.** The documentation surface is *partially* governed: the Documentation
Rules block (lines 119–178) require docs to be updated when behavior changes and
require a `Parity surface` section in every plan, but the parity enumeration within
that clause does not include documentation as a parity surface. An operator reading
only the parity enumeration would conclude that documentation has no parity
requirement — the requirement only exists via the separate documentation rules.

---

## 3. Gap table

**Full set (11 surfaces):**
REST, MCP, CLI, comm channel, YAML/config, PWA, Android, iPhone/iOS,
documentation, definitions, howtos.

A surface is marked **named** if the text of the rule (or a rule it cross-references)
includes it in its parity enumeration. A surface is marked **missing** if the rule
speaks to parity or multi-surface coverage but omits it.

| # | Rule name | Location (file + line range) | Surfaces named today | Surfaces missing from the full set | Proposed corrected text |
|---|-----------|------------------------------|----------------------|-----------------------------------|-------------------------|
| 1 | Configuration Accessibility Rule | `AGENT.md:383–413` | YAML/config, REST, MCP, CLI, comm channel, PWA (Web UI), Android, iPhone/iOS (note at :407–413 explicitly adds Android/iPhone as config surfaces) | **documentation** (no rule requires the config field to appear in a parity-relevant doc section), **definitions** (no rule references the configuration reference doc as a parity surface) | See §4, item 1 |
| 2 | Parity-surface plan-section clause | `AGENT.md:151` (Documentation Rules checklist item 8) | REST, MCP, CLI, comm channel, YAML/config, PWA, Android, iPhone/iOS — exactly 8 | **documentation**, **definitions**, **howtos** | See §4, item 2 |
| 3 | Mobile-Parity Rule — full set clause | `AGENT.md:462–463` | REST, MCP, CLI, comm channel, YAML/config, PWA, Android, iPhone/iOS — exactly 8 | **documentation**, **definitions**, **howtos** | See §4, item 3 |
| 4 | Mobile-Parity Rule — iOS notes | `AGENT.md:469–478` | REST (platform-neutral endpoints), PWA (implied) | **documentation** (no requirement that new iOS-facing behavior be documented in howtos or the configuration reference), **definitions** | See §4, item 3 (iOS clause addition) |
| 5 | Federation-Parity Rule — parity-surface enumeration clause | `AGENT.md:556–561` | REST, MCP, CLI, comm channel, YAML/config, PWA, Android, iPhone/iOS — explicitly enumerated at :558–559 | **documentation** (the clause requires a *plan or doc* to enumerate surfaces, but does not require the doc itself to be a parity surface), **definitions**, **howtos** | See §4, item 4 |
| 6 | Feature Documentation: All Access Methods | `AGENT.md:1486–1534` | YAML, CLI, Web UI (PWA), REST API, comm channel — 5 access methods | **MCP** (not in the 5-method table), **Android**, **iPhone/iOS**, **documentation** (the rule governs doc content but does not name documentation as a parity surface), **definitions**, **howtos** | See §4, item 5 |
| 7 | Monitoring & Observability Rule | `AGENT.md:1216–1239` | Stats metrics, API endpoint, MCP tool, Web UI card, comm channel command, Prometheus | **YAML/config** (no requirement that stats knobs be config-settable), **Android**, **iPhone/iOS**, **documentation**, **definitions**, **howtos** | See §4, item 6 |
| 8 | Testing Requirements — test all interfaces | `AGENT.md:1055–1071` | API, Web UI, CLI, comm channel, config, WebSocket, MCP — 7 methods | **Android**, **iPhone/iOS** (no mobile-client test requirement), **documentation**, **definitions**, **howtos** | See §4, item 7 |
| 9 | Per-sprint checklist — B6 (config parity) | `AGENT.md:1140` | YAML, REST, MCP, CLI, comm, PWA — 6 | **Android**, **iPhone/iOS** (B6 does not extend to mobile clients), **documentation**, **definitions**, **howtos** | See §4, item 8 (B6 row update) |
| 10 | Per-sprint checklist — B8 (access docs) | `AGENT.md:1141` | YAML, CLI, Web UI (PWA), REST, comm — 5 access methods | **MCP**, **Android**, **iPhone/iOS**, **documentation** (as a parity surface), **definitions**, **howtos** | See §4, item 9 (B8 row update) |
| 11 | Parity enforcement note | `DATAWATCH-CONTEXT.md:87` | REST, MCP, CLI, comm channel, YAML/config, PWA, Android, iPhone/iOS — exactly 8 | **documentation**, **definitions**, **howtos** | See §4, item 2 (applies to this note) |
| 12 | Parity-status.md — Plan/Proposal parity gate | `docs/parity-status.md:52–62` | REST, MCP, CLI, comm, PWA, Android/iPhone (named at :54) | **documentation**, **definitions**, **howtos** | See §4, item 10 |
| 13 | Parity-status.md — Feature Parity Table header | `docs/parity-status.md:15–51` | PWA, Android, iOS (3-client column set) | REST, MCP, CLI, comm, YAML/config (by design — this is a *client* parity table, not a full-surface table; the gap is that no *full-surface* table exists alongside it), **documentation**, **definitions**, **howtos** | See §4, item 11 (new full-surface table) |
| 14 | Release workflow — parity audit checklist item | `AGENT.md:656` | REST, MCP, CLI, comm channel, YAML/config, PWA, Android, iPhone/iOS — exactly 8 | **documentation**, **definitions**, **howtos** | See §4, item 12 (checklist item text) |
| 15 | Docs-as-MCP rules (Docs-as-MCP Currency Rule + Howto-Coverage Rule) | `AGENT.md:1250–1274` | howtos (governance of doc content and MCP tool references) | **documentation** (the rules govern howtos but do not name documentation as a *parity surface* — they are content-quality rules, not parity rules), **definitions** | See §4, item 13 (howto-surface clause) |
| 16 | Configuration Rules (6 registration points) | `AGENT.md:1473–1484` | Web UI (PWA), REST GET/PUT, MCP, comms (all), CLI — 6 | **Android**, **iPhone/iOS**, **documentation**, **definitions**, **howtos** | Covered by §4, item 1 (Configuration Accessibility Rule is the canonical rule; this is a sub-rule) |
| 17 | Config accessibility note in DATAWATCH-CONTEXT | `DATAWATCH-CONTEXT.md:340` | YAML, REST, MCP, comm, CLI, PWA — 6 channels | **Android**, **iPhone/iOS** (no mention), **documentation**, **definitions**, **howtos** | Covered by §4, item 1 |
| 18 | B7 Monitoring checklist row | `AGENT.md:1141` | SystemStats, API, MCP tool, Monitor card, Prometheus — 5 | **comm channel** (no comm variant in the B7 token, unlike the Monitoring Rule item 5), **YAML/config**, **Android**, **iPhone/iOS**, **documentation**, **definitions**, **howtos** | See §4, item 6 (covers both Monitoring Rule and B7) |

**Summary of missing-surface frequencies across all 18 rows audited:**

| Surface | Named by (rows #) | Missing from (rows #) |
|---|---|---|
| REST | 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 14, 16, 17, 18 | 13 (by design) |
| MCP | 1, 2, 3, 5, 7, 8, 9, 11, 12, 14, 16, 17, 18 | 4, 6, 10, 13 |
| CLI | 1, 2, 3, 5, 6, 7, 8, 9, 10, 11, 12, 14, 16, 17, 18 | 4, 13 |
| comm channel | 1, 2, 3, 5, 6, 7, 8, 9, 10, 11, 12, 14, 16, 17 | 4, 13, 18 |
| YAML/config | 1, 2, 3, 5, 6, 7, 9, 11, 12, 14, 16, 17 | 4, 13, 18 |
| PWA | 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 16, 17, 18 | — |
| Android | 1, 2, 3, 5, 11, 12, 13, 14 | 4, 6, 7, 8, 9, 10, 16, 17, 18 |
| iPhone/iOS | 1, 2, 3, 4, 5, 11, 12, 13, 14 | 6, 7, 8, 9, 10, 16, 17, 18 |
| documentation | 15 (as doc-content rule, not parity surface) | 1, 2, 3, 5, 6, 7, 8, 9, 10, 11, 12, 14 |
| definitions | — | all 18 rows |
| howtos | 15 (as doc-content rule, not parity surface) | 1, 2, 3, 5, 6, 7, 8, 9, 10, 11, 12, 14 |

**Key finding:** documentation, definitions, and howtos are governed by *content-quality*
rules (lines 119–178, lines 1250–1274, lines 1486–1534) but are never folded into any
parity *enumeration*. Every parity enumeration in the three audited files uses exactly
the eight-surface set and omits the documentation-family surfaces.

---

## 4. Proposed corrected text (concrete rule wording)

The following text is proposed for insertion or replacement. Each item references the
line range it applies to.

### Item 1 — Configuration Accessibility Rule (`AGENT.md:383–413`)

**Replace lines 400–413 with:**

> **Before marking a feature complete**, verify the config value round-trips:
> ```
> PUT /api/config {"key":"feature.setting","value":true}
> GET /api/config → feature.setting = true (verify)
> configure feature.setting=true → verify response
> ```
>
> **Parity-surface note (expanded):** the six input channels above are the
> *write* surfaces for configuration. Per the full parity set in this file, a
> config field is additionally a parity target on the following surfaces, each
> of which must be addressed in the plan's `Parity surface` section with an
> include or an exclude-with-reason:
>
> - **Web UI (PWA)** — field visible and editable in Settings
> - **Android** — setting reflected in the Android client, or a reason-logged
>   exclusion if the feature is server-only
> - **iPhone/iOS** — setting reflected in the iOS client, or a reason-logged
>   exclusion
> - **Documentation** — the field appears in the configuration reference
>   (`docs/config-reference.yaml`) with type, default, and a one-line description;
>   if the field controls operator-facing behavior, it also appears in the
>   relevant howto under `docs/howto/`
>
> Adding Android/iPhone as *config-input* channels (i.e., `configure` from a
> mobile client) is still explicitly out of scope for this rule, per the
> Mobile-Parity Rule's capability-parity clause.

### Item 2 — Parity-surface plan-section clause (`AGENT.md:151`)

**Replace line 151 with:**

> 8. **Parity surface section** — every generated plan document under `docs/plans/`
>    MUST contain a `## Parity surface` section with one row per surface in the
>    **full parity-surface set**: `REST`, `MCP`, `CLI`, `comm channel`,
>    `YAML/config`, `PWA`, `Android`, `iPhone/iOS`, `documentation`,
>    `definitions`, `howtos`. Each row is either **included** (surface ships in
>    this feature) or **excluded with reason** (one sentence stating why the
>    surface is not applicable). A plan with fewer than 11 rows, or with an
>    excluded row lacking a stated reason, is incomplete and does not pass the
>    release parity audit.

### Item 3 — Mobile-Parity Rule full-set clause (`AGENT.md:462–463`)

**Replace lines 462–463 with:**

> **Full parity-surface set** (used across this file and in every `Parity surface`
> plan section): `REST`, `MCP`, `CLI`, `comm channel`, `YAML/config`, `PWA`,
> `Android`, `iPhone/iOS`, `documentation`, `definitions`, `howtos`.
>
> The first eight are *capability-parity* surfaces — each must ship the feature
> or carry a reason-logged exclusion. The last three are *documentation-parity*
> surfaces: a feature that changes operator-visible behavior MUST ship with
> updated documentation (configuration reference), any affected
> datawatch-definitions entries, and any affected how-tos, or carry a
> reason-logged exclusion per surface.

### Item 4 — Federation-Parity Rule enumeration clause (`AGENT.md:556–561`)

**Replace lines 556–561 with:**

> **Parity-surface enumeration:** a federation-guarded endpoint is an API surface.
> Per the full parity-surface set (11 surfaces), the plan or docs for that
> endpoint MUST enumerate which of the full set the capability is reachable
> from — and which are excluded, with reason — including `documentation`
> (the endpoint appears in the API reference and the relevant howto),
> `definitions` (the capability is named in the definitions doc), and
> `howtos` (an operator howto exists that exercises the guarded path). A
> `fedCap`-guarded REST surface shipped without its documentation-parity
> counterparts is incomplete.

### Item 5 — Feature Documentation All Access Methods (`AGENT.md:1486–1534`)

**Replace the five-method table at lines 1495–1510 with an 8-method table:**

```markdown
| Method | How |
|--------|-----|
| **YAML**      | Edit `~/.datawatch/config.yaml` → `section:` (details) |
| **CLI**       | `datawatch setup <feature>` or `datawatch <command>` |
| **Web UI (PWA)** | Settings tab → Section → **Card Name** |
| **REST API**  | `PUT /api/config` with `{"key": value}` or specific endpoint |
| **Comm channel** | `configure key=value` or specific chat command |
| **MCP**       | `<tool_name>` MCP tool with parameter table |
| **Android**   | Client screen path, or *N/A — server-side feature* with reason |
| **iPhone/iOS**| Client screen path, or *N/A — server-side feature* with reason |
```

**And add after line 1513:**

> In addition to the eight access-method rows, the feature documentation MUST
> include a **Parity surface** block (one row per surface in the full 11-surface
> set, included or excluded-with-reason) so that the doc itself is a
> documentation-parity artifact, not just a content artifact.

### Item 6 — Monitoring & Observability Rule (`AGENT.md:1216–1239`)

**Append to the existing 6-item list (after item 6, line 1238):**

> 7. **Comm channel variant** — if the subsystem has a `stats` or `status`
>    command accessible from messaging channels, wire it and note it in
>    `Parity surface` (comm channel row).
> 8. **YAML/config knob** — if the feature has on/off or threshold settings,
>    they must be settable via `PUT /api/config` and the PWA Settings card;
>    the configuration reference must document them (documentation row).
> 9. **Documentation parity** — the Monitor card, the stats endpoint, and any
>    how-to that references the subsystem must all appear in the plan's
>    `Parity surface` section with include/exclude-with-reason rows.

**Update B7 row (`AGENT.md:1141`) to:**

> | B7 | New feature | Monitoring/observability wired: SystemStats + `/api/<sub>/stats` + MCP tool + Monitor card + Prometheus **+ comm variant (if applicable)** | `observability: added` |

### Item 7 — Testing Requirements (`AGENT.md:1055–1071`)

**Insert after item 3 (line 1068) as new item 3b:**

> 3b. **Mobile clients** — for operator-visible features, verify the PWA
>    behavior and confirm (or reason-log exclusion for) Android and iOS/iPhone
>    client behavior. A PWA feature with no mobile parity row in the plan's
>    `Parity surface` section is incomplete.
>
> 3c. **Documentation** — verify the configuration reference, any affected
>    how-to, and any affected definitions entry are updated (or reason-log
>    exclusion). Documentation drift is a parity violation, not a documentation
>    afterthought.

### Item 8 — B6 row update (`AGENT.md:1140`)

**Replace with:**

> | B6 | New or changed config field | Config-parity verified (YAML + REST + MCP + CLI + comm + PWA **+ documentation** (config reference updated) **+ Android/iOS row** (included or excluded-with-reason in plan)) | `config-parity: verified` |

### Item 9 — B8 row update (`AGENT.md:1141`)

**Replace with:**

> | B8 | New feature with operator-facing config or workflow | Feature docs updated: **all 8 access-method rows** (YAML/CLI/WebUI/REST/comm/**MCP**/**Android**/**iPhone**) **+ documentation-parity rows** (config reference + howtos + definitions, each included or excluded-with-reason) | `access-docs: added` |

### Item 10 — parity-status.md Plan/Proposal gate (`docs/parity-status.md:54`)

**Replace line 54 with:**

> The table above tracks **shipped** client parity. This section tracks
> **in-flight** work so the parity gate is visible before a feature lands.
> Per AGENT.md, every plan/proposal under `docs/plans/` must ship across all
> surfaces in the full parity-surface set — `REST`, `MCP`, `CLI`, `comm
> channel`, `PWA`, Android, iPhone — *and* must address the documentation
> surfaces: `documentation` (configuration reference + relevant operational
> doc), `definitions` (definitions doc entry if the feature changes a named
> capability), and `howtos` (an operator howto that exercises the feature) —
> or carry a reason-logged exclusion for each. A plan with fewer than 11
> `Parity surface` rows is incomplete.

### Item 11 — Full-surface parity table (new section in `docs/parity-status.md`)

**Append after line 51 (Feature Parity Table):**

> ## Full-Surface Parity Table
>
> This table tracks the full 11-surface set per feature. It complements the
> client-only table above. A feature is **parity-complete** when all 11 rows
> are `✅` or `excluded (reason)`.
>
> | Feature | REST | MCP | CLI | Comm | YAML | PWA | Android | iOS | Docs | Defs | Howtos |
> |---------|------|-----|-----|------|------|-----|---------|-----|------|------|--------|
> | *(example) Capacity admission* | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | pending | pending | ✅ | ✅ | ✅ |
>
> Populate one row per planned feature when its `Parity surface` section is
> approved; move the row to `parity-complete` when all 11 columns resolve.

### Item 12 — Release workflow parity-audit checklist item (`AGENT.md:656`)

**Replace line 656 with:**

> - [ ] **parity audit passed** — every plan/proposal doc under `docs/plans/`
>      touched by the release has a `Parity surface` section with **at least 11
>      rows** (one per surface in the full set: `REST`, `MCP`, `CLI`,
>      `comm channel`, `YAML/config`, `PWA`, `Android`, `iPhone/iOS`,
>      `documentation`, `definitions`, `howtos`), each row **included** or
>      **excluded with a stated reason**; no row is blank. The documentation
>      rows (`documentation`, `definitions`, `howtos`) must each name the
>      specific file that was updated (e.g. `docs/config-reference.yaml`,
>      `docs/howto/<name>.md`) or state the exclusion reason.

### Item 13 — Howto-surface clause (`AGENT.md:1250–1274`)

**Append after line 1274:**

> ### Howto-Parity Rule
>
> Every feature that introduces an operator workflow MUST have a corresponding
> howto under `docs/howto/` that:
> 1. References every MCP tool the operator needs (verified live).
> 2. Shows the comm channel command equivalent (if one exists).
> 3. Shows the CLI command equivalent (if one exists).
> 4. Shows the PWA / Android / iOS screen path (or *N/A — server-side* with
>    reason).
>
> A feature that ships without a howto must carry an explicit
> `excluded (reason)` in the `howtos` row of its `Parity surface` section.
> The reason must state *why* no operator workflow exists for this feature
> (e.g. "internal-only automation path, no operator action required").

---

## 5. Release-checklist audit

**Question:** Does the AGENT.md release-workflow / rule-audit checklist require a
parity-audit pass for any plan or proposal?

**Finding: YES — but with a scope gap.**

1. `AGENT.md:656` (Release workflow checklist, line 656): requires a parity audit
   pass for every plan/proposal doc under `docs/plans/` touched by a release,
   using the **8-surface** set. → **Present, but the surface set is incomplete**
   (missing documentation, definitions, howtos).

2. `AGENT.md:1106–1200` (Per-sprint rules audit, Sections A–E):
   - **B6** (line 1140): config-parity check — 6 channels, no mobile, no docs.
   - **B7** (line 1141): observability check — 5 items, no docs, no mobile.
   - **B8** (line 1141): access-docs check — 5 methods, no MCP, no mobile, no docs-parity.
   - **No row explicitly requires the `Parity surface` section to be complete
     (11 rows).** The B6/B7/B8 rows check *inputs* to the parity audit but not
     the *artifact* (the `Parity surface` section itself).

**Proposed new checklist row (insert as B19 after `AGENT.md:1151`):**

> | B19 | Any plan or proposal doc under `docs/plans/` is in scope for this sprint | `Parity surface` section present with ≥ 11 rows; every row is **included** or **excluded with a stated reason**; documentation rows name the specific file updated or state the exclusion reason | `parity-surface: complete (11/11)` |

**Proposed B19 verification command (for the audit token):**

```bash
# Count Parity-surface rows in a plan doc (expect ≥ 11)
grep -c "^| " docs/plans/<plan>.md | head -1
# Or: check the Parity surface section specifically
sed -n '/## Parity surface/,/^## /p' docs/plans/<plan>.md | grep -c "^| "
```

**Release sign-off checklist update (`DATAWATCH-CONTEXT.md:87`):**

> Add to the parity-enforcement note:
>
> Plans and proposals MUST document per-surface intent (include/exclude-with-reason)
> across the **full 11-surface set** — including `documentation` (naming the
> specific config-reference or operational doc updated), `definitions` (naming
> the definitions entry or stating the exclusion), and `howtos` (naming the
> howto file or stating the exclusion) — *before* implementation begins.
> A plan with fewer than 11 `Parity surface` rows is incomplete and does not
> pass the release `Parity surface` audit.

---

## 6. Three planned features — Parity-surface section verification

The parent plan (`docs/plans/harness-impl/`) lists three planned features.
Each is checked below for whether it *would be required* to carry a `Parity
surface` section under the rules as they stand today (8-surface set) and under
the proposed corrections (11-surface set).

### 6.1 Eval suite × backend sweep

`docs/parity-status.md:58` records its target surfaces as: **REST, MCP, CLI,
comm, PWA** (5 of 8 today; 5 of 11 under the proposed set).

| Surface | Named in plan spec today? | Required by current rules? | Required by proposed rules? |
|---|---|---|---|
| REST | ✅ | ✅ (8-surface set) | ✅ |
| MCP | ✅ | ✅ | ✅ |
| CLI | ✅ | ✅ | ✅ |
| comm channel | ✅ | ✅ | ✅ |
| YAML/config | ❌ (not named) | ⚠️ (implied — sweep config is a knob) | ✅ (explicit row required) |
| PWA | ✅ | ✅ | ✅ |
| Android | ❌ (not named) | ⚠️ (exclusion reason required) | ✅ (explicit row — include or exclude-with-reason) |
| iPhone/iOS | ❌ (not named) | ⚠️ (exclusion reason required) | ✅ (explicit row — include or exclude-with-reason) |
| documentation | ❌ (not named) | ❌ (not in 8-surface set) | ✅ (row required: name config reference + howto) |
| definitions | ❌ (not named) | ❌ | ✅ (row required) |
| howtos | ❌ (not named) | ❌ | ✅ (row required: name `docs/howto/<name>.md`) |

**Conclusion (current rules):** The plan's `Parity surface` section would need
to include or reason-exclude YAML/config, Android, and iPhone/iOS even under
the current 8-surface set. **Under the proposed 11-surface rules**, the plan
must also carry documentation, definitions, and howtos rows. The current spec
(names only 5 of 8) is **incomplete** under both rule sets.

### 6.2 Eval node in the PRD-DAG orchestrator

`docs/parity-status.md:59` records its target surfaces as: **REST, MCP, CLI,
comm, PWA**.

| Surface | Named in plan spec? | Current rules | Proposed rules |
|---|---|---|---|
| REST | ✅ | ✅ | ✅ |
| MCP | ✅ | ✅ | ✅ |
| CLI | ✅ | ✅ | ✅ |
| comm | ✅ | ✅ | ✅ |
| YAML/config | ❌ | ⚠️ (orchestrator config is a knob) | ✅ |
| PWA | ✅ | ✅ | ✅ |
| Android | ❌ | ⚠️ | ✅ |
| iPhone/iOS | ❌ | ⚠️ | ✅ |
| documentation | ❌ | ❌ | ✅ |
| definitions | ❌ | ❌ | ✅ |
| howtos | ❌ | ❌ | ✅ |

**Conclusion:** Same gap profile as 6.1. Under current rules, the plan must
carry exclusion-reason rows for Android/iPhone (mobile clients would display
orchestrator graph status, as parity-status.md:35 shows the Orchestrator graphs
row). Under proposed rules, the plan must additionally name the documentation,
definitions, and howto files.

### 6.3 Red-team validator pipeline

`docs/parity-status.md:60` records its target surfaces as: **REST, MCP, CLI,
comm, PWA**.

| Surface | Named in plan spec? | Current rules | Proposed rules |
|---|---|---|---|
| REST | ✅ | ✅ | ✅ |
| MCP | ✅ | ✅ | ✅ |
| CLI | ✅ | ✅ | ✅ |
| comm | ✅ | ✅ | ✅ |
| YAML/config | ❌ | ⚠️ (validator guard settings are knobs) | ✅ |
| PWA | ✅ | ✅ | ✅ |
| Android | ❌ | ⚠️ (validator status is operator-visible) | ✅ |
| iPhone/iOS | ❌ | ⚠️ | ✅ |
| documentation | ❌ | ❌ | ✅ |
| definitions | ❌ | ❌ | ✅ |
| howtos | ❌ | ❌ | ✅ |

**Conclusion:** Under current rules, Android and iPhone/iOS rows are required
with reasons (the validator pipeline changes operator-visible security behavior;
the parity-status.md:36 capacity-admission row pattern — "REST, MCP, CLI, comm
all shipped" vs. "pending app issue" — shows the expectation that mobile
surfaces are either shipped or explicitly tracked). Under proposed rules,
documentation, definitions, and howtos rows are additionally required.

---

## 7. Conclusions

1. **No rule in the three audited files names documentation, definitions, or
   how-tos as a parity target inside any parity enumeration.** Every parity
   enumeration in use (AGENT.md:151, AGENT.md:462–463, AGENT.md:556–561,
   DATAWATCH-CONTEXT.md:87, docs/parity-status.md:54) uses exactly the same
   8-surface set and omits the documentation-family surfaces.

2. **Documentation is governed by content-quality rules** (Documentation Rules
   `AGENT.md:117–178`, Docs-as-MCP `AGENT.md:1250–1274`, Feature Documentation
   `AGENT.md:1486–1534`) but **not by parity rules**. This creates a gap: a
   feature can pass every parity check (all 8 surfaces addressed) while shipping
   stale documentation, because no parity row requires the docs to be current.

3. **Every one of the three planned features** (eval suite × backend sweep,
   eval node in the PRD-DAG orchestrator, red-team validator pipeline)
   **would be required to carry a `Parity surface` section** under the rules
   as they stand today (because each feature has a plan doc under `docs/plans/`,
   triggering the `AGENT.md:151` clause and the `AGENT.md:656` release audit).
   However, all three currently name only 5 of the 8 required surfaces in
   `docs/parity-status.md`; they are each non-compliant with the current rule
   set for the missing Android, iPhone/iOS, and YAML/config rows, and would
   additionally be non-compliant with the proposed 11-surface set for the
   missing documentation, definitions, and howtos rows.

4. **The release checklist (`AGENT.md:656`) does require a parity-audit pass**,
   but it checks only 8 surfaces and does not verify the *artifact* (11-row
   `Parity surface` section) — the B6/B7/B8 sprint rows check inputs to the
   audit but not the completeness of the plan's section itself. The proposed
   B19 row and the revised release checklist item (Item 12) close this gap.

5. **The proposed 13 text corrections in §4, if adopted, would make the
   documentation surface a first-class parity target** alongside REST, MCP, CLI,
   comm, YAML/config, PWA, Android, and iPhone/iOS, and would require every
   plan/proposal doc to carry a complete 11-row `Parity surface` section with
   include/exclude-with-reason rows — making the three planned features'
   current 5-surface specs formally incomplete and forcing the documentation
   gap to be closed before implementation.

---

*This audit is a read-only recommendation document. It changes no rule text,
no code, and no plan spec. Adoption of the proposed corrections is a separate
deliberation.*

# Release Notes — v6.2.0 (BL221 Automata Redesign)

Released: 2026-05-03

## Summary

v6.2.0 is a major feature release delivering the full BL221 Automata redesign across 6 phases and all 7 surfaces (REST · MCP · Comm channel · CLI · PWA · locale en/de/fr/es/ja · datawatch-app issues). It also closes BL239 (nav bar width on wide screens) and BL240 (rate-limit auto-schedule recovery).

---

## BL221 — Automata Redesign

### Phase 1 — Launch Automaton Wizard (v6.1.0-dev)

- **Progressive-disclosure wizard** replaces the old "New PRD" modal
- **Intent textarea** with live type inference (research/operational/personal/software)
- **Type selector** — 4 clickable type buttons; auto-detected label shown below
- **Workspace section** — directory picker + project profile dropdown
- **Execution section** — backend selector + effort selector
- **Advanced section** (collapsed) — Guided Mode toggle, scan/rules/story-approval checkboxes, skills placeholder
- **Template shortcut** link opens Templates tab

### Phase 2 — Template Store CRUD

Full lifecycle for automaton template reuse:

- **REST endpoints**: `GET/POST /api/autonomous/templates`, `GET/PUT/DELETE /api/autonomous/templates/{id}`, `POST /api/autonomous/templates/{id}/instantiate`, `POST /api/autonomous/prds/{id}/clone_to_template`
- **Template model**: `{id, title, description, spec, type, tags[], is_template, created_at, updated_at}`
- **Instantiate**: fills `{{variable}}` placeholders; creates PRD with project_dir + backend + effort
- **Clone**: copies a completed PRD's spec + type + tags into a new template
- All surfaces wired (Phase 5)

### Phase 3 — Security Scan Framework

Three built-in scanners run on PRD project directories:

**SAST (16 rules)**
- Python: eval/exec, hardcoded credentials, SQL injection, unsafe deserialization, command injection
- Go: `fmt.Sprintf` in SQL, `os/exec` shell injection, unsafe pointer, math/rand seeding
- JS/TS: `innerHTML`/`eval`/`document.write`, `process.env` credential leak
- Shell: `eval`, unquoted variables, `curl | bash`

**Secrets scanner (8 patterns)**
- SEC001–SEC008: AWS key, GH token, private key PEM, Slack/Stripe/Twilio/SendGrid tokens, generic password assignments

**Dependency scanner**
- Detects 15+ manifest types (requirements.txt, package.json, go.mod, Cargo.toml, etc.)
- Checks lock files against 8 known-CVE pinned versions
- Reports missing lock files

**LLM grader (Option C)**
- Inline LLM call via `POST /api/ask` loopback (same pattern as `decomposeFn`)
- Returns `verdict` (pass/warn/fail) + `notes`
- Swappable `GraderFn` interface for full evals in future

**API**: `POST /api/autonomous/prds/{id}/scan`, `GET /api/autonomous/prds/{id}/scan`, `GET/PUT /api/autonomous/scan/config`

**Config fields**: `enabled`, `sast_enabled`, `secrets_enabled`, `deps_enabled`, `fail_on_severity`, `max_findings`, `grader_enabled`, `fix_loop_enabled`, `fix_loop_max_retries`

### Phase 3b — Fix Loop + Rule Editor

- **`CreateFixPRD`** — creates a child PRD whose spec describes all scan violations; operators run it normally; `fix_loop_enabled` + `fix_loop_max_retries` in scan config control automatic retry
- **`ProposeRuleEdits`** — calls LLM via `/api/ask` loopback to propose AGENT.md additions that would prevent the found issues from recurring
- **API**: `POST /api/autonomous/prds/{id}/scan/fix`, `POST /api/autonomous/prds/{id}/scan/rules`

### Phase 4 — Type Registry, Guided Mode, Skills

**Type registry**
- 4 built-in types: `software`, `research`, `operational`, `personal`
- Operator-extensible: `POST /api/autonomous/types` registers custom types with label, description, color
- `GET /api/autonomous/types` lists all types (builtins + custom)

**Guided Mode**
- Per-PRD boolean flag: `guided_mode: true` signals step-by-step operator checkpoint approval before each story
- Set at creation: `POST /api/autonomous/prds` body `{guided_mode: true}`
- Or via action: `POST /api/autonomous/prds/{id}/set_guided_mode` `{guided_mode: bool}`

**Skills**
- Per-PRD list of skill IDs (e.g. `["git","docker","pytest"]`)
- Set at creation: `POST /api/autonomous/prds` body `{skills: [...]}`
- Or via action: `POST /api/autonomous/prds/{id}/set_skills` `{skills: [...]}`
- Passed to spawned task sessions for context

**PRD model additions**: `type`, `guided_mode`, `skills`

### Phase 5 — 7-Surface Parity

**MCP tools added** (18 new tools total for BL221):
- Scan: `autonomous_scan_config_get`, `autonomous_scan_config_set`, `autonomous_prd_scan`, `autonomous_prd_scan_results`, `autonomous_prd_scan_fix`, `autonomous_prd_scan_rules`
- Types/Guided/Skills: `autonomous_type_list`, `autonomous_type_register`, `autonomous_prd_set_type`, `autonomous_prd_set_guided_mode`, `autonomous_prd_set_skills`
- Templates: `autonomous_template_list`, `autonomous_template_create`, `autonomous_template_get`, `autonomous_template_update`, `autonomous_template_delete`, `autonomous_template_instantiate`, `autonomous_prd_clone_to_template`

**CLI** (`datawatch autonomous`): 20 new subcommands across Phase 2/3/4 features. `prd-create` gains `--type`, `--guided-mode`, `--skills` flags.

**Comm channel**: 16 new verbs across Phase 2/3/4/5 (scan/type/guided/skills/template CRUD).

**PWA**:
- Launch wizard type buttons + guided mode checkbox + skills placeholder in advanced section
- PRD detail: type badge, guided mode indicator, skills chips in metadata
- Scan results section with verdict badge, findings list, fix/rules buttons
- Settings → Automata tab: scan config toggles

**Locale keys**: 24 new keys across all 5 bundles (en/de/fr/es/ja)

**datawatch-app issues filed**:
- #43 — Phase 4 mobile: type/guided/skills in create sheet + PRD detail
- #44 — Template Store UI: list, create, detail, instantiate, clone
- #45 — Scan results in PRD detail: verdict badge, findings, fix PRD, propose rules

---

## BL239 — Nav Bar Width on Wide Screens

- Bottom nav items now distribute evenly across the full 480px card width on desktop/tablet
- Fix: `justify-content: space-around` + `flex: 1` on `.nav-btn` in `@media (min-width: 480px)` breakpoint

---

## BL240 — Rate-Limit Auto-Schedule Recovery

- **6 additional patterns** added to detection: `"claude usage limit"`, `"you've exceeded"`, `"plan limit"`, `"resets at"`, `"stop and wait"`, `"wait for limit to reset"`
- **Line-length gate** raised from 1024 → 2048 chars (longer multi-line dialog text no longer bypasses detection)
- **Enter key** now sent after "1" selection (fixes the auto-accept flow that was sending "1" without confirming the choice)
- Auto-schedule via `schedStore.Add` was already present; now reliably triggers on all detected dialogs

---

## Testing

- 1644 unit tests passing
- Smoke test: 91 pass / 0 fail / 6 skip (`scripts/release-smoke.sh`)

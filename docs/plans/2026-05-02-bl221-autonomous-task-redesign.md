# BL221 — Autonomous Task ("PRD") Complete Redesign

**Backlog item:** BL221  
**Date:** 2026-05-02  
**Status:** Design discussion in progress — skeleton plan, not ready for implementation  
**Sprint target:** v6.2.0 (after v6.1 skills/identity/evals/council feature window)

---

## 1. What We Have Today — Honest Assessment

### 1.1 Backend Lifecycle (accurate)

The backend state machine is correct and well-structured:

```
draft → decomposing → needs_review → approved → running → completed
                    ↘ revisions_asked ↗                  ↘ cancelled
                                                          ↘ blocked (guardrail)
                                                          ↘ rejected (operator)
```

Sub-entity states:
- **Story**: `pending → awaiting_approval → in_progress → completed | blocked | failed`
- **Task**: `pending → queued → in_progress → running_tests → verifying → completed | failed | blocked | cancelled`

The backend is solid. The problem is entirely presentation and creation UX.

### 1.2 What the PWA Shows Today — Gaps

**List view:**
- Flat list of cards, no pagination, all PRDs loaded regardless of status
- Filter row hidden behind a toggle — operator must discover it
- No active/history split — `completed` PRDs clutter the default view
- No search by title/spec
- No multi-select; Delete is per-card only
- No checkboxes; cannot batch-archive or batch-delete

**Card layout:**
- Header: tiny `id` code + status pill + badges + title
- Sub-line: `N stories · N tasks · N decisions` (no progress)
- Actions row: all buttons rendered but conditional — no visual indication of *which step the operator should do now*
- `Stories & tasks` hidden behind a `<details>` — requires expand to see any progress
- `Decisions log` behind another nested `<details>`
- Children (child PRDs) behind yet another `<details>`
- **"running" tells the operator nothing**: no indication of which story or task is active

**The 7 action buttons (current):** Decompose · LLM · Approve · Reject · Revise · Run · Cancel · Edit · Delete — shown/hidden conditionally but no visual hierarchy; `LLM` shows at unexpected times; `Edit` shows when not logically needed.

**Child PRD navigation:**
- `scrollToPRD()` just scrolls to the row in the flat list
- No true drill-in / breadcrumb navigation
- Deep hierarchies (depth > 1) are essentially unusable

**New PRD modal:**
- Good: profile/dir picker, dynamic model list per backend, cluster picker
- Missing: session type, algorithm mode, skill assignment, PRD type (research/operational/personal/software), rules/security scan config per-PRD
- Labeled "New PRD" — wrong terminology for the redesigned system
- Too flat — creates a `draft` that then requires a separate Decompose step; a simple task shouldn't need to know about "decomposition"

**Security scan:**
- `autonomous.security_scan` → `SecurityScan()` in `security.go`: Python-only regex scan for `os.system()`, `eval()`, hardcoded secrets, HTTP to raw IPs
- Runs as a pre-commit quality gate in the verifier
- PWA exposes it only in Settings; no per-PRD override; no scan results shown in the card
- Extension for Go, TypeScript, JavaScript, Rust not implemented
- **No "validate rules followed" check**: no mechanism to verify that LLM output followed CLAUDE.md / AGENT.md / custom rule documents

---

## 2. Design Goals

1. **List view matches Sessions tab** — filter bar with status badges, active-by-default, history toggle, search, compact cards
2. **Multi-select + batch operations** — checkboxes, select-all, batch delete/archive
3. **Cards are scannable** — title, type badge, current lifecycle step, progress indicator, 3–5 contextual actions; full detail is one click away
4. **Detail view** — click a card → full workflow view with breadcrumbs, stories/tasks tree, timeline, all controls
5. **Lifecycle is always clear** — no ambiguous "running"; show "Story 2/5 · Task 3/8 · verifying" in real time
6. **Creation is a wizard** — "New Autonomous Task" starts narrow (type selection) and reveals only relevant fields
7. **Skill + type + algorithm mode** — first-class fields in creation, aligned with unified platform design
8. **Security scan + Rules check** — per-PRD toggle; security scan extended to all project languages; new LLM-based rules check verifies AGENT.md/CLAUDE.md compliance
9. **All 7 surfaces** — YAML + REST + MCP + CLI + Comm + PWA + Mobile companion parity throughout
10. **datawatch-app alignment** — every UI decision has a companion issue filed for the mobile app

---

## 3. Terminology Change

| Old | New |
|-----|-----|
| PRD | Autonomous Task |
| New PRD | New Autonomous Task |
| Spec | Task description |
| Decompose | Plan |
| Autonomous tab | Tasks tab (or keep Autonomous?) |
| Stories | Plans / Workstreams (TBD — see Q1) |
| Tasks (inside PRD) | Steps / Jobs (TBD — see Q1) |

> **Design question Q1** (see Section 8): Should we rename Stories → Workstreams and Tasks → Jobs to match the non-software ISA generalization? Or keep Stories/Tasks for backward compatibility with nightwire interop?

---

## 4. List View Redesign

### 4.1 Header bar additions

```
[ Autonomous Tasks ]   [?  How-to]   [⊞ Filter ▾]   [History]   [+ New Task]
```

- **`? How-to`** link: opens `/docs/howto/autonomous-planning.md` in a docs panel or new tab (BL221 requirement: header link to docs)
- **`⊞ Filter`** toggle: reveals filter bar (same pattern as Sessions tab)
- **`History`** toggle: switches between "Active" (default) and "All" list (same as Sessions tab's history button)

### 4.2 Active vs History

**Active** (default): shows `draft`, `decomposing`, `needs_review`, `approved`, `running`, `blocked`, `revisions_asked`  
**History**: additionally shows `completed`, `rejected`, `cancelled`, `archived`

Completed tasks are hidden by default — just like sessions.

### 4.3 Filter bar (visible when ⊞ toggled)

```
[ 🔍 Search title/spec... ]  
Status: [All ▾] [draft] [planning] [review] [approved] [running] [blocked]   (badge buttons, multi-select)
Type:   [All ▾] [software] [research] [operational] [personal]
[ ☐ Templates ] [ ☐ My tasks only ]
```

Status badges are clickable filters (same visual as Sessions tab's status pills). Multiple can be active at once.

### 4.4 Compact card (list row)

Each card is compact — enough to scan, not enough to read. Roughly:

```
┌─────────────────────────────────────────────────────────────────────────┐
│ ☐  [research]  auth-service refactor                        [blocked] ▶ │
│    2/5 stories · 7/18 tasks   ████████░░░░░░░░░░  38%                   │
│    Story 2: "API layer" · Task 4: verifying                              │
│    [Plan] [Review] [▶ Run] [⏹] [⋯]                                      │
└─────────────────────────────────────────────────────────────────────────┘
```

Elements:
- **Checkbox** (left): multi-select
- **Type badge**: `[software]` / `[research]` / `[operational]` / `[personal]` — color-coded
- **Title** (bold)
- **Status pill** (right, color = lifecycle step color)
- **Progress bar + percentage**: completed tasks / total tasks
- **Current position** (one-liner, only when `running`): `Story N: "title" · Task N: <status>`
- **Action strip** (always visible, 5 max): shows only the phase-relevant buttons, greyed out if not reachable

### 4.5 Multi-select + batch operations

- Checkbox top-left of each card
- "Select all" checkbox in toolbar when filter bar is open
- When ≥1 selected: toolbar shows `Delete selected (N)` and `Archive selected (N)` buttons

---

## 5. Action Button Design — The Lifecycle Strip

### 5.1 Problem with current approach

All buttons (`Decompose`, `LLM`, `Approve`, `Reject`, `Revise`, `Run`, `Cancel`, `Edit`, `Delete`) are conditionally shown/hidden. The operator cannot tell at a glance:
- What step they're on
- What the logical sequence is
- Which buttons they've already passed

### 5.2 Proposed: Lifecycle step buttons + phase indicator

The lifecycle steps are a fixed ordered sequence:

```
[1. Plan] → [2. Review] → [3. Approve] → [4. Run] → [5. Done]
```

In the compact card, these render as 5 small step-buttons, color-coded:

| Step | Color when current | Color when passed | Color when future |
|------|-------------------|-------------------|-------------------|
| Plan | Blue (active) | Green (done) | Grey (disabled) |
| Review | Blue (active) | Green (done) | Grey (disabled) |
| Approve/Reject/Revise | Blue (active) | Green (done) | Grey (disabled) |
| Run | Blue (active) | Green (done) | Grey (disabled) |
| Done | Green | — | Grey |

Plus two always-available controls: `Edit` and `Delete` (in the `⋯` overflow menu on compact cards).

**Backward navigation**: If a step can be revisited (e.g., `Revise` sends back to `draft` → re-Plan), the earlier step button re-activates. The visual sequence doesn't need to show the exact status name — it shows the operator's position in the workflow.

### 5.3 Detailed view: full workflow strip

In the detail view, the lifecycle strip expands to show sub-steps and provides access to less-common actions (Instantiate from template, view decisions log, etc.).

---

## 6. Detail View

Clicking a card navigates to a full detail view (replaces the list — back button returns to list, breadcrumb navigates the hierarchy).

### 6.1 Breadcrumb

```
Autonomous Tasks > auth-service refactor > Story: API layer > Child Task: openapi-spec > [Child PRD: openapi-migration]
                         ↑ root           ↑ story (click)    ↑ task (click)              ↑ child PRD (click)
```

Each breadcrumb item is clickable. Clicking a parent navigates back to that level's detail view. This solves the "no way to navigate depth > 1" problem.

### 6.2 Detail view layout

```
┌─ Breadcrumb ────────────────────────────────────────────────────────────┐
│ ← Autonomous Tasks > auth-service refactor                              │
├─ Header ────────────────────────────────────────────────────────────────┤
│  [research] auth-service refactor                     [running] id:a3f1 │
│  backend: claude · effort: high · model: claude-sonnet-4-6              │
│  created: 2026-05-02 · project: ~/src/auth · depth: 0                  │
├─ Lifecycle strip ───────────────────────────────────────────────────────┤
│  ●Plan ──── ●Review ──── ●Approve ──── ▶Run ──── ○Done                 │
│                                               ← current                │
├─ Progress ──────────────────────────────────────────────────────────────┤
│  Story 2 of 5 · Task 7 of 18 · 38% complete                            │
│  ████████░░░░░░░░░░░░░░░░                                               │
├─ Stories + Tasks tree ──────────────────────────────────────────────────┤
│  ▼ Story 1: Project setup               [completed] ✓                  │
│    Task 1.1: init repo          [completed] ✓  claude-code             │
│    Task 1.2: CI config          [completed] ✓  claude-code             │
│  ▶ Story 2: API layer                   [in_progress] ⚡               │
│    Task 2.1: design routes      [completed] ✓  claude-code             │
│    Task 2.2: implement handlers [in_progress] ⚡ claude-code  → session│
│    Task 2.3: write tests        [pending] ○                            │
│  ○ Story 3: Database migrations         [pending]                      │
│  ○ Story 4: Auth middleware             [pending]                      │
│  ○ Story 5: Integration tests           [pending]                      │
├─ Guardrail verdicts ────────────────────────────────────────────────────┤
│  security: pass ✓    rules: pass ✓    release-readiness: warn ⚠       │
├─ Decisions timeline ────────────────────────────────────────────────────┤
│  (collapsible — shows decision log with LLM calls, costs, verdicts)    │
└─────────────────────────────────────────────────────────────────────────┘
```

Key: task rows show a clickable session link when `session_id` is set (`→ session` links to the Sessions tab filtered to that session). Child PRDs show a `↳ child PRD` link that navigates into that child's detail view (pushing breadcrumb).

---

## 7. "New Autonomous Task" — Creation Wizard

### 7.1 Three creation paths

The wizard starts with a **type selector** that determines which fields appear:

```
What kind of task?

  [⚡ Quick task]          [📋 Complex plan]       [📄 From template]
  Write a spec, let        Write a spec, review     Pick a saved
  it decompose and run     stories before running   template + fill vars
  automatically            
```

**Quick task** (path A — minimal friction):
1. Title + spec textarea
2. Workspace (dir or profile picker)  
3. Session type (coding / research / operational / personal) — with smart default from workspace
4. `[Create & Plan]` → creates + kicks off decomposition automatically
5. Shows in list as `decomposing` immediately

**Complex plan** (path B — full control):
1. Title + spec textarea
2. Session type
3. Workspace (dir or profile picker)
4. Algorithm mode toggle (on by default for `research`/`operational`/`personal`)
5. Skills assignment (optional: pick a skill to run pre or post)
6. LLM settings (backend/effort/model — only shown if configurable for backend)
7. Decomposition profile (separate from execution profile)
8. Permission mode (only shown for claude backends)
9. Guardrails (per-task, per-story — shown as a collapsible advanced section)
10. Scan options: security scan + rules check toggles
11. `[Create as draft]` → creates draft, operator manually triggers Plan step

**From template** (path C):
1. Select template from list
2. Fill template variables (auto-rendered from `template_vars` schema)
3. Override workspace if needed
4. `[Create from template]`

### 7.2 Field visibility rules

| Field | Show when |
|-------|-----------|
| Backend selector | Always (when in complex or quick path without profile) |
| Effort selector | Backend is selected and has effort options |
| Model selector | Backend has a known model list |
| Permission mode | Backend is `claude-code` |
| Cluster profile | Project profile selected |
| Algorithm mode | Session type is research / operational / personal |
| Skills | Skills are installed (`~/.datawatch/skills/` non-empty) |
| Decomposition profile | Complex path only |
| Guardrails | Complex path → advanced section (collapsed by default) |
| Security scan toggle | Always (complex path) |
| Rules check toggle | Always (complex path) |

### 7.3 Session type integration

The session type field (from unified platform Week 5) drives:
- Default algorithm mode (research/operational/personal default on)
- Memory namespace (personal → personal namespace)
- Decomposition prompt variant (software/research/operational/personal)
- Default verifier (test suite vs citation check vs runbook check)

---

## 8. Security Scan + Rules Check — Design

### 8.1 Current security scan

`SecurityScan()` in `internal/autonomous/security.go`:
- Python-only (10 dangerous patterns: `os.system`, `eval`, `exec`, hardcoded secrets, HTTP to raw IPs, etc.)
- Runs as a verifier quality gate before marking a task `completed`
- No per-PRD override (only global `autonomous.security_scan` toggle)
- Results not surfaced in the card or detail view — just included in verification failure message

**Needed improvements:**
- Extend to Go, TypeScript, JavaScript, Rust, Bash (language-specific pattern sets)
- Surface scan results as a guardrail verdict (`security` guardrail) in the detail view
- Allow per-PRD scan configuration (enable/disable, language list, severity threshold)
- Show last scan result inline on the card (pass/warn/block badge)

### 8.2 New: Validate Rules Followed

**What it is:** An `llm_rubric` grader (from the evals framework, BL221 depends on this from unified platform Week 7) that runs after each task/story/PRD completes and checks whether the LLM's output respected the rules documented in:
- `AGENT.md` (project operating rules)
- `CLAUDE.md` (LLM instructions)
- Any custom rules file at a configurable path

**What it checks (examples for this repo):**
- Did new UI strings get added to all 5 locale bundles and use `t()`/`data-i18n`? (BL214 localization rule)
- Do new REST endpoints have MCP + CLI + messaging + PWA counterparts? (7-surface parity rule)
- Are new config keys reachable from all 7 surfaces?
- Does new code pass `gosec` at the function level?
- Were tests added for new functionality?

**How it works:**
1. After a task completes, the rules checker spawns an `llm_rubric` eval with:
   - The task git diff as context
   - The AGENT.md / CLAUDE.md as the rubric source
   - A structured prompt: "Review the diff against these rules. For each violated rule, output { rule: '...', status: 'violated', evidence: '...' }"
2. Results go into the `GuardrailVerdict` array under the `rules` guardrail name
3. `block` threshold configurable (default: any `violated` finding is `warn`; configurable to `block`)
4. Result shown as a verdict badge on the card + detail view

**This is the PAI connection:** PAI's Algorithm mode SUMMARIZE phase explicitly asks "did this follow the rules?" This is the datawatch-native counterpart — automated, not manual.

**Config keys (7-surface parity):**
- `autonomous.rules_check` (bool, enable/disable globally)
- `autonomous.rules_check_backend` (LLM backend for the checker)
- `autonomous.rules_check_model` (model for the checker)
- `autonomous.rules_file` (path to rules file, defaults to `AGENT.md` in project dir)
- Per-PRD: `rules_check` field (override global)
- REST: `PUT /api/config` for global; `PATCH /api/autonomous/prds/{id}` for per-PRD
- MCP: `config_set`, `autonomous_update_prd`
- CLI: `datawatch config set autonomous.rules_check true`
- Comm channel: `configure autonomous.rules_check=true`
- PWA: Settings → Autonomous section + per-PRD advanced settings
- Mobile: file datawatch-app parity issue

### 8.3 Relationship to evals framework

Both `security_scan` (as a `binary_test` grader running a security scanner) and `rules_check` (as an `llm_rubric` grader) are instances of the evals framework. When evals land in v6.1 (unified platform Week 7), these should be migrated to eval definitions:

```yaml
# ~/.datawatch/evals/security-scan.yaml
name: security-scan
applies_to:
  session_types: [coding, skill]
graders:
  - type: binary_test
    weight: 1.0
    command: "datawatch security-scan --dir {{session.project_dir}} --lang auto"
    pass_on_exit: 0
```

```yaml
# ~/.datawatch/evals/rules-check.yaml  
name: rules-check
applies_to:
  session_types: [coding, research, operational, personal, skill]
graders:
  - type: llm_rubric
    weight: 1.0
    rubric: |
      Review the diff against the rules in AGENT.md.
      For each rule: did the change comply? Output violations with evidence.
    rubric_context_file: "{{session.project_dir}}/AGENT.md"
    model: claude-sonnet
```

---

## 9. Lifecycle Tracking — What "Running" Should Mean

### 9.1 Today

`status: "running"` means `Manager.Run()` is in flight. That's it.

### 9.2 Needed

The card and detail view should show a **live progress summary** when `status === "running"`:

```
Story 2 of 5  ·  Task 7 of 18  ·  38%
Current: "Implement handlers" [in_progress] (started 4m ago)
```

Derived from:
- Count of completed tasks vs total tasks
- Count of completed stories vs total stories
- The task(s) with `status: "in_progress"` (can be plural if parallel)
- Time since `started_at` of the in-progress task

This is a pure frontend change — all the data is already in `PRD.Story[].Tasks[]`. The backend needs no changes. The frontend just needs to traverse the task tree instead of showing only the PRD-level status.

### 9.3 Real-time updates

The WebSocket already delivers `prd_update` events. The current handler (`case 'prd_update'`) reloads the entire PRD panel. In the redesign:
- List view: update just the affected card's progress bar and status
- Detail view: update the stories/tasks tree in place (no full reload)

---

## 10. Sprint Plan (implementation weeks — builds on v6.1 foundation)

This section is a planning skeleton. Actual week assignment happens when v6.1 ships.

### Phase 1 — Frontend redesign (no backend changes needed)
- Week A: List view (filter bar, status badges, compact cards, history toggle, checkboxes)
- Week B: Card lifecycle strip (step buttons, progress bar, live "running" summary)
- Week C: Detail view (breadcrumb, stories/tasks tree, timeline, live updates)
- Week D: Creation wizard (3-path wizard, field visibility rules, type integration)

### Phase 2 — Backend enhancements
- Week E: Rules check (llm_rubric grader wired as a guardrail, AGENT.md as rubric source)
- Week F: Security scan extended (Go/TS/JS/Rust language pattern sets, severity tiers, REST endpoint for results)
- Week G: PRD type field (software/research/operational/personal, decomposition prompt variants)
- Week H: Session type + algorithm mode wiring (pass through to session manager)

### Phase 3 — Integration + 7-surface parity
- Week I: MCP tools (`autonomous_create`, `autonomous_list`, `autonomous_status`, `autonomous_plan`, `autonomous_run`, `autonomous_cancel` — ensure all accept all new fields)
- Week J: CLI (`datawatch task create`, `datawatch task list`, `datawatch task plan`, `datawatch task run`) — rename from `autonomous` subcommand
- Week K: Comm channel commands (`task new`, `task status`, `task run`, `task cancel`)
- Week L: datawatch-app alignment (file comprehensive issue covering all redesign elements)

---

## 11. datawatch-app Alignment Issue (to file)

A single GitHub issue should be filed against the `datawatch-app` repo covering:

**Title:** `Autonomous Tasks redesign — mobile companion parity`

**Body checklist:**
- [ ] Compact task card with status color, type badge, progress bar
- [ ] Lifecycle step strip (5 steps, current step highlighted)
- [ ] Active vs History list toggle
- [ ] Filter bar: status badge filters, type filter, search
- [ ] Multi-select + batch delete/archive
- [ ] Detail view: breadcrumb navigation for child PRDs
- [ ] Detail view: stories/tasks tree with live status
- [ ] Detail view: guardrail verdicts (security, rules, release-readiness)
- [ ] Detail view: decisions timeline
- [ ] New Autonomous Task wizard: 3 creation paths
- [ ] New Task: session type selector
- [ ] New Task: algorithm mode toggle
- [ ] New Task: skills assignment
- [ ] New Task: security scan + rules check toggles
- [ ] How-to link in tab header
- [ ] Real-time updates via WebSocket for in-progress task progress
- [ ] Notification when task completes / is blocked

---

## 12. Open Design Questions

These need operator input before detailed implementation work begins:

**Q1 — Terminology: Stories / Tasks naming inside the PRD**  
The unified platform design generalizes PRDs beyond software (research, operational, personal). The terms "stories" and "tasks" are software-specific (Jira/Agile vocabulary). Options:
- Keep `Stories` + `Tasks` (nightwire interop, operator familiarity)
- Rename to `Workstreams` + `Steps` (more general)
- Use session-type-sensitive labels: `stories/tasks` for software, `phases/actions` for research, `sections/items` for operational
- Recommendation: keep Stories/Tasks for now; generalized labels in v6.3+ when PRD types are fully proven

**Q2 — "New Autonomous Task" vs "New Task" vs keep "New PRD"**  
The tab header is currently "Autonomous." Options:
- Rename tab to "Tasks" and button to "New Task"
- Keep tab as "Autonomous" but button becomes "New Autonomous Task"
- Tab stays "Autonomous" for brand continuity; button says "New Task"
- Recommendation: tab stays "Autonomous" (it's a mode, not just a list); button becomes "New Task" — shorter and matches the concept

**Q3 — Detail view navigation: slide-in panel vs full page replace**  
Options:
- Slide-in drawer alongside the list (like iOS detail view on iPad)
- Full page replace with breadcrumb back button (like iOS detail view on iPhone)
- Modal overlay
- Recommendation: full page replace with breadcrumb (matches mobile, cleaner PWA implementation, avoids off-canvas state management)

**Q4 — Rules check blocking vs warning**  
When a rules check violation is found:
- Always warn (never block run/completion) — low friction, operator can override
- Block by default, operator must acknowledge — high integrity
- Configurable per-rule (some rules block, others warn)
- Recommendation: warn by default globally; per-PRD `rules_check_mode: warn|block`; document how to set specific rules to block for teams that need enforcement

**Q5 — Security scan multi-language support**  
The current scanner is Python-only. For extension:
- Single configurable regex file per language (operator-extensible)
- Plug into existing scanners: gosec (Go), eslint-security (JS/TS), cargo-audit (Rust)
- Both (built-in patterns + delegate to external tools)
- Recommendation: both — built-in patterns for the immediate term (language-auto-detect from file extensions), delegate to external tools as `binary_test` graders in the evals framework for completeness

**Q6 — PRD type field: operator-visible or auto-inferred**  
The session type (from unified platform Week 5) flows into PRD creation. Should `type` be:
- Explicit operator selection in the creation wizard (operator picks software/research/etc.)
- Auto-inferred from the spec text and workspace (same inference rules as session type)
- Both (auto-infer with operator override)
- Recommendation: both — auto-infer as the default, show the inferred type in the wizard with a "change" affordance

**Q7 — Template management: same list vs separate section**  
Templates are currently shown with a "template" badge in the same flat list (when the "Include templates" checkbox is on). Options:
- Same list with checkbox (current)
- Separate "Templates" section under a tab or link in the header
- Hidden by default, accessible via a "Templates" button that replaces the list
- Recommendation: separate "Templates" button in the header bar (reduces clutter, makes templates discoverable without polluting the main task list)

---

## 13. Related Files

**Backend:**
- `internal/autonomous/models.go` — PRD/Story/Task/GuardrailVerdict structs; add `Type`, `RulesCheck`, `SecurityScanConfig` fields
- `internal/autonomous/security.go` — extend to multi-language; add severity tiers
- `internal/autonomous/executor.go` — wire rules_check grader into verification flow
- `internal/autonomous/api.go` — new endpoints: `GET /api/autonomous/prds/{id}/scan-results`
- `internal/autonomous/manager.go` — wire rules_check as post-task guardrail

**Frontend:**
- `internal/server/web/app.js` — `renderPRDRow`, `renderPRDActions`, `renderAutonomousView`, `openPRDCreateModal` — full redesign
- `internal/server/web/app.css` — new card classes, lifecycle strip, breadcrumb styles

**Documentation:**
- `docs/howto/autonomous-planning.md` — update to reflect new wizard + terminology
- `docs/howto/autonomous-review-approve.md` — update for new lifecycle strip
- `docs/api/autonomous.md` — add new fields, new endpoints

**Cross-references:**
- `docs/plans/2026-05-02-unified-ai-platform-design.md` — Week 5 (session types), Week 7 (evals framework for rules check)
- `docs/plans/README.md` — BL221 backlog entry

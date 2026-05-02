# BL221 — Automata Redesign (née "PRD")

**Backlog item:** BL221  
**Date:** 2026-05-02  
**Last updated:** 2026-05-02 — configuration surface added; "decomposing" renamed to "planning" throughout; guided mode term resolved; secrets scanner added; LLM-assisted fix loop + rule editor design; type extensibility via plugins; datawatch-app Watch/Auto expanded; identity automaton entry point added (Section 12b, Q22)  
**Status:** Design complete — implementation spec next  
**Sprint target:** v6.2.0 (after v6.1 skills/identity/evals/council feature window)

---

## 0. Design Decisions — Resolved (2026-05-02)

These questions were discussed and answered by the operator. Downstream sections reflect these decisions.

| # | Question | Decision |
|---|----------|----------|
| Q1 | Stories/Tasks naming | **Keep.** Even research/personal tasks use the same breakdown. Add display aliases per task type (e.g., "Phases" instead of "Stories" for research) — same data structure underneath. |
| Q2 | Create button label | **"Launch Automaton"** (singular — you're launching one thing that will spawn many). Tab label: **"Automata"** (see Section 3). |
| Q3 | Detail view navigation | **Full page replace + breadcrumb.** Same feel as session detail. |
| Q4 | Rules check: warn vs block | **Warn by default, prompt operator to intentionally decide.** Per-PRD AND per-story/task override. Operator offered a "update the rule" path from the prompt. Default secure mandate = intentional override required. |
| Q5 | Security scan expansion | **Pluggable scan framework.** Built-in scanners for all 6 container language layers using tools already installed in those images. Heavier/extended scanners (full SAST suites, DAST) as skills running in their own container instance under the daemon. See Section 8 for full tooling inventory. |
| Q6 | Task type auto-infer | **Auto-infer from spec + workspace.** Operator can override. |
| Q7 | Templates | **Separate "Templates" tab** with full CRUD interface. Clone-to-template from any completed automaton. Templates have their own workflow and list. |
| Q8 | Tab rename | **Rename to "Automata" aggressively.** No legacy alias in UI. CLI gets backward-compat `autonomous` alias for a deprecation window only. |
| Q9 | Display aliases per type | **Hardcoded defaults** (software→Stories/Tasks, research→Phases/Steps, operational→Workstreams/Actions, personal→Threads/Items) with operator override in Settings → Automata. |
| Q10 | "Edit rule" path | **Link to the rules file path** (operator opens it themselves). Full rule editor deferred to v6.3+. |
| Q11 | Scan initial scope | **Built-in scanners for all 6 container language layers** using tools already installed (golangci-lint, ruff, eslint, clippy, rubocop, spotbugs). Heavier scans (Semgrep, ZAP) as skills in separate containers. |
| Q12 | Rules check granularity | **Task level by default.** All layers (task/story/PRD) get rules check. Configurable per-automaton, per-story, per-task with opt-out in wizard. Ollama/local backends incur only time not cost, so default on is appropriate. |
| Q13 | "Decomposing" terminology | **Rename to "Planning"** everywhere — UI, status badge, docs, messaging. Backend status code `decomposing` → `planning` (read old values for back-compat, write new). |
| Q14 | "Algorithm mode" terminology | **Rename to "Guided mode"** — fits the Automata aesthetic; implies the automaton is guided through structured phases before acting. See Section 3.2. |
| Q15 | Config tab for Automata settings | **New "Automata" settings tab** in PWA Settings. Consolidates all related config currently scattered across General + autonomous sections. All settings follow 7-surface rule. See Section 8b. |
| Q16 | Type extensibility | **Plugin-extensible type system.** Built-in 4 types (software/research/operational/personal) are defaults. Plugins and skills can register new types via manifest. See Section 3.3. |
| Q17 | Multi-select batch actions | **Contextual batch actions only.** Show only actions that are valid for ALL selected automata simultaneously. E.g. "Run all" only if all are `approved`. See Section 4.5 update. |
| Q18 | Secrets scanner | **Always-on secrets scanner** when `.git` present — scans git history, not just current files. Blocks on any secret found. Uses `gitleaks` (built-in) + `trufflehog` (skill for deep scan). See Section 8.6. |
| Q19 | Rule editor LLM path | **LLM proposes rule diff, operator approves.** Configured model analyzes the violation + AGENT.md and generates 2–3 proposed edits. Operator approves or uses LLM to insert custom text. See Section 8.7. |
| Q20 | Reject/fix loop | **LLM-assisted fix proposal loop.** On reject/block, system spawns a fix analysis mini-session, proposes specific changes, operator approves, retry spawns, verifies. Loops until accepted or max retries. See Section 8.8. |
| Q21 | datawatch-app Watch + Auto | **Explicit Watch OS + Android Auto design requirements** in the app alignment issue. datawatch-app owns the platform-specific design; the issue defines the data surface and intent. See Section 11 update. |
| Q22 | Identity automaton entry point | **Robot icon in PWA header** → lookup existing identity automaton (any state) → open it or launch create modal. Interview skill type (`type: interview`) drives structured Q&A to build/update `identity.yaml`. GET/SET REST endpoints for programmatic access; "configure" always goes through an automaton run. `FilePath` added to PRD struct for file-targeted automata. See Section 12b. |

---

## 1. What We Have Today — Honest Assessment

### 1.1 Backend Lifecycle (accurate)

The backend state machine is correct and well-structured:

```
draft → planning → needs_review → approved → running → completed
                 ↘ revisions_asked ↗                  ↘ cancelled
                                                       ↘ blocked (guardrail)
                                                       ↘ rejected (operator)
```

> **Terminology note:** Backend status code `decomposing` is renamed to `planning` (BL221 Q13). Old stored values are read as `planning` for back-compat; all writes use the new code.

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
- Missing: session type, guided mode, skill assignment, automaton type (research/operational/personal/software), rules/security scan config per-automaton
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
6. **Creation is a wizard** — "Launch Automaton" starts narrow (intent field) and reveals only relevant fields
7. **Skill + type + Guided Mode** — first-class fields in creation, aligned with unified platform design
8. **Security scan + secrets scanner + Rules check** — per-automaton toggle; secrets scanner always-on for `.git` repos; security scan extended to all project languages; LLM-based rules check verifies AGENT.md/CLAUDE.md compliance
9. **LLM-assisted operations** — fix proposal loop on rejection; LLM rule editor on violations; structured diagnosis before any retry
10. **Plugin-extensible type system** — 4 built-in types + unlimited plugin-registered types
11. **Consolidated settings surface** — Settings → Automata tab; all config follows 7-surface rule
12. **All 7 surfaces** — YAML + REST + MCP + CLI + Comm + PWA + Mobile companion parity throughout
13. **datawatch-app alignment** — 3 issues filed for Phone / Wear OS / Android Auto platforms

---

## 3. Terminology — Resolved

| Old | New |
|-----|-----|
| PRD | **Automaton** (singular) / **Automata** (plural) |
| New PRD | **Launch Automaton** |
| Autonomous tab | **Automata** tab |
| Spec | Intent (the operator's natural language description of what they want to happen) |
| Decompose | **Plan** |
| Stories | **Stories** — kept for nightwire interop. Display alias shown per type: "Stories" for software, "Phases" for research, "Workstreams" for operational, "Threads" for personal. Same data structure. |
| Tasks (inside PRD) | **Tasks** — kept. Display alias: "Steps" for research/operational/personal. Same data structure. |
| Templates tab | **Templates** — separate tab, full CRUD, clone-from-automaton |

### 3.1 Why "Automata"

An automaton is a self-operating machine. When the operator launches one, they're starting a system that will independently analyze, plan, decompose into stories and tasks, spawn workers, verify results, and capture learnings — without needing to be driven step by step. The name matches the reality of what the system does.

"PRD" was always a misnomer — it's not a product requirements document; it's a declarative intent that gets executed. "Automaton" captures the execution-first nature.

### 3.2 The Automata Lifecycle as Guided Mode

A key insight from the unified platform design (Guided Mode, Week 5): **the automaton lifecycle is Guided Mode applied at the project scale**.

```
Launch intent           → OBSERVE:   system reads context (project dir, memory, identity)
                        → ORIENT:    system infers type, constraints, success criteria
Operator approves plan  → DECIDE:    reviewed decomposition becomes the execution contract
Execution runs          → ACT:       stories + tasks fire in DAG order
Learnings captured      → SUMMARIZE: memory records outcomes, decisions, surprises
```

The creation wizard is therefore the Observe + Orient phases made interactive. The "Plan" step is the system's first act. The review/approve gate is the Decide phase. This means:

- Automata created with **Guided Mode ON** → the Plan step produces a structured Observe → Orient → Decide output before planning begins
- The operator reviews the system's *framing of the problem* before the task breakdown
- This surfaces assumptions early, reducing wasted execution

The existing `guided_mode` flag (Week 5 of the sprint plan) flows directly into automaton creation.

### 3.3 Plugin-Extensible Type System

The 4 built-in types (software, research, operational, personal) are the defaults. Plugins and skills can register new types via a manifest, enabling domain-specific automata (e.g., `incident-response`, `security-audit`, `data-pipeline`).

**Type manifest schema** (in a plugin's `plugin.yaml`):

```yaml
automaton_types:
  - name: incident-response
    display_name: Incident Response
    story_alias: Phases         # shown instead of "Stories"
    task_alias: Actions         # shown instead of "Tasks"
    type_badge_color: "#dc2626" # red
    decomposition_prompt_template: |
      You are a senior SRE. Decompose this incident response plan into
      phases (investigation, mitigation, remediation, postmortem) and
      concrete actions with clear owners and timeboxes.
      Incident: {{intent}}
    default_guided: true        # Guided Mode ON by default for this type
    default_scanner: sre-runbook-check  # skill name for intent scanner
    icon: "🚨"
```

**Registry:** datawatch loads all installed plugins on startup and populates the type registry. The Launch Automaton wizard reads the registry to populate the type dropdown.

**Backend:** `internal/autonomous/typeregsitry/` package — a map from type name to `AutomatonTypeSpec`. Built-in types are registered at init. Plugin-registered types are loaded from plugin manifests at plugin-load time.

**7-surface parity:**
- YAML: `autonomous.custom_types: [...]` (static config) or loaded from plugins
- REST: `GET /api/autonomous/types` (returns all registered types)
- MCP: `list_automaton_types` tool
- CLI: `datawatch automaton types`
- Comm: `automaton types`
- PWA: populated automatically in Launch wizard type dropdown
- Mobile: datawatch-app parity issue

---

## 4. List View Redesign

### 4.0 Tab structure

The "Autonomous" tab becomes a top-level section with **two sub-tabs**:

```
┌─────────────────────────────────────────────────────┐
│  Automata  │  Templates                              │
└─────────────────────────────────────────────────────┘
```

- **Automata** — active and recent automata (the redesigned main view); has a History toggle in its header bar
- **Templates** — full CRUD for reusable automaton templates (see Section 9b); Templates have no "history" concept — they persist until deleted

**Why no History sub-tab:** History is a *contextual view modifier* on the Automata list, not a separate destination. A dedicated sub-tab would imply a different data model. Instead, the `History` toggle lives in the Automata tab's header bar exactly as it does in the Sessions tab. Templates have no completed/archived state, so no toggle needed there.

### 4.1 Header bar additions

The header bar is **contextual to the active sub-tab**. When the Automata sub-tab is active:

```
[ Automata ]   [?  How-to]   [⊞ Filter ▾]   [History]   [⚡ Launch Automaton]
```

When the Templates sub-tab is active:

```
[ Templates ]   [?  How-to]   [⊞ Filter ▾]   [+ New Template]
```

The `History` toggle is **absent on the Templates tab** — Templates persist indefinitely; there is no completed/archived history for them.

Button meanings:
- **`? How-to`**: opens `/docs/howto/autonomous-planning.md` in a docs panel or new tab
- **`⊞ Filter`**: reveals the filter bar (same pattern as Sessions tab); available on both sub-tabs
- **`History`** (Automata tab only): switches the Automata list between "Active" (default) and "All" (includes completed/cancelled/archived)

### 4.2 Active vs History

**Active** (default): shows `draft`, `planning`, `needs_review`, `approved`, `running`, `blocked`, `revisions_asked`  
**History** (toggle on): additionally shows `completed`, `rejected`, `cancelled`, `archived`

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

### 4.5 Multi-select + contextual batch operations

**Selection mechanics:**
- Checkbox top-left of each card
- "Select all" checkbox in the list toolbar (selects all visible filtered results)
- Selected count shown in toolbar: `3 selected`

**Contextual batch actions — only show actions valid for ALL selected automata:**

When ≥1 automaton is selected, the toolbar shows a context-sensitive action bar. An action only appears if *every* selected automaton can take that action right now.

| Action | Shows when all selected are... |
|--------|-------------------------------|
| `▶ Run all (N)` | `approved` |
| `✓ Approve all (N)` | `needs_review` |
| `⏹ Cancel all (N)` | `running` or `approved` |
| `🗄 Archive all (N)` | `completed` or `rejected` or `cancelled` |
| `🗑 Delete all (N)` | Any non-`running` status |

If the selection is mixed (e.g., some `approved`, some `running`), only `Delete` appears (the one action valid across both). This avoids accidentally running actions that don't apply to part of the selection.

**Selection cleared** when the user navigates away or clicks outside the list.

**Keyboard shortcut:** Space bar toggles selection on focused card; Cmd/Ctrl+A selects all visible.

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

## 7. "Launch Automaton" — Creation Wizard

### 7.1 The creation philosophy

The operator's experience: **"I have an idea and I want to do X to it."**

The wizard's job: take that one sentence and turn it into a properly-structured automaton that will intelligently analyze, plan, and execute. The wizard should feel like starting a conversation, not filling out a form.

Starting point is always the **intent** field — a single large text area with a helpful placeholder:

```
What do you want to accomplish?

  "Add rate limiting to the API gateway, with per-user quotas and
   a Redis backend. Include tests and update the docs."

  "Research the current state of post-quantum cryptography standards
   and summarize what we need to prepare for migration."

  "Write a blog post about the new session management features we
   shipped in v6.0."
```

The system does the rest — inferring type, workspace suggestion, appropriate defaults.

### 7.2 Wizard flow — single stream, progressive disclosure

The wizard is a **single vertical stream**, not tabbed steps. Fields appear as earlier choices clarify what's needed. This avoids the "which tab am I on?" problem.

```
┌──────────────────────────────────────────────────────────────┐
│  ⚡ Launch Automaton                                    [✕]  │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│  What do you want to accomplish?                             │
│  ┌────────────────────────────────────────────────────────┐  │
│  │ Add rate limiting to the API...                        │  │
│  │                                                    🎤  │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                              │
│  ─── inferred ──────────────────────────────────────────── │
│  Type: [software ▾]   Workspace: [~/src/api ▾]             │
│  ← auto-detected — tap to change                            │
│                                                              │
│  ─── execution ────────────────────────────────────────── │
│  Backend: [claude-code ▾]   Effort: [high ▾]               │
│  Model: [claude-sonnet-4-6 ▾]   Permission: [default ▾]    │
│  (Permission mode shown only for claude backends)           │
│                                                              │
│  ─── advanced (collapsed by default) ──────────────────── │
│  [ ] Guided mode (Observe→Orient→Decide before planning)    │
│  [ ] Skills:  [none selected ▾]                             │
│  [ ] Per-story approval gate                                │
│  Guardrails: [─────────────────────────────]  expand ▾     │
│  Scan: [✓] Security   [✓] Rules check                      │
│                                                              │
│  ─── or use a template ─────────────────────────────────── │
│  [📄 Choose from Templates →]                               │
│                                                              │
│  [Cancel]                        [⚡ Launch →]              │
└──────────────────────────────────────────────────────────────┘
```

"Launch →" creates the automaton and immediately starts the planning (decomposition) step. The operator is dropped into the detail view watching the planning happen in real time. They review and approve the plan before execution begins.

### 7.3 Field visibility rules

| Field | Show when | Hidden when |
|-------|-----------|-------------|
| Type selector | Always (starts auto-inferred, operator can change) | — |
| Workspace picker | Always | — |
| Backend selector | Always | — |
| Effort selector | Backend has effort options | Backend has no effort concept |
| Model selector | Backend has a known model list | Backend has no model list |
| Permission mode | Backend is `claude-code` | Other backends |
| Cluster profile | Project profile selected as workspace | Using directory mode |
| Guided mode | Type is research / operational / personal (pre-checked); coding (unchecked) | — |
| Skills | `~/.datawatch/skills/` is non-empty | No skills installed |
| Decomposition profile | Always in advanced section | — |
| Per-story approval | Always in advanced section | — |
| Guardrails | Always in advanced section (collapsed) | — |
| Security scan | Always in advanced section (pre-checked) | — |
| Rules check | Always in advanced section (pre-checked) | — |

### 7.4 Type auto-inference

The system infers type from two signals:

1. **Workspace**: if the selected directory contains a `.git` + source files → `software`. Cluster profile → `software` by default.
2. **Intent text**: keyword scan on submission:
   - "research", "analyze", "summarize", "literature", "survey", "read" → `research`
   - "write", "blog", "post", "content", "draft" → `operational`
   - "personal", "goal", "plan my", "habit" → `personal`
   - Default: `software`

Both signals are combined (workspace wins ties). Inferred type shown with a "← auto-detected" annotation, tap/click to override.

### 7.5 The planning experience after "Launch"

After the operator clicks "Launch →", the system:

1. Creates the automaton in `draft` state
2. If **Guided Mode** is on: runs the Observe → Orient → Decide pre-planning session first (produces a structured framing of the problem — assumptions, constraints, success criteria) — shown in the detail view as a collapsible "Planning context" card
3. Runs planning (`planning` state)
4. Transitions to `needs_review` — operator sees the plan appear in real time in the detail view
5. Operator reviews stories + tasks, edits if needed, approves or requests revision
6. On approval: execution begins (`running` state)

This is **intentional flow, not accidental clicks**. The operator sees exactly what the system is planning to do before any workers are spawned.

---

## 8. Security Scan + Rules Check — Resolved Design

### 8.1 Current security scan — honest state

`SecurityScan()` in `internal/autonomous/security.go` is **Python-only regex patterns** (a port of nightwire's `quality_gates.py` with 10 hardcoded regexes). It is not gosec or eslint. It runs as a pre-commit verifier gate. Results are buried in verification failure text, never surfaced as a verdict badge. `autonomous.security_scan` in Settings is the only control; no per-automaton override exists.

This is a starting point, not a security system.

### 8.2 Language layers — tooling already available

The 6 container language layers (`Dockerfile.lang-*`) already install the key quality tools. The scan framework uses these — no new packages needed for the baseline scanners:

| Layer | Already installed | Built-in scanner | Needs adding |
|-------|------------------|-----------------|--------------|
| **Go** | `golangci-lint v2.6.1` | golangci-lint (includes gosec, staticcheck, errcheck, shadow) | `govulncheck` for dependency scan |
| **Python** | `ruff`, `pyright` | ruff (linting + select security rules) | `bandit` for SAST, `pip-audit` for dependency scan |
| **Node/JS/TS** | `eslint v9` | eslint + security plugin | `eslint-plugin-security`, npm audit (already in npm) |
| **Rust** | `clippy`, `rustfmt` | clippy (security-relevant lints built in) | `cargo-audit` for dependency CVEs |
| **Ruby** | `rubocop v1.86` | rubocop + rubocop-rails-omakase | `brakeman` for Rails SAST, `bundler-audit` for dependency scan |
| **Kotlin/Java** | `gradle`, `kotlin v2.1` | gradle build warnings | `dependency-check` (OWASP) as skill |

**The key insight**: golangci-lint already includes the gosec rule set (as `gocritic` + security analyzers). Ruff already flags many Python security antipatterns. Clippy includes memory safety and unsoundness lints. These run in the same container instance as the task — no separate scan container needed.

### 8.3 Pluggable scan framework architecture

```
internal/autonomous/scan/
├── framework.go        ← Scanner interface, registry, language detection
├── runner.go           ← Run applicable scanners, aggregate verdicts
├── builtin/
│   ├── golangci.go     ← wraps golangci-lint --out-format json
│   ├── ruff.go         ← wraps ruff check --output-format json
│   ├── eslint.go       ← wraps eslint --format json
│   ├── clippy.go       ← wraps cargo clippy --message-format json
│   ├── rubocop.go      ← wraps rubocop --format json
│   └── npmaudit.go     ← wraps npm audit --json
└── intent/
    ├── factcheck.go    ← llm_rubric: verifies factual claims (research tasks)
    └── piiscan.go      ← pattern scan for PII (personal tasks)
```

**Scanner interface:**
```go
type Scanner interface {
    Name() string
    Category() string          // sast | dependency | lint | intent
    Languages() []string       // ["go"] or ["js","ts"] or ["*"] for intent
    Run(ctx context.Context, req ScanRequest) (ScanResult, error)
}

type ScanRequest struct {
    ProjectDir   string
    FilesTouched []string      // from Task.FilesTouched — limits scan scope
    TaskID       string
    TaskType     string        // coding | research | operational | personal
}

type ScanResult struct {
    Findings []Finding
    Outcome  string            // pass | warn | block
}

type Finding struct {
    File     string
    Line     int
    Rule     string
    Severity string            // info | low | medium | high | critical
    Message  string
}
```

**Language detection**: framework inspects `FilesTouched` extensions → selects scanners. A Go task that touches `.go` files runs golangci-lint + govulncheck. A task touching `.py` + `.js` runs both ruff and eslint. Cross-language tasks get all applicable scanners.

**Severity mapping** (determines `GuardrailVerdict.Outcome`):
- `block`: critical CVE, hardcoded secret, injection sink (e.g., `gosec G101: hardcoded credentials`)
- `warn`: medium CVE, deprecated API, gosec G306 (file permissions), eslint-security rule violation
- `pass`: only info/low findings

**Heavier scanners as skills** (run in separate container instance):
- Semgrep (cross-language SAST, community ruleset)
- OWASP Dependency Check (Java/Kotlin deep dependency tree)
- ZAP (DAST — future, requires a running service endpoint)
- Trivy (container image scanning — for Dockerfile tasks)

Each is a skill manifest in `~/.datawatch/skills/scan-*/skill.yaml` that takes a project dir and returns the standard `ScanResult` JSON. Skills run via the skill executor, not the task worker.

**For research and personal tasks:**
The same `Scanner` interface applies with intent-specific implementations:
- Research: `fact-check` scanner (llm_rubric — verifies factual claims, flags unsupported assertions)
- Research: `source-quality` scanner (checks if sources are cited, primary vs. secondary)
- Personal: `pii-scan` scanner (regex + LLM pattern scan for PII that shouldn't persist)

### 8.4 Adding missing tools to language layers

The Dockerfiles need minor additions for full coverage. These are small additions to the existing `RUN` blocks:

| Layer | Tool to add | How |
|-------|------------|-----|
| `lang-go` | `govulncheck` | `go install golang.org/x/vuln/cmd/govulncheck@latest` |
| `lang-python` | `bandit`, `pip-audit` | `pipx install bandit pip-audit` |
| `lang-node` | `eslint-plugin-security` | Added to global eslint install |
| `lang-rust` | `cargo-audit` | `cargo install cargo-audit` |
| `lang-ruby` | `brakeman`, `bundler-audit` | `gem install brakeman bundler-audit` |

These additions are minimal — each tool is small and installs quickly. They belong in a `BL228 — add security scanner tools to language layers` backlog item (filed separately — prerequisite for Phase 3).

### 8.5 Rules check — resolved design (task level by default, warn + intentional override)

**Resolved:** On by default at **task level** (catches violations immediately, before the next task starts). Ollama/local backends incur only time — not cost — so default-on is appropriate. Any violation pauses execution and prompts the operator with an intentional override dialog. The operator can:
1. Override and continue ("I know, proceed")
2. Reject the task/story and request a fix
3. Update the rule (links to the rules file for editing)

This is not optional to skip silently. The default secure mandate means violations require human acknowledgment. If the operator doesn't respond, the automaton stays in `blocked` state.

**How the rules check works:**

After each task completes, the rules checker:
1. Reads `AGENT.md` + `CLAUDE.md` from the project directory (configurable)
2. Gets the task's git diff (`Task.FilesTouched` + SHA)
3. Runs an `llm_rubric` grader:
   ```
   "Review this diff against the rules in the attached AGENT.md.
   For each rule in AGENT.md, output:
   { rule_name: '...', status: 'complied|violated|not_applicable', evidence: '...' }
   Return only items where status = 'violated'."
   ```
4. Any `violated` findings → `GuardrailVerdict` with `guardrail: "rules"`, `outcome: "warn"` (or `"block"` if per-PRD override)
5. Task transitions to `blocked` with a `rules_check_pending` sub-state
6. Operator sees the violation prompt (all 7 surfaces: alert + messaging + PWA notification)

**Override prompt (all 7 surfaces):**
```
⚠ Rules check: 2 violations in task "implement handlers"

  • BL214: New UI string "rate_limit_exceeded" has no locale key in fr.json
  • Config parity: POST /api/rate-limits has no MCP tool counterpart

  [Fix and re-verify]   [Override and continue]   [Edit rule]
```

**Config (7-surface parity):**
- `autonomous.rules_check` (bool, global default: true)
- `autonomous.rules_check_backend` / `autonomous.rules_check_model`
- `autonomous.rules_check_mode: warn|block` (default: warn)
- `autonomous.rules_file` (default: `AGENT.md` in project dir, fallback to `CLAUDE.md`)
- Per-automaton override: `rules_check`, `rules_check_mode` fields on the PRD struct
- Per-story/task override: same fields on Story/Task structs
- All 7 surfaces: YAML + `PUT /api/config` + MCP `config_set` + CLI `datawatch config set` + comm `configure` + PWA Settings → Autonomous + mobile parity issue

### 8.4 Relationship to evals framework

Both scanners and the rules check are instances of the evals framework (unified platform Week 7). When evals land in v6.1, they migrate to eval definitions:

```yaml
# ~/.datawatch/evals/security-scan.yaml
name: security-scan
applies_to:
  session_types: [coding, skill]
graders:
  - type: binary_test
    weight: 1.0
    command: "datawatch scan --dir {{session.project_dir}} --category sast --lang auto"
    pass_on_exit: 0
  - type: binary_test
    weight: 0.8
    command: "datawatch scan --dir {{session.project_dir}} --category dependency"
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
    rubric_context_file: "{{session.project_dir}}/AGENT.md"
    rubric: |
      Review the diff against every rule in the attached AGENT.md.
      Output only violations: { rule_name, status: "violated", evidence }.
    model: claude-sonnet
    block_threshold: 0    # any violation → warn (operator must acknowledge)
```

The existing `security.go` Python-only scanner becomes one `binary_test` grader definition. The framework replaces the hard-coded quality gate.

### 8.5 CLI scan subcommand

The scan framework gets a dedicated CLI entrypoint (independent of automaton execution):

```bash
datawatch scan --dir ~/src/api --category sast,dependency --lang auto
datawatch scan --dir ~/src/api --category rules --rules-file AGENT.md
```

This allows operators to run scans manually, wire them into CI, and verify scanner configuration outside of an automaton run. All 7-surface parity: REST `POST /api/scan`, MCP `run_scan`, CLI, comm `scan <dir>`, PWA "Run scan" button in automaton detail view, mobile parity issue.

### 8.6 Secrets Scanner — Always-on when `.git` present

**Rationale:** Secrets in git history are a critical security failure. The current scanner only checks current file content. Secrets in previous commits persist forever — they must be caught and remediated before an automaton's code is ever pushed.

**Behavior:**
- **Trigger**: When a task's project directory contains a `.git` folder, the secrets scanner runs automatically. No opt-out for `sast` or `dependency` categories; secrets are always-on.
- **Scope**: Scans the **entire git history** from initial commit to HEAD, not just the working tree or staged changes. A secret committed three months ago and `git rm`'d still exists in history.
- **Outcome**: Any secret finding → `block`. This cannot be overridden with "Override and continue." The operator must remediate first (purge history with `git filter-repo`, rotate credentials, then re-run).

**Tools:**
- **`gitleaks`** — built-in. Installed in all 6 language layer containers plus the base worker image. Fast, low false-positive rate, covers 150+ secret types. Runs with `--source . --log-opts --all`.
- **`trufflehog`** — skill. Deeper entropy analysis, S3/GCS remote scanning, filesystem mode. Invoked as `skill: trufflehog-scan` for comprehensive audits. Not default-on (slower); operator can trigger manually or configure to run as part of automaton launch.

**Scanner category:** `secrets`. Added to the Scanner interface `Category()` return values: `sast | dependency | lint | secrets | intent`.

**Integration point:** Runs in `internal/autonomous/scan/builtin/secrets.go` as a `SecretsScanner` implementing the `Scanner` interface. The runner invokes it before any other scanner when `.git` is detected — it is a gate, not an informational check.

**7-surface parity:**
- Config: `autonomous.secrets_scan: true` (default), `autonomous.secrets_scan_deep: false` (trufflehog)
- REST: scan results included in `GET /api/autonomous/{id}/scan-results`
- MCP: `run_scan` with `category: "secrets"`
- CLI: `datawatch scan --category secrets --dir .`
- Comm: `scan secrets <dir>`
- PWA: verdict badge in detail view; block overlay with remediation instructions
- Mobile: notification when secrets found with link to detail view

### 8.7 LLM-Assisted Rule Editor

When a rules check violation is surfaced, the operator can request a **rule edit** rather than overriding or ignoring the violation. The LLM assists with the edit rather than requiring the operator to manually locate and modify `AGENT.md`.

**Flow:**

1. Rules check produces violation: e.g., "BL214: New UI string has no locale key in fr.json"
2. Operator clicks **"Edit rule"** in the violation prompt
3. System opens a **rule editor panel** showing:
   - The violation in context (diff excerpt + matched rule text)
   - The current rule in `AGENT.md`
4. Operator is offered two paths:
   - **"Let LLM propose changes"** — system sends to configured model: the violation, the diff, the current AGENT.md rule, and a prompt to generate 2–3 proposed modifications (clarify rule, add exception, tighten scope, etc.)
   - **"Edit directly"** — opens an inline editor for AGENT.md; LLM assists with insertion placement

**LLM-proposed diff flow:**

```
┌── Rule Edit: "localization_rule" ───────────────────────────────────────┐
│ Violation: "rate_limit_exceeded" string added, no fr.json key           │
│ Current rule: "Every new user-facing string adds keys to all 5 locale   │
│   bundles + wires through t()/data-i18n"                                │
│                                                                          │
│ LLM proposes 3 options:                                                  │
│                                                                          │
│ Option 1: Add exception clause                                           │
│   + "Exception: error-only strings visible to operators in PWA          │
│     (not end users) are exempt from locale requirements"                 │
│   [ Accept this ]                                                        │
│                                                                          │
│ Option 2: Clarify scope                                                  │
│   ~ Change "user-facing string" to "end-user visible string"            │
│   [ Accept this ]                                                        │
│                                                                          │
│ Option 3: Add explicit list of exempt locations                          │
│   + "Exempt: admin panel strings, PWA debug views, error codes"         │
│   [ Accept this ]                                                        │
│                                                                          │
│ [ Edit custom → ] (opens editor with LLM insertion assistance)          │
└─────────────────────────────────────────────────────────────────────────┘
```

**LLM insertion:** When the operator accepts an option or writes custom text, the configured model is given:
- The full current `AGENT.md`
- The accepted change
- Instruction: "Insert this change into the appropriate section of AGENT.md, preserving formatting, numbering, and existing structure. Return only the modified file."

The operator sees a diff of the proposed AGENT.md change and clicks **Apply** to write it. The rules check is re-run against the task that triggered the violation.

**Config (7-surface):**
- `autonomous.rule_editor_model` (default: `autonomous.rules_check_model`)
- REST: `POST /api/autonomous/rules/propose-edit` with `{ violation, diff, rules_file }`
- MCP: `propose_rule_edit` tool
- CLI: `datawatch rules edit --violation-id <id>` (interactive)
- Comm: not applicable (complex UI interaction)
- PWA: inline rule editor panel triggered from violation prompt
- Mobile: deep-link to PWA rule editor (complex editing stays on PWA/desktop)

### 8.8 LLM-Assisted Fix Proposal Loop

When an operator rejects a task or story, or when a guardrail blocks execution, the system does not silently retry. Instead it enters a **structured fix loop** with LLM-assisted diagnosis and operator-approved fixes.

**Trigger conditions:**
- Operator clicks **"Reject and request fix"** on a task/story
- Security scanner returns `block` on a task
- Rules check returns `block` (max-violations exceeded)
- Guardrail verdict: `blocked`

**Fix loop flow:**

```
             Violation / Rejection
                      │
                      ▼
         ┌─── Fix analysis mini-session ───┐
         │  Spawns a short-lived session   │
         │  with the task output, diff,    │
         │  violation details, and rules   │
         │  as context.                    │
         │                                 │
         │  Produces: structured proposal  │
         │  with 1–3 concrete fix options  │
         └────────────┬────────────────────┘
                      │
                      ▼
         ┌─── Operator review ─────────────┐
         │  Fix proposal shown on all 7    │
         │  surfaces. Options listed with  │
         │  concrete action descriptions.  │
         │                                 │
         │  [Accept option 1]              │
         │  [Accept option 2]              │
         │  [Edit custom fix]              │
         │  [Abandon task]                 │
         └────────────┬────────────────────┘
                      │ (operator approves)
                      ▼
         ┌─── Retry task ──────────────────┐
         │  Task re-spawns with:           │
         │  - Original task spec           │
         │  - Accepted fix as prepended    │
         │    context/instruction          │
         │  - Max retries counter decremented│
         └────────────┬────────────────────┘
                      │
                      ▼
         ┌─── Verify ──────────────────────┐
         │  Same verifier + scanner suite  │
         │  as original task run.          │
         └────────────┬────────────────────┘
                      │
                   pass?
              ┌──────┴──────┐
             yes            no
              │              │
          Continue     Loop back to
          automaton    fix analysis
                       (max_retries check)
```

**Max retries:** Configurable per-automaton (`fix_loop_max_retries`, default: 3). When max retries is exceeded, the task is permanently blocked and the automaton transitions to `blocked` state. The operator must manually intervene.

**Fix analysis mini-session:**

The mini-session is a short `claude-code` session (or configured backend) with a constrained system prompt:
```
You are a code review assistant. Analyze this task failure and propose
specific, concrete fixes. Do not execute anything. Output only a structured
list of fix options. Each option: { title, description, concrete_changes: [...] }.
```

This is not a full session — it has no filesystem access, no tool calls. It's a single LLM inference pass to produce structured options.

**Proposal surfaces (all 7):**
- PWA: inline fix proposal panel in the blocked task/story row of the detail view
- Comm: message to configured channel with fix options as numbered list (`accept 1`, `accept 2`, `abandon`)
- MCP: `get_fix_proposal` tool returns proposal; `accept_fix` applies it and retries
- CLI: `datawatch automaton fix-propose <id> --task <task-id>` (interactive)
- REST: `GET /api/autonomous/{id}/tasks/{task-id}/fix-proposal`, `POST /api/autonomous/{id}/tasks/{task-id}/accept-fix`
- Mobile: notification with fix options; accept from notification or deep-link to PWA
- YAML: `autonomous.fix_loop_max_retries: 3`, `autonomous.fix_analysis_model`

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

## 8b. Automata Settings Tab

All Automata-related configuration is consolidated into a dedicated **Settings → Automata** tab in the PWA. This replaces the scattered `autonomous.*` entries currently found in General settings.

Every setting in this section follows the 7-surface rule: YAML + REST + MCP + CLI + Comm + PWA + Mobile companion.

### 8b.1 Settings tab layout

```
Settings
├── General
├── Sessions
├── Automata          ← new consolidated tab
│   ├── Execution
│   ├── Scan & Security
│   ├── Rules & Compliance
│   └── Display
├── Skills
├── Identity
├── Channels
└── Advanced
```

### 8b.2 Execution settings

| Setting | Description | Default |
|---------|-------------|---------|
| `autonomous.default_backend` | Default backend for new automata | `claude-code` |
| `autonomous.default_effort` | Default effort level | `high` |
| `autonomous.default_model` | Default model | session default |
| `autonomous.default_permission_mode` | Default permission mode | `default` |
| `autonomous.default_guided_mode` | Guided Mode on by default? | `false` |
| `autonomous.per_story_approval` | Require approval between stories | `false` |
| `autonomous.fix_loop_max_retries` | Max retry loops on rejection/block | `3` |
| `autonomous.fix_analysis_model` | Model for fix analysis mini-session | `autonomous.default_model` |
| `autonomous.story_alias.*` | Override display aliases per type | type defaults |
| `autonomous.task_alias.*` | Override task display aliases per type | type defaults |

### 8b.3 Scan & Security settings

| Setting | Description | Default |
|---------|-------------|---------|
| `autonomous.security_scan` | Enable security scanning | `true` |
| `autonomous.secrets_scan` | Always-on secrets scan (when `.git`) | `true` |
| `autonomous.secrets_scan_deep` | Use trufflehog for deep history scan | `false` |
| `autonomous.scan_categories` | Which scan categories to run | `sast,dependency,secrets` |
| `autonomous.scan_on_severity` | Minimum severity to report | `medium` |
| `autonomous.scan_block_on_severity` | Minimum severity to block | `critical` |
| `autonomous.scan_skills` | Additional scan skills to invoke | `[]` |

### 8b.4 Rules & Compliance settings

| Setting | Description | Default |
|---------|-------------|---------|
| `autonomous.rules_check` | Enable LLM rules compliance check | `true` |
| `autonomous.rules_check_model` | Model for rules check inference | `autonomous.default_model` |
| `autonomous.rules_check_backend` | Backend for rules check | `autonomous.default_backend` |
| `autonomous.rules_check_mode` | `warn` or `block` on violation | `warn` |
| `autonomous.rules_file` | Rules file to check against | `AGENT.md` → `CLAUDE.md` |
| `autonomous.rule_editor_model` | Model for LLM rule edit proposals | `rules_check_model` |
| `autonomous.rules_check_granularity` | `task` / `story` / `automaton` | `task` |

### 8b.5 Display settings

| Setting | Description | Default |
|---------|-------------|---------|
| `autonomous.show_planning_context` | Show Guided Mode pre-plan in detail | `true` |
| `autonomous.default_list_view` | `active` or `all` | `active` |
| `autonomous.card_progress_bar` | Show progress bar in list cards | `true` |
| `autonomous.scan_verdict_badges` | Show scan verdict badges in card | `true` |

### 8b.6 7-surface parity for all settings above

| Surface | Access |
|---------|--------|
| **YAML** | `~/.datawatch/config.yaml` under `autonomous:` key |
| **REST** | `GET/PUT /api/config` with `autonomous.*` keys |
| **MCP** | `config_get`/`config_set` with `autonomous.*` keys |
| **CLI** | `datawatch config get autonomous.*`, `datawatch config set autonomous.X Y` |
| **Comm** | `configure autonomous.X Y` via any configured channel |
| **PWA** | Settings → Automata (this tab) |
| **Mobile** | datawatch-app Settings → Automata (companion parity issue) |

---

## 9b. Templates — Full CRUD Interface (resolved)

Templates are a first-class entity, not a flag on a regular automaton. They live in a separate **Templates** tab in the Automata section.

### Template vs Automaton distinction

| | Automaton | Template |
|-|-----------|----------|
| Has stories/tasks | After planning | No — has a spec + template vars |
| Can be run | Yes | No — must be instantiated first |
| Appears in Automata list | Yes | No — lives in Templates tab |
| Created by | Launch wizard | Authoring or clone |

### Template CRUD operations

**Create:** Either:
1. "New Template" button in Templates tab → same intent field as Launch wizard, but saved as a template without planning/running. Template vars are defined in the spec using `{{variable_name}}` syntax.
2. **Clone to Template**: from any completed automaton's detail view, a "Save as Template" button creates a template from the automaton's spec + structure (strips story/task execution state, keeps the shape)

**Read:** Templates tab shows a grid/list with: name, description, variable count, last instantiated, created date. Click → template detail view.

**Update:** Edit template spec, title, description, template vars (name/default/required/help), tags.

**Delete:** Standard delete with confirmation (will not delete instantiated automata derived from it).

**Instantiate:** From template detail view: "Use this template →" → opens a pre-filled Launch wizard with the template's spec, template var fields rendered as form inputs, workspace required.

### Template var UX

Template vars use `{{variable_name}}` in the spec. The instantiation form auto-renders them:

```
Template: "Add {{feature_name}} to {{service_name}}"
Variables:
  feature_name: [_________________________]  ← required, no default
  service_name: [api-gateway             ]  ← default from template
```

### Template discovery

Templates are stored in the main datawatch store (not filesystem). They appear in the Launch wizard under "Choose from Templates →" for quick access. Templates can be tagged (e.g., `[software]`, `[onboarding]`, `[maintenance]`) and searched.

MCP: `list_templates`, `get_template`, `instantiate_template`, `create_template`, `update_template`, `delete_template`  
REST: full CRUD at `/api/autonomous/templates/*`  
CLI: `datawatch template list`, `datawatch template use <id>`  
Comm: `template list`, `template use <name>`  
Mobile: datawatch-app parity issue

---

## 10. Sprint Plan (implementation weeks — builds on v6.1 foundation)

This section is a planning skeleton. Actual week assignment happens when v6.1 ships.

All phases depend on v6.1 evals framework (Week 7 of unified platform sprint) being shipped first. Rules check and scan framework use the eval grader types.

### Phase 1 — Frontend redesign (no backend changes for Phase 1)
- Week A: Automata list view (Automata/Templates tabs, filter bar, status badge filters, compact cards, history toggle, checkboxes + multi-select)
- Week B: Card lifecycle strip (5-step ordered buttons, progress bar, live "running" position summary)
- Week C: Detail view (breadcrumb nav, stories/tasks tree with live status, timeline, session links, scan + rules verdict badges)
- Week D: Launch wizard (progressive disclosure, intent field, type auto-inference, field visibility rules, Guided Mode toggle, skills picker)

### Phase 2 — Templates
- Week E: Templates tab (CRUD list, template detail view, var rendering)
- Week F: Template instantiation flow (pre-filled wizard from template), clone-to-template from completed automaton, tags + search

### Phase 3 — Backend: Scan framework, secrets scanner, config tab
- Week G: `internal/autonomous/scan/` package (Scanner interface with `secrets` category, language auto-detect, result aggregation into GuardrailVerdict); Settings → Automata tab in PWA (Section 8b)
- Week H: Built-in scanner implementations: SAST (Go/Python/JS/TS/Rust), dependency (govulncheck/npm-audit/pip-audit/cargo-audit), severity mapping; `gitleaks` secrets scanner always-on for `.git` repos
- Week I: Rules check grader (llm_rubric against AGENT.md, `rules_check_pending` blocked sub-state, operator override prompt on all 7 surfaces)
- Week J: Per-task/story/PRD rules_check + security_scan config fields, per-level override propagation

### Phase 3b — LLM fix loop + rule editor
- Week J2: Fix analysis mini-session (spawns short-lived inference session on reject/block, produces structured fix proposal, surfaces on all 7 surfaces); fix loop retry + verify cycle with max retries
- Week J3: LLM rule editor (violation → LLM proposes 2–3 AGENT.md diffs → operator approves or edits directly → LLM inserts at correct location → rules re-check)

### Phase 4 — Type extensibility, Guided Mode, Skills wiring
- Week K: Automaton type field (software/research/operational/personal + plugin-extensible registry via `internal/autonomous/typeregistry/`), type-sensitive display aliases (Stories vs Phases vs Workstreams), planning prompt variants, verifier rubric variants
- Week L: Guided Mode in automaton launch (Observe→Orient→Decide pre-planning output surfaced in detail view before planning begins), skills assignment wiring

### Phase 5 — 7-surface parity + datawatch-app
- Week M: MCP tools audit (`autonomous_create` / `autonomous_plan` / `autonomous_run` / `autonomous_cancel` / `autonomous_list` / `autonomous_status` — all accept new fields: type, guided_mode, rules_check, scan config, fix_loop_max_retries)
- Week N: CLI redesign (`datawatch automaton launch`, `datawatch automaton list`, `datawatch automaton status`, `datawatch scan`, `datawatch rules propose-edit`) — backward-compat aliases for old `autonomous` subcommand
- Week O: Comm channel command update (`automaton launch`, `automaton status <id>`, `automaton cancel <id>`, `scan <dir>`, `accept fix <n>`)
- Week P: datawatch-app 3 issues filed (Phone / Wear OS / Android Auto per Section 11) + quick mobile wins (status card, push notifications)

### Phase 6 — Release
- Week Q: Integration, smoke tests (`scripts/release-smoke.sh`), release notes for v6.2.0

---

## 11. datawatch-app Alignment Issue (to file)

**Three issues** should be filed against the `datawatch-app` repo — one per platform surface. The datawatch-app team owns the platform-specific UX and design details; these issues define the data surface and intent that must be supported, aligned with the server's redesigned API.

---

### Issue 1: Android Phone — Automata companion parity

**Title:** `[BL221] Automata redesign — Android phone companion parity`

**Context:** The server's autonomous task system is being redesigned in v6.2.0 under the "Automata" name. The PWA gets a full redesign. The Android app should align with the new data model, terminology, and interaction patterns. datawatch-app owns the platform-specific design; this issue defines what the server exposes and what the app must support.

**Body checklist:**
- [ ] Rename "Autonomous Tasks" → "Automata" throughout the app
- [ ] Compact automaton card: status color, type badge (`software`/`research`/`operational`/`personal`/custom), progress bar (tasks completed/total)
- [ ] Lifecycle step strip: 5 steps (Plan → Review → Approve → Run → Done), current step highlighted, color-coded
- [ ] Active vs History list toggle (default: active only)
- [ ] Filter bar: status badge filters, type filter, search by title/intent
- [ ] Multi-select: checkboxes, select-all; contextual batch actions (run/approve/cancel/archive/delete — shown only when valid for all selected)
- [ ] Detail view: breadcrumb navigation for child automata
- [ ] Detail view: stories/tasks tree with live status + session links
- [ ] Detail view: guardrail verdicts (security, rules, release-readiness) as color-coded badges
- [ ] Detail view: decisions/timeline collapsible section
- [ ] Detail view: scan results (finding count per category, block/warn/pass)
- [ ] "Launch Automaton" flow: intent text field → type inference/override → execution config → Guided Mode toggle → launch
- [ ] Launch: skills assignment picker
- [ ] Launch: security scan + rules check toggles
- [ ] Launch: choose from templates
- [ ] Templates tab: list view, template detail, instantiation flow (fill template vars → launch)
- [ ] Fix proposal UI: when a task is blocked, show fix proposal options with accept/abandon actions
- [ ] Rules violation prompt: show violation details + override / fix / edit rule options
- [ ] Real-time WebSocket updates for in-progress automaton progress (progress bar, current story/task)
- [ ] Push notifications: automaton completed / blocked / fix proposal ready / rules violation
- [ ] Settings → Automata: expose Execution + Scan & Security + Rules & Compliance + Display settings (aligned with Settings → Automata PWA tab)

---

### Issue 2: Wear OS — Automata status glanceable interface

**Title:** `[BL221] Automata redesign — Wear OS companion`

**Context:** Wear OS should provide a **glanceable, low-interaction** view of automata status. The intent is "at a glance, is anything blocked or done?" with minimal action capability. Platform-specific design is fully owned by the datawatch-app team; this issue defines the data surface the server exposes.

**Data the server exposes (available via WebSocket + REST):**
- List of active automata: title, status, type, progress (N/total tasks, %)
- Current story/task label when `running`
- Blocked automata: blocking reason summary (1-liner)
- Guardrail verdict: pass/warn/block per automaton

**Suggested Wear OS capability targets** _(datawatch-app decides final UX)_:
- **Complication**: active automata count + blocked count. Glanceable on watch face.
- **Tile**: scrollable list of active automata — title, progress bar, status chip. Refresh on WebSocket push.
- **App screen**: compact card list with status color and progress %. Tap → status detail (title, current task, verdicts).
- **Quick actions** (minimal — tap-to-confirm): "Cancel" on a running automaton; "Approve" when status is `needs_review` (critical path action — operator may be away from desktop).
- **Notification**: vibrate + glanceable notification when: automaton blocked, fix proposal ready, rules violation needs decision, automaton completed.

**Explicitly out of scope for Wear OS:** Launch wizard, template management, rule editing, fix proposal review (redirect to phone).

---

### Issue 3: Android Auto — Automata voice-first interface

**Title:** `[BL221] Automata redesign — Android Auto voice interface`

**Context:** Android Auto should expose automata status through a **voice-first, safety-conscious** interface. No complex interactions while driving. The goal: the operator can hear a status summary and give simple spoken approvals or cancellations without looking at a screen. Platform-specific design is fully owned by the datawatch-app team.

**Data surface:**
- Active automata count by status (running, blocked, needs_review, approved)
- Per-automaton: title, status, progress summary (e.g., "Story 2 of 5, 38% complete")
- Pending approvals: automata in `needs_review` state with plan title
- Blocked automata: blocking reason as spoken summary

**Suggested Android Auto capability targets** _(datawatch-app decides final UX)_:
- **Dashboard card** (static): "N automata active · M blocked" — visible when parked
- **Voice command**: "Hey [assistant], what's the status of my automata?" → reads out active automata summary
- **Voice command**: "Approve [automaton name]" → approves `needs_review` automaton with 5-second confirmation window ("Approving [name] in 5 seconds — say Cancel to abort")
- **Voice command**: "Cancel [automaton name]" → same confirmation window
- **Notification read-aloud**: when automaton completes or is blocked → read title + outcome via audio notification
- **Parked-only UI**: when vehicle is parked, allow viewing blocked automata detail and approving fix proposals (minimal tap interface, large touch targets)

**Explicitly out of scope for Android Auto (safety):** Launch wizard, template management, rule editing, fix proposal options (too complex — redirect to phone when parked), any text input requiring spelling.

**Intent format for voice → server:** The server exposes `POST /api/autonomous/{id}/approve` and `POST /api/autonomous/{id}/cancel` REST endpoints already. Android Auto would call these through the app's API layer after voice confirmation. No new server changes needed for basic voice actions.

---

## 12. Design Complete

All Q1–Q21 resolved. See Section 0 table.

**Implementation prerequisites before Phase 3 (scan framework):**
- **BL228** _(new, to be filed)_: Add security scanner tools to language layer Dockerfiles — `govulncheck` (Go), `bandit`+`pip-audit` (Python), `eslint-plugin-security` (Node), `cargo-audit` (Rust), `brakeman`+`bundler-audit` (Ruby)
- **v6.1 evals framework** (unified platform Week 7): rules check depends on the `llm_rubric` grader type being available

**Implementation prerequisites for Phase 3b (fix loop + rule editor):**
- Phase 3 (scan framework) must be complete first — fix loop uses scanner verdicts as the trigger condition
- Fix analysis mini-session uses the skill executor — skills infrastructure (v6.1) must be available

---

## 12b. Identity Automaton — Built-in Personal Entry Point

The operator's `identity.yaml` is the Telos layer — the context that shapes every session via the L0 wake-up stack. Today it is a file the operator edits by hand. The Automata redesign makes it possible to build and maintain `identity.yaml` through a **first-class personal automaton**: an interview-style workflow that asks the operator structured questions and writes the answers into the file.

This section covers the robot icon entry point in the PWA header, the identity automaton lookup, the create flow, the `interview` skill type, and the REST convenience endpoints.

> **Prerequisite alignment:** The identity backend layer (`GET/PUT /api/identity`, `identity.yaml`, wake-up L0 injection) is designed in `docs/plans/2026-05-02-unified-ai-platform-design.md` Week 4. This section covers the *Automata-layer entry point* on top of that backend — the PWA workflow for building and managing `identity.yaml` through an automaton run.

---

### 12b.1 Robot Icon in PWA Header

A robot icon (`&#129302;` — 🤖) appears in the `<header>` bar between the search button and the status dot. It is shown/hidden by the same visibility gate as the Autonomous nav button: only visible when `GET /api/autonomous/config` returns `enabled: true`.

```
[←]  Datawatch   [🔍] [🤖]  ●
```

On tap/click: `openIdentityWorkflow()` fires — no navigation yet; first checks for an existing identity automaton.

---

### 12b.2 Identity Automaton Lookup

`openIdentityWorkflow()` algorithm:

1. Calls `GET /api/autonomous/prds` with a flag that includes all statuses (completed, archived, cancelled — not just active). Backend needs an `?all=true` or equivalent parameter to bypass the default active-only filter.
2. Searches the response array for any PRD where `prd.spec.toLowerCase().includes('identity')`. The canonical intent is `"identity, projects, values and goals"` but any spec containing `"identity"` qualifies — the operator may have phrased it differently.
3. **If found**: navigate to the Automata tab (`navigate('autonomous')`), then call `scrollToAutomaton(prd.id)` (the BL221 rename of `scrollToPRD`). The card flashes the accent highlight. The operator can view, edit, and rerun it from the redesigned card UI.
4. **If not found**: open the Identity Setup modal (§12b.3).

The lookup checks ALL states including `completed` and `archived` — if the operator has ever run an identity interview, they should return to that same automaton rather than create a duplicate.

---

### 12b.3 Identity Setup Modal

A simplified creation modal pre-configured for the identity workflow. The operator only controls the parts that vary per-run:

| Field | Default value | Editable? | Notes |
|-------|---------------|-----------|-------|
| Intent | `identity, projects, values and goals` | No — read-only display | Shown so operator knows what will be created |
| Type | `personal` | No | Hidden; set automatically |
| Skill | `interview-identity` | No | Hidden; set automatically |
| LLM / Backend | (model picker — same as session creation) | **Yes** | Full model selector |
| Model | (model name) | **Yes** | |
| Effort | `normal` | **Yes** | |
| File path | `~/.datawatch/identity.yaml` | **Yes** | Full path field — editable, not a directory picker |

**"File path" vs "Project directory":** Interview automata target a specific output file rather than a project checkout. The wizard shows a **file path** text input (with filesystem browse button) instead of the usual `project_dir` picker. On launch, `project_dir` is inferred from the file's parent directory unless the operator overrides it.

**"Launch" action:** Creates a PRD with `spec: "identity, projects, values and goals"`, `type: personal`, `skill: interview-identity`, `file_path: <path>`, then navigates to the Automata tab and opens the new automaton's detail view.

---

### 12b.4 `interview` Skill Type

The `interview` skill type drives a structured multi-phase conversation rather than code execution. Interview skills are built-in manifests under `~/.datawatch/skills/` and run as `personal`-type sessions with Guided Mode ON.

**New manifest fields:**

```yaml
name: interview-identity
type: interview          # new skill type (alongside: code, pipeline, scan)
description: PAI-style interview to build and maintain identity.yaml

phases:
  - id: role
    prompt: |
      Ask the operator about their professional role, primary domain, and how
      they spend most of their working time. One focused question at a time.
    output_key: role
  - id: goals
    prompt: |
      Ask about north-star goals — the 3–5 outcomes that matter most over the
      next 6–12 months, professionally and personally.
    output_key: goals
  - id: values
    prompt: |
      Ask about values — the principles that guide decisions when things are
      ambiguous or when tradeoffs are hard.
    output_key: values
  - id: current_focus
    prompt: |
      Ask what the operator is actively working on right now — projects, tasks,
      contexts that should be top-of-mind in every AI session.
    output_key: current_focus
  - id: context_notes
    prompt: |
      Ask for any additional context the operator wants the AI to always know:
      constraints, preferences, recurring frustrations, communication style.
    output_key: context_notes

output_file: "{{file_path}}"    # resolved from PRD.FilePath at runtime
output_format: yaml             # writes structured YAML to the output file
update_mode: merge              # if file already exists, read current values as
                                # defaults and present them to the operator for
                                # confirmation or revision rather than starting over
```

**Execution model:**
- The interview skill runs as a `personal`-type session with Guided Mode ON
- The LLM progresses through phases sequentially, asking one question per exchange and waiting for operator response
- Each phase's output is captured as a structured value
- On completion, the skill executor renders the collected values as `identity.yaml` and writes to `PRD.FilePath`
- **Update flow:** If `identity.yaml` already exists and `update_mode: merge` is set, the skill reads the current file first and shows the operator their existing values before asking if they want to revise them

---

### 12b.5 REST Endpoints — GET and SET

These are convenience endpoints for programmatic read/write. They are **not the configure path** — building or updating identity via an LLM-guided interview always goes through an automaton run.

`GET /api/identity`
- Returns the raw content of `identity.yaml` at the configured path
- Response: `{"content": "...yaml string...", "path": "/home/user/.datawatch/identity.yaml"}`
- Returns `{"content": null}` with HTTP 204 if the file does not exist yet

`PUT /api/identity`
- Writes content to `identity.yaml`
- Body: `{"content": "...yaml string..."}`
- Validates YAML syntax before writing; returns 400 with error detail on invalid YAML
- Creates the file (and parent directories) if they do not exist

---

### 12b.6 PRD Struct Extension

Add `FilePath` to the `PRD` struct alongside `ProjectDir`:

```go
// FilePath is the target output file for interview-type automata that
// produce a specific file rather than working in a project directory.
// When set, worker sessions receive it as the target; ProjectDir is
// inferred as the file's parent directory unless also set explicitly.
FilePath string `json:"file_path,omitempty"`
```

The Launch Automaton wizard (§7) shows the `FilePath` input instead of `ProjectDir` when the selected skill manifest has `type: interview`. Both can coexist (interview skill writing a file inside a project checkout).

---

### 12b.7 7-Surface Parity

| Surface | Implementation |
|---------|---------------|
| **YAML** | `identity.file_path` (default: `~/.datawatch/identity.yaml`) |
| **REST** | `GET /api/identity` (read file), `PUT /api/identity` (write file), `GET /api/autonomous/prds?all=true` (lookup), `POST /api/autonomous/prds` (create with `type:personal, skill:interview-identity`) |
| **MCP** | `get_identity` (returns file content + path), `set_identity` (writes file), `run_identity_interview` (creates automaton, returns PRD id) |
| **CLI** | `datawatch identity show`, `datawatch identity set --file <path>`, `datawatch identity configure` (creates automaton and prints id) |
| **Comm channel** | `identity` (shows current `identity.yaml` content), `identity configure` (creates automaton, posts id) |
| **PWA** | Robot icon in header → lookup flow → open existing card or show Identity Setup modal |
| **Mobile** | File datawatch-app parity issue: view/edit `identity.yaml` content; button to trigger re-interview (creates automaton) |

---

### 12b.8 Built-in Skill — `interview-identity`

`~/.datawatch/skills/interview-identity/skill.yaml` is a **built-in skill**: embedded in the binary and extracted to the skills directory on first run if not already present (same pattern as built-in templates). Operators can override it by placing their own `skill.yaml` at that path.

The skill follows the PAI Telos interview structure (role → goals → values → current_focus → context_notes). The phase prompts are designed to draw out specific, actionable answers rather than generic platitudes — each phase prompt instructs the LLM to ask follow-up questions until the operator's answer is concrete enough to be useful as L0 context.

---

## 13. Related Files

**Backend (new/modified):**
- `internal/autonomous/models.go` — add `Type`, `GuidedMode`, `RulesCheck`, `RulesCheckMode`, `ScanConfig`, `FixLoopMaxRetries`, `FilePath` fields to PRD/Story/Task; rename `StatusDecomposing` → `StatusPlanning`
- `internal/server/identity.go` — **new**: `GET /api/identity`, `PUT /api/identity` handlers; reads/writes `identity.yaml` at configured path; YAML validation on PUT
- `internal/autonomous/scan/` — new package: scanner framework (Scanner interface with `secrets` category), built-in scanners, intent scanners
- `internal/autonomous/scan/builtin/secrets.go` — `gitleaks` wrapper, always-on for `.git` repos, full history scan
- `internal/autonomous/security.go` — **replace** with `scan/builtin/` implementations; keep as compatibility shim
- `internal/autonomous/executor.go` — wire rules_check grader + scan framework into verification flow; fix loop trigger logic
- `internal/autonomous/fix_loop.go` — **new**: fix analysis mini-session spawn, proposal struct, retry cycle, max-retries enforcement
- `internal/autonomous/rule_editor.go` — **new**: LLM rule diff proposal, AGENT.md insertion logic
- `internal/autonomous/api.go` — new endpoints: scan results, templates CRUD, display aliases config, fix proposals, rule editor
- `internal/autonomous/manager.go` — wire rules_check as post-task guardrail with operator block prompt; `planning` status code
- `internal/autonomous/templates.go` — **new**: Template CRUD store + instantiation
- `internal/autonomous/typeregistry/` — **new**: type registry, built-in types, plugin-loaded types

**Frontend (full redesign):**
- `internal/server/web/app.js` — `renderPRDRow`, `renderPRDActions`, `renderAutonomousView`, `openPRDCreateModal` replaced with Automata redesign; Settings → Automata tab
- `internal/server/web/app.css` — new card classes, lifecycle strip, breadcrumb, detail view, tabs, fix proposal panel, rule editor panel

**Docker (language layer updates — BL228):**
- `docker/dockerfiles/Dockerfile.lang-go` — add `govulncheck`, `gitleaks`
- `docker/dockerfiles/Dockerfile.lang-python` — add `bandit`, `pip-audit`
- `docker/dockerfiles/Dockerfile.lang-node` — add `eslint-plugin-security` to global npm install
- `docker/dockerfiles/Dockerfile.lang-rust` — add `cargo-audit`
- `docker/dockerfiles/Dockerfile.lang-ruby` — add `brakeman`, `bundler-audit`
- `docker/dockerfiles/Dockerfile.worker` — add `gitleaks` to base worker image (shared across all language layers)

**Skills (new):**
- `~/.datawatch/skills/scan-trufflehog/skill.yaml` — trufflehog deep secrets scan skill
- `~/.datawatch/skills/scan-semgrep/skill.yaml` — Semgrep cross-language SAST skill
- `~/.datawatch/skills/interview-identity/skill.yaml` — built-in identity interview skill (embedded in binary, extracted on first run)

**Documentation:**
- `docs/howto/autonomous-planning.md` — full rewrite for Automata terminology + wizard
- `docs/howto/autonomous-review-approve.md` — rewrite for lifecycle strip UI + fix loop
- `docs/howto/automata-secrets-scan.md` — **new**: secrets scanner usage, git history remediation
- `docs/howto/automata-rules-check.md` — **new**: rules check, violation workflow, rule editor
- `docs/api/autonomous.md` — add new fields, new endpoints, planning status code

**Cross-references:**
- `docs/plans/2026-05-02-unified-ai-platform-design.md` — Week 5 (session types / Guided Mode), Week 7 (evals framework for rules check)
- `docs/plans/README.md` — BL221 backlog entry, BL228 prerequisites

---

## 14. Implementation Plans

All phases build on the v6.1 foundation (skills, evals, identity layer). Each phase has a **Phase Gate** — all checked items must pass before starting the next phase.

**Prerequisites before any BL221 implementation begins:**
- [ ] v6.1 shipped: skills layer, evals framework (llm_rubric grader), identity layer
- [ ] BL228 complete: govulncheck, bandit, pip-audit, eslint-plugin-security, cargo-audit, brakeman, bundler-audit installed in language layer Dockerfiles + gitleaks in worker base image
- [ ] `internal/autonomous/models.go` status constant `PRDDecomposing` → `PRDPlanning` (read old value for back-compat)

---

### Phase 1 — Frontend Redesign (no backend changes required)

**Target:** v6.2.0 (patch series during implementation)  
**Prerequisites:** None beyond v6.1 shipping. All data already exists in the backend.

#### Week A — Automata List View

**Files to modify:**
- `internal/server/web/app.js` — `renderAutonomousView()` (line ~8403)
- `internal/server/web/app.css` — new tab, card, filter bar classes

**Tasks:**
- [ ] Rename section in nav from "Autonomous" → "Automata"
- [ ] Replace flat list with 2-tab layout: `Automata` | `Templates`
- [ ] Add header bar: `? How-to`, `⊞ Filter`, `History` toggle, `⚡ Launch Automaton`
- [ ] Implement filter bar (hidden by default, toggle reveals): search input, status badge buttons (draft/planning/review/approved/running/blocked), type badges (software/research/operational/personal)
- [ ] Implement History toggle: active-by-default (shows draft/planning/needs_review/approved/running/blocked/revisions_asked); History ON adds completed/rejected/cancelled/archived
- [ ] Replace `renderPRDRow()` compact card with new layout: checkbox + type badge + title + status pill + progress bar + progress text + action strip
- [ ] Add `renderProgressBar(prd)` helper: counts completed tasks / total tasks → percentage + bar HTML
- [ ] Add `renderCurrentPosition(prd)` helper: for status=running, traverse stories/tasks to find in_progress task → "Story N: title · Task N: status"
- [ ] Add select-all checkbox in filter toolbar; update toolbar to show `N selected` count when ≥1 selected
- [ ] Add status badge CSS classes (per status: draft=grey, planning=blue, needs_review=amber, approved=green outline, running=green, blocked=red, completed=grey-dim)
- [ ] Update `switchSettingsTab` / equivalent tab switching for Automata/Templates sub-tabs
- [ ] Wire `History` toggle to re-filter list in place (no server round-trip; all PRDs already loaded or paginated)
- [ ] Add localization keys: `automata_tab_automata`, `automata_tab_templates`, `automata_header_howto`, `automata_btn_launch`, `automata_filter_*`, `automata_status_*` → all 5 locale bundles

**Acceptance checks:**
- [ ] Automata tab shows only active PRDs by default; History toggle reveals completed/archived
- [ ] Filter bar hidden on load; revealed by ⊞ Filter button; filter state persists in localStorage
- [ ] Status badges act as multi-select filters; multiple can be active simultaneously
- [ ] Compact cards show: checkbox, type badge, title, status pill, progress bar, current position (when running)
- [ ] Select-all checkbox selects/deselects all visible cards
- [ ] Localization keys present in all 5 locale files

---

#### Week B — Card Lifecycle Strip

**Files to modify:**
- `internal/server/web/app.js` — new `renderLifecycleStrip(prd)` function replacing `renderPRDActions()`
- `internal/server/web/style.css` — lifecycle strip classes

**Tasks:**
- [ ] Replace `renderPRDActions()` (line ~5293) with `renderLifecycleStrip(prd)` implementing the 5-step ordered sequence: Plan → Review → Approve → Run → Done
- [ ] Color logic: current step = blue; completed step = green; future step = grey/disabled
- [ ] Button state:
  - `Plan` button: active if `status === 'draft'`; clickable triggers planning; green if past draft
  - `Review` button: active if `status === 'needs_review'`
  - `Approve` button: active if `status === 'needs_review'`; shows Reject/Revise sub-menu on long-press or hover
  - `Run` button: active if `status === 'approved'`
  - `Done` indicator: green when `status === 'completed'`
- [ ] Overflow menu `⋯` for: Edit, Delete, Clone to Template, Archive (only when completed/rejected/cancelled)
- [ ] Backward navigation: if `status === 'revisions_asked'`, re-activate Plan button (re-plan path)
- [ ] Add `renderBatchActionBar(selectedIds)`: appears above list when ≥1 selected; contextual actions based on intersection of valid actions across all selected IDs
  - Compute valid batch actions: `approved`-only → Run all; `needs_review`-only → Approve all; any → Delete; non-running → Cancel all; completed/rejected/cancelled → Archive all
- [ ] Add localization keys for all button labels + batch action labels → all 5 locale bundles

**Acceptance checks:**
- [ ] Lifecycle strip always visible in card; exactly 5 steps rendered
- [ ] Current step has blue highlight; completed steps have green check; future steps are grey
- [ ] Only the current-step button is clickable; others are visually disabled
- [ ] Overflow `⋯` shows Edit/Delete/Clone always; Archive only for terminal states
- [ ] Batch action bar appears on first checkbox selection; disappears when selection cleared
- [ ] Batch actions show only those valid for ALL selected automata; mixed selection shows only Delete

---

#### Week C — Detail View

**Files to modify:**
- `internal/server/web/app.js` — new `renderPRDDetailView(prdId)` function
- `internal/server/web/style.css` — breadcrumb, detail view, stories/tasks tree classes

**Tasks:**
- [ ] Add `renderPRDDetailView(prdId)` that replaces list view with full detail view (push to `history.state` for back button)
- [ ] Breadcrumb navigation: `← Automata > {root title} > {story} > {child PRD}` — each segment clickable
- [ ] Detail view header: type badge, title, status pill, id, backend, effort, model, created, project dir, depth
- [ ] Full lifecycle strip (expanded): 5 steps + sub-step indicators; current step with action buttons
- [ ] Progress section: `Story N of X · Task N of X · NN%` + progress bar
- [ ] Stories/tasks tree: collapsible stories, tasks inside each story with status icon + session link when `session_id` set
  - Task row: status icon (✓/⚡/○/✗) + task title + status label + session link `→ session`
  - Child PRD rows: `↳ [child PRD: title]` — clickable (pushes child detail view, updates breadcrumb)
- [ ] Guardrail verdicts row: `security: pass ✓`, `rules: warn ⚠`, `release-readiness: pass ✓` — color-coded
- [ ] Decisions timeline: collapsible section; shows decision log entries with timestamp, LLM call info
- [ ] Fix `scrollToPRD()` — replace with `renderPRDDetailView()` navigation for all child PRD links
- [ ] Real-time updates: WebSocket `prd_update` events update only the affected card's progress/status in list view; update stories/tasks tree in place in detail view (no full re-render)
- [ ] Add scan results section in detail view (once backend Phase 3 ships): finding count per category, verdict badge per category
- [ ] Add localization keys for detail view labels → all 5 locale bundles

**Acceptance checks:**
- [ ] Clicking a card navigates to detail view; browser back returns to list
- [ ] Breadcrumb renders correctly for depth-0, depth-1, depth-2 PRDs
- [ ] Child PRD links in task rows push child detail view and update breadcrumb
- [ ] Stories/tasks tree shows real-time status updates without full page reload
- [ ] Session links in task rows navigate to Sessions tab filtered to that session
- [ ] Guardrail verdict badges present and color-coded (pass=green, warn=amber, block=red)

---

#### Week D — Launch Automaton Wizard

**Files to modify:**
- `internal/server/web/app.js` — replace `openPRDCreateModal()` (line ~5441) with `openLaunchAutomatonWizard()`
- `internal/server/web/style.css` — wizard styles

**Tasks:**
- [ ] Replace "New PRD" modal with `openLaunchAutomatonWizard()`: single vertical stream with progressive disclosure
- [ ] Intent field: large textarea with placeholder examples (software/research/operational)
- [ ] Voice input button (🎤) — if available: `navigator.mediaDevices.getUserMedia` → speech-to-text
- [ ] "Inferred" section: type badge (auto-detected with annotation), workspace picker — both editable
- [ ] Type auto-inference: keyword scan on intent text + workspace dir extension scan (see Section 7.4)
- [ ] "Execution" section: backend selector, effort selector (shown/hidden per backend), model selector, permission mode (claude-code only)
- [ ] "Advanced" section (collapsed): Guided Mode toggle, skills picker (shown only if `~/.datawatch/skills/` non-empty), per-story approval toggle, guardrails expander, security scan toggle (pre-checked), rules check toggle (pre-checked)
- [ ] "Or use a template" link: opens Templates tab with pre-selection UX
- [ ] Cancel + Launch buttons; Launch creates automaton immediately and navigates to detail view
- [ ] Button label: "⚡ Launch Automaton" everywhere (remove "New PRD" label)
- [ ] Add localization keys for all wizard labels → all 5 locale bundles

**Acceptance checks:**
- [ ] Wizard opens with intent field focused
- [ ] Type auto-infers from intent text + workspace; shows `← auto-detected` annotation
- [ ] Advanced section collapsed by default; expands on click
- [ ] Skills picker absent when no skills installed; present when skills directory non-empty
- [ ] Launch creates automaton, transitions to detail view watching planning in real time
- [ ] Guided Mode toggle stored per-session; default from `autonomous.default_guided_mode` config

**Phase 1 Gate:**
- [ ] All 4 weeks complete (A–D)
- [ ] No regression in existing autonomous view (existing PRDs load, actions work)
- [ ] `scripts/release-smoke.sh` passes
- [ ] Localization keys present in all 5 locale files
- [ ] datawatch-app Issue 1 (Phone) filed with link to this phase's completed PWA as reference

---

### Phase 2 — Templates

**Target:** v6.2.0 (continued patch series)  
**Prerequisites:** Phase 1 complete

#### Week E — Templates Tab

**Files to create/modify:**
- `internal/autonomous/templates.go` — **new**: Template struct, CRUD store
- `internal/autonomous/api.go` — add template endpoints
- `internal/server/web/app.js` — `renderTemplatesView()`
- `internal/server/web/style.css` — template card classes

**Backend tasks:**
- [ ] Add `Template` struct to `internal/autonomous/models.go`:
  ```go
  type Template struct {
      ID          string            `json:"id"`
      Title       string            `json:"title"`
      Description string            `json:"description"`
      Spec        string            `json:"spec"`
      Type        string            `json:"type"`
      Tags        []string          `json:"tags"`
      Vars        []TemplateVar     `json:"vars"`
      IsBuiltin   bool              `json:"is_builtin"`
      CreatedAt   time.Time         `json:"created_at"`
      UpdatedAt   time.Time         `json:"updated_at"`
      LastUsedAt  *time.Time        `json:"last_used_at"`
      UseCount    int               `json:"use_count"`
  }
  type TemplateVar struct {
      Name        string `json:"name"`
      Default     string `json:"default"`
      Required    bool   `json:"required"`
      Description string `json:"description"`
  }
  ```
- [ ] `internal/autonomous/templates.go`: `TemplateStore` with `Create`, `Get`, `List`, `Update`, `Delete`, `ExtractVars(spec)` (parses `{{var_name}}` patterns)
- [ ] Store templates in existing autonomous SQLite table or new `prd_templates` table (migration)
- [ ] REST endpoints (7-surface parity):
  - `GET /api/autonomous/templates` — list all templates
  - `POST /api/autonomous/templates` — create template
  - `GET /api/autonomous/templates/{id}` — get template
  - `PUT /api/autonomous/templates/{id}` — update template
  - `DELETE /api/autonomous/templates/{id}` — delete template
  - `POST /api/autonomous/templates/{id}/instantiate` — create automaton from template with var substitution

**Frontend tasks:**
- [ ] `renderTemplatesView()`: grid/list of template cards — name, description, type badge, var count, last used, use count
- [ ] Template detail view: spec display with `{{var_name}}` highlighted, var list with defaults
- [ ] "New Template" button → opens template editor (title, spec with var syntax help, description, tags, type)
- [ ] Template editor shows auto-extracted var list as user types (live preview of `{{...}}` matches)
- [ ] Delete button with confirmation (won't delete automata derived from this template)
- [ ] MCP + CLI + Comm parity (see Section 9b for full list)
- [ ] Add localization keys → all 5 locale bundles

**Acceptance checks:**
- [ ] Template CRUD all working: create, read, update, delete
- [ ] Template vars extracted correctly from spec using `{{var_name}}` pattern
- [ ] Templates tab shows all templates with search and type filter
- [ ] Template detail shows spec with highlighted vars

---

#### Week F — Template Instantiation + Clone to Template

**Tasks:**
- [ ] "Use this template →" button in template detail view → opens Launch wizard pre-filled with template spec
- [ ] Wizard renders template vars as form inputs above the spec field (required vars get `*`)
- [ ] Var substitution: on launch, replace `{{var_name}}` in spec with submitted values before creating automaton
- [ ] "Save as Template" in completed automaton detail view → opens template editor pre-filled with automaton's spec (strips execution state)
- [ ] Clone preserves type, tags, but sets `use_count: 0`, `last_used_at: nil`
- [ ] Templates discoverable in Launch wizard: "📄 Choose from Templates →" link
- [ ] Tag search in Templates tab: click a tag filters to that tag
- [ ] Add localization keys → all 5 locale bundles

**Acceptance checks:**
- [ ] Instantiate from template substitutes all vars correctly
- [ ] Required var missing → wizard shows validation error, blocks launch
- [ ] Clone to template from completed automaton creates template with correct spec
- [ ] Template use count increments on each instantiation
- [ ] Tags are searchable in Templates tab

**Phase 2 Gate:**
- [ ] Templates CRUD fully functional end-to-end
- [ ] Instantiation flow creates automaton correctly with var substitution
- [ ] Clone-to-template works from any completed automaton
- [ ] MCP tools `list_templates`, `get_template`, `instantiate_template`, `create_template`, `update_template`, `delete_template` all work
- [ ] CLI `datawatch template list`, `datawatch template use <id>` work
- [ ] `scripts/release-smoke.sh` passes

---

### Phase 3 — Backend: Scan Framework, Secrets Scanner, Config Tab

**Target:** v6.2.0 (continued)  
**Prerequisites:** Phase 1 complete; BL228 (language layer Docker tools) complete; v6.1 evals framework available

#### Week G — Scan Package + Automata Settings Tab

**Files to create:**
- `internal/autonomous/scan/framework.go`
- `internal/autonomous/scan/runner.go`
- `internal/server/web/app.js` — Automata Settings tab in `renderSettingsView()`

**Backend tasks:**
- [ ] Create `internal/autonomous/scan/` package
- [ ] Define `Scanner` interface in `framework.go`:
  ```go
  type Scanner interface {
      Name() string
      Category() string // sast | dependency | lint | secrets | intent
      Languages() []string
      Run(ctx context.Context, req ScanRequest) (ScanResult, error)
  }
  ```
- [ ] Define `ScanRequest`, `ScanResult`, `Finding` structs
- [ ] Language detection in `framework.go`: inspect `FilesTouched` extensions → select applicable scanners
- [ ] `Registry`: register/discover scanners; built-in scanners registered at init
- [ ] `runner.go`: `RunAll(ctx, req, category_filter)` → `[]ScanResult`; aggregate → single `GuardrailVerdict`
- [ ] Severity mapping: `critical` → block; `high/medium` → warn; `low/info` → pass
- [ ] Wire scan runner into `internal/autonomous/executor.go` post-task hook (replaces old `SecurityScan()`)
- [ ] Keep `internal/autonomous/security.go` as a compatibility shim calling the new scan framework
- [ ] Add `ScanResults` field to `PRD`, `Story`, `Task` structs
- [ ] REST: `GET /api/autonomous/{id}/scan-results`, `POST /api/scan` (standalone scan)
- [ ] MCP: `run_scan` tool
- [ ] Config: add all `autonomous.scan.*` config keys from Section 8b.3 (7-surface parity)

**Frontend tasks (Automata Settings tab):**
- [ ] Add "Automata" tab to settings tab bar in `renderSettingsView()`
- [ ] New tab shows 4 collapsible sections: Execution, Scan & Security, Rules & Compliance, Display
- [ ] Wire all settings inputs to `saveGeneralField()` with the `autonomous.*` config keys from Section 8b
- [ ] Add localization keys for all Automata settings labels → all 5 locale bundles

**Acceptance checks:**
- [ ] `internal/autonomous/scan/` package compiles with no errors
- [ ] `Scanner` interface is satisfied by a test stub (unit test)
- [ ] Language detection returns correct scanner set for `.go`, `.py`, `.ts`, `.rs`, `.rb` extensions
- [ ] `POST /api/scan` endpoint returns a valid `ScanResult` JSON
- [ ] Automata settings tab visible in PWA Settings with all 4 sections
- [ ] Config keys `autonomous.security_scan`, `autonomous.secrets_scan`, `autonomous.rules_check` readable/writable via REST

---

#### Week H — Built-in Scanners + Secrets Scanner

**Files to create:**
- `internal/autonomous/scan/builtin/golangci.go`
- `internal/autonomous/scan/builtin/ruff.go`
- `internal/autonomous/scan/builtin/eslint.go`
- `internal/autonomous/scan/builtin/clippy.go`
- `internal/autonomous/scan/builtin/rubocop.go`
- `internal/autonomous/scan/builtin/npmaudit.go`
- `internal/autonomous/scan/builtin/secrets.go`

**Tasks:**
- [ ] `golangci.go`: wrap `golangci-lint run --out-format json`; parse JSON output → `[]Finding`; language=`go`; category=`sast`
- [ ] `ruff.go`: wrap `ruff check --output-format json`; parse → `[]Finding`; language=`python`; category=`sast`
- [ ] `eslint.go`: wrap `eslint --format json`; parse → `[]Finding`; language=`js,ts`; category=`sast`
- [ ] `clippy.go`: wrap `cargo clippy --message-format json 2>&1`; parse → `[]Finding`; language=`rust`; category=`sast`
- [ ] `rubocop.go`: wrap `rubocop --format json`; parse → `[]Finding`; language=`ruby`; category=`sast`
- [ ] `npmaudit.go`: wrap `npm audit --json`; parse advisories → `[]Finding` with severity from CVSS; language=`js,ts,node`; category=`dependency`
- [ ] `secrets.go`: wrap `gitleaks detect --source . --log-opts --all --report-format json --report-path /tmp/gitleaks-{id}.json`; parse findings; language=`*`; category=`secrets`; `Outcome` always `block` if any finding
- [ ] Secrets scanner: triggered when `.git` directory present in `ScanRequest.ProjectDir`; scans full history (`--log-opts --all`)
- [ ] Register all built-in scanners in `framework.go` init()
- [ ] Unit tests: each scanner's JSON output parsing (use fixture files from real tool output)
- [ ] Integration test: golangci scan on a known-bad Go file → expected finding count

**Acceptance checks:**
- [ ] Each scanner satisfies the `Scanner` interface (compile check)
- [ ] Each scanner parses its tool's JSON output correctly (unit test with fixture)
- [ ] Secrets scanner triggers when `.git` present; does NOT trigger when no `.git`
- [ ] Secrets scanner always returns `block` outcome on any finding
- [ ] `govulncheck` scanner present (uses `go install golang.org/x/vuln/cmd/govulncheck@latest` from BL228)

---

#### Week I — Rules Check Grader

**Files to create/modify:**
- `internal/autonomous/scan/intent/rulescheck.go`
- `internal/autonomous/manager.go` — wire rules check as post-task guardrail
- `internal/server/web/app.js` — rules violation prompt overlay

**Backend tasks:**
- [ ] `rulescheck.go`: implements `Scanner` interface; category=`intent`; language=`*`
  - Reads `AGENT.md` (fallback `CLAUDE.md`) from `ScanRequest.ProjectDir`
  - Gets task git diff via `git diff HEAD~1 -- {FilesTouched}` or task's stored diff
  - Calls `evals.LLMRubricGrader` with the rules-check prompt (see Section 8.5)
  - Returns `[]Finding` with `Rule` = rule_name, `Message` = evidence, `Severity` = "medium"
- [ ] Add `rules_check_pending` sub-state to `Task` status — transitions here after a rules violation
- [ ] `manager.go`: after `verifier.Verify()` succeeds, run rules check via `scan.RunAll(ctx, req, "intent")`
- [ ] On violation: transition task to `blocked` with `SubState: "rules_check_pending"`; emit alert to all 7 surfaces
- [ ] Operator override: `POST /api/autonomous/{prd-id}/tasks/{task-id}/override-rules` — accepts `{ reason: "..." }`; transitions task back to `completed`
- [ ] Config: `autonomous.rules_check_backend`, `autonomous.rules_check_model`, `autonomous.rules_file` (7-surface parity)

**Frontend tasks:**
- [ ] Rules violation prompt overlay in detail view: shows violation list with rule name + evidence
- [ ] Three action buttons: "Fix and re-verify", "Override and continue", "Edit rule"
- [ ] "Edit rule" opens rule editor panel (placeholder; full editor in Phase 3b)
- [ ] Override records the operator's reason in the task's decisions log
- [ ] Add localization keys → all 5 locale bundles

**Acceptance checks:**
- [ ] Rules check runs after each task verifies successfully
- [ ] A task with a known rule violation transitions to `blocked/rules_check_pending`
- [ ] Violation prompt shows on PWA with correct rule name and evidence
- [ ] Override-and-continue transitions task to `completed` and records reason in decisions
- [ ] `autonomous.rules_check: false` in config disables the check globally
- [ ] Per-automaton `rules_check` field overrides global config

---

#### Week J — Per-level Config Fields + Override Propagation

**Files to modify:**
- `internal/autonomous/models.go` — add config fields to PRD/Story/Task
- `internal/autonomous/api.go` — accept new fields in create/update
- `internal/server/web/app.js` — Launch wizard advanced section wires new fields

**Backend tasks:**
- [ ] Add to `PRD` struct: `RulesCheck *bool`, `RulesCheckMode string`, `SecurityScan *bool`, `ScanCategories []string`, `GuidedMode *bool`, `FixLoopMaxRetries int`
- [ ] Add to `Story` struct: `RulesCheck *bool`, `RulesCheckMode string`
- [ ] Add to `Task` struct: `RulesCheck *bool`, `RulesCheckMode string`, `ScanCategories []string`
- [ ] Override propagation in executor: task-level config wins over story-level wins over PRD-level wins over global config
- [ ] `POST /api/autonomous` and `PUT /api/autonomous/{id}` accept new fields
- [ ] Story and task update endpoints accept their new fields
- [ ] MCP `autonomous_create`, `autonomous_update` tools accept new fields
- [ ] CLI `datawatch automaton launch` gains flags: `--rules-check`, `--rules-check-mode`, `--security-scan`, `--guided-mode`

**Frontend tasks:**
- [ ] Launch wizard "Advanced" section: Security scan toggle (pre-checked), Rules check toggle (pre-checked), Guided Mode toggle (default from config), per-story approval toggle
- [ ] Wizard passes all fields to `POST /api/autonomous` on launch

**Acceptance checks:**
- [ ] Per-automaton `rules_check: false` disables rules check for that automaton only
- [ ] Per-task `scan_categories: ["sast"]` limits scan to SAST only for that task
- [ ] Global → PRD → Story → Task override chain tested (unit test for override resolution)
- [ ] Launch wizard advanced section visible; Guided Mode toggle reads from `autonomous.default_guided_mode`

**Phase 3 Gate:**
- [ ] All 4 weeks complete (G–J)
- [ ] Scan framework runs on a real task in the test environment — verify a `sast` finding appears in the task's scan results
- [ ] Secrets scanner: confirm `gitleaks` is installed in worker image (`gitleaks version` returns output)
- [ ] Rules check runs on a test task with a known AGENT.md violation → `blocked` state + violation prompt appears
- [ ] Automata Settings tab shows all sections; values save correctly via REST
- [ ] `scripts/release-smoke.sh` passes

---

### Phase 3b — LLM Fix Loop + Rule Editor

**Target:** v6.2.0 (continued)  
**Prerequisites:** Phase 3 complete; fix loop uses scan verdicts as trigger

#### Week J2 — LLM Fix Proposal Loop

**Files to create/modify:**
- `internal/autonomous/fix_loop.go` — **new**
- `internal/autonomous/manager.go` — wire fix loop trigger
- `internal/server/web/app.js` — fix proposal UI

**Backend tasks:**
- [ ] Add `FixProposal` struct to models:
  ```go
  type FixProposal struct {
      ID       string       `json:"id"`
      TaskID   string       `json:"task_id"`
      Options  []FixOption  `json:"options"`
      Status   string       `json:"status"` // pending | accepted | rejected | abandoned
      Retries  int          `json:"retries"`
      MaxRetries int        `json:"max_retries"`
      CreatedAt time.Time  `json:"created_at"`
  }
  type FixOption struct {
      Index       int      `json:"index"`
      Title       string   `json:"title"`
      Description string   `json:"description"`
      Changes     []string `json:"changes"` // concrete file/change descriptions
  }
  ```
- [ ] `fix_loop.go`: `SpawnFixAnalysisSession(ctx, task, violation)` → creates a short-lived `skill`-type session with constrained system prompt (no filesystem access, analysis only); parses LLM output into `[]FixOption`
- [ ] `AcceptFix(ctx, proposal, optionIndex)`: applies accepted fix as task context, re-queues task for retry, decrements `Retries`
- [ ] `MaxRetriesExceeded(proposal)`: transitions task to permanently `blocked`; requires manual operator intervention
- [ ] Wire in `manager.go`: on task `rejected` or scan `block` → call `SpawnFixAnalysisSession`; persist `FixProposal`
- [ ] REST: `GET /api/autonomous/{id}/tasks/{task-id}/fix-proposal`, `POST /api/autonomous/{id}/tasks/{task-id}/accept-fix`
- [ ] MCP: `get_fix_proposal`, `accept_fix`
- [ ] Comm: on blocked task, send fix proposal to messaging channel as numbered list; `accept fix 1` / `accept fix 2` / `abandon` commands

**Frontend tasks:**
- [ ] Fix proposal panel in task row of detail view (appears when task has a pending `FixProposal`)
- [ ] Shows: violation summary, 1–3 numbered options with title + description
- [ ] Action buttons: "Accept option N", "Abandon task"
- [ ] After accept: task transitions to re-queued, panel shows retry counter
- [ ] After max retries: panel shows "max retries exceeded" with manual intervention instructions
- [ ] Add localization keys → all 5 locale bundles

**Acceptance checks:**
- [ ] Blocking scan result triggers fix proposal creation
- [ ] Fix analysis mini-session completes and produces ≥1 fix option
- [ ] Accepting fix option re-queues task; retry counter visible in UI
- [ ] Max retries exceeded transitions task to permanently blocked; no further retries
- [ ] Comm channel receives fix proposal and `accept fix N` command works
- [ ] `autonomous.fix_loop_max_retries` config respected

---

#### Week J3 — LLM Rule Editor

**Files to create/modify:**
- `internal/autonomous/rule_editor.go` — **new**
- `internal/server/web/app.js` — rule editor panel

**Backend tasks:**
- [ ] `rule_editor.go`: `ProposeRuleEdits(ctx, violation, agentMdPath, model)` → reads AGENT.md, sends to LLM with violation + diff context, parses 2–3 structured `RuleEditOption` structs (each with `title`, `description`, `diff` patch)
- [ ] `ApplyRuleEdit(ctx, agentMdPath, model, acceptedOption)` → calls LLM with full AGENT.md + accepted change; verifies output is valid AGENT.md (length sanity check, required sections preserved); writes to file
- [ ] REST: `POST /api/autonomous/rules/propose-edit` with `{ violation_id, diff, rules_file }` → `[]RuleEditOption`; `POST /api/autonomous/rules/apply-edit` with `{ option_index, rules_file, edit_text }`
- [ ] MCP: `propose_rule_edit`, `apply_rule_edit`
- [ ] After edit applied: trigger rules re-check on the blocked task (re-run rulescheck.go)

**Frontend tasks:**
- [ ] "Edit rule" button in rules violation prompt opens rule editor panel
- [ ] Panel shows: violation in context, current rule text from AGENT.md, two paths:
  - "Let LLM propose" → calls `propose-edit` API → renders 3 options with diffs
  - "Edit directly" → inline AGENT.md editor (textarea) with "Apply with LLM insertion" button
- [ ] Each option shows: title, description, diff preview; "Accept this" button
- [ ] After accept: LLM applies edit, shows diff of full AGENT.md change; "Confirm" button writes to file
- [ ] After write: trigger rules re-check (spinner while checking)
- [ ] Add localization keys → all 5 locale bundles

**Acceptance checks:**
- [ ] `POST /api/autonomous/rules/propose-edit` returns ≥2 options for a test violation
- [ ] Accepting an option and applying produces a valid modified AGENT.md (verify AGENT.md length increases and is valid markdown)
- [ ] After edit applied, rules re-check runs on the blocked task
- [ ] Direct edit path also triggers LLM insertion and re-check

**Phase 3b Gate:**
- [ ] Fix loop end-to-end: block → proposal → accept → retry → verify → continue
- [ ] Rule editor end-to-end: violation → propose edit → accept → AGENT.md updated → re-check passes
- [ ] `scripts/release-smoke.sh` passes
- [ ] No regression in existing autonomous task execution

---

### Phase 4 — Type Extensibility, Guided Mode, Skills Wiring

**Target:** v6.2.0 (continued)  
**Prerequisites:** Phase 1 complete; v6.1 skills layer

#### Week K — Automaton Type Field + Plugin-Extensible Registry

**Files to create/modify:**
- `internal/autonomous/typeregistry/` — **new package**
- `internal/autonomous/models.go` — add `Type` field to `PRD`
- `internal/plugins/plugins.go` — wire plugin type registration
- `internal/server/web/app.js` — type-sensitive display aliases

**Backend tasks:**
- [ ] Create `internal/autonomous/typeregistry/` package:
  - `AutomatonTypeSpec` struct: `Name`, `DisplayName`, `StoryAlias`, `TaskAlias`, `TypeBadgeColor`, `DecompositionPromptTemplate`, `DefaultGuided`, `DefaultScanner`, `Icon`
  - `Registry` with `Register(spec)`, `Get(name)`, `List() []AutomatonTypeSpec`
  - Built-in types registered at init: software, research, operational, personal
- [ ] Load plugin-registered types from plugin manifests in `automaton_types:` key at plugin discovery time
- [ ] Add `Type` field to `PRD` struct (string, default `"software"` for back-compat)
- [ ] Decomposer reads type spec's `DecompositionPromptTemplate` for planning prompt
- [ ] Verifier reads type spec to select appropriate rubric
- [ ] REST: `GET /api/autonomous/types` — returns all registered types
- [ ] MCP: `list_automaton_types` tool
- [ ] CLI: `datawatch automaton types`
- [ ] Comm: `automaton types`

**Frontend tasks:**
- [ ] Display aliases: story heading shows type spec's `StoryAlias` (not hardcoded "Stories"); task heading shows `TaskAlias`
- [ ] Type dropdown in Launch wizard reads from `GET /api/autonomous/types`
- [ ] Type badge color in cards reads from type spec's `TypeBadgeColor`
- [ ] Custom type badge shown if plugin-registered type

**Acceptance checks:**
- [ ] `GET /api/autonomous/types` returns 4 built-in types with correct aliases
- [ ] Automaton with `type: research` shows "Phases" heading instead of "Stories"
- [ ] Plugin registering a type manifest results in that type appearing in `/api/autonomous/types`
- [ ] Existing PRDs without explicit type treated as `software` (back-compat)

---

#### Week L — Guided Mode + Skills Assignment

**Files to modify:**
- `internal/autonomous/executor.go` — Guided Mode pre-planning session
- `internal/autonomous/models.go` — `GuidedMode` field already added in Phase 3/J
- `internal/autonomous/api.go` — `Skills` field on PRD
- `internal/server/web/app.js` — Guided Mode planning context card

**Backend tasks:**
- [ ] `GuidedMode` field on `PRD` struct (already added in Phase 3/J)
- [ ] When `GuidedMode: true` and status transitions from `draft` → `planning`: first run a guided pre-planning session (Observe→Orient→Decide system prompt) — produces structured framing output stored as `PRD.PlanningContext` (new JSON field)
- [ ] `PRD.PlanningContext` struct: `Observations []string`, `Constraints []string`, `SuccessCriteria []string`, `Approach string`
- [ ] After guided pre-planning, continue with normal planning (decomposition) using type-specific prompt
- [ ] Add `Skills []string` field to `PRD` struct: names of skills to make available to the automaton's worker sessions
- [ ] Skill injection: when spawning a task worker session, if `PRD.Skills` is non-empty, configure the session to have access to those skill definitions
- [ ] Config: `autonomous.default_guided_mode` (bool, default false) — 7-surface parity

**Frontend tasks:**
- [ ] In detail view, when `planning_context` is present: show "Planning context" collapsible card above the stories/tasks tree
- [ ] Planning context card shows: Observations list, Constraints list, Success criteria list, Approach
- [ ] Guided Mode indicator in card lifecycle strip: when guided mode ON, show "Guided" label under Plan step
- [ ] Skills picker in Launch wizard: shows skills from `GET /api/skills`; selected skills stored as `PRD.Skills`

**Acceptance checks:**
- [ ] Guided Mode automaton produces a `planning_context` visible in detail view before stories/tasks appear
- [ ] Planning context card is collapsible; collapsed by default once familiar
- [ ] Non-guided automaton skips guided pre-planning (no performance penalty)
- [ ] Skills picker in wizard shows installed skills; selected skills are stored in PRD

**Phase 4 Gate:**
- [ ] Type registry returns correct aliases for all 4 built-in types
- [ ] Guided Mode creates planning context visible in PWA
- [ ] Skills picker in wizard works end-to-end (skill names stored + available to worker)
- [ ] `scripts/release-smoke.sh` passes

---

### Phase 4b — Identity Automaton Entry Point

**Target:** v6.2.0 (continued, parallel with Phase 4 or immediately after)  
**Prerequisites:** Phase 1 complete; v6.1 identity layer (Week 4) shipped (`GET/PUT /api/identity` available); v6.1 skills layer (Week 3) shipped

#### Week L2 — Identity REST + PRD FilePath + Interview Skill

**Files to create/modify:**
- `internal/autonomous/models.go` — add `FilePath` field to `PRD`
- `internal/server/identity.go` — **new**: `GET /api/identity`, `PUT /api/identity`
- `internal/server/api.go` — register new identity routes
- `internal/autonomous/api.go` — add `?all=true` to list endpoint to include completed/archived
- `internal/skills/embed.go` (or similar) — embed `interview-identity` skill manifest
- `internal/server/web/app.js` — robot icon in header + `openIdentityWorkflow()` + Identity Setup modal
- `internal/server/web/index.html` — add robot icon button to `<header>`
- `~/.datawatch/skills/interview-identity/skill.yaml` — built-in skill (extracted from binary on first run)

**Create-endpoint surface parity gap (cross-phase dependency)**

The identity interview uses `POST /api/autonomous/prds` with `type`, `skill`, `file_path`, `model`, and `guided_mode` fields. Today those fields are not accepted by the create endpoint or its mirrors. Each field lands in a specific phase; Phase 4b surface-parity work coordinates with those phases to ensure the field is wired through all four create surfaces simultaneously when first introduced:

| Field | Lands in | REST | MCP `autonomous_prd_create` | CLI `prd-create` | Comm `autonomous create` |
|-------|----------|------|----------------------------|-----------------|--------------------------|
| `type` | Phase 4 Week K | ✗ today → add | ✗ today → add | ✗ today → add `--type` | ✗ today → add `type=X` |
| `model` | Phase 4 Week L | ✗ today → add | ✗ today → add | ✗ today → add `--model` | ✗ today → add `model=X` |
| `skill` (single) | Phase 4 Week L | ✗ today → add | ✗ today → add | ✗ today → add `--skill` | ✗ today → add `skill=X` |
| `guided_mode` | Phase 4 Week L | ✗ today → add | ✗ today → add | ✗ today → add `--guided` | ✗ today → add `guided=true` |
| `file_path` | Phase 4b Week L2 | ✗ today → add | ✗ today → add | ✗ today → add `--file-path` | ✗ today → add `file=X` |

**Comm channel `autonomous create` key=value syntax:** The comm command gains optional `key=value` tokens parsed before the spec text. Example:
```
autonomous create type=personal skill=interview-identity file=/home/user/.datawatch/identity.yaml my spec text
```
Tokens matching `word=value` before any non-matching word are consumed as params; the rest is `spec`. This keeps the existing positional `autonomous create <spec>` working for the common case.

**`identity configure` dedicated shorthand** (comm + CLI + MCP): These shortcuts create the identity interview automaton without the operator needing to know the type/skill incantation. They are separate from the general `autonomous create` command and always preset `type=personal`, `skill=interview-identity`, and `file_path=<config default>`.

**Backend tasks:**
- [ ] Add `FilePath string \`json:"file_path,omitempty"\`` to `PRD` struct in `models.go`
- [ ] Wire `FilePath` through PRD create (`POST /api/autonomous/prds`), update, and retrieve
- [ ] Coordinate with Phase 4 Week K: when `Type` field lands, wire it through `POST /api/autonomous/prds`, `prd-create` CLI, `autonomous_prd_create` MCP, and `autonomous create` comm simultaneously
- [ ] Coordinate with Phase 4 Week L: same for `Model`, `Skill` (single string), `GuidedMode`
- [ ] `internal/server/identity.go`: implement `GET /api/identity` and `PUT /api/identity`
  - `GET`: reads file at config path `identity.file_path` (default `~/.datawatch/identity.yaml`); returns `{"content":"...","path":"..."}` or `{"content":null}` + 204 if not found
  - `PUT`: accepts `{"content":"..."}`, validates YAML with `gopkg.in/yaml.v3`, writes file (creates parents if needed)
- [ ] Register routes in `api.go`: `GET /api/identity`, `PUT /api/identity`
- [ ] Add `?all=true` parameter to `GET /api/autonomous/prds` list endpoint; when set, includes all statuses (no active-only filter)
- [ ] Add `identity` command dispatch to router (`internal/router/`): `identity` → GET /api/identity; `identity configure` → POST /api/autonomous/prds with identity preset
- [ ] Add `identity configure` CLI subcommand to `cmd/datawatch/cli_autonomous.go` (or new `cli_identity.go`): calls POST /api/autonomous/prds with identity preset + accepts `--backend`, `--model`, `--effort`, `--file-path` flags
- [ ] MCP tools: `get_identity`, `set_identity` (YAML content in/out), `run_identity_interview` (creates PRD with identity preset, returns id)

**Built-in skill:**
- [ ] Write `~/.datawatch/skills/interview-identity/skill.yaml` following the schema in §12b.4
- [ ] Embed it in the binary (use `//go:embed` or the existing plugin embed pattern)
- [ ] Extract to `~/.datawatch/skills/interview-identity/skill.yaml` on startup if not present (same pattern as built-in templates)
- [ ] Skill registered as `type: interview` in the skill registry; skill executor handles `type: interview` by running a Guided Mode `personal` session with phase-driven prompts

**Frontend tasks:**
- [ ] Add robot icon button `<button id="identityBtn" class="header-identity-btn">&#129302;</button>` in `index.html` `<header>` between search button and status dot
- [ ] Show/hide `identityBtn` using same gate as `navBtnAutonomous`: hidden until `GET /api/autonomous/config` returns `enabled: true`
- [ ] Implement `openIdentityWorkflow()`:
  1. `GET /api/autonomous/prds?all=true` — fetch all PRDs
  2. Search for `prd.spec.toLowerCase().includes('identity')`
  3. If found: `navigate('autonomous')` then `scrollToAutomaton(prd.id)`
  4. If not found: open Identity Setup modal
- [ ] Identity Setup modal: pre-filled intent (read-only), LLM/model/effort pickers, file path input (text + browse button), "Launch" button
- [ ] On "Launch": `POST /api/autonomous/prds` with `{spec:"identity, projects, values and goals", type:"personal", skill:"interview-identity", file_path:"...", backend:"...", model:"...", effort:"..."}` then navigate to detail view
- [ ] Add i18n keys for all new strings (robot icon tooltip, modal title, file path label, launch button label) in all 5 locale bundles
- [ ] File datawatch-app parity issue for identity view/edit and re-interview trigger

**Acceptance checks:**
- [ ] `GET /api/identity` returns content when `identity.yaml` exists; 204 when absent
- [ ] `PUT /api/identity` with invalid YAML returns 400 with error message
- [ ] `PUT /api/identity` with valid YAML writes file; subsequent GET returns same content
- [ ] `GET /api/autonomous/prds?all=true` includes completed/archived PRDs (without `?all=true`, they remain filtered)
- [ ] Robot icon appears in header when autonomous enabled; hidden when disabled
- [ ] Clicking robot icon with existing identity PRD (any status) → navigates to Automata tab + highlights the card
- [ ] Clicking robot icon with no identity PRD → opens Identity Setup modal
- [ ] Launching from modal creates a PRD with `type: personal`, `skill: interview-identity`, correct `file_path`
- [ ] Interview skill manifest loads without error from skills layer
- [ ] All 7 surfaces reach identity content (manual smoke check: YAML config, REST, MCP, CLI, comm, PWA, mobile parity issue filed)

**Phase 4b Gate:**
- [ ] GET/PUT `/api/identity` work end-to-end (read, write, 204 on missing, 400 on invalid YAML)
- [ ] Robot icon shows/hides correctly based on autonomous feature flag
- [ ] Identity lookup finds existing PRDs across all statuses
- [ ] Identity Setup modal creates a valid PRD with all required fields
- [ ] `interview-identity` skill embedded and extractable from binary
- [ ] i18n keys present in all 5 locale bundles
- [ ] `scripts/release-smoke.sh` passes

---

### Phase 5 — 7-Surface Parity + datawatch-app

**Target:** v6.2.0 (final sprint)  
**Prerequisites:** Phases 1–4 complete

#### Week M — MCP Tools Audit

**Files to modify:**
- `internal/mcp/` or `internal/server/mcp.go` (wherever MCP tools are registered)

**Tasks:**

> **Note:** `type`, `model`, `skill`, `guided_mode`, `file_path` on the create endpoint are introduced per-field in Phase 4 Week K/L and Phase 4b Week L2. Phase 5 Week M sweeps for any remaining gaps and audits parity across all surfaces.

- [ ] Verify `POST /api/autonomous/prds` now accepts all new fields added in Phases 4/4b: `type`, `model`, `skill`, `guided_mode`, `file_path`, `rules_check`, `rules_check_mode`, `security_scan`, `scan_categories`, `fix_loop_max_retries`, `skills`
- [ ] Verify CLI `prd-create` has flags for all same fields: `--type`, `--model`, `--skill`, `--guided-mode`, `--file-path`, `--rules-check`, `--security-scan`
- [ ] Verify comm `autonomous create` key=value parser handles all same fields: `type=X`, `model=X`, `skill=X`, `guided=true`, `file=X`
- [ ] Audit all existing autonomous-related MCP tools: `autonomous_prd_create`, `autonomous_prd_plan`, `autonomous_prd_run`, `autonomous_prd_cancel`, `autonomous_prd_list`, `autonomous_status`
- [ ] Add new params to `autonomous_prd_create`: `type`, `model`, `guided_mode`, `skill`, `file_path`, `rules_check`, `rules_check_mode`, `security_scan`, `scan_categories`, `fix_loop_max_retries`, `skills`
- [ ] Add new MCP tools:
  - `list_automaton_types` — returns type registry
  - `get_fix_proposal` — returns pending fix proposal for a task
  - `accept_fix` — accepts a fix option and retries the task
  - `propose_rule_edit` — given violation, returns rule edit options
  - `apply_rule_edit` — applies accepted rule edit option
  - `run_scan` — runs scan on a directory
  - `list_templates`, `get_template`, `instantiate_template`, `create_template`, `update_template`, `delete_template`
- [ ] Update MCP tool descriptions to use "automaton" not "PRD" terminology
- [ ] Test each new tool with Claude Desktop as MCP client
- [ ] Update `docs/mcp.md` with all new tools

**Acceptance checks:**
- [ ] `list_automaton_types` returns 4 types via MCP
- [ ] `autonomous_create` with `type: "research"` creates research automaton
- [ ] `get_fix_proposal` returns pending proposal for a blocked task
- [ ] `list_templates` returns all templates
- [ ] All new tools documented in `docs/mcp.md`

---

#### Week N — CLI Redesign

**Files to modify:**
- `cmd/datawatch/main.go` or CLI command registrations

**Tasks:**
- [ ] Add `automaton` CLI subcommand group: `datawatch automaton <cmd>`
  - `datawatch automaton launch` — interactive or flags-based automaton creation
  - `datawatch automaton list` — list automata with status filter
  - `datawatch automaton status <id>` — show current status + progress
  - `datawatch automaton approve <id>` — approve a plan
  - `datawatch automaton cancel <id>` — cancel a running automaton
  - `datawatch automaton fix-propose <id> --task <task-id>` — show fix proposal
  - `datawatch automaton fix-accept <id> --task <task-id> --option <n>` — accept fix
- [ ] Add `template` subcommand group: `datawatch template list`, `datawatch template use <id>`
- [ ] Add `scan` subcommand: `datawatch scan --dir . --category sast,dependency,secrets --lang auto`
- [ ] Add `rules` subcommand: `datawatch rules check --dir . --rules-file AGENT.md`
- [ ] Backward-compat aliases: `datawatch autonomous` → `datawatch automaton` (deprecated warning + redirect)
- [ ] Update CLI help text: replace "PRD" with "automaton/automata" throughout

**Acceptance checks:**
- [ ] `datawatch automaton list` returns current automata
- [ ] `datawatch autonomous list` works (back-compat alias) with deprecation warning
- [ ] `datawatch scan --dir . --category secrets` runs gitleaks and returns result
- [ ] `datawatch template list` returns templates

---

#### Week O — Comm Channel Commands

**Files to modify:**
- `internal/channel/commands.go` or equivalent command router

**Tasks:**
- [ ] Update command routing: `automaton launch <intent>` (alias: `prd new <intent>` deprecated)
- [ ] `automaton status <id>` — shows current status, progress, current story/task
- [ ] `automaton cancel <id>` — cancels
- [ ] `automaton approve <id>` — approves plan
- [ ] `accept fix <n>` — accepts fix option N for the most recent blocked task in the conversation context
- [ ] `scan <dir>` — runs scan on directory
- [ ] `template list` — lists templates
- [ ] `template use <name>` — instantiates template (prompts for vars in follow-up message)
- [ ] Deprecation notices: old `prd` commands show "prd is deprecated, use automaton" for 1 full release before removal
- [ ] Update all channel command documentation

**Acceptance checks:**
- [ ] `automaton launch "add rate limiting to API"` creates an automaton and responds with detail link
- [ ] `automaton status <id>` returns progress summary
- [ ] `accept fix 1` works after a fix proposal is sent to the channel
- [ ] Deprecated `prd` commands still work but log deprecation notice

---

#### Week P — datawatch-app Issues + Quick Mobile Wins

**Tasks:**
- [ ] File Issue 1 (Phone) against datawatch-app with full checklist from Section 11 Issue 1
- [ ] File Issue 2 (Wear OS) against datawatch-app with Section 11 Issue 2 capability targets
- [ ] File Issue 3 (Android Auto) against datawatch-app with Section 11 Issue 3 voice interface targets
- [ ] Quick mobile wins (implement in datawatch API — no app changes needed):
  - [ ] Ensure `GET /api/autonomous` returns type-aware alias fields (`story_alias`, `task_alias`) so app can use them
  - [ ] Ensure WebSocket `prd_update` events include `planning_context` field when Guided Mode is on
  - [ ] Ensure `POST /api/autonomous/{id}/approve` and `POST /api/autonomous/{id}/cancel` are clean REST endpoints usable by Android Auto voice integration
  - [ ] Ensure fix proposal REST endpoints have appropriate brevity for notification payloads

**Acceptance checks:**
- [ ] 3 datawatch-app issues filed with links in `docs/plans/README.md`
- [ ] `GET /api/autonomous` response includes `story_alias` and `task_alias` fields
- [ ] `prd_update` WebSocket events tested and include all fields needed by mobile app

**Phase 5 Gate:**
- [ ] All MCP tools documented and tested
- [ ] CLI `automaton` subcommand works for all core operations
- [ ] Comm `automaton` commands work end-to-end
- [ ] 3 datawatch-app issues filed

---

### Phase 6 — Release

**Target:** v6.2.0 final release

#### Week Q — Integration, Smoke Tests, Release Notes

**Tasks:**
- [ ] Cross-feature integration tests:
  - Guided Mode → planning context visible in PWA
  - Rules check violation → fix proposal loop → accepted → retry → verified
  - Secrets found in git history → scan blocks task → operator forced to remediate
  - Plugin-registered type → type appears in wizard dropdown → used in launch
  - Template with vars → instantiated → automaton runs correctly
- [ ] Run `scripts/release-smoke.sh` — all tests must pass
- [ ] `gosec` scan on all new packages; resolve any findings
- [ ] Documentation updates:
  - [ ] `docs/howto/autonomous-planning.md` — full rewrite for Automata terminology + wizard
  - [ ] `docs/howto/autonomous-review-approve.md` — rewrite for lifecycle strip + fix loop
  - [ ] `docs/howto/automata-secrets-scan.md` — new: secrets scanner usage, git history remediation
  - [ ] `docs/howto/automata-rules-check.md` — new: rules check, violation workflow, rule editor
  - [ ] `docs/api/autonomous.md` — new fields, endpoints, status codes
  - [ ] `docs/mcp.md` — all new MCP tools documented
- [ ] Update version to v6.2.0 in `cmd/datawatch/main.go` and `internal/server/api.go`
- [ ] Write v6.2.0 release notes covering all 6 phases
- [ ] Update README
- [ ] `datawatch update && datawatch restart`

**Release Gate:**
- [ ] All Phase 1–5 + Phase 4b gates passed
- [ ] `scripts/release-smoke.sh` passes cleanly
- [ ] No `gosec` critical findings in new code
- [ ] All 7 surfaces verified for: automaton create, plan, approve, run, cancel (manual smoke check)
- [ ] Secrets scanner blocks a task in the test environment
- [ ] Rules check violation prompt appears and all 3 actions work (fix, override, edit rule)
- [ ] v6.2.0 tagged and published

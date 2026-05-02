# BL221 — Automata Redesign (née "PRD")

**Backlog item:** BL221  
**Date:** 2026-05-02  
**Last updated:** 2026-05-02 — configuration surface added; "decomposing" renamed to "planning" throughout; guided mode term resolved; secrets scanner added; LLM-assisted fix loop + rule editor design; type extensibility via plugins; datawatch-app Watch/Auto expanded  
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

## 13. Related Files

**Backend (new/modified):**
- `internal/autonomous/models.go` — add `Type`, `GuidedMode`, `RulesCheck`, `RulesCheckMode`, `ScanConfig`, `FixLoopMaxRetries` fields to PRD/Story/Task; rename `StatusDecomposing` → `StatusPlanning`
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

**Documentation:**
- `docs/howto/autonomous-planning.md` — full rewrite for Automata terminology + wizard
- `docs/howto/autonomous-review-approve.md` — rewrite for lifecycle strip UI + fix loop
- `docs/howto/automata-secrets-scan.md` — **new**: secrets scanner usage, git history remediation
- `docs/howto/automata-rules-check.md` — **new**: rules check, violation workflow, rule editor
- `docs/api/autonomous.md` — add new fields, new endpoints, planning status code

**Cross-references:**
- `docs/plans/2026-05-02-unified-ai-platform-design.md` — Week 5 (session types / Guided Mode), Week 7 (evals framework for rules check)
- `docs/plans/README.md` — BL221 backlog entry, BL228 prerequisites

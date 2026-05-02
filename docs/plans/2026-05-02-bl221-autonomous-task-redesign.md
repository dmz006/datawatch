# BL221 — Automata Redesign (née "PRD")

**Backlog item:** BL221  
**Date:** 2026-05-02  
**Last updated:** 2026-05-02 — all Q1–Q11 resolved; scan tooling inventory complete; ready for implementation planning  
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

### 3.2 The Automata Lifecycle as Algorithm Mode

A key insight from the unified platform design (Algorithm Mode, Week 5): **the automaton lifecycle is Algorithm Mode applied at the project scale**.

```
Launch intent           → OBSERVE:   system reads context (project dir, memory, identity)
                        → ORIENT:    system infers type, constraints, success criteria
Operator approves plan  → DECIDE:    reviewed decomposition becomes the execution contract
Execution runs          → ACT:       stories + tasks fire in DAG order
Learnings captured      → SUMMARIZE: memory records outcomes, decisions, surprises
```

The creation wizard is therefore the Observe + Orient phases made interactive. The "Plan" step (decompose) is the system's first act. The review/approve gate is the Decide phase. This means:

- Automata created with Algorithm Mode ON → the Plan step produces a structured Observe → Orient → Decide output before decomposing
- The operator reviews the system's *framing of the problem* before the task breakdown
- This surfaces assumptions early, reducing wasted execution

The existing `algorithm_mode` flag (Week 5 of the sprint plan) flows directly into automaton creation.

---

## 4. List View Redesign

### 4.0 Tab structure

The "Autonomous" tab becomes a top-level section with **three sub-tabs**:

```
┌─────────────────────────────────────────────────────┐
│  Automata  │  Templates  │  (History — toggle)      │
└─────────────────────────────────────────────────────┘
```

- **Automata** — active and recent automata (the redesigned main view)
- **Templates** — full CRUD for reusable automaton templates (see Section 7)
- **History** — completed/archived/cancelled/rejected (toggle, same as Sessions)

### 4.1 Header bar additions

```
[ Automata ]   [?  How-to]   [⊞ Filter ▾]   [History]   [⚡ Launch Automaton]
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
│  [ ] Algorithm mode (Observe→Orient→Decide before planning) │
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
| Algorithm mode | Type is research / operational / personal (pre-checked); coding (unchecked) | — |
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
2. If Algorithm Mode is on: runs the Observe → Orient → Decide pre-planning session first (produces a structured framing of the problem — assumptions, constraints, success criteria) — shown in the detail view as a collapsible "Planning context" card
3. Runs decomposition (`decomposing` state)
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
- Week D: Launch wizard (progressive disclosure, intent field, type auto-inference, field visibility rules, Algorithm Mode toggle, skills picker)

### Phase 2 — Templates
- Week E: Templates tab (CRUD list, template detail view, var rendering)
- Week F: Template instantiation flow (pre-filled wizard from template), clone-to-template from completed automaton, tags + search

### Phase 3 — Backend: Rules check + scan framework
- Week G: `internal/autonomous/scan/` package (scanner interface, language auto-detect, result aggregation into GuardrailVerdict)
- Week H: Built-in scanner implementations: SAST pattern library (Go/Python/JS/TS/Rust), dependency scanner (delegates to gosec/npm-audit/govulncheck), severity mapping
- Week I: Rules check grader (llm_rubric against AGENT.md, `rules_check_pending` blocked sub-state, operator override prompt on all 7 surfaces)
- Week J: Per-task/story/PRD rules_check + security_scan config fields, per-level override propagation

### Phase 4 — Type, Algorithm Mode, Skills wiring
- Week K: Automaton type field (software/research/operational/personal), type-sensitive display aliases (Stories vs Phases vs Workstreams), decomposition prompt variants, verifier rubric variants
- Week L: Algorithm Mode in automaton launch (Observe→Orient→Decide pre-planning output surfaced in detail view before decomposition), skills assignment wiring

### Phase 5 — 7-surface parity + datawatch-app
- Week M: MCP tools audit (`autonomous_create` / `autonomous_plan` / `autonomous_run` / `autonomous_cancel` / `autonomous_list` / `autonomous_status` — all accept new fields: type, algorithm_mode, rules_check, scan config)
- Week N: CLI redesign (`datawatch automaton launch`, `datawatch automaton list`, `datawatch automaton status`, `datawatch scan`) — backward-compat aliases for old `autonomous` subcommand
- Week O: Comm channel command update (`automaton launch`, `automaton status <id>`, `automaton cancel <id>`, `scan <dir>`)
- Week P: datawatch-app comprehensive issue filed + any quick mobile wins (status card, basic launch)

### Phase 6 — Release
- Week Q: Integration, smoke tests, release notes for v6.2.0

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

## 12. Design Complete

All Q1–Q12 resolved. See Section 0 table.

**Implementation prerequisites before Phase 3 (scan framework):**
- **BL228** _(new, to be filed)_: Add security scanner tools to language layer Dockerfiles — `govulncheck` (Go), `bandit`+`pip-audit` (Python), `eslint-plugin-security` (Node), `cargo-audit` (Rust), `brakeman`+`bundler-audit` (Ruby)
- **v6.1 evals framework** (unified platform Week 7): rules check depends on the `llm_rubric` grader type being available

---

## 13. Related Files

**Backend (new/modified):**
- `internal/autonomous/models.go` — add `Type`, `RulesCheck`, `RulesCheckMode`, `ScanConfig` fields to PRD/Story/Task
- `internal/autonomous/scan/` — new package: scanner framework, built-in scanners, intent scanners
- `internal/autonomous/security.go` — **replace** with `scan/builtin/` implementations; keep as compatibility shim
- `internal/autonomous/executor.go` — wire rules_check grader + scan framework into verification flow
- `internal/autonomous/api.go` — new endpoints: scan results, templates CRUD, display aliases config
- `internal/autonomous/manager.go` — wire rules_check as post-task guardrail with operator block prompt
- `internal/autonomous/templates.go` — **new**: Template CRUD store + instantiation

**Frontend (full redesign):**
- `internal/server/web/app.js` — `renderPRDRow`, `renderPRDActions`, `renderAutonomousView`, `openPRDCreateModal` replaced with Automata redesign
- `internal/server/web/app.css` — new card classes, lifecycle strip, breadcrumb, detail view, tabs

**Docker (language layer updates — BL228):**
- `docker/dockerfiles/Dockerfile.lang-go` — add `govulncheck`
- `docker/dockerfiles/Dockerfile.lang-python` — add `bandit`, `pip-audit`
- `docker/dockerfiles/Dockerfile.lang-node` — add `eslint-plugin-security` to global npm install
- `docker/dockerfiles/Dockerfile.lang-rust` — add `cargo-audit`
- `docker/dockerfiles/Dockerfile.lang-ruby` — add `brakeman`, `bundler-audit`

**Documentation:**
- `docs/howto/autonomous-planning.md` — full rewrite for Automata terminology + wizard
- `docs/howto/autonomous-review-approve.md` — rewrite for lifecycle strip UI

**Documentation:**
- `docs/howto/autonomous-planning.md` — update to reflect new wizard + terminology
- `docs/howto/autonomous-review-approve.md` — update for new lifecycle strip
- `docs/api/autonomous.md` — add new fields, new endpoints

**Cross-references:**
- `docs/plans/2026-05-02-unified-ai-platform-design.md` — Week 5 (session types), Week 7 (evals framework for rules check)
- `docs/plans/README.md` — BL221 backlog entry

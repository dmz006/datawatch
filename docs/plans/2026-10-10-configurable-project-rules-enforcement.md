# Plan: Configurable Project-Rules Enforcement (BL406)

- **Date**: 2026-10-10
- **Version at planning**: v9.0.10
- **Status**: Planned — not started

## Context

Found while designing BL405's single-thread backlog-execution workflow,
not requested in isolation: AGENT.md's own **Decomposer Scope-Drift Rule
(BL384)** documents a two-layer mitigation stack — "guided mode pauses
before each task" and "a PRD scan rule — scope drift detector" — and both
layers turned out to be dead code (**B111**, **B112**, filed this
session). Only mitigation #2 in BL384's own list (manually patching each
task spec before approval) has ever actually worked. The scan framework
that exists (`internal/autonomous/scan`) is real and working, but it only
checks security (SAST/secrets/deps) — nothing checks a task/story/PRD's
content against the project's own rules, nothing checks the parity-surface
inheritance AGENT.md's prose already requires, and nothing automates
filing a cross-repo issue when a gap can't be closed in this repo alone.

This matters beyond fixing two dead flags: the operator's goal is to
eventually run datawatch's **own backlog** (BL403/BL404/BL405/etc.)
through datawatch's **own PRD/Automata engine**, continuously, stopping
only for real decisions. That's only safe if something structurally
enforces the project's rules as PRDs complete — version-sync, CHANGELOG,
parity, docs — rather than trusting an LLM task's own judgment or a human
re-reading every diff. This plan is that enforcement layer, and it is
explicitly designed to generalize past this one repo: every filename this
plan reads (rules file, context file, upstream repo) is operator-
configured, never hardcoded, so another datawatch operator can point it
at their own equivalents of AGENT.md / DATAWATCH-CONTEXT.md / an upstream
app repo without renaming anything to match a convention this plan
invents.

## Confirmed current-state findings (live-verified 2026-10-10)

- `internal/autonomous/scan/scan.go`: `Scanner` interface (`Name() string`,
  `Scan(dir string) ([]Finding, error)`), orchestrated by `scan.Run(dir,
  cfg, scanners, grader)`. `Config{Enabled, SASTEnabled, SecretsEnabled,
  DepsEnabled, FailOnSeverity, MaxFindings, RulesGraderEnabled,
  FixLoopEnabled, FixLoopMaxRetries}` — a fixed set of three scanner
  categories; no operator-definable rule concept exists.
- Exactly three `Scanner` implementations exist: `sast.go`, `secrets.go`,
  `deps.go` — all security-focused. Zero hits anywhere in the codebase for
  `scope-drift`/`ScopeDrift` — the rule AGENT.md documents was never built
  (**B112**).
- `GraderFn func(findings []Finding, projectDir string) (verdict, notes
  string, err error)` and `RuleEditorFn` already exist as LLM-grading
  hooks over scan findings — reusable as-is for grading a project-rule
  finding (e.g. "is this version-mismatch a real problem"), not something
  to build twice.
- `internal/git/github.go`'s `GitHub` provider has a real `OpenPR` (used
  by `internal/agents/post_session.go`'s `PostSessionPRHook`) via `gh`
  through a `run(ctx, args...)` wrapper, but **no issue-creation method**
  — confirmed via repo-wide grep for `CreateIssue`/`OpenIssue`/`gh issue
  create`, zero hits. A sibling method on the same provider, not a new
  client.
- No config field anywhere points at a rules file or a context file by
  path — `AGENT.md` and `DATAWATCH-CONTEXT.md` are referenced by hardcoded
  convention wherever a human or an LLM session reads them, never as a
  configured path. `DATAWATCH-CONTEXT.md` (confirmed present, 43.8KB,
  last touched 2026-10-07) is a descriptive "state of the project"
  orientation doc (what-is-this, directory structure, package inventory,
  build/test/release commands) — a different kind of document from
  AGENT.md's prescriptive rules, and the operator notes not every project
  has one.
- AGENT.md's Decomposer Scope-Drift Rule section already states the
  parity-inheritance requirement in prose: *"decomposed PRD stories MUST
  inherit the parent plan's Parity surface list. Each story spec carries
  the same full surface set... and the same per-surface include/
  exclude-with-reason entries as its parent plan. A story that narrows or
  drops a surface without a stated reason is a scope drift and fails the
  scan."* No code checks this today — it becomes this plan's first
  concrete built-in rule, not a new spec to invent.

## Decisions

1. **New fourth scan category**, `project-rules`, alongside the existing
   `sast`/`secrets`/`deps` — implemented as one new `Scanner`
   (`ProjectRulesScanner`), reusing `scan.Run`'s existing orchestration,
   not a parallel scan engine. Its rules are **data-driven** (operator-
   defined, named), not one-Go-function-per-rule like the three existing
   scanners — this is the actual "configurable" ask.
2. **Rule types at launch**: content/keyword pattern match (generalizes
   the original scope-drift ask — e.g. flag "Implement ..."/"Write code
   for ..." in a task spec when the PRD says doc-only); cross-file
   consistency (two files' values must match — e.g. the version-sync
   rule this project's own release checklist already needs by hand);
   presence check (a file or pattern must exist — e.g. a CHANGELOG entry,
   a docs update); parity-surface inheritance (a story's declared surface
   set must match its parent plan's, per AGENT.md's existing prose above).
3. **Action types**: `warn` / `fail` (existing `Finding.Severity`
   semantics, reused as-is) plus a new `file_upstream_issue` — opens a
   GitHub issue in an operator-configured second repo via the new
   `GitHub.CreateIssue` sibling method, for gaps this repo's own PRD can't
   close alone (mirrors this session's own manual pattern of filing
   `datawatch-app#206`/`#207`/`#208`).
4. **Granularity is per-rule, not one fixed gate**: each rule declares
   where it's checked — `task_complete` / `story_complete` /
   `prd_complete`. A parity rule wants story-level; a CHANGELOG rule only
   makes sense PRD-level; a content-pattern rule might want task-level.
5. **New operator-configured paths**, never hardcoded: `autonomous.
   rules_file` (default `AGENT.md`) and `autonomous.context_file`
   (default `CONTEXT.md`, explicitly optional — "not everyone has one").
   The decomposer/task-spec generation and the new scanner both read
   whichever path is configured; an operator with a differently-named
   file (this repo's own `DATAWATCH-CONTEXT.md`) points the config at it
   instead of renaming it.
6. **Upstream targets are named config, not inferred**: a rule's
   `file_upstream_issue` action references a named entry in a new
   `autonomous.upstream_repos []{name, owner_repo}` list — explicit,
   operator-maintained, not auto-detected from git remotes.
7. **B111 (GuidedMode) and B112 (scope-drift) are fixed here, not
   separately** — see Phase 5. Fixing them twice (once as a narrow patch,
   once as part of this engine) would be wasted work; this plan is where
   both actually get load-bearing behavior.

## Phases

### Phase 0 — Config scaffolding
- `autonomous.rules_file`, `autonomous.context_file`,
  `autonomous.upstream_repos[]` — new `AutonomousConfig` fields.
- New `scan.ProjectRule{ID, Name, Type, Granularity, Pattern/Check,
  Severity, Action}` type + CRUD store (same shape discipline as alert
  rules' `Condition`/`Action`, reused conceptually).
- Full Configuration Accessibility Rule pass: YAML, REST
  (`GET/PUT /api/config`), MCP, CLI, comm, PWA (Settings → Automate,
  alongside the existing scan-config card).

### Phase 1 — Rule engine core
- `ProjectRulesScanner` implementing the existing `Scanner` interface;
  registered alongside `sast`/`secrets`/`deps` in `scan.Run`'s scanner
  list — no new orchestration path.
- Content/keyword, cross-file-consistency, and presence-check rule types
  implemented and tested against this repo's own `AGENT.md` rules as the
  first real dogfood case (e.g. a rule encoding "Version sync both files,"
  already a standing `feedback_version_sync` session-memory lesson).

### Phase 2 — Parity-inheritance rule (first built-in)
- Ships as a default-enabled `project-rules` entry once Phase 1 exists:
  checks a story's Parity surface section against its parent plan's
  surface list and per-surface exclusion reasons, per AGENT.md's own
  prose (quoted above) — closing the gap between what the rule already
  says and what code has ever checked.

### Phase 3 — Multi-granularity wiring
- New executor hook points at task-complete, story-complete, and
  PRD-complete (today, scanning is PRD-wide and on-demand via
  `autonomous_prd_scan`, never automatically wired to a completion
  event) — each hook runs only the rules declared at its granularity.

### Phase 4 — Upstream issue-filing action
- `GitHub.CreateIssue(ctx, opts IssueOptions) (string, error)` — sibling
  to the existing `OpenPR`, same `run(ctx, args...)` → `gh issue create`
  pattern.
- `file_upstream_issue` action wired to Phase 1's rule engine, resolving
  its target against `autonomous.upstream_repos`.

### Phase 5 — Fix B111 and B112 for real
- **B111 (GuidedMode)**: redesigned as a pluggable gate *source*, not a
  blanket pre-task pause — `operator` (pause for a human, the original
  intent), `council` (pause for a `CouncilProfile` consensus decision
  instead — ties directly to BL405 Phase 8's council profiles),
  `guardrail_auto` (continue automatically when this plan's quality
  gates + rule checks pass; retry or reset-with-new-context when they
  fail). This is the mechanism BL405's "only stop for a real decision"
  workflow actually needs.
- **B112 (scope-drift)**: re-implemented as a first-class Phase 1 rule
  (content/keyword type, task-granularity) instead of staying
  prose-only in AGENT.md.

## Parity surface

| Capability | REST | MCP | CLI | Comm | YAML | PWA | Android/iOS |
|---|---|---|---|---|---|---|---|
| Rule config (Phase 0) | `/api/config` extension | `autonomous_scan_config_set` extension | `datawatch config set` | `configure autonomous.rules_file=...` | `autonomous.rules_file`/`context_file`/`upstream_repos` | Scan config card, Settings → Automate | reporting-only |
| Project rule CRUD (Phase 1) | new `/api/autonomous/scan/rules*` | new `scan_rule_*` tools | n/a (complex object, PWA/REST, same precedent as other structured-definition objects) | n/a | n/a (runtime store) | rule builder UI | reporting-only |
| Granularity hooks (Phase 3) | reporting via scan results | reporting via `autonomous_prd_scan_results` | n/a | n/a | n/a | scan results per task/story/PRD | reporting-only |
| Upstream issue filing (Phase 4) | `/api/autonomous/scan/upstream*` | new tool | n/a | n/a | `autonomous.upstream_repos` | upstream-repo config UI | reporting-only |
| GuidedMode pluggable source (Phase 5) | `set_guided_mode` extension (source param) | same MCP tool, extended | same CLI cmd, extended | same comm verb, extended | n/a (runtime PRD state) | guided-mode source picker | reporting-only |

## Reuse-and-Expand audit

- New scanner → existing `Scanner` interface + `scan.Run` orchestration,
  not a new scan engine.
- Rule grading → existing `GraderFn`/`RuleEditorFn`, not new LLM-grading
  plumbing.
- Upstream issue filing → existing `git.Provider`/`GitHub.run` pattern
  (sibling to `OpenPR`), not a new GitHub client or a new provider
  abstraction.
- Parity-inheritance rule → AGENT.md's own already-written prose
  requirement, not a newly invented spec.
- Rule CRUD shape → the same named-definition pattern already used by
  alert rules (`Condition`/`Action`), profiles, guardrail profiles, and
  (per BL405) council profiles — a fifth instance of one established
  pattern, not a sixth bespoke shape.

## Out of scope / deferred

- Auto-detecting an operator's rules/context file by scanning the repo
  root for likely candidates — explicit config only, per Decision 5.
- Auto-detecting upstream repos from git remotes — explicit named config
  only, per Decision 6.
- A generic cross-project "rules marketplace" or shared rule-definition
  library — this plan ships the engine and a handful of concrete rules
  for this repo; sharing rule sets across operators is a natural
  follow-up, not designed here.

## Testing / verification

- Unit tests per rule type (content/keyword, cross-file-consistency,
  presence-check, parity-inheritance) with both a passing and a
  deliberately-failing fixture.
- Regression test: running `project-rules` scanning against this repo's
  actual `AGENT.md`-derived rules (version-sync, parity-inheritance) must
  pass on current `main` before this plan is considered done — if it
  doesn't, that's a real finding to fix, not a test to loosen.
- Live smoke: a real PRD run with a deliberately-introduced violation
  (e.g. a story spec dropping a parity surface without a reason) must be
  caught before the PRD is allowed to complete.
- Federation-Parity Rule checklist for any new `fedCap`-guarded endpoint.
- Mobile-Parity Rule audit for the new PWA surfaces (expected
  reporting-only on mobile, per the table above).

## Files (representative, not exhaustive)

- `internal/autonomous/scan/scan.go`, new
  `internal/autonomous/scan/project_rules.go` — Phases 1-3.
- `internal/config/config.go` (`AutonomousConfig` new fields) — Phase 0.
- `internal/git/github.go` (`CreateIssue`) — Phase 4.
- `internal/autonomous/manager.go`, `internal/autonomous/executor.go`
  (`GuidedMode` real behavior) — Phase 5.
- `internal/server/autonomous.go`, `internal/mcp/bl221_scan.go`,
  `cmd/datawatch/cli_autonomous.go`, `internal/router/sx2_parity.go`,
  `internal/server/web/app.js` — parity surface, all phases.

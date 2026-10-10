# Plan: Configurable Project-Rules Enforcement (BL406)

- **Date**: 2026-10-10
- **Version at planning**: v9.0.10
- **Status**: In progress — Phase 0 started (config scaffolding types added)

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
- **B113** (found starting Phase 0 implementation): `scan.Config` has
  zero YAML persistence and is never wired at daemon startup —
  `cmd/datawatch/main.go:3961`'s `amgrCfg := autonomouspkg.Config{...}`
  literal never sets `Scan:`, so it's always the zero-value (all
  scanners disabled) regardless of `autonomous.DefaultConfig()`'s stated
  intent. `SetScanConfig` is only ever called from the REST handler, so
  any operator-enabled scan config is pure in-memory state, silently
  lost on restart. This must be fixed as part of Phase 0, not after it —
  the new fields below ride the same wiring and would inherit the same
  bug otherwise.
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

Each phase lists concrete, checkable items. Check items off as they land
— don't batch the checking to the end — and update the phase's status
line + this doc's own top-level Status/shipped-version note as each
phase ships, per AGENT.md's Planning Rule. A phase with any unchecked
item is not done, regardless of what's been committed elsewhere.

### Phase 0 — Config scaffolding
**Status: In progress.**
- [x] Fix B113: new `ScanConfig`/`ProjectRuleConfig`/`UpstreamRepoConfig`
  mirror types added to `internal/config/config.go` (mirrors
  `scan.Config`, no import cycle — same pattern as `QualityGateConfig`).
  `AutonomousConfig` gained `Scan ScanConfig`, `RulesFile string`,
  `ContextFile string`, `UpstreamRepos []UpstreamRepoConfig`.
- [ ] Wire `cmd/datawatch/main.go:3961`'s `amgrCfg` construction to copy
  `acfgIn.Scan` → `amgrCfg.Scan` (converting the mirror type to the real
  `scan.Config`/`scan.ProjectRule`), and change the construction to
  start from `autonomouspkg.DefaultConfig()` with fields overridden so
  `scan.DefaultConfig()`'s all-on intent is actually reached at startup
  (closes B113's startup-defaults half).
- [ ] Persist `SetScanConfig` REST writes back into the YAML-facing
  config (not just in-memory `Manager.cfg`) — closes B113's
  restart-survives-a-write half.
- [ ] New `scan.ProjectRule{ID, Name, Type, Granularity, Pattern,
  Severity, Action, UpstreamTarget}` real type in
  `internal/autonomous/scan` (the config-layer mirror above already
  exists; this is the runtime type it converts into) + CRUD store.
- [ ] REST: `GET/PUT /api/config` round-trips the new fields.
- [ ] MCP: `autonomous_scan_config_get/set` extended with the new fields;
  new `scan_rule_list/get/create/update/delete` tools.
- [ ] CLI: `datawatch config set autonomous.rules_file <path>` etc. work
  (generic config-set path — confirm no special-casing needed).
- [ ] Comm: `configure autonomous.rules_file=...` works (same generic
  path check as CLI).
- [ ] PWA: Settings → Automate scan-config card gains the new fields +
  a rule-list sub-view (full CRUD can land in Phase 1 alongside the real
  engine; Phase 0 just needs the config fields visible/settable).
- [ ] Unit tests: config round-trip (YAML → struct → YAML), the
  `amgrCfg` copy bridge includes `Scan`, `SetScanConfig` persists.
- [ ] Live smoke: set a scan config value via REST, restart the daemon,
  confirm it survived (the literal B113 regression test).

### Phase 1 — Rule engine core
**Status: Not started.**
- [ ] `ProjectRulesScanner` implementing the existing `Scanner`
  interface; registered alongside `sast`/`secrets`/`deps` in
  `scan.Run`'s scanner list — no new orchestration path.
- [ ] Content/keyword rule type implemented + tested.
- [ ] Cross-file-consistency rule type implemented + tested.
- [ ] Presence-check rule type implemented + tested.
- [ ] Dogfood: encode this repo's own "version sync both files" lesson
  (`feedback_version_sync` session memory) as a real cross-file-
  consistency rule; confirm it actually catches a deliberately
  introduced mismatch.
- [ ] Unit tests: one passing + one deliberately-failing fixture per
  rule type.

### Phase 2 — Parity-inheritance rule (first built-in)
**Status: Not started.**
- [ ] Rule ships default-enabled once Phase 1 exists.
- [ ] Checks a story's Parity surface section against its parent plan's
  surface list + per-surface exclusion reasons, per AGENT.md's existing
  prose (quoted in Confirmed findings above).
- [ ] Regression test: a story that narrows/drops a surface with no
  stated reason must fail the scan.

### Phase 3 — Multi-granularity wiring
**Status: Not started.**
- [ ] New executor hook at task-complete.
- [ ] New executor hook at story-complete.
- [ ] New executor hook at PRD-complete.
- [ ] Each hook runs only rules declared at its granularity (not every
  rule at every hook).
- [ ] Tests: a task-granularity rule fires at task-complete and not at
  story/PRD-complete, and vice versa for the other granularities.

### Phase 4 — Upstream issue-filing action
**Status: Not started.**
- [ ] `GitHub.CreateIssue(ctx, opts IssueOptions) (string, error)` —
  sibling to the existing `OpenPR`, same `run(ctx, args...)` → `gh issue
  create` pattern.
- [ ] `file_upstream_issue` action wired to Phase 1's rule engine,
  resolving its target against `autonomous.upstream_repos`.
- [ ] Test: a firing rule with `Action: file_upstream_issue` calls
  `CreateIssue` with the right repo/title/body (fake `GitHub.Provider`).

### Phase 5 — Fix B111 and B112 for real
**Status: Not started.**
- [ ] `PRD.GuidedMode bool` → `PRD.GuidedModeSource string` (one of
  `operator`/`council`/`guardrail_auto`; empty = feature off, preserving
  today's no-op for existing PRDs with `guided_mode: true` persisted
  under the old shape — migrate on read, don't break existing records).
- [ ] `operator` source: executor actually pauses before each task and
  waits for an approval call (the original BL384 intent — the part that
  was never built).
- [ ] `council` source: executor pauses and dispatches to a
  `CouncilProfile` (BL405 Phase 8) for a consensus go/no-go instead of a
  human.
- [ ] `guardrail_auto` source: executor continues automatically when
  quality gates + this plan's rule checks pass; on failure, retries or
  resets with the failure fed back into the task's context as new
  information (not a blind retry).
- [ ] REST/MCP/CLI/comm/PWA parity for the source selector (extends the
  existing `set_guided_mode` surfaces, doesn't add new ones).
- [ ] B112: scope-drift re-implemented as a first-class Phase 1 rule
  (content/keyword type, task-granularity) — remove its prose-only
  status in AGENT.md once the real rule exists; cross-reference instead.
- [ ] Regression test: a task spec containing "Implement ..."/"Write
  code for ..." under a doc-only PRD is caught by the new rule.
- [ ] Update B111/B112's entries in `docs/plans/README.md` from "fixed
  as part of BL406 Phase 5" to a real shipped-version note once this
  phase ships.

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

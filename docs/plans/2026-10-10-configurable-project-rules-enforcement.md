# Plan: Configurable Project-Rules Enforcement (BL406)

- **Date**: 2026-10-10
- **Version at planning**: v9.0.10
- **Status**: In progress — Phase 0 done (v9.0.11), Phase 1 done (v9.0.12, real rule engine), Phase 2 done (v9.0.13, first built-in rule), Phase 3 done (v9.0.14, rules fire automatically); Phase 4 next

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

**Every phase also carries AGENT.md's Phase Completion Checklist**
(Planning Rules §5 — added 2026-10-10 after Phase 0 shipped missing
several items on its first pass; see Phase 0's retrofitted checklist
below for what that looked like in practice). Phases 1-5 include it
from the start; don't treat "functionality implemented + unit tests
pass" as done without it.

### Phase 0 — Config scaffolding
**Status: Done.** Shipped on `main` 2026-10-10 (pre-v9.0.10 patch chain,
not yet tagged).
- [x] Fix B113: new `ScanConfig`/`ProjectRuleConfig`/`UpstreamRepoConfig`
  mirror types added to `internal/config/config.go` (mirrors
  `scan.Config`, no import cycle — same pattern as `QualityGateConfig`).
  `AutonomousConfig` gained `Scan ScanConfig`, `RulesFile string`,
  `ContextFile string`, `UpstreamRepos []UpstreamRepoConfig`.
- [x] Wire `cmd/datawatch/main.go`'s `amgrCfg` construction to copy
  `acfgIn.Scan`/`RulesFile`/`ContextFile`/`UpstreamRepos` (new
  `scanConfigFromYAML`/`upstreamReposFromYAML` helpers at end of
  main.go), and added `scanConfigIsUnset` so `scan.DefaultConfig()`'s
  all-on intent is reached when the operator has never touched any
  scan knob, while any single explicitly-set field (including an
  explicit disable) is preserved exactly — closes B113's
  startup-defaults half.
- [x] Persist `SetScanConfig` REST writes back into the YAML-facing
  config (`internal/server/autonomous.go`'s PUT handler now mirrors the
  same body-key parsing onto `s.cfg.Autonomous.Scan` and calls
  `s.saveConfig()`) — closes B113's restart-survives-a-write half.
  Logs a warning (doesn't fail the request) if the YAML save errors, so
  the in-memory change still takes effect even if persistence hiccups.
- [x] New `scan.ProjectRule{ID, Name, Type, Granularity, Pattern,
  Severity, Action, UpstreamTarget}` real type (+ `RuleType`/
  `RuleGranularity`/`RuleAction`/`UpstreamRepo`) in
  `internal/autonomous/scan/scan.go`. `project_rules` added to
  `SetScanConfig`'s body parsing as a full-replace (not merge) list —
  CRUD store is Phase 1's job (needs the real engine to act on rules
  the store holds; this phase only needed the type + round-trip).
- [x] REST: `GET/PUT /api/config` round-trips `rules_file`/
  `context_file`/`upstream_repos` (new `applyConfigPatch` cases +
  `handleGetConfig` map entries). `scan` is GET-exposed read-only,
  matching the existing `default_quality_gates` precedent — its write
  path stays the dedicated `/api/autonomous/scan/config` endpoint
  (already fixed for persistence above), not duplicated into the
  generic patch.
- [x] MCP: `rules_file`/`context_file`/`upstream_repos` already fully
  work via the existing generic `config_set` tool (dot-path →
  `PUT /api/config`, tries raw-JSON then quoted-string — zero new code
  needed). `autonomous_scan_config_set` extended with a `project_rules`
  JSON-array string param (full-replace, same semantics as the REST
  body key).
- [x] CLI: `datawatch config set autonomous.rules_file <path>` etc.
  confirmed already generic (same raw-then-quoted dot-path pattern as
  MCP's `config_set`, same `PUT /api/config` target) — zero new code.
- [x] Comm: `configure autonomous.rules_file=...` confirmed already
  generic (`handleConfigure` has no per-key allowlist) — zero new code.
- [x] PWA: Settings → Automate autonomous-config panel gains Rules
  file / Context file (text inputs) and Upstream repos (JSON textarea)
  rows, using the existing `saveGeneralField` → `PUT /api/config` path.
  Full rule-list CRUD UI deferred to Phase 1 (needs the real engine to
  exist first). 182/182 existing `node --test` JS tests still pass.
- [x] Unit tests: `TestApplyConfigPatch_BL406ProjectRulesFields`
  (REST round-trip), `TestScanConfigIsUnset`/
  `TestScanConfigFromYAML_PreservesExplicitDisable`/
  `TestScanConfigFromYAML_ProjectRulesConvert`/
  `TestUpstreamReposFromYAML` (`cmd/datawatch/bl406_scan_bridge_test.go`
  — the B113 regression case specifically: an explicit disable must
  never be silently re-enabled). Full repo test suite green (3369
  tests, 83 packages).
- [x] Live smoke: real sandbox daemon (own data dir, port 18091),
  confirmed cold start reaches the all-on default on BOTH
  `/api/autonomous/scan/config` and the generic `/api/config` mirror
  (they'd diverged in an earlier version of this fix — see below), PUT
  a change (disable SAST + add a project rule), confirmed it landed in
  the YAML file, `kill -9` + real process restart, confirmed both
  survived exactly as set. **This caught a real bug before it shipped**:
  the PUT handler's first version re-derived the new value from the
  raw request body and mutated `s.cfg.Autonomous.Scan` directly — but
  that's a SEPARATE copy from the Manager's own config, which never
  received the startup default (only `amgrCfg.Scan`, inside
  `cmd/datawatch/main.go`, did). Saving that still-zero-valued copy
  with only the one touched field set produced a YAML block missing
  every other field (including the true defaults) to `omitempty`.
  Fixed two places: (1) the PUT handler now round-trips through JSON
  from `GetScanConfig()` (the Manager's own current, correctly-
  defaulted value — the single source of truth) instead of re-deriving
  a second copy from the request; (2) `cmd/datawatch/main.go`'s startup
  default-fallback now syncs the defaulted value back onto
  `cfg.Autonomous.Scan` too, so the two copies can't diverge even
  before any PUT ever happens. Full suite re-confirmed green after the
  fix (3369 Go tests, 83 packages).

**Phase Completion Checklist** (AGENT.md Planning Rules §5 — retrofitted
2026-10-10 after the gap was found; every later phase does this from the
start, not after the fact):
- [x] `go test ./...` + `node --test` both green (3369 Go tests, 182 JS).
- [x] `CHANGELOG.md` — v9.0.11 entry added.
- [x] `docs/config-reference.yaml` — full `autonomous.scan`/`rules_file`/
  `context_file`/`upstream_repos` block added (also closed a pre-existing
  gap: `scan:` itself, from BL221 Phase 3/v6.2.0, was never documented
  there either).
- [x] `docs/operations.md` — **N/A**: that doc's scope is service
  lifecycle (start/stop/restart/boot), not per-feature config; this
  phase doesn't touch deployment or security posture.
- [x] `README.md` — **N/A**: no functioning user-visible feature yet
  (the new fields are inert until Phase 1-2 ship real rule-checking);
  revisit when Phase 1-2 land.
- [x] Documentation index — N/A, no new doc files.
- [x] `docs/testing-tracker.md` — entry added (BL406 Phase 0 row).
- [x] No internal tracker IDs in user-facing PWA text — confirmed
  (Settings labels/tooltips say "Rules file"/"Context file"/"Upstream
  repos," never "BL406").
- [x] **Mobile-Parity Rule** — triggered (API contract change to
  `/api/config` + `/api/autonomous/scan/config`; new PWA affordances).
  Filed [`datawatch-app`#246](https://github.com/dmz006/datawatch-app/issues/246).
- [x] Localization Rule — 3 new tooltip keys in all 5 locale bundles,
  confirmed by `TestLocales_AllAppJSKeysExistInEnglishBundle`.
- [x] Version bumped both files (`9.0.10` → `9.0.11`) — this phase is
  its own shippable unit.
- [x] `docs/mcp.md` — **N/A for this phase specifically**: confirmed
  `autonomous_scan_config_get/set` (and the whole BL221 scan-tool
  family) were never documented there at all — a large, pre-existing,
  project-wide gap (the doc covers 43 of ~390 real tools), not something
  proportionate to fix as a side effect of adding one param to an
  already-undocumented tool. Worth its own backlog item if a full
  `docs/mcp.md` audit is ever prioritized — not filed as one yet.
- [x] This plan doc's own phase-status line updated to reflect reality.
- [x] Conditional docs (`docs/datawatch-definitions.md`, `docs/flow/*.md`,
  `docs/howto/*.md`) — **N/A**: this phase introduces no new operator-
  facing concept or data flow yet, just inert config fields nothing
  reads. Revisit at Phase 1-2, where "project rules" becomes a real,
  usable thing — that's where a definitions entry and likely a flow
  diagram (rule → finding → action, at task/story/PRD-complete) belong.

### Phase 1 — Rule engine core
**Status: Done.** Shipped v9.0.12.
- [x] `ProjectRulesScanner` (`internal/autonomous/scan/project_rules.go`)
  implementing the existing `Scanner` interface; registered alongside
  `sast`/`secrets`/`deps` in `internal/autonomous/manager.go`'s PRD
  scan path (fires whenever `len(sc.ProjectRules) > 0`, independent of
  the three boolean toggles — project rules aren't security scanners)
  — no new orchestration path. Deliberately left
  `guardrail_registry.go`'s separate single-scanner guardrail path
  untouched — extending it is Phase 3's job (multi-granularity
  checkpoints), not this phase's.
- [x] Content rule type (`"<glob>|<regex>"`) implemented + tested.
- [x] Consistency rule type (`"<fileA>|<fileB>|<regex-with-1-group>"`)
  implemented + tested, including the "pattern not found in either
  file" case.
- [x] Presence rule type (`"<glob>"`) implemented + tested.
- [x] A malformed rule (bad pattern syntax, wrong field count, unknown
  type) produces one self-reporting finding rather than crashing the
  scan — tested for all 5 ways a rule can be malformed.
- [x] Dogfood: encoded this repo's own "version sync both files" lesson
  (`feedback_version_sync` session memory) as a real consistency rule;
  confirmed via a scratch test against a live copy of this repo's
  actual `cmd/datawatch/main.go`/`internal/server/api.go` — 0 findings
  synced, 1 finding after a deliberate mismatch (scratch test deleted
  after manual verification, not part of the committed suite, since
  pointing a permanent unit test at this repo's own live files would be
  fragile against future refactors).
- [x] Unit tests: 7 in `internal/autonomous/scan/project_rules_test.go`
  — one passing + one deliberately-failing fixture per rule type, plus
  the malformed-rule and `parity`-no-op cases.
- [x] **Phase Completion Checklist**: `go test`/`node --test` green
  (3376 Go tests, up from 3369; 182 JS unchanged — no PWA surface this
  phase). `CHANGELOG.md` v9.0.12 entry added. `docs/config-reference.yaml`
  — no new field (Phase 0 already documented `project_rules`'s shape);
  N/A. `docs/operations.md`/`README.md`/doc index — N/A, same reasoning
  as Phase 0 (still no PWA/REST-visible behavior change — rules run via
  the existing `autonomous_prd_scan` on-demand path, not a new surface).
  `docs/testing-tracker.md` entry added. No leaked tracker IDs (checked
  the new glossary entry and all finding messages — plain English
  throughout). **Mobile-Parity Rule**: N/A, confirmed — no REST contract
  change, no PWA affordance; `autonomous_prd_scan`'s response shape is
  unchanged (same `Finding` struct, just a 4th possible `Scanner` value).
  Localization Rule: N/A, no new `t('key')` usage. Version bumped
  (`9.0.11` → `9.0.12`). **Conditional docs**: added the
  `docs/datawatch-definitions.md` "Project rule" glossary entry —
  judged this phase (not Phase 2) the point the concept becomes real
  for an operator, since a rule can be written and actually evaluated
  today. This plan's status line updated.

### Phase 2 — Parity-inheritance rule (first built-in)
**Status: Done.** Shipped v9.0.13.
- [x] Rule ships default-enabled: `scan.DefaultConfig()` now includes
  the `parity-inheritance` rule — the only rule any operator gets
  without configuring one.
- [x] Checks a story's Parity surface section against its parent
  plan's surface list, per AGENT.md's existing prose. Implemented as a
  **candidate detector** (a missing surface → a warning-severity
  finding), not a full semantic judge of "was there a stated reason" —
  that nuance deliberately reuses the existing `rules_grader_enabled`
  LLM pipeline (already wired since BL221 Phase 3) rather than trying
  to regex-parse "stated reason," which isn't realistically
  deterministic. `internal/autonomous/bl406_parity_context.go` (new)
  translates `PRD.Spec`/`PRD.Story` into the scan package's generic
  `PRDParityContext`/`StoryParityInfo` (can't import PRD/Story types
  directly — import cycle — same "mirror, don't import" pattern as
  every other config bridge in this codebase). `ProjectRulesScanner`
  gained an optional `PRDContext` field, consulted only by this rule
  type; a scan without it reports that explicitly (`SeverityInfo`), not
  a crash or silent skip.
- [x] Regression test: `TestProjectRulesScanner_ParityRule_DroppedSurface_Fails`
  — a story that drops 3 surfaces (PWA/Android/iPhone) with no stated
  reason produces exactly 3 findings, each naming the drifted story.
  Plus: a fully-compliant story passes clean, a multi-story case
  confirms only the actually-drifted story gets flagged, and a
  dedicated `REST`-word-boundary test guards against false-positiving
  on "arrest"/"interest" (14 new tests total across
  `project_rules_test.go` and the new `bl406_parity_context_test.go`).
- [x] **Phase Completion Checklist**: `go test`/`node --test` green
  (3387 Go tests, up from 3376; 182 JS unchanged). `CHANGELOG.md`
  v9.0.13 entry added. `docs/config-reference.yaml` updated — the
  `project_rules` example now shows the real default rule instead of
  an empty list. `docs/operations.md`/`README.md`/doc index — N/A,
  same reasoning as Phase 0/1 (no deployment/security change, no new
  REST/PWA surface). `docs/testing-tracker.md` entry added. No leaked
  tracker IDs (checked finding messages and the glossary update — plain
  English). **Mobile-Parity Rule**: N/A, confirmed — same `Finding`
  struct shape as every other scanner, no contract change. Localization
  Rule: N/A, no new `t('key')` usage. Version bumped (`9.0.12` →
  `9.0.13`). **Conditional docs**: updated (not re-added) the
  `docs/datawatch-definitions.md` "Project rule" entry to note the
  parity rule ships default-enabled. Considered a `docs/flow/*.md`
  diagram (rule → finding → action) as a second concrete example;
  deferred — the glossary entry plus this plan's own worked examples
  cover the mechanism adequately for now, and a diagram with only one
  real built-in rule to show wouldn't earn its keep yet. This plan's
  status line updated.

### Phase 3 — Multi-granularity wiring
**Status: Done.** Shipped v9.0.14.
- [x] Task-complete and story-complete checkpoints: **reused the
  existing guardrail hooks** (`runPerTaskGuardrails`/
  `runPerStoryGuardrails`, already firing at exactly those points)
  rather than building new executor hooks — a new built-in guardrail
  entry, `project-rules-scan` (`guardrail_registry.go`), dispatches
  through the same `invokeScanGuardrail` path `sast-scan`/
  `secrets-scan`/`deps-scan` already use. An operator opts in the same
  way as any other guardrail: add `project-rules-scan` to
  `per_task_guardrails`/`per_story_guardrails` (already free-text
  fields — no PWA/config change needed for that surface).
- [x] PRD-complete checkpoint: **new**, since no `PerPRDGuardrails`
  list exists (deliberately — PRD-level checks are the BL117
  orchestrator's territory per `docs/config-reference.yaml`'s own
  existing comment, and building a second generic list wasn't needed
  for what this phase actually required). `runPRDCompletionProjectRulesCheck`
  is a direct, self-gating call wired right before the existing
  `PRDCompleted` rollup in `executor.go` — zero cost when no rule is
  declared at `prd_complete`; a blocking finding flips the PRD to the
  already-existing `PRDBlocked` status and records a
  `project_rules_block` `Decision`.
- [x] Each hook runs only rules declared at its granularity:
  `scan.FilterRulesByGranularity(rules, inv.Level + "_complete")` at
  task/story; `GranularityPRD` at PRD-complete — not every rule at
  every hook.
- [x] Tests: `TestInvokeScanGuardrail_ProjectRules_TaskGranularity_OnlyFiresTaskRule`
  / `..._StoryGranularity_OnlyFiresStoryRule` confirm each level fires
  its own rule; `..._TaskLevel_DoesNotFireStoryOrPRDRule` proves the
  negative (satisfying only the task-rule's target flips the verdict to
  pass, proving story/PRD rules targeting different, still-missing
  files aren't also being evaluated at task granularity);
  `TestRunPRDCompletionProjectRulesCheck_PRDGranularity_Blocks` +
  `..._NoOpWhenNoPRDGranularityRules` cover the PRD checkpoint (5 new
  tests total, `internal/autonomous/project_rules_integration_test.go`).
- [x] **Phase Completion Checklist**: `go test`/`node --test` green
  (3392 Go tests, up from 3387; 182 JS unchanged except the dropdown
  addition below, confirmed no regression). `CHANGELOG.md` v9.0.14
  entry added. `docs/config-reference.yaml` — corrected a pre-existing
  stale comment (it didn't even list the 3 already-existing built-in
  scan guardrails) while adding `project-rules-scan` to the real name
  list. `docs/operations.md`/doc index — N/A, no deployment/security
  change, no new doc files. `README.md` — N/A, still an internal
  quality-gate mechanism, not a new operator command/interface.
  `docs/testing-tracker.md` entry added. No leaked tracker IDs.
  **Mobile-Parity Rule**: triggered and closed — the session
  quick-command "Guardrails" dropdown (`app.js`) had a hardcoded
  3-item list missing the new guardrail name; added
  `project-rules-scan` as a 4th option (no new locale string — the
  option text is the raw guardrail name, never `t()`-wrapped).
  Localization Rule: N/A per the above. Version bumped (`9.0.13` →
  `9.0.14`). This plan's status line updated.
- [x] **Naming correction (operator-raised mid-phase, not originally
  planned)**: this phase's own new file was initially named
  `bl406_parity_context.go` — renamed to `project_rules_integration.go`
  per the new AGENT.md Code Quality Rule (files/functions named for
  what they do, never for the tracker ID that created them). Filed
  **BL409** to audit the rest of the codebase's pre-existing
  BL/GH-prefixed files later, placed last in the v10.0.0 Stage 3 queue
  — not retroactively fixed as a side effect of this phase.

### Phase 4 — Upstream issue-filing action
**Status: Not started.**
- [ ] `GitHub.CreateIssue(ctx, opts IssueOptions) (string, error)` —
  sibling to the existing `OpenPR`, same `run(ctx, args...)` → `gh issue
  create` pattern.
- [ ] `file_upstream_issue` action wired to Phase 1's rule engine,
  resolving its target against `autonomous.upstream_repos`.
- [ ] Test: a firing rule with `Action: file_upstream_issue` calls
  `CreateIssue` with the right repo/title/body (fake `GitHub.Provider`).
- [ ] **Phase Completion Checklist** (AGENT.md Planning Rules §5, full
  list — see Phase 0's worked example above).

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
- [ ] **Phase Completion Checklist** (AGENT.md Planning Rules §5, full
  list — this phase definitely needs the Mobile-Parity check: a new
  guided-mode source selector is a real PWA affordance change).

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

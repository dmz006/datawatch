# Design: diff-aware project-rules enforcement (the "test arrived" check)

- **Date**: 2026-10-22 (ISO 8601)
- **Version at planning**: 9.5.87 (development build; the next release is 10.0.0)
- **Status**: **Phase 0 (this design) Done on merge. Phases 1–3 Planned.** Nothing in this document is implemented. This is a **design-only** deliverable: it changes no code, no configuration, no tests, no `CHANGELOG.md` and no version string.
- **Tracker**: BL413 in `docs/plans/README.md` (row "Diff-aware project-rules enforcement", currently "not started (needs design)"). Updating that row to point at this document is a merge-step follow-up; this task writes only this file.
- **Slug matches**: `*diff-aware-rules*.md`

## Scope

Files and packages the later phases would touch (none are touched by this design):

- `internal/autonomous/` — a new deterministic diff test-coverage check (proposed `diff_test_coverage.go` and `diff_test_coverage_test.go`), small read-only additions beside the existing git helpers in `internal/autonomous/file_drift.go`, a stored record on the Automaton (PRD), calls at the three checkpoints in `internal/autonomous/executor.go` (task verification and PRD completion) and `internal/autonomous/automation_coordinator.go` (`onMemberTerminal`, which feeds `step` in `internal/autonomous/automation_advance.go`).
- `internal/autonomous/guardrail_registry.go` — a library entry so the check is discoverable next to `rules-review` and `ci-watch`.
- `internal/config/` — the new `autonomous.diff_test_*` keys and their normalizers (same shape as `internal/config/plan_coverage.go`).
- `internal/server/`, `internal/mcp/`, `cmd/datawatch/`, `internal/router/`, `internal/server/web/` (REST, MCP, CLI, comm channel, PWA and locales) and `internal/server/web/openapi.yaml`.
- Docs: this plan, `docs/implementation.md`, `docs/config-reference.yaml`, `docs/operations.md`, `docs/testing-tracker.md`, a how-to under `docs/howto/`, `CHANGELOG.md`.

## 1. Purpose and operator decision

The Automation coordinator advances a chain of Automata when a member reaches a terminal status. Today "terminal and completed" is trusted as is. This design adds a **deterministic, no-LLM check that a member whose diff adds or changes non-test source also adds or changes a test**, and gives the coordinator a stored verdict it can act on without running git itself.

The decision this serves is recorded as **Open question 17** in `docs/plans/2026-10-14-automation-orchestration-design.md` ("The coordinator as a real quality/regression gate, not just a status poll"). **Decided by the operator 2026-10-22 (advance gate):** before advancing, the coordinator verifies the member's terminal status is trustworthy — guardrails and verification passed, loud alerts on any blocked or failed member, and **new functionality must come with a specific new test**. That check is BL413 and is in scope for the 10.0.0 release. Open question 17 itself names the mechanism: reuse BL413's diff-aware work "rather than building a second diff-coverage checker". This document is that design pass.

Design goals, in priority order:

1. **Deterministic.** The same branch diff and the same configuration always give the same verdict. No model call in the verdict (the lesson of `docs/plans/2026-10-20-plan-coverage-gate.md`).
2. **Consumable by the coordinator.** `step` in `internal/autonomous/automation_advance.go` is a pure function with no I/O; the verdict must therefore be a stored record the coordinator reads, not something it computes.
3. **Loud, specific and overridable.** A block names the uncovered files; an operator can acknowledge a specific failing surface with a note, exactly like the plan-coverage acknowledgement.
4. **Safe to roll out.** Default `warn`, so the first release measures false positives before anything blocks (see Open question 1).

## 2. The problem and evidence of how work reached done without tests

AGENT.md's **Phase Completion Checklist** requires unit tests for anything new, and its Code Quality Rules require close to 100% coverage with real (not skeleton) tests. Nothing enforces this mechanically at the moment a member is judged done. Three existing mechanisms look adjacent but each leaves the gap open:

- The per-task verifier and quality gate (`runQualityGateTests` in `internal/autonomous/executor.go`) check that the **existing** suite did not regress. A new code path with zero new tests passes an unchanged suite trivially. Open question 17 states this directly: "a new code path with zero new tests can pass the existing suite trivially".
- The `rules-review` guardrail (`internal/autonomous/rules_review.go`) is an LLM judgment over the diff. It is opt-in through the PRD-level guardrail list (empty by default), and its own header explains it was built as a judgment pass because a scripted checklist would hard-code one project's rules.
- The project-rules scan (`internal/autonomous/scan/project_rules.go`) has four rule types — content, consistency, presence, parity — plus scope-drift. The **Correction (2026-10-11)** paragraph of `docs/plans/2026-10-10-configurable-project-rules-enforcement.md` records the exact gap: "presence rules only check 'does a file exist,' not 'was it touched in this diff'". That is why BL413 was filed as its own item.

Specific incidents found while preparing this design:

| Evidence | Where recorded | What it shows |
|---|---|---|
| **Two PRD tasks failed and sat silently blocked for about 50 minutes before the operator, not the coordinator, noticed** (operator-raised 2026-10-15, live incident) | Open question 17 in `docs/plans/2026-10-14-automation-orchestration-design.md` | The advance decision trusted a terminal status and nothing verified it or alerted loudly. This is the incident that motivated the gate. |
| **An empty-diff first attempt passed as already landed** (v9.5.2, PRD `1e79466d`), listed with the wrong-verifier-model fix (v9.5.1, PRD `9d3033b8`) | `docs/plans/README.md`, planning registry row 65 | A member can reach done with no diff at all. A diff-aware check must treat an empty diff as a first-class case, not assume "no diff, nothing to test". |
| **A retry whose work already landed failed with "no changes detected; task produced no output"**, and a missing verification script was reported as empty output (PRD `1d1a0001`, 2026-10-19); and the open item "Retry with already-correct output must not fail on empty diff" (PRD `191d0c95`) | `docs/plans/README.md`, registry row 94 and row 27 | Empty-diff handling has already been wrong in both directions (pass when it should not, fail when it should not). The new check must not add a third behaviour; it reports empty diffs as `no_changes` and never blocks on them alone. |
| **A finished decompose was dropped**: twice a 25 to 38 KB plan was lost and the PRD ended with zero stories, no error; all 8 tasks of the related PRD completed while the completion judge still needed a retry (PRD `93940001`) | `docs/plans/README.md`, registry row 59 (BL478), row 60 (BL479) and row 93 (BL512) | Work and "done" flags can be produced with no mechanical proof of what changed. The completion judge is an LLM and was itself unreliable in those incidents; a deterministic complement is needed. |
| **BL406 Phase 0 shipped missing a CHANGELOG entry, config reference, testing-tracker section and a companion-app issue** | AGENT.md Planning Rules, item 5 | Checklist items were skipped while the phase was marked done. The same failure applies to "tests" and is the reason the check is mechanical rather than a prose reminder. |

The common thread: every layer that exists today either judges a unit against its own narrow spec or asks a model, and none compares **what the branch actually changed** against **what tests arrived with it**.

## 3. What the check inspects

The check is a function of the **diff of a member's branch against its base**, never of the working tree alone. It reads three things from git, all read-only, using the injectable git runner already present in `internal/autonomous/file_drift.go` (`driftGitRunner`, so tests substitute a fake):

1. The changed-file list with status — added, modified, deleted, renamed (`git diff --name-status` with rename detection). `internal/autonomous/file_drift.go` already has `CommittedFilesSince` and `CommittedLineStatsSince`; the new reader extends the same family with the status column.
2. Per-file added and removed line counts (`git diff --numstat`, already parsed by `CommittedLineStatsSince`).
3. For a test file, whether its **added** lines contain a test marker (for Go, a `func Test`, `func Benchmark`, `func Fuzz` or `func Example` line; for JavaScript, a `test(`/`it(`/`describe(` call). This is a text match on added lines only, so a test file touched only by deletions or whitespace does not count.

**Which base.** The diff range depends on the checkpoint:

| Checkpoint | Range | Source of the base |
|---|---|---|
| Task verification | `Task.PreTaskSHA` to `HEAD` in the task's resolved directory | `PreTaskSHA` is already captured before each worker attempt (`internal/autonomous/executor.go`); `Task.BaseSHA` anchors the first attempt so retries are covered |
| PRD completion | Worktree PRDs: merge-base of the base branch (`PRD.Git.BaseBranch`, resolved default branch when empty) and `HEAD` of the Automaton's branch. Non-worktree PRDs: the earliest task `BaseSHA` to `HEAD` | New: a PRD-level base SHA recorded when the first task starts, so a non-worktree PRD has an anchor that survives restarts |
| Automation advance | **No git at all.** The coordinator reads the record stored at PRD completion | The branch or worktree may already be merged or gone when `onMemberTerminal` fires, and boot reconcile in `internal/autonomous/automation_reconcile.go` replays terminal events; both must be free of git I/O |

**Classification.** Each changed path is placed in exactly one bucket, in this order:

1. **exempt** — matches the exemption rules in section 5.
2. **test file** — matches the language adapter's test globs (section 4, mechanism A).
3. **source file** — everything else the adapter recognises as code.
4. **unmapped** — a path the adapter has no rule for (for example an unfamiliar extension). Unmapped files never block; they raise `warn` (section 6).

Deleted files need no test and are ignored as sources. A rename with no content change (git similarity 100%) is ignored; a rename with content changes counts as a modified source at its new path.

**The mapping.** A changed source file is **covered** when the diff also changes or adds a test file the adapter maps to it. Source files with fewer than `autonomous.diff_test_min_changed_lines` added non-blank, non-comment lines are treated as trivial and not required to have a test (so a one-line import tidy does not block). The record lists every source file with its verdict: `covered`, `uncovered`, `trivial`, `exempt`, or `unmapped`.

## 4. Mechanisms

Four options were evaluated. Each is described as it would work in this codebase, with trade-offs.

### A. Path-convention mapping (file or package level)

**How it works.** Per-language adapters declare how a source path maps to test paths. For Go: a changed non-test `*.go` file requires a changed or added `*_test.go` file **in the same directory** (the Go package) at package granularity, or `<stem>_test.go` at file granularity. For JavaScript in this repo: `internal/server/web/<name>.js` maps to `internal/server/web/*.test.js`. Adapters are data: a source glob, one or more test globs (with `{dir}` and `{stem}` placeholders so tests may live elsewhere, for example under `scripts/test-stories`), and the test markers from section 3. The check is a single pass over the changed-file list.

**Trade-offs.**
- Pros: purely deterministic; one `git diff` call; no build or test execution; trivially explainable ("`internal/foo/bar.go` changed, no test changed in `internal/foo`"); matches this repo's own convention, where tests sit beside the code (`file_drift.go` and `file_drift_test.go`); works at all three checkpoints; fails closed to a readable list of files.
- Cons: proves **a test file was touched**, not that the new behaviour is tested; package-level mapping lets one test elsewhere in the package satisfy a large change; needs an adapter per language (unmapped languages only warn).

### B. Function-level mapping from diff hunks

**How it works.** Parse hunk headers (`@@ … @@ func Name`) or, for Go, parse the changed file with `go/parser` to find functions whose lines intersect an added hunk, then require that a changed test file contains a test whose name mentions the function (`TestName`, `TestType_Method`). A new exported function would need a test naming it.

**Trade-offs.**
- Pros: much closer to the operator's wording ("a *specific* new test"); catches a big change covered only by an unrelated one-line test.
- Cons: Go-specific (each language needs its own parser); relies on naming conventions that this repo does not enforce (tests are named for behaviour, not always for the function); hunk headers name only the enclosing function of the hunk start; renames and moves produce noise; a table-driven test covering several functions would false-positive. Higher build cost and a higher false-positive rate, so it is poor as the *first* blocking gate. Good as an opt-in strict mode for Go later.

### C. Coverage-delta based

**How it works.** Run the tests with coverage on the base and on the head (for Go, `go test -coverprofile`), then compute coverage of the **changed lines** only (the "diff coverage" idea). A change passes when changed executable lines are covered above a threshold.

**Trade-offs.**
- Pros: measures execution of the new lines, not just file names; independent of naming conventions.
- Cons: needs a full test run of the project at the member's commit, so it costs minutes per member and needs a working build environment (the quality gate already shows how fragile that is across worktrees); flaky tests make the verdict flaky, which breaks the determinism goal; an **existing** test that merely calls the new code passes the check although no new test arrived, which contradicts the operator's decision ("a specific new test"); every language needs its own coverage tooling; and the repo's own `docs/plans/2026-10-22-flaky-test-scan.md` is a reminder that suite timing is not a safe thing to gate on. Reasonable as an advisory signal after the cheap check, not as the gate.

### D. LLM judge over the diff

**How it works.** Reuse the existing judgment path: a guardrail call (like `rules-review` in `internal/autonomous/rules_review.go`) asks a model to read the diff and say whether the new behaviour is tested.

**Trade-offs.**
- Pros: understands semantics — whether a change needs a test at all, and whether a test is meaningful; no adapters.
- Cons: non-deterministic and costly; its verdict depends on the judge model and the PRD's tooling; and it is the same class of control that **let the evidence incidents through** (the completion judge was unreliable in the rows cited in section 2). A small local model judging a diff is least reliable exactly where the gate matters most. It also cannot be a mechanical gate the coordinator trusts without reading prose.

### Recommendation: A as the core, with B and C as later opt-ins and D kept out of the gate

**Recommended: mechanism A** (path-convention mapping, package granularity by default, file granularity as an option), plus the exemption and test-marker rules.

Justification:

1. It is the only option that is cheap, deterministic and explainable at all three checkpoints, including the Automation advance where git must not be called.
2. It matches the failure that actually occurred: a member reached done with **no test change at all**. A is exactly the check that catches that; B and C refine a question that is not yet the bottleneck.
3. It follows the precedent of the plan-coverage gate: pure function, stored record, fingerprint-bound acknowledgement, `off|warn|block` modes.
4. Its weakness (a touched test file may not test the change) is covered by what already exists — `rules-review` (D, judgment, opt-in) stays as a separate layer, and C can be added later as an advisory.

Sequencing: A ships in 10.0.0 (Phases 1–3 below); B is a Go-only strict option once false-positive data exists; C is an advisory signal at most; D is not part of this check.

**Extension point.** The original BL413 filing was about *diff-aware project rules* in general, not only tests: a rule of the form "when files matching X change in the diff, files matching Y must also change" also expresses CHANGELOG, `docs/config-reference.yaml` and version-sync rules. The design keeps that door open: the changed-file list reader is shared, and a generic rule type for `internal/autonomous/scan/project_rules.go` could consume the same list. It is **not** built in this scope (Open question 8).

## 5. Exemptions

Not every diff needs a test. Exemptions are applied first (section 3, bucket 1). A path is exempt when it matches any of:

- **Docs-only:** `*.md`, `docs/**`, `LICENSE`, `*.txt`.
- **Generated files:** a file whose first lines contain a generated-code marker (for Go, `Code generated … DO NOT EDIT`), plus configured globs (for example `**/*.pb.go`). The marker is read from the file's added or first lines at check time, never by the model.
- **Configuration and data:** `*.yaml`, `*.yml`, `*.json`, `*.toml`, `Makefile`, `.github/**`, locale bundles under `internal/server/web/locales/`, `go.mod` and `go.sum`.
- **Test support:** files under a `testdata/` directory.

How they are declared (three layers, same precedence shape as `resolveBlockOnFileDrift` in `internal/autonomous/file_drift.go`):

1. **Built-in defaults** (above) shipped with the binary.
2. **`autonomous.diff_test_exempt_globs`** — operator list, additive to the built-ins, settable on every surface in section 8.
3. **Per-task declaration:** a task that legitimately needs no tests (a pure refactor with the existing suite as proof, a design document) carries a `no tests needed` reason, a non-empty string recorded in the task spec at decompose time or set by `edit_task`. The reason is stored on the record and audited; an empty reason is rejected, as in the plan-coverage acknowledgement.

A diff in which **every** changed path is exempt passes with the note `all changes exempt`. This design document's own PRD is an example: it changes only Markdown.

## 6. Where the check runs and what it returns

**Results.** Three values:

- `pass` — every non-exempt, non-trivial source file is covered, or every change is exempt.
- `warn` — recorded and shown, nothing blocked. Used for: mode `warn`, unmapped files, empty diff (`no_changes`), and an infrastructure failure of the check itself.
- `block` — one or more source files are uncovered and the mode is `block`, and no acknowledgement matches the failing surface. The `reason` string **names the specific uncovered files**, for example `diff_test_uncovered: internal/foo/bar.go (+42 lines, no test change in internal/foo); internal/baz/qux.go (+9 lines, no test change in internal/baz)`. At most 10 files are listed, then `and N more`; the full list is in the record.

**Record.** A `DiffTestCoverage` record stored on the PRD (same persistence path as the plan-coverage record): mode used, verdict, `base_sha`, `head_sha`, the per-file table, the reason, a **fingerprint** (stable hash of the sorted uncovered files and mode, so an acknowledgement cannot cover a different failure), and the acknowledgement (actor, non-empty note, time).

**Checkpoint 1 — task verification.** Immediately after `checkUndeclaredEdits` in `internal/autonomous/executor.go`, once per worker attempt. The task-level result is recorded as a Decision and shown, but **never blocks** (tests may legitimately arrive in a later task of the same story). It can still raise `warn`. See Open question 3.

**Checkpoint 2 — PRD completion.** In the completion rollup in `internal/autonomous/executor.go`, directly after `runPRDCompletionProjectRulesCheck` and before the judgment-based `runPRDCompletionRulesReview`: deterministic checks run before model calls. `block` sets the PRD to `PRDBlocked` with reason `diff_test_block` through the same `commitRollupStatus` path, and appends a `diff_test_block` Decision. The record is computed here, while the branch or worktree still exists.

**Checkpoint 3 — Automation advance.** In `onMemberTerminal` (`internal/autonomous/automation_coordinator.go`), after `prdEnd(status)` and before `c.advance`. Because `step` is pure, the coordinator reads the stored record and **rewrites the member's end** before calling `step`; `step` itself gains no I/O:

- verdict `block` (and not acknowledged): turn the `completed` end into the existing `direct` form (`endOutcome{memStatus: MemberBlocked, outcome: "completed", direct: "<reason naming files>"}`). `step` already handles `direct` by finishing the member and calling `block`, and `afterAdvance` already raises the blocked alert and emits the Automation-level terminal event. The operator gets the same loud alert path used for any blocked Automation.
- verdict `warn`: advance normally; carry the note into the advance Decision and raise a warn alert through the existing alert path.
- a **fork branch** needs separate handling: in `step`, a branch ignores `direct` and `edge` and records the end on the join, which counts only `success`. For a branch the rewrite must therefore also set `success=false` and a `note` naming the files, so the join rule sees a failed branch.
- no record (a member that completed before this feature, or mode `off`): advance as today. Boot reconcile uses the same read, so a replayed terminal event gives the same decision with no git call.

The record, not the live diff, is the single source of truth at checkpoint 3, so the verdict is the same whether the event arrives live, from `reconcile`, or after a daemon restart.

**Relationship to existing guardrail resolution.** The mode follows the three-tier shape already used by `resolveBlockOnFileDrift` (PRD override, then guardrail profile, then global config) rather than the guardrail **name list** of `resolveGuardrails` in `internal/autonomous/guardrail_registry.go`. Reason: name lists default to empty, so a check that depends on being listed is silently off, which is how `rules-review` ended up opt-in. A library entry `diff-test-check` is still registered for discoverability (`guardrail_library_list`), but dispatch is direct and self-gating on the mode, like `runPRDCompletionProjectRulesCheck`.

## 7. Configuration keys and defaults

All under `autonomous.*`, consistent with `autonomous.plan_coverage_check`, `autonomous.drift_mode` and `autonomous.undeclared_edit_mode`. Every key must be settable on every configuration surface (AGENT.md Configuration Accessibility Rule: YAML, web Settings, REST, comm `configure`, MCP, CLI) and round-trip through `PUT /api/config`.

| Key | Values | Default | Notes |
|---|---|---|---|
| `autonomous.diff_test_check` | `off` \| `warn` \| `block` | **`warn`** | Explicit choice: **default-warn**, not default-off and not default-block. `off` still records nothing. Unknown values are a config error. See Open question 1 for the pre-10.0.0 flip. |
| `autonomous.diff_test_granularity` | `package` \| `file` | `package` | Mechanism A granularity. |
| `autonomous.diff_test_min_changed_lines` | integer >= 0 | `3` | Added non-blank, non-comment lines below which a source file is trivial. |
| `autonomous.diff_test_exempt_globs` | list of globs | empty (built-ins always apply) | Additive. |
| `autonomous.diff_test_mappings` | list of `{source_glob, test_globs, test_markers}` | empty (built-in Go and JavaScript adapters always apply) | Operator-added adapters. |

Per-Automaton override: `diff_test_check` pointer on the PRD and on the guardrail profile, resolved PRD, then profile, then global, as in `resolveBlockOnFileDrift`.

Normalizers live beside `internal/config/plan_coverage.go`. As in that file, none of these fields uses `omitempty`, so an explicit `off` or `0` survives the REST config round trip.

**Documentation requirement.** Per the Code Quality Rules, every new config field **must** have an entry in `docs/implementation.md` when it is implemented (the drift-mode and undeclared-edit-mode entries there are the model). The same phase also adds `docs/config-reference.yaml` entries and `docs/operations.md` text.

## 8. Operator surface

Shapes follow `docs/plans/2026-10-20-plan-coverage-gate.md` (record read on the PRD view, acknowledge as an explicit operator action) and the existing scan read verbs.

| Surface | Read the result | Act on it |
|---|---|---|
| **REST** | `GET /api/autonomous/prds/{id}/diff_test_coverage` (the stored record); the same record as `diff_test_coverage` in `GET /api/autonomous/prds/{id}?detail=full`; each member in `GET /api/automations/{id}` carries its `diff_test_verdict` | `POST /api/autonomous/prds/{id}/diff_test_coverage` (recompute from the branch, like `POST …/scan`); `POST /api/autonomous/prds/{id}/diff_test_coverage/acknowledge` (actor plus non-empty note, fingerprint-bound). Config: `GET`/`PUT /api/autonomous/config` and dotted `PUT /api/config` |
| **MCP** | `autonomous_prd_diff_test_coverage` (read; `recheck` flag recomputes); `automation_get` (member verdict) | `autonomous_prd_diff_test_coverage_acknowledge` (id, note); `autonomous_config_set` args for the keys. Capability grouping as the plan-coverage tools (`internal/federation/mcp_tool_caps.go`) |
| **CLI** | `datawatch autonomous prd-diff-tests <id>`; `datawatch automation get <id>` shows member verdicts | `datawatch autonomous prd-ack-diff-tests <id> --actor … --note …`; `datawatch config set autonomous.diff_test_check=…` |
| **Comm channel** | `autonomous diff-tests <prd-id>` (sibling of `autonomous scan-results <prd-id>` in `internal/router/sx2_parity.go`); `automation status <id>` includes the verdict | `configure autonomous.diff_test_check=…`. **No acknowledge verb**, mirroring the plan-coverage precedent (a gate override with a required note stays on explicit surfaces); see Open question 7 |
| **PWA** | PRD panel showing the table of files and verdicts; verdict badge on each member in the Automation pipeline strip | Acknowledge control posting to REST; Settings fields for the five keys; locale keys in all five bundles |
| **Android / iPhone** | Same REST fields; companion-app issue filed per the Mobile-Parity Rule (capability parity, not implementation parity) | Same |

**Alerts and audit.** A `block` at the Automation level raises an `error` alert through the existing blocked path, naming the files; a `warn` raises a `warn` alert. Audit events `autonomous_diff_test_checked` and `autonomous_diff_test_acknowledged` carry the record id, verdict, files and actor. Operator-facing copy says "Automaton"/"Automation"; tracker IDs appear only in plan documents and code comments.

## 9. Failure modes and false positives

| Case | Behaviour | Mitigation |
|---|---|---|
| **Generated code** (protobuf, bundled assets, code with a generated header) | Would look like uncovered source | Header-marker exemption plus `autonomous.diff_test_exempt_globs`; the marker is checked in the file itself |
| **Renames and moves** | A pure rename would look like a new source file | Rename detection: similarity 100% is ignored; a rename with edits counts at the new path, and a moved test file counts as a test file |
| **Shared test files / one test for several files** | File granularity false-positives when one test covers several sources | Default is package granularity; file granularity is opt-in |
| **Tests in another place** (E2E stories, smoke scripts, `scripts/test-stories`) | A change covered by an integration test elsewhere looks uncovered | `test_globs` in an adapter; the operator acknowledges a one-off with a note |
| **Non-Go packages** (JavaScript, shell, Python, future languages) | No adapter means no verdict | Built-in Go and JavaScript adapters; unknown extensions become `unmapped` and only `warn`; operators add adapters via `autonomous.diff_test_mappings` |
| **A member that legitimately needs no tests** (pure refactor, docs, design) | Would block | Docs and config exemptions; per-task `no tests needed` reason (recorded, audited) |
| **Empty diff** (an attempt that "already landed", per the evidence in section 2) | Nothing to check | Reported as `no_changes` with `warn`; never blocks alone and never converts an empty diff into a pass silently |
| **Test file touched but not meaningful** | Passes mechanism A | Test-marker rule (added lines must contain a test marker); the remaining gap is what `rules-review` and later B and C address. This is a known limit, stated rather than hidden |
| **Deleted tests with added source** | Test file changed, but only by deletion | Only **added** lines count as test arrival |
| **Trivial source change** | Would demand a test for an import tidy | `diff_test_min_changed_lines` |
| **Infrastructure failure** (git error, missing base SHA, unreadable file) | Not a content failure | Classified as infra, not content (the BL410 failure categories): recorded as `warn` with an error alert, never `block`, one retry first. A silent skip is the failure this design exists to avoid, so the alert is mandatory. See Open question 6 |
| **Very large diffs / binary files** | Cost and noise | Binary entries count as 0 lines (as in `CommittedLineStatsSince`); a configurable cap on files listed in the reason; the record keeps the full table |
| **Member in a different project directory** (per-task `project_dir` override) | The base lives in another repo | The check runs per task directory using that directory's base SHA; the PRD record merges per-directory results |
| **Replay / restart** | Re-running git after a merge could give a different answer | The coordinator reads the stored record only; the record is computed once at PRD completion and recomputed only by an explicit `recheck` |

## 10. Phased build plan

Phases follow the operator's order: design, deterministic core, integration into the three checkpoints, surfaces. Each phase's checklist includes AGENT.md's full **Phase Completion Checklist**. Items that do not apply say so with a reason; nothing is skipped silently.

### Phase 0 — Design

**Status: Done on merge** (this document). No version bump; docs-only commits get no version bump or versioned CHANGELOG entry.

- [x] Functionality implemented per the phase's own description: this document, with the mechanisms compared and a recommendation.
- [x] Unit tests: N/A — documentation only. Mechanical check: `sh .datawatch/verify/verify-bl413-design-doc.sh` confirms every required section is present.
- [x] Live/smoke test: N/A — no persistence, security or capability gating is touched.
- [x] `go test ./...` and `node --test internal/server/web/*.test.js`: N/A — no Go or web file changes in this phase.
- [x] General documentation checklist (8 items): N/A except no leaked tracker IDs in operator-facing text — this is an internal plan document, where tracker IDs belong; `CHANGELOG.md`, `docs/config-reference.yaml`, `docs/operations.md`, `README.md`, the doc index, `docs/testing-tracker.md` are N/A for a design-only phase; the Parity surface section is section 8 above.
- [x] Conditional docs: N/A — no operator-facing concept ships yet.
- [x] Mobile-Parity Rule: N/A — no operator-visible change.
- [x] Localization Rule: N/A — no `t('key')` usage.
- [x] Version bump: N/A — not a shippable unit.
- [x] Phase-status line updated: Phase 0 Done on merge; the planning-registry row for BL413 is updated by the merge step.

### Phase 1 — Deterministic core

**Description.** A pure function (proposed `CheckDiffTestCoverage`) over a list of changed files (status, added lines, test-marker presence) and the configuration, returning the record. The git reader for the changed-file list beside the helpers in `internal/autonomous/file_drift.go`. Classification, exemptions, adapters (Go, JavaScript), min-lines rule, fingerprint, acknowledgement, and the config normalizers in `internal/config/`. No integration, no surfaces.

**Status: Planned.**

**Test strategy.**
- Unit tests, near-100% coverage of the new code, in `internal/autonomous/diff_test_coverage_test.go`.
- **Deterministic fixtures of branch-diff scenarios**, each a small table row (changed files in, verdict and reason out), plus the same scenarios built as real throwaway git repositories using the `driftTestRepo` / `driftGit` helpers pattern in `internal/autonomous/file_drift_test.go`: (1) source and same-package test changed — pass; (2) source only — block, reason names the file; (3) test only — pass; (4) docs only — pass, all exempt; (5) generated-header file — exempt; (6) pure rename — ignored; (7) rename with edits — counts; (8) deleted source — ignored; (9) JavaScript pair; (10) unknown extension — `unmapped` warn; (11) test file changed by deletions only — still uncovered; (12) empty diff — `no_changes` warn; (13) test in an adapter's other directory; (14) per-task `no tests needed` reason; (15) trivial change under the min-lines threshold; (16) fingerprint stable and acknowledgement not reusable for a different failing set; (17) determinism — same input, same output, repeated.
- Config normalizer tests: empty to default, unknown mode rejected, round trip of explicit `off` and `0`.
- Live smoke: N/A for this phase (pure functions); the live run is in Phase 2.

Phase Completion Checklist:

- [ ] Functionality implemented per the phase's own description.
- [ ] Unit tests added/passing for anything new (fixtures above).
- [ ] Live/smoke test: N/A (no persistence, security or gating yet) — recorded with that reason.
- [ ] `go test ./...` and `node --test internal/server/web/*.test.js` both green.
- [ ] General documentation checklist (8 items): `docs/implementation.md` entries for the new keys (**required**: new config fields), config-reference scaffolding, the others N/A until the operator-visible phase; no leaked tracker IDs in names (names are by function, for example `CheckDiffTestCoverage`; the tracker ID appears only as a comment-level cross-reference).
- [ ] Conditional docs: N/A — the concept is not yet reachable.
- [ ] Mobile-Parity Rule: N/A — no operator-visible change yet.
- [ ] Localization Rule: N/A — no `t('key')` yet.
- [ ] Version bump in both `cmd/datawatch/main.go` and `internal/server/api.go`: state explicitly whether this phase is a tagged unit (expected: not).
- [ ] Phase-status line updated with the shipped version noted.

### Phase 2 — Integration at the three checkpoints

**Description.** Record a PRD-level base SHA when the first task starts; call the check at task verification (record only), at PRD completion (block through `commitRollupStatus`), and read the stored record in `onMemberTerminal` to rewrite the member's end (block and warn, including the fork-branch case). Stored `DiffTestCoverage` on the PRD; per-task `no tests needed` reason; per-PRD and per-profile mode override; Decisions, audit events and alerts. One bounded corrective attempt (Open question 5) if chosen.

**Status: Planned.**

**Test strategy.**
- Unit tests with the fake git runner: task-level result is recorded and never blocks; PRD completion with `block` sets `PRDBlocked` and reason `diff_test_block`; `warn` completes; `off` records nothing.
- Coordinator tests in the style of `internal/autonomous/automation_coordinator_test.go` (which calls `onMemberTerminal` directly): a `completed` member with a stored `block` blocks the Automation with the file names in the reason and raises the blocked alert; an acknowledged record advances; `warn` advances with a note; a fork branch with a `block` verdict counts as failed in the join; a missing record advances as before; a replayed event through `reconcile` gives the same result with **no git call** (assert the fake runner was never invoked).
- Deterministic branch-diff fixtures from Phase 1 re-used end to end: build a worktree branch with each scenario, complete the PRD, assert the stored record.
- **Live smoke against a real Automation advance** on the sandbox daemon (per AGENT.md's E2E rules, never the production daemon): a two-member Automation whose first member is a scripted one-shot that adds a Go function with no test. Expected: Automation `blocked`, alert raised, reason names the file, record readable on REST. Then acknowledge with a note and observe the advance; then a second run where the first member also adds a test and advances with no operator action. Record the run in `docs/testing-tracker.md`; mark Validated only if it was actually performed.

Phase Completion Checklist:

- [ ] Functionality implemented per the phase's own description.
- [ ] Unit tests added/passing for anything new (including the no-git-on-replay assertion).
- [ ] Live/smoke test against a real running daemon (this phase touches persistence and gating, where a unit test can mock away the actual bug): the two-member Automation run above.
- [ ] `go test ./...` and `node --test internal/server/web/*.test.js` both green.
- [ ] General documentation checklist (8 items): `CHANGELOG.md` (operator language), `docs/config-reference.yaml`, `docs/operations.md`, `README.md` (N/A with reason if no new top-level interface), the doc index (N/A if no new doc file), `docs/testing-tracker.md`, no leaked tracker IDs, this plan's own Parity surface section still accurate.
- [ ] Conditional docs: `docs/howto/` page for the workflow (blocked on missing tests, acknowledge, add the test), `docs/datawatch-definitions.md` entry, a `docs/flow/` note for the record-at-completion, read-at-advance data flow.
- [ ] Mobile-Parity Rule: the verdict appears on PRD and Automation views the companion apps parse; file or comment on a `datawatch-app` issue.
- [ ] Localization Rule: N/A until the PWA phase adds `t('key')` usage — recorded explicitly.
- [ ] Version bump in both `cmd/datawatch/main.go` and `internal/server/api.go`: this phase is the first behaviour change; state whether it is a tagged unit.
- [ ] Phase-status line updated with the shipped version noted.

### Phase 3 — Operator surfaces

**Description.** REST (read, recheck, acknowledge, config), MCP tools, CLI verbs, comm channel read and `configure`, PWA panel, Automation strip badge, Settings fields, five locale bundles, `internal/server/web/openapi.yaml`, and a single parity test pinning the shared contract literals across REST, MCP, CLI and comm (the precedent is `internal/server/plan_coverage_surfaces_parity_test.go`).

**Status: Planned.**

**Test strategy.**
- Unit and handler tests for each surface; the parity test fails if a key, verb or path drifts on any one surface.
- `node --test` for the Settings field list and PWA panel; `TestLocales_AllAppJSKeysExistInEnglishBundle` for the locale keys.
- Config round trip: `PUT /api/config` then `GET /api/config` for every key; `configure` over the simulated comm path (`POST /api/test/message`); MCP via a test client; CLI output checked.
- **Live smoke** against the sandbox daemon: block an Automation (as in Phase 2), then read the same record from REST, MCP, CLI and the comm channel, acknowledge from REST, MCP and CLI, and confirm the PWA panel and strip badge render (browser automation, per AGENT.md).
- E2E story under `scripts/test-stories` covering block, read on each surface, acknowledge and advance.

Phase Completion Checklist:

- [ ] Functionality implemented per the phase's own description.
- [ ] Unit tests added/passing for anything new (parity test, handlers, PWA).
- [ ] Live/smoke test against a real running daemon across every surface the operator uses.
- [ ] `go test ./...` and `node --test internal/server/web/*.test.js` both green.
- [ ] General documentation checklist (8 items) all checked or N/A with reasons.
- [ ] Conditional docs: how-to updated with every surface; glossary entry final; flow note final.
- [ ] Mobile-Parity Rule: `datawatch-app` issue filed or commented for Android and iOS (capability parity; no native UI required for server-enforced gating, as in the plan-coverage precedent).
- [ ] Localization Rule: every new `t('key')` has a real entry in all five locale bundles.
- [ ] Version bump in both `cmd/datawatch/main.go` and `internal/server/api.go`: state explicitly (expected: the tagged unit for this feature).
- [ ] Phase-status line updated with the shipped version noted; the planning-registry row for BL413 moved to Completed with the version.

## 11. Open questions

Each has concrete options and a recommendation. The operator decides; nothing here is assumed.

1. **Default mode at first ship.** (a) `warn` — records and alerts, blocks nothing. (b) `block` — enforces immediately. (c) `off` — opt-in only. **Recommendation: (a)**, with an explicit pre-10.0.0 checklist item to flip the default to `block` once a soak of real Automata shows an acceptable false-positive rate. This keeps the 10.0.0 advance gate real without risking a new silent-block incident on day one.
2. **Mapping granularity.** (a) Package level (any changed test in the source's directory). (b) File level (`foo.go` needs `foo_test.go`). (c) Function level (mechanism B, Go only). **Recommendation: (a) default, (b) as `autonomous.diff_test_granularity=file`, (c) deferred** until false-positive data exists.
3. **Task-level checkpoint behaviour.** (a) Record and warn only. (b) Block only on the last task of a story. (c) Block on every task. **Recommendation: (a)**: tests often arrive in a later task of the same story; the authoritative block is at PRD completion and Automation advance.
4. **Trivial-change threshold.** (a) Any added line counts. (b) 3 added non-blank, non-comment lines. (c) 10 lines. **Recommendation: (b)** with the value configurable.
5. **What happens on a PRD-completion block.** (a) Block and wait for the operator. (b) Synthesize one corrective "add tests for these files" task through the existing corrective-work path (`synthesizeCorrectiveWork`) and re-run. (c) One corrective attempt, then block if still uncovered. **Recommendation: (c)**: it recovers the common case without an operator, and the bound avoids a retry loop.
6. **Infrastructure failure of the check itself.** (a) Fail open: `warn` plus an error alert. (b) Fail closed: block. (c) Retry once, then fail open with an alert. **Recommendation: (c)**: an infra fault must not be reported as a content failure, and it must never be silent.
7. **Acknowledge from the comm channel.** (a) Not on comm (the plan-coverage precedent). (b) Add an acknowledge verb with a required note. (c) Allow it only for Automation-advance blocks. **Recommendation: (a)** for consistency now; revisit if operators report friction responding to chat alerts.
8. **Scope of the rule engine.** (a) Test-arrived check only in this release. (b) Also a generic "when X changes, Y must change" rule type in `internal/autonomous/scan/project_rules.go` (CHANGELOG, version-sync, docs). (c) Generic engine only, with test-arrived as one built-in rule. **Recommendation: (a)** for 10.0.0 — the operator decision is scoped to tests — with (b) as a follow-up that reuses the same changed-file reader.
9. **Empty diff on a completed member.** (a) Pass. (b) `warn` with a `no_changes` note. (c) Block. **Recommendation: (b)**: the evidence shows empty-diff handling has been wrong in both directions; report it, do not decide on it here.
10. **Where a legitimate "no tests" exemption is declared.** (a) Config globs only. (b) Config globs plus a per-task reason field. (c) Also an in-code marker comment. **Recommendation: (b)**: it is recorded, audited and visible in the plan; in-code markers are easy to leave behind.
11. **Coverage-delta (mechanism C) later.** (a) Never. (b) Add as an advisory signal after a passing mechanism-A check. (c) Make it a required second gate. **Recommendation: (a) for 10.0.0, revisit (b)** once suite time and flakiness are understood.

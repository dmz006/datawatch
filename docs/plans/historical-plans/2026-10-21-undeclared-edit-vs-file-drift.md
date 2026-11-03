# Undeclared edits vs. the existing file-drift guardrail

Date: 2026-10-21
Status: design analysis, read-only review of the finished branch
`automaton/a8500001` (read with `git diff main...automaton/a8500001`; never merged or cherry-picked).

## 1. Purpose

The requirement is a deterministic check that, when a task completes, compares
the files it changed against the files it declared (or generated), and surfaces
anything outside that set. This document records, requirement by requirement,
what `main` already does, what is missing, and what the branch adds, then fixes
the design decision that keeps the new check and the existing guardrail from
fighting over the same file.

## 2. What main already has

All in `internal/autonomous/file_drift.go`, wired from the drift step in
`executeOne` (`internal/autonomous/executor.go`, ~line 1228).

- `ClassifyFileDrift(task, preTaskUntracked, preTaskDirty, committed)` splits the
  files committed in `PreTaskSHA..HEAD` into two disjoint groups:
  - **preExisting**: committed, already dirty or untracked before the worker
    started, and not in `task.FilesPlanned`.
  - **inRun**: committed and *not* dirty/untracked beforehand, i.e. the worker's
    own edits, whether planned or not. A pre-dirty file that *is* planned lands
    in neither group.
  - The comparison uses `FilesPlanned` (json `files`) only. `FilesGenerated` is
    never consulted, and `inRun` is not filtered by `FilesPlanned` at all, so
    it also lists planned files.
- `checkForeignFileDrift` runs once per worker attempt, right after the verifier.
  It skips worktree-mode PRDs (`prd.Git.Branch != ""`), empty task dirs and empty
  `PreTaskSHA`.
  - **preExisting** findings are saved to `Task.ForeignFileDrift`, appended to the
    PRD as a `file_drift_detected` Decision, audited
    (`autonomous_file_drift_detected`, kind `pre_existing`) and logged. They
    **block** the task (`TaskBlocked`, error `foreign_file_drift: ...`) when
    `resolveDriftMode` is `block` (the default; empty or unknown also resolves to
    `block`) unless `driftBlockExplicitlyOff` finds `block_on_file_drift=false` on
    the PRD or its guardrail profile. `drift_mode: warn` makes them warn-only.
  - **inRun** findings are recorded only as a `file_drift_detected` PRD Decision
    ("in-run edit"), one audit event (kind `in_run`) and a log line. They
    **never block**, in any mode.
- Setting surface: `drift_mode` (`block` | `warn`) in `autonomous.Config`
  (`manager.go`) and `config.AutonomousConfig` (`config.go`), validated by
  `ValidateDriftMode`, exposed through `PUT /api/config` and
  `PUT /api/autonomous/config`. `block_on_file_drift` is the older boolean and
  per-PRD/profile opt-out.
- Surfaces that show *pre-existing* drift: `foreign_file_drift` in the PRD detail
  JSON (MCP `autonomous_prd_get` full detail), the PWA task badge and the status
  file list (`app.js`, keys `automata_task_foreign_file_drift` and
  `status_foreign_drift` in `en.json`), and `docs/howto/file-drift-guardrail.md`.

## 3. Requirement-by-requirement

| # | Requirement | What main's guardrail does today | What is missing | Branch `automaton/a8500001` |
|---|---|---|---|---|
| 1 | Compare files changed on task completion against declared plus generated files | `ClassifyFileDrift` compares the committed files in `PreTaskSHA..HEAD` against `FilesPlanned` and the pre-task dirty/untracked sets. `FilesGenerated` is **not** used. In-run edits are classified but never compared to the declared list, so a worker edit outside `FilesPlanned` is indistinguishable from a planned one. | A comparison of the worker's own edits against `FilesPlanned ∪ FilesGenerated`. | `checkUndeclaredEdits` compares committed files against `FilesPlanned` only, minus pre-dirty/untracked files. It also ignores `FilesGenerated`, so a task's generated files (e.g. a verification script) would be reported. It does not skip worktree-mode PRDs. It adds `CommittedLineStatsSince` (git `--numstat`) for per-file line counts. |
| 2 | Visible warning on the task | In-run edits: log line only. Pre-existing drift: `Task.ForeignFileDrift` plus a PWA badge. There is no per-task field for the worker's own undeclared edits. | A per-task field and a visible badge for undeclared edits. | `Task.UndeclaredEdits []UndeclaredEdit{Path, LinesChanged}` (json `undeclared_edits`), accumulated across retries with line counts summed. The PWA badge is not part of the branch (see row 7). |
| 3 | Warning on the PRD | A `file_drift_detected` Decision is appended for in-run and pre-existing findings. There is no distinct kind for undeclared edits, so the two cannot be told apart or filtered. | A dedicated decision kind. | Appends an `undeclared_edit_detected` Decision naming the task and files. |
| 4 | Alert | None. Drift is audit plus log only. | An operator-facing alert. | Adds `Manager.alertSink` / `SetAlertSink` / `AlertSink` and raises a `warn` alert "Undeclared edit detected". **Gap:** nothing outside tests wires `SetAlertSink`, so in production the alert would be silently skipped until the daemon wires it to its alert store. |
| 5 | `off` / `warn` / `block` setting, default `warn` | `drift_mode` is `block` or `warn` only, defaults to `block`, and governs pre-existing drift only. There is no `off`, and in-run edits cannot be made to block. | A separate three-state setting defaulting to `warn`. | `undeclared_edit_mode` in `autonomous.Config` and `config.AutonomousConfig`, default `warn`, resolved by `EffectiveUndeclaredEditMode` (lowercase, unknown or empty falls back to `warn`). `off` is a full no-op, `block` sets `TaskBlocked` with error `undeclared_edit: ...`. `PUT /api/config` rejects other values with 400. **Gap:** `PUT /api/autonomous/config` has no equivalent validation, and the branch predates `drift_mode`, so it does not reconcile with it (see section 4). |
| 6 | Parity across MCP / REST / CLI / comm | `drift_mode` is in `GET`/`PUT /api/config` and `PUT /api/autonomous/config`. The `drift_mode` MCP and comm-router parity is thinner than `block_on_file_drift`'s. | `undeclared_edit_mode` on every surface, with the same validation. | REST `GET`/`PUT /api/config`: yes. MCP `autonomous_config_get` / `autonomous_config_set`: yes, with a `undeclared_edit_mode` string parameter. Comm `configure` help text in `router.go`: lists the key. CLI: no dedicated change (it goes through `config set`). Needs a check that `PUT /api/autonomous/config` accepts and validates it. |
| 7 | PWA and locales | PWA shows only `foreign_file_drift` (badge and status list). Locale keys exist for the foreign-drift labels only. | A settings field plus a per-task undeclared-edit display, in all five locales. | `app.js` adds one `GENERAL_CONFIG_FIELDS` text field (`settings_undeclared_edit_mode`) and the key is added in `de`, `en`, `es`, `fr`, `ja`. **Gap:** no task badge or detail list for `undeclared_edits`, and the field is free text rather than a select. |
| 8 | How-to | `docs/howto/file-drift-guardrail.md` covers pre-existing drift only. | A section for undeclared edits and the off/warn/block choice. | Adds an "Undeclared edits" section, plus `docs/operations.md`, `docs/implementation.md`, `docs/config-reference.yaml` and `docs/mcp.md` entries. The branch's text says freshly generated files are counted as undeclared, which contradicts the `FilesGenerated` rule below. |

## 4. Design decision

`undeclared_edit_mode` governs **only the worker's own edits that are outside
`FilesPlanned ∪ FilesGenerated`**. `drift_mode` keeps governing **pre-existing**
drift exactly as today. The two settings never both block the same file.

Rules that make this precise:

1. **Scope split.** `ClassifyFileDrift` stays the single classifier. Its
   `preExisting` set belongs to `drift_mode`. Its `inRun` set is the input to
   the new check, filtered further by item 2.
2. **Declared set.** A file is *undeclared* when it is in `inRun` and in neither
   `FilesPlanned` nor `FilesGenerated`. `FilesGenerated` must be included (the
   branch omits it). This keeps verification-step outputs, such as a story's
   `VerificationScript` under `.datawatch/verify/`, from raising false findings.
3. **No double reporting.** A file already reported as pre-existing drift is
   excluded from the undeclared finding. Because `inRun` and `preExisting` are
   disjoint by construction, the check should derive its input from
   `ClassifyFileDrift` rather than re-deriving the pre-dirty set on its own, so
   the two cannot diverge. The branch's inline `pre` map already excludes
   pre-dirty files, but it also drops a pre-dirty *planned* file; deriving from
   `inRun` keeps the behaviour identical to the existing classifier.
4. **One block per file.** `drift_mode: block` can block on `preExisting`;
   `undeclared_edit: block` can block on the undeclared remainder. Since the sets
   are disjoint, a single file can never trigger both. If both modes block in the
   same attempt, the drift block fires first (it runs first in `executeOne`) and
   the undeclared step is not reached for that attempt.
5. **Defaults.** `undeclared_edit_mode` defaults to `warn` and never blocks
   unless an operator opts in. `drift_mode` keeps its `block` default. An unknown
   or empty `undeclared_edit_mode` resolves to `warn` (the branch's
   `EffectiveUndeclaredEditMode` behaviour); `PUT` paths reject unknown values.
6. **Worktree mode.** Match `checkForeignFileDrift`: skip PRDs running in an
   isolated worktree (`prd.Git.Branch != ""`) unless that is deliberately
   changed, so the two guardrails have the same coverage.
7. **Read-only.** Neither check reverts, resets, amends or un-stages anything.
8. **Recording.** Undeclared findings use their own decision kind
   (`undeclared_edit_detected`), their own audit action and the per-task
   `undeclared_edits` field. Existing `file_drift_detected` in-run Decisions stay
   as they are; they are not removed or reclassified.

## 5. Work needed to adopt the branch

The branch is based on an older `main` (it predates `drift_mode` and
`ClassifyFileDrift`), so it cannot be merged as is. Implement on current `main`:

- Reuse `ClassifyFileDrift`'s `inRun` set; subtract `FilesPlanned ∪ FilesGenerated`.
- Keep `CommittedLineStatsSince`, `Task.UndeclaredEdits`, `EffectiveUndeclaredEditMode`,
  the alert sink and the REST/MCP/router/PWA/locale additions, with the section 3
  gaps fixed: wire `SetAlertSink` in the daemon, validate on
  `PUT /api/autonomous/config`, add the per-task PWA display, render the setting
  as a select, and correct the how-to wording.
- Do not copy the branch's `Version` edits in `internal/server/api.go` and
  `cmd/datawatch/main.go`; versions are not touched here.
- Tests to carry: the branch's `file_drift_undeclared_test.go`,
  `undeclared_edit_test.go`, `internal/mcp/undeclared_edit_test.go`,
  `internal/server/undeclared_edit_config_test.go` and the config test, plus new
  cases for the `FilesGenerated` exemption and the no-double-report rule. Run as
  `env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous/ ./internal/config/ ./internal/server/ ./internal/mcp/`.

## 6. Implementation status

Shipped on current `main`, following section 4:

- `checkUndeclaredEdits` takes the `inRun` set from `ClassifyFileDrift` and subtracts `FilesPlanned ∪ FilesGenerated`; worktree-mode PRDs are skipped; read-only git only.
- `Task.UndeclaredEdits`, `CommittedLineStatsSince`, the `undeclared_edit_detected` decision, the `autonomous_undeclared_edit_detected` audit event and a warn alert.
- `autonomous.undeclared_edit_mode` (`off`/`warn`/`block`, default `warn`) on `GET`/`PUT /api/config`, `PUT /api/autonomous/config` (validated, section 3 gap fixed), MCP, CLI via `config set`, comm `configure`, YAML and the PWA (rendered as a select, section 3 gap fixed). `SetAlertSink` is wired to the alerts store in the daemon (section 3 gap fixed). The PWA shows a per-task badge and list.
- The how-to wording now says generated files count as declared.

Deviations and open items:

- None from the section 4 rules.
- The comm `configure` path and the PWA select/badge have no dedicated automated tests, and no row is live-validated yet; `docs/testing-tracker.md` records both honestly.
- The OpenAPI spec has no dedicated task schema, so `undeclared_edits` is documented in the description of `GET /api/autonomous/prds/{id}`.

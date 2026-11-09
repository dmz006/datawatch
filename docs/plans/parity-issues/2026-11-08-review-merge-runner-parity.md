# Parity issue draft — the merge read: held lock, last validator result, and run history (Part of finishing review and merge by the daemon)

**Filed 2026-11-09 as dmz006/datawatch-app#421, one consolidated issue for the merge lock, validator and merge run drafts** (shipped in v9.118.0).

Parent work: "Part of finishing review and merge by the daemon (Phase 4 of the
autonomy handover plan). Operator, 2026-11-07, quoted:".

This draft is a file only. It captures the parity gaps this work leaves open for the
merge-pipeline read model — the read side of review and merge by the daemon: the
per-repository merge lock, the release-bump validator result, and the merge pipeline run
history, as every surface reports them. No hosting CLI is run from it, nothing is filed on
any tracker, and nothing is created on any host.

## Affected area

The affected area is the read path for the daemon's merge pipeline. A single read model is
served by the daemon and every surface renders that same body:

- `internal/autonomous/merge_status.go` and `internal/autonomous/merge_validator_result.go`
  — the `MergeStatus` body of `GET /api/autonomous/merge` (`mode`, `alert_minutes`,
  `automata`, `shadow_decisions`, `pipeline_runs`, `merge_lock`, `last_validator`), the
  `heldMergeLock` and `latestMergeValidator` selectors, and the read-only
  safe-to-delete report `MergeSafeToDelete`.
- `internal/autonomous/merge_text.go` — the shared text renderers (`RenderMergeState`,
  `RenderSafeToDelete`) that the CLI, the MCP tool result and the comm channel all use, so
  none of those surfaces can show different wording. The validator line is rendered as
  "Last release-bump validation: passed (version …) / failed", with one indented line per
  failing check, and the lock as "Merge lock held: run … for Automaton … (branches …) since
  …".
- `internal/server/merge_test.go` and `internal/autonomous/merge_validator_status_test.go`
  — the surface and selector tests, including the newest-run-wins rule for `last_validator`
  and the rule that a lock entry naming no run and no Automaton holds nothing.
- `internal/mcp/merge_tools.go` and `internal/mcp/merge_tools_test.go` — the two
  read-only tools `autonomous_merge` and `autonomous_safe_to_delete`, which proxy the REST
  bodies; the tool description names `merge_lock` and `last_validator` for the caller.
- `internal/router/merge_comm_test.go` — the comm-channel `autonomous merge` verb reading
  the same body.
- `cmd/datawatch/cli_merge_test.go` — the `datawatch autonomous merge` CLI command reading
  the same body.
- `internal/server/web/app.js`, `internal/server/web/app-merge-pipeline.test.js`
  and the `merge_*` strings in `internal/server/web/locales/{en,de,es,fr,ja}.json` —
  the PWA Merge pipeline card, which shows the pipeline source, one line per step, the
  "Merge lock held: …" line, the "Last release check passed/failed: version …" line with
  the failing checks under it, and a help link to the howto section for the release-bump
  validator.
- `docs/howto/merge-pipeline.md` and `docs/datawatch-definitions.md` — the manual section
  (lock semantics, the release-bump validator and its five checks, running a merge twice)
  and the definitions, including the surface list that shows where the lock and the last
  validator result can be read.

Downstream of the read model, the merge pipeline itself — `internal/mergepipeline/`
(`run.go`, `definition.go`, `default.go`, `release_validator.go`), the run records in
`internal/autonomous/merge_pipeline_run.go`, the lock store in
`internal/autonomous/merge_lock.go`, and the run start at
`POST /api/autonomous/merge/run` — is what the read model reports on.

## Current behavior

The read is one object, served read-only, and every human surface renders it from the same
source. `GET /api/autonomous/merge` reports the mode in effect (off, shadow or on), the
alert threshold, one entry per finished Automaton with a work branch and its cached
comparison state, the shadow decisions when the mode is shadow, and — filled whatever the
mode is — the merge pipeline runs, newest first, each with its pipeline source and one
entry per step (start, end, result); the held merge lock, absent when nothing is held or
when the lock store is unreadable or no base repository is configured; and
`last_validator`, the release-bump validator result of the newest run that reached the
validate step, absent when no run has one. No read runs git: the Automaton entries come
from the coordinator's in-memory comparison cache, and the safe-to-delete report behind
`GET /api/autonomous/safe-to-delete` comes from a short-lived per-repository cache.

The surfaces show the same body. The CLI `datawatch autonomous merge`, the comm
`autonomous merge` verb and the MCP `autonomous_merge` tool all read
`GET /api/autonomous/merge` and render it through `merge_text.go`; the MCP tools are
annotated read-only and their descriptions point a caller at `POST /api/autonomous/merge/run`
for starting a run. The PWA Merge pipeline card shows the same lock and validator lines,
localized in English, German, Spanish, French and Japanese, with a link to the howto
section for the release-bump validator. The howto lists exactly this surface set — PWA,
REST, MCP, comm, CLI — and states that `last_validator` is the result of the newest run
that reached the validate step and that a run that stopped before that step, or whose
release steps were all skipped, has no validator field.

The gaps this work leaves open are policy and reach, not accuracy of the read. The merge
read is read-only on every surface; nothing outside REST can start a run. The run records
are bounded to the last 200, so a long-lived daemon eventually reports on older runs
without them. `last_validator` is a single newest-with-one result, not a trend:
a run that passed validation and then failed at a later step is indistinguishable at a
glance from a run that has shipped, and the read does not say whether the passing
validation was followed by a push. The lock is reported per base repository but nothing
else in the read names which repository the runs belong to beyond what the run record
itself carries. The Phase 4 decisions that surround this engine — the release approval
mode per release size (automatic, ask, manual) with per-project overrides, the
prepare-only mode, the direct-push versus pull-request release path, the shadow
comparison against the supervisor's manual merge, the first-N model-resolved-conflict
confirmation rule, git hosts beyond GitHub with audited credential sources, the enforced
pre-push secret scan, and the fallback rule that moves a release size back to ask on a
failed validator or a red release CI — are none of them modeled in this read or in the
pipeline: the reader can see that a run happened and whether its validation passed, but
cannot see, for example, an ask-mode hold, an unreleased prepared bump, or the fallback
moving a size back to ask. Android and iPhone have not been checked against this surface;
the parity question for the mobile apps is open.

## Expected behavior

Each surface that reports the merge pipeline reports the same held lock, the same last
validator result and the same run list as `GET /api/autonomous/merge`, in the same meaning
— a held lock with no run and no Automaton is nothing at all, `last_validator` is always
from the newest run that has one, and a run that never reached the validate step shows no
validator line anywhere. A surface that cannot start a run, or that cannot show the lock
or the validator line at all, is an explicit and reason-logged exclusion rather than a
silent gap; today MCP, CLI, comm and PWA are all read-only by design with the start action
REST-only, and the issue should carry that as a stated, operator-decided scope rather
than let it drift. When the read model carries a hold state — an ask or manual release
approval, a prepare-only bump that was never pushed, or the fallback rule moving a release
size back to ask with its alert — every surface reports it the same way the lock and the
validator are reported now, and the PWA card shows it with the same wording as the shared
text, in every locale it is provided. When a run's push is still outstanding after a
passed validation, the read distinguishes it from a shipped run instead of leaving the
operator to tell the two apart from the step list.

## Suggested next step

File the datawatch-app parity issue from this draft, in this order. First, the run state
wording: extend the read model (or the run record it reads) so a run that passed
validation but has not finished pushing is distinguishable from one that has shipped, and
give that distinction the same treatment as `merge_lock` and `last_validator` — one field,
rendered by `merge_text.go`, shown on the PWA card and localized in the five locales that
the card strings already exist in. Second, the hold states: when release approval modes,
prepare-only and the fallback rule are modeled, add their read to `MergeStatus` and to the
same five-surface list in the howto, with the PWA card updated first. Third, the mobile
rows: confirm the datawatch-app state of this read on Android and iPhone and add both rows
to the howto surface list, each included or excluded with a reason. Until then the issue
should record the read side as complete within its stated scope — same body on every read
surface, read-only, start REST-only — and the open items as the hold states and the
mobile rows.

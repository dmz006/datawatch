# [sync] feat(merge): release-bump validator and its run record (Part of finishing review and merge by the daemon)

**Filed 2026-11-09 as dmz006/datawatch-app#421, one consolidated issue for the merge lock, validator and merge run drafts** (shipped in v9.118.0).

Parent work: "Part of finishing review and merge by the daemon (Phase 4 of the autonomy
handover plan)", operator-approved 2026-11-07. This draft is a planning note for the
datawatch-app parity issue. It is a file only: nothing has been filed on any tracker, and
no hosting CLI was run.

## Affected area

The release-bump validator: the read-only check that runs inside the merge pipeline before
a release is pushed, and the standalone script that runs the same five checks by hand. The
validator's result is kept on the merge pipeline run record as `validator`.

Code and files involved:

- `internal/mergepipeline/release_validator.go` and `internal/mergepipeline/release_validator_test.go`
  — `ValidateReleaseBump`, the five named checks (`version_variables_equal`,
  `changelog_heading_matches`, `one_step_above_last_tag`, `tag_not_taken`,
  `readme_line_matches`) and their tests.
- `internal/autonomous/merge_validator_result.go` — the `MergeValidatorCheck` and
  `MergeValidatorResult` types that carry the result (`pass`, `version`, `checked_at`, and
  one entry per check).
- `scripts/verify-release-bump.sh` and `scripts/verify-release-bump_test.sh` — the shell
  version of the same five checks, printing one PASS or FAIL line per check.
- `internal/mergepipeline/definition.go` and `internal/mergepipeline/default.go` — the
  `validate_release_bump` action and the built-in `validate-release-bump` step, which sits
  after the tag step and before `push-release`.
- `internal/mergepipeline/run.go` and `internal/mergepipeline/run_test.go` — the step runner
  (`validateReleaseBump`) that builds the input, stores the result on the run and stops the
  pipeline on a failing check.
- `internal/autonomous/merge_pipeline_run.go` — the run record field `Validator`
  (`json:"validator,omitempty"`).

## Current behavior

- **The check runs and gates the push.** In a pipeline that includes the built-in
  `validate-release-bump` step, the five checks run after the bump commit and tag and before
  the push. Any failing check fails the step, names every failing check in the step's error,
  and stops the run, so nothing is pushed. A passing run records the step as passed with a
  one-line summary.
- **Every check runs.** The result lists all five checks in order, so a failure names each
  check that failed, not only the first.
- **The result is stored on the run record.** `Validator` is set when the run reaches the
  validator step and is reset at the start of a run. It is serialised as `validator` on each
  entry of `pipeline_runs`, which `GET /api/autonomous/merge` returns.
- **The MCP read tool does not name the field.** The `autonomous_merge` tool description
  lists `pipeline_runs` and its per-step results, but does not mention `validator`. The
  field is present in the JSON, so an MCP caller can read it, but the description does not
  say so. (Checked by reading the tool description, not by a live call.)
- **The shell script is a manual-only surface.** `bash scripts/verify-release-bump.sh` runs
  the same five check names and prints PASS or FAIL lines. It is documented in
  `docs/howto/merge-pipeline.md` and `docs/operations.md`. There is no REST, MCP, CLI verb,
  comm-channel verb or PWA control that runs the validator on demand, outside a pipeline run.
- **Text renderers do not show the validator.** The shared renderer in
  `internal/autonomous/merge_text.go` does not reference `validator`, so the CLI and the chat
  verb do not show which checks passed or failed for a run.
- **The PWA does not show run records or the validator.** A search of
  `internal/server/web/app.js` found no reference to `pipeline_runs` or to the validator
  result. This is a code search, not a live check of the Automation view.
- **Android and iPhone** have not been checked in this draft.

## Expected behavior

- A run that stopped at the validator shows which checks failed, by name, on every surface
  that shows the run: REST, MCP, CLI, comm channel and PWA. Today only the raw JSON carries
  it.
- The on-demand check is available from the same surfaces as the pipeline's `validate`
  action, or the gap is recorded as an explicit, reason-logged exclusion in the feature's
  `Parity surface` section. Today the only on-demand path is the shell script.
- The five check names and their meanings are the same in the Go validator, the shell script
  and every rendered surface. The shell script's own test file already pins the names; the
  surfaces should reuse the same strings rather than restate them.
- The MCP `autonomous_merge` description names the `validator` field next to the
  per-step results, so an MCP caller knows where the check result is.
- Android and iPhone state their own status in the datawatch-app issue, each marked included,
  partly included, or excluded with a reason.

## Parity surface

| Surface | Status | Note |
|---|---|---|
| REST | Partly included | The result is a field of `GET /api/autonomous/merge` `pipeline_runs`. No REST call runs the validator on demand. |
| MCP | Partly included | `autonomous_merge` returns the field but its description does not name it. No MCP tool runs the validator. |
| CLI | Partly included | `scripts/verify-release-bump.sh` is a manual script, not a `datawatch` verb. The `datawatch autonomous merge` output does not show the validator. |
| comm channel | Not included | No chat verb shows the validator result or runs the check. Reason to confirm with the operator: scope of the daemon's merge authority. |
| YAML/config | Included | The `validate-release-bump` step is part of the merge pipeline definition, which is readable and writable through the existing pipeline config surfaces. The check has no separate tunable beyond the pipeline definition and the `readme_file` and `remote` inputs. |
| PWA | Not included (as found) | No rendering of run records or the validator result was found in `app.js`. To be confirmed on a live daemon before filing. |
| Android | Not verified | Not checked in this draft. The datawatch-app issue must state its own status. |
| iPhone | Not verified | Not checked in this draft. Same as Android. |

## Suggested next step

Write the datawatch-app parity issue from this draft after the live check below. The issue
should ask for three things, in this order:

1. On the Automation view, show the run's validator result when a merge pipeline run stopped
   at the release-bump step: each of the five checks, its pass or fail, and its detail, using
   the field names from `GET /api/autonomous/merge`.
2. Add Android and iPhone rows to the same parity table, each marked included, partly
   included, or excluded with a reason.
3. Confirm the wording of the five check names is shared with the shell script and the
   Go validator, so the strings are not restated per client.

On the datawatch side, the small next step is to name the `validator` field in the MCP
`autonomous_merge` description and to add the validator line to the shared text renderer in
`merge_text.go`. Whether a comm-channel verb or an on-demand REST or MCP run of the validator
is wanted is an operator decision about the scope of the daemon's merge authority, so it is
not decided here.

Verification still to do before this issue is filed: a live run on a sandbox daemon, started
with `env -u TMUX -u TMUX_TMPDIR`, that drives a pipeline to the validator step with one
deliberately failing check, then reads `GET /api/autonomous/merge` and the PWA Automation view
to see whether the validator result appears. Unit tests in `release_validator_test.go` and
`run_test.go` cover the checks and the step, but they do not confirm the live rendering.

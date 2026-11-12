# CodeQL Analyze job failures — findings

Recorded 2026-10-26. Investigated read-only with `gh` (reachable).

## What failed

Two runs of the `CodeQL` workflow (event `dynamic`, i.e. GitHub default setup)
started 12 seconds apart on the same commit `5e96b477`:

- run 40366382818: `Analyze (python)` failed.
- run 40366394552: `Analyze (actions)` failed.

In both, every step up to and including query execution succeeded and the
SARIF was exported. The failing step is `Perform CodeQL Analysis`. The job
conclusions and step results were read from the run's job data; `gh run view
--log-failed` returned no text for these runs, so no server error message was
available and the point of failure within the step is not quoted from a log. Both languages reported zero
results. The other languages in the same runs passed, and later runs on other
commits passed.

## Which cause holds

| Candidate | Verdict | Evidence |
|---|---|---|
| New high/critical alerts fail the check through a ruleset or merge protection | Ruled out | `main-protection` has deletion, non-fast-forward, pull-request and required-status-check rules only; there is no code-scanning rule. The failing jobs had zero results, and jobs with 94 results (go) passed. |
| Default setup and the `upload-sarif` steps in `release.yaml` conflict | Ruled out | Those steps use the categories `gosec` and `trivy-*`. The default setup uses `/language:*`. They do not overlap. |
| The upload step itself failed (concurrent or duplicate upload) | Most likely | Two runs on one commit uploaded the same `/language:*` categories at the same time. Only the jobs that lost the race failed, at the upload call, with zero findings. No error text could be retrieved, so this is an inference from timing and the pattern, not a quoted server message. |

Open alerts do exist (2 critical, 35 high, 2 medium at the time of
investigation), but they are not what fails the Analyze jobs. Nothing currently
turns them into a failing check, which is the gap the new workflow closes.

Default setup also analyses python and `javascript` / `typescript` as separate
entries. The repository tracks about 20 Python files (under `scripts/` and
`examples/plugins/`), so the advanced workflow keeps python alongside go,
javascript-typescript and actions, with no build step, so no language that
default setup analysed loses coverage.

## Outcome

- `.github/workflows/codeql.yaml` is an explicit CodeQL workflow: one job per
  language with a unique category (`/language:<name>`), `upload: always`,
  explicit permissions, and a concurrency group per ref so two runs cannot
  upload the same category at once.
- A final `alert-gate` job fails with a named message listing the critical and
  high counts when open alerts of those severities exist on the ref. Only alerts
  in state `open` count, so an alert dismissed in code scanning is excluded.
  The gate does not read `security/accepted-risks.yml`; that registry records
  the reason for each dismissal, which the operator applies in GitHub. No check was removed or weakened and nothing is `continue-on-error`.

## Operator action required

The repository is on default setup. Switching to this workflow requires
disabling default setup in the repository settings (Settings > Advanced
Security > Code scanning > CodeQL analysis). This task did not and must not do
that. Until default setup is disabled, uploads from this workflow are rejected
as duplicates of the default-setup categories.

Also expect `alert-gate` to fail on first run: the open high and critical
alerts must be fixed or dismissed with a registered reason first. The
dismissals listed in `docs/plans/path-injection-alert-decisions.md` are the
operator-applied part of that.

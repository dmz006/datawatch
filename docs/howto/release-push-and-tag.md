---
docs:
  index: true
  topics: [release, push, tag, release path, ci window, automaton, audit, settings]
exec_params:
  - {name: project, required: true, description: "Project name whose release settings and last release you want to read"}
exec_steps:
  - tool: release_push_status
    description: Read whether the daemon pushes and tags finished releases, the release path, the CI window and the last release recorded for the project
    args: {project: "{{params.project}}"}
    read_only: true
---
# How-to: Release push and tag

When an Automaton finishes a release, the daemon can publish it: push the
release commit to the remote and create the annotated release tag. This guide
turns that on, chooses between the two release paths, explains the refusal rule
and the audit entry, says what happens when CI is slow or red, and shows where
every setting lives on each surface.

Two words are used throughout:

- **Push** sends the release commit to the remote branch.
- **Tag** creates the annotated release tag on that commit and sends the tag to
  the remote.

The definitions are in [datawatch-definitions.md](../datawatch-definitions.md#push-and-tag).

## Status in this build

Read this first. The settings, the refusal rule, the audit record shape and the
status read are all in place and tested. The code that acts on them is not yet
called by the daemon's release run in this build:

- Turning `autonomous.push_and_tag` on stores the setting. It does not, by
  itself, push or tag anything yet.
- No release record store is installed in this build, so `release_push_status`
  reports `last_release` as `null` for every project.
- The CI window fall-back is built but no release run calls it, so a red CI
  outcome does not change the release approval mode yet.
- Even once a release run calls it, the fall-back to `ask` is held only in
  memory, so a daemon restart clears it.

The sections below describe the behaviour the daemon follows once the release
run calls this code. Check `release_push_status` to see what the daemon
currently reports.

## Turning on push and tag

1. Set `autonomous.push_and_tag` to `true`. It is off by default.
2. Choose a release path with `autonomous.release_path`: `direct_push` (the
   default) or `pull_request`. See [Choosing a release path](#choosing-a-release-path).
3. Optionally change `git.release_ci_window_minutes` from its default of 60. It
   must be a whole number of minutes greater than 0.
4. Read the result back with the `release_push_status` tool or `GET /api/config`.
   Each value shows what the daemon will use.

Setting keys are listed in [Settings per surface](#settings-per-surface). An
invalid value is refused and not stored. For example, a `release_path` other
than `direct_push` or `pull_request` is refused with the message
`autonomous.release_path must be "direct_push" or "pull_request"`.

## Choosing a release path

| Path | What the daemon does | Use it when |
|---|---|---|
| `direct_push` | Fast-forwards the default branch to the release commit, creates the tag on that commit and pushes both. | The default branch is yours to update and CI runs after the push. |
| `pull_request` | Pushes the release branch, opens a pull request into the default branch, polls its checks, and merges only when every check succeeded. | The default branch is protected. The release lands only after the merge. |

Both paths:

- push with a plain fast-forward and never force;
- refuse to run when the working tree has uncommitted changes;
- never move a tag that already points at another commit. The run fails and names
  the tag instead.

Direct push only pushes the default branch. If HEAD is on any other branch the
push is refused. The pull request path leaves the pull request open and unmerged
when a check fails or is still pending when its window ends.

## The refusal rule

The daemon may push the default branch on its own only when the pre-push secret
scan is enforcing. The rule is:

- An **unattended** push (the daemon, not an operator, performs it) to the default
  branch is refused unless `git.secret_scan.mode` is `enforce`. With `shadow` or
  `off` it is refused.
- An **attended** push (an operator runs it) is not affected by this rule.
- A push to any branch other than the default branch is not affected by this rule.

A refusal is recorded in the audit log with outcome `refused`, and the release
record keeps the same outcome. The error names the current mode, for example
`current mode "shadow"`. To allow unattended pushes, set `git.secret_scan.mode`
to `enforce` first; see the secret scan settings in [Settings — General](../datawatch-definitions.md#settings--general).

## Reading the audit entry

Every release push and tag writes an audit entry. Two actions are used:

| Action | Written for |
|---|---|
| `release_tag` | Creating the local annotated tag, then pushing it. |
| `release_push` | Pushing the default branch (direct path) or the release branch (pull request path). |

Each entry has these fields:

| Field | Meaning |
|---|---|
| `subject` | The project. |
| `details.project` | The project again, for filtering. |
| `details.ref` | The branch ref (`refs/heads/<default>`) or the tag name. |
| `details.remote` | The git remote used, `origin` unless `autonomous.ship.remote` names another. |
| `details.credential_source` | Where the credential came from, as a label only, for example `host-login` or an environment variable name. The token itself is never written. |
| `details.outcome` | What happened. |

Outcomes you will see:

| Outcome | Meaning |
|---|---|
| `created` | A tag was created for this release. |
| `unchanged` | The tag already existed on the same commit, so nothing was created. |
| `pushed` | The branch or tag reached the remote. |
| `prepared` | Push and tag is off; only the local tag was created, and the release waits for the operator. |
| `refused` | The refusal rule, a non-default branch, or a dirty tree stopped the push. |
| `failed` | The push or tag failed. The error on the run names the step. |
| `merged`, `checks_failed`, `checks_timed_out` | Pull request path only. The merge happened, or a check failed, or checks did not settle before the window ended. |

Query these with the audit tools described in [audit-logging.md](audit-logging.md),
filtering on the action `release_push` or `release_tag`.

## CI window and the fall-back to ask

`git.release_ci_window_minutes` (default 60) is how long after the push the
daemon still watches the release's CI. The window applies in two places:

- On the pull request path, it is how long the pull request's checks are polled
  before the release is treated as `checks_timed_out` and left unmerged.
- As the fall-back window: if the release CI run goes **red** within the window
  after the push, the daemon falls back to asking before it ships another release
  of that size. For that project and that release size (`patch`, `minor` or
  `major`) it:
  - changes the release approval mode to `ask` (see
    [Release approval](../datawatch-definitions.md#release-approval));
  - raises an error alert naming the release, the project, and how many minutes
    after the push it went red;
  - files a defect to fix the failing release CI run.

A red outcome after the window, and a green or pending outcome, change nothing.
The fall-back is held in memory, so a daemon restart clears it until the
approval setting is written. This is the step described in
[Status in this build](#status-in-this-build).

## Last release and the status tool

Use the `release_push_status` MCP tool to read the release settings for one
project. It is read-only and answers:

| Field | Meaning |
|---|---|
| `project` | The project asked about. |
| `push_and_tag` | Whether the daemon pushes and tags releases. Default `false`. |
| `release_path` | `direct_push` or `pull_request`. Default `direct_push`. |
| `ci_window_minutes` | The CI window. Default `60`. |
| `last_release` | The newest release recorded for the project: `tag`, `commit`, `release_path`, `push_outcome`, `ci_outcome`. `null` when none is recorded. |

In the web view the same record appears as a `Last release: <tag>` line in the
release settings block, and only when a release record exists. In this build
there is no record store, so the line is not shown.

## Settings per surface

Every surface writes the same three keys. The values are stored in the same
config, so a change on one surface shows on all of them.

| Setting | config.yaml | Web Settings page | REST `/api/config` | Comm channel `configure` | `datawatch config set` | MCP status tool |
|---|---|---|---|---|---|---|
| Push and tag | `autonomous.push_and_tag` (`true` / `false`, default `false`) | Settings > Sessions, "Push and tag finished releases" switch | `PUT /api/config` with `"autonomous.push_and_tag": true` | `configure autonomous.push_and_tag=true` | `datawatch config set autonomous.push_and_tag true` | `release_push_status` reads `push_and_tag` (read-only) |
| Release path | `autonomous.release_path` (`direct_push` or `pull_request`, default `direct_push`) | Settings > Sessions, "Release path" select | `PUT /api/config` with `"autonomous.release_path": "pull_request"` | `configure autonomous.release_path=pull_request` | `datawatch config set autonomous.release_path pull_request` | `release_push_status` reads `release_path` (read-only) |
| CI window | `git.release_ci_window_minutes` (whole minutes, greater than 0, default `60`) | Settings > Sessions, "CI window (minutes)" | `PUT /api/config` with `"git.release_ci_window_minutes": 45` | `configure git.release_ci_window_minutes=45` | `datawatch config set git.release_ci_window_minutes 45` | `release_push_status` reads `ci_window_minutes` (read-only) |

Reads: `GET /api/config` returns the same values nested under `autonomous` and
`git`. To change a value through MCP, use the `config_set` tool with the dotted
key. The status tool itself only reads.

## Before you tag: make pre-tag

`make pre-tag` must pass on the build host before a tag is created, by an Automaton or by hand. It runs every gate in order, stops at the first failure, and replaces `go test` in the CI and release workflows, so a code failure is found on the build host and not after the tag is pushed. It appends one line per run (commit, date, step count, result and seconds) to the gitignored `.datawatch/pre-tag-record`; tag only from a commit whose last line says `result=pass`.

New work must carry its unit and functional tests and the relevant smoke script before it ships. The e2e and sandbox smoke run stays the operator's major-release step; it is not a product feature and `make pre-tag` does not run it.

## When the code scanning upload fails

The Release workflow copies its gosec and Trivy SARIF reports to GitHub Code Scanning. Those uploads never block a release: each tries up to four times with waits of 60, 180 and 300 seconds (for example when GitHub's rate limit answers), a failed attempt never fails the job, and if every attempt fails the step logs a warning and the run goes on. The blocking gosec baseline check and Trivy scan steps before them are unchanged and are still the real gates. To get the results anyway, download the `gosec-results` or `trivy-<image>-sarif` artifact from the run, or re-run the workflow later. If a release shows no new entry in Code Scanning, look for the "failed on every attempt" warning on the upload steps. When you edit a workflow, `make lint` runs `make check-sarif-upload`, which fails if an upload step becomes blocking or loses its retries. Details: [operations.md](../operations.md#code-scanning-upload-in-the-release-workflow).

## Related

- [Automata checks and ship](automata-checks-and-ship.md) — the ship steps that run after a release finishes.
- [Merge pipeline](merge-pipeline.md) — the release approval modes the fall-back changes.
- [Audit logging](audit-logging.md) — where the release audit entries are queried.
- [Automata overview](automation-tab.md)
- [Autonomous planning](autonomous-planning.md)

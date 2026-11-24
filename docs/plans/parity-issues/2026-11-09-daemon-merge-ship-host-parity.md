# [sync] Daemon merge-and-ship and repo CI watch: per-host behavioral parity (tag signing, release, audit, host action phase)

**Not filed yet.** — this is the tracked draft; the issue is opened when the
operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI and creates
nothing on any host.

**Work this draft captures** — *Part of finishing review and merge by the
daemon* (Phase 4 of the autonomy handover plan, operator 2026-11-07: "b
finsih now we need to be using it to debug it and monitor its right."), PRD
`7bd50001`. In this slice the daemon's merge loop gained the
provider-neutral pieces: the git host facade that resolves a project's
remote to a `git.hosts` entry and dispatches change request, issue and
release to that host's provider (`internal/git/host_facade.go`), the merge
pipeline's built-in post-ship hooks with the host-aware `parity_issue`
variant (`internal/mergepipeline/run.go`,
`internal/mergepipeline/posthook_builtins.go`), per-host CI run listing —
`gh run list` for GitHub hosts, `glab api` pipelines for GitLab hosts
(`internal/git/ci_watch.go`) — and the repo CI watch's host path, which
polls a repository whose origin belongs to a configured host and records
each run (`internal/autonomous/repo_ci_watch_poll.go`,
`internal/autonomous/repo_ci_watch_host.go`). The tag and push helpers
learned the host's signing policy and a fast-forward-only discipline
(`internal/git/tag.go`), and every host-facing operation — merge request,
issue, release, tag — writes an operator audit entry carrying the
credential source and never the token (`internal/git/host_audit.go`).

## Affected area

The area is the daemon's own merge-and-ship path — the ship step of the
merge pipeline: version check, changelog, merge, tag, push, deploy, the
post-ship hooks (parity issue, tag, notify) — plus the repo CI watch's
treatment of a non-PRD repository's CI on a **configured git host**. Phase
4's design decision (per
[docs/plans/2026-11-03-autonomy-handover-plan.md](../../plans/2026-11-03-autonomy-handover-plan.md))
is that the daemon reviews, merges and ships on the operator's behalf on
whatever forge the operator configured, tags under the host's signing
policy (`git.hosts[].sign_tags`, `signing_key`), and leaves one audit
entry per host-facing operation with its credential source
(`host_login`, `vault`, `broker`) so the operator can answer "what did the
daemon do on my host, with which login".

The **gap this work leaves** is on the daemon's own behavior, not on the
operator surfaces: on this slice the host-aware pieces exist and are
tested, but the daemon's production wiring does not reach most of them.
Concretely: (1) the daemon's merge wiring
(`newMergeRunner` in `internal/server/merge_run.go`, used by the REST
`POST /api/autonomous/merge/run`, the supervisor loop and the MCP/comm
merge verbs) builds the `mergepipeline.Runner` with the gh-based
`ExecShipRunner` and **never sets `Runner.Hosts` or `Runner.RemoteURL`**,
so `Runner.configuredHost()` is always false in production: the
host-aware `parity_issue` hook (`RunParityIssueOnHost`) is unreachable
from the daemon, the built-in `tag` hook tags an unsigned annotated tag
(`ExecShipRunner.Tag` runs `git tag -a`, `internal/mergepipeline/run.go:758`)
even when the host entry says `sign_tags: true` — the signing-aware helper
`CreateTagForHost` (`internal/git/tag.go:92`) has no caller outside its
tests — and every
ship step runs `gh`/`git` with whatever login is ambient on the machine
rather than the credential source named in `git.hosts`. (2) `PublishRelease`
—the facade's release operation — has no caller: nothing in the merge loop
publishes a release on the host after tagging and pushing. (3) None of the
audited facade operations is reached from a production path: the audit
sink itself is live — `cmd/datawatch/main.go:4033` wires the operator
audit log through `gitpkg.SetHostAuditLog` at daemon start, so
`RecordHostAudit` writes whenever it is called — but its callers are
unreachable: `PublishRelease`, the signing-aware tag helper
`CreateTagForHost` and the `OpenChangeRequest` facade entry have no
non-test callers at all, and `FileIssue` is only reachable through
`RunParityIssueOnHost`, which gap (1) keeps off. The net effect is the
same as a dead sink — the Phase 4 promise of one credential-sourced entry
per MR, issue, release and tag yields zero `host_git_operation` rows from
the daemon's ship path — but the fix is to route the daemon's operations
through the facade, not to add wiring. (4) The repo CI watch's host path
(`pollRefOnHost`) records runs only: no superseded naming, no cancel/rerun
decisions, no alert — the action phase is documented as
"GitHub-specific" and is skipped for host-path runs, so a GitLab-host
repository is watched but never judged, never alerted on, and never
acted on, while the identical failure on the gh path gets the full
treatment. The floor is stated where it should be: `LatestRuns` refuses
the `gitea`, `bitbucket` and `git` kinds with a worded "has no CI watch"
answer rather than a wrong-format success.

## Current behavior (what this work delivers, and where it stops)

The slice's own contract, as built and tested:

| Piece | Delivered |
| --- | --- |
| Host resolution | `ResolveProjectHost` matches the project's remote host against each configured `git.hosts[].base_url` in list order; the built-in github.com/gitlab.com fallbacks exist but the Runner treats them as non-configured — only a real `Hosts` entry selects the facade. |
| Facade operations | `OpenChangeRequest`, `FileIssue`, `PublishRelease` dispatch by kind (GitHub → gh, GitLab → glab, plain-git kinds → `ErrNoForgeAPI` with the "open the change in the host's web interface" wording); each successful call records one `audit.ActionHostGitOperation` entry (host, kind, operation, repo, ref, credential source) and returns the created item's URL. |
| Tags and pushes | `CreateTagForHost` honors the host's `sign_tags`/`signing_key` (`git tag -s [-u KEY]`) or falls back to annotated; `PushBranchRef`/`PushTag` are fast-forward refspecs only; `checkRefName` rejects option injection (`-`), force (`+`) and illegal ref characters; `TagStatus` distinguishes absent / same-commit / other-commit before anything is created. |
| Host CI listing | `LatestRuns` per host kind under a 15-second deadline: `gh run list` for GitHub, `glab api projects/<repo>/pipelines?ref=` for GitLab; one neutral `CIRun` shape; stderr never enters the data; a worded refusal for the plain-git kinds. |
| Repo CI watch host path | A watched repository whose origin resolves to a configured host is listed through the facade and every run recorded into the RepoCIWatchStore with the usual statuses — observation only (see gap 4). |
| Audit entry shape | Names and credential source only; `redactCreds` strips URL-embedded credentials before anything is stored; a failed audit write never fails the operation. |
| Pipeline host hook | `builtinPostShipHook` routes `parity_issue` through `RunParityIssueOnHost` when `configuredHost()` succeeds and through the `ShipRunner` (gh) otherwise — the split is correct and tested with `Runner.Hosts` set directly. |

Where it stops, precisely: the daemon itself never populates `Runner.Hosts`
or `Runner.RemoteURL`, never calls `PublishRelease`, never calls
`CreateTagForHost` (its only tagging path is `ExecShipRunner.Tag`, an
unsigned `git tag -a`), and never takes the host-aware parity-issue
branch — so no audited facade operation, and therefore no
`RecordHostAudit` call, ever happens in the daemon's ship path, even
though the operator audit log *is* wired there (`SetHostAuditLog` at
daemon start, `cmd/datawatch/main.go:4033`). From the operator's point of
view (REST `GET /api/autonomous/merge-pipeline`,
`POST /api/autonomous/merge/run`, the MCP merge-pipeline tools, the comm
merge verbs, the CLI, the PWA's dashboard console reading
`GET /api/autonomous/repo-ci-watch`) the ship step is indistinguishable
from the pre-slice gh-based behavior: an unsigned tag, a push to `origin`,
a `gh`-filed parity issue, no release on the host, and no
`host_git_operation` entries in the audit log — on every host kind. And on a GitLab-host repository the repo CI watch answers the
same read shape (runs, statuses, filterable by `status`) but never
reaches the state that would have produced the alert, the superseded
naming or the cancel/rerun decision, with no field in the record saying
"this repository is watched, actions not yet supported here". The
readiness check from the sibling slice and the per-kind capability matrix
in [2026-11-09-git-provider-capability-parity.md](./2026-11-09-git-provider-capability-parity.md)
tell the operator what is *connected* and what each kind *can*; nothing yet
tells the operator which of these shipped, tested behaviors the daemon is
actually using on this host.

## Expected behavior

Once parity is met, the daemon's own ship-and-watch behavior depends only
on the `git.hosts` entry the project's remote resolves to, identically
across the five kinds as far as each kind's capability allows:

- **The daemon's merge wiring sets `Runner.Hosts` (the configured list)
  and `Runner.RemoteURL` (the project's remote)** for every production
  entry point — the REST merge run, the supervisor loop, the MCP and comm
  merge verbs — so `configuredHost()` is a real branch, not a test-only
  one. A project on a GitHub or GitLab host then files its parity issue
  with that host's provider and credential source; a project on a
  plain-git host gets the worded `ErrNoForgeAPI` refusal from the hook
  record, which is the honest answer for that kind as far as this hook
  goes. A project whose remote matches no configured entry keeps today's
  gh-based `ShipRunner` behavior with no behavior change.
- **The `tag` built-in and the `Ship` tag step honor the host's signing
  policy when a host entry was resolved** — signed with `signing_key`
  when `sign_tags: true`, annotated otherwise — so `sign_tags`/`signing_key`
  in `git.hosts` means what the config and the howto say they mean: today
  `CreateTagForHost` and `TagCreateArgsForHost` exist and pass their tests
  but have no non-test caller at all, so the daemon's only tagging path
  is `ExecShipRunner.Tag`'s unsigned `git tag -a`.
- **The loop publishes the release on the host** (`git.PublishRelease`
  through the resolved host) as the ship step's final leaving-the-machine
  action for hosts that support it, and records the worded refusal for
  kinds that don't — a ship currently leaves the tag on the remote without
  a release, which is the halfway state between "the operator ships by
  hand" and "the daemon ships".
- **The audit trail lands with no new wiring.**
  `SetHostAuditLog` is already called at daemon start
  (`cmd/datawatch/main.go:4033`), so the audit sink is live; once the
  items above route the parity issue, the tag and the release through the
  audited facade calls, every completed operation — PR/MR, issue,
  release, tag — records its `host_git_operation` entry with the
  credential source and never the token into the audit trail the operator
  already reads. The entry shape is already the agreed one
  (`internal/audit/host_git_events.go`); what is missing is reaching the
  operations, not modeling.
- **The repo CI watch's host path reaches the same decisions the gh path
  does**, within the kind's capability: a failed GitLab-host run is
  classified (infra / security / policy / code_or_test / judging / hung use
  the same vocabulary) and, for decisions that translate — the
  superseded naming, the hang alert, the shadow-mode `would_do` record —
  applied; for decisions that are GitHub-specific (cancel and rerun a
  `gh`-run), the record carries a stated "not actioned: no CI action on
  this host kind" note instead of silence, in both shadow and on modes.
  Alternatively the plan states deliberately that non-GitHub hosts are
  observe-only, in which case the record and the docs say so in one place
  and the PWA status does not imply the actions will come.
- **The status reads agree with the behavior.** Whatever the daemon does —
  or says it does not do — per host kind, the merge run record, the
  repo-CI-watch record and `GET /api/autonomous/merge-pipeline` say the
  same thing as `docs/howto/git-hosts.md` and the capability matrix in
  the sibling draft; the mobile clients render the run records they
  already parse without a new required field (each added note is
  additive, following the localization rule and the five-locale set for
  any new operator strings).

## Suggested next step

File one `feat(merge-ship):`-prefixed issue in datawatch covering the
daemon-side wiring in one arc, in dependency order: (1) `newMergeRunner`
sets `Hosts` and `RemoteURL` from the config and the project's remote —
a small change that switches the parity-issue hook, and by extension
every host-aware branch, from test-only to live on the operator's own
hosts; acceptance is a recorded merge run on a GitLab-configured project
filing its parity issue with `glab` (host and credential source visible in
the record) and one on an unconfigured remote behaving exactly as today.
(2) the ship tag step routes through `CreateTagForHost`/the host policy
when a host resolved — acceptance is one signed tag (`git tag -v`
verifiable) from a `sign_tags: true` entry and one annotated tag from a
default entry, with the existing unsigned-behavior tests preserved for the
unconfigured path. (3) the release step: `PublishRelease` as the ship
step's final action with the worded refusal recorded for plain-git
kinds. (4) the audit trail is then observable without new wiring —
`SetHostAuditLog` is already called at daemon start
(`cmd/datawatch/main.go:4033`), so once (1)–(3) route the parity issue,
the tag and the release through the audited facade calls, the acceptance
criterion is one `host_git_operation` entry per completed operation in
the audit log, each carrying the credential source and no token. (5) the host CI-watch
decision — apply the translatable actions with the stated note, or
declare observe-only — landed with its record shape and the howto line,
since it changes what `GET /api/autonomous/repo-ci-watch` reports for
non-GitHub repositories. Reference this draft, the two sibling drafts
([git hosts](./2026-11-09-git-hosts-parity.md) for the config surface,
[provider capability](./2026-11-09-git-provider-capability-parity.md) for
the per-kind matrix — this issue must not duplicate the registry,
`file_upstream_issue` or token-mint items) and the Phase 4 plan; the
registry, the broker's GitLab mint and the hosts settings card stay with
their own drafts. The per-kind floor (gitea/bitbucket/git refusing forge
actions with the worded no-forge-API answer, not a silent success) is
already where it should be and needs nothing but the docs to agree, which
items (2)–(5)'s howto lines deliver.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included | `POST /api/autonomous/merge/run`, `GET /api/autonomous/merge-pipeline`, `POST …/validate` and the `GET /api/autonomous/repo-ci-watch` reads are the surfaces that will show the wired behavior; nothing in the draft changes their contracts — the added note fields (host, credential source, actioned/not-actioned) are additive on records clients already parse. |
| MCP | included | The merge-pipeline tools (`internal/mcp/merge_pipeline_tools.go`) reach the same `newMergeRunner` path, so fixing the wiring fixes the MCP surface too; the repo-CI-watch tools (`internal/mcp/repo_ci_watch_tools.go`) read the records as they land. |
| CLI | included | The CLI's merge and repo-CI-watch verbs (`cmd/datawatch/merge_pipeline.go`, `cmd/datawatch/cli_repo_ci_watch*.go`) read the same records; no new verb proposed, the behavior they report is what changes. |
| comm channel | included | `internal/router/merge_pipeline_comm.go` reaches the same Runner; the host-audit credential-source note is visible wherever the audit log is already read. |
| YAML/config | included | `git.hosts[]` (`sign_tags`, `signing_key`, `credential_source`) and `autonomous.merge_pipeline.*` are where the operator already declares the expected behavior; the gap is that the daemon does not act on the first, not that the config is missing a key. |
| PWA | included | The dashboard console (`api_dashboard_console.go`) and the repo-CI-watch status render the records they already parse; the observe-only / not-actioned note is one additive field, with strings through the five-locale rule. |
| Android | excluded | No app code in this repo. The mobile clients must at minimum keep parsing the merge-run and repo-CI-watch records unchanged; any new required field would be a contract break — everything itemized here is additive, so there is no app work to file. |
| iPhone/iOS | excluded | Same as Android: additive fields only, no client work required. |

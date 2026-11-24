# [sync] Git providers: per-host capability parity (PR/MR, issue, release, token, clone/push)

**Not filed yet.** — this is the tracked draft; the issue is opened when the
operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI and creates
nothing on any host.

**Work this draft captures** — *Part of finishing review and merge by the
daemon* (Phase 4 of the autonomy handover plan, operator 2026-11-07: "b
finsih now we need to be using it to debug it and monitor its right."), PRD
`7bd50001`. In this slice the per-host providers of the daemon's git surface
gained cross-forge capabilities: GitLab merge request creation
(`glab mr create`) and issue creation (`glab issue create`)
(`internal/git/gitlab.go`), GitLab release creation
(`glab release create`, `internal/git/gitlab_release.go`) — GitLab was a
*GitLab — stub that returns ErrNotImplemented* before this work — GitLab
clone and push over the host login
(`internal/git/gitlab_transport.go`, setting the repository's local git
credential helper to `glab auth git-credential` for HTTPS remotes, and doing
nothing for SSH remotes), GitHub release creation
(`gh release create`, `internal/git/github.go`), and a plain-git provider
for the `gitea`, `bitbucket` and `git` host kinds that clones and pushes with
`git` itself and nothing else (`internal/git/generic_git.go`). The provider
registry is `internal/git/provider.go` (`Resolve`, `stubProvider`).

## Affected area

The area is the capability split across the five configured host kinds —
`github`, `gitlab`, `gitea`, `bitbucket`, `git` — in the daemon's own git
layer. Phase 4's design decision (per
[docs/plans/2026-11-03-autonomy-handover-plan.md](../../plans/2026-11-03-autonomy-handover-plan.md))
was that the daemon pushes and tags on its own, that "GitLab [is]
implemented the same way through its own command line tool, other hosts
through plain git over SSH or HTTPS", and that the provider layer gets
"provider-neutral issue and release steps". This slice delivers exactly the
first clause for `gitlab` (glab-backed PR/MR, issue and release, plus
clone/push) and completes `github` (release creation joins its existing
PR, issue and host-login clone/push), while the other three kinds get the
floor only: clone/push, and explicit, worded refusals
(`ErrNoForgeAPI`: "this host has no forge API integration; open the change
in the host's web interface") for everything that needs the host's web API.

The **gap this work leaves** has two axes. The first axis is cross-host
capability parity: `gitea`, `forgejo`-style hosts, `bitbucket` and plain
git still cannot have a pull request, an issue or a release opened by the
daemon, GitLab still cannot hand the token broker a minted token (GitHub
alone can), the project-rules `file_upstream_issue` action resolves the
provider as `resolve("github")` regardless of where the target repo lives,
and `CreateRelease` sits on the concrete types (`GitHub`, `GitLab`,
`GenericGit`) rather than on the `Provider` interface — so "create a
release" and "clone/push" are not part of the parity contract the interface
states, and a future forge that satisfies the interface still cannot be
asked to do either without holding the concrete type. The second axis is
the surfaces the operator can see this from: the readiness check and the
`POST /api/git/hosts/{name}/test` endpoint prove *connectivity* (installed
CLI, login present, scopes right, remote reachable) but say nothing about
*capability*, and no surface — REST, MCP, CLI, comm channel, PWA or the
mobile clients — shows, per host, which of PR/MR, issue, release, token
minting and clone/push the daemon can actually do there. An operator whose
PRD lands on a `gitea` host discovers the capability gap at failure time
(`ErrNotImplemented` from the registry stub), not at configuration time.
Note also the two distinct refusal messages the operator meets: plain-git
kinds answer `ErrNoForgeAPI` (the change is possible, just not by
datawatch), while `gitlab` or an unknown kind at the registry answers
`ErrNotImplemented` (the daemon itself lacks the work) — today those two
mean the same thing in some places and different things in others (a
GitLab broker mint is a genuine daemon gap; a GitLab PR is not one anymore).

## Current behavior (what this work delivers, and what the registry hands out)

The capability matrix as it stands, per configured host `kind`:

| Host `kind` | Open a PR / MR | Create an issue | Create a release | Clone / push | Mint / revoke token |
| --- | --- | --- | --- | --- | --- |
| `github` | `gh pr create` → PR URL | `gh issue create` → issue URL | `gh release create <tag>` → release URL (new with this change) | `git` with the `gh` host login | GitHub-only in the broker (`gh auth token`) |
| `gitlab` | `glab mr create` → MR URL (was a not-implemented stub before) | `glab issue create` → issue URL (new) | `glab release create <tag>` → release URL (new) | `git clone`/`git push`; HTTPS remotes get a local credential helper pointing at `glab auth git-credential`, SSH remotes rely on the SSH agent | not implemented — `ErrNotImplemented`, stated in the package, not hidden |
| `gitea`, `bitbucket`, `git` | `ErrNoForgeAPI` — "open the change in the host's web interface" | same | same | plain `git clone` / `git push` against the base URL (or a full remote), using the host's own credentials | not implemented for these kinds |

Two consumer-side facts make the split visible in the daemon's own behavior.
First, PRD completion opens a PR by resolving the PRD's provider through
`git.Resolve` (`internal/autonomous/git_completion.go`); `Resolve` still
returns GitHub and GitLab only, and everything else — including
`gitea`, `bitbucket` and `git`, even though a usable `GenericGit` provider
exists for them — returns the `stubProvider`, so a PRD completed on one of
those hosts fails at the open-PR step with `ErrNotImplemented` rather than
`ErrNoForgeAPI`. Second, the project-rules `file_upstream_issue` action
calls `resolve("github")` unconditionally
(`internal/autonomous/project_rules_integration.go`), so a finding whose
upstream target lives on GitLab is filed with the wrong provider (and
fails), and a target on any other kind cannot be filed at all — while a
`GitLab` sitting one line down the codebase could open that exact issue. The
release, tag and merge loop itself does not exist yet for any kind:
`CreateRelease` and the clone/push capabilities are in the git package
waiting for the Phase 4 follow-up loop, as the howto states.

On the surfaces: the per-kind capability position is documentation-only.
[docs/howto/git-hosts.md](../../howto/git-hosts.md)
("What the daemon does on each host kind") is where the operator can read
the matrix; the readiness check and connection test
(`internal/git/host_status.go`) report connectivity and scopes only; the two
REST reads covered by the sibling draft
[2026-11-09-git-hosts-parity.md](./2026-11-09-git-hosts-parity.md) carry the
config block with no capability field; nothing in MCP, CLI, comm, PWA or
the mobile clients models "what can the daemon do on this host". All CLI
calls are bounded (a 30-second cap on `gh`/`glab`), vault-sourced tokens
reach only that one run's environment, and failure output passes the
redaction of `redactHostText` (`internal/git/host_status.go`) — the token discipline is not where the gap is.

## Expected behavior

Once parity is met, the daemon either does a git operation on every host
kind it claims to support, or says so at configuration time rather than at
failure time:

- **`gitea`/`forgejo` hosts** — PR, issue and release opened through the
  host's own tooling (a Gitea CLI such as `tea`, or the host's API over
  HTTP), or an explicit plan decision that these kinds are
  clone/push-only by design — in which case the config-level readiness
  check and the docs say that in one place, and the registry stub for
  `gitea` should answer `ErrNoForgeAPI` (the change is possible on the
  host, datawatch just won't do it) instead of `ErrNotImplemented`, which
  reads as "datawatch can't build this yet".
- **`bitbucket` and plain `git`** — `bitbucket` gains issue/PR support
  through Bitbucket's API (or is scoped clone/push-only with the same
  readiness wording); plain `git` stays clone/push-only by definition, but
  its refusal must remain the explicit "no forge API integration" answer
  rather than a registry stub.
- **The registry matches the kinds it configures.** `git.Resolve` knows the
  five configured kinds: `gitea`, `bitbucket` and `git` hand back the
  existing `GenericGit` clone/push provider instead of `stubProvider`, so
  no code path can ask a plain-git host for a forge action it structurally
  cannot have and get a "not implemented" that means "no forge here".
- **`CreateRelease` joins the `Provider` interface** (with `GenericGit`
  answering `ErrNoForgeAPI` and `GitLab`/`GitHub` answering with the URL),
  or the plan states deliberately per-concrete-type release support, so the
  next forge added as "one new file" is held to a parity contract that
  includes the operations the Phase 4 loop will actually call — today the
  contract covers exactly the operations the loop will not call.
- **`file_upstream_issue` resolves the provider of the target repo**, not a
  hard-coded `github` — a finding with a GitLab upstream target files the
  issue with `glab`, and one with any other target gets that kind's honest
  answer, with the decision recorded instead of the error logged.
- **Token minting is scoped per kind in one place.** Either GitLab gains a
  mint via its own tooling so the broker works on every forge kind, or the
  broker's documentation lists per-kind mint support (GitHub only) as a
  stated, versioned gap — either way the readiness check and the docs agree
  about what `credential_source: broker` means on a non-GitHub host.
- **One capability statement per host, on the surfaces that list hosts.**
  The hosts list (REST `GET /api/git/hosts`, and wherever the config block
  is rendered — PWA settings card, MCP/CLI/comm reads, the mobile clients)
  carries per host which of PR/MR, issue, release, clone/push and token
  mint the daemon does on that kind, so "can datawatch push this plan to my
  Gitea?" is answered before the plan runs, not after the open-PR step
  fails. New strings follow the localization rule (all five locale bundles)
  and the app-issue step of the Mobile-Parity Rule.

## Suggested next step

Split the filing by owner. (1) One `feat(git-providers):`-prefixed issue in
datawatch covering the kind gap on the daemon side in one arc: register
`gitea`/`bitbucket`/`git` in `git.Resolve` to the existing `GenericGit`
provider (an immediate behavior fix, no new capability), give `GenericGit`
— and any later Gitea/Bitbucket forge support — the honest `ErrNoForgeAPI`
/ `tea` / API split, decide `CreateRelease` on the interface vs per-type,
and point `file_upstream_issue` at the target repo's kind; reference this
draft, the capability matrix in `docs/howto/git-hosts.md`, and
`internal/git/provider.go` as the contract to extend. (2) GitLab token
minting for the broker is a second, smaller issue — it blocks
`credential_source: broker` on GitLab and nothing else in this slice.
(3) The capability statement on the operator surfaces belongs with the
git-hosts settings card in the sibling draft
[2026-11-09-git-hosts-parity.md](./2026-11-09-git-hosts-parity.md): add to
that card's acceptance criteria a per-host capability line driven by the
same matrix, so the `GET /api/git/hosts` response gains one field per
capability rather than a second endpoint. (4) The release, tag and merge
loop that consumes `CreateRelease` and the clone/push paths stays in
[plans/2026-11-03-autonomy-handover-plan.md](../../plans/2026-11-03-autonomy-handover-plan.md),
Phase 4, and is not filed separately. Nothing here changes a REST or MCP
contract an existing client parses — the capability statement is additive —
so it is a display-and-contract story, not a migration.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | partial — tracked | `GET /api/git/hosts` and `POST …/test` (landed in the sibling slice) prove connectivity and scopes per host but carry no per-kind capability; the capability line for the hosts listing is tracked here. |
| MCP | partial — tracked | `get_config` with `section: git` returns the block; no forge capability is readable and no forge action is callable — by design (forge work is the daemon's, not a remote call the client makes), the per-kind capability read is still tracked here. |
| CLI | partial — tracked | `datawatch config get git` reads the block; no per-kind capability in the `git hosts` read, tracked with the sibling draft's CLI item. |
| comm channel | partial — tracked | The generic `config` reads carry the block; "can the daemon do X on host Y" is not answerable in chat today; tracked here. |
| YAML/config | included | `git.hosts` is where the kind is chosen and where the per-kind behavior contract (including the clone/push-only kinds) is documented in `docs/config-reference.yaml`; kind validation lives here. |
| PWA | partial — tracked | The block renders under `GET /api/config`; the settings card's per-host capability line is tracked with the sibling draft. |
| Android | excluded | No app code in this repo. The mobile clients must at minimum not render a host of any kind as fully capable; the capability line lands with the sibling draft's card issue — implementation parity is not required. |
| iPhone/iOS | excluded | Same as Android. |

# [sync] Git hosts: config, readiness and connection test — parity across the operator surfaces

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI and creates
nothing on any host.

**Work this draft captures** — *Part of finishing review and merge by the
daemon* (Phase 4 of the autonomy handover plan, operator 2026-11-07: "b
finsih now we need to be using it to debug it and monitor its right."), PRD
`7bd50001`. In this slice the daemon gained its configured list of git hosts
(`git.hosts` in `internal/config/config.go`, `internal/config/git_hosts.go`)
and the first live surfaces around it: a per-host readiness check and a
connection test (`internal/git/host_status.go`, `internal/git/host_connection.go`),
vault-token environment injection for host commands
(`internal/git/host_env.go`), and two read-only REST reads
(`internal/server/git_hosts_api.go`, routed in `internal/server/api.go`):
`GET /api/git/hosts` and `POST /api/git/hosts/{name}/test`.

## Affected area

The area is the credential-and-host model the autonomy handover plan builds
the daemon's own push-and-tag on — Phase 4, "review and merge by the daemon",
where the daemon reviews, merges, pushes and tags on the operator's behalf and
every push or tag is audit-logged with its credential source and never the
token. A git host is one entry in the daemon's named list of push targets: a
GitHub, a GitLab, a Gitea, a Bitbucket or a plain git remote — with its
`kind`, its `base_url` (required for the self-hosted kinds; empty means
github.com or gitlab.com for those), how its token is obtained
(`credential_source`: `host_login` (default), `vault`, or `broker`), the
secrets-vault entry **name** holding the token when the source is `vault`
(`vault_secret` — the name only, the value is never stored in the config),
and `sign_tags`/`signing_key` for signed tags. The list is validated at
config load, every problem reported together, and one invalid entry fails the
load. The dotted-key addressing scheme `git.hosts.<name>.<field>`
(`GetGitHostField` / `SetGitHostField`) is the one addressing the later
surfaces — REST, MCP, CLI, comm channel — are meant to share, with the host
`name` as the immutable key.

The **gap this work leaves** is that of all the parity-surface set, the
dedicated live surface is REST alone. The config file is the only write
surface. The PWA has no git-hosts settings card, no per-host readiness view
and no Test connection button — the PWA can only see the raw `git.hosts`
block under `GET /api/config`. There is no `git hosts` CLI subcommand, no
dedicated `git_hosts_*` MCP tool, and no dedicated comm-channel verb: MCP,
CLI and comm reach the hosts only through the generic `get_config`
(`section: git`) read, and the connection test is reachable off-channel only
via a comm channel's REST passthrough where one exists. And on the
operator-facing mobile clients the gap is the full one: a client that lists
its configuration will meet a new `git.hosts` block it has no model for, and
it has no way to read the readiness of a configured host or run the
connection test at all. This matters more than an ordinary settings gap,
because the next plan in Phase 4 — the daemon's own push-and-tag — will read
these exact entries and will report, per push and per tag, the credential
source used; the operator who cannot see, test or rotate the host entries
from a settings surface is debugging the daemon's push by hand-editing the
config file.

## Current behavior (what this work delivers on the server and PWA)

On the server the area is complete as this slice intends it to be. The
config model carries defaults (`credential_source` reads `host_login` when
empty) and validation (slug names, known kinds, known credential sources,
`vault` requires a `vault_secret` name, self-hosted kinds require a
`base_url`), applied at load and, for single-field writes, re-validated over
the whole list so a bad write is refused and the stored list is unchanged.
The readiness check (`CheckHostReadiness`) runs the host's own tooling with
a 15-second deadline on every external command — `gh auth status` (checking
the `repo` and `workflow` scopes) for GitHub, `glab auth status` (checking
the `api` scope) for GitLab, `git ls-remote` reachability for Gitea,
Bitbucket and plain git — and reports a plain-language problem with a
`fix_command` when anything is missing, without ever returning a token; a
token-shaped value that reaches the answer (`ghp_…`, `github_pat_…`,
`glpat-…`, `user:password@host`) is replaced with `[redacted]`. The
connection test (`TestHostConnection`) first runs the readiness check — a
readiness problem returns `ok: false` with the problem and fix and does not
probe — then one read-only probe (`gh api user`, `glab api user`, or
`git ls-remote`) under the same deadline, reporting the account it connected
as. Vault credentials work at the command level: the token is read through
the vault resolver and placed only in that one run's environment slice as
`GH_TOKEN` (github), `GITLAB_TOKEN` (gitlab) or `DATAWATCH_GIT_TOKEN`
(everything else), never in the daemon's own environment, the config, or an
error message. `GET /api/git/hosts` lists every configured host with its
live readiness; `POST /api/git/hosts/{name}/test` runs the test for one,
with 404s for any other path shape and for an unknown name. Both
authenticate as the config-read federation capability.

The parity position per surface, as it stands:

| Surface | Read the hosts | Test one host | Write the hosts |
| --- | --- | --- | --- |
| YAML/config | `git:` → `hosts:` in the config file | — | the config file is the **only** write surface |
| REST | `GET /api/git/hosts` (live readiness), `GET /api/config` (raw block) | `POST /api/git/hosts/{name}/test` | — |
| MCP | `get_config` with `section: git` (raw block) | — (REST-only) | — |
| CLI | `datawatch config get git` (or `git.hosts`) | — (REST-only) | the generic config interface only; no `git hosts` subcommand |
| comm channel | the generic `config` reads carry the block | only via a channel's REST passthrough | — |
| PWA | the raw block under `GET /api/config`, with `credential_source` shown as `host_login` | — | — |
| Android, iPhone/iOS | (parity question, tracked below) | (parity question) | (parity question) |

The operator guidance lives in the new
[docs/howto/git-hosts.md](../../howto/git-hosts.md), the *Git hosts* entry
in [docs/datawatch-definitions.md](../../datawatch-definitions.md), the
*Git hosts* operations section of
[docs/operations.md](../../operations.md) (including its "not here yet"
review items), the two endpoints in `docs/api/openapi.yaml`, and the doc
indexes in `README.md` and `docs/README.md`. The howto's *What is not here
yet* list names the deliberate gaps: no push path consumes `git.hosts` yet
(the Phase 4 follow-up), no host write surface beyond the config file, both
REST reads authenticate as config-read (a separate git-hosts capability is
an open review item), and `sign_tags`/`signing_key` are configured but
nothing signs with them yet.

## Expected behavior

Once parity is met, every operator surface that can read config can also see
the git hosts as first-class entries rather than a raw block. Concretely:

- **PWA** — a Git hosts settings card lists every configured host with its
  kind, credential source (a vault host shown by its secret **name** only)
  and its readiness state, and a Test connection button per host calls
  `POST /api/git/hosts/{name}/test` and renders `ok`, `account`, `message`
  and `fix_command`. The readiness and test responses are new shapes the PWA
  has not rendered before; they must render like any other settings detail,
  with a missing `repo`/`workflow`/`api` scope shown as a named problem with
  its fix command, and no response field ever carrying a token. New strings
  go through the localization rule (all five locale bundles) and the
  app-issue step of the Mobile-Parity Rule.
- **MCP** — a dedicated git-hosts read (list with readiness) and, if the
  operator wants tool-level access, the connection test, or an explicit
  reason in the plan section that `get_config`'s raw block is deliberately
  the whole MCP surface until the push path lands.
- **CLI** — a `git hosts` read naming the same readiness fields the REST
  read returns, and a way to run the test for one host without a raw `curl`;
  or the dotted-key `git.hosts.<name>.<field>` write wired through the
  generic config set so a single field can be changed without editing the
  file — the addressing scheme already exists in the config model, it is not
  wired anywhere yet.
- **comm channel** — a host read and, where the channel has a REST
  passthrough, the test, so "is my host logged in" is a chat question, not a
  curl.
- **Android and iPhone** — capability parity with the PWA: the card, the
  readiness view and the Test button (each platform idiomatic), driven by
  the same two REST endpoints; implementation parity is not required. In
  the absence of all of the above, each client must at minimum not misrender
  or drop the new `git.hosts` block it meets in `GET /api/config`, and must
  never display a secret value where the server sends only a secret **name**
  or a `[redacted]` marker.

## Suggested next step

File one `feat(git-hosts):`-prefixed issue in datawatch-app covering the
card, the readiness view and the Test connection button on Android and
iPhone, referencing this draft and `docs/howto/git-hosts.md` as the source
of truth for wording, shapes and the token rule; the acceptance criteria are
a pass-through of the two REST responses (a seeded host list with one
missing-scope problem, and one successful plus one failed test) displayed
correctly on both platforms, plus the five-locale strings. The non-app
surfaces (the MCP tools, the CLI verb or the dotted-key write, the comm
verb) belong with the Phase 4 push-and-tag follow-up in
[2026-11-03-autonomy-handover-plan.md](../../plans/2026-11-03-autonomy-handover-plan.md)
rather than a separate issue, so that the host model, its surfaces and its
consumer land as one arc — the review item about whether `POST
/api/git/hosts/{name}/test` deserves its own capability gate should be
answered in that follow-up, because a push path will make the test's
capability question concrete. Nothing in this slice changes a REST or MCP
contract an existing client parses — the two endpoints are new and the
`git.hosts` config block is additive — so the app work is a
display-and-settings story, not an integration story.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included | `GET /api/git/hosts` (live readiness) and `POST /api/git/hosts/{name}/test` land in this slice, both guarded by the config-read federation capability; the raw block also reads under `GET /api/config`. |
| MCP | partial — tracked | `get_config` with `section: git` returns the raw block; there is no `git_hosts_*` tool and the readiness and test answers are REST-only. A dedicated read (and test, if wanted) is tracked here and lands with the Phase 4 push follow-up. |
| CLI | partial — tracked | `datawatch config get git` reads the block from the running daemon; there is no `git hosts` subcommand and the `git.hosts.<name>.<field>` dotted-key write the config model exposes is not wired to a channel yet. Tracked here. |
| comm channel | partial — tracked | The generic `config` reads carry the block and the connection test is reachable only via a channel's REST passthrough; no dedicated git-hosts verb. Tracked here. |
| YAML/config | included | `git.hosts` in `config.yaml` is the write surface, with defaults applied and the whole list validated at load; the block is commented in `docs/config-reference.yaml`. |
| PWA | included (read-only) | The PWA sees the `git.hosts` block under the generic `GET /api/config`; the settings card with readiness and the Test connection button is not built yet — that is exactly the tracked gap above. |
| Android | excluded | No app code lives in this repo. Capability parity is required and is tracked by this draft: the hosts card, the readiness view and the Test connection button via the two existing endpoints — implementation parity is not. |
| iPhone/iOS | excluded | Same as Android: no code in this repo, and the same three items are tracked here for the native client. |

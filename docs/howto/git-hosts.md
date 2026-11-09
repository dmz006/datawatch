---
docs:
  index: true
  topics: [git, hosts, push, credentials, vault, readiness, connection-test, tag-signing, github, gitlab, gitea, bitbucket]
exec_steps:
  - tool: get_config
    description: Read the git.hosts block — the list of configured git hosts
    args: {section: git}
    read_only: true
---

# How to: Configure your git hosts (push targets)

`git.hosts` is the daemon's named list of git hosts it may push to. Each entry names one host — a GitHub, a GitLab, a Gitea, a Bitbucket, or a plain git remote — how its token is obtained (`host_login` by default; optionally a secrets-vault entry by name, or the credential broker), and whether the tags pushed to it are signed. The list is validated at config load: every problem in the list is reported together, and one invalid entry fails the whole load.

With this change you also get **readiness and connection-test answers** over the daemon's web API: one read lists every configured host with its login state, account, scopes and a plain-language fix when something is missing; another runs the connection test for one host. Both are read-only — and neither read returns a token. A vault-backed host is shown by its secret **name** only.

> **Where this sits:** `git.hosts` is the credential and host model the autonomy handover plan builds the daemon's own push-and-tag on ([docs/plans/2026-11-03-autonomy-handover-plan.md](../plans/2026-11-03-autonomy-handover-plan.md), Phase 4 — "the daemon pushes and tags, configurable, per project"). With this change the daemon's per-host git work is implemented behind this model: it can open a PR or merge request on GitHub **and** GitLab, create issues on both, create releases on both, and clone and push to every host kind — including the self-hosted kinds, where clone/push are plain `git` and pull requests, issues and releases stay manual (see ["What the daemon does on each host kind"](#what-the-daemon-does-on-each-host-kind) below). The release-and-merge loop that consumes the `git.hosts` list per project is the follow-up; until it lands, the hosts you configure here are the ones its checks use and, later, its pushes will use.

## Base requirements

- A running daemon (web server) you can reach over HTTPS or your configured web port; the REST reads authenticate the way the rest of the API does (bearer token where you use one).
- The host's own tooling, matching the host `kind`:
  - `github` — the `gh` CLI installed and logged in (`gh auth status`), or a vault entry with a token;
  - `gitlab` — the `glab` CLI installed and logged in (`glab auth status`), or a vault entry;
  - `gitea`, `bitbucket`, `git` — `git` is enough; the base URL is reached over the wire.
- For `credential_source: vault` — the secrets vault entry you name in `vault_secret` exists and holds the host token. The config carries the entry **name** only; the secret value is never stored in the config, the process environment, an error message or any response.

## Setup: writing `git.hosts`

`git.hosts` is a list in the config file (where your daemon keeps `~/.datawatch/config.yaml` and friends — the `git:` block already holds `push_on_complete`, `push_remote` and `secret_scan` next to it). An empty list means none; the field is commented in [`config-reference.yaml`](../config-reference.yaml):

```yaml
git:
  hosts: []
  #   - name: prod-github
  #     kind: github                       # github | gitlab | gitea | bitbucket | git
  #     # credential_source: host_login    # default — the gh/glab login already on this machine
  #     sign_tags: false                   # sign the tags datawatch creates for this host
  #
  #   - name: work-gitea
  #     kind: gitea
  #     base_url: https://git.example.test # required for gitea, bitbucket and git
  #     credential_source: vault
  #     vault_secret: gitea-token          # the vault entry NAME holding the token — never the value
```

The two entries from the walkthrough, filled in:

```yaml
git:
  hosts:
    - name: prod-github
      kind: github
      # No base_url: empty means github.com. Empty credential_source reads
      # host_login — the operator's own `gh auth login`, which carries the
      # repo + workflow scopes.
      sign_tags: false

    - name: work-gitea
      kind: gitea
      base_url: https://git.example.test
      credential_source: vault
      vault_secret: gitea-token
```

### Field rules

| Field | Required | Rules and defaults |
| --- | --- | --- |
| `name` | yes | Unique per list; a lowercase slug (`a-z`, `0-9`, single hyphens). It is the key — it cannot be changed after the host exists, and it is the `<name>` in `git.hosts.<name>.<field>` and in `/api/git/hosts/<name>/test`. |
| `kind` | yes | One of `github`, `gitlab`, `gitea`, `bitbucket`, `git`. |
| `base_url` | by kind | Required for `gitea`, `bitbucket` and `git` (they have no public default). For `gitlab` only when self-hosted (empty = gitlab.com); for `github` empty = github.com. Used here only for its **host part** and as the CLI's `--hostname`. |
| `credential_source` | no | `host_login` (default), `vault` or `broker`. An empty value reads `host_login`. |
| `vault_secret` | for `vault` | The secrets-vault entry **name** holding the token. Required when `credential_source: vault`; the name only, never the value. |
| `sign_tags` | no | `true`/`false` (default `false`): sign the tags datawatch creates for this host. |
| `signing_key` | no | Optional key id used when signing (`sign_tags: true`); empty uses git's default signing key. |

**Validation runs one pass and reports every problem together** — a bad slug, an unknown kind, a `vault` source with no `vault_secret`, or a self-hosted kind with no `base_url` — all in one error at config load, so you fix the whole list in one pass rather than one-by-one. A duplicate `name` is a problem of its own.

### What the `credential_source` choices mean at use time

The source tells the daemon where the token comes from when a command runs against the host:

- **`host_login` (default, recommended).** The login already configured on the host — `gh auth login` for GitHub, `glab auth login` for GitLab. The process environment passes through unchanged and nothing is added. This is the way you run it on your own machine, and the way the [autonomy handover plan](../plans/2026-11-03-autonomy-handover-plan.md) decided it should stay the recommendation.
- **`vault`** (the fallback for servers and clusters). The token is read from the secrets vault entry named in `vault_secret` and placed in the command's environment under `GH_TOKEN` (github), `GITLAB_TOKEN` (gitlab) or `DATAWATCH_GIT_TOKEN` (gitea, bitbucket, git) — only in that one run's environment slice, never in the daemon's own environment, never in the config, never in an error message. An error names the host and the secret — never a secret value.
- **`broker`.** The credential broker supplies tokens to worker sessions; the daemon adds no host-side token itself. The source is still reported so a later audit log can name it — the invariant the plan sets: *every push or tag audit-logged with the credential source and never the token*.

## What the daemon does on each host kind

The daemon's git work is done by the per-host providers in `internal/git`, each matching the host's own tooling — the same login the readiness check above verifies. What each host family supports, after this change:

| Host `kind` | Open a PR / MR | Create an issue | Create a release | List a ref's CI runs | Clone / push |
| --- | --- | --- | --- | --- | --- |
| `github` | `gh pr create` → returns the PR URL | `gh issue create` → issue URL | `gh release create <tag>` → release URL | `gh run list --repo <owner/name> --branch <ref>` — used by the [repo CI watch](automation-repo-ci-watch.md) | `git` with the `gh` login |
| `gitlab` | `glab mr create` → returns the merge request URL — **new with this change**; it was a not-implemented stub before | `glab issue create` → issue URL — **new** | `glab release create <tag>` → release URL — **new** | `glab api projects/<owner%2Fname>/pipelines?ref=<ref>` — used by the [repo CI watch](automation-repo-ci-watch.md) | `git clone` / `git push`; for HTTPS remotes the repository's local git config sets the credential helper to `glab auth git-credential`, so git uses the `glab` host login directly — no token is read, stored or logged by datawatch. An SSH remote changes nothing and relies on the SSH agent |
| `gitea`, `bitbucket`, `git` | — no forge API: the daemon says so explicitly ("this host has no forge API integration; open the change in the host's web interface") | — same | — same | — same: the CI list is not attempted, the watch records the host as unavailable for listing | plain `git clone` / `git push` against the base URL (or a full remote), using the host's own credentials |

Notes the operator should keep in mind:

- **Every CLI call is bounded** (a 30-second cap on the `gh`/`glab` commands; the CI-run list is under the same 15-second ceiling as the readiness reads) and the commands run with the environment the credential source provides — a `vault`-sourced host's token appears only in that one run's environment, exactly as in the [credential-source table](#what-the-credential_source-choices-mean-at-use-time).
- **Host operations are audited.** Every completed facade call — opening a change request, filing an issue, publishing a release, creating a host tag — writes one entry to the operator audit log under the action `host_git_operation`, naming the host **name** and kind, the operation (`mr`, `issue`, `release` or `tag`), the repository, the ref or title, and the credential source (`host_login`, `vault` or `broker`). A token is never part of the entry, and a failed audit write never fails the host operation itself: the operation stands, the log misses one line. Read it with the [Audit & Access Logging](audit-logging.md) howto (`GET /api/audit?action=host_git_operation`). The CI-run list the repo CI watch takes is a read and writes no audit entry of its own.
- **The tag primitive respects `sign_tags`.** A tag created against a host entry that has `sign_tags: true` and a signing key (named in `signing_key`, or git's default key when empty) is **signed**; `sign_tags: true` **without** a key is refused with the missing key named; `sign_tags` off produces an annotated tag. As of this change no shipping path calls it yet — the merge pipeline's `tag` step and the post-ship `tag` hook still tag locally with plain `git tag` — so `sign_tags`/`signing_key` remain configured-but-unused until the push-and-tag loop lands (see ["What is not here yet"](#what-is-not-here-yet-flagged-for-the-operator)).
- **GitLab token minting is still not implemented** — the broker's mint flow is GitHub-only (`gh auth token`) as of this change; GitLab and the plain-git kinds return a not-implemented error for mint/revoker rather than failing silently.
- **The release-creation and clone/push capabilities are in the git package, not yet in a release loop.** Opening a PR/MR and creating issues is already wired into the daemon (PRD completion opens the PR via the PRD's own git provider — a GitLab PRD now opens a merge request instead of failing — and the project-rules `file_upstream_issue` action creates an issue, which today resolves the `github` provider); the per-project release, tag and merge loop is the Phase 4 follow-up listed under ["What is not here yet"](#what-is-not-here-yet-flagged-for-the-operator). Two live consumers already reach this list: the **repo CI watch** lists a watched repository's runs through the host when its origin belongs to one of your entries ([howto](automation-repo-ci-watch.md) — read-only; the watch records those runs and never acts on them), and the **post-ship hook `parity_issue`** files the companion issue through the host when the merge pipeline's runner is given the project's remote and your host list ([howto](post-ship-hooks.md)).

## Walkthrough

**1. Add the hosts** to your `git:` block as above and let the daemon carry the config (edit + restart, or your usual config route). If the list is invalid, the load fails with every problem named — fix and reload.

**2. Read the list back — it is the raw config.**

```bash
$ datawatch config get git
{
  "push_on_complete": "branch",
  "push_remote": "origin",
  "hosts": [
    {
      "name": "prod-github",
      "kind": "github",
      "credential_source": "host_login"
    },
    {
      "name": "work-gitea",
      "kind": "gitea",
      "base_url": "https://git.example.test",
      "credential_source": "vault",
      "vault_secret": "gitea-token"
    }
  ]
}
```

The same block reads back under `git` from `GET /api/config` and, on MCP, from `get_config` with `section: git`.

**3. Read the readiness of every host — it runs the check live.**

```bash
$ curl -s https://127.0.0.1:8443/api/git/hosts
{
  "hosts": [
    {
      "name": "prod-github",
      "kind": "github",
      "credential_source": "host_login",
      "readiness": {
        "host": "prod-github",
        "kind": "github",
        "logged_in": true,
        "account": "dmz006",
        "scopes": ["repo", "workflow", "read:org", "gist"],
        "has_repo_scope": true,
        "has_workflow_scope": true
      }
    },
    {
      "name": "work-gitea",
      "kind": "gitea",
      "base_url": "https://git.example.test",
      "credential_source": "vault",
      "vault_secret": "gitea-token",
      "readiness": {
        "host": "work-gitea",
        "kind": "gitea",
        "logged_in": true
      }
    }
  ]
}
```

What the readiness result tells you, per `kind`:

- **github** — `gh auth status` runs on the configured hostname; you get `logged_in`, `account`, `scopes`, and `has_repo_scope` / `has_workflow_scope`. Missing the `repo` or `workflow` scope is a named problem with a `fix_command` (`gh auth refresh -s repo,workflow`, plus `-h <host>` off the public default); `gh` not installed is its own problem with `gh auth login` as the fix.
- **gitlab** — `glab auth status` on the hostname (gitlab.com when empty); `has_api_scope` is checked and a missing `api` scope is a problem with `glab auth login` as the fix.
- **gitea / bitbucket / git** — reachability is what gets checked: `git ls-remote` against the base URL under a 15 second deadline. Any credential-shaped value in a base URL (a `user:password@host`) is stripped from the answer (shown `[redacted]`); an unreachable host reports so, without the token.

The readiness read **never returns a token**: only the account name, the scope names, a plain-language problem and a fix command. Every external command it runs is bounded by a 15 second deadline.

**4. Run the connection test for one host — it is the readiness check plus one live probe.**

```bash
$ curl -s -X POST https://127.0.0.1:8443/api/git/hosts/prod-github/test
{
  "host": "prod-github",
  "ok": true,
  "account": "dmz006",
  "message": "Connected to github.com as dmz006."
}
```

```bash
$ curl -s -X POST https://127.0.0.1:8443/api/git/hosts/no-such-host/test
{"error":"git host not found"}   # HTTP 404
```

The test first runs the readiness check; when that reports a problem it returns `ok: false` with the readiness problem and fix command and does not probe. When the host is ready it runs one read-only probe under the same 15 second deadline — `gh api user` for github, `glab api user` for gitlab, `git ls-remote` for the plain kinds — and reports the account it connected as. A 15-second timeout reads "…timed out after 15 seconds."; a missing CLI reads "…the `gh` command is not installed…"; a probe that can't read the account reads "…the login may have expired." with the `gh auth login` / `glab auth login` fix.

## All options (per channel)

With this change the dedicated surfaces for git hosts are the config file (the write surface) and the two read-only REST endpoints; the generic config reads of every channel carry the block. No channel has a dedicated git-hosts verb yet — see "What is not here yet".

| Channel | Read the list | Test one host |
| --- | --- | --- |
| **Config file (write)** | `git:` → `hosts:` in the config file — the only write surface; the block is commented in [`config-reference.yaml`](../config-reference.yaml). A dotted key `git.hosts.<name>.<field>` (`name`, `kind`, `base_url`, `credential_source`, `vault_secret`, `sign_tags`, `signing_key`) is the addressing scheme the config model exposes for later surfaces; renaming a host is not one of them (`name` is the key). The host must exist — a host is created by writing the whole list, not by a dotted key. | — |
| **Web / PWA** | `GET /api/config` shows the `git.hosts` block with defaults applied (`credential_source` filled to `host_login`). No dedicated card yet — the plan's "Git hosts settings card … with a readiness check … and a Test connection button" is the follow-up in [the handover plan](../plans/2026-11-03-autonomy-handover-plan.md). | `POST /api/git/hosts/<name>/test` is the endpoint a card would call. |
| **REST API** | `GET /api/git/hosts` lists every host with its live readiness; `GET /api/config` reads the raw block. Both authenticate like the rest of the API and both require the config-read federation capability. | `POST /api/git/hosts/<name>/test` runs the connection test; any path under the prefix that is not `<name>/test` is a 404, and an unknown name is a 404. |
| **MCP** | `get_config` with `section: git` returns the `git` block (the hosts list inside it). There is no dedicated `git_hosts_*` tool — the readiness and test answers are REST-only as of this change. | — (REST-only) |
| **CLI** | `datawatch config get git` (or `datawatch config get git.hosts`) reads the block from the running daemon, defaults applied. Writing: `datawatch config` is the generic config interface (get/set); there is no `git hosts` subcommand in this tree — the config file is the primary write surface. | — (REST-only) |
| **Comm channel** | The generic `config` reads carry it; a channel with the REST passthrough can run `GET /api/git/hosts`. No dedicated git-hosts verb in this tree. | `POST /api/git/hosts/<name>/test` via the REST passthrough |

**The token rule — every read, every channel.** No surface returns a token. The vault credential appears in every response as its secret **name** (`vault_secret`); readiness and test results carry only the account, scope names, a plain-language problem and a fix command; any token-shaped value that reaches an answer (a credential in a base URL, a `github_pat_`…/`glpat-…`/`ghp_`…-style personal access token, a `user:password@` in a URL) is replaced with `[redacted]` before the response is written. The [secrets howto](secrets-manager.md) is how the vault entry itself is created and managed.

## What is not here yet (flagged for the operator)

These are deliberate gaps in *this* change, not bugs — they follow the plan's order (see [docs/plans/2026-11-03-autonomy-handover-plan.md](../plans/2026-11-03-autonomy-handover-plan.md), Phase 4). They are flagged here so the docs state them:

1. **No release-and-merge loop consumes `git.hosts` yet.** The config model, readiness check, the two REST reads and the per-host providers (PR/MR open, issue and release creation, clone/push — see ["What the daemon does on each host kind"](#what-the-daemon-does-on-each-host-kind)) land in this change; the daemon's own per-project push-and-tag loop (release path, direct push vs pull-request, signed tags) is the follow-up in [plans/2026-11-03-autonomy-handover-plan.md](../plans/2026-11-03-autonomy-handover-plan.md), Phase 4. Until it lands, `sign_tags`/`signing_key` are configured but nothing in a shipping path signs with them (the [host tag primitive](#what-the-daemon-does-on-each-host-kind) implements the signing, nothing calls it yet), and release creation is a capability the loop will call rather than one that runs today. What is live on the list: the PRD/project facade operations (each audit-logged as `host_git_operation`), the repo CI watch's read-only host run listing, and the post-ship `parity_issue` host routing (its issue filing also lands as a `host_git_operation` entry).
2. **No forge API for the plain-git kinds.** For `gitea`, `bitbucket` and `git` hosts the daemon opens the change in *your* browser: pull requests, issues and releases return the explicit "no forge API integration" answer instead of being attempted. Clone and push work; everything that needs the host's web API is manual.
3. **GitLab token minting is unimplemented.** The broker's mint flow is GitHub-only (`gh auth token`); a mint/revoker request for `gitlab` or a plain-git kind returns a not-implemented error rather than a token.
4. **No host write surface beyond the config file.** The config model exposes `GetGitHostField` / `SetGitHostField` as the dotted-key addressing (`git.hosts.<name>.<field>`) for later surfaces — MCP `config_set`, CLI, a comm verb, or a config file edit — but none of those is wired to git hosts in this tree, and a host is created by writing the list, not by a dotted key (`name` is the key). The Git hosts **settings card** with an in-UI **Test connection** button is still to be built.
5. **`GET /api/git/hosts` and `POST …/test` authenticate as config-read.** They require the config-read federation capability; there is no separate git-hosts capability yet. Whether an operator wants the connection test behind its own gate is a review item.
6. **`vault` credentials and the broker are documented here by their contract**, not by a UI: the vault entry is created via the [secrets surface](secrets-manager.md), and the broker supplies tokens to worker sessions on its own. Confirm the `vault_secret` entry name matches what your vault holds before relying on `credential_source: vault`.

## Further reading

- [`config-reference.yaml`](../config-reference.yaml) — the `git.hosts` block and its comments (defaults and required-vs-optional per field).
- [`datawatch-definitions.md`](../datawatch-definitions.md) — the **Git hosts** entry (what a git host is, the credential sources, the token rule).
- [`operations.md`](../operations.md) — the **Git hosts** operations section (how to add a host, how to read and test it, how to rotate a vault credential, how to remove a host, and the "not here yet" list).
- [`secrets-manager`](secrets-manager.md) — creating and managing the vault entry a `vault`-sourced host names.
- [`merge-pipeline.md`](merge-pipeline.md) / [`automata-checks-and-ship.md`](automata-checks-and-ship.md) — where the daemon's push-and-tag loop will run once the Phase 4 follow-up lands, and where PR/MR opening and issue creation already run today.
- [`automation-repo-ci-watch.md`](automation-repo-ci-watch.md) — the standing CI watch; watched repositories on a `github`/`gitlab` host of yours have their runs listed through the host (read-only).
- [`post-ship-hooks.md`](post-ship-hooks.md) — the post-ship hooks that can file the companion parity issue through the host.
- [`audit-logging.md`](audit-logging.md) — the `host_git_operation` entries the facade operations write, and how to read them.
- [plans/2026-11-03-autonomy-handover-plan.md](../plans/2026-11-03-autonomy-handover-plan.md) — Phase 4, the plan this host model is building toward.

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
| `vault_secret` | for `vault` | The secrets-vault entry **name** holding the token. Required when `credential_source: vault`; the name only, never the value. On a `host_login` host it is optional and names a fallback secret, read only when the host login is not available. |
| `sign_tags` | no | `true`/`false` (default `false`): sign the tags datawatch creates for this host. |
| `signing_key` | no | Optional key id used when signing (`sign_tags: true`); empty uses git's default signing key. |

**Validation runs one pass and reports every problem together** — a bad slug, an unknown kind, a `vault` source with no `vault_secret`, or a self-hosted kind with no `base_url` — all in one error at config load, so you fix the whole list in one pass rather than one-by-one. A duplicate `name` is a problem of its own.

### Setup: writing the fields after the host exists

Once a host is in the list, its **individual fields** are writable without touching the file — the dotted key `git.hosts.<name>.<field>` (`name` must be the host's existing `name`, `field` one of the fields above) is accepted by the three write channels below, all of which route it through the daemon so the running config applies it immediately:

```bash
# REST — a single field...
curl -s -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    -d '{"git.hosts.work-gitea.base_url": "https://git2.example.test"}' http://datawatch.local/api/config
# ...or two fields in one write (credential source and its secret together):
curl -s -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    -d '{"git.hosts.prod-github.credential_source": "vault", "git.hosts.prod-github.vault_secret": "gh-push"}' http://datawatch.local/api/config

# CLI
datawatch config set git.hosts.work-gitea.base_url https://git2.example.test

# Comm channel (reply echoes the key, not the stored value)
configure git.hosts.work-gitea.base_url=https://git2.example.test
```

The same load-time rules apply to the write: a dotted key names a host that **must already exist** (an unknown host is rejected with the name, not created), and a patch is judged atomically over the list's final state — a bad field, kind or credential source, or a non-boolean `sign_tags`, rejects the whole write and the config file is left untouched. Two practical consequences:

- a single REST write can flip `credential_source` to `vault` together with the `vault_secret` it needs (one write that validates); a single-key CLI/comm write should set `vault_secret` first, then `credential_source` — a `vault` source without a `vault_secret` name is invalid at load, and that is exactly the check that rejects the out-of-order single-key write.
- **creating, renaming and removing a host is a whole-list write** — the `hosts:` block in the config file, or the `git.hosts` key (a JSON array) on `PUT /api/config` — because a dotted key can only point at a name that already exists.

### What the `credential_source` choices mean at use time

The source tells the daemon where the token comes from when a command runs against the host:

- **`host_login` (default, recommended).** The login already configured on the host — `gh auth login` for GitHub, `glab auth login` for GitLab. The process environment passes through unchanged and nothing is added. This is the way you run it on your own machine, and the way the [autonomy handover plan](../plans/2026-11-03-autonomy-handover-plan.md) decided it should stay the recommendation.
- **`vault`** (the fallback for servers and clusters). The token is read from the secrets vault entry named in `vault_secret`, inside the daemon's host command runner, and placed in the command's environment under `GH_TOKEN` (github), `GITLAB_TOKEN` (gitlab) or `DATAWATCH_GIT_TOKEN` (gitea, bitbucket, git) — only in that one child process's environment, never in the daemon's own environment, never on the command line, never in the config, never in a log line, an audit entry or an error message, and never stored outside the vault. Anything the command prints is scrubbed of the exact token value. This applies to every host command: the readiness check and Test connection, the `gh`/`glab` release, issue and change-request commands, the CI-run list, and clone and push. An error names the host and the secret — never a secret value; a missing entry gives an error that names the entry.
- **`host_login` with a `vault_secret`** (optional fallback). The host login runs first with the environment untouched. Only when a command fails and the host's own login is not available does the daemon read the named secret and run that command once more with the token. A command that fails while the login works is never retried with the token. See [Connect datawatch to your git host](connect-git-host.md#the-vault-token-fallback).
- **`broker`.** The credential broker supplies tokens to worker sessions; the daemon adds no host-side token itself. The source is still reported so a later audit log can name it — the invariant the plan sets: *every push or tag audit-logged with the credential source and never the token*.

## What the daemon does on each host kind

The daemon's git work is done by the per-host providers in `internal/git`, each matching the host's own tooling — the same login the readiness check above verifies. What each host family supports, after this change:

| Host `kind` | Open a PR / MR | Create an issue | Create a release | List a ref's CI runs | Clone / push |
| --- | --- | --- | --- | --- | --- |
| `github` | `gh pr create` → returns the PR URL | `gh issue create` → issue URL | `gh release create <tag>` → release URL | `gh run list --repo <owner/name> --branch <ref>` — used by the [repo CI watch](automation-repo-ci-watch.md) | `git` with the `gh` login |
| `gitlab` | `glab mr create` → returns the merge request URL — **new with this change**; it was a not-implemented stub before | `glab issue create` → issue URL — **new** | `glab release create <tag>` → release URL — **new** | `glab api projects/<owner%2Fname>/pipelines?ref=<ref>` — used by the [repo CI watch](automation-repo-ci-watch.md) | `git clone` / `git push`; for HTTPS remotes the repository's local git config sets the credential helper to `glab auth git-credential`, so git uses the `glab` host login directly — no token is read, stored or logged by datawatch. An SSH remote changes nothing and relies on the SSH agent |
| `gitea`, `bitbucket`, `git` | — no forge API: the daemon says so explicitly ("this host has no forge API integration; open the change in the host's web interface") | — same | — same | — same: the CI list is not attempted, the watch records the host as unavailable for listing | plain `git clone` / `git push` against the base URL (or a full remote), using the host's own credentials |

Notes the operator should keep in mind:

- **Every CLI call is bounded** (a 30-second cap on the `gh`/`glab` commands; the CI-run list is under the same 15-second ceiling as the readiness reads) and the commands run with the environment the credential source provides — a `vault`-sourced host's token (or the fallback token of a `host_login` host whose login is not available) appears only in that one run's environment, exactly as in the [credential-source table](#what-the-credential_source-choices-mean-at-use-time). Tag creation (`git tag`) is local and needs no token.
- **Host operations are audited.** Every completed facade call — opening a change request, filing an issue, publishing a release, creating a host tag — writes one entry to the operator audit log under the action `host_git_operation`, naming the host **name** and kind, the operation (`mr`, `issue`, `release` or `tag`), the repository, the ref or title, and the credential source (`host_login`, `vault` or `broker`). A token is never part of the entry, and a failed audit write never fails the host operation itself: the operation stands, the log misses one line. Read it with the [Audit & Access Logging](audit-logging.md) howto (`GET /api/audit?action=host_git_operation`). The CI-run list the repo CI watch takes is a read and writes no audit entry of its own.
- **The tag primitive respects `sign_tags`.** A tag created against a host entry that has `sign_tags: true` is always **signed** (`git tag -s`): the key comes from `signing_key` when set (passed as `-u <key>`), or from git's own configuration when `signing_key` is empty — the daemon runs the sign with no `-u` in that case and performs no refusal of its own; tag creation then fails in git when git has no key configured. `sign_tags` off produces an annotated tag. As of this change no shipping path calls it yet — the merge pipeline's `tag` step and the post-ship `tag` hook still tag locally with plain `git tag` — so `sign_tags`/`signing_key` remain configured-but-unused until the push-and-tag loop lands (see ["What is not here yet"](#what-is-not-here-yet-flagged-for-the-operator)).
- **GitLab token minting is still not implemented** — the broker's mint flow is GitHub-only (`gh auth token`) as of this change; GitLab and the plain-git kinds return a not-implemented error for mint/revoker rather than failing silently.
- **The release-creation and clone/push capabilities are in the git package, not yet in a release loop.** Opening a PR/MR and creating issues is already wired into the daemon (Automaton completion opens the PR via the Automaton's own git provider — a GitLab Automaton now opens a merge request instead of failing — and the project-rules `file_upstream_issue` action creates an issue, which today resolves the `github` provider); the per-project release, tag and merge loop is the Phase 4 follow-up listed under ["What is not here yet"](#what-is-not-here-yet-flagged-for-the-operator). Two live consumers already reach this list: the **repo CI watch** lists a watched repository's runs through the host when its origin belongs to one of your entries ([howto](automation-repo-ci-watch.md) — read-only; the watch records those runs and never acts on them), and the **post-ship hook `parity_issue`** files the companion issue through the host when the merge pipeline's runner is given the project's remote and your host list ([howto](post-ship-hooks.md)).

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

**3b. The same two answers, from the other channels.**

```bash
$ datawatch git-hosts                 # CLI: the daemon's list with readiness (GET /api/git/hosts)
```

MCP hosts see `git_hosts_status` (no arguments — the same data as `GET /api/git/hosts`) and, in step 4's test, `git_hosts_test` with the host `name`. The comm channel lists the hosts with `configure git.hosts list` — each host's name, kind, credential source and vault entry name, never a token — and runs the connection test over its REST passthrough where it has one.

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

The MCP tool `git_hosts_test` (argument `name`) runs the same test from an MCP host, and the comm channel's REST passthrough runs it where a channel has one; an unknown name is a 404 there too.

The test first runs the readiness check; when that reports a problem it returns `ok: false` with the readiness problem and fix command and does not probe. When the host is ready it runs one read-only probe under the same 15 second deadline — `gh api user` for github, `glab api user` for gitlab, `git ls-remote` for the plain kinds — and reports the account it connected as. A 15-second timeout reads "…timed out after 15 seconds."; a missing CLI reads "…the `gh` command is not installed…"; a probe that can't read the account reads "…the login may have expired." with the `gh auth login` / `glab auth login` fix.

## All options (per channel)

With this change every channel carries the git hosts list — the config file and `PUT /api/config` are the write surfaces (whole-host creation stays a whole-list write; field edits go through the dotted key on every write channel), and readiness and the connection test read from REST, MCP, the CLI and the comm channel. The token rule holds on all of them.

| Channel | Read the list | Test one host | Write |
| --- | --- | --- | --- |
| **Config file (write)** | — | — | `git:` → `hosts:` in the config file, commented in [`config-reference.yaml`](../config-reference.yaml); the **whole-list write** — the only way to create, rename or remove a host, alongside the `git.hosts` list key on `PUT /api/config` (REST row) |
| **Web / PWA** | the **Git hosts card** in Settings → General ([howto](connect-git-host.md)): `GET /api/config` with defaults applied plus the live readiness of every host from `GET /api/git/hosts` | the card's **Test connection** button — `POST /api/git/hosts/<name>/test` for the host's **saved** name (a new or renamed host must be saved first) | the card's **Add host** / **Remove** / **Save git hosts** — a whole-list write via `PUT /api/config` (`git.hosts`) after local validation; the dotted `git.hosts.<name>.<field>` keys are the other write keys on `PUT /api/config` |
| **REST API** | `GET /api/git/hosts` lists every host with its live readiness; `GET /api/config` reads the raw block. Both authenticate like the rest of the API and both require the config-read federation capability | `POST /api/git/hosts/<name>/test` runs the connection test; any path under the prefix that is not `<name>/test` is a 404, and an unknown name is a 404 | `PUT /api/config` with `git.hosts` (a JSON array — the whole list) or one or more `git.hosts.<name>.<field>` keys (edits to existing hosts; the patch is applied atomically and validated with the load rules — a single write can set `credential_source: vault` with its `vault_secret`) |
| **MCP** | `get_config` with `section: git` returns the `git` block; **`git_hosts_status`** (no arguments) returns every configured host with kind, credential source and readiness — the same data as `GET /api/git/hosts` | **`git_hosts_test`** with the host `name` — readiness plus one probe; an unknown name is a tool error | no git-hosts set tool — `PUT /api/config` is deliberately not exposed to MCP (config writes stay operator surfaces) |
| **CLI** | `datawatch config get git` (or `git.hosts`) reads the block, defaults applied; **`datawatch git-hosts`** lists every configured host with its readiness, read from the running daemon (the source of truth for the applied config) | no test subcommand — **`datawatch git-hosts`** already prints each host's readiness, and `datawatch config set` of `credential_source`/`vault_secret` prints the readiness for that host after the set; the connection test runs from MCP (`git_hosts_test`), REST (`POST /api/git/hosts/<name>/test`) or the comm channel's REST passthrough | **`datawatch config set git.hosts.<name>.<field> <value>`** — the field write through the daemon (echoes the key, never the value; a rejected patch comes back whole, nothing applied) |
| **Comm channel** | `configure git.hosts list` — every host's name, kind, credential source and vault entry name; never a token | `POST /api/git/hosts/<name>/test` via the REST passthrough | `configure git.hosts.<name>.<field>=<value>` (the reply echoes the key only) |

**The token rule — every read, every channel.** No surface returns a token. The vault credential appears in every response as its secret **name** (`vault_secret`); readiness and test results carry only the account, scope names, a plain-language problem and a fix command; any token-shaped value that reaches an answer (a credential in a base URL, a `github_pat_`…/`glpat-…`/`ghp_`…-style personal access token, a `user:password@` in a URL) is replaced with `[redacted]` before the response is written. The [secrets howto](secrets-manager.md) is how the vault entry itself is created and managed.

## What is not here yet (flagged for the operator)

These are deliberate gaps in *this* change, not bugs — they follow the plan's order (see [docs/plans/2026-11-03-autonomy-handover-plan.md](../plans/2026-11-03-autonomy-handover-plan.md), Phase 4). They are flagged here so the docs state them:

1. **No release-and-merge loop consumes `git.hosts` yet.** The config model, readiness check, the two REST reads and the per-host providers (PR/MR open, issue and release creation, clone/push — see ["What the daemon does on each host kind"](#what-the-daemon-does-on-each-host-kind)) land in this change; the daemon's own per-project push-and-tag loop (release path, direct push vs pull-request, signed tags) is the follow-up in [plans/2026-11-03-autonomy-handover-plan.md](../plans/2026-11-03-autonomy-handover-plan.md), Phase 4. Until it lands, `sign_tags`/`signing_key` are configured but nothing in a shipping path signs with them (the [host tag primitive](#what-the-daemon-does-on-each-host-kind) implements the signing, nothing calls it yet), and release creation is a capability the loop will call rather than one that runs today. What is live on the list: the Automaton/project facade operations (each audit-logged as `host_git_operation`), the repo CI watch's read-only host run listing, and the post-ship `parity_issue` host routing (its issue filing also lands as a `host_git_operation` entry).
2. **No forge API for the plain-git kinds.** For `gitea`, `bitbucket` and `git` hosts the daemon opens the change in *your* browser: pull requests, issues and releases return the explicit "no forge API integration" answer instead of being attempted. Clone and push work; everything that needs the host's web API is manual.
3. **GitLab token minting is unimplemented.** The broker's mint flow is GitHub-only (`gh auth token`); a mint/revoker request for `gitlab` or a plain-git kind returns a not-implemented error rather than a token.
4. **The PWA Git hosts card edits the host fields only.** The [card in Settings → General](connect-git-host.md) covers name, kind, base URL, credential source and vault secret name, readiness, and Test connection, but it has no `sign_tags`/`signing_key` fields: both are preserved from the loaded config when the card saves an existing host, and a host added from the card starts with signing off — set them on the config file or a field write. The card's Test connection runs against the host's **saved** name, so a new or renamed host must be saved before it can be tested. Host writes on the other channels remain the dotted-key field edits and the whole-list `git.hosts` key on `PUT /api/config`; creating or renaming a host is a whole-list write (config file, REST or the card), and MCP has read/test tools but no dedicated set tool.
5. **`GET /api/git/hosts` and `POST …/test` authenticate as config-read.** They require the config-read federation capability; there is no separate git-hosts capability yet. Whether an operator wants the connection test behind its own gate is a review item.
6. **`vault` credentials and the broker are documented here by their contract**, not by a UI: the vault entry is created via the [secrets surface](secrets-manager.md), and the broker supplies tokens to worker sessions on its own. Confirm the `vault_secret` entry name matches what your vault holds before relying on `credential_source: vault`.

## Further reading

- [`config-reference.yaml`](../config-reference.yaml) — the `git.hosts` block and its comments (defaults and required-vs-optional per field).
- [`connect-git-host`](connect-git-host.md) — the PWA Git hosts card in Settings → General: the per-host fields, save and test from the card, and its token rule.
- [`datawatch-definitions.md`](../datawatch-definitions.md) — the **Git hosts** entry (what a git host is, the credential sources, the token rule).
- [`operations.md`](../operations.md) — the **Git hosts** operations section (how to add a host, how to read and test it, how to rotate a vault credential, how to remove a host, and the "not here yet" list).
- [`secrets-manager`](secrets-manager.md) — creating and managing the vault entry a `vault`-sourced host names.
- [`merge-pipeline.md`](merge-pipeline.md) / [`automata-checks-and-ship.md`](automata-checks-and-ship.md) — where the daemon's push-and-tag loop will run once the Phase 4 follow-up lands, and where PR/MR opening and issue creation already run today.
- [`automation-repo-ci-watch.md`](automation-repo-ci-watch.md) — the standing CI watch; watched repositories on a `github`/`gitlab` host of yours have their runs listed through the host (read-only).
- [`post-ship-hooks.md`](post-ship-hooks.md) — the post-ship hooks that can file the companion parity issue through the host.
- [`audit-logging.md`](audit-logging.md) — the `host_git_operation` entries the facade operations write, and how to read them.
- [plans/2026-11-03-autonomy-handover-plan.md](../plans/2026-11-03-autonomy-handover-plan.md) — Phase 4, the plan this host model is building toward.

---
docs:
  index: true
  topics: [git, hosts, credentials, vault, readiness, connection-test, settings]
exec_steps:
  - tool: git_hosts_status
    description: Read every configured git host with its live readiness (login, account, scopes, fix command)
    args: {}
    read_only: true
  - tool: git_hosts_test
    description: Run the connection test for one saved host by its name (replace prod-github with your host's name)
    args: {name: prod-github}
    read_only: true
---

# How to: Connect datawatch to your git host

datawatch works with a git host (GitHub, GitLab, Gitea, Bitbucket or any plain git remote) through that host's own login. The host's own login is the default and recommended source. datawatch holds no token of its own for a git host. The token lives with the host's login, or in a vault entry that you create yourself.

Every Automaton and Automation that pushes, opens a pull or merge request, files an issue or reads CI on a host uses the hosts you set up here.

There are three credential sources. Pick one per host:

| Source | What it means | When to use it |
|---|---|---|
| `host_login` (default, recommended) | The login already on this machine: `gh auth login` for GitHub, `glab auth login` for GitLab, or git's own credential helper for the plain kinds. Nothing is added to the environment. If you also set `vault_secret`, that secret is an optional fallback used only when the host login is not available (see [The vault token fallback](#the-vault-token-fallback)). | Your own workstation, or any machine where you already log in to the host. |
| `vault` | A token you stored in the secrets vault under a name. datawatch reads it only when it runs a host command, and passes it in that one command's environment. | A server or cluster with no interactive login. |
| `broker` | The credential broker supplies tokens to worker sessions. datawatch adds no host-side token. The source is recorded in the audit log. | Worker sessions that get their tokens from the broker. |

Start with `host_login`. Switch to `vault` only when the host cannot be logged in on the machine that runs datawatch.

## Base requirements

- A running datawatch daemon you can reach (the web UI, REST, MCP, the CLI or a comm channel).
- The host's own command-line tool for `github` (`gh`) and `gitlab` (`glab`). `gitea`, `bitbucket` and `git` need only `git`.
- For `vault`: a secrets vault entry that holds the token. See the [secrets manager](secrets-manager.md).

## Setup

### 1. Add the host to the config

Hosts are the `git.hosts` list. Each entry needs a `name` and a `kind`. Self-hosted kinds also need a `base_url`.

```yaml
git:
  hosts:
    - name: prod-github
      kind: github
      # credential_source defaults to host_login

    - name: work-gitea
      kind: gitea
      base_url: https://git.example.test
      credential_source: vault
      vault_secret: gitea-token     # the vault entry NAME, never the token
      sign_tags: false
      signing_key: ""
```

| Key | Required | Rules |
|---|---|---|
| `name` | yes | Unique lowercase slug (`a-z`, `0-9`, single hyphens). It is the key in `git.hosts.<name>.<field>`, so a field write can never rename it — renaming (like creating or removing) is a whole-list write. |
| `kind` | yes | `github`, `gitlab`, `gitea`, `bitbucket` or `git`. |
| `base_url` | by kind | Required for `gitea`, `bitbucket` and `git`. For `gitlab` and `github`, empty means the public host (gitlab.com, github.com); set it for a self-hosted instance. |
| `credential_source` | no | `host_login` (default), `vault` or `broker`. Empty reads as `host_login`. |
| `vault_secret` | when `credential_source: vault` | The vault entry name. Required for `vault`; never the value. |
| `sign_tags` | no | `true` or `false` (default `false`). Tags created for this host are signed when `true`. |
| `signing_key` | no | Key id used when signing. Empty uses git's default signing key. |

The whole list is validated when the config loads. Every problem is reported together, so fix them in one pass. A host that breaks the rules stops the config from loading.

You can also change one field of an existing host without editing the file:

```bash
datawatch config set git.hosts.work-gitea.base_url https://git2.example.test
datawatch config set git.hosts.work-gitea.vault_secret gitea-token
```

A field write names a host that already exists — it can change the host's fields but not its name. Creating, renaming or removing a host is a whole-list write: edit the `hosts:` block, or use the Settings card described below.

### 2. Log in on the host (`host_login`)

Use the login command for the host's kind (see the tables below). Then check it with the readiness read in step 3.

### 3. Store a token (`vault` only)

```bash
datawatch secrets set gitea-token "<token value>"
datawatch config set git.hosts.work-gitea.credential_source vault
```

Set `vault_secret` before `credential_source` if you use single-key writes. A `vault` source with no `vault_secret` is rejected at load.

### 4. Check readiness

```bash
datawatch git-hosts
```

For each host, this shows the readiness row: whether it is logged in, the account, the scopes it found, and, when something is missing, the problem and the exact fix command.

### 5. Test connection

The connection test runs one live, read-only probe against the host. In the web UI it is the **Test connection** button on the host row of the Git hosts card (Settings → General). From the other surfaces:

```bash
curl -s -X POST -H "Authorization: Bearer $TOKEN" https://127.0.0.1:8443/api/git/hosts/prod-github/test
```

```json
{
  "host": "prod-github",
  "ok": true,
  "account": "dmz006",
  "message": "Connected to github.com as dmz006."
}
```

Test by the host's **saved** name. A new or renamed host must be saved before it can be tested.

## Per-host-kind reference

Each table gives the values for one host kind. The base URL is the value you put in `base_url`, or the public default when it is empty. The login command is the one to run on the datawatch machine. The fix command is what datawatch prints when readiness reports a problem. The test command is the probe datawatch runs on Test connection; you can run the same command by hand to see the same answer.

### GitHub (`kind: github`)

| Item | Value |
|---|---|
| Base URL | `https://github.com` (empty `base_url` means github.com). For GitHub Enterprise, set `base_url` to the instance URL. |
| Login command | `gh auth login --hostname github.com` (use your instance's hostname for Enterprise) |
| Scopes to check | `repo` and `workflow` |
| Fix command | `gh auth refresh -s repo,workflow` (for an Enterprise host datawatch prints `gh auth refresh -h <host> -s repo,workflow`) |
| Test command | `gh api user` (datawatch runs this; for an Enterprise host it adds `--hostname <host>`) |

### GitLab (`kind: gitlab`)

| Item | Value |
|---|---|
| Base URL | `https://gitlab.com` (empty `base_url` means gitlab.com). For a self-hosted instance, set `base_url`. |
| Login command | `glab auth login --hostname gitlab.com` (use your instance's hostname when self-hosted) |
| Scopes to check | `api` |
| Fix command | `glab auth login --hostname <host>` |
| Test command | `glab api user --hostname gitlab.com` |

### Gitea (`kind: gitea`)

| Item | Value |
|---|---|
| Base URL | Required, for example `https://git.example.test` |
| Login command | No `gitea` login command. datawatch uses git's own credentials for the base URL. Run `git ls-remote --heads -- https://git.example.test` once; if the host asks for a username and token, git's credential helper keeps them for later runs. |
| Scopes to check | datawatch checks no scopes for this kind. Create the token in Gitea with read access to the repositories you use, and push access where you push. |
| Fix command | datawatch prints no fix command. Its problem line reads "Could not reach <base URL>." Check the base URL and the git login, then run the test again. |
| Test command | `git ls-remote --heads -- https://git.example.test` |

### Bitbucket (`kind: bitbucket`)

| Item | Value |
|---|---|
| Base URL | Required, for example `https://bitbucket.org` |
| Login command | No `bitbucket` login command. Use git's credential helper, as for Gitea. |
| Scopes to check | datawatch checks no scopes for this kind. Give the token repository read access, and write access where you push. |
| Fix command | datawatch prints no fix command. Its problem line reads "Could not reach <base URL>." |
| Test command | `git ls-remote --heads -- https://bitbucket.org` (use your own base URL) |

### Plain git (`kind: git`)

| Item | Value |
|---|---|
| Base URL | Required: any git remote URL the machine can reach |
| Login command | None. Use git's own credential helper or an SSH key for the remote. |
| Scopes to check | Not applicable. datawatch checks reachability only. |
| Fix command | None. Its problem line reads "Could not reach <base URL>." |
| Test command | `git ls-remote --heads -- <base_url>` |

Readiness and the test both give up after 15 seconds. A timeout reads "...timed out after 15 seconds."

## What the readiness check does

`datawatch git-hosts`, `GET /api/git/hosts`, the MCP tool `git_hosts_status` and the Git hosts card all run the same check for each host:

- **github:** `gh auth status` on the host name. It reports `logged_in`, the `account`, the token `scopes`, and whether `repo` and `workflow` are present. A missing scope or a missing login is a problem with its fix command. A missing `gh` binary is its own problem.
- **gitlab:** `glab auth status` on the host name. It reports `logged_in`, the `account`, the scopes, and whether `api` is present.
- **gitea, bitbucket, git:** `git ls-remote --heads <base_url>`. It reports whether the base URL answered. No scope is read.

Readiness never returns a token. It returns only the account name, the scope names, a problem in plain language and a fix command. Any token-shaped text that reaches an answer is replaced with `[redacted]`.

Readiness and Test connection use the same credential source as every other host command. A `vault` host is checked with its vault token in that one command's environment, so it shows ready only when the token is present and accepted. When the vault entry is missing, the problem names the entry (for example `cannot resolve vault secret "gh-push"`), never a value. A `host_login` host with a fallback `vault_secret` is checked with the machine's login first, and with the vault token only when that login is not available.

## The Test connection action

Test connection runs in three steps:

1. It runs the readiness check. If that reports a problem, it stops there, returns `ok: false` with the problem and the fix command, and does not probe.
2. If the host is ready, it runs the one probe for its kind from the table above.
3. It returns `ok`, the `account` it connected as, and a plain message. A failure names the cause: a missing CLI, a timeout, or an expired login (with the login command as the fix).

Test connection by the saved name. From MCP, call `git_hosts_test` with `name`. An unknown name is a 404 on REST and a tool error on MCP.

## The vault token environment variables

When a host uses `credential_source: vault`, the daemon puts the token into the environment of the one host command it runs. This covers the readiness check and Test connection, the release, issue and pull or merge request commands (`gh` and `glab`), the CI watch, and git clone and push. It uses one variable per kind:

| Host kind | Variable |
|---|---|
| `github` | `GH_TOKEN` |
| `gitlab` | `GITLAB_TOKEN` |
| `gitea`, `bitbucket`, `git` | `DATAWATCH_GIT_TOKEN` |

The variable is set only in that command's environment. It is never written to the daemon's own environment, the config, an error message or a response. An error names the host and the vault entry, never the value. A `broker` host gets no variable from datawatch, and a `host_login` host gets none either, except through the fallback below.

The daemon reads the secret by name from the secrets vault inside the command runner, at the moment it starts the command, and passes it only to that child process. The token is never put on the command line, never logged, never stored outside the vault, and never included in an error message, an audit line or a response. Anything a command prints is scrubbed of the exact token value before it is returned.

### The vault token fallback

A `host_login` host can also name a `vault_secret`. The host login then stays the default, and the secret is read only when both of these are true:

1. A host command failed, and
2. The host's own login is not available (`gh auth status` or `glab auth status` fails; for the plain kinds there is no login of their own, so a failed command counts).

The command is then run once more with the token in its environment. A command that fails while the login works is not retried, and a missing CLI, a timeout or a cancellation never reads the vault. A `broker` host never falls back.

If the fallback is needed and the secret is missing, the readiness problem and the error say so and name the entry, for example `the host login is not available and the fallback vault secret "gh-push" could not be used`. Check it with `datawatch secrets list` and create it with `datawatch secrets set <name> <value>`.

**One limit to know about.** The audit entry for an operation records the credential source from the config (`host_login` for a fallback host), not whether the fallback token was used for that one command.

## The Git hosts card in Settings

Settings → General → **Git hosts** is the in-browser editor for the same `git.hosts` list.

- Each row shows the readiness (Ready, Not logged in or Status unknown), the account, the problem and the fix command.
- Editable fields: **Name**, **Kind**, **Base URL** (hidden for GitHub), **Credential source**, and **Vault secret name** (shown only for `vault`; the entry name, never the value).
- **Add host** and **Remove** change the list. **Save git hosts** validates the whole list and writes it in one `PUT /api/config` (`git.hosts`). A failure names the first problem.
- **Test connection** runs the test for the host by its saved name.
- The card does not edit `sign_tags` or `signing_key`. It keeps the values you set in the config.
- If the config fails to load, the editor stays off, so a save cannot overwrite the list with one you did not see.

## All options (per channel)

| Channel | Readiness and list | Test one host | Write |
|---|---|---|---|
| **Config file** | `git:` → `hosts:` in the config file | — | Edit `hosts:`. This is the only way to create, rename or remove a host by hand. |
| **Web / PWA** | Settings → General → Git hosts | **Test connection** button | Add / Remove / Save git hosts (whole list) |
| **REST** | `GET /api/git/hosts` | `POST /api/git/hosts/<name>/test` | `PUT /api/config` with `git.hosts` (whole list) or `git.hosts.<name>.<field>` keys |
| **MCP** | `git_hosts_status` (no arguments) | `git_hosts_test` with `name` | No git-hosts write tool. Config writes stay on operator surfaces. |
| **CLI** | `datawatch git-hosts` | Readiness is printed by `datawatch git-hosts`; the probe runs from REST or MCP | `datawatch config set git.hosts.<name>.<field> <value>` |
| **Comm channel** | `configure git.hosts list` (name, kind, credential source and vault entry name; never a token) | Via the REST passthrough | `configure git.hosts.<name>.<field>=<value>` |

Every channel is subject to the same rule: no response ever contains a token.

## Troubleshooting

| You see | Do this |
|---|---|
| "Not logged in to github.com with gh." | Run `gh auth login --hostname github.com`, then check again. |
| "The gh login is missing the repo and workflow scope." | Run `gh auth refresh -s repo,workflow`. |
| "The glab login is missing the api scope." | Run `glab auth login --hostname <host>` and grant the `api` scope. |
| "The gh command is not installed..." | Install the GitHub CLI, then log in. |
| "Could not reach <base URL>." | Check the URL and the network path, and run `git ls-remote --heads -- <base URL>` by hand. |
| "No base URL is set for this host." | Set `base_url` for a `gitea`, `bitbucket` or `git` host. |
| "credential_source vault requires vault_secret" at load | Set `vault_secret` to the vault entry name, or set `credential_source` back to `host_login`. |
| "cannot resolve vault secret ..." or "vault secret ... is empty" | The vault entry named in `vault_secret` is missing or has no value. Create it with `datawatch secrets set <name> <value>`; the message names the entry, never a value. |
| "the host login is not available and the fallback vault secret ... could not be used" | The host login is missing and the fallback entry is missing too. Log in to the host (`gh auth login`, `glab auth login`) or create the vault entry. |
| "...no secret resolver..." | The secrets store did not start, so the vault cannot be read. Check the daemon start-up log for a secrets store warning. |

## Related

- [Git hosts](git-hosts.md): the full model, per-host operations and what is not here yet.
- [Secrets manager](secrets-manager.md): creating the vault entry a `vault` host names.
- [Audit and access logging](audit-logging.md): where host operations are recorded (`host_git_operation`).
- [Automation repo CI watch](automation-repo-ci-watch.md): reads a watched repository's CI through a host of yours.

---
docs:
  index: true
  topics: [git, hosts, credentials, vault, readiness, connection-test, settings]
exec_steps:
  - tool: get_config
    description: Read the git.hosts list shown in Settings → General → Git hosts
    args: {section: git}
    read_only: true
---

# How to: Connect a git host from Settings

Settings → General → **Git hosts** lists every host datawatch may push to. Each host is one editable group. The card reads the list from `GET /api/config` and each host's login state from `GET /api/git/hosts`, and saves with `PUT /api/config` (key `git.hosts`).

For the full model (kinds, credential sources, signing and what the daemon does on each kind), see [How to: Configure your git hosts](git-hosts.md).

## Fields

| Field | When shown | What it does |
|---|---|---|
| Name | always | Unique identifier for the host. Used in the connection-test path. |
| Kind | always | `github`, `gitlab`, `gitea`, `bitbucket` or `git`. |
| Base URL | every kind except `github` | Required for `gitea`, `bitbucket` and `git`. Optional for `gitlab` (empty = gitlab.com). Only its host part is used for login checks. |
| Credential source | always | `host_login` (default: the `gh` / `glab` login already on this machine), `vault` (a secrets-vault entry), or `broker`. |
| Vault secret name | only when the source is `vault` | The vault entry **name**. The token value is never stored in the config and never shown here. |

## Steps

1. Open **Settings → General → Git hosts**.
2. Click **Add host**, then fill in name, kind, and base URL or credential source as needed.
3. Click **Save git hosts**. Validation runs first, and the card names the first problem it finds.
4. Click **Test connection** on the host. A ready host reports its account; a missing login reports the fix command to run.

Save before testing a new or renamed host, because the test reads the saved list.

## Help

The **?** next to each host opens this page.

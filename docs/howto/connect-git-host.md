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

The **Git hosts** card in **Settings → General** of the PWA is the in-UI editor for `git.hosts` — the daemon's named list of git hosts it may push to and tag on. It reads the list from `GET /api/config` and each host's live readiness from `GET /api/git/hosts`, and saves by writing the whole list back with `PUT /api/config` (`git.hosts`). It is the same data and the same validation as every other write surface — see [git-hosts.md](git-hosts.md) for the full model, the credential sources, the per-host operations and the token rule.

**What the card shows per host.** A row of readiness (`Ready` / `Not logged in` / `Status unknown`, the account, and the repo-scope status), the plain-language problem and the exact fix command when the daemon found one, then the editable fields:

| Field | Notes |
|---|---|
| **Name** | required and unique; the key in `git.hosts.<name>.<field>` and in `/api/git/hosts/<name>/test` |
| **Kind** | `github`, `gitlab`, `gitea`, `bitbucket`, or plain `git` |
| **Base URL** | hidden for `github` (always github.com); required for `gitea`, `bitbucket` and `git`; for `gitlab` only when self-hosted |
| **Credential source** | `host_login` (default — the `gh`/`glab` login on this machine), `vault`, or `broker` |
| **Vault secret name** | shown only for `vault`; the secrets-vault entry **name** holding the token — never the value |

The card carries no `sign_tags`/`signing_key` fields: both are preserved from the loaded config when you save an existing host, and a host you add starts with signing off. Set them on the config file or a field write — see [git-hosts.md](git-hosts.md).

**Saving and testing.** **Add host** appends a new row; **Remove** drops one; **Save git hosts** validates the whole list locally first (a failure names the host number and the reason — e.g. missing name, duplicate name, missing base URL for that kind, missing vault secret name) and, when valid, writes the **entire list** in one `PUT /api/config` — so a save from the card is a whole-list write, exactly like the YAML `hosts:` block. **Test connection** runs `POST /api/git/hosts/<name>/test` for one host by its **saved** name — a new or renamed host must be saved before it can be tested (the card's intro line says so). A test reports `Connection OK` / `Connection failed` with the daemon's plain-language message, the same answer every other channel gets.

**The card is safe by construction.** If the config never loads, the editor stays off, so a save can never overwrite `git.hosts` with a list you never saw. No token or secret value is ever read into the card — a vault host shows its secret **name** only — and any problem text that reaches a row is redacted of token-shaped values and `user:password@` credentials before it is shown.

## Steps

1. Open **Settings → General → Git hosts** in the PWA.
2. **Add host**; fill in the name, kind, base URL (when the field is shown), credential source and, for `vault`, the vault entry name.
3. **Save git hosts**. The save either succeeds (a "Git hosts saved" toast) or names the first problem.
4. **Test connection** on the host. If it reports a problem, follow its fix command (for example `gh auth login` or `gh auth refresh -s repo,workflow`) and test again.
5. For a `vault` source with no entry yet, create the entry in the [secrets manager](secrets-manager.md) first, then point the host at its name.

## Help

The **?** button on each host row opens this howto in the built-in docs viewer. The full reference — credential sources, readiness rules per kind, per-host operations, every write and read surface, and what is deliberately not here yet — is [git-hosts.md](git-hosts.md).

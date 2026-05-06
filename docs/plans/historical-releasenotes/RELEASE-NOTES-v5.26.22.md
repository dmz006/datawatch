# datawatch v5.26.22 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.21 → v5.26.22
**Patch release** (no binaries — operator directive).
**Closed:** Git credentials abstracted for k8s + SSH-key Secret support in the Helm chart.

## What's new

### Git credentials in k8s — three patterns, one chart

Operator-asked: *"Can the credentials for the recent changes be abstracted and documented to work in k8s?"*

The v5.26.21 daemon-side clone of `project_profile`-based PRDs needed git auth at clone time. Locally that's whatever the daemon user has (SSH agent / credential helper / token-in-URL). In k8s, none of those naturally exist. v5.26.22 abstracts the auth into three operator-pickable patterns, all wired through the existing Helm chart:

| Pattern | Setup | When to pick it |
|---------|-------|-----------------|
| **HTTPS + PAT in Secret** | `gitToken.existingSecret=datawatch-git-token`. Chart projects the token as `DATAWATCH_GIT_TOKEN` env. Daemon auto-rewrites `https://...` URLs to `https://x-access-token:<token>@...` at clone time. Token auto-redacted from error output. | GitHub / GitLab / cloud providers with PAT-based auth. Simplest. |
| **SSH key in Secret** *(new in v5.26.22)* | `ssh.existingSecret=datawatch-ssh`. Chart mounts the Secret's `id_ed25519` + `known_hosts` keys at `/root/.ssh/` inside the daemon Pod (`defaultMode: 0400`). Daemon shells out to `git clone` against `git@host:...` URLs; SSH client picks up the mounted key. | SSH URLs / providers without PAT support / deploy keys for repo isolation. |
| **F10 BL113 token broker** *(future v5.26.23+)* | Daemon mints short-lived per-spawn tokens via the parent's `TokenBroker`. No long-lived secret in the Pod. | Multi-tenant deployments where each spawn should authorize independently. |

### Implementation

- `internal/server/git_auth.go`:
  - `injectGitToken(rawURL, token)` rewrites HTTPS URLs to embed the token. SSH URLs and HTTPS URLs that already carry userinfo are returned unchanged (operator's pre-existing token-in-URL profiles are not overwritten).
  - `redactGitToken(blob, originalURL)` masks any `<user>:<secret>@` occurrence — both the token we injected AND any operator-embedded token in the original URL — before the daemon surfaces a clone error to the caller. Naive but enough for git's stderr blast radius.
- The clone callsite in `handleStartSession` reads `os.Getenv("DATAWATCH_GIT_TOKEN")`; when set, calls `injectGitToken` before `git clone`. When unset, the URL goes through unchanged (local-machine git config / SSH agent take over).
- 9 new unit tests in `git_auth_test.go` cover: HTTPS public, HTTPS-with-existing-auth, SSH passthrough, empty-token, bad-URL, GitLab-style, redact-injected, redact-embedded, redact-no-token-blob.

### Helm chart

- New `ssh.existingSecret` value. Pairs with `gitToken.existingSecret` — operators can wire both, neither, or just one.
- Mount: `/root/.ssh/` (read-only, mode 0400). Secret keys must be `id_ed25519` and `known_hosts` (ed25519 is the recommended default; RSA works if you rename the key).
- Chart `version` 0.22.0 → **0.23.0** (chart-side change). `appVersion` 5.26.5 → **5.26.22** (operator can `helm install` without `--set image.tag` and pull the current daemon).

### Setup-howto

`docs/howto/setup-and-install.md` Option E (Helm install) now has:
- A tabular comparison of the three patterns (HTTPS+PAT / SSH key / future broker).
- Rationale on which to pick when (simplest first, repo-isolation when SSH-only).
- The redaction guarantee on clone-error logs.

The existing `# 3. (optional) git token` block is annotated to call out v5.26.22's new dual-purpose use (BL113 broker + daemon-side clone auto-inject).

## Configuration parity

- `gitToken.existingSecret` already in chart since BL113.
- `ssh.existingSecret` is **new** in chart.
- Daemon-side: pure env-var read (`DATAWATCH_GIT_TOKEN`); SSH path is filesystem-only via the chart mount.

## Tests

- 1404 + 9 (git_auth) = 1413 Go unit tests passing.
- Functional smoke unaffected (daemon still in scope; clone path is exercised by the new tests; full-cluster end-to-end clone test is a future kind-cluster smoke).

## Known follow-ups

- **F10 BL113 token-broker integration** (v5.26.23) — replace the long-lived `DATAWATCH_GIT_TOKEN` Pod secret with per-spawn short-lived tokens minted by the parent.
- **Per-session workspace reaper** — clone targets persist after session ends (matches F10 cluster-spawn semantics).
- v6.0 packaging items unchanged.

## Upgrade path

```bash
git pull
datawatch restart

# k8s upgrade — pick whichever auth pattern fits:

# (a) HTTPS+PAT — already wired since BL113.
# (b) SSH key:
kubectl -n datawatch create secret generic datawatch-ssh \
  --from-file=id_ed25519=$HOME/.ssh/id_ed25519 \
  --from-file=known_hosts=$HOME/.ssh/known_hosts
helm upgrade dw ./charts/datawatch \
  --namespace datawatch --reuse-values \
  --set ssh.existingSecret=datawatch-ssh

# Then create a profile with an SSH URL — the daemon's clone uses the
# mounted key automatically, no further config:
curl -X POST -H "Content-Type: application/json" \
  -d '{"name":"my-private-repo","git":{"url":"git@github.com:org/private","branch":"main"},"image_pair":{"agent":"agent-claude"}}' \
  https://localhost:8443/api/profiles/projects
```

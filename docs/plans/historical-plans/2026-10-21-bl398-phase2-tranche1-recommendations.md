# BL398 Phase 2 tranche 1 recommendations

Removals suggested by reachability traces. The operator decides; no suppression or package has been removed.

## Summary of the 12 traced ids

Each id below states whether it is reachable, whether removal is recommended, and that the operator decides. No suppression has been removed; `.trivyignore` and `docs/security-review.md` change only through `scripts/gen_trivyignore.py`.

| Id | Component | Reachable | Removal | Decision |
|---|---|---|---|---|
| CVE-2025-7458 | libsqlite3-0 | no | Removal recommended (drop `gnupg` from agent-base) | Operator decides |
| CVE-2026-11822 | libsqlite3-0 | no | Removal recommended (drop `gnupg` from agent-base) | Operator decides |
| CVE-2026-11824 | libsqlite3-0 | no | Removal recommended (drop `gnupg` from agent-base) | Operator decides |
| CVE-2023-45853 | zlib1g | no | Removal not recommended | Operator decides |
| CVE-2026-6276 | libcurl4 / curl | no | Removal not recommended | Operator decides |
| CVE-2023-2953 | libldap-2.5-0 | no | Removal not recommended | Operator decides |
| CVE-2025-69720 | ncurses family | no | Removal not recommended (vulnerable binary already absent) | Operator decides |
| CVE-2026-9538 | perl family | no | Removal not possible | Operator decides |
| CVE-2026-53615 | util-linux family | no | Removal not recommended | Operator decides |
| CVE-2026-55453 | libcups2 | no | Removal not possible | Operator decides |
| CVE-2025-26625 | git-lfs | yes | Removal optional, only if LFS remotes are never used | Operator decides |
| CVE-2026-7210 | python3.11 | no (agent-gemini); yes (agent-aider) | Removal not possible | Operator decides |

## Per-id notes

## cve/CVE-2025-7458, cve/CVE-2026-11822, cve/CVE-2026-11824 (libsqlite3-0)

- Image: agent-base (inherited by agent-gemini, agent-aider, parent-full).
- Recommendation: drop `gnupg` from the agent-base `install_packages` list in docker/dockerfiles/Dockerfile.agent-base. It is the only package in agent-base that depends on libsqlite3-0 (`/usr/bin/gpg` is the only binary linking it), so removing it removes the library from agent-base and clears all three findings there. agent-gemini and lang-node already install `gnupg` themselves, so those images would keep the library until that is reworked.
- Evidence: `docker run --network none` on a built agent-base image showed gpg as the sole dpkg dependent and sole ldd user; no datawatch Go code or script invokes gpg. Check that git commit/tag signing is not needed by agent sessions before removing.

## cve/CVE-2026-53615 (util-linux family)

- Image: agent-base. No removal recommended: util-linux, mount, libblkid1, libmount1, bsdutils are essential Debian packages, and the trace found the vulnerable libblkid path unreachable (no device probing, uid 10001, no block devices). Keep the suppression until Debian ships a bookworm fix.

## cve/CVE-2026-9538 (perl family)

- Image: agent-base. No removal possible: `git` Depends on `perl` and `liberror-perl`, and debconf/adduser also need perl. Only the optional `ptar*`/`instmodsh` utilities use Archive::Tar and nothing runs them; they cannot be removed separately from the perl package.

## cve/CVE-2025-69720 (ncurses family)

- Image: agent-base. No removal needed: the vulnerable `infocmp` binary (ncurses-bin) is already not installed; only the libraries libtinfo6/libncursesw6 remain, required by tmux, bash and less.

## cve/CVE-2026-55453 (libcups2)

- Image: parent-full. No removal possible: libcups2 is a hard Depends of `openjdk-17-jre-headless` (Dockerfile.parent-full), which signal-cli needs. Trace found no cupsd, cups-browsed, lp/lpr call or ipp:// use, so the vulnerable scheduler/backend path is unreachable. Keep the suppression until Debian ships a bookworm fix.

## cve/CVE-2025-26625 (git-lfs)

- Images: agent-base, agent-gemini, agent-aider, parent-full. No removal recommended: the trace found it reachable, because /etc/gitconfig wires the lfs smudge/clean filter (required=true) and internal/agents/worker_clone.go clones operator-configured remotes. If the operator never uses LFS-enabled remotes, dropping `git-lfs` from the agent-base `install_packages` list would clear it in all four images; that is an operator decision.

## cve/CVE-2026-7210 (python3.11)

- Image: agent-gemini. python3.11 is present only as a dependency of nodejs/rsync (apt-cache rdepends) and no Python application runs there, so it is dormant; it cannot be removed while nodejs depends on it. agent-aider: reachable through litellm/botocore ElementTree use, no removal.

## cve/CVE-2023-45853, cve/CVE-2026-6276, cve/CVE-2023-2953 (zlib1g, libcurl4, libldap-2.5-0)

### CVE-2023-45853

- Reachable: no. Removal recommended: no. The operator decides; no suppression is removed.
- zlib1g is an essential dependency of git, curl, libcurl4, openssh-client and tmux. The flaw is in MiniZip's zip-writing path (zipOpenNewFileInZip4_64); no image installs libminizip and datawatch uses only Go's pure-Go compress/gzip, so nothing calls it.

### CVE-2026-6276

- Reachable: no. Removal recommended: no. The operator decides; no suppression is removed.
- Needs curl to hold cookies and follow a redirect (-L). Runtime curl callers (hookinstaller, memory hooks, openwebui backend) use neither; build-time Dockerfile curl calls carry no cookies. curl cannot be removed.

### CVE-2023-2953

- Reachable: no. Removal recommended: no. The operator decides; no suppression is removed.
- libldap-2.5-0 enters the images as a dependency of libcurl4. No LDAP use, URL, slapd or ldap-utils exists; every curl call targets http(s). Reaching it would need curl to be handed a hostile ldap:// URL. It cannot be removed without removing curl and git.


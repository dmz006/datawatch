# BL398 Phase 2 tranche 1 recommendations

Removals suggested by reachability traces. The operator decides; no suppression or package has been removed.

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

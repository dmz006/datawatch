# BL398 Phase 2 tranche 1 recommendations

Removals suggested by reachability traces. The operator decides; no suppression or package has been removed.

## cve/CVE-2025-7458, cve/CVE-2026-11822, cve/CVE-2026-11824 (libsqlite3-0)

- Image: agent-base (inherited by agent-gemini, agent-aider, parent-full).
- Recommendation: drop `gnupg` from the agent-base `install_packages` list in docker/dockerfiles/Dockerfile.agent-base. It is the only package in agent-base that depends on libsqlite3-0 (`/usr/bin/gpg` is the only binary linking it), so removing it removes the library from agent-base and clears all three findings there. agent-gemini and lang-node already install `gnupg` themselves, so those images would keep the library until that is reworked.
- Evidence: `docker run --network none` on a built agent-base image showed gpg as the sole dpkg dependent and sole ldd user; no datawatch Go code or script invokes gpg. Check that git commit/tag signing is not needed by agent sessions before removing.

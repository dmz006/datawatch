# ci-watch completion guardrail — live verification

**Date:** 2026-10-13
**Version:** v9.0.62
**Scope:** docs/howto/ci-watch-guardrail.md, docs/mcp.md, docs/api-mcp-mapping.md, plus the live verification of the `ci-watch` completion guardrail (sections added by other tasks).

## Template fit

**Conclusion: templates cannot enable ci-watch today without a new mechanism, so none was built; the howto documents pairing an instantiated Automaton with a guardrail profile (or an explicit completion list) instead.** The reason: `Template` (internal/autonomous/models.go) carries only `title`/`description`/`spec`/`type`/`tags`/`vars`. `TemplateStore.Instantiate` + `Manager.InstantiateFromTemplateStore` (`autonomous_template_instantiate`) return only a substituted spec and call `CreatePRD(spec, projectDir, backend, "", effort)`, with no profile or guardrail argument. The PRD-as-template path (`Manager.InstantiateTemplate`, `autonomous_prd_instantiate`) copies spec, title, project dir, backend/model/effort and stories, but not `GuardrailProfile` or `PerPRDGuardrails`. There are no built-in release templates in internal/autonomous. The documented recipe ("Using with templates" in the howto) is therefore two calls: instantiate, then `per_automaton_guardrails_set` with `guardrail_profile` (or `per_prd_guardrails_json`) before the Automaton completes. Or list `ci-watch` in the global `autonomous.per_prd_guardrails`.

A caveat found while checking this, which also affects the profile recipe: `resolveGuardrails` (internal/autonomous/guardrail_registry.go) returns a profile's single `Guardrails` list at **every** level (task, story, prd) when the Automaton has no explicit list for that level. A profile containing `ci-watch`/`rules-review` therefore also sends each task and story to the guardrail judge under those names (one extra LLM review per unit; task/story loops in executor.go don't skip judgment-type completion-only names). `rules-review` in a profile already behaves this way. The howto states it and offers the explicit `per_prd_guardrails` list as the completion-only alternative. It is not changed here and is left for an operator decision.

## Release

**Version:** v9.0.63 · **Commit:** `33864705` (tag `v9.0.63`, pushed 2026-10-14T01:28Z) · **Deployed:** local daemon restarted via `systemctl --user restart datawatch`; running daemon reports 9.0.63.

Release published 2026-10-14T01:40Z with 15 assets (goreleaser binaries, stats tarballs, checksums).

| Workflow | Result | Run |
|---|---|---|
| CI | success | https://github.com/dmz006/datawatch/actions/runs/38495885477 |
| security-scan | success | https://github.com/dmz006/datawatch/actions/runs/38495885478 |
| CodeQL | success | https://github.com/dmz006/datawatch/actions/runs/38495884946 |
| Matrix Integration | success | https://github.com/dmz006/datawatch/actions/runs/38495885474 |
| secret-scan | success | https://github.com/dmz006/datawatch/actions/runs/38495885462 |
| docs-sync | success | https://github.com/dmz006/datawatch/actions/runs/38495885400 |
| Release CI Summary (×4) | success | https://github.com/dmz006/datawatch/actions/runs/38496433905 (latest) |
| Release (tag v9.0.63) | **failure** (2 jobs) | https://github.com/dmz006/datawatch/actions/runs/38495887540 |

Release jobs: go test + lint, gosec baseline-diff, govulncheck, goreleaser, containers/validator, containers/stats-cluster all succeeded. Two failed. Neither is caused by this release's code, and both are **escalated to the operator, not fixed here**:

1. **containers / agent-base: blocking Trivy SARIF scan.** Seven HIGH findings in the agent-base image:
   - openssh-client 1:9.2p1-2+deb12u10: CVE-2026-106553, CVE-2026-106555, CVE-2026-106583 (Debian status `fix_deferred`, no fixed version).
   - golang.org/x/net v0.58.0: CVE-2026-78669 (fixed in 0.60.0).
   - Go stdlib v1.27.1: CVE-2026-78667, CVE-2026-78669, CVE-2026-97031 (fixed in 1.27.2).

   This is the same container-vulnerability class that prompted the ci-watch guardrail. Fixing it is explicitly out of scope for this release. The v9.0.62 Release run (https://github.com/dmz006/datawatch/actions/runs/38484929685) failed on the same agent-base job, so the failure predates this release.
2. **attach stats-cluster tarball: upload step exhausted its retries.** Every attempt from 01:46Z to 02:10Z got `release not found` with API quota at 5000 (not rate-limited). The release itself had been published at 01:40Z, which rules out the step's "read-path lag" assumption. The step's lookup of the release looks broken, not slow. The four `datawatch-stats_9.0.63_*` goreleaser tarballs are attached. Only the `datawatch-stats-cluster-9.0.63-linux-amd64.tar.gz` container-image tarball is missing from the release. In v9.0.62 the same job succeeded within 30s.

## Live probe

- 2026-10-13: ci-watch live probe commit pushed by an automaton.

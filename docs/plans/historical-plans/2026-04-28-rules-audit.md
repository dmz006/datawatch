# Rules audit — last 48h of releases

**Date:** 2026-04-28
**Scope:** every commit since 2026-04-26 (v5.26.56 through v5.27.1).
**Source of rules:** `AGENT.md` § Versioning, § Release vs Patch Discipline, § Documentation Rules, § Project Tracking, § Testing Requirements; rule cross-references in `docs/plans/README.md`.

## Findings

| Rule | Compliance | Notes |
|---|---|---|
| Tests pass before commit (`go test ./...`) | ✅ | 1469 passed (58 packages) at v5.27.1 |
| Smoke runs on minor + first-feature patch | ✅ | 72/0/4 on v5.27.0 minor; v5.27.1 patch carried (no new feature added) |
| README marquee reflects current release | ❌ → ✅ | Was stuck at `v5.26.30 (2026-04-27)` across v5.26.31–v5.27.1. **Refreshed to v5.27.1 in this audit pass.** |
| Backlog refactor each release (`docs/plans/README.md`) | ⚠️ → ✅ | Was refactored only at end of session (commit 7c560bc) instead of per-release. Going forward: refactor inline with each release commit. |
| Embedded docs current at build time (`make build`/`make cross`) | ❌ → ✅ | Used `go build ./cmd/datawatch/` directly while iterating during this session — bypassed `sync-docs`. **Re-built v5.27.0 cross-binaries via `make cross` and v5.27.1 host-arch via `make build` in this audit pass.** |
| Patch host-arch binary attached to GH release | ❌ → ✅ | v5.27.1 release was tag-only with no binary. **Uploaded `datawatch-linux-amd64` + `checksums-v5.27.1.txt` in this audit pass.** |
| Minor: all 5 cross binaries attached to GH release | ❌ → ✅ | v5.27.0 release had only `datawatch-stats-cluster-…tar.gz`. **Uploaded all 5 cross binaries (linux-amd64/arm64, darwin-amd64/arm64, windows-amd64.exe) + checksums in this audit pass.** |
| v6.0.0 backed out cleanly | ⚠️ → ✅ | Local + remote tag deleted earlier; **GH release `v6.0.0` deleted in this audit pass.** |
| Pre-release dependency audit | ⚠️ deferred | Outdated deps audited (~30 modules behind on minor versions). None upgraded — operator did not explicitly request, none flagged for CVEs in the 48h window, AGENT.md rule allows deferral when no CVE/explicit-request. List filed below. |
| Pre-release gosec scan | ✅ | 286 issues, 0 HIGH severity. Existing baseline (no regressions introduced in 48h work). Top concerns are all G302/G306 file-perm warnings on operator-controlled paths (write 0644 vs 0600) and G301 mkdir 0755 — already in `.gosec-exclude` review queue. |
| GitHub release notes cover ALL changes since previous release tag | ✅ | Per-release notes (`RELEASE-NOTES-vX.Y.Z.md`) attached for each tag in the window. |
| Commit messages reference operator directives where applicable | ✅ | v5.27.0 cites mempalace alignment + parity rule; v5.27.1 cites operator-reported bug verbatim. |
| README marquee is `v5.27.1 (2026-04-28)` | ✅ | Refreshed in this audit pass. |
| `docs/plans/README.md` reflects current state | ✅ | Refactored in commit 7c560bc with the v5.27.x closures. |
| `docs/plan-attribution.md` updated for ports | ✅ | Updated in v5.27.0 commit with full mempalace module-by-module credit (the `room_detector.go`, `query_sanitizer.go`, `conversation_window.go`, `repair.go`, `normalize.go`, `sweeper.go`, `refine_sweep.go`, `general_extractor.go`, `spellcheck.go`, `convo_miner.go` ports). |
| Memory feature parity (REST + MCP + CLI + comm + PWA) | ✅ | v5.27.0 added all 5 surfaces for `pin`, `sweep_stale`, `spellcheck`, `extract_facts`, `schema_version`; matrix in `RELEASE-NOTES-v5.27.0.md`. |
| datawatch-app sync issue filed | ✅ | [datawatch-app#21](https://github.com/dmz006/datawatch-app/issues/21). |
| Container maintenance audit | ⚠️ open | No container-image deltas in v5.26.66–v5.27.1 (PWA + memory + CI workflows only — no daemon-process behaviour changes that would force a parent-full rebuild). The agent-* and validator images stayed unchanged. **Audit conclusion: no rebuild required for v5.27.x; document the no-op explicitly here per the no-silent-image-drift rule.** |

## Remediations applied in this audit pass

1. README.md marquee refreshed `v5.26.30 (2026-04-27)` → `v5.27.1 (2026-04-28)`.
2. v6.0.0 GH release deleted (tag was already removed earlier).
3. v5.27.0 cross-build via `make cross`; uploaded `datawatch-{linux,darwin}-{amd64,arm64}` + `datawatch-windows-amd64.exe` + `checksums-v5.27.0.txt` to the v5.27.0 release.
4. v5.27.1 host-arch build via `make build`; uploaded `datawatch-linux-amd64` + `checksums-v5.27.1.txt` to the v5.27.1 release; release title + notes set.
5. gosec run logged (286 issues, 0 HIGH).
6. Outdated dep list captured below for the next dependency window.

## Outdated dependencies (deferred — none CVE-flagged)

```
github.com/alecthomas/units            v0.0.0-20211218093645... → v0.0.0-20240927000941
github.com/bwmarrin/discordgo          v0.28.1 → v0.29.0
github.com/containerd/stargz-snapshotter/estargz v0.16.3 → v0.18.2
github.com/coreos/go-systemd/v22       v22.5.0 → v22.7.0
github.com/cpuguy83/go-md2man/v2       v2.0.6 → v2.0.7
github.com/docker/cli                  v29.2.0+incompatible → v29.4.1+incompatible
github.com/docker/docker-credential-helpers v0.9.3 → v0.9.6
github.com/go-quicktest/qt             v1.101.1-0.20240301... → v1.102.0
github.com/go-test/deep                v1.0.4 → v1.1.1
github.com/godbus/dbus/v5              v5.0.4 → v5.2.2
github.com/golang/protobuf             v1.5.0 → v1.5.4 (deprecated — migrate to google.golang.org/protobuf)
github.com/google/go-containerregistry v0.20.6 → v0.21.5
github.com/google/jsonschema-go        v0.4.2 → v0.4.3
github.com/gorilla/mux                 v1.8.0 → v1.8.1
github.com/jackc/pgx/v5                v5.9.1 → v5.9.2
github.com/jsimonetti/rtnetlink/v2     v2.0.1 → v2.2.0
github.com/klauspost/compress          v1.18.0 → v1.18.5
github.com/lib/pq                      v1.10.9 → v1.12.3
github.com/mark3labs/mcp-go            v0.46.0 → v0.49.0
```

Per AGENT.md § Pre-release dependency audit: only upgrade if CVE-flagged or operator-requested (none of the above flagged). Pick up on the next minor-release dependency window.

## Process improvements

To prevent the same gaps next session:

1. **Pre-tag check** — before `git push origin v<tag>`, audit:
   ```bash
   # Did README marquee get bumped?
   grep "Current release" README.md | head -1
   # Did make build run (for patches) or make cross (for minors)?
   ls -la ./bin/datawatch* | head -3
   ```
2. **Post-tag upload** — after a tag pushes, immediately:
   ```bash
   gh release create v<tag> --notes-file docs/plans/RELEASE-NOTES-v<tag>.md ./bin/<binaries>
   ```
   The pattern fires on the same shell turn the tag was pushed.
3. **Embedded-docs guard** — never run `go build ./cmd/datawatch/` directly when iterating on a release; use `make build` so `sync-docs` runs.

These are reminders for the operator's next agent-driven release cycle; they don't go into AGENT.md as new rules (the existing rules are correct — the gap was in execution, not policy).

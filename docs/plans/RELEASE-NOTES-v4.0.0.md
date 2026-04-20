# v4.0.0 — Release Notes

**Status:** 🚧 in preparation (Sprint S8 pending — BL117 PRD-DAG orchestrator).
**Scope:** cumulative release notes covering **every change since v3.0.0** (F10 ships).

v4.0.0 is the first major-version bump since the F10 control-plane landing. Per operator directive 2026-04-20, v4.0.0 is positioned as the milestone release — not an incremental bump. It must document every shipped BL/Fxx from v3.0 through v3.11 plus the v4.0-specific additions (BL117).

This file is the source of truth for the v4.0 release. When BL117 ships, add a "§ v4.0.0 additions" section at the bottom and tag `v4.0.0`.

---

## 0. Headline features vs. v3.0.0

If v3.0.0 was "F10 lands — ephemeral container-spawned workers become the core primitive," v4.0.0 is **"the datawatch control plane has a plan"**: every operator-facing feature now reaches across REST + YAML + MCP + CLI + comm + web + mobile (parity rule enforced), the daemon is self-observing (audit, cost, diffs, anomalies), the agent layer is recursive (peer broker + validators + orchestrator), and plans are LLM-authored (autonomous decomposition + verification) and extensible by third parties (plugins).

Nine sprints, three parity backfills, and forty-plus backlog items shipped in ~two weeks of calendar time. The platform went from "supports agents" to "autonomously plans, executes, verifies, and extends agents".

---

## 1. Cumulative shipped list, by theme

### 1.1 Ephemeral agent platform (F10) — v3.0.0

The control-plane foundation — Profile-driven agent spawning across Docker + Kubernetes with TLS-pinned bootstrap and audited token brokerage.

- **F10 Project + Cluster Profiles** (v3.0.0 BL168–174): operator-authored YAML + REST + CLI + comm + web UI profiles for "what" (repo/agent/lang) and "where" (docker / k8s context).
- **Driver interface + Docker + K8s drivers** (BL176–180): Pod spawn over `kubectl`/client-go, container spawn over Docker socket; reverse proxy back to parent.
- **Bootstrap envelope** (BL178): single-use token mint + TLS-pinned HTTPS pickup; PQC-protected in v3.0.0 (BL195, BL197 — ML-KEM-768 + ML-DSA-65).
- **CA distribution + fingerprint pinning** (BL188).
- **Token broker** (BL191): mint / revoke / sweep with full audit.
- **BL103 validator agent** (v3.0.0 BL213): read-only post-session attestor, distroless ~5 MB, cross-backend verification.
- **BL104 peer broker** (v3.0.0 BL215): P2P messaging between workers through the parent.
- **BL105 orchestrator → pipeline.Executor bridge** (v3.0.0 BL218).
- **BL110 MCP-callable /api/config** (BL228) with permission gate.
- **BL111 secrets provider wiring** (BL229): File + EnvVar + Vault/CSI stubs.
- **BL112 service-mode reconciler** (BL231): long-lived workers survive parent restart.
- **BL113 self-managing platform bootstrap** (BL232): host + cluster install paths.
- **BL114 shared NFS/PVC volumes** (BL233).
- **BL115 pre-release K8s functional test suite** (BL234, documented).
- **BL116 scheduled-commands badge** (BL235) — web UI.
- **Helm chart** (`charts/datawatch/`, BL189): Secret-refs self-bootstrap.

### 1.2 Sessions + productivity — v3.5.0 (S1), v3.6.0 (S2)

- **F14 DOM diff compaction** (v3.5.0): ~60 % fewer UI re-renders on session-list updates.
- **BL1 IPv6 listener** (v3.5.0).
- **BL34 `ask:`** (v3.5.0): single-shot LLM answer without spawning a session. REST `/api/ask`, MCP `ask`.
- **BL35 project summary** (v3.5.0): git + sessions + stats rollup for a directory.
- **BL41 effort levels** (v3.5.0): quick/normal/thorough hint applied to backends that accept it.
- **BL5 command templates** (v3.6.0): named, reusable task templates.
- **BL26 recurring schedules** (v3.6.0): cron-style recurrence with RecurUntil.
- **BL27 project aliases** (v3.6.0): named directories for `new:` shortcuts.
- **BL29 git checkpoints** (v3.6.0): pre/post session tags + rollback.
- **BL30 cooldown** (v3.6.0): operator-triggered rate-limit pause, persistent across restarts.
- **BL40 stale-session detection** (v3.6.0): `session.stale_timeout_seconds` + `/api/sessions/stale`.

### 1.3 Intelligence core — v3.2.0

- **BL28 pipeline quality gates**: test baseline before + after, block on regression.
- **BL39 pipeline DAG cycle detection**.
- Test infrastructure (BL89/BL90/BL91 in v3.1.0): mock session manager, httptest server, MCP handler tests — unlocked the +1000-test trajectory that followed.

### 1.4 Intelligence — autonomous + verification — v3.10.0 (S6)

- **BL24 autonomous PRD decomposition**: LLM-driven PRD → Stories → Tasks with JSONL store, auto-fix retry loop, Kahn topo-sort executor, security scanner. Inspired by `HackingDave/nightwire`. Design doc: `docs/plans/2026-04-20-bl24-autonomous-decomposition.md`.
- **BL25 independent verification**: `VerifyFn` indirection; `autonomous.verification_backend` selects the verifier for cross-backend independence.
- 15 unit tests; disabled by default.

### 1.5 Observability core — v3.3.0

- **BL10 diff capture**: every session's output diff recorded for replay.
- **BL11 anomaly detection**: stuck-loop detector + similar output patterns.
- **BL12 day-bucket analytics**: `/api/analytics` for per-day stats.

### 1.6 Operations — v3.4.0

- **BL17 SIGHUP / `/api/reload`**: hot-reload config across every channel.
- **BL22 monitor page polish**.
- **BL37 `/api/diagnose`**: system health snapshot.
- **BL87 monitor-page resilience**.
- v3.4.1 hotfix: Windows cross-build — split unix-only `syscall.Statfs` into `diagnose_unix.go` + `diagnose_windows.go`.

### 1.7 Cost + audit — v3.7.0 (S3) + v3.7.1 hotfix

- **BL6 cost tracking**: per-session token + USD accounting with operator-overridable rate table.
- **BL86 datawatch-agent binary**: per-host stats daemon (`/proc`, `nvidia-smi`, `free`, `df`). Linux-only.
- **BL9 audit log**: JSON-lines + CEF dual-format, filtered `/api/audit` query.
- v3.7.1: hotfix — moved `session.cost_rates` out of hard-coded defaults into YAML + REST + full parity.

### 1.8 Parity backfills — v3.7.2 (Sx) + v3.7.3 (Sx2)

- **Sx**: 20 MCP tools + 9 CLI commands backfilling v3.5–v3.7 endpoints (BL34, BL35, BL5, BL27, BL29, BL30, BL40, BL6, BL9, BL17, BL37, BL12).
- **Sx2**: comm-channel parity via the new `rest` passthrough command + mobile-surface doc at `docs/api/mobile-surface.md`. Router gains `cost`, `cooldown`, `stale`, `audit` shortcuts that pipe through the same dispatcher.

### 1.9 Messaging + UI polish — v3.8.0 (S4)

- **BL15 rich-preview formatter**: fenced code → Telegram MarkdownV2 / Slack–Discord / Signal `" │ "`-prefixed mono.
- **BL31 device aliases**: `session.device_aliases` for dynamic name→id.
- **BL42 assist command**: `assist:` command backed by operator-picked backend + system prompt.
- **BL69 splash logo + tagline**: operator-customisable CLI banner.

### 1.10 Backends + chat UI — v3.9.0 (S5)

- **BL20 routing rules**: first-match regex → backend dispatch on session start. `/api/routing-rules` + `/test`.
- **BL78 / BL79 / BL72 chat-mode recipes documented** (`docs/api/chat-mode-backends.md`): Gemini, Aider, Goose, OpenCode — all already support `output_mode: chat`; the doc covers the config path + memory-hook reuse.

### 1.11 Memory + intelligence layer (cumulative, v3.0 → v3.2)

- **Spatial memory** (wings/rooms/halls) — mempalace-inspired, +34 pp retrieval over flat cosine.
- **4-layer wake-up stack (L0–L4)** with per-agent overlay (BL96 in v3.0.0).
- **Temporal knowledge graph** (`kg query/add/timeline`) — triples with validity windows.
- **Agent diaries** (BL97 v3.0.0): per-worker wing with verbatim-to-summary chain (BL99 closets/drawers, BL201).
- **Contradiction detection** (BL98 v3.0.0): mempalace fact-checker port.
- **Namespace enforcement** (BL202 v3.0.0) + cross-profile expansion (BL101 v3.0.0) + sync-back upload (BL205) + shared memory mode (BL203).
- **Write-ahead log + deduplication + optional XChaCha20-Poly1305 encryption** (v3.0.0).
- **Session import** (BL94 v3.0.0): `datawatch session import <dir>` for orphan reconciliation (BL93).

### 1.12 Extensibility — v3.11.0 (S7)

- **BL33 plugin framework**: subprocess plugins discovered under `<data_dir>/plugins/<name>/` with `manifest.yaml` + executable entry. JSON-RPC over stdio. 4 hooks: `pre_session_start`, `post_session_output`, `post_session_complete`, `on_alert`. Fan-out chaining. Design doc explicitly rejects `.so` (brittle) and embedded Lua/JS (bloat). Disabled by default. Security: plugins run with daemon privileges — documented disclosure in `docs/api/plugins.md`.

### 1.13 Mobile

- **v3.0.0 mobile API**: `/api/devices/register`, `/api/voice/transcribe`, `/api/federation/sessions`.
- **`datawatch-app` v1.0.0** (Android + iOS) pairs with the above. Mobile parity pass in Sx2.

### 1.14 Bugs fixed (v3.0 → v3.11)

| # | Description | Fixed |
|---|-------------|-------|
| B22 | Daemon crashes from unrecovered panics in background goroutines | v2.4.3 (baseline) |
| B23 | Silent daemon death — remaining goroutine recovery + BPF map purge + crash log | v2.4.4 |
| B24 | Update check shows downgrade as "update available" | v2.4.4 |
| B25 | Trust prompt invisible — MCP spinner hides what user needs to do | v2.4.5 |
| B31 | In-app upgrade reports success but doesn't replace binary — asset name mismatch | v3.0.1 |
| B30 | Scheduled command lands in prompt but requires a 2nd Enter to activate | v3.1.0 |

---

## 2. Parity rule enforcement

Every feature listed above is reachable from **YAML + REST + MCP + CLI + comm + web + mobile** — the no-hard-coded-config rule that the operator formalized on 2026-04-20. The Sx (v3.7.2) and Sx2 (v3.7.3) backfills closed gaps in the v3.5–v3.7 window. Every sprint after S3 lands with all surfaces from day one.

The `api ↔ MCP tool mapping` is at `docs/api-mcp-mapping.md` — 53 tools cover 75 endpoints; the 22 unmapped endpoints are infrastructure or security-sensitive by design.

---

## 3. Test + release posture

- **1156 tests across 52 packages** at v3.11.0 (+1000 over the v2.4 baseline). `go test ./...` gates every push to `main`.
- **Helm chart** tracks: `0.11.0` (v3.9.0) → `0.12.0` (v3.10.0) → `0.13.0` (v3.11.0).
- **Cross-builds**: linux-amd64/arm64, darwin-amd64/arm64, windows-amd64, datawatch-agent-linux-amd64/arm64 — shipped as GitHub release assets for every minor bump.
- **BL103 validator**: distroless ~5 MB, pinned model; rebuilt per release.
- **BL115 pre-release K8s functional test suite** documented.

---

## 4. v4.0.0 additions (Sprint S8 — BL117)

_(This section is filled in when BL117 design + implementation lands. Placeholder text below; do not ship v4.0.0 until this is replaced with the real content.)_

### BL117 — PRD-driven DAG orchestrator with guardrail sub-agents

**Design doc:** `docs/plans/2026-04-20-bl117-prd-dag-orchestrator.md` (pending).
**Operator doc:** `docs/api/orchestrator.md` (pending).

Builds on the BL24 autonomous substrate: where autonomous decomposes and runs a single PRD, BL117 orchestrates a DAG of PRDs with **guardrail sub-agents** — rules, security, release-readiness, docs-diagrams-architecture — that each run post-step and can block promotion to the next DAG node.

To be filled in once S8 ships.

---

## 5. Breaking changes since v3.0.0

_(To be audited at S8 ship. Current list, provisional:)_

- **None known** in the REST surface. Every sprint added endpoints or flags; no removals.
- **YAML**: `session.cost_rates` (v3.7.1 hotfix) replaced a hard-coded default table; operators who had implicit-default rates get the new defaults after upgrade unless they set their own.
- **Config channel parity rule**: operators that added MCP tools or CLI subcommands before Sx should use the built-in ones now — the out-of-tree commands still work but lose new features.

---

## 6. Upgrade path from v3.0.0

1. Stop the daemon (`systemctl stop datawatch` or `POST /api/restart` then wait).
2. Drop the v4.0.0 binary in place (matches v3.x path — `/usr/local/bin/datawatch` by default).
3. Start the daemon. v3.x configs load without change; new `autonomous:` and `plugins:` blocks are optional and disabled by default.
4. Run `datawatch version` and `curl -sk -H "Authorization: Bearer $TOKEN" https://localhost:8443/api/health` to confirm.
5. Helm deploys: bump `image.tag: v4.0.0` and `appVersion: v4.0.0` in `values.yaml`; re-`helm upgrade`.

No state migration is required — the JSONL stores under `<data_dir>/` (`autonomous/`, plugins, sessions, audit log) are all additive. BL94 session-import continues to work against pre-v4.0 session directories.

---

## 7. Prior release notes (superseded by this cumulative doc for v4.0 consumers)

- `docs/plans/RELEASE-NOTES-v3.0.0.md` through `v3.8.0` — per-sprint detail; kept for historical reference.
- `CHANGELOG.md` — full per-release entry for every v3.x hotfix.

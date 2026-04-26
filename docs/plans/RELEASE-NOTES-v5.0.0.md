# datawatch v5.0.0 — release notes

**Date:** 2026-04-26
**Spans:** v4.7.2 (2026-04-25) → v5.0.0 (2026-04-26)
**Closed:** 24 backlog items + 8 partial-or-deferred carry-overs
**Releases shipped during the stretch:** 28 patches + this major

## Why v5.0.0?

This major bump captures one cohesive piece of work: the
**operator-driven design pass** that turned a backlog of 19
operator-flagged questions and bugs into shipped code, structured
docs, and a clear conversation queue for the one remaining
operator-driven design (BL191 — autonomous PRD lifecycle). The
ABI hasn't broken; the package layout hasn't broken; nothing
breaks for an upgrading operator. The major bump signals
*completeness of a chapter*, not API churn.

## What shipped, by area

### Cross-cluster federation + observer

| Item | Version | What |
|---|---|---|
| **S14a foundation** | v4.8.0 | `observer.federation.parent_url` config, federation pusher goroutine, push-with-chain loop prevention (HTTP 409), `Envelope.Source` attribution. A primary datawatch can register itself as a Shape "P" peer of a root primary. |
| **BL177 — eBPF arm64 artifacts** | v4.8.22 | Per-arch vmlinux.h tree (amd64 locally BTF-dumped, arm64 from libbpf/vmlinux.h community dumps). Both `netprobe_*_bpfel.{go,o}` committed. Cross-build verified. |
| **BL181 — eBPF without `CAP_SYS_PTRACE`** | v4.8.21 | Pre-load kernel BTF via `btf.LoadKernelSpec()` instead of the cilium/ebpf default `/proc/self/mem` path. Operators no longer need a third capability beyond `CAP_BPF` + `CAP_PERFMON`. |
| **BL180 Phase 1 — ollama runtime tap** | v4.9.1 | Polls `/api/ps` every 5 s; per-loaded-model envelopes with `Caller` / `CallerKind="ollama_model"` / `GPUMemBytes`. New `Envelope.Caller` + `CallerKind` fields for the eventual Phase 2 eBPF correlation. |

### Container distribution + binary size

| Item | Version | What |
|---|---|---|
| **BL195 — public container distribution** | v4.8.22 | `.github/workflows/containers.yaml` matrix-pushes 8 multi-arch images to GHCR on every `v*` tag. `stats-cluster` air-gap tarball as a release asset. `make containers-push` / `containers-tarball` for local mirror. |
| **BL196 — binary size** | v4.8.17 | HTTP gzip middleware (`app.js` 372 KB → 90 KB on the wire). `make cross` rebuilt with `-trimpath -s -w` + opt-in UPX (`apt install upx-ucl` once for ~50 % shrink). |

### LLM + voice

| Item | Version | What |
|---|---|---|
| **BL189 — Whisper backend factory** | v4.9.0 | Local Python venv (default; unchanged) **or** OpenAI-compatible HTTP. Works against cloud OpenAI, OpenWebUI fronting ollama, faster-whisper-server, whisper.cpp server-mode. New `whisper.{backend,endpoint,api_key}` config fields. |

### PWA, docs viewer, and operator UX

| Item | Version | What |
|---|---|---|
| **BL198 / BL199** | v4.8.18 | `/diagrams.html` mobile fixes — collapsed aside no longer leaks a strip; diagram never disappears on Chrome mobile / installed PWA. Header link cleanup (drop "back to web UI"; API spec + MCP tools open in current tab). |
| **`/diagrams.html` UX upgrades** | v4.8.5 | Collapsible sidebar, mobile responsive, marked.js renders prose for files without mermaid blocks. |
| **BL182 — Input Required popup** | v4.8.8 | Yellow popup now patches in place via `refreshNeedsInputBanner` from the WebSocket state-change event — no more back-out/re-enter. |
| **BL183 — Orphan-cleanup affordance** | v4.8.8 | Always visible in Settings → Monitor → System Statistics (was hidden when count was zero). |
| **BL184 primary — opencode-acp recognition lag** | v4.8.20 | `markChannelReadyIfDetected` runs unconditionally on every output + `chat_message` WS event. (Thinking-message UX deferred.) |
| **BL178 — "view last response" stale** | v4.8.10 | Always fetches the live response; cached value shown first as "(updating…)" then patched in place. |
| **BL179 — search-icon to header bar** | v4.8.6 | Moved out of the sessions-card into the header next to the daemon-status light. |
| **Inline doc links toggle** | v4.8.7 | Per-browser preference in Settings → General; `docs` chip next to high-value section headers. |
| **BL197 partial — chat-channel autonomous PRD parity** | v4.9.2 | `autonomous {status, list, get, decompose, run, cancel, learnings, create}` + `prd` alias. PWA UI portion folded into BL191. |

### Docs

| Item | Version | What |
|---|---|---|
| **BL190 — six how-to docs** | v4.9.3 | autonomous-planning, cross-agent-memory, prd-dag-orchestrator, container-workers, pipeline-chaining, daemon-operations. Per-channel reach matrix on every walkthrough. |
| **BL192 — doc-coverage audit** | v4.8.16 / v4.8.19 | Added `docs/api/memory.md`, `docs/api/voice.md`, `docs/api/devices.md`, `docs/api/sessions.md`. Architecture-overview rows point at the new operator references. |
| **BL193 — comparison/mapping audit** | v4.8.13 / v4.8.14 / v4.8.15 | Rewrote `docs/llm-backends.md`, `docs/api-mcp-mapping.md`, `docs/messaging-backends.md`, `docs/architecture-overview.md`, `docs/data-flow.md` from source-of-truth. Internal IDs swept across all five. |
| **BL188 — attribution accuracy** | v4.8.9 + v4.8.16 | `docs/plan-attribution.md` — nightwire credit expanded; Aperant under "Researched and skipped"; mempalace closets/drawers + agent diaries + KG contradiction detection corrected from "not implemented" to "implemented". |
| **BL176 — RTK upgrade string** | v4.8.9 | PWA chip + OpenAPI + chat help all show the install.sh one-liner. |
| **BL186 — internal-IDs in CLI/log strings** | v4.8.8 | CLI long-help + setup ebpf epilogue swept of "Shape A/B/C" / "F10" / "S13". |
| **BL194 — MCP tools link in /diagrams.html** | v4.8.11 | Header gains a third link beside "API spec". |
| **Internal-ID PWA sweep** | v4.8.1 / v4.8.2 / v4.8.3 / v4.8.4 | eBPF noop msg, federated peers card, profile placeholder, "Cluster nodes (Shape C)" subscript, flow-doc renames + Mermaid additions, diagrams cleaned of internal IDs. |

### Process discipline

| Item | Version | What |
|---|---|---|
| **README marquee + backlog refactor each release** rules | v4.8.8 / v4.8.12 | Added to AGENT.md as canonical operator process. |
| **Binary-build cadence rule** | v4.8.12 | Patch releases skip `make cross` + binary asset attachment; minor / major releases attach all five. v5.0.0 is the first major after this rule. |
| **Container-maintenance audit rule** | v4.8.12 | Every release audits the Dockerfile + Helm chart product surface. |
| **Backlog structural refactor** | v4.8.16 | `docs/plans/README.md` reorganised: Pending / Open backlog above Frozen Features; Frozen / external right after; Open backlog split into Active work (table) vs. Awaiting operator action (structured prose with What's-needed + Options + Recommendation per item). |
| **`docs/plans/README.md` rules dedupe** | v4.8.12 | Operating rules consolidated in `/AGENT.md` as single source of truth. The plans file holds project-tracking only. |
| **Docs-sync hardening (BL175)** | v4.8.18 | `docs/_embed_skip.txt` manifest; `scripts/check-docs-sync.sh`; `hooks/pre-commit-docs-sync`; `.github/workflows/docs-sync.yaml` CI guard. |

### Earlier work (carried forward in this stretch)

- **S13 follow — observer enrichment** (v4.7.2) — orchestrator graph
  endpoint joins per-PRD session IDs to envelopes from local + every
  peer's last snapshot.

## Open / carry-over

| Item | Status |
|---|---|
| **BL191** — autonomous PRD lifecycle design | Six structured questions (review-and-edit gate, library, decisions log, recursion, PWA UI, BL117 overlap) ready for the design conversation. **Do not implement until aligned.** Doc: [`2026-04-26-bl191-autonomous-prd-lifecycle.md`](2026-04-26-bl191-autonomous-prd-lifecycle.md). |
| **BL184 secondary** | Thinking-message UX deferred — needs daemon to wrap thinking blocks in `<thinking>…</thinking>` or relaxed regex. |
| **BL180 Phase 2** | eBPF socket-tuple `(client_pid, server_pid)` correlation. Depends on the in-progress kprobe attach loader (BL173 task 1 stub). |
| **BL173-followup** | Live cluster→parent observer push verification on a production cluster. Operator-side. |
| **Binary upload backlog** | Several v4.5.x–v4.8.x patches shipped code-only because GitHub release-asset upload was intermittent. v5.0.0 binaries should land cleanly. |

## Upgrade path

- **From any v4.x:** `datawatch update && datawatch restart` picks
  up v5.0.0 cleanly. ABI unchanged; config keys are additive
  (whisper backend factory, observer ollama tap, observer
  federation, docs-sync skip manifest).
- **Container operators:** new images at `ghcr.io/dmz006/datawatch-*:v5.0.0`.
  Helm chart `appVersion` should bump to match.
- **arm64 operators:** eBPF is now opt-in-able on Thor / Apple
  Silicon — the artifacts are in the binary; no extra steps.

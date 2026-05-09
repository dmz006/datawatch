# datawatch v7.0.0 — Cumulative release notes (v6.0 → v7.0)

## What v7.0.0 is

The major architectural shift from "Council Mode is a stub framework" to **real, federated, observable LLM workloads** managed by datawatch as first-class infrastructure.

Three new concepts, one shipped surface:

1. **ComputeNode** — anywhere local LLM workloads run (host, GPU box, cluster behind a load balancer, container, remote-proxied datawatch peer). Each Node has identity, declared capacity, monitoring (via existing `datawatch-stats`), RBAC, scheduling priority, costs, and maintenance windows.
2. **LLM registry** — operators define `(name, kind, model, ordered_compute_node_failover_list)` once and every datawatch consumer (Council, /api/ask, BL297 persona wizard, agent spawn) calls them through a single dispatcher. Adapters for ollama, openwebui, opencode, claude.
3. **Real Council debates** — STUB strings stripped. Each persona response is a real LLM call routed through the dispatcher with ordered ComputeNode failover. Async-first POST + SSE live updates stream round-by-round events to the Automata UI Council tab.

Plus: **scope-hierarchy persistent memory** (persona-global → persona-in-project → project-shared → session-local) with read-only borrow, operator-curated seed, and breadcrumb-preserving promote.

## Headline changes since v6.0

### v6.0 → v6.10 — Council framework foundation
- v6.11.0 BL260 Council Mode — multi-persona structured debate framework (stubbed inference, real synthesis)
- v6.7.0 BL255 Skills registries + Identity Telos (BL257)
- v6.9.0 BL258 Algorithm Mode 7-phase per-session harness
- v6.10.0 BL259 Evals framework

### v6.11 → v6.13 — PAI work + Automata redesign
- BL221 Automata redesign (multiple phases through v6.2.x)
- v6.13.x — Automata mobile-first overhaul, lifecycle strip, persona affordance, theme polish
- BL277/278/279 closed (banner ripout / theme toggle / docs viewer spacing)

### v6.14 → v6.15 — docs viewer + secrets
- v6.14.0 BL279 full-corpus See-also cross-link sweep (48 docs)
- v6.15.0 BL267 HashiCorp Vault / OpenBao secrets backend

### v6.16 → v6.21 — BL274 Docs-as-MCP-Interface (6 sprints)
- Vector + BM25 hybrid search across howtos
- Plan-then-execute pattern with approval-token round-trip
- Risk gate; 22 curated howtos; bulk-trust UX
- 4 release-smoke lints + meta-howto + 3 AGENT.md rules

### v6.22 — Council Mode polish + persona wizard
- v6.22.3 BL297 Council "Add Persona" wizard with LLM-drafted prompts (5-step interview + tune-loop, 7-surface parity)
- v6.22.3 BL298 showError() UX (16px, no auto-dismiss, ✕ to ack)

### v6.22.4 → v6.22.6 — v7 pre-flight
- v6.22.4 Council.DraftRetentionDays full-surface parity (audit-honesty fix)
- v6.22.5 datawatch-stats `-help` aliases + `--debug-connections`
- v6.22.6 datawatch-stats `--setup-ebpf` helper

### v7.0.0-alpha.1 → alpha.7 — the v7 build
- **alpha.1 (S1)** ComputeNode registry + datawatch-stats integration
- **alpha.2 (S2)** LLM-inference registry + dispatcher + 4 adapters
- **alpha.3 (S3)** Council orchestrator wired to dispatcher (real LLM debates; STUB strings gone)
- **alpha.4 (S4)** SSE live updates + async-first /api/council/run + Automata live-watch cards
- **alpha.5 (S5)** Scope-hierarchy memory (recall/borrow/seed/promote) + datawatch-stats howto polish
- **alpha.6** datawatch-stats `--diag` envelope diagnostic + plan-doc → APPROVED
- **alpha.7** Federated peers self-as-peer + Reuse-and-Expand AGENT.md rule + Memory MCP/comm

## Operator-visible surface changes

### New REST endpoints
```
/api/compute/nodes/*              ComputeNode registry CRUD + health + on-demand pull
/api/llms/*                       LLM registry CRUD + /test
/api/council/runs/{id}/events     SSE stream
/api/council/runs/{id}/cancel     in-flight run cancel
/api/council/personas/draft*      v6.22.3 persona-wizard drafts (also BL297)
/api/council/config               draft retention runtime knob
/api/memory/scopes/{recall,borrow,seed,promote}
```

### New CLI subcommands
```
datawatch compute node {list,get,add,update,delete,health,detail}
datawatch llm {list,get,add,update,delete,test}
datawatch council cancel <run-id>
datawatch council persona-wizard one-shot --name X --role Y --focus ...
datawatch council config {get,set draft-retention-days <N>}
datawatch memory scope {recall,borrow,seed,promote}
datawatch-stats --debug-connections | --setup-ebpf | --diag
```

### New comm verbs
```
compute node {list,get,add,update,delete,health,detail}
llm {list,get,add,update,delete,test}
council cancel <id>  |  council config  |  council persona-wizard ...
memory scope {recall,borrow,seed,promote}
```

### New MCP tools
17 new tools across compute_node_*, llm_*, council_run_cancel, council_config_*, council_persona_*, memory_scope_*.

## Migration

**Auto-migration runs on first v7 daemon startup.** Existing `cfg.ollama.host` and `cfg.openwebui.url` become `ollama-default` and `openwebui-default` LLM registry entries; matching ComputeNodes (`local-ollama`, `local-openwebui`) are derived and linked. No operator action required for existing configs.

The legacy `/api/ask` `backend` field (`"ollama"` or `"openwebui"`) keeps working — it shims to the auto-migrated LLM names. Prefer the new `llm` field in new code.

**No breaking changes** for operators using only the v6.x surface. The cfg shim continues until v8.0.

## What's deferred

These items are tracked but ship in v7.x patches:

- **Per-persona session spawning** — operator-attachable `council-<run>-<persona>` sessions (S4.c rolled forward)
- **Council comm push at milestones** — coupled to operator's "watch switch" UX (#183)
- **PWA Memory panel** — browse-by-scope + promote button (CLI/REST cover the path today)
- **datawatch-stats multi-parent registration** + per-instance/process attribution (#203)
- **Statusline / large project dashboard** for autonomous sessions (#202)
- **btop-inspired system-status dashboard** with per-ComputeNode tabs (#213)
- **Automata git-repo fork + merge-with-approval workflow** + focused-builder persona (#214)
- **Full-browser PWA mode** with window borders + multi-column big-screen layouts (#215)
- **Howto screenshot conventions** (link to GH raw + offline-graceful) (#212)
- **PWA-wide refresh-button audit** (replace all manual refresh with SSE/polling) (#210)
- **Comprehensive docs/diagrams audit** (#182)

## Known issues at stable cut

- Per-persona session spawning is intentionally deferred (see above). Operator can `datawatch attach <session-id>` for sessions but not for individual personas yet.
- Comm push at council milestones is intentionally deferred. Council activity is visible via SSE in the Automata UI; chat channels see only the operator's manual `council run` reply.

## Acknowledgments

This release was built across 2026-05-08 → 2026-05-09 in a sustained autonomous run, driven by operator's BL295 design interview (30 questions answered upfront so sprints didn't need to stop mid-stream). 9 alpha cuts shipped in ~24 hours.

🤖 Generated with [Claude Code](https://claude.com/claude-code)

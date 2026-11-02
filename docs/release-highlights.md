# Release highlights

Older releases in one line per feature, grouped by minor version. The current
and most recent releases are summarised on the [project README](../README.md);
the full patch-by-patch history is in [CHANGELOG.md](../CHANGELOG.md).

## v8.63.0–v8.73.1 highlights

- claude-code and ollama sessions now report real token usage/cost
  (`Session.TokensIn/TokensOut/EstCostUSD` had been zero for every session
  on every backend). The PWA shows the animated datawatch eye for
  content-loading states across 123 cards/panels/sheets, matching the apps
  (GH#186), and council live-run logs render markdown instead of truncated
  plain text (GH#181).
- Native ACME/Let's Encrypt feature-complete: HTTP-01, DNS-01 via
  Cloudflare (wildcards / port-80-unavailable), zero-downtime hot-swap
  renewal, and native APNs push dispatch straight to registered iOS
  devices. See [docs/howto/letsencrypt-acme.md](docs/howto/letsencrypt-acme.md).
- Several real security/config-sync bugs found and fixed: the LLM
  registry leaking a literal API key in clear text (GH#179), a config/
  manager desync where `PUT /api/autonomous/config` and a council LLM
  change didn't take effect until a daemon restart, APNs push dispatch
  mixing up sandbox/production hosts per-device.
- A help-link anchor mismatch fixed across most PWA cards (GH#189), 3
  missing help-link pages, a Community Plugins browse/install card
  (GH#191), and PRD scratch artifacts (`CHECKPOINT.md`,
  `.decompose-output-*.json`) relocated out of the shared project
  repository into a managed per-PRD scratch directory.

## v8.45.0–v8.61.1 highlights (PWA parity sweep, Phases 2–3, now complete)

- Repair-deps button, council-run markdown rendering, memory-scope browser + promote, voice-reply quick commands, "memory promote to" wizard field, watch sessions/automata, terminal search + copy, skeleton loading shimmer, mute-session notifications, splash status line + replay.
- PRD `permission_mode` editor, a persisted session Chrome-enabled badge, pause/resume for a running Automaton (new paused status + cooperative executor drain), Observer Backend Health / Envelopes / quick add-memory cards, an About-screen subsystem reload + MCP channel/tools status cards, a Config Viewer + safe raw editor, and a hand-rolled SVG dependency-graph card for an Automaton's stories/tasks.

See [CHANGELOG.md](CHANGELOG.md) for the complete patch-by-patch history.

## Release eras

- **v9.x** — Memory-lifecycle-complete era: the subprocess scope isolation / lifecycle management / PRD-integration memory trilogy, full Android/iOS parity sweeps, and federation live-store consistency fixes.
- **v8.x** — Federation era: capability-based access control (50 capabilities, 14 built-in groups) gating every REST endpoint and MCP tool, multi-mode Compute Node routing (direct / docker-network / cross-peer proxy), channel routing, federated file service, discussion-scoped shared memory, full operational data encryption, and the Android/Wear/Auto app.
- **v7.x** — Compute abstraction era: Compute Node registry + LLM Registry with automatic failover dispatch, the Ollama Marketplace, Claude Code hooks + live status board, systematic capability enforcement across 110+ endpoints.
- **v6.x and earlier** — PAI-parity era: operator identity, Algorithm Mode's 7-phase reasoning harness, the Evals framework, Council Mode's multi-persona debate, Skill Registries, the native Secrets Manager, Tailscale mesh, and the original Signal-bridge core.

Full detail for any version: [CHANGELOG.md](CHANGELOG.md).

---

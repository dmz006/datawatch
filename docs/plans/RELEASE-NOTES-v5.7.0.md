# datawatch v5.7.0 — release notes

**Date:** 2026-04-26
**Spans:** v5.0.0 (2026-04-26) → v5.7.0 (2026-04-26)
**Closed:** BL200 (howto coverage expansion) + leak-audit + observer-OOM hotfix series + BL202 + BL203 + BL288 + BL289 + BL291 + BL292
**Releases shipped during the stretch:** v5.0.1 → v5.6.1 patches + this minor

## Why v5.7.0 (minor)?

The v5.x patch series since v5.0.0 has been mostly bug-fix work
(leak audit pass 1 + 2, observer correlator OOM, sw.js cache, FAB
position, mobile aside-collapse), but two real *features* landed
that the operator surface needs to advertise:

- **BL200 — howto coverage expansion**: the docs/howto/ suite went
  from 6 walkthroughs to **13**, covering setup+install,
  chat+LLM quickstart, comm channels, MCP tools, voice input,
  federated observer, and autonomous review/approve in addition
  to the original six. Existing walkthroughs were swept for
  references to features shipped after they were first written.
- **`datawatch reload` CLI subcommand** (parity fix): BL17 already
  exposed hot-config reload as SIGHUP + `POST /api/reload` + the
  MCP `reload` tool, but the CLI was missing. Closing this gap
  satisfies the configuration-parity rule and gives every howto a
  single canonical command operators can copy-paste after a
  `datawatch config set …`.

A minor bump (rather than another patch) is correct because (a) a
new CLI subcommand IS a new operator-visible feature and (b) the
howto coverage is a cohesive feature delivery, not a fix.

## What's new

### BL200 — How-to coverage expansion (13 walkthroughs)

| Doc | What it covers |
|-----|----------------|
| `setup-and-install.md` | First-time install end-to-end |
| `chat-and-llm-quickstart.md` | The 1-2 most-common chat + LLM pairings |
| `comm-channels.md` | All 11 messaging backends with copy-paste config |
| `mcp-tools.md` | Wire datawatch into Claude Code / Cursor / any MCP host; live tool surface |
| `voice-input.md` | Whisper / openai / openai_compat / openwebui / ollama backends |
| `federated-observer.md` | Multi-host stats — Shape A/B/C peers, token mint, primary registration |
| `autonomous-review-approve.md` | The PRD lifecycle gate: review the decomposition, approve / reject / request-revision before run |
| (refreshed) `autonomous-planning.md` | Free-form spec → stories+tasks → run with verification |
| (refreshed) `prd-dag-orchestrator.md` | PRD graphs with guardrails |
| (refreshed) `container-workers.md` | Project + cluster profiles, ephemeral workers |
| (refreshed) `pipeline-chaining.md` | DAG pipelines with before/after gates |
| (refreshed) `cross-agent-memory.md` | Episodic memory + KG across builds + peers |
| (refreshed) `daemon-operations.md` | Day-two operator workflow |

Each walkthrough keeps the per-channel reachability matrix at the
bottom (CLI / REST / MCP / chat / PWA) so operators can pick the
surface they prefer.

PWA screenshot rebuild for these 13 docs is queued as **BL190
follow-up** (chrome plugin removed; will use puppeteer-core +
seeded JSONL fixtures from the CLI per the BL190 plan doc).

### `datawatch reload` CLI parity fix

Hot-reloads daemon config without a full restart — the same path
operators have had via SIGHUP and `POST /api/reload` since BL17,
now reachable from the CLI:

```bash
datawatch config set signal.allowed_recipients '["+15551234567"]'
datawatch reload                        # pick up the change live
```

`datawatch restart` is still the right call for changes that
aren't hot-reloadable (server port, log path, encryption mode).

## What's *not* in v5.7.0 (carry-over)

- **BL180 Phase 2** — eBPF kprobes (backed out cleanly mid-edit
  in v5.6.0; will resume with `BPF_MAP_TYPE_LRU_HASH` + userspace
  TTL pruner) and cross-host federation correlation.
- **BL191 Q4 / Q6** — recursive child-PRDs through the BL117
  orchestrator, and guardrails-at-all-levels.
- **BL201** — voice/whisper backend inheritance (PWA dropdown
  shipped v5.2.0; daemon-side resolution still queued).
- **BL190** — PWA screenshot rebuild against the now-13-doc suite.

## Upgrade path

```bash
datawatch update         # check + install
datawatch restart        # apply the new binary; preserves tmux sessions
datawatch reload --help  # verify the new CLI command landed
```

The two-place version sync (`cmd/datawatch/main.go` +
`internal/server/api.go`) was off by 4 patch releases (api.go
stuck at 5.0.3 while main.go marched to 5.6.1); both files are
now back at 5.7.0 in lockstep, and the discipline note in
AGENT.md § Versioning has a stronger reminder.

## See also

- [`RELEASE-NOTES-v5.0.0.md`](RELEASE-NOTES-v5.0.0.md) — the
  v4.7.2 → v5.0.0 cumulative cut.
- [`RELEASE-NOTES-v4.0.0.md`](RELEASE-NOTES-v4.0.0.md) — the
  v3.0.0 → v4.0.0 cumulative.
- [`docs/howto/README.md`](../howto/README.md) — the new 13-doc index.

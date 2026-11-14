# Document the self-config group, the alias, and the hardening: plan

**Date:** 2026-11-02
**Version:** set at merge time. The merge helper sets the next patch above main; this plan names no number and adds no versioned changelog heading.
**Design record:** `docs/plans/2026-10-29-bl412-self-config-gate-proposal.md` (option C — the alias over a builtin capability group — and the operator's answers are recorded there)

## What this delivers

The capability already ships (BL412, option C): a builtin **self-config** group in the
federation capability system, an operator-facing alias `mcp.allow_self_config` (default
enabled), bootstrap protection of the alias itself, and a three-surface audit log. What
was missing is the operator-facing documentation of all of it, as one coherent story:

1. **A how-to that walks the whole surface.** `docs/howto/self-config-capability.md` —
   what the group is and which capabilities it carries, what the alias does and how the
   default (enabled) behaves, how to enable or disable it on every operator surface
   (YAML, CLI, Web UI, REST, comm channel, MCP — where the last is precisely the gated
   path), the bootstrap-protected key, the audit log (what a record holds, how to read
   it on every surface, what `self_config_audit_denied` means), and the documented
   hardening steps with the exact refusal messages. Written per the how-to index rules
   (`docs/howto/README.md`): detail, all channels, diagrams-free links to the
   definitions instead of a second copy.
2. **A definition, not a second how-to.** A **Self-config** section
   (`#self-config`) in `docs/datawatch-definitions.md` — the group, the alias, the
   default, the bootstrap rule, the audit — with the how-to as the link. The PWA settings
   card's "Definitions" link now points at it explicitly
   (`gc_selfconfig` in `internal/server/web/app.js` `DOCS_LINKS_BY_KEY`).
3. **The settings in the config reference.** `mcp.allow_self_config`,
   `mcp.self_config_audit_path`, `mcp.self_config_audit_denied` in the `mcp:` block of
   `docs/config-reference.yaml`, and the same three fields in the `mcp.*` rows of the
   `docs/implementation.md` configuration table.
4. **Mentions where operators actually look.** A "Self-config" section in
   `docs/operations.md` (next to the other security/MCP settings), an optional
   post-install section in `docs/setup.md` (decide the default early), a short
   "Changing the daemon's own settings" note in `docs/cursor-mcp.md` (the MCP surface
   operators configure first), and index lines in `docs/README.md` and
   `docs/howto/README.md`.

Every behaviour the docs describe is already a setting whose default is the operator's
answer: enabled by default, per-session scoping via `session.capabilities`, denial
recording on by default, audit path defaulted under the data dir. Nothing in the docs is
tied to this repository's name, paths or people.

## Parity surface

| Surface | The alias (`mcp.allow_self_config`) | The audit log |
|---|---|---|
| YAML | `mcp.allow_self_config: false` (+ restart) | `mcp.self_config_audit_path`, `mcp.self_config_audit_denied` |
| CLI | `datawatch config set mcp.allow_self_config false` / `config get` | `datawatch audit self-config [--limit]` |
| Web UI | Settings > Self-config card (allow toggle, audit path, record-denied toggle) | same card |
| REST | `PUT /api/config` `{mcp:{allow_self_config:false}}` | `GET /api/audit/self-config[?limit=1..1000]` |
| Comm channel | `configure mcp.allow_self_config=false` (admin) | read-only: `GET /api/audit/self-config` via REST |
| MCP | `config_get` reads it; `config_set` refuses it — bootstrap-protected, by design | `self_config_audit` (read-only, never gated, limit 1–1000 default 100) |

### Monitoring and observability

Decided per surface; none adds code.

| Monitoring surface | Decision |
|---|---|
| SystemStats field | Not applicable. SystemStats reports resource and capacity gauges for the daemon and its Automata. Self-config writes are rare, operator-scale events, not a load signal, and the audit log already holds the exact record of each one. |
| Monitor card stats | Not applicable. The Monitor card charts load over time. The Settings > Self-config card already shows the audit view, so a second count there would duplicate it. |
| Prometheus metric | Not applicable. A counter of config-write attempts would only restate the audit log, which is append-only JSONL and can be shipped to an archive or alerting pipeline. Alerting on denied writes is done from that log. |

If self-config writes ever become high-volume, a single counter incremented at the
audit write point would be the place to add one, exposed on all three surfaces at once.

The group itself is managed through the federation group machinery, not through the
alias: `datawatch federation group builtins|list|add|update`, and per-session scoping
with `session.capabilities` (the default grant is `config:write` present iff the alias
is on, per `SessionDefaultGrantCaps`).

## Documentation updates

- **Added** `docs/howto/self-config-capability.md` (group, alias, default, surfaces,
  bootstrap rule, audit, hardening, verification with the exact refusal texts).
- **Added** `docs/datawatch-definitions.md` § Self-config (with TOC entry).
- **Added** `gc_selfconfig` → `DOCS_DEFINITIONS + '#self-config'` in
  `internal/server/web/app.js`.
- **Added** the three `mcp.*` fields to `docs/config-reference.yaml` and
  `docs/implementation.md`.
- **Added** `docs/operations.md` § Self-config; `docs/setup.md` optional section;
  `docs/cursor-mcp.md` note; index lines in `docs/README.md` / `docs/howto/README.md`.
- No locale changes needed: the PWA card keys (`settings.self_config*`) already exist in
  `internal/server/web/locales/{fr,de,es}.json`.

## Verification

- `env -u TMUX -u TMUX_TMPDIR go build -o datawatch ./cmd/datawatch/`
- `make sync-docs`
- The how-to's refusal texts match the code:
  gate refusal (`internal/mcp/bl220_mcp_tools.go`), bootstrap refusal
  (`internal/mcp/memory_tools.go`), comm refusal
  (`internal/server/config_audit.go` / `federation_cap.go`), audit record shape
  (`internal/federation/config_audit.go`, `internal/mcp/self_config_audit.go`).

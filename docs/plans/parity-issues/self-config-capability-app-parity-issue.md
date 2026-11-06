# Title

`feat(self-config): parity for the self-config capability group, the mcp.allow_self_config alias and the config-write audit (parent: datawatch, version assigned by the merge helper)`

## Body

The PWA (and the native clients that mirror it) gained a **Self-config** card and a
read path for a new single audit log. The server-side behaviour shipped in datawatch;
this issue tracks the datawatch-app side: the three settings on the settings screen,
the changed/added API contract, and the screen the audit lives on.

## Settings (three keys, one card)

| Key | Type | Default | Notes for the app |
|---|---|---|---|
| `mcp.allow_self_config` | boolean | enabled (absent = `true`) | The alias for the builtin `self-config` capability group. Off = sessions/agents may not write the daemon's non-secret configuration; every attempt is then refused and audited. Changes take effect on daemon restart for newly spawned sessions. A session credential can never write this key (bootstrap-protected) — only operator surfaces (YAML, CLI, Web UI, REST) can. |
| `mcp.self_config_audit_path` | string | `<data dir>/audit/config-self-modify.jsonl` | Where the audit JSONL is written; changing it reopens the sink, existing records stay put. |
| `mcp.self_config_audit_denied` | boolean | `true` | `false` = record only allowed writes. Denied attempts are recorded by default, with the refusal reason. |

All three are settable without a restart from YAML, CLI, Web UI, REST, comm channel
(the alias and the two audit keys; `mcp.allow_self_config` itself is
bootstrap-protected on the session/MCP write surface only).

## API contract changes

- `GET /api/config` — the `mcp` section gains `allow_self_config` (boolean, default
  `true`), `self_config_audit_path` (string), `self_config_audit_denied` (boolean,
  default `true`). Clients should treat an absent `allow_self_config` as `true`.
- `PUT /api/config` — accepts the same three fields in the `mcp` object
  (e.g. `{"mcp": {"allow_self_config": false}}`); changes are written to the config
  file and take effect immediately (restart required for the default capability
  grant of newly spawned sessions).
- **New endpoint** `GET /api/audit/self-config` (read-only, optional `?limit=N`,
  1–1000, newest first). One record per configuration-write attempt by a session or
  peer credential, regardless of surface:

  | Field | Type | Notes |
  |---|---|---|
  | `time` | string (RFC3339, UTC) | When the attempt happened |
  | `surface` | string | `mcp`, `rest` or `comm` |
  | `tool` | string | The MCP tool name, `PUT /api/config`, or `configure` |
  | `keys` / `values` | arrays/objects | What was attempted; secret-looking values redacted; absent on some denials |
  | `caller` | string | `session:<id>` or `peer:<name>`; operator admin writes are not recorded |
  | `allowed` | boolean | |
  | `reason` | string | Present only when `allowed` is `false` |

- MCP tools (for clients that surface MCP state): `config_get` reads the three keys;
  the read-only `self_config_audit` tool (`limit` 1–1000, default 100) is never
  gated; `config_set` with key `mcp.allow_self_config` is always refused
  (`refused: mcp.allow_self_config is bootstrap-protected; edit the YAML directly
  to change it`), and every other config-write tool is refused while the alias is
  off (`permission denied: self-modify is disabled (mcp.allow_self_config: false) —
  remove the explicit false (the default is enabled) and restart to enable`).
- Comm channel: `configure mcp.allow_self_config=false` is refused for a session
  credential once it lacks `config:write`
  (`session token lacks capability: config:write`), refusal audit-logged.

## Screens

- **Settings → General → Self-config card** (PWA parity baseline): an
  "Allow self-config" toggle (`mcp.allow_self_config`), an audit-path text field
  (`mcp.self_config_audit_path`, placeholder `<data dir>/audit/config-self-modify.jsonl`),
  and a "Record denied attempts" toggle (`mcp.self_config_audit_denied`), with help
  text on both toggles. Locale keys are `settings.self_config*` (all five bundles).
- **Recent self-config audit records** on the same card: newest-first list from
  `GET /api/audit/self-config` showing time, surface, tool, keys, caller, allowed
  and refusal reason; states for loading, empty ("No self-config audit records
  yet."), truncation ("N older record(s) not shown") and unavailable
  ("Self-config audit log unavailable"). Locale keys are
  `settings.self_config_recent*` (all five bundles).
- **Definitions link**: the card's "Definitions" link points at
  `datawatch-definitions.md#self-config`.
- Android/iPhone: mirror the card, the settings and the audit reader (or record an
  explicit exclusion with reason per the parity rule); translations of the new
  keys come through the Compose Multiplatform pipeline.

## References

- datawatch how-to: `docs/howto/self-config-capability.md`
- datawatch glossary: `docs/datawatch-definitions.md#self-config`
- OpenAPI: `docs/api/openapi.yaml` (`/api/audit/self-config`, `ConfigMcp`)
- Design record: `docs/plans/2026-11-02-self-config-capability.md`

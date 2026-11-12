# Title

`feat(sensitive-writes): parity for the sensitive write tool class and the mcp.allow_sensitive_writes setting (parent: datawatch, version assigned by the merge helper)`

## Body

The PWA gained one new row on the **Self-config** card: an **Allow sensitive writes**
toggle. The server-side behaviour shipped in datawatch; this issue tracks the
datawatch-app side: the new toggle on the settings screen, its help text in five
locales, and the changed API contract.

A **sensitive write** is a call to one of a fixed list of high-impact MCP tools that a
model session or a federation peer makes. The list is: `secret_set`, `secret_delete`
and `secret_get` (it only reads, but it returns the secret value), `set_identity`,
`update_identity`, `configure_identity`, `restore_identity`, `reset_identity`,
`restart_daemon`, `reload`, `plugin_install`, `plugin_run_subcommand`, `send_input`,
`start_session`, `tailscale_acl_push`, `tailscale_auth_key` and `acme_renew`. Listing
secret names, checking that a secret exists and the vault status are not in the class.
Every MCP tool belongs to exactly one of three classes: configuration writes (covered
by the self-config group), sensitive writes, and everything else.

## Setting (one key)

| Key | Type | Default | Notes for the app |
|---|---|---|---|
| `mcp.allow_sensitive_writes` | boolean | enabled (absent = `true`) | On: a session or peer may call the sensitive tools. Off: each such call from a session or peer is refused, with a message that names the setting, and audited. Operator-run calls (the PWA, REST, the CLI and an admin MCP caller) are never refused. It is independent of `mcp.allow_self_config`. Unlike `mcp.allow_self_config` it is **not** bootstrap-protected: a session that holds `config:write` can write it through `config_set`. |

The key is settable without a restart from the Web UI, the CLI
(`datawatch config set mcp.allow_sensitive_writes false`), `PUT /api/config`, and the
comm channel (`configure mcp.allow_sensitive_writes=false`; it is listed by
`configure list`). A change is applied to the running daemon and saved to the config
file; the gate reads it on every call. From YAML (`mcp:` block) it is read at
restart. Write exactly `true` or `false`: the daemon reads `true`, `yes` and `1` as on
and anything else as off, so a client must send a real boolean and never free text.

## API contract changes

- `GET /api/config` — the `mcp` section gains `allow_sensitive_writes` (boolean). It is
  the **effective** value, so a config that never set the key reads `true`. Clients
  should treat an absent value as `true`.
- `PUT /api/config` — accepts `mcp.allow_sensitive_writes` as a JSON boolean (for
  example `{"mcp.allow_sensitive_writes": false}`, the same way
  `mcp.allow_self_config` is sent). It is stored as an explicit boolean, takes effect
  immediately and is written to the config file.
- `GET /api/audit/self-config` — records gain an optional `class` field. MCP records
  carry `config` for a configuration-write tool and `sensitive` for a sensitive write
  tool; `rest` and `comm` records carry none. A `sensitive` record lists the argument
  names in `keys` and **never any values**. Refused records appear unless
  `mcp.self_config_audit_denied` is `false`.

  | Field | Type | Notes |
  |---|---|---|
  | `class` | string, optional | `config` or `sensitive`; absent on `rest` and `comm` records and on records written before the class existed |

  The refusal reason for a sensitive tool reads: `tool <name> is refused: sensitive
  writes are disabled for sessions and peers (mcp.allow_sensitive_writes is false). An
  operator can re-enable them by setting mcp.allow_sensitive_writes to true (or
  removing the key) in the datawatch config.`
- MCP tools (for clients that surface MCP state): `get_config` with `section: "mcp"`
  reports `allow_sensitive_writes` beside `allow_self_config`, each as its effective
  on/off state. Reading is never gated.

## Screens

- **Settings → General → Self-config card**: add an **Allow sensitive writes** toggle
  (`mcp.allow_sensitive_writes`) after the "Record denied attempts" toggle, with help
  text. The PWA row uses the label `Allow sensitive writes` and the help text "On: a
  model session may run secret, identity and daemon-control tools. Off: each such call
  is refused and audited. Operator-run calls from the PWA, REST and CLI are
  unaffected." Show the on state when the server value is absent.
- **Locales (five bundles: English, German, Spanish, French, Japanese)**: three new
  keys in each bundle, `settings.sensitive_writes` (the section name, "Sensitive
  writes"), `settings.sensitive_writes.allow` (the toggle label) and
  `settings.sensitive_writes.allow_help` (the help text above). The PWA bundles are
  `internal/server/web/locales/{en,de,es,fr,ja}.json`; take the translated strings
  from there.
- **Audit list on the same card**: show the `class` value on each record when present,
  so an operator can tell a sensitive call from a configuration write. No new
  endpoint and no new screen.
- **Definitions link**: the card's "Definitions" link keeps pointing at
  `datawatch-definitions.md#self-config`; the sensitive writes glossary entry is at
  `datawatch-definitions.md#sensitive-writes`.
- Android/iPhone: mirror the toggle and the `class` display (or record an explicit
  exclusion with reason per the parity rule); translations of the three new keys come
  through the Compose Multiplatform pipeline.

## References

- datawatch how-to: `docs/howto/self-config-capability.md#sensitive-writes-a-separate-switch`
- datawatch glossary: `docs/datawatch-definitions.md#sensitive-writes`
- datawatch operations guide: `docs/operations.md#sensitive-writes-what-sessions-and-peers-may-not-do`
- MCP reference: `docs/mcp.md` (`self_config_audit`, the `class` field)
- OpenAPI: `docs/api/openapi.yaml` (`PUT /api/config`, `mcp.allow_sensitive_writes`)
- Tool classification: `internal/federation/mcp_tool_caps.go` (`sensitiveWriteTools`)
- Companion parity issue: `docs/plans/parity-issues/self-config-capability-app-parity-issue.md`

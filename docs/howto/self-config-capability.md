---
docs:
  index: true
  topics: [self-config, capability, group, allow_self_config, audit, hardening, bootstrap]
exec_steps:
  - tool: get_config
    description: Read the current configuration first (the mcp section carries allow_self_config, self_config_audit_path and self_config_audit_denied). Reading is never gated by the alias
    args: {section: "mcp"}
    read_only: true
  - tool: self_config_audit
    description: Read the self-config audit trail, newest first. One record per configuration-write attempt, allowed and (by default) refused, with the surface (mcp/rest/comm), the keys and the caller. Never gated by the alias, so the history is reviewable even while the gate is closed
    args: {limit: 20}
    read_only: true
  - tool: config_set
    description: Worked example, a state-changing call while the alias is on (the default). Raise the session cap; the attempt is recorded in the audit file. Any config-write tool through self-config requires the alias to be on
    args: {key: "session.max_sessions", value: "3"}
  - tool: config_set
    description: Worked example, a refusal that is recorded the same way. The alias itself is bootstrap-protected — config_set can never write mcp.allow_self_config in any state, so a self-configuring session can never re-open the gate that closed it. Re-enabling is an operator act (remove the false, restart)
    args: {key: "mcp.allow_self_config", value: "true"}
---

# Self-config — Operator Guide

**Self-config** is the ability of a session or a connected agent to change the
daemon's own non-secret configuration at runtime — `session.max_sessions`,
detection patterns, an LLM's backend, a burn-guard budget, and the like. The
daemon implements it as a **capability group** named `self-config`, and the
single operator-facing switch that turns the whole group on or off is
`mcp.allow_self_config`.

- The group is **enabled by default**. The default is the operator's answer
  for this installation: absent `mcp.allow_self_config` = enabled.
- An explicit `false` disables it. Re-enabling is removing the `false` and
  restarting — not a session writing `true` back.
- Every configuration write that goes through the self-config path (allowed
  or refused) is recorded in an **audit file**, viewable from every surface.
- Nothing here is secret-related: the group deliberately excludes secrets,
  federation control and daemon identity.

Related: [datawatch-definitions.md — Self-config](../datawatch-definitions.md#self-config),
[howto/federation-cbac.md](federation-cbac.md).

## The group, and what `mcp.allow_self_config` does

The `self-config` group is a **builtin** capability group. It cannot be edited
or deleted, and its membership is fixed in code:

| In the group | What it lets a credential do |
|---|---|
| `config:write` | Change daemon configuration (the `config_set` family of tools, `PUT /api/config`, comm `configure`) |
| `autonomous:write` | Change autonomous/Automaton tuning |
| `observers:write` | Add/update observer settings |
| `llms:write` | Add/update LLM registry entries and their settings |
| `usage:write` | Write the usage budget and burn-guard settings |

**Not in the group (by design).** The group grants *no* access to:

- anything under `secrets:` — read or write;
- anything under `federation:` — peers, groups, token mint;
- daemon identity control (sessions kill, compute control, plugin install,
  tailscale admin, pipeline/admin surfaces).

`mcp.allow_self_config` is an **alias for the group**, not a separate gate in
itself. One field, three effects:

1. **MCP gate** — while disabled, every MCP configuration-write tool
   (`config_set`, `detection_config_set`, `dns_channel_config_set`,
   `proxy_config_set`, `compute_pin_settings`) refuses with:
   `permission denied: self-modify is disabled (mcp.allow_self_config: false) — remove the explicit false (the default is enabled) and restart to enable`.
2. **Session-default capability grant** — the default capability set handed to
   every session is `session-default` **plus** `self-config` while the alias is
   on, and `session-default` alone while it is off. An explicit
   `session.capabilities` list wins over the default either way.
3. **Comm channel** — with the alias off, a session's scoped credential lacks
   `config:write`, so a comm `configure` for a configuration key is refused —
   `session token lacks capability: config:write` (a federation peer gets
   `federation peer lacks capability: config:write`) — and the refusal is
   audit-logged with surface `comm`.

### The three settings

| Key | Type | Default | What it does |
|---|---|---|---|
| `mcp.allow_self_config` | bool | enabled (absent) | The alias for the `self-config` group: on = sessions and agents may write their daemon's non-secret configuration; explicit `false` = they may not |
| `mcp.self_config_audit_path` | string | `<data dir>/audit/config-self-modify.jsonl` | Where the self-config audit file (JSONL) is written; one line per configuration-write attempt |
| `mcp.self_config_audit_denied` | bool | on | When on (default), refused attempts are recorded too — `allowed: false` plus a `reason`. Set `false` to record only the successful writes |

`get_config` (reading configuration) and the `self_config_audit` read tool are
never gated by the alias — a session that cannot write configuration can still
read the current values and the audit trail.

## Access methods (all three keys)

| Method | How |
|--------|-----|
| **YAML** | `mcp:` block in your config file (see [config-reference.yaml](../config-reference.yaml)) — restart takes the new default for newly spawned sessions |
| **CLI** | `datawatch config set mcp.allow_self_config false` / `datawatch config get mcp.allow_self_config`; audit: `datawatch audit self-config` |
| **Web UI** | Settings → General → **Self-config** card (Allow self-config toggle, audit path, record-denied toggle) |
| **REST API** | `PUT /api/config` with `{"mcp.allow_self_config": false}`; read back `GET /api/config` (boolean `allow_self_config`); audit: `GET /api/audit/self-config` (optionally `?limit=N`, newest first) |
| **Comm channel** | `configure mcp.allow_self_config=false`; refused for the session's credential once it lacks `config:write` (`session token lacks capability: config:write`), refusal audit-logged with surface `comm` |
| **MCP** | `config_set` with key `mcp.allow_self_config` — **refused by bootstrap protection**: `refused: mcp.allow_self_config is bootstrap-protected; edit the YAML directly to change it`. Reads and the audit view are unaffected (see below). Every *other* `config_set`-family write requires the alias to be on |

The bootstrap protection exists so a session that is self-configuring can
never flip its own gate back on. The operator's own surfaces (YAML, CLI, Web
UI, REST with the operator/diag credentials) can set the key in either
direction; a session's credential cannot.

## Hardening (the documented steps)

If your policy is "agents may not rewrite the daemon", do all of
these — each is a real switch:

1. **Close the gate.** In the config file:
   ```yaml
   mcp:
     allow_self_config: false
   ```
   then restart the daemon. From that point, every session credential lacks
   `config:write`, the MCP config-write tools refuse, and comm `configure`
   for configuration keys is refused and audit-logged.
2. **Remove the group from the session default, explicitly.** Even with the
   alias on, you can strip the group from the default grant:
   ```yaml
   session:
     capabilities: ["session-default"]
   ```
   An explicit `session.capabilities` list is always used as written — it can
   remove or narrow the default. Omit it (or leave it empty) and the default
   (with the group, while the alias is on) applies.
3. **Define a narrower group instead.** Create a custom group with exactly
   the surfaces you want, and grant that instead of `self-config`:
   ```sh
   datawatch federation group add config-autonomous --caps config:write,autonomous:write
   datawatch federation group get config-autonomous
   datawatch federation group update config-autonomous --caps config:write
   ```
   then
   ```yaml
   session:
     capabilities: ["session-default", "config-autonomous"]
   ```
   A per-daemon narrowing works because each daemon's sessions carry the caps
   that daemon hands out at spawn; a federation peer is narrower still — its
   capability set is whatever it registered with (`federation peer
   add --capabilities …`), independent of this daemon's session default.
4. **Read the audit after the fact, from anywhere:**
   - MCP tool `self_config_audit` (read-only; `limit` 1–1000, default 100) —
     the tool itself is *not* gated by the alias, so you can review even
     while it is closed;
   - CLI: `datawatch audit self-config` (newest first);
   - REST: `GET /api/audit/self-config`;
   - Web UI: the **Self-config** card on the Settings screen;
   - the file itself: `mcp.self_config_audit_path` (JSONL, default
     `<data dir>/audit/config-self-modify.jsonl`).
5. **Grant one session (or one daemon's sessions) the group** when you want
   an exception, without opening the door for everyone:
   ```yaml
   session:
     capabilities: ["session-default", "self-config"]
   ```
   or with your custom group from step 3. Everything you do not list is not
   granted.

## Read paths that are never gated

Two read paths stay open even while the alias is off — deliberately, so an
operator (or a session with `config:read`) can always see the state and the
history:

- `get_config` — current configuration values, all surfaces;
- `self_config_audit` — the audit file view (MCP tool, `limit` 1–1000, default
  100, newest first).

## The audit log, in detail

One record per configuration-write attempt, regardless of surface — exactly
the surfaces a session or agent actually writes from (**mcp**, **rest**,
**comm**):

```json
{"time":"2026-10-29T02:14:03Z","surface":"mcp","tool":"config_set","keys":["session.max_sessions"],"values":{"session.max_sessions":3},"caller":"session:opencode-a3f2","allowed":true}
{"time":"2026-10-29T02:13:41Z","surface":"mcp","tool":"config_set","caller":"session:goose-b7c1","allowed":false,"reason":"refused: mcp.allow_self_config is bootstrap-protected; edit the YAML directly to change it"}
```

| Field | Meaning |
|---|---|
| `time` | UTC timestamp of the attempt |
| `surface` | `mcp` (an MCP config-write tool), `rest` (`PUT /api/config`), or `comm` (the chat-channel `configure` verb) |
| `tool` | the MCP tool name, `PUT /api/config` for REST, `configure` for comm |
| `keys` / `values` | what was attempted; values are redacted for secret-looking keys |
| `caller` | the acting credential — a session (`session:<id>`) or a federation peer (`peer:<name>`); the operator's own admin writes are not recorded as session writes |
| `allowed` | `true` / `false` |
| `reason` | refusal reason when `allowed` is `false` |

Allowed attempts are always recorded. Refused attempts are recorded unless
you set `mcp.self_config_audit_denied: false`. The file is append-only JSONL
and safe to tail or ship to an archive; the read endpoints are a view over the
same file, newest first.

### Monitoring and metrics

This capability adds no SystemStats field, no Monitor card stat and no
Prometheus metric. The audit log above is the monitoring surface:

- **SystemStats field:** not applicable. SystemStats carries resource and
  capacity gauges; config writes are rare operator-scale events, not load.
- **Monitor card stats:** not applicable. The Settings > Self-config card
  already shows the audit view.
- **Prometheus metric:** not applicable. A counter would only restate the
  audit log. To alert on refused writes, tail or ship the JSONL file and
  match `"allowed":false`.

## Walking through: disable, verify, re-enable

**Disable.**
```sh
# 1. Close the gate (operator surface)
datawatch config set mcp.allow_self_config false
# 2. Restart the daemon so every newly spawned session drops config:write
```
**Verify a refusal is gated and logged.** Ask a session to change anything
that goes through the self-config path, for example over MCP:

```
> config_set session.max_sessions=5
permission denied: self-modify is disabled (mcp.allow_self_config: false) —
remove the explicit false (the default is enabled) and restart to enable

> config_set mcp.allow_self_config=true   (refused by the gate first — the key is also bootstrap-protected, so config_set can never write it even with the gate on)
permission denied: self-modify is disabled (mcp.allow_self_config: false) —
remove the explicit false (the default is enabled) and restart to enable
```

and `datawatch audit self-config` shows both refused attempts
(`allowed: false`, the refusal reason) — because recording refused attempts
is on by default.
**Re-enable.**
```sh
datawatch config set mcp.allow_self_config true   # same as removing the false
# Restart the daemon again
```
Nothing else changed: the key was an alias all along, so "removing the
false" and "setting true" are the same state.

## Migration notes (from the old single gate)

Before the group model, `mcp.allow_self_config` was the only knob: a single
global allow/deny for *all* configuration writes by sessions and agents, with
no per-session scoping. Three things changed for operators who had it set:

| Before | Now |
|---|---|
| `mcp.allow_self_config: false` (the only way to harden) | Unchanged in meaning: the alias for the `self-config` group. Same key, same YAML, same default (absent = enabled). No config migration; nothing to rewrite |
| No per-session scoping | `session.capabilities` (explicit list wins) and custom groups via `datawatch federation group add` let you disable it globally and re-grant a narrowed set to selected daemons/sessions |
| One implicit gate | The same gate now *is* the capability grant: the session's scoped credential carries `config:write` from the group, and refusals everywhere carry the same reason text |

Existing configs that never set the key keep the enabled default; configs that
set it to `false` keep the hardened behaviour. Only the *new* surface is the
ability to scope and audit more precisely.

## Settings summary

| Key | Type | Default | Settable by a session? |
|---|---|---|---|
| `mcp.allow_self_config` | bool | enabled | **No — bootstrap-protected.** YAML/CLI/Web UI/REST (operator) only |
| `mcp.self_config_audit_path` | string | `<data dir>/audit/config-self-modify.jsonl` | Only while the alias is on (it is a config-write like any other) |
| `mcp.self_config_audit_denied` | bool | on | Same as above |

## See also

- [datawatch-definitions.md — Self-config](../datawatch-definitions.md#self-config) — the glossary entry
- [howto/federation-cbac.md](federation-cbac.md) — the capability model, builtin groups, per-peer capability sets
- [howto/mcp-tools.md](mcp-tools.md) — `config_set` and the other config-write tools, `get_config`, `self_config_audit`
- [config-reference.yaml](../config-reference.yaml) — the `mcp:` block keys
- `CHANGELOG.md` — the release record for the group, the alias, the single audit point and the multi-surface exposure

---
docs:
  index: true
  topics: [imap-mcp, plugins, service, attach, managed]
exec_params:
  - {name: plugin, required: false, default: "imap-mcp", description: "Plugin name as the daemon scans it"}
exec_steps:
  - tool: plugins_list
    description: List the discovered plugins and their enabled state
    args: {}
    read_only: true
  - tool: plugin_enable
    description: Enable the service plugin in the daemon
    args: {name: "{{params.plugin}}"}
    read_only: false
  - tool: plugin_service_status
    description: Read state, mode, address, version, uptime and last health check
    args: {name: "{{params.plugin}}"}
    read_only: true
  - tool: plugin_service_start
    description: Start the supervised child (managed mode)
    args: {name: "{{params.plugin}}"}
    read_only: false
  - tool: plugin_service_stop
    description: Stop the supervised child
    args: {name: "{{params.plugin}}"}
    read_only: false
  - tool: plugin_widget_list
    description: List the plugin's widgets with their latest values
    args: {name: "{{params.plugin}}"}
    read_only: true
  - tool: plugin_token_list
    description: Show which vault secrets and service tokens the plugin references (names only)
    args: {name: "{{params.plugin}}"}
    read_only: true
---
# How to: run imap-mcp as a watched service

This walks an imap-mcp deployment through datawatch's service surface, in the
order you will do it. imap-mcp runs standalone the whole time — datawatch never
requires a special mode inside imap-mcp; every choice in this document (attach
vs managed, thresholds) is a datawatch-side decision.

Reference for the concepts: [docs/plugins.md](../plugins.md).

## Before you start

- A working imap-mcp build and a host for it (this box or another on the LAN).
- The datawatch daemon you will attach to, with its vault and PWA reachable.
- The imap-mcp manifest (below) placed where your daemon scans plugins, named
  e.g. `imap-mcp`.

## 1. Run imap-mcp standalone (zero datawatch)

Do this first — the service is the product and must work without the host:

```
imap-mcp --listen 127.0.0.1:9700 --token-file /run/imap-mcp/token
```

Confirm:

```
curl -sS http://127.0.0.1:9700/healthz -H "Authorization: Bearer $(cat /run/imap-mcp/token)"
# → { "ok": true, "version": "1.4.2" }
```

Now it is a long-running service with a health path and a bearer token. Under
systemd, make that command a unit; datawatch will attach to the same address.

## 2. Keep the bearer in the vault (done yourself)

The bearer imap-mcp accepts when datawatch talks to it must live in the vault,
not in the manifest — a literal credential in the manifest is refused at load.
Generate a value, store it under a name, and point the manifest at the name:

```
datawatch secrets set imap-mcp-token <generated-value> --desc "imap-mcp bearer"
```

Write that same `<generated-value>` (e.g. the output of `openssl rand -hex 32`)
into imap-mcp's config as its bearer. datawatch resolves
`${secret:imap-mcp-token}` from the vault on every probe and widget pull, so
the manifest never changes when the value does — and the daemon never needs to
tell imap-mcp anything; you tell both sides the same value.

Second credential, only if imap-mcp calls *into* the daemon (resolving a
scoped secret for itself, pushing widget values): a **service token** for
imap-mcp. Mint it yourself, directly in a real terminal — not through an
agent, and not through a shell-passthrough that lands the output in a chat
transcript — because that printout is the only place the token is ever visible:

```
datawatch secrets mint-service-token imap-mcp --desc "imap-mcp to daemon"
```

Write the printed token into imap-mcp's config. `list-service-tokens` shows the
name only; `revoke-service-token imap-mcp` drops it.

## 3. Attach datawatch (attach_only)

The manifest (attach_only is enough for step 1's standalone setup):

```yaml
name: imap-mcp
kind: service
version: 1.4.2
connection:
  base_url: http://127.0.0.1:9700
  health_path: /healthz
  auth:
    type: bearer
    token: "${secret:imap-mcp-token}"
lifecycle:
  supported_modes: [attach_only]
widgets:
  - id: queue_depth
    surface: dashboard
    kind: stat_tile
    data_mode: pull
    pull_path: /api/queue/depth
    interval_seconds: 30
```

Then in the daemon:

- enable the plugin (PWA → Plugins, or the equivalent REST/CLI action)
- ensure the mode is attach_only: leave `plugins.managed` without `imap-mcp`,
  and optionally pin `plugins.services.per_service.imap-mcp.lifecycle_mode: attach_only`

What datawatch now does, without touching imap-mcp: probes `/healthz` on the
configured interval (default 60 s), pulls the `queue_depth` widget every 30 s,
and watches the four conditions (down, token rejected, backlog growing, node
unreachable). Read the card: **PWA → Settings → Plugins → imap-mcp** shows
state (`up`/`down`), mode (`attach-only`), address, version, uptime, last
check, last error, the next action, and — when any condition is raised — the
open-events count as a link into the Events feed.

## 4. (Optional) Let datawatch own the process (managed)

Only if you want the daemon to start and restart imap-mcp itself:

1. The manifest must **declare** managed: `supported_modes: [attach_only, managed]`
   plus `entry`, `args`, `restart_policy`.
2. You then **opt in**: add `imap-mcp` to `plugins.managed` (or set
   `per_service.imap-mcp.lifecycle_mode: managed`). The manifest is the ceiling
   — without it declaring `managed`, the opt-in does not apply.
3. Start it from any surface:

   ```
   POST /api/plugins/imap-mcp/enable   # body: {"managed": true}
   ```

   or simply `plugin_service_start` (MCP), or the PWA control.

4. Watch it come up: the card's state walks through `starting` → `healthy`.
   If it keeps crashing, you will see `crashed` → `restarting` (per the
   policy) up to the restart budget, then `failed` — at which point datawatch
   has given up starting it and is reporting instead of hot-looping.

Leaving managed mode: set the mode back to `attach_only`, stop the supervised
child (`plugin_service_stop`), and start imap-mcp again under your own
systemd unit. Both directions are reflected in the probes without a daemon
restart.

## 5. Rotate a token (each family has its own move)

**Connection bearer (the vault secret `imap-mcp-token`)** — the one datawatch
presents to imap-mcp:

1. Generate a new value.
2. Store it under the same name: PWA → Settings → Plugins → imap-mcp →
   **Tokens** → **Rotate** for `imap-mcp-token` (paste the new value), or
   `datawatch secrets set imap-mcp-token <new-value>` in your terminal.
3. Point imap-mcp's config at the new value and restart it.
4. datawatch picks the new value up on its next probe; if a
   `token_rejected` condition was raised in the meantime, it clears
   automatically with the “token accepted again” alert once both sides agree.

**Service token (imap-mcp → daemon)**: `datawatch secrets mint-service-token
imap-mcp` again in your terminal (prints once), update imap-mcp's config —
the old token stops verifying. `revoke-service-token imap-mcp` drops the
credential entirely.

**Widget push token**: `POST /api/plugins/imap-mcp/widgets/token` (returns
the new push token once); update imap-mcp's push config — the old push token
stops verifying.

## 6. Reading the status card (what each field means)

| Field | Meaning / source |
|---|---|
| **state** | attach_only: the last health probe (`up` / `down`). managed: the supervisor state (`healthy`, `unhealthy`, `restarting`, `failed`, …) |
| **mode** | `attach_only` or `managed` — the effective choice after manifest ceiling + your opt-in |
| **address / version** | from `connection.base_url`; version from the health response when it carries one |
| **uptime** | managed: the child's run so far. attach_only: how long the probe has been seeing it up |
| **last probe** | last health check time + up/down |
| **last error** | last failure detail (managed: exit code/health failure; attach: probe error) |
| **open events** | how many of the four conditions are currently raised |
| **tokens** | names of referenced vault secrets + mint/revoke for push tokens — **never values** |

## 7. Act on each event

| You see | What it means (from the daemon's own words) | Do |
|---|---|---|
| **Plugin imap-mcp is down** — "the health check failed N times in a row" | `service_down` (attach_only: it stopped answering; managed: it is not coming back within policy) | Confirm imap-mcp is running and reachable from *this* host (the daemon's host, which is where the probes originate). In managed mode check the card's **state**, **pid**, **restarts**, **last_exit**, **last_error**. Then wait for the "Plugin imap-mcp is back up" cleared alert. |
| **Plugin imap-mcp token rejected** — "answered with HTTP 401/403" | `token_rejected` — imap-mcp no longer accepts the bearer (or a stale token after rotation) | In the card's **Tokens** panel re-mint/rotate, update imap-mcp's token, expect the "token accepted again" cleared alert. |
| **Plugin imap-mcp backlog is growing** — "N pulls in a row, ≥ M per pull, now P pending" | `enrichment_backlog_growing` — imap-mcp's queue is rising faster than it drains | Open imap-mcp in Settings, read its processing rate and queue, or raise its worker count. Cleared on the first flat/falling pull ("backlog stopped rising"). |
| **Plugin imap-mcp model node unreachable** | the Ollama node that backs a model imap-mcp calls stopped answering | Confirm that node/box is up and the model is loaded. Watch the "model node is reachable again" cleared alert. |

## 8. Verify the loop (end-to-end)

- **Kill imap-mcp** (attach_only): within one probe interval the card flips to
  `down`, the `service_down` alert fires once, its *Next* tells you to confirm
  the service — restart it, the cleared alert follows.
- **Present a wrong bearer** once: `token_rejected` raises, then clears on the
  first good answer.
- **Let the queue grow** past the streak in Settings: `enrichment_backlog_growing`;
  drain it, the cleared alert follows.
- **Stop an Ollama node** that backs a model: `model_node_unreachable`; bring it
  back and the cleared alert follows.

## Troubleshooting

| Symptom | First thing to check |
|---|---|
| attach card says `down` but `curl` from the same host works | The probe runs from **datawatch's host**; confirm base_url + health_path are reachable from *there*, and that the bearer the vault resolves is the one imap-mcp accepts |
| `token_rejected` immediately after a rotation | Both sides must move: vault (daemon) and imap-mcp's config. Until both agree, the probes read 401/403 and the condition stays raised |
| managed flips to `failed` after repeated crashes | The restart budget is spent — by design. Read **last_exit** / **last_error** on the card; fix the cause, then `plugin_service_start` again. Do not raise the budget to paper over a boot-loop cause |
| a widget tile shows stale data | Pull mode: the service must answer `pull_path` (and the pull uses the service token); push mode: check the push token is still valid after the last rotate |
| `pull_path` rejected at load | For service plugins it must start with `/`; it must be empty for push-mode widgets |

> **Troubleshooting:** if the daemon starts with imap-mcp installed but no plugin
> token source wired, it no longer crashes; the model-use card is empty and
> `GET /api/plugins/imap-mcp/models` answers 503 until the source is wired. See
> [operations.md](../operations.md#plugin-services-lifecycle-semantics).
>
> **Operator review:** after any start with imap-mcp installed, check `daemon_logs` for the skipped model-node task and confirm the plugin token source is wired; until then the model-use card stays empty.

## Related

- [docs/plugins.md](../plugins.md) — the concepts, thresholds and surfaces in
  full
- [docs/operations.md](../operations.md) — deployment and lifecycle semantics in
  the operations manual
- [docs/mcp.md](../mcp.md) — the MCP tools that mirror this surface
  (`plugin_service_*`, `plugin_widget_*`, `plugin_token_*`)
- [docs/howto/](README.md) — the how-to index

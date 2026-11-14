# Plugin services

A **service plugin** is an ordinary long-running process *you* own — its binary,
its ports, its uptime — that also describes itself to datawatch in a small
manifest. Typical example: an imap-mcp server you run as your own systemd
service.

datawatch is a *host*, not a requirement. The service runs standalone; datawatch
never requires a special mode, library or handshake inside the service. What
datawatch adds is: it knows where the service is, it probes it, it holds the
secrets the service needs, it shows the service's tiles and token usage in the
PWA, and it raises events when something goes wrong. The service itself does
not change because datawatch is watching it.

Every fact in this document is read from the daemon you run — there is no
separate "plugin server" to reach.

## Kinds of plugin

Manifests have two kinds:

| Kind | What it is | How datawatch talks to it |
|---|---|---|
| `automation` | A hook library the daemon calls at defined moments (e.g. after a session outputs) and, optionally, polls for widget data | The daemon invokes hooks and/or polls `status_poll`; the plugin has no lifecycle of its own |
| `service` | An independent long-running process with an HTTP endpoint | The daemon attaches to a running instance (**attach_only**) or starts and supervises one itself (**managed**) |

This document covers `service`. (Both kinds may declare widgets — the widget
section applies to either.)

## Attach_only vs managed

These are two different answers to one question: **who owns the process?**

| | **attach_only** (the default stance) | **managed** |
|---|---|---|
| Who starts it | You — systemd, Docker, a shell, whatever you already use | datawatch |
| Who can stop it | Only you | You, and datawatch (per its stop policy) |
| What datawatch observes | Health probes against the manifest's `health_path` | Health probes, child exit codes, restart counts, last error |
| If it dies | datawatch raises a `service_down` event and waits for you | datawatch restarts it per the manifest's `restart_policy`, bounded by its restart budget |
| Requires from the manifest | `attach_only` in `supported_modes` (or no `lifecycle` block at all) and a probeable `connection` | `managed` in `supported_modes`, a `connection`, and a `lifecycle.entry` (the command to run) |

Neither mode imposes anything on the process. In attach_only the service does
not know datawatch is polling its health path. In managed, datawatch spawns the
very binary it would have spawned itself had you started it — the service has
no notion of "being managed".

A manifest may declare both modes; then *you* choose per deployment (see below).
A manifest may declare neither, which means the plugin can be attached to but
never spawned.

## The manifest

Service plugins carry a manifest (`manifest.yaml` / `manifest.json`) that
declares:

- `name`, `kind: service`, `version`
- **`connection`** — where to reach it and how to authenticate:

  | Field | Meaning |
  |---|---|
  | `base_url` | Absolute base URL of the service (e.g. `http://127.0.0.1:9700`) |
  | `health_path` | Path datawatch GETs to check liveness (a 2xx = up) |
  | `auth.type` | `bearer` when the service expects an `Authorization` header |
  | `auth.token` | The literal token, or a vault reference `${secret:<name>}` resolved from the daemon's vault |

- **`lifecycle`** — only for managed mode:

  | Field | Meaning |
  |---|---|
  | `supported_modes` | List from `attach_only`, `managed` (the ceiling — the operator can never force a mode the manifest did not declare) |
  | `entry` | The executable to run (absolute path, or relative to the plugin directory) |
  | `args` | Arguments for `entry` |
  | `restart_policy` | `on-failure` (default), `always`, or `never` |
  | `stop_timeout_ms` | How long to wait for a graceful stop before killing (default 10 s, plus a fixed kill margin) |

- **`widgets`** — optional tiles in the PWA (see below)

Example (attach_only):

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
  supported_modes: [attach_only, managed]
  entry: /usr/local/bin/imap-mcp
  args: ["--listen", "127.0.0.1:9700"]
  restart_policy: on-failure
widgets:
  - id: queue_depth
    surface: dashboard
    kind: stat_tile
    data_mode: pull
    pull_path: /api/queue/depth
    interval_seconds: 30
```

## Choosing the mode per deployment

The operator's choice lives in the daemon config, not the manifest:

- **`plugins.managed`** (list of plugin names): names datawatch is allowed to
  start/stop. A name only takes effect when the manifest also declares
  `managed` — the manifest is the ceiling, this list is the opt-in. Absent =
  nothing is ever managed.
- **`plugins.services.per_service.<name>.lifecycle_mode`**: names the mode
  directly (`attach_only` or `managed`); honoured only when the manifest
  declares it. Absent = the plugin's only supported mode, or `attach_only` when
  it supports both.

Flipping between modes at runtime (enable/disable, mode change) is reflected in
the probes and supervisor without a daemon restart.

## How the daemon behaves in each mode

### attach_only

- Every **`plugins.services.health_interval_seconds`** (default **60** s,
  minimum **5** s — values below the minimum are refused at load time), datawatch
  GETs `base_url + health_path`. A 2xx is "up"; anything else (or no answer)
  is a failed check.
- datawatch never sends the service a signal, never kills it, never restarts it.
  When the checks keep failing it raises events (below) and otherwise stays quiet.

### managed

datawatch spawns the `lifecycle.entry` child with its `args`, injects the
resolved connection auth, and supervises it:

- **Health**: probes at the same interval. A service that is alive but fails
  its health path repeatedly is treated as unhealthy; one that exits is a
  failure.
- **Restart policy** (manifest `restart_policy`):

  | Policy | Restart after a crash? | Clean exit (0) means |
  |---|---|---|
  | `on-failure` (default) | yes | the operator stopped it on purpose → stopped, no restart |
  | `always` | yes, even after a clean exit | an anomaly → restart |
  | `never` | no | stopped (0) or crashed (non-zero); datawatch only reports |

- **Restart budget**: a run that stays healthy for the configured *stable*
  window resets the failure counter, so one long-lived healthy run breaks the
  streak. Repeated crashes without reaching healthy are counted; when the
  budget is spent the supervisor gives up (state `failed`) and raises an event
  rather than hot-looping. There is a per-restart back-off that grows with each
  consecutive failure.
- **Stop**: a graceful stop (SIGTERM) with `stop_timeout_ms` (default 10 s)
  plus a fixed kill margin; a stop request during startup is honoured and the
  supervision loop ends.
- **States you may see**: `stopped`, `starting`, `healthy`, `unhealthy`,
  `crashed`, `restarting`, `stopping`, `failed`.

## Widgets: kinds and data modes

A widget is one tile in the PWA (dashboard or a session view) whose value the
service produces. Both plugin kinds may declare them; for `service` plugins the
two data modes are:

| Mode | Direction | Mechanism |
|---|---|---|
| **push** | service → daemon | The service POSTs the value to `/api/plugins/<name>/widgets/<widget_id>` using its *widget push token* (a per-plugin credential datawatch mints and you hand to the service; it is separate from, and never returned by, the service token above). The widget id `token` is reserved and may not be used as a widget id. |
| **pull** | daemon → service | datawatch GETs `base_url + pull_path` every `interval_seconds` (default **30** s) and stores the JSON body as the widget's latest value. Pull requires a `pull_path` starting with `/`. |

Rules that apply to both:

- A widget **kind** is one of `stat_tile`, `table`, `list`, `sparkline`,
  `status_badge`, `markdown` — this is purely how the PWA renders the value;
  the value itself is arbitrary JSON.
- A widget's **surface** is `dashboard` or `session`.
- The daemon stores **the latest value per widget** (plus, for pull mode, the
  last pull error if the service failed to answer while a value was on file).
  There is no history buffer — a tile shows the current value.
- Push and pull are mutually exclusive per widget: a pull-mode widget rejects a
  push, and a push-mode widget must not set `pull_path`.
- Payloads are capped (64 KiB); oversized values are rejected, prior values
  kept.

Read values (operator or any authenticated surface):

```
GET /api/plugins/<name>/widgets
GET /api/plugins/<name>/widgets/<widget_id>
```

## Tokens and rotation

Three distinct credential families live around one service plugin — do not
confuse them:

| | Connection vault secret (e.g. `imap-mcp-token`) | Service token (e.g. `imap-mcp`) | Widget push token |
|---|---|---|---|
| Who holds it | imap-mcp (accepts it), the vault (stores it) | imap-mcp (presents it *to the daemon*) | imap-mcp (presents it when pushing a widget) |
| What it authorises | Nothing on its own — it is the bearer the daemon resolves and **sends** when it probes or pulls | imap-mcp calling into the daemon, e.g. `GET /api/external/secrets/<name>` for a scoped secret; the daemon's other service-token-verified endpoints | `POST /api/plugins/<name>/widgets/<widget_id>` for push-mode widgets |
| Stored as | A named vault secret | A token record (0600 file store, survives restarts) | A bcrypt hash (the plaintext exists once, at mint) |
| Minted by | You: `datawatch secrets set <name> <value>` | You, in a real terminal: `datawatch secrets mint-service-token <name>` (printed **once**) | `POST /api/plugins/<name>/widgets/token` (operator auth; returned **once**) |
| Rotated by | Storing a new value under the name (PWA Tokens panel → Rotate, or `POST /api/plugins/<name>/tokens/<name>/rotate`), then pointing the service at the new value | Minting again for the same name (the old token stops verifying); `datawatch secrets revoke-service-token <name>` drops it entirely | Calling the mint endpoint again (the old token stops verifying) |

Rules that cover all three:

- The manifest's `connection.auth.token` must be a `${secret:<name>}`
  reference — a literal credential in the manifest is refused at load. The
  daemon resolves the reference against the vault when it probes or pulls, so
  a rotated value takes effect on the next probe without re-editing the
  manifest.
- Names are shown everywhere (PWA card's Tokens panel, `GET
  `/api/plugins/<name>/tokens``, `plugin_token_list`). **Values are never
  echoed** by REST, MCP or the PWA; the only time any of these passwords is
  visible is the single printout at mint, in your terminal.

### Minting a service token — do it yourself, in a real terminal

```
datawatch secrets mint-service-token imap-mcp --desc "imap-mcp to datawatch"
datawatch secrets list-service-tokens       # names only
datawatch secrets revoke-service-token imap-mcp
```

Run `mint-service-token` directly in a real terminal — not through an agent, and
not via a shell-passthrough that lands the output in a chat transcript —
because the printed line is the only place the password ever exists.
Write it into imap-mcp's config where it uses it when talking to the daemon;
the daemon keeps the store side.

### Reading token usage and the secret refs

- `GET /api/plugins/<name>/tokens` — the names of the vault secrets the
  manifest references, and the names of the service tokens minted for the
  plugin (**names only; never values**).
- `GET /api/plugins/<name>/models` — which models the service calls, on which
  Ollama node, the memory each holds, and the parameterised findings
  (overloaded node, small-model-large-context, etc.) — the input to the
  events below and to the PWA's model-use card.

  If the daemon has no model-use source wired (for example a service-kind
  plugin is installed but no plugin token source is configured), the daemon no
  longer crashes at startup: the model-node event task simply does not run, and
  this route answers `503 plugins not available` instead of panicking. Fix it
  by wiring the plugin token source / restoring the plugin manager, then
  reload plugins.

### Surfaces

| Surface | How |
|---|---|
| PWA | Settings → **Plugins**: per-service card (state, mode, address, version, uptime, last check/last error, next action, open-events link) + a **Tokens** panel (the vault secret names the manifest references, each with a Rotate action that takes the new value; the service token names with their age — never values) |
| CLI | `datawatch secrets set/get/list/delete <name>` (vault) · `datawatch secrets mint-service-token / list-service-tokens / revoke-service-token <name>` (service tokens) |
| REST | `GET/POST/DELETE /api/plugins/<name>/tokens[/<secret>[/rotate]]`; `POST /api/plugins/<name>/widgets/token`; `GET /api/external/secrets/<name>` (service→daemon) |
| MCP | `plugin_service_status`, `plugin_widget_list`, `plugin_widget_get`, `plugin_token_list`, `plugin_service_start/stop/restart`, `plugin_token_rotate` |

## Service events and what to do about each

datawatch watches four conditions and raises an event **once per
transition** (never once per probe), with a matching *cleared* event when the
condition goes away. Each raise is an operator-visible alert with a concrete
"Next:" action; each is also written to the audit trail.

| Condition | Raised when | Typical "Next" the alert gives you | Cleared when |
|---|---|---|---|
| **`service_down`** | the health path fails the configurable streak of consecutive probes | Confirm the service is running and reachable from this host, then wait for the recovery alert | The next probe passes |
| **`token_rejected`** | a probe or a widget pull answers 401/403 | Check the service token in Settings; re-mint/rotate it and point the service at the new value | A probe/pull answers 2xx/3xx |
| **`enrichment_backlog_growing`** | the service's reported backlog rises N consecutive pulls by at least the minimum rise (streak and floor both configurable) | Open the service in Settings, look at its processing rate/queue, or raise its worker count | A pull shows the backlog flat or falling |
| **`model_node_unreachable`** | an Ollama node the service depends on stops answering its reachability check | Confirm that node is running and the model is loaded there | The node answers again |

Reading them:

- **Alerts**: the daemon's own alert feed — each carry plain-language body with the plugin name and the next action.
- **Audit**: one entry per raise and per clear, with the plugin name and the
  condition's figures (streak, floor, pending count, status, source).
- **PWA**: the plugin service card's *open events* count; the model-use card
  shows the reachability findings.
- **REST**: `GET /api/plugins/<name>` → `open_events` (count of currently
  open conditions).
- **MCP**: `plugin_service_status` (per-plugin state + open events),
  `plugin_token_list`.

## Thresholds (the settings that matter)

All under `plugins.services` in the daemon config (daemon-wide settings). The
per-plugin override exists for the lifecycle mode only
(`per_service.<name>.lifecycle_mode`), not for the numeric thresholds.
(Config surface: YAML, `PUT /api/config`, the PWA Settings fields, the CLI
config command.)

| Key | Default | Meaning |
|---|---|---|
| `health_interval_seconds` | 60 | Attach probe cadence (minimum 5 — below that is refused at load) |
| `backlog_growth_pulls` | 3 | Consecutive backlog rises required before "growing" |
| `node_load_ratio` | 1.5× | Node considered overloaded when it holds `> ratio ×` the model memory of another node that can serve the same model |
| `small_model_max_params_billions` | 8.0 | Parameter ceiling (billion) for the "small model" findings |
| `small_model_context_tokens` | 8192 | Context ceiling (tokens) for the small-model-large-context finding |
| `per_service.<name>.lifecycle_mode` | (plugin's only mode; attach_only if both) | attach_only vs managed, per plugin |
| `plugins.managed` | (empty) | Names datawatch may start/stop |

The *minimum rise per pull* that counts as "growth" for
`enrichment_backlog_growing` is a separate knob read alongside the streak; the
effective pair is reported by the daemon for diagnostics.

## What datawatch never does (with a service plugin)

- Never requires the service to know datawatch exists — attach_only is
  observation; managed is spawn, nothing more special.
- Never echoes a token or vault secret value on any read surface.
- Never restarts a service the manifest did not declare `managed` for, and
  never force-manages a manifest that only declared attach_only.
- Never kills a service on the operator's behalf in attach_only mode — it
  reports, and *you* stop it.
- Never keeps a widget history — only the latest value per widget.
- Never mints a service token on the operator's behalf: that is your action,
  in your terminal.

See [howto/imap-mcp-service.md](howto/imap-mcp-service.md) for the end-to-end
imap-mcp walkthrough (standalone run, attach, manage, mint, rotate, act on
events).

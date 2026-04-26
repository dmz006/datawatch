# Device push registry — operator reference

Per-device push registry that the daemon uses to fan out
notifications (state changes, alerts, completion events) to mobile
devices. Each registered device has an alias, a transport (APNS /
FCM / ntfy / etc.), and an opt-in topic list.

## Surface

### REST

| Endpoint | Purpose |
|---|---|
| `GET /api/devices` | List registered devices. |
| `POST /api/devices` | Register a new device `{alias, transport, token, topics[]}`. |
| `DELETE /api/devices/{alias}` | De-register; in-flight pushes complete. |
| `POST /api/devices/{alias}/test` | Send a test notification. |

### MCP

`devices_list`, `devices_register`, `devices_remove`,
`devices_test` — see [`docs/api-mcp-mapping.md`](../api-mcp-mapping.md).

### CLI

```bash
datawatch device list
datawatch device add  --alias my-phone --transport ntfy --topic alerts
datawatch device test my-phone
datawatch device rm   my-phone
```

### Chat / messaging

Devices are registered out-of-band (CLI / web / MCP); chat channels
*receive* notifications routed via the registry but don't manage it.

## Storage

Device records live at `<data_dir>/devices.json` (encrypted at rest
when `secrets.encryption_enabled` is on).

## See also

- [`docs/architecture-overview.md`](../architecture-overview.md) — where the registry sits in the broader push-notification flow
- [`docs/messaging-backends.md`](../messaging-backends.md) — `ntfy` backend specifics

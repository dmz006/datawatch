# Channel Tab Testing Guide

How to manually test MCP channel communication and verify the channel tab displays
bidirectional messages.

## Prerequisites

- datawatch running with `session.channel_enabled: true`
- A claude-code session that has passed the startup prompts (channel_ready = true)
- Web UI open to the session detail view

## Step 1: Verify Channel Is Connected

```bash
# Check session has channel_ready and channel_port
curl -s http://localhost:8080/api/sessions | python3 -c "
import json,sys
for s in json.load(sys.stdin):
    if s.get('channel_ready'):
        print(f'{s[\"id\"]} port={s.get(\"channel_port\",0)} ready={s[\"channel_ready\"]}')"
```

Expected: at least one session with `ready=True` and a non-zero port.

## Step 2: Send a Test Message via Channel

Use the channel port from Step 1 to send a message directly to Claude:

```bash
# Replace PORT with the actual channel_port from Step 1
# Replace SESSION_ID with the full session ID (hostname-xxxx)
curl -s -X POST "http://127.0.0.1:PORT/send" \
  -H 'Content-Type: application/json' \
  -d '{"text":"Hello from datawatch test","source":"datawatch","session_id":"SESSION_ID"}'
```

Expected: `{"ok":true}` — the message was delivered to Claude via MCP notification.

## Step 3: Send via the API (recommended)

```bash
# This routes through datawatch which also broadcasts to the channel tab
curl -s -X POST http://localhost:8080/api/channel/send \
  -H 'Content-Type: application/json' \
  -d '{"text":"Test message from API","session_id":"SESSION_ID"}'
```

Expected: `{"status":"ok"}` — the message is sent AND broadcast to the web UI channel tab.

## Step 4: Check Channel Tab in Web UI

1. Open the session in the web UI
2. Click the **Channel** tab (next to **Tmux**)
3. You should see:
   - `-> Test message from API` (blue, outgoing) — from Step 3
   - `<- [Claude's response]` (amber, incoming) — if Claude replied via the `reply` tool

## Troubleshooting

### Channel tab is empty

**Cause:** The channel tab only shows messages from the current browser session.
Historical messages are not persisted — they exist only in the browser's JavaScript state.

**Fix:** Keep the web UI open, then send a message via Step 3. The outgoing message
should appear immediately in the channel tab.

### "channel server unreachable" error

**Cause:** The `/api/channel/send` endpoint can't reach the per-session channel server.

**Check:**
1. Verify `channel_port` is non-zero: `curl -s http://localhost:8080/api/sessions | python3 -c "import json,sys; [print(s['id'], s.get('channel_port',0)) for s in json.load(sys.stdin) if s.get('channel_ready')]"`
2. If port is 0, the channel server didn't report its actual port. Restart the session.
3. Check if the node process is running: `ss -tlnp | grep node`

### Claude doesn't respond via channel

Claude only sends messages through the channel when it uses the `reply` MCP tool.
During normal operation, Claude processes tasks silently — it doesn't proactively
send status updates via the channel. The channel is primarily used for:

- **Task delivery** (outgoing): datawatch sends the task to Claude on channel_ready
- **Reply tool** (incoming): Claude calls `reply` when it wants to send a structured response
- **Permission relay** (notification): Claude forwards tool approval requests

To trigger a reply, send a message that asks Claude to use the reply tool:
```bash
curl -s -X POST http://localhost:8080/api/channel/send \
  -H 'Content-Type: application/json' \
  -d '{"text":"Please use the reply tool to confirm you received this message","session_id":"SESSION_ID"}'
```

### Channel tab shows direction indicators

- `->` (blue) — outgoing message from datawatch to Claude
- `<-` (amber) — incoming reply from Claude to datawatch
- lightning (purple) — notification (permission relay, etc.)

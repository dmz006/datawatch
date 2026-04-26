# datawatch v5.18.0 — release notes

**Date:** 2026-04-26
**Spans:** v5.17.0 → v5.18.0
**Closed:** MCP channel one-way bug for TLS + claude-code daemons

## What's new

### Fix — MCP channel `/api/channel/ready` blocked by HTTP→HTTPS redirect

**Symptom:** `claude mcp list` shows `datawatch-<session>: ✓ Connected`
(stdio handshake works, claude-code can call the `reply` tool fine),
but the daemon never pushes messages back to claude. Output that the
operator types in chat channels never reaches the running session.

**Root cause:** The daemon runs HTTP on `cfg.Server.Port` (8080
default) and HTTPS on `cfg.Server.TLSPort` (8443 default). The HTTP
listener unconditionally redirects every path to HTTPS via 307. The
MCP bridge (`datawatch-channel`) is configured with
`DATAWATCH_API_URL=http://127.0.0.1:8080` and POSTs
`/api/channel/ready` on startup so the daemon learns its loopback
port. The bridge follows the redirect to HTTPS, hits the daemon's
self-signed TLS cert, fails verify, and returns the error to its
stderr (which goes to claude-code, not the daemon log):

```
[datawatch-channel] notify ready (non-fatal):
  Post "https://127.0.0.1:8443/api/channel/ready":
  tls: failed to verify certificate: x509: certificate signed by unknown authority
```

The daemon never gets the bridge's listening port, so the daemon's
push path (`/api/channel/send` → `http://127.0.0.1:<bridge-port>/send`)
has nowhere to send. Reply tool works one-way only.

This bug is not specific to the OOM triage — TLS introduction
broke this path some time ago, and the operator only noticed the
asymmetric channel behaviour recently. The OOM emergency fix
(BL292 / v5.6.0) didn't introduce the bug but coincided with the
operator noticing it.

**Fix:** The HTTP→HTTPS redirect handler now bypasses the redirect
for **loopback** requests to `/api/channel/*` paths. The bridge is
loopback-only by design (it binds to 127.0.0.1), so plaintext on
the loopback interface is safe and matches the bridge's existing
`/send` and `/permission` endpoints.

```go
// internal/server/server.go redirect handler
if isLoopbackRemote(r.RemoteAddr) && strings.HasPrefix(r.URL.Path, "/api/channel/") {
    s.srv.Handler.ServeHTTP(w, r)  // serve directly without redirect
    return
}
// otherwise: 307 → HTTPS
```

New `isLoopbackRemote` helper handles 127/8 + `::1` + IPv4-mapped
IPv6 loopback (covers `::ffff:127.0.0.1` from dual-stack listeners).

### Verification

After the fix, on session bridge spawn:

```
$ DATAWATCH_API_URL=http://127.0.0.1:8080 datawatch-channel
[datawatch-channel] HTTP listener on 127.0.0.1:41117
[datawatch-channel] MCP stdio transport starting
# (no more TLS verify error)

$ tail ~/.datawatch/daemon.log
[channel] ready for session ralfthewise-eac4 (channel_ready=true, port=41117)
```

### Tests

- `TestIsLoopbackRemote` covers 127/8 + ::1 + IPv6 + IPv4-mapped
  + missing-port + empty-string + garbage cases.
- 1358 total (1357 → 1358).

## Upgrade path

```bash
datawatch update                 # check + install
datawatch restart                # apply the fix; existing claude-code
                                 # MCP registrations re-spawn the bridge
                                 # automatically and notifyReady() now
                                 # succeeds.
```

After the restart, the daemon log should show
`[channel] ready for session <id>` for every active claude-code
session. From the operator's side, sending a chat-channel message
(`new:`, `reply:`, etc.) should now propagate to the running session
without the asymmetric one-way silence.

## Known follow-ups (still open)

- BL190 deeper density (failure popups, mid-run progress, verdict
  drill-down) — iterative cosmetic.

# PWA Setup Guide

## What the PWA provides vs Signal

| Feature | Signal | PWA |
|---|---|---|
| Session start/stop | Yes (async) | Yes (real-time) |
| Session list | Yes (on demand) | Yes (live-updating) |
| Output streaming | No — tail on demand | Yes — real-time WebSocket push |
| Needs-input notification | Signal message | Browser notification + highlight |
| Network model | Global Signal infrastructure | Tailscale overlay (direct to machine) |
| Requires internet | Yes | Only Tailscale (local or remote) |
| Install to home screen | No | Yes (Android Chrome, iOS Safari) |

Both interfaces share the same session state. Commands sent via Signal are immediately visible in the PWA and vice versa.

---

## Prerequisites

- **Tailscale** installed and connected on:
  - The machine(s) running `datawatch`
  - Your phone / tablet
- `datawatch` daemon running with `server.enabled: true` (the default)
- Chrome on Android (for "Add to Home Screen" PWA install)
  or Safari on iOS (for "Add to Bookmark")

---

## Finding the Tailscale IP of a machine

On the machine running `datawatch`:

```bash
tailscale ip -4
```

This gives you an IP in the `100.x.x.x` range. The PWA is accessible at:

```
http://100.x.x.x:8080
```

You can also use the Tailscale MagicDNS hostname if enabled:

```
http://hostname.your-tailnet.ts.net:8080
```

---

## Accessing the PWA

1. Make sure `datawatch start` is running on the target machine.
2. Connect your phone to Tailscale.
3. Open Chrome on Android (or Safari on iOS) and navigate to:

   ```
   http://<tailscale-ip>:8080
   ```

4. You should see the Sessions view with any current sessions.

---

## Installing to Android Home Screen

1. Open the PWA URL in **Chrome** on Android.
2. Tap the three-dot menu (top right).
3. Tap **Add to Home Screen**.
4. Confirm the name ("Datawatch") and tap **Add**.

The app now appears on your home screen and opens in standalone mode (no browser chrome).

---

## Installing to iOS Home Screen

1. Open the PWA URL in **Safari** on iOS (Chrome does not support PWA install on iOS).
2. Tap the **Share** button (box with arrow pointing up).
3. Scroll down and tap **Add to Home Screen**.
4. Confirm and tap **Add**.

---

## Enabling Notifications

Browser notifications alert you when a session enters `waiting_input` state — identical to the Signal message but delivered as a native phone notification.

1. Open the PWA.
2. Tap **Settings** (gear icon in nav bar).
3. Tap **Request Notification Permission**.
4. Allow notifications when the browser prompts.

Notifications fire whenever any session on the connected machine transitions to `waiting_input`. The notification includes the session ID and the last prompt text.

---

## Token Authentication

By default the PWA is open to anyone who can reach the machine via Tailscale. Since Tailscale is an encrypted, authenticated overlay network this is sufficient for most setups.

If you want an additional layer of auth (e.g. you share your Tailscale network with others):

1. Add to `~/.datawatch/config.yaml`:

   ```yaml
   server:
     token: your-secret-token
   ```

2. Restart the daemon.
3. Open the PWA → Settings → enter the token in the **Bearer Token** field → **Save Token & Reconnect**.

The token is sent as a WebSocket query parameter (`?token=...`) and as an HTTP `Authorization: Bearer ...` header for API calls. It is stored in the browser's `localStorage`.

---

## Multi-Machine Setup

Each machine has its own PWA at its own Tailscale IP. Bookmark all of them in Chrome or add each to the home screen.

```
http://100.100.1.10:8080  → laptop
http://100.100.1.20:8080  → desktop
http://100.100.1.30:8080  → vps
```

Sessions from each machine are shown independently — the PWA for `laptop` only shows `laptop` sessions.

---

## Custom Port

To change the port:

```yaml
server:
  port: 9090
```

To bind only on Tailscale (not all interfaces), find your Tailscale IP and set:

```yaml
server:
  host: 100.x.x.x
  port: 8080
```

---

## TLS / HTTPS Setup

HTTPS is **required** for a full standalone PWA experience (service worker, install-to-homescreen, push notifications). Without HTTPS, "Add to Home Screen" creates a browser shortcut instead of a standalone app.

### Quick Start (self-signed cert)

```yaml
server:
  tls: true
  tls_port: 8443
  tls_auto_generate: true   # generates self-signed cert with hostname + all IPs
```

Or via any comm channel: `configure server.tls=true server.tls_auto_generate=true`

This enables **dual-port mode**: HTTP on port 8080 (redirects to HTTPS) and HTTPS on port 8443. Both ports are configurable.

### Using your own certificates

```yaml
server:
  tls: true
  tls_port: 8443
  tls_cert: /path/to/cert.pem
  tls_key: /path/to/key.pem
```

### Using Tailscale HTTPS (easiest)

If you use Tailscale, it provides valid certificates automatically:

```bash
tailscale cert hostname.your-tailnet.ts.net
```

```yaml
server:
  tls: true
  tls_cert: /path/to/hostname.your-tailnet.ts.net.crt
  tls_key: /path/to/hostname.your-tailnet.ts.net.key
```

Access via `https://hostname.your-tailnet.ts.net:8443` — no cert warnings.

### Installing the CA Certificate (self-signed only)

When using `tls_auto_generate: true`, browsers will show a certificate warning. To eliminate this and enable full PWA standalone mode, install the CA cert on your device.

**Download the certificate** from the web UI: Settings > Comms > Web Server > **Download CA Certificate (.crt)**. Or visit directly:
- PEM format: `https://your-host:8443/api/cert`
- DER format: `https://your-host:8443/api/cert?format=der`

**Android:**
1. Download the .crt (DER format) from the link above
2. Settings > Security & privacy > More security & privacy > Encryption & credentials > Install a certificate > CA certificate
3. Select the downloaded file, confirm install
4. Restart Chrome — the certificate warning should be gone

**iPhone / iPad:**
1. Download the .pem file from the link above
2. Settings > General > VPN & Device Management > tap the downloaded profile > Install
3. Settings > General > About > Certificate Trust Settings > enable full trust for the datawatch certificate
4. Restart Safari

### PWA Standalone Mode Checklist

For the PWA to install as a standalone app (no browser chrome):

1. **HTTPS enabled** — `server.tls: true`
2. **CA cert installed** on your device (or using Tailscale/valid cert)
3. **PNG icons present** — datawatch ships with 192x192 and 512x512 PNG icons in the manifest
4. Navigate to `https://your-host:8443`
5. Tap browser menu > "Add to Home Screen" or "Install app"

---

## Architecture: Why Tailscale is sufficient (without TLS)

If you don't need standalone PWA mode or push notifications, Tailscale encrypts all traffic between nodes, making plain HTTP secure:

- **Encrypted**: WireGuard's ChaCha20-Poly1305
- **Authenticated**: only your Tailscale-authenticated devices can connect
- **Direct or relayed**: tries direct peer-to-peer; falls back to DERP relay if NAT prevents it

Plain HTTP on Tailscale is equivalent to HTTPS on the public internet. TLS is only needed for browser features that require a secure context (service worker, push notifications, standalone PWA install).

---

## Troubleshooting

**Cannot reach the PWA**
- Check that `datawatch` is running: `datawatch start`
- Verify the port is correct (default 8080)
- Verify Tailscale is connected on both the server and your phone: `tailscale status`
- Check the server is not firewalled: `curl http://100.x.x.x:8080/api/sessions`

**WebSocket disconnects immediately**
- Check the token setting in Settings matches `config.yaml`
- Look at the daemon stdout for error messages

**Sessions list is empty**
- The PWA shows sessions from the machine it is connected to
- Run `datawatch session list` on the machine to verify sessions exist

**Notifications not working**
- Ensure notification permission was granted in Settings
- On Android: check that Chrome notifications are not blocked in system settings
- The PWA must be open in the background (or installed to home screen) to receive push events

**Service worker not installing**
- Open DevTools → Application → Service Workers to check registration status
- Clear the site cache and reload

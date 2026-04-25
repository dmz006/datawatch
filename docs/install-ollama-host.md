# Installing datawatch-stats on a remote Ollama / GPU host

This guide covers the **Shape B** standalone observer daemon — the
right deployment for an Ollama box, GPU inference host, or any node
that should report per-process stats to a primary datawatch without
running the full parent.

For the cluster sidecar (Shape C — DaemonSet, eBPF, DCGM scrape, k8s
metrics) see [`docs/api/observer.md#shape-c`](api/observer.md#shape-c)
and the Helm chart's `observer.shapeC.*` values.

## Two install paths

1. **Static binary on the host** — fastest, smallest footprint.
2. **Docker container** — easier for operators who already manage
   everything via compose / portainer.

Both paths push to the same `/api/observer/peers/{name}/stats`
endpoint; the parent doesn't care which you pick.

---

## Path 1 — Static binary + systemd

### 1. Download the binary

```bash
OS=$(uname -s | tr '[:upper:]' '[:lower:]')   # linux | darwin
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
VER=v4.5.1                                     # or pin a specific release

curl -L -o /tmp/datawatch-stats \
  "https://github.com/dmz006/datawatch/releases/download/${VER}/datawatch-stats-${OS}-${ARCH}"
chmod +x /tmp/datawatch-stats
sudo mv /tmp/datawatch-stats /usr/local/bin/
datawatch-stats --version
```

### 2. Register the peer with your primary datawatch

This mints a bearer token. The token is returned **once**; capture it
into the unit-file env or a config file before continuing.

```bash
# Option A — from any host with curl and the parent reachable:
curl -k -X POST https://primary:8443/api/observer/peers \
  -H 'Content-Type: application/json' \
  -d '{"name":"ollama-box","shape":"B"}'
# → {"name":"ollama-box","shape":"B","token":"…long-base64…"}

# Option B — if you have the primary's CLI installed somewhere:
datawatch observer peer register ollama-box B "$(uname -r)"

# Option C — over chat (Signal / Telegram, after BL172 v4.5.1):
peers register ollama-box B
```

### 3. Persist the token

```bash
sudo install -d -m 0700 /var/lib/datawatch-stats
echo "<token-from-step-2>" | sudo tee /var/lib/datawatch-stats/peer.token
sudo chmod 0600 /var/lib/datawatch-stats/peer.token
```

### 4. Drop the systemd unit + start

```bash
# Pull the unit file from the release source tree:
curl -L -o /tmp/datawatch-stats.service \
  https://raw.githubusercontent.com/dmz006/datawatch/main/deploy/systemd/datawatch-stats.service
sudo mv /tmp/datawatch-stats.service /etc/systemd/system/

# Edit the Environment lines to point at YOUR primary + name:
sudoedit /etc/systemd/system/datawatch-stats.service
#   Environment="DATAWATCH_PARENT=https://primary:8443"
#   Environment="DATAWATCH_PEER_NAME=ollama-box"

# Tell the unit where to read the token:
sudo mkdir -p /etc/systemd/system/datawatch-stats.service.d
cat <<'EOF' | sudo tee /etc/systemd/system/datawatch-stats.service.d/token.conf
[Service]
ExecStart=
ExecStart=/usr/local/bin/datawatch-stats \
    --datawatch ${DATAWATCH_PARENT} \
    --name     ${DATAWATCH_PEER_NAME} \
    --token-file /var/lib/datawatch-stats/peer.token \
    --push-interval 5s \
    --listen 127.0.0.1:9001
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now datawatch-stats
sudo systemctl status datawatch-stats --no-pager | head -20
```

### 5. Verify the round-trip

From the primary datawatch:

```bash
curl -ks https://primary:8443/api/observer/peers \
  | jq '.peers[] | select(.name=="ollama-box") | {last_push_at, version, host_info}'

# Or in the PWA: Settings → Monitor → Federated peers
# Or over chat: peers ollama-box stats
```

A green health dot + recent `last_push_at` means it's working.

### 6. (Optional) Enable per-process eBPF net

Runs cleanly on Linux ≥ 5.10 with `CAP_BPF` + `CAP_PERFMON`. The
unit file ships with `AmbientCapabilities` commented out (least
privilege by default); the setup helper edits the drop-in for you:

```bash
sudo datawatch setup ebpf --target stats
sudo systemctl restart datawatch-stats
# Verify host.ebpf.kprobes_loaded=true on the next push:
curl -ks https://primary:8443/api/observer/peers/ollama-box/stats | jq .host.ebpf
```

---

## Path 2 — Docker container

Docker doesn't need the systemd unit; the binary is the entrypoint.

```bash
# Pull the matching binary into a slim base. (No prebuilt image is
# published for Shape B today — Shape C ships as ghcr.io/dmz006/
# datawatch-stats-cluster but it's distroless + privileged. For
# Shape B a plain alpine wrapper is enough.)

cat > Dockerfile.stats <<'EOF'
FROM alpine:3.20
RUN apk add --no-cache ca-certificates curl tzdata
ARG TARGETARCH
ARG VER=v4.5.1
RUN curl -L -o /usr/local/bin/datawatch-stats \
      "https://github.com/dmz006/datawatch/releases/download/${VER}/datawatch-stats-linux-${TARGETARCH}" \
    && chmod +x /usr/local/bin/datawatch-stats
ENTRYPOINT ["/usr/local/bin/datawatch-stats"]
EOF

docker buildx build --platform linux/amd64,linux/arm64 \
    -f Dockerfile.stats -t local/datawatch-stats:v4.5.1 .
```

Or skip the buildx + run the host binary inside a one-off container:

```bash
TOKEN=$(cat /var/lib/datawatch-stats/peer.token)
docker run -d --name datawatch-stats \
  --restart unless-stopped \
  --network host \
  -v /var/lib/datawatch-stats:/data:ro \
  -v /proc:/host/proc:ro \
  -v /sys:/sys:ro \
  -e DATAWATCH_PARENT=https://primary:8443 \
  local/datawatch-stats:v4.5.1 \
    --datawatch ${DATAWATCH_PARENT} \
    --name ollama-box-docker \
    --token-file /data/peer.token \
    --insecure-tls \
    --push-interval 5s
```

`--network host` is needed so the daemon's `/proc` walk + GPU
detection see the host (not the container) namespace. `/proc` and
`/sys` mounts are read-only and only used for the observer's
collector.

For per-process eBPF inside Docker, add:
```
--cap-add BPF --cap-add PERFMON
```

---

## Operator surfaces

Once installed and pushing, the peer shows up in every datawatch
surface (BL172 v4.5.1 added MCP + CLI + chat parity):

| Surface | Command / location |
|---|---|
| REST | `GET /api/observer/peers` |
| PWA | Settings → Monitor → Federated peers card |
| MCP | `observer_peers_list`, `observer_peer_stats` |
| CLI | `datawatch observer peer list` (run on the parent) |
| Comm (Signal / Telegram / etc.) | `peers` / `peers ollama-box stats` |

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| Peer never appears in `peers` list | Token file missing / wrong | re-run step 2 + 3; check `journalctl -u datawatch-stats` |
| `last_push_at` stuck in the past | Network blocked or parent down | curl the parent from the box; check firewall |
| `host.ebpf.kprobes_loaded: false` after `setup ebpf --target stats` | Kernel < 5.10 or missing BTF | accept /proc-only mode; document kernel rev |
| Push fails with 401 every tick | Token rotated on parent | peer auto-re-registers if writable token file; otherwise re-seed |
| TLS verify error against self-signed parent | No CA trust | `--insecure-tls` flag (dev only) |

## Related

- Operator doc: [`docs/api/observer.md`](api/observer.md)
- BL172 design: [`docs/plans/2026-04-25-bl172-shape-b-standalone-daemon.md`](plans/2026-04-25-bl172-shape-b-standalone-daemon.md)
- Shape C (cluster container): [`docs/plans/2026-04-25-bl173-shape-c-cluster-container.md`](plans/2026-04-25-bl173-shape-c-cluster-container.md)

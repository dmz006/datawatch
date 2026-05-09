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

**Which path if Ollama itself is in Docker?** Use **Path 2 (Docker)** —
ride the same compose / pod as Ollama so they share lifecycle + network
namespace + restart policy. Sidecar pattern: one healthcheck, one
update path, one place to look in `docker ps`. The static-binary path
adds a host-OS dependency you've already containerized away.

---

## Path 1 — Static binary + systemd

### 1. Download the binary

```bash
OS=$(uname -s | tr '[:upper:]' '[:lower:]')   # linux | darwin
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

# Resolve the latest release tag from GitHub (no jq required).
VER=$(curl -s https://api.github.com/repos/dmz006/datawatch/releases/latest \
        | grep -oE '"tag_name":\s*"[^"]+"' | head -1 | cut -d'"' -f4)
echo "Installing datawatch-stats $VER"

curl -L -o /tmp/datawatch-stats \
  "https://github.com/dmz006/datawatch/releases/download/${VER}/datawatch-stats-${OS}-${ARCH}"
# /tmp is often mounted noexec, so move to the final dest first, then chmod.
sudo mv /tmp/datawatch-stats /usr/local/bin/
sudo chmod +x /usr/local/bin/datawatch-stats
datawatch-stats --version
```

To pin a specific release instead, set `VER=v6.16.0` (or any other tag) before running `curl`.

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

The state directory must be owned by the user the systemd unit
runs as (default `datawatch`). Order matters: create → chown → chmod
→ write the token (so the file inherits the right owner).

```bash
# Create the system user if it doesn't exist:
id datawatch || sudo useradd -r -s /usr/sbin/nologin datawatch

# Create + own + lock the state dir:
sudo mkdir -p /var/lib/datawatch-stats
sudo chown -R datawatch:datawatch /var/lib/datawatch-stats
sudo chmod 700 /var/lib/datawatch-stats

# Write the token AS the datawatch user so it inherits the right owner:
echo "<token-from-step-2>" | sudo -u datawatch tee /var/lib/datawatch-stats/peer.token
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

# Resolve latest release tag (or override VER on the buildx command).
VER=$(curl -s https://api.github.com/repos/dmz006/datawatch/releases/latest \
        | grep -oE '"tag_name":\s*"[^"]+"' | head -1 | cut -d'"' -f4)

cat > Dockerfile.stats <<EOF
FROM alpine:3.20
RUN apk add --no-cache ca-certificates curl tzdata
ARG TARGETARCH
ARG VER=${VER}
RUN curl -L -o /usr/local/bin/datawatch-stats \\
      "https://github.com/dmz006/datawatch/releases/download/\${VER}/datawatch-stats-linux-\${TARGETARCH}" \\
    && chmod +x /usr/local/bin/datawatch-stats
ENTRYPOINT ["/usr/local/bin/datawatch-stats"]
EOF

docker buildx build --platform linux/amd64,linux/arm64 \
    --build-arg VER=${VER} \
    -f Dockerfile.stats -t local/datawatch-stats:${VER} .
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
  local/datawatch-stats:${VER} \
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

---

## Datawatch features that USE Ollama (and what happens without it)

**Hard rule:** datawatch never *requires* Ollama. Every feature listed below
has a documented degraded-mode fallback when `cfg.Ollama.Host` is empty
or the host is unreachable. Operators on a no-GPU box stay fully functional;
only the GPU-accelerated path opts into Ollama.

| Feature | Ollama endpoint hit | What GPU buys you | Degraded mode (no Ollama / unreachable) |
|---|---|---|---|
| **Memory embedder** (`internal/memory`) | `/api/embeddings` (model: `cfg.Memory.embedder_model`, default `nomic-embed-text`) | 100-1000× faster batch embed of session output → richer cross-session recall | Embedder skipped; memory recall uses keyword + recency only. No errors. |
| **BL274 vector index** (`internal/docsindex/vector.go`) | Same embedder as above (reused) | Sub-100ms semantic docs_search across the corpus | Falls back to BM25 (`internal/docsindex/bm25.go`) — keyword search keeps every operator surface working. The fallback is the test harness for the rule. |
| **`/api/ask` endpoint** (`internal/server/ask.go`, BL34) | `/api/generate` (model: `cfg.Ollama.Model`) | Single-shot LLM Q&A from the chat surface; instant answers without spawning a session | Returns `503 ollama not configured` with clear remediation. PWA hides the card; CLI `datawatch ask` errors with the same text. OpenWebUI is the documented alternate backend. |
| **BL274 Sprint 4 LLM-translation fallback** (`internal/server/docs_translator.go`) | Same `/api/generate` as `ask` | Translates non-curated howto prose into MCP-call sequences | `LLMTranslator` is nil; `docs_apply` against an unauthored howto returns `501` with explicit message. Curated howtos (22/22 today) work without Ollama. |
| **Council Mode synthesis** (`internal/council`) | `/api/generate` (model from council config) | Multi-persona debate synthesis | Returns each persona's raw output; synthesis step skipped, operator reviews manually. |
| **Eval llm_rubric grader** (`internal/evals/graders`) | `/api/generate` per grading rule | Auto-scores LLM output against rubric | Grader marked `skipped`; suite still runs the deterministic rules (regex/contains/etc.) |
| **Autonomous decompose** (`internal/autonomous`) | `/api/generate` | Splits a PRD into stories | Returns 503 with explicit message; operator decomposes manually or switches PRD backend to claude-code/openwebui. |

**Sidecar pattern.** All seven features read `cfg.Ollama.Host` — set it once
in `~/.datawatch/config.yaml` and every feature follows. Pointing
multiple datawatch hosts at one shared Ollama box (e.g. dedicated GPU
on Tailscale) is the recommended deployment.

**Verify the rule.** `bash scripts/release-smoke.sh` runs against the
local daemon with whatever Ollama config you have. To verify the
no-GPU path: `unset` the Ollama host, restart, run the smoke — it
must still pass (BL274 vector layer, /api/ask fail-soft, etc. all
have fallbacks).

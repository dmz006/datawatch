# Compute Node Routing (v8.0)

> **v8.0 feature** — routing modes are new in v8.0. All v7 nodes default to `direct` automatically.

Datawatch separates **what** protocol a Compute Node speaks (kind: `ollama`, `openai-compat`, etc.) from **how** the daemon reaches it at inference time (routing: `direct`, `docker-network`, `datawatch-proxy`).

---

## Prerequisites

- Datawatch daemon running (v8.0+)
- For `docker-network`: Docker Engine installed and `/var/run/docker.sock` accessible by the daemon
- For `datawatch-proxy`: a second datawatch instance registered as a Remote Server (Settings → Comms → Remote Servers)

---

## 1. Direct routing (default)

Add an Ollama node at a static address:

```yaml
# datawatch.yaml
compute_nodes:
  - name: gpu-1
    kind: ollama
    address: http://10.0.0.10:11434
    # routing: direct   ← implicit default
```

Or via CLI:

```
datawatch compute node add gpu-1 kind=ollama address=http://10.0.0.10:11434
```

Or REST:

```bash
curl -X POST $DW/api/compute/nodes \
  -H "Content-Type: application/json" \
  -d '{"name":"gpu-1","kind":"ollama","address":"http://10.0.0.10:11434"}'
```

**Probe:** the daemon pings the address on create/update. Use `?probe=skip` if the node is temporarily unreachable.

---

## 2. docker-network routing

The daemon manages the container lifecycle via `docker` CLI. On first inference request, the daemon:
1. Creates the Docker network (if missing).
2. Starts the container (if `auto_start: true` and container not running).
3. Pulls the image first (if `auto_pull: true` and image not present).
4. Routes the request to the container's port on the network.

**Teardown:** when the Compute Node is deleted from the registry, the daemon stops and removes the associated container.

```yaml
# datawatch.yaml
compute_nodes:
  - name: local-gpu
    kind: ollama
    routing: docker-network
    routing_docker_network:
      image: ollama/ollama:latest
      network_name: datawatch-llm    # created if missing
      port: 11434
      container_name: dw-ollama      # optional explicit name
      auto_start: true               # start on first probe
      auto_pull: false               # pull image only if missing
      env:
        - OLLAMA_NUM_GPU=1
```

CLI:

```
datawatch compute node add local-gpu kind=ollama routing=docker-network \
    image=ollama/ollama:latest network=datawatch-llm port=11434 auto_start=true
```

MCP:

```json
{
  "tool": "compute_node_add",
  "name": "local-gpu",
  "kind": "ollama",
  "routing": "docker-network",
  "routing_docker_network_json": "{\"image\":\"ollama/ollama:latest\",\"network_name\":\"datawatch-llm\",\"port\":11434,\"auto_start\":true}"
}
```

**Check status:**

```
datawatch compute node detail local-gpu
# Shows container_running, container_id, image
```

**Troubleshooting:**

| Symptom | Cause | Fix |
|---|---|---|
| `docker not found` error on probe | Docker CLI not in daemon PATH | Install docker, or set `DOCKER_HOST` |
| Container keeps restarting | Ollama GPU config wrong | Check `OLLAMA_NUM_GPU` env var |
| Network not created | Docker permission denied | Add daemon user to `docker` group |
| Node probe fails but container is up | Address scheme wrong | Port mismatch or container not on network |

---

## 3. datawatch-proxy routing

Route inference through a federated peer's `/api/proxy/llm/<name>` endpoint. Useful for:
- Accessing an Ollama instance on a private network through a remote datawatch
- Distributing load across geographically separated deployments

**Step 1 — Register the peer in Remote Servers (Settings → Comms → Remote Servers):**

```yaml
# datawatch.yaml
servers:
  - name: dc2
    url: https://datawatch-dc2.internal:8443
    token: ${secret:dc2-token}
    federated: true
    capabilities: [sessions:list, sessions:input]
```

Or via Settings → Comms → Remote Servers → Add.

**Step 2 — Add the proxy Compute Node:**

```yaml
compute_nodes:
  - name: dc2-ollama
    kind: ollama
    routing: datawatch-proxy
    routing_datawatch_proxy:
      peer: dc2
      remote_llm_name: llama3      # LLM name on the peer
      timeout_seconds: 30
```

CLI:

```
datawatch compute node add dc2-ollama kind=ollama routing=datawatch-proxy \
    peer=dc2 remote_llm=llama3 timeout=30
```

**Step 3 — Add an LLM referencing the proxy node:**

```yaml
llms:
  - name: remote-llama3
    kind: ollama
    compute_nodes: [dc2-ollama]
    models:
      - node: dc2-ollama
        model: llama3
```

**Troubleshooting:**

| Symptom | Cause | Fix |
|---|---|---|
| `peer "dc2" not found` | Server not registered | Add server under Settings → Comms → Remote Servers |
| `401 Unauthorized` from peer | Wrong token | Update token in Remote Servers |
| Timeout | `timeout_seconds` too short | Increase to 60+ for large models |
| `capability denied` | Peer token missing `sessions:input` cap | Update capabilities in Remote Servers |

---

## 4. gemini-api LLM

The `gemini-api` kind talks directly to Google's Generative Language v1beta API.

**Step 1 — Store your API key as a secret:**

```
datawatch secret set gemini-key AIza...
```

Or Settings → Secrets.

**Step 2 — Add the Compute Node:**

```yaml
compute_nodes:
  - name: gemini-node
    kind: gemini-api
    address: https://generativelanguage.googleapis.com
    routing: direct
```

**Step 3 — Add the LLM with api_key_ref:**

```yaml
llms:
  - name: gemini-flash
    kind: gemini-api
    compute_nodes: [gemini-node]
    models:
      - node: gemini-node
        model: gemini-1.5-flash
    api_key_ref: "${secret:gemini-key}"
```

**Test:**

```
datawatch llm test gemini-flash
```

---

## 5. opencode-api LLM

`opencode-api` uses the OpenAI-compatible `/v1/chat/completions` endpoint that opencode exposes when run with `opencode serve`.

**Start opencode in API mode (on the inference host):**

```bash
opencode serve --port 4000
```

**Add Compute Node and LLM:**

```yaml
compute_nodes:
  - name: opencode-local
    kind: opencode-api
    address: http://localhost:4000
    routing: direct

llms:
  - name: my-opencode
    kind: opencode-api
    compute_nodes: [opencode-local]
    models:
      - node: opencode-local
        model: claude-sonnet-4-5
```

---

## 6. Routing fallback chain

Nodes with different routing modes can co-exist in a single LLM's failover chain:

```yaml
llms:
  - name: llama3
    compute_nodes: [local-gpu, dc2-ollama, backup-direct]
    # dispatcher tries nodes in order; docker-network nodes auto-start their container
```

---

## 7. Troubleshooting quick-reference

| Error | Routing | Likely cause |
|---|---|---|
| `routing_docker_network.image required` | docker-network | Missing `image` field |
| `routing_datawatch_proxy.peer required` | datawatch-proxy | Missing or blank `peer` |
| `unknown routing "x"` | any | Typo in routing field |
| Probe timeout on docker-network | docker-network | `auto_start: false` and container not running |
| `peer unreachable` | datawatch-proxy | Peer URL or network issue |
| `api_key_ref required` | gemini-api | No `api_key_ref` on the LLM |

**See also:**
- [`datawatch-definitions.md#compute-nodes`](../datawatch-definitions.md#compute-nodes) — field reference
- [`compute-nodes.md`](compute-nodes.md) — general Compute Node management
- [`federation-cbac.md`](federation-cbac.md) — capability-based access for proxy peers
- [`llm-registry.md`](llm-registry.md) — LLM registry and failover routing

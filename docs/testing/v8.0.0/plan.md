# datawatch v8.0.0 Test Plan

**Version**: v8.0.0  
**Framework**: `docs/testing/plan.md`  
**Story catalog**: `docs/testing/master-cookbook.md` (T40: TS-609–TS-626)  
**Results**: `docs/testing/v8.0.0/cookbook.md`

---

## What's New in v8.0.0 Testing

v8.0.0 introduces compute node routing modes as a new orthogonal dimension:
- `direct` — existing behavior, node contacted at `address`  
- `docker-network` — daemon manages Docker container lifecycle; routes via container IP  
- `datawatch-proxy` — request forwarded to a peer datawatch daemon

## Sprint T40 — Compute Node Routing (TS-609–TS-626)

18 stories covering all three routing modes, validation, lifecycle, and smoke.

### Running T40

```bash
# Local (auto-starts isolated daemon):
bash scripts/run-tests.sh --feature=routing

# Via docker compose:
make test-e2e-docker
```

### Environment

| Variable | Purpose | Default |
|---|---|---|
| `TEST_PORT` | Daemon HTTP port | auto (free port) |
| `TEST_MCP_PORT` | MCP SSE port | auto (free port) |
| `DW_PEER_URL` | Peer daemon URL for proxy tests | — (TS-619 skips if unset) |
| `DW_PEER_TOKEN` | Peer auth token | `peer-test-token` |
| `DOCKER_NETWORK` | Docker network name for dn tests | `datawatch-llm` |

### TS-609–TS-626 Story Index

| TS# | Routing | Description |
|---|---|---|
| TS-609 | direct | POST node routing:direct → 200; GET shows routing field |
| TS-610 | direct | invalid routing value → 400 |
| TS-611 | docker-network | missing image field → 400 |
| TS-612 | docker-network | node add creates Docker network |
| TS-613 | docker-network | container launches on probe with auto_start |
| TS-614 | docker-network | re-probe does not spawn extra container |
| TS-615 | docker-network | GET /detail returns container_running field |
| TS-616 | docker-network | DELETE node removes container |
| TS-617 | datawatch-proxy | missing peer field → 400 |
| TS-618 | datawatch-proxy | proxy node added against registered peer |
| TS-619 | datawatch-proxy | peer /api/proxy/llm route reachable |
| TS-620 | smoke | daemon health 200 with status:ok (blocking) |
| TS-621 | smoke | compute node REST CRUD includes routing field |
| TS-622 | smoke | MCP SSE connects without 401 |
| TS-623 | smoke | PWA root returns HTML with title |
| TS-624 | smoke | unknown token on MCP SSE → 401 |
| TS-625 | direct | probe=skip bypasses connectivity check |
| TS-626 | k8s-sidecar | returns 400 (not yet supported) |

### docker-network requirements

- Docker daemon accessible (socket at `/var/run/docker.sock` or via `DOCKER_HOST`)  
- `conflict:docker` tag — stories skip gracefully if docker unavailable

### datawatch-proxy requirements

- A second datawatch daemon (the peer) must be running  
- TS-619 requires `DW_PEER_URL` env var; skips if unset  
- In docker-compose: peer is at `http://datawatch-peer:8080`

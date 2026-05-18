# E2E Test Cookbook — v8.0.0

**Version**: v8.0.0  
**Sprint**: T40 — Compute Node Routing  
**Stories**: TS-609–TS-626 (18 tests)  
**Last Run**: —  
**Pass Rate**: — (0/18)  
**Status**: 📋 Ready to run

---

## T40 Results

| TS# | Description | Status | Notes |
|---|---|---|---|
| TS-609 | direct routing POST+GET | 📋 planned | — |
| TS-610 | invalid routing → 400 | 📋 planned | — |
| TS-611 | docker-network missing image → 400 | 📋 planned | conflict:docker |
| TS-612 | docker-network creates network | 📋 planned | conflict:docker |
| TS-613 | docker-network container launch | 📋 planned | conflict:docker |
| TS-614 | docker-network idempotent re-probe | 📋 planned | conflict:docker |
| TS-615 | /detail returns container_running | 📋 planned | conflict:docker |
| TS-616 | DELETE removes container | 📋 planned | conflict:docker |
| TS-617 | datawatch-proxy missing peer → 400 | 📋 planned | — |
| TS-618 | datawatch-proxy node added | 📋 planned | — |
| TS-619 | peer /api/proxy/llm reachable | 📋 planned | skip if no DW_PEER_URL |
| TS-620 | health smoke 200 (blocking) | 📋 planned | ⛔ blocking |
| TS-621 | CRUD smoke with routing | 📋 planned | — |
| TS-622 | MCP SSE connects | 📋 planned | — |
| TS-623 | PWA root HTML | 📋 planned | — |
| TS-624 | bad token → 401 | 📋 planned | — |
| TS-625 | probe=skip bypass | 📋 planned | — |
| TS-626 | k8s-sidecar → 400 | 📋 planned | — |

---

## How to Update This File

After each run, update the Status column:
- ✅ pass
- ❌ fail  
- ⏭ skip
- 📋 planned (not yet run)

Update **Last Run** and **Pass Rate** at the top after each full run.

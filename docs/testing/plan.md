# datawatch Master Test Plan
**Applies to**: all versions  
**Test runner**: `scripts/run-tests.sh`  
**Story catalog**: `docs/testing/master-cookbook.md`  
**Version results**: `docs/testing/v{N}.{N}.{N}/cookbook.md`

---

## Dashboard Monitoring During Test Runs

Every test run integrates with the datawatch dashboard. Open the PWA at `https://localhost:8443` while running tests to see live progress.

### Smoke Run Card (BL303)

Both `scripts/release-smoke.sh` and `scripts/run-tests.sh` write per-run progress to `~/.datawatch/smoke-runs/{run_id}.json`. The **Smoke Run** card on the dashboard polls `GET /api/smoke/progress` (which returns an array of all run envelopes) every 2.5 seconds and shows:

- A selectable list of run envelopes (expandable, deletable per run)
- Pass / Fail / Skip counts with a live progress bar for the selected run
- The currently running section or story name
- A compact history of completed sections (✅ pass · ❌ fail · ⏭ skip)

> **Note**: The old single-file `~/.datawatch/smoke-progress.json` has been replaced by the multi-envelope directory `~/.datawatch/smoke-runs/`. Each run gets its own `{run_id}.json` file.

To add the Smoke Run card to your dashboard layout: click **Edit** in the dashboard stat bar → **Add** → select **🔬 Smoke Run**.

### Dashboard Cards for Testing

| Card | What you see during testing |
|---|---|
| 🔬 Smoke Run | Live pass/fail per section, progress bar, current phase |
| ⣿ Automata | Automata created by smoke (CRUD probes) with status |
| ⚡ Live Events | Real-time hook events from sessions spawned by tests |
| ≡ Timeline | Gantt bars for active test Automata by start/end time |
| ◎ Network | Graph of sessions + Automata; click to inspect |

### Future Full Integration (multi-sprint)

The current smoke integration uses a progress file polled via REST. Full integration will:
- Create a tracking Automaton per test run with one story per T-Sprint
- Update story status (pending → in_progress → completed/failed) as each sprint runs
- Show the run as a Gantt timeline with phase durations
- Persist test history in episodic memory for trend analysis
- Show coverage trend in the 30-Day Activity heatmap card

This multi-sprint roadmap is tracked separately and scoped for a future minor version.

---

## 1. Overview

### Evidence vs Cookbook vs Plan

| Artifact | Location | Persisted? | Purpose |
|---|---|---|---|
| **Plan** (`plan.md`) | `docs/testing/vX.Y.Z/plan.md` | ✅ Yes (force-added) | Defines every story: steps, expected, evidence filenames. Reference for all future runs. |
| **Cookbook** (`cookbook.md`) | `docs/testing/vX.Y.Z/cookbook.md` | ✅ Yes (force-added) | Live status table updated after every story. After a run it is the only persistent record of what passed/failed. |
| **Evidence** (`evidence/TS-NNN/`) | `../datawatch-<id>/evidence/` | ❌ Outside repo, auto-deleted | JSON responses, screenshots, CLI output. Exists only during a run. On FAIL, working dir + evidence are retained for diagnosis. |
| **Story scripts** (`TS-NNN.sh`) | `scripts/test-stories/` | ✅ Yes (force-added) | One bash file per story; sourced by `scripts/run-tests.sh`. `lib.sh` provides shared env + helpers. |

**For future releases**: copy `docs/testing/vX.Y.Z/` → `docs/testing/vX.Y+1.Z/`, reset cookbook to 📋, add stories for new features. The previous version plan is preserved as a baseline for regression.

### Design decisions

| Decision | Choice |
|---|---|
| Isolation | Same host; unique data dir `.datawatch-test-<hash>/` (hash = shell PID) prevents parallel-run conflicts |
| Evidence | Structured JSON + screenshots saved to `../datawatch-<id>/evidence/TS-NNN/` (outside the repo, kept on failure) |
| Organisation | T-Sprints group stories by feature/surface area per version |
| Parallelism | Full isolation via `TEST_RUN_HASH` (data dir) + `TEST_PORT_OFFSET` (daemon ports); both auto-set per invocation |
| Cleanup | After every run: stop test daemon, remove `.datawatch-test-<hash>/`, remove evidence/, remove all `test-*` resources |
| Pass criteria | HTTP response matches expected shape (asserted via python3); CLI stdout matches pattern; PWA screenshot saved + no console errors |

---

## 2. Environment Variables

| Variable | Default | Description |
|---|---|---|
| `TEST_RUN_HASH` | `<pid>` (5-digit shell PID) | Unique suffix for data dir; auto-set per invocation for parallel-run safety |
| `TEST_PORT_OFFSET` | `0` | Shift all daemon ports by N (e.g. `10` → 18090/18453/18091/18443); for full parallel isolation |
| `TEST_BASE` | `http://127.0.0.1:18080` | Base URL for test daemon (HTTP) |
| `TEST_TLS` | `https://127.0.0.1:18443` | TLS base URL |
| `TEST_HTTP` | `http://127.0.0.1:18080` | HTTP (non-TLS) base |
| `TEST_MCP_PORT` | `18081` | MCP SSE port |
| `TEST_CHAN_PORT` | `18433` | Channel port |
| `TEST_TOKEN` | `dw-test-token-12345` | Bearer token |
| `TEST_DATA` | `.datawatch-test-<hash>` | Data directory (in testing folder, never inside repo) |
| `TEST_BINARY` | `./bin/datawatch` | Path to daemon binary |
| `TEST_SIGNAL_GROUP` | `YOJtFDXm8WQCjna6dVGTOM8b4+aINRx4D4QgQ8Nmo54=` | Signal group ID for comm tests (production group) |
| `TEST_NTFY_TOPIC` | *(unset)* | ntfy topic for comm tests — skip TS-099 if unset |
| `TEST_OLLAMA_HOST` | `http://datawatch:11434` | Ollama base URL for LLM-tagged stories |
| `K8S_CONTEXT` | `testing` | kubectl context for Kubernetes stories |
| `TEST_WEBHOOK_PORT` | `19080` | Local listener port for webhook receipt |
| `TEST_SURFACE` | *(unset)* | Filter: `api\|cli\|pwa\|mcp\|comms\|docker\|k8s` |
| `TEST_FEATURE` | *(unset)* | Filter: `sessions\|automata\|memory\|...` |
| `TEST_SKIP_CONFLICT` | *(unset)* | Skip stories with matching conflict tag |
| `EVIDENCE_DIR` | `../datawatch-<id>/evidence` | Evidence output root (set by `scripts/run-tests.sh`) |
| `DATAWATCH_TEST_DIR` | `../datawatch-<id>` | Working dir created per run; set automatically |
| `DATAWATCH_TEST_ID` | random 6-char hex | Run identifier; set to a previous value to resume |
| `DATAWATCH_REPO_DIR` | repo root | Set by the runner so story scripts can find sources |

---

## 4. Tag Taxonomy

### Surface tags
| Tag | Meaning |
|---|---|
| `[surface:api]` | REST API surface |
| `[surface:cli]` | CLI (datawatch subcommands) |
| `[surface:pwa]` | PWA web app (Chrome headless CDP; fallback to API-only if Chrome unavailable) |
| `[surface:mcp]` | MCP tool/resource/prompt surface |
| `[surface:comms]` | Communication backends |
| `[surface:docker]` | Docker deployment simulation |
| `[surface:k8s]` | Kubernetes deployment |

### Feature tags
| Tag | Meaning |
|---|---|
| `[feature:bootstrap]` | Daemon start/health/auth |
| `[feature:sessions]` | Session lifecycle |
| `[feature:automata]` | Automaton/Automata lifecycle |
| `[feature:council]` | Council deliberation |
| `[feature:memory]` | Episodic memory |
| `[feature:kg]` | Knowledge graph |
| `[feature:secrets]` | Secrets store |
| `[feature:config]` | Config CRUD |
| `[feature:plugins]` | Plugin registry/invoke |
| `[feature:skills]` | Skills registry/invoke |
| `[feature:filters]` | Filter store |
| `[feature:schedules]` | Schedule store |
| `[feature:agents]` | Agent lifecycle |
| `[feature:profiles]` | Project/cluster profiles |
| `[feature:identity]` | Identity surface |
| `[feature:algorithm]` | Algorithm surface |
| `[feature:evals]` | Evals surface |
| `[feature:compute]` | Compute nodes |
| `[feature:routing]` | Compute node routing modes (direct / docker-network / datawatch-proxy) |
| `[feature:parity]` | Cross-surface parity audit (7-surface, config, hook, locale, comm) |
| `[feature:locale]` | Locale/i18n key completeness |

### Conflict tags

| Tag | Auto-run? | Notes |
|---|---|---|
| `[conflict:signal]` | ✅ runs automatically | Signal CLI available: account `+18435409771`, production group configured as default |
| `[conflict:llm]` | ✅ runs automatically | Ollama at `http://datawatch:11434`, model `qwen3:1.7b` pulled; daemon wired in test config |
| `[conflict:k8s]` | ✅ runs automatically | `kubectl --context=testing`, 3-node cluster; full deploy stories honest-skip (no container image) |
| `[conflict:docker]` | ⚠ skip if unavailable | Docker daemon required; stories skip gracefully if `/var/run/docker.sock` not accessible |
| `[conflict:pwa]` | opt-out | T11 tests run automatically via Chrome CDP (`pwa_cdp.py`); pass `--skip-conflict=pwa` to skip browser tests entirely |
| `[conflict:db-write]` | ✅ runs automatically | Mutates test data dir only; cleaned up after every run |
| `[conflict:keepassxc]` | ⛔ always skip | `keepassxc-cli` not installed on this machine |
| `[conflict:op]` | ⛔ always skip | `op` (1Password CLI) not installed on this machine |
| `[conflict:ntfy]` | ⚠ skip unless set | `TEST_NTFY_TOPIC` not set; TS-099 skips unless the env var is provided |

### What Cannot Be Tested

The following are explicitly excluded from automated runs. They are documented here so that gaps are acknowledged, not hidden.

- **KeePass backend** (`[conflict:keepassxc]`): `keepassxc-cli` not installed. Always skips.
- **1Password backend** (`[conflict:op]`): `op` CLI not installed. Always skips.
- **ntfy** (`[conflict:ntfy]`): `TEST_NTFY_TOPIC` not set. Skips unless the env var is provided at runtime.
- **Slack, Discord, Telegram, Matrix, Twilio, Email comm backends**: Not configured on this machine. T9 stubs for these backends always skip.
- **K8s full deployment**: No container image exists yet. Full deploy stories skip with an honest "no image" message; namespace/configmap/probe-pod stories do run.
- **Docker-network routing** (`[conflict:docker]`): Requires Docker daemon. Stories skip gracefully if Docker unavailable.

---

> For future releases: copy `docs/testing/vX.Y.Z/` to a new version folder, reset cookbook to 📋, add stories for new features. The prior version plan is preserved as a regression baseline.

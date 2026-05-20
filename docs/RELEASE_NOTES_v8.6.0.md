# datawatch v8.6.0

**Released:** 2026-05-20
**Covers:** v8.1.0 through v8.6.0 (all work since the v8.0.0 GitHub release)

> v8.6.0 is the first GitHub release since v8.0.0. All work from v8.1.0 through
> v8.6.0 is included — compute routing modes, community registry, Android blockers,
> channel routing, file service, discussion scopes, and full operational data encryption.

---

## Quick summary

| Version | Backlog | Theme |
|---|---|---|
| v8.1.x | BL318–BL326 | Compute routing modes, community registry, plugin install, mic popup, PWA fixes |
| v8.2.0 | BL327–BL330 | Android 1.0 blockers: async decompose, UnifiedPush, identity POST alias, badge inputs |
| v8.3.0 | BL331 + BL333 | Channel routing (inbound → peer map), federated file service |
| v8.4.0 | BL332 | Discussion scopes — federated append-only WAL memory |
| v8.5.0 | BL334 T43a–T43e | Operational data encryption: WAL, participants.json, channel_routing.json |
| v8.6.0 | BL334 T43g–T43h | Encryption: 4 JSON stores + encrypted daemon-app.log |

---

## v8.6.0 — BL334: Encryption completion (T43g + T43h)

### Added

**T43g — JSON store encryption.** Four stores now encrypted when `--secure` is active:

| File | Contents |
|---|---|
| `~/.datawatch/servers.json` | Federation peer registry + peer tokens |
| `~/.datawatch/skills.json` | Skills registry index and sync state |
| `~/.datawatch/compute/nodes.json` | ComputeNode registry |
| `~/.datawatch/inference/llms.json` | LLM registry and API key refs |

Each uses `secfile.ReadFile` / `secfile.WriteFile` (XChaCha20-Poly1305, DWDAT2 format). On first `--secure` startup after upgrade, `secfile.MigrateJSONStore` encrypts each file in place. Already-encrypted files are detected by DWDAT2 header and skipped.

**T43h — Encrypted application log.** When `--secure` is active, `log.SetOutput` redirects to `secfile.EncryptedLogWriter` at `<data-dir>/daemon-app.log` immediately after key derivation. DWLOG1 format: `DWLOG1\n` + `[u32le length][nonce24 + ciphertext]` blocks. Append-mode on restart — existing blocks are preserved.

**New CLI command:**
```bash
datawatch security logs           # decrypt + print all
datawatch security logs --tail 100
```

**Encryption status** probes all six categories: channel_routing, servers, skills, compute/nodes, inference/llms, daemon-app.log. Migrate and wipe-plaintext endpoints cover all six.

### What cannot be encrypted (by design)

| Item | Reason |
|---|---|
| `.git/` repo history | SHA-1 content-addressed — file-level encryption breaks all object references |
| Screen captures | Ephemeral; dispatched over WebSocket, never persisted |
| `daemon.log` (boot messages) | Written by parent `daemonize` before child derives key |
| `secrets.db` | Separately encrypted with AES-256-GCM + own keyfile |
| Memory embeddings | Per-entry encryption via `memory.encrypt_content` |

---

## v8.5.0 — BL334: Operational data encryption (T43a–T43e)

### Added

- **Discussion WAL encryption** — each line in `~/.datawatch/discussions/<id>/wal.jsonl` stored as `ENC:<base64(nonce24+ciphertext)>`. Append-compatible; lines without `ENC:` prefix read as plaintext (upgrade compat).
- **`participants.json` + `channel_routing.json` encryption** — XChaCha20-Poly1305, DWDAT2 format via `secfile.WriteFile`.
- **Startup migration** — `secfile.MigrateDiscussionWALs` and `secfile.MigrateChannelRouting` encrypt all existing plaintext files on first `--secure` startup. Idempotent.
- **Server struct `encKey` field** — `SetEncKey()` on `Server` and `HTTPServer`; wired from `main.go` after Argon2id derivation.
- **REST**: `GET /api/security/encryption/status`, `POST /api/security/encryption/migrate`, `POST /api/security/wipe-plaintext` (`{"confirm":true}`).
- **CLI**: `datawatch security encryption {status,migrate}`, `datawatch security wipe-plaintext --confirm`.
- **28 new E2E stories** (TS-751–TS-778).

### Fixed

- Discussion WAL mode `0644` → `0600` (was world-readable on multi-user systems).
- Conflict-resolve WAL markers written via `discussionAppendWALEntry` so they are encrypted when `--secure` is active.
- WAL scanner buffer raised to 1 MB (default 64 KB too small for encrypted lines ~33% larger).

---

## v8.4.0 — BL332: Discussion Scopes

### Added

- **Discussion WAL** — append-only JSONL at `~/.datawatch/discussions/<id>/wal.jsonl` with entries timestamped, origin-peer-tagged, and sequence-numbered.
- **Conflict detection** — `GET /api/memory/discussion/{id}/conflicts` flags writes from different origin peers with matching 64-character content prefix within 5 seconds. `POST .../conflicts/resolve` appends resolution WAL marker.
- **Rate throttle** — 60 writes/min per Bearer token (sync.Map token bucket); HTTP 429 on overflow.
- **Participant sync** — `GET/PUT /api/memory/discussion/{id}/participants`; every write fans out to all participants via `POST /api/push/<discussion-id>`.
- **`ScopeDiscussion`** constant in `internal/memory/scopes.go`.
- **REST**: full discussion CRUD + WAL + conflicts + participants; `CapCommRead`/`CapCommWrite` gates.
- **MCP**: `memory_discussion_write`, `memory_discussion_recall`, `memory_discussion_wal`, `memory_discussion_participants`.
- **CLI**: `datawatch memory discussion {list,write,recall,wal,participants}`.
- **PWA**: Settings → General → Discussion Scopes card.
- **32 new E2E stories** (TS-719–TS-750).

### Fixed

- `newDiscussionCmd()` was registered only to the shadowed `newMemoryCmd()` in `cli_memory.go`. Fixed by also registering in `newMemoryCliCmd()` in `main.go`.

---

## v8.3.0 — BL331 + BL333: Channel Routing + File Service

### Added

**BL331 — Channel Routing:**
- `channelRoutingRule`: `{channel_pattern, peer_name, automata_type, default_project_dir}`.
- Persisted at `~/.datawatch/channel_routing.json`. `GET/PUT /api/channel/routing` with `CapCommRead`/`CapCommWrite`.
- `multiserver.Entry` gains `ChannelIdentity []string`; CLI: `datawatch federation peer add --channel-identity`.
- **14th built-in federation group: `comms-channel-agent`** — sessions:list/read/input/write + comm:read/write + alerts:list/read + autonomous:list/read/write.
- Session `OwnerPeer` and PRD `OwnerPeer` fields (omitempty).
- PWA: Settings → Comms → Channel Routing card.

**BL333 — Federated File Service:**
- `checkPathTraversal(root, target string)` — prevents directory escape on every write path.
- `POST /api/files` (multipart, 50 MB limit), `DELETE /api/files` (JSON `{path}`), `GET /api/files/peers/{name}`, `GET /api/files/discussions/{id}`, `GET /api/files/meta`, `POST /api/files/upload` (MCP-friendly text upload).
- CLI: `datawatch files {list,upload,delete,peer}`.
- PWA: Settings → General → File Service card.
- **36 new E2E stories** (TS-683–TS-718).

### Fixed

- `PUT /api/channel/routing` accepted rules without `channel_pattern`. Added per-rule validation; returns HTTP 400.
- `federation peer add` and `federation peer update` lacked `--channel-identity` flag. Added to both.

---

## v8.2.0 — BL327–BL330: Android 1.0.0 Blockers + Settings UX

### Added

**BL327 — Badge/chip multi-select:**
- All comma-separated settings fields (secrets tags, federation caps, LLM fallback chain, compute tags, profile memory_shared_with, profile skills) replaced with badge-input components.
- Enter or comma creates a badge; × removes; known-set fields show dropdown with filter-on-type. Drag-to-reorder for LLM fallback chain. REST payloads unchanged.

**BL328 — Async PRD decompose:**
- `POST /api/autonomous/prds/<id>/decompose` returns `{task_id, stream_url}` immediately (HTTP 200/202).
- Stories stream as SSE events; `Last-Event-ID` enables mid-stream reconnect replay. Second POST returns same `task_id` (idempotent).
- `GET .../decompose/status` → `{status, progress, stories[], total, error}`.
- CLI: `datawatch autonomous prd decompose <id>`. MCP: `autonomous_prd_decompose`.

**BL329 — Identity POST alias:**
- `POST /api/identity` now aliases `PATCH` (partial update / merge non-empty fields) for Android compatibility. All four methods (GET/PUT/PATCH/POST) share one handler.

**BL330 — UnifiedPush:**
- `GET /.well-known/unifiedpush` → `{version:1, unifiedpush:{gateway:"/api/push/notify"}}`.
- `POST /api/push/register`, `GET /api/push/register`, `DELETE /api/push/unregister`, `POST /api/push/notify`.
- PWA: Settings → Comms → Push Notifications card. CLI: `datawatch push {register,unregister,notify,status}`. MCP: `push_register`, `push_unregister`, `push_notify`, `push_status`.
- **46 new E2E stories** (TS-637–TS-682).

**Release pipeline hardened:**
- `release.yaml` uploads govulncheck results, gosec SARIF, Trivy SARIF to Code Scanning, and `security-summary.md` to each release.
- `owasp-zap.yaml` triggers automatically on release via `workflow_run`.

### Fixed

- gosec baseline was frozen at v4.0 (stale findings); updated to current fingerprint. All new findings are accepted false positives.
- gosec `//nolint:gosec` → `// #nosec G402` in `sx_parity.go`.
- gosec scanning now excludes `.claude/worktrees/`.

---

## v8.1.x — BL318–BL326: Compute Routing, Community Registry, Plugin Install

### Added

**BL318–BL322 — Compute node routing modes:**
- `routing` field separates transport from protocol. Three modes: `direct` (default), `docker-network` (daemon manages container lifecycle), `datawatch-proxy` (forward through a federated peer's `/api/proxy/llm/<name>`).
- `RoutingDockerNetworkConfig`: image, network_name, port, container_name, docker_endpoint, auto_start, auto_pull, env.
- `RoutingDatawatchProxyConfig`: peer, remote_llm_name, timeout_seconds.
- `Node.Validate()` extended; `Node.WithAddress()` shallow-copy for address override.
- `DockerLifecycle` (`internal/compute/docker_lifecycle.go`): `EnsureRunning`, `EnsureNetwork`, `Teardown`, `Status` — wraps `docker` CLI.
- `ProxyRouter` (`internal/inference/proxy_router.go`): transient vs final error classification; registers inbound handler at `/api/proxy/llm/<name>`.

**BL321 — New adapters:**
- `gemini-api`: Google Generative Language v1beta `POST /v1beta/models/<model>:generateContent?key=<api_key>`.
- `opencode-api`: OpenAI-compatible `/v1/chat/completions` distinct from `openwebui`.

**BL324–BL326 — Community skills + plugins registry:**
- Community registry at `dmz006/datawatch-community` launched with seed Skills and Plugins.
- `datawatch skills registry connect <url>` + `datawatch skills sync` for community sync.
- **BL325 — Plugin install**: `datawatch plugins install <url>` installs plugins from community registry. Plugins can be toggled without restart.
- **BL326 — Mic popup**: PWA voice input button opens a modal microphone capture popup instead of in-line recording.

### Fixed

- v8.1.1: Session OneShot flag not respected on `DATAWATCH_COMPLETE` screen-capture path.
- v8.1.2: xterm viewport shrink on mobile keyboard (iOS 100dvh bug); input blocked by keyboard; tmux input still hidden via `view-full` override.

---

## E2E test coverage growth since v8.0.0

| Cohort | Stories | Range |
|---|---|---|
| v8.2.0 (BL327–BL330) | 46 | TS-637–TS-682 |
| v8.3.0 (BL331–BL333) | 36 | TS-683–TS-718 |
| v8.4.0 (BL332) | 32 | TS-719–TS-750 |
| v8.5.0 (BL334 T43a–T43e) | 28 | TS-751–TS-778 |
| **Total new** | **142** | |

**301 total E2E stories** across REST, MCP, CLI, PWA, channel, session, and security surfaces.

---

## Upgrade guide (from v8.0.0)

```bash
# 1. Stop the daemon
datawatch stop

# 2. Install v8.6.0 (replace the binary)

# 3. Start with --secure (if you use encryption)
datawatch --secure

# On first startup the daemon automatically:
#   - Migrates discussion WALs to encrypted format
#   - Migrates channel_routing.json, participants.json
#   - Migrates servers.json, skills.json, compute/nodes.json, inference/llms.json
#   (all idempotent — already-encrypted files are skipped)

# 4. Verify
datawatch security encryption status
```

Non-`--secure` upgrades require no migration steps.

---

## Known gaps (not in this release)

- **PWA browser-nav E2E** (GH#78) — current PWA stories check API endpoints and JS syntax; real browser navigation + component interaction (Playwright) not yet scheduled.
- **Matrix channel (BL241)** — full implementation sprint starting after v8.6.0.

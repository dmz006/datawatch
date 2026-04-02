# Backlog Plans — BL3, BL14, BL16, BL21, BL18, BL19, BL2, BL4

Plans for 8 backlog items. Each can be implemented independently.

---

## BL16: Health Check Endpoint

**Effort:** 30min | **Priority:** high (prerequisite for BL3)

### Current state
- `/api/health` exists (returns JSON with status, hostname, version, uptime) — public, no auth
- No `/healthz` or `/readyz` endpoints (k8s standard)

### Plan
1. Add `GET /healthz` — returns `200 OK` with `{"status":"ok"}` if HTTP server is responding. No auth. Used for k8s liveness probe.
2. Add `GET /readyz` — returns `200 OK` only when:
   - Session store is loaded
   - At least one messaging backend is connected (or web server is listening)
   - Signal-cli subprocess is alive (if signal backend enabled)
   Used for k8s readiness probe. Returns `503` with detail if not ready.
3. Both endpoints added to the public mux routes in `server.go:48-64` (before auth middleware).
4. Document in operations.md and config-reference.yaml.

### Files
- `internal/server/server.go` — route registration
- `internal/server/api.go` — handler functions
- `docs/operations.md` — documentation

---

## BL21: Fallback Chains with Multi-Profile Support

**Effort:** 4-6hr | **Priority:** medium

### Current state
- Single `llm_backend` string in config — no list/chain support
- Single API key / account per backend — no multi-profile
- Rate limit detection sets `StateRateLimited` and schedules auto-resume on same backend
- `llm.Get(name)` registry lookup supports all registered backends
- `Manager.Start()` accepts optional backend override via `StartOptions`

### Plan

#### Phase 1: Backend profiles
1. **Config**: Add `profiles` section for named backend configurations with different accounts/API keys:
   ```yaml
   profiles:
     claude-work:
       backend: claude-code
       # Uses default claude auth (Max subscription on work account)
     claude-personal:
       backend: claude-code
       env:
         ANTHROPIC_API_KEY: "sk-ant-..."
         # Or: claude auth switches account context
     gemini-fallback:
       backend: gemini
       env:
         GEMINI_API_KEY: "AIza..."
     opencode-backup:
       backend: opencode
   ```
2. **Profile resolution**: Each profile inherits from its parent backend config but can override `env` vars, `binary`, `model`, and `console_cols`/`console_rows`.
3. **Config exposure**: Profiles configurable via YAML, web UI (Settings → Profiles card), REST API (`/api/config` profiles section), and CLI (`datawatch setup profile`).

#### Phase 2: Fallback chain
4. **Config**: Add `session.fallback_chain: ["claude-personal", "gemini-fallback"]` — ordered list of profile names to try when primary hits rate limit.
5. **Detection hook**: In `processOutputLine` rate-limit handler (manager.go), when rate limit detected:
   - If `fallback_chain` is configured and not exhausted, start a new session with the next profile in the chain.
   - Copy the task, project_dir, and session name. Mark original session as `rate_limited`.
   - Link the fallback session to the original (new field `FallbackOf` on Session).
   - Set environment variables from the profile before launching the backend.
6. **Fallback session**: Creates a real new session with the fallback profile's backend and env. The original session stays in `rate_limited` with its auto-resume schedule as backup.
7. **Web UI**: Show fallback chain indicator on session card ("fallback from X → profile Y").
8. **Alert**: Notify comm channels: "Rate limit on {profile}, falling back to {next_profile}".

#### Phase 3: Profile management
9. **Web UI**: Settings → Profiles card with add/edit/delete for named profiles.
10. **New Session form**: Profile dropdown alongside backend dropdown (profile overrides backend).
11. **CLI**: `datawatch setup profile add claude-personal` interactive wizard.
12. **Chat commands**: `new claude-personal: <task>` to start with a specific profile.

### Files
- `internal/config/config.go` — `ProfileConfig` struct, `Profiles map[string]ProfileConfig`, `FallbackChain []string`
- `internal/config/config.go` — profile resolution (merge base backend + profile overrides)
- `internal/session/manager.go` — rate-limit handler + fallback logic + env injection
- `internal/session/store.go` — `FallbackOf`, `Profile` fields on Session
- `internal/server/api.go` — profiles in GET/PUT config, profile in session start API
- `internal/server/web/app.js` — Settings profiles card, New Session profile dropdown, fallback indicator
- `docs/config-reference.yaml` — profiles + fallback_chain docs
- `docs/operations.md` — profile usage guide
- `docs/llm-backends.md` — mention profiles for multi-account setups

---

## BL3: Container Images and Helm Chart

**Effort:** 1-2 days | **Priority:** medium

### Current state
- Makefile supports cross-compilation, GoReleaser for releases
- No Dockerfile, docker-compose, or Helm chart exists
- Runtime deps: tmux, signal-cli (Java 17+), Node.js 18+, claude CLI, opencode (optional)

### Plan

#### Phase 1: Dockerfile (multi-stage)
```
Stage 1: Go builder
  - golang:1.22-bookworm
  - COPY source, go build

Stage 2: Runtime
  - debian:bookworm-slim
  - Install: tmux, openjdk-17-jre-headless, nodejs (18+), curl
  - COPY --from=builder /datawatch
  - COPY signal-cli tarball (or install via apt)
  - Claude CLI: install via npm or download binary
  - Volume mounts:
    - /data (maps to ~/.datawatch — config, sessions, logs)
    - /workspace (default project directory — NFS mountable)
  - ENV: DATAWATCH_SECURE_PASSWORD (for encrypted mode)
  - EXPOSE 8080 (web) 8081 (MCP SSE)
  - ENTRYPOINT: datawatch start --foreground
```

#### Phase 2: NFS / external workspace support
- Add `session.workspace_root` config field — base directory for session project dirs
- Docker volume mount: `-v /nfs/projects:/workspace`
- Session `project_dir` resolves relative to `workspace_root` if set
- Validate mount is accessible on startup (readyz check)
- Document NFS mount options: `nfsvers=4,soft,timeo=30`

#### Phase 3: docker-compose.yaml
```yaml
services:
  datawatch:
    build: .
    ports: ["8080:8080", "8081:8081"]
    volumes:
      - datawatch-data:/data
      - /nfs/shared/projects:/workspace  # NFS mount
    environment:
      - DATAWATCH_SECURE_PASSWORD=${DATAWATCH_PASSWORD}
    restart: unless-stopped
volumes:
  datawatch-data:
```

#### Phase 4: Helm chart (charts/datawatch/)
- Deployment with configurable replicas (1 for now — stateful)
- ConfigMap for config.yaml
- Secret for API keys, passwords
- PersistentVolumeClaim for /data
- Optional NFS PV for /workspace
- Service (ClusterIP) + optional Ingress
- Health/readiness probes using /healthz and /readyz (requires BL16)
- Values: image, tag, resources, nfs.server, nfs.path, config overrides

### Files
- `Dockerfile` — multi-stage build
- `docker-compose.yaml` — quick start
- `charts/datawatch/` — Helm chart directory
- `internal/config/config.go` — `workspace_root` field
- `docs/deployment.md` — new doc for container deployment

---

## BL14: Voice Input via Whisper Transcription

**Effort:** 4-6hr | **Priority:** low

### Current state
- Signal: `IncomingMessage.DataMessage` has no Attachments field — needs to parse signal-cli's full envelope
- Telegram: only extracts `Text` from `update.Message` — ignores Voice/Audio fields
- No audio processing or Whisper integration exists

### Plan

#### Phase 1: Attachment parsing
1. **Signal**: Extend `signal/types.go` — add `Attachments []Attachment` to `DataMessage` with fields: `ContentType`, `Filename`, `Size`, `Id`, `StoredFilename`.
2. **Telegram**: In `telegram/backend.go`, check `update.Message.Voice` and `update.Message.Audio` fields. Download voice file via Telegram Bot API `getFile`.
3. **Messaging interface**: Add optional `Attachment` struct to the common message type.

#### Phase 2: Whisper transcription
1. Add `whisper` config section: `enabled`, `model` (tiny/base/small/medium), `binary` (path to whisper CLI or API URL).
2. Support two modes:
   - **Local**: `whisper` CLI (from openai-whisper pip package) — `whisper audio.ogg --model base --output_format txt`
   - **API**: OpenAI Whisper API (`POST https://api.openai.com/v1/audio/transcriptions`) or local whisper.cpp server
3. Audio pipeline: receive attachment → save to temp file → convert to WAV if needed (ffmpeg) → run whisper → get text → route as normal text command.

#### Phase 3: Integration
1. When voice message received, transcribe and process as text command.
2. Reply with transcription confirmation: "Voice: {transcribed text}" before executing.
3. If transcription fails, reply with error and save audio for manual review.

### Dependencies
- whisper CLI or API access
- ffmpeg (for audio format conversion)
- Signal-cli must be configured to download attachments

### Files
- `internal/signal/types.go` — attachment fields
- `internal/messaging/backends/telegram/backend.go` — voice handling
- `internal/transcribe/` — new package for Whisper integration
- `internal/config/config.go` — whisper config section
- `docs/messaging-backends.md` — voice input docs

---

## BL18: Prometheus Metrics Export

**Effort:** 2-3hr | **Priority:** medium

### Current state
- System stats already collected in `internal/stats/` — CPU, memory, disk, GPU, network, per-session resources
- Channel stats tracked in `internal/stats/channel.go` — per-channel message counts, bytes, errors
- No Prometheus endpoint or metric registration exists
- Web UI renders stats via WS `stats` message type

### Plan
1. **Add `/metrics` endpoint** to the public mux (no auth — standard for Prometheus scraping). Use `prometheus/client_golang` library.
2. **Gauge metrics**:
   - `datawatch_sessions_active` (labels: backend, state)
   - `datawatch_sessions_total` (counter, labels: backend, outcome)
   - `datawatch_cpu_usage_percent`
   - `datawatch_memory_usage_bytes`
   - `datawatch_disk_usage_bytes`
3. **Counter metrics**:
   - `datawatch_messages_total` (labels: channel, direction)
   - `datawatch_alerts_total` (labels: level)
   - `datawatch_inputs_total` (labels: source — web/signal/telegram/mcp)
4. **Histogram metrics**:
   - `datawatch_session_duration_seconds` (labels: backend)
   - `datawatch_api_request_duration_seconds` (labels: endpoint)
5. **Config**: `metrics.enabled: true`, `metrics.path: /metrics`
6. Wire into existing stats collection goroutine — update Prometheus gauges on each stats tick.

### Files
- `internal/metrics/` — new package, Prometheus registration
- `internal/server/server.go` — `/metrics` route
- `internal/config/config.go` — metrics config
- `go.mod` — add `prometheus/client_golang`
- `docs/operations.md` — Prometheus/Grafana setup guide

---

## BL19: Copilot/Cline/Windsurf Backends

**Effort:** 1-2hr per backend | **Priority:** low

### Current state
- Backend interface is well-defined (`llm.Backend`) with Launch, Version, Name
- 8 backends already implemented — good patterns to follow
- Claude-code and opencode have the most complex integrations (MCP, ACP)

### Plan
Each backend follows the same pattern — create `internal/llm/backends/{name}/backend.go`:

#### GitHub Copilot CLI (`copilot`)
- Binary: `github-copilot-cli` or `gh copilot`
- Launch: `gh copilot suggest -t shell "{task}"` in tmux
- Interactive: limited (single-shot suggest mode)
- Config: `copilot.enabled`, `copilot.binary`

#### Cline (VS Code extension CLI) (`cline`)
- Binary: `cline` (if CLI exists) or communicate via VS Code extension API
- Challenge: Cline is primarily a VS Code extension, not a CLI. May need to use its API or spawn VS Code headless.
- Alternative: Use Cline's underlying API provider directly (OpenAI/Anthropic/etc.) — in which case this is just a config pointing to the right API.
- Config: `cline.enabled`, `cline.api_url`, `cline.api_key`, `cline.model`

#### Windsurf (`windsurf`)
- Binary: `windsurf` CLI or Cascade API
- Similar challenge to Cline — IDE-first tool
- If CLI available: launch in tmux, detect prompts
- Config: `windsurf.enabled`, `windsurf.binary`

#### Implementation per backend
1. Create `backend.go` with `New()`, `Name()`, `Launch()`, `Version()`
2. Register in `cmd/datawatch/main.go` with `llm.Register()`
3. Add config section and `setup` wizard command
4. Add to docs/llm-backends.md
5. Test session creation and prompt detection

### Files (per backend)
- `internal/llm/backends/{name}/backend.go`
- `internal/config/config.go` — config section
- `cmd/datawatch/main.go` — registration + setup command
- `docs/llm-backends.md` — documentation

---

## BL2: Live Cell DOM Diffing

**Effort:** 3-4hr | **Priority:** low

### Current state
- Session list re-renders the entire view on every `sessions` or `session_state` WS message via `renderSessionsView()` or `onSessionsUpdated()`
- Each render replaces `view.innerHTML` with full HTML — causes flicker on mobile, resets scroll position
- Session detail view has `updateSessionDetailButtons()` for partial updates — good pattern to follow

### Plan
1. **Session list diffing**: Instead of replacing the entire session list HTML, diff the current DOM against the new session data:
   - Keep a Map of session cards by `full_id`
   - On update: add new cards, remove deleted cards, update changed fields (state badge, timestamp, prompt text) in place
   - Only create/destroy DOM elements when sessions are added/removed
2. **Implementation**:
   - Add `updateSessionCard(sess)` function that finds the existing card and patches individual elements (state badge class, time ago text, prompt line)
   - Add `reconcileSessionList(sessions)` that adds/removes cards and calls updateSessionCard for existing ones
   - Replace `renderSessionsView()` calls in `onSessionsUpdated()` with `reconcileSessionList()`
3. **Preserve scroll position**: Since we're patching in place, scroll position is maintained automatically.
4. **Animation**: Optional — add CSS transition on state badge changes for visual feedback.

### Files
- `internal/server/web/app.js` — new reconcile/update functions, replace renderSessionsView in onSessionsUpdated
- `internal/server/web/style.css` — optional transition styles

---

## BL4: Session Chaining

**Effort:** 1-2 days | **Priority:** low

### Current state
- Sessions are independent — no linking, sequencing, or conditional logic
- `ScheduleStore` supports `RunAfterID` (run after another scheduled command completes) — basic sequencing exists
- `onSessionEnd` callback fires when a session completes — hook point for triggering next session
- Session state includes `State` (complete/failed/killed) which could drive conditional branching

### Plan

#### Phase 1: Sequential chains
1. **Config/API**: New chain definition format:
   ```json
   {
     "chain": [
       {"task": "run linter", "backend": "claude-code", "dir": "/project"},
       {"task": "fix lint errors", "backend": "claude-code", "dir": "/project"},
       {"task": "run tests", "backend": "shell", "dir": "/project"}
     ]
   }
   ```
2. **Chain store**: New `ChainStore` (or extend `ScheduleStore`) to persist chain definitions.
3. **Execution**: `onSessionEnd` checks if session is part of a chain → starts next session in chain if previous completed successfully.
4. **Failure handling**: Default: stop chain on failure. Optional: `on_failure: "skip"` or `on_failure: "continue"`.

#### Phase 2: Conditional branching
1. **Exit status**: Capture session outcome (complete vs failed) and last output.
2. **Conditionals**: `{"if": "complete", "then": {...}, "else": {...}}` in chain definition.
3. **Output piping**: Optional — pass summary of previous session as context to next session's task.

#### Phase 3: Web UI
1. Chain builder in New Session tab — add steps with + button
2. Chain progress visualization — show completed/active/pending steps
3. Chain templates — save and reuse chain definitions

### Files
- `internal/session/chain.go` — ChainStore and execution logic
- `internal/session/manager.go` — onSessionEnd hook for chain continuation
- `internal/server/api.go` — `/api/chains` CRUD endpoints
- `internal/server/web/app.js` — chain builder UI
- `docs/operations.md` — chain usage guide

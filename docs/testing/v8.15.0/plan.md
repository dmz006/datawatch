# Test Plan — v8.15.0

**Version**: v8.15.0 (pending release)  
**Sprint**: Vision Input System — multi-backend image description  
**Stories**: TS-648–TS-660 (13 stories, 9 automated + 4 pending live)  
**Go unit tests**: 25 new (10 vision service, 6 server vision handler, 5 router injection, 4 manifest)

---

## Scope

The Vision Input System adds image description capability across all comms
surfaces. Images attached to messages (SMS, Matrix, email, etc.) are described
by a configurable LLM backend (ollama, openai, openai\_compat) and injected into
the session text. The Council endpoint accepts an `image_path` for pre-describing
an image that all personas receive. Skills declare `accepts_images: true` to
signal readiness for image context.

### Phase 1 — Vision service
- `internal/vision/` package: `Describer` interface, `New()` constructor, `Describe()` method
- Backends: ollama (`/api/generate` with `images`), openai/openai\_compat (`/v1/chat/completions` with `image_url`)
- `POST /api/vision/describe` multipart endpoint; gated by `CapLLMsList`
- MCP `vision_describe` tool (local file path, no upload)

### Phase 2 — Comms injection
- Router `processMessage` block: describes first image attachment, injects `[image: <desc>]` into session text
- Command-aware injection: detects `remember:` prefix and injects description into the command body (not before it) so `Parse()` still recognises the command

### Phase 3 — MCP image\_paths
- MCP tool calls accept an `image_paths []string` field; first path is described and prepended to the tool response

### Phase 4 — Skill + Council
- `AcceptsImages bool` field in skill manifests (`accepts_images: true`)
- Council `POST /api/council/run` accepts `image_path`; describes once, prepends to proposal for all personas

---

## Configuration Accessibility Validation

All access paths for vision config fields (enabled, backend, endpoint, api\_key, model, default\_prompt, max\_image\_bytes):

| Method | Status | Notes |
|---|---|---|
| YAML `config.yaml` | ✅ | `VisionConfig` in `internal/config/config.go` |
| REST GET `/api/config` | ✅ | returns all 7 vision fields |
| REST PUT `/api/config` | ✅ | `applyConfigPatch` has cases for all 7 keys |
| Web UI Settings → LLM → Vision | ✅ | `LLM_CONFIG_FIELDS` vision section in app.js |
| Comm `configure vision.model llava` | ✅ | config key passthrough in comm handler |
| MCP `config_set` | ✅ | passes config keys to same patch endpoint |
| CLI `datawatch config set vision.backend ollama` | ✅ | CLI config set subcommand |

---

## Live Test Instructions

These require a running datawatch daemon with a vision backend configured.

### TS-657 — ollama describe via API

```bash
# Prerequisites: ollama running, llava pulled
ollama pull llava

# Configure
datawatch config set vision.enabled true
datawatch config set vision.backend ollama
datawatch config set vision.endpoint http://localhost:11434
datawatch config set vision.model llava

# Test
curl -sF "image=@/path/to/test.png" http://localhost:8282/api/vision/describe
# Expected: {"description":"..."} with non-empty text
```

### TS-658 — comms image injection

```bash
# Send a message with an image attachment via any comms channel that
# supports image attachments (e.g. Matrix or Signal).
# Observe the session output — it should contain [image: <description>]
# before or within the message text.
```

### TS-659 — skill accepts_images injection

```bash
# Create a skill with accepts_images: true in SKILL.md frontmatter.
# Assign the skill to a session and send a message with an image.
# Verify the task text delivered to the skill contains [image: <description>].
```

### TS-660 — council image_path

```bash
# POST with a local image file path
curl -s -X POST http://localhost:8282/api/council/run \
  -H 'Content-Type: application/json' \
  -d '{"proposal":"What do you see?","image_path":"/path/to/test.png"}'
# Expected: all persona responses reference image content
```

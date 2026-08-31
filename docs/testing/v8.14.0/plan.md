# Test Plan — v8.14.0

**Version**: v8.14.0  
**Sprint**: BL363 — Goose full integration (T1–T4 + testing gap closure)  
**Stories**: TS-647–TS-657 (11 stories, 7 automated + 3 pending live; TS-637–TS-646 are PWA stories added in v8.8.0)  
**Go unit tests**: 22 (goose package, up from 11 in v8.13.36)

---

## Scope

BL363 delivered the Goose LLM backend across four patches (v8.13.36–v8.13.39) plus testing
gap closure (v8.14.0). This plan documents what needs live validation before the v9.0.0 cut.

### T1 (v8.13.36) — Functional backend
- `goose` interactive TUI backend: named sessions, resume, version normalization
- `goose-prompt` one-shot backend: `goose run --text`, DATAWATCH_COMPLETE detection

### T2 (v8.13.37) — Provider/model/API-key injection
- `GooseConfig.{Provider,Model,APIKeyRef}` fields in config
- `providerKeyEnvVar()` maps provider → env var (ANTHROPIC_API_KEY / OPENAI_API_KEY / etc.)
- `gooseEnvPrefix()` builds env var prefix string prepended to every launch command

### T3 (v8.13.38) — MCP channel bridge
- `GooseConfig.ChannelEnabled`: injects `GOOSE_MCP__DATAWATCH__TYPE/CMD/ARGS` env vars
- `datawatch mcp --caller-session-id <id>` flag: wires caller session into MCP server
- `mcp.Server.SetCallerSessionID()`: stored for tool-level routing

### T4 (v8.13.39) — agent-goose container
- `Dockerfile.agent-goose`: `ENV GOOSE_CLI_THEME=plain`, `/home/datawatch/.config/goose/` pre-created

### Testing gap closure (v8.14.0)
- 22 unit tests (up from 11)
- `docs/config-reference.yaml` updated with T2/T3 fields
- `docs/implementation.md` updated with GooseConfig field table
- `docs/llm-backends.md` goose section rewritten (T2/T3 YAML, env injection, both backends)
- `app.js` LLM_CONFIG_FIELDS: Goose (Block) section with 6 fields
- Minor version bump retroactive for new LLM backend

---

## Configuration Accessibility Validation

All access paths for the 4 new goose config fields (provider, model, api_key_ref, channel_enabled):

| Method | Status | Notes |
|---|---|---|
| YAML `config.yaml` | ✅ | `GooseConfig` fields in `internal/config/config.go` |
| REST GET `/api/config` | ✅ | `handleGetConfig` returns all 4 fields |
| REST PUT `/api/config` | ✅ | `applyConfigPatch` has cases for all 4 keys |
| Web UI Settings | ✅ | Goose (Block) card in `LLM_CONFIG_FIELDS` |
| Comm channel `configure` | ✅ | Routes through same PUT API |
| MCP `config_set` | ✅ | Routes through same PUT API via localhost |
| CLI `datawatch config set` | ✅ | Routes through same PUT API |

---

## Stories

| TS# | Surface | Description | Status |
|---|---|---|---|
| TS-637 | REST | GET /api/config goose section includes provider, model, api_key_ref, channel_enabled | ✅ automated |
| TS-638 | REST | PUT /api/config {goose.provider:"anthropic"} round-trips | ✅ automated |
| TS-639 | REST | PUT /api/config {goose.model:"claude-sonnet-4-6"} round-trips | ✅ automated |
| TS-640 | REST | PUT /api/config {goose.channel_enabled:true} round-trips | ✅ automated |
| TS-641 | MCP | config_set goose.provider=openai (requires allow_self_config) | ✅ automated |
| TS-642 | CLI | datawatch config set goose.model gpt-4o; GET confirms | ✅ automated |
| TS-643 | PWA | Settings → LLM → Goose (Block) section visible with 6 fields | ✅ automated |
| TS-644 | Unit | 22 goose unit tests pass | ✅ automated |
| TS-645 | Live | goose session injects GOOSE_PROVIDER/GOOSE_MODEL env vars | 📋 pending-live |
| TS-646 | Live | goose-prompt with provider+api_key_ref completes with DATAWATCH_COMPLETE | 📋 pending-live |
| TS-647 | Live | goose session with channel_enabled has GOOSE_MCP__DATAWATCH__* env vars | 📋 pending-live |

### Live test prerequisites

TS-645–647 require `goose` binary installed and a valid API key. To validate:

```bash
# Verify goose is installed
goose version

# Set config
curl -sk -XPUT http://localhost:8080/api/config \
  -H "Authorization: Bearer $DW_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"goose.provider":"anthropic","goose.model":"claude-haiku-4-5-20251001","goose.api_key_ref":"'"$ANTHROPIC_API_KEY"'"}'

# Start a goose-prompt session and check DATAWATCH_COMPLETE appears
DW_SESSION=$(curl -sk -XPOST http://localhost:8080/api/sessions \
  -H "Authorization: Bearer $DW_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"task":"echo hello","llm_backend":"goose-prompt"}' | jq -r .id)

# Wait ~30s then check output
curl -sk "http://localhost:8080/api/sessions/$DW_SESSION/output?lines=50" \
  -H "Authorization: Bearer $DW_TOKEN" | jq -r .lines[]
```

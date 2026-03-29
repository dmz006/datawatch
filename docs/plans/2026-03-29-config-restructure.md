---
date: 2026-03-29
version: 0.7.3
scope: Restructure config.yaml with grouped fields, YAML comments, inline documentation
status: planned
---

# Plan: Config File Restructuring

## Problem

The config.yaml file has grown organically with fields added across many versions. Fields lack inline comments, grouping is inconsistent, and saved configs don't include all fields with defaults. The web UI General Configuration card doesn't mirror the config structure.

## Scope

- `internal/config/config.go` — Config struct ordering, defaults, Save with comments
- `internal/config/template.go` — NEW: annotated YAML template generator
- `internal/server/web/app.js` — GENERAL_CONFIG_FIELDS grouping
- `docs/operations.md` — config reference section

## Phases

### Phase 1 — Config Struct Reordering (Planned)

- Group Config struct fields logically:
  1. **Identity**: hostname, data_dir
  2. **Session**: llm_backend, max_sessions, idle_timeout, git settings, guardrails
  3. **Server**: enabled, host, port, TLS, token
  4. **MCP**: enabled, sse_host, sse_port, TLS
  5. **Signal**: account, group, config_dir, device_name
  6. **Messaging**: telegram, discord, slack, matrix, ntfy, email, twilio, github_webhook, webhook
  7. **DNS Channel**: enabled, mode, domain, listen, upstream, secret
  8. **LLM Backends**: claude-code, aider, goose, gemini, ollama, opencode, openwebui, shell
  9. **Update**: enabled, schedule, time_of_day
- Add Go struct tags for YAML field ordering

### Phase 2 — Annotated Config Template (Planned)

- New file: `internal/config/template.go`
- `func GenerateAnnotatedConfig(cfg *Config) ([]byte, error)` — produces YAML with:
  - Section headers as YAML comments (`# ── Session ──`)
  - Inline comments for each field (`# Max concurrent sessions (0 = unlimited)`)
  - All fields present (no omitempty for config — every field visible)
  - Default values filled in for unset fields
- `config init` uses this template for new installations
- `config save` uses this template when writing (preserves comments)

### Phase 3 — Config Save Preserves Comments (Planned)

- Current `Save()` uses `yaml.Marshal` which strips comments
- Replace with template-based approach: read template, fill values, write
- Alternatively: use `gopkg.in/yaml.v3` Node API to preserve comments
- Evaluate: simplicity of template approach vs robustness of Node API

### Phase 4 — Web UI Mirror (Planned)

- `GENERAL_CONFIG_FIELDS` groups match config file groups exactly
- Section headers in web UI match YAML section comments
- Field order in UI matches field order in config file
- Add "Show raw config" button that displays the annotated YAML

### Phase 5 — Documentation (Planned)

- `docs/operations.md` — complete config reference with all fields, types, defaults
- Each field documented with: YAML key, type, default, description
- Example annotated config.yaml in docs/
- **README.md** — update the config example section to show the new grouped format
  with inline YAML comments matching the generated template
- `docs/encryption.md` — update encrypted file format if config structure changes header

## Key Files

- `internal/config/config.go` — struct reordering, tag updates
- `internal/config/template.go` — NEW: annotated YAML generator
- `internal/server/web/app.js` — GENERAL_CONFIG_FIELDS matching
- `docs/operations.md` — config reference

## Estimated Effort

1 week. Phase 1-2 are the core; Phase 3-4 are polish.

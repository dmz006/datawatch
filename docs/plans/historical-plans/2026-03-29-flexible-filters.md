---
date: 2026-03-29
version: 0.7.3
scope: Move hardcoded prompt/detection patterns to per-LLM/per-channel config
status: done
---

# Plan: Flexible Detection Filters

## Problem

Prompt detection patterns, rate limit patterns, and completion patterns are hardcoded in `internal/session/manager.go`. Different LLM backends produce different prompts (e.g., claude's "Enter to confirm", opencode-acp's "[opencode-acp] ready", ollama's ">>> "). Adding a new pattern requires a code change and rebuild.

## Scope

- `internal/session/manager.go` — promptPatterns, rateLimitPatterns, completionPatterns
- `internal/config/config.go` — per-LLM detection config
- `internal/server/api.go` — GET/PUT for detection config
- `internal/server/web/app.js` — per-LLM filter editor in Settings
- `internal/session/filter.go` — existing filter engine (extend, don't replace)

## Phases

### Phase 1 — Config Structure (Planned)

- Add `DetectionConfig` struct to each LLM config:
  ```yaml
  claude-code:
    prompt_patterns: ["Enter to confirm", "Esc to cancel", "trust this folder"]
    completion_patterns: ["DATAWATCH_COMPLETE:"]
    rate_limit_patterns: ["You've hit your limit", "rate limit exceeded"]
  ollama:
    prompt_patterns: [">>> "]
  opencode-acp:
    prompt_patterns: ["[opencode-acp] awaiting input", "[opencode-acp] ready"]
  shell:
    prompt_patterns: ["$ ", "# "]
  ```
- Global defaults in `DefaultConfig()` matching current hardcoded patterns
- Per-LLM overrides merge with (not replace) global defaults

### Phase 2 — Manager Uses Config Patterns (Planned)

- `monitorOutput` and `processOutputLine` read patterns from session's LLM config
- Session struct stores `LLMBackend` — use it to look up the right pattern set
- Manager receives pattern config at startup from main.go
- Hardcoded `promptPatterns`/`rateLimitPatterns`/`completionPatterns` become fallback defaults

### Phase 3 — API and Web UI (Planned)

- Config GET/PUT returns per-LLM detection patterns
- Settings > LLM Configuration > Edit popup shows pattern lists (textarea, one per line)
- Global defaults section in General Configuration
- "Reset to defaults" button per LLM

### Phase 4 — Per-Channel Detection (Planned)

- Messaging backends can have their own detection overrides
- E.g., DNS channel might have different response truncation patterns
- Config structure: `messaging.<backend>.detection_overrides`

### Phase 5 — AGENT.md Rule (Planned)

- Add rule: "Do not hardcode prompt, completion, or rate limit patterns in Go source.
  All detection patterns must be in config and editable via Settings."
- Remove hardcoded patterns from manager.go
- Seed defaults via `datawatch seed` like saved commands

## Key Files

- `internal/config/config.go` — DetectionConfig per LLM
- `internal/session/manager.go` — pattern lookup by LLM backend
- `internal/server/api.go` — pattern GET/PUT
- `internal/server/web/app.js` — pattern editor in LLM edit popup

## Estimated Effort

1-2 weeks. Phase 1-2 are the core changes; Phase 3-4 are UI polish.

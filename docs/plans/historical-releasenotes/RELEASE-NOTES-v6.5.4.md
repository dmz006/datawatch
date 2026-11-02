# Release Notes — v6.5.4 (BL251 — Agent auth/settings injection)

Released: 2026-05-04
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.5.4
Smoke: 91/0/6

## Summary

Patch release completing **BL251** — agent auth/settings injection for claude-code and opencode containers. `AgentSettings` on `ProjectProfile` resolves `ANTHROPIC_API_KEY` from the secrets store at spawn time (claude-code) and injects `OPENCODE_PROVIDER_URL`/`OPENCODE_MODEL` (opencode). Full 7-surface parity.

## Added

- **`internal/profile/project.go`** — `AgentSettings` struct on `ProjectProfile` with `claude_auth_key_secret`, `opencode_ollama_url`, `opencode_model` fields.
- **`internal/agents/spawn.go`** — at spawn time, resolves `ClaudeAuthKeySecret` from the secrets store and injects `ANTHROPIC_API_KEY`; injects `OPENCODE_PROVIDER_URL` and `OPENCODE_MODEL` for opencode agents.
- **`internal/server/profile_api.go`** — `PATCH /api/profiles/projects/{name}/agent-settings` endpoint for targeted update of the AgentSettings block only.
- **`internal/mcp/profile_tools.go`** — `profile_set_agent_settings` MCP tool.
- **`cmd/datawatch/profile_cli.go`** — `datawatch profile project agent-settings <name>` CLI subcommand with `--claude-key-secret`, `--ollama-url`, `--model` flags.
- **`internal/router/profile.go`** — `profile project agent-settings <name> [key=value ...]` comm verb.
- **`internal/server/web/app.js`** — Agent Settings fields (Claude auth key secret, Ollama URL, model) in project profile editor form.
- **Locale** — `profile_agent_settings_section`, `profile_claude_key_secret_label`, `profile_claude_key_secret_ph`, `profile_ollama_url_label`, `profile_ollama_url_ph`, `profile_ollama_model_label`, `profile_ollama_model_ph` in all 5 bundles.
- **Tests** — 5 new BL251 unit tests (`internal/agents/bl251_agent_settings_test.go`); 1714 total.

## See also

CHANGELOG.md `[6.5.4]` entry.

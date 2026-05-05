# Release Notes — v6.4.3 (BL242 Phase 4 — `${secret:name}` config refs + spawn injection)

Released: 2026-05-03
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.4.3

## Summary

Patch release delivering **BL242 Phase 4**: `${secret:name}` reference resolution in config files and spawn-time env-var injection. Closes the original BL242 backlog scope (Phases 5a/5b/5c follow as additional work in v6.4.5–v6.4.7).

## Added

- **`internal/secrets.ResolveRef(s, store)`** — replaces all `${secret:name}` tokens in a string with values from the active store. Name chars: alphanumeric, `_`, `-`, `.`.
- **`internal/secrets.ResolveMapRefs(m, store)`** — resolves tokens in a `map[string]string`, returning a new map (original untouched). Accumulates partial errors and continues.
- **`internal/secrets.ResolveConfig(v, store)`** — reflection-based walker that resolves tokens in all exported string fields and `map[string]string` values of any struct (used on the full daemon config at startup).
- **Config resolution at startup** — after the secrets store is wired, `main.go` calls `ResolveConfig(cfg, store)`. Every YAML field — LLM API keys, messaging tokens, webhook URLs — can now reference `${secret:my-key}` instead of storing plaintext.
- **Spawn-time env injection** (`agents.Manager.SecretsStore`) — at `Spawn()` time, `project.Env` is resolved into `Agent.EnvOverride` via `ResolveMapRefs`. Docker and k8s drivers use `EnvOverride` when non-nil, leaving the shared `ProjectProfile` unmodified.

## Example

```yaml
# datawatch.yaml — reference secrets instead of hardcoding
telegram:
  token: "${secret:telegram-bot-token}"

ollama:
  api_key: "${secret:ollama-key}"
```

```yaml
# project profile env
env:
  ANTHROPIC_API_KEY: "${secret:anthropic-key}"
  GITHUB_TOKEN: "${secret:gh-pat}"
```

## See also

CHANGELOG.md `[6.4.3]` entry.

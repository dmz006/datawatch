# datawatch v8.6.1 — Release Notes

**Released:** 2026-05-20
**Previous release:** v8.6.0 (2026-05-20)
**Type:** Patch — bug fixes and the BL241 Matrix backend (P1 items only)

---

## What v8.6.1 Is

v8.6.1 is a patch release that ships the BL241 Matrix backend (P1 feature tier: cleartext-only foundation) and two bug fixes discovered during its integration. No breaking changes, no new configuration requirements beyond the Matrix block which is opt-in.

---

## Highlights

- **Matrix backend (BL241 P1)** — Send and receive messages through a Matrix homeserver. Cleartext-only (E2EE is P2). Full 7-surface parity where applicable.
- **`ValidateSecrets` fix** — Matrix was silently disabled for any operator using the secrets store. Fixed.
- **Instance-scoped Claude config (BL318)** — Test daemon instances can no longer corrupt the production operator's `~/.claude.json` or `~/.mcp.json`.

---

## New Features

### Matrix Backend (BL241 P1)

Connect datawatch sessions to a Matrix homeserver. The daemon joins a configured room and routes messages bidirectionally between Matrix and datawatch sessions.

**Configuration** (`~/.datawatch/config.yaml`):

```yaml
matrix:
  enabled: true
  homeserver: https://matrix.example.com
  user_id: "@bot:example.com"
  access_token: "${secret:matrix-access-token}"   # recommended
  room_id: "!roomid:example.com"
```

**Store the token securely:**
```bash
datawatch secrets set matrix-access-token <token>
```

**What ships in P1:**
- Cleartext send and receive (E2EE is P2)
- Alias resolution: `#room:server` → `!roomid:server`
- Bridge classifier: Matrix traffic identified correctly in session telemetry
- `m.datawatch.session` tag on every outbound message: `{"role":"output","host":"<hostname>"}`
- Observer status surface, PWA status card, 7-surface REST/MCP/CLI parity

**Matrix integration CI** — `.github/workflows/matrix-integration.yaml` runs `scripts/test-matrix-synapse.sh` against a real Docker Synapse instance on every push touching Matrix code. 13/13 checks.

---

## Bug Fixes

### `ValidateSecrets` ordering (BL241)

`ValidateSecrets(cfg.Matrix.AccessToken)` was called after `ResolveConfig()`. By that point the `${secret:…}` reference had already been resolved to the real token, which never starts with `${secret:`, so the check always returned `ErrPlaintextToken` and disabled the backend.

**Impact:** Any operator who stored their Matrix access token in the secrets store (the documented recommendation) would find Matrix silently disabled on every daemon start.

**Fix:** Moved the `ValidateSecrets` call to before `ResolveConfig()` so it sees the raw config value.

### Instance-scoped Claude config (BL318)

`SweepUserScopeMCPConfig` and `claude mcp add` both targeted `$HOME`-level files (`~/.mcp.json`, `~/.claude.json`) on every session spawn. On a shared host running multiple daemon instances (production + test), each instance overwrote the other's MCP registration.

**Impact:** Running an e2e test daemon replaced the production operator's MCP server entry, causing Claude Code to silently connect to the test instance.

**Fix:**
- `WriteInstanceMCPConfig` writes to `$DATAWATCH_DATA_DIR/.mcp.json` (instance-owned)
- `SetClaudeConfigDir` injects `CLAUDE_CONFIG_DIR=$DATAWATCH_DATA_DIR/.claude` into every `claude mcp add/remove/list` subprocess
- Production default (`~/.datawatch/`) is unchanged; only non-default data dirs gain isolation

---

## Upgrade Notes

No migration required. The Matrix block is opt-in. BL318 is transparent — existing `~/.mcp.json` and `~/.claude.json` are not modified on upgrade; new writes go to the instance data dir.

Operators who previously had Matrix configured but found it silently disabled should restart their daemon — the backend will now start correctly if `access_token` is stored in the secrets vault.

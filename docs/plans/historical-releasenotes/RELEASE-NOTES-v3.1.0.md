# Release Notes — v3.1.0

**2026-04-19.** Bug fix + test-infrastructure minor. Clears the last
open bug from v3.0.0 and adds three test-infrastructure building blocks
that unblock deeper coverage for the rest of the v3.x series.

---

## Highlights

- **B30 fix** — Scheduled commands no longer require a 2nd Enter to
  execute in TUI backends (claude-code, ink-based). Root cause: the
  parent fired `tmux send-keys <text> Enter` the instant the
  `waiting_input` state transition was observed, but the TUI was still
  finishing its prompt setup (phase 1: print prompt → phase 4: accept
  stdin). Fix: `TmuxManager.SendKeysWithSettle` splits the literal-push
  and the Enter into two separate `tmux send-keys` calls with a
  configurable delay between them. `Manager.SendInput` uses the settle
  variant when `source == "schedule"`.
- **BL89 — Mock session manager.** Extracted `TmuxAPI` interface from
  `TmuxManager`; added `FakeTmux` that records every call in memory.
  Tests can now construct a real `session.Manager` without a tmux
  server via `mgr.WithFakeTmux()`.
- **BL90 — httptest API test infrastructure.** Real `Server` + real
  `session.Manager` + `FakeTmux` → fast, hermetic tests covering
  health, info, sessions, config get/put, devices register/list/delete,
  federation, and more.
- **BL91 — MCP handler tests.** Direct handler invocation (no
  stdio/SSE transport needed). Covers `list_sessions`, `get_version`,
  `send_input` error paths, `rename_session` validation.

---

## Bugs fixed

| ID | Symptom | Root cause | Fix |
|---|---|---|---|
| B30 | Scheduled command lands in prompt but needs a 2nd Enter | claude-code/ink TUIs aren't ready for input at the moment their `waiting_input` state fires; tmux's one-shot `send-keys <text> Enter` swallowed the Enter as prompt-setup | `SendKeysWithSettle` two-step send; `session.schedule_settle_ms` config (default 200 ms) |

---

## Backlog items shipped

| ID | What | Why |
|---|------|-----|
| BL89 | TmuxAPI interface + FakeTmux | Tests can spin up a real `session.Manager` with no tmux server |
| BL90 | httptest-driven API coverage (9 tests) | Fast, hermetic regression coverage for REST endpoints |
| BL91 | Direct MCP handler coverage (6 tests) | Catches handler regressions without running the stdio/SSE transport |

---

## Configuration changes

One new field, fully parity-wired (YAML + REST + MCP /api/config + UI):

```yaml
session:
  # B30 (default 200 ms). 0 = legacy one-shot send-keys.
  schedule_settle_ms: 200
```

Existing configs migrate transparently: the loader defaults 0 → 200.
Set to 0 if you hit a regression; file an issue.

---

## Container images

Per the new "Container maintenance" rule in the backlog:

| Image | Change | Action |
|---|---|---|
| `parent-full` | Daemon behaviour changed (B30 scheduler settle path) | **Rebuild required** |
| `agent-base`, `agent-claude`, `agent-gemini`, `agent-aider`, `agent-opencode`, `validator` | No change | No rebuild |
| `lang-python`, `lang-go`, `lang-node`, `lang-rust`, `lang-kotlin`, `lang-ruby` | No change | No rebuild |
| `tools-ops` | No change | No rebuild |

Rebuild command (operator-pass, requires `.env.build` with `REGISTRY`):

```bash
make container-parent-full PUSH=true CONTAINER_TAG=v3.1.0
```

Helm chart bumped: `version: 0.3.0`, `appVersion: v3.1.0`.

---

## Testing

- **965 tests / 47 packages**, all passing (+22 vs. v3.0.0).
  New: 3 B30, 4 BL89, 9 BL90, 6 BL91.
- `go test ./cmd/datawatch/... ./internal/...` is the full-suite command.

---

## Upgrading from v3.0.0 / v3.0.1

### Single host

```bash
datawatch update
```

The v3.0.1 updater fix (B31) is required for this to succeed — if
you're on v3.0.0, upgrade to v3.0.1 first, then run `datawatch update`
again for v3.1.0. State (`~/.datawatch/`) carries forward.

### Cluster (Helm)

```bash
helm upgrade dw datawatch/datawatch \
  -n datawatch \
  -f my-values.yaml \
  --set image.tag=v3.1.0
```

No breaking `my-values.yaml` changes.

---

## What's next (per group-release plan)

- **v3.2.0 — Intelligence group**: BL24 autonomous task decomposition,
  BL25 independent verification, BL28 quality gates, BL39 circular
  dependency detection. (Depends on F15 pipelines; BL24 is the
  anchor.)
- **v3.3.0 — Observability group**: BL10 session diffing, BL11 anomaly
  detection, BL12 historical analytics + charts, BL86 remote GPU
  stats.
- **v3.4.0 — Operations group**: BL17 hot config reload (SIGHUP),
  BL22 RTK auto-install, BL37 system diagnostics, BL87 `datawatch
  config edit`.

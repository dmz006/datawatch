# State paths follow the configured data directory: plan

**Date:** 2026-11-07
**Version:** next patch above main (no number; the merge helper records the version in the changelog)

## Scope

- `internal/server/` — all state-path resolutions migrate to `Server.statePath()`
- `cmd/datawatch/` — startup migration, tmux socket env, secrets provider wiring
- `internal/session/` — tmux socket directory derived from data dir
- `internal/secrets/` — file provider default path via data dir
- `internal/autonomous/` — default worktree directory via data dir
- `internal/config/` — `DefaultPRDWorktreeDirFor(dataDir)` helper
- `internal/router/` — tmux socket path in route wiring
- Test and live-check files (guard, isolation, per-handler pins, live script)

## What this delivers

Every datawatch-owned state file and directory that the daemon reads or writes now
resolves through the operator's configured `data_dir` (the `cfg.DataDir` field, or
its default `$HOME/.datawatch` when `data_dir` is unset). Before this change, most
handlers constructed paths directly from `os.UserHomeDir()` or
`os.Getenv("DATAWATCH_DATA_DIR")`, falling back to `$HOME/.datawatch` regardless
of the configured data directory. The result: an operator who set `data_dir` to
an external volume still found discussion WALs, dashboard layout, smoke-run
records, merge pipeline records, usage readings, encryption state and secrets
written under the process home directory.

A single helper — `Server.statePath(name…)` backed by `dataDirPath(cfg.DataDir)`
— is the sole resolver. The static guard test fails on any new `os.UserHomeDir`
or `os.Getenv("HOME")` call in `internal/server/*.go` that is not in a closed
allow-list of explicitly documented operator-owned reads. The isolation test
starts the full handler surface against a decoy HOME and requires the decoy tree
to be byte-identical after. The live check script proves it end-to-end against a
sandbox daemon.

## Inventory of migrated state paths

Every entry below resolved from the process home directory (or an environment
variable) before this change and now resolves through `Server.statePath()` or
the equivalent in the startup path (`expandHome(cfg.DataDir)`). The "old source"
column names the exact expression the code used; the "new source" column names
the replacement.

| File (package) | Function / entry point | Old source (home-derived) | New source (data-dir-derived) |
|---|---|---|---|
| `internal/server/state_paths.go` (new) | `Server.statePath()`, `dataDirPath()` | — | single-source builder: `s.dataDir` → `cfg.DataDir` → `config.DefaultConfig().DataDir`, tilde-expanded only when the operator wrote `~` |
| `internal/server/api_dashboard_layout.go` | `dashLayoutPath` | `filepath.Join(os.UserHomeDir(), ".datawatch", "dashboard-layout.json")` | `s.statePath("dashboard-layout.json")` |
| `internal/server/channel_routing.go` | `channelRoutingPath` | `filepath.Join(os.UserHomeDir(), ".datawatch", "channel_routing.json")` | `s.statePath("channel_routing.json")` |
| `internal/server/api_smoke_progress.go` | `smokeRunsDir` | `filepath.Join(os.UserHomeDir(), ".datawatch", "smoke-runs")` | `s.statePath("smoke-runs")` |
| `internal/server/api_smoke_progress.go` | `legacyProgressPath` | `filepath.Join(os.UserHomeDir(), ".datawatch", "smoke-progress.json")` | `s.statePath("smoke-progress.json")` |
| `internal/server/merge_pipeline.go` | `mergePipelineRecords` | `mergepipeline.NewRecordStore(mergeDataDir())` where `mergeDataDir()` read `DATAWATCH_DATA_DIR` env then `$HOME/.datawatch` | `mergepipeline.NewRecordStore(s.statePath())` |
| `internal/server/merge_run.go` | `newMergeRunner` (record store) | same `mergeDataDir()` | `dataDir` parameter from `s.statePath()` at the call site |
| `internal/server/supervisor_loop_wiring.go` | `supervisorLoopDeploy`, `supervisorLoopMerge` | `mergeDataDir()` (env + home) | `dataDirPath(cfg.DataDir)` (no Server in scope) |
| `internal/server/usage_readings.go` | `usageDataDir` | `cfg.DataDir` if set, else `filepath.Join(os.UserHomeDir(), ".datawatch")` | `s.statePath()` |
| `internal/server/bl332_discussion_scope.go` | `discussionsBaseDir`, `discussionAppendWALEntry`, `discussionReadWAL` | `filepath.Join(os.UserHomeDir(), ".datawatch", "discussions")` | `s.statePath("discussions")` / `s.statePath("discussions", id)` |
| `internal/server/bl332_discussion_sync.go` | `discussionParticipantsPath`, `discussionReadParticipants`, `discussionWriteParticipants`, `discussionDetectConflicts` | `discussionsBaseDir()` → `$HOME/.datawatch/discussions` | `s.statePath("discussions", id)` |
| `internal/server/bl334_security.go` | `handleSecurityEncryptionStatus`, `handleSecurityEncryptionMigrate`, `handleSecurityWipePlaintext` | `filepath.Join(os.UserHomeDir(), ".datawatch")` (base), then `filepath.Join(base, "discussions")` | `base := s.statePath()`; `s.statePath("discussions")` |
| `internal/server/api.go` | `handleStartSession` (clone root) | `$HOME/.datawatch/workspaces` via home | `s.statePath("workspaces")` |
| `internal/server/migration.go` | `handleMigrationStatus` | `$HOME/.datawatch/v7-migration-status.json` | `s.statePath("v7-migration-status.json")` |
| `internal/server/v52710_channel_info.go` | `handleChannelInfo` | `os.Getenv("DATAWATCH_DATA_DIR")` then `$HOME/.datawatch` (2 env reads removed) | `s.statePath(...)` for each sub-path |
| `cmd/datawatch/main.go` | startup discussion WAL migration | `filepath.Join(home, ".datawatch", "discussions")` | `startupDiscussionsDir(cfg.DataDir)` → `filepath.Join(expandHome(cfg.DataDir), "discussions")` |
| `cmd/datawatch/main.go` | secret file provider default | `secretspkg.NewFileProvider(filepath.Join(expandHome(cfg.DataDir), "secrets"))` (old) / previously `~/.datawatch/secrets` in `NewFileProvider` | `secretspkg.DefaultFileProviderDir(cfg.DataDir)` → `<data_dir>/secrets` |
| `internal/secrets/file.go` | `DefaultFileProviderDir(dataDir)` (new) | `NewFileProvider` internally defaulted to `$HOME/.datawatch/secrets` | `DefaultFileProviderDir` exported; `NewFileProvider` no longer silently defaults to home |
| `internal/session/tmux.go` | tmux socket path | `TMUX_TMPDIR` env / `$TMPDIR` / `os.TempDir()`, no data-dir awareness | socket dir derived from `m.DataDir()` (session manager's configured data dir), respecting the existing `TMUX_TMPDIR` operator override |
| `internal/router/router.go` | tmux socket env wiring | `ensureTmuxTmpdirEnv()` ran before config load, using bare env | moved after config load in `main.go`; receives expanded data dir |
| `internal/autonomous/worktree.go` | `defaultWorktreeDir(dataDir)` | no-arg: `$HOME/.datawatch/prd-worktrees` | now takes `dataDir` param; delegates to `config.DefaultPRDWorktreeDirFor(dataDir)` |
| `internal/config/config.go` | `DefaultPRDWorktreeDirFor(dataDir)` (new) | `DefaultPRDWorktreeDir()` → `$HOME/.datawatch/prd-worktrees` | `DefaultPRDWorktreeDirFor`: `expandHome(dataDir)` + `/prd-worktrees`; empty dataDir falls through to `DefaultPRDWorktreeDir()` |
| `internal/autonomous/manager.go` | `DeletePRD` | `config.DefaultPRDWorktreeDir()` | `defaultWorktreeDir(m.dataDir)` |
| `internal/autonomous/task_worktree.go` | `ensureTaskWorktreeFor` (2 call sites) | `defaultWorktreeDir()` (home) | `defaultWorktreeDir("")` / `defaultWorktreeDir(m.dataDir)` |
| `internal/autonomous/worktree_cleanup.go` | worktree cleanup path | `config.DefaultPRDWorktreeDir()` | `defaultWorktreeDir(m.dataDir)` |
| `internal/autonomous/verify_script_saved.go` | verify script path | `defaultWorktreeDir()` | `defaultWorktreeDir("")` (data dir comes from config default when empty) |
| `internal/autonomous/worktree.go` | worktree creation (2 call sites) | `defaultWorktreeDir()` (home) | `defaultWorktreeDir("")` / `defaultWorktreeDir(m.dataDir)` |

## Home reads that stay on purpose

These reads are **not** datawatch daemon state. They either belong to an external
tool the daemon reads from, are operator-facing project paths, or are historical
legacy that has no data-directory equivalent. Each is in the static guard
allow-list (`statePathAllowList` in `state_paths_guard_test.go`) with a
one-line reason, and a line that matches no current call is a stale-entry
failure:

| File | Function / site | Home read | Why it stays |
|---|---|---|---|
| `internal/server/api.go` | `handleStartSession`, `handleWS` | `os.UserHomeDir()` for the default project directory | Operator's own workspace: the project directory the operator chose (or the default). Not daemon state; changing it would relocate the operator's source tree. |
| `internal/server/api.go` | `handleFiles`, `handleFilesMkdir`, `handleFilesDownload` | `os.UserHomeDir()` for file browser roots | File browser operates on the operator's filesystem, not on daemon state. |
| `internal/server/bl333_file_service.go` | `sessionRoot`, `fileServiceRoot`, upload/delete handlers | `os.UserHomeDir()` fallback for user file roots | User file roots are operator-chosen folders (`FileServiceRoot`/`RootPath` config keys) or their `~/…` tilde prefixes; the home fallback is the operator's own workspace. |
| `internal/server/session_progress.go` | `piSessionsRootServer` | `os.UserHomeDir()` → `~/.pi/agent/sessions` | Pi tool's own session log directory (`PI_CODING_AGENT_DIR` env override checked first). This is a third-party tool's path, not datawatch state. |
| `internal/server/v52710_channel_info.go` | `scanStaleMCPJSON` | `$HOME/.mcp.json` | Read-only staleness check of the operator's own project MCP config file. Historical layout; not daemon state. |
| `internal/config/config.go` | `ConfigPath()` | `~/.datawatch/config.yaml` | The config file itself lives in `$HOME/.datawatch/` by convention (the operator edits it there). The daemon reads it from there; it is not state the daemon writes. |
| `internal/config/config.go` | `DefaultConfig().DataDir` | `filepath.Join(home, ".datawatch")` as **default** value | The default data directory is `~/.datawatch` when the operator has not specified one. The static guard flags this as the known unresolved blocker: until the default is home-free (or operator-declared), the isolation test remains deliberately red on this path. |
| `internal/config/config.go` | `expandHome()` helper | `os.UserHomeDir()` for `~/` expansion | Utility used throughout config resolution; expands user-written `~` prefixes. Not a state path in itself. |
| `internal/config/config.go` | `DefaultProjectDir` default | `home` (operator's home directory) | Default project directory is the operator's workspace, not daemon state. |
| `internal/config/config.go` | Signal ConfigDir | `~/.local/share/signal-cli` | signal-cli's own default config directory, set by the tool's own convention. External tool state. |
| `internal/llm/backends/pi/backend.go` | `agentSourceDir()` | `~/.pi/agent` | Pi coding agent's own config directory. External tool convention. |
| `internal/llm/backends/goose/backend.go` | goose home dir | `~/.goose` (or equivalent) | Goose's own config directory. External tool convention. |
| `internal/llm/backends/opencode/backend.go` | opencode home dir | `~/.config/opencode` | OpenCode's own config directory. External tool convention. |
| `internal/gitguard/policy.go`, `projectdir.go` | `homeResolver` | `os.UserHomeDir` | Git guard policy and project directory resolution reference the operator's home for relative path resolution in guard rules. |
| `internal/memory/hooks.go` | hook state, transcript path | `$HOME/.datawatch/hook_state`, `TRANSCRIPT_PATH` expansion | Memory hook state file and transcript path. These are tied to the operator's home because they are written by the operator's CLI (Claude hook scripts), not by the daemon's server package. |
| `internal/mcp/search/server.go` | search MCP websearch | HOME-based paths | The search MCP bridge runs as a child process and uses the operator's home for its own sqlite DB; documented in the live-check byproduct allow-list. |
| `internal/observerpeer/client.go` | observer peer | HOME | Observer peer state tied to the operator's home for its own conventions. |
| `internal/channel/channel.go` | npm lookup candidates | `$HOME/.nvm`, etc. | Looking up the operator's Node.js installation to find `npm` for auxiliary bridges. Not daemon state. |

## Parity surface

This change moves **where the daemon stores its state files**. It adds no new
operator-visible setting, no new REST endpoint, no new MCP tool, no new CLI
command, no new comm channel command, no new YAML config key, and no new PWA,
Android, or iPhone affordance.

| Surface | New in this change? | Statement |
|---|---|---|
| REST | No | `GET` and `PUT` response shapes for channel routing, dashboard layout, smoke progress, discussion scopes, encryption status, usage readings, merge pipeline, and migration status are unchanged; only the on-disk location of the backing files moved. |
| MCP | No | No new MCP tool. Existing tools that read the state files (e.g. burn guard, usage) are unaffected because the Server resolves paths internally. |
| CLI | No | No new CLI command. The `datawatch start` command wires the same config to the same internal helpers. |
| Comm channel | No | No new comm-channel command. The `configure` command does not gain a key; `data_dir` already existed. |
| YAML config | No | No new key. `data_dir` (or `session.data_dir`) already exists in `docs/config-reference.yaml` and the parsed `config.Config.DataDir` field. |
| PWA | No change | No new card, no new settings field, no new locale key. |
| Android | No change | No new client-visible affordance. |
| iPhone | No change | No new client-visible affordance. |

No new setting and no new card means no help entry, no locale key, and no
config-reference addition are required.

## Phases and status

| # | Phase | Status |
|---|---|---|
| 1 | State path helper: `Server.statePath()`, `dataDirPath()`, `dataDir` field on Server, `statePathAllowList` guard foundation | Done |
| 2 | Dashboard layout, channel routing, dashboard card paths → `statePath` | Done |
| 3 | Smoke progress and smoke run paths → `statePath` | Done |
| 4 | Discussion WAL store (read, write, delete, list) and discussion sync (participants, conflicts, sync) → `statePath` | Done |
| 5 | Merge pipeline record paths, merge runner record store, supervisor loop deploy/merge → `statePath` / `dataDirPath` | Done |
| 6 | Usage readings, usage lanes, burn guard state → `statePath` | Done |
| 7 | Encryption status, migrate and wipe handlers → `statePath` | Done |
| 8 | Migration status, workspace clone root, channel info handlers, startup discussion WAL migration, `DATAWATCH_DATA_DIR` env reads removed | Done |
| 9 | Secrets file provider default → `DefaultFileProviderDir(dataDir)`; `NewFileProvider` no longer home-defaults | Done |
| 10 | Tmux socket directory fallback → configured data directory; `ensureTmuxTmpdirEnv` moved after config load | Done |
| 11 | Default automaton/PRD worktree directory → `DefaultPRDWorktreeDirFor(dataDir)`; `defaultWorktreeDir(dataDir)` signature change; all autonomous call sites updated | Done |
| 12 | Static guard test: AST scan of `internal/server/*.go` for `os.UserHomeDir` / `os.Getenv("HOME")` against the closed allow-list; known blocker documented | Done |
| 13 | Isolation test: full handler surface against a decoy HOME, byte-identity requirement | Done |
| 14 | Per-handler pin tests (discussions, dashboard, smoke, merge, usage, security, startup, api.go) | Done |
| 15 | Live check script: sandbox daemon with throwaway home, four GET endpoints, byte-identical decoy verification | Done |
| 16 | Per-handler pin tests for the remaining server files (discussion scope/sync, api.go, channel routing, smoke, usage, security, merge pipeline, startup) | Done |

All sixteen phases are done and reflected in the branch's 19 task commits
(`fdac0001`).

## Verification gates

- `go build ./...` — clean.
- `rtk go test ./internal/server/ ./internal/secrets/ ./internal/autonomous/ ./internal/config/ ./cmd/datawatch/` — all pin tests, guard test, isolation test pass (guard test is intentionally red on the known `dataDirPath` home-default blocker; see guard test header).
- `bash scripts/state-data-dir-live-check.sh` — sandbox daemon with throwaway HOME serves state from the configured data directory; decoy HOME byte-identical; `RESULT: PASS`.
- Static guard: `TestStatePathGuardNoHomeStateOutsideAllowList` — every non-allow-listed `os.UserHomeDir` call in `internal/server/*.go` is a blocker.

## No-edit list

Per the task rules, this plan document intentionally does **not**:

- Edit `CHANGELOG.md` (auto-generated by the daemon at release time).
- Edit `docs/testing-tracker.md` or the registry.
- Add or bump any version constant in `internal/server/api.go` or `cmd/datawatch/main.go`.

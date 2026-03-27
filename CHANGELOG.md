# Changelog

All notable changes to datawatch will be documented here.
Format based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [Unreleased]

### Planned
- Native Go Signal backend (libsignal-ffi)
- Container images and Helm chart
- Test suite

## [0.5.5] - 2026-03-27

### Fixed
- **Windows cross-compile**: moved `daemonize()` to `daemon_unix.go` (`//go:build !windows`) and `daemon_windows.go` so the Windows binary builds without the Unix-only `syscall.SysProcAttr.Setsid` field. Pre-built Windows/amd64 binary now included in releases.

---

## [0.5.4] - 2026-03-27

### Added
- **`datawatch diagnose [signal|telegram|discord|slack|all]`**: connectivity diagnostic command for all messaging backends. Signal diagnose lists all known groups and validates the configured `group_id`; Telegram, Discord, Slack check live API connectivity. `--send-test` flag sends a test message to verify outbound delivery.
- **`datawatch diagnose --send-test`**: sends a test message to the configured group/channel to verify end-to-end delivery.
- **Signal backend stderr capture**: signal-cli's stderr is now always piped and logged (`[signal-cli stderr] ...`). Java startup errors, auth failures, and exceptions are now visible in daemon.log.
- **Signal backend verbose logging**: `datawatch start -v` now logs every raw JSON-RPC line sent/received from signal-cli, and full pretty-printed notifications for debugging.
- **Signal 4MB scanner buffer**: increased from 64KB default to 4MB to handle large group messages without silent truncation.
- **Signal self-filter fix**: normalises both sides of the number comparison with `strings.TrimPrefix("+")` to avoid format mismatch false-positives/negatives; uses `EffectiveSource()` (preferring `sourceNumber` in signal-cli v0.11+ responses).
- **Signal `EffectiveSource()`**: `Envelope.EffectiveSource()` returns `sourceNumber` (populated in signal-cli v0.11+) if set, falling back to `source`.
- **Signal scanner error logging**: `readLoop` now logs scanner errors explicitly instead of silently exiting.
- **Installer `--version X.Y.Z`**: install script now accepts `--version X.Y.Z` to install a specific release instead of the latest.

### Changed
- **Signal notification dispatch**: non-data envelopes (receipts, typing, sync) are logged in verbose mode and silently skipped; previously they caused confusing empty-message drops.
- **AGENT.md**: added "Supported Commands / Notification Events" requirement to the "New messaging/communication interface" rule; added interactive input, filter, and saved command documentation requirements to the "New LLM backend" rule.
- **`docs/messaging-backends.md`**: added "Command Support by Backend" comparison table; added "Supported Commands" section to Signal, Telegram, Discord, Slack, Matrix, and Twilio; added "Notification Events" table to ntfy and Email.
- **`docs/llm-backends.md`**: added "Command and Filter Support" section covering saved commands, output filters, and interactive input support by backend.

---

## [0.5.3] - 2026-03-27

### Changed
- **AGENT.md**: Added "Functional Change Checklist" under Release Discipline — after any functional change, bump version, run `make release-snapshot` to verify build, verify `datawatch update --check` reports the new version, and confirm install script downloads the prebuilt binary. Documents that `datawatch update --check` (CLI) and `update check` (messaging) are the canonical ways to check for available upgrades.

---

## [0.5.2] - 2026-03-27

### Fixed
- **install.sh**: prebuilt binary is now tried first; Go source build is only used as a fallback when the release archive download fails. Previously, if Go was installed on the host, the installer skipped the prebuilt download entirely and built from source (downloading Go if it wasn't new enough), which was slow and unnecessary.

---

## [0.5.1] - 2026-03-27

### Fixed
- **install.sh**: version is now resolved dynamically from the GitHub releases API at install time (was hardcoded as `"0.1.0"`); falls back to `"0.5.0"` if the API is unreachable.
- **install.sh**: Go fallback installer no longer fails with `mv: cannot overwrite` when the Go versioned directory already exists (`~/.local/go-X.Y.Z` is removed and re-extracted cleanly).
- **install.sh**: prebuilt binary download now uses GoReleaser archive format (`datawatch_VERSION_linux_ARCH.tar.gz`) instead of the old bare-binary URL that was never published.

---

## [0.5.0] - 2026-03-27

### Added
- **`alerts` messaging command**: send `alerts [n]` to any messaging backend to view recent alert history; alerts are also broadcast proactively to all active messaging backends when they fire.
- **`setup llm <backend>`**: CLI and messaging-channel setup wizards for all LLM backends — `claude-code`, `aider`, `goose`, `gemini`, `opencode`, `ollama`, `openwebui`, `shell`.
- **`setup session`**: wizard to configure session defaults (LLM backend, max sessions, idle timeout, tail lines, project dir, skip permissions).
- **`setup mcp`**: wizard to configure the MCP server (enable, SSE, host, port, TLS, token).
- **`datawatch test [--pr]`**: collects non-sensitive interface status (endpoints, binary paths, model names) for all enabled interfaces and optionally opens a GitHub PR updating `docs/testing-tracker.md`.
- **GoReleaser integration**: `.goreleaser.yaml` added; `make release` creates GitHub releases with pre-built binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64.
- **AGENT.md rules**: added rules for new communication interfaces (testing tracker, data flow docs, config field documentation, security options), and for release discipline (GoReleaser, PR check before commit/release).
- **DNS channel design**: expanded `docs/covert-channels.md` with command subset, query format specification, Mermaid sequence diagram, config block, security model, and threat model table.
- **`docs/testing-tracker.md`**: expanded with LLM backend rows, Web/API section, validation checklists per interface type, and DNS channel row.

### Changed
- `BACKLOG.md`: removed implemented `#future` items (LLM setup wizards, alerts command). Remaining backlog: DNS channel implementation.
- `Makefile`: updated VERSION to 0.5.0; added `release` and `release-snapshot` targets; added `windows/amd64` to `cross` target.

---

## [0.4.1] - 2026-03-27

### Added
- **Upgrade guide** in `docs/operations.md` (new section 2): covers `datawatch update`, in-place upgrade procedure, data compatibility across versions, encrypted store stability, and rollback.
- **`docs/covert-channels.md`**: research notes on DNS tunneling, ICMP, NTP, HTTPS, and steganographic channels for constrained network environments.

### Changed
- `docs/operations.md`: section numbering shifted by one (new section 2 = Upgrading; former sections 2–8 are now 3–9).
- `BACKLOG.md`: removed completed `#today` and `#next` sections; retained `#future` and `#backlog`.

---

## [0.4.0] - 2026-03-27

### Added
- **`--secure` now encrypts all data stores**: sessions.json, schedule.json, commands.json, filters.json, alerts.json are all encrypted with AES-256-GCM when `--secure` is set. A 32-byte symmetric key is derived once at startup via Argon2id and a persistent salt at `~/.datawatch/enc.salt`. Per-write operations use a fresh nonce with no KDF overhead.
- **`internal/secfile` package**: `Encrypt`/`Decrypt`/`ReadFile`/`WriteFile` helpers for AES-256-GCM store encryption without re-running the KDF. All stores have `*Encrypted` constructor variants.
- **`config.DeriveKey` + `config.LoadOrGenerateSalt`**: key derivation and salt persistence for the data encryption layer.
- **Command library** (`internal/session/cmdlib.go`): named reusable command strings backed by `~/.datawatch/commands.json`
- **`datawatch cmd add/list/delete`**: CLI commands for managing the command library
- **`datawatch seed`**: pre-populates the command library and filter store with useful defaults for common AI session interactions
- **Session output filters** (`internal/session/filter.go`): regex-based rules that fire `send_input`, `alert`, or `schedule` actions when output lines match
- **`FilterStore` + `FilterEngine`**: persistent filter store; engine processes each output line against enabled filters via `onOutput` callback on session Manager
- **`Manager.SetOutputHandler`**: new callback on the session manager, called for each output line; used by the filter engine
- **System alert channel** (`internal/alerts/store.go`): persistent alert store at `~/.datawatch/alerts.json`; listener pattern for WebSocket broadcast on new alerts
- **`GET /api/commands`**, **`POST /api/commands`**, **`DELETE /api/commands`**: REST endpoints for command library management
- **`GET /api/filters`**, **`POST /api/filters`**, **`PATCH /api/filters`**, **`DELETE /api/filters`**: REST endpoints for filter management
- **`GET /api/alerts`**, **`POST /api/alerts`** (mark read): REST endpoints for alert history
- **`MsgAlert` WebSocket type**: server pushes new alerts to all connected Web UI clients in real time
- **Alerts view in Web UI**: dedicated Alerts nav tab with unread badge counter; shows full alert history; marks all as read on open
- **Saved Commands section in Web UI Settings**: lists saved commands, allows deletion
- **Output Filters section in Web UI Settings**: lists filters with enable/disable toggle and deletion
- **Scheduler NeedsInputHandler bug fix**: `runScheduler` previously overwrote the combined NeedsInputHandler set in `runStart`. Fixed by extracting `fireInputSchedules` as a standalone helper called from the combined handler instead.

### Changed
- `--secure` mode previously only encrypted `config.yaml`; now all data stores are encrypted when the flag is set
- `session.NewManager` accepts an optional `encKey []byte` variadic parameter for encrypted session store
- `ScheduleStore`, `CmdLibrary`, `FilterStore`, `alerts.Store` all expose `*Encrypted` constructors
- Version bumped to 0.4.0

## [0.3.0] - 2026-03-27

### Added
- **`datawatch update [--check]`**: check for and install updates via `go install` from GitHub releases
- **`datawatch setup server`**: CLI wizard to add/edit remote datawatch server connections with connectivity test
- **`--server <name>` global flag**: target any CLI command at a configured remote server
- **`datawatch session schedule` subcommands**: `add`, `list`, `cancel` for scheduling commands to sessions
- **Schedule daemon goroutine**: fires time-based scheduled commands every 10s; on-input commands fire when sessions enter `waiting_input` state
- **`version` messaging command**: reply with current daemon version
- **`update check` messaging command**: reply with version + update availability
- **`schedule <id>: <when> <cmd>` messaging command**: schedule a command from any messaging backend
- **`GET /api/servers`**: list configured remote servers (tokens masked)
- **`GET/POST/DELETE /api/schedule`**: REST endpoints for schedule management
- **`GET|POST /api/proxy/{serverName}/{path}`**: proxy endpoint for Web UI to reach remote servers
- **Remote Server setup wizard** over all messaging channels (`setup server`)
- **Session ordering in Web UI**: up/down buttons on session cards; order persisted in localStorage
- **Remote server selector in Web UI Settings**: lists configured servers with reachability status; click to select active server
- **`RemoteServerConfig` + `Servers []RemoteServerConfig`** config fields
- **`internal/session/ScheduleStore`**: persistent schedule store at `~/.datawatch/schedule.json`
- **MCP daemon compatibility note** in `docs/mcp.md`

### Changed
- Router `handleSetup` now lists `server` as an available service
- `newRouter` helper in `runStart` wires schedule store, version, and update checker into every router
- Version bumped to 0.3.0 (new features, non-breaking additions)

## [0.2.0] - 2026-03-27

### Added
- **Daemon mode**: `datawatch start` now daemonizes by default (PID file at `~/.datawatch/daemon.pid`, logs to `~/.datawatch/daemon.log`). Use `--foreground` to run in terminal.
- **`datawatch stop` command**: sends SIGTERM to the running daemon; `--sessions` flag also kills all active AI sessions.
- **`datawatch setup <service>` command**: interactive CLI wizards for all 11 backends (signal, telegram, discord, slack, matrix, twilio, ntfy, email, webhook, github, web). Telegram/Discord/Slack/Matrix wizards auto-discover channels/rooms via API.
- **Setup wizards over messaging channels**: `setup <service>` command available via every messaging backend (Signal, Telegram, Discord, Slack, Matrix, etc.) — stateful multi-turn conversation engine in `internal/wizard/`.
- **Config encryption**: `--secure` flag enables AES-256-GCM config file encryption with Argon2id key derivation. Use with `datawatch --secure config init` and `datawatch --secure start --foreground`.
- **`GET/PUT /api/config` REST endpoint**: read and patch backend enable/disable status and server settings. Sensitive fields (tokens, passwords) are masked in GET responses.
- **Web UI backend status**: Settings view now shows enable/disable status for all backends with toggle buttons (calls `/api/config`).
- **Quick-input buttons in Web UI**: when a session is waiting for input, y/n/Enter/Ctrl-C buttons appear above the text input for one-click responses.
- **Web server setup wizard** (`setup web`): enable/disable the web UI and configure host/port/bearer token/TLS from CLI or any messaging backend.
- `server.tls_enabled` and `server.tls_auto_generate` config fields for TLS configuration.
- `docs/testing-tracker.md`: interface validation status tracker for all 14 backends.
- `internal/wizard/` package: `Manager`, `Session`, `Step`, `Def` types for cross-channel stateful wizards.

### Changed
- `datawatch start` now defaults to daemon mode. Existing `--foreground` flag keeps the old behavior.
- `datawatch config init` wizard is now service-agnostic (Signal section is optional).
- "No backends enabled" error message now points at `datawatch setup <service>`.
- `datawatch link` auto-creates the config file if it does not exist.
- `router.NewRouter()` signature updated to accept an optional `*wizard.Manager`.
- `server.New()` signature updated to accept `fullCfg` and `cfgPath` for `/api/config`.
- Version bumped to 0.2.0 (new features, non-breaking additions).

### Dependencies
- `golang.org/x/crypto` promoted from indirect to direct (Argon2id for config encryption)
- `golang.org/x/term` promoted from indirect to direct (password prompts for `--secure`)

## [0.1.4] - 2026-03-26

### Added
- Session naming: set a human-readable name at creation (`--name` flag or PWA) and rename at any time (`session rename`, `/api/sessions/rename`)
- Per-session LLM backend override: `--backend` flag on `session new`, backend selector in PWA
- `session stop-all` CLI command: kill all running sessions on this host
- `backend list` CLI command: list registered LLM backends with active marker
- `completion` CLI command: shell completion for bash, zsh, fish, powershell
- New REST endpoints: `/api/backends`, `/api/files`, `/api/sessions/start`, `/api/sessions/rename`
- Directory browser in PWA: navigate the filesystem to select a project directory when starting a session
- `skip_permissions` config field: pass `--dangerously-skip-permissions` to claude-code
- `kill_sessions_on_exit` config field: kill all active sessions when the daemon exits
- `--llm-backend`, `--host`, `--port`, `--no-server`, `--no-mcp` flags on `datawatch start`
- ANSI escape code stripping from session log output sent via messaging backends
- `NO_COLOR=1` set when launching claude-code to reduce color noise
- Extended claude-code permission dialog patterns for `waiting_input` detection
- Debug logging for Signal group ID mismatches to aid troubleshooting
- Session list now shows `NAME/TASK` and `BACKEND` columns
- AGENT.md versioning rule: every push requires a patch version bump
- AGENT.md docs rule: every commit must include documentation updates

### Changed
- `session new` now uses `/api/sessions/start` REST endpoint when daemon is running
- Config `show` subcommand displays all sections including messaging and LLM backends
- Signal self-filter is now lenient for phone number format variations

## [0.1.0] - 2026-03-26

### Added
- Signal group integration via signal-cli JSON-RPC daemon mode
- QR code device linking (terminal and PWA)
- claude-code session management via tmux
- Session state machine: running, waiting_input, complete, failed, killed
- Async Signal notifications for state changes and input prompts
- Session persistence across daemon restarts (JSON file store)
- Multi-machine support with hostname-prefixed session IDs
- Progressive Web App with real-time WebSocket interface
- Mobile-first dark theme PWA installable on Android home screen
- Browser push notifications for session input requests
- OpenAPI 3.0 specification with Swagger UI at /api/docs
- REST API: sessions, output, command, health, info, link endpoints
- Server-Sent Events for QR linking flow in PWA
- Pluggable LLM backend interface (internal/llm)
- Pluggable messaging backend interface (internal/messaging)
- claude-code directory constraints via --add-dir flag
- Automatic git tracking: pre/post session commits
- CLI session subcommands (no daemon required for local ops)
- Universal Linux installer (root and non-root modes)
- Systemd service with security hardening
- macOS LaunchAgent support
- Windows/WSL2 installation instructions
- Debian .deb packaging configuration
- RPM .spec for RHEL/CentOS/Fedora
- Arch Linux PKGBUILD
- Comprehensive documentation suite

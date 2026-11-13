# Encryption audit: what datawatch stores and what `--secure` covers

- **Date:** 2026-11-13
- **Version audited:** v9.172.5 (tree at the commit that set that version)
- **Scope:** every place the daemon, its CLI and the processes it launches keep data on disk or hand data to another process: the data directory, per-project working copies, temporary files, exports and backups, and the connections between peers. Third-party tools that datawatch launches are listed where they leave data behind, but their own storage is not datawatch's to encrypt.
- **Purpose:** the operator asked for an encryption audit before 10.0 so that anything that can be encrypted can be, with the secure feature (`--secure`). This document is the evidence base for that work: it says what each store holds, what protects it today, and whether `--secure` can cover it.
- **Status:** complete. The inventory (written by the first task) is unchanged. Gaps, Cannot be encrypted, Storage direction and Prioritised fixes are written, and the coverage cross-check at the end of Prioritised fixes maps every inventory row to one of them. Nothing in this document is a decision on scope: the operator decides which fixes enter 10.0 (see "Decisions that stay with the operator").
- **Related:** [docs/encryption.md](../encryption.md) is the operator-facing description of the secure feature. This audit checks that page against the code; the differences are in "Claim check" at the end of the inventory.

## Method

1. For every store the task named, and for every package that writes to disk (a search for file create/write calls across `internal/` and `cmd/`, counted per package so none was skipped), the code was read to see what is written, where, and through which function. Write sites that only handle build or test helpers (the rtk binary update, plugin file copies on install, smoke-test progress files, the test home helper, the self-update download in `cmd/datawatch/main.go`) were looked at and hold no operator data, so they have no row.
2. A store counts as encrypted only when the write path goes through an encrypting call with a key that is present. A constructor that accepts a key is not enough: the call site in `cmd/datawatch/main.go` was read to see whether the key is passed.
3. Hints and the existing `docs/encryption.md` were treated as claims to confirm. Where the code disagrees, the code wins and the difference is recorded.
4. Nothing was run: no daemon was started, no `--secure` install was exercised, and no operator home directory was read. Every "confirmed" below means confirmed by reading the code. Where a behaviour looks wrong but was not reproduced, the row says so.
5. No secret value, token, key or password appears in this document. Where a secret lives is described by file name and code path only.
6. Sizes are qualitative. The Automaton store is the largest store (on the order of hundreds of MB; the store's own header notes that one Automaton record can reach about 13 MB).

### Vocabulary used in the inventory

**Encrypted today** names the mechanism, or says none:

| Term | Meaning |
|------|---------|
| **secfile** | The XChaCha20-Poly1305 layer in `internal/secfile`, keyed with the data key derived from the `--secure` password. Whole file (`DWDAT2`), streaming blocks (`DWLOG1`) or per-line (`ENC:`). |
| **config envelope** | The encrypted `config.yaml` (`DWATCH3`, XChaCha20-Poly1305 with Argon2id) in `internal/config/encrypt.go`. |
| **vault** | The secrets vault in `internal/secrets` (`secrets.db`, AES-256-GCM, its own key file). |
| **memory field** | Per-field XChaCha20-Poly1305 in `internal/memory` (`ENC:` prefix on a database column). |
| **TLS** | Encryption in transit (`internal/tlsutil`, `internal/acme`). Not at rest. |
| **none** | Plain bytes on disk. |

**Can `--secure` cover it** is one of:

| Value | Meaning |
|-------|---------|
| **yes** | Only the daemon reads and writes the data, through a code path that can take the data key. Covering it is a code change in datawatch, not a design change. |
| **partly** | Some fields or phases can be covered, or a second reader (another process, a search index, a SIEM, git) needs the plain form for part of it. |
| **no** | The bytes must be plain for something else to use them (git, a launched tool, the kernel, a certificate consumer), or the data is not at rest. |

## How the secure layer works

**Enabling.** `--secure` is a persistent flag on the root command (`cmd/datawatch/main.go`, `secureMode`). The password comes from the `DATAWATCH_SECURE_PASSWORD` environment variable or an interactive prompt (`getSecurePassword`). If `config.yaml` is already encrypted, secure mode turns on without the flag (`isConfigEncrypted`). Starting in the background with `--secure` only prints a warning that daemon mode needs interactive password entry and suggests `--foreground`; it then continues to the service manager or the background launcher (`main.go`, the daemon-mode check near `secureMode`). The practical way to run a secure daemon unattended is therefore the environment variable, which puts the password in the service's environment (row K0).

**Key derivation.** `loadConfigAndDeriveKey` (`cmd/datawatch/main.go`) does two derivations from the one password:

- The config file is opened with Argon2id at time 4, memory 512 MiB, 4 threads (`internal/config/encrypt.go`, `DWATCH3`). The older `DWATCH1` and `DWATCH2` envelopes still read; new writes are `DWATCH3`.
- The **data key** that every other store uses is `config.DeriveKey(password, salt)`: Argon2id at the older, weaker setting (time 1, memory 64 MiB, 4 threads), 32 bytes, with the 16-byte salt taken from the config file header (`config.ExtractSalt`; a legacy `enc.salt` file is the fallback). The source comment explains why: the stores carry no version marker, so strengthening it needs a key-rotation migration that does not exist yet.
- The key lives in process memory as `encKey`, is passed to the store constructors, and is zeroed on exit (`defer zeroBytes(encKey)`).

**File formats** (`internal/secfile`):

- `secfile.go`: `DWDAT2\n` plus base64 of a 24-byte nonce and the ciphertext. `DWDAT1` (AES-256-GCM) reads for compatibility. `WriteFile` writes to a temporary file, syncs and renames. `ReadFile` returns a plain file as-is when a key is supplied, so a store converts to encrypted on its next write.
- `logwriter.go`: `DWLOG1\n` then blocks of up to 4096 plaintext bytes, each with its own 24-byte nonce and a 4-byte length prefix. A file that already has the header is opened for append.
- `fifo.go`: `EncryptingFIFO` creates a named pipe that tmux writes into and re-encrypts the stream into a `DWLOG1` file.
- `migrate.go`: in-place migration helpers (discussion WAL lines as `ENC:<base64>`, single JSON stores, plain `output.log` files).

**Startup migration** (`cmd/datawatch/main.go`, the block that starts with "If --secure is active but config file is plaintext"). When a key exists the daemon encrypts: `config.yaml`; every `sessions/<id>/output.log` and, in `secure_tracking: full` mode, `sessions/<id>/tracking/*.md` (`MigratePlaintextToEncrypted`, guarded by a `.enc.migrated` marker so it runs once); discussion WAL lines and `participants.json`; `channel_routing.json`; and `servers.json`, `skills.json`, `compute/nodes.json`, `inference/llms.json`. Every other secfile store converts when it next saves. `datawatch security encryption status`, `datawatch security encryption migrate` and `datawatch security wipe-plaintext`, with the matching `/api/security/encryption/*` routes (`internal/server/bl334_security.go`), cover only the same fixed list of files.

## Inventory

`<data_dir>` is the `data_dir` setting (default: a `.datawatch` folder under the operator's home). Paths below are relative to it unless they say otherwise. Row ids (K, S, M, A, O, F, E, R, X) are for cross-reference from the later sections.

### K. Configuration, keys and credentials

| Id | Location | What it holds | Encrypted today | Can `--secure` cover it |
|----|----------|---------------|-----------------|-------------------------|
| K0 | The `--secure` password and derived key (process memory; `DATAWATCH_SECURE_PASSWORD` environment variable; interactive prompt) | The root of trust for everything marked secfile | Not stored on disk. The environment variable is readable by the same user through the process environment and by whatever supplies it (service unit, shell profile). Key zeroed on exit (`cmd/datawatch/main.go`). | **no**: it is the root of trust, not a store |
| K1 | `config.yaml` (path from `resolveConfigPath`, default `.datawatch/config.yaml` under the home directory) | Every setting, including `server.token`, messaging backend tokens and passwords (`internal/config/config.go`: Telegram, Discord, Slack, Twilio, SMTP, ntfy, GitHub, DNS and others) unless a field holds a `${secret:name}` reference. Matrix tokens must be references. | **config envelope** (`DWATCH3`) when secure mode is on (`internal/config/encrypt.go`; written at startup by `config.SaveSecure`, `main.go`). **But** every runtime config write goes through `config.Save`, which writes plain YAML (`internal/config/config.go`); callers include `internal/server/api.go` (`saveConfig`), `internal/server/channels.go`, `internal/server/projects.go`, `internal/server/templates.go`, `internal/server/sec016_token_rotation.go`, `internal/acme/manager.go` and `internal/wizard/defs.go`. `SaveSecure` is called only by the startup migration and the CLI helper `saveConfigSecure`. No guard was found, so a running secure daemon appears to rewrite `config.yaml` unencrypted at its first config change (read, not run). | **yes**: the envelope exists; the runtime write path does not use it |
| K2 | `secrets.db` and `secrets.key` | Every vault secret: name, value, scopes (`internal/secrets/builtin.go`) | **vault**: AES-256-GCM with a random 12-byte nonce per save. The key is 32 random bytes in `secrets.key` (0600) in the same directory, or the `DATAWATCH_SECRETS_KEY` environment variable. It is not derived from the `--secure` password, so a copy of the data directory carries both ciphertext and key. | **partly**: `--secure` can protect or derive the vault key; the environment-key option and the external backends (KeePass, 1Password, a vault server, chosen by `secrets.backend` in `cmd/datawatch/main.go`) sit outside datawatch's storage |
| K3 | File secrets provider (`internal/secrets/file.go`; folder supplied by the caller) | One file per secret, value as plain text | **none** (0600 permissions only). Whether any deployment selects this provider was not traced; the built-in vault is the daemon's default. | **yes** if used: values could be sealed with secfile |
| K4 | `service_tokens.json` | Tokens minted for external services (`internal/secrets/service_tokens.go`); the source comment says tokens are stored in plain text | **none** (0600, atomic write) | **yes** |
| K5 | `session_tokens.json` | The scoped credential for each live session, token in clear (`internal/auth/session_token.go`); must survive a daemon restart | **none** (0600) | **yes** |
| K6 | `auth/tokens.json`, `auth/audit.jsonl` | Short-lived git tokens minted for workers, token in clear so a sweep can revoke it; audit lines name worker, repo and event (`internal/auth/token_broker.go`; paths in `cmd/datawatch/main.go`) | **none** (0600) | **yes** |
| K7 | `devices.json` | Mobile push registrations: device id, FCM token, ntfy topic URL or APNs token (`internal/devices/store.go`) | **none** (0600) | **yes** |
| K8 | `plugins/widget_tokens.json`; `plugins/widgets.json`, `last-request.json` | Plugin push tokens as bcrypt hashes only (`internal/plugins/widget_tokens.go`); widget payloads | Tokens: hashed, nothing recoverable. Widgets: **none**. | **yes** (widgets); tokens need nothing |
| K9 | `.mcp.json` in each project directory (`internal/channel/mcp_config.go`) | The bridge entry with `DATAWATCH_TOKEN`: a per-session scoped token on the per-session write, the daemon's `server.token` on the instance-level write (`cmd/datawatch/main.go`, `channelEnv`) | **none** (0600, forced with a chmod). The writer's own comment records the plain-text token. | **no**: the file is read by the launched tool (claude-code, opencode and others), not by datawatch |
| K10 | `tls/<name>/cert.pem`, `tls/<name>/key.pem`; `tls/acme/<domain>/fullchain.pem`, `privkey.pem` | The server certificate and its private key (`internal/tlsutil/tls.go`, `internal/acme/manager.go`) | **none** (key files 0600, certificates 0644/0600) | **partly**: the key files can be sealed and loaded from memory; the certificates are public |
| K11 | `acme/account.json` | The ACME account email, registration and account private key (`internal/acme/account.go`) | **secfile** when a key is given; plain without `--secure` | **yes** (done) |

### S. Sessions

| Id | Location | What it holds | Encrypted today | Can `--secure` cover it |
|----|----------|---------------|-----------------|-------------------------|
| S1 | `sessions.d/p.<id>.rec` (one file per session) and the marker `LAYOUT`; legacy `sessions.json` | Every session record: task text, name, project directory, backend, state, tags, scope (`internal/session/store_records.go`, `store.go`) | **secfile** (`secfile.WriteFile` with `encKey`). The startup migration does not touch these; a record becomes encrypted when it is next written, so a quiet old record stays plain. | **yes** (done for new writes) |
| S2 | `sessions-archive.jsonl` | Archived terminal sessions, one JSON line each (`internal/session/store_archive.go`) | **secfile** per line (the `rec` field becomes a ciphertext string); `id` and `ts` stay readable so the file can be indexed. Append-only; old plain lines stay. | **yes** (done) |
| S3 | `sessions/<id>/fields/<field>.txt` | Full text of a session field that was too long to keep inline (`store_archive.go`) | **secfile** | **yes** (done) |
| S4 | `sessions.json.pre-records.bak`, `sessions.json.corrupted-<unix time>` | Raw byte copies of the old store taken at migration or on a parse failure (`store_records.go`, `store.go`) | Whatever the source file was: plain if the install was plain at migration time | **partly**: new copies inherit the state of the source; existing plain copies stay until wiped |
| S5 | `sessions/<id>/output.log.enc` | The session's terminal output (everything the model and the operator typed and saw) | **secfile** (`DWLOG1`) when a key exists: tmux pipes into `EncryptingFIFO` (`internal/session/manager.go`, at session create and at restart). Without `--secure` the file is the plain `output.log`. | **yes** (done) |
| S6 | `sessions/<id>/output.log` (plain) | Created empty at session start (`internal/session/tracker.go`, `writeInitialFiles`). In secure mode it receives real content from three writers that do not use the FIFO: subprocess sessions (`manager.go` `runSubprocess`, `subprocess_resume.go`) and the opencode ACP backend (`internal/llm/backends/opencode/acpbackend.go`). | **none** | **yes**: all three writers run inside the daemon |
| S7 | After a daemon restart, live encrypted sessions | The output of sessions whose tmux survived the restart | In secure mode the re-pipe step is skipped on purpose (`manager.go`, "We skip re-pipe for encrypted sessions for now"), so such a session stops recording output until it is restarted. This is an availability gap rather than a plain-text one. | **yes** (re-create the FIFO) |
| S8 | `sessions/<id>/session.json` | A copy of the session record | **secfile only when `secure_tracking` is `full`** and the tracker was created for this run. `ResumeTracker` (used after restart, at several call sites in `manager.go` and `end_reason.go`) builds a tracker with no key. Default setting is not `full`, so the default is plain. | **yes** |
| S9 | `sessions/<id>/task.md`, `README.md`, `conversation.md`, `timeline.md` | The task text, state history, prompts that needed input, timeline (`tracker.go`) | **none** by default. In `full` mode `conversation.md` and `timeline.md` appends are encrypted, but the initial files and every `README.md` update are written with plain `os.WriteFile`. A resumed tracker appends plain text to a file an earlier run encrypted. The startup migration looks for `tracking/*.md`, a directory nothing creates, so it migrates nothing. | **yes** |
| S10 | `sessions/<id>/AGENT.md` or `CLAUDE.md`, and the merged copy in the project directory's `CLAUDE.md` | The guardrail text with the session id, task and project directory filled in (`tracker.go`) | **none** | **partly**: the copy in the tracker folder can be sealed; the project-directory copy must stay plain because the launched tool reads it |
| S11 | `sessions/<id>/.git/` | The tracker repository: `git add -A` and a commit at each state change (`tracker.go`, `commitAll`). Live output files are excluded. | **none**: git objects are plain. Encrypted tracker files would be committed as ciphertext; anything committed before they were encrypted stays plain in history. | **partly** |
| S12 | `sessions/<id>/backend_state.json`, subprocess pid and exit-status files | Backend reconnect state (ACP address and session id), process id, exit code (`backend_state.go`, `manager.go`) | **none** | **yes** (low sensitivity) |
| S13 | `schedule.json`, `commands.json`, `filters.json`, `alerts.json` | Scheduled work (task text), saved command library, output filters, alerts (`internal/session/schedule.go`, `cmdlib.go`, `filter.go`; `internal/alerts/store.go`) | **secfile** via the `...Encrypted` constructors, chosen in `cmd/datawatch/main.go`. Note the schedule file is `schedule.json`, not `schedules.json`. | **yes** (done) |
| S14 | `exit_hooks.json`, `queue.json`, the discussion subscription file (`internal/session/exit_hooks.go`, `queue.go`, `discussion_subs.go`) | Exit hooks (session names, actions), queue items with their instructions, discussion subscriptions | **none** (0600) | **yes** |
| S15 | tmux pane capture (`capture-pane` for state detection, scrollback and the PWA view) and `pipe-pane` | Live pane text | **Not at rest.** The captured text is returned to the daemon as a string (`internal/session/tmux.go`); no write of capture content was found in `internal/session`. In plain mode `pipe-pane` appends to `output.log`; in secure mode it feeds the FIFO (`output.log.pipe`, a 0600 named pipe holding no data at rest). The tmux server keeps scrollback in its own memory. | **no** (nothing at rest to encrypt) |

### M. Memory

| Id | Location | What it holds | Encrypted today | Can `--secure` cover it |
|----|----------|---------------|-----------------|-------------------------|
| M1 | `memory.db` (SQLite; `memory.db_path` can move it), table `memories` | Remembered content and summaries, plus the embedding vector, a content hash, project directory, session id, role, placement fields (wing, room, hall, floor, shelf, box), namespace, source, tags, pin state, timestamps (`internal/memory/store.go`) | **memory field** on `content` and `summary` only (`ENC:` plus base64), when the store has a key. The key is the `--secure` data key; without `--secure` it is only used if a `memory.key` file exists, and no code path creates that file (`KeyManager.Generate` has no caller outside tests). So in practice memory is encrypted when `--secure` is on, and not otherwise. Plain: embedding, content hash (unsalted SHA-256 of the trimmed, lower-cased plain text, used for duplicate detection, so a guessed text can be confirmed against it), every metadata column. | **partly**: content and summary are covered; the embedding must stay plain for vector search and can leak the topic of the text; the hash could be keyed |
| M2 | Same database: `kg_entities`, `kg_triples` | Knowledge graph names, relationships and objects as plain text (`internal/memory/knowledge_graph.go`) | **none** | **yes**: the graph is read only through the daemon, but its names are also search keys |
| M3 | Postgres memory backend (`memory.postgres_url`; `internal/memory/pg_store.go`, `pg_kg_adapter.go`) | The same fields and graph tables in Postgres | **memory field** on `content` and `summary` when `--secure` is on (`NewPGStoreEncrypted`); the connection URL with its credentials lives in `config.yaml` (K1). Postgres traffic and the server's own storage are outside datawatch. | **partly** |
| M4 | `memory-wal.jsonl` beside the database | Operation log: project directory, placement fields, source, namespace, content length, session id, tags (`store.go`, `walLog`) | **none** | **yes**: per-line `ENC:` already exists for the discussion WAL |
| M5 | `GET /api/memory/export`, `memory_export` | A JSON download of all memories | SQLite `Export` writes the stored value, so an encrypted store exports `ENC:` ciphertext that no other install can read. The Postgres `Export` decrypts, so its download is plain JSON. | **partly**: export is meant to be portable, so a plain export is a decision, not an accident |
| M6 | `POST /api/memory/import` | Memories brought in from an export or a worker sync | SQLite `Import` inserts `content` and `summary` without calling `encryptField`, so imported rows are plain in an otherwise encrypted store. Postgres `Import` goes through `Save` and is encrypted. | **yes** |
| M7 | Keyword fallback (`SearchByText`, `content LIKE ?`) | Not stored data | Against an encrypted column the match runs on ciphertext and finds nothing. A functional cost of covering memory, noted for the later sections. | n/a |

### A. Automata, plans and spend

| Id | Location | What it holds | Encrypted today | Can `--secure` cover it |
|----|----------|---------------|-----------------|-------------------------|
| A1 | `autonomous/prds/<id>.json`, `autonomous/prds/<id>.files/<sha256>.txt`, marker `LAYOUT`; legacy `autonomous/prds.jsonl` and `prds.jsonl.pre-records.bak`; `autonomous/learnings.jsonl`, `guardrail_profiles.jsonl`, `tags.jsonl`, `retry_profiles.jsonl`, `templates.jsonl` | The Automaton store, the largest store (hundreds of MB): specs, stories and tasks, task prompts, worker output and checkpoints, raw judge and verifier text, project directories, LLM choices, learnings (`internal/autonomous/store.go`, `store_records.go`, `templates.go`) | **none.** `NewStore(dataDir)` takes no key, `store_records.go` writes plain JSON through a temporary file and rename, and no `secfile` call exists in the package except in A5. | **yes**: records and side files are whole-file writes that can be sealed one by one; the cost is load time, because startup reads the whole store |
| A2 | `autonomous/budget-ledger.jsonl`; `spend_ledger.json` (`internal/spendledger/ledger.go`); `usage.json`, `lanes.json`, `autolanes.json` (`internal/usagecalc`); burn guard state (`internal/server/burn_guard.go`); `autonomous/backoff.json` | Spend, usage and lane history, backoff state | **none** (0600) | **yes** (append-only files would use per-line sealing) |
| A3 | `autonomous/judge_verdict_cache.json`, `lane_fill_decisions.json`, `merge_shadow_decisions.json`, `merge_pipeline_runs.json`, `pending_items.json`, `cleanup_status.json`, `tag_sweep_marker.json` (all in the autonomous store directory); `unlanded-keep.json` and `defect_intake_decisions.jsonl` (state or data directory; not traced further) | Decision and verdict caches: judge verdict text, lane fill decisions, merge trial results, queued work items, defect intake decisions | **none** | **yes** |
| A4 | `questions.jsonl`, `decision_records.jsonl` | Operator questions and answers, decision records (`internal/autonomous/questions_store.go`, `decision_record_store.go`; paths in `cmd/datawatch/main.go`) | **none** (0600, append) | **yes** |
| A5 | `lifecycle_subscriptions.json` | Webhook and peer subscriptions for Automaton lifecycle events (URLs and secrets) | **secfile** (`internal/autonomous/lifecycle_subscription_store.go`; key passed at `main.go`) | **yes** (done) |
| A6 | `plan-records/<kind>/<uid>.json` | Plans, registry rows, findings (`internal/planrecords`) | **secfile** payload in a JSON envelope (`planrecords.KeySealer`, wired at `main.go`). `kind`, `project`, `state`, ids and timestamps stay readable by design; title, notes and links are inside the seal. | **yes** (done) |
| A7 | `autonomous/scratch/<prd>/` (decompose output, and copies of the judge and verifier output files taken from the project: `cmd/datawatch/session_judgment.go`) | Short-lived working files for one Automaton (`internal/autonomous/scratch.go`) | **none** (0600) | **yes** |
| A8 | `supervisorloop` runs (`runs.jsonl`), orchestrator `graphs.jsonl`, tag registry `tags.json` cache, alert rules `alert-rules.yaml`, agent audit `audit/agents.jsonl`, eval suites and run results (`evals/`, `evals/runs/<id>.json`), classifier calibration (`internal/classifier/calibration.go`), the last-shutdown record (`internal/shutdowncause`), dashboard layout (`internal/server/api_dashboard_cards.go`), `federation/groups.json`, change records (`internal/changerecord`), `identity.yaml` (see E1) | Small operational stores; contents are configuration and event history, with no credentials found | **none** | **yes** (low sensitivity) |

### O. Observability, audit and logs

| Id | Location | What it holds | Encrypted today | Can `--secure` cover it |
|----|----------|---------------|-----------------|-------------------------|
| O1 | `audit.log`, `audit.log.cef`, `access.log`, `access.log.cef` (`internal/audit/log.go`) | Operator actions with actor, session id, details; HTTP access and auth-failure lines | **none**. The CEF files exist so a SIEM can read them. | **partly**: the JSON-lines files can be sealed per line (readable only through the daemon); the CEF mirror is plain by purpose |
| O2 | `trace/<session>.jsonl`, `trace/index.json` (`internal/trace/store.go`) | Tool-call trace per session, with an argument summary and outcome | **none**, but redacted before writing (home paths scrubbed, argument text capped or dropped depending on the trace mode) | **yes** |
| O3 | `websearch.db` (SQLite) | One row per web search: query text, provider, session id, caller, outcome (`internal/websearch/store.go`) | **none** | **partly**: the daemon and the separate `mcp-search` process both open this file, so the second process would need the key |
| O4 | `docs-index/core/vectors.sqlite`, `docs-trust.json`, `docs-trust-pending.json` (`internal/docsindex`) | Vectors of the product documentation, trust decisions about documentation sources | **none**. The indexed text is public documentation. | **partly**: vectors must stay plain for search; trust files could be sealed |
| O5 | `council/runs/<id>.json`, `council/personas/*.yaml`, `council/wizard-sessions.db` (`internal/council`) | Debate transcripts, persona definitions, persona drafts (SQLite, deleted after a retention period) | **none** | **yes** (runs, personas); **partly** (drafts database) |
| O6 | `daemon-app.log` and `daemon.log` | Application log lines after key derivation; boot messages from the parent process | `daemon-app.log`: **secfile** (`DWLOG1`), written by `main.go` once the key exists. It is opened for append when the header is valid, so it accumulates across restarts (the docs say it is cut on restart). `daemon.log`: **none**; the parent process writes it before any key exists. | `daemon-app.log` **yes** (done); `daemon.log` **no** |
| O7 | Standard output and error of the daemon (service journal, `daemon.log`), `daemon-crash.log`, the restart log | Startup and warning lines; crash traces (`cmd/datawatch/main.go`, `daemon_unix.go`). The config-write audit sink also writes every config key change, secret-like values redacted, to standard error (`internal/federation/config_audit.go`). | **none** (0644) | `daemon-crash.log` **partly** (it is written while the process is failing, possibly before a key exists); the journal **no** (owned by the process manager) |

### F. Federation, peers, channels and messaging

| Id | Location | What it holds | Encrypted today | Can `--secure` cover it |
|----|----------|---------------|-----------------|-------------------------|
| F1 | `servers.json` (`internal/server/multiserver/store.go`), `compute/nodes.json` (`internal/compute/registry.go`), `inference/llms.json` (`internal/inference/store.go`), `skills.json` (`internal/skills/store.go`), `channel_routing.json` (`internal/server/channel_routing.go`), `profiles/projects.json` and `profiles/clusters.json` (`internal/profile`) | Peer URLs and tokens, compute node registry, LLM registry with endpoints and keys, skills index, channel routing table, project and cluster profiles | **secfile** (whole file) via the `...Encrypted` constructors wired in `cmd/datawatch/main.go`; the first four plus `channel_routing.json` are also converted at startup | **yes** (done) |
| F2 | `discussions/<id>/participants.json`, `discussions/<id>/wal.jsonl` (`internal/server/bl332_discussion_sync.go`, `bl332_discussion_scope.go`) | Discussion membership and the discussion log | **secfile**: whole file for participants, per-line `ENC:` for the log | **yes** (done) |
| F3 | `autonomous/fingerprint/matches.json` (`internal/fingerprint/matches.go`); `observer/peers.json` (`internal/observer/peer_registry.go`); the file named by `mcp.self_config_audit_path` | Peer fingerprint matches (8-character hash prefixes only), registered observer peers (token hashes, not tokens), config-write audit lines | **none**, but no recoverable credential is stored in the first two | **yes** |
| F4 | Peer token files: the stats agent's `peer.token` under its own home folder (`cmd/datawatch-stats/main.go`, `internal/observerpeer/client.go`) and the federation token file (`observer.federation.token_path`, default `./observer/federation.token`) | The bearer token a peer uses to talk to this daemon | **none** (0600) | **partly**: the file belongs to a separate process that must read it |
| F5 | Peer, worker and observer connections | Session lists, envelopes, proxied API calls, tokens in headers | **TLS** (`internal/tlsutil`, `internal/acme`) when the server has TLS on. Verification can be turned off per peer (`tls_skip_verify`; `multiserver.HTTPClient`), and worker observer pushes default to skipping verification (`startWorkerObserverPush`). | **no**: at-rest feature; transit is TLS's job |
| F6 | Messaging backends (`internal/messaging/backends/`: discord, dns, email, github, imapmcp, matrix, ntfy, signal, slack, telegram, twilio, webhook) | Tokens and passwords sit in `config.yaml` (K1) or the vault (K2). The only files these backends write are the Matrix application-service registration (`matrix/as.go`, 0600, plain tokens at an operator-chosen path) and short-lived temporary media files (R5). No message store was found. | The Matrix registration file: **none**. Others: see K1, K2. Signal keeps its own keystore in the signal-cli folder (`signal.config_dir`, default under the home directory), which belongs to that tool. | Matrix file **yes**; signal-cli store **no** |

### E. Exports and backups

| Id | Location | What it holds | Encrypted today | Can `--secure` cover it |
|----|----------|---------------|-----------------|-------------------------|
| E1 | `identity.yaml` and `identity-backups/identity-<timestamp>.yaml` next to it (`internal/identity`) | The operator's identity text and each saved earlier version | **none** | **yes** |
| E2 | `datawatch export --all|--config|--log` output folder (`cmd/datawatch/main.go`) | A plain-text copy: config re-serialised as YAML (with all tokens), session logs, and the stores `sessions.json`, `alerts.json`, `commands.json`, `filters.json`, `schedules.json` | **none, on purpose**: the command decrypts for the operator (folder 0700, files 0600). It reads `sessions.json` and `schedules.json`, which are the legacy and a wrong name, so it exports neither the current session records nor the schedule store. | **partly**: a decrypting export is the feature, but it should not drop data silently |
| E3 | Memory export and import | See M5 and M6 | | |
| E4 | `.pre-records.bak` copies in `sessions` and `autonomous`; `datawatch security wipe-plaintext` | Raw backups from the layout migrations; a 3-pass overwrite of plain files in the fixed list the status command uses | Backups: as their source. Wipe: reaches only the fixed list (discussions, `channel_routing.json`, `servers.json`, `skills.json`, `compute/nodes.json`, `inference/llms.json`). On SSD and copy-on-write file systems an overwrite may not reach the medium. | **partly** |
| E5 | `/api/sessions/import` | Brings session directories in from disk | Reads what is there; writes through the session store (S1) | **yes** |
| E6 | Key and password backup | Nothing is backed up by datawatch. The salt is in the `config.yaml` header, so losing that file loses the data key. There is no key-rotation or password-change command (`datawatch security` offers `encryption status|migrate`, `wipe-plaintext` and `logs`). | n/a | n/a |

### R. Repository copies, scratch space and temporary files

| Id | Location | What it holds | Encrypted today | Can `--secure` cover it |
|----|----------|---------------|-----------------|-------------------------|
| R1 | Project directories and their `.git` (including the attempt snapshots the daemon commits under `refs/datawatch/attempts/`, `internal/autonomous/attempt_snapshot.go`) | The operator's source trees and history | **none** | **no**: git and the launched tools need plain files; use disk-level encryption |
| R2 | PRD worktrees (`autonomous.worktree_dir`; default a `prd-worktrees` folder under the data directory, falling back to the home `.datawatch` folder; `internal/config/config.go`, `internal/autonomous/worktree.go`) | A full working copy and `.git` for each task | **none** | **no** (same reason as R1) |
| R3 | Merge and trial scratch: rebase folders (`internal/mergepipeline/run.go`), conflict attempts (`conflict_propose.go`), shadow trial merges (`internal/mergeshadow/trial.go`) | Temporary repository copies | **none** | **no** |
| R4 | Task capture files in the system temp folder: `datawatch-<backend>-task-*.txt` for claude-code, pi, opencode, aider and goose; pi scratch and agent folders (with `models.json`); the claude-code starting-context file; the shell backend's task file (`internal/llm/backends/...`, `internal/llm/claudecode/backend.go`; the last two locations were not traced) | The complete task text handed to the model | **none** (0600). Tracked in an in-memory inventory (`internal/llm/scratch`) and removed when the session ends; `internal/autonomous/temp_sweep.go` removes leftovers. A crash before cleanup leaves them. | **no**: the launched tool reads the file; mitigation is a memory-backed temp folder |
| R5 | Temporary media: `dw-voice-*`, `dw-audio-*`, `whisper-result-*` (`internal/server/voice.go`, `internal/transcribe`), image uploads (`vision.go`), `tg-voice-*` (Telegram), `dw-webhook-img-*` (webhook) | Voice clips, images, transcripts for the duration of one request | **none**; removed after the request | **no** (consumer is ffmpeg, whisper or an LLM) |
| R6 | Commit guard hooks folder in the system temp folder (`internal/gitguard/commitguard.go`) | Hook scripts that carry a random commit token, mode 0700 | **none** | **no** |
| R7 | Files the daemon writes into projects for the launched tools: claude-code `settings.json` (`internal/channel/claude_settings.go`, `internal/hookinstaller/install.go`, `internal/session/manager.go`), the opencode LSP settings file (`internal/llm/backends/opencode/lsp.go`), memory hook scripts (`internal/memory/hooks.go`), the platform rules in `CLAUDE.md` and `CLAUDE.local.md` (`internal/platformcontext`), generated plan and report documents (`internal/genartifacts`), and operator-supplied files through `/api/files` (`internal/server/bl333_file_service.go`) | Settings, rules text and operator-supplied files | **none** | **no**: the launched tools and git read them |

### X. Data other tools keep because datawatch launched them

| Id | Location | What it holds | Encrypted today | Can `--secure` cover it |
|----|----------|---------------|-----------------|-------------------------|
| X1 | The LLM tools' own state: claude-code transcripts and settings in its home folder, aider's `.aider.chat.history.md` and caches, `.opencode/`, `.goose/`, `.gemini/` (listed in `internal/tooling/artifacts.go`) | Conversation history and tool state | **none** (owned by the tools) | **no** |
| X2 | tmux server (dedicated socket under `TMUX_TMPDIR`) | Pane contents and scrollback in memory; socket file | **none**; memory only | **no** |
| X3 | External stores datawatch talks to: Postgres, signal-cli, KeePass and other secrets backends, Ollama and other model servers | Their own data | Their own storage | **no** |

### Claim check against `docs/encryption.md`

| Claim in the page | What the code shows |
|-------------------|---------------------|
| `config.yaml` is `DWATCH2` with Argon2id time 1, memory 64 MB | Current writes are `DWATCH3`, Argon2id time 4, memory 512 MiB. The 64 MiB setting survives only as the data-key derivation (`DeriveKey`). |
| `sessions.json` holds the session index | The current layout is one file per session in `sessions.d/`; `sessions.json` is legacy. |
| `schedules.json` | The file is `schedule.json`. |
| `sessions/<id>/output.log` is `DWLOG1` | The live encrypted log is `output.log.enc`. The plain `output.log` is created empty and still receives output from subprocess and ACP sessions. Legacy plain logs are migrated in place as whole-file `DWDAT2`, not `DWLOG1`. |
| `sessions/<id>/session.json` is encrypted with `--secure` | Only with `secure_tracking: full`, and not for resumed trackers. The setting is not documented anywhere under `docs/`. |
| Streaming log nonce is 24 bytes; the file is truncated on each restart | The nonce is 24 bytes (the file's own header comment says 12). The writer appends when the header is valid and truncates only otherwise. |
| Memory encryption is "controlled by `memory.encrypt_content`" | No such setting exists in the code. Encryption follows `--secure`; the alternative `memory.key` file is never created. |
| Memory embeddings are the only plain part of a memory row | Metadata, the content hash, the knowledge graph tables and `memory-wal.jsonl` are plain too. |
| Rotation: "re-encrypt `config.yaml` with a new password via `datawatch security`" | No such command exists. |
| `export --all` exports every store | It reads `sessions.json` and `schedules.json`, so it misses the current session records and the schedule store. |
| Startup migration encrypts the listed stores | True for the fixed list; other stores convert on their next write, and the `tracking/*.md` migration targets a folder nothing writes. |

## Gaps

This section covers every inventory row that is **not** encrypted or only **partly** encrypted and that datawatch itself could change. Rows that datawatch cannot change are in "Cannot be encrypted". "Coverage cross-check" at the end of Prioritised fixes lists every inventory row against the section that handles it.

### How recommendations are worded

| Recommendation | Meaning |
|----------------|---------|
| **Encrypt now** | Bring the store under what `--secure` already promises, as a fix with no new switch. Cost is small and bounded, only the daemon reads the data, and the exposure is real (a credential, conversation text or operator-written text). |
| **Encrypt behind `--secure` only** | Do it, but only when `--secure` is on, and measure first or sequence it after a prerequisite, because it costs load time, a migration, or a second process that needs the key. A plain install does not change. |
| **Leave plain, with reason** | Sealing buys nothing or breaks a consumer. The reason is written down so the next audit does not reopen it. |

Every sealed store needs the data key, which exists only in secure mode. So "encrypt now" and "behind `--secure` only" both mean "when `--secure` is on"; the difference is whether the work is a defect fix against the current promise or a feature with a cost that needs its own decision.

### Costs that apply to many rows

These are referred to as C1 to C6 in the gap entries so they are written once.

**C1. Performance and size.**

- A whole-file `DWDAT2` file is base64 of nonce plus ciphertext plus tag, so a sealed file is about a third larger than the plain one. A reader holds the base64 text, the ciphertext and the plaintext at the same time while it loads, so a 13 MB Automaton record needs several times that in memory for a moment.
- These figures are **not measured**. The expectation is that the cipher (XChaCha20-Poly1305) is not the bottleneck and the extra copies, base64 and JSON parsing are. The first task of any large-store fix is to measure load time and peak memory on a copy of a real store.
- A fresh random nonce is used on every write, so unchanged content produces different bytes. Anything that relies on byte-identical files loses that: git deltas in the tracker repositories, rsync or backup de-duplication, and content-addressed names that are the hash of the plain text.
- Streaming logs (`DWLOG1`) add about 1 percent (24-byte nonce, 4-byte length and 16-byte tag per block of up to 4096 bytes). The cost is reading: a tail request must decrypt from the start of the file unless blocks are indexed or checkpointed.

**C2. Key rotation and re-encryption of existing data.**

- The data key comes from the password and the salt in the `config.yaml` header, using the old, weaker Argon2id setting. Stores carry a format marker (`DWDAT1`, `DWDAT2`, `DWLOG1`, `ENC:`) but no key id, so a store cannot say which key sealed it.
- Today a file may be plain or sealed and the reader accepts both. That makes any migration safe to interrupt, and also means a quiet old file stays plain until something rewrites it.
- A rotation has to read with the old key, write with the new key, and cope with a half-finished run. Without a key id the reader has to try the new key and fall back to the old one. Backups and `.pre-records.bak` copies taken before a rotation stay under the old key, so either the old password stays valid for them or they are wiped.
- The salt lives in the `config.yaml` header, so a password change rewrites the one file that gap G1 shows is rewritten as plain text.

**C3. Features that need plain bytes.** Memory vector search, the keyword fallback and SQL filters on namespace and project; docs-index vectors; web search history filtering; operators who `tail` or `grep` `daemon.log`, `audit.log` or a session log; the SIEM that reads the CEF files; `git log` and `git diff` on the tracker repositories; and any script, cron job or runbook that reads a store file directly (supervisor tooling and backup scripts are the likely ones). The first task of each fix searches `scripts/`, `docs/` and the CLI for direct readers of that file before sealing it.

**C4. Backups and exports.** A sealed file in a backup is still sealed, which is the point, but it can only be opened with the password and the salt, and the salt is in `config.yaml`. A backup that misses `config.yaml`, or one taken after the G1 defect rewrote it, cannot be opened. `datawatch export` decrypts on purpose. The SQLite memory export writes ciphertext that no other install can read.

**C5. Crash recovery.**

- Whole-file writes use a temporary file, a sync and a rename, so after a crash each file is at its old or its new generation. Sealing does not change that.
- A half-migrated store is a mix of plain and sealed files and reads correctly.
- Streaming logs can lose the unflushed tail of one block on a crash. Whether a torn last block is tolerated by the reader, and whether the append-open of an existing file handles one, was **not** checked; the fix that adds more streaming writers has to check it.
- Whole-database approaches have a different crash story; see Storage direction.

**C6. Federation interop.** Peers exchange data through the API over TLS as plain JSON, and each node seals at rest with its own key, so nothing sealed crosses a peer link. Three consequences: a worker with `--secure` off that holds copies of Automata or memories from a secure primary re-creates plain copies, so the promise is per node; imports from a peer must be sealed on arrival (the SQLite memory import is not, row M6); and the transit leg depends on TLS verification, which is off by default for worker observer pushes (row F5).

### Credentials and keys

#### G1. Runtime config writes drop the envelope (K1)

- **Today.** Startup writes `config.yaml` as a `DWATCH3` envelope, but every later change goes through `config.Save`, which writes plain YAML. Seven files call it (settings API, channels, projects, templates, token rotation, ACME and the wizard). The file holds the server token and every messaging token that is not a `${secret:name}` reference. This was confirmed by reading, not by running.
- **Cost.** Low in code (route `Save` through `SaveSecure` when a key is present, or have the config object remember it was loaded encrypted). Each `DWATCH3` write costs one Argon2id derivation at time 4 and 512 MiB, which is seconds and a memory spike, so writes must be serialised and the derived envelope key reused for the life of the process rather than re-derived per save.
- **Risk.** The salt for the data key is in the envelope header. If the file becomes plain YAML, the salt is gone, and the next start may not be able to derive the data key for every other store. This was **not reproduced**; the first task must reproduce it on a throwaway data directory (C2, C4). Until then treat it as the highest-risk item in the audit.
- **Recommendation: encrypt now.** Highest priority. Add a guard so `Save` refuses to write plain YAML over an encrypted file.

#### G2. The vault key sits beside the vault (K2) and the file provider (K3)

- **Today.** `secrets.db` is AES-256-GCM, but its key is a random file in the same directory (or an environment variable), independent of `--secure`. Copying the data directory copies the lock and the key. The file provider (K3) stores one plain file per secret; whether any deployment uses it was not traced.
- **Cost.** Wrap the vault key with the data key when `--secure` is on: read the plain key file, write the wrapped form, remove the plain one. Small code, one migration, and a failure mode where a lost password loses the vault (the same as every other sealed store). The external backends (KeePass, 1Password, a vault server) and the environment-key option are outside datawatch's storage and unchanged.
- **Risk.** A crash between writing the wrapped key and removing the plain one leaves both; the order must be wrap, sync, verify by unwrapping, then remove. Wrapping also makes vault re-keying independent of the password (C2).
- **Recommendation: encrypt behind `--secure` only.** For K3, first trace whether anything selects it; if not, remove the provider, and if so seal its values (encrypt behind `--secure` only).

#### G3. Plain token and credential files (K4, K5, K6, K7, K8 widgets)

- **Today.** `service_tokens.json`, `session_tokens.json`, `auth/tokens.json`, `auth/audit.jsonl`, `devices.json`, and the plugin widget payloads are plain (mode 0600). The first three hold working credentials in clear (the source says so); `devices.json` holds push tokens; the audit file names workers and repositories. Plugin push tokens are already hashed.
- **Cost.** Small files, so C1 is irrelevant. `auth/audit.jsonl` is append-only and takes the per-line `ENC:` form already used for the discussion log. Session tokens must survive a restart, which works because the store is read after the key exists.
- **Risk.** Low. Check (C3) that no CLI command reads these files without a running daemon, because a command run without the key would then see ciphertext.
- **Recommendation: encrypt now.**

#### G4. TLS private keys (K10)

- **Today.** Generated and ACME key files are plain at 0600. Certificates are public.
- **Cost.** Seal the key file and load it from memory (`tls.X509KeyPair` takes bytes). ACME renewal writes the key through the manager, so that write path must seal too. The daemon derives the key before it opens the listener, so start-up order already allows it.
- **Risk.** A deployment that points the daemon at certificate files it does not own (an operator-supplied path, or a reverse proxy sharing the files) must keep working, so only daemon-generated and ACME material is sealed.
- **Recommendation: encrypt behind `--secure` only**, for daemon-generated and ACME keys only. Leave operator-supplied key files alone: they belong to the operator's own certificate process.

### Sessions

#### G5. Session output that bypasses the encrypting pipe (S6) and is lost on restart (S7)

- **Today.** Tmux sessions write through the encrypting FIFO. Subprocess sessions and the opencode ACP backend write the plain `output.log` instead, even in secure mode, so their full terminal text is plain. After a daemon restart the re-pipe step is skipped for encrypted sessions, so a live session stops recording output until it is restarted.
- **Cost.** All three plain writers run inside the daemon, so each can write through the `DWLOG1` log writer (about 1 percent size, C1). Readers of `output.log` (the PWA, tail and output endpoints, summarisers) must read through the same layer. The re-pipe fix creates a new FIFO and points `pipe-pane` at it, and appends to the existing `DWLOG1` file, which depends on how a torn last block is handled (C5).
- **Risk.** Medium for S7: the old FIFO writer must be gone before a new one appends, or two writers interleave blocks.
- **Recommendation: encrypt now.** S6 is plain conversation text in a mode that says it is encrypted. S7 is a recording gap, not a plain-text one, but sits with it.

#### G6. Tracker files are plain by default and after a restart (S8, S9, S10, S12)

- **Today.** `session.json` is sealed only when `secure_tracking` is `full` and only for a tracker created in this run (`ResumeTracker` has no key). `task.md`, `README.md`, `conversation.md` and `timeline.md` are plain by default, and in `full` mode only the appends are sealed. A resumed tracker appends plain text to a file an earlier run sealed. The startup migration looks for a `tracking` folder that nothing creates. `secure_tracking` is not documented. `backend_state.json` and the pid and exit files are plain (low sensitivity). The guardrail copy in the tracker folder (S10) is plain; the copy in the project directory is in "Cannot be encrypted".
- **Cost.** Making `full` the default under `--secure` is a small code change. The cost is behavioural: an operator who reads these files with `cat` loses that, and the UI or CLI must read them through the daemon (C3). Whole-file rewrites of `README.md` on every state change are small.
- **Risk.** Moderate: the three code paths (create, resume, update) each need the key, and the existing mixed files must read in both forms.
- **Recommendation: encrypt now** for S8, S9 and S12 (carry the key through `ResumeTracker`, seal the initial and rewritten files, fix the migration's target); **encrypt behind `--secure` only** for the tracker folder copy of the guardrail text (S10).

#### G7. Tracker git history (S11)

- **Today.** Each tracker folder is a git repository committed at every state change. Anything committed in plain form stays in history even after the file is sealed; sealed files are committed as ciphertext.
- **Cost.** With a random nonce per write (C1), every commit stores a whole new blob instead of a delta, so tracker repositories grow faster, and `git log -p` on sealed files shows nothing useful. The history is the daemon's own and local, so it can be dropped.
- **Risk.** Dropping history loses the state timeline unless it is also recorded elsewhere (`timeline.md` records the same events).
- **Recommendation: encrypt behind `--secure` only**, in one of two ways to be chosen when the fix is designed: stop committing content-bearing files in secure mode (commit only a manifest), or re-initialise the repository when a tracker is migrated.

#### G8. Plain copies left by migrations (S4, E4)

- **Today.** `sessions.json.pre-records.bak`, `sessions.json.corrupted-<time>` and the Automaton store's `.pre-records.bak` are byte copies of whatever the source was, so a plain install's plain copy persists after the operator turns `--secure` on.
- **Cost.** Seal new copies when a key exists; extend the wipe command to find existing plain copies. A 3-pass overwrite may not reach the medium on SSD or copy-on-write file systems, so the honest advice is disk-level encryption plus deleting the copies.
- **Risk.** Low.
- **Recommendation: encrypt now** for new copies; leave existing plain copies to the extended wipe, with the SSD limit written in the docs.

#### G9. Small operational stores (S14, A3, A4, A7 judge copies, A8, F3 audit lines)

- **Today.** Exit hooks, the queue (task instructions), discussion subscriptions, judge verdict and lane-fill caches, pending items, merge results, operator questions and decision records, the copies of judge and verifier output kept in the Automaton scratch folder, supervisor and orchestrator runs, agent audit, evaluation results, the classifier calibration, the dashboard layout, change records, and the config-write audit file are plain.
- **Cost.** All small, all read by the daemon only, and the pattern (whole-file or per-line) already exists.
- **Risk.** Low. The questions and decision records (A4) are written by the operator and hold reasoning text, so they matter more than the caches.
- **Recommendation: encrypt now** for S14, A4 and the judge copies in A7; **encrypt behind `--secure` only** for the caches and event history (A3, A8, F3 audit lines). **Leave plain, with reason**: `alert-rules.yaml` and other hand-edited YAML (the operator edits them by hand and they hold no credentials), fingerprint matches (8-character hash prefixes) and `observer/peers.json` (token hashes only).

### Automata, memory and spend

#### G10. The Automaton store (A1)

- **Today.** No key path at all: `NewStore` takes no key, every record and side file is plain. The largest store (hundreds of MB; one record can reach about 13 MB). It holds specs, task prompts, worker output, raw judge text and project directories.
- **Cost.** Records are already one file each and only changed records are rewritten, so per-record sealing keeps the write pattern. The cost is start-up: the daemon reads the whole store, so every record is base64-decoded and decrypted (C1). Not measured. Side files are named by the SHA-256 of their plain text, so a sealed side file's name still confirms a guessed text; a keyed hash (HMAC with a subkey) fixes that but renames every file. Mitigations to weigh: a raw binary container instead of base64, and a record envelope with a readable header (id, state, project, times) and a sealed body that loads on demand (this is the same pattern as the plan records and the session archive).
- **Risk.** Highest migration risk in the audit: the longest rewrite, a mixed store for as long as it takes, and any script that reads `autonomous/prds/*.json` directly breaks (C3). Per-record temporary-file-then-rename means the run can be interrupted and resumed, and the reader accepts both forms, so no marker is needed.
- **Recommendation: encrypt behind `--secure` only**, and only after the store registry (fix 7) and the key-versioned format (fix 8) exist so 500 MB is not rewritten twice. The first task is a measurement on a copy of a real store.

#### G11. Spend, usage and lane files (A2)

- **Today.** `budget-ledger.jsonl`, `spend_ledger.json`, `usage.json`, `lanes.json`, `autolanes.json`, burn-guard state and `backoff.json` are plain (0600).
- **Cost.** Append-only files take per-line sealing; the rest take whole-file. These are read by dashboards and the reporting index (Storage direction), so sequencing matters.
- **Risk.** Low to medium: they reveal work patterns and spend, not credentials.
- **Recommendation: encrypt behind `--secure` only**, sequenced with the reporting index decision (a derived index that holds copies of these files undoes the sealing).

#### G12. Memory residuals (M1, M3)

- **Today.** `content` and `summary` are sealed. The embedding, the content hash and every metadata column (project directory, session id, role, placement fields, namespace, source, tags) are plain. The hash is an unsalted SHA-256 of the normalised text, so a guessed text can be confirmed against it. The Postgres backend seals the same two fields; the server's own storage and the connection are outside datawatch.
- **Cost.**
  - **Hash:** replace with an HMAC under a subkey of the data key. One pass that decrypts each row once and recomputes. De-duplication keeps working.
  - **Embedding:** it must stay plain for vector search, and it leaks the topic of the text and can allow approximate reconstruction. Sealing it means loading vectors into memory at start (roughly rows times dimensions times 4 bytes; for 100 000 rows of 768 dimensions about 300 MB; `memory_stats` gives the real row count) and searching there, which is a design change.
  - **Metadata:** namespace and project are the access boundary and are used in `WHERE` clauses, so they stay plain; tags and source could be sealed with a loss of server-side filtering.
- **Risk.** The hash change is safe to interrupt. The embedding change is a search redesign.
- **Recommendation: encrypt now** for the keyed hash; **leave plain, with reason** for embeddings and the placement columns unless the operator chooses the in-memory vector approach (Storage direction); **encrypt behind `--secure` only** for the Postgres connection (require a verified TLS mode when the URL points off the host).

#### G13. Knowledge graph (M2)

- **Today.** `kg_entities` and `kg_triples` hold names, relationships and objects as plain text in the same database.
- **Cost.** Sealing names breaks lookup. The standard answer is a blind index: store an HMAC of the normalised name for lookups and a sealed value for display. Moderate code, a migration that touches every row, and some loss of prefix search.
- **Risk.** Moderate: every graph query path changes.
- **Recommendation: encrypt behind `--secure` only**, after the memory fixes in G12 and G14.

#### G14. Memory log and import (M4, M6)

- **Today.** `memory-wal.jsonl` records project, placement, source, session id and tags per operation in plain text. The SQLite `Import` inserts content and summary without sealing them, so imported rows are plain in an encrypted store (the Postgres path seals).
- **Cost.** The log takes the per-line `ENC:` form that already exists for the discussion log. The import fix is one call.
- **Risk.** Low. Both are defects against the stated promise.
- **Recommendation: encrypt now.**

#### G15. Memory export and the other export surfaces (M5, E2, E3)

- **Today.** The SQLite memory export writes ciphertext no other install can read; the Postgres export decrypts and writes plain JSON; `datawatch export` decrypts everything it reads but reads two stale names (`sessions.json`, `schedules.json`), so it silently omits the current session records and the schedule store.
- **Cost.** Small code. The real work is a decision about intent (below).
- **Risk.** A plain export is a copy of the operator's data, so it must be a visible, deliberate act. A ciphertext export that cannot be restored elsewhere fails both goals.
- **Recommendation: leave plain, with reason** (a decrypting export is the feature) **but fix now**: the file list, an explicit confirmation, and an audit entry. Offer a passphrase-sealed export as a later option. Make the memory export behave the same on SQLite and Postgres. The operator decides the default (Storage direction, "Decisions that stay with the operator").

#### G16. Keyword search on sealed memory (M7)

- **Today.** The fallback `content LIKE ?` runs against ciphertext and finds nothing.
- **Cost.** Decrypt-and-scan in memory is O(rows) per query and fine below tens of thousands of rows; above that a blind index of tokens is needed.
- **Risk.** A functional regression that appears only when `--secure` is on, so ordinary tests do not see it.
- **Recommendation: encrypt now** (fix the fallback to scan decrypted rows, with a row cap), as part of the memory fixes.

### Observability, audit and logs

#### G17. Audit and access logs (O1) and config-write audit lines (F3)

- **Today.** `audit.log` and `access.log` (JSON lines) and the CEF mirrors are plain. The CEF files exist so a SIEM can read them.
- **Cost.** Per-line sealing for the JSON files is cheap and survives a crash (one line at a time). Operators lose `tail`/`grep` and need the daemon's audit query. Confidentiality is the lesser concern for these files; tamper evidence is the greater one (a hash chain would serve that, and is a separate matter).
- **Risk.** Moderate: runbooks that grep these files break (C3).
- **Recommendation: encrypt behind `--secure` only** for the JSON files and the config-write audit file; **leave plain, with reason** for the CEF mirrors (their purpose is an outside reader), and make the CEF mirror an explicit opt-in when `--secure` is on.

#### G18. Tool-call trace (O2)

- **Today.** `trace/<session>.jsonl` and `trace/index.json` are plain but already redacted (home paths scrubbed, arguments capped or dropped by mode).
- **Cost.** Per-line sealing; the index holds session ids and can stay readable or be sealed whole. Trace queries and rollups scan the files, so they decrypt on every scan.
- **Risk.** Low to medium (scan time on large traces; C1).
- **Recommendation: encrypt behind `--secure` only.**

#### G19. Web search history (O3)

- **Today.** `websearch.db` stores the query text of every search, with provider, session and outcome. The daemon and the separate `mcp-search` process both open the file.
- **Cost.** Sealing the query column is easy for the daemon. The second process would need the key, which means passing it through the environment or a descriptor (widening where the key lives) or making `mcp-search` call the daemon instead of opening the file. The latter is the cleaner design.
- **Risk.** Medium: a change to a second process's contract.
- **Recommendation: encrypt behind `--secure` only**, after `mcp-search` stops opening the database directly.

#### G20. Council runs and documentation trust files (O5, O4)

- **Today.** Council debate transcripts, persona drafts (SQLite, removed after a retention period) and the two documentation trust files are plain. Documentation vectors and indexed text are public product documentation.
- **Cost.** Run files are whole-file writes; the drafts database is short-lived (a column seal is enough); trust files are tiny.
- **Risk.** Low.
- **Recommendation: encrypt behind `--secure` only** for runs, drafts and the trust files; **leave plain, with reason** for persona YAML files (shared and hand-edited) and for the documentation vectors (public text, needed for search).

#### G21. Identity file and its backups (E1)

- **Today.** `identity.yaml` and every earlier saved version are plain.
- **Cost.** Small whole-file writes; the file is changed through the identity API and CLI, so the daemon can seal it. Confirm no operator workflow edits it by hand.
- **Risk.** Low.
- **Recommendation: encrypt behind `--secure` only.**

### Key lifecycle, tooling and documentation

#### G22. Key lifecycle (E6, How the secure layer works)

- **Today.** The data key uses the weaker Argon2id setting; stores carry no key id; there is no password-change or rotation command; nothing backs up the salt, which sits in the `config.yaml` header.
- **Cost.** A new format header with a key id and the KDF parameters, a `rekey` command that walks every store (needs the registry in G23), and a stronger derivation applied at the same time. Large, and every earlier gap's migration is cheaper once it exists (C2).
- **Risk.** Highest consequence in the audit if wrong: a failed rotation can strand data. It needs a dry run, per-store verification and a documented rollback.
- **Recommendation: encrypt now** in the sense of building the format first, ahead of the large migrations.

#### G23. Status, migrate and wipe cover a fixed short list (E4)

- **Today.** `security encryption status|migrate` and `security wipe-plaintext` (and the matching REST routes) name a short, hard-coded list (discussion files, `channel_routing.json`, `servers.json`, `skills.json`, `compute/nodes.json`, `inference/llms.json`). Newer stores, the Automaton store and the tracker files are not in it, so the status report can say "all encrypted" while large stores are plain.
- **Cost.** Replace the list with a registry in which every store declares its path, format and migrate function, and have status count plain and sealed files per store. Medium, mostly mechanical.
- **Risk.** Low, and it reduces the risk of every other fix.
- **Recommendation: encrypt now.** It is the verification tool for the rest. Optionally show peer secure state in federation status (C6).

#### G24. Documentation disagrees with the code (Claim check)

- **Today.** Ten differences are listed in the Claim check, among them the wrong file names, a setting (`memory.encrypt_content`) that does not exist, a rotation command that does not exist, and a statement that `session.json` is encrypted that holds only in one mode.
- **Cost.** One documentation change; no code.
- **Risk.** False assurance. An operator who believes the page is relying on protection that is not there.
- **Recommendation: encrypt now** (correct `docs/encryption.md` first; update it again as each fix lands).

#### G25. Log files that accumulate and the crash log (O6, O7)

- **Today.** `daemon-app.log` is sealed but accumulates across restarts (the docs say it is cut on restart). `daemon-crash.log`, the restart log and standard error are plain; the first two are written while the process is failing, possibly before a key exists.
- **Cost.** Add retention or rotation to `daemon-app.log`. Sealing the crash log would need the key to survive a crash path; a crash handler that depends on key state is fragile.
- **Risk.** Low.
- **Recommendation: encrypt behind `--secure` only** for retention of `daemon-app.log`; **leave plain, with reason** for the crash log (a crash handler must not depend on key state); its content is a stack trace, so the fix is to check it does not print configuration or arguments.

#### G26. Transit verification defaults (F5)

- **Today.** Not an at-rest gap. Verification can be switched off per peer, and worker observer pushes skip verification by default.
- **Cost.** Changing the default can break existing worker setups that rely on self-signed certificates; they need a pinned fingerprint or a certificate the worker trusts.
- **Risk.** Medium: connectivity breaks on upgrade if the default flips silently.
- **Recommendation: encrypt now** in the narrow sense of fixing the default with a visible warning and a migration note. It sits outside `--secure`.

## Cannot be encrypted

These rows cannot be sealed by datawatch because something else must read the bytes, because they are not data at rest, or because they are the root of the secure feature. Each row says what to do instead. "No" means no for datawatch's own secfile layer; the column "Instead" names the control that applies.

| Id | Why it cannot be sealed | Instead |
|----|-------------------------|---------|
| K0 | The password and derived key are the root of trust. Something has to hold them: the prompt, the environment of the service, process memory. The salt must be readable before any key exists, so it sits in the clear in the `config.yaml` header. | Keep the password out of shell history and unit files where possible (a secrets manager that injects the environment at start); keep the key zeroed on exit (done); back up `config.yaml` with the data (C4). |
| K9 | `.mcp.json` is read by the launched tool (claude-code, opencode and others), so it must be plain. The per-session write carries a scoped token; the instance-level write carries the daemon's `server.token`. | Stop writing the instance-level token into project files and pass a scoped, short-lived token instead (fix 13). Keep 0600 and remove the file when the session ends. |
| S10 (project copy) | The platform rules copy in the project directory's `CLAUDE.md` is read by the launched tool. The tracker-folder copy is in G6. | None needed beyond 0600: the text is rules and the session task, written by the daemon. |
| S15, X2 | Pane text exists only in the tmux server's memory and in the daemon's memory while it is processed; nothing is at rest. The launched tool itself sees plain text. | Keep the dedicated tmux socket folder at mode 0700 (check it is); disk encryption covers swap. |
| M1 (embedding, placement columns), O4 (vectors) | Vector search needs plain vectors in the database engine, and the namespace and project columns are filter keys. | Leave plain; a different approach (in-memory vector index) is a design decision, listed in Storage direction. |
| A7 (decompose output) | If the file is written by a launched session rather than by the daemon, the daemon cannot seal it. Who writes it was not traced. | Trace the writer before the first Automaton-store fix; seal it if the daemon writes it, otherwise leave it with 0600 and remove it when the decompose finishes. |
| K10 (certificates), K8 (hashed widget tokens) | Certificates are public. Widget tokens are already bcrypt hashes with nothing recoverable. | None. |
| O6 (`daemon.log`) | The parent process writes it before the key exists. | Move the first lines of boot output to standard error only and keep secrets out of boot messages; service-manager journal controls apply. |
| O7 | Crash logs, the restart log and the journal are written while the process is failing or by the process manager. | Keep crash output free of configuration and arguments (G25); journal access controls belong to the host. |
| F4 | The stats agent's `peer.token` and the federation token file are read by a separate process that must read them in the clear. The federation token default is a relative path, so its location depends on the working directory. | Pass the token as a `${secret:name}` reference or an environment value at launch instead of a file, 0600 meanwhile; make the default an absolute path under the data directory. |
| F5 | Transit, not at rest. TLS handshake metadata (server name, certificate, sizes and timing) is visible to an observer on the network by design, unless an encrypted client hello is available end to end, which is not a choice datawatch can make alone. | Verify certificates by default (G26); use Tailscale or another overlay for peer links. |
| F6 (Matrix registration, signal-cli store) | The Matrix application-service registration file is written so the homeserver can read it, so it must be plain. The signal-cli keystore belongs to that tool. | 0600 and an operator-chosen path outside shared folders. See the note below the table. |
| R1, R2, R3 | Git and the launched tools need plain files. Task worktrees, merge and trial copies are full working trees. | Disk-level encryption of the volume that holds the project and worktree folders. |
| R4 | The launched tool reads the task file. A crash before cleanup leaves it behind. | Put task capture files in a per-user memory-backed folder instead of the shared temp folder, so a crash and a reboot both remove them (fix 12); the existing sweep stays. |
| R5 | The consumers are ffmpeg, whisper and an LLM. The files last one request. | Same memory-backed folder (fix 12). |
| R6 | The commit guard hook scripts are executed by git; the commit token inside is random per run. | Same memory-backed folder; mode 0700 (done). |
| R7 | Settings files, rules files, memory hook scripts and operator-supplied files are read by the launched tools and git. | None beyond 0600 and the project's own protection. |
| X1, X3 | The LLM tools' own state, Postgres, signal-cli, secrets backends and model servers are owned by those tools. | Disk-level encryption; for Postgres, the options in Storage direction. |

**Differences from the inventory.** The inventory marks the Matrix registration file "yes" because it is a plain file written by datawatch. On reading its purpose again, it exists for the homeserver to read, so it cannot be sealed. The inventory text is left as written; this section is the corrected view. Nothing was run to confirm which process reads the file, so the first task of any change here should confirm it.

## Storage direction

The question this section answers: does the choice of how stores are laid out (files with an index, an optional Postgres mode, or SQLite as the source of truth) change what `--secure` can protect? Short answer: yes. The layout decides which fields can be sealed, where plain copies appear, and whether a key can reach the data at all.

### What is decided and what is open

On 2026-11-10 the operator chose, for reports and dashboards, that files stay the source of truth and a derived SQLite reporting index, rebuildable from the files, serves reports; a migration of the source of truth to a database is a possible later version, revisited if database use grows (recorded in the autonomy handover plan, "Reporting data store"). This audit takes that as the starting point and does not reopen it. What it adds is the effect of each option on encryption, so the later decision is made with that in view.

### The three layouts compared

| | Files with a derived index (current direction) | Optional Postgres mode | SQLite as source of truth |
|-|-----------------------------------------------|------------------------|---------------------------|
| Source of truth | One file per record (`secfile`), index rebuilt from files | Postgres tables | One SQLite file per domain |
| File-level sealing (`secfile`) | Works record by record, keeps the write pattern (only changed records are rewritten), accepts mixed plain and sealed during a migration | Not applicable to table rows; applies only to dumps and exports | Not workable on a live database (random page writes); possible only as seal-at-shutdown with a plain working copy in memory, which loses writes on a crash |
| Column-level sealing | Not needed for the source; needed for any text copied into the index | Works with the key held in the daemon (already used for memory `content` and `summary`); the server never sees plaintext but cannot index or search sealed columns | Works the same way (already used for memory) with the same loss of server-side search |
| Whole-database encryption | Only the index could use it, and only with a driver that supports it | Not a datawatch feature; disk or volume encryption, or a vendor extension (see below) | SQLCipher and SEE need a C build; the current driver (`modernc.org/sqlite`, pure Go, released builds use `CGO_ENABLED=0`) has none. A custom encrypting VFS is possible in principle in pure Go but would be unproven code that datawatch owns |
| Transparent encryption extensions | n/a | PostgreSQL core has no transparent data encryption; vendor or extension builds exist, each its own dependency; key held by the server or its key manager, not by datawatch | n/a (see whole-database row) |
| Key handling | The data key stays in the daemon | Data key stays in the daemon for column sealing; the connection URL and its credentials sit in `config.yaml`, so the K1 gap matters more; a server-side function that takes a key (for example `pgcrypto`) sends the key and plaintext to the server and can land them in its logs, so it is not recommended | Same as files |
| Other places plain data appears | The index itself | Server data files, its write-ahead log and archives, `pg_dump` output, replicas | The database file, its `-wal` and `-shm` files in WAL mode (the memory store uses WAL), and SQLite temporary files |
| Backups and crash recovery | Per-file, already safe; backup is a copy of the folder | Server's own tools (`pg_dump`, base backups); sealed columns stay sealed in a dump, the rest do not | Database file plus its WAL files must be copied together |
| Two code paths | One | Two (SQLite and Postgres), so each security fix is done twice; the memory store already has both | One |
| Search and filtering needing plain data | Handled by readable header fields in each record, as in the plan records and the session archive | Server-side search and indexes work only on plain columns | Same as Postgres |

### Effect on the three options

**Files with an index.** This option keeps the largest store sealable and keeps the migration interruptible. The new exposure is the index. A derived index that copies free text (titles, task text, learnings) into a plain SQLite file defeats the sealing of the files it was built from. Three ways to keep it safe, in order of cost:

1. Keep text out of it: store ids, states, times, counts, costs and tags, and look text up in the sealed record on demand. The index then holds only metadata and needs no sealing of its own.
2. Make it disposable: rebuild it at start under `--secure` from the sealed files, keep it on a memory-backed path, and delete it at shutdown. The cost is a full read of the store at start, which the daemon does today.
3. Seal the columns that hold text, accepting that those columns cannot be searched by the database.

The same pattern (a readable header with the fields queries need, plus a sealed body) answers the Automaton store's start-up cost (G10): list and filter views read headers, and bodies are decrypted when one record is opened.

**Optional Postgres mode.** It adds a second implementation of every store that adopts it, so every sealing fix is paid twice, and it moves the data to a service that datawatch does not control (its storage, backups and replicas). Column sealing with the key in the daemon is the only option that keeps plaintext off the server; it removes server-side search on those columns. Volume encryption on the database host and TLS with verified certificates on the connection are required alongside it, not instead of it. The connection URL and credentials are in `config.yaml` or the vault, so the key-lifecycle gaps (G1, G22) apply first.

**SQLite as the source of truth.** The pure-Go driver cannot encrypt a whole database, so the realistic protection is column sealing, with WAL and shared-memory files and temporary files left as extra plain locations to check. It would merge several stores into larger files, so one sealed column or a plain index column decides how much is exposed. It keeps one code path. It is also the largest change in how the Automaton store is read and written, because today's per-record file layout and its crash and migration behaviour would be replaced.

### Recommendation

This does not decide scope; the operator does that (see below).

1. **Keep the 2026-11-10 direction** (files as source of truth, a derived index for reports) as the layout that costs least to protect, and add three rules to that work: the index holds no free text unless the columns are sealed; it is rebuildable and covered by the status and wipe registry (G23); and it is deleted or rebuilt when secure mode changes.
2. **Adopt one record pattern for sealed stores that need filtering**: a readable header (ids, state, project, times) and a sealed body. The plan records and the session archive already do this; the Automaton store should follow it (G10).
3. **Do not move a store to Postgres or to SQLite as the source of truth for encryption reasons.** Neither gives datawatch a whole-store key without a new build dependency or a custom component. If a move is chosen for other reasons, treat column sealing with the key in the daemon as the baseline and put the volume encryption and verified TLS requirements in the move's plan.
4. **Memory stays on column sealing.** It already works that way on both SQLite and Postgres. Embeddings stay plain unless the operator chooses the in-memory vector approach.

### Decisions that stay with the operator

These change scope or behaviour that the audit does not decide:

- Whether any of the fixes below are in the 10.0 scope.
- Whether the reporting index may hold free text, and if so whether to seal it, rebuild it at start, or keep text out (Storage direction, option list above).
- Whether `secure_tracking: full` becomes the default under `--secure` (G6), given that tracker files stop being readable with `cat`.
- Whether embeddings stay plain (the audit's default) or move to an in-memory index (G12).
- Whether a plain export is the default with a confirmation, or the default is a passphrase-sealed export (G15).
- Whether to stop writing the instance-level server token into project files, which changes how launched tools authenticate (row K9, fix 13).
- Whether the Automaton store migration (G10) runs automatically at start, or only on an explicit command with progress.

## Prioritised fixes

Each item below is meant to be filed as one of the **separate Automata** that deliver the work; an item marked **L** needs a design step or a split into smaller Automata before it runs. The list is ordered by exposure first (credentials, then conversation content, then history and metadata), and then adjusted so that every item comes after what it depends on. Sizes: **S** is a handful of tasks in one Automaton, **M** is a normal Automaton, **L** is more than one Automaton or begins with a measurement and a design record. Titles are written as the title the Automaton would carry and contain no version number or tracker id.

| Order | Title | Size | Depends on |
|-------|-------|------|------------|
| 1 | Keep the encrypted config file encrypted when settings change | M | none |
| 2 | Correct the encryption page to match the code | S | none |
| 3 | Seal the plain token and credential files | M | none |
| 4 | Close the plain session output paths and the restart recording gap | M | none |
| 5 | Seal the tracker files and stop committing plain history | M | none |
| 6 | Seal the vault key and the daemon-generated TLS keys when secure mode is on | M | 1 |
| 7 | Replace the fixed encryption file list with a registry of stores | M | none |
| 8 | Add a key id to the sealed formats and a command to change the password | L | 1, 7 |
| 9 | Close the memory gaps that need no redesign | M | none |
| 10 | Seal the small operational stores and the plain copies left by migrations | M | 7 |
| 11 | Seal the Automaton store and the spend files | L | 7, 8 |
| 12 | Move task capture files and temporary media to a memory-backed folder | S | none |
| 13 | Stop writing the instance-level server token into project files | S | none |
| 14 | Fix the export commands and make a plain export a visible choice | S | none |
| 15 | Seal the audit, trace, council and identity files | M | 7, 8 |
| 16 | Seal web search history and stop the second process from opening the database | M | 8 |
| 17 | Add a blind index for the memory knowledge graph | M | 9 |
| 18 | Decide and build the protection for memory embeddings | L | 9, operator decision |
| 19 | Verify peer TLS certificates by default | S | none |
| 20 | Retention for the application log and a check of the crash log | S | none |

Items 2, 3, 4, 5, 7, 9, 12, 13, 14, 19 and 20 have no dependency and can run in parallel with item 1; nothing in them is blocked by it. Items 11, 15 and 16 should not start before 8 so the large stores are rewritten once.

### 1. Keep the encrypted config file encrypted when settings change

- **What.** First reproduce, on a throwaway data directory, what happens when a running secure daemon changes a setting: confirm whether `config.yaml` becomes plain, and whether the next start can still derive the data key. Then route every runtime write through the envelope (or carry the key in the config object), reuse the derived envelope key for the life of the process, serialise writes, and make `Save` refuse to overwrite an encrypted file with plain YAML.
- **Why.** G1. It exposes every token in the file, and if the salt is lost it can strand every other sealed store.
- **Risk.** A write guard can block a legitimate plain write on a non-secure install; the guard must key off the file's current state. Argon2id at 512 MiB per save must not run on hot paths (token rotation, ACME renewal).
- **Covers.** K1, G1.

### 2. Correct the encryption page to match the code

- **What.** Rewrite `docs/encryption.md` against the Claim check: file names, the real memory behaviour (follows `--secure`, no `memory.encrypt_content`), the fact that no rotation command exists yet, `secure_tracking` and what it does, and the streaming log's nonce size and append behaviour. Add the list of what is still plain, taken from the inventory, with a pointer to this audit.
- **Why.** G24. False assurance is an exposure.
- **Risk.** None to behaviour. It must be revised again as items 1 to 11 land.
- **Covers.** G24, the Claim check.

### 3. Seal the plain token and credential files

- **What.** Seal `service_tokens.json`, `session_tokens.json`, `auth/tokens.json`, `devices.json` and plugin widget payloads with the existing whole-file layer; seal `auth/audit.jsonl` per line. Trace the file secrets provider and remove it or seal it. Search for CLI paths that read these files without a running daemon and fix them first.
- **Why.** G3, G2 (K3). These are working credentials in clear.
- **Risk.** Low. A CLI command that reads a file without the key would see ciphertext.
- **Covers.** K3, K4, K5, K6, K7, K8, G2, G3.

### 4. Close the plain session output paths and the restart recording gap

- **What.** Write subprocess and ACP session output through the encrypting log writer and make every reader use the same layer. Re-create the encrypting pipe after a daemon restart and append to the existing log. Confirm how a torn last block is handled by the reader and the append-open.
- **Why.** G5. Session output is the largest body of conversation text and is plain today for two session types.
- **Risk.** Two writers interleaving blocks during re-pipe; readers that parse `output.log` directly.
- **Covers.** S6, S7, S5 (verification), G5.

### 5. Seal the tracker files and stop committing plain history

- **What.** Carry the data key through `ResumeTracker`, seal the initial and rewritten tracker files (`session.json`, `task.md`, `README.md`, `conversation.md`, `timeline.md`, backend state), fix the startup migration's target folder, document `secure_tracking`, and choose and build one of the two history approaches (commit only a manifest, or re-initialise the tracker repository on migration). Decide the default with the operator.
- **Why.** G6, G7. Plain task text and prompts sit next to a sealed log.
- **Risk.** Mixed plain and sealed files; tracker repository growth with random nonces; losing `cat` access.
- **Covers.** S8, S9, S10 (tracker copy), S11, S12, G6, G7.

### 6. Seal the vault key and the daemon-generated TLS keys when secure mode is on

- **What.** Wrap the vault key with the data key and remove the plain key file after verifying the unwrap; seal daemon-generated and ACME private keys and load them from memory. Leave operator-supplied key files untouched.
- **Why.** G2, G4. The vault's lock and key are in one folder today.
- **Risk.** A crash between steps leaves two copies (order: wrap, sync, verify, remove). A lost password now also loses the vault key.
- **Covers.** K2, K10, G2, G4.

### 7. Replace the fixed encryption file list with a registry of stores

- **What.** Give every store a registration (path pattern, format, migrate function, whether plain copies can be wiped). Make `security encryption status|migrate` and `security wipe-plaintext` and their routes walk the registry and count plain and sealed files per store, including the Automaton store and tracker files.
- **Why.** G23. The status report cannot say what the fixes achieved without it.
- **Risk.** Low. Status must be cheap on a store of hundreds of MB (count by header, do not decode).
- **Covers.** E4, G23, and the verification step of every later item.

### 8. Add a key id to the sealed formats and a command to change the password

- **What.** Design record first: a new format header with a key id and the KDF parameters, read compatibility for every existing marker, a stronger derivation for new data, and a command that re-seals every registered store with progress, verification and rollback. Needs a decision on what happens to backups taken under the old key.
- **Why.** G22. Without it, every later migration is a one-way door.
- **Risk.** The largest consequence if wrong; needs a dry run and a stored-copy test before the first live run.
- **Covers.** E6, G22, and the first sentence of C2.

### 9. Close the memory gaps that need no redesign

- **What.** Seal imported rows in the SQLite store; seal `memory-wal.jsonl` per line; replace the content hash with a keyed hash; make the keyword fallback scan decrypted rows with a cap; make the SQLite and Postgres exports behave the same; require a verified TLS mode for an off-host Postgres URL.
- **Why.** G12 (hash, Postgres), G14, G15 (memory export), G16.
- **Risk.** The keyed-hash pass must be resumable. A keyword scan over a very large store is slow, so the cap and a log line when it applies are needed.
- **Covers.** M1 (hash), M3, M4, M5, M6, M7, E3, G12, G14, G15, G16.

### 10. Seal the small operational stores and the plain copies left by migrations

- **What.** Seal exit hooks, queue, discussion subscriptions, operator questions and decision records, judge copies in the Automaton scratch folder, and the event-history and cache files; seal new migration backup copies and teach the wipe command to find existing plain ones, with the SSD caveat in the docs. Leave hand-edited YAML and hash-only files plain.
- **Why.** G8, G9. Operator reasoning and task text sit in these files.
- **Risk.** Low. Trace who writes the decompose output before touching the scratch folder.
- **Covers.** S4, S14, A3, A4, A7 (judge copies), A8, F3, E4 (copies), G8, G9.

### 11. Seal the Automaton store and the spend files

- **What.** Begin with a measurement of load time and peak memory on a copy of a real store. Then adopt a record envelope (readable header, sealed body), seal records and side files, key the side-file names, and add a one-time migration command with progress and resume. Seal the spend and usage files with the same work and sequence them with the reporting index decision. Split into several Automata if the measurement shows start-up cost needs lazy loading.
- **Why.** G10, G11. It is the largest store and holds prompts, worker output and judge text, with no key path today.
- **Risk.** Longest rewrite, mixed store during it, direct readers of the files break, start-up time. Do not start before 7 and 8.
- **Covers.** A1, A2, G10, G11.

### 12. Move task capture files and temporary media to a memory-backed folder

- **What.** Put the task capture files, the pi scratch folders, the claude-code starting-context file, temporary media and the commit guard scripts in a per-user memory-backed folder (mode 0700) rather than the shared temp folder, with a fallback when none exists. Keep the existing cleanup sweep.
- **Why.** R4, R5, R6 cannot be sealed, so reduce how long they can survive a crash.
- **Risk.** Memory-backed folders have size limits; large media needs a size check and a fallback.
- **Covers.** R4, R5, R6.

### 13. Stop writing the instance-level server token into project files

- **What.** Replace the daemon's own `server.token` in the instance-level `.mcp.json` with a scoped, short-lived token (as the per-session write already does), and remove the file when the session ends. Check each launched tool still authenticates.
- **Why.** K9 cannot be sealed, so the fix is to put less in it.
- **Risk.** A launched tool or an operator workflow may depend on the long-lived token; this is an operator decision.
- **Covers.** K9.

### 14. Fix the export commands and make a plain export a visible choice

- **What.** Make `datawatch export` read the current names (session records, schedule store) and the Automaton store, add an explicit confirmation and an audit entry for any plain export, and offer an optional passphrase-sealed output.
- **Why.** G15. The command silently omits stores today.
- **Risk.** An operator script that relies on the current quiet behaviour needs a flag.
- **Covers.** E2, G15.

### 15. Seal the audit, trace, council and identity files

- **What.** Seal `audit.log` and `access.log` (JSON lines), the config-write audit file, trace files and index, council runs, drafts and trust files, and `identity.yaml` with its backups. Keep the CEF mirrors plain and make them an explicit opt-in under secure mode; document the audit query as the way to read sealed logs.
- **Why.** G17, G18, G20, G21.
- **Risk.** Runbooks that `grep` these files break; scan time on large traces.
- **Covers.** O1, O2, O4 (trust files), O5, E1, F3 (audit lines), G17, G18, G20, G21.

### 16. Seal web search history and stop the second process from opening the database

- **What.** Make `mcp-search` read and write the history through the daemon, then seal the query column.
- **Why.** G19. Search queries are a record of what the operator was working on.
- **Risk.** A contract change for a second process; the process must handle the daemon being down.
- **Covers.** O3, G19.

### 17. Add a blind index for the memory knowledge graph

- **What.** Store an HMAC of each normalised name for lookups and a sealed value for display, and migrate every graph row.
- **Why.** G13.
- **Risk.** Every graph query path changes; prefix search is lost.
- **Covers.** M2, G13.

### 18. Decide and build the protection for memory embeddings

- **What.** The operator decides whether embeddings stay plain. If they move, build an in-memory vector index loaded at start from sealed vectors, with a memory-size check against `memory_stats` first. If they stay plain, record the reason in the docs and close the item.
- **Why.** G12. Embeddings leak the topic of the text.
- **Risk.** A search redesign, with memory use and start-up cost.
- **Covers.** M1 (embedding, placement), G12.

### 19. Verify peer TLS certificates by default

- **What.** Change the default for worker observer pushes and per-peer links to verify, with a visible warning and a documented way to pin a self-signed certificate for existing setups.
- **Why.** G26. It is outside `--secure` but is part of how sent data is protected.
- **Risk.** Connectivity breaks on upgrade for setups that rely on skipping verification.
- **Covers.** F5, G26.

### 20. Retention for the application log and a check of the crash log

- **What.** Add retention to `daemon-app.log` (the docs promise a cut on restart), and check that the crash log, the restart log and the config-write audit sink never print configuration values or launch arguments.
- **Why.** G25.
- **Risk.** Low.
- **Covers.** O6, O7, G25.

### Coverage cross-check

Every inventory row appears in exactly one of: a gap (G), "Cannot be encrypted" (C), or "done". "Done" means the store is already fully covered when `--secure` is on, with the caveat noted.

| Rows | Where |
|------|-------|
| K0 | Cannot be encrypted |
| K1 | G1, fix 1 |
| K2, K3 | G2, fixes 3 and 6 |
| K4, K5, K6, K7, K8 (widgets) | G3, fix 3 |
| K8 (tokens) | Cannot be encrypted (hashed) |
| K9 | Cannot be encrypted, fix 13 |
| K10 | G4, fix 6 (keys); Cannot be encrypted (certificates) |
| K11 | Done |
| S1, S2, S3, S5, S13 | Done (S1: quiet old records stay plain until rewritten; covered by the registry, fix 7) |
| S4 | G8, fix 10 |
| S6, S7 | G5, fix 4 |
| S8, S9, S10 (tracker copy), S12 | G6, fix 5 |
| S10 (project copy) | Cannot be encrypted |
| S11 | G7, fix 5 |
| S14 | G9, fix 10 |
| S15 | Cannot be encrypted |
| M1 | G12, fixes 9 and 18; Cannot be encrypted (embedding and placement columns) |
| M2 | G13, fix 17 |
| M3 | G12, fix 9 |
| M4, M6 | G14, fix 9 |
| M5 | G15, fixes 9 and 14 |
| M7 | G16, fix 9 |
| A1, A2 | G10, G11, fix 11 |
| A3, A4, A7 (judge copies), A8 | G9, fix 10 |
| A5, A6 | Done |
| A7 (decompose output) | Cannot be encrypted (pending trace), fix 10 |
| O1 | G17, fix 15 |
| O2 | G18, fix 15 |
| O3 | G19, fix 16 |
| O4 | G20, fix 15 (trust files); Cannot be encrypted (vectors) |
| O5 | G20, fix 15 |
| O6, O7 | G25, fix 20; Cannot be encrypted (`daemon.log`, journal, crash output) |
| F1, F2 | Done |
| F3 | G9 and G17, fixes 10 and 15; hash-only files plain with reason |
| F4 | Cannot be encrypted |
| F5 | Cannot be encrypted (transit), G26, fix 19 |
| F6 | Cannot be encrypted |
| E1 | G21, fix 15 |
| E2 | G15, fix 14 |
| E3 | G15, fix 9 |
| E4 | G8, G23, fixes 7 and 10 |
| E5 | Done |
| E6 | G22, fix 8 |
| R1 to R3 | Cannot be encrypted |
| R4, R5, R6 | Cannot be encrypted, fix 12 |
| R7 | Cannot be encrypted |
| X1, X2, X3 | Cannot be encrypted |
| Claim check | G24, fix 2 |

## Parity surface

This is an audit document. It changes no behaviour and adds no setting, endpoint, command or screen, so every surface is unchanged by this document. The fixes it leads to will each need their own parity list.

| Surface | Touched by this document | Reason |
|---------|--------------------------|--------|
| REST | No | Documentation only. The `/api/security/encryption/*` routes are described, not changed. |
| MCP | No | Documentation only. |
| CLI | No | Documentation only. `datawatch security` and `datawatch export` are described, not changed. |
| Comm channel | No | Documentation only. |
| YAML/config | No | No setting added. `secure_tracking` is noted as undocumented; documenting it belongs to the fix. |
| PWA | No | No screen change. |
| Android | No | Nothing for the app to mirror; the app's stored data is outside this audit. |
| iPhone | No | Same as Android. |

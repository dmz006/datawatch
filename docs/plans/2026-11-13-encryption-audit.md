# Encryption audit: what datawatch stores and what `--secure` covers

- **Date:** 2026-11-13
- **Version audited:** v9.172.5 (tree at the commit that set that version)
- **Scope:** every place the daemon, its CLI and the processes it launches keep data on disk or hand data to another process: the data directory, per-project working copies, temporary files, exports and backups, and the connections between peers. Third-party tools that datawatch launches are listed where they leave data behind, but their own storage is not datawatch's to encrypt.
- **Purpose:** the operator asked for an encryption audit before 10.0 so that anything that can be encrypted can be, with the secure feature (`--secure`). This document is the evidence base for that work: it says what each store holds, what protects it today, and whether `--secure` can cover it.
- **Status:** the header, the method note and the inventory are written. The sections Gaps, Cannot be encrypted, Storage direction and Prioritised fixes are headings only and are filled in by the next task.
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

> **Not yet written.** The next task fills this section in.
>
> Seed list: facts confirmed above that this section should turn into numbered gaps, with the row ids to cite:
> runtime `config.Save` rewrites `config.yaml` in plain text (K1); the vault key sits beside the vault and is independent of `--secure` (K2); plain token files (K4, K5, K6, K7, K9); tracker files and `session.json` are plain by default and after a restart (S8, S9, S10); plain output from subprocess and ACP sessions (S6); no re-pipe of encrypted sessions after a restart (S7); the Automaton store, its ledgers and caches have no key path at all (A1 to A4); memory metadata, graph, WAL, SQLite import and export (M1, M2, M4, M5, M6); audit, trace, websearch, council, identity files (O1, O2, O3, O5, E1); weak data-key derivation and no rotation (How the secure layer works, E6); status, migrate and wipe cover a fixed short list (E4); documentation that disagrees with the code (Claim check).

## Cannot be encrypted

> **Not yet written.** The next task fills this section in. Start from the rows marked **no** above: K0, K9, S15, O6 (`daemon.log`), O7, F5, R1 to R7, X1 to X3.

## Storage direction

> **Not yet written.** The next task fills this section in. It should say which stores should move to a form that `--secure` can cover (for example the Automaton store, whose size makes whole-file sealing a load-time question), and which should stay plain on purpose.

## Prioritised fixes

> **Not yet written.** The next task fills this section in, ordered by exposure (credentials first, then conversation content, then history and metadata), with the Gaps section as input.

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

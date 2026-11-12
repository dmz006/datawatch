# Plan: Turn-Chunked Session History (BL436 — design pass)

- **Date**: 2026-10-14
- **Version**: v9.5.26 (top released entry of CHANGELOG.md at execution time)
- **Status**: needs_review

## Scope

This is a **design pass only — no code implementation**. It produces this document and nothing else. No Go code is written or changed, and the live WebSocket streaming path (`output` / `raw_output` / `pane_capture` frames) is **not** modified; the only statements made about that path are compatibility constraints (see Rendering).

The goal: give each session a history that is addressable *per turn* (the span between one prompt and the next), so scrollback and rendering no longer depend on tmux's pane scrollback, and so a session that is reused, resumed or respawned does not appear to "overwrite" its earlier output.

## Provenance note (corrections to the task brief)

The brief names a prior investigation report (`.datawatch/reports/turn-chunked-history-turn-history-investigation.md`) and a verify script (`.datawatch/verify/turn-chunked-history-design-doc-landed.sh`). **Neither file exists in this worktree.** The only BL436 script present is `.datawatch/verify/bl436-design-doc-landed.sh`, which checks this same doc path and was used for verification. Because the report is missing, every finding below was re-derived directly from the code (`internal/session/manager.go`, `tmux.go`, `tracker.go`, `internal/secfile/*`, `internal/server/*`) rather than cited from the report. Line numbers are as of this commit and will drift.

## Investigation findings

Investigation findings below are re-derived from code (the prior report is missing; see the provenance note).

### 1. Mechanism behind the "session overwrites" complaint

Confirmed from code, with one caveat that the complaint may have two distinct causes:

- **output.log is append-only on the plain path.** `TmuxManager.PipeOutput` runs `tmux pipe-pane -o -t <session> "cat >> '<logFile>'"` (`internal/session/tmux.go:326-330`) — `>>` append, never truncate. `RepipeOutput` (`tmux.go:340-360`) closes any existing pipe and reopens with the same `>>` form. `runSubprocess` opens the log `O_APPEND|O_CREATE` (`manager.go:3013`). The log path is `<sessionDir>/output.log` (`tracker.go:449`), built from the session's FullID, so it is stable per session.
- **No code path truncates the plain log.** A grep for `O_TRUNC`/`Truncate` finds only `secfile` (below) and unrelated timers. So the log itself does not lose history by being overwritten.
- **The `--secure` path has a truncation flag.** `secfile/logwriter.go:64-70` chooses `O_APPEND` only if the existing file begins with the log magic header; otherwise it opens `O_CREATE|O_WRONLY|O_TRUNC`. A `.enc` file that is empty, short, or has an unreadable header is therefore truncated on reopen. This is the only place an "overwrite" of persisted history can occur in code.
- **What the operator most likely sees is pane reuse, not file overwrite.** The *only* history the PWA and capture-based detection read live is the tmux pane: `StartScreenCapture` polls `capture-pane` (`manager.go:2226+`), and the four `capture-pane` variants (`tmux.go:259-312`) return the visible screen or a bounded scrollback. When a session is resumed/restarted onto the same pane, or a TUI repaints (and opencode's alternate screen is disabled specifically to keep scrollback, `manager.go:~1850`), the pane content for earlier turns is replaced even though `output.log` still holds the raw bytes. `TailOutput` (`manager.go:3860`) reads only the last 64 KB of the log (or decrypts the whole `.enc` file), and the raw log is escape-sequence/spinner noise (`manager.go:1006`), so the log is not a usable per-turn history today.

Conclusion: *history is not lost on disk in the plain case; it is unaddressable.* The design therefore needs turn boundaries layered on the existing log, not a new capture mechanism.

### 2. Existing prompt-sent / turn-ended signals, per mode

| Signal | Where | Meaning |
|---|---|---|
| `MarkInputSent` / `inputSentAt` | `manager.go:~3003`, field near `:381` | Operator or daemon delivered input to the session (prompt-sent). Also drives the 5-minute task-echo suppression of completion detection. |
| `DATAWATCH_COMPLETE:` | patterns `manager.go:187`; `completion_summary.go:8`; `oneshot_reap.go:13` | One-shot sessions: end of task; interactive: transitions to `StateWaitingInput`. |
| `DATAWATCH_NEEDS_INPUT:` | `manager.go:192` | Session asks a question; turn ends, awaiting operator. |
| Prompt/finish detection in `StartScreenCapture` | `manager.go:2226+` (visible-scan ~2496, scrollback-scan ~2539) | Screen-scrape completion for TUI backends. |
| State engine transitions | `state_engine.go` | Running ↔ WaitingInput ↔ Complete/Failed; structured-channel events (`message.completed`) for backends with a channel. |
| Subprocess exit code | `runSubprocess` (`manager.go:~3010`) | Subprocess mode: exit 0 → Complete, else Failed; no scraping. |

### 3. tmux-server crash incident status

**Root-caused and cited elsewhere in the repo; not re-investigated here.** `docs/plans/README.md` rows 84–85 (BL503/BL504) record that the production tmux server died on 2026-10-18 20:18:09 because a worker's sandbox helper ran `TMUX_TMPDIR=<dir> tmux kill-server`, which — since the `TMUX` variable takes priority over `TMUX_TMPDIR` inside a production pane — hit the production server. `docs/plans/2026-10-17-supervisor-rules-and-lessons.md:297` records the same cause. Whether any *other* tmux-server crash cause exists is **not found** in the code or docs; this design treats "tmux server dies for any reason" as an input to survive, not a bug to fix.

## Turn-boundary marking

A **turn** is the output produced between one *prompt-sent* event and the next. Turn 0 is everything before the first prompt (launch banner, backend startup).

| Mode | Turn starts at | Turn ends at |
|---|---|---|
| **Interactive** (operator/PWA/comm channel sends input) | `send_input` / `MarkInputSent` for operator-originated input | the next operator-originated input (which also opens turn N+1); `StateWaitingInput` is recorded as `ended_hint` but does not close the turn |
| **One-shot** | task delivery (`MarkInputSent` at spawn/delivery) | the existing `DATAWATCH_COMPLETE:` / `DATAWATCH_NEEDS_INPUT:` marker, or a terminal state (Complete/Failed/Killed); reuses the marker, adds no new one |
| **TUI backends** (claude-code, opencode) | same input events | completion-detection shapes already in `StartScreenCapture` (prompt-box scan, finish scan) or the structured-channel `message.completed` event when present; screen-scrape is the fallback |
| **Chat-mode backends** (ollama, openwebui) | request send | response-complete event from the chat client (structured; no scraping) |
| **Subprocess** | process start | process exit |

**Where the boundary is written and why:** at the daemon, in the session manager, at the moment the existing signal fires — *not* in the pane and not parsed back out of the log. The daemon already knows these instants (it sets `inputSentAt` and drives state changes); parsing boundaries from raw `output.log` bytes would be fragile because the raw stream is escape-code noise. A boundary record carries `{turn_number, started_at, byte_offset_start, trigger}` when it opens and `{ended_at, byte_offset_end, ended_hint}` when it closes. Offsets are read from the log's current size at the instant of the signal. Re-delivered input within the 5-minute task-echo window (the echo of a just-delivered task) does **not** open a new turn.

## Storage-shape comparison

### Option (a): physically separate per-turn files (`output.0001.log`, `output.0002.log`, …)

- **`--secure` interaction (bad).** Today a single `EncryptingFIFO` (`secfile.NewEncryptingFIFO(logFile+".pipe", logFile+".enc", key)`, `manager.go:1963`, `3556`) is wired once as the `pipe-pane` target. Rotating per turn means either re-pointing `pipe-pane` at a new FIFO each turn (a toggle race: bytes emitted between `pipe-pane` close and reopen are lost or land in the wrong turn) or running a rotating writer inside the FIFO. Either changes the encrypted pipeline that is the most security-sensitive code here. Also, the plaintext `RepipeOutput` after daemon restart is explicitly **skipped for encrypted sessions** (`manager.go:~4770`, BL263 follow-up) — rotation would compound that gap.
- **Small-file pressure (bad).** A long interactive session with hundreds of short turns yields hundreds of tiny files, each with its own header/nonce in the encrypted case, plus tracker git commits (`tracker.commitAll`) that already track the session dir.
- **Index/log consistency (good).** The file boundaries *are* the index; nothing can drift.

### Option (b): one continuous log plus a sidecar index of `{turn_number, byte_offset_start, byte_offset_end, started_at, ended_at}`

- **`--secure` interaction (good).** The log and FIFO are untouched. The index stores plaintext offsets into the *decrypted* stream, or — if the operator wants no plaintext metadata — is itself written through `secfile` encrypted writes. Reading a turn means decrypting the stream and slicing, which `NewEncryptedLogReader` already does (`TailOutput`, `manager.go:3860+`).
- **Small-file pressure (good).** Two files per session regardless of turn count.
- **Index/log consistency (the risk).** The index can disagree with the log if the daemon dies between the log write and the index append, or if offsets are taken from a size that tmux's `cat` has not yet flushed. Mitigation: offsets are advisory-checked on read (clamped to file size, sorted, open-ended last turn); an unclosed final turn is closed at the log size on next boot; the index is append-only JSONL so a torn last line is dropped.

### Recommendation

Recommendation (committed): option (b) —

**Recommendation: option (b) — keep one continuous `output.log` (and the `.enc` + FIFO pipeline) unchanged, and add a sidecar `turns.jsonl` index.** The deciding factor is the encrypted FIFO: option (a) forces changes to the capture pipeline for `--secure` sessions and multiplies files, while option (b) adds an independent, append-only metadata file and leaves capture exactly as it is. Option (b)'s one weakness — index/log drift — is bounded and recoverable (truncate-to-valid on read), whereas option (a)'s weakness is structural. No hybrid is needed; if per-turn files are ever wanted for export, they can be *derived* from log + index without changing capture.

Index shape (JSONL, one object per turn, append-only with a closing update record): `turn_number`, `byte_offset_start`, `byte_offset_end` (absent while open), `started_at`, `ended_at`, `trigger` (`operator_input` | `task_delivery` | `launch`), `ended_hint` (`complete` | `needs_input` | `next_input` | `state_terminal` | `daemon_restart`), `encrypted` (bool).

## Rendering

How the PWA consumes chunked history — read path only; no change to live streaming.

- **REST list:** `GET /api/sessions/<id>/turns` returns `[{turn_number, started_at, ended_at, byte_length, ended_hint}]`. It follows the `?view=summary` / pagination convention already used on `GET /api/sessions` (`view=summary`, `since`, `states`; see `session_list_view_test.go` and `mcp/list_sessions_view.go`): metadata-only by default, `limit`/`before` cursor for long sessions.
- **REST fetch one turn:** `GET /api/sessions/<id>/turns/<n>` returns that turn's content (ANSI-stripped by default, `raw=1` for the raw bytes), sliced from the log by the index offsets; for `--secure` sessions the slice is decrypted server-side with the existing key.
- **Relationship to existing endpoints:** `GET /api/output?id=&n=` (`handleSessionOutput`, `api.go:1767`) keeps its tail semantics and is unchanged; the turn endpoints are an additional read path, not a replacement.
- **Compatibility constraints with the live WS path:** `output`, `raw_output` and `pane_capture` frames, `hello` negotiation (`output: all|subscribed`) and `server.ws_output_scope_default` (`ws_output_scope.go`) are untouched. History is fetched over REST on demand when the operator scrolls above the live view; it is never pushed over WS, so it cannot compete with the scoped streaming path or re-introduce the all-sessions output flood that scoping removed. The live pane remains the source of truth for the current turn.

## Survives what

| Event | Survives? | Notes |
|---|---|---|
| **tmux-server crash** | **Yes for bytes already written; no for bytes after the crash.** | Plain case: `output.log` is a real on-disk file written by a `cat >>` child; the file persists after the tmux server dies, but pipe-pane stops, so output after the crash is not captured. `--secure` case: `output.log.enc` is a real on-disk file written by the daemon's own `EncryptingFIFO` reader; it also persists. The FIFO file `.pipe` is a named pipe with no content and is not history. The index (`turns.jsonl`) also persists. A crashed pane cannot be re-attached; a restart creates a new pane. |
| **Pane reuse / resumption / respawn** | **Yes**, once boundaries and the log are decoupled from the pane. | The log path is per-session, not per-pane; turn offsets are in the log, not the pane. Resume appends and opens a new turn. The one gap is the `--secure` truncation flag (`logwriter.go:64-70`): an empty/short `.enc` is reopened with `O_TRUNC`; the implementation must either never reopen a non-empty `.enc` in truncate mode or verify the header first. |
| **Daemon restart** | **Mostly.** | `output.log`'s path is stable for a resumed session: `ResumeTracker` rebuilds `sessionDir = <dataDir>/sessions/<FullID>` (`tracker.go:64-69`) and `sess.LogFile` is persisted in the store, so the same file is appended to. Plain sessions are re-piped with `RepipeOutput` (`manager.go:~4770`). **`--secure` sessions are not re-piped after restart** (documented BL263 follow-up) — output after the restart is not captured until the session is restarted. Output emitted while the daemon was down is lost in both modes if the pipe died with the daemon. The open final turn is closed with `ended_hint=daemon_restart`. |
| **Disk loss / log deletion / tracker cleanup** | No | Out of scope; the design adds no redundancy. |
| **Operator deleting the session** | No | Delete removes the session directory, including log and index. |

## Scope boundary

This design does not replace tmux and does not remove tmux: tmux remains the process and PTY manager: session creation, `send-keys` input, `capture-pane` for live rendering and completion detection, and `pipe-pane` as the capture feed. This design only changes how output *history* is captured into turns, stored (a sidecar index next to the unchanged log) and consumed (a REST read path for scrollback and rendering). The live WS stream stays as it is.

## Parity surface

Per the Mobile-Parity Rule (AGENT.md), each surface is included or excluded with a reason.

| Surface | Decision | Reason |
|---|---|---|
| REST | **Include** | Primary read path: turn list and turn fetch. |
| MCP | **Include** | `session_turns` / `session_turn` tools mirroring REST, built on the existing `session_output` tool; required for parity. |
| CLI | **Include** | `datawatch session turns <id>` / `turn <id> <n>` over REST. |
| Comm channel | **Include (minimal)** | A `turns <id>` / `turn <id> <n>` command returning the last turn's text; per-turn output is exactly what chat channels want instead of raw tail. Paginated by the channel's message limit. |
| YAML/config | **Include** | A `session.turn_history` block: `enabled`, `index_encrypt` (follow `--secure`), `max_turn_bytes_returned`. Default enabled, because the index is cheap. |
| PWA | **Include** | Turn list and per-turn view reached by scrolling above the live view; placement (a card, a tab or a sub-tab) is a design question for the operator and is **not decided here**. |
| Android | **Exclude from this repo, track** | Rendering code is not planned here; a `datawatch-app` tracking issue is carried (see below), same pattern as the WS output-scoping work. |
| iPhone/iOS | **Exclude from this repo, track** | Same reason, same `datawatch-app` issue. |

**Mobile tracking:** a `datawatch-app` Mobile-Parity issue is to be filed at the first merge that adds the REST endpoints (not by this design-only pass). It must carry the endpoint shapes, the `?view=summary`-style pagination parameters, the `ended_hint` vocabulary, and the statement that mobile clients keep using the existing WS output scoping for live output and fetch history only over REST. Pointer: `datawatch-app` issue — *to be filed with the implementation PRD; number pending*.

## Follow-up implementation

Sketch only; nothing here is implemented. Phases (to be decomposed into an Automaton later):

1. **Boundary recorder + index writer (first milestone).** In the session manager, at the existing signal sites (`MarkInputSent`, `DATAWATCH_COMPLETE:` / `NEEDS_INPUT` handling, state-engine terminal transitions, subprocess exit), append open/close records to `turns.jsonl` with offsets read from the log size. Unit tests with a fake log; no REST yet. Milestone passes when a scripted session produces a correct index for each mode.
2. **Read path.** A slicing reader that handles plain and `.enc` logs (reuse `NewEncryptedLogReader`), clamp-to-valid recovery, and the two REST endpoints with summary/pagination behaviour.
3. **Surfaces.** MCP tools, CLI commands, comm-channel command, config block, locale strings, docs and howto, the `datawatch-app` parity issue.
4. **PWA.** Turn list and per-turn view with the operator-chosen placement; no changes to the WS handlers.
5. **Hardening.** Fix the `--secure` `O_TRUNC` reopen hazard; decide the BL263 secure re-pipe follow-up; boot-time closing of an open final turn; e2e on the sandbox daemon (never production).

Verification for the PRD: the loopback e2e must create sessions inline and cover plain and `--secure`, each mode's boundary signal, a tmux-server kill on a *scoped, throwaway* socket, and a daemon restart.

## Open questions

1. Should the index be encrypted for `--secure` sessions (offsets leak turn count and sizes) or is plaintext metadata acceptable? The recommendation assumes encrypted-when-secure, but this is the operator's call.
2. For interactive TUI sessions where the operator types several inputs before the backend replies, is each input its own turn, or are inputs coalesced until the next response completes?
3. Should the daemon-generated task echo, re-delivered inputs and `send_saved_command` count as turn-opening events, or only operator-originated input?
4. Should `--secure` sessions be re-piped after a daemon restart as part of this work, or does it stay the separate BL263 follow-up?
5. Is ANSI-stripped output the right default for the turn-fetch endpoint, given the raw log is dominated by TUI redraw noise? How much noise reduction (spinner collapse, `\r` handling) belongs here versus in a later pass?
6. What retention applies to the log and index for very long sessions (size cap, compaction, or none)?
7. Where in the PWA should turn history live (card, tab, sub-tab)? This needs the operator's decision before any UI work.
8. Should a derived per-turn export (files cut from log plus index) be offered for users who want option (a)'s files, or is that out of scope?

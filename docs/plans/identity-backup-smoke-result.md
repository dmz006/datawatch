# Identity backup smoke result

Date: 2026-10-30. Binary: built from this worktree with
`env -u TMUX -u TMUX_TMPDIR go build -o datawatch ./cmd/datawatch/` (daemon v9.46.0).
Run: `SMOKE_ONLY=13,91 env -u TMUX -u TMUX_TMPDIR bash scripts/release-smoke.sh`, with `DW_BASE` unset so the
smoke started and ended its own scratch daemon with its own data directory. Section 13 is the identity
round-trip section, section 91 is the identity backups section.

## Outcome: FAILED (7 of 10 checks failed, all in section 91)

Summary line from the run:

```
  Pass:  3
  Fail:  7
  Skip:  0
```

(The Summary header is `== Summary ==`; the script prints no separate section count, so the sections that ran
are 13 and 91, two. Every other section was muted by `SMOKE_ONLY`.)

## Per-check table

| Check | Result | Reason |
|---|---|---|
| 13: identity GET reachable | passed | |
| 13: identity PATCH round-trip: role merged | passed | |
| 13: identity PATCH round-trip: saved identity put back | passed | |
| 91: two writes (A then B) accepted | FAILED | PUT A and PUT B answered HTTP 000000 (expected 200 and 200) |
| 91: GET /api/identity/backups lists a backup whose role is smoke-role-a | FAILED | no backup with role smoke-role-a: empty response |
| 91: an all-empty write is refused (409 identity_wipe_refused) and the identity is kept | FAILED | empty write answered HTTP 000, code='', role now '?' (expected 409, identity_wipe_refused, smoke-role-b) |
| 91: restore latest brings back smoke-role-a | FAILED | restore latest answered HTTP 000000, role now '?' (expected 200, smoke-role-a) |
| 91: POST /api/identity/reset leaves an empty identity | FAILED | reset answered HTTP 000000 or the identity still has content |
| 91: the backups list is still non-empty after the reset | FAILED | backups list empty after the reset: empty response |
| 91: restore latest after the reset | FAILED | restore latest after the reset answered HTTP 000000, role '?' |

No check was skipped.

Failing output, verbatim from the run:

```
== 91. Identity backups: backup on write, empty-write refusal, restore ==
  FAIL  identity backups: PUT A answered HTTP 000000, PUT B answered HTTP 000000 (expected 200 and 200)
  FAIL  identity backups: no backup with role smoke-role-a:
  FAIL  identity backups: empty write answered HTTP 000 code='', role now '?' (expected 409, identity_wipe_refused, smoke-role-b)
  FAIL  identity backups: restore latest answered HTTP 000000, role now '?' (expected 200, smoke-role-a)
  FAIL  identity backups: reset answered HTTP 000000 or the identity still has content
  FAIL  identity backups: backups list empty after the reset:
  FAIL  identity backups: restore latest after the reset answered HTTP 000000, role '?'
```

## What the failures mean

The seven failures are not an answer from the identity routes. `HTTP 000000` is `curl`'s `000` printed twice
(its `-w` output plus the script's `|| echo 000` fallback after a non-zero exit), and curl exited 7,
`Failed to connect to 127.0.0.1 port <tls port> after 0 ms`: connection refused. The scratch daemon's
access log holds no `PUT /api/identity`, `GET /api/identity/backups` or `POST /api/identity/restore` from
section 91. The identity backups feature was therefore not exercised, and this run proves nothing about it
either way.

Why the daemon was unreachable, from repeated runs (each run showed the same thing):

- The daemon is healthy from start (HTTP 200 on `/api/health`) until about 150 s after it began listening, then its
  listener closes while the process is still alive. Measured on one run: listening at 21:56:26, listener gone at
  21:58:57.9.
- Its own audit log records `shutdown`, signal `terminated`, source `procscan`, sender unknown, with `release-smoke.sh`
  as its parent, about 0.4 s earlier than that. No panic or crash is in the daemon log.
- Release-smoke runs every section's setup side effects even under `SMOKE_ONLY`, so the run spends about 150 s in
  earlier sections (the ones muted by `SMOKE_ONLY`) before it reaches section 91. In a trace the daemon was already gone
  during section 85 (usage calculator and capacity), and every request from there on, including the `cleanup_all`
  deletes, was refused.
- Section 91 passes or fails on whether the daemon is still up at that point; with the 150 s death it never is.
  Section 13 comes early in the script, well before the 150 s mark, which is the likely reason it passed.

I did not find what sends the SIGTERM. The script has no `kill` of the daemon and `scripts/test-daemon-lib.sh` has
no 150 s limit. It may come from outside the script (a reaper of test daemons) or from something in sections 80 to 85.
A follow-up should find the sender (for example by tracing `kill` and `tgkill` syscalls during a run) before
section 91 is judged.

## Pre-gate note

The first attempt of `SMOKE_ONLY=13,91 ... bash scripts/release-smoke.sh` aborted before starting a daemon: the
release pre-gate `scripts/tidy-plans.sh --check` failed ("69 file(s) need to move out of docs/plans/"). Fixing
that means moving plan files, which this task forbids. All runs after it set the unrelated static lint pre-gates off
with the documented `DW_SKIP_TIDY_CHECK=1 DW_SKIP_DOCS_CHECK=1 DW_SKIP_INTERNAL_REFS_CHECK=1
DW_SKIP_VERSION_BUMP_CHECK=1 DW_SKIP_DOCKERFILE_GO_VERSION_CHECK=1 DW_SKIP_DOCS_AS_MCP_CHECK=1` (the same set the
comment above section 85 in the script records for a targeted run). The `docs/plans/` backlog is a separate release
blocker.

## Leftover check

`scripts/check-no-leftover-test-daemons.sh` takes `snapshot <file>` / `check <file>`, not no arguments. No pre-run
snapshot existed, so after the last run I took a snapshot and checked it (`OK: no leftover test daemons`), and
`pgrep` for this run's `datawatch-smoke-*/test-config.yaml` daemons found none. The snapshot recorded 12 live test
daemons that belong to other sessions; none of them is from these runs. Nothing was killed.

Two scratch data directories were kept by `KEEP_TEST_DIR=1` during diagnosis and sit next to this worktree:
`/home/dmz/.datawatch/prd-worktrees/datawatch-smoke-f72fa3` and `/home/dmz/.datawatch/prd-worktrees/datawatch-smoke-dcf346`
(they hold the daemon logs). They are outside the project directory, so I left them for the operator to remove.

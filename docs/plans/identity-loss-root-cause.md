# Identity loss: root cause

Written 2026-10-30. This note records what emptied the operator identity on 2026-10-24
and the evidence for it. It changes no source code.

## Verdict

Verdict: likely

The identity was emptied by a release smoke run pointed at the operator's live daemon.
Section 13 of `scripts/release-smoke.sh` sends `PATCH {"role":"smoke-test-role"}` to
`$BASE/api/identity` and then, as "cleanup", `PUT {}` to the same URL. In its default mode
`$BASE` is a scratch daemon, but when `DW_BASE` is set (external-daemon mode) `$BASE` is
whatever daemon `DW_BASE` names. The live daemon's audit log holds exactly that pair of
writes, `identity_update` then `identity_set`, about 69 ms apart at 08:26:27 EDT on
2026-10-24, sitting between the smoke script's section 14 and section 17 requests, and the
`updated_at` the daemon reports today is the instant of that `identity_set`. The `PUT {}` is
handled by `Manager.Set` in `internal/identity/identity.go`, which writes an empty identity
without complaint, and an empty identity serializes to a single `updated_at` line, which is
the symptom. The verdict is "likely" rather than "confirmed" because the audit entry does not
record the request body or the client, so the link from the audit pair to a smoke run rests on
timing, ordering and resource names, and the process that set `DW_BASE` is not recorded
anywhere I can read. What the content was before 08:26:27 is also unknown (see the unknowns
at the end of Evidence).

## Evidence

All reads of the live daemon used its own interfaces (MCP tools `get_identity`, `audit_query`,
`daemon_logs`, `get_version`). Nothing outside the project directory was opened. Times are
EDT (UTC-04:00) unless marked UTC.

**Current identity** (`get_identity`, 2026-10-30 19:33 local): the whole document is
`{"updated_at":"2026-10-24T12:26:27.399149598Z"}`, which is 08:26:27.399 EDT. No role, goals,
projects, values, focus or notes.

**Identity audit entries** (`audit_query` with `action=identity_*`, `limit=200`): exactly two
entries exist in everything the audit log still holds.

| Time (EDT) | Action | Actor |
|---|---|---|
| 2026-10-24 08:26:27.330528596 | `identity_update` (PATCH or POST) | operator |
| 2026-10-24 08:26:27.399286073 | `identity_set` (PUT) | operator |

The two are 68.8 ms apart. The `updated_at` on disk (12:26:27.399149598 UTC) precedes the
`identity_set` audit entry by 136 microseconds, which is the order in the handler: `Set` stamps
`UpdatedAt` and writes, then the handler writes the audit entry. So the last write to the
identity file is that `identity_set`, and what it wrote has no identity fields. A PUT whose
body carried any field would have kept it.

**Neighbouring audit entries** (`audit_query` for 08:15 to 08:33, saved to a file and sliced
with a script; 158 entries; I read every entry from 08:25:54 to 08:26:55 and the action counts for the rest).
The smoke-related entries, in order:

| Time (EDT) | Action | Resource |
|---|---|---|
| 08:26:26.250 | `skills_registry_add_default` | `community,pai` |
| 08:26:27.031 | `algorithm_start` | `smoke-algo-1792844787` |
| 08:26:27.112 | `algorithm_advance` | `smoke-algo-1792844787` |
| 08:26:27.190 | `algorithm_reset` | `smoke-algo-1792844787` |
| 08:26:27.330 | `identity_update` | self |
| 08:26:27.399 | `identity_set` | self |
| 08:26:28.475 | `start` (session) | task text begins `smoke-state-engine` |

In the script, section 12 does the skills registry add-default, section 14 drives an algorithm
session through start, advance and reset, section 13 does the identity PATCH and PUT, and
section 17 starts a fresh idle session for the state engine check. The sections sit in the
file in the order 12, 14, 13, 17, and the audit order is exactly that file order. The number in the algorithm session name,
1792844787, is a Unix time; decoded it is 2026-10-24 12:26:27 UTC, 08:26:27 EDT, the same
second as the identity pair.

**It was the live daemon.** The same audit log carries other smoke-named resources
(`smoke-cap-backoff-3867257-1792844971`, `smoke-llm-keyredact-3867257`,
`smoke-fedpeer-3867257`, `smoke-llm-forcedel`) created and deleted between 08:25 and 08:33,
and session ids prefixed with the live daemon's host name. In the default mode the smoke run
starts its own daemon with host name `dw-smoke-test`, its own data directory and its own audit
log, so none of this could land in the live audit log. That leaves external-daemon mode
(`DW_BASE`) pointed at the live daemon.

**Nothing else wrote identity.** The audit log reaches back to at least 2026-10-07 (an entry at
2026-10-07 15:28:57 exists; a query for anything before 2026-10-01 returns nothing), and
across that whole span the two entries above are the only identity writes. Every REST write,
and every MCP, chat, CLI `set`, CLI `configure` and PWA write, goes through the REST handlers
and is audited, so none of those wrote identity in that span except at 08:26:27. There is also
no identity entry after 08:26:27, so nothing has modified the identity since. An `audit_query`
with `actor=mcp` for 08:26:20 to 08:26:35 returned no entries.

**Script history** (`git log -S 'smoke-test-role' -- scripts/release-smoke.sh`): the PATCH and
the `PUT {}` cleanup were added together in the commit that introduced the identity feature,
2026-05-05. The script has carried the wipe for almost six months; it only bites when
`DW_BASE` names a daemon with a real identity. The only commit touching the script in the
08:00 to 09:00 window on 2026-10-24, across all branches, is `fc51898ca` at 08:38:24, twelve
minutes after the wipe ("Wire the mobile-fit suite into the PWA runner and release smoke",
recorded as "Verification: not run before this commit"). The 08:23 to 08:24 commits mentioned
in the task brief do not touch `scripts/release-smoke.sh` in this repository (`git log --all`
over that window shows only `fc51898ca` for the script). So a worker was editing the smoke
script at the time and a live-daemon run during that work is plausible, but the commit does
not prove it.

**Daemon log: evidence gap.** `daemon_logs` returned a short excerpt of a reported 31,881 lines even with
`n=40000`, none timestamped, all start-up lines (they include "datawatch v9.0.5 started"
while `get_version` reports 9.44.0 running). The tool gives no way to page to 07:30 to 09:30
on 2026-10-24, so the window is not covered and I found no identity line in it. Nothing from
the daemon log is used as evidence in this note.

**Still unknown.**

- Who or what set `DW_BASE` for the 08:26 run, and from which process. The audit records the
  actor as `operator` for every REST write, with no client address and no request body.
- The content of the identity before 08:26:27. No identity write appears earlier in the
  retained audit log, so if it held content, that was written before 2026-10-07 or through a
  path that is not audited (`datawatch identity edit`, or a hand edit of the file). No backup
  exists, so the content cannot be recovered from this side.
- Whether the PATCH at 08:26:27.330 saw a populated identity. It would have merged
  `smoke-test-role` into whatever the daemon's in-memory copy held, and the PUT then replaced
  that with nothing.

## Candidate paths checked

| Write path | Outcome | Reason |
|---|---|---|
| PWA Save (`saveIdentity`, `internal/server/web/app.js`) | ruled out | PUTs the whole form, so a blank form would wipe, but it sends one PUT. The audit shows a PATCH-class write 69 ms before the PUT, which the PWA never sends, and a human click cannot produce two requests 69 ms apart sandwiched between smoke script steps. |
| PWA wizard (`identityWizardNext`) | ruled out | Same: one PUT of the whole wizard state on the last step, no preceding PATCH, six interactive steps. |
| CLI `identity configure` | ruled out | PUTs a merged document after six prompts on stdin. It cannot run in 69 ms and sends no PATCH. It could wipe if the pre-fill GET failed and every answer was blank, but that is a minutes-long interactive run. |
| CLI `identity set` | ruled out | PATCH only, and refuses an empty `--value`. Merge semantics cannot clear anything, and it would audit `identity_update`, not `identity_set`. |
| CLI `identity edit` | ruled out | Opens a file in `$EDITOR`; nothing is sent to the daemon and nothing is audited. The last write to the file is the `identity_set` at 08:26:27.399, so no editor save came after it. Observation on path resolution below. |
| Chat `identity set` (`internal/router/identity.go`) | ruled out | Sends PATCH for one field; cannot clear and audits `identity_update`. |
| MCP `set_identity` | cannot tell | It PUTs whatever fields were given, so a call with none clears everything and would produce exactly an `identity_set`. The audit entry is the same for any PUT. Ruled unlikely rather than out: no MCP-actor audit entries exist in the window, and a call would not also produce a PATCH 69 ms earlier. |
| MCP `update_identity` | ruled out | PATCH, merge only. |
| REST PUT, PATCH, POST | ruled in (the mechanism) | The incident is a PATCH then a PUT at the REST layer. `PUT {}` through `Server.handleIdentity` into `Manager.Set` produces precisely the file found. POST is an alias for PATCH and cannot clear. |
| `scripts/release-smoke.sh` section 13 | ruled in (likely) | Issues the PATCH then `PUT {}` against `$BASE/api/identity`. Order, timing, the epoch in the section 14 session name and the smoke-named resources in the live audit log all match, and external-daemon mode is the only way that reaches the live daemon. |
| E2E stories (TS-153, TS-211, TS-246, TS-333, TS-334) | ruled out | None sends a PUT. TS-153 sends PATCH with a `name` field the identity type does not have, so it merges nothing and rewrites the current document with a new `updated_at`; it would audit `identity_update`, never `identity_set`. TS-211 and TS-246 only GET. TS-333 runs `identity show`, TS-334 runs `identity configure --help`. |
| Go tests | ruled out | The only tests that build an identity manager or post to the identity route are in `internal/identity` and `internal/server`, and every one uses `t.TempDir()`. The `cmd/datawatch` tests never touch identity. |

## Test and script isolation audit

Can any test or script write to the real data directory instead of a scratch one? Yes: one
can do it by being pointed there on purpose, and nothing in it stops that.

1. **`scripts/release-smoke.sh`, external-daemon mode (`DW_BASE`, near line 160).** Fails to
   protect. `BASE` is set to `$DW_BASE` with no check that the target is a scratch daemon, and
   nothing refuses a daemon that holds real data. The header says everything but plugin
   sections "runs identically against any already-running daemon", and
   `docs/howto/testing-against-a-running-daemon.md` describes the mode without warning that
   section 13 replaces the identity. Section 13's cleanup `PUT {}` is the wipe. This is the
   path that reached the real data directory on 2026-10-24. The same run also made `configure`
   changes on the live daemon (the audit shows `session.skip_permissions`,
   `session.permission_mode`, `session.claude_auto_accept_disclaimer` and
   `autonomous.per_story_approval` being toggled between 08:25:57 and 08:26:14). I did not
   investigate whether those were restored; that is a separate question.
2. **`scripts/release-smoke.sh`, default mode.** Protected. The script writes a config whose
   `data_dir` is a fresh directory under the repo's parent
   (`datawatch-smoke-<run id>/.datawatch-test-<pid>`), starts its own daemon from it, and the
   CLI checks pass `--config` for that file so they do not read the production config.
3. **`scripts/run-tests.sh`, default mode.** Protected. It renders `testdata/datawatch.yaml`
   with `data_dir` rewritten from `/data` to a per-run directory under the test work dir and
   picks free ports.
4. **`scripts/run-tests.sh`, `DW_BASE` or `--no-daemon`.** `TEST_BASE` becomes the named
   daemon, so the stories run against it. Not protected, but the only identity traffic in the
   stories is GET plus the no-op PATCH in TS-153, which preserves content (it merges nothing
   and rewrites the daemon's in-memory copy). If that copy were stale relative to the file it
   would overwrite the file with it; that is an effect of how `Update` merges against the
   cache, not a wipe.
5. **`scripts/test-stories/lib.sh`.** Standalone runs default to ports 18080 and 18443, not the
   live 8080 and 8443, and `TEST_DATA` is a scratch directory. The `cli_test` helper passes
   `--config "$TEST_DATA/config.yaml"` and a URL built from `TEST_PORT`; its `:-8080`
   fallback is unreachable because `TEST_PORT` is always set earlier in the file.
6. **A config without `data_dir`.** `applyDefaults` in `internal/config/config.go` sets
   `DataDir` to `~/.datawatch` when it is empty, and `cmd/datawatch/main.go` builds the
   identity manager at `expandHome(cfg.DataDir)/identity.yaml`. Any scratch daemon started
   from a config that omits `data_dir` would use the real directory. `testdata/datawatch.yaml`
   sets it and the two scripts above rewrite it, so nothing found does this today; the
   protection is the template, not a check.
7. **Go tests.** Protected. `internal/identity` and `internal/server` tests build managers on
   `t.TempDir()`. `cmd/datawatch` tests that run the CLI set `HOME` to a temp directory with
   `t.Setenv`, and none starts a daemon with the default data directory. No Go test posts to
   `/api/identity` on a real address.
8. **`tests/ci`, `tests/integration`, `tests/live-smoke`, PWA runner scripts.** None of them
   references the identity route or file.

Observation, not fixed here: `datawatch identity edit` opens
`~/.datawatch/identity.yaml` (home directory plus `.datawatch`), while the daemon reads and
writes `<data_dir>/identity.yaml`. They agree only when `data_dir` is the default. Also, the
daemon does not reload after an edit, and `Manager.Update` merges against its in-memory copy
rather than the file, so a hand edit can be silently overwritten by the next PATCH. Neither
played a part in this incident.

## What the guards now do

This change set delivers these protections. This note was written against the tree before
they landed, and each is checked by a verify script in `.datawatch/verify/`.

- **Timestamped backup before every write.** Before `Set` or `Update` replaces the file, the
  previous content is saved as a timestamped backup. The number kept is configurable
  (`backups_to_keep`).
- **Refusal of an all-empty overwrite.** A whole-document replace in which every field is
  empty is refused instead of written, so a stray `PUT {}` or a blank form cannot wipe a
  populated identity.
- **The explicit reset.** Clearing the identity is now a separate, deliberate action
  (`/api/identity/reset`, with CLI and MCP equivalents), so the only way to empty the
  identity is to ask for exactly that.
- **Restore on every surface.** Backups can be listed and restored through REST
  (`/api/identity/backups`, `/api/identity/restore`), the PWA (Restore and Revert buttons),
  the CLI (`identity backups`, `identity restore`), MCP (`identity_backups`,
  `restore_identity`) and chat.
- **The smoke fix.** Section 13 of `scripts/release-smoke.sh` no longer sends an empty body to
  the identity route.
- **The lint guard.** `scripts/check-no-identity-wipe.sh`, run from the Makefile, fails the
  build when any script sends an empty PUT to `/api/identity`.

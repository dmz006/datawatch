# Automation merge-pending — live check on a sandbox daemon

**Date:** 2026-10-30 (run 00:21–00:32, local time, UTC-4)
**Version under test:** 9.34.3 (binary built from this worktree)
**Scope:** `GET /api/autonomous/merge`, `GET /api/autonomous/safe-to-delete`, the `datawatch autonomous merge` and `datawatch autonomous safe-to-delete` commands, and the merge-pending alert, in `shadow` and then `on` mode, against three completed Automata in a throwaway git repository.

## Setup

- **Sandbox daemon only.** Started with `td_start` from `scripts/test-daemon-lib.sh`, using a config generated from `testdata/datawatch.yaml` by the same `sed` substitutions the test runner uses (data folder, ports, loopback host, test token `dw-test-token-12345`). The only addition to the template was `autonomous.worktree_base_repo`, pointing at the throwaway repository (the check reports every branch as `unknown` without it). The production daemon and its service were never touched: no stop, restart or kill, and nothing was killed by name.
- **Ports.** The template's 18080/18443 were already held by another job's sandbox, so this run used HTTP 28080, TLS 28443, MCP 29090 and 29053/29080/29180 for the DNS, webhook and notification endpoints. All were free before and closed after.
- **Temporary folder.** Everything lived under the session scratch folder (`.../scratchpad/live-check/`: `repo/`, `data/`, `config.yaml`, `daemon.log`, `tdstate/`), not inside the worktree.
- **Every shell command** ran under `env -u TMUX -u TMUX_TMPDIR`; no tmux server command was run.
- **Baseline.** `scripts/check-no-leftover-test-daemons.sh snapshot` recorded 12 test daemons that belong to other jobs before this run started.
- **Original values read back from the sandbox:** `merge_pending_mode` = `off`, `merge_pending_alert_minutes` = `30`.

Throwaway repository (`main` checked out, branch heads at the time of the run):

| Automaton id | Name | Branch | How the branch was built | Expected |
|---|---|---|---|---|
| `aaaa0001` | Pending sample Automaton | `automaton/aaaa0001` | one commit ("Add pending work file") that is not on `main` | `pending` |
| `bbbb0002` | Empty sample Automaton | `automaton/bbbb0002` | created last, at the tip of `main` | `empty` |
| `cccc0003` | Needs-look sample Automaton | `automaton/cccc0003` | one commit "Add shared notes file" with one content; `main` then received a commit with the same subject and different content | `needs_look` |

`git for-each-ref` after setup: `automaton/aaaa0001 aeb029a`, `automaton/bbbb0002 9907643`, `automaton/cccc0003 9e42b0d`, `main 9907643`.

The three Automata were created as `completed` with their work branch recorded (see Mismatches, item 1, for why they were seeded into the sandbox's Automaton store before its first start rather than created through a route).

## Steps and observed results

### 1. Shadow mode, default threshold

`PUT /api/config {"autonomous.automation.merge_pending_mode":"shadow"}` → `{"status":"ok"}`. Immediately after, before the first tick, the merge read listed all three Automata as `"state":"unknown"` with no decisions, as expected. The first pass landed at 00:24:26 (one minute after the daemon started):

```
GET /api/autonomous/merge
{"mode":"shadow","alert_minutes":30,
 "automata":[
  {"automaton_id":"aaaa0001","branch":"automaton/aaaa0001","state":"pending","commits_ahead":1,"pending_since":"2026-10-30T00:24:26.020151509-04:00","alerted_at":null,"would_alert":false},
  {"automaton_id":"bbbb0002","branch":"automaton/bbbb0002","state":"empty","commits_ahead":0,"pending_since":null,"alerted_at":null,"would_alert":false},
  {"automaton_id":"cccc0003","branch":"automaton/cccc0003","state":"needs_look","commits_ahead":1,"pending_since":null,"alerted_at":null,"would_alert":false}],
 "shadow_decisions":[ ...one row per Automaton, repeat_count 1... ]}
```

States `pending`, `empty` and `needs_look` match the expectation for the three branches. **PASS.** With the default 30-minute threshold `would_alert` is `false`, which is correct this early.

### 2. Safe-to-delete report (shadow)

```
GET /api/autonomous/safe-to-delete
{"safe":[{"branch":"automaton/bbbb0002","worktree":"","reason":"no commits ahead of main","commits":0}],
 "held":[{"branch":"automaton/aaaa0001","worktree":"","state":"pending"},
         {"branch":"automaton/cccc0003","worktree":"","state":"needs_look"}],
 "held_dirty":0}
```

Only the empty branch is listed as safe; the pending and needs-look branches are held. Immediately after the read, `git for-each-ref` returned the same four heads as before (`aeb029a`, `9907643`, `9e42b0d`, `9907643`): the read deleted nothing. **PASS.**

### 3. Shadow would-alert and no alert

Baseline `GET /api/alerts` → `{"alerts":[],"unread_count":0}`. Then `merge_pending_alert_minutes` was set to `1` (still in shadow; see Mismatches, item 2). At 00:25:26 the merge read showed the pending Automaton with `"would_alert":true`; the next tick (00:26:26) wrote the matching decision row:

```
{"at":"2026-10-30T00:26:26.020247619-04:00","automaton_id":"aaaa0001","state":"pending","commits_ahead":1,"would_alert":true,"repeat_count":1}
```

The empty and needs-look Automata stayed `would_alert:false`. Throughout shadow mode `GET /api/alerts` stayed `{"alerts":[],"unread_count":0}`, and no Automaton carried a stored triage record. **PASS:** would-alert is true for the pending Automaton, and no alert and no record were produced.

### 4. CLI reads (shadow)

Run with `./bin/datawatch --config <sandbox config> autonomous ...` (exit 0 for both):

```
$ datawatch autonomous merge
Automaton merge pending: mode shadow (alert threshold: 1 min)
3 finished Automata with a work branch:
  Automaton "Pending sample Automaton" (id aaaa0001): branch automaton/aaaa0001 is pending, 1 commit(s) ahead of main, merge pending since 2026-10-30T00:24:26-04:00, would alert
  Automaton "Empty sample Automaton" (id bbbb0002): branch automaton/bbbb0002 is empty, 0 commit(s) ahead of main
  Automaton "Needs-look sample Automaton" (id cccc0003): branch automaton/cccc0003 is needs_look, 1 commit(s) ahead of main
Shadow decisions (nothing was sent), newest first:
  2026-10-30T00:26:26-04:00 Automaton "Pending sample Automaton" (id aaaa0001): ... pending, 1 commit(s) ahead, would alert
  ... (3 more rows, each "no alert", with "seen N times")
Nothing has been merged, deleted or pushed.

$ datawatch autonomous safe-to-delete
Automaton branches safe to delete: 1, held back: 2
Safe to delete:
  automaton/bbbb0002 (0 commit(s)): no commits ahead of main
Held back:
  automaton/aaaa0001: pending
  automaton/cccc0003: needs_look
This is a report only: nothing has been deleted.
```

The CLI text agrees with the API bodies. **PASS.**

### 5. Mode `on`, threshold 1 minute

`PUT /api/config {"autonomous.automation.merge_pending_mode":"on","autonomous.automation.merge_pending_alert_minutes":1}` at 00:26:49.

- 00:27:26 tick: the pending Automaton got a stored record, `pending_since` 00:27:26.019863855.
- 00:28:26 tick: no alert yet (see Mismatches, item 4).
- 00:29:26 tick: one alert raised.
- Waited past the 00:30:26 tick and read again at 00:30:50.

```
GET /api/alerts   (count 1, unread 1)
{"id":"da868878","level":"warn","title":"Automaton Pending sample Automaton merge pending",
 "body":"Automaton \"Pending sample Automaton\" (id aaaa0001) has finished, but its work branch automaton/aaaa0001 has 1 commit(s) that are not on main yet.\nMerge pending since 2026-10-30T00:27:26-04:00 (2m0s).\nReview the Automaton branch and merge it into main; nothing has been merged, deleted or pushed.",
 "source":"system","read":false,"created_at":"2026-10-30T00:29:26.020382871-04:00"}

GET /api/autonomous/merge
{"mode":"on","alert_minutes":1,
 "automata":[
  {"automaton_id":"aaaa0001","state":"pending","commits_ahead":1,"pending_since":"2026-10-30T00:27:26.019863855-04:00","alerted_at":"2026-10-30T00:29:26.020139997-04:00"},
  {"automaton_id":"bbbb0002","state":"empty","commits_ahead":0,"pending_since":null,"alerted_at":null},
  {"automaton_id":"cccc0003","state":"needs_look","commits_ahead":1,"pending_since":null,"alerted_at":null}],
 "shadow_decisions":[]}
```

Exactly one merge-pending alert exists, for the pending Automaton, and it did not repeat on the tick after it fired. The detail read of `aaaa0001` shows a stored record with `"status":"merge_pending"`, `"branch":"automaton/aaaa0001"`, `"commits_ahead":1` and `alerted_at` set; the detail reads of `bbbb0002` and `cccc0003` show `"triage": null`. `shadow_decisions` is empty outside shadow mode. **PASS.**

### 6. CLI reads (on) and safe-to-delete again

```
$ datawatch autonomous merge
Automaton merge pending: mode on (alert threshold: 1 min)
3 finished Automata with a work branch:
  Automaton "Pending sample Automaton" (id aaaa0001): branch automaton/aaaa0001 is pending, 1 commit(s) ahead of main, merge pending since 2026-10-30T00:27:26-04:00, alerted at 2026-10-30T00:29:26-04:00
  Automaton "Empty sample Automaton" (id bbbb0002): ... is empty, 0 commit(s) ahead of main
  Automaton "Needs-look sample Automaton" (id cccc0003): ... is needs_look, 1 commit(s) ahead of main
Nothing has been merged, deleted or pushed.
```

`datawatch autonomous safe-to-delete` and `GET /api/autonomous/safe-to-delete` returned the same report as in step 2 (one safe branch, `automaton/bbbb0002`; two held). Branch heads were again unchanged. **PASS.**

## Mismatches

None of these changed a verdict. They are differences between what the brief assumed and what was observed.

1. **No route creates a completed Automaton with a work branch.** The create route makes a draft, and no route sets a branch or moves an Automaton to `completed` (the git route only takes `auto_pr` and `base_branch`). The three Automata were therefore written into the sandbox's own Automaton store (the legacy `prds.jsonl` import path) before the first start, with `status: completed` and `git.branch` set. They were listed, read and deleted through the API afterwards. Running three real Automata to completion would have needed model sessions, which this check deliberately avoids.
2. **Order of the threshold change.** The brief expects a shadow would-alert of `true` before switching to `on` with a 1-minute threshold. At the default 30 minutes `would_alert` is correctly `false` for a branch seen minutes ago, so the threshold was set to `1` while still in shadow, which let the would-alert flip within the run. The default-threshold reading is step 1.
3. **Read-time versus recorded would-alert.** At 00:25:26 the Automaton entry already said `would_alert:true`, but the newest decision row for the same Automaton said `false` (repeat count 2) until the next tick at 00:26:26. The 00:25:26 tick ran about 147 microseconds before the one-minute mark. The entry is computed at read time and the row at tick time, so the two can disagree for one tick at the boundary. Not a defect of the check, but worth knowing when comparing the two.
4. **Alert after three ticks, not two.** In `on` mode the record was created at 00:27:26 and the alert fired at 00:29:26 (shown as "2m0s" waiting), not at 00:28:26. Same cause as item 3: each tick lands a fraction of a millisecond before the whole minute, so a 1-minute threshold is met one tick later. With the real 30-minute default this is invisible.
5. **List view leaves out the stored record.** `GET /api/autonomous/prds` showed no triage field for any Automaton, while `GET /api/autonomous/prds/{id}` (and the merge read) showed the `merge_pending` record. This is the list view's summary shape, not a missing record.
6. **A plain delete only cancels.** `DELETE /api/autonomous/prds/{id}` set the three Automata to `cancelled` and left them listed. `?hard=true` (documented) removed them. This run used the plain form first and then the hard form.
7. **Ports and location.** Ports and temporary folder differ from the brief's defaults for the reasons in Setup. No daemon log line mentioned an error or a warning (39 lines, 0 matches).

## Cleanup

- **Automata deleted through the API** with `DELETE /api/autonomous/prds/{id}?hard=true` for `aaaa0001`, `bbbb0002` and `cccc0003` (each returned `"status":"deleted"`); the following list returned `{"prds":[],"total":0}`.
- **Config restored** through the API: `merge_pending_mode` back to `off`, `merge_pending_alert_minutes` back to `30`. The read-back was `{"agent_insert_policy":"allow","idle_alert_minutes":0,"merge_pending_alert_minutes":30,"merge_pending_mode":"off","triage_alert_minutes":30}`, and the merge read returned `{"mode":"off","alert_minutes":30,"automata":[],"shadow_decisions":[]}`.
- **Temporary repository deleted** (`repo/` removed from the temporary folder), then the sandbox's remaining temporary folder (data, config, log, daemon records) was removed.
- **Sandbox daemon ended** with `td_stop_all` (the one recorded process; return code 0). Its pid no longer exists and ports 28080, 28443, 29090, 29053 and 29080 are closed.
- **Leftover check** against the baseline taken before the run: `scripts/check-no-leftover-test-daemons.sh check` printed `OK: no leftover test daemons` (exit 0).
- The production daemon and its service were not touched at any point. The 12 test daemons from other jobs recorded in the baseline were not touched either.

Result: PASS

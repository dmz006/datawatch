---
docs:
  index: true
  topics: [autonomous, landing, merge, archive, cancel, digest, audit, housekeeping]
exec_steps:
  - tool: autonomous_finished_work
    description: Show the finished work that is waiting to land, with the owner and open question of each
    args: {}
    read_only: true
---

# Finished work never sits unmerged

An Automaton can finish its work on a branch and then sit: the record is
archived or cancelled, or a docs task stalls the whole Automaton, or someone
answers "leave the branch alone", and the finished code never reaches the
default branch and nobody owns it. The daemon now guards against all of these on
its own. None of it depends on a model.

The setting is `autonomous.finished_work`: `on` (the default), `shadow` or
`off`. It applies live.

## 1. Archive and cancel refuse unmerged work

With the setting `on`, an Automaton whose branch holds commits that are not on
the default branch cannot be archived or cancelled. The call is refused with the
branch and the commit count. A branch that cannot be compared (git failed)
is refused too; the daemon never guesses.

To go on, record an explicit decision with the call:

- `keep` — the branch is kept unmerged on purpose. The decision is dated and the
  daemon asks again in 14 days if the work is still not on the default branch.
- `drop` — the work will not be merged. The branch itself is not deleted.

A decision needs a reason. A record whose shipped record names its work, and one
whose landing is already landed, kept or dropped, is not refused. Without a base
repository nothing can be checked and the call goes through as before.

A keep or drop decision covers the commit the branch pointed at when it was
made. Once the branch moves past that commit, archive and cancel are refused
again until you record a new decision. Work that the default branch already
carries, a port made by hand with the same file content included, is not
unmerged work: the guard, the sweep and the audit let it be.

### With the closed outcome rule

The archive also has to say how the work ended
([closed-work-outcome.md](closed-work-outcome.md)). The two rules are separate
and both apply: the landing decision says what happens to the branch, the
outcome says how the Automaton ended. Neither is guessed from the other, so a
`drop` decision does not record the outcome `dropped` for you. One request can
carry both: add `outcome`, `delivered_via` and `note` beside `decision` and
`reason` (CLI `--outcome`, `--via`, `--note`; chat
`autonomous cancel <id> outcome=dropped drop <reason>`, where the reason takes
the rest of the line and replaces a note). An unknown outcome or twin refuses
the whole request before anything is recorded. When the outcome rule refuses an
archive for another reason (no outcome, or a hold that is not met), a landing
decision sent with the request stays recorded.

Recording an outcome with the archive step (`prd-set-outcome --archive`,
`autonomous_prd_set_outcome` with `archive`, chat
`autonomous outcome <id> ... archive=true`) runs the same guard. For unmerged
work, record the landing keep or drop first, then run it.

| Surface | Cancel | Archive |
|---|---|---|
| REST | `DELETE /api/autonomous/prds/{id}?decision=keep&reason=...` or `POST .../cancel` with `{"decision","reason"}` | `POST /api/autonomous/prds/{id}/archive` with `{"decision","reason"}` |
| MCP | `autonomous_prd_cancel` with `decision` and `reason` | the REST call (there is no MCP archive status tool) |
| CLI | `datawatch autonomous prd-cancel <id> --decision keep --reason "..."` | the REST call |
| Comm channel | `autonomous cancel <id> keep <reason>` | the REST call |
| PWA | Cancel and Archive on an Automaton: when refused, a prompt asks for keep or drop and a reason, then repeats the call | the same prompt |

The supervisor loop's own close step keeps its branch check and the
`autonomous.archive_keep_unmerged` setting, so it never archives an Automaton
with unmerged work while that setting is on (the default). Turning that setting
off is the operator's explicit decision to let finished records archive.

## 2. The sweep: an owner and one question after a day

A bounded sweep runs on the idle tick, at most every ten minutes. It looks at
Automata that finished (or whose code finished) while their branch holds commits
not on the default branch, and that nobody has decided about. It compares at
most 100 branches per pass and starts the next pass after the last Automaton it
reached, so every Automaton is reached across passes even at 500 Automata.

For work that has waited more than 24 hours the sweep records a finding with an
**owner** (the person or surface that last acted on the record, or the operator)
and raises one open question with three answers:

- **I will land it** — the landing stays pending, so the Automaton cannot be
  archived, and you are asked again in 3 days if it is still not merged.
- **leave it alone** (the default) — a dated keep decision; asked again in 14 days.
- **drop the work** — a drop decision.

An unanswered question never lands, drops or loses anything. When the work lands
the finding goes away and the question is withdrawn.

**Withdrawing the question.** You can also settle the question without
answering it, by withdrawing it (`datawatch question withdraw <id> --reason "..."`,
or the same action on the other surfaces). A withdrawn finished-work question is
not raised again until one of these happens:

- the branch **moves** to another commit; or
- the **re-ask date** passes, 14 days after the withdrawal.

The date is written on the Automaton's decision log and shown on its finding
(`datawatch autonomous finished-work --json`). The work stays in the list and
the daily digest, because it is still not merged. A withdrawal is **not** a
keep or drop decision: archive and cancel still refuse. To record a decision,
answer the question or archive with `keep` or `drop` instead. A git read that
fails does not count as a move.

**Code done, the rest stuck.** When every code task of an Automaton is complete
and only docs or gate tasks remain (a docs task stalled on an infrastructure
failure, a final gate waiting), the sweep treats the rest as separately
retryable: it offers to land the code and leaves every task and the Automaton
status exactly as they are. Retry the remaining tasks with `reset-task`.
A task is a code task unless it is a smoke or final gate task or its title
starts with docs, document or changelog.

**"Leave the branch alone".** The branch-refresh question's answer "leave the
branch alone" still stops the refresh, and now also records a dated
`leave_alone` decision with a re-ask date. The sweep asks again on that date.

Run one pass on demand with `datawatch autonomous finished-work run sweep`
(REST `POST /api/autonomous/finished-work/run` with `{"run":"sweep"}`, MCP
`autonomous_finished_work_run`, chat `autonomous finished-work run sweep`).

## 3. The daily digest

Once a day, while anything waits, the daemon sends one alert: every Automaton
waiting, with its cause, commit count, owner and question. Read the same list at
any time, from every surface:

| Surface | Command |
|---|---|
| REST | `GET /api/autonomous/finished-work` |
| MCP | `autonomous_finished_work` |
| CLI | `datawatch autonomous finished-work [--json]` |
| Comm channel | `autonomous finished-work` |
| PWA | Automata, Housekeeping sub-tab, the Housekeeping repairs card, the **Finished work waiting to land** section |

## 4. The weekly audit

The audit reads every Automaton, cancelled, archived and branchless ones
included, with no per-pass bound. Besides the three causes above it reports a
work branch that is gone with nothing recording a landing
(`branch_gone_unlanded`) and an unfinished Automaton nothing has touched for a
week (`stuck_no_activity`). It never asks, never changes a record and reads git
read-only. In mode `on` the result is written as one decision record per week
(a second run in the same week returns the same record); in `shadow` the
findings are only stored.

The idle tick runs the audit when the last one is a week old.
`datawatch autonomous finished-work run audit` and the
`autonomous_finished_work_run` tool run it on demand. If you already keep a
weekly audit schedule, it can run the same command; no second schedule is
needed, and a second run in a week writes no second decision record.

## 5. Refusals are reported once

A refused archive, repair or merge step is recorded in the refusal ledger
(`datawatch autonomous housekeeping-repairs`, the `refusals` list) and not tried
again every pass while the record is unchanged: the close step and a failed
repair wait six hours, a branch comparison that could not be made waits five
minutes. A change to the record, or the wait running out, tries again. A step
that goes through forgets its refusal.

## Modes

| Mode | Guard | Sweep and audit |
|---|---|---|
| `on` (default) | refuses archive and cancel | records findings, asks, sends the digest, writes the audit record |
| `shadow` | records what it would refuse in the refusal ledger; the step goes through | records findings only |
| `off` | nothing | nothing |

Set it with `datawatch config set autonomous.finished_work shadow`, the Settings
page, chat (`configure autonomous.finished_work=shadow`), MCP
(`autonomous_config_set`, key `finished_work`) or REST (`PUT /api/config`).

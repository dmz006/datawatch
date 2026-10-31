# Infrastructure retry: transient failures retry on their own

**Date:** 2026-10-30
**Version:** 9.47.1 (the `var Version` in `cmd/datawatch/main.go` when this plan was written; the shipping version is the next minor and is set at merge time)

**Terminology.** Operator-facing text says **Automaton / Automata**, following the Terminology
Rule in `AGENT.md`. The internal Go type is `PRD` (`internal/autonomous`); this document uses
"PRD" only for code identifiers, API paths and tool names.

## Problem

A task attempt can end because the machinery around the task failed, not because the work was
wrong: the worker could not be spawned, a storage write failed because the disk was full or
read-only, the session was killed before the task was delivered, or the daemon restarted under
the running task. Until now each of these counted as a failed attempt, spent the task's failure
budget and, on a repeat, needed an operator to reset the task and every task that failed behind
it by hand.

## Design

### Classifier

`ClassifyInfraFailure` and `ClassifyInfraText` (`internal/autonomous/infra_failure.go`) decide
whether an attempt ended on infrastructure and which of four classes it was: `spawn_error`,
`storage_write`, `session_killed_before_delivery` and `daemon_restart`. They read only the
error's type, its wrapped sentinels and its errno, or, for an already-recorded failure text,
fixed phrases the system or the operating system produces. They never judge work content: a
verifier failure, a content or scope failure, a stall that is not a launch failure, a session
that never started (which has its own start budget) and a rate or usage limit are all left
unclassified.

### Retry and back-off

`autonomous.infra_retry` (`internal/config/infra_retry.go`) holds the policy: `enabled` (default
`true`), `max_tries` (default 3), `backoff_seconds` (default `[60,300,540]`, 15 minutes in all,
the last entry repeating, an explicit empty list meaning no delay), `categories` (default all
four, an explicit empty list meaning none), `exhausted_action` (`hold` by default, or `count`),
`retry_dependents` (default `true`) and `raise_question` (default `true`). Unset keys read their
defaults, and an explicit `false` or `[]` survives a config round trip.

When an attempt fails on a listed class and the task has tries left, the executor loop
(`internal/autonomous/executor.go`) records the retry and sleeps out the delay
(`internal/autonomous/infra_retry.go`). The retry is written to the task as an
`InfraRetryRecord` (number, class, reason cut to 500 characters, time, delay, next time; the
newest 50 kept) with a separate consecutive count, and as an `infra_retry` decision on the
Automaton. While it waits, the task's wait reason reads *infrastructure retry n of max at
<time> (<class>)*. A retry after a daemon restart waits at most the first delay and keeps the
attempt budget.

### Held state and question

When the tries are used up, `exhausted_action` decides (`internal/autonomous/infra_retry_hold.go`).
`count` books one ordinary failed attempt. `hold` parks the task for operator review without
counting a failure, with a failure reason of category infra, a report that lists each retry, an
`infra_retry_exhausted` decision, one error-level alert and, when `raise_question` and the
operator question channel are on, one high-priority blocking question that names the task, the
Automaton, the class and the cause and offers *Retry the task now* (the default) or *Cancel the
task*. Answering it resolves the held task through the same path as resolving the task review;
resolving the review another way withdraws the open question.

### Dependents

A dependent that failed behind a task whose failure was infrastructure is marked with category
infra, source `dependency-infra` and the root task named (`internal/autonomous/infra_retry_dependents.go`).
Once the root is no longer failed, cancelled, blocked or held, `recoverInfraDependents` returns
the marked dependents to pending in dependency order and, when an Automaton ended failed or
blocked only because of them, back to running, recording an `infra_dependents_recovered`
decision. A dependent of a cancelled or content-failed task is never marked. Both halves are
off with `retry_dependents: false`.

### Read model

`GET /api/autonomous/infra-retries` (`internal/server/infra_retry.go`) returns the seven
settings in effect and the tasks that were retried or are held, most recently retried first,
with `automaton` and `limit` (1 to 1000, default 50) as query parameters. The CLI, the comm
channel and the PWA read it; `internal/autonomous/infra_retry_text.go` renders the text the CLI
and the comm channel share.

## Parity surface

| Surface | Included? | Intent |
|---|---|---|
| REST | Yes | `GET /api/autonomous/infra-retries` with `automaton` and `limit`; `PUT /api/config` and `GET /api/config` carry the seven `autonomous.infra_retry.*` keys, the two lists taking a JSON array or a comma list and a bad value refusing the whole put; the task view gains `infra_retries` and `infra_retry_count`; documented in `openapi.yaml`. |
| MCP | Yes | The read-only `autonomous_infra_retries` tool (`automaton`, `limit`) proxies the route; `autonomous_config_set` takes the seven `infra_retry_*` arguments and sends them as dotted keys; the federation capability table lists the tool. |
| CLI | Yes | `datawatch autonomous infra-retries [automaton] [--limit N] [--json]` shows the policy and the retried or held tasks; `datawatch config set autonomous.infra_retry.<key>` covers every key. |
| comm channel | Yes | `autonomous infra-retries [automaton] [limit]` replies with the settings line and the newest tasks cut to one chat message; `configure autonomous.infra_retry.<key>=<value>` sets the keys; a held task is decided with the existing review verb. |
| YAML/config | Yes | The `autonomous.infra_retry` block with all seven keys and their defaults in `docs/config-reference.yaml`. |
| PWA | Yes | An Infrastructure retries card in the Automation view (policy line, each task with *Retry n of max*, class, reason and time, held label and a button for the open question), the badge and held label on the Automaton detail task rows, the seven settings fields with description and glossary link, and 31 locale keys in all five bundles. |
| Android | Excluded in this work | This work does not change the apps. The single mobile parity record is `docs/plans/parity-issues/infra-retry.md`, filed as one sync issue on the datawatch-app repository, which covers the new route, the task fields, the settings block and the locale keys. |
| iPhone/iOS | Excluded in this work | Same single sync issue as Android: `docs/plans/parity-issues/infra-retry.md`. The apps pick the change up from that issue, not from this branch. |

## Scope

Affected packages and trees:

- `internal/config`: the `autonomous.infra_retry` block, its defaults, validation and YAML
  round trip.
- `internal/autonomous`: the classifier, the retry plan, record and wait, the executor loop
  wiring, the hold with its alert and question, dependent marking and recovery, the read model
  and the shared text rendering, the task fields in `models.go`.
- `internal/server`: the read route, the config keys and their checks, `openapi.yaml`, the
  federation read guard.
- `internal/mcp`: the `autonomous_infra_retries` tool and the `autonomous_config_set`
  arguments.
- `internal/router`: the comm verb and the `configure` keys.
- `cmd/datawatch`: the `autonomous infra-retries` command.
- `internal/server/web`: the card, the task badge and held label, the settings fields, and the
  locale keys in all five bundles.
- `docs`: `docs/datawatch-definitions.md` (the Infrastructure retry section the PWA help links
  open), `docs/howto/infrastructure-retry.md`, `docs/operations.md`, `docs/config-reference.yaml`,
  `docs/mcp.md`, `docs/api-mcp-mapping.md`, `docs/testing-tracker.md`, `docs/parity-status.md`,
  `CHANGELOG.md`, and this plan.

Out of scope: retrying a model provider's throttle or usage limit, retrying a session that
never started (it keeps its own start budget), retrying a content, verifier or scope failure,
and any change to the mobile apps.

## Phases

Status as of 2026-10-30: all phases are **Done** on branch `automaton/ced90001`. Nothing is
tagged yet: the version bump is made at merge time, so the shipping version is set then and is
not written here. Every phase's own checklist is the Phase Completion Checklist below.

1. **Phase 1: configuration keys.** Status: **Done**. The `autonomous.infra_retry` block, its
   defaults, validation and round trip in `internal/config/infra_retry.go`.
2. **Phase 2: classifier.** Status: **Done**. The four classes from errors and recorded text in
   `internal/autonomous/infra_failure.go`.
3. **Phase 3: records and back-off.** Status: **Done**. The plan, the task record, the decision
   and the wait in `internal/autonomous/infra_retry.go`.
4. **Phase 4: executor wiring.** Status: **Done**. Each class retries through the attempt loop;
   a daemon restart waits at most the first delay and keeps the attempt budget.
5. **Phase 5: hold and question.** Status: **Done**. The exhausted action, the held state, the
   alert and the operator question, and withdrawal when the task is settled another way.
6. **Phase 6: dependents.** Status: **Done**. Marking of dependents behind an infrastructure
   failure and their automatic recovery.
7. **Phase 7: REST read and config keys.** Status: **Done**. `GET /api/autonomous/infra-retries`
   and the seven keys on `GET` and `PUT /api/config`.
8. **Phase 8: MCP tool.** Status: **Done**. `autonomous_infra_retries` and the
   `autonomous_config_set` arguments.
9. **Phase 9: CLI command.** Status: **Done**. `datawatch autonomous infra-retries`.
10. **Phase 10: comm channel verb.** Status: **Done**. `autonomous infra-retries` and the
    `configure` keys.
11. **Phase 11: PWA and locale keys.** Status: **Done**. The card, the task badge, the settings
    fields and the keys in all five bundles.
12. **Phase 12: mobile parity record.** Status: **Done**. `docs/plans/parity-issues/infra-retry.md`
    and the single sync issue.
13. **Phase 13: operator documentation.** Status: **Done**. The how-to, the operations section
    and the config reference.
14. **Phase 14: glossary, tracker, changelog and plan.** Status: **Done**. The Infrastructure
    retry glossary section, the testing tracker section (Tested: Yes, Validated: No, pending
    the sandbox live check), the `[Unreleased]` changelog entry and this plan.

### Phase Completion Checklist

- [x] Functionality implemented per each phase's description (phases 1 to 11).
- [x] Unit tests added and passing for everything new (`TestInfraRetry*`, `TestInfraFailure_*`,
      `TestInfraBackoff_*`, `TestInfraRetryCLI_*`, `TestInfraRetryComm_*`, the Node tests in
      `internal/server/web/app-infra-retry.test.js`).
- [ ] Live/smoke test against a real running daemon: **pending the sandbox live check**; every
      Validated cell in `docs/testing-tracker.md` stays No until it runs.
- [x] The new infrastructure retry tests green; the full `go test ./...` and
      `node --test internal/server/web/*.test.js` run is the branch's final gate.
- [x] General documentation checklist: `CHANGELOG.md`, `docs/config-reference.yaml`,
      `docs/operations.md`, `README.md`, the doc index in `README.md` and `docs/README.md`,
      `docs/testing-tracker.md`, no leaked tracker IDs (`scripts/check-no-internal-refs.sh`),
      and the Parity surface section above is accurate.
- [x] Conditional docs: `docs/datawatch-definitions.md` (Infrastructure retry section) and
      `docs/howto/infrastructure-retry.md` added. `docs/flow/*.md`: N/A, no new data flow
      beyond the existing attempt loop and review path.
- [x] Mobile-Parity Rule: one sync issue filed on the datawatch-app repository, recorded in
      `docs/plans/parity-issues/infra-retry.md` and `docs/parity-status.md`.
- [x] Localization Rule: every new key is in all five bundles and
      `TestLocales_AllAppJSKeysExistInEnglishBundle` passes.
- [x] Version bump: N/A on this branch. The minor version bump is made at merge time, so
      `var Version` in `cmd/datawatch/main.go` and `internal/server/api.go` are unchanged here.
- [x] This plan's phase-status line is current (all phases Done on this branch).

Shipped in: not yet released (the merge helper sets the version at merge; this document does
not name a version on purpose).

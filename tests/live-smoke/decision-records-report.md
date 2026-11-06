# Decision records: sandbox live check

- Date: 2026-11-06 13:42:15 UTC
- Sandbox port: 18443 (TLS, own data directory and config)
- Daemon version: datawatch v9.49.1

| Step | Check | Result | Observation |
|------|-------|--------|-------------|
| 1 | create a record over REST and read back its question | PASS | ok: created dr-494690fdd9a6b543e324, question q-18e5781438977d8a47fc; ok: question text built from the record; ok: 2 options, both with trade-offs; ok: recommendation fast; ok: default hold;  |
| 2 | one question per record (REST, CLI list, comm-style path) | PASS | ok: second record dr-6ee29d900b0b2fbe6b0b got its own question q-18e578143b37f91c0f10; ok: two records, two different questions; ok: record dr-494690fdd9a6b543e324 has exactly 1 question in the question list; ok: record dr-6ee29d900b0b2fbe6b0b has exactly 1 question in the question list; ok: CLI 'autonomous decisions' lists both questions; ok: comm-style path /api/autonomous/decisions answers 200 and names q-18e5781438977d8a47fc (1 time(s)); ok: record list holds one record for question q-18e5781438977d8a47fc;  |
| 3 | clarity: refusal in refuse mode, warning in warn mode | PASS | ok: PUT clarity_mode=refuse; ok: refuse mode: 422 with findings no_tradeoffs,text_too_short,label_not_human; ok: the refused record was not stored; ok: warn mode: stored dr-51f48c70864524dc0b76 with warnings no_tradeoffs,text_too_short,label_not_human;  |
| 4 | edit an open record: question text changes on the same question id | PASS | ok: PUT answered 200, question id still q-18e5781438977d8a47fc; ok: question text now carries the edit (was different before);  |
| 5 | answer with a multi-line quote; unconfigured follow_up is refused | PASS | ok: record dr-6985004edb693409f130 links no Automaton; ok: follow_up not configured -> 400; ok: question still open after the refused answer; ok: answer without follow_up accepted (201); ok: read-back quote is the 3-line quote, byte for byte; ok: actor live-operator; ok: surface cli; ok: record answered, question state answered; ok: the list shows the quote, actor, surface and question state; ok: configured follow_up 'kick' accepted; ok: follow-up ran: status=error result=start template no-such-template: template "no-such-template" not found;  |
| 6 | document status line: report mode leaves the file, on mode rewrites one line | PASS | ok: report mode: file byte-identical; ok: report mode shows the change: 'Confirmed: pending the live check' -> 'Confirmed: fast - "keep it small" (live-operator, 2026-11-06)' (status reported); ok: exactly one line rewritten; ok: line now reads: Confirmed: fast - "apply it now" (live-operator, 2026-11-06);  |
| 7 | folder watch: raise, edit, delete; then another delimiter and separator | PASS | ok: folder configured; ok: [folder-default] file raised record dr-e7f4a54899373f625f36 and question q-18e5782201e626504363; ok: [folder-default] edit updated the record, same question; ok: [folder-default] deleting the file withdrew the record; ok: delimiter +++ and separator = configured; ok: [folder-custom] file raised record dr-b930773ea5c010ed9d2f and question q-18e5784bea0fdb5cdf87; ok: [folder-custom] edit updated the record, same question; ok: [folder-custom] deleting the file withdrew the record;  |
| 8 | a task spec naming the question id waits, then starts after the answer | SKIPPED | the sandbox has no planning backend, so the Automaton stays draft with 0 tasks and cannot hold a task (decompose answered 200); ok: gate question q-18e57867ebe32478b23f raised; ok: Automaton ed1a0001 created;  |

## Cleanup

Records withdrawn: 6; already answered or withdrawn: 4. Automaton removed: ed1a0001. Sandbox daemon ended by its recorded PID. Temporary folders removed. Leftover check: no leftover test daemon.

RESULT: PASS

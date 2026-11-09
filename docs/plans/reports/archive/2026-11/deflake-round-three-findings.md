# Deflake round three — findings (TestUIPromptBlocked_RunningParksSuppressesAndExposes)

- Date: 2026-11-02
- Package: `internal/session`
- Test: `TestUIPromptBlocked_RunningParksSuppressesAndExposes`
- Toolchain: go 1.27.2, linux/amd64, 48-core host
- Scope: read-only investigation; reproduction only. No test or production code was edited, nothing else was changed.

## Verdict

The flake is a **test-ordering (happens-before) gap, not a production race**:
the parking is committed (state `waiting_input` + `UIPrompt` set) inside a
store `Mutate`, but the state-change callback the test asserts on
(`cb.oldStates`) is fired by the capture goroutine only **after** that
`Mutate` returns (and after a `SaveSession`). The test's parking wait
(`parkSession`) only polls the *store state* — it never waits for the
callback — so the assertion `len(old) != 1` can legally run in the window
between "state visible" and "callback fired". The hypothesis is confirmed.

## Exact failure (reproduced under load)

```
--- FAIL: TestUIPromptBlocked_RunningParksSuppressesAndExposes (0.07s)
    ui_prompt_blocked_test.go:194: side effects while blocked: needsInput=0 oldStates=[]; want 0 and exactly [running]
FAIL
```

All preceding assertions in the test (state `waiting_input`, `UIPrompt`
exposed, `UIPromptFor` snapshot) **passed** in the failing run — the store
mutate was fully visible — and only the callback snapshot was empty.
That is precisely the predicted gap, not a detection or save problem.

## Reproduction

Environment (single core + self-managed busy loops):

- `taskset -c 0` on the test, `GOMAXPROCS=1`
- 16 background busy loops (`bash -c 'while :; do :; done'`) pinned to
  core 0, started before and killed **by their own recorded PIDs** after
  each run (never by name, no `tmux kill` involved)
- scratch manager only (`t.TempDir()` via `newTestManagerWithFake`); the
  real data directory is never touched

Command:

```sh
# baseline (unloaded): PASS (20/20 clean)
go test ./internal/session/ -race -count=20 -run TestUIPromptBlocked_RunningParksSuppressesAndExposes

# under load:
GOMAXPROCS=1 taskset -c 0 go test ./internal/session/ \
  -race -count=200  -run TestUIPromptBlocked_RunningParksSuppressesAndExposes   # PASS
GOMAXPROCS=1 taskset -c 0 go test ./internal/session/ \
  -race -count=500  -run TestUIPromptBlocked_RunningParksSuppressesAndExposes   # PASS
GOMAXPROCS=1 taskset -c 0 go test ./internal/session/ \
  -race -count=2000 -run TestUIPromptBlocked_RunningParksSuppressesAndExposes   # first FAIL (below)
GOMAXPROCS=1 taskset -c 0 go test ./internal/session/ \
  -race -count=300  -run TestUIPromptBlocked_RunningParksSuppressesAndExposes   # 10 further batches, PASS
```

Counts: **1 failure within the 2000-run iteration (the runner aborts at the
first failed repeated run, so its exact index is unknown)**; every other
iteration in this matrix — 200 + 500 + 10×300 = **≥3,700 runs, all green**
— plus 20 unloaded baseline runs. Observed failure rate is on the order of
**10⁻⁴–10⁻³ (a few in several thousand, upper 95 % bound ≈ 1 in ~4,000)**
and scheduler-pressure dependent: it did not surface in the first 700
loaded runs but hit within the 2000-run iteration, then not again in the
following 3,000.

## The timing assumption, with file and line

Production path (`internal/session/ui_prompt_blocked.go`):

1. `Manager.moveToBlocked` (line 104)
2. `m.store.Mutate(...)` (line 110) — one write-lock critical section that
   sets **both** `sess.State = StateWaitingInput` (line 128) and
   `sess.UIPrompt = info` (line 131), plus `UpdatedAt` (line 132).
   **Confirmed: UI-prompt and state are written in the same mutate**, so a
   reader that observes `waiting_input` always also sees `UIPrompt` set.
3. `m.SaveSession(sess)` (lines 139–141)
4. `m.onStateChange(sess, rec.fromState)` (line 143) — the state-change
   callback is fired **after** the mutate has released the store lock and
   after the save attempt, on the capture goroutine.

Test side (`internal/session/ui_prompt_blocked_test.go`):

1. `parkSession` (line 131) waits only on the store view:
   `pollUntil(... snap.State == StateWaitingInput ..., 15s)` (lines
   134–136, condition at line 135). It returns at the instant the store
   shows `waiting_input` — with the `onStateChange` call (ui_prompt_blocked.go
   line 143) still not executed on the capture goroutine.
2. The test then asserts the callback side effects at lines 193–194
   (`cb.snapshot(); needs != 0 || len(old) != 1 || old[0] != StateRunning`
   → the exact `oldStates=[]` failure above).

The gap: between step 2.1 and 1.3 there is no happens-before edge visible
to the test. The only synchronisation the test relies on is the store lock
(its `liveSnap` → `Store.Snapshot`, store.go line 842, takes the read
lock), which orders the *store writes* but says nothing about the
post-mutate callback. On a loaded single core the capture goroutine can be
descheduled somewhere after the `Mutate` unlock (e.g. inside the
`applied` check / `SaveSession` / `store.Save` / `onStateChange` prologues
in ui_prompt_blocked.go lines 135–143) while the test goroutine's 5 ms
poll timer fires and it marches through the remaining sub-microsecond
assertions — exactly the observed interleaving. The race detector found no
data race in the failing run (the failure is a plain assertion, not a
`WARNING: DATA RACE`), consistent with "correctly-locked, wrongly-ordered
test expectation" rather than a memory race.

## Proposed fix (not applied in this task)

Prefer the test-side fix — make the parking wait observe the same thing
the test then asserts (the announced transition), instead of the store
state the callback trail derives from. All suggested changes are confined
to `internal/session/ui_prompt_blocked_test.go`:

1. **Recommended (minimal, one helper):** when a callback recorder is
   wired, extend `parkSession`'s `pollUntil` condition (ui_prompt_blocked_test.go
   lines 134–136) to also require the transition to have been announced:
   `snap.State == StateWaitingInput && (cb == nil || cb.snapshot() shows
   needs==0 and exactly [StateRunning])`. `old[0] == StateRunning` is
   already checked at line 194, so the wait and the assertion agree. The
   final block at lines 193–194 then becomes deterministic.
2. **Alternative (keeps `parkSession` byte-identical):** after
   `parkSession`, in the affected test, replace the direct
   `cb.snapshot()` check at lines 193–194 with a bounded
   `pollUntil(..., cond, ...)` on the same condition (pattern already used
   elsewhere in this package's tests), then assert once.
3. **Generalise:** the identical pattern exists in the sibling tests
   `TestUIPromptBlocked_StaticPaneTicksKeepParked` (lines 219–222),
   `TestUIPromptBlocked_PaneClearsRestoresOriginalState` (lines 244 and
   263–264), `TestUIPromptBlocked_TerminalStateNeverRestored` (line 380)
   and `TestUIPromptBlocked_TimelineOneLinePerChange` (via `parkSession`).
   Fixing the shared `parkSession` helper (option 1) de-flakes all of
   them at once.
4. No production-code change is required: the callback ordering in
   `moveToBlocked` (mutate → save → notify) is the intended contract for
   daemon consumers; only the test's synchronisation assumption was wrong.

Do **not** fix this by asserting on `UIPrompt` only (the state/UIPrompt
pair is indeed atomic — same mutate at ui_prompt_blocked.go lines 128
and 131), and do not add a `time.Sleep`: the fix must close the ordering
gap itself, not widen the test's tolerance for it.

## Evidence hygiene

- Reproduction output for the failed iteration: retained in the task
  worker's session log; the assertion line above is the exact
  `t.Fatalf` text from `ui_prompt_blocked_test.go:194`.
- No changes to `internal/session` code or tests; no changes outside this
  report file.

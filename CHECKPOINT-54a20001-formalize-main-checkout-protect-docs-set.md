# Checkpoint — docs set for automata created by defect intake (main-checkout protection)

## Status: COMPLETE (docs verified accurate; one wording fix applied; gates green)

## Verified facts (code as of HEAD, branch automaton/54a20001--...)

1. **Defect intake creates Automata with no project dir.** `cmd/datawatch/defect_queue_backend.go`:
   `defectProjectDir(project string)` always returns `""` (e276475f3 removed the
   `filepath.IsAbs` branch). Comment: "Empty dir is the worktree-mode signal, so
   the worker runs against a dedicated worktree of the repository instead of the
   repository's main checkout". Used at lines 287, 390, 476.
2. **Launch guard refuses approve + run.** `internal/autonomous/main_checkout_guard.go`:
   - `isMainCheckout(projectDir, baseRepo)` — only true when projectDir resolves
     to a path that has a `.git` **directory** (main tree, not linked worktree
     which has a `.git` file) and matches `baseRepo` via EvalSymlinks.
   - `refuseMainCheckout(prd)` — checks `ProjectDir`, then `Project` (legacy),
     against `Config.WorktreeBaseRepo`; injectable `mainCheckoutCheck` seam.
   - Error: "automaton %q points at %s, the main working tree of the daemon's
     own repository; approve and run are refused there; recreate the Automaton
     without a project dir, then it runs isolated in its own worktree".
   - Call sites: `manager.go:4158` (approve flow) and `executor.go:213` (start flow).
3. **Supervisor finding.** `internal/autonomous/supervisor_watch_main_checkout.go`:
   fires only for open statuses (not failed/rejected/cancelled/archived/completed,
   not running/waiting); message: "open automaton '<id>' runs in the main
   checkout of the daemon's own repository; approve and run are refused there;
   recreate the automaton without a project dir and it runs in its own worktree".
   Operator text identical (`supervisor_watch_text.go`).
4. **Pass wiring + triage notes.** `supervisor_watch_pass.go:134` runs the check;
   `supervisorWatchAutomatonScoped` note kinds = task_failed, task_review_needed,
   automaton_not_merged, worktree_no_gomod, worktree_not_registered,
   worktree_silent, verify_script_not_executable, main_checkout_automaton —
   exactly the list in the howto's "The triage record" section.
5. **Findings count:** 18 kinds; script also runs 12 + `priority_stall` +
   `backlog` (14 total); watch-only = worktree_not_registered,
   verify_script_not_executable, main_checkout_automaton, archived_unmerged (4).
   12 + 2 + 4 = 18. Consistent across all three docs.

## Doc state (final)

- `docs/howto/automation-supervisor-watch.md` — table row, triage-note list,
  "Where the watch and the script differ" (four watch-only kinds), open/closed
  sections all correct. **Fixed this pass:** one formatting artifact in the
  "Four watch-only kinds" bullet (awkward forced line breaks from 139d0004/139d0005
  edits) — reflowed to normal prose, wording unchanged.
- `docs/datawatch-definitions.md` — Supervisor watch entry correct: "eighteen
  checks — twelve that the script also runs, priority_stall and backlog, which
  both the script and the watch now report, plus worktree_not_registered,
  verify_script_not_executable, main_checkout_automaton and archived_unmerged,
  which only the watch has"; defect-intake sentence ("creates its Automatons
  with no project dir, so each runs isolated in its own worktree; the launch
  guard refuses approve and run ... and main_checkout_automaton surfaces an open
  Automaton still pointed at it") matches the code behavior and remediation.
- `docs/operations.md` — Supervisor watch section correct (same counts, same
  watch-only list, links to the howto resolve).

## Links & gates

- All relative links in both in-scope docs resolve (checked file + anchor).
- `bash scripts/check-plans-links.sh --quiet` → rc 0.
- `sh scripts/check-docs-sync.sh` → "docs/ and internal/server/web/docs in
  sync" rc 0 (embedded copy is gitignored, regenerated at build).
- Only working-tree change: `M docs/howto/automation-supervisor-watch.md`.
- `grep -r "defectQueue\|defect_queue" docs/howto/automata-defect-queue.md` has
  no stale project-dir / checkout claims to fix (file does not discuss project
  dir at all).

## Operator-review flags (outside story docs boundary, not edited)

1. `internal/server/web/openapi.yaml` (enum at line 14279) includes
   `main_checkout_automaton` but is missing `archived_unmerged`,
   `priority_stall` and `backlog` — the OpenAPI `kind` enum lags the actual
   18-kind vocabulary the docs and code report. Spec-only gap; the runtime
   surfaces (REST/MCP/CLI/comm/PWA) are unaffected.
2. `docs/howto/README.md` line 80 (summary of the howto) names three of the four
   watch-only kinds (worktree_not_registered, verify_script_not_executable,
   archived_unmerged) and omits `main_checkout_automaton`. Readable as a
   non-exhaustive feature summary, but could mention it.
3. `docs/commands.md` and `docs/plans/2026-11-04-supervisor-watch.md` predate the
   18-kind set; neither makes a contradictory claim in this pass, but anyone
   writing a new doc from them should use the definitions/howto as the
   vocabulary source of truth.

## Files touched this pass

- `docs/howto/automation-supervisor-watch.md` — wording/formatting only
  ("Four watch-only kinds" bullet reflow), no semantic change.

DATAWATCH_COMPLETE: Verified the supervisor-watch docs set (howto, glossary entry, operations section) is accurate against the main-checkout protection code (defect intake creates project-dir-less Automata → worktree isolation; launch guard refuses approve/run on the main checkout; main_checkout_automaton is a watch-only finding of the 18-kind vocabulary). Fixed one formatting artifact in the howto. All links resolve; check-plans-links and check-docs-sync green. Three operator-review flags recorded (openapi kind enum missing 3 kinds; howto/README summary omits main_checkout_automaton; older planning docs predate the 18-kind set).

# Automaton verify scripts: cancel and recreate — final gate report

Run date: 2026-11-02.

## Verify scripts did not run

The four `bash .datawatch/verify/*.sh` checks could not run. `.datawatch/verify/` does not exist in this worktree
(`saved-verify-store.sh`, `cancel-saves-verify-scripts.sh`, `automaton-restore-stray-folder.sh`,
`task-worktree-restores-script.sh` and `final-gate-report.sh` all returned "No such file or directory", exit 127).
No task owns those files, so there was nothing to fix. As a substitute, the covering Go tests were run directly:
`go test ./internal/autonomous -run 'TestEnsureWorktree_|TestCancelSavesVerifyScripts_|TestTaskWorktree_Restores|VerifyScriptStore' -v`
(not the whole package). Result: `ok`, every test below PASS.

## Required cases

| # | Case | Test | Result |
|---|------|------|--------|
| 1 | Cancel saves scripts | `TestCancelSavesVerifyScripts_CopyAndRelease`, `TestCancelSavesVerifyScripts_HeldReleaseStillSaves`, `TestVerifyScriptStore_SaveCopiesAllFiles` | PASS |
| 2 | Creation restores them | `TestEnsureWorktree_RestoresSavedScripts`, `TestVerifyScriptStore_RestoreFillsOnlyMissing` | PASS |
| 3 | Stray folder moved aside, real worktree created | `TestEnsureWorktree_StrayFolderMovedAside` | PASS |
| 4 | Task worktree receives the script | `TestTaskWorktree_RestoresVerifyScriptFromAutomatonWorktree`, `TestTaskWorktree_RestoresVerifyScriptFromSavedFolder`, `TestTaskWorktree_RestoresVerifyScriptAfterRecreate` | PASS |
| 5 | Missing everywhere still yields the setup fault | `TestTaskWorktree_RestoresVerifyScriptMissingEverywhereIsNotAnError` (`ErrVerificationScriptMissing`) | PASS |

Extra coverage that also passed: `TestCancelSavesVerifyScripts_DeleteRemovesSaved`,
`TestCancelSavesVerifyScripts_NoScriptsSavesNothing`, `TestVerifyScriptStore_RemoveDeletesSavedFolder`.

## Changelog check

`CHANGELOG.md` has an entry under `## [Unreleased]` ("Cancelling an Automaton now keeps its verification scripts…").
It covers cancel saving, restore on create and recreate, the stray-folder move-aside and the setup fault. Result: present.

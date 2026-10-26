# Path-injection alert decisions

Scope: open `go/path-injection` code-scanning alerts in the session, autonomous, gitguard and pipeline code. Line numbers are the ones the alert reported at triage time.

The shared containment helper is `gitguard.ResolveWithin(base, rel)` in `internal/gitguard/contain.go`, next to the project directory guard. It refuses an empty base, an absolute or NUL-bearing `rel`, any `..` element, and any result whose symlink-resolved path leaves the symlink-resolved base (checked with `filepath.Rel`). Missing path tails are allowed; nothing is created.

| Alert | File:line | Decision | Reason |
|-------|-----------|----------|--------|
| 696 | internal/session/manager.go:1923 | fix (relative input) / dismiss (absolute input) | `checkExistingWorktree` does `os.Stat(dir)` on `projectDir`, which comes from the start request (REST, MCP, automaton task). A relative value was joined under the workspace root with no containment, so `../..` left the root. Now `resolveWorkspaceDir` calls `ResolveWithin` before the join is used. An absolute value is an intended feature (callers pass absolute project directories) and the worktree check only reads metadata, so absolute input is dismissed. |
| 695 | internal/session/manager.go:1913 | fix (relative input) / dismiss (absolute input) | Same flow as 696, the `os.Stat` at the top of `checkExistingWorktree`; covered by the same change and the same reason. |
| 658 | internal/session/manager.go:2008 | fix (relative input) / dismiss (absolute input) | `os.MkdirAll(projectDir)` on the same value. The relative case is contained by `resolveWorkspaceDir`. An explicit absolute directory is how operators place sessions outside the workspace, so it stays; no check was removed. |
| 689 | internal/session/store_records.go:440 | dismiss | `os.Remove(s.recordPath(id))`. The file name comes from `recordFileName`, which keeps the id only when it is 1 to 120 characters of `[A-Za-z0-9_-]` and otherwise replaces it with a SHA-256 hex digest. No separator or dot can reach the path, and the directory is the store's own records directory under the daemon data dir. |
| 688 | internal/autonomous/store_records.go:244 | dismiss | `os.Remove` of `<recordsDir>/<recordName(id)>.json`. `recordName` applies the same allow-list (64 characters of `[A-Za-z0-9_-]`) or a SHA-256 digest; the directory is the store's own. |
| 687 | internal/autonomous/store_records.go:241 | dismiss | `os.RemoveAll` of `<recordsDir>/<recordName(id)>.files`, same sanitized name as 688. The CodeQL query does not model the allow-list loop as a sanitizer. |
| 690 | internal/gitguard/projectdir.go:59 | dismiss | This is the project directory guard itself. `os.Lstat(filepath.Join(abs, m))` joins a fixed marker name (`go.mod`, `.git`, ...) onto the directory being judged and only tests existence. The directory is normalized first and nothing is read, written or deleted. |
| 675 | internal/gitguard/policy.go:74 | dismiss | `Policy.Keep` only calls `os.Stat` to read a file's size and type for the staging filter. Candidates are the paths git itself lists for the repository (`attempt_snapshot.go`, `session/git.go`), and a stat of an odd path only changes whether it is staged; `git add` then applies its own repository boundary. No content is read or written. |
| 676 | internal/gitguard/policy.go:158 | fix | `CleanTempPacks` reads `<repoDir>/.git/objects/pack` where `repoDir` is the session project directory. A symlinked `.git/objects/pack` would send the listing, and the deletes at 677, outside the repository. The pack directory now goes through `ResolveWithin` first; an escape returns `ErrPathEscapes` and deletes nothing. A missing base or pack directory still returns no error and no removals. |
| 677 | internal/gitguard/policy.go:189 | fix | `os.Remove` of an entry in the pack directory. Entry names come from `os.ReadDir`, so they carry no separator, but the directory was uncontained; covered by the 676 change. |
| 681 | internal/pipeline/quality.go:96 | dismiss | `RunTests` does `os.Stat(projectDir)` and sets it as the working directory of the configured test command. The directory comes from the PRD record an operator created; the command is an arbitrary shell string that the same operator configures, so restricting its working directory would not narrow what it can reach. Only metadata is read. |

## Tests added for the fixes

- `internal/gitguard/contain_test.go`: valid in-base paths (including a missing tail and a symlink that stays inside), traversal strings, absolute and NUL paths, symlink escape, symlinked base, missing base.
- `internal/gitguard/policy_test.go`: `TestCleanTempPacksRefusesSymlinkEscape` with a `t.TempDir` target outside the repository.
- `internal/session/project_dir_test.go`: valid relative directory, missing root created, traversal strings, symlink escape.

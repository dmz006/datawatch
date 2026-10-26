# Path-injection alert decisions

Scope: open `go/path-injection` code-scanning alerts in the session, autonomous, gitguard and pipeline code. Line numbers are the ones the alert reported at triage time.

The shared containment helper is `gitguard.ResolveWithin(base, rel)` in `internal/gitguard/contain.go`, next to the project directory guard. It refuses an empty base, an absolute or NUL-bearing `rel`, any `..` element, and any result whose symlink-resolved path leaves the symlink-resolved base (checked with `filepath.Rel`). A dangling symlink is followed to its missing target and judged by where that target lies (relative targets are resolved against the link's directory; loops are refused after 40 hops). Missing plain path tails are allowed; nothing is created.

| Alert | File:line | Decision | Reason |
|-------|-----------|----------|--------|
| 696 | internal/session/manager.go:1923 | fix | `checkExistingWorktree` does `os.Stat(dir)` on `projectDir`, which comes from the start request (REST, MCP, automaton task). Relative values are now joined under the workspace root (or the process working directory when no root is configured) through `resolveWorkspaceDir`/`ResolveWithin`, so `..` and symlink escapes are refused. Absolute values, which operators legitimately use to place a session anywhere, go through `gitguard.CleanProjectDir`: NUL bytes, empty values and a filesystem root are refused and the path is cleaned before any `os.Stat` or `os.MkdirAll`. |
| 695 | internal/session/manager.go:1913 | fix | Same flow and same change as 696 (the `os.Stat` at the top of `checkExistingWorktree`). |
| 658 | internal/session/manager.go:2008 | fix | `os.MkdirAll(projectDir)` on the same value; it now only sees the contained relative result or the cleaned absolute result. |
| 689 | internal/session/store_records.go:440 | dismiss | `os.Remove(s.recordPath(id))`. The file name comes from `recordFileName`, which keeps the id only when it is 1 to 120 characters of `[A-Za-z0-9_-]` and otherwise replaces it with a SHA-256 hex digest. No separator or dot can reach the path, and the directory is the store's own records directory under the daemon data dir. |
| 688 | internal/autonomous/store_records.go:244 | dismiss | `os.Remove` of `<recordsDir>/<recordName(id)>.json`. `recordName` applies the same allow-list (64 characters of `[A-Za-z0-9_-]`) or a SHA-256 digest; the directory is the store's own. |
| 687 | internal/autonomous/store_records.go:241 | dismiss | `os.RemoveAll` of `<recordsDir>/<recordName(id)>.files`, same sanitized name as 688. The CodeQL query does not model the allow-list loop as a sanitizer. |
| 690 | internal/gitguard/projectdir.go:59 | dismiss | This is the project directory guard itself. `os.Lstat(filepath.Join(abs, m))` joins a fixed marker name (`go.mod`, `.git`, ...) onto the directory being judged and only tests existence. The directory is normalized first and nothing is read, written or deleted. |
| 675 | internal/gitguard/policy.go:74 | dismiss | `Policy.Keep` only calls `os.Stat` to read a file's size and type for the staging filter. Candidates are the paths git itself lists for the repository (`attempt_snapshot.go`, `session/git.go`), and a stat of an odd path only changes whether it is staged; `git add` then applies its own repository boundary. No content is read or written. |
| 676 | internal/gitguard/policy.go:158 | fix | `CleanTempPacks` reads `<repoDir>/.git/objects/pack` where `repoDir` is the session project directory. A symlinked `.git/objects/pack` would send the listing, and the deletes at 677, outside the repository. The pack directory now goes through `ResolveWithin` first; an escape returns `ErrPathEscapes` and deletes nothing. A missing base or pack directory still returns no error and no removals. |
| 677 | internal/gitguard/policy.go:189 | fix | `os.Remove` of an entry in the pack directory. Entry names come from `os.ReadDir`, so they carry no separator, but the directory was uncontained; covered by the 676 change. |
| 681 | internal/pipeline/quality.go:96 | fix | `RunTests` does `os.Stat(projectDir)` and uses it as the working directory of the configured test command. The value comes from the PRD record, not the daemon config, so it is not dismissible. `projectDir` now goes through `gitguard.CleanProjectDir` first (NUL, empty and filesystem-root values are refused with an unusable result, the path is cleaned). Restricting it to a base is not possible: PRD project directories are absolute operator choices and the test command is an arbitrary shell string. |

## Absolute project directories

Absolute project directories are an intended product behaviour: an operator names where a session or test run happens. No base exists to contain them under, so the fix for these alerts is normalisation plus refusal of unusable values (`gitguard.CleanProjectDir`), not a boundary. The sink is not moved to a stricter policy such as `RefuseNonProject`, because that guard refuses a not-yet-created project directory that `Start` is expected to create. Raising the bar (an allow-list of roots) is a separate product choice and is not needed for any of these alerts to carry a decision.

## Tests added for the fixes

- `internal/gitguard/contain_test.go`: valid in-base paths (including a missing tail and a symlink that stays inside), traversal strings, absolute and NUL paths, symlink escape, dangling symlink escape (absolute and relative targets), dangling link that stays inside, symlink loop, symlinked base, missing base.
- `internal/gitguard/policy_test.go`: `TestCleanTempPacksRefusesSymlinkEscape` with a `t.TempDir` target outside the repository.
- `internal/gitguard/contain_test.go`: `TestCleanProjectDir` (cleaning, empty, root, NUL, relative made absolute).
- `internal/pipeline/quality_test.go`: `TestRunTestsRefusesUnsafeProjectDir`.
- `internal/session/project_dir_test.go`: valid relative directory, missing root created, traversal strings, symlink escape.

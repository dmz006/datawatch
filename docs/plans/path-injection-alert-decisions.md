# Path-injection alert decisions

Scope: open `go/path-injection` code-scanning alerts in the session, autonomous, gitguard and pipeline code. Line numbers are the ones the alert reported at triage time.

Operator decision, 2026-10-26 (recorded in `2026-10-26-codeql-path-injection-decisions.md`): allowed project roots are the PRD worktree base and the daemon's workspace root, always enforced, with more roots only through `server.allowed_project_roots`. The four alerts that turn on an absolute, request-supplied project directory (695, 696, 658, 681) are therefore **fixes**, not dismissals.

Two shared helpers live in `internal/gitguard`, next to the project directory guard:

- `gitguard.ResolveProjectDir(dir, roots)` (`roots.go`) for a project directory that may be absolute. It cleans `dir` (empty, NUL and filesystem-root values are refused), resolves `..` and symlinks (a dangling link is followed to where it points), and accepts the result only when it equals or lies under one of `roots` (`filepath.Rel` after both sides are symlink-resolved). An empty `roots` list refuses everything; the check never fails open. It returns `ErrOutsideAllowedRoots`, with a message that names the roots.
- `gitguard.ResolveWithin(base, rel)` (`contain.go`) for a path that must be relative to a base.

The roots come from `Config.AllowedProjectRoots()`: `autonomous.worktree_dir` (default `~/.datawatch/prd-worktrees`), `session.workspace_root` when set, then `server.allowed_project_roots`. The first two are always in the list.

`ResolveWithin` refuses an empty base, an absolute or NUL-bearing `rel`, any `..` element, and any result whose symlink-resolved path leaves the symlink-resolved base (checked with `filepath.Rel`). A dangling symlink is followed to its missing target and judged by where that target lies (relative targets are resolved against the link's directory; loops are refused after 40 hops). Missing plain path tails are allowed; nothing is created.

| Alert | File:line | Decision | Reason |
|-------|-----------|----------|--------|
| 696 | internal/session/manager.go:1923 | fix | `checkExistingWorktree` does `os.Stat(dir)` on `projectDir`, which comes from the start request (REST, MCP, chat, CLI, automaton task). `Start` now resolves a relative value under the workspace root (or the process working directory when none is set) through `resolveWorkspaceDir`/`ResolveWithin`, cleans an absolute one with `CleanProjectDir`, and then checks every request-supplied value with `gitguard.ResolveProjectDir` against `Manager.AllowedProjectRoots()` before any `os.Stat` or `os.MkdirAll`. A directory outside the PRD worktree base, the workspace root and `server.allowed_project_roots` is refused (REST answers 400). Not request input, so not held to the roots: the empty value (the daemon default) and `session.default_project_dir`, which is daemon configuration. |
| 695 | internal/session/manager.go:1913 | fix | Same flow and same change as 696 (the `os.Stat` at the top of `checkExistingWorktree`). |
| 658 | internal/session/manager.go:2008 | fix | `os.MkdirAll(projectDir)` on the same value; it only sees a directory that passed `ResolveProjectDir`. A refused start creates nothing, including through a symlink or a dangling link (tested). |
| 689 | internal/session/store_records.go:440 | dismiss | `os.Remove(s.recordPath(id))`. The file name comes from `recordFileName`, which keeps the id only when it is 1 to 120 characters of `[A-Za-z0-9_-]` and otherwise replaces it with a SHA-256 hex digest. No separator or dot can reach the path, and the directory is the store's own records directory under the daemon data dir. |
| 688 | internal/autonomous/store_records.go:244 | dismiss | `os.Remove` of `<recordsDir>/<recordName(id)>.json`. `recordName` applies the same allow-list (64 characters of `[A-Za-z0-9_-]`) or a SHA-256 digest; the directory is the store's own. |
| 687 | internal/autonomous/store_records.go:241 | dismiss | `os.RemoveAll` of `<recordsDir>/<recordName(id)>.files`, same sanitized name as 688. The CodeQL query does not model the allow-list loop as a sanitizer. |
| 690 | internal/gitguard/projectdir.go:59 | dismiss | This is the project directory guard itself. `os.Lstat(filepath.Join(abs, m))` joins a fixed marker name (`go.mod`, `.git`, ...) onto the directory being judged and only tests existence. The directory is normalized first and nothing is read, written or deleted. |
| 675 | internal/gitguard/policy.go:74 | dismiss | `Policy.Keep` only calls `os.Stat` to read a file's size and type for the staging filter. Candidates are the paths git itself lists for the repository (`attempt_snapshot.go`, `session/git.go`), and a stat of an odd path only changes whether it is staged; `git add` then applies its own repository boundary. No content is read or written. |
| 676 | internal/gitguard/policy.go:158 | fix | `CleanTempPacks` reads `<repoDir>/.git/objects/pack` where `repoDir` is the session project directory. A symlinked `.git/objects/pack` would send the listing, and the deletes at 677, outside the repository. The pack directory now goes through `ResolveWithin` first; an escape returns `ErrPathEscapes` and deletes nothing. A missing base or pack directory still returns no error and no removals. |
| 677 | internal/gitguard/policy.go:189 | fix | `os.Remove` of an entry in the pack directory. Entry names come from `os.ReadDir`, so they carry no separator, but the directory was uncontained; covered by the 676 change. |
| 681 | internal/pipeline/quality.go:96 | fix | `RunTests` does `os.Stat(projectDir)` and uses it as the working directory of the configured test command. The value comes from the PRD record, not the daemon config, so it is not dismissible. `RunTests` and `EvaluateGate` now take the allowed roots and run `gitguard.ResolveProjectDir` first; a directory outside every root is reported as an unusable run ("project directory refused: ..."), never executed. The pipeline executor and the autonomous manager pass `Config.AllowedProjectRoots()` live; with no roots configured nothing runs. |

## Absolute project directories

An absolute project directory is accepted only inside the allowed roots. This is the operator's rule and it is enforced whether or not an API token is set.

| Where | How |
|-------|-----|
| Session start (REST, MCP, chat, CLI, PRD task sessions, restarts of stored sessions) | `session.Manager.Start` checks every request-supplied directory with `ResolveProjectDir` against `Manager.AllowedProjectRoots()`: the config roots, the manager's workspace root and any root added with `AllowProjectRoot` (embedders and tests). |
| Automaton creation with an explicit `project_dir`, per-task `project_dir`, instantiate from a template store | `autonomous.API.CreatePRD`, `SetTaskProjectDir` and `InstantiateFromTemplateStore` call `Manager.CheckProjectDir`, which uses the roots source set with `Manager.SetProjectRoots`. A refused create stores nothing. Clearing a task override (empty value) is always allowed. |
| Pipeline and quality-gate test runner | `pipeline.RunTests(command, projectDir, timeout, roots)` and `EvaluateGate(..., roots)`; the executor and the autonomous manager supply the live roots. |
| Git guard | `gitguard.SetProjectRoots` registers the daemon's roots (plus `session.default_project_dir`, which is configuration) and `RefuseNonProject` refuses an automatic commit or attempt snapshot in a project directory outside them. The refusal is a `NonProjectError` and is not remembered, so adding a root takes effect at the next commit. With no source registered (a library or test use) the check is off. |

Decisions inside the rule:

- `..` is resolved lexically (`filepath.Clean`) and the cleaned path is the one stored and used, so the checked path and the used path are the same string. Symlinks are then resolved on the deepest existing part, and a dangling link is followed to its missing target, so a link that would make `MkdirAll` create a directory outside a root is refused before anything is created.
- A root that does not exist yet is fine (the PRD worktree base is created on first use); a root that is itself a symlink works because both sides are resolved.
- A directory equal to a root is accepted.
- `session.default_project_dir` and the empty value are daemon configuration (the dismissal criterion in the task) and are not held to the roots. A request that names exactly the configured default is treated the same, because the REST layer fills it in.
- The returned and stored path is the cleaned lexical path, not the symlink-resolved one. A link swapped in between the check and the `MkdirAll` is a race this does not close; it needs write access inside an allowed root.
- Not changed: stored PRD and session records are checked when they are used (session start, gate run, commit), not rewritten. A stored directory outside the roots stops at the next use with the message that names the roots.

Operator note: an installation whose repositories live outside `~/.datawatch/prd-worktrees` must set `session.workspace_root` to their parent folder or list them in `server.allowed_project_roots`; see "Allowed project roots" in `docs/operations.md`.

## Tests added for the fixes

- `internal/gitguard/contain_test.go`: valid in-base paths (including a missing tail and a symlink that stays inside), traversal strings, absolute and NUL paths, symlink escape, dangling symlink escape (absolute and relative targets), dangling link that stays inside, symlink loop, symlinked base, missing base.
- `internal/gitguard/policy_test.go`: `TestCleanTempPacksRefusesSymlinkEscape` with a `t.TempDir` target outside the repository.
- `internal/gitguard/contain_test.go`: `TestCleanProjectDir` (cleaning, empty, root, NUL, relative made absolute).
- `internal/gitguard/roots_test.go`: `ResolveProjectDir` for a path inside a root, a missing tail, the root itself, traversal strings, a sibling that shares a name prefix, symlink escape, dangling-link escape (and a dangling link that stays inside), symlink loop, a path inside a configured extra root, a path outside every root (the error names the roots), a symlinked root, empty roots never fail open, unusable values; `RefuseNonProject` with registered roots (inside, outside, extra root added later, traversal, symlink, dangling link).
- `internal/pipeline/quality_test.go`: `TestRunTestsRefusesUnsafeProjectDir`, `TestRunTestsEnforcesProjectRoots` (inside, extra root, outside, no roots, traversal, symlink escape, dangling link).
- `internal/session/project_dir_test.go`: valid relative directory, missing root created, traversal strings, symlink escape.
- `internal/session/project_roots_start_test.go`: `Start` inside the worktree base, workspace root and an extra root, relative path under the workspace root, outside every root (nothing created), the error names the roots, traversal, symlink escape, dangling-link escape (nothing created), nothing configured, configured default project directory.
- `internal/autonomous/project_roots_test.go`: `CreatePRD`, `SetTaskProjectDir` and `InstantiateFromTemplateStore` through the API (inside, extra root, outside, traversal, symlink, dangling link, filesystem root, no roots, nothing stored on refusal) and the quality-gate baseline run outside the roots.
- `internal/config/allowed_project_roots_test.go` and `internal/server/allowed_project_roots_surfaces_test.go`: the root list and its validation, and the setting on YAML, REST GET/PUT (list and comma string), the Settings card, the five locale bundles and the docs.
- Existing tests that start sessions or run gates in `t.TempDir` directories configure the system temp directory as their root (`allowTestTemp`, `newTestAPI`, `allowTempProjectRoots`, `testRoots`).

## Certificate-check and JS sanitization alerts

Scope: open code-scanning alerts for disabled certificate validation (Go and JS) and the JS sanitization rules that fire in test files. Alert numbers and lines are as reported at triage time.

### Per-node TLS option

Nodes carry an explicit `tls_insecure_skip_verify` field (`compute.Node`, mirrored in the config node struct and mapped onto the node by `computeNodeFromConfig` in `cmd/datawatch/main.go`, documented in `docs/implementation.md`). The zero value verifies certificates, so existing configs keep working for any node with a valid certificate; a node with a self-signed certificate needs the one-line opt-out. Tests: `cmd/datawatch/compute_node_config_test.go` (config key reaches the node), `internal/server/node_usage_tls_test.go` (httptest TLS server with a self-signed cert: the probe fails by default and succeeds with the opt-out).

Other node probes still skip verification and were left alone (outside this task): `internal/server/compute.go` (monitoring stub fetch, models list) and `internal/compute/probe.go` (`probeHTTP`). They can read the same field when the operator wants them tightened.

### Decisions

| Alert | File | Decision | Reason |
|-------|------|----------|--------|
| go disabled certificate check | internal/server/node_usage.go (newNodeUsageClient) | fix | Verification is on by default; `Node.TLSInsecureSkipVerify` is an explicit per-node opt-out, used for both the `/api/ps` probe and the monitoring-endpoint GPU fetch. |
| 697 | scripts/test-stories/pwa/TS-821.mjs:14 | fix | Removed the process-wide `NODE_TLS_REJECT_UNAUTHORIZED=0`. API calls use `https.request` through a dedicated `https.Agent({ rejectUnauthorized: false })` that only talks to the sandbox TLS origin. |
| 698 | scripts/test-stories/pwa/TS-824.mjs:29 | fix | Same change as 697. |
| 699 | scripts/test-stories/pwa/TS-822.mjs:39 | dismiss | Test-only, local-only listener. The relaxed setting is already scoped to one request to the sandbox host (`rejectUnauthorized: false` on `https.request`); there is no process-wide setting. |
| 664, 665 | scripts/test-stories/pwa/TS-797.mjs:329, 350 | dismiss | Test-only. The proxy dials the sandbox daemon's own self-signed listener with a per-connection option; nothing process-wide. |
| 694 | internal/server/web/app-shared-nav.test.js:32 | dismiss | The test strips HTML comments from the repo's own `index.html` to find nav buttons. It is a parser over a trusted checked-in file; the output is never rendered. |
| 685 | internal/server/web/app-automation-empty.test.js:62 | dismiss | Unescapes an attribute value generated by the app under test so the test can run the handler in a VM sandbox. Test input only, never served. |
| 671, 672, 673, 674 | internal/server/web/app-result-panel.test.js:149, 157 | dismiss | The DOMPurify and marked stand-ins are deliberately crude stubs that prove the app calls the sanitizer; they never process untrusted input and are not shipped. |
| 630, 631 | internal/server/web/app-fed-conn-status.test.js:63, 92 | dismiss | `JSON.stringify` of a literal fixture built in the test is interpolated into a script run in a VM sandbox. No external input. |

## Dismissals for the operator

Sixteen alerts are decided as dismiss and each has an entry in `security/accepted-risks.yml` (`code-scanning/<alert number>`) with its own reason. The operator applies the dismissals; none was called from this task. Use `false positive` for the five source-code alerts and `used in tests` for the eleven test-only script and test-file alerts.

```bash
# false positives in product code
for n in 689 688 687 690 675; do
  gh api -X PATCH repos/dmz006/datawatch/code-scanning/alerts/$n \
    -f state=dismissed -f dismissed_reason="false positive" \
    -f dismissed_comment="Traced and recorded in security/accepted-risks.yml (code-scanning/$n)."
done
# test-only scripts and test files
for n in 699 664 665 694 685 671 672 673 674 630 631; do
  gh api -X PATCH repos/dmz006/datawatch/code-scanning/alerts/$n \
    -f state=dismissed -f dismissed_reason="used in tests" \
    -f dismissed_comment="Test-only; traced and recorded in security/accepted-risks.yml (code-scanning/$n)."
done
```

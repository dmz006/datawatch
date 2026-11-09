# Workflow upstream breaking changes (dependabot action bumps)

Read-only review of the five open `dependabot/github_actions/*` branches against this branch. No tracked file was changed. The `gh` command was not run.

## Actions and target pins

| Action | Dependabot branch | Current pin (this branch) | New pin and comment line | Files |
|---|---|---|---|---|
| actions/upload-artifact | `actions/upload-artifact-7.0.2` | `330a01c4…  # v5` | `cf430e030ddbb5b0abf93d22962f4752f3646cd9  # v7.0.2` | ci.yaml, matrix-integration.yaml, owasp-zap.yaml, release.yaml (4 uses) |
| docker/setup-qemu-action | `docker/setup-qemu-action-4.4.0` | `c7c53464…  # v3` | `99012661954931238ded8c8b007157a8430204e1  # v4.4.0` | containers.yaml (2), image-refresh.yaml (2), release.yaml (2) |
| golangci/golangci-lint-action | `golangci/golangci-lint-action-9` | `@v8` (floating tag) | `@v9` (floating tag, no SHA, no comment) | ci.yaml, release.yaml |
| zaproxy/action-baseline | `zaproxy/action-baseline-0.15.0` | `41aee98ebc7cf2802c3beae4e7d4336413a21e43  # v0.9.0 (newest tag that still resolves; v0.15.0 pin was dangling)` | `de8ad967d3548d44ef623df22cf95c3b0baf8b25` with the **unchanged, now stale** comment `# v0.9.0 (newest tag that still resolves; v0.15.0 pin was dangling)` | owasp-zap.yaml (2 uses) |
| zaproxy/action-full-scan | `zaproxy/action-full-scan-0.13.0` | `bdd21ad4090d552d299261226c553efbc216f5ee  # v0.9.0 (newest tag that still resolves; v0.10.0 pin was dangling)` | `3c58388149901b9a03b7718852c5ba889646c27c` with the **unchanged, now stale** comment `# v0.9.0 (newest tag that still resolves; v0.10.0 pin was dangling)` | owasp-zap.yaml (1 use) |

Notes:
- The two ZAP branches change the SHA but leave the version comment saying v0.9.0. Merged as-is, the comments would be wrong. Fix the comments when merging.
- `zaproxy/action-api-scan` (two uses, pinned to v0.9.0 at `77dfa9a6…`) has no open dependabot branch. It stays on v0.9.0, while baseline and full-scan move to v0.15.0 and v0.13.0.
- The golangci-lint action is the only one that is not SHA-pinned. v9 keeps that floating-tag style.

Dangling-tag check (`git ls-remote --tags`):
- `zaproxy/action-baseline` `v0.15.0` **resolves**. The tag object is `6c5a0075…` and the peeled commit is `de8ad967d3548d44ef623df22cf95c3b0baf8b25`, which is exactly the new dependabot pin. Tags v0.8.2 to v0.15.0 all exist. The old comment "v0.15.0 pin was dangling" no longer holds today. The earlier pin was probably a SHA that did not match the tag's peeled commit (the tag is annotated, so the tag object SHA differs from the commit SHA).
- `zaproxy/action-full-scan` `v0.13.0` **resolves** (tag `bee4e5be…`, peeled commit `3c58388149901b9a03b7718852c5ba889646c27c`, which is the new pin). v0.10.0 also resolves (peeled `d2a07475…`). The "v0.10.0 pin was dangling" comment is also stale.
- The new pins are therefore commit SHAs that sit on real release tags. The dependabot pins are not dangling.
- Caveat: I checked that the tags resolve. I did not check that these SHAs are reachable from the default branch. A runner fetch by SHA works for any commit in the repo.

## Breaking changes that apply

Release notes were read from each project's GitHub releases page. None labels a v-to-v change as breaking beyond the Node runtime. Findings per action:

**actions/upload-artifact v5 → v7.0.2**
- Node 24 runtime (v5/v6). v6 requires Actions Runner >= 2.327.1. This applies only to self-hosted runners. All workflows here should be on GitHub-hosted runners; confirm no `runs-on: self-hosted`.
- v7 adds an `archive` input (`false` skips zipping). With `archive: false`, `name` is ignored and a glob that matches multiple files fails. No workflow here sets `archive`, so this does not apply.
- v7 moved to ESM. This is not labelled breaking and has no effect on workflow YAML.
- v7.0.1 only updates docs and dependencies. v7.0.2 improves download retries on HTTP 429.
- Name uniqueness and `overwrite`: the release notes say nothing new for v5–v7. The v4 rule still holds, so two uploads of the same artifact name in one run fail. Local check: the release.yaml `trivy-${{ matrix.name }}-sarif` uploads run in two matrices (`agent-base`, `validator`, `stats-cluster` and `agent-claude`, `agent-opencode`, `agent-aider`, `agent-gemini`, `agent-goose`, `parent-full`). The names do not overlap, so there is no collision. Other names (`datawatch-build-${{ github.run_number }}`, `matrix-daemon-log`, `govulncheck-results`, `gosec-results`, `owasp-zap-reports`) are unique per run. No workflow sets `overwrite`.
- `path` and `if-no-files-found`: no release-note changes. `matrix-integration.yaml` uses `if-no-files-found: ignore` with a glob path, and the other uploads use the default (warn). Behaviour is unchanged.

**golangci/golangci-lint-action v8 → v9**
- Only documented change: runtime moves from node20 to node24 (v9.0.0). It needs a current runner; this applies to self-hosted runners only.
- v9.1.0 adds "automatic module directories". v9.2.0 adds a `version-file` option. Both are additions.
- `version` input: this repo sets `version: v2.12.2`. It satisfies the v8 requirement of golangci-lint >= v2.1.0. No v9 change to `version` handling is documented.
- `args: --timeout=5m` and `skip-cache: true`: the release notes mention no change. The config file `.golangci.yml` is `version: "2"`, which matches what the action supports.
- The release notes are thin. If this matters, diff `action.yml` between v8 and v9 before merging.

**docker/setup-qemu-action v3 → v4.4.0**
- v4.0.0: Node 24 runtime (runner >= 2.327.1, self-hosted only) and ESM. v4.1.0 adds a `reset` input. v4.2.0 to v4.4.0 are dependency and internal changes.
- The `platforms` input is not mentioned in any release note. No workflow here passes `with:` to setup-qemu (`uses: docker/setup-qemu-action@…` with no inputs), so the default platforms apply and nothing breaks.

**zaproxy/action-baseline v0.9.0 → v0.15.0**
- v0.10.0 adds authentication environment variables. v0.11.0 runs on node20. v0.13.0 adds rate limiting when calling the GitHub API and allows writing any file from the Docker container. v0.14.0 stops using the deprecated `upload-artifact` version. v0.15.0 runs on node24.
- Nothing is listed for `target`, `cmd_options`, `rules_file_name`, `fail_action`, `allow_issue_writing`, `issue_title` or `artifact_name`. The workflow uses all of them. No documented break.
- Failure behaviour (`fail_action`) has no documented change.
- v0.14.0 changes the upload-artifact version inside the action. Local `artifact_name` values (`zap-pwa-baseline`, `zap-diagrams-baseline`) are unique, so uniqueness is satisfied.
- v0.13.0 rate limiting could slow or fail issue writing if the GitHub API limit is hit. Unlikely to matter here.

**zaproxy/action-full-scan v0.9.0 → v0.13.0**
- Notes cover dependency updates, rate limiting, a Docker file-write fix, the upload-artifact update and Node 24. No change to `target`, `cmd_options`, `fail_action` or `artifact_name` is documented. The workflow step is `continue-on-error: true` and `fail_action: false`, so failures stay advisory.

Overall: no documented breaking input or behaviour change affects the workflows in this repo. The common risk is Node 24, which only matters on self-hosted runners. Open items to settle before merging:
1. Update the stale v0.9.0 comments on the two ZAP pins.
2. Decide whether `zaproxy/action-api-scan` should move with them, since it stays on v0.9.0.
3. Release notes for golangci-lint v9 are sparse. A diff of `action.yml` would confirm there is no input change.

## Sources

- `git ls-remote --heads origin 'dependabot/github_actions/*'` on this repository, plus `git fetch origin <branch>` and `git diff HEAD FETCH_HEAD -- .github/workflows` for each of the five branches (the diff shows the new pin and comment lines).
- `git ls-remote --tags` against https://github.com/zaproxy/action-baseline.git and https://github.com/zaproxy/action-full-scan.git.
- https://github.com/actions/upload-artifact/releases
- https://github.com/golangci/golangci-lint-action/releases
- https://github.com/docker/setup-qemu-action/releases
- https://github.com/zaproxy/action-baseline/releases
- https://github.com/zaproxy/action-full-scan/releases
- Local files: `.github/workflows/{ci,release,matrix-integration,owasp-zap,containers,image-refresh}.yaml`, `.golangci.yml`.
- Limitation: the releases pages were summarised by the fetch tool, which said several of them were partial views. Treat "not mentioned" as "not seen", not as "confirmed absent".

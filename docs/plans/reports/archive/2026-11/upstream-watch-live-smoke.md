# upstream-watch — live smoke run report

- **Date:** 2026-11-02 03:56 UTC (report timestamp `2026-11-02T03:56:44Z` from the JSON)
- **Command (from the project directory, against the real network, unauthenticated):**
  ```
  python3 scripts/upstream_watch.py --report-md .upstream-watch-smoke/report.md --report-json .upstream-watch-smoke/report.json
  ```
- **Result headline:** 83 finding(s) — 46 advisories, 37 stale pins — across 37 checked pins; 10 unmapped; 2 not-checked feed failures.

## Spec checks

| # | Check | Result | Evidence |
|---|-------|--------|----------|
| 1 | Exit status 0 | **PASS** | `python3 scripts/upstream_watch.py …` exited `0` (captured shell status in the same shell line); script self-reported "83 finding(s); reports written to .upstream-watch-smoke/report.md and .upstream-watch-smoke/report.json". No `--offline-fixtures` fallback was needed — the live network path worked. |
| 2 | report.md starts with `<!-- upstream-watch -->` | **PASS** | `head -1 .upstream-watch-smoke/report.md` printed exactly `<!-- upstream-watch -->` as line 1. |
| 3 | report.json parses and has `findings_count`, `findings`, `standing`, `unmapped`, `not_checked` | **PASS** | `json.load` succeeded; key set is `marker, last_checked, findings_count (int 83), findings (list), standing (list), unmapped (list of 10), not_checked (list of 2)` plus `checked_pins` (int 37) and `report_sha256_prefix`. |
| 4 | Standing notes list the Trivy installer fetched from `main` in the three workflow files | **PASS** | Standing note reads: "Trivy installer fetched from the moving `main` branch in 3 workflow file(s), 8 line(s) total: `.github/workflows/containers.yaml` (2 lines, L93-170), `.github/workflows/image-refresh.yaml` (4 lines, L86-563), `.github/workflows/release.yaml` (2 lines, L280-374). Monitoring only — not edited by this script." Independently re-grepped: the `raw.githubusercontent.com/aquasecurity/trivy/main/contrib/install.sh` lines are exactly at containers.yaml L93/L170, image-refresh.yaml L86/L164/L235/L563, release.yaml L280/L374 — 3 files, 8 lines, matching. |
| 5 | Every version ARG in `docker/dockerfiles` is checked, under Unmapped pins, or under Not checked (counted against `grep -h '^ARG .*_VERSION=' docker/dockerfiles/Dockerfile.*`) | **PASS** | grep yields **43 unique version ARGs**, which account for exactly. Breakdown: 33 ARGs appear in Findings (Advisories and/or Stale pins, i.e. checked with a finding); 9 ARGs appear under **Unmapped pins** (`GH_XNET_VERSION`, `GOPLS_VERSION`, `GOVULNCHECK_VERSION`, `JDK_VERSION`, `ANDROID_CMDLINE_TOOLS_VERSION`, `GRADLE_VERSION`, `RUBY_PKG_VERSION`, `RUST_VERSION`, `TERRAFORM_VERSION`); `CARGO_AUDIT_VERSION`=0.21.0 is the pin behind both **Not checked** entries (OSV query 400 for cargo-audit 0.21.0; crates.io feed with no stable version in response) — listed by package name rather than ARG name, which is the one notational wrinkle; `GH_GO_VERSION`=1.27.3 is checked (mapped in the script alongside `GO_VERSION` to the go.dev feed) and is up to date — the same run reports the Go upstream latest as `go1.27.3`, so it is correctly absent from findings. The remaining unmapped entry `NODE_MAJOR`=22 is not a `_VERSION` ARG and is outside the grep set. Nothing unaccounted for. |
| 6 | The run itself modified no tracked file (`git status --porcelain` shows nothing new apart from the scratch directory, before writing the report) | **PASS** | `git status --porcelain` after the run and inspection showed exactly one line: `?? .upstream-watch-smoke/` (untracked scratch). No tracked file was modified, added, or deleted by the run. The scratch directory was deleted afterwards. |

All six checks **pass**; no `FAIL` lines and no offline-fixture fallback was required.

## What the run found (live, 2026-11-02 03:56 UTC)

- **83 findings** total: **46 advisories** (OSV, advisory-only for pins that may still date even where it is stated that there is no newer version) and **37 stale pins** (a newer upstream release exists).
- **Most advisory-heavy pins:** pnpm 10.33.0 (18 advisories), Go stdlib 1.26.6 (15 advisories), @anthropic-ai/claude-code 2.1.114 (4), aider-chat 0.86.0 (4, including duplicate OSV+PYSEC for the same issue), poetry 1.8.5 (4).
- **Stale pins by severity (from the report):**
  - *patch (auto-eligible):* claude-code 2.1.114→2.1.316, pyright 1.1.404→1.1.414, aider-chat 0.86.0→0.86.2, bundler-audit 0.9.2→0.9.3, ruby-lsp 0.26.9→0.26.11, signal-cli 0.14.2→v0.14.9.
  - *minor (auto-eligible):* bandit, gemini-cli, gh 2.102.0→v2.103.3 (with the `GH_COMMIT`-linked edit), kotlin, rubocop, ruff, bun, delve, golangci-lint, goose, kubectl, opencode, pip-audit, tsx, uv, yq.
  - *minor, human-only:* Go toolchain 1.26.10/1.26.6→go1.27.3 in 5 Dockerfiles (with the toolchain-floor script linked edit), Trivy v0.73.0→v0.76.1 in `.github/workflows/image-refresh.yaml`.
  - *major, human-only:* brakeman, bundler, eslint, poetry, typescript, eslint-plugin-security, helm, pnpm, rtk.
- **Standing note (monitoring only, not edited by the script):** the Trivy installer is pulled from the moving `main` branch in 3 workflow files (8 lines; see check 4).
- **Unmapped pins (10):** no in-scope upstream feed — each line has a "reason for that" (Go-module advisory-only, apt package, image variant tag, no JSON feed, etc.); never silent.
- **Not checked (2 entries, 1 pin):** `CARGO_AUDIT_VERSION`=0.21.0 — OSV query HTTP 400 and no stable entry in the crates.io release feed.
- **Report self-description / integrity:** `report.md` carries the `<!-- upstream-watch -->` marker; `report.json` additionally carries `checked_pins` (int), `report_sha256_prefix`, and `last_checked` for machine consumers.

## Environment / process notes

- Network was available; no `--offline-fixtures` fallback was needed.
- `GITHUB_TOKEN`/`GH_TOKEN` were not required; unauthenticated feed calls were sufficient for this one-shot run (no rate-limit errors observed).
- The scratch directory `.upstream-watch-smoke` (report.md, report.json, one captured stdout log) was **deleted** after the checks; it is the only non-tracked artifact the run produced and it did not survive.
- This report (`docs/plans/reports/archive/2026-11/upstream-watch-live-smoke.md`) is the only tracked file written by the task.

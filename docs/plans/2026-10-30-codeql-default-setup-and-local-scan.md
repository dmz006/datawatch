# CodeQL default setup and the local scan (2026-10-30)

**Date:** 2026-10-30
**Scope:** whether the analysis GitHub default setup runs for this repository (languages `actions`, `go`, `javascript-typescript`, `python`; default code-scanning suite) can be reproduced on a developer machine with the CodeQL command line and without uploading anything. Measurement only: no source, workflow or config file is changed by this document. Prior art, re-measured here rather than copied: [2026-10-27-codeql-local-reproduction.md](2026-10-27-codeql-local-reproduction.md) (go and javascript-typescript, CLI 2.27.3).
**Status:** measured; the answer is recorded in `## Local run feasibility`. The working files (bundle, databases, SARIF, scripts) live under `.datawatch/codeql/`, which is gitignored (`.gitignore` line 1), so none of them are committed.
**Version:** the fix that follows from this ships as the next patch above `main`; no number is assigned here.

## Decision

Operator, 2026-10-30, quoted: "Keep gh, remove the local,  unless we can align them and test locally and find issues before ci failure".

GitHub default setup stays the analysis of record. A local run is kept only if it can be aligned with default setup and can find issues before the CI alert gate fails. The measurements below answer the "can be aligned" half; the "find issues before ci failure" half is covered in the match comparison and the caveats.

## Local run feasibility

**Local run feasible:** yes

All four languages ran locally with the default code-scanning suite, nothing was uploaded, and every open alert on `main` was reproduced at the same rule, file and line. Per-language result counts and rule counts equal the numbers GitHub recorded for its own analysis of `main`.

Method. Machine: 48 cores, 122 GB RAM (of which about 68 GB available during the run), local NVMe disk. CodeQL CLI/bundle 2.27.3, which is the version default setup used (read from the latest `main` analysis with `gh api 'repos/dmz006/datawatch/code-scanning/analyses?ref=refs/heads/main&per_page=1' --jq '.[0].tool.version'`). Every `codeql` command ran under `env -u TMUX -u TMUX_TMPDIR`. No upload command or option was used and nothing was posted to the code-scanning API; the only GitHub calls were read-only `GET`s of analyses and alerts.

```bash
gh release download codeql-bundle-v2.27.3 -R github/codeql-action -p 'codeql-bundle-linux64.tar.*' -D .datawatch/codeql/dl
tar -xzf .datawatch/codeql/dl/codeql-bundle-linux64.tar.gz -C .datawatch/codeql/bundle
codeql database create .datawatch/codeql/db/<lang> --language=<lang> --build-mode=<autobuild|none> --source-root=. --overwrite
codeql database analyze .datawatch/codeql/db/<lang> <pack>:codeql-suites/<lang>-code-scanning.qls \
  --format=sarifv2.1.0 --output=.datawatch/codeql/out/<lang>.sarif
```

Packs (names checked in the bundle's `qlpacks/codeql/`): `codeql/go-queries` (suite `go-code-scanning.qls`, build mode `autobuild`), `codeql/javascript-queries` (language `javascript-typescript`, suite `javascript-code-scanning.qls`, `none`), `codeql/python-queries` (`python-code-scanning.qls`, `none`), `codeql/actions-queries` (`actions-code-scanning.qls`, `none`). The bundle contains the query packs, so the analysis itself needed no network.

### Install size

| Item | Size | Time |
|------|------|------|
| Download, `codeql-bundle-linux64.tar.gz` | 664 MB | 15 s for both archives below |
| Download, `codeql-bundle-linux64.tar.zst` (also fetched by the `-p` glob; not needed) | 533 MB | included above |
| `.datawatch/codeql/dl/` as downloaded (both archives and checksums) | 1.2 GB | |
| Extracted bundle, `.datawatch/codeql/bundle/` | 1.4 GB | 3 s (gz, SHA-256 matched the published checksum) |

A single archive plus the extracted bundle is about 2.1 GB (gz) or 1.9 GB (zst); the archive can be deleted after extraction, leaving 1.4 GB.

### Runtime per language

Two runs on the same machine, one language after another: (1) this branch's tree, run with the bundle inside the source root; (2) an exact export of the commit default setup last analyzed (`git archive`), so the comparison is line for line. The `--threads=0` option (all cores) was used for analysis.

| Language | Build mode | Create, branch tree | Analyze, branch tree | Create, analyzed commit | Analyze, analyzed commit |
|----------|-----------|--------------------:|---------------------:|------------------------:|-------------------------:|
| go | autobuild | 63 s | 16 s | 63 s | 16 s |
| javascript-typescript | none | 18 s | 13 s | 17 s | 14 s |
| python | none | 41 s | 9 s | 4 s | 5 s |
| actions | none | 1 s | 5 s | 2 s | 5 s |
| **Total** | | **123 s** | **43 s** | **86 s** | **40 s** |

The four languages took 166 s wall clock on the branch tree (19:05:06 to 19:07:52 local) and 126 s on the analyzed-commit export. The Go build used a warm Go build cache; a cold cache was not measured. The Python create time on the branch tree (41 s against 4 s) is an artifact of where the bundle was installed: the Python extractor walked the Python files inside the in-tree `.datawatch/codeql/bundle/` (67 log lines name it) and its database grew to 140 MB against 34 MB. Results were not affected (0 either way), but a developer script should install the bundle outside the source root, or the Python step pays that cost.

### Database size

| Language | Branch tree | Analyzed commit |
|----------|------------:|----------------:|
| go | 329 MB | 332 MB |
| javascript-typescript | 210 MB | 212 MB |
| python | 140 MB (inflated by the in-tree bundle) | 34 MB |
| actions | 17 MB | 16 MB |
| Four databases | 696 MB | 594 MB |
| SARIF output (four files) | 9.5 MB | |

Peak extra disk use during the branch-tree run, sampled with `df` every 5 seconds against the level just before it started: 1.3 GB at the peak, 0.73 GB once finished. The disk is shared with other processes, so the peak is an upper bound. The whole `.datawatch/codeql/` directory after both runs, including the downloads, the bundle, both sets of databases and the exported source (102 MB), is 3.3 GB.

### Result counts

Local results against the numbers GitHub recorded for its own analyses of `main` at the same CLI version. The rule counts are the rules in the SARIF run against the `rules_count` GitHub reports.

| Language | Local results (branch tree) | Local results (analyzed commit) | GitHub results | Local rules | GitHub rules |
|----------|---:|---:|---:|---:|---:|
| go | 69 | 69 | 69 | 34 | 34 |
| javascript-typescript | 23 | 23 | 23 | 87 | 87 |
| python | 0 | 0 | 0 | 43 | 43 |
| actions | 0 | 0 | 0 | 17 | 17 |
| **Total** | **92** | **92** | **92** | | |

The 92 results sit on 89 distinct rule, file and line keys: three results share a key with another result (same rule and line, different dataflow).

### Match comparison

Tree check. The analysis default setup last ran on `main` is for commit `2aa267b60320a97ed2394b638de68069255e7217`. This branch's `HEAD` is `97c6bb03e2d67536fbff855aee6ed3cfa28d9f57`, which is not a descendant of it: the branch forked at `aff96b9db` (4 commits ahead, 56 commits behind that analyzed commit). The two trees differ in 101 files: 3 files changed on the branch (the CodeQL workflow, the supervisor watch script and a smoke-result note), and the rest are changes on `main` that the branch does not have, among them `cmd/datawatch/main.go`, `internal/server/api.go` and `internal/server/web/app.js`. The five files holding the five open alerts are byte-identical in both trees. Because lines shifted in the three files above, the match was run twice: on the branch tree, and on the exact analyzed commit.

Alerts fetched for `refs/heads/main` in every state: 5 open, 106 dismissed, 82 fixed. Each SARIF result and each alert was reduced with `python3 -I` to (rule id, path, start line); an alert's key is its most recent instance on `main`.

| Check | Branch tree | Analyzed commit |
|-------|------------:|----------------:|
| (a) open alerts reproduced at the same rule, file and line | 5 of 5 | 5 of 5 |
| (a) open alerts not reproduced | 0 | 0 |
| (b) local results that are not open alerts | 87 | 87 |
| (b) of those, match a dismissed alert | 81 | 87 |
| (b) of those, match a fixed alert | 0 | 0 |
| (c) local results matching no alert | 6 | 0 |
| (d) results with security-severity 7.0 or more (high) | 90 | 90 |
| (d) results with security-severity 9.0 or more (critical) | 9 | 9 |

(a) The five open alerts, all `high`, all reproduced: `go/disabled-certificate-check` in `internal/autonomous/supervisor_watch_host.go` line 465, `js/double-escaping` in `internal/server/web/app-questions-history.test.js` line 211, and `js/disabling-certificate-validation` in `scripts/test-stories/pwa/TS-833.mjs` line 23, `TS-834.mjs` line 29 and `TS-835.mjs` line 49.

(c) On the branch tree, the six results that match no alert are all in files that differ from the analyzed commit, which is why a line moved: `go/disabled-certificate-check` at `cmd/datawatch/main.go` lines 12584, 13041 and 14151, `go/clear-text-logging` at `cmd/datawatch/main.go` line 11165, `go/request-forgery` at `internal/server/api.go` line 10208, and `js/xss-through-dom` at `internal/server/web/app.js` line 10836. On the analyzed commit the list is empty: every non-open result is a dismissed alert at the same rule, file and line.

(d) The nine critical results are `go/command-injection` (4, score 9.8), `go/email-injection` (1, 9.8) and `go/request-forgery` (4, 9.1). The CI alert gate counts open alerts of level high or critical for the ref, which is 5 high and 0 critical today. A local run on its own reports 90 results at 7.0 or more because 85 of them are dismissed on GitHub; reproducing the gate's number needs those dismissals subtracted.

Reverse direction: 87 of the 106 dismissed alerts are reproduced locally on the analyzed commit (81 on the branch tree), and 0 of the 82 fixed alerts, as expected. The other 19 dismissed alerts are not produced by the current code; GitHub keeps a dismissed alert dismissed after the code stops matching it, so a local run cannot confirm them.

### Caveats

- A local run cannot know GitHub dismissals. Dismissals live only in the code-scanning alerts API, so the local result list has to be reduced against `gh api --paginate 'repos/dmz006/datawatch/code-scanning/alerts?ref=refs/heads/main&state=dismissed&per_page=100'` (a read-only call) before its high and critical counts mean the same thing as the gate. Without it, a clean tree reports 85 results that the gate does not count.
- It needs the network once to fetch the 664 MB bundle (and `gh` credentials for `gh release download` and the alert reads). After that the database and analyze steps ran without any download.
- Go autobuild needs the Go toolchain `go.mod` asks for. `go.mod` declares `go 1.26.10`; the machine had go 1.27.2, which satisfies it. A machine with an older toolchain would have to fetch a newer one, and the autobuild time above assumes a warm build cache.
- Pin the bundle to the version default setup used, and check for drift before trusting a comparison: compare `codeql version` with `gh api 'repos/dmz006/datawatch/code-scanning/analyses?ref=refs/heads/main&per_page=1' --jq '.[0].tool.version'`. Both were 2.27.3 here, the latest bundle release at the time (`codeql-bundle-v2.27.3`, 2026-10-20). Default setup upgrades on GitHub's schedule, not this repository's, so a newer version there means different queries and the local numbers stop being comparable until the bundle is re-pinned.
- Compare on the same tree. The branch is behind `main`; the six unmatched results above come entirely from that. A usable script scans the tree it is asked about and compares by rule, file and line only against an analysis of the same commit, or falls back to matching by rule and file when the commit differs.
- Install the bundle outside the source root (or exclude it) so the Python extractor does not walk it.
- Python and actions produce 0 results, as they do on GitHub, so a zero from those two is expected, not a sign the step failed. The run logs and the database-created line, not the exit status alone, confirmed each step finished.
- The CLI messages "scanned N out of M files" refer to one language per invocation and are not a coverage gap.

## Parity surface

This is a measurement plus a developer-side script and a workflow decision. No product surface is touched.

| Surface | Status | Reason |
|---------|--------|--------|
| REST | not touched | No endpoint is added or changed; the only API used is GitHub's read-only code-scanning API from a developer machine. |
| MCP | not touched | No tool is added or changed. |
| CLI | not touched | The `datawatch` binary gains no command; the CodeQL command line is a separate developer tool. |
| Comm channel | not touched | No message command or notification is added. |
| YAML / config | not touched | No config key is added; the local scan is configured by command-line options and the gitignored `.datawatch/codeql/` directory. |
| PWA | not touched | The operator asked for no PWA change. |
| Android | not touched | No client-visible behaviour changes, so there is nothing to mirror. |
| iPhone | not touched | No client-visible behaviour changes, so there is nothing to mirror. |

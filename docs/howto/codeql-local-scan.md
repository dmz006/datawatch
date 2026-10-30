# CodeQL local scan (find alerts before CI does)

GitHub runs CodeQL on every push through default setup and a CodeQL workflow
gates on the result. The same analysis can run on a developer machine, so a
finding shows up before the push instead of as a failed check afterwards.
Nothing is uploaded: the scan reads the working tree and prints a report.

## What it is and why

- **CI side.** GitHub default setup analyzes every push in four languages:
  `go`, `javascript-typescript`, `python` and `actions`, using the default
  code-scanning suite. The CodeQL workflow's alert gate then fails when the
  repository has open alerts of level high or critical.
- **Local side.** `scripts/codeql-local.sh` runs the same four languages with the
  same suite and the same CodeQL bundle version, writes SARIF files under
  `.datawatch/codeql/out/` (gitignored) and prints a summary. It never uploads
  results or posts to the code-scanning API.

A finding that appears locally is the finding the gate would count, once the
alerts already dismissed on GitHub are subtracted (see the `--compare` step).

## Prerequisites

- **Go toolchain** at or above the version `go.mod` declares. The `go` language
  is built with autobuild, so the toolchain has to be on `PATH`. A warm Go build
  cache keeps the build short.
- **`gh`, authenticated** (`gh auth status`). Needed to download the bundle with
  `--install` and for `--compare` and `--check-version`. Without it the scan
  still runs, but it cannot tell which findings are already dismissed.
- **Network, once**, to fetch the CodeQL bundle. After that the scan runs
  offline because the bundle holds the query packs.
- **Disk and time.** The bundle is a 664 MB download and 1.4 GB once extracted.
  The four databases add about 600 to 700 MB and the SARIF output about 10 MB, so
  plan for about 3 GB under `.datawatch/codeql/` including the download. On a
  48-core machine with a warm Go cache a full run took about 2 to 3 minutes
  (Go about 80 s, JavaScript/TypeScript about 30 s, Python and actions under
  a minute each). A cold Go cache or a smaller machine takes longer.

## Steps

1. **Install the pinned bundle once.**

   ```bash
   bash scripts/codeql-local.sh --install
   ```

   This downloads the bundle version pinned in the script, checks its SHA-256 and
   extracts it into `.datawatch/codeql/bundle/`. The archive is deleted after
   extraction. Set `DATAWATCH_CODEQL_HOME` to keep the bundle and databases
   somewhere else.

2. **Run the scan.**

   ```bash
   make codeql-local
   ```

   This is `bash scripts/codeql-local.sh --compare`: all four languages, then a
   read-only comparison with the alerts GitHub holds for `main`.

3. **Scan only some languages.** Pass a space-separated list; the allowed names
   are `actions`, `go`, `javascript-typescript` and `python`.

   ```bash
   bash scripts/codeql-local.sh --compare --languages "go javascript-typescript"
   ```

4. **Read the output.** The report lists the number of results per rule and
   level, then every high or critical result with its rule, file, line and
   status. With `--compare` each result is marked one of:

   - `open on GitHub`: an open alert; the gate counts it.
   - `dismissed on GitHub`: someone dismissed this alert on GitHub (for example
     as a false positive or used in tests), so the gate does not count it.
   - `not on GitHub`: no alert matches by rule, file and line. This is usually a
     new finding in code GitHub has not analyzed yet, or a line that moved.

   The report ends with the number of results that are not dismissed on GitHub,
   which is the number the alert gate would see. Python and actions normally
   report zero results, the same as on GitHub, so a zero there is not a failed
   step.

5. **Use the exit code.**

   | Code | Meaning |
   |------|---------|
   | 0 | clean: no high or critical result that is not dismissed on GitHub |
   | 1 | high or critical results not dismissed on GitHub (or any high or critical result when the comparison was not usable); also `--check-version` drift |
   | 2 | usage error |
   | 3 | CodeQL CLI missing: run `--install` or put `codeql` on `PATH` |
   | 4 | a CodeQL database or analyze step failed |

6. **Check for pin drift.**

   ```bash
   bash scripts/codeql-local.sh --check-version
   ```

   It prints the pinned bundle version and the version default setup last used on
   `main`, and exits 1 when they differ or the GitHub value cannot be read. GitHub
   upgrades default setup on its own schedule. After a drift report, change the
   pinned version at the top of `scripts/codeql-local.sh`, re-run `--install` and
   treat comparisons made on the old version as out of date.

## Optional: run it before every push

A pre-push hook scans only the languages the pushed commits touch (`.go`, `.js`,
`.mjs`, `.ts`, `.py` and workflow files) and blocks the push on high or critical
findings that are not dismissed on GitHub. It is never installed automatically
and does nothing unless you opt in per push.

```bash
# install once
ln -sf ../../hooks/pre-push-codeql "$(git rev-parse --git-path hooks)/pre-push"

# push with the scan enabled
DATAWATCH_CODEQL_PRE_PUSH=1 git push

# bypass once, for example when the scan itself is the problem
git push --no-verify
```

Without `DATAWATCH_CODEQL_PRE_PUSH=1` the hook exits at once, so an installed hook
costs nothing on ordinary pushes.

## Optional: run it before shipping

Run `make codeql-local` before cutting a release, after `--check-version` shows no
drift. A clean exit means the gate on the release commit has nothing new to count.

## Limits

- A local run cannot know GitHub dismissals on its own. Dismissals live only in
  the code-scanning alerts API, so without `--compare` (and a working `gh`) every
  high or critical result counts, including ones already dismissed.
- Dismissed alerts that the current code no longer produces cannot be confirmed
  locally; GitHub keeps them dismissed.
- Results can differ if GitHub's bundle moves ahead of the pinned version. Run
  `--check-version` first when the numbers look wrong.
- The comparison is by rule, file and line against `main`. A branch that is
  behind or ahead of `main` can show `not on GitHub` for a finding whose line
  moved; compare on a tree that matches `main` to rule that out.
- A message such as "scanned N out of M files" refers to one language in one
  step and is not a coverage gap.

## See also

- [Testing against a running daemon](testing-against-a-running-daemon.md)
- [Container image refresh](container-image-refresh.md)

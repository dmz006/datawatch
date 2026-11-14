# gofmt format check (`make check-gofmt`)

The repository now has a dedicated gofmt audit: `scripts/check-gofmt.sh`,
reachable as the `check-gofmt` target in the `Makefile`, one of the local
checks that `make lint` runs in sequence. This is a reference for people who
touch the build; it has no daemon surface, no MCP steps and no setting — it is
a script and a Makefile target.

## What the check does

`scripts/check-gofmt.sh` lists the Go files under `cmd/` and `internal/` that
are not gofmt-formatted and reports each one:

- The listing comes from `gofmt -l cmd internal`.
- **Excluded from the report:** files whose first five lines contain a
  `Code generated` header (generator output is never re-formatted by hand),
  and any file under a `testdata/` or `vendor/` path.
- **Any remaining offender** is listed with its fix command
  (`gofmt -w <file>`) and the script exits `1`.
- **A clean tree** prints `OK: all Go files under cmd/ and internal/ are
  gofmt-formatted` and exits `0`.
- **No Go toolchain** (`gofmt` not on `PATH`) is a `FAIL` with a note to
  install the toolchain.

The script is read-only: it only reads files and runs `gofmt -l`. It never
writes or formats anything, even when it reports offenders.

## Running it

| How | Command |
|-----|---------|
| Standalone | `bash scripts/check-gofmt.sh` |
| Make target | `make check-gofmt` |
| As part of the lint battery | `make lint` — `check-gofmt` is one of the checks in the `lint` target (after `check-plans-links`, together with `check-tmux`, `check-test-daemons`, `check-dependabot-security-only`, `check-service-scope`, `check-go-pins`, `check-internal-refs`, `check-sarif-upload`, `check-no-identity-wipe`, `check-security-ignore-writes`, `check-leaks`, and `check-no-config-copies`, before the `golangci-lint run`) and part of the local pre-tag run on the build host |
| Listed for humans | `make help` prints the local pre-tag checks, including `make check-gofmt` |

`check-gofmt` is `.PHONY` in the `Makefile`, so it always re-runs.

## Fixing a report

For each reported file, apply the fix the report names — `gofmt -w <file>` —
or reformat the tree with the existing `fmt` target, `gofmt -w .`
(`make fmt`), then re-run `bash scripts/check-gofmt.sh` to confirm a clean
tree. CONTRIBUTING asks contributors to format with `gofmt -w .` before
committing, and records that the format check runs in `make lint` and in the
local pre-tag run, failing on any unformatted Go file.

## Where it does not run

- It is **not** a GitHub Actions step: no workflow under `.github/workflows/` (including `ci.yaml`) runs `scripts/check-gofmt.sh` today. Enforcement is the local `make lint` battery and the local pre-tag run on the build host. See "For the operator to review" below.
- It is not part of `make test`, and no test script in `tests/` covers it
  yet.
- It touches no daemon setting, no REST/MCP/CLI/push channel and no PWA
  surface.

## For the operator to review

- **The `go test` step is gone from CI.** The commit that landed the gofmt
  check also changed `.github/workflows/ci.yaml`, and the only diff there
  removes the `go test ./...` step from the `test + lint` job. The commit
  message says a "CI step" was added; no gofmt step was added to the workflow.
  Confirm whether removing the CI `go test` step is intended, and — given the
  commit message promises a CI step — whether `scripts/check-gofmt.sh`
  should be added to `.github/workflows/ci.yaml` as its own step (it needs
  only `gofmt` on the runner, so it composes with the existing
  `setup-go` step). Note the release path is not affected: `release.yaml`'s
  `go test + lint` job still runs the full `go test ./...` step; the removed
  test coverage is only the push/PR `test + lint` job in `ci.yaml`.
- **Coverage of the tree.** The check audits `cmd/` and `internal/` only.
  If the repository adds top-level Go files or new trees, extend the
  `gofmt -l cmd internal` listing in `scripts/check-gofmt.sh`.
- **Generated files.** The exclusion trusts a `Code generated` phrase in the
  first five lines. If a generator starts emitting formattable headers it
  should not be trusted, tighten the header match in the script.

## See also

- [operations.md](../operations.md#gofmt-format-check) — the guard rules and where the check runs.
- [datawatch-definitions.md](../datawatch-definitions.md) — the "gofmt format check" glossary entry.
- [CI Code Scanning upload](ci-code-scanning-upload.md) — the sibling reference for the `make lint` battery's SARIF guard.

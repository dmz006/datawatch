# Toolchain freshness check (build host)

`scripts/check-toolchain.sh` tells you whether the development tools on a
build host are behind the newest stable release: Go, `gosec`, `govulncheck`,
`gitleaks`, `golangci-lint`, `trivy` and `goreleaser`. It is a **development
tool for the machine that builds datawatch**. It is not a datawatch feature: the
daemon does not run it, it has no setting, and nothing in the web app, the API
or a chat channel shows its result. People and sessions that build or release
datawatch run it by hand.

The check only reads. It runs each tool's own version command, reads the newest
version from a public source over HTTPS, prints a table and exits. It installs
nothing, upgrades nothing and writes nothing outside a temporary folder it
removes on exit.

## Base requirements

- `bash`, plus `sed`, `awk`, `sort`, `head`, `tr` and `mktemp` (any Linux or
  macOS shell has them). `timeout` is used when present to cap each version
  command at 20 seconds.
- `curl` (preferred) or `wget`, and outbound HTTPS to `go.dev`,
  `api.github.com` and `proxy.golang.org`. Without either downloader every
  tool shows `UNREACHABLE`; the check does not fail.
- Optional: set `GITHUB_TOKEN` in the environment to lift GitHub's anonymous
  rate limit. It is sent only to `api.github.com`, never to another host.
- Run it from anywhere inside the repository. The tool list and `go.mod` are
  found relative to the script, not to your current folder.

## Run it

```bash
bash scripts/check-toolchain.sh
echo $?
```

Example output:

```text
TOOL            INSTALLED    NEWEST       STATUS
go              1.26.9       1.26.11      GO-BEHIND
gosec           2.22.1       2.22.1       CURRENT
govulncheck     1.9.0        1.9.0        CURRENT
gitleaks        8.30.1       8.30.2       PATCH-BEHIND
golangci-lint   2.14.0       2.15.0       BEHIND
trivy           -            -            MISSING
goreleaser      2.15.4       2.15.4       CURRENT
note: ...
```

The table goes to standard output. Notes about sources that could not be read
go to standard error, one `note:` line each.

### Statuses

| Status | Meaning |
|---|---|
| `CURRENT` | The installed version equals the newest, or is newer. |
| `PATCH-BEHIND` | A tool other than Go has the same major and minor version as the newest, with an older patch. Informational only. |
| `BEHIND` | A tool other than Go is a minor or a major version behind. |
| `GO-BEHIND` | Go is older than the newest stable release of the Go line that `go.mod` declares. |
| `MISSING` | The tool is not on `PATH`. Reported, never fatal. |
| `UNREACHABLE` | The newest version could not be read or parsed, or the installed version could not be parsed. A `note:` line says which. |

The Go line is read from the `go` directive in `go.mod` when the check runs
(for example `1.26.11` gives the line `1.26`), so the check follows the
repository instead of a number written in the script. The newest stable patch
of **that line** counts: a newer `1.27.x` release, or an unstable release
candidate of the line, is ignored. A Go release published without a patch
number (`go1.26`) is read as `1.26.0`.

### Exit code

| Exit code | When |
|---|---|
| `1` | Go is `GO-BEHIND`, or any other tool is `BEHIND`. |
| `0` | Every other outcome, including `PATCH-BEHIND`, `MISSING` and `UNREACHABLE`. |

A network error never makes the check fail. That is deliberate: an offline
build host should not look like an out-of-date one. The cost is that
`UNREACHABLE` is not a pass. Read the table, not only the exit code.

Note that `BEHIND` applies to every tool in the list, not only the security
scanners, and that for a tool whose version starts with `0.` (for example
`trivy`) one minor version behind already counts as `BEHIND`.

## What is checked: `toolchain.yaml`

The tool list is `toolchain.yaml` at the repository root. It is read with plain
shell, so it uses a small fixed shape instead of general YAML:

- A line starting with `- name:` begins a new tool.
- Every other line is an indented `key: value` pair of the current tool. The
  value is everything after the first `: `; one pair of single quotes around it
  is removed.
- Blank lines and lines starting with `#` are ignored.

| Key | Meaning |
|---|---|
| `name` | The tool name, also the command looked up with `command -v`. |
| `version_cmd` | The command that prints the installed version. This is the only command the check ever runs for the tool. |
| `version_sed` | A `sed -E` expression, used as `sed -nE`, that prints the version number from that output. The first line printed wins. |
| `target_file` | Go only: the file whose `go` directive gives the Go line. Defaults to `go.mod`. |
| `newest_kind` | Where the newest version comes from: `go-dl` (the go.dev download feed, newest stable release of the target line), `github` (the `latest` release tag of a GitHub repository) or `goproxy` (the `@latest` version from the Go module proxy). |
| `newest_url` | The HTTPS address of that source. It is only read. |

To add a tool, append an entry in the same shape. Example:

```yaml
- name: shellcheck
  version_cmd: shellcheck --version
  version_sed: 's/^version:[[:space:]]*([0-9]+\.[0-9]+\.[0-9]+).*/\1/p'
  newest_kind: github
  newest_url: https://api.github.com/repos/koalaman/shellcheck/releases/latest
```

Run the version command by hand first and make sure the `version_sed`
expression prints only the number. Then run the check and confirm the new row
is `CURRENT` or a real difference, not `UNREACHABLE`.

## Environment variables

| Variable | Effect |
|---|---|
| `TOOLCHAIN_FILE` | Use another tool list instead of `toolchain.yaml` at the repository root. A missing file prints a warning and exits `0`. |
| `GITHUB_TOKEN` | Sent as a bearer token to `api.github.com` only, to lift the rate limit. |
| `TOOLCHAIN_FIXTURE_DIR` | A test hook. When set, the network is never touched: the newest version of each tool is read from a file in this folder (`<tool name>` holding a version such as `2.22.1` or `v2.22.1`, and `go.json` in the shape of the go.dev feed for Go). A missing or unreadable file makes that tool `UNREACHABLE`. Installed versions still come from running the version commands. |

## Reading the result

- **`GO-BEHIND`** — install the newest patch of the line shown, then run the
  check again. After a Go update rebuild the Go-built tools (`gosec`,
  `govulncheck`, `gitleaks`, `golangci-lint`) so they use the new standard
  library, and rebuild the datawatch binary at the next release so the
  deployed daemon carries the fixed standard library.
- **`BEHIND`** — upgrade the tool a minor or major version.
- **`PATCH-BEHIND`** — nothing is required. Upgrade when convenient.
- **`MISSING`** — the tool is not installed on this host, or it is not on the
  `PATH` of the shell that ran the check. A service or session may have a
  shorter `PATH` than your login shell.
- **`UNREACHABLE`** — read the `note:` line:
  - *could not read or parse the newest version from …* — the source could not
    be reached (offline, a proxy, or GitHub's rate limit; set `GITHUB_TOKEN`
    and run again) or it changed shape.
  - *could not parse the installed version from …* — the tool printed no
    release number. A tool built from source without version stamping prints a
    placeholder such as `Version: dev` (`gosec`) or `version is set by build
    process` (`gitleaks`), and a placeholder cannot be compared. Rebuild that
    tool from a tagged release with its version stamped, or install a release
    binary, and the row becomes comparable.

## Test it

```bash
bash scripts/check-toolchain_test.sh
```

The test runs the check only through the fixture hook, so it never touches the
network and never reads your real tools: it puts fake tool scripts that print a
chosen version first on a private `PATH`, with no `curl` or `wget` reachable.
Everything it creates lives in one temporary folder removed on exit, and it
checks that the repository's `git status` is unchanged afterwards. It uses the
real `toolchain.yaml` and a fixture `go.mod` with the line `1.26`.

Scenarios, each asserting the printed row and the exit code:

| Scenario | Expected |
|---|---|
| Go one patch behind the newest stable release of its line | `GO-BEHIND`, exit `1` |
| Installed equals newest for every tool | all `CURRENT`, exit `0` |
| Fixture files absent or unreadable | `UNREACHABLE` with notes, exit `0` |
| One tool absent from `PATH` | `MISSING`, exit `0` |
| A tool other than Go a minor (or major) behind | `BEHIND`, exit `1` |
| A tool other than Go only a patch behind | `PATCH-BEHIND`, exit `0` |

It ends with a count of passed and failed checks and exits `1` if any failed.
Run it after changing `scripts/check-toolchain.sh` or `toolchain.yaml`.

## Where it is and is not used

- It is a **script you run**. It is not part of `make lint`, `make test` or the
  merge helper, and there is no `make` target for it yet.
- The daemon does not run it on a schedule. No alert, Events row, dashboard
  card or notification is produced from it.
- It checks the host it runs on. A federated peer, a compute node or a
  container image has its own tools; run the check there to see them.

| Surface | Available | Reason |
|---|---|---|
| Shell script | Yes | `bash scripts/check-toolchain.sh` |
| REST, MCP, CLI (`datawatch …`), chat | No | Development tool for the build host, not a daemon feature. |
| `config.yaml` / Settings | No | There is no setting; the tool list is the repository file `toolchain.yaml`. |
| Web app, Android, iPhone | No | Nothing operator-facing to show. |

## See also

- [Dependency update](automation-dependency-update.md) — the weekly Automation
  that updates the repository's *dependencies* (Go modules, base images, GitHub
  Actions). This check is about the *tools* on the build host, which that
  Automation does not touch.
- [CodeQL local scan](codeql-local-scan.md) — another build-host developer
  check that runs by hand and uploads nothing.
- [Operations: Build-host toolchain freshness check](../operations.md#build-host-toolchain-freshness-check)

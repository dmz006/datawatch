---
docs:
  index: true
  topics: [toolchain, build-host, scripts, go, schedule, development]
---
# How-to: Keep the build host toolchain current

`make check-toolchain` tells you whether the development tools on the machine that
builds datawatch are behind the newest stable release: Go and the security and
release tools listed in `toolchain.yaml`. This page covers running it, reading the
result, running it daily, and updating Go when it reports `GO-BEHIND`.

**This is a development tool for people and sessions that build datawatch.** It
is not an operator feature. The daemon never runs it, it has no setting, and it
has no REST, MCP, CLI (`datawatch …`), chat, web app or phone app surface. It
reads only: it runs each tool's own version command, reads the newest version
over HTTPS and prints a table. It installs and upgrades nothing.

For the full reference (every status, the `toolchain.yaml` keys, the offline test
hook), see [Toolchain freshness check](toolchain-freshness-check.md).

## What the check covers

The tools are the entries of `toolchain.yaml` at the repository root:

| Tool | Newest version comes from |
|---|---|
| `go` | the go.dev download feed, newest stable release of the line in `go.mod` |
| `gosec` | latest GitHub release |
| `govulncheck` | the Go module proxy |
| `gitleaks` | latest GitHub release |
| `golangci-lint` | latest GitHub release |
| `trivy` | latest GitHub release |
| `goreleaser` | latest GitHub release |

The Go line is read from the `go` directive in `go.mod` each time the check
runs, so it follows the repository. To add a tool, append an entry to
`toolchain.yaml`; the format is described in the reference above.

## Run it

From the repository checkout:

```bash
make check-toolchain
echo $?
```

`make help` lists the main targets, including this one. `make check-toolchain`
runs `bash scripts/check-toolchain.sh`. It is part of the **local pre-tag run**
only: `make build`, `make lint`, `make test` and the CI workflows do not call it.

## Reading the table and the exit code

The table has one row per tool: `TOOL`, `INSTALLED`, `NEWEST`, `STATUS`. Notes
about sources that could not be read go to standard error, one `note:` line each.

| Status | Meaning | Action |
|---|---|---|
| `CURRENT` | Installed equals the newest, or is newer. | None. |
| `PATCH-BEHIND` | A tool other than Go has the same major and minor version, with an older patch. | Optional. |
| `BEHIND` | A tool other than Go is a minor or major version behind. | Upgrade the tool. |
| `GO-BEHIND` | Go is older than the newest stable patch of the line in `go.mod`. | [Update Go](#update-go). |
| `MISSING` | The tool is not on the `PATH` of the shell that ran the check. | Install it, or fix `PATH`. |
| `UNREACHABLE` | The newest or the installed version could not be read or parsed. | Read the `note:` line. |

| Exit code | When |
|---|---|
| `1` | Go is `GO-BEHIND`, or any other tool is `BEHIND`. |
| `0` | Everything else, including `PATCH-BEHIND`, `MISSING` and `UNREACHABLE`. |

A network error never fails the check, so an offline host exits `0`. That means
`UNREACHABLE` is not a pass: read the table, not only the exit code.

## Run it daily

### As a person: cron or a systemd timer

Run the make target from the repository checkout, and send the output somewhere
you will see it. With cron (`crontab -e`):

```cron
# every day at 06:30, from the checkout; mail or log the output
30 6 * * * cd <path-to-datawatch-checkout> && make check-toolchain
```

With a systemd user timer, a service unit whose `WorkingDirectory` is the
checkout and whose `ExecStart` runs `make check-toolchain`, and a timer unit
with `OnCalendar=daily` and `Persistent=true`. A non-zero exit marks the unit
failed, which is the signal that Go or a tool is behind. Check it with
`systemctl --user status <service-name>`.

### With a datawatch schedule

datawatch's schedule feature can run a shell command on a cron, in a working
directory, without a language model. A scheduled shell run ends when the command
exits, and its exit code signals completion.

Create it from the CLI:

```bash
datawatch schedule spawn \
  --cron "30 6 * * *" \
  --path <path-to-datawatch-checkout> \
  --schedule-name toolchain-check \
  --shell "make check-toolchain"
```

The same schedule can be created with the `schedule_spawn` MCP tool (shell
command, project directory, cron and schedule name). See
[commands](../commands.md) for the full set of `schedule spawn` options.

- List schedules, and see the fire count, last fire and last result, with
  `datawatch schedule list` or the `schedule_list` MCP tool.
- Cancel with the `schedule_cancel` MCP tool, or with the schedule cancel command
  in [commands](../commands.md).
- The overlap guard skips a fire while the previous one is still running.

This schedule is something you set up on the build host. It is not a built-in
Automaton or Automation, and the daemon does not create it for you. The check
result itself raises no alert and no Events row: you read it in the schedule
history or in the scheduled session's output.

## Update Go

When the table says `GO-BEHIND`, install the newest patch of the line shown. Do
not edit the system install in place: put the new release beside the old one, so
you can go back.

1. **Find the release.** Open go.dev/dl and pick the archive for your platform
   and CPU architecture, at the version the table shows in the `NEWEST` column.
2. **Download the archive and its checksum.** The download page lists the
   SHA-256 of each archive. Copy the value from the page for the exact archive
   you downloaded.
3. **Verify the sha256 against the published checksum** before you extract
   anything:

   ```bash
   sha256sum <downloaded-archive>.tar.gz
   ```

   The output must match the published value exactly. If it does not, delete the
   download and fetch it again; do not extract it.
4. **Extract into a new versioned directory** next to the old one, for example
   `go-<new-version>` beside `go-<old-version>` under the same parent folder.
   Never extract over the existing `go` directory.

   ```bash
   mkdir <parent-folder>/go-<new-version>
   tar -C <parent-folder>/go-<new-version> --strip-components=1 -xzf <downloaded-archive>.tar.gz
   ```
5. **Switch to the new directory.** If `PATH` names the directory, change it to
   `<parent-folder>/go-<new-version>/bin`. If `PATH` goes through a symlink,
   repoint the symlink at the new directory. Open a new shell and confirm:

   ```bash
   go version
   make check-toolchain
   ```

   `go` should now be `CURRENT`.
6. **Keep the old directory until a build and test run pass.** Run
   `go build -o datawatch ./cmd/datawatch/` and `go test ./...` with the new
   Go. Only after both pass, delete the old versioned directory. If either
   fails, switch `PATH` or the symlink back and investigate.
7. **Rebuild the Go-built tools.** `gosec`, `govulncheck`, `gitleaks` and
   `golangci-lint` are built with Go. After a Go update, rebuild or reinstall
   them so they use the new standard library. A service or scheduled session may
   have a shorter `PATH` than your login shell, so update that `PATH` as well.

The datawatch binary itself needs a rebuild at the next release to carry the
fixed standard library. The check does not do that for you.

## Where it applies

| Surface | Available | Reason |
|---|---|---|
| `make check-toolchain` | Yes | The build host, from the checkout. |
| cron, systemd timer, `datawatch schedule spawn` | Yes | Ways to run the same make target daily. |
| REST, MCP (as a tool of its own), CLI command of its own, chat | No | Development tool for the build host, not a daemon feature. |
| `config.yaml` / Settings | No | No setting; the tool list is `toolchain.yaml`. |
| Web app, Android, iPhone | No | Nothing operator-facing to show. |

The check covers the host it runs on. A federated peer, a compute node or a
container image has its own tools; run the check on that machine to see them.

## See also

- [Toolchain freshness check](toolchain-freshness-check.md) — the full reference
  for statuses, `toolchain.yaml`, environment variables and the offline test.
- [CodeQL local scan](codeql-local-scan.md) — another build-host developer check
  run by hand.
- [Dependency update](automation-dependency-update.md) — the weekly Automation
  that updates the repository's dependencies, which is separate from the tools on
  the build host.

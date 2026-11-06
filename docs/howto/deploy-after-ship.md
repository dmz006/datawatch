---
docs:
  index: true
  topics: [deploy, ship, restart, health, rollback, merge, pipeline, automata, autonomous]
exec_steps:
  - tool: autonomous_deploy_get
    description: Read the effective deploy settings, whether a merge or deploy is running, the last deploy phase and outcome, any deferral and its reason, and the snapshot time and SHA-256
    args: {}
    read_only: true
---
# How-to: Deploy after ship

After a release ships, the daemon can build the new binary, install it, restart
itself and check that it came back healthy. If it did not, the daemon puts the
previous binary back, restarts again and asks you what to do. This guide shows
how to set it up, what each outcome means and what record a failed deploy
leaves behind.

Nothing here runs until you set it up: **every step is a setting, and an empty
setting means that step does nothing.** A daemon with no deploy settings never
builds, installs or restarts anything.

The deploy is the `deploy_latest` step of the [merge pipeline](merge-pipeline.md),
and you can also start it by hand at any time.

## What happens, in order

1. **Snapshot.** If an install command is set, the daemon copies the installed
   binary aside and records its SHA-256. Nothing else has changed yet.
2. **Build.** The build command runs in the project. If it fails, nothing is
   installed.
3. **Drain.** The daemon waits for running final-gate tasks to finish, up to the
   drain timeout.
4. **Record.** It saves the sessions and capacity leases it holds, so it can
   compare after the restart.
5. **Install.** The install command replaces the binary.
6. **Restart.** The restart command restarts the daemon.
7. **Health check.** The restarted daemon runs the health check until it passes
   or the health timeout ends, then confirms every session that was running
   before the restart is running again and no held lease was lost.

If step 7 passes, the deploy is done. If it fails, the daemon restores the
snapshot, runs the restart command a second time and checks health once more.

## Set it up

Every key lives under `autonomous.deploy`. Each one is a shell command line or a
duration, and each is optional.

| Key | Meaning | Default |
|---|---|---|
| `build_command` | Builds the new binary, run in the project | empty: skip |
| `install_command` | Installs the new binary over the installed one; needs `binary_path` | empty: skip |
| `binary_path` | Absolute path of the installed binary; the previous binary is copied from here | empty |
| `restart_command` | Restarts the daemon | empty: skip |
| `health_check` | A command whose exit code 0 means healthy | empty: no check |
| `drain_timeout` | How long to wait for final-gate tasks, for example `10m` | `10m` |
| `health_timeout` | How long the health check may keep failing, for example `2m` | `2m` |

Example, using generic commands (use your own):

```yaml
autonomous:
  deploy:
    build_command: "make build"
    install_command: "install -m 0755 ./build/app /usr/local/bin/app"
    binary_path: /usr/local/bin/app
    restart_command: "systemctl --user restart app"
    health_check: "curl -fsS http://localhost:8080/healthz"
    drain_timeout: 10m
    health_timeout: 2m
```

An install command with no `binary_path` is refused, because there would be
nothing to roll back to. A `binary_path` must be a clean absolute path, and a
command that kills tmux without a throwaway socket is refused. Commands run through `sh -c`; the daemon removes
anything that looks like a credential from the text it records.

## The five ways to use it

| Method | How |
|--------|-----|
| **YAML** | Edit `~/.datawatch/config.yaml` > `autonomous:` > `deploy:` (the seven keys above) |
| **CLI** | `datawatch config set autonomous.deploy.<key> <value>` to change a setting; `datawatch autonomous deploy show` to read the state; `datawatch autonomous deploy run` to deploy now (both take `--json`) |
| **Web UI** | Settings tab > Automation > **Deploy** card: the settings in effect, the last phase and outcome, any deferral reason, and a **Run now** button that stays disabled until a build, install or restart command is set |
| **REST API** | `PUT /api/config` with `{"autonomous.deploy.health_timeout": "3m"}` to change a setting; `GET /api/autonomous/deploy` to read the state; `POST /api/autonomous/deploy/run` to deploy now |
| **Comm channel** | `configure autonomous.deploy.health_timeout=3m` to change a setting; `autonomous deploy` (or `autonomous deploy show`) to read the state; `autonomous deploy run` to deploy now |

The MCP tools are `autonomous_deploy_get` and `autonomous_deploy_run`, and the
settings are changed with `autonomous_config_set` or `config_set`. Each help link
in the card opens the matching entry in
[the definitions](../datawatch-definitions.md#deploy-after-ship).

## Read the state

`datawatch autonomous deploy show` prints the settings (an unset one reads
`(not set)`), whether a merge or deploy is running now, and the last record:

```
Deploy settings:
  binary_path: /usr/local/bin/app
  build_command: make build
  ...
Last deploy:
  phase: done
  outcome: ok
  snapshot: 2026-01-01T10:00:00Z sha256 <hash>
```

The snapshot is shown by time and SHA-256 only, never its contents.

## The outcomes

| Outcome | What it means | What you do |
|---|---|---|
| **done** | The new binary is healthy, every session that was running came back and no capacity lease was lost. The phase is `done` and the last outcome is `ok`. | Nothing. |
| **skipped** | No build, install or restart command is set, so nothing was run. This is not a failure and the merge run carries on. | Set the commands you want, if you want a deploy. |
| **deferred** | Final-gate tasks were still running when the drain timeout ended. Nothing was installed and the binary is untouched. The reason (how many tasks, after how long) is in the state. This is not a failure. | Wait for the tasks to end and run the deploy again, or raise `drain_timeout`. |
| **failed health, rolled back** | The restarted daemon failed the health check, lost a session or lost capacity leases. The daemon restores the snapshot, restarts again, checks health once more and raises a question. The phase is `rolled_back`. | Answer the question (see below). |
| **rollback_failed** | The snapshot could not be restored, or the second restart failed. The daemon takes **no further automatic action** and raises a question. | Inspect the daemon and the binary by hand. |

A build, install or restart command that fails before the restart ends the run
as `failed` with the command's exit code and the end of its output; the phase
records where it stopped (`install_failed` or `restart_failed`). A build failure
leaves the installed binary alone.

## What the record shows

Each deploy the restart interrupted ends with one deploy outcome record, kept in
the daemon's data directory and read back as the last outcome. It shows:

- the **outcome**: `ok`, `rolled_back` or `rollback_failed`;
- the **failed check**: the health check output, or the sessions that did not
  come back (named), or the lease count that fell;
- whether the previous binary was **restored**;
- the **final health** after the second restart (`healthy`, `failing: ...` or
  `not run` when nothing was restored);
- the snapshot's SHA-256, when the deploy started and a plain-language summary,
  for example: *Deploy rolled back. A new binary was installed. Failed check:
  health check: still failing after 2m0s. Previous binary restored: yes. Final
  health after the second restart: healthy.*

A deploy is not tied to one Automaton, so this is its own small record and not
the Automaton triage record.

## The question you are asked

A rollback, and a failed rollback, each raise one question from the `deploy`
asker, carrying the summary above. The options are **Acknowledge** (nothing more
is done automatically; the default) and **Investigate by hand**. The question
is raised once for each failed deploy. Answer it where you answer any question:
the PWA, the `question_answer` MCP tool or the chat channel.

## Deploy now, by hand

Use `datawatch autonomous deploy run` (or the Web UI button, the REST route or
the chat command above) to run the sequence for the current build without a
ship. The answer is the status (`skipped`, `deferred`, `failed` or `restarting`)
and a note. While a merge or a deploy is already running the request is refused
with a conflict; wait and try again. A `restarting` answer means the restart
command ran; the health check and any rollback finish after the daemon is back.

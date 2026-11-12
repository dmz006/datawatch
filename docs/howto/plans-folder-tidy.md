---
docs:
  index: true
  topics: [plans, tidy, folder, archive, automation, schedule, release-smoke]
exec_params: []
exec_steps:
  - tool: automation_list
    description: List the Automations and their status
    args: {}
    read_only: true
  - tool: schedule_list
    description: List the recurring schedules
    args: {}
    read_only: true
---

# How-to: Plans folder tidy and the daily Automation

`docs/plans/` is kept small by one script, `scripts/tidy-plans.sh`, and a
daily wrapper, `scripts/plans-tidy-daily.sh`. This guide covers the folder
rules, what each daily run does, and every setting with how to change it.

## 1. Base requirements

- A git checkout of the project with `python3` (standard library only) and `bash`.
- Output of both scripts is counts only. File names appear in one place: the daily report.

## 2. The folder rules

Age cutoff: a file is old enough when it is at least `--days` days old (default 7).
Age comes from the `YYYY-MM-DD` name prefix, then the last commit date, then the file time.

**References that keep a file.** An old file stays when any of these names it:

- an open (not closed) row of the registry, `docs/plans/README.md`;
- any file under `scripts/`;
- a root rules file (AGENT.md, CLAUDE.md, CONTEXT.md, CONTRIBUTING.md, DATAWATCH-CONTEXT.md, README.md, SECURITY.md);
- any file under `docs/` outside `docs/plans/`.

| Place | Stays | Moves |
|-------|-------|-------|
| `docs/plans/` root | README.md, HISTORY.md, roadmap archive files, dated plans under the cutoff or still referenced, release notes of the current minor version | dated plans past the cutoff, unreferenced, with a closed registry row go to `historical-plans/`; release notes of an older minor version go to `historical-releasenotes/<minor-version>/` |
| `reports/` | README.md, files under the cutoff or referenced, anything already in an archive folder | old unreferenced files go to `reports/archive/YYYY-MM/` |
| `parity-issues/` | README.md, referenced files, unfiled drafts | old unreferenced **filed** files go to `parity-issues/archive/YYYY-MM/` |
| Closed registry rows | rows of the current stage (the stage holding the lowest-numbered open row) and rows outside stage tables | closed rows of other stages go to `roadmap-archive-YYYY-MM.md`; each keeps its number and text and gets an anchor so links still resolve |

**Archive month folders.** The `YYYY-MM` comes from the file's age date as above.

**Roadmap archive files.** `roadmap-archive-YYYY-MM.md` (UTC month of the run) are never moved.

**Filed-only rule for parity-issue drafts.** A draft moves only when it has a "Filed issue" section that records an issue URL or number and carries no "not filed", "not yet filed" or "not needed" marker.

**Unclassified files.** Anything that fits no rule (an old unfiled draft, an unrecognised top-level file) is counted as unclassified and never moved. A person opens the daily report, reads the list, then files the draft, renames the file to a dated plan, or deletes it by hand.

On every move, links to the file are rewritten everywhere (registry, rules files, `scripts/`, `docs/`).

## 3. What a daily run does

1. Preflight: a git repository and no uncommitted or untracked changes in `docs/plans/`. Either failure raises an alert and stops.
2. Simulate the tidy and the plans link check in a throwaway worktree. A failure raises an alert; the real tree is untouched.
3. Run the tidy in the real tree, then the link check. A failed check restores `docs/plans/` and raises an alert.
4. If `docs/plans/` changed, stage only that folder, commit as `docs: daily plans tidy` with a counts line, and push to the remote and branch.
5. Write the run stamp and daily report, print counts. A push failure keeps the local commit and raises an alert.

**Alert.** On failure stdout carries one `ALERT` line and `.datawatch/reports/plans-tidy-alert.md` is written (result name and counts, no file names). A successful run removes it.

**Run stamp.** `.datawatch/reports/plans-tidy-last-run.json`: `ran_at` (UTC), `result` (ok, nothing_to_do, preflight_failed, link_check_failed, push_failed), counts and the commit.

**Daily report.** `.datawatch/reports/plans-tidy-daily-report.md`: counts plus the unclassified file names.

**24-hour release smoke window.** `scripts/release-smoke.sh` skips its `tidy-plans.sh --check` when the stamp says `ok` and `ran_at` is under 24 hours old. Any other result, an older stamp or a missing stamp keeps the check and its abort. `DW_SKIP_TIDY_CHECK=1` skips the gate.

## 4. Settings

**Environment variables** (read by the daily script; set them in the Automation's environment or before the command):

| Variable | Default | Meaning |
|----------|---------|---------|
| `PLANS_TIDY_DAYS` | 7 | age cutoff, whole days; anything else fails preflight |
| `PLANS_TIDY_REMOTE` | origin | push remote |
| `PLANS_TIDY_BRANCH` | current branch | push branch |

**Flags of `scripts/tidy-plans.sh`:**

| Flag | Effect |
|------|--------|
| `--dry-run` | counts only, changes nothing |
| `--check` | dry run that exits 1 if anything would move |
| `--days N` | age cutoff (default 7) |
| `--no-hygiene` | skip the registry hygiene pass |
| `--no-releases` | skip grouping old release notes |

**Fields of `scripts/plans-tidy-automation.json`:** only `name` and `description`. The file is an import that creates a draft. The intended schedule (03:15 daily), command, working directory, `PLANS_TIDY_DAYS=7` and finish and alert behaviour are written in `description` and are not fields.

## 5. Schedule, command, environment and notification

The Automation docs describe an Automation as a named chain of Automata with members, edges, forks, joins and a budget. They document no schedule, command, environment or notification field on an Automation, in either the Automation page of the web UI or the Automation MCP tools (`automation_create`, `automation_update`, `automation_get`, `automation_list`). Do not look for them there.

What the docs do confirm:

- **Schedule and command.** Recurring work uses the schedule feature: `schedule_add` (MCP), `datawatch schedule add --cron "<5-field cron>"` (CLI), `POST /api/schedule` (REST). Cancel with `schedule_cancel` or `datawatch schedule cancel name=<name>`. A schedule targets a session by name.
- **Environment.** Set the three variables in the environment of the shell that runs the command.
- **Notification.** Use the alert features: `create_alert` and the alert rule tools, or watch for the `plans-tidy-alert.md` file.

Check the current state with `automation_list` and `schedule_list`.

## Related

- [Automation chains](automation-chains.md)
- [Automation tab](automation-tab.md)
- The registry folder rules: `docs/plans/README.md`

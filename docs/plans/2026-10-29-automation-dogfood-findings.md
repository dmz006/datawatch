# Running real work through an Automation: findings log

**Date:** 2026-10-29
**Version:** none (planning document; plan-document commits are exempt from the version bump)
**Tracker:** BL431 / BL473 / BL644 (Automation use by the supervisor)

Operator, 2026-10-29, quoted: "And shouldn't prd be in automation, how else are we going to test and debug". On 2026-10-28 the operator had already said: "We should use automation as much as we can, in chance to debug and make it best, see if there are any suggestions to make it better". Until now every PRD ran on its own and the supervisor did the Automation's work by hand; the three draft arc Automations were never activated. This file logs what happens when real work goes through an Automation, so each defect or surprise becomes a tracker row.

## Rule from now on

New PRDs are put into an Automation before they run: the supervisor approves the PRD, creates or extends an Automation with it as a member, and activates the Automation; the coordinator starts and advances the member. A PRD that is already running is not adopted until a cheap PRD has tested adoption.

## Run 1: Operator decision channel (Automation `47dfb6ef`)

One member: BL610 Step 2 (PRD `e0830001`, 14 tasks). Step 3 is to be inserted with `automation insert` after Step 2 ships, which also tests inserts.

| When | What happened | Verdict |
|---|---|---|
| 2026-10-29 05:09 | `automation create` with one member and an explicit entry, `set-llm --backend pi-prompt`, `activate`. The coordinator logged `automation_advance` then `run` for the approved PRD; the member went to `running`; `automation status` shows the member by the PRD title. | works |
| 2026-10-29 05:09 | `automation activate` prints the raw JSON of the Automation in the CLI, while `get` and `status` print tables. | cosmetic, noted |
| 2026-10-29 05:09 | The PRD had to be approved separately before the Automation could start it; the Automation did not offer to approve it. | by design, noted |
| 2026-10-29 05:09 | The member id had to be written by hand (an 8-hex string). Whether the API generates one when it is left out was not tried. | untested |

Further rows are added as the run goes on: completion and advance, what the Automation's default backend does to the member's tasks, how a held or failed task shows at the Automation level, and what the supervisor watch does and does not see for a PRD that an Automation runs.

| 2026-10-29 05:54 | `automation insert` without `--expected-version` was refused with a 409 ("expected version 0, current version is 3"): the CLI sends 0 when the flag is omitted instead of reading the current version. Retrying with `--expected-version 3` worked and the insert was held as a pending insert for the operator's approval (member id `9c30ef8e`, status pending), as the design says for any insert into an active Automation. | defect: the CLI should default the version to the current one (or fetch it) |
| 2026-10-29 05:54 | `automation get` and `automation status` show no pending inserts (the PENDING column read 0 while one was pending; the pending insert is visible only in the REST body under `pending_inserts`). | defect: the CLI tables and the status line should show pending inserts and how to approve them |
| 2026-10-29 05:54 | A held draft PRD was inserted as a member to represent a step that must wait; it is held by the pending-insert approval, not by the PRD being a draft. | works, a clean way to represent a held step |

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
| 2026-10-29 07:19 | The rules-review judge blocked the Step 2 PRD while it ran as the member of Automation `47dfb6ef`. The coordinator's content auto-fix queued a corrective task whose text quotes the review verbatim, including an absolute temporary path, and which waited for a pi slot behind two verifiers. To change it (narrow it, drop the path, move it to Claude) the PRD had to be cancelled, because a running PRD refuses task edits. | friction: the supervisor's only route to fix a held member is the PRD-level cancel, edit and reset dance |
| 2026-10-29 07:20 | Cancelling the member PRD outside the Automation turned the Automation to `blocked` with the reason "member cancelled outside the Automation"; the pending insert for Step 3 was kept. | works, a clear reason and no data lost |
| 2026-10-29 07:21 | After the PRD was reopened (`reset_task` with force) and was running again, the Automation stayed `blocked` and its member stayed `blocked`. `automation resume` answers 409 ("this operation needs an automation that is paused") and no other control moves a blocked Automation back. | defect: an Automation should follow a reopened member, or offer a retry-member or unblock control |
| 2026-10-29 07:19 | The Automation advances when its member PRD completes, but the merge to main is still a manual step, so a following member could start from a main that lacks the previous member. Step 3 is held (unapproved insert, draft PRD) until the Step 2 merge lands for that reason. | gap, already listed as the merge-and-ship step |


- 2026-10-29 10:37: when the PRD record of a finished member is cancelled after the ship, the Automation turns `blocked` ("member cancelled outside the Automation") instead of completed, so it needs a manual cancel. The coordinator does not merge or ship, so the ship-then-cancel order of the merge procedure always ends this way. Same defect as the earlier cancel finding; completing the member on ship would remove it.

- 2026-10-29 15:33: the auto-fix task generated from a rules-review verdict on the hung-request PRD again quoted absolute paths (the home directory) from the findings, so the scope lint refused to spawn it and the PRD failed with its Automation blocked. Fixed by rewriting the task spec without the paths and resetting the task. The generator should paraphrase paths the way it should paraphrase throttle phrases; the reopened PRD is not followed by its blocked Automation, so the Automation needs a manual cancel when the PRD ends.

- 2026-10-29 16:30: two more findings. (1) The stuck-running watchdog moved the hung-request PRD to `blocked` 15 minutes after its last task finished although every task was complete and the last rules-review pass was a warn, so a finished PRD ends blocked and its Automation cannot finish; the completion rollup after a re-fired review did not complete. It was merged by hand. (2) A decompose of the search-before-create PRD logged "output file ready (42296 bytes), treating as complete" and killed the session, yet the PRD stayed a draft with no tasks; the decompose was started again at 16:30. If it repeats, the output file is not applied.

- 2026-10-29 20:37: cancelling a shipped member's PRD record (the merge procedure's last step) turns a chain that still has later members into `blocked` instead of advancing it, so the next steps of a chain never start; chain A (lane occupancy, search before create, merge pending) stopped after its first member shipped and was rebuilt for the remaining members. Either the coordinator must advance on a merged-and-cancelled member, or the procedure must leave the record to complete and cancel only the last member's. This is the same cause as the earlier blocked-after-ship finding and it is the main gap between the Automation coordinator and unattended running: it does not merge or ship, and a manual ship breaks its own chain.

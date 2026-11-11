# Automata pages redesign (first pass), operator notes 2026-11-06

Status: design notes captured, no PRD filed. Each decision below is asked in chat one at a time and the
operator's answer is quoted here. UI placement is a design question, so nothing is filed until it is answered.

Goal, in the operator's words: "biggest thing make it human consumable and easy to digest ... lets clean up the
automata pages". Also: "there will be more but this is first pass of redesign of the tabs and cards".

## Operator notes, grouped by tab and card (quoted or close paraphrase)

### Automata / Automation, Inbox tab

- Each Triage card is "very terse, says the title and if failed or blocked but no reason or details or info on why
  and i can't select and go to any more details"; it is unclear whether an item is an Automaton or an Automation;
  the detail is "not actionable nor human readable".
- "the open and held questions should be at the top of the list they are the only real actionable"; four held
  questions are shown with "no detail on why they are held or how to change or edit or view or do anything with
  them".
- "supervisor watch looks like a log there should be a automata/automation events tab that has logs and history".

### Automations tab

- "error loading automations tab" (a defect to reproduce and file on its own).

### Resources tab

- Lane fill card: the top makes sense, but there should be a gap before Recent decisions; the headers "lanes and
  recent decisions are blended and hard to tell where one starts/ends". Recent decisions is a log: it belongs in an
  events tab, scrollable, searchable ("may want to browse/search history").
- Usage calculator: "i don't like at all". A mix of explanatory text and graphics; the weekly allowance graphic
  "does not look good on full screen browser"; wording is a lot and not cleanly organised. Wanted: smaller graphs
  that do not expand with the page, and the graphs should be dashboards.
- Saving a reading, usage policy, extra credit and burn guard settings are configuration: they belong in the
  settings page, with any other settings on this tab.
- Infrastructure retries and Automata checks belong in the new events tab.
- Progress and cost: "good but again not made for human consumption and could be made to be nicer".
- It shows only the "current" stats: "what happened with all of my automations from 9.0->10.0? how do we organize
  runs like this and show progress/cost and details per project? I am going to want to select a project and get
  details on automations/automata/sessions." Keep the high-level in-flight view, but "it is important to view these
  efforts project by project" (example: after 10.0, the Android/iOS alignment work with all its open issues).
- "inactive windows": good data, unclear meaning, overwhelming; wants it organised and tied to the Gantt charts
  when they ship.

### Setup and policy tab

- "not sure what i am getting here. nothing is clickable if it is just showing what is set up i can get that from
  the settings page".
- Merge pipeline says "no project"; unclear why it asks anything; if it needs configuration it belongs in the
  settings page.
- Supervisor loop looks interesting but shows 0 escalations and no counts, so it is static; it could be useful with
  per-project detail. Open question: is setup and policy the right place for it?

### Housekeeping tab

- Safe-to-delete report: "nothing actionable what is this for? is it a log of actions is it something waiting for
  me is it just a log? if it's a log it should be in events and collapsible/searchable".
- "so many worktrees with so many lines / commits not on main, something seems off on that?!?!?" (to verify against
  the branch audit, which shows no PENDING branches).
- Cleanup status (after about eight pages of scrolling): raw blobs of text, hard to read, shows permission errors
  with no action, request or explanation of why they are shown.
- Uncommitted work: "what is action, what do i do, how do i get it committed?".

## Cross-cutting requirements derived from the notes

1. Every card says what it is for, what state it is in, why, and what the operator can do next (or that nothing is
   needed).
2. Logs and history (supervisor watch, recent decisions, infrastructure retries, Automata checks, safe-to-delete
   report, cleanup status) move to one events tab: scrollable, searchable, collapsible.
3. Configuration (reading entry, usage policy, extra credit, burn guard, merge pipeline) moves to the settings page.
4. Items are selectable and open a detail with the reason; Automaton versus Automation is always labelled.
5. Graphs are small, bounded in size and dashboard-like; they do not stretch on a full-screen browser.
6. A project axis: pick a project and see its Automations, Automata, sessions, progress and cost, including history
   (the 9.0 to 10.0 runs), next to the cross-project in-flight view.

## Prior designs found (searched 2026-11-06 after the operator recalled a projects idea)

- `2026-10-30-dashboard-purpose-proposal.md` (registry row 273, `Confirmed: no`): option 3 is a per-project view
  (same cards filtered to one project, "needs a project picker"); option 4 is configurable boards (console, health,
  project). Nothing was decided. This answer decides the project view part.
- `historical-plans/2026-11-02-dashboard-card-audit.md`: the Gantt "Timeline" card is marked "Keep (primary project view)", so the
  Gantt work (Automaton `ef660001`) belongs on the project page.
- Registry row 334 (shipped 9.88.0): per-project automation policy under `projects.<alias>.policy`, a tier between an
  Automaton's settings and the daemon defaults. The project page shows and edits it.
- Registry row 268 and `historical-plans/2026-11-01-merge-pipeline-and-onboarding-report.md`: the merge pipeline as per-project
  configuration (`autonomous.merge_pipeline.*` with per-project overrides). The "no project" the operator saw on the
  merge pipeline card is this per-project setting with no project chosen.

## Decisions (asked one at a time; answers quoted)

### 1. The project view (asked 2026-11-06 08:40)

Options offered: A a new Projects tab (project page with Automations, Automata, sessions, progress, cost and
history, plus a project filter chip on the existing lists), B a filter on every existing tab, C project as the top
level of everything.

Operator answer, quoted: "a yes projects tab - in fact check designs i beleive that this was brought up sometime in
the last few weeks as something needed."

Decision: A. A Projects tab. The designs above were checked; the dashboard proposal's per-project option was never
decided, so this answer settles it for the Automata pages. The project page also hosts the Gantt charts, the
project policy and the per-project merge pipeline.

### 2. Where the Events tab lives (asked 2026-11-06 08:45)

Options offered: A a new Events sub-tab in the Automata view (one scrollable, searchable stream with kind and
project filters), B events only on the project page, C reuse the Live Events dashboard card.

Operator answer, quoted: "a - events sub-tab in automata view before templates."

Decision: A. The Automata view's sub-tabs become Automation, Automata, Events, Templates (Events sits before
Templates). It is one stream with kind chips and a project filter; the project page shows it filtered.

Same message, a new requirement, quoted: "also noticed above automation/automata/templates but under Server: there is
a 'repo CI: off, no failing or pending runs, last poll never' -- that is not needed/wanted and if there are events
like that iti is alerts && || in an events log somewhere".

Decision: remove the always-visible "repo CI" status line from under the sub-tab bar. When the repo CI watch is off
it shows nothing. Anything it would have said (a failing or pending run, a poll that has not happened) becomes an
alert, an Events entry, or both, never a permanent status strip. This is a general rule for the page: a status that
is "off", "none" or "never" is not shown at all.

### 3. The Inbox/Triage cards (asked 2026-11-06 08:50)

Options offered: A a plain-language headline, the actual reason in one or two lines, the type labelled Automaton or
Automation, how long it has waited, buttons for the next step, open and held questions sorted first with their text
and options and answered in place, a click opens a detail panel; B the same without in-place actions; C a dense table.

Operator answer, quoted: "a recommendation is good".

Decision: A, the recommended option.

## Dashboard review notes (operator, 2026-11-06 08:51), with what was checked

Quoted: "in dashboards what is the network card it doesn't make sense, live events card is waiting for hook events
but automations apparently are not using them?? the timeline is empty but we know we are doing stuff, 30 day
activity shows 3 days but not the full month and we've been busy for weeks now, multi-ekg is empty and says 383$
but we are not using claude api what is ekg for, not sure what smoke run dashboard is for but apparently none of
our smoke tests are registering???, the ci & release card just making sure it's right to be empty? (all 0)?, cost
report is not accurate with all of the token and burn rate and other stuff we've worked on it needs to be
reassessed and search looks good but not sure if anything is really using it or not".

This extends `historical-plans/2026-11-02-dashboard-card-audit.md` (cards kept or hidden on paper) and the purpose proposal; the card
decisions below are findings to settle, not yet decided.

| Card | What was found (2026-11-06) |
|---|---|
| Network | The card is an orbital map of sessions and observer peers built from the session list and the observer boards. The operator cannot tell what it answers. Decide: drop it, or make it say what it shows. |
| Live Events | Fed only by hook events (PostToolUse, Stop and so on) that Claude sessions send over the WebSocket. The Automata workers on the GPU lanes run pi, which is not known to send these, so the card stays at "Waiting for hook events". To verify with a running pi session. Decide: feed it from the daemon's own events (task started, verified, merged), which is what the Events tab will hold. |
| Timeline (6h) | Drawn from the Automaton fetch plus timings kept from the same hook events. It is empty for the same reason. Decide: draw it from Automaton and task timestamps; it becomes the Gantt on the project page. |
| 30-Day Activity | The analytics data only has sessions from 2026-11-04 on (the three days before it are zero and nothing older exists). Older session records are not there, so the card cannot show the month. To find out why those records are gone (retention or cleanup). History must come from Automaton records, the registry and git, not live sessions (see decision 1). |
| Multi-EKG | A pulse line of board events, plus a burn side panel that reads the cost total. The 383 dollars is the cost report total: 25.5 million Claude output tokens at the API list price of 0.015 per thousand. It is an API-equivalent figure, not what the flat plan costs, and the page does not say so. |
| Smoke Run | Reads `GET /api/smoke/progress`, which only the end-to-end test scripts write to. The operator's smoke tests are not registering, so either the scripts do not post progress or the card reads the wrong place. To verify; the 2026-11-02 audit already says hide it unless the feature has data. |
| CI & release | Fed by the repo CI watch records, and the watch is off (the same fact as the "repo CI: off" status line). The zeros are therefore wrong, not "right to be empty": CI is running and failing or passing every day. Decide: turn the watch on, or read the CI state some other way, and when it is off say so on the card. |
| Cost report | Counts Claude output tokens and 69 thousand input tokens, with no cache reads or cache writes (the burn script counts 64 million cache-read and 3.3 million cache-write tokens for the last few hours alone), so tokens and dollars are both wrong for real usage. The 359 pi sessions and 48 subprocess sessions show zero tokens. The reconciliation against the usage reading and burn rate is the "reassess" the operator asks for (registry row `090f0001`, cost report, still a draft). |
| Search | Operator: looks good. Unknown whether anything calls it; check the search usage counts and the callers. |

### 4. Dashboard cards: keep, fix or remove (asked 2026-11-06 08:55)

Operator answer, quoted: "we can add/remove dashboards as we need so no need to remove if we can fix the data for
them or clearly know what they are used for so operators can use or not use them. if the data isn't fixed then they
should be removed. network could be useful if we properly linked all of things from project->multi automations
->many prd -> many sessions and we could browse/rotate/review it has potential just is a mess. same for ekg. see
what creative ways we can fix the datasets based on all of the 10.0 changes and observations we have made we may
even have new dashboards right?"

Decision: the rule is "fix the data or remove the card". No card is removed now; each card gets a data fix and a
one-line purpose, and a card whose data cannot be fixed is removed. Network and Multi-EKG are kept as ideas to
rebuild on linked data. New dashboards are welcome.

### 5. The usage page (asked 2026-11-06 08:58)

Options offered: A three small fixed-size cards (weekly allowance, pace line, burn per day) that serve as dashboard
cards and as a row on the Resources tab, a one-sentence verdict above them, explanations behind a "?" popover, and
the reading entry, usage policy, extra credit and burn guard moved to Settings under Automata; B the same cards on
the Resources tab only; C a separate Budget sub-tab.

Operator answer, quoted: "A recommendation is good".

Decision: A, the recommended option.

## Housekeeping finding (checked 2026-11-06 09:00): the worktree concern is real

The operator asked "so many worktrees with so many lines / commits not on main, something seems off on that?!?!?".
An earlier guess in this session was that rewritten merges make landed branches look unmerged. The branch audit
(which compares by commit subject) does account for those and lists 13 branches as landed. It also shows:

- 173 directories under `~/.datawatch/prd-worktrees`, 28 GB, and 136 registered git worktrees.
- 77 branches classed "other": archived Automata whose commits are not found on main by subject (for example
  `4e190001` 16 commits and `2713dbb9` 16 commits with 0 and 1 matching, `28510001` 5 commits, 0 matching).
- 4 live branches, which are the running Automata (their commits are not on main yet, as expected).

So the 77 are not explained by squash merges. They are archived work with no sign of landing: dropped, cancelled,
superseded or lost. Nothing may be deleted until each is classified (rule: never lose features or useful code).
The Housekeeping card must therefore show these as a reviewable list (Automaton, what it was for, commits, last
activity, and a recommendation: landed, superseded by `<release>`, or needs a decision), with the action next to
each. Cleanup of the 28 GB follows only after that review.

## Data that exists today and can feed the cards (checked 2026-11-06)

- **Progress history.** The progress trend has a point per day since 2026-10-24 (completed Automata 170 on 10-24,
  260 on 11-01, 315 on 11-05; known and estimated tasks, throughput, utilization, a completion range and the
  direction "widening"). This is the month the 30-Day card cannot show from sessions, and it is real history.
  One oddity to file: the 11-06 point reads 0 completed, 0 throughput and a completion range of 8 to 396 days,
  computed right after the redeploy; the point should not be written from an empty post-restart state.
- **Linked work trace.** The trace rollup links project, Automaton, task and session (it returned 227 thousand
  characters, so it is rich). Placement tags add node, lane and role to each session. This is the linkage the
  Network card needs.
- **Capacity ledger.** Leases with acquire times, holders, pools, node and model, plus verifier wait and run
  statistics (runs, average wait, pass and timeout categories). This gives lane occupancy over time, which no card
  shows.
- **Usage readings and the burn.** The operator's readings, the pace line and the burn per day.
- **Supervisor loop runs and stats, decision records, defect intake, held questions, attention list,
  infrastructure retries, shipped counts, release and CI state.**

## Proposed fixes per card, and new dashboards (to settle with the operator, none built)

| Card | Fix |
|---|---|
| Network | Rebuild as a work map: project, then Automations, Automata, tasks and sessions as a linked graph from the trace data, coloured by node and lane. Click a project to expand and rotate through its running work; sessions open from it. |
| Multi-EKG | Rebuild as lane pulse: one trace per lane and node from lease start and end (busy, idle, waiting), with the usage burn panel in points against the pace line, not dollars. |
| Live Events | Feed it from the daemon's own events (task started and verified, lease taken, merge shipped, decision recorded), the same stream as the Events tab, so it works for pi workers. |
| Timeline | Draw it from Automaton and task timestamps; this becomes the Gantt on the project page. |
| 30-Day Activity | Read the daily progress history and shipped counts, and the release tags, so the 9.0 to 10.0 month shows. |
| CI & release | Show the real release and CI state (ship status, latest tag, failing runs), and say "repo CI watch off" when it is off. |
| Smoke Run | Verify why the smoke runs do not register; if the scripts cannot post progress, fix that or remove the card. |
| Cost | Reconcile against the usage readings: count cache tokens and pi tokens, and label dollars as API-equivalent or show points. |
| Search | Show who calls it (counts by caller) so its use is visible. |

New dashboards the data now allows: lane utilization over time (an Automaton `a4b40001` already exists for a lane
utilization view); throughput and completion forecast (the progress trend with its low and high range); verifier
health (waits, runs, pass rate per node); release train (tags, CI, deploy state); review backlog (plans waiting for
a decision, held questions and their age); defects found and fixed per day.

## Questions still to ask (one at a time)

4. (answered above)
6. Housekeeping: what stays as an action card (uncommitted work with a commit button, permission errors with a fix)
   and what becomes events.
7. Setup and policy: which items become Settings entries and which stay as a project page section.

### 6. Housekeeping (asked 2026-11-06 09:02)

Options offered: A an action list (new "Unlanded work" card with a recommendation and Keep, Archive branch and Delete
worktree buttons per row, a Commit button with a diff and message for uncommitted work, one-line reasons and a Fix
button for permission errors, the safe-to-delete report and cleanup history moved to Events); B read-only
recommendations with printed commands; C a one-time review first.

Operator answer, quoted: "a yes, action list! and i'm looking forward to your review of worktree hopefully some good
work in there to use!!"

Decision: A, with the review of the 77 unlanded branches done first (the C step), so the list starts sorted and any
useful work is recovered before anything is deleted. The review result is recorded in
`2026-11-06-unlanded-branch-review.md`.

## Why the worktree cleanup did not prevent this (operator question 2026-11-06 09:08)

Quoted: "so yes c a one time review too need to get worktrees fixed and why did our work on cleaning up and having
datawatch manage worktrees see this is this a bug, overlook or what?"

Checked: the cleanup from registry row 256 (`f8f60001`) is on and working. Its last status shows worktrees: mode
on, 67 removed by the owner and 229 removed in total, 37.6 GB freed in total, and the last pass skipped 168. It
removes only branches whose commits are all on the base branch (the safe-to-delete rule in
`internal/autonomous/merge_safe_delete.go`: "strictly read-only ... deletion stays a separate, operator-approved
step") and never touches a running Automaton, a live session or lease, or a tree with uncommitted changes. So the
168 skipped are exactly the work it is built to protect.

Finding: an overlook in the design, not a bug in the cleaner. Cleanup covers the safe side (landed work) and the
live side; nothing resolves the middle. An archived Automaton whose commits never reached main stays forever:
the cleaner keeps it, the safe-to-delete report does not list it (it lists only safe entries), and
`scripts/branch-audit.py` flags only a PENDING record (a completed Automaton not on main). The audit's category
"other" (archived, not landed, 77 now) has no rule that says it must be merged or recorded as dropped. Two causes
feed that middle: the supervisor loop archives a cancelled Automaton within seconds (defect `61c60001`, running,
which keeps an Automaton with unmerged commits from that fix on, but does nothing for the existing ones), and
Automata that failed or were replaced were archived without a recorded outcome.

Fix in three parts: (1) the one-time review of the 77 (running, results in
`2026-11-06-unlanded-branch-review.md`); (2) the "Unlanded work" action list on the Housekeeping tab (decision 6),
so each one gets an outcome (land, keep as reference, delete after classification); (3) the registry audit rule and
`branch-audit.py` treat "other" as a finding to be resolved, and the cleaner reports the skipped worktrees by
reason with the unlanded ones named.

### 7. Setup and policy, and where configuration lives (asked 2026-11-06 09:08)

Options offered: A Setup and policy stops being a tab, everything editable moves to Settings under Automata, the
per-project parts (project policy, merge pipeline) sit on the project page with a project picker, the supervisor
loop's runs go to Events with per-project counts on the project page; B keep the tab and make every row clickable
and editable; C move everything to Settings only with a project dropdown there.

Operator answer, quoted: "q7, move to settings all configuration should be in settings - also are all
configuratable options fully aligned with parity rule? hopefully ??"

Decision: all configuration lives in Settings (A and C combined: no editable configuration on the Automata pages;
the project page links to the project's entries in Settings and may show them read-only). Setup and policy as a tab
goes away; what it showed that is not configuration (supervisor loop runs and counts) goes to Events and the
project page.

Parity question, answered 2026-11-06: the rule exists (AGENT.md: every option reachable by YAML, web Settings, REST
`GET` and `PUT /api/config`, the comm channel `configure`, MCP where it has a status tool, and the CLI `config set`,
plus a datawatch-app issue for PWA changes). It is enforced feature by feature: 47 `*config_surface_test.go` files
check the surfaces of one feature each. There is no single audit that proves every option in `config.go` reaches
every surface, so "all aligned" is not proven today. An audit script and a CI gate are needed (registry row 374).
Moving the configuration out of the Automata tabs into Settings must keep every one of the six surfaces and file
the mobile parity issue.

### 8. File the PRDs (asked 2026-11-06 10:15)

Options offered: A file everything in the recommended order, B defect fixes only now, C a different order.

Operator answer, quoted: "a - everything in that order. and are we using a single automation for these or are you
still doing 1:1 automata to automation? we need to start doing things with the arcs and plans together how will the
project organize sub automations is it worth organizing these like we will with projects and projects should also
control order for the automations in them - anything we need to iterate on project details that we didn't before?"

Decision: A. File the six redesign Automata in the order Triage cards, Events, Settings and usage cards,
Housekeeping, Projects, dashboard data, plus the defect fixes (rows 371, 372, 374, 375, 377).

Open design question raised in the same message (answered in chat, recorded below once the operator answers): how
Automations, arcs, plans and projects fit together.

## Proposed model: project, arc, Automaton (for the operator to confirm)

- A **project** owns an ordered list of **arcs**. An arc is an Automation: a named body of work with a goal, for
  example "Automata pages redesign", "10.0.0 release", "Android and iOS alignment". The project sets the order and
  priority of its arcs and which one the lanes serve first.
- An **arc** (Automation) holds its **members**: Automata, or nested Automations for a sub-arc. Order inside an arc is
  the edges between members (the design already supports stages, edges and nested Automations). A plan or design
  document is attached to the arc and gates it: members of an arc do not run until the plan says `Confirmed: yes`.
- An **Automaton** stays one body of work with tasks. A standalone Automaton that is run by hand is the exception
  (rescue), not the way work is started.
- One Automation per arc, not one per Automaton. Today some Automata sit in an Automation (`ee660001` and `6aca0001`
  are members of `d954318c`) and some were run directly (`90d60001`, started by the supervisor on 2026-11-06 to fill
  the Claude lane, against the 2026-10-29 rule that new work runs inside an Automation).
- Proposed for the redesign: one Automation "Automata pages redesign" with the six Automata chained by edges in the
  decided order, and one Automation "Defect fixes (2026-11-06)" with the five fixes as parallel members.

### The arc as the major feature being grown (operator, 2026-11-07)

Operator, quoted: "projects tend to start with a root idea that turns into the major feature that is developed over time as features are rolled into it and it's expaned and rounded out as it's tested and used untl the full major is ready for release as a working featureset." An arc (Automation) can therefore be marked a milestone: its member Automata release as minors and patches while the feature is tested, used and rounded out, and the arc's completion raises the major release step (approval mode `ask` by default). The project decides what triggers a major (`manual`, `milestone` or `never`). The release size rules and the approval modes are in [the autonomy handover plan](2026-11-03-autonomy-handover-plan.md), Phase 4. The Projects tab and the arc view show the milestone, the minors shipped under it and what is left.

## Project details that need iteration (not covered before)

1. The project record today: alias, directory, per-project policy (`projects.<alias>.policy`), merge pipeline.
2. Missing and wanted: a description and owner; the repository and default branch; the ordered list of arcs with a
   priority and a state (planned, active, done); a target such as a release (10.0.0) and its definition of done.
3. Lane and backend defaults per project (GPU or Claude, models), and a share of the Claude weekly allowance, so
   projects compete for lanes by priority.
4. Links: the plans and design documents of each arc; the datawatch-app issues (the Android and iOS alignment arc is
   mostly open issues); the registry rows.
5. Roll-ups: progress, cost (in points, not API dollars), throughput and history per project and per arc, including
   the 9.0 to 10.0 history kept from Automaton records.
6. Retention and cleanup policy per project (how long an archived Automaton keeps its branch), so the unlanded-branch
   gap does not come back.

## Filed 2026-11-06 10:25 (decision 8)

Eleven draft Automata, none decomposed yet, in two Automations (drafts until the plans are validated and the first
member is approved):

- Automation `3981110c` "Automata pages redesign" (arc): `6a1b0001` Triage cards and the repo CI strip, then
  `85430001` Events tab, `9f8f0001` Settings move and usage cards, `ba340001` Housekeeping action list, `d3680001`
  Projects tab, `edc10001` dashboard data fixes, chained by on-complete edges in the decided order.
- Automation `869ff55c` "Defect fixes 2026-11-06" (arc): `3b7c0001` task merge-back, `55270001` verifier idle and task
  override, `21610001` resumed-session lease, `08800001` zombie badge, `70900001` configuration parity audit.
- Note: the redesign arc was created by reusing an empty draft Automation made by a schema probe; the defects arc is a
  chain too, so its fixes run one after another; if parallel is wanted they can be rewired with a fork and a join.

### 9. The project, arc and Automaton model (asked 2026-11-06 10:30)

Options offered: A confirm the model (one Automation per arc, the project controls arc order, the model goes into
the Projects tab Automaton as its spec); B confirm but lay the defects arc out in parallel with a fork and a join;
C change something.

Operator answer, quoted: "plan looks good, claude at 72%".

Decision: A. The model stands as written above: a project owns an ordered list of arcs (Automations), an arc holds
Automata and nested Automations ordered by edges, a plan gates its arc, a standalone Automaton run by hand is only a
rescue. The Projects tab Automaton (`d3680001`) already carries the model and the project record additions in its
spec and reads this document first. The defects arc stays a chain unless the operator asks for parallel.

### 11. Where the protected-paths card lives (asked 2026-11-06 16:55 and 17:10)

Options offered: A leave it on the Housekeeping sub-tab, B move it to Settings, C elsewhere.

Operator answer, quoted: "if it is an output/log/details then A in housekeeping; if it is configuration details
then it should be in settings".

Decision: a general placement rule for every card on these pages: what the operator reads or acts on (outputs, logs,
details, held items) stays on its Automata page; configuration details live in Settings. The protected-paths card has
both parts, so it is split: the list of held paths and their actions stay on the Housekeeping sub-tab, and its settings
(the protected paths list, the action word and the rules review source) belong in Settings under Automata. The Settings
move Automaton (`9f8f0001`) carries the settings part with the other moved configuration; until it ships the card keeps
both parts on Housekeeping as shipped in 9.104.0.


### 12. Tags and counts (asked 2026-11-06 17:25 and 17:45)

Questions asked: which tags every Automata-launched session should carry by default (A add the Automaton id and the
project; B only the project; C another set), and, as a finding rather than a question, why the 10.0.0 status
shows 45 PRDs and not the whole history.

Operator answer, quoted: "what were the questions for the tags and for the counting? we want to have the counts
proper and make sure it's ready for projects so we can have totals overall, and by project/automation/automata. and
anything we can tag should be tagged".

Decision: everything that can be tagged is tagged, and the counts are corrected and grouped. Tag every session at launch
(worker, verifier, planner, judge, supervisor), tag the objects too (Automata, Automations, tasks, decision records,
held questions) with project, arc and Automaton, turn the Automaton, Automation, project and stage axes on by default,
and sweep existing sessions. Counts: an archived record with every task completed counts as done, and every total is
available overall and by project, by Automation and by Automaton, with records that have no project shown as (none).
Filed as drafts `b7910001` (tag everything) and `dae00001` (counts by project); both join the Projects arc.

The tag filtering and the tags listing land on the existing surfaces rather than a new command (task 606c0019, implemented 2026-11-07 over the REST/MCP endpoints from 606c0018): `datawatch session list` takes `--tag` (repeatable, or comma-separated) and `--tag-match any|all`, plus axis shortcuts `--project`, `--automaton`, `--automation`, `--stage`, `--kind` and `--role`, each of which is exactly `--tag <axis>:<value>` under the same match rule; `datawatch session tags` with no session id lists every tag in use with its count from the existing GET /api/sessions/tags inventory (plain table, or `--json`), while `session tags <session-id> --tags <ids>` stays the replace (empty clears) mutation. The comm channel gets the same two views: `list tag=<axis:value> [tag=...] [tag_match=any|all]` (already there, now named in the help text) and the new `session tags` verb, and `autonomous session-tags list` routes to the same inventory endpoint next to `autonomous session-tags <session-id> [tag-ids]`. Router help text and the `sx2_parity.go` table name all of it. Tests: `cmd/datawatch/session_tag_filter_cli_test.go` (flags to query params, the listing, the mutation) and `internal/router/session_tag_filter_test.go` (the `session tags` verb, the `session-tags list` routing).

### 13. Run telemetry, billing modes and the counts in the tick (2026-11-06 17:55)

Operator, quoted: "and make sure with counts, we should have other prd for this, that we capture time, tools, details so
we can have nice graphs and charts and gantt and other timelines and details and know what time we take and how much
tokens (or $ if we are using api) - in fact thinking of how we are tracking spend/token/budget we should be sure there
can be a switch, per cloud LLM for API or pool/consumption/% or whatever the best name is for what we currently have.
these stats are going to be super helpful later!! (and great dashboards/graphs/reports for eye candy to get people to
like datawatch). can you update the tick script so it shows the current overall counts and details?"

Decisions: (1) draft Automaton `4aa80001` captures run times, tokens, tools, files, commits, results and cost for every
session, attempt, verifier, planner and judge run, and adds a per-LLM `billing_mode`: `api` (dollars), `allowance` (the
plan or pool measured in points or percent, which is what the Claude entry is today) and `local` (no money cost); units
are never added together; it builds the capture that the spend ledger (`24400001`) and cost report (`090f0001`) read, and
the Gantt, charts and dashboards. The word "allowance" is the proposed name for what is tracked today; the operator can
rename it. (2) The tick now runs `scripts/overall-counts.py` before the v10 status table: all records with archived
finished work counted as done, by kind, backend and Automation, with project filled in once records carry one.

## Operator decisions, 2026-11-06 late evening

- Burn guard: "yes change the guard and we will watch carefully up to saturday night". The target line is the ceiling less the usage calculator's `safety_points`, not the guard's `margin_points`. `usage.calculator.safety_points` changed from 5 to 0.5, so the target is 99.5. Watch every tick; revert to 5 if the projection nears the line.
- imap-mcp model: "point imap-mcp to datawatch, load the model there and unload here". The imap-mcp `enrichment.ollama_url` now points at the datawatch node (backup in supervisor-scratch), the service was restarted, and qwen3:1.7b was unloaded on the local node, freeing about 6 GB of GPU memory.
- imap-mcp tracking: "imap-mcp should be a plugin or integrated as a service ... it should have tracking since it is tied that is a benefit of it being part of datawtach ... this is suppose to support all datawatch users, not just our own dev cycles". Filed as a draft Automaton (service-kind plugin per docs/plans/2026-10-09-plugin-extension-surfaces.md).

## Operator decisions, 2026-11-07 early morning

- Claude budget reading, quoted: "Claude is at 89%, probably need to activate guard for a while". The reading at 23:20 on 2026-11-06 was 80%; burn since was about 1.5 points per hour at the end, far over the allowed 0.6. At 89% the weekly projection was 123% at the recent burn (running out Saturday 13:57). The burn guard ladder was set back to `lower_inflight`, `route_pi_prompt`, `hold_claude` and the guard stepped to rung 3 at 05:54 (new Claude work held, running sessions continue, mechanical work on pi-prompt). Release it when a reading shows the projection back under 99.5%.
- Parity audit, quoted: "no parity audit until am features are done". The configuration parity audit (Automaton `70900001`) stays parked; its CI workflow change was not approved (question answered "change the spec" so the task stays blocked) until every feature is finished.

## Operator and supervisor decisions, 2026-11-07 midday

- Operator, quoted: "You're plan for claude is ok, dependabot issue need to be addressed, ok to proceed and fix breaking changes". Claude spend: one lane, hold back above about 0.6 points per hour. Dependabot: the dependency Automaton (`5e000001`) bumps all open pull requests including majors and fixes breaking changes.
- Supervisor answer under that instruction, 12:22: the protected paths question for the setup-qemu pin bump in three workflow files (task `d310000f`) was approved because the change is only the pinned commit, identical to dependabot's own branch. The CodeQL workflow change of the CI rate limit fix (`30c00004`) is not a version pin (it changes the gate logic), so it still waits for the operator.

## Operator answers, 2026-11-07 evening (quoted)

- Protected paths, artifact upload action pins in four workflow files (task of the dependency Automaton), answered through the queue: "approve question 2". Approved; the change is only the pinned commit, v5 to v7.0.2, in seven lines.
- State paths and the guard test, asked as: allow-list the single builder (home-derived default kept) or a home-free default. Quoted: "a - allow". The state path guard test now allows `dataDirPath` as the one sanctioned reader of the home directory.
- Protected paths, the CodeQL push trigger ignoring documents-only pushes, answered through the queue: "approved to ignore docs only pushes". The pull request trigger edit raised its own question and is not yet answered.
- Decision records card on the Automata page, after the supervisor explained it: "yes make the changes you recommended that is better and matches what i would do". The card shows only when a record exists, and its settings move to Settings under Automata (carried by the Settings move Automaton).
- Claude pace: "can we do one or two an hour with claude and monitor usage to make sure we don't go over our total before 7am?" Answered yes with the guard: one extra Claude Automaton per hour for about three hours, stop when the projection passes about 98.5 percent.
- Decomposing: "we may want to wait until these tasks finish before kicking off the decomposing circuit since we can't do one per hour we have a little buffer since our goal is 7am, then we can decompose faster". The decompose circuit is paused until the running agent tasks finish, then several drafts are decomposed in a row.
- The redesign proposal itself has no `Confirmed: yes` line: the operator asked to see the decisions again ("give me the page redesign again, i thought i answered these a while ago but lets redo it") and has not given an overall confirmation. Each numbered decision above is answered; the supervisor does not add the line until the operator says so.
## Parity surface

For the tag-and-counts work settled in decision 12 (Automaton `b7910001`, "Tag everything") — every surface carries
the same data from the same code (the combined how-to is `docs/howto/automata-tags.md`):

| Surface | Touched by this decision | Detail |
|---|---|---|
| REST | Touched | `GET /api/sessions/tags` (the tag inventory: tag, axis, value, count over live and archived sessions) and `POST /api/autonomous/tags/sweep` (re-derives every existing object's `system_tags` under the in-force settings; `?dry_run=true` previews; per-kind `{examined, changed, unchanged, failed}` tally; a second run is a no-op). The sessions list (`GET /api/sessions`) and the Automaton list (`GET /api/autonomous/prds`) take repeatable `tag` + `tag_match any\|all` plus the axis-shortcut parameters (`project=`, `automation=`, `automaton=`, `stage=`, `kind=`, `role=`, `lane=`, `compute=`; `backend=` stays the backend-family filter). `POST /api/sessions/tags` remains the replace-a-session's-tags mutation; the `/api/autonomous/tags` CRUD carries the registry snapshot. |
| MCP | Touched | `session_tag_list` (the inventory, read-only, no arguments) and `autonomous_tag_sweep` (`dry_run` boolean; absent or false writes, the default). `list_sessions` gained the same `tag`, `tag_match` and axis-shortcut parameters. Both tools are documented in `docs/cursor-mcp.md`; caps `CapSessionsList` and `CapAutonomousWrite` respectively. |
| CLI | Touched | `datawatch session list` takes `--tag` (repeatable or comma-separated), `--tag-match any\|all` and the axis shortcuts `--project`, `--automaton`, `--automation`, `--stage`, `--kind` and `--role` (each exactly `--tag <axis>:<value>`). `datawatch session tags` with no session id lists the inventory (`--json` available); `session tags <session-id> --tags <ids>` stays the replace. `datawatch tags create\|rename\|sweep` reaches the tag CRUD and the sweep. Tests: `cmd/datawatch/session_tag_filter_cli_test.go`, `cmd/datawatch/cli_autonomous_tags_sweep_test.go`. |
| Comm channel | Touched | `list tag=<axis:value> [tag=...] [tag_match=any\|all]` (the session-list filter, named in the help text) and the new `session tags` verb over the same inventory endpoint; `autonomous session-tags list` sits next to `autonomous session-tags <session-id> [tag-ids]`. `configure` reaches the placement settings through the same validation as every other surface. |
| YAML/config | Touched | `autonomous.tags.placement` — per-axis `enabled` + `template` for `compute`, `peer`, `backend`, `lane`, `role`, `project`, `automaton`, `automation`, `stage`, `kind` and the custom axes; the five Automata-fact axes (`automaton`, `automation`, `stage`, `kind`, `project`) are on by default under the `autonomous.tags.placement.enabled` master switch. Each axis template must contain its placeholder; a bad value is refused identically from REST config set, CLI `config set`, comm `configure` and the PWA settings screen. |
| PWA | Touched | The Sessions toolbar tag filter (chips grouped by axis, drawn from `GET /api/sessions/tags`, with the one-in-flight / one-request-on-failure fetch discipline this branch fixes), the per-session tags modal with the how-to link, the placement settings rows and help for the five axes, the `tag_update` WebSocket frame that refreshes both in place, and the new locale keys in all five bundles (the key set is listed in `docs/plans/parity-issues/2026-11-06-tag-everything-parity.md`). |
| Android | Excluded from this change, tracked | No Android code lives in this repository. Tracked by the parity issue `docs/plans/parity-issues/2026-11-06-tag-everything-parity.md` (filed in `dmz006/datawatch-app` per the Mobile-Parity Rule): the settings rows, the tag/axis filter on the sessions and Automations lists, the inventory-backed counts, the sweep action with its dry-run, and the locale keys. |
| iPhone | Excluded from this change, tracked | Same parity issue as Android. The Compose-Multiplatform app is the single mobile target. |

## 2026-11-08: the Templates tab and the built-in standard tasks

The operator found the Automata Templates tab empty while every Automaton's task list carried built-in "template tasks"; the two are different things. Asked whether the built-in ones should be renamed (for example "standard tasks") and whether they should be shown in the Automata settings with their mode. The operator answered, quoted: "1 yes, maybe sub tabs under templates for each type, we may have more later. 2, yes". Decision: the built-in tasks are called standard tasks; the Templates tab gets one sub-tab per kind (Automaton templates and Standard tasks first, extensible by registering one entry); the standard task mode is shown in the Automata settings once the mode setting exists. Filed as a draft Automaton from the spec in the supervisor scratch folder.

## 2026-11-11: the dashboard card decisions were built

The decisions of section 4 ("Dashboard cards: keep, fix or remove") and the per-card fixes proposed above are built. Network is now the work map and Multi-EKG is the lane pulse with the burn panel in points. Live Events reads the daemon's own event feed, and the Timeline and 30-Day Activity cards were fixed. The CI and release card, the cost report and the smoke and search usage cards each read their own source. Six new cards (lane utilization, throughput forecast, verifier health, release train, review backlog, defects per day) are in Edit → Add Card. Any card with nothing to show is not shown, and each card has a one-line purpose and a help link to its own section of `docs/howto/dashboard.md`. The data is also on MCP, the CLI and the chat channel, with no new setting. The Sprint pipeline strip stays removed.

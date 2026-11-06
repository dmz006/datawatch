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
- `2026-11-02-dashboard-card-audit.md`: the Gantt "Timeline" card is marked "Keep (primary project view)", so the
  Gantt work (Automaton `ef660001`) belongs on the project page.
- Registry row 334 (shipped 9.88.0): per-project automation policy under `projects.<alias>.policy`, a tier between an
  Automaton's settings and the daemon defaults. The project page shows and edits it.
- Registry row 268 and `2026-11-01-merge-pipeline-and-onboarding-report.md`: the merge pipeline as per-project
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

This extends `2026-11-02-dashboard-card-audit.md` (cards kept or hidden on paper) and the purpose proposal; the card
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

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

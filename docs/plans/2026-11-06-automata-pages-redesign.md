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

## Decisions (asked one at a time; answers quoted)

(none yet)

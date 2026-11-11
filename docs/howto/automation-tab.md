---
docs:
  index: true
  topics: [automation, automata, pipeline, pwa]
---
# How-to: the Automation view

The **Automation** view is the place in the web app where you build, watch and
steer chains of Automata. It is the first sub-tab of the **Automata** tab: open
Automata in the bottom navigation and Automation is selected on the sub-tab bar,
left of **Automata** and **Templates**. It shows every Automation as a card, draws the chain
of the one you open as a strip, and puts the controls (pause, resume, cancel,
skip, insert, remove, edit edges) next to the members they act on. Inside the
Automation sub-tab, a second row of five sub-tabs splits the view, in this
order: **Inbox**, **Automations**, **Resources**, **Progress and cost**
and **Housekeeping**. The former **Setup and policy** sub-tab is gone: a
stored or bookmarked link to it opens Inbox, its status cards moved to the
Events tab and its configuration moved to Settings. Inbox is the one open by default.

The first sub-tab row also holds an **Events** sub-tab (between **Automata**
and **Templates**): the one stream of what the daemon watches and does for
you — supervisor watch, lane fill decisions, infrastructure retries, checks,
cleanup, the repo CI watch and the recorded errors. See
[automata-events.md](automata-events.md).

The second row works like the About and Settings strips: one row that scrolls
sideways with momentum on touch, a hidden scrollbar, sticky at the top, the
active tab scrolled into view, no wrapping, and it fits at phone width. Cards
for features that have not shipped yet are listed below as "appears here when it
ships"; until then the tab shows nothing for them. A later feature adds its card
to its tab without changing the other tabs.

| Sub-tab | What is on it |
|---|---|
| **Inbox** | Everything waiting on you: the needs-attention card, the questions, the held questions and the security failure report. The supervisor watch now lives in the Events tab. A count badge on the tab totals what is waiting. |
| **Automations** | The list of Automations with the status badges. The filter and history buttons show here only. The release arc and its stages, and the Gantt per Automation, appear here when they ship. |
| **Resources** | Capacity, lane fill, usage against the plan, and the Automata ship status card. Infrastructure retries and the Automata checks card now live in the Events tab. Lane utilization and verifier slots appear here when they ship. |
| **Progress and cost** | The Progress card. The all-projects Gantt, the cost and billing report and the completion forecast appear here when they ship. |
| *(Setup and policy, removed)* | The sub-tab no longer exists. The onboarding and requirements report, the supervisor loop runs and counts, the release readiness bar, the model request watch and the merge shadow now show on the Events tab, and only while they have something to show. The merge pipeline, decision records folder, usage reading and extra credit settings are in **Settings → General → Automata**. The placement axes and the tag sweep are under **Settings → General → Automata → Placement tags**; see [automata-tags](automata-tags.md). |
| **Housekeeping** | The uncommitted work card. The safe-to-delete report and the cleanup card now live in the Events tab. Stale worktrees appears here when it ships. |

## What an Automation is

An **Automation** is a named chain of Automata. An **Automaton** is one unit of
work; an Automation is the chain that decides which Automaton runs next when the
current one completes, is blocked, or fails. Once an Automation is active,
datawatch starts the next Automaton by itself. See
[automation-queue-advance.md](automation-queue-advance.md) for how the hand-off
works behind the scenes.

The bottom navigation has no Automation button of its own. Inside the Automata
tab, the sub-tab bar reads **Automation**, **Automata**, **Templates**. The
Automata sub-tab lists single Automata; the Automation sub-tab lists the chains
that connect them.

## Opening it, the remembered sub-tab and saved links

- **First visit.** Tapping Automata in the bottom navigation opens on the
  Automation sub-tab, on its Inbox sub-tab. Tap **Automata** or **Templates**
  on the sub-tab bar, or any of the six sub-tabs on the second row under it,
  to switch. There is no New Automation button in the bar: an Automation is
  created with the round button at the bottom right, or with **＋ Create
  Automation** on the empty state, both opening the same form. The action
  buttons at the right of the bar (select, filter, history, watch) change with
  the sub-tab, and so does the round button; on the Automation sub-tab, filter
  and history show on Automations only, because both act on the list.
- **Remembered sub-tab.** While the web app stays open, the sub-tab you chose
  stays selected when you leave the Automata tab and come back. The same rule
  holds on the second row: the tab you chose is kept while the page stays
  open, and a fresh load of the page starts on Inbox again. The choice is not
  stored across loads.
- **Saved links.** A bare `#automation` link opens the Automata tab on the
  Automation sub-tab, on Inbox. `#automation/<id>` and `?automation=<id>` open
  that Automation on Automations, and the back control on a member's Automaton
  page returns to the Automation that holds it, on Automations. Older links and
  history entries that name the Automation view land in the same place.
- **Subsystem off.** When the autonomous subsystem is not enabled, the
  Automation sub-tab is not offered and every one of these links lands on the
  Automata sub-tab.
- **Phone width.** On a narrow screen the first row fits at 360 px, and the
  second row scrolls sideways instead of wrapping; every sub-tab in both rows
  is at least 40 px tall and wide enough to tap.

## Base requirements

- A running daemon with `autonomous.enabled: true`.
- At least one approved Automaton that is not already part of another
  Automation, to have something to put in a chain.

## The list

On the Automations sub-tab, one card per Automation, ordered by status (active,
blocked, paused, draft, completed, cancelled) and then by name.

### The needs-attention card

On Inbox, the needs-attention card is titled **Triage**. It holds one card per
Automaton that needs an operator because it failed, is blocked, or has finished
work that is ready to merge. While nothing is stuck it shows a one-line empty
note. A `?` beside the title opens this section. See the *Triage records* entry
under [Related](#related) for the record itself and how it fills and clears.

Each card shows:

- **Headline.** What happened, in one line: *Stopped: the task "…" failed*,
  *Blocked: …*, or *Ready to merge: branch … is waiting*.
- **Reason.** The reason recorded for it, cut to two lines. Open details to read
  the whole record.
- **Automaton and Automation.** An **Automaton** chip, and an **in Automation
  *name*** chip when the Automaton belongs to an Automation.
- **Wait time.** *waiting …*, how long the Automaton has been waiting on you.

The card has up to four buttons:

- **Open details** opens the detail panel. A click anywhere on the card, outside
  a button, does the same.
- **Retry** resets the failed task so it runs again. It appears only when the
  record names a task.
- **Resolve review** sends a task that is held for review back to its worker. It
  appears when a task is held for review or the branch is ready to merge.
- **Cancel** cancels the Automaton.

Open questions and held questions sit above the Triage card on Inbox, so they
are the first things you see. You answer an open question in place, on its own
card, without opening the Automaton. The order on Inbox is: open questions, held
questions, Triage, security failure report, supervisor watch.

### The held-questions card

The held-questions card lists tasks that an Automaton has held for an operator
decision. An Automaton holds a task when it names a question it cannot settle
itself while the task is in one of the held states: by default `review_held`,
`decompose_blocked`, `decompose_stalled`, `decompose_failed` and
`decompose_rejected`. The card's badge shows the mode the feature is in:

- **shadow** (the default) records the questions it would have held as *would-be*
  questions, and holds no task.
- **on** holds the task and lists it here until you answer.
- **off** turns the feature off.

Each held entry shows the recommended option, *Held on the default: hold* (what
happens if you do nothing), a link to open the Automaton or task, and an
**Answer** button, which opens the question so you can respond.

To view or change the mode or the held states, open **Settings** under **Automata**
and look at **Held questions mode** (`autonomous.questions.held_mode`) or **Held
question states**.

Clicking a Triage card opens its detail panel.

### The cards on the other sub-tabs

Each card loads with its own loading skeleton and, if the request fails, its own
error with a Retry button; a failed card never blanks the list, and a failed list
never blanks the cards.

- **Inbox:** the needs-attention card above, the open questions (see
  [Questions](#questions-rest-only-for-now)), the held-question card and the
  security failure card. The supervisor watch card is in the Events tab. The badge on the tab
  counts what is waiting on you.
- **Resources:** the capacity and queue card (the pools and their usage, any
  usage back-off, budgets, and the Automata waiting for capacity), the lane fill
  card, the usage card (usage against the plan) and the Automata ship status
  card (the push, tag and release steps). The infrastructure retries and the
  Automata checks cards (the four report-only checks) are in the Events tab.
- **Progress and cost:** the Progress card, with the kind table, work left and
  completion range.
- **Setup and policy (removed):** its cards are on the Events tab (see
  [automata-events.md](automata-events.md#status-cards-on-the-events-tab)) and
  its configuration is in Settings under Automata.
- **Housekeeping:** the uncommitted work card. The safe-to-delete report and
  the cleanup card are in the Events tab.

### Reading the merge state and the safe-to-delete report

A finished Automaton can look done while its work is still only on its own branch. When the merge-pending check is turned on, the Automation view tells you which finished Automata still have commits that are not on `main`. The check is **off by default**; switch it on in Settings (**Automaton merge-pending check**: `off`, `shadow` or `on`). It only reads: nothing here merges, deletes or pushes.

**The merge badge.** On a finished Automaton's detail view, and on its Automation's card in the list, a badge appears when the Automaton needs you:

- **Automaton merge pending** (warning colour): the branch has commits that are not on `main`. With the check `on`, one alert is raised after the branch has waited for the **Automaton merge-pending alert** time (30 minutes unless you changed it; `0` turns the alert off).
- **Automaton needs a look** (error colour): the check could not tell whether the work landed. Hover or long-press the badge for the branch name and how many commits are not on `main`.

No badge means the Automaton is merged, has nothing ahead of `main`, or has not been compared yet. On an Automation card that has several members, the badge shows the member that most needs you, and needs a look wins over merge pending.

**Why "needs a look" and not "merge pending".** A commit whose patch changed while it was merged shows as needs a look, not as merge pending. The check recognises merged work by matching each commit's patch and its subject line against `main`. If a commit was merged but the patch that landed is not the patch on the branch (a conflict fixed by hand, an edit on the way in), the subject line matches and the patch does not, and the check will not guess either way. Treat the badge as a request: compare the branch and `main`, then merge it or clear it up yourself. Needs a look raises no alert.

**Trying it first with shadow mode.** Set the check to `shadow` and it records what it would have done (state, commits ahead, whether it would have alerted) without raising any alert or writing any merge-pending record. Read that history with `datawatch autonomous merge` or `GET /api/autonomous/merge` (`shadow_decisions`, newest first) and switch to `on` once the states match what you expect. The badge also appears in shadow mode, so you can see the states on your own branches before anything alerts.

**The safe-to-delete report.** On the Housekeeping sub-tab, the **Automaton safe-to-delete report** card lists two things. **Automaton branches safe to delete** are branches whose commits are all on `main` (or that have none) and whose worktree has nothing uncommitted. **Automaton branches held** must stay: still merge pending, needing a look, not comparable, or merged but with a worktree that has uncommitted changes (the card shows "The Automaton worktree has uncommitted changes."). Each row shows the branch, its worktree path, and why. The card refreshes every 60 seconds, wraps long branch names and paths on a phone, and shows **Could not load the Automaton safe-to-delete report** if the read fails (for example when no worktree base repository is configured). Where the report carries it, the card also shows when the report was computed, and a stale badge when the daemon told the reader the result was past its 60-second lifetime.

**The report never deletes anything.** The card has no delete, merge or push button, and no command acts on the report. It tells you what is safe; removing a branch or a worktree is a step you take yourself after checking it. The read is not slow each time: the daemon keeps the report in memory for 60 seconds per base repository, so the first read within a lifetime does the git work and the rest get the stored result at once. A read that misses the lifetime still returns at once (marked stale) and starts at most one background refresh, bounded at 60 seconds — further reads in the same window start none, and while that refresh is still running they keep serving the last successful result, so a slow or failing refresh never leaves the card blank or blocked; a failed refresh is counted in the daemon stats. The same two reads are available as `datawatch autonomous merge` and `datawatch autonomous safe-to-delete`, as the chat verbs `autonomous merge` and `autonomous safe-to-delete`, and over MCP as `autonomous_merge` and `autonomous_safe_to_delete`. The states, the alert and the config keys are described in the *Merge-pending check* section of [operations.md](../operations.md), and the response shapes in [the API reference](../api/autonomous.md#merge-state-and-safe-to-delete-report-get-apiautonomousmerge-get-apiautonomoussafe-to-delete).

### The empty state

When the list loads successfully and there are no Automations at all, the
sub-tab shows **No Automations yet**, a one-line explanation (an Automation chains
Automata; an Automaton is a single planned unit of work) and a **＋ Create
Automation** button that opens the same form as the round **New Automation**
button at the bottom right. It never appears while the list is still loading or
after a load that failed; a failed load shows the error and Retry instead. If
Automations exist but the filters hide them all, you see the plain "No
Automations yet" line without the button.

### Filtering

- **Status badges.** Draft, Active, Paused, Blocked, Completed and Cancelled.
  Tap a badge to show or hide Automations in that status. Everything except
  Cancelled is on by default.
- **History toggle.** The clock button shows or hides cancelled Automations
  without touching the badges ("Show cancelled" / "Hide cancelled").
- **Search.** Open the filter bar with the grid button and type part of a name
  or ID.

### What a card shows

| Line | Meaning |
|---|---|
| Name and status | The Automation's name and whether it is draft, active, paused, blocked, completed or cancelled |
| Progress | "N / M members": how many work members have completed or been skipped. Forks and joins steer the chain and are not counted |
| Running | The Automaton (or nested Automation) running now. With several branches active, one line per branch |
| Idle time | "Idle for …", shown on an active Automation that has no member running |
| Blocked reason | The reason from the most recent block, shown in red on a blocked Automation |
| Budget | The ceiling and window, for example "Ceiling: $20 · Window: Weekly". A ceiling set as a share of your allocation carries an **(estimated)** label |

Tap a card to open the Automation.

## The pipeline strip

The detail page draws the Automation as a strip of member cards joined by
connectors, starting at the entry member and following each member's
"on complete" edge.

- **Status dots.** Every member card has a dot: grey for pending, amber for
  approved, a pulsing dot for running, a tick for completed, yellow for
  blocked, red for failed.
- **Active ring.** Members that are running or are the current position of the
  chain are drawn with a ring. When several branches are live, each has its own
  ring.
- **Branch outcomes as sub-rows.** A member can say what happens when it is
  blocked or when it fails, as well as when it completes. Those extra outcomes
  are drawn as sub-rows below the main row, labelled "If blocked →" or
  "If failed →", and a ⑂ mark on the card tells you the member has more than
  one outcome. A member that already appeared earlier in the strip is shown as
  a short reference rather than drawn again, so a loop cannot run away.
- **Dashed untaken edges.** Once a member has finished, the connector it took is
  drawn solid and the connectors it did not take are dashed. Before the member
  finishes, all its connectors look the same.
- **Fork lanes.** A **fork** starts several members at once. Its branches are
  drawn side by side as lanes under the fork, between the fork and its join.
- **Joins and the join rule.** A **join** waits for the branches of its fork.
  Its **join rule** decides what a failed branch means:
  - *All branches must succeed*: one failed branch stops the join from
    succeeding.
  - *One branch is enough*: the join succeeds as soon as one branch does.
  - *Continue anyway*: the chain carries on after the branches finish,
    whatever happened to them.

  While a join is waiting, the detail page says "Join waiting: N of M branches
  finished".
- **Nested Automations.** A member can be a whole other Automation. It is drawn
  as one card with a **Show nested Automation** button; tap it to expand the
  nested chain as another level underneath, and **Hide nested Automation** to
  fold it away again.
- **Inserted markers.** A member that was added after the Automation was first
  built shows an **inserted** marker. Hover it to see who added it and why. The
  **Details** button on any card opens a drawer with its ID, kind, status,
  outcome, the edge it took, and start and finish times.

Below the strip the page lists any **pending inserts** waiting for approval and
the most recent **decisions** (what changed, who did it and when); the
lane fill decisions that decide which Automaton runs next now appear in the
Events tab as `decision` rows.

## Creating and editing

Create an Automation with the round button at the bottom right, or with
**＋ Create Automation** on the empty state; both open the same form. The same
form edits an existing Automation.

1. Give it a **name** and, if you like, a description.
2. **Add Automaton** to pick members. The picker lists your Automata and greys
   out the ones you cannot use, with the reason: it is a template, it is a
   child Automaton, it already belongs to another Automation, or it is already
   in this one.
3. **Linear wiring.** Members are chained in the order you add them: each
   member's "on complete" points at the next one and the last one stops. The
   first member is the entry member.
4. **Edge selects.** Each member also has "On complete", "On blocked" and "On
   failed" selects. Each lists the other members, plus **Stop and alert**. Change
   one to send a blocked or failed member somewhere else.
5. **Fork and join.** **Add fork and join** asks for at least two branch
   Automata and a join rule, then adds a fork, its lanes and the paired join
   into the chain.
6. **Nested.** **Add nested Automation** lets you pick another Automation to run
   as a single member. An Automation cannot contain itself, and one that is
   already inside another Automation cannot be nested again.
7. **Default models.** Under **Default models** you can set an execution,
   planning and verification backend, model and effort for the whole chain.
   Members that name their own backend keep it; the Automation default fills
   what they leave open.
8. **Budget policy.** Under **Budget policy** choose a ceiling type, a ceiling,
   a window (lifetime, daily, weekly or monthly), pacing (spend freely or pace
   evenly), what to do on breach (pause or block) and an optional backend. The
   ceiling must be above zero, and at most 100 when it is a percentage.
   **Share of allocation** is marked **estimated** in the form and on the
   card, because a share of a plan allocation is an estimate rather than an
   exact figure.
9. **Save.**

Only a **draft** lets you change its members in the form. Once an Automation has
started, the form shows the members as they are and asks for a **reason** for
the change; to change the chain itself, use the member buttons described below.

## Control buttons

The row of buttons above the strip depends on the status:

| Button | When it shows | What it does |
|---|---|---|
| **Pause** | Active | Pauses the chain so it starts nothing new until you resume |
| **Resume** | Paused or blocked | Carries on |
| **Cancel Automation** | Draft, active, paused or blocked | Ends the Automation |

An Automation is activated from the CLI, REST, MCP or chat; see
[automation-queue-advance.md](automation-queue-advance.md).

### Cancel always asks

Cancel opens a dialog with two choices and **neither is preselected**. You must
pick one before **Cancel Automation** will go through:

- **Stop advancing; let the running member finish.** Nothing new starts; work
  already running carries on to its end.
- **Also cancel the member that is running now.** The running work is stopped
  as well.

It always asks because cancelling cannot be undone and the two choices have very
different effects on work that is already in flight.

### Member buttons

Each member card carries its own buttons, and each dialog names the member it
acts on in its title.

- **Skip member.** Choose which outcome the skipped member takes (on complete,
  on blocked or on failed) and, optionally, a reason. The chain carries on along
  that edge. Available on a member that is pending, approved or running.
- **Insert before / Insert after.** Pick an Automaton to splice into the chain
  next to this member. A **reason is required**: the dialog will not continue
  without one, and the reason is kept on the new member's inserted marker. The
  **Insert urgently** box takes the next slot. The entry member has no slot
  before it once the Automation has started, so only **Insert after** is offered
  there.
- **Remove.** Takes out a member that has not started, after a confirmation.
- **Edit edges.** Reorder where the member's on complete, on blocked and on
  failed outcomes lead. Each select offers the other members or **Stop and
  alert**.
- **Approve insert.** An insert made by an agent or session can be held until
  you approve it, depending on the agent insert policy. Held inserts appear
  under **Pending inserts** with who asked and why; **Approve insert** lets one
  in.

### When several branches are active

With a fork running, several members are live at once. The control row then
shows an **Acting on branch** selector. Pause, resume and cancel always apply to
the **whole Automation**, and the row says so. When only one branch is live the
row simply names it. The member buttons never guess: each one acts on the card
it sits on, and its dialog title names that member.

## When someone else changed the Automation

Every change you make carries the version you loaded. If someone else (another
operator, an agent or the CLI) changed the Automation in the meantime, the
daemon refuses your change with a conflict (409) and the page shows **Out of
date: This Automation was changed elsewhere. Reload it before making changes.**
Tap **Reload** to load the current version, then make the change again. Nothing
is retried for you, so a change is never applied on top of something you have
not seen.

## Questions (REST only for now)

An Automaton can **raise a question** when it needs a decision it should not
make alone, and you, the operator, **answer it** the way you like best. For now
the question channel is on REST only: the in-PWA queue, the CLI subcommands,
the MCP tools and answering from a comm channel all come after and build on
exactly this surface, so the shapes below will not change under them.

### Turn the channel on

The channel is **off by default** behind the switch
`autonomous.questions.enabled`. While it is off, every endpoint under
`/api/questions` answers with a 503 that names the switch. Turn it on by
setting the switch to `true` — the PWA Settings toggle (labeled in English,
German, Spanish, French and Japanese), `configure` from chat, or REST:

```bash
env -u TMUX -u TMUX_TMPDIR rtk curl -sS -X PUT http://127.0.0.1:8787/api/config \
  -H 'Content-Type: application/json' \
  -d '{"autonomous.questions.enabled": true}'
```

The switch takes effect on the next request — no restart. Reads need the
`autonomous:read` capability, writes (raise, answer, withdraw) need
`autonomous:write`; local operators always pass.

### Raise a question

`POST /api/questions` raises one. A question carries a label, risk, a context
(`none` when it has no work of its own), its text, a set of options (2 to 20),
an optional recommendation, and a **required `default`** — the fallback option,
or the word `hold`. It is required precisely so that a question always has
some position attached and no choice is ever left silent.

```bash
env -u TMUX -u TMUX_TMPDIR rtk curl -sS -X POST http://127.0.0.1:8787/api/questions \
  -H 'Content-Type: application/json' \
  -d '{
    "label": "decision",
    "risk": "low",
    "context": "none",
    "text": "Should the retry keep using the free backend, or should it move to the paid one?",
    "options": [
      {"id": "stay-free",  "label": "Stay on the free backend"},
      {"id": "move-paid",  "label": "Move to the paid backend now"}
    ],
    "recommendation": {"id": "stay-free", "rationale": "The task is close to done and the free backend is idle"},
    "default": "stay-free"
  }'
```

The response is the stored question with its generated id and state `open`.

### Read them

```bash
# List, with filters: state, context, asker, text, limit
env -u TMUX -u TMUX_TMPDIR rtk curl -sS 'http://127.0.0.1:8787/api/questions?state=open&limit=50'

# One question (the id from the raise response — ids look like `q-0195c8e2a4b6d7f81c`)
env -u TMUX -u TMUX_TMPDIR rtk curl -sS http://127.0.0.1:8787/api/questions/q-0195c8e2a4b6d7f81c

# Its answer log, oldest first, showing which answer stands
env -u TMUX -u TMUX_TMPDIR rtk curl -sS http://127.0.0.1:8787/api/questions/q-0195c8e2a4b6d7f81c/answers
```

### Answer — option, words, or both

An answer is an **option**, your own **words** (the quote), **or both**.
Answers are **immutable**: nothing about the question can be edited after the
fact, and your quote is stored **verbatim, byte for byte** (no trims, no
normalization) with its SHA-256 over exactly those bytes, so any reader can
check later that it was never altered.

```bash
# Pick the option and add words
env -u TMUX -u TMUX_TMPDIR rtk curl -sS -X POST http://127.0.0.1:8787/api/questions/q-0195c8e2a4b6d7f81c/answer \
  -H 'Content-Type: application/json' \
  -d '{
    "chosen_option_id": "move-paid",
    "quote": "Move it — the deadline is tomorrow and the free backend has been thrashing",
    "surface": "rest"
  }'

# Words alone work too; so does the option alone.
```

### Change your mind — supersede, don't edit

A changed mind is a **new answer that names the one it supersedes**; the old
one stays in the log, marked as superseded. Superseding an answer that is not
the currently standing one is refused, as is answering a withdrawn question.

```bash
env -u TMUX -u TMUX_TMPDIR rtk curl -sS -X POST http://127.0.0.1:8787/api/questions/q-0195c8e2a4b6d7f81c/answer \
  -H 'Content-Type: application/json' \
  -d '{
    "chosen_option_id": "stay-free",
    "quote": "Actually, hold — the paid backend has the same backlog today",
    "supersedes": "a-0195c8e3b5c7e8a92d"
  }'
```

### Withdraw a question that never needed answering

```bash
env -u TMUX -u TMUX_TMPDIR rtk curl -sS -X POST http://127.0.0.1:8787/api/questions/q-0195c8e2a4b6d7f81c/withdraw \
  -H 'Content-Type: application/json' -d '{"reason": "The situation resolved while it was open"}'
```

### What the channel does not do

- **No clock expiry.** A question does not time out. It sits in the channel
  until it is answered or withdrawn; `expired` only ever appears as an
  explicit terminal move recorded in the log, never as a timer firing.
- **A secret answer is refused, not stored.** If your words look like a
  credential, the answer is refused with a 422 that names the two safe routes
  — put the value in the datawatch secrets store and reference it as
  `${secret:name}`, or in a session-facing file — and it is never stored, the
  audit line for the refusal never carries the text, and nothing of it is
  echoed back to you.
- **Every state change is audited.** Raise, answer, supersede, withdraw and
  expire each write one line to the single audit log under its own CEF name
  (`QuestionRaised`, `QuestionAnswered`, `AnswerSuperseded`,
  `QuestionWithdrawn`, `QuestionExpired`, `QuestionAnswerRefused`).

## Judge settings

A judge reads the work of an Automaton and decides whether it passes. When the
text it is given is very long, typing it into the judge's session is slow and can
fail. Two settings control how a long prompt reaches the judge. Settings >
General > Automata holds both, and each has a `?` entry that opens this section.

### What the file route does

When a judge prompt is longer than the size limit, datawatch writes the prompt to
a file in the Automaton's working directory and types only a short instruction
telling the judge to read that file. A shorter prompt is typed in full, as before.
The judge sees the same text either way.

- **Judge prompt file threshold** (`autonomous.judge_prompt_file_threshold_lines`):
  the size limit, in lines. The default is `300`. Set it to `0` to turn the file
  route off, so the full prompt is always typed in. Whole number, 0 to 100000.
- **Judge extra wait per 1000 prompt lines**
  (`autonomous.judge_delivery_wait_per_thousand_lines_seconds`): extra seconds a
  judge run waits for a long prompt to be delivered, added for every 1000 lines
  of the prompt. The default is `60` seconds. `0` adds no extra wait. Whole
  number, 0 to 3600.

A value outside the range, or one that is not a whole number, is refused with a
message and the saved value stays.

### See which outcome a judge had

When a judge does not pass an Automaton, the Automaton shows one of three
outcomes, in plain words, on its detail view:

- **Could not start** (`judge_could_not_start`): the prompt never reached the
  judge, so it never ran. This is an infrastructure problem, not a verdict on the
  work. Lower the file threshold or raise the delivery wait.
- **Timed out** (`judge_timed_out`): the judge ran but hit its time limit
  (`autonomous.judge_timeout_seconds`).
- **Found a problem** (`judge_found_problem`): the judge ran and blocked the work
  with real findings to fix.

The same outcome is the `judge_outcome` and `judge_outcome_text` fields of the
Automaton's review state over REST and MCP, and each one is written to the audit
log as `autonomous_judge_outcome`.

### Tune both settings

- **Settings page:** Settings > General > Automata, the two fields above.
- **CLI:** `datawatch config set autonomous.judge_prompt_file_threshold_lines 300`
  and `datawatch config set autonomous.judge_delivery_wait_per_thousand_lines_seconds 60`.
- **Comm channel:** `configure autonomous.judge_prompt_file_threshold_lines=300`,
  or `autonomous judge-policy-set judge_prompt_file_threshold_lines=300
  judge_delivery_wait_per_thousand_lines_seconds=60`.
- **REST and MCP:** `PUT /api/config`, `PUT /api/autonomous/config` and the
  `autonomous_config_set` tool take the same two keys.

## Where the moved cards went

These cards now live in the **Events** tab of the Automata view, as rows of
their event kind — each still has its own **?** link that opens the howto
for it:

| Card | Where it is now |
|---|---|
| Supervisor watch | `watch` rows in [Events](automata-events.md) |
| Recent decisions (lane fill) | `decision` rows in [Events](automata-events.md) |
| Infrastructure retries | `retry` rows in [Events](automata-events.md) |
| Automata checks | `check` rows in [Events](automata-events.md) |
| Safe-to-delete report | `cleanup` rows in [Events](automata-events.md) |
| Cleanup status | the `cleanup` status row in [Events](automata-events.md) |

Their reads over REST, MCP, CLI and chat are unchanged; the Events tab reads
the same state as one stream. The repo CI watch's CI line is a `watch` row
there too.

## Related

- [projects-tab.md](projects-tab.md): the Projects tab, a separate top-level
  tab that groups an Automation under its project as an arc, with the project's
  Automata, sessions, progress, history and policy on one page
- [Triage records](../api/autonomous.md#triage-records-get-api-autonomoustriage): the record of every failed or blocked
  Automaton in your Automations — how the pass fills, refreshes and clears
  it, the single alert it can raise, and where to read it (REST, MCP, CLI,
  comm, this detail view); the operator reference is the *Triage records*
  section of [operations.md](../operations.md)
- [Merge state and safe-to-delete report](../api/autonomous.md#merge-state-and-safe-to-delete-report-get-apiautonomousmerge-get-apiautonomoussafe-to-delete):
  whether a finished Automaton's branch has reached `main`, the five states, shadow
  mode and the one alert; the operator reference is the *Merge-pending check*
  section of [operations.md](../operations.md)
- [automation-queue-advance.md](automation-queue-advance.md): activating an
  Automation, automatic advance, idle alert, model defaults and the
  all-channels table
- [dashboard.md](dashboard.md): the fleet view, including the sprint pipeline
  for running Automata
- [autonomous-planning.md](autonomous-planning.md): how single Automata are
  created and decomposed
- [automata-events.md](automata-events.md): the Events tab — the one stream of
  watch, decisions, retries, checks, cleanup and errors, with the cards that
  moved there
- [datawatch-definitions](../datawatch-definitions.md#automation): the Automation
  section of the manual

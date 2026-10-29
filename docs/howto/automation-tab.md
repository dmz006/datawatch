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
Automation sub-tab, a second row of sub-tabs splits the view: **Overview**, with
the Progress card, the Capacity and queue card, and the other summary cards that
sit with them; and **Chains**, with the status badges, the search and filter
bar, the list of Automations, and the open Automation with its pipeline strip
and controls. Chains is the one open by default. A **Questions** sub-tab will
join the second row when the question queue lands; no Questions sub-tab exists
today.

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
  Automation sub-tab, on its Chains sub-tab. Tap **Automata** or **Templates**
  on the sub-tab bar, or **Overview** or **Chains** on the second row under it,
  to switch. There is no New Automation button in the bar: an Automation is
  created with the round button at the bottom right, or with **＋ Create
  Automation** on the empty state, both opening the same form. The action
  buttons at the right of the bar (select, filter, history, watch) change with
  the sub-tab, and so does the round button; on the Automation sub-tab, filter
  and history show on Chains, because both act on the list.
- **Remembered sub-tab.** While the web app stays open, the sub-tab you chose
  stays selected when you leave the Automata tab and come back. The same rule
  holds on the second row: the sub-tab you chose is kept while the page stays
  open, and a fresh load of the page starts the view on Chains again. Storing a
  chosen sub-tab across loads is switched off until the operator decides how it
  should behave.
- **Saved links.** A link to `#automation` opens the Automata tab on the
  Automation sub-tab, on Chains, and shows its list. `#automation/<id>` and
  `?automation=<id>` land the same way and open that Automation. Older links and
  history entries that name the Automation view land in the same place, and the
  back control on a member's Automaton page returns to the Automation that holds
  it, on Chains.
- **Subsystem off.** When the autonomous subsystem is not enabled, the
  Automation sub-tab is not offered and every one of these links lands on the
  Automata sub-tab.
- **Phone width.** On a narrow screen the two rows of sub-tabs fit at 360 px
  without scrolling or clipping, and every sub-tab in both rows is at least
  40 px tall and wide enough to tap.

## Base requirements

- A running daemon with `autonomous.enabled: true`.
- At least one approved Automaton that is not already part of another
  Automation, to have something to put in a chain.

## The list

On the Chains sub-tab, one card per Automation, ordered by status (active,
blocked, paused, draft, completed, cancelled) and then by name.

### The needs-attention card

On Chains, between the filter bar and the list, sits the needs-attention card:
one card per Automaton that needs an operator because it failed or is blocked,
with the reason it needs attention. While nothing is stuck it shows a one-line
empty note. See the *Triage records* entry under [Related](#related) for the
record itself and how it fills and clears.

### The Overview cards

On the Overview sub-tab sit the summary cards: the Progress card, the same
capacity and queue summary card the Automata sub-tab shows (the pools and their
usage, any usage back-off, budgets, and the Automata waiting for capacity), and
the other summary cards that sit with them. Each card loads with its own
loading skeleton and, if the request fails, its own error with a Retry button;
a failed card never blanks the list, and a failed list never blanks the cards.

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
the most recent **decisions** (what changed, who did it and when).

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

## Related

- [Triage records](../api/autonomous.md#triage-records-get-api-autonomoustriage): the record of every failed or blocked
  Automaton in your Automations — how the pass fills, refreshes and clears
  it, the single alert it can raise, and where to read it (REST, MCP, CLI,
  comm, this detail view); the operator reference is the *Triage records*
  section of [operations.md](../operations.md)
- [automation-queue-advance.md](automation-queue-advance.md): activating an
  Automation, automatic advance, idle alert, model defaults and the
  all-channels table
- [dashboard.md](dashboard.md): the fleet view, including the sprint pipeline
  for running Automata
- [autonomous-planning.md](autonomous-planning.md): how single Automata are
  created and decomposed
- [datawatch-definitions](../datawatch-definitions.md#automation): the Automation
  section of the manual

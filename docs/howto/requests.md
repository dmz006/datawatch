---
docs:
  index: true
  topics: [delivery, requests, operator, autonomous, messaging]
exec_steps:
  - tool: request_list
    description: List the requests with their states, reason codes and plain readings; no request content
    args: {}
    read_only: true
  - tool: delivery_status
    description: Show the delivery list; requests that failed, expired, are overdue or wait on you are listed there with identifiers and reason codes only
    args: {}
    read_only: true
---

# Requests: work that is not lost between sessions

A **request** is one saved thing that must happen, addressed to a **role** and
not to a session. A session holds the role for a while. If it ends, datawatch
decides what may go to the next holder and lists the rest for you.

## What this does not cover

- **Do not use pipelines or graphs for work that must not be lost.** They are
  not requests. An interrupted pipeline is only listed.
- **This computer only.** A request to or from another datawatch computer is
  refused and nothing is saved.
- **Containers, opencode and other workers** have no proven way to receive a
  request (see [Workers](#workers)).
- **No commands are run.** A request's "done when" list cannot run a test or a
  script.
- **Delivery is not work.** A confirmed delivery does not show that the model
  paid attention, understood, or did the work correctly.

## Who gets it

- One session holds a role in a scope. Today the role you can give is the
  **supervisor of a project**.
- Taking a held role is refused while its holder is alive, unless you force it
  with a reason code. A session that gives itself a role is recorded as having
  done so.
- A new holder first gets a **hello**, a request with no work in it. Until the
  hello counts for that session, it is given no work that must be delivered,
  and that work is listed as `take_not_proven`.
- The proof is per session. It carries to a second role while the session holds
  one. A session that holds no role any more (released, or ended) proves it
  again at its next one.

## Workers

| Worker | What counts as delivery | Reminders |
|---|---|---|
| Claude Code | the session's own take | a typed line with a number only |
| pi | only a report from datawatch's pi extension that the exact text was in a completed model call. **Not switched on yet** | none: pi is never typed into |
| anything else | nothing | the request's text is typed **once**; it reads "typed, unconfirmed" |

### Reminders

A reminder is one line that says how many requests wait. It holds no request
text. It is saved before it is typed.

- At most `delivery.reminders_per_session_hour` (2) to one session and
  `delivery.reminders_per_host_hour` (20) on this computer each hour, and no
  more often than `delivery.reminder_interval_seconds`. `0` means no
  reminders. The counts survive a restart.
- A request that was fetched and not accepted gets at most 2 reminders, then it
  is only listed. A request nobody fetches has only the hourly limits.
- Nothing is typed while datawatch is paused, held for usage or the session is
  rate limited. When the session is at a permission or confirmation prompt,
  nothing is typed and the try does not count.

### Typed once

A worker with no proven take gets the request's text typed one time, as one
line. It is never typed again, also after a restart. If datawatch cannot tell
whether the keys arrived, it counts as typed. Only independent proof closes it.

datawatch does **not** type request text into a shell session, a one-shot run,
a chat-mode or HTTP conversation backend, a pi session, or where it cannot see
that something other than the pane's shell reads the keyboard. Such a request
stays waiting and is listed.

The session's tracker and the session record say only `request <id> typed`. The
terminal still shows what was typed, so **the pane's own output log holds the
text**, and so does whatever the worker itself saves.

### pi

**pi cannot receive requests yet.** datawatch writes the extension and a private
credential file for each pi session, but no launch loads the extension, because
two things are not decided: when a busy pi is handed a request, and what its
model is told about such messages. A request to a pi holder stays waiting and is
listed.

When it is on:

- The extension fetches with the session's own credential, puts the text into
  the conversation, and reports "seen" only after the exact text was in a model
  call that the model server answered and that ended normally.
- **A pi confirmation shows datawatch's pi extension saw the text in a completed
  model call.** It does not show that the model heeded it. Until 11.0 datawatch
  cannot prove that the extension, not other use of that session's credential,
  sent the report.
- A model can accept a request with the extension's `datawatch_ack` tool, using
  a code that is only in the text. It is extra evidence, never the delivery.
- Work that was fetched but not confirmed is not put into a conversation again
  by itself. It is listed.
- **pi has no way to report finished work yet**, so pi work stays "accepted",
  becomes overdue and is listed to you.

The model's behaviour is the weak part: in the proof trials, a model handed a
request in the middle of a task sometimes made up further requests and acted on
them. datawatch refuses unknown request IDs; it cannot stop what the model does
with its own tools.

## Done

"Done" is the request's [done-when list](done-when.md) reading done. Nothing a
doer says makes its own request done. There is no second proof engine.

- The list is made and fixed **before** the request is delivered.
- Lists must be on (`acceptance.mode: report`). With them off, requests are
  saved but **never close**, and are listed as `lists_off`. A request made
  while lists were off has no list and stays `no_list`.
- An item is checked by `receipt`, `state:file`, `confirm` or `review`. No
  reviewer is wired yet, so a `review` item reads "cannot tell".
- A `state:file` item must show a **change**: a file that was already right when
  the list was made proves nothing. The change must be **committed** in the
  request's project folder. Set `effect` to `work` (the file is the result) or
  `ref:<branch>` (it must be on that branch); without an effect a file the doer
  can write is only its claim.
- A list for work reads **not reviewed**, not done, until somebody other than
  the doer confirms that the list covers the whole ask.
- The holder's own answer or waiver is kept as a claim and closes nothing. So is
  an answer sent with the daemon token that names the holder as its session.
- When a request gets a new holder, the list is told the work was replaced:
  earlier answers and receipts stop counting and the earlier holder stays a doer.
- A hello, an answer and a rule bundle close on a counted receipt and read
  **delivered (not the same as done)**.
- A `receipt` item on a list for work needs a named recipient who is not the
  doer. Only the holder fetches a request, so such an item is not met today.
- A failed check sends the work back, up to the list's rework limit. Each round
  needs a changed version of what failed. Without one, or past the limit, the
  request fails and is listed.
- **Accept unproven** records the open items as accepted without proof, with
  your words. The list's other rules still apply: it needs the coverage
  confirmation, and then reads "done, accepted without proof".

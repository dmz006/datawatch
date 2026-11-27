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
| pi | only a report from datawatch's pi extension that the exact text was in a completed model call. Interactive pi only | none: pi is never typed into |
| anything else | nothing | the request's text is typed **once**; it reads "typed, unconfirmed" |

### Reminders

A reminder is one line that says how many requests wait. It holds no request
text. It is saved before it is typed.

- At most `delivery.reminders_per_session_hour` (2) to one session and
  `delivery.reminders_per_host_hour` (20) on this computer each hour, and no
  more often than `delivery.reminder_interval_seconds` (15 minutes). A request
  [typed once](#typed-once) in full counts toward the same two limits. `0` types
  neither. The counts survive a restart.
- A request that was fetched and not accepted gets at most 2 reminders, then it
  is only listed.
- A request nobody fetched gets no more reminders once its take-by time (1 hour)
  has passed; it is listed. This is also so for a holder who gets it after that
  time, for example a role given later: `datawatch request mine` and
  `request_mine` still show it.
- Nothing is typed while datawatch is paused, held for usage or the session is
  rate limited. When the session is at a permission or confirmation prompt,
  nothing is typed and the try does not count.

### Typed once

This applies to a request addressed straight to one session. A **role** held by
a worker with no proven take is given no work: its hello never counts, so the
work stays listed as `take_not_proven`.

A worker with no proven take gets such a request's text typed one time, as one
line, within the two hourly limits above. It is never typed again, also after a
restart. If datawatch cannot tell
whether the keys arrived, it counts as typed. Only independent proof closes it.

datawatch does **not** type request text into a shell session, a one-shot run,
a chat-mode or HTTP conversation backend, a pi session, or where it cannot see
that something other than the pane's shell reads the keyboard. Such a request
stays waiting and is listed.

The session's tracker and the session record say only `request <id> typed`. The
terminal still shows what was typed, so **the pane's own output log holds the
text**, and so does whatever the worker itself saves.

### pi

An interactive pi session (the `pi` backend) gets requests through datawatch's
pi extension, which datawatch writes and the pi launch loads with `-e`. One-shot
pi (`pi-prompt`) does not.

**A busy pi gets a request after its current task**, not in the middle of it,
with a short note in its instructions that such messages are real. A long task
makes the request wait: it reads "fetched, unconfirmed", and past its take-by
time it is listed.

- The extension fetches with the session's own credential, puts the text into
  the conversation, and reports "seen" only after the exact text was in a model
  call that the model server answered and that ended normally.
- **A pi confirmation shows datawatch's pi extension saw the text in a completed
  model call.** It does not show that the model heeded it. Until 11.0 datawatch
  cannot prove that the extension, not other use of that session's credential,
  sent the report.
- The model accepts a request with the extension's `datawatch_ack` tool, using a
  code that is only in the text. It is extra evidence, never the delivery.
- When it has finished, it calls `datawatch_report`, as Claude Code calls
  `request_report`. **A report only starts datawatch's check. It never marks a
  request done.** A hello needs no report.
- Work that was fetched but not confirmed is not put into a conversation again
  by itself. It is listed.
- pi keeps its own conversation files, which hold the text of a request given to
  it. datawatch does not seal them.
- This was proven with pi 1.3.0 and one local model through Ollama. Other pi
  versions, providers and models are not proven.

The model's behaviour is the weak part. In earlier trials, a model handed a
request in the middle of a task sometimes made up further requests and acted on
them, which is why delivery waits for the task to end. A model can also miscopy
an ID. datawatch refuses unknown request IDs; it cannot stop what the model does
with its own tools.

## Times

A request has 1 hour to be picked up (`delivery.request_take_by_seconds`) and 24
hours to be finished (`delivery.request_due_seconds`). A hello has 1 hour;
`delivery.request_due_seconds_by_kind` sets any kind's own time.

## If the request storage is damaged

This should be very rare. Damaged request storage never stops datawatch from
starting. At the next start datawatch repairs what it safely can:

| Damage | What datawatch does |
|---|---|
| the manifest (`queue.d/meta.rec`) is damaged or missing | rebuilds it from the intact record files |
| on a secure install, a record's readable index differs from its sealed copy | restores it from the sealed copy |
| a record cannot be believed | sets it aside; the others work |
| a record is gone from the disk | reports it; it is not repaired |
| the key opens none of the sealed files, a newer datawatch wrote them, or the originals cannot be copied | changes nothing; delivery requests are off |
| on a secure start, `queue.d/store.key` is wrong, damaged or missing | changes nothing; delivery requests **and the delivery check** are off |

- Every file as it was is copied to `queue.d/repair-<time>/originals`, and
  records set aside are moved to its `set-aside` folder. datawatch never deletes
  that folder; remove it yourself when you no longer need it.
- A repair never marks anything done. A request rebuilt while it was open, and
  one that waited for a record that could not be kept, reads **failed** with the
  reason `repaired`. It is not run again until you reissue it (say it may run
  twice) or drop it. A hello, an answer or a rule bundle can only be dropped.
- You are told what was repaired and what was not: a system alert, the daemon
  log and, when the delivery check runs, the delivery list.

Limits of the repair:

- It runs only when the daemon starts.
- Role holders and reminder counts are kept only in the manifest. A rebuild
  loses them: give the roles again.
- A rebuild takes each record as its file has it. A change that was only in the
  damaged manifest is lost, and an older copy of a file cannot be told from the
  current one. Items of the older work queue are not held: they read as their
  files do.
- A plain install has no sealed copy, so nothing can be restored there, and an
  edited record cannot be told from a good one after the manifest is damaged.
- While requests are off, the older work queue is unavailable too if it had
  already moved into `queue.d`.
- Starting a secure install without secure mode is still refused: that is the
  wrong mode, not damage.

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
  **delivered (not the same as done)**. Because they close on receipt, only
  datawatch itself creates them: create and reissue refuse them on every
  surface.
- A `receipt` item on a list for work needs a named recipient who is not the
  doer. Only the holder fetches a request, so such an item is not met today.
- A failed check sends the work back, up to the list's rework limit. Each round
  needs a changed version of what failed. Without one, or past the limit, the
  request fails and is listed.
- **Accept unproven** records the open items as accepted without proof, with
  your words. The list's other rules still apply: it needs the coverage
  confirmation, and then reads "done, accepted without proof".

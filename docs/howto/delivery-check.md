---
docs:
  index: true
  topics: [delivery, operator, autonomous, schedules, messaging, monitoring]
exec_steps:
  - tool: delivery_status
    description: Show the current delivery report (result, why it is red, new, backlog and accepted items) without running a check
    args: {}
    read_only: true
  - tool: delivery_check
    description: Run the delivery check now and show its report; the check blocks nothing, but it may send one message about listed items
    args: {}
---

# Delivery check: one list, one check, you are told

The delivery check keeps **one list** of the work that was not delivered or is
not finished, checks it every few minutes, and sends you a message about what
is on it.

**It only reports.** A red result blocks nothing: no pause, start, resume or
merge looks at it. It never types into a session. Whether a red result should
block anything is not decided.

The list and the messages hold identifiers, kinds, states, reason codes and
times only. They never hold titles, specs, prompts, commands or a message
channel's error text.

It reads the records datawatch already has. It does not keep a second copy of
them.

## Listed and told are different

- **Listed** means the item is on the list.
- **Told** means a message channel accepted a send that named the item. It does
  not show that you read it.
- **Acknowledged** means it was marked as seen, on any surface.

## What is listed

An item is listed when its record shows a problem, or when it has stayed as it
is for longer than its limit.

| What | Listed when |
|---|---|
| An Automaton being planned | longer than 2 hours |
| An Automaton awaiting review or approved and not started, with revisions asked for, blocked, failed or paused | longer than 24 hours; blocked with no reason: at once |
| A draft Automaton | older than 7 days |
| A held Automaton | at once if it has no reason or no review date, if the date has passed, or if the date is more than 30 days away |
| A running Automaton or task | no live session and no recorded wait (after two check intervals), or a recorded wait older than 24 hours |
| A task that failed, is blocked, needs review or awaits approval | longer than 24 hours |
| A bug, or a bug filed in a batch | older than 7 days while nothing is working on it; it stays open until its fix is on the main branch or you drop it |
| Completed work that is not on the main branch, and branches that need a decision | longer than 24 hours |
| A schedule | at once if it can never fire, failed, was cancelled by datawatch, or missed its time (all missed times are one item); a day if it waits for input or for a session name |
| A session of automated work waiting for input | longer than 1 hour |
| A question, a pending item | a question after 24 hours, a pending item after 7 days |
| An answer or one-time message that did not arrive | longer than 1 hour, or at once when it needs review |
| An interrupted pipeline, a deferred deploy, an output filter whose typed send failed | at once |

A record that cannot be read turns the check red. It is never treated as
"nothing there".

## New, backlog and accepted

- **Backlog** is everything that was already wrong when the check first read
  each kind of record. It is listed for you to review once. It is **never**
  sent as a message and never turned into an action.
- **New** items came after that start mark.
- **Accepted** items wait until a date you chose, at most 30 days ahead (a date
  without a time means the end of that day, UTC, so at most 29 days after
  today). When
  the date passes they are listed as new, and you are told again.

Two guards stop a storm. If more than 20 items would run out of time in one
check, none of them does, and one alarm is listed; acknowledge the alarm to let
them through. At most 50 new items are listed by one check; the rest raise one
alarm, and **no further new item is listed until you acknowledge it**.

While items wait behind an alarm they are counted and not named in any message,
and the heartbeat is `red`, which the outside timer does not report. A failure
whose record goes away while it waits is never listed. So acknowledge an alarm
soon. Acceptances that reach their date are all listed again at once: the guard
does not hold them back.

## Green and red

The check is **green** only when nothing unaccepted is listed and the check
itself is healthy. The report says why it is red, for example: items need you,
records cannot be read or saved, the start mark is missing, no message channel
can reach you, every channel refused the last message, an item has waited more
than 15 minutes with no accepted message in 24 hours and no acknowledgement, or
the check has not run on time.

The report also prints what the check does **not** cover (for example work on
other hosts). Green does not mean those are fine.

## How you are told

- At most **one message per check**, sent on every message channel that can
  reach you. One channel accepting it is enough for the items to count as told.
- New items go first, oldest first. A message has at most **20 item lines** and
  at most 1500 characters, so that it fits the channel with the smallest limit.
  Only the items that have a line are marked as told; the rest follow in later
  messages, and the message says how many there are.
- **A failed send is tried again** after 1 minute, then 2, 4, 8, 16 and 32, and
  then every hour. Each item keeps its last 20 send results, as a channel name
  and a code: `timeout`, `send_failed`, `cancelled` or `channel_gone`.
- A send that does not answer within 45 seconds counts as failed (`timeout`).
  No second send is started on that channel until the first one returns. If a
  late send gets through after all, you can receive the same item again.
- **A reminder once a day** names items you were told about and have not
  acknowledged, those longest unnamed first, at most 20. Only the items it
  names count as sent. With more than 20 such items, each one is named every
  few days, and until it is, the check counts it as not told.
- An item gets **no more messages** once you acknowledge it, while it is
  accepted, or when it is closed. Backlog items are never sent.
- If the records cannot be saved or read, one fixed warning is sent from memory
  and nothing else: once for each fault. Until a channel accepts it, it is tried
  again after 1 minute, doubling to once an hour.

### Which channels can reach you

An ntfy send counts only when the ntfy server itself answers with success. A
redirect, for example to a sign-in page, is a failed send. This applies to
every ntfy message datawatch sends: set the server address to where ntfy
answers directly.

| Can tell | Cannot tell |
|---|---|
| Signal, Telegram, Discord, Slack, Matrix, Twilio SMS, ntfy, email, imap-mcp email | the generic webhook, the GitHub webhook and the DNS channel: they only receive, and a send on them succeeds without sending anything |

imap-mcp email counts only when its `to` recipient is set.

If no channel that can reach you is set up, or `delivery.notify` is off, the
check is **red** and its heartbeat says `cannot_tell`.

Use **Send test message** to see what each channel answers. An accepted send
shows that the channel took the message. Look on your device to see that it
arrived. The test message is limited to one a minute and 6 an hour, and refused
while `delivery.notify` is off. The limit starts over when the daemon restarts.

## Surfaces

| Action | CLI | REST | MCP | Chat | Web app |
|---|---|---|---|---|---|
| Current report | `datawatch delivery status` | `GET /api/delivery` | `delivery_status` | `delivery` | Settings → Automata → **Delivery check** |
| Run the check now | `datawatch delivery check` (exit 1 when not green) | `POST /api/delivery/check` | `delivery_check` | `delivery check` | **Check now** |
| Acknowledge | `datawatch delivery ack --key <key>` | `POST /api/delivery/ack` | `delivery_ack` | `delivery ack <key…>` or `delivery ack backlog` | **Acknowledge** |
| Accept until a date | `datawatch delivery accept --key <key> --reason <code> --until <date>` | `POST /api/delivery/accept` | `delivery_accept` | `delivery accept <key…> reason=<code> until=<date>` | **Accept until date** |
| Drop | `datawatch delivery drop --key <key> --reason <code>` | `POST /api/delivery/drop` | `delivery_drop` | `delivery drop <key…> reason=<code>` | **Drop** |
| Test message | `datawatch delivery test-message` | `POST /api/delivery/test-message` | `delivery_test_message` | `delivery test` | **Send test message** |

An item's **key** is `<kind>:<id>:<state>`, for example
`automaton:1a2b3c4d:blocked`. Besides keys, the command line takes `--kind`,
`--state`, `--match-reason` and `--backlog`; each one you add narrows the
choice, and at least one is needed.

- **Acknowledge** stops the messages. The item stays listed. An alarm or a
  clock-jump notice closes when you acknowledge it.
- **Alarms and clock-jump notices can only be acknowledged.** Accept and drop
  refuse any choice that includes one, however it is named, and change nothing:
  accepting an alarm would hide what waits behind it. Alarm keys look like
  `alarm:flood:raised`.
- **Accept** needs a reason **code** (lower-case letters, digits, dot, dash and
  underscore, up to 64) and a date in the future, at most 30 days ahead. A date
  without a time means the end of that day in UTC, so the latest such date is 29
  days after today; today plus 30 is refused. You can
  accept a whole group with one reason and one date.
- **Drop** closes **failed** and **backlog** items only. Work that is only stuck
  cannot be dropped off the list; accept it with a date instead.

### Who may act

The rule is that only you acknowledge, accept and drop. **It is not
enforced**: AI sessions may acknowledge, accept, drop and send test messages.

> Known, also in earlier code: a credential with sessions:list can use the chat
> test route with operator access; permissions and recorded actor names cannot
> be relied on.

## Settings

```yaml
delivery:
  enabled: true           # default: keep the list and run the check
  notify: true            # default: send messages about listed items
  interval_seconds: 300   # default 300, 60 to 86400
```

In the web app: Settings → Automata → **Delivery check settings**. `enabled` and
`interval_seconds` are read when the daemon starts; restart it after changing
them.

With `enabled: false` there is no list, no heartbeat, and the delivery routes
answer "the delivery check is not available". With `notify: false` the list is
kept, nothing is sent, and the check is red with `cannot_tell`. Do not install
the outside timer in either case: it would alert for as long as the setting is
off.

## Records and secure mode

The list and the start mark are kept in the `delivery` folder of the data
folder. On a secure install they are sealed with a key of their own, kept
separately from the configuration file's key. The heartbeat file is never
sealed: it holds a time, a state and counts, so the outside timer can read it
without a password.

The key's salt, settings and a sealed test value are in
`<data dir>/queue.d/store.key`. The key itself comes from your secure-mode
password. The file is written once, on the first secure start where the check is
on and every other store opened. Records that were plain before secure mode was
turned on are sealed at that start.

**datawatch does not start**, and says why, when:

- the password does not open `store.key`, or the file is damaged;
- `store.key` is there and datawatch is started without secure mode;
- `store.key` is missing while sealed delivery records exist. It is never made
  again, because a new key could not open them. Restore the file.

Keep `store.key` in your backups of the data folder. Changing a setting does not
change this key.

If the list or the start mark is damaged or has missing parts, the start mark
is missing, or the list file is gone while a heartbeat shows one was saved, the
check does nothing, changes nothing and is red. Restore the files. Removing the
whole `delivery` folder starts the check over: everything then wrong becomes a
new backlog, and acknowledgements and acceptances are lost.

These refusals to start apply **also with `delivery.enabled: false`**. To leave
secure mode on purpose, stop datawatch and remove both `queue.d/store.key` and
the `delivery` folder; removing only the key still refuses. That ends this
refusal only. The other sealed stores have their own rules.

If datawatch stops during the first secure start, after the key is written and
before the older plain records are sealed, the next start seals them.

A failed output-filter send that cannot be saved is held in memory and saved
once the records work again; the check stays red until then. At most 200 are
held, and an alarm says how many more were lost. A daemon restart before they
are saved loses them.

## The outside timer

datawatch cannot tell you that it is dead. After each completed check that it
could save, it writes `<data dir>/delivery/heartbeat.json`.
`scripts/delivery-outside-timer.sh` runs on its own and reports when that file:

| Reason | Meaning |
|---|---|
| `heartbeat_missing` | the file is not there |
| `heartbeat_unreadable` | it cannot be read or is not a whole heartbeat |
| `heartbeat_stale` | it is older than two check intervals plus 60 seconds |
| `heartbeat_in_future` | it is dated that far ahead of the clock, so its age cannot be judged |
| `cannot_tell` | datawatch itself says it cannot tell you anything |

A fresh `ok` or `red` is not reported: datawatch can tell you itself. The
script reads that one file, writes nothing in the data folder, and needs only
`bash` and the core utilities.

**It has no message route yet.** How the timer reaches you is not decided. As
shipped, when there is something to report it prints it and exits 1, so the
unit shows as failed in `systemctl --user status` and the journal. **Until a
route is added, a dead datawatch reaches nobody.**

### Where the route plugs in

One function in the script, `delivery_timer_send_alert`, under the comment
**MESSAGE ROUTE: THIS FUNCTION IS WHERE IT PLUGS IN**. A route:

- gets the whole message as its first argument;
- returns 0 **only** when the message was accepted for delivery;
- must not depend on datawatch, which may be dead;
- must give up on its own (for `curl`, set `--max-time`);
- keeps its credential outside the datawatch data folder, readable by you only.

A delivered report is not repeated for six hours while the reason stays the
same (`DELIVERY_TIMER_REPEAT_SECONDS`). A changed reason, or a failure after a
healthy run, is reported at once. A report that was not delivered is tried again
at every run. The note of the last delivered one is kept in
`~/.local/state/datawatch-delivery-timer` (`DELIVERY_TIMER_STATE_DIR`).

### Install

The script and its units are shipped but **not installed**. From the source
folder, as the user datawatch runs as:

```bash
install -D -m 0755 scripts/delivery-outside-timer.sh ~/.local/bin/datawatch-delivery-outside-timer
install -D -m 0644 deploy/systemd/datawatch-delivery-timer.service ~/.config/systemd/user/datawatch-delivery-timer.service
install -D -m 0644 deploy/systemd/datawatch-delivery-timer.timer ~/.config/systemd/user/datawatch-delivery-timer.timer
systemctl --user daemon-reload
systemctl --user enable --now datawatch-delivery-timer.timer
loginctl enable-linger "$USER"   # so it runs when you are not logged in
```

If your data folder is not `~/.datawatch`, change the argument in the
`ExecStart=` line of the service unit.

To see what it would report now:
`scripts/delivery-outside-timer.sh ~/.datawatch; echo $?`

The timer runs on the same host. It cannot tell you that the host is down.

## Related

- [Pause new work](pause-new-work.md): a pause is not affected by the check.
- [Schedules: re-arming after a failed start](schedule-rearm.md)
- [Holding an Automaton](automaton-hold.md): give a hold a reason and a review date.

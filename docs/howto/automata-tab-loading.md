---
docs:
  index: true
  topics: [automata, pwa, loading, performance]
---
# How-to: how the Automata tab loads

The **Automata** tab in the web app loads a short, paged summary of your
Automata instead of every record in full. A long-running daemon can hold
thousands of Automata with a lot of story and task text each; the list only
needs enough to draw one card per Automaton, so that is all the tab asks for
first. This page explains what you see while it loads, what **Load more** does,
what happens when you open a row, and how the tab behaves when the connection
drops and comes back.

For the API behind it (`limit`, `cursor`, ETag / 304, gzip) see
[list-endpoints-and-paging.md](list-endpoints-and-paging.md). For the chains
tab next to it see [automation-tab.md](automation-tab.md).

## What loads when you open the tab

Opening the Automata tab requests one **summary page of 50 rows**, newest
activity first. A summary row carries the identity of the Automaton (id,
title, status, revision), its story and task counts, its status counts, cost
totals, tags, type, backend, priority and its parent, but none of the story or
task text. The size of a row does not depend on how much text an Automaton
holds, so the first page stays small even when one Automaton is many
megabytes in full.

By default the tab shows the active Automata. The status filters, the search
box and the history toggle narrow the list on the server, so a filtered tab
also fetches only one page of matching rows.

Next to the list the tab loads the capacity and queue card, the list of LLM
backends and the tag list. Each one has its own request and its own error
handling, so a failure in one never blanks the list.

## Load more

When more Automata match than the 50 shown, a **Load more** button appears
under the last card with the count, for example **Load more (50/273)**.

- Tap it to fetch the next 50 rows. The button shows **Loading…** and is
  disabled while the request runs.
- Rows already on screen stay where they are; the new rows are added below.
  A row that arrived twice is shown once.
- If the request fails, the rows you have stay, and the message **Could not
  load this list** with the reason appears beside the button. Tap **Load
  more** again to retry from the same place.
- The button disappears when there is nothing left to load.

The Sessions and Alerts tabs page the same way and use the same control.

## Loading, cached, stale and failed

The tab always shows something and tells you where the data came from.

| You see | It means |
|---|---|
| Three grey placeholder rows and **Loading…** | First load, with nothing saved on this device yet. |
| The list with a **Cached** label and **Last updated N s ago**, and a small **Loading…** beside it | The list from your last visit, drawn immediately from this browser's storage while a fresh request runs. |
| The list with **Last updated N s ago** | A fresh copy. The age counts up while you look at it. |
| The list with **Data may be out of date** | The copy is older than the 30 second refresh interval and no load is running, for example because the last refresh was slow. The rows are still shown. |
| A red **Could not load this list: <reason>** banner with a **Retry** button | The request failed or took longer than 15 seconds (the reason then reads `timeout (15 s)`). Any cached rows stay visible below the banner. |
| **No automata. Launch one with ⚡.** | The load succeeded and there is nothing to show. It never appears while loading or after a failure. |

Stale is not failed: a slow refresh keeps the old rows on screen. **Retry**
sends the same request again. A late answer to an older request cannot replace
a newer one.

The tab refreshes itself every 30 seconds while it is on screen. A tab you are
not looking at does not poll, and the browser's own background throttling for a
hidden page is respected, so a phone is not kept busy. When you come back to
the tab it refreshes if the data is older than 30 seconds.

## Opening a row

A row on the list holds counts only, so its stories and tasks are not there
yet. Open a row and the tab fetches the **full record for that one Automaton**
(one request), then its **children**. The text **Open to load stories and
tasks** is what a closed row stands for. While the record is loading the row
shows a loading line; if the request fails the row shows the error and you can
close and open it again.

The full record is kept for the row and fetched again only when the
Automaton's revision changes, so opening and closing a row does not re-download
it, and a running Automaton you are watching updates when it changes.

Very long text inside a record (verifier transcripts, task output) is capped
in the record shown on the card and marked as cut. The complete text is one
more request away through the outputs route described in the paging guide.

## Children

A child is an Automaton spawned by a task of another Automaton. A child row
shows a purple **parent** badge with the parent's id.

- On the list the children are ordinary rows. A child that is on a page you
  have already loaded shows up on its own card and in its parent's **Children**
  section.
- When you open a parent, the tab asks the server for that parent's children
  once, whether or not they are on a loaded page. The section lists each child
  with its id, status and counts; tap a child's id to jump to its row when it
  is loaded.
- When none of the parent's tasks spawned a child, the section says so:
  "no children".

So you never have to press **Load more** just to see the children of an
Automaton you have opened.

## What happens when the connection drops and returns

The web app keeps a live socket to the daemon. When it drops and then
reconnects (a phone going through a tunnel, a Tailscale route changing, the
daemon restarting), the app does **not** reload the tabs.

- It sends **one small request**: the first summary page of sessions (50 rows),
  with the same row-only shape as the list. Several reconnect events in a row
  share that one request.
- If you have a session open, its full record is fetched once more only when
  the summary row for it was cut.
- Every other list refreshes only if its tab is on screen and its data is older
  than the refresh interval, and then it reloads the **first page only**. Open
  rows and the rows you already loaded by **Load more** stay on screen.
- Retries of the socket back off, up to about 30 seconds between attempts, so a
  long outage does not create a burst of requests.

A drop that is repeated again and again stays inside the same small request
and byte budget each time.

## Troubleshooting a slow tab over a phone or Tailscale link

1. **Look at the label first.** **Cached** with a ticking age means the list
   on screen is real but old and the new one is on its way. **Data may be out
   of date** means a refresh is overdue. Neither is a failure.
2. **Wait for the 15 second timeout before assuming it is stuck.** On a slow
   link the tab shows **Loading…** and then, if the request cannot finish, the
   red banner with **Retry**. Your cached rows stay visible under it. Tap
   **Retry** once the link is better.
3. **Check the path, not just the tab.** Over Tailscale, check that the route
   to the daemon is direct and not relayed: `tailscale ping <daemon-host>`
   prints whether the path is direct or through a relay. A relayed path is much
   slower than a direct one.
4. **Measure the request.** Set `DW` to the daemon base URL, then time the first
   page and look at its size:

   ```bash
   export DW="http://<host>:<port>"
   curl -sS --compressed -o /dev/null \
        -w 'status %{http_code}  bytes %{size_download}  total %{time_total}s\n' \
        "$DW/api/autonomous/prds?limit=50&active=1"
   ```

   A healthy first page is tens of kilobytes at most, and a repeat that sends
   the ETag back answers **304 Not Modified** with no body. A very large
   response here means something is asking for the full form; see the last
   item.
5. **Narrow what the tab asks for.** Use the status filters or the search box;
   the server filters first, so a narrower list is a smaller first page.
6. **Keep the tab visible.** A hidden browser tab stops polling to save
   battery and refreshes when you return. Switching apps on a phone and back
   triggers that one refresh.
7. **If a page is huge, find the full form.** The only requests that return
   whole records are `full=1` on the Automata routes and an `/api/sessions`
   call with no `view=summary` or `limit`. The web app does not send these.
   A script or an older client that does is the likely cause.
8. **Clear the saved copy if the tab shows something odd.** The cached list
   lives in this browser's storage and is replaced by the next successful
   load. Clearing site data for the daemon's address removes it.

## Checking it yourself

The load scenarios run a throttled slow-4G profile against a daemon holding
thousands of sessions and hundreds of Automata:

```bash
cd scripts/test-stories/pwa
env -u TMUX -u TMUX_TMPDIR node run-all.mjs --group load
```

One scenario checks that the first load of the Automata and Sessions tabs makes
no full-form list request and stays inside a byte budget. Another drops the
connection, holds it down, restores it, and checks that the refresh stays
inside a small request and byte budget, twice in a row. The same checks run in
release smoke against the smoke daemon.

## Where to go next

- [list-endpoints-and-paging.md](list-endpoints-and-paging.md): the API behind
  the summary list, paging, ETag / 304 and compression.
- [automation-tab.md](automation-tab.md): the Automation tab that chains
  Automata.
- [automata-tags.md](automata-tags.md): tagging Automata; the tag filter works
  on the server too.

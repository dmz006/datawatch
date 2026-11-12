# Parity issue draft — discussion folder id guard (code scanning alert #768, go/path-injection)

**Filed 2026-11-08 as https://github.com/dmz006/datawatch-app/issues/398 (release v9.112.4).** Original note: — this is the tracked draft; the issue on the app tracker is
opened when the operator files it. Writing this draft changes nothing on any
host, and filing it never runs a hosting CLI or creates anything on a tracker.

**Record update, 2026-11-08:** the one follow-up attempt after this draft —
"try the second barrier form once if the `go` scan still reports the old
finding" — was run and made no change, because the scan report it required was
absent (see `CODEQL-768-SECOND-FORM-DECISION.md`). The first form — the
cleaned-path prefix check in `discussionFolderPath`, exactly as described
below — is what is committed, and this draft stands unchanged apart from this
note.

**Server-side status:** the server half of this record is done — the
discussion scope, sync and file-service handlers now build every user-id
folder path through the shared helper (the switch-over commits for
`bl332_discussion_scope.go`, `bl332_discussion_sync.go` and
`bl333_file_service.go` are committed), and `docs/operations.md` records the
remaining operator items: confirming the CodeQL alert gate on main no longer
reports alert #768 open, and the client-side parity below that is not yet
filed. This draft stands as the standing record for that client-side gap.

**Parent work:** "Close the last open high code scanning alert, number 768
(go/path-injection), by making the discussion id check visible" — the change
that added the shared, unit-tested path helper `discussionFolderPath` in
`internal/server/discussion_folder.go` (with `TestDiscussionFolderPath` in
`internal/server/discussion_folder_test.go`), switched every production
route in `internal/server/bl332_discussion_scope.go`,
`internal/server/bl332_discussion_sync.go` and
`internal/server/bl333_file_service.go` to build its discussion folder path
through it, and documented the discussion id rule in
`docs/datawatch-definitions.md`, `docs/howto/discussion-scopes.md`,
`docs/howto/file-service.md` and `docs/operations.md`.

## Affected area

Server side: every route that builds a path into the discussion folder — the
discussion memory routes in `internal/server/bl332_discussion_scope.go`
(scope write, recall, list, delete, WAL, participants, conflicts and resolve,
receive) and `internal/server/bl332_discussion_sync.go` (participants and
sync), plus the file service discussion folder route in
`internal/server/bl333_file_service.go` (the discussion folder handler under
`GET /api/files/discussions/<id>`). All of these now validate the id and
join it into a path through `discussionFolderPath`; the remaining
`filepath.Join` calls in the three files join only fixed file names
(`wal.jsonl`, `participants.json`) onto the helper's result, or enumerate
directory entries read from disk, so no user-supplied id is joined inline
anywhere.

Client side: any Android or iPhone screen that names, types or pastes a
discussion id, or that lists discussion scopes and discussion file folders —
the same surface the id-rule fix landed earlier in this cycle already left
unpaired. The REST contract the apps parse (HTTP 400 with the plain-text
body `invalid discussion id`) is unchanged by this work.

## Current behavior

On the server the fix is in and exercised by the routes: the helper
validates the id, refuses anything carrying `/`, `\` or exactly `.`/`..`,
and verifies at the join itself that the cleaned result is a direct child of
the cleaned base folder (`<data_dir>/discussions` for the memory scopes,
`<file_service_root>/discussions` for the file service). Because the
containment check now sits visibly on the data flow — the scanner can trace
the id from the request into the checked function — the code scanner sees
the guard, which is what the high `go/path-injection` alert #768 requires.
Behavior is otherwise unchanged: the same ids that passed before pass, the
same ones are refused with 400 `invalid discussion id` before any file is
touched, and a refused id never opens, reads or creates a file.

Two things remain open on the operator side, both recorded in
`docs/operations.md`: code-scanning engines report on the next analysis, not
immediately, so the next CodeQL "Open high/critical alerts" gate run on main
still has to be confirmed green (and until then the [Unreleased] changelog
entry must not claim the alert closed); and any pre-existing discussion
folder whose name carries other characters (spaces, slashes, non-ASCII)
still sits on disk, is unreachable through the id-bearing routes, and must
be listed and renamed by the operator.

On the clients the situation is unchanged and unpaired: the apps still send
a discussion id they have not checked against the rule; a rejected id
arrives as a plain-text 400 body rather than the usual JSON error shape, so
an app that treats every non-2xx answer as a server fault can show a generic
failure or even retry it; and a previously stored id that no longer passes
the rule has no dedicated "unreachable id" state, so the app has no way to
point the operator at renaming the folder.

## Expected behavior

- Every discussion folder path on the server is built by
  `discussionFolderPath` (done — this holds for the scope, sync and
  file-service routes, with the WAL and participants files joining to its
  result), so the check the scanner must accept lives in exactly one place
  that the routes exercise, and the inline join sites in the three handler
  files stay away.
- The day after the switch-over, the code-scanning alert gate on main
  reports no open high alerts; until it does, the release record must say
  alert 768 is still open and the [Unreleased] entry must not claim it
  closed.
- On Android and iPhone the app checks a discussion id against the same
  rule before sending, shows the 400 reason plainly without retrying it or
  counting it as an outage, and marks a previously stored id that no longer
  passes the rule as unreachable with a hint that the operator should rename
  the folder — identically on both platforms and matching the PWA.

## Suggested next step

Server: run (or watch) the next CodeQL alert gate on main and record whether
alert 768 clears. If it does not, say so in the same record and keep the
operator-review items in `docs/operations.md` open; and list the existing
discussion folders (the memory scopes under the data directory's
`discussions` folder, and the file-service discussion folders), renaming or
deleting any that use other characters, as the how-tos describe.

Client: file one parity issue on the app tracker covering the input check,
the plain-text 400 handling and the stale-id display described above, for
both Android and iPhone, with a client test using the same examples as the
server test table (a valid id, an id with a slash, an id with a backslash,
`.`, `..`, an empty id and a 129-character id). Filing it is a manual
operator action on the tracker; nothing in this repository runs a hosting
CLI or creates that issue automatically.

## Parity surface

- **REST** — the only surface this work touches; contract unchanged (same
  routes, same plain-text 400 body), behavior identical before and after the
  switch-over.
- **MCP, CLI, comm channel, YAML/config** — all reach the discussion routes
  only through REST or not at all; no new key, tool or flag, so nothing to
  add here.
- **PWA** — no screen change; the PWA already renders the 400 the daemon
  sends.
- **Android, iPhone** — intentionally excluded from this server-only change
  set; they owe the client-side row in *Expected behavior* above, and that
  is what this issue is the standing record for.

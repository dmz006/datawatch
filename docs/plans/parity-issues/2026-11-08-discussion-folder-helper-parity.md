**Not filed yet.** — this is the tracked draft; the issue on the app tracker is
opened when the operator files it. Writing this draft changes nothing on any host.

Parent work: "Close the last open high code scanning alert, number 768
(go/path-injection), by making the discussion id check visible" — the change that
added the shared, unit-tested path helper `discussionFolderPath` in
`internal/server/discussion_folder.go` (with `TestDiscussionFolderPath` in
`internal/server/discussion_folder_test.go`) and documented the discussion id
rule in `docs/datawatch-definitions.md`, `docs/howto/discussion-scopes.md`,
`docs/howto/file-service.md` and `docs/operations.md`.

## Affected area

Server side: every route that builds a path into the discussion folder — the
discussion memory routes in `internal/server/bl332_discussion_scope.go` and
`internal/server/bl332_discussion_sync.go` (scope write, recall, list, delete,
WAL, participants, conflicts and resolve, receive, delete) and the file service
discussion folder route in `internal/server/bl333_file_service.go`
(`GET /api/files/discussions/<id>`). The new helper is only reachable in tests
today; no handler calls it.

Client side: any Android or iOS screen that names, types or pastes a
discussion id, or that lists discussion scopes and discussion file folders —
the same surface the id-rule fix landed earlier in this cycle already left
unpaired. The REST contract the apps parse (HTTP 400 `invalid discussion id`)
is unchanged by this work.

## Current behavior

The helper validates the id, refuses anything carrying `/`, `\` or exactly
`.`/`..`, and verifies the cleaned join is a direct child of the cleaned base
folder. It is covered by its own test table (one accept case, seven refuse
cases). But the production routes do not go through it: they still call
`validateDiscussionID` and `filepath.Join` inline, call site by call site.
Behavior is identical — an accepted id cannot carry a path separator, so the
inline joins cannot escape the base directory — yet the code tree now carries
two ways to build a discussion folder path, and a future edit to any one of the
inline sites would silently re-open the gap the helper exists to close.

Because the alert number 768 (go/path-injection) fires on a path the scanner
can trace into an unsafe join, and those joins are the inline ones, the
scanner has nothing to see in the new helper: a code-scanning engine only
accepts the fix when the data actually flows through the checked function.
Until the routes call `discussionFolderPath`, the next alert gate run on main
may still report the alert open, and the work's stated goal — the last open
high alert closed — is not actually reached. `docs/operations.md` already
carries an operator-review note recording exactly this: the helper is present
and tested, but no production route calls it yet.

On the clients the situation is unchanged by this work: the apps still send a
discussion id they have not checked, and a rejected id arrives as a plain-text
400 body rather than the usual JSON error shape, so an app that treats every
non-2xx answer as a server fault can show a generic failure or retry it.

## Expected behavior

- Every discussion folder path on the server is built by `discussionFolderPath`
  (with the WAL and participants files joining to its result), and the inline
  validate-plus-join sites in the three handler files are gone, so the check
  the scanner must accept exists in exactly one place — and that place is
  exercised by the routes, not only by tests.
- The day after the switch-over, the code-scanning alert gate on main reports
  no open high alerts; until then the release record must say alert 768 is
  still open and the [Unreleased] entry must not claim it closed.
- The switch-over is behavior-neutral: the same ids that pass today pass, the
  same ones get 400 `invalid discussion id`, and nothing is read or written for
  a refused id.
- On Android and iOS the app checks a discussion id against the same rule
  before sending, shows the 400 reason plainly without retrying it or counting
  it as an outage, and marks a previously stored id that no longer passes the
  rule as unreachable with a hint that the operator should rename the folder —
  identically on both platforms and matching the PWA.

## Suggested next step

Server: one small change set that points every discussion folder path build at
`discussionFolderPath`, deletes the now-duplicated inline checks, re-runs the
server test suite, and — the load-bearing part — runs the next CodeQL alert
gate on main and records whether alert 768 clears. If it clears, note the
version it closed in; if it does not, say so in the same record and keep the
operator-review note in `docs/operations.md` open.

Client: file one parity issue on the app tracker covering the input check, the
plain-text 400 handling and the stale-id display described above, for both
Android and iOS, with a client test using the same examples as the server test
table (a valid id, an id with a slash, `a\b`, `.`, `..`, an empty id and a
129-character id). Separately, the operator should list the existing
discussion folders and rename any that use other characters, as the how-tos
describe.

## Parity surface

- **REST** — the only surface this work touches; contract unchanged (same
  routes, same 400 body), behavior identical before and after the switch-over.
- **MCP, CLI, comm channel, YAML/config** — all reach the discussion routes only
  through REST or not at all; no new key, tool or flag, so nothing to add here.
- **PWA** — no screen change; the PWA already renders the 400 the daemon sends.
- **Android, iPhone** — intentionally excluded from this server-only change set;
  they owe the client-side row in *Expected behavior* above, and that is what
  this issue is the standing record for.

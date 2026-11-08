**Not filed yet.** — this is the tracked draft; the issue on the app tracker is
opened when the operator files it. Writing this draft changes nothing on any host.

Parent work: the code-scanning fix (security triage, 2026-11-08) that made the
daemon validate discussion ids on the discussion memory routes and the file
service discussion folder route.

## Affected area

Any mobile-app (Android and iOS) screen that lets a person name, type, pick or
paste a discussion id, and any screen that lists discussion scopes or discussion
file folders. This covers the discussion memory routes (scope write, recall,
list and delete; WAL; participants; conflicts; receive) and the file service
discussion folder route. Docs: `docs/howto/discussion-scopes.md` (section
*Discussion id rule*) and `docs/howto/file-service.md`.

## Current behavior

The daemon now accepts a discussion id only when the whole id is 1 to 128
characters from ASCII letters, digits, `.`, `_` and `-`, and is not `.` or `..`.
Any other id gets HTTP 400 with the plain text `invalid discussion id` before
anything is read or written, so a rejected request creates no scope, no WAL file
and no participant list.

The apps were built before this rule. They have no matching check, so a person
can type an id with a space, a slash or non-ASCII characters and only learn it is
wrong from a bare 400 whose body is plain text, not the usual JSON error shape. An
app that treats every non-2xx answer as a network or server fault may show a
generic failure or retry it.

Discussion ids created earlier with other characters stay on disk but are no
longer reachable through the API. An app that cached such an id, or listed it
from an older daemon, will now get 400 whenever it uses it.

## Expected behavior

- The app checks a discussion id against the same rule as the daemon before it
  sends the request, and shows a clear message that names the rule (1 to 128
  letters, digits, dot, underscore or hyphen; not `.` or `..`).
- If the daemon still answers 400 `invalid discussion id`, the app shows that
  reason, does not retry, and does not count it as an outage.
- A stored or cached id that no longer passes the rule is marked as unreachable
  with a hint to ask the operator to rename the folder, instead of failing
  silently.
- Behavior is the same on Android and iOS and matches the PWA.

## Suggested next step

Open one parity issue on the app tracker for both platforms. It should cover the
input check, the 400 handling (including the plain-text body) and the display of
stale ids. Add a client test with the accepted and rejected examples from the
how-to: a valid id, an id with a slash, `.`, `..`, an empty id and a 129-character
id. Separately, the operator should list the existing discussion folders with
`ls ~/.datawatch/discussions` and `ls ~/.datawatch/files/discussions`, and rename
any that use other characters, as the how-to describes.

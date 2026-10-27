# PWA: session header clean-up and a Tags dropdown (2026-10-27)

Operator request, quoted: "to clean up inside a session in pwa, the time counter is taking space and not accurate, it can be removed, the eye selector can also be removed or better yet, between alerts and the status indicator in your right add a tags dropdown and have eye and tags selected and edit tags or something clean and organized, ask questions if that doesn't make sense".

## What exists (internal/server/web/app.js, index.html)

- In the session detail header row: the "time since the backend last produced output" label (`session-last-activity`) and the eye button, which is the per-session watch toggle (`toggleSessionWatch`; watched sessions are counted separately in alerts and can be filtered in the session list).
- Tag chips for the open session on their own row (`sessionDetailTagRow`), with tag editing in the Session tags section; a tag filter state for the list.
- The page header holds, on the right, the refresh spinner, expand, install, the alerts entry and the connection status dot.

## Decision

Operator, quoted: "Yes header suggestion is good". The design:

```
page header:   ... [alerts] [Tags v] [status dot]

Tags v opens:
  Watched only                (toggle: filter the session list to watched sessions)
  -----------
  [ ] tag-a  [ ] tag-b  ...   (checkboxes: filter the session list by tags)
  -----------
  Watch this session          (only while a session is open: toggles the eye)
  Edit tags of this session   (only while a session is open)
```

Inside a session the time counter and the eye button leave the header row. The tag chips stay visible. All strings go through the five locale bundles. Mobile parity (the datawatch-app) is listed in the PRD's final output; no datawatch-app issue is filed without the operator's word.

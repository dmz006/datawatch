# [sync] PWA peer-stale nav badge removed; per-peer stale / never-pushed indicator in the Observer peer list (datawatch next release after v9.90.2)

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened on the
datawatch-app repo when the operator files it. Filing the issue does not change a
line of this repo.

Parent release: the datawatch release after v9.90.2 that carries the Unreleased
CHANGELOG entry "Peer-stale badge replaced by a per-peer connectivity indicator"
(PRD `20070001`, operator request: "Remove the PWA peer-stale badge from the
Settings nav button and replace it with a real connectivity alert").

## Affected area

Two places in the web app:

1. The **Settings nav button** (the gear button in the top navigation), which
   carried a red count badge of stale federated peers (`#peerStaleBadge` in
   `index.html`, `updatePeerStaleBadge` in `app.js`).
2. The **Observer view → Federated Peers list** (`loadObserverPeers` in
   `app.js`), which lists every federated peer plus the daemon's own synthesized
   row.

This work is a display-only change. The REST contract is untouched:
`GET /api/observer/peers` already returned everything the new display needs —
`last_push_at` (RFC 3339, omitted when the peer never pushed) and `is_self` on
the synthesized self row. There is no new endpoint, no new config key, and no
migration.

## Current behavior (what datawatch shipped)

- The peer-stale count badge on the Settings nav button is **gone**: the badge
  markup is out of `index.html`, and `updatePeerStaleBadge`, its 30-second
  `setInterval` poll, the initial 1.5-second poll, and the
  `navigateToStalePeer` click handler are out of `app.js`. No references to
  `peerStaleBadge` remain in either file. As a side effect the standing
  30-second `GET /api/observer/peers` poll that ran on **every** view (the
  2026-10-14 PWA performance audit row 14) is gone with it.
- The replacement lives where the peers are listed: each peer row in the
  Observer peer list now shows an inline warning-colored note —
  **"Stale (age)"** when the peer's `last_push_at` is more than 60 seconds old
  (the age is the human relative age the row already shows), or
  **"Never pushed"** when the peer has never pushed. The daemon's own row
  (`is_self`) never shows the note. The labels ship in all five locales — en
  ("Stale" / "Never pushed"), de ("Veraltet" / "Nie gesendet"), es
  ("Obsoleto" / "Nunca enviado"), fr ("Obsolète" / "Jamais envoyé"), ja
  ("古い" / "未送信") — under the new locale keys `observer_peer_stale` and
  `observer_peer_never_pushed`, and `v5280_locales_test.go`'s
  `TestLocales_CommonNavKeysPresent` now fails if either key is missing from a
  bundle.
- One wording note for the app team: the operator asked for "a real
  connectivity alert", and what shipped is the per-peer connectivity
  **indicator in the peer list** — there is no new channel alert, push
  notification or alert-queue entry behind it. The stale data source
  (`last_push_at` on `GET /api/observer/peers`) and the 60-second threshold are
  the same ones the old badge used.

## Expected behavior (what the app should do)

If the datawatch-app implementation of the original Option A breadcrumb (the
Settings-nav peer-stale count badge with the tap-through to the Federated
Peers card, filed as the datawatch-app issue of the v6.12.0 batch) is present
in the app, it now points at UI that no longer exists in the PWA:

- **Remove** the peer-stale count badge from the Settings nav button and its
  independent 30-second `GET /api/observer/peers` poll, and the tap-through
  navigation it performed. The badge element and its poll are gone from the
  web app; keeping them in the app leaves a stale count with no PWA
  counterpart and one extra poll the daemon no longer expects anyone to run.
- **Add** the same inline indicator the PWA shows: in the Observer /
  Federated Peers peer list, on each non-self peer row, a warning-colored note
  reading "Stale (age)" when the peer's `last_push_at` is more than 60 seconds
  old, or "Never pushed" when it is absent; the daemon's own row (the one the
  API marks `is_self: true`) never shows a note. English reference strings are
  exactly "Stale" and "Never pushed"; the non-English PWA labels above are the
  natural translations to mirror.
- No API work is needed on the app side: the fields, their names and the
  60-second threshold are unchanged, so this is a pure view-and-localization
  change.

## Suggested next step

File this as a Mobile-Parity issue on datawatch-app (the operator files it;
this session only writes the draft): remove the Settings-nav peer-stale badge
and its poll, add the per-row "Stale (age)" / "Never pushed" notes in the
Federated Peers list (self row excluded), and cover the strings in the app's
locale bundles. Suggested issue body: the "Expected behavior" section above
plus the two locale keys as a reference list. Link this file and PRD
`20070001` in the issue.

## Files this work touched in datawatch

- `internal/server/web/app.js` — badge functions and pollers removed;
  `loadObserverPeers` renders the per-row stale note.
- `internal/server/web/index.html` — `#peerStaleBadge` markup removed.
- `internal/server/web/locales/en.json`, `de.json`, `es.json`, `fr.json`,
  `ja.json` — `observer_peer_stale` and `observer_peer_never_pushed`.
- `internal/server/v5280_locales_test.go` — both keys guarded in
  `TestLocales_CommonNavKeysPresent`.

## Related stories, docs and the silent-peer alert

The "real connectivity alert" half of the operator request is delivered by the
daemon-side silent-peer alert, not by the PWA, so it needs no app change beyond
showing the alerts the app already lists:

- `docs/howto/federated-observer.md` documents the per-row "Stale (age)" note
  and the "Silent-peer alert". When a peer has pushed nothing for
  `observer.peers.silent_alert_minutes` (default 5), the daemon raises one
  warning alert, "Observer peer silent: <name>", once per outage. It reaches the
  app through the normal alerts surface and the messaging channels.
- `docs/config-reference.yaml` carries the `observer.peers.silent_alert_minutes`
  key. Any app settings screen that mirrors observer settings may expose it; the
  value is a whole number of minutes.
- Story `scripts/test-stories/pwa/settings-nav-clear-of-peer-alert.mjs`
  (run through `scripts/test-stories/TS-848.sh`) checks at a 390 px viewport that
  nothing sits over the Settings nav button label. The datawatch-app team can
  mirror that check on a narrow screen: after the badge is removed, the Settings
  entry must show only its own icon and label.

Parity gap closed by this work: the PWA no longer carries a stale count on the
Settings nav, so the app's badge is the only remaining copy. Parity gap left
open: the app has no per-row stale note yet and may not surface the silent-peer
alert distinctly; the suggested next step above covers both.

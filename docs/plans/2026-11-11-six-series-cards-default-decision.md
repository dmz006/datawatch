# Six series dashboard cards: why they are not in the Go default list

Date: 2026-11-11

## Decision

The six opt-in series cards (lane utilization, throughput forecast, verifier health,
release train, review backlog, defects found and fixed per day) are **not** added to
`defaultDashCards()` in `internal/server/api_dashboard_cards.go`, and `DASH_DEFAULT_LAYOUT`
in `internal/server/web/app.js` stays unchanged.

## Why this overrides the original request

The original request asked for the cards to be added to the Go default list and also to be
"available in Edit, Add Card". Those two requirements cannot both hold:

- Every card in `defaultDashCards()` is a system card (`System: true`). `mergeSystemCards`
  re-adds any system card that is missing on every read, so a system card cannot be removed.
- Add Card (`DASH_CARD_DEFS` picker in `app.js`) marks any card already in the layout as
  used and does not offer it. A system card is always in the layout, so it would never be
  addable again after the operator's first removal attempt, and it could never be removed.

The existing 2026-11-02 dashboard card audit made the same split for the other feature tiles
(guardrails, smoke, web-search usage, memory scope): they were moved out of the defaults and
stay addable in Add Card, hidden until their feature has data.

So the six cards follow that precedent: opt-in, addable, removable, and hidden through
`DASH_CARD_FEATURES` until their window holds data.

## What pins this

- `TestDashOptInSeriesCardsStayOutOfDefaults` in `internal/server/api_dashboard_cards_test.go`
  checks that the six IDs are not factory defaults, that a POST stores them as ordinary
  cards, and that a re-read does not add them back.
- `internal/server/web/app-dash-new-dashboards.test.js` covers the frame text, locale keys and
  the hidden-until-data behaviour.

## If the operator wants them as defaults anyway

That is a product decision for the operator. It needs both a change to `defaultDashCards()`
and `DASH_DEFAULT_LAYOUT`, and a decision on whether the cards become unremovable system
cards. Until then, leave this as it is.

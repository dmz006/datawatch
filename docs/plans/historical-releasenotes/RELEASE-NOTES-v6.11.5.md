# Release Notes — v6.11.5 (header icon order: 🤖 left of 🔍)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.5

## Summary

Operator: "The robot icon in the header in automation tab should be to the left of the search button."

## Changed

- **`internal/server/web/index.html`** — swapped DOM order so `headerIdentityBtn` (🤖) appears before `headerSearchBtn` (🔍) inside the `<header>`. The header's `display:flex; gap:12px` preserves alignment automatically.

No JS, locale, or API changes.

## Mobile parity

[`datawatch-app#61`](https://github.com/dmz006/datawatch-app/issues/61) — same icon order on the Compose Multiplatform app bar.

## See also

- CHANGELOG.md `[6.11.5]`

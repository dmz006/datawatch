# [sync] header Tags icon redrawn as a filled accent-colored tag (datawatch next release)

Related release: datawatch's next patch above main. This record deliberately carries no version number.

## Body sent with the issue

## What changed in the datawatch PWA

The header **Tags** button icon was redrawn. It is no longer a plain outline glyph: it is now a **filled tag in the theme accent color**, with the punched **hole** rendered in a lighter tone and a subtle darker edge for depth. The filled icon gets **brighter** in its active state — when the Tags dropdown is open, or when a tag filter is on. The button keeps a visible focus ring, keeps its accessible name and tooltip, and the existing tag **count badge** (the small pill next to the icon) stays readable on top of the filled accent shape.

## What the Android and iOS apps should do

- Replace the header Tags button's outline tag glyph with the same filled tag shape: theme accent color fill, a lighter hole, and a subtle darker edge.
- Match the active state: render the icon brighter while the tags menu is open or a tag filter is on.
- Keep the tag count badge exactly as it is today and keep it readable over the filled accent icon.
- Keep the button's accessible name and tooltip unchanged.
- No new user-facing strings are needed: no new locale keys on either side, and no REST or server change is required — this is a pure PWA icon styling change with a visual parity counterpart.

## Acceptance

- Android and iOS header Tags buttons show the filled accent-colored tag (lighter hole, subtle darker edge).
- The icon is visibly brighter when the tags menu is open or a tag filter is active.
- The count badge, accessible name, and tooltip are unchanged and remain readable.
- No locale bundles and no daemon/OpenAPI surface were touched by the datawatch change.

## Filing record

Filed: https://github.com/dmz006/datawatch-app/issues/299 (via `gh issue create -R dmz006/datawatch-app`)

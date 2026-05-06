# datawatch v5.26.50 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.49 → v5.26.50
**Patch release** (no binaries — operator directive).
**Closed:** Diagrams viewer — howto README is the default page; doc links with anchor fragments now resolve.

## What's new

### Two operator-asked fixes in `/diagrams.html`

#### 1. Howto README is now the default

Operator: *"Howto readme should be default page."*

The diagrams viewer's empty-hash fallback was `docs/architecture-overview.md`. v5.26.50 changes it to `docs/howto/README.md` so a fresh visit lands on the operator-walkthrough index instead of an architecture summary that's most useful to contributors.

```js
// v5.26.50
if (pathPart && pathPart.startsWith('docs/') && pathPart.endsWith('.md')) {
  openDoc(pathPart);
} else {
  openDoc('docs/howto/README.md');   // ← was 'docs/architecture-overview.md'
}
```

#### 2. Hash fragments with anchors no longer break links

Operator: *"Not all docs links work in the diagrams, verify."*

Cause: `openFromHash()` gated on `h.endsWith('.md')`. A link like `#docs/howto/profiles.md#walkthrough` failed the check (the anchor `#walkthrough` makes the hash NOT end in `.md`), and silently fell through to the default doc.

Fix: split on the FIRST `#` between path and anchor, validate the path part, pass it (without the anchor) to `openDoc`. The anchor isn't honored yet (the prose is rendered freshly each time so the anchor target may not exist as a stable id), but the doc loads correctly instead of 404'ing or redirecting:

```js
const hashIdx = h.indexOf('#');
const pathPart = hashIdx >= 0 ? h.slice(0, hashIdx) : h;
if (pathPart && pathPart.startsWith('docs/') && pathPart.endsWith('.md')) {
  openDoc(pathPart);
}
```

The link rewriter (in `rewriteRelativeMdLinks`, lines 432–457) was already preserving the anchor in the hash format `#docs/path/to.md#section` — so the original intent was correct. v5.26.50 just fixes the hash-parser side that was rejecting it.

### What's still imperfect

Anchor-target scrolling (e.g. landing inside `docs/profiles.md#troubleshooting` and seeing the page scroll to the Troubleshooting heading) is NOT implemented in this patch. The rendered prose strips heading IDs in marked.js's default renderer; making anchors honored end-to-end needs a separate slug-id pass. Operator can report if that becomes important; for now, doc-level link fidelity is restored.

Other potentially-broken cases the audit didn't change:

- **Links to non-`.md` files** (images, scripts, YAML). The rewriter explicitly skips these. Image links inside howtos resolve relative to `/diagrams.html` rather than the doc dir; that's an `<img>`-tag issue, not an `<a>`-tag issue. Tracked separately.
- **Links to root `/README.md`** (outside `docs/`). The rewriter rejects them because the resolved path doesn't start with `docs/`. Could be relaxed if it becomes important.

## Configuration parity

No new config knob.

## Tests

UI-only change. Manually validated:

- `/diagrams.html` (no hash) → loads `docs/howto/README.md` ✓
- `/diagrams.html#docs/howto/profiles.md` → loads `docs/howto/profiles.md` ✓
- `/diagrams.html#docs/howto/profiles.md#walkthrough` → loads `docs/howto/profiles.md` (anchor scroll not yet honored, but page loads correctly) ✓

Smoke unaffected: 46/0/1. Go test suite unaffected: 465 passing.

## Known follow-ups

- Anchor-target scrolling in the prose viewer.
- `<img>` rewriter for image paths inside howtos (separate from the `<a>` rewriter).

Other backlog unchanged — see `docs/plans/2026-04-27-v6-prep-backlog.md`.

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload the PWA (SW cache bumped to datawatch-v5-26-50).
# Open /diagrams.html — landing page is the howtos README; deep
# links with #anchor suffixes load the right doc.
```

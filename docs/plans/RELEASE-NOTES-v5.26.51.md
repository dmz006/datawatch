# datawatch v5.26.51 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.50 → v5.26.51
**Patch release** (no binaries — operator directive).
**Closed:** Diagrams viewer — `<img>` rewrite + heading anchor ids + scroll-to-anchor (v5.26.50 follow-ups).

## What's new

Three follow-ups to v5.26.50, all in `rewriteRelativeMdLinks`:

### 1. `<img>` sources now resolve against the doc directory

Pre-v5.26.51 `marked.js` rendered `![alt](screenshots/foo.png)` → `<img src="screenshots/foo.png">`. Browser resolved that against `/diagrams.html` and 404'd. Howto walkthroughs that embed screenshots showed broken-image icons. Same-shape fix as the `<a>` rewriter:

```js
proseRoot.querySelectorAll('img[src]').forEach(img => {
  const src = img.getAttribute('src');
  if (!src || /^(?:[a-z]+:)?\/\//i.test(src)) return;
  if (src.startsWith('data:') || src.startsWith('/')) return;
  img.setAttribute('src', '/' + resolvePath(baseDir, src));
});
```

Skips already-absolute / data: / http(s): URLs. The daemon already serves `/docs/howto/screenshots/*.png` from the embedded docs tree, so the rewritten path resolves cleanly.

### 2. Heading anchor ids

`marked.js` doesn't emit slug ids on headings by default. v5.26.51 walks every `<h1>`–`<h6>` after render and sets a slugified id (lowercase Unicode-alphanumerics + dashes; collision-safe via a counter suffix). Closes the v5.26.50 known follow-up "Anchor-target scrolling not yet honored".

### 3. Scroll-to-anchor on doc open

After the heading-id pass, if `location.hash` carries a `#anchor` suffix (e.g. `#docs/howto/profiles.md#walkthrough`), scroll the matching heading into view smoothly. Deferred one animation frame so the id-assignment pass has flushed:

```js
const hashIdx = (location.hash || '').indexOf('#', 1);
if (hashIdx > 0) {
  const anchorId = decodeURIComponent(location.hash.slice(hashIdx + 1));
  requestAnimationFrame(() => {
    const el = document.getElementById(anchorId);
    if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' });
  });
}
```

End-to-end now: open `/diagrams.html#docs/howto/profiles.md#walkthrough` → loads `profiles.md` → renders prose → assigns `id="walkthrough"` to the matching heading → scrolls to it.

## Configuration parity

No new config knob — UI fixes.

## Tests

UI-only changes. Manually validated:

- `<img>` resolution: open a howto with a screenshot, image renders.
- Heading ids: inspect any heading after open, has `id="..."`.
- Anchor scroll: open `/diagrams.html#docs/howto/profiles.md#walkthrough` and confirm the page scrolls to "## Walkthrough".

Smoke unaffected (46/0/1). Go test suite unaffected (465 passing).

## Known follow-ups

Doc-side: nothing immediate.

Other backlog unchanged — see `docs/plans/2026-04-27-v6-prep-backlog.md`.

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload the PWA (SW cache datawatch-v5-26-51). Open any
# howto with screenshots; images now resolve. Anchor links
# inside howto prose deep-scroll to the right heading.
```

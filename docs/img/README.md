# Howto screenshots — convention

**Operator-directed 2026-05-10:** screenshots are NOT packaged with the
single-binary daemon. Embedded howto pages reference images from this
GitHub directory by canonical raw URL, and pages must fail clean when
offline.

## Add a screenshot

1. Drop the PNG into this directory (`docs/img/<slug>.png`).
2. Commit + push to `main`. The raw URL becomes:
   `https://raw.githubusercontent.com/dmz006/datawatch/main/docs/img/<slug>.png`
3. Reference it in the howto markdown:

   ```markdown
   <!-- screenshot: see docs/img/<slug>.png -->
   ![Caption that reads fine without the image](https://raw.githubusercontent.com/dmz006/datawatch/main/docs/img/<slug>.png)
   ```

The markdown→HTML pipeline (and the PWA's docs viewer) renders the
above as `<img>` — when the image fails to load (offline, GitHub
blocked, or yet-uncommitted), the alt text shows in place. The
surrounding caption already reads as full content.

## Embedded HTML pages — fail-clean pattern

When a daemon-served HTML page directly references screenshots, use:

```html
<img src="https://raw.githubusercontent.com/dmz006/datawatch/main/docs/img/<slug>.png"
     alt="Caption that reads fine without the image"
     onerror="this.style.display='none'" />
```

`onerror` collapses the broken-image icon when offline so the page
stays clean.

## Rules

- **No `embed.FS` of image files.** The Go embed list stays text-only
  (HTML / CSS / JS / JSON / Markdown).
- **Caption = the content.** Image is enrichment. The page must
  read fine without it.
- **No third-party hosts** (imgur, etc.). Screenshots live in this
  repo so they version with the docs.
- Refresh-only changes (replace image, keep filename + caption) need
  no daemon rebuild — push to GitHub and the reference resolves.

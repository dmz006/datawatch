# Navigation at phone widths

The bottom navigation bar of the web UI (the PWA) has to fit a phone screen: six buttons, each with an icon and a label, on a bar about 320 to 414 px wide. This page covers the check that proves it fits, how to run that check, where its report goes, how to read a finding, and what you see in the PWA now.

This is a test-runner check, not a feature you configure. Nothing here changes a setting, an API, an MCP tool, a CLI command or a comm-channel command. The surface is the PWA only.

The same page also covers how every page body is laid out at phone width (the shared page container, the Dashboard header counters and the filter rows); see [Page layout at phone width](#page-layout-at-phone-width). That layout is a style rule of the PWA, not part of the bottom-bar check.

## What the check measures

The check loads the PWA in a real headless Chrome, against an isolated daemon the test runner starts for itself. It sizes the window to each phone width and reads the bottom navigation bar. It runs in two themes, light and dark.

| Setting | Value |
|---------|-------|
| Widths gated | 320x568, 360x740, 390x844 (CSS px) |
| Themes gated | light and dark |
| Tabs measured | the bottom navigation bar only |
| Zoom | 100 percent only; text zoom is not measured |

It reports three kinds of finding. Any one of them fails the check.

| Check | What it means | Limit |
|-------|---------------|-------|
| `clipped-text` | A label runs past the visible navigation bar, so the bar cuts it off | the bar's own edges |
| `tap-target` | A navigation button is smaller than 44 by 44 CSS px | 44 x 44 px |
| `overlap` | A label or a button box intersects another navigation item | none |

A passing run measures six navigation items at each width and theme and finds no problems. The report also prints the bar's `scrollWidth` and `clientWidth` for each combination. The check records those two numbers but does not test them, so sideways scrolling in the bar does not fail the run by itself. Read them yourself: on a bar that fits, the two are equal (after the fix, 320, 360 and 390 px in both themes). A scroll width larger than the client width means the bar scrolls, even when the run passes.

The check does not look at the header or the tabs below the bar. Those are measured by a separate report-only pass, described under [Other modes](#other-modes). How the tab strips behave on a phone is described under [Tab strips at phone width](#tab-strips-at-phone-width).

## Base requirements

- **Node.js with npm**, on the `PATH`. The runner uses them for the story script.
- **Google Chrome** at `/usr/bin/google-chrome`, or the path in `DW_CHROME_PATH`. If no browser is found the check is skipped, not failed.
- **Go**, only if `bin/datawatch` is missing. The runner uses an existing `bin/datawatch` as it is, so rebuild it after any web change:

  ```bash
  go build -o bin/datawatch ./cmd/datawatch/
  ```

  The PWA files are compiled into the daemon (`internal/server/server.go` embeds the `web` directory). A stale binary serves the old navigation bar, and the check then measures the old bar.
- **A short `TMPDIR`.** Chrome keeps a Unix socket in `TMPDIR`, and the socket path must stay under 108 bytes. The default `/tmp` is fine. A long `TMPDIR` makes Chrome exit at once, and the runner then reports `chrome DevTools endpoint did not come up`.
- **Sandbox isolation.** Run the check with `env -u TMUX -u TMUX_TMPDIR` in front of the command. The daemon it starts must not reach your own tmux sessions.

## Run the check

From the repository root:

```bash
env -u TMUX -u TMUX_TMPDIR make test-e2e-shared-nav
```

The target runs `scripts/e2e-pwa.sh group:shared-nav`. That script does the following:

1. Picks free loopback ports and writes a daemon config from `testdata/datawatch.yaml`, with the token `dw-e2e-pwa-token` (unless `TEST_TOKEN` is set).
2. Starts the daemon from `bin/datawatch` in the foreground and waits up to 60 seconds for `/api/health`.
3. Runs `npm ci` in `scripts/test-stories/pwa`. This replaces the tracked symlink `scripts/test-stories/pwa/node_modules` with a real directory, and `git status` then shows the symlink as a deleted file. Restore it after the run with `git checkout -- scripts/test-stories/pwa/node_modules`, and do not commit the deletion.
4. Runs `run-all.mjs --group shared-nav`, which runs `shared-nav-mobile-fit.mjs`. That file runs the probe `shared-nav-mobile-fit-probe.mjs` in gate mode.
5. Stops the daemon.

A run takes about half a minute on a development machine. The summary line at the end reads `summary: 1 passed, 0 failed, 0 skipped, 1 total` on a pass.

Exit codes:

| Result | Make exit | Summary |
|--------|-----------|---------|
| Pass, no navigation finding | 0 | `shared-nav-mobile-fit PASS (exit 0)` |
| Fail, a finding or the probe could not run | 2 (from `make`) | `shared-nav-mobile-fit FAIL (exit 1)` |
| Skipped, no Chrome or no daemon | 0 | `shared-nav-mobile-fit ... skipped` |

A skip does not fail the target. Read the summary line before you call a run clean.

To run against a daemon you already have, set `TEST_HTTP`, `TEST_TLS` and `TEST_TOKEN` together. The runner then starts nothing and checks that daemon. Use a sandbox daemon, not your production one.

## Where the report is written

The gate writes two files into the run's evidence folder:

- `shared-nav-mobile-fit/repro-report.txt`: the readable report, one block per theme and width.
- `shared-nav-mobile-fit/repro-report.json`: the same results as data, with each finding's fields.

In local mode the evidence folder is `dw-e2e-pwa-XXXXXX/evidence/` inside `${TMPDIR:-/tmp}`. The runner removes the whole `dw-e2e-pwa-XXXXXX` folder when the run passes, so the report is gone after a pass. When the run fails, the runner keeps the folder and prints its path, with the daemon log.

Setting `EVIDENCE_DIR` does not move the report in local mode: the runner resets it. To keep the report of a passing run, copy the two files out while the run is still going.

## Read a finding

A failed run prints one line per finding under its theme and width:

```text
Theme dark, viewport 320x568: 2 findings, 6 nav items, nav bar 320x60, scrollWidth 440, clientWidth 320
  [clipped-text] <selector> "<label text>" clipped <n>px left, <n>px right
  [tap-target] <selector> "<label text>" <w>x<h>
  [overlap] <selector> "<label text>" with <other item>
```

The placeholders above stand for the values the run prints. Read the fields in this order:

1. **Theme and width.** Which combination failed. A finding that appears at only one width usually comes from the bar's width at that size, not from the bar's design.
2. **Check.** Which of the three checks flagged it.
3. **Selector and text.** Which button or label. The selector is the element's CSS path. The text is its label.
4. **The measurement.** The overflow in pixels for `clipped-text`, the box size in CSS pixels for `tap-target`, or the other item for `overlap`.

What to do:

- **`clipped-text`.** The label is wider than the bar can show. The labels now truncate with an ellipsis, so a clipped label means the truncation has stopped working. Check that the label's CSS still sets `text-overflow: ellipsis`, `white-space: nowrap` and `overflow: hidden`, and that the button's `min-width` is still `0` or the flex share is still in place.
- **`tap-target`.** The button is smaller than 44 by 44 CSS px. Check the button's `min-width` and `min-height` in `internal/server/web/style.css`. The stylesheet sets a 40 px minimum width on the bottom buttons, which is 4 px below the check's 44 px line, so the stylesheet alone does not keep a button above the line. What keeps it above is the equal share of the bar: six buttons on a 320 px bar are each more than 44 px wide. A button between 40 and 43 px wide passes the stylesheet but fails the check, and it points at a change that shrank the button's share of the bar (padding, a fixed width, or a seventh item).
- **`overlap`.** A label or a button's box crosses a neighbour. Look for padding or a fixed width added to the button, or a badge placed over the label.

After a fix, rebuild `bin/datawatch` and run the check again. The report is evidence only for the binary it was run against. Through `make`, the report's `daemon_version` reads `unknown`, so record the version yourself with `bin/datawatch version`.

## What you see in the PWA now

- **Six equal buttons.** The bottom bar gives every button an equal share of its width, so the six buttons fit a 320 px phone without the bar scrolling sideways.
- **Labels truncate.** A long label ends with an ellipsis (`…`) inside its button and does not wrap onto a second line.
- **The full name.** Each bottom button carries its full name as a `title` attribute. On a desktop browser, hovering the button shows the name after the browser's usual delay. On a phone, a long press may show the name, depending on the browser. Some phone browsers show nothing, so the truncated label is all you see there.
- **Tap targets.** The stylesheet sets a 40 px minimum width on the bottom buttons; the check's line is 44 by 44 CSS px. The equal share of the bar is what keeps the buttons above 44 px. In the last run no bottom button fell under the line at 320, 360 or 390 px, in either theme.

What is not covered yet:

- Buttons in the header and in the tabs below the bar are not gated. Some are still under 44 px. The header search button and the Observer section toggle now reach 40 px.
- Text zoom above 100 percent is not measured.
- Sideways scrolling in the bar is recorded in the report, not tested (see above).
- The ellipsis and the `title` attribute are covered only by a small unit test, `internal/server/web/app-shared-nav.test.js`, which reads the stylesheet and `index.html`. It does not render the bar, and it does not test hover or long press. Run it by hand with `node --test internal/server/web/app-shared-nav.test.js`; `make test-e2e-shared-nav`, the other make targets and CI do not run it.
- The check runs on the PWA only. Android and iOS are not covered by this check.

## Tab strips at phone width

Several screens have a row of tabs under the header. At phone width every such row follows one rule: it stays on a single line and scrolls sideways, instead of wrapping or being cut off. The Automata and Projects bars switch to this at a viewport of 600 px or less or on a touch screen; the other strips use the shared `tab-strip-scroll` style at every width, where it only matters once the tabs no longer fit. This is a layout rule in the PWA only; it is not gated by the check above.

| Strip | Where |
|-------|-------|
| Automata sub-tab bar (Projects, Automation, Automata, Events, Templates) and its action buttons | Automata view |
| Automation second row (Inbox, Automations, Resources and the rest) | Automata → Automation |
| Projects tab bar | Automata → Projects |
| Templates sub-tabs (Templates, Standard tasks) | Automata → Templates |
| Automaton detail tabs | An Automaton's page |
| Session detail tabs (Tmux or Chat, Channel, Status) and their controls | A session's page |
| Settings tabs | Settings |

What you see:

- **Sideways scroll, no wrapping.** Tabs keep their natural width and do not shrink or wrap. The scrollbar is hidden; swipe the strip.
- **Edge fade.** A soft fade appears on the left, the right or both edges, only on a side that still hides tabs. When every tab fits, there is no fade.
- **Active tab in view.** When you choose a tab, open a page by link or the page redraws, the active tab is scrolled to the middle of the strip.
- **Tap targets.** Each tab and each action button in these strips is at least 40 px tall (the 44 px line above is the bottom bar's check, not these strips').
- **Short header title.** On the Automata view the header shows the Projects sub-tab as **Projects** from its own short-name text (`nav_projects_short`), so a longer translation does not push the title onto a second line. The header title font is slightly smaller at phone width.

The Observer and Dashboard views have no tab strip. The rule was audited at 320 and 360 px.

What is not covered: the strips are not part of the `make test-e2e-shared-nav` gate, and there is no automated pixel check of the fade. The unit tests under `internal/server/web/` (`app-automation-subtab-load.test.js`, `app-automation-tab-registry.test.js`, `app-automaton-detail-tab-strip.test.js`) cover the scripts, not how the strip looks.

## Page layout at phone width

Every top-level page of the PWA now uses the same layout at phone width that the Sessions list uses, so moving between pages does not change the side margins or card sizes. The rule applies at a viewport of 600 px or less, or on a touch screen. Above that, and on a desktop browser, the pages keep their earlier layout.

**Pages covered:** Sessions, Automata (every sub-tab, an Automaton's page and the Automaton form), Projects (including a project's focus page), Alerts, Observer, Dashboard and Settings. Each page's outer wrapper carries the shared `phone-page` style.

What you see on a phone:

- **One side gutter.** Every page has a 12 px gutter on the left and right, the same as the Sessions list. A page inside another page does not add a second gutter.
- **Full-width cards, one padding.** Automaton cards, alert cards and Settings sections are as wide as the gutter allows and no wider. Automaton cards and alert cards use 12 px 14 px of inner padding and an 8 px gap between cards, the same as a session card. Settings sections keep their own inner padding; only their width and the gap between sections follow the rule.
- **Dashboard cards fill the row.** The Dashboard card grid becomes one column of full-width cards with an 8 px gap, instead of a narrower inset grid, and a card's wide or tall grid size is ignored.
- **No sideways page scroll.** The page container clips anything wider than the screen. Wide content such as the project Gantt chart scrolls inside its own box instead of widening the page.

The three spacing values live in one place in `internal/server/web/style.css` as `--phone-gutter`, `--phone-card-padding` and `--phone-gap`; one edit there moves every page.

### Dashboard header counters

The strip at the top of the Dashboard holds the **DASHBOARD** title, the session, task and guardrail counters, the burn-rate summary, the placement filter and the icon buttons. At phone width it wraps onto more lines instead of squeezing each item into a narrow column:

- Each counter (for example `51 sess`, `3 active`, `$521.32 today`) is one unit and never breaks in the middle of a value.
- The burn-rate summary takes its own full line and wraps only between its parts.
- The placement filter and its select shrink to the screen width instead of running off the right edge.
- The buttons keep their size and move to the next line when they no longer fit.

### Filter rows

Filter and control rows wrap onto more lines at phone width, and their children shrink to the row width instead of running off the right edge. Selects and search boxes in these rows take at least about 140 px and grow to fill a line; they cut long text with an ellipsis. The rows covered are the Dashboard Timeline card filters, the Automata filter bar, the Automation controls, the Plans and Plan records controls, the Records tab controls, the cost charts controls, the list controls, the trace filters, the paging controls, the Observer filter row and the project Gantt toolbar.

The Sessions toolbar is not one of these rows. It keeps its own one-line layout (see [Sessions deep dive](sessions-deep-dive.md) and [Sessions tiles and grouping](sessions-tiles-and-grouping.md)).

What is not covered:

- None of this is gated by `make test-e2e-shared-nav`, which measures the bottom bar only, and no automated check renders the pages at 320, 360 or 390 px. The change was verified by reading the style rules; look at each page on a real phone or in a narrow browser window before relying on it.
- The wrapping rules name specific rows and classes. A new filter row that uses a different class does not wrap until its class is added to the rule in `internal/server/web/style.css`.
- Android and iPhone are not changed by this; their layout is the mobile app's own.

## Other modes

The probe has two more modes. Both run through the same environment variables as the gate, and both are run directly rather than through `make`:

- **Report-only** (the default mode of the probe). It audits every tab at six phone widths in one theme. It writes `report-only.json` and `occurrences.json` to `PROBE_OUT_DIR`. It never fails a run. Use it to see what the next fix has to cover.
- **Baseline** (`SHARED_NAV_MODE=baseline`). It records the bottom bar's boxes at 768x1024, 1024x768 and 1280x800, in both themes, to `nav-baseline.json`, with the tolerance (2 px or 1 percent) written into the file. The probe does not compare a later run against that file; the comparison is a separate step.

The probe's header comment, `scripts/test-stories/pwa/shared-nav-mobile-fit-probe.mjs`, lists the variables each mode reads.

## Surfaces

| Method | How |
|--------|-----|
| **YAML** | N/A. No config key. |
| **CLI** | N/A. The check is run by `make test-e2e-shared-nav`. |
| **Web UI** | The bottom navigation bar, six buttons: Sessions, Automata, Alerts, Observer, Dashboard and Settings (Automata and Dashboard appear when enabled). The tab strips below it scroll sideways on a phone (see [Tab strips at phone width](#tab-strips-at-phone-width)), and every page body shares one gutter, card width and filter-row wrapping (see [Page layout at phone width](#page-layout-at-phone-width)). |
| **REST API** | N/A. |
| **Comm channel** | N/A. |

The navigation bar and the page layout send no commands and call no endpoint, so no other surface changes.

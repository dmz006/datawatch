# datawatch-app issue body — shipped status parity

**Ready to file as `dmz006/datawatch-app` issue #333** (the next free issue
number as of 2026-11-01; the newest open issue is #332). Title and body
below.

The shipped state ("this work landed in a release — merge commit recorded")
is fully wired server-side in datawatch: REST endpoints, CLI verbs, MCP
tools, the comm verbs, the merge-step recording helper, and a dashboard
card endpoint. But **no client renders it**: the PWA (vanilla JS,
`app.js`) has no shipped chip, no `shipped` list filter, and no entry in
its `DASH_CARD_DEFS` for the shipped dashboard card, and the Android/iPhone
apps have nothing either. An operator working from the app today cannot tell
which Automatons shipped, cannot see the shipped buckets, and the dashboard
card exists only as a server endpoint.

---

## Title

**Mobile parity: shipped status (chip on Automaton rows, `shipped` list filter, shipped dashboard card, record action)**

## Body

### Background

datawatch's `autonomous.shipped` feature records the **shipped state** of an
Automaton (PRD) and of an Automation: the release the work was merged into,
the merge time, the merge commit, and how the state was recorded
(`merge_step` or `backfill`). Shipped is **orthogonal to the stored status**
(a cancelled Automaton whose work landed still reads `cancelled`; what makes
it shipped is the non-empty merge commit), and recording shipped state never
changes the status.

All the data plumbing ships in the next patch above main:

- REST: `POST /api/autonomous/prds/{id}/shipped`,
  `POST /api/automations/{id}/shipped`,
  `POST /api/autonomous/shipped/backfill` (`?dry_run=true`),
  `GET /api/autonomous/shipped/counts`;
- the `shipped` field on `GET /api/autonomous/prds`,
  `GET /api/automations` and the `shipped=true|false` list filter
  (absent → the server's `autonomous.shipped.filter_default` view);
- the shipped dashboard card: `GET /api/dashboard/cards/shipped?refresh=&settings_version=`;
- CLI (`datawatch autonomous shipped record|backfill|counts`,
  `datawatch automation shipped <id>`, `--shipped true|false`),
  MCP (`autonomous_shipped_record`, `autonomous_shipped_backfill`,
  `autonomous_shipped_counts`) and the comm verbs (`autonomous
  shipped-record / shipped-backfill / shipped-counts`).

**No client renders any of it yet.** The PWA's JS and locale files contain
no shipped chip, filter, or card; `DASH_CARD_DEFS` in `internal/server/web/app.js`
has no `shipped` entry even though its sibling cards (lane-fill,
repo-ci-watch, …) are all there. The Android/iPhone apps are behind the PWA,
as usual for this surface until it exists.

### Data available (datawatch REST, all in the same build)

| Endpoint | Purpose |
|---|---|
| `GET /api/autonomous/shipped/counts` | Per-status shipped buckets — `prds: {status: n}`, `automations: {status: n}` — plus the in-force `settings` (`enabled`, `show_chip`, `filter_default`, `count_separately`). A PRD/Automation cancelled without its work landing counts under `cancelled`; one whose work landed counts under `shipped` while the stored status stays `cancelled` (while `enabled` and `count_separately` hold). |
| `GET /api/dashboard/cards/shipped` | The shipped dashboard card body: `settings`, `shipped_settings`, `prds`, `automations`, `generated_at`; `?refresh=1` recomputes (same endpoint id, `shipped`, as the lane-fill and repo-ci-watch cards — same `settings_version` cache semantics) |
| `GET /api/autonomous/prds[?shipped=true|false]` | Automaton list; each record carries `shipped {merged_in_release, merged_at, merge_commit}` — empty `merged_commit` = unshipped. `shipped` absent = the daemon's `autonomous.shipped.filter_default` (`all`, `shipped` or `unshipped`) |
| `GET /api/automations[?shipped=true\|false]` | Automation list, same shape and same selection semantics |
| `POST /api/autonomous/prds/{id}/shipped` | Record: `{merged_in_release, merged_at, merge_commit, source}` — `merge_commit` and `source` (`merge_step` or `backfill`) required; idempotent per commit; never touches the status |
| `POST /api/automations/{id}/shipped` | Same body, for one Automation (all non-skipped members shipped) |
| `POST /api/autonomous/shipped/backfill` | One pass over the base repository's main-branch history (`?dry_run=true` to scan without writing); reply: `prds_scanned`, `prds_marked`, `prds_already_marked`, `automations_marked`, `left_cancelled`, `unmatched_ids`, `errors` |

Settings are operator-configurable (`autonomous.shipped.enabled`,
`show_chip`, `filter_default`, `count_separately`, `backfill_on_start`),
reachable from the app through the existing generic config endpoints the
Settings screens already use.

### Proposed surfaces

**PWA first (this is also part of the gap — the web client renders none of
it today):**

1. **Shipped chip** on each Automaton and Automation row in the list
   screens, when the record carries shipped state and
   `autonomous.shipped.show_chip` is on — release + merge time, e.g.
   `v1.4.0 · 2d ago`. Chip semantics follow the in-force settings (a
   cancelled-but-shipped Automaton shows its `cancelled` status *and* the
   chip; while `count_separately` is off, a cancelled one shows no chip).
2. **`shipped` list filter** on the Automata and Automations list screens
   (the `shipped=true|false` query param), with the default selection
   following `autonomous.shipped.filter_default` like the server does.
3. **Dashboard card "Shipped"** added to `DASH_CARD_DEFS` (id `shipped`,
   endpoint `/api/dashboard/cards/shipped`), alongside the existing
   lane-fill and repo-ci-watch cards: the per-status `shipped` buckets for
   PRDs and Automations with the `settings` line, `?refresh=1` on demand,
   opt-in via the card picker (not in the default layout).
4. Optional: a "Record shipped" action on an Automaton/Automation detail
   screen (merge commit required; release, merge time, source) and a
   "Backfill" action with a dry-run first — the endpoints exist; skip if
   writing is reserved for CLI/MCP/comm by design.

**Android / iPhone (native parity, mirroring the PWA above):**

5. Shipped chip in the Automaton list items (same data, same settings
   semantics),
6. the `shipped` filter toggle in the list screens,
7. an equivalent dashboard/homemesh widget or card for the shipped buckets
   (opt-in, same as the PWA card),
8. the record / backfill actions only if step 4 ships on the PWA.

### Acceptance criteria

- [ ] PWA Automata/Automations list rows show the shipped chip for shipped
      records (and hide it for unshipped ones, per `show_chip`).
- [ ] PWA list screens can filter `shipped` on/off; the default selection
      matches `autonomous.shipped.filter_default`.
- [ ] The "Shipped" dashboard card is addable from the card picker and
      renders the per-status buckets from
      `GET /api/dashboard/cards/shipped`, with a manual refresh.
- [ ] Android: chip + filter + shipped card equivalent (screenshots in the
      PR against the PWA).
- [ ] iPhone: same.
- [ ] A cancelled Automaton whose work landed still shows its `cancelled`
      status on the app, with the shipped chip alongside — the two are not
      conflated in the UI.
- [ ] Locale strings added (the app renders no hardcoded English for
      shipped surfaces).
- [ ] If the record action ships: a shipped record made from the app shows
      the chip without an app restart, and an unknown commit / id still
      reads back 404 / unshipped.

### Cross-references

- datawatch docs: [howto/automata-shipped-status](../howto/automata-shipped-status.md),
  [api/autonomous — the shipped state](../api/autonomous.md#the-shipped-state-autonomousshipped),
  [datawatch-definitions — Shipped](../datawatch-definitions.md#shipped),
  [parity-status — shipped row](../parity-status.md),
  implementation plan [2026-11-01-shipped-status](2026-11-01-shipped-status.md).
- datawatch server: `internal/autonomous/shipped.go`,
  `shipped_counts.go`, `shipped_backfill.go`,
  `internal/server/autonomous.go`, `internal/server/api_dashboard_cards.go`,
  `internal/server/web/openapi.yaml` (already complete for shipped).
- Precedent for sibling PWA→native parity: datawatch-app #252, #228
  (dashboard cards pattern).

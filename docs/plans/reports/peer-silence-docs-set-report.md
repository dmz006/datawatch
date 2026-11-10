# Peer-stale badge removal — docs set report

Story: *Remove the PWA peer-stale badge from the Settings nav button and
replace it with a real connectivity alert.* (Operator: "R")

Task `ecbe0001-tpl-docs_set` · 2026-11-10 · Read-only verification of the docs
set; this report is the only file this task writes.

## What the docs now say (current tree)

The badge was removed in code by the sibling story (`ecbe0002`, already merged
into this branch); staleness now appears as a per-row inline note on the
Observer Federated Peers card, and a server-side connectivity alert replaces
the badge. The four affected operator-facing docs describe the post-change
behavior and are committed on this branch (`0b0330ef9`, `aaa54d564`):

| Doc | Entry |
|---|---|
| `docs/howto/federated-observer.md` | Stale-indicator item (10.) now says the nav button has no peer badge and points to the new **§ Connectivity alert** ("## Connectivity alert", line ~216): alert fires in the Alerts tab (`source: system`) when a peer stays unreachable past a configurable threshold; the 60 s per-row **Stale (age)** / **Never pushed** note is only a hint; default threshold **5 minutes — picked by the supervisor, not yet confirmed by the operator**; peer-health state-transition alerts keep firing as before. |
| `docs/howto/daemon-operations.md` | New **§ Peer connectivity alert** (lines 242–249): badge removed; outage surfaces as per-row notes + a connectivity alert (default 5 min, pending operator confirmation); links to `federated-observer.md#connectivity-alert`. |
| `docs/operations.md` | New **§ Peer outage visibility (PWA)** table (lines 6233–6240): Settings nav button — "No peer-stale badge (removed)"; Observer card — per-row **Stale (age)** / **Never pushed**; Alerts tab — transition alerts (60 s) + connectivity alert (default 5 min, operator to confirm). |
| `docs/datawatch-definitions.md` | Federated Peers entry (line ~757): per-row note semantics (daemon's own row never shows the note); "The Settings nav button no longer carries a peer-stale badge"; points to the connectivity alert with the 5-minute default flagged as pending operator confirmation; link to `howto/federated-observer.md#connectivity-alert`. |

## Verification evidence (read-only, current tree)

- **Badge gone from code**: zero references to `peerStale`/`peer-stale badge`
  logic in `internal/server/web/app.js` and `internal/server/web/index.html`.
- **Per-row indicator matches the docs**: `app.js` shows **Never pushed** when
  a peer has no `last_push_at`, **Stale (age)** when the push is older than
  60 s, and skips the daemon's own row (`!p.is_self`) — exactly what the four
  docs state.
- **Locales**: `observer_peer_stale` and `observer_peer_never_pushed` present
  in all five bundles (`en/de/es/fr/ja.json`, 2 hits each) and guarded by
  `internal/server/v5280_locales_test.go` (lines 78–79).
- **Links and anchors reachable**: the anchor `#connectivity-alert` resolves
  to a real heading in `docs/howto/federated-observer.md`; both referencing
  links (`daemon-operations.md`, `datawatch-definitions.md`) point at that
  same target; linked sibling files exist.
- **Vault-unreachable badge untouched** (`vaultUnreachableBadge` /
  `_setVaultBadge` still in `app.js`): out of this story's scope; reported
  separately under story `ecbe0003`.
- **Historical records left as-is (deliberate)**: point-in-time plan docs and
  test-run reports (`docs/plans/2026-10-14-pwa-performance-audit.md`,
  `docs/plans/web-list-fetch-audit.md`, `docs/plans/2026-10-25-shared-nav-mobile-fit.md`,
  `docs/plans/2026-10-22-observer-mobile-fit.md`, `docs/testing-tracker.md`,
  `docs/testing/runs/observer-mobile-fit/repro-report.txt`) still mention the
  old nav badge. These describe the pre-change state; rewriting them would
  falsify the record.
- **Changelog** (owned by sibling task `ecbe0001-tpl-changelog_entry`, already
  merged at `e29a30db6`): describes the operator-visible result and the
  five-locale labels.

## Flagged for operator review

1. **Default threshold = 5 minutes, not yet confirmed.** The supervisor
   picked 5 minutes ("open to change" in the planning registry); the docs say
   so explicitly. The operator must confirm or change the default.
2. **Config key name intentionally absent from these docs.** The silence-alert
   setting (threshold, `0` disables) is implemented in story `ecbe0005` and its
   config-reference/howto update (`ecbe000d`) is still queued.
   `federated-observer.md` deliberately does not name a key it has not
   verified; the operator should confirm that task lands before merge so the
   named setting and the five locale strings are documented.
3. **Vault-unreachable badge awaits the operator's word** (planning registry
   row 340): left unchanged; separate read-only report under story `ecbe0003`.
4. **Parity issue for datawatch-app** (alert text + API surface visible to it)
   is drafted under `docs/plans/parity-issues/` and is filed at merge by the
   supervisor, per the PRD constraints.

## Verdict

The docs set for this story is consistent with the shipped code, all links and
anchors resolve, and the open items are the operator-confirmation flags above.

# Release Notes — v6.12.0

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.12.0

### Summary — UX polish + central documentation system

Closes the operator's "Unclassified" batch from `docs/plans/README.md` plus Option A federated-peer-badge breadcrumb. Ships as a minor (new docs system + new card behaviour + new badge interaction).

Plan: [docs/plans/2026-05-05-v6.12.0-uncategorized-batch.md](docs/plans/2026-05-05-v6.12.0-uncategorized-batch.md).

### Added

- **`docs/datawatch-definitions.md`** — the central manual. Every PWA tab and card has a section with description + controls + links. New rule: every NEW card must add a section here before merge. Scaffold + key sections ship in v6.12.0; remaining card prose tracked as BL268.
- **`internal/server/web/index.html` + `app.js`** — `?` help icon in the header bar (left of search). Visible on Sessions / Session detail / Automata / Observer / Settings; deep-links into the matching anchor of `datawatch-definitions.md` (GitHub-rendered until the daemon serves `/docs/` locally — tracked as BL273).
- **Federated-peer stale badge breadcrumb (Option A)** — clicking the cog-icon stale count now navigates to Observer → Federated Peers and flashes the offending peer row red so the operator can find it without hunting. Per-peer card already had the colored health dot.
- **`docs/plans/README.md`** — added BL267 (open-source vault backend), BL268–BL273 deferred sub-tasks for the v6.12.x follow-up work.

### Changed

- **`internal/server/web/style.css` `.session-card.state-{complete,killed,failed}`** — clickable affordances inside greyed cards (`.card-actions`, `.drag-handle`, `.last-response-link`) stay at full opacity. Operator no longer has to guess what's still actionable on a done session.
- **Observer audit log default** — was 50 entries, now 5 (selector keeps 5 / 20 / 50 / 100). Operator's specific request — "audit log should default to 5 entries".
- **Settings → About — System documentation & diagrams** — now links to `docs/datawatch-definitions.md` (the new central manual) plus the existing in-app `/diagrams.html` viewer.
- **`README.md`** — stripped internal `BLXXX` references from current-release section + feature subheaders, kept the `*new in vX.Y.Z*` version annotations (operator clarification: keep the version, drop the BL number). Restored the Daniel Keys Moran "The Long Run" acknowledgement verbatim, with new "Additional Acknowledgements" header above the upstream-project attribution list.
- **`README.md` Secrets Manager description** — clarified: native AES-256-GCM store at `~/.datawatch/secrets.db` is the default; KeePass + 1Password backends are optional / additive, not replacements.
- **`docs/plans/README.md` Unclassified section** — closed every item; consolidated into v6.12.0-batch summary + BL268–BL273 deferral list.

### Removed

- **Settings → About — "Branding / Splash" card** — operator: "the logo and branding is the only thing that isn't a configuration, it is you". `loadBrandingPanel` left in place for now (called from the about renderer site that has been removed; will GC in a follow-up).

### Mobile parity

Datawatch-app issues to file (operator-directed):

- **#74** — Done-state sessions: keep clickable affordances ungrayed (last response, restart, delete, reorder)
- **#75** — Automata: multi-select bar position + new-automaton form layout pass
- **#76** — Federated peer stale indicator on per-peer card + tap-to-navigate from cog badge
- **#77** — Central documentation index + per-card `?` icon helper deep links

### Deferred to v6.12.x

- BL268 — full prose for remaining cards in `datawatch-definitions.md`
- BL269 — Automata help overlay → datawatch-definitions.md anchor
- BL270 — Multi-select bar position in Automata
- BL271 — New-automaton form layout pass
- BL272 — Card buffer/spacing pass for Observer + Settings cards
- BL273 — Daemon-side `/docs/` static file server (so ? icons resolve locally)

### Tests

- 1804 go tests pass
- Smoke: 106 pass / 0 fail / 7 skip (claude-code state-engine variant skipped behind DW_MAJOR=1 per the cost rule)

# Release Notes — v6.12.4

Released: 2026-05-06
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.12.4

### Summary

Multi-area UX + docs + Council enrichment release. Closes the v6.12.x deferred backlog (BL268/BL271/BL272), all of v6.12.3's Automata batch-bar feedback, the multi-tab popup-suppression bug, and adds 2 new Council personas + add/remove API.

### Added

- **Council**: 2 new default personas (`hacker`, `app-hacker`); total goes 10 → 12. Add/remove via REST + MCP + PWA modal; deletes durable across daemon restarts via the existing `.seeded` marker; built-in defaults restorable via `RestoreDefaultPersona`.
- **Council** REST: `POST /api/council/personas`, `DELETE /api/council/personas/<name>`, `POST /api/council/personas/<name>/restore`.
- **Council** PWA: persona modal now has × Remove per persona + collapsible "+ Add Persona" form at the bottom.
- **8 fully-developed how-tos** (per the `docs/howto/README.md` rules): Identity + Telos, Algorithm Mode, Evals, Council Mode, Secrets Manager, Tailscale Mesh, Sessions deep-dive, Channel state engine. Each includes Base requirements → Setup → Walkthrough → Walkthrough → ALL channels reference (PWA / mobile / REST / MCP / CLI / comm / YAML) → Screenshots needed (TODO for operator weekend pass) → Common pitfalls → Linked references. Indexed in the howto README.
- **Settings/Observer card prose**: every listed card in `datawatch-definitions.md` now has its own section + control descriptions + linked references (covers Authentication, Servers, Communication Configuration, Proxy Resilience, Routing Rules, LLM Configuration, Cost Rates, Detection filters, Project + Cluster Profiles, Container Workers, Tailscale, Notifications, Plugin Manager, About + API + Mobile pointer + Orphaned tmux, plus Observer's Inactive backends, Federated peers, Process envelopes, eBPF, Installed plugins, Global cooldown, Session analytics, Audit log, Knowledge graph, Daemon log).
- **Multi-tab popup suppression** (BroadcastChannel presence): tabs in a session detail view post presence every 1 s; sibling tabs check before firing the needs-input popup. Suppressed when ANY tab is in the session AND `suppressActiveToasts` is on.
- **Automata batch bar**: state-aware action buttons. Every action button always shown; per-button count of currently-selected items eligible for that action; click acts only on the eligible subset (leaves ineligible items selected for follow-up actions); long-hold tooltip lists item IDs.
- **Automata batch bar**: horizontal scroll on overflow (desktop + mobile) so action buttons never disappear off-screen on narrow viewports.
- **Automata batch bar**: visible whenever select-mode is on (not just when items are selected). Operator: "the floating action bar doesn't appear until i click on one and it should display when the checkboxes are activated".
- **Automata Select-All**: now ticks every visible row (was: deletable-only). Safe because buttons are state-aware.
- **Automata Templates tab-strip**: Automata + Templates rendered as `output-tab` tab strip matching the in-session tmux/channel/stats pattern. "+ New Template" → "+ Template".
- **New-automaton wizard**: Inferred + Execution sections render as 2-column grid (collapses to single column at <480px). New `wizard-grid-2col` + `wizard-field-label` CSS classes.
- **Settings card spacing**: tightened `.settings-section + .settings-section { margin-top }` from 12px → 6px so adjacent cards visually group.
- **Settings/Comms order**: Proxy Resilience moved under Communication Configuration per operator request.
- **Locale strings**: `automata_tmpl_new_short`, `action_done`, `council_persona_add_title`, `council_persona_add_btn` mirrored across all 5 bundles (en/de/es/fr/ja).

### Mobile parity

Three datawatch-app issues filed: #72 (done-state affordances), #73 (Automata multi-select + form layout), #74 (federated-peer breadcrumb + central docs viewer + multi-tab presence + persona modal).

### Tests

Per the patch-vs-minor rule, this minor release ran the full regression:

- **1804 go tests pass** (`go test ./...`).
- **Smoke 106/0/9** (`scripts/release-smoke.sh`).

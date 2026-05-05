# Release Notes — v6.8.1 (BL257 Phase 2 — Identity Wizard / robot-icon nav)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.8.1
Smoke: 97/0/6

## Summary

BL257 Phase 2 — Identity Wizard. Robot-icon entry point + 6-step interactive flow on PWA + CLI. Closes BL257.

## Added

- **PWA 🤖 robot icon in header** — opens a 6-step Identity Wizard modal. Each step prompts for one identity field (role → north-star goals → current projects → values → current focus → context notes) with the existing answer pre-filled. Final step PUTs the assembled document.
- **CLI `datawatch identity configure`** — interactive 6-step prompt mirroring the PWA wizard. Reads from stdin; press Enter to keep existing value.
- **MCP tool `configure_identity`** — returns wizard-launch instructions (PWA / CLI / direct REST). Multi-step interview can't run inside a stateless tool call.
- **Comm verb `identity configure`** — same instructional message routed via Signal/Telegram/Matrix.
- **Locale**: 6 new keys × 5 bundles (`identity_wizard_title`, `identity_wizard_step`, `identity_wizard_back`, `identity_wizard_next`, `identity_wizard_finish`, `identity_wizard_one_per_line`).

## What didn't change

- Phase 1 surfaces (REST/MCP/CLI/comm/PWA card/locale/wake-up) untouched.
- No new go-mod dependencies.
- No persisted-state migration.

## Mobile parity

[`datawatch-app#53`](https://github.com/dmz006/datawatch-app/issues/53) — comment update with Phase 2 spec (robot-icon nav + wizard modal).

## See also

- CHANGELOG.md `[6.8.1]`
- `docs/plans/2026-05-05-bl257-260-pai-parity-plan.md` — BL257 marked closed; next is BL258 (Algorithm Mode v6.9.0).

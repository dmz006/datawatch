# Release Notes — v6.7.1 (BL255-followup — Skills card buttons)

Released: 2026-05-04
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.7.1
Smoke: 95/0/6

## Summary

Patch release fixing two operator-reported bugs in the v6.7.0 Skill Registries surface:

1. PWA card buttons (Connect / Browse / Edit / Delete + the browse-modal Sync-selected) were rendering but were no-ops on click.
2. Browse modal rows rendered with empty descriptions / dependency hints because the API was marshaling manifest fields in CamelCase but the PWA expected lowercase keys.

## Fixed

- **BL255-followup #1** (`internal/server/web/app.js` `_renderSkillsRegistries` + `_skillsRenderBrowseModal`) — same bug pattern that v5.26.3 fixed for `renderPRDActions`. `JSON.stringify(name)` produces `"name"` with literal `"` characters; embedded inside an `onclick="..."` HTML attribute they terminate the attribute value mid-string and break the handler. Wrapped both `idJ` callsites with `escHtml(JSON.stringify(...))` so the embedded `"` becomes `&quot;` — the browser decodes back when parsing the attribute, so the JS expression remains valid. Affected buttons (all functional now): Connect · Browse · Edit · Delete · Sync selected (in browse modal). The Add default (PAI) and Add registry buttons were unaffected (no string params in their onclick).
- **BL255-followup #2** (`internal/skills/manifest.go` `Manifest` + `Applicability` structs) — JSON marshaling was using Go's default CamelCase field names (`Name`, `Description`, `CompatibleWith`, etc.) because the structs only had `yaml:` tags. The PWA browse modal reads `m.description` / `m.requires` (lowercase) and got `undefined` for every row → silent rendering of empty descriptions and missing dependency hints. Added matching `json:` tags to every field. The browse modal now shows the full PAI skill metadata as designed (verified live: 96 skills with descriptions visible).

## See also

CHANGELOG.md `[6.7.1]` entry.

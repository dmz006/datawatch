# Release Notes — v6.7.4 (BL247-followup hotfix — Observer view empty)

Released: 2026-05-04
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.7.4
Smoke: 95/0/6

## Summary

Hotfix for v6.7.3. The Observer top-level view (which v6.7.3 restored as the new home for the former Settings → Monitor cards + Federated Peers card) rendered empty because `secContent()` — the per-section collapse-state CSS helper used by every `settings-section` block — was scoped local to `renderSettingsView()`. When `renderObserverView()` called it, the JS engine threw `ReferenceError: secContent is not defined` and the whole render aborted.

## Fixed

- **`internal/server/web/app.js`** — promoted `const secContent = (key) => settingsCollapsed[key] ? 'display:none' : ''` to module scope (placed next to the existing module-level `settingsCollapsed`). Removed the now-duplicate inner declaration inside `renderSettingsView()`. Both views share it.

## Process note

The structural change in v6.7.3 was correct. The bug was a reference-scoping miss that smoke (REST-only) didn't surface. Should have visually verified the Observer view in the browser between the v6.7.3 build and the tag — adding "manually open changed PWA views before tagging" to the per-release checklist would have caught this.

## See also

CHANGELOG.md `[6.7.4]` entry.

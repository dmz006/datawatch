# datawatch v5.28.3 — release notes

**Date:** 2026-04-30
**Patch.** BL214 UX fix — language picker promoted + whisper.language tracks app language.

## What's new

### Language picker promoted to the datawatch identity card

Operator-asked 2026-04-30:

> "The language picker should be in the datawatch card at top, not buried under General."
> "Why is there a separate whisper language setting?"
> "Is whisper language not the default app language?"

Three coupled UX problems in one fix:

1. **Discoverability** — v5.28.0 put the picker at Settings → General → Language. That's the second tab; operators expect locale next to the app identity (icon + name + version), which is the **About** card.
2. **Redundancy** — the Whisper config card had its own `whisper.language` form field (free-text input). After picking PWA language in one place, an operator had to remember to also pick it again in the Whisper card for transcription to match. Two settings for what the operator considered one preference.
3. **Default behaviour** — `whisper.language` should *default* to the PWA UI language, not require a separate manual configuration.

**v5.28.3 lands all three:**

- **New "Language" row at the top of Settings → About**, right under the datawatch icon + "AI Session Monitor & Bridge" header — same dropdown options (Auto / English / Deutsch / Español / Français / 日本語) as the Settings → General → Language section, kept in sync via post-render hook.
- **`setLocaleOverride()` now syncs `whisper.language`** via PUT /api/config when picking a concrete locale. Picking `Auto` (browser-detect) deliberately leaves `whisper.language` alone — that path is "follow the browser", not "reset everything else". Best-effort: failure is non-fatal and the page still reloads.
- **Whisper card's `whisper.language` form field replaced with a read-only "tracks PWA language" indicator** pointing at Settings → About → Language as the canonical control. New `readonly` field type added to the config-form renderer for this kind of derived-value display.

The old Settings → General → Language section is kept as well — some operators go to General first when looking for a setting; both pickers stay in sync.

**Configuration parity preserved.** `whisper.language` is still a YAML key + REST `PUT /api/config` field + MCP `config_set` arg + CLI `datawatch config set whisper.language <code>` + chat `configure whisper.language=<code>` — operators who need a different transcription language than UI language (e.g. UI in English but transcribing German speech) can still override through any of those channels. The PWA's read-only display will reflect the override.

## Tests

```
Go build:  Success (via `make build` + `make cross`)
Go test:   1544 passed in 58 packages
Smoke:     run after install
JS check:  node --check internal/server/web/app.js → ok
```

## Backwards compatibility

- Existing `whisper.language` config values are honoured untouched. The PWA picker only writes to `whisper.language` when the operator changes the PWA picker to a concrete locale — never overwrites without an explicit user action.
- The Settings → General → Language picker still works for operators who navigate there; it was kept (not removed) so operators don't have to re-learn placement.
- No data migration. No new schema. No new config keys.

## Upgrade path

```bash
git pull
datawatch update && datawatch restart
# Hard-reload the PWA (cache name → datawatch-v5-28-3).
```

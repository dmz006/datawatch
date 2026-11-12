# feat(settings): rate-limit pattern defaults now per-backend (datawatch next release)

## What changed in datawatch

Rate-limit (throttle) detection now uses a built-in per-backend table in the daemon instead of a generic list. The shipped default of `detection.rate_limit_patterns` is therefore empty (unset), where it used to be a short flat list of phrases. In the PWA, Settings -> Detection Filters -> Rate Limit Patterns now shows no phrases while no list is set, plus a note that the built-in per-backend table applies, instead of displaying the old flat phrases as defaults.

An operator-configured list still works: a non-empty `detection.rate_limit_patterns` replaces the built-in table and is matched as a flat substring on all backends. Clearing the list restores the built-in table.

## Change to mirror in the mobile app

| Item | Value |
|---|---|
| Config key | `detection.rate_limit_patterns` (unchanged key and type) |
| REST field | `rate_limit_patterns` under `detection` in `GET`/`PUT /api/config` (now returns an empty list or null when unset) |
| Displayed default | none; do not seed the field with the former flat phrases |
| Empty state | show a hint that the built-in per-backend table applies, and that adding a pattern replaces the table with a flat substring list for all backends |
| Count label | "built-in table" instead of "N defaults" when the list is empty |

Saving the settings screen must not write the old phrases back: only patterns the operator adds are sent.

## New locale keys

| Key | EN | DE, ES, FR, JA (placeholder) |
|---|---|---|
| `settings_detection_rate_limit_builtin` | built-in table | built-in table |
| `settings_detection_rate_limit_builtin_hint` | No list is set, so the built-in per-backend table applies: each backend is matched only on its own throttle line, at the start of the line. Adding a pattern here replaces the table with a plain substring list for all backends. | same English text |

The DE, ES, FR and JA bundles carry an English placeholder, as the Localization Rule requires. Please send translations through the Compose Multiplatform pipeline; the PWA bundles will mirror them.

## Acceptance

- With no operator list the screen lists no phrases and shows the hint and the "built-in table" label.
- With an operator list the screen lists the entries as custom, without the hint.
- Adding or removing a pattern writes only the operator's entries; removing the last one writes an empty list.
- Both new keys are localized in EN, DE, ES, FR and JA.

Parent release: the datawatch release that ships per-backend throttle matching (next release after 9.31.3).

## Filing record

Filed: https://github.com/dmz006/datawatch-app/issues/296 (2026-10-28, via `gh issue create -R dmz006/datawatch-app`).

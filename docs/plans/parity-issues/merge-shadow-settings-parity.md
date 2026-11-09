# Parity issue draft — the three shadow-merge settings, once they are settings an operator can touch

**Not filed. Fileable now — the parent work made the obligation concrete.**
This is a file-write only task (template: parity_issue_draft): no CLI, no tracker.
When the operator files it, this is the text for the issue; the sibling drafts
`shadow-merge-plan-parity.md` and `shadow-merge-compare-parity.md` remain the
read-side gaps this one depends on.

## Parent work

"Part of finishing review and merge by the daemon (Phase 4 of the autonomy
handover plan)" — the settings step of that work (operator, 2026-11-07). The step
made the three tuning knobs of the shadow merge plan real settings across every
operator-facing surface the operator already uses to set configuration:

- `internal/config/merge_shadow.go` — the `autonomous.merge_shadow` block
  (`enabled`, `gate_timeout`, `plan_keep`), defaults `true` / `20m` / `200`, with
  `Effective*` readers so an unset key shows its default.
- `internal/server/merge_shadow.go` + `api.go` — one shared
  `ValidateMergeShadowValue` behind both `PUT /api/config` (any bad value or
  unknown key rejects the whole PUT with the problem named, nothing written) and
  the CLI, and the `autonomous.merge_shadow` block in `GET /api/config`.
- `internal/router/router.go` — the comm channel's `configure` help lists the
  three keys and they are settable through `configure`.
- `cmd/datawatch/config.go` + `main.go` — `datawatch config
  set/get autonomous.merge_shadow.<key>`.
- `internal/server/web/app.js` — a **Merge shadow** card in the PWA Settings
  (General tab): a toggle for `enabled`, a duration text field for
  `gate_timeout`, a number field for `plan_keep`.
- `internal/server/web/locales/{en,de,es,fr,ja}.json` — seven new keys in all
  five bundles: `merge_shadow_settings_title`, `settings_merge_shadow_enabled`,
  `settings_merge_shadow_enabled_help`, `settings_merge_shadow_gate_timeout`,
  `settings_merge_shadow_gate_timeout_help`, `settings_merge_shadow_plan_keep`,
  `settings_merge_shadow_plan_keep_help`. In `de`, `es`, `fr` and `ja` the values
  are currently the English text, shipped as the placeholder the localization
  process starts from.
- The docs set (`docs/config-reference.yaml`, `docs/operations.md`,
  `docs/datawatch-definitions.md`, `docs/howto/merge-shadow.md`) names all five
  set-and-read surfaces and the defaults.

What this closes on the parity side: the earlier siblings' condition — "no new
settings until the remaining parts of Phase 4 bring them in, at which point
**this one must be filed**" — is now satisfied. The obligation has fired.

## Affected area

The operator surface of the shadow merge plan, on the datawatch-app side: the
Android and iPhone apps have a settings surface of their own (the app mirrors
the operator's settings screens and is a required parity surface), and the new
card above is a new block of settings with operator-visible labels and help
text in five languages. Nothing in this work touched the app.

## Current behavior

An operator can now see and set the three shadow-merge settings on every
daemon-side surface — `datawatch config set/get`, `PUT`/`GET /api/config` as a
flat `{"autonomous.merge_shadow.<key>": …}` object, comm `configure`, the PWA
Settings card, and the YAML `autonomous.merge_shadow` block — with one
validation function enforcing the rules (a boolean or `true`/`false` string; a
positive Go duration; a whole number of at least 1), one bad value refusing the
whole write with the problem named, and `GET` reporting the value in effect.
An operator who opens the Android or iPhone app to adjust these settings finds
no such card and no such keys anywhere in the app, and the seven new
datawatch-side locale strings have no app-side counterpart at all: the app
cannot display a settings card for them, cannot set them, and the four
non-English bundles carry English placeholder text for the card on the daemon
side as well.

## Expected behavior

The app's settings surface carries the **Merge shadow** block with the same
three keys, the same defaults (`enabled` on, `gate_timeout 20m`,
`plan_keep 200`) and the same validation semantics (a refused value is refused
with the problem named on the app side too, and an unset key reads back its
default), settable and readable through the app the way the other
`autonomous.*` settings are. The seven keys are translated into the app's
German, Spanish, French and Japanese via Compose Multiplatform — the
`datawatch-i18n` rule's step 4, i.e. this is precisely the
`feat(i18n): add <key>(s) for <feature> parity` issue its fourth step requires,
not an optional enhancement.

## Suggested next step

1. File one datawatch-app issue (check the tracker first; it was not searched
   for this file-only task). Title along the lines of
   `feat(i18n): add merge-shadow settings keys for shadow-merge-settings parity`;
   name this issue as parent release; include the seven key names from the
   affected-area list; and ask for the translation of the current English
   values (they are the reference copy) into the four non-English locales
   through the Compose Multiplatform localization path.
2. In the same issue or a linked one: the **Merge shadow** card itself — the
   toggle, the duration field and the number field — with the same defaults
   and the same "refused with the problem named" behavior, reading the same
   `autonomous.merge_shadow` block the daemon already serves.
3. Hold the read side of the plan and the comparison to the two sibling drafts;
   the settings are tunable but the record they tune is still not readable
   from any operator surface, and filing that gap is their job, not this one's.
4. Record the app-issue link in the filing record below when it is filed.

## Parity surface (this step)

| Surface | Status |
|---|---|
| REST | **Done** — `GET`/`PUT /api/config` `autonomous.merge_shadow` block |
| MCP | Covered by the generic `config_set` tool (same standing pattern as the sibling settings; no dedicated parameters added) |
| CLI | **Done** — `datawatch config set/get autonomous.merge_shadow.<key>` |
| Comm channel | **Done** — `configure autonomous.merge_shadow.<key>=<value>` |
| YAML / config | **Done** — `autonomous.merge_shadow` block with annotated defaults |
| PWA | **Done** — Merge shadow card, General tab, five locale bundles (four holding EN placeholders) |
| Android | **GAP** — this draft |
| iPhone | **GAP** — this draft |

## Filing record

- Not filed yet.
- Filing rule: datawatch-app issue, title
  `feat(i18n): add merge-shadow settings keys for shadow-merge-settings parity`,
  parent release = this issue; keys to translate: `merge_shadow_settings_title`,
  `settings_merge_shadow_enabled(_help)`,
  `settings_merge_shadow_gate_timeout(_help)`,
  `settings_merge_shadow_plan_keep(_help)`.

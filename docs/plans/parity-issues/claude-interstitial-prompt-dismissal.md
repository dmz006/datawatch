# [sync] Claude Code interstitial prompt dismissal — settings and stats

Parity issue for dmz006/datawatch-app. It covers Android and iPhone/iOS together, so no platform tag is used. The text between the two body markers below is the issue body; the sections after the end marker record the filed issue.

<!-- body-start -->
## Daemon version

Lands with v9.49.1 of the datawatch daemon (the release the branch is targeting). The mobile apps should gate the feature on the daemon reporting that release or later.

## What changed in datawatch

A session that pauses on a Claude Code interstitial prompt — the *"Want to autocompact at 400k tokens instead?"* question, where **Enter** confirms the highlighted option (Autocompact at 400k — a settings change — or Keep auto) — used to hold its lane until an operator noticed it was silent. The daemon now recognises the known prompt kinds from a built-in per-kind table (today: `autocompact-threshold`; idle prompts, quoted wording, startup and decision dialogs deliberately classify as `none`) and dismisses each with the one safe key the classifier names for the kind — **Esc**, never **Enter** (Enter, Tab, space, digits and arrow keys are outside the closed safe-key allow-list).

The dismissal is bounded and fully switchable per daemon, through four config keys:

| Key | Type | Default | Meaning |
|---|---|---|---|
| `session.dismiss_ui_prompts` | bool | `true` | Master switch. `false` = the daemon never sends a dismissal key; the prompt is flagged for the operator instead. |
| `session.ui_prompt_dismiss_delay_seconds` | whole seconds, 0 or more | `5` | Settle wait before the first key. 0 sends it at once. |
| `session.ui_prompt_clear_bound_seconds` | whole seconds, minimum 1 | `30` | How long the prompt must survive after the last key before another key may go to the same still-visible prompt. |
| `session.ui_prompt_max_attempts` | whole number, minimum 1 | `2` | Retry budget per prompt occurrence before the session is flagged waiting-for-input and an operator question is raised (with the last 40 lines of the pane as evidence). |

Every attempt is audited (`ui_prompt_dismissed`, `ui_prompt_not_cleared`, `ui_prompt_refused`, each with kind, key and the option that was on screen) and counted. The counters travel two ways the apps may already read:

- `GET /api/stats` (and the v2 observer stats read) now carries a `ui_prompts` object: `dismissed`, `not_cleared`, `refused` (totals since daemon start) and a `by_kind` breakdown. In-memory — nil (absent from the JSON) until the first tracked dismissal, and reset by a daemon restart.
- Prometheus: `datawatch_ui_prompt_dismissals_total` (counter, labels `kind`, `outcome`).

The PWA already shows the four settings (Settings → Session) with help text and a docs link each; the apps need to match.

## REST contract

The four keys are read and written through `GET`/`PUT /api/config`, exactly like every other `session.*` key. A bad value (a fraction, a non-number, `ui_prompt_clear_bound_seconds` or `ui_prompt_max_attempts` below 1, `dismiss_ui_prompts` not a boolean) rejects the whole `PUT` with HTTP 400 naming the key and nothing changes. Absent keys read their defaults. No restart: a good write applies at once.

```json
{ "session": {
    "dismiss_ui_prompts": false,
    "ui_prompt_dismiss_delay_seconds": 10,
    "ui_prompt_clear_bound_seconds": 60,
    "ui_prompt_max_attempts": 3 } }
```

Locale keys the PWA added (English values in `internal/server/web/locales/en.json` of the datawatch repository; de, es, fr and ja carry them too): `settings_dismiss_ui_prompts`, `settings_dismiss_ui_prompts_help`, `settings_ui_prompt_dismiss_delay_seconds`, `settings_ui_prompt_dismiss_delay_seconds_help`, `settings_ui_prompt_clear_bound_seconds`, `settings_ui_prompt_clear_bound_seconds_help`, `settings_ui_prompt_max_attempts`, `settings_ui_prompt_max_attempts_help`.

## What the mobile settings screen should show

- **Toggle.** "Dismiss Claude Code interstitial prompts (default on)", help "datawatch detects known Claude Code interstitial prompts and closes them with a safe key. Switch it off to answer them yourself.", written as `session.dismiss_ui_prompts`.
- **Number fields.** dismiss delay ("whole seconds, 0 = at once, default 5"), clear bound ("whole seconds, minimum 1, default 30") and max attempts ("whole number, minimum 1, default 2"), each with its PWA help sentence, written as the matching `session.ui_prompt_*` key. A value below the field's minimum (or not whole) is refused in the field the way a 400 reads, and a saved value says it saved.
- **Stats (if the app shows stats or Monitor).** Render `ui_prompts` from the stats read when present: totals `dismissed` / `not_cleared` / `refused` plus `by_kind`. When the object is absent (no tracked dismissal yet on this daemon) show the existing empty state — never an error. Do not show the object on daemons older than the gated version; unknown fields must render as today.

## Acceptance criteria

### Android

- [ ] The toggle and the three number fields read and write the four `session.*` keys through `/api/config` and show the daemon's defaults when unset.
- [ ] A value below a field's minimum is refused locally with the field's sentence and is not sent.
- [ ] The eight locale keys exist in EN, DE, ES, FR and JA, matching the PWA's English values.
- [ ] Setting `session.dismiss_ui_prompts=false` and back to `true` is round-tripped without a restart, and a daemon 400 on a bad value shows the key named.
- [ ] Where stats or a Monitor card exists, `ui_prompts` (absent and present) renders without an error, and the four settings sit in the Session section with their help text.
- [ ] Layout holds at 360 dp width with the longest help sentence open.

### iPhone/iOS

- [ ] Same list as Android, checked at the narrowest supported iPhone width.
- [ ] Dynamic Type at the largest size keeps the four fields, their help text and any refusal sentence readable without horizontal scroll.
- [ ] VoiceOver reads the toggle and each number field with its help sentence.
<!-- body-end -->

## Parity surface

Every surface the change could reach, and whether it changes. Where it does not change, the reason is given.

| Surface | Changes? | Detail or reason |
|---|---|---|
| REST | Yes | Four `session.*` config keys on `GET`/`PUT /api/config` (400 naming the key on bad values); `ui_prompts` object on `GET /api/stats` and the v2 observer stats; both OpenAPI copies updated. |
| MCP | No | The keys travel through the existing `config_set` tool and the stats through `get_stats` — no new tools. |
| CLI | No | `datawatch config set session.dismiss_ui_prompts=...` already writes them; no new command. |
| Comm channel | No | `configure session.dismiss_ui_prompts=false` already works through the existing `configure` verb. |
| YAML/config | Yes | The four keys in `~/.datawatch/config.yaml` under `session:`, with the defaults above. |
| PWA | Yes | Settings → Session already carries the four fields with help text and docs links; locale bundles all five languages. |
| Android | Yes, not built | Needs the settings fields and stats rendering above. Tracked in the issue filed at the merge. |
| iPhone | Yes, not built | The same as Android. Tracked in the issue filed at the merge. |

## Filed issue

The supervisor files this issue at the merge; it is not filed from the branch. The issue body is the text between the two body markers above.

- Issue: not yet filed.

# Datawatch App — Warning event kinds (scope_warning / guardrail_warn) note

Date: 2026-10-25

## What changed

The two opt-in notify event kinds (`scope_warning`, `guardrail_warn`) shipped
on the backend in the immediately preceding release (they only fire because some Automaton opted in —
unlike every other kind, they ride no default set) and until now were buried
unmarked in generic lists in the PWA. This change marks them as opt-in in the
two places where the operator decides which events fire:

1. **Automaton settings modal — "Notification events" panel**
   - New `NOTIFY_EVENT_OPT_IN` constant in `app.js` mirroring
     `internal/autonomous/notify_events.go`'s `OptInNotifyEvents()` —
     `guardrail_warn`, `scope_warning`, `task.session_started`,
     `task.session_cleared`.
   - Kind rows now carry an **`(opt-in)` marker** (key
     `notify_events_opt_in_marker`) alongside the existing `(default)`
     marker; a kind never carries both (the two sets are disjoint, enforced
     by a test), and no non-opt-in kind is marked as opt-in.
   - The bare warning kinds are grouped under a localized **`Warnings`**
     header (`notify_events_group_warnings`, en value "Warnings") at the
     bottom of the kind list, after the Automaton / Task / Session groups.
   - An explainer line (`notify_events_opt_in_help`) states that opt-in
     events are off by default and only notify when listed.

2. **Lifecycle subscription editor (Automaton detail → Subscriptions →
   Add subscription)**
   - `_LIFECYCLE_SUB_KIND_GROUPS` now has a dedicated
     `lifecycle_sub_form_group_warnings` group ("Warnings (opt-in)")
     holding `scope_warning` + `guardrail_warn`; the previous "Other" group
     keeps the session kinds and `prd.updated`.
   - A hint line below the kind grid (`lifecycle_sub_form_opt_in_hint`)
     explains that an opt-in kind only fires when the Automaton's
     notification events include it — the one place the operator might
     otherwise pick it up and wonder why nothing ever delivers.

## Locales (all 5 bundles)

| key | en | de | es | fr | ja |
|-----|----|----|----|----|----|
| `notify_events_opt_in_marker` | opt-in | opt-in | opt-in | opt-in | opt-in |
| `notify_events_opt_in_help` | Opt-in events are off by default and only notify when listed here. | Opt-in-Ereignisse sind standardmäßig deaktiviert und melden nur, wenn sie hier aufgeführt sind. | Los eventos opt-in están desactivados por defecto y solo notifican si se enumeran aquí. | Les événements opt-in sont désactivés par défaut et ne notifient que s'ils sont listés ici. | オプトインイベントはデフォルトで無効で、ここに表示されている場合のみ通知されます。 |
| `notify_events_group_warnings` | Warnings | Warnungen | Avisos | Avertissements | 警告 |
| `lifecycle_sub_form_group_warnings` | Warnings (opt-in) | Warnungen (opt-in) | Avisos (opt-in) | Avertissements (opt-in) | 警告 (オプトイン) |
| `lifecycle_sub_form_opt_in_hint` | An opt-in kind only fires when the Automaton's notification events include it. | Eine opt-in-Art löst aus, wenn die Benachrichtigungsereignisse des Automaten sie einschließen. | Un tipo opt-in solo se dispara cuando los eventos de notificación del autómata lo incluyen. | Un type opt-in ne se déclenche que si les événements de notification de l'automate l'incluent. | オプトインの種類は、オートマタの通知イベントに含まれている場合のみ発火します。 |

Terminology Rule respected: "Automaton" rendered per locale (Automat /
autómata / automate / オートマタ), never the untranslated English word in
de/es/fr/ja, never "PRD", never "Automation".

## Tests

- **Go** — `internal/server/notify_event_kinds_pwa_test.go`: now extracts
  `NOTIFY_EVENT_OPT_IN` from `app.js` and cross-checks it against
  `autonomous.OptInNotifyEvents()` in both directions, asserts every opt-in
  kind is a selectable kind, and asserts the default and opt-in sets are
  disjoint (a kind can never render both markers).
- **Go** — `internal/server/v5280_locales_test.go`: the 5 new
  high-visibility keys are added to the `mustHave` locale-guard list
  (locale-guard: added notify_events_group_warnings,
  notify_events_opt_in_marker, notify_events_opt_in_help,
  lifecycle_sub_form_group_warnings, lifecycle_sub_form_opt_in_hint).
- **Go** — `internal/server/lifecycle_subscription_form_kinds_test.go`
  (existing, unchanged) still pins the subscription-form kind set to
  `autonomous.KnownEventKinds`.
- **JS** — `internal/server/web/app-notify-events.test.js`: the
  `NOTIFY_EVENT_OPT_IN` constant is asserted (exact set, subset of kinds);
  the rendered kind options carry exactly one `(opt-in)` marker per
  opt-in kind (4 total), no marker collisions, and the `Warnings` group
  header resolves through the locale bundle.
- **JS** — `internal/server/web/app-lifecycle-subscriptions.test.js`: the
  rendered form shows the localized "Warnings (opt-in)" group and the
  opt-in explainer below the kind grid.

## REST surface (what the app must know about)

The endpoints do not change (the 2026-10-23 note's set: `POST
/api/autonomous/prds/{id}/set_notify_events`, guardrail-profile create/update,
`GET /api/autonomous/prds/{id}`); the *contents* of two fields change:

- `notify_events` (Automaton, and guardrail profile): two bare names are now
  valid kinds — `scope_warning` and `guardrail_warn`. They take no
  arguments. A `400` on an unknown kind still lists the full valid set, so
  the app can keep deriving the kind list from the daemon instead of
  hard-coding it.
- `notify_events_effective.events`: the resolved list may now contain the two
  warning kinds (with `source` = `automaton` / `profile` / `global`)
  **only** when some tier listed them explicitly — they never enter the
  resolved list from the baseline. A warning kind present in the resolved
  list is therefore always explicitly opted in by the operator.
- Opt-in set (`autonomous.OptInNotifyEvents()`: `guardrail_warn`,
  `scope_warning`, `task.session_started`, `task.session_cleared`) — these
  four are off unless some tier lists them; the default set stays the five
  `prd.*` terminal states. Practical app rule: a kind outside the default
  set is opt-in, and the two new ones are the kinds a warning alert carries.
- Warning delivery: when a warning event fires, the app receives it through
  the existing push path — no new delivery field. Its `kind` is one of the
  two bare names; the level (task / story / Automaton) is carried in the
  event's `level` field, not in the kind. The payload fields are `files`
  (`scope_warning`: the out-of-scope paths), `guardrail`
  (`guardrail_warn`: the guardrail name) and optional `link` (a PWA
  deep-link such as `?alert=<id>` where one was raised alongside).

## Datawatch-app parity

The 5 new keys follow the Android naming convention (`notify_events_*`,
`lifecycle_sub_form_*`). A datawatch-app issue
(`feat(i18n): add notify_events_opt_in_* / lifecycle_sub_form_group_warnings
/ lifecycle_sub_form_opt_in_hint for warning event kinds parity`) should be
filed requesting DE/ES/FR/JA reviews through the Compose Multiplatform
pipeline; the repo ships the translations above until then, and every PWA
call site carries an English fallback.

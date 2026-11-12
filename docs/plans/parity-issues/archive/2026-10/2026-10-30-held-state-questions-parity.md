# feat(questions): held-state questions: Held queue section and mode setting

Parent release: the next datawatch patch above main (no number at the time of writing).

## What changed in datawatch

When an Automaton parks a task in one of the five held states — `blocked`,
`review_held`, `revision_requested`, `awaiting_decision`, `pending_insert` — the
daemon's held-question pass can raise an operator question for it instead of
only recording what it would do. The mode is set by
`autonomous.questions.held_mode` (`off`, `shadow` — the default when the key is
absent — or `on`): in `shadow` mode the pass only writes a "would be raised"
note onto the Automaton's triage record, while in `on` mode it raises open
questions into the question store that are visible in the queue and can be
answered. `autonomous.questions.held_states` names which of the five held state
ids the pass covers: an absent list reads all five, an explicit empty list
covers none, any other value must be a list of the known ids.

A new read-only REST route `GET /api/autonomous/held-questions` reports the
whole surface in one call: the mode in effect, the held state ids it covers,
the open held questions (raised in `on` mode) and the shadow entries the
daemon logged in `shadow` mode. Both config keys are readable through the
existing `GET /api/config` and writable through `PUT /api/config` (which
validates them with the same rules). The mobile app already talks to none of
this surface: it has no Held queue section and no mode setting.

## What to add to the mobile app

| Item | Value |
|---|---|
| Where | the questions queue (the existing questions surface), plus the settings surface |
| REST source | `GET /api/autonomous/held-questions` (GET only; optional `?prd_id=` and `?state=` filters, `state` must be one of the five held state ids), response `{mode, states, open, shadow}` — `mode` is `off`/`shadow`/`on` as in effect; `states` is the covered held state id list (never null); `open[]` entries are `{question_id, state, prd_id, task_id, automation_id, title, reason, recommended_option, default, created_at}` (all fields but `question_id` and optional — empty lists render as `[]`, never null); `shadow[]` entries are `{prd_id, state, recommended_option, at}` |
| Held section in the questions queue | a "Held questions" section on the questions surface, fed from `held-questions`: for each `open[]` entry render the Automaton title (or `prd_id` when absent), the held state, the reason and the recommended option; for each `shadow[]` entry render `prd_id`, state, recommended option and time, under a shadow heading that makes clear shadow mode records these without holding a task; empty `open` + empty `shadow` renders the empty text |
| Mode setting | a "Held questions mode" setting exposing `autonomous.questions.held_mode` as off / shadow / on (absent reads shadow), read from `GET /api/config` and written through `PUT /api/config`; a second "Held question states" control (comma-separated held state ids, empty = all five) is optional but rides the same read/write round trip |
| Answering | no new surface: an open held question is answered through the existing `POST /api/questions/{question_id}/answer` with the chosen option id (and the app's usual actor/surface fields); the hold then resolves exactly like any other answered worker question |

Empty `open` and `shadow` lists mean: render the empty text for that block rather
than a spinner or error.

## New locale keys

| Key | EN | DE, ES, FR, JA (placeholder) |
|---|---|---|
| `held_question_title` | Held questions | Held questions |
| `held_question_mode_off` | off | off |
| `held_question_mode_shadow` | shadow | shadow |
| `held_question_mode_on` | on | on |
| `held_question_empty` | No held questions. A task is added here with a decision to make when an Automaton names an unanswered question in one of the held states. | No held questions. A task is added here with a decision to make when an Automaton names an unanswered question in one of the held states. |
| `held_question_recommended` | Recommended option: %1$s | Recommended option: %1$s |
| `held_question_default_hold` | Held on the default: hold | Held on the default: hold |
| `held_question_answer` | Answer | Answer |
| `held_question_open_automaton` | Open Automaton | Open Automaton |
| `held_question_open_task` | Open task in Automaton | Open task in Automaton |
| `held_question_shadow_title` | Would-be questions (shadow mode records these without holding a task) | Would-be questions (shadow mode records these without holding a task) |
| `held_question_failed` | Failed to load held questions: %1$s | Failed to load held questions: %1$s |
| `held_question_settings_mode` | Held questions mode (off, shadow or on; default shadow) | Held questions mode (off, shadow or on; default shadow) |
| `held_question_settings_states` | Held question states (comma-separated held task state ids; empty = all five) | Held question states (comma-separated held task state ids; empty = all five) |

The DE, ES, FR and JA bundles carry an English placeholder, as the Localization
Rule requires. Please send translations of all fourteen keys for DE, ES, FR and
JA through the Compose Multiplatform pipeline, using the app's own word for
"Automaton" (De: Automat, Es: autómata, Fr: automate, Ja: オートマトン); the PWA
bundles then mirror them in place of the placeholders. The mode words
(`off`, `shadow`, `on`) stay untranslated, as they are config values.

## Acceptance

- The questions surface shows a "Held questions" section fed by
  `GET /api/autonomous/held-questions`: open held questions with Automaton, held
  state, reason and recommended option, and shadow entries under the shadow
  heading.
- Answering an open held question through `POST /api/questions/{id}/answer`
  resolves it; no hold is released by any endpoint on this new surface (it is
  GET only).
- The mode setting writes `autonomous.questions.held_mode` off / shadow / on
  through `PUT /api/config`; an invalid value is surfaced verbatim from the API
  error.
- Empty `open` + `shadow` lists render the empty text, not an error.
- Labels and rows are localized in EN, DE, ES, FR and JA.

## Filed issue

Filed: https://github.com/dmz006/datawatch-app/issues/316
(2026-10-30, via `env -u TMUX -u TMUX_TMPDIR gh issue create -R dmz006/datawatch-app --title '[sync] Held-state questions: Held queue section and mode setting' --body-file docs/plans/parity-issues/archive/2026-10/2026-10-30-held-state-questions-parity.md`).

Exact issue text as filed (title: `[sync] Held-state questions: Held queue section and mode setting`):

````markdown
# feat(questions): held-state questions: Held queue section and mode setting

Parent release: the next datawatch patch above main (no number at the time of writing).

## What changed in datawatch

When an Automaton parks a task in one of the five held states — `blocked`,
`review_held`, `revision_requested`, `awaiting_decision`, `pending_insert` — the
daemon's held-question pass can raise an operator question for it instead of
only recording what it would do. The mode is set by
`autonomous.questions.held_mode` (`off`, `shadow` — the default when the key is
absent — or `on`): in `shadow` mode the pass only writes a "would be raised"
note onto the Automaton's triage record, while in `on` mode it raises open
questions into the question store that are visible in the queue and can be
answered. `autonomous.questions.held_states` names which of the five held state
ids the pass covers: an absent list reads all five, an explicit empty list
covers none, any other value must be a list of the known ids.

A new read-only REST route `GET /api/autonomous/held-questions` reports the
whole surface in one call: the mode in effect, the held state ids it covers,
the open held questions (raised in `on` mode) and the shadow entries the
daemon logged in `shadow` mode. Both config keys are readable through the
existing `GET /api/config` and writable through `PUT /api/config` (which
validates them with the same rules). The mobile app already talks to none of
this surface: it has no Held queue section and no mode setting.

## What to add to the mobile app

| Item | Value |
|---|---|
| Where | the questions queue (the existing questions surface), plus the settings surface |
| REST source | `GET /api/autonomous/held-questions` (GET only; optional `?prd_id=` and `?state=` filters, `state` must be one of the five held state ids), response `{mode, states, open, shadow}` — `mode` is `off`/`shadow`/`on` as in effect; `states` is the covered held state id list (never null); `open[]` entries are `{question_id, state, prd_id, task_id, automation_id, title, reason, recommended_option, default, created_at}` (all fields but `question_id` optional — empty lists render as `[]`, never null); `shadow[]` entries are `{prd_id, state, recommended_option, at}` |
| Held section in the questions queue | a "Held questions" section on the questions surface, fed from `held-questions`: for each `open[]` entry render the Automaton title (or `prd_id` when absent), the held state, the reason and the recommended option; for each `shadow[]` entry render `prd_id`, state, recommended option and time, under a shadow heading that makes clear shadow mode records these without holding a task; empty `open` + empty `shadow` renders the empty text |
| Mode setting | a "Held questions mode" setting exposing `autonomous.questions.held_mode` as off / shadow / on (absent reads shadow), read from `GET /api/config` and written through `PUT /api/config`; a second "Held question states" control (comma-separated held state ids, empty = all five) is optional but rides the same read/write round trip |
| Answering | no new surface: an open held question is answered through the existing `POST /api/questions/{question_id}/answer` with the chosen option id (and the app's usual actor/surface fields); the hold then resolves exactly like any other answered worker question |

Empty `open` and `shadow` lists mean: render the empty text for that block rather
than a spinner or error.

## New locale keys

| Key | EN | DE, ES, FR, JA (placeholder) |
|---|---|---|
| `held_question_title` | Held questions | Held questions |
| `held_question_mode_off` | off | off |
| `held_question_mode_shadow` | shadow | shadow |
| `held_question_mode_on` | on | on |
| `held_question_empty` | No held questions. A task is added here with a decision to make when an Automaton names an unanswered question in one of the held states. | No held questions. A task is added here with a decision to make when an Automaton names an unanswered question in one of the held states. |
| `held_question_recommended` | Recommended option: %1$s | Recommended option: %1$s |
| `held_question_default_hold` | Held on the default: hold | Held on the default: hold |
| `held_question_answer` | Answer | Answer |
| `held_question_open_automaton` | Open Automaton | Open Automaton |
| `held_question_open_task` | Open task in Automaton | Open task in Automaton |
| `held_question_shadow_title` | Would-be questions (shadow mode records these without holding a task) | Would-be questions (shadow mode records these without holding a task) |
| `held_question_failed` | Failed to load held questions: %1$s | Failed to load held questions: %1$s |
| `held_question_settings_mode` | Held questions mode (off, shadow or on; default shadow) | Held questions mode (off, shadow or on; default shadow) |
| `held_question_settings_states` | Held question states (comma-separated held task state ids; empty = all five) | Held question states (comma-separated held task state ids; empty = all five) |

The DE, ES, FR and JA bundles carry an English placeholder, as the Localization
Rule requires. Please send translations of all fourteen keys for DE, ES, FR and
JA through the Compose Multiplatform pipeline, using the app's own word for
"Automaton" (De: Automat, Es: autómata, Fr: automate, Ja: オートマトン); the PWA
bundles then mirror them in place of the placeholders. The mode words
(`off`, `shadow`, `on`) stay untranslated, as they are config values.

## Acceptance

- The questions surface shows a "Held questions" section fed by
  `GET /api/autonomous/held-questions`: open held questions with Automaton, held
  state, reason and recommended option, and shadow entries under the shadow
  heading.
- Answering an open held question through `POST /api/questions/{id}/answer`
  resolves it; no hold is released by any endpoint on this new surface (it is
  GET only).
- The mode setting writes `autonomous.questions.held_mode` off / shadow / on
  through `PUT /api/config`; an invalid value is surfaced verbatim from the API
  error.
- Empty `open` + `shadow` lists render the empty text, not an error.
- Labels and rows are localized in EN, DE, ES, FR and JA.

## Filed issue

(filed below — issue number and exact issue text recorded on filing)
````

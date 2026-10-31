Issue: dmz006/datawatch-app#315

Issue URL: https://github.com/dmz006/datawatch-app/issues/315

# [sync] usage calculator: readings carry a ledger lane stamp

## Body of the issue

The usage calculator no longer takes a lane count from the person typing a reading. The daemon stamps each reading with the Claude lanes it sees running, and the advice reports the lanes running now apart from the what-if lanes. This follows the calculator issue [#286](https://github.com/dmz006/datawatch-app/issues/286).

## Request shapes

`POST /api/usage/readings`

```json
{"percent": 42, "at": "2026-10-30T20:00:00Z", "session_percent": 10}
```

`lanes` is deprecated and ignored. A body that still carries it (any value, even a malformed one) is accepted and the value is dropped. The same holds on MCP (`usage_reading_add`), the CLI (`datawatch usage reading add --lanes`) and the comm channel (`usage reading <percent> lanes=N`).

`GET /api/usage/advice?lanes=N` — `lanes` is the what-if (0 included). It is used for that call only and is never saved.

## Response shapes

Each reading, on the `POST` response and in `GET /api/usage/readings` under `readings[]`:

```json
{"id": "r7", "at": "2026-10-30T20:00:00Z", "percent": 42, "lanes": 2, "lanes_source": "live", "source": "typed"}
```

| `lanes_source` | Meaning |
|---|---|
| `live` | The reading is at the clock; the ledger count now |
| `recorded` | An earlier time the lane history covers |
| `current` | An earlier time the history does not cover, so the ledger count now |
| `unavailable` | No ledger wired; `lanes` is absent |
| `typed` | Stored before the stamp existed; shown as it was and never used by the advice |

The advice carries three lane numbers, kept apart:

| Field | Type | Notes |
|---|---|---|
| `lanes_running_now` | integer or null | The Claude lanes in the capacity ledger at the call; null means not known |
| `lanes_stamped` | integer or null | The stamp on the newest reading whose source is live, recorded or current |
| `what_if_lanes` | integer, omitted | Present only when the call set `lanes` |

## Android, iOS and Wear implications

- **Android:** drop any lanes field from the add-reading form and stop sending `lanes`. Show "Claude lanes running now: N" from `lanes_running_now`, or the not-known line when it is null. Label the lane stepper as a what-if and send its value as the advice `lanes` query parameter; do not store it. Render a reading with `lanes_source` of `unavailable` (no count) or `typed` (old value) without an error. Unknown or absent fields must render as today.
- **iOS:** the same contract and the same acceptance as Android. The iPhone app has no calculator screen yet (tracked in #286), so the stamp fields join that work and no earlier screen changes.
- **Wear:** no change. The watch does not show the calculator. If a lane figure is ever added there, read `lanes_running_now` and treat null as not known.

## Locale keys that need Android translations

Four new keys and one relabelled key. The PWA bundles already carry de, es, fr and ja text for all five; Android should check its translations against them.

| Key | EN value |
|---|---|
| `usage_calc_lanes_label` (relabelled) | Claude lanes running now |
| `usage_calc_whatif_label` | What if: Claude lanes to run |
| `usage_calc_whatif_hint` | Sets how many lanes to run in this what-if. It is not saved and does not change how many lanes are running. |
| `usage_calc_running_now` | Claude lanes running now: %1$s |
| `usage_calc_running_unknown` | Claude lanes running now: not known |

Use the app's own word for Automaton or Automata in operator text (de Automat, es autómata, fr automate, ja オートマトン); never "PRD" in a string the operator reads.

## Acceptance (mobile)

- The add-reading form has no lanes field and sends no `lanes`.
- The calculator shows the lanes running now, or the not-known line when `lanes_running_now` is null.
- The lane stepper is a what-if: it calls the advice with `lanes` and saves nothing.
- A reading with `lanes_source` of `unavailable` or `typed` renders without an error.
- The five keys are localized in EN, DE, ES, FR and JA.

## Parity surface

| Surface | Status | Reason |
|---|---|---|
| REST | Changed | `lanes` deprecated and ignored on the reading request; `lanes` and `lanes_source` on the reading response; `lanes_running_now`, `lanes_stamped`, `what_if_lanes` on the advice; both OpenAPI copies updated |
| MCP | Changed | `usage_reading_add` ignores `lanes`; `usage_advice` `lanes` is the what-if; `usage_readings` returns the stamp |
| CLI | Changed | `reading add --lanes` is deprecated, accepted and not sent; `advice --lanes` is the what-if; the text shows the lane numbers apart |
| Comm channel | Changed | `usage reading` parses `lanes=N` and does not send it; `usage advice lanes=N` is the what-if |
| YAML/config | Not touched | No config key is added or changed, so `docs/config-reference.yaml` needs no edit |
| PWA | Changed | No lanes input on the reading form; "Claude lanes running now" line; the what-if stepper; four new locale keys and one relabelled, in all five bundles |
| Android | Pending, issue filed | [#315](https://github.com/dmz006/datawatch-app/issues/315); the contract and keys are above |
| iPhone | Pending, issue filed | [#315](https://github.com/dmz006/datawatch-app/issues/315); the app has no calculator screen yet, so this joins the work tracked in #286 |

## Filing record

Checked for an open issue first: `gh issue list -R dmz006/datawatch-app --state all --search 'usage calculator lanes'` on 2026-10-30 returned #286, #287 and #288, all about the calculator before this change, so a new issue was filed that points back to #286.

Filed: https://github.com/dmz006/datawatch-app/issues/315 (2026-10-30, via `gh issue create -R dmz006/datawatch-app --title "[sync] usage calculator: readings carry a ledger lane stamp" --body-file <scratch draft>`). Filed exactly once.

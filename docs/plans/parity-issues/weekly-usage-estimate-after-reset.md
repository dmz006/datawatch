# Parity: show the estimated week after a weekly reset (datawatch-app)

Draft only; no issue filed.

## Server behaviour

After a weekly reset with no new usage reading, the usage advice and the burn
guard estimate the new week from the tokens used since the reset.

- `GET /api/usage/advice`: `stale: true`, `stale_reason` says a weekly reset
  passed since the newest reading, `advice.estimated: true`,
  `advice.estimate_note`. `advice.percent` is the estimate; `advice.read_at` is
  the older reading's time. There is no `copied_at`. `advice.daily` and
  `advice.series` are empty and there is no `advice.session`. Without token
  data: `advice.needs: "reading"`.
- `GET /api/usage/burn-guard`: `last_projection.estimated`, and
  `week_from_tokens` on a pace decision.

## Mobile client

- Mark the percentage as estimated wherever it is shown, with the plain note
  "Estimated from tokens since the reset; enter a reading to correct it."
- Do not show a "last copy" stale line when `copied_at` is absent.
- Do not prefill a reading form with an estimated percentage.
- New strings in en, de, es, fr and ja.

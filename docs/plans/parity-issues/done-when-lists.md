# Parity: "done when" lists (datawatch-app)

Draft only; no issue filed.

## Server behaviour

Report only, behind `acceptance.mode` (`off` default, `report`). Nothing is
blocked because of a list.

- `GET /api/acceptance` answers `{"mode":"off"}` or every list in short with
  `overview.groups` (open items grouped by source and reason).
- `GET /api/acceptance?id=<request>` returns `list`, `rollup` (`status`,
  `label`, `done`, `plain_done`, per-item `result`,
  `accepted_without_proof`, `claim`, `changed`), `flags`, `changes` and
  `hash`. 404 means the request reads "no criteria".
- Writes: `POST /api/acceptance/edit` (and `freeze` with `hash`),
  `/confirm`, `/accept`.

## Mobile client

- A "Done when" table on an Automaton's overview, hidden when the mode is off.
- Show every status and result as itself. Only `done` and `done_accepted` are
  done; never show an unknown value, "delivered", "filed" or
  `met_on_changed_list` as done. Show accepted items as "accepted without
  proof", never met, and a `claim` as a claim.
- Actions: view, edit, confirm, accept. Each sends the words that were said and
  the list hash.
- A list view with open items grouped by reason.
- Strings: the `done_when_*` and `settings_acceptance*` keys in
  `internal/server/web/locales/`, in en, de, es, fr and ja.
- Android Auto and voice: out of scope for the first version.

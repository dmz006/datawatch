# Parity: shared cache sweep switch and withdrawn finished-work questions (datawatch-app)

Draft only; no issue filed.

## Server behaviour

- New setting `autonomous.automation.cleanup_shared_cache_sweep` (boolean,
  default false). `GET /api/config` and `GET /api/autonomous/config` carry
  `cleanup_shared_cache_sweep`; `PUT` accepts only true or false. It lets the
  cleanup remove Go build and Trivy scan caches in the temp folder, whichever
  program made them; `cleanup_mode` still decides off, report or on.
- A withdrawn finished-work question is not raised again until the branch moves
  or 14 days pass. While settled, the finding in
  `GET /api/autonomous/finished-work` has no `question_id`, carries `reask_at`
  and says so in `action`. The Automaton's decision log gains the kind
  `finished_work_withdrawn`.

## Mobile client

- If the app shows the Automata cleanup settings, add the toggle, off by
  default, with the full label: it must say that caches made by any program are
  removed. New string in en, de, es, fr and ja.
- If the app lists finished work, do not assume a finding has a question; show
  its `action` text and `reask_at`.
- If the app shows decision-log kinds by name, accept
  `finished_work_withdrawn`.

# Mobile parity: what datawatch may merge, and accepting a commit

Server change: one merge selection rule, and an acceptance of an exact commit.

## What the web app does

- On an Automaton that is completed or needs_review with a pending landing, a
  button **May datawatch merge it?** under the landing block.
- It reads `GET /api/autonomous/merge?automaton=<id>` and shows the first entry
  of `eligible` or `excluded`: when eligible, whether by passed review or by
  acceptance, with the commit; otherwise every string in `reasons`.
- When excluded and `tip` is set, **Accept commit <tip> for merge** asks for a
  reason (empty cancels) and posts
  `POST /api/autonomous/prds/{id}/landing/accept` with
  `{"actor": "operator", "commit": "<tip>", "reason": "<text>"}`. A 400 body
  says why it was refused; show it.
- Show the full commit id: the operator is accepting exactly that commit.

## Locale keys

`merge_select_check_btn`, `merge_select_review_passed`,
`merge_select_accepted`, `merge_select_excluded`, `merge_select_accept_btn`,
`merge_select_accept_prompt` (en, de, es, fr, ja in the web bundles).

## Parity surface

REST, MCP, CLI, comm channel and PWA are done in the server repository. No
YAML setting was added. Android and iPhone: this note.

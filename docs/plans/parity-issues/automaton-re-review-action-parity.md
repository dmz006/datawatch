# [sync] Re-review action on finished Automata whose completion review is open

Draft only. No issue has been filed on any tracker.

## Affected area

The Automaton detail screen and its overflow actions in the datawatch-app mobile clients (Android and iOS). REST, MCP, CLI, chat and the PWA already have the action.

## What the daemon does

The re-review (re-judgment) re-runs the completion rules review of an Automaton as a fresh judgment. It never resets an Automaton: no task goes back to pending and no story reopens. Every task must already be `completed` or `cancelled`, with at least one `completed`.

- **Who it applies to:** a `blocked` Automaton, one held for review whose head has moved, or a `completed` or `archived` Automaton whose `merge_state` is `finished_not_merged` and whose review is still open (no verdict yet, the last verdict a block, or a pass judged on an older head). A finished Automaton whose work is already on the base branch, or whose latest pass covers the current head, is refused with a message naming the states the action applies to.
- **What the verdict does:** a pass clears the stale block (a blocked Automaton goes to `completed`; a finished one keeps its status); a block flips the record to `blocked` with the new reason; no verdict keeps the status and returns an error. The outcome is written to the decision log first (kind `guardrail_rejudge`, with `judged_commit_sha` and `verdict_reused`).
- **Replay or fresh:** a fresh review when the review head or the rules text changed since the stored verdict; the stored verdict is reused only when both are unchanged and the per-commit verdict cache is on.
- **Auto re-review:** when a completion auto-fix merges back into the Automaton branch, the daemon runs one re-review of the new head itself.

Calls: `POST /api/autonomous/prds/{id}/rejudge` with body `{"actor": "...", "backend": "..."}` (backend optional, it pins the judge). With `?view=compact` the reply carries a plain-language `message` saying whether a new review ran or the stored verdict was reused.

## What the PWA does

- A "Re-review" button (locale keys `prd_btn_rejudge`, `prd_btn_rejudge_title`) in the Automaton detail header, shown for `blocked`, `completed` and `archived` records. The server is the authority: a refusal comes back with its reason and is shown in an error notice.
- It posts `{actor: "operator"}` to the compact view and shows the reply `message` for six seconds, then refreshes the record. With no message it shows a short success notice.
- When a review timed out the card also shows Retry and Reroute buttons that call the same endpoint (Reroute pins the Automaton's stronger guardrail backend).

## Expected behavior in the apps

1. Detail actions: add Re-review for `blocked`, `completed` and `archived` records; call the compact view; show `message`; refresh.
2. Show the server's refusal text as is. Do not decide locally whether the action applies.
3. Keep Retry and Reroute on the timed-out review card if the app shows that card.
4. Reuse the locale strings.

## Compatibility

No migration is needed. Older apps simply lack the button.

## Tests

Daemon side: the re-judgment tests and the surface tests for REST, MCP, CLI and chat. PWA side: node tests for the button and the reply handling.

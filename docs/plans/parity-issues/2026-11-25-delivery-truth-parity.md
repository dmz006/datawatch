# Parity draft: answers, wake-ups and sends say whether they arrived

Draft for the datawatch-app mobile client. File only; no issue has been filed.

## What changed in the daemon
- A task resumed by a decision carries `decision_resume.delivery`:
  `waiting`, `sending`, `sent`, `not_typed` or `review_needed`, with
  `delivery_reason`, `delivery_session_id` and `delivered_at`. All are
  optional; a record without `delivery` says nothing about the send.
- A task also carries `owed_decisions` (`question_id`, `session_id`,
  `waiting_noticed`): blocking questions still waiting for their line.
- The supervisor wake status gains `pass_count` (passes event wakes ran).
  `event_count` and `last_event` now cover only wakes whose line was typed; the
  web status line shows both counts (`supervisor_wake_status_passes`).
- New alerts through the existing alert list: *Automaton decision not
  delivered yet*, *Automaton decision: review needed*, *Supervisor wake not
  delivered yet*, *Supervisor wake: review needed*, *Subscription delivery:
  review needed* and *Discussion entry not delivered to a peer*.
- A discussion write can now fail with HTTP 500 when its log entry is not
  saved.
- A command forwarded to another server now reports a refused or unreadable
  reply as an error.

## Surfaces
| Surface | State |
|---|---|
| REST, MCP, CLI | the task fields are part of the task record; alerts use the existing alert reads |
| Comm channel | alerts are announced as other system alerts are |
| Web app | shows the alerts; no new task badge |
| YAML / config | no new setting |
| Android, iPhone | to do: alerts need no change; show the delivery state on a task where decisions are shown, with `review_needed` marked as needing a person |

## Words for the app
- `not_typed`: "Decision not delivered yet"
- `review_needed`: "Check the session: the decision may be missing"

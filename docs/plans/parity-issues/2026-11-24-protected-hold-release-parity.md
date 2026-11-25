# Parity draft: protected-path holds stay held unless a daemon-only change is proven

File only; no issue filed.

## What changed in the daemon

- The start-up check that releases a false-positive protected-paths hold now
  keeps the hold when the question was answered or is no longer open, when the
  task's own working tree is not recorded, and when no daemon-only diff is
  shown.
- The commit gate no longer treats any change to a file that is not a rules
  file as the daemon's own.
- A planner that fails after its Automaton was closed no longer writes over the
  closed record.

## Mobile client

No change needed. There is no new setting, endpoint, field, status, decision
kind or text. A client sees more holds stay held, with the same fields and the
same operator question as before.

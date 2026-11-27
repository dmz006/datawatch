# Parity: slots of resumed workers (datawatch-app)

Draft only; no issue filed.

## Server behaviour

No new setting, endpoint, field, command or wait reason.

- A resumed worker session (after a question, an outside wait, a retry in the
  same session, or a restart) waits for a capacity slot and holds it while the
  session runs. A task can show `waiting_capacity` in two places where it did
  not before: a retry that continues the same session (plain pool reason), and
  a parked worker found again after a restart (its `await_external` reason).
- A task that finds its project tree taken gives its slot back while it shows
  the `tree lock:` reason, then queues for a slot again.

## Mobile client

No change needed: every state uses wait reasons the app already shows.

# Durable session notes: usage

## What it is for

Capturing and retrieving the facts a team should keep between working
sessions: decisions, constraints, corrections and pointers. It also keeps the
notes tidy by updating duplicates and removing wrong entries.

## Inputs you supply

- `NOTES_STORE`: where the notes live.
- `SCOPE`: the project or topic.
- `RETENTION_DAYS`: how old a note may be before it is re-checked.
- `FORBIDDEN_CONTENT`: what must never be stored.

## Example

Prompt: "Save what we learned today. NOTES_STORE is the notes folder in the
repository, SCOPE is the billing service, RETENTION_DAYS is 60, and never store
secrets or customer data."

Result: one short note per lasting fact with its reason and date, an updated
index line for each, and a short report of what changed.

---
name: durable-session-notes
description: Record, recall and prune the facts a team needs to keep between working sessions, using a notes store the user names.
version: "1"
tags: [memory, notes, handoff, knowledge]
compatible_with: [any]
applies_to:
  backends: [any]
cost_hint: low
---

# Durable session notes

Keep short, verified facts that outlive a single working session, so the next
session starts informed instead of rediscovering the same things.

## Inputs the user supplies

- `NOTES_STORE`: where notes live (a folder, a wiki page or a note tool).
- `SCOPE`: the project or topic the notes belong to.
- `RETENTION_DAYS`: how old a note may be before it needs re-checking.
- `FORBIDDEN_CONTENT`: kinds of content never to store (for example secrets,
  personal data, internal addresses).

If any input is missing, ask for it once before writing anything.

## Steps

1. Read the existing notes in `NOTES_STORE` for `SCOPE`. Note titles and dates.
2. Decide what is worth keeping. Keep facts that are not obvious from the work
   itself: decisions and their reasons, constraints, corrections the user gave,
   and pointers to external resources. Skip anything the project files or
   history already record.
3. Check each candidate against `FORBIDDEN_CONTENT`. Drop or generalise any
   candidate that matches. Never store credentials.
4. Look for an existing note on the same fact. Update it rather than adding a
   duplicate. Delete a note that turned out to be wrong.
5. Write each new note as one fact with: a short title, the fact, the reason
   it matters, when to apply it, and today's absolute date.
6. Add or refresh a one-line entry for the note in the index of `NOTES_STORE`.
7. When recalling, search by topic, then check that any named file, command or
   setting still exists before relying on the note. A note older than
   `RETENTION_DAYS` is a hint to verify, not a fact.
8. Report what was added, updated and removed, in plain titles.

## Rules

- One fact per note; link related notes by title.
- Use absolute dates, never "yesterday" or "last week".
- Never store anything listed in `FORBIDDEN_CONTENT`.
- State plainly when a recalled note could not be verified.

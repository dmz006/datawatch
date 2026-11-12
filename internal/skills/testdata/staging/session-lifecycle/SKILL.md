---
name: session-lifecycle
description: Start, resume, tag, stop and clean up AI coding sessions safely, with explicit confirmation before any kill, delete or stop-all action.
version: "1"
tags: [sessions, lifecycle, start, resume, cleanup]
compatible_with: [any]
applies_to:
  backends: [any]
cost_hint: low
---

# Session lifecycle

Run the whole life of a coding session in a controlled way: start it in the
right place with the right engine, find and resume it later, tag it so it can
be found again, and end it without destroying work by accident.

## Inputs the user supplies

- `PROJECT_DIR_OR_ALIAS`: the project directory or the short alias that names it.
- `TASK_PROMPT`: the work the session should do, in plain words.
- `BACKEND`: which coding engine (and model, if relevant) should run it.
- `SESSION_REF`: an existing session to resume, or the tag to find it by.
- `TAGS`: labels to attach so the session can be found later.

If `PROJECT_DIR_OR_ALIAS` or `TASK_PROMPT` is missing when starting, ask for it
once before doing anything. Never guess a project or a backend.

## Steps

### Start

1. Resolve `PROJECT_DIR_OR_ALIAS` to one real directory. If an alias matches
   more than one directory, or none, stop and ask.
2. Check the list of existing sessions for that directory. If one is already
   running the same task, offer to resume it instead of starting a duplicate.
3. Confirm `BACKEND` is available. If it is not, name the available choices and
   ask; do not substitute silently.
4. Start the session with `TASK_PROMPT`, then attach `TAGS`.
5. Report the new session by its name and state, not just an identifier.

### Resume

6. Find the session from `SESSION_REF` by identifier first, then by tag. If
   several match, list them with name, state and last activity and ask which.
7. Read its recent output and state before sending anything. Resume only a
   session that is waiting or finished; do not interrupt one that is mid-work.
8. Send the follow-up instruction, then confirm the session picked it up.

### Stop and clean up

9. Prefer the gentlest action that solves the problem: wait, then send input,
   then stop, and only then kill.
10. Before any kill or delete, show the user the session name, state, project
    and whether it holds uncommitted work, and ask for an explicit yes.
11. Treat a stop-all action as a separate, larger decision. List every session
    it would affect and require a fresh, explicit confirmation naming the count.
12. After a kill or delete, list sessions again and report what remains.

## Rules

- Never kill, delete or stop-all without an explicit confirmation given in this
  conversation for that exact action; earlier approval does not carry over.
- Never delete a session that has unsaved or uncommitted work without saying so.
- Never start a second session for work an existing session is already doing
  unless the user asks for it.
- Report outcomes plainly, including failures, using session names.

# Session lifecycle: usage

## What it is for

Starting, resuming, tagging and ending coding sessions in a safe order. It
avoids duplicate sessions and makes sure nothing is killed, deleted or
stopped in bulk without an explicit confirmation.

## Inputs you supply

- `PROJECT_DIR_OR_ALIAS`: the project directory or its alias.
- `TASK_PROMPT`: the work to do.
- `BACKEND`: the coding engine to run.
- `SESSION_REF`: a session to resume, or a tag to find it by.
- `TAGS`: labels to attach.

## Example

Prompt: "Start a session for the billing project to fix the failing invoice
test. PROJECT_DIR_OR_ALIAS is billing, BACKEND is the default coding engine,
TAGS is invoice-fix."

Result: the alias is resolved, existing sessions are checked for duplicates,
the session is started and tagged, and its name and state are reported.

## Note

No second-ranked skill was added: the gap ranking report was not present in the repository, so no rank-2 gap could be read.

No third-ranked skill was added: the gap ranking report was not present in the repository, so no rank-3 gap could be read.

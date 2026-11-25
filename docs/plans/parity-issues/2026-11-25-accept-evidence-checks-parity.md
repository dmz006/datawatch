# Mobile parity draft: Accept checks the held task's evidence

Draft only. No issue has been filed.

## Change in the daemon

Accept on a task held for review now needs more than the worker's completion
marker. The daemon also needs a task commit (not a pre-task snapshot), every
file the task names, and a passing run of the task's verification script. A
refusal is an ordinary error on `resolve_task_review` whose text names what is
missing. There is no new endpoint, field or setting.

## What datawatch-app needs

- No change unless the app offers Accept.
- If it does: show the daemon's error text as it is, and allow for a slow
  response, because the request waits while the task's check runs (up to the
  script gate time limit). On a client timeout, read the task again instead of
  assuming that Accept failed.
- Do not add a force or override control.

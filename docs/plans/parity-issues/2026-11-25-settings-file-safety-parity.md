# [sync] A session start or restart can now be refused over a project's opencode.json

## Body of the issue

datawatch no longer rewrites a tool settings file it cannot read and parse in full. For the `opencode` and `opencode-acp` backends this can refuse a launch.

### What the app can see

| Where | What |
|---|---|
| `POST /api/sessions/start` | HTTP 500 with a plain-text body that starts `launch refused:` and says why (the file, and what is wrong with it) |
| `POST /api/sessions/restart` | the same; the session record is unchanged and a running session keeps running |
| Automaton tasks | the task is retried on the infrastructure retry schedule, then held; the reason carries the same text |

### Wanted in the app

Show the response body of a failed start or restart to the user as it is, not only a generic failure. No new field, setting, verb or locale key.

# Mobile parity issue draft — discussion id validation (path traversal fix)

- **Status:** draft only. No issue has been filed on any tracker.
- **Target repo:** dmz006/datawatch-app
- **Suggested title:** chore(parity): discussion ids are now validated server-side — check the mobile client against the new rules

## Affected area

Discussion routes and the discussion scope, sync and file-service code in the server (`internal/server/discussion_id.go`, `bl332_discussion_scope.go`, `bl332_discussion_sync.go`, `bl333_file_service.go`). This work fixes the high-severity code scanning alerts about discussion ids reaching file paths. It adds no new screen or setting, so the parity surface is the id format the mobile client sends and displays.

## Current behavior

Before this work, a discussion id taken from a request could be used to build a file path without a check. A crafted id containing `..` or a path separator could point outside the discussion store. The mobile client has no matching rule. It passes through whatever id the user or the server gives it.

## Expected behavior

The server now accepts a discussion id only if it is 1 to 128 characters of ASCII letters, digits, dot, underscore or hyphen. The names `.` and `..` are rejected. Any other id returns the error text `invalid discussion id`. The scope, sync and file-service paths call this check before touching the filesystem. A traversal test covers the routes.

Mobile expectations:

- Any discussion id the client creates or types must follow the same format.
- The client must show the `invalid discussion id` error as a normal validation message, not a generic failure.
- Existing stored ids that break the rule (spaces, slashes, non-ASCII, longer than 128) will now be refused by the server. The client should handle that without crashing or retrying in a loop.

## Suggested next step

Open one issue in datawatch-app to audit where the client builds or accepts discussion ids, add the same format check on input, and map the `invalid discussion id` response to a clear message. Also ask the operator whether any existing deployment holds ids that break the rule, since those would need a rename or migration. Mark this issue as a follow-up to the next release that includes the fix.

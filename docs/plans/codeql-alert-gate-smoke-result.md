# CodeQL alert gate smoke test (2026-10-30)

Read-only run of the two `run:` blocks of `.github/workflows/codeql.yaml`, extracted
with `python3 -I` + yaml into `.datawatch/codeql/smoke/` (gitignored) and run with bash
against `dmz006/datawatch`. Nothing was written to GitHub.

Environment: `GH_TOKEN` from `gh auth token`, `REPO=dmz006/datawatch`,
`REF=refs/heads/main`, `GITHUB_EVENT_NAME=push`. The wait step is gated by the step-level
`if:` on the event name, so the extracted `run:` block does not read it.
`SHA` = newest default-setup analysis on main = `2aa267b60320a97ed2394b638de68069255e7217`.

| # | Step | Input | Result |
|---|------|-------|--------|
| 1 | Wait | real SHA | exit 0 at once: "All default setup analyses are present" |
| 2 | Wait | SHA=0000...0000, WAIT_MAX_SECONDS=20, WAIT_INTERVAL_SECONDS=5 | exit 1 after 20 s; error names all four missing categories: `/language:actions /language:go /language:javascript-typescript /language:python` |
| 3 | Gate | refs/heads/main | exit 1: 0 critical, 5 high open alerts; no HTTP 403/404 |

Open high alerts listed by the gate (sorted):

- 723 `js/disabling-certificate-validation` scripts/test-stories/pwa/TS-833.mjs
- 724 `js/disabling-certificate-validation` scripts/test-stories/pwa/TS-835.mjs
- 725 `js/disabling-certificate-validation` scripts/test-stories/pwa/TS-834.mjs
- 726 `js/double-escaping` internal/server/web/app-questions-history.test.js
- 729 `go/disabled-certificate-check` internal/autonomous/supervisor_watch_host.go

Conclusion: the wait and gate steps behave as designed against the live repository. The gate
failure in step 3 is a valid outcome (real open alerts), not a workflow fault.

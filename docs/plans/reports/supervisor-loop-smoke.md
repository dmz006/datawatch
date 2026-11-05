# Supervisor loop live smoke check

Run against an isolated test daemon (fresh config directory, sandbox template), never the operator's daemon.

| Check | Result |
|-------|--------|
| GET /api/config returns every supervisor_loop key (23 keys) | pass |
| PUT then GET round trip on one key | pass |
| GET /api/supervisor-loop/jobs lists 3 model jobs and 8 mechanical steps | pass |
| POST run of a mechanical step yields a run record in GET /api/supervisor-loop/runs | pass |
| Model job with no model configured escalates with an attempt summary | pass |
| CLI `supervisor-loop jobs` | pass |
| CLI `supervisor-loop runs` | pass |
| Daemon PID teardown (cmdline check, end, port closed) | pass |

Totals: 8 pass, 0 fail.

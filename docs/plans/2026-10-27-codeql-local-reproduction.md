# Local reproduction of the open CodeQL alerts (2026-10-27)

Summary of the local CodeQL run. The full working files live in `.datawatch/codeql/`, which is
gitignored (`.gitignore` line 1), so they are not committed: `rescan.sh`, `REPRODUCE.md` (dataflow
source, steps and sink for each of the 21 alerts), `baseline/*.sarif`, `out/*.sarif`.

## Result: the local scan does not match the live list

Every one of the 21 open alerts (GitHub `state=open`) is reproduced locally at the same rule, file and
line. The local scan reports 108 results in total, because a local run cannot know about dismissals
and reports the alerts GitHub has dismissed again.

| rule | live open | local |
|---|---|---|
| go/path-injection | 14 | 63 |
| go/command-injection | 2 | 5 |
| go/reflected-xss | 3 | 4 |
| js/unvalidated-dynamic-method-call | 1 | 1 |
| js/xss | 1 | 3 |

All 84 distinct extra rule/file/line triples match an alert GitHub reports as `dismissed`.
`.datawatch/verify/reproduce-local-codeql-alerts.sh` expects 14, 2, 3, 1, 1, so it exits 1 with
"local scan does not reproduce the open alert counts". The expected numbers were not changed.
To compare like with like, filter the local results to the rule/file/line triples of the open alerts.

## Commands

Run from the project root. `CODEQL=.datawatch/codeql/bundle/codeql/codeql` (CLI 2.27.3).
`.github/workflows/codeql.yaml` sets no `queries:` block, so each language uses its default
code-scanning suite.

```bash
.datawatch/codeql/rescan.sh

env -u TMUX -u TMUX_TMPDIR $CODEQL database create .datawatch/codeql/db/go --language=go --build-mode=autobuild --source-root=. --overwrite
env -u TMUX -u TMUX_TMPDIR $CODEQL database create .datawatch/codeql/db/javascript-typescript --language=javascript-typescript --build-mode=none --source-root=. --overwrite
env -u TMUX -u TMUX_TMPDIR $CODEQL database analyze .datawatch/codeql/db/go codeql/go-queries:codeql-suites/go-code-scanning.qls --format=sarifv2.1.0 --output=.datawatch/codeql/out/go.sarif
env -u TMUX -u TMUX_TMPDIR $CODEQL database analyze .datawatch/codeql/db/javascript-typescript codeql/javascript-queries:codeql-suites/javascript-code-scanning.qls --format=sarifv2.1.0 --output=.datawatch/codeql/out/javascript-typescript.sarif
python3 -I .datawatch/verify/codeql_sarif.py list .datawatch/codeql/out

gh api 'repos/dmz006/datawatch/code-scanning/alerts?state=open&per_page=100' --paginate
```

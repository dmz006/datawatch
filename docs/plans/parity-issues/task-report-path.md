# Mobile parity issue — read-only task `report_path`

- **Issue number:** #352
- **Issue URL:** https://github.com/dmz006/datawatch-app/issues/352
- **Repo:** dmz006/datawatch-app
- **Title:** feat(parity): read-only task report_path — one report file per read-only task (add_task / edit_task)

## What changed in datawatch (unreleased)

A read-only task (one that changes no files, so it may skip the tree lock when `autonomous.parallel.read_only_tasks` is on) now carries an optional `report_path`: the one clean repo-relative file under `.datawatch/reports/` it is allowed to write. The dirty check (`git status --porcelain --untracked-files=all -- . ":(exclude).datawatch" ":(exclude)report_path"`) excludes exactly that path, so the report is the task's only allowed write.

Contract changes, both new **optional** request fields (absent = today's behavior):

- `POST /api/autonomous/prds/{id}/add_task` — `report_path` (string, requires `read_only: true`)
- `POST /api/autonomous/prds/{id}/edit_task` — `report_path` (string; omitted = unchanged, `""` clears; requires the task to be read-only, including via `read_only: true` in the same call)
- Task objects (responses, `GET /api/autonomous/prds/{id}` and story detail) gain `report_path` (string, absent = the task writes nothing)

## Request / response shapes

### add_task (200)

```
POST /api/autonomous/prds/e4fb0001/add_task
{
  "story_id": "s2",
  "title": "Collect the gate counts",
  "spec": "Run the plan coverage gate and write the three counts to the report",
  "read_only": true,
  "report_path": ".datawatch/reports/coverage-gates.txt"
}
```

→ `200`:

```json
{
  "id": "e4fb0001",
  "story": [
    { "title": "Fix the gate",
      "tasks": [ { "id": "t3", "title": "Collect the gate counts",
                   "read_only": true,
                   "report_path": ".datawatch/reports/coverage-gates.txt" } ] }
  ]
}
```

### add_task (400, report path on a writer)

```json
{"error": "report_path requires a read_only task"}
```

### edit_task (200)

```
POST /api/autonomous/prds/e4fb0001/edit_task
{ "task_id": "t1", "read_only": true, "report_path": ".datawatch/reports/gate.txt" }
```

→ `200` with the same Automaton shape; the edited task carries `read_only: true` and the `report_path`.

## Field meanings

| Field | Type | Meaning |
|---|---|---|
| `report_path` (request) | string | The one clean repo-relative report file this task may write. Must be under `.datawatch/reports/`; no `..`, no absolute paths. Valid only with a read-only task. Empty = none (add) / unchanged (edit). |
| `report_path` (task in response) | string | Same value stored on the task; absent = the task writes nothing. |
| `read_only` (task in response) | bool | Unchanged field; pairs with `report_path`. `true` + valid `report_path` = the task may write exactly that one file. |

## App surface requested

1. Task cards: show the report path next to the read-only mark (e.g. a muted path line or chip); absent = nothing extra.
2. Add/edit task forms: a path field enabled only while `read_only` is on; on save send `report_path` only when set/changed; surface a `400` message verbatim.
3. Backward compatible: an app that ignores the field keeps sending exactly today's payloads.

## datawatch-side verification

- REST: `internal/server/task_report_path_rest_test.go` (add/edit set, writer refused, invalid path refused, add rejected before creation, OpenAPI documents it)
- Router: `internal/router/task_report_path_test.go` (chat/comm channel `add task` / `edit task` with a `report path` token)
- Core: `internal/autonomous/task_report_path_test.go` (manager set/clear/refuse paths), `internal/autonomous/read_only_report_path_test.go` (path validation), `internal/autonomous/read_only_gate_report.go` + `read_only_gate_report_test.go` (gate counts report writer/parser), `internal/autonomous/read_only_parallel_test.go` (`TestReadOnlyTaskThatDirtiesTreeFails` — the dirty check a report path must not trip)
- CLI parity: `--report-path` on `prd-add-task` / `prd-edit-task` (`cmd/datawatch/cli_autonomous.go`), body tests in `cmd/datawatch/task_read_only_cli_test.go`
- MCP: `report_path` accepted on `autonomous_prd_add_task` / `autonomous_prd_edit_task` (`internal/mcp/task_report_path_test.go`)
- OpenAPI: `docs/api/openapi.yaml` + `internal/server/web/openapi.yaml` (both endpoint request schemas + task schema)

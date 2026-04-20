# `GET /api/project/summary` — project overview

**Shipped in v3.5.0 (BL35).** Returns a structured snapshot of a
project directory: git status, recent commits, sessions that have
worked on the directory, and aggregated stats. Read-only.

---

## When to use

- An AI agent landing on a new task wants situational awareness:
  what's the branch, what's been done here recently, did past
  sessions succeed?
- An operator wants a one-glance "current state" of a workspace from
  a comm channel or terminal without opening the web UI.
- A wrapper script wants per-project rollups for dashboards.

---

## Endpoint

```
GET /api/project/summary?dir=<absolute-path>
```

### Required query

- `dir` — absolute path. Relative paths return 400.

### Response (200 OK)

```json
{
  "dir":               "/home/op/work/foo",
  "is_git_repo":       true,
  "branch":            "main",
  "uncommitted_count": 2,
  "recent_commits": [
    {"hash":"abc1234","subject":"feat: x","author":"op","date":"2026-04-19"}
  ],
  "sessions": [
    {"id":"aa01","task":"refactor x","state":"complete",
     "updated_at":"2026-04-19T11:42:00Z","diff_summary":"3 file(s), +47/-12"}
  ],
  "stats": {
    "total_sessions": 14,
    "completed":      11,
    "failed":         2,
    "killed":         1,
    "success_rate":   0.7857
  },
  "generated_at": "2026-04-19T13:00:00Z"
}
```

### Error codes

| Code | Cause |
|------|-------|
| 400  | Missing `dir` query, or `dir` not absolute |
| 405  | Method other than GET |

When the directory is not a git repo, `is_git_repo: false` and the
git-related fields are omitted; session-related fields are always
populated based on the manager's record (empty when no sessions
have used the path).

---

## Examples

### curl

```bash
curl -sS "http://localhost:8080/api/project/summary?dir=$(pwd)" | jq .
```

### From an AI session

```python
import urllib.request, json, os
url = f"http://localhost:8080/api/project/summary?dir={os.getcwd()}"
data = json.loads(urllib.request.urlopen(url).read())
print("branch:", data.get("branch"), "uncommitted:", data.get("uncommitted_count"))
print("past sessions success rate:", data["stats"]["success_rate"])
```

---

## Notes for AI / MCP integration

- `dir` MUST be absolute. If your runtime gives you a relative path,
  resolve it first (`os.path.abspath` etc.) before calling.
- The recent-commits list is capped at 10 entries; the sessions list
  is capped at 20.
- `success_rate` is `completed / total_sessions`; missing when there
  are no sessions for the directory yet.

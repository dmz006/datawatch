# Foreign-file-drift check — live verification (2026-10-14)

Live reproduction of the incident shape (a worker running `git add -A` and
sweeping files that were already modified or untracked into its commit). It was
run against the real local daemon, not unit tests. Every value below was copied
from the daemon's own responses (`autonomous_prd_get` / REST
`GET /api/autonomous/prds/<id>`, `audit_query`) or from `git log` in the
scratch repos.

## Deployed version

- `make build` → `bin/datawatch` → `datawatch v9.0.74`. The version was already
  past 9.0.63, so it was not bumped.
- The binary was copied over `~/.local/bin/datawatch`, which is the systemd
  `ExecStart` path, and the daemon was restarted with
  `systemctl --user restart datawatch`.
- `get_version` (MCP) reported `Current version: 9.0.74`.
- `datawatch version` reported `datawatch v9.0.74`.
- Daemon-wide `autonomous.block_on_file_drift` was `false` (the default).
  `worktree_base_repo` was `/home/user/workspace/datawatch`.

## Scratch repo shape (each run used a fresh one)

Each repo was created under `.datawatch/scratch/<name>/` (a gitignored path,
resolved from `git rev-parse --show-toplevel`) as follows:

1. `git init`, with local `user.name` and `user.email`.
2. One initial commit containing `planned.txt` and `other.txt`.
3. `other.txt` was then left modified (` M`) and `stray.txt` was created
   untracked (`??`).

Each Automaton was created in direct mode (explicit absolute `project_dir`) with
backend `claude-code`, one story, and one task with `files: ["planned.txt"]`.
The task spec said: append a line to `planned.txt`, then
`git add -A && git commit -m 'update planned'`.

Between repo setup and task start, the daemon's own session tooling also wrote
these untracked files into each scratch repo: `.claude/settings.local.json`,
`.mcp.json`, `CLAUDE.md`, `.gitignore`, and the decompose step's
`.datawatch/skills/...` and `.datawatch/verify/...` files. They were present
before the worker started and were not in the task's `files`, so the check
correctly reported them as drift alongside `other.txt` and `stray.txt`.

## Run A — warn-only default (`block_on_file_drift` unset)

### Attempt 1 — Automaton `0854f1d6`, task `115ebeb4`: no drift, because the worker did not stage broadly

The worker staged only `planned.txt`. It ignored the literal `git add -A` in
favour of the spec's "do not edit any other file". The resulting commit
`332b0e9` touched one file:

```
332b0e9 update planned
 planned.txt | 1 +
```

The task's record shows nothing was flagged:

```json
"status": "completed", "files": ["planned.txt"], "files_touched": ["planned.txt"],
"foreign_file_drift": null, "pre_task_sha": "e2bd633ad445213587c2dc420fbab18ff0b208fd"
```

No `file_drift_detected` decision was recorded. This is a correct true
negative, but it does not reproduce the incident, so the warn-only run was
repeated with a spec that leaves the worker no room to narrow the staging.

### Attempt 2 — Automaton `7f32cc5d`, task `c9b52b5c`: incident reproduced

Scratch repo: `.datawatch/scratch/file-drift-live-warn2`. Pre-task SHA:
`e0a76a9d854d838b9ca9459c9c6df8c80e5d5ea7`. The worker ran `git add -A`, which
produced this commit:

```
f91c7b9 update planned
 .claude/settings.local.json                        |  28 +++++
 .datawatch/skills/web-search-guidance/SKILL.md     |  41 +++++++
 .../verify/append-drift-check-line-and-commit.sh   |  25 +++++
 .gitignore                                         |  10 ++
 .mcp.json                                          |  13 +++
 CLAUDE.md                                          | 119 +++++++++++++++++++++
 other.txt                                          |   1 +
 planned.txt                                        |   1 +
 stray.txt                                          |   1 +
```

Task excerpt from `autonomous_prd_get 7f32cc5d`:

```json
{
  "id": "c9b52b5c",
  "status": "completed",
  "error": null,
  "files": ["planned.txt"],
  "foreign_file_drift": [
    ".claude/settings.local.json",
    ".datawatch/skills/web-search-guidance/SKILL.md",
    ".datawatch/verify/append-drift-check-line-and-commit.sh",
    ".gitignore",
    ".mcp.json",
    "CLAUDE.md",
    "other.txt",
    "stray.txt"
  ],
  "pre_task_sha": "e0a76a9d854d838b9ca9459c9c6df8c80e5d5ea7"
}
```

What this shows:
- `other.txt` and `stray.txt` are both listed.
- `planned.txt` is not listed.
- The task status is `completed`, so the warn-only default works.

Decision entry:

```json
{
  "at": "2026-10-14T08:24:21.068251813-04:00",
  "kind": "file_drift_detected",
  "actor": "autonomous",
  "note": "task c9b52b5c: committed pre-existing uncommitted file(s) outside its planned files: .claude/settings.local.json, .datawatch/skills/web-search-guidance/SKILL.md, .datawatch/verify/append-drift-check-line-and-commit.sh, .gitignore, .mcp.json, CLAUDE.md, other.txt, stray.txt"
}
```

Audit line (`audit_query action=autonomous_file_drift_detected`, also present in
`~/.datawatch/audit.log`):

```json
{"ts":"2026-10-14T08:24:21.070978977-04:00","actor":"autonomous","action":"autonomous_file_drift_detected","details":{"files":[".claude/settings.local.json",".datawatch/skills/web-search-guidance/SKILL.md",".datawatch/verify/append-drift-check-line-and-commit.sh",".gitignore",".mcp.json","CLAUDE.md","other.txt","stray.txt"],"prd_id":"7f32cc5d","task_id":"c9b52b5c"}}
```

The Automaton's own status ended `blocked` even though the task completed. The
cause was a `project_rules_block` decision ("1 finding(s) at or above
fail_on_severity blocked PRD completion") from the daemon-wide `project_rules`
scan. That is unrelated to the drift check, which did not block. Attempt 1
(`0854f1d6`) ended the same way.

## Run B — strict: per-Automaton `block_on_file_drift=true`

Automaton `5aebcccb`, task `fdcdacfc`. Scratch repo:
`.datawatch/scratch/file-drift-live-block`.

The override was set before approval with
`PUT /api/autonomous/prds/5aebcccb/guardrails {"block_on_file_drift": true}`.
REST was used because the MCP client's cached `per_automaton_guardrails_set`
schema predates the new field. The response showed `"block_on_file_drift": true`,
and the `set_guardrails` audit entry recorded `"block_on_file_drift":true`.

The worker ran `git add -A`, and commit `6578620` swept in the same 8 foreign
files plus `planned.txt`. Task excerpt:

```json
{
  "id": "fdcdacfc",
  "status": "blocked",
  "error": "foreign_file_drift: .claude/settings.local.json, .datawatch/skills/web-search-guidance/SKILL.md, .datawatch/verify/append-drift-check-line.sh, .gitignore, .mcp.json, CLAUDE.md, other.txt, stray.txt",
  "files": ["planned.txt"],
  "foreign_file_drift": [
    ".claude/settings.local.json",
    ".datawatch/skills/web-search-guidance/SKILL.md",
    ".datawatch/verify/append-drift-check-line.sh",
    ".gitignore",
    ".mcp.json",
    "CLAUDE.md",
    "other.txt",
    "stray.txt"
  ],
  "pre_task_sha": "e2bd633ad445213587c2dc420fbab18ff0b208fd"
}
```

Decision entry (`kind: file_drift_detected`, at
`2026-10-14T08:21:06.625405902-04:00`):
`task fdcdacfc: committed pre-existing uncommitted file(s) outside its planned files: …, other.txt, stray.txt`.

Audit line:

```json
{"ts":"2026-10-14T08:21:06.628380056-04:00","actor":"autonomous","action":"autonomous_file_drift_detected","details":{"files":[".claude/settings.local.json",".datawatch/skills/web-search-guidance/SKILL.md",".datawatch/verify/append-drift-check-line.sh",".gitignore",".mcp.json","CLAUDE.md","other.txt","stray.txt"],"prd_id":"5aebcccb","task_id":"fdcdacfc"}}
```

Outcome with `block_on_file_drift=true`:
- The task ended `blocked`, with the drift files in its `error`.
- The Automaton ended `blocked`.
- The scratch commit was left in place. Nothing was reverted, reset or amended.

## Run C — worktree mode (Automaton without `project_dir`)

Automaton `ddcb5141`, created with no `project_dir`. The daemon set up the
worktree itself, giving `project_dir=~/.datawatch/prd-worktrees/ddcb5141` and
`git.branch=automaton/ddcb5141` from `main` @ `e78937e2`. Auto-PR was off, so
nothing was pushed (`ci_watch: status=skipped … HEAD is not on any remote-tracking ref`).

The task was `3751b569`, with `files: ["drift-worktree-probe.txt"]`. Its spec
was to create the file, then `git add -A && git commit -m 'update planned'`.

1. Commit `c10646a7` landed.
2. The daemon-wide `rules-review` completion guardrail then appended an auto-fix
   story (`a5e5270a`, task `a62004cc`).
3. That auto-fix worker amended the throwaway branch's commit to `5d80c54e`.

Observed:
- Task `3751b569`: `status: completed`, `foreign_file_drift: null`.
- Task `a62004cc`: `status: completed`, `foreign_file_drift: null`.
- The decisions list has no `file_drift_detected` entry: decompose, edit_task,
  set_task_files, approve, run, auto_retry (content), run, ci_watch.
- `audit_query action=autonomous_file_drift_detected` returns only the two
  direct-mode entries above. There is none for `ddcb5141`.

This matches the deliberate early return in `checkForeignFileDrift`
(`prd.Git.Branch != ""`). The unit test
`TestCheckForeignFileDrift_WorktreeModeMakesZeroGitCalls`
(`internal/autonomous/file_drift_executor_test.go`) asserts that this path makes
zero git calls.

## Side observations (reported, not acted on)

- **Empty guardrail list is ignored.** Setting `per_prd_guardrails: []` through
  `PUT /guardrails` was stored as `null` and did not suppress the daemon-wide
  `rules-review` / `ci-watch` completion guardrails. That is why Run C got an
  auto-fix story.
- **Daemon's own files count as drift.** The tooling files the daemon writes
  into a direct-mode `project_dir` are untracked and pre-date the worker, so a
  broad `git add -A` sweeps them in. They then show up in `foreign_file_drift`
  next to the operator's own files. The detector is behaving as designed.
  Whether daemon-injected tooling files should be excluded is a separate
  question for the operator.

## Out of scope

- No git history rewrite, revert, amend or un-stage by the drift check or by
  this verification. The amend in Run C was made by an auto-fix worker on a
  throwaway branch, not by the check.
- No owner attribution of drifted files (which session or person modified
  them).
- Worktree mode is a deliberate no-op for the drift check.
- AGENT.md git-discipline wording is unchanged.
- No change to how workers invoke `git add`.

## Cleanup

All four scratch Automata (`0854f1d6`, `5aebcccb`, `7f32cc5d`, `ddcb5141`) were
cancelled. The scratch dirs under `.datawatch/scratch/` were removed. The
throwaway worktree `~/.datawatch/prd-worktrees/ddcb5141` and the local branch
`automaton/ddcb5141` were removed. That branch was never pushed.

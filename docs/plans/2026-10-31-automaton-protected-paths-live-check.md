# Automaton protected-paths hold — live check on a sandbox daemon

**Date:** 2026-11-06 (run started 14:05:32, local time)
**Version under test:** datawatch v9.51.0 (binary built from this worktree)
**Script:** `scripts/protected-paths-live-check.sh` (run as `env -u TMUX -u TMUX_TMPDIR bash scripts/protected-paths-live-check.sh`)
**Scope:** the protected-paths settings, `GET /api/autonomous/protected-paths` and its MCP, CLI and chat reads, a held task in worktree and direct mode, the approve, reject and change_spec answers, the per-Automaton authorised paths, the warn, block and off modes and project-specific names, against throwaway project repositories whose default branch is `trunk`.

## Result

**Overall:** PASS

| # | Check | Result | Assertions |
|---|---|---|---|
| 1 | Config: protected_paths, protected_paths_action, rules_review_source | PASS | 9 of 9 passed |
| 2 | Read surfaces agree | PASS | 5 of 5 passed |
| 3 | Hold: an edit to a protected file is held, in worktree mode and in direct mode | PASS | 13 of 13 passed |
| 4 | Answers: approve, reject and change_spec | PASS | 9 of 9 passed |
| 5 | Authorised path: the same edit passes, clearing it brings the hold back | PASS | 6 of 6 passed |
| 6 | Modes: warn, block and off | PASS | 4 of 4 passed |
| 7 | Different names: the project's own rules file and a directory | PASS | 3 of 3 passed |
| 8 | Judge: the base-commit rules review | PASS | 1 of 1 passed |
| 9 | Cleanup | PASS | 1 of 1 passed |

## Setup

- **Sandbox daemon only.** Started with `td_start` from `scripts/test-daemon-lib.sh` from a config generated from `testdata/datawatch.yaml` (data folder, ports, loopback host, test token). Added: `autonomous.worktree_base_repo` and `autonomous.worktree_dir`, `autonomous.rules_file` (`RULES.md`, the throwaway project's own rules file), `autonomous.questions.enabled`, `autonomous.verification_backend: shell`, `server.allowed_project_roots` and `shell_backend.script_path` (the stand-in worker). The production daemon, its service and its data were not touched, and nothing was ended by name.
- **Ports.** HTTP 53415, TLS 38335, MCP 39925, DNS 45211, webhook 50851, notify 49277, each probed free just before use.
- **Session sockets.** The daemon ran with its multiplexer socket directory under the throwaway folder; the script itself issues no multiplexer command.
- **Everything** lives under one temporary folder outside the worktree, removed on every exit path.
- **Throwaway project.** A bare repository whose default branch is `trunk`, cloned to the worktree base repository and, for direct mode, to a second clone; `refs/remotes/origin/HEAD` records the default branch and `main` does not exist. It tracks `RULES.md`, `AGENT.md` and `policy/`.
- **Worker.** The daemon's shell backend runs a generated script. It writes a sample file and, by mode, appends a line to the rules file, to the `AGENT.md`-named file or to a file under `policy/`. The daemon's verification (a one-shot session on the same backend) is answered by the same script with a fixed pass, so no model is involved anywhere.

```
refs/remotes/origin/trunk; main exists: no
```

| Automaton | Worker mode | Kind | Used in |
|---|---|---|---|
| `e0c10001` hold-approve | rules-once | worktree | checks 3, 4 (approve) |
| `e0c10002` hold-reject | rules-once | worktree | checks 3, 4 (reject) |
| `e0c10003` hold-spec | rules | worktree | checks 3, 4 (change_spec) |
| `e0c10004` authorised | rules | worktree | check 5 (authorised) |
| `e0c10005` authorised-cleared | rules | worktree | checks 3, 5 (list cleared) |
| `e0c10006` mode-warn | rules | worktree | check 6 (warn) |
| `e0c10007` mode-block | rules | worktree | check 6 (block) |
| `e0c10008` mode-off | rules | worktree | check 6 (off) |
| `e0c10009` names-other | other | worktree | check 7 (outside the list) |
| `e0c1000a` names-policy | policy | worktree | check 7 (inside the list) |
| `e0c1000b` hold-direct | rules-once | direct | check 3 (direct mode) |


## 1. Config: protected_paths, protected_paths_action, rules_review_source

- **PASS** — a fresh config reads action hold and rules source base_commit
  - status route: action=`hold` rules_source=`base_commit`; raw settings before any write: action=`hold` source=`base_commit` (unset reads as the default)

- **PASS** — a fresh config reads the default list: the project's rules file, the supervisor rules document, CODEOWNERS and the workflows directory
  - default list: ["RULES.md","docs/plans/2026-10-17-supervisor-rules-and-lessons.md","CODEOWNERS",".github/workflows/"]

- **PASS** — PUT then GET round-trips protected_paths_action (warn, block, off, hold)
  - warn: HTTP 200, read `warn`, status `warn`; block: HTTP 200, read `block`, status `block`; off: HTTP 200, read `off`, status `off`; hold: HTTP 200, read `hold`, status `hold`; 

- **PASS** — PUT then GET round-trips rules_review_source (worktree, base_commit)
  - worktree: HTTP 200, read `worktree`, status `worktree`; base_commit: HTTP 200, read `base_commit`, status `base_commit`; 

- **PASS** — PUT then GET round-trips protected_paths (a list) and the status route shows it as the effective list
  - HTTP 200, read ["docs/","notes.md"], effective list ["docs/","notes.md"]

- **PASS** — an empty list clears protected_paths back to the default list
  - HTTP 200, effective list ["RULES.md","docs/plans/2026-10-17-supervisor-rules-and-lessons.md","CODEOWNERS",".github/workflows/"]

- **PASS** — an unknown action word is a 400 that names hold, warn, block and off
  - `PUT /api/config`: 400 autonomous.automation.protected_paths_action: must be "hold", "warn", "block" or "off" 

- **PASS** — an unknown rules source word is a 400 that names base_commit and worktree
  - `PUT /api/config`: 400 autonomous.automation.rules_review_source: must be "base_commit" or "worktree" 

- **PASS** — the refused writes saved nothing
  - action `hold`, rules source `base_commit` after the refused writes

## 2. Read surfaces agree

- **PASS** — `GET /api/autonomous/protected-paths` answers 200
  - HTTP 200

- **PASS** — the body has the action, the rules source, the list, a held list whose entries carry the Automaton, task, paths, action, question and time, and an authorised list
  - {"action":"hold","rules_source":"base_commit","held":[{"automaton_id":"e0c10001","task_id":"e0c10001t1","paths":["RULES.md"]},{"automaton_id":"e0c10002","task_id":"e0c10002t1","paths":["RULES.md"]},{"automaton_id":"e0c10003","task_id":"e0c10003t1","paths":["RULES.md"]},{"automaton_id":"e0c10005","task_id":"e0c10005t1","paths":["RULES.md"]},{"automaton_id":"e0c1000b","task_id":"e0c1000bt1","paths":["RULES.md"]}],"authorized":[{"automaton_id":"e0c10004","automaton_title":"authorised","paths":["RULES.md"]}]}

- **PASS** — POST is a 405
  - HTTP 405

CLI text (`datawatch autonomous protected-paths`):
```
Automaton protected paths (action: hold, rules source: base_commit):
  Protected list (4): RULES.md, docs/plans/2026-10-17-supervisor-rules-and-lessons.md, CODEOWNERS, .github/workflows/
  Held (5):
  - Automaton hold-approve (e0c10001), task Write the sample file for hold-approve (e0c10001t1)
      paths: RULES.md
      action: hold
      reason: the task's commit changed protected paths
      held since: 2026-11-06 14:05:39 EST
      question: q-18e58a2a067ee00dee41 (answer approve, reject or change the spec)
  - Automaton hold-reject (e0c10002), task Write the sample file for hold-reject (e0c10002t1)
      paths: RULES.md
      action: hold
      reason: the task's commit changed protected paths
      held since: 2026-11-06 14:05:51 EST
      question: q-18e58a2cc46d18b11f0e (answer approve, reject or change the spec)
  - Automaton hold-spec (e0c10003), task Write the sample file for hold-spec (e0c10003t1)
      paths: RULES.md
      action: hold
      reason: the task's commit changed protected paths
      held since: 2026-11-06 14:05:48 EST
      question: q-18e58a2c086e3c68ac59 (answer approve, reject or change the spec)
  - Automaton authorised-cleared (e0c10005), task Write the sample file for authorised-cleared (e0c10005t1)
      paths: RULES.md
      action: hold
      reason: the task's commit changed protected paths
      held since: 2026-11-06 14:05:58 EST
      question: q-18e58a2e5bcd61bfa1b9 (answer approve, reject or change the spec)
  - Automaton hold-direct (e0c1000b), task Write the sample file for hold-direct (e0c1000bt1)
      paths: RULES.md
      action: hold
      reason: the task's commit changed protected paths
      held since: 2026-11-06 14:06:01 EST
      question: q-18e58a2f17b81db0f915 (answer approve, reject or change the spec)
  Authorised in advance (1):
  - Automaton authorised (e0c10004): RULES.md
This is a status read only: answer the question in the question queue to release a hold.

```

Chat reply (`POST /api/test/message`, text `autonomous protected-paths`):
```
[dw-e2e-test] autonomous protected-paths
Automaton protected paths (action: hold, rules source: base_commit)
Protected list (4): RULES.md, docs/plans/2026-10-17-supervisor-rules-and-lessons.md, CODEOWNERS, .github/workflows/
Held: 5
- Automaton hold-approve (e0c10001), task Write the sample file for hold-approve (e0c10001t1): RULES.md
    reason: the task's commit changed protected paths
    question: q-18e58a2a067ee00dee41 (approve, reject or change the spec)
- Automaton hold-reject (e0c10002), task Write the sample file for hold-reject (e0c10002t1): RULES.md
    reason: the task's commit changed protected paths
    question: q-18e58a2cc46d18b11f0e (approve, reject or change the spec)
- Automaton hold-spec (e0c10003), task Write the sample file for hold-spec (e0c10003t1): RULES.md
    reason: the task's commit changed protected paths
    question: q-18e58a2c086e3c68ac59 (approve, reject or change the spec)
- Automaton authorised-cleared (e0c10005), task Write the sample file for authorised-cleared (e0c10005t1): RULES.md
    reason: the task's commit changed protected paths
    question: q-18e58a2e5bcd61bfa1b9 (approve, reject or change the spec)
- Automaton hold-direct (e0c1000b), task Write the sample file for hold-direct (e0c1000bt1): RULES.md
    reason: the task's commit changed protected paths
    question: q-18e58a2f17b81db0f915 (approve, reject or change the spec)
Authorised in advance: 1
- Automaton authorised (e0c10004): RULES.md
Answer a held task in the question queue: question list, then question answer <question-id> approve | reject | change_spec.

```

MCP result (`autonomous_protected_paths`, first 700 bytes):
```
{"action":"hold","rules_source":"base_commit","paths":["RULES.md","docs/plans/2026-10-17-supervisor-rules-and-lessons.md","CODEOWNERS",".github/workflows/"],"held":[{"automaton_id":"e0c10001","automaton_title":"hold-approve","task_id":"e0c10001t1","task_title":"Write the sample file for hold-approve","paths":["RULES.md"],"action":"hold","reason":"the task's commit changed protected paths","question_id":"q-18e58a2a067ee00dee41","since":"2026-11-06T14:05:39.679509599-05:00","base_commit":"0488e09cd002b90df61e1da26b59a175633cd4f4","awaiting_spec_change":false},{"automaton_id":"e0c10002","automaton_title":"hold-reject","task_id":"e0c10002t1","task_title":"Write the sample file for hold-reject","

```

- **PASS** — REST, MCP, CLI (--json) and the chat verb return the same held list
  - comparison: `OK`; held entries: [{"automaton_id":"e0c10001","task_id":"e0c10001t1"},{"automaton_id":"e0c10002","task_id":"e0c10002t1"},{"automaton_id":"e0c10003","task_id":"e0c10003t1"},{"automaton_id":"e0c10005","task_id":"e0c10005t1"},{"automaton_id":"e0c1000b","task_id":"e0c1000bt1"}]

- **PASS** — the automaton filter narrows the list (an unknown id reads empty)
  - `?automaton=e0c10001` holds 1, an unknown id holds 0

## 3. Hold: an edit to a protected file is held, in worktree mode and in direct mode

Approach used: the **shell backend with a generated worker script** ran each task headlessly. The worker appends a line to `RULES.md`, the project's own rules file (set as `autonomous.rules_file`, so it is on the default protected list). Worktree mode: the daemon made the worktree from the throwaway project. Direct mode: the Automaton's project directory is its own clone of the throwaway project.

Task JSON of the held task, worktree mode (`protected_path_hold` is the hold):
```
{
  "id": "e0c10001t1",
  "status": "blocked",
  "error": "protected_path: RULES.md",
  "protected_path_hold": {
    "paths": [
      "RULES.md"
    ],
    "action": "hold",
    "reason": "the task's commit changed protected paths",
    "question_id": "q-18e58a2a067ee00dee41",
    "base_commit": "0488e09cd002b90df61e1da26b59a175633cd4f4",
    "since": "2026-11-06T14:05:39.679509599-05:00"
  }
}

```

- **PASS** — worktree mode: the task is held (blocked), not completed
  - status `blocked`, error `protected_path: RULES.md`

- **PASS** — worktree mode: the hold object names the changed protected path, the action and the base commit
  - hold: {"paths":["RULES.md"],"action":"hold","base_commit":"0488e09cd002b90df61e1da26b59a175633cd4f4","reason":"the task's commit changed protected paths"}

The question, worktree mode (`GET /api/questions/<id>`):
```
{
  "id": "q-18e58a2a067ee00dee41",
  "label": "protected_paths",
  "state": "open",
  "default": "hold",
  "context": "task",
  "options": [
    "approve",
    "reject",
    "change_spec"
  ],
  "text": "Task \"Write the sample file for hold-approve\" (e0c10001t1) of Automaton \"hold-approve\" (e0c10001) changed protected paths in its commit.\n\nProtected paths changed:\n- RULES.md\n\nApprove keeps the change and the task completes; the approval covers only these paths for this task. Reject puts the listed paths back to their state before the task, in a new commit, and runs the task again. Change the spec keeps the task blocked so you can edit its instructions, then reset it.\n\nNothing changes until you answer."
}

```

- **PASS** — worktree mode: an open question offers approve, reject and change_spec (default hold) and lists the path
  - question `q-18e58a2a067ee00dee41`: options ["approve","reject","change_spec"], default `hold`, state `open`, text names `RULES.md`: yes

- **PASS** — worktree mode: exactly ONE question exists for the task
  - questions carrying the task id: 1

- **PASS** — worktree mode: the Automaton is not completed
  - status `blocked`

Task JSON of the held task, direct mode (`protected_path_hold` is the hold):
```
{
  "id": "e0c1000bt1",
  "status": "blocked",
  "error": "protected_path: RULES.md",
  "protected_path_hold": {
    "paths": [
      "RULES.md"
    ],
    "action": "hold",
    "reason": "the task's commit changed protected paths",
    "question_id": "q-18e58a2f17b81db0f915",
    "base_commit": "0488e09cd002b90df61e1da26b59a175633cd4f4",
    "since": "2026-11-06T14:06:01.443185415-05:00"
  }
}

```

- **PASS** — direct mode: the task is held (blocked), not completed
  - status `blocked`, error `protected_path: RULES.md`

- **PASS** — direct mode: the hold object names the changed protected path, the action and the base commit
  - hold: {"paths":["RULES.md"],"action":"hold","base_commit":"0488e09cd002b90df61e1da26b59a175633cd4f4","reason":"the task's commit changed protected paths"}

The question, direct mode (`GET /api/questions/<id>`):
```
{
  "id": "q-18e58a2f17b81db0f915",
  "label": "protected_paths",
  "state": "open",
  "default": "hold",
  "context": "task",
  "options": [
    "approve",
    "reject",
    "change_spec"
  ],
  "text": "Task \"Write the sample file for hold-direct\" (e0c1000bt1) of Automaton \"hold-direct\" (e0c1000b) changed protected paths in its commit.\n\nProtected paths changed:\n- RULES.md\n\nApprove keeps the change and the task completes; the approval covers only these paths for this task. Reject puts the listed paths back to their state before the task, in a new commit, and runs the task again. Change the spec keeps the task blocked so you can edit its instructions, then reset it.\n\nNothing changes until you answer."
}

```

- **PASS** — direct mode: an open question offers approve, reject and change_spec (default hold) and lists the path
  - question `q-18e58a2f17b81db0f915`: options ["approve","reject","change_spec"], default `hold`, state `open`, text names `RULES.md`: yes

- **PASS** — direct mode: exactly ONE question exists for the task
  - questions carrying the task id: 1

- **PASS** — direct mode: the Automaton is not completed
  - status `blocked`

- **PASS** — the same hold is placed on the other worktree Automata (`e0c10002`)
  - task status `blocked`, paths ["RULES.md"]

- **PASS** — the same hold is placed on the other worktree Automata (`e0c10003`)
  - task status `blocked`, paths ["RULES.md"]

- **PASS** — the hold is in the audit log and the decision log
  - audit events 1; decisions `approve run operator_decision protected_paths_held`

## 4. Answers: approve, reject and change_spec

- **PASS** — approve: the answer is accepted and the task completes
  - answer HTTP 201; task status `completed`

- **PASS** — approve: the Automaton completes, the hold is cleared and the approval covers only the listed path for this task
  - Automaton status `completed`; approved_protected_paths ["RULES.md"]; the Automaton's authorised paths []

- **PASS** — approve: the change is kept in the Automaton branch
  - last line of `RULES.md` at the branch head: `edit by e0c10001`

- **PASS** — reject: the answer is accepted
  - answer HTTP 201

- **PASS** — reject: the protected file is back at its base content
  - base commit `0488e09cd`; `RULES.md` at the base: `base rules`, at the branch head now: `base rules`

- **PASS** — reject: it is a new commit on top of the old history (nothing rewritten)
  - head before the answer `cb1e73fb9`, after `49160b825`; the old head is an ancestor of the new one; new commits: task(e0c10002): restore protected paths|

- **PASS** — reject: the task ran again (the worker ran a second time) and completed
  - worker runs 1 then 2; task status `completed`; Automaton `completed`

Task JSON after change_spec:
```
{
  "id": "e0c10003t1",
  "status": "blocked",
  "error": "protected_path: change the task instructions, then reset the task",
  "protected_path_hold": {
    "paths": [
      "RULES.md"
    ],
    "action": "hold",
    "reason": "the task's commit changed protected paths",
    "question_id": "q-18e58a2c086e3c68ac59",
    "base_commit": "0488e09cd002b90df61e1da26b59a175633cd4f4",
    "since": "2026-11-06T14:05:48.301765039-05:00",
    "awaiting_spec_change": true
  }
}

```

- **PASS** — change_spec: the task stays blocked, the hold waits for a spec change and the task did not run again
  - answer HTTP 201; task status `blocked`; awaiting_spec_change `true`; error `protected_path: change the task instructions, then reset the task`; worker runs 1

- **PASS** — each answer is in the audit log
  - answered events per case (approve, reject, change_spec): 111

## 5. Authorised path: the same edit passes, clearing it brings the hold back

- **PASS** — `POST .../set_authorized_paths` accepts the list
  - 200 {"id":"e0c10004","spec":"Live check sample authorised LIVECHECK[rules:e0c10004]","title":"authorised","project_dir":"","git":{},"backend":"shell","status":"

- **PASS** — the authorised list shows in the protected-paths status
  - [{"automaton_id":"e0c10004","automaton_title":"authorised","paths":["RULES.md"]}]

- **PASS** — with the path authorised the same edit passes with no question and the change is kept
  - Automaton `completed`; questions for the task: 0; decisions `protected_paths_authorised_set protected_paths_authorised_set approve run protected_paths_authorised`

- **PASS** — an empty list clears the authorisation
  - {"authorized_paths":null}

- **PASS** — an absolute authorised path is refused with a 400
  - 400 authorised path "/etc/passwd" is absolute; use a repository-relative path 

- **PASS** — clearing the authorised list brings the hold back (the Automaton had the path authorised, the list was cleared, and the same edit was held)
  - clear answered `200 {"id":"e0c10005","spec":"Live check sample authorised-cleared LIVECHECK[rules:e0c10005]","title":"authorised-cleared","project_dir":"","git":{},"backend":"shell","status":"needs_review","created_at":"2026-11-06T19:05:28.514959Z","updated_at":"2026-11-06T14:05:33.140490018-05:00","rev":2,"stories":[{"id":"e0c10005s1","prd_id":"e0c10005","title":"Sample story","status":"pending","tasks":[{"id":"e0c10005t1","story_id":"e0c10005s1","prd_id":"e0c10005","title":"Write the sample file for authorised-cleared","spec":"Write the sample file. LIVECHECK[rules:e0c10005]","status":"pending","created_at":"2026-11-06T19:05:28.514959Z","updated_at":"2026-11-06T19:05:28.514959Z","verification_script":".checks/ok.sh"}],"created_at":"2026-11-06T19:05:28.514959Z","updated_at":"2026-11-06T19:05:28.514959Z"}],"decisions":[{"at":"2026-11-06T14:05:33.097360572-05:00","kind":"protected_paths_authorised_set","actor":"operator","note":"authorised protected paths: RULES.md"},{"at":"2026-11-06T14:05:33.140487825-05:00","kind":"protected_paths_authorised_set","actor":"operator","note":"authorised protected paths cleared"}],"memory_seed":{},"memory_harvest":{},"write_dirs":["<sandbox>/worktrees/e0c10005"]} `; task status `blocked`, hold paths ["RULES.md"]

## 6. Modes: warn, block and off

Decision log of the warn case:
```
[{"kind":"protected_paths_warned","note":"task e0c10006t1: protected paths changed: RULES.md"}]

```

- **PASS** — warn: the task and the Automaton complete although a protected file changed
  - Automaton `completed`, task `completed`

- **PASS** — warn: a decision and an audit event are recorded and no question is raised
  - decisions `approve run protected_paths_warned`; audit events 1; questions for the task 0

Task JSON of the block case:
```
{
  "id": "e0c10007t1",
  "status": "blocked",
  "error": "protected_path_blocked: RULES.md",
  "protected_path_hold": null
}

```

- **PASS** — block: the task is blocked with a recorded decision and no question
  - task status `blocked`, error `protected_path_blocked: RULES.md`; decisions `approve run protected_paths_blocked`; questions for the task 0

- **PASS** — off: the task and the Automaton complete and nothing of the check is recorded
  - Automaton `completed`; decisions `approve run`; questions 0; audit events 0

## 7. Different names: the project's own rules file and a directory

- **PASS** — protected_paths is set to the throwaway project's own rules file name and a directory
  - HTTP 200; effective list ["RULES.md","policy/"]

- **PASS** — an edit to `AGENT.md`, outside the configured list, passes with no question
  - Automaton `completed`; decisions `approve run`; questions 0

- **PASS** — an edit under the listed directory is held
  - task status `blocked`, hold paths ["policy/policy.md"]

## 8. Judge: the base-commit rules review

- **PASS** — limitation, not a live result: the judge read of the rules at the base commit (rules_review_source base_commit) is covered by the Go tests with fake judges
  - 10 test functions in `internal/autonomous/rules_review_base_test.go`; test files: internal/autonomous/protected_paths_answer_test.go internal/autonomous/protected_paths_gate_test.go internal/autonomous/protected_paths_hold_test.go internal/autonomous/protected_paths_status_test.go internal/autonomous/protected_paths_test.go internal/autonomous/rules_review_base_test.go 

**Not shown live.** The sandbox has no judge model, so no live judge result is claimed here. The judge's read of the project rules at the base commit (instead of the worktree) is exercised only by the Go tests, which use fake judges. This check records that coverage and nothing more.

## 9. Cleanup

- **PASS** — the sandbox daemon and its sessions are gone and no test daemon was left behind
  - daemon gone: yes; port 38335 closed: yes; leftover check exit 0

- The daemon was ended by `td_stop_all` from `scripts/test-daemon-lib.sh` (the recorded pid, config path as marker); nothing was ended by name.
- The scratch area (projects, remote, worktrees, data, config, log, worker, state) is removed by the exit handler right after this report is written.

## Daemon log, the protected-paths lines

```
{"ts":"2026-11-06T14:06:02.285387293-05:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/protected-paths","remote_ip":"127.0.0.1","status":200,"user_agent":
{"ts":"2026-11-06T14:06:02.296948326-05:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/protected-paths","remote_ip":"127.0.0.1","status":200,"user_agent":
{"ts":"2026-11-06T14:06:02.526173972-05:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/protected-paths","remote_ip":"127.0.0.1","status":200,"user_agent":
{"ts":"2026-11-06T14:06:02.543392016-05:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/protected-paths","remote_ip":"127.0.0.1","status":200,"user_agent":
{"ts":"2026-11-06T14:06:02.54876474-05:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/protected-paths","remote_ip":"127.0.0.1","status":200,"user_agent":"
{"ts":"2026-11-06T14:06:14.797864972-05:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/protected-paths","remote_ip":"127.0.0.1","status":200,"user_agent":
{"ts":"2026-11-06T14:06:14.804807753-05:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/protected-paths","remote_ip":"127.0.0.1","status":200,"user_agent":
{"ts":"2026-11-06T14:06:50.288857309-05:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/protected-paths","remote_ip":"127.0.0.1","status":200,"user_agent":
{"ts":"2026-11-06T14:06:50.296487152-05:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/protected-paths","remote_ip":"127.0.0.1","status":200,"user_agent":
2026/11/06 14:06:02 [autonomous] e0c10001/e0c10001t1 protected-paths: approved 1 path(s)
2026/11/06 14:06:02 [autonomous] e0c10002/e0c10002t1 protected-paths: operator rejected, restored 1 path(s) in commit 49160b825b3c
2026/11/06 14:06:06 [autonomous] e0c10003/e0c10003t1 protected-paths: waiting for a spec change
```

## What was and was not checked

- **Checked live, through the running daemon:** the three settings (round trip, refused words, defaults), the status route and its MCP, CLI and chat reads, the hold and its single question in worktree and direct mode, the three answers, the authorised list (set, used, cleared, refused when absolute), the warn, block and off modes, and project-specific names.
- **Not checked live: the judge.** The sandbox has no judge model. The judge read of the rules at the base commit is covered by the Go tests with fake judges; nothing in this report is a live judge result.
- **The Automata were written into the sandbox store, not created through the create route.** The route only makes drafts and a plan needs a planner model. Approve, run, the answers, every read and every setting go through the REST API of the running daemon; the worktrees and branches were made by the daemon.
- **Reject was shown in worktree mode only.** Direct mode is shown for the hold; the restore code is the same in both modes and is covered by the Go tests.
- **Not shown live:** a restore that fails (the hold kept with a fresh question), a stale or withdrawn question, the PWA card and the held-task banner. They are covered by the Go tests and the PWA tests; this script drives the daemon through REST, MCP, the command line and chat only.
- **Generality.** The projects are throwaway repositories whose default branch is `trunk`, with a rules file this script names itself; nothing names the repository this script lives in.

Result: PASS

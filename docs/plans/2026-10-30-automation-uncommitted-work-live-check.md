# Automaton uncommitted-work check — live check on a sandbox daemon

**Date:** 2026-10-31 (run started 02:18:59, local time)
**Version under test:** datawatch v9.47.1 (binary built from this worktree)
**Script:** `scripts/uncommitted-work-live-check.sh` (run as `env -u TMUX -u TMUX_TMPDIR bash scripts/uncommitted-work-live-check.sh`)
**Scope:** the uncommitted-work policy and scope settings, `GET /api/autonomous/uncommitted-work` and its MCP, CLI and chat reads, a held task, the commit, discard and retry answers, the whole-worktree block before an Automaton is reported finished, and the warn and off modes, against a throwaway project repository whose default branch is `trunk`.

## Result

**Overall:** PASS

| # | Check | Result | Assertions |
|---|---|---|---|
| 1 | Config: the two settings | PASS | 6 of 6 passed |
| 2 | Read surfaces agree | PASS | 5 of 5 passed |
| 3 | Held task: a task that leaves a file uncommitted is not completed | PASS | 10 of 10 passed |
| 4 | Answers: commit, discard and retry | PASS | 11 of 11 passed |
| 5 | Whole-worktree block | PASS | 8 of 8 passed |
| 6 | Modes: warn and off | PASS | 7 of 7 passed |
| 7 | Cleanup | PASS | 1 of 1 passed |

## Setup

- **Sandbox daemon only.** Started with `td_start` from `scripts/test-daemon-lib.sh` from a config generated from `testdata/datawatch.yaml` by the substitutions the test runner uses (data folder, ports, loopback host, test token). Added: `autonomous.worktree_base_repo` and `autonomous.worktree_dir` (the throwaway project and its worktree root), `autonomous.questions.enabled` (the operator question routes are off by default), `autonomous.verification_backend: shell`, `server.allowed_project_roots`, and `shell_backend.script_path` (the stand-in worker). The production daemon, its service and its data were not touched, and nothing was ended by name.
- **Ports.** HTTP 45311, TLS 45901, MCP 33119, DNS 58063, webhook 45277, notify 37973, each probed free just before use.
- **Session sockets.** The daemon ran with its multiplexer socket directory under the throwaway folder, so no session of the operator was reachable and none was touched. The script itself issues no multiplexer command.
- **Everything** lives under one temporary folder outside the worktree, removed on every exit path.
- **Throwaway project.** A bare repository whose default branch is `trunk`, cloned to the worktree base repository; `refs/remotes/origin/HEAD` records it and `main` does not exist.
- **Worker.** The daemon's shell backend runs a generated script as the worker. Each Automaton declares the write directory `src/`; the script writes its sample file there (the session's own commit stages it) and, in the stray modes, a stray file at the worktree root, outside that directory (never staged). Every task carries a deterministic verification script, and the daemon's verification (a one-shot session on the same backend, `autonomous.verification_backend: shell`) is answered by the same script with a fixed pass, so no model is involved anywhere.

```
refs/remotes/origin/trunk; main exists: no
```

| Automaton | Worker mode | Used in |
|---|---|---|
| `d0c10001` held-commit | stray | checks 3, 4 (commit) |
| `d0c10002` held-discard | stray-once | checks 3, 4 (discard), 5 |
| `d0c10003` held-retry | stray-fix | checks 3, 4 (retry) |
| `d0c10004` mode-warn | stray | check 6 (warn) |
| `d0c10005` mode-off | declared | check 6 (off) |
| `d0c10006` mode-hold-control | declared | check 6 (control, policy hold) |


## 1. Config: the two settings

- **PASS** — a fresh config reads policy hold and scope task_files
  - status route: policy=`hold` scope=`task_files`; raw settings before any write: policy=`hold` scope=`task_files` (unset reads as the default)

- **PASS** — PUT then GET round-trips the policy (warn, off, hold) and the scope (worktree, task_files)
  - warn: HTTP 200, read `warn`, status `warn`; off: HTTP 200, read `off`, status `off`; hold: HTTP 200, read `hold`, status `hold`; worktree: HTTP 200, read `worktree`, status `worktree`; task_files: HTTP 200, read `task_files`, status `task_files`; 

- **PASS** — the Automata settings route round-trips both settings too
  - PUT HTTP 200 read policy `warn` scope `worktree`; restored with HTTP 200

- **PASS** — an unknown policy word is a 400 that names hold, warn and off (both routes)
  - `PUT /api/config`: 400 autonomous.automation.uncommitted_work_policy: must be "hold", "warn" or "off"  — settings route: 400 autonomous.automation.uncommitted_work_policy: must be "hold", "warn" or "off" 

- **PASS** — an unknown scope word is a 400 that names task_files and worktree (both routes)
  - `PUT /api/config`: 400 autonomous.automation.uncommitted_work_scope: must be "task_files" or "worktree"  — settings route: 400 autonomous.automation.uncommitted_work_scope: must be "task_files" or "worktree" 

- **PASS** — the refused writes saved nothing
  - policy `hold`, scope `task_files` after the four refused writes

## 2. Read surfaces agree

- **PASS** — `GET /api/autonomous/uncommitted-work` answers 200
  - HTTP 200

- **PASS** — the body has the policy, the scope and a held list whose entries carry the Automaton, kind, files, outside-scope files, question and time
  - {"policy":"hold","scope":"task_files","held":[{"automaton_id":"d0c10001","task_id":"d0c10001t1","kind":"task","files":["stray-d0c10001.txt"]},{"automaton_id":"d0c10002","task_id":"d0c10002t1","kind":"task","files":["stray-d0c10002.txt"]},{"automaton_id":"d0c10003","task_id":"d0c10003t1","kind":"task","files":["stray-d0c10003.txt"]}]}

- **PASS** — POST is a 405
  - HTTP 405

CLI text (`datawatch autonomous uncommitted-work`):
```
Automaton uncommitted work (policy: hold, scope: task_files):
  3 held:
  - Automaton held-commit (d0c10001), task Write the sample file for held-commit (d0c10001t1)
      files: stray-d0c10001.txt
      outside the task's declared files: stray-d0c10001.txt
      reason: files were still uncommitted after the commit
      held since: 2026-10-31 02:19:06 EDT
      question: q-18e388da0a947a69a051 (answer commit, discard or retry)
  - Automaton held-discard (d0c10002), task Write the sample file for held-discard (d0c10002t1)
      files: stray-d0c10002.txt
      outside the task's declared files: stray-d0c10002.txt
      reason: files were still uncommitted after the commit
      held since: 2026-10-31 02:19:18 EDT
      question: q-18e388dce537b6d6680c (answer commit, discard or retry)
  - Automaton held-retry (d0c10003), task Write the sample file for held-retry (d0c10003t1)
      files: stray-d0c10003.txt
      outside the task's declared files: stray-d0c10003.txt
      reason: files were still uncommitted after the commit
      held since: 2026-10-31 02:19:15 EDT
      question: q-18e388dc28e52acdc6e3 (answer commit, discard or retry)
This is a status read only: answer the question to release a hold.

```

Chat reply (`POST /api/test/message`, text `autonomous uncommitted-work`):
```
[dw-e2e-test] autonomous uncommitted-work
Automaton uncommitted work (policy: hold, scope: task_files):
  3 held:
  - Automaton held-commit (d0c10001), task Write the sample file for held-commit (d0c10001t1)
      files: stray-d0c10001.txt
      outside the task's declared files: stray-d0c10001.txt
      reason: files were still uncommitted after the commit
      held since: 2026-10-31 02:19:06 EDT
      question: q-18e388da0a947a69a051 (answer commit, discard or retry)
  - Automaton held-discard (d0c10002), task Write the sample file for held-discard (d0c10002t1)
      files: stray-d0c10002.txt
      outside the task's declared files: stray-d0c10002.txt
      reason: files were still uncommitted after the commit
      held since: 2026-10-31 02:19:18 EDT
      question: q-18e388dce537b6d6680c (answer commit, discard or retry)
  - Automaton held-retry (d0c10003), task Write the sample file for held-retry (d0c10003t1)
      files: stray-d0c10003.txt
      outside the task's declared files: stray-d0c10003.txt
      reason: files were still uncommitted after the commit
      held since: 2026-10-31 02:19:15 EDT
      question: q-18e388dc28e52acdc6e3 (answer commit, discard or retry)
This is a status read only: answer the question to release a hold.

```

MCP result (`autonomous_uncommitted_work`, first 700 bytes):
```
{"policy":"hold","scope":"task_files","held":[{"automaton_id":"d0c10001","name":"held-commit","task_id":"d0c10001t1","task_title":"Write the sample file for held-commit","kind":"task","files":["stray-d0c10001.txt"],"outside_scope":["stray-d0c10001.txt"],"reason":"files were still uncommitted after the commit","question_id":"q-18e388da0a947a69a051","held_at":"2026-10-31T02:19:06.685562508-04:00"},{"automaton_id":"d0c10002","name":"held-discard","task_id":"d0c10002t1","task_title":"Write the sample file for held-discard","kind":"task","files":["stray-d0c10002.txt"],"outside_scope":["stray-d0c10002.txt"],"reason":"files were still uncommitted after the commit","question_id":"q-18e388dce537b6d66

```

- **PASS** — REST, MCP, CLI (--json) and the chat verb return the same held list
  - comparison: `OK`; held entries: [{"automaton_id":"d0c10001","task_id":"d0c10001t1","kind":"task"},{"automaton_id":"d0c10002","task_id":"d0c10002t1","kind":"task"},{"automaton_id":"d0c10003","task_id":"d0c10003t1","kind":"task"}]

- **PASS** — the automaton filter narrows the list (an unknown id reads empty)
  - `?automaton=d0c10001` holds 1, an unknown id holds 0

## 3. Held task: a task that leaves a file uncommitted is not completed

Approach used: the **shell backend with a generated worker script** ran the task headlessly in the worktree the daemon made (no staging by hand was needed). The Automaton declares one write directory (`src/`); the worker writes its sample file there, which the session's own commit stages, and also drops `stray-<id>.txt` at the worktree root, outside the write directory, which that commit never stages.

Task JSON of the held task (`uncommitted_work` is the hold):
```
{
  "id": "d0c10001t1",
  "status": "blocked",
  "error": "uncommitted_work: stray-d0c10001.txt",
  "uncommitted_work": {
    "kind": "task",
    "policy": "hold",
    "files": [
      "stray-d0c10001.txt"
    ],
    "outside_scope": [
      "stray-d0c10001.txt"
    ],
    "reason": "files were still uncommitted after the commit",
    "question_id": "q-18e388da0a947a69a051",
    "held_at": "2026-10-31T02:19:06.685562508-04:00"
  }
}

```

- **PASS** — the task is held (blocked), not completed
  - status `blocked`, error `uncommitted_work: stray-d0c10001.txt`

- **PASS** — the hold lists the stray file
  - files ["stray-d0c10001.txt"], outside the declared files ["stray-d0c10001.txt"], reason `files were still uncommitted after the commit`

- **PASS** — only the stray file is listed: the files the daemon itself wrote into the worktree (session config, injected skills, ignore-file lines) are not counted
  - worktree status at that moment: `?? .datawatch/ ?? .gitignore ?? .mcp.json ?? stray-d0c10001.txt `

The question (`GET /api/questions/<id>`, the `question` record):
```
{
  "id": "q-18e388da0a947a69a051",
  "label": "uncommitted_work",
  "state": "open",
  "default": "hold",
  "context": "task",
  "options": [
    "commit",
    "discard",
    "retry"
  ],
  "text": "Task \"Write the sample file for held-commit\" (d0c10001t1) of Automaton \"held-commit\" (d0c10001) finished, but its worktree still has uncommitted files.\nCause: files were still uncommitted after the commit.\n\nFiles:\n- stray-d0c10001.txt (outside the task's declared files)\n\nNothing changes until you answer."
}

```

- **PASS** — an open question offers commit, discard and retry with the default hold
  - question `q-18e388da0a947a69a051`: options ["commit","discard","retry"], default `hold`, state `open`

- **PASS** — git agrees: the stray file is untracked in the worktree
  - ?? .datawatch/ ?? .gitignore ?? .mcp.json ?? stray-d0c10001.txt 

- **PASS** — the hold shows in the Automaton JSON, on the task
  - `GET` of the Automaton: stories[0].tasks[0].uncommitted_work present

- **PASS** — the Automaton is not completed
  - status `blocked`

- **PASS** — the hold is in the audit log and the decision log
  - audit events 1; decisions `approve run operator_decision uncommitted_work_held`

- **PASS** — the same hold is placed on the other two Automata (`d0c10002`)
  - task status `blocked`, files ["stray-d0c10002.txt"]

- **PASS** — the same hold is placed on the other two Automata (`d0c10003`)
  - task status `blocked`, files ["stray-d0c10003.txt"]

## 4. Answers: commit, discard and retry

- **PASS** — commit: the answer is accepted and the task completes
  - answer HTTP 201; task status `completed`

- **PASS** — commit: the task completes with a new post-verify commit (the worktree head)
  - head before the answer `d41d9408f`, post_verify_sha now `fb26890fe`, head now `fb26890fe`

- **PASS** — commit: the worktree is clean (apart from the daemon's own session files)
  - git status without those files: `clean`; with them: `?? .datawatch/ ?? .gitignore ?? .mcp.json `

- **PASS** — commit: the listed file is in the new commit on the Automaton branch
  - task(d0c10001): Write the sample file for held-commit  stray-d0c10001.txt 

Worktree of the discard case just before the answer (a sibling file was added after the hold was placed):
```
?? .datawatch/
?? .gitignore
?? .mcp.json
?? sibling-d0c10002.txt
?? stray-d0c10002.txt

```

- **PASS** — discard: the answer is accepted
  - answer HTTP 201

- **PASS** — discard: the listed file is gone from the worktree
  - stray file present: no

- **PASS** — discard: a sibling file that was not listed survives
  - sibling present: yes

- **PASS** — discard: the task ran again on the clean tree and completed
  - worker runs 2; task status `completed`

- **PASS** — retry: the task ran again (the worker ran a second time) and completed
  - answer HTTP 201; worker runs 1 then 2; worktree before the answer: `?? .datawatch/ ?? .gitignore ?? .mcp.json ?? stray-d0c10003.txt `; task status `completed`

- **PASS** — retry: the Automaton then completed on a clean tree
  - Automaton status `completed`; git status without the daemon's own session files ``

- **PASS** — each answer is in the audit log
  - resolved events per case (commit, discard, retry): 111

## 5. Whole-worktree block

The discard case from check 4 is the stray-file case: its sibling file is a stray untracked file that no task owns, left in the worktree when the task finished (the same state a cancel or a backend move leaves). Its task completed in check 4; the Automaton must now be held at the finished check.

Automaton JSON, the hold on the whole worktree (`uncommitted_work`) and the blocked reason:
```
{
  "status": "blocked",
  "blocked_reason": "files were still uncommitted when the Automaton finished: sibling-d0c10002.txt. Commit or discard them, then run the Automaton again.",
  "uncommitted_work": {
    "kind": "worktree",
    "policy": "hold",
    "files": [
      "sibling-d0c10002.txt"
    ],
    "reason": "files were still uncommitted when the Automaton finished",
    "question_id": "q-18e388de2e85515de126",
    "held_at": "2026-10-31T02:19:24.468363057-04:00"
  }
}

```

- **PASS** — with a stray untracked file in the worktree the Automaton is not reported completed
  - status `blocked`; hold kind `worktree`, files ["sibling-d0c10002.txt"]

`GET /api/autonomous/merge` while the Automaton is held (mode shadow lists completed Automata with a branch):
```
{"mode":"shadow","automata":[{"automaton_id":"d0c10001","branch":"automaton/d0c10001-held-commit","state":"unknown"},{"automaton_id":"d0c10003","branch":"automaton/d0c10003-held-retry","state":"unknown"}]}

```

- **PASS** — the held Automaton is not in the merge list
  - listed: ["d0c10001","d0c10003"]

- **PASS** — a completed Automaton on a clean tree is in the merge list (the list works, so the absence above is the block)
  - listed: ["d0c10001","d0c10003"]

- **PASS** — a question for the whole worktree is open with the same three options
  - question `q-18e388de2e85515de126`

- **PASS** — after the answer (commit) the Automaton completes
  - answer HTTP 201; status `completed`

- **PASS** — the tree is clean and the hold is cleared
  - git status without the daemon's own session files ``

- **PASS** — once completed it is listed as merge ready
  - listed: ["d0c10001","d0c10002","d0c10003"]

- **PASS** — the held list is empty again
  - []

## 6. Modes: warn and off

- **PASS** — control (policy hold): a file the task declared but the session did not commit is committed by the executor and the Automaton completes
  - Automaton status `completed`; recent commits `task(d0c10006): Write the sample file for mode-hold-control|task(d0c10006): Write the sample file for mode-hold-control|Initial commit|`; git status `?? .datawatch/ ?? .gitignore ?? .mcp.json `

Decision log of the warn case:
```
[{"kind":"uncommitted_work_warned","note":"task d0c10004t1: files were still uncommitted after the commit; uncommitted: stray-d0c10004.txt; outside the declared files: stray-d0c10004.txt"},{"kind":"uncommitted_work_warned","note":"files were still uncommitted when the Automaton finished; uncommitted: stray-d0c10004.txt"}]

```

- **PASS** — warn: the task and the Automaton complete although the stray file is uncommitted
  - Automaton `completed`, task `completed`; git status `?? .datawatch/ ?? .gitignore ?? .mcp.json ?? stray-d0c10004.txt `

- **PASS** — warn: a decision and an audit event are recorded, no hold and no question
  - decisions `approve run uncommitted_work_warned uncommitted_work_warned`; audit events 2; held entries 0

Worktree of the off case after it completed (the declared file is still untracked: no commit attempt):
```
?? .datawatch/
?? .gitignore
?? .mcp.json
?? notes/
---
6e1b774 task(d0c10005): Write the sample file for mode-off
c5795cd Initial commit

```

- **PASS** — off: the task and the Automaton complete
  - Automaton `completed`, task `completed`

- **PASS** — off: no decision and no audit event of the check
  - decisions `approve run`; events 0

- **PASS** — off: no commit attempt (the declared file that the executor commits under hold stays untracked)
  - git status `?? .datawatch/ ?? .gitignore ?? .mcp.json ?? notes/ `; commits `task(d0c10005): Write the sample file for mode-off|Initial commit|`

- **PASS** — off: nothing is held
  - policy `off`

## 7. Cleanup

- **PASS** — the sandbox daemon and its sessions are gone and no test daemon was left behind
  - daemon gone: yes; port 45901 closed: yes; leftover check exit 0

- The daemon was ended by `td_stop_all` from `scripts/test-daemon-lib.sh` (the recorded pid, config path as marker); nothing was ended by name.
- The scratch area (project, remote, worktrees, data, config, log, records, worker, state) is removed by the exit handler right after this report is written.

## Daemon log, the uncommitted-work lines

```
{"ts":"2026-10-31T02:19:21.181044994-04:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/uncommitted-work","remote_ip":"127.0.0.1","status":200,"user_agent"
{"ts":"2026-10-31T02:19:21.189545109-04:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/uncommitted-work","remote_ip":"127.0.0.1","status":200,"user_agent"
{"ts":"2026-10-31T02:19:34.581078046-04:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/uncommitted-work","remote_ip":"127.0.0.1","status":200,"user_agent"
{"ts":"2026-10-31T02:19:34.587983123-04:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/uncommitted-work","remote_ip":"127.0.0.1","status":200,"user_agent"
{"ts":"2026-10-31T02:19:50.755942899-04:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/uncommitted-work","remote_ip":"127.0.0.1","status":200,"user_agent"
{"ts":"2026-10-31T02:19:58.916341557-04:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/uncommitted-work","remote_ip":"127.0.0.1","status":200,"user_agent"
{"ts":"2026-10-31T02:19:58.923291401-04:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/uncommitted-work","remote_ip":"127.0.0.1","status":200,"user_agent"
{"ts":"2026-10-31T02:19:58.930623548-04:00","actor":"admin","action":"http_access","details":{"method":"GET","path":"/api/autonomous/uncommitted-work","remote_ip":"127.0.0.1","status":200,"user_agent"
2026/10/31 02:19:21 [autonomous] d0c10001 task d0c10001t1 uncommitted-work: operator committed 1 file(s) as fb26890f
2026/10/31 02:19:21 [autonomous] d0c10002 task d0c10002t1 uncommitted-work: operator discarded 1 file(s)
2026/10/31 02:19:34 [autonomous] d0c10002 worktree uncommitted-work: operator committed 1 file(s) as 139fd19b
2026/10/31 02:19:40 [autonomous] d0c10006/d0c10006t1 uncommitted-work: committed 1 file(s) as f4a91224
```

## Spec amendments and what is not shown live

- **How the held state was made.** The shell backend ran a generated worker script headlessly in the worktree the daemon created, so no state had to be staged by hand. The stray file is outside the Automaton's declared write directory, which is what keeps the session's own commit from staging it and what makes the executor treat it as outside the task's declared files (it never commits those by itself).
- **The Automata were written into the sandbox store, not created through the create route.** The route only makes drafts and a plan needs a planner model, which a sandbox does not have. Approve, run, the answers, every read and both settings go through the REST API of the running daemon; the worktrees and branches were made by the daemon.
- **Check 5 reaches the whole-worktree block with the sibling file of the discard case.** A file that appears in the worktree after the hold was placed is left alone by discard (check 4), so it is still there when the task completes and the Automaton rolls up: the same state a cancel or a backend move leaves behind. The merge list is read with the merge-pending mode set to `shadow`, which lists completed Automata with a branch.
- **Warn and off are shown on one Automaton each**, with the policy changed through the config route between runs. Off is shown as "no commit attempt" by a file the task declared: under hold (the control run) the executor commits it, under off it stays untracked.
- **Two defects the live runs found, fixed in the daemon (each has a unit test).** First, the check counted the files the daemon itself writes into a worktree when a session starts (the MCP discovery file, injected skills, and the lines it adds to the ignore file) as uncommitted work, so the first task of any project that does not already ignore them was held; the detector now leaves them out, and check 3 asserts that only the stray file is listed. Second, when the whole-worktree hold raised its question, the question store's own save of the Automaton made the roll-up read another writer and abort, which left the Automaton `running` with nothing to run instead of `blocked`; the hold now counts that save as its own, and check 5 asserts the blocked state.
- **Not shown live:** a failing commit hook (the commit-failed hold and its fresh question after a failed answer), a discard that is refused because a listed path leaves the task directory, an expired or withdrawn question keeping its hold, the `worktree` scope on a task, a second hold on an Automaton that already carries one, and the PWA card. These are covered by the unit tests of the check and by the PWA tests; this script drives the daemon through REST, MCP, the command line and chat only.
- **Generality.** The project is a throwaway repository whose default branch is `trunk` and has no `main`; the base repository and the worktree root come from the daemon configuration; nothing names the repository this script lives in.

Result: PASS

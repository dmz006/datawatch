---
docs:
  index: true
  topics: [autonomous, automation, automaton, uncommitted, worktree, commit, hold, discard, retry, merge ready, policy, scope]
exec_params: []
exec_steps:
  - tool: autonomous_uncommitted_work
    description: Read the uncommitted-work policy and scope in effect and every task or Automaton that is held because its work was not committed (read-only)
    args: {}
    read_only: true
  - tool: question_list
    description: List the open operator questions; a held task or Automaton waits on one labelled uncommitted_work
    args: {state: open}
    read_only: true
  - tool: question_get
    description: Open one question to read the files, the reason and the three options
    args: {id: ""}
    read_only: true
  - tool: question_answer
    description: Answer the held question with commit, discard or retry; the answer is acted on at once
    args: {}
    read_only: false
  - tool: get_config
    description: Read the daemon settings, including the two autonomous.automation uncommitted-work keys
    args: {}
    read_only: true
  - tool: config_set
    description: Set the uncommitted-work policy (hold, warn or off); it applies to the next task that finishes
    args:
      key: autonomous.automation.uncommitted_work_policy
      value: "{{params.policy}}"
    read_only: false
  - tool: config_set
    description: Set the uncommitted-work scope (task_files or worktree)
    args:
      key: autonomous.automation.uncommitted_work_scope
      value: "{{params.scope}}"
    read_only: false
---

# Automaton uncommitted-work check

An Automaton that runs in worktree mode does its work on its own branch
(`automaton/<id>`), checked out in its own worktree. The work only counts once
it is **committed** to that branch: merging the branch later ships commits, not
files that happen to be lying in the worktree.

Before this check, a task could be marked complete while its work sat
uncommitted. The next tasks then ran on top of it, the Automaton finished, and
a merge of the branch shipped without that work. Nothing said so. The
**uncommitted-work check** closes that gap. It looks at the worktree at two
moments, and when it finds work that is not committed it stops and tells you
which files, instead of carrying on.

It works for any project you run Automata on. It only applies to an Automaton
that commits its own work: one that has git settings and either a worktree
branch or declared write directories. An Automaton that does not commit is not
checked.

## The two checks

| Check | When it runs | What it looks at | What it can do |
|---|---|---|---|
| **Per task** | When a task has finished, after its verification and quality gate pass, before it is marked complete | The files the task owns (the setting *scope* below) | Commit the task's own files, then hold the task if anything is still uncommitted |
| **Before finished** | Once, when every task of an Automaton is done and it is about to be reported finished or merge ready | The **whole worktree** | Block the finished state and list the files. It never commits anything itself |

### What happens to each task

When a task's worker finishes and the task passes its checks, the daemon looks
at the worktree with `git status`:

1. **Commit the declared files.** Files the task declared (its planned files and
   the files it generated), or that sit under the write directories declared for
   it, are staged by name and committed with a message of the form
   `task(<short id>): <task title>`. Nothing is staged with `git add -A` or
   `git add .`, and the commit runs the repository's own commit hooks: a failing
   hook is a failed commit.
2. **Look again.** If the worktree is now clean for the task, it is marked
   complete as usual, and the commit is the one its post-verify revision is
   pinned to.
3. **Otherwise hold it.** If the commit failed, or files are still uncommitted
   (including any outside the task's declared files), the task is **held**:
   it is parked as blocked with the reason and the list of files, and one
   question is raised for you (see *The question* below). It does not use up the
   task's automatic-retry budget and records no failed attempt, because nothing
   about the work failed; it is waiting for you.

The daemon's own scratch files are never counted: the worker checkpoint note,
the decompose output, the advisory tree lock, and anything the daemon's staging
guard refuses to commit. Files git ignores never show up in `git status`, so
they are not counted either.

### The check before an Automaton is reported finished

When every task is done, the Automaton does not go straight to `completed`.
First the daemon looks at the **whole worktree**, not only task files. This
catches work no single task owns, and work left behind by a cancel or a move to
another backend. If the worktree is dirty:

- with policy `hold` (the default) the Automaton does **not** reach the
  finished state. It becomes `blocked`, its blocked reason names the cause and
  lists the files, and a question is raised. Because it is not `completed`, it
  is never listed as merge ready, so a branch with uncommitted work cannot
  slip into a merge;
- with policy `warn` the warning is recorded and the Automaton finishes;
- with policy `off` nothing is checked.

The scope setting does not narrow this check: it always covers the whole
worktree, and it never stages or commits anything. You decide what happens to
the files.

## The policies

One key chooses what the daemon does when work is found uncommitted:
`autonomous.automation.uncommitted_work_policy`.

| Policy | What it does |
|---|---|
| `hold` (default) | The task (or the Automaton, at the finished check) is held with the reason and files, and a question is raised. Nothing changes until you answer |
| `warn` | The daemon still commits a task's own declared files. If anything remains uncommitted it records the warning (a decision, an audit event and a warning alert) and lets the task or Automaton complete. No question is raised |
| `off` | No check and no commit attempt, at either moment. The task behaves as it did before this check existed |

An unset key, or a word that is not one of these, reads as `hold`. A write of
any other word is refused on every surface and nothing is saved.

## The scopes

The second key, `autonomous.automation.uncommitted_work_scope`, decides which
files the **per-task** check counts.

| Scope | Files the per-task check counts |
|---|---|
| `task_files` (default) | Only files the task itself changed. Files that were already modified or untracked before the task started are left out, so a worktree that was already dirty does not hold every task |
| `worktree` | Every uncommitted file in the worktree, whoever left it |

An unset key, or a word that is not one of these, reads as `task_files`. The
scope has no effect on the check before an Automaton is reported finished,
which always covers the whole worktree.

## Files outside a task's declared files

A file the task neither declared nor was allowed to write (it is not in the
task's planned or generated files and not under its write directories) is
**outside the task's declared files**. The daemon reports it but never commits
it by itself: it might be a stray edit, a generated file or another session's
work, and committing it would put it on the branch without anyone looking.

- The question and the status list these files separately.
- When a hold contains any, the question's recommended answer is **retry**,
  with a note to check those files first. When every file is inside the
  declared files, the recommended answer is **commit**.
- Only you can commit them, by answering **commit**: that is your decision,
  made with the list in front of you.

## The question

A hold raises one question per hold, labelled `uncommitted_work`. It names the
Automaton, the task, the cause and the files (at most the first 20, with a
count of the rest), and marks the ones outside the declared files. Its options:

| Option | Meaning |
|---|---|
| `commit` | Commit exactly the listed files and carry on |
| `discard` | Remove the listed changes from the worktree. They cannot be recovered |
| `retry` | Touch no file: run the task again, or look at the worktree again |

The question's default is **hold**: until you answer one of the three options,
nothing changes. A question that is withdrawn or expires keeps the hold, and so
does an answer to a question that no longer matches the hold (the hold was
replaced or already resolved).

Answer it like any operator question: the question queue in the PWA, `datawatch
question answer`, the MCP tool `question_answer`, `POST /api/questions/{id}/answer`
or the comm channel verb `question answer`. See
[Answering operator questions](operator-questions-answering.md). If
the daemon has no question store, the hold still stands and the status shows no
question; run the task again with the reset task action once you have dealt
with the files yourself.

### What the three answers do

| Answer | On a held task | On a held Automaton (whole worktree) |
|---|---|---|
| `commit` | Looks at the worktree again, stages and commits **exactly** the listed files that are still uncommitted (the outside-the-declared-files ones too, because you chose it), then marks the task complete with its post-verify revision. If files are still uncommitted after that, the task is held again under the current policy | Commits the listed files that are still uncommitted, clears the hold and lets the Automaton roll up again, which checks the worktree once more |
| `discard` | Restores the listed tracked files to the branch's last commit (index and worktree) and deletes the listed untracked files, then sends the task back to run again with its attempt history kept | Does the same for the listed files, clears the hold and lets the Automaton roll up again |
| `retry` | Touches no file. Sends the task back to run again, with its attempt history kept | Touches no file. Clears the hold and lets the Automaton roll up again, which holds it again if files remain |

When the answer is acted on, the Automaton runs on by itself: a running
Automaton is woken, and one that had gone `blocked` on the worktree check goes
back to running.

### What each answer never touches

- **commit** never stages anything it was not asked to: only the paths in the
  hold, by name, never `-A` and never `.`, and never anything another process
  staged in the meantime. It never skips the repository's commit hooks, and it
  never pushes, merges or rebases.
- **discard** never touches a path that is not in the hold, so a file that
  appeared after the hold was placed is left alone. It never touches the `.git`
  directory, never follows a path out of the worktree, and if any listed path
  would lie outside the task directory it refuses and deletes nothing at all.
- **retry** never touches a file, a commit or the branch.

If a commit or a discard cannot be done (a failing hook, a git error, a refused
path), the hold stays, the reason is recorded, a warning alert is raised and a
**fresh question** is raised, so you always have a way to answer again.

## Reading the state

Every surface reads the same status and none of them changes anything.

| Surface | How to read it |
|---|---|
| REST | `GET /api/autonomous/uncommitted-work`, optionally `?automaton=<id>` (only GET; 503 while Automata are disabled; a federated peer needs the autonomous read capability) |
| MCP | `autonomous_uncommitted_work` (read-only), with the optional `automaton` argument |
| CLI | `datawatch autonomous uncommitted-work`; add `--automaton <id>` to narrow it, `--json` for the raw body |
| Comm channel | `autonomous uncommitted-work [automaton-id]` |
| PWA | Automata tab → Automation view → **Overview** sub-tab → the **Automata uncommitted work** card, refreshed every 30 seconds; a **Uncommitted work (N)** chip on a held task; a banner on a held Automaton's detail page |
| Mobile and wear | The same REST contract, described in `docs/api/openapi.yaml` |

The status is built from what the daemon has stored. It runs no git command, so
reading it is cheap and cannot disturb a running worktree.

It holds the policy and scope in effect and a list `held` (an empty list when
nothing is held), one entry per held task or Automaton:

| Field | Meaning |
|---|---|
| `automaton_id`, `name` | The Automaton |
| `task_id`, `task_title` | The task; absent for a whole-worktree hold |
| `kind` | `task` or `worktree` |
| `files` | The uncommitted files |
| `outside_scope` | The subset outside the task's declared files |
| `reason` | Why it is held: the commit failed, or files were still uncommitted |
| `question_id` | The question waiting for your answer |
| `held_at` | When the hold was placed |

The same hold is also carried as `uncommitted_work` on the held task in the
Automaton's task list, and on the Automaton's own summary when the hold is on
the whole worktree.

## Where to see

| Location | What you see |
|---|---|
| **Web UI** | Automation view → Overview sub-tab → **Automata uncommitted work** card; a chip on a held task row; a banner on a held Automaton |
| **REST API** | `GET /api/autonomous/uncommitted-work` → `policy`, `scope`, `held[]` |
| **MCP** | `autonomous_uncommitted_work` |
| **CLI** | `datawatch autonomous uncommitted-work` |
| **Comm channel** | `autonomous uncommitted-work` reply |
| **Audit log** | Events `autonomous_uncommitted_work_held`, `autonomous_uncommitted_work_warned`, `autonomous_uncommitted_work_resolved` |
| **Automaton decision log** | Decisions `uncommitted_work_held`, `uncommitted_work_warned`, `uncommitted_work_resolved`, `uncommitted_work_discarded` and the failure kinds in the operations reference |
| **Alerts** | Warning alerts *Uncommitted work held*, *Uncommitted work warning* and *Uncommitted work not resolved* |

## Changing the settings

| Method | How |
|--------|-----|
| **YAML** | `~/.datawatch/config.yaml` → `autonomous:` → `automation:` → `uncommitted_work_policy: hold` and `uncommitted_work_scope: task_files` |
| **CLI** | `datawatch config set autonomous.automation.uncommitted_work_policy warn` and `datawatch config set autonomous.automation.uncommitted_work_scope worktree` |
| **Web UI** | Settings → General → Automata settings → **Uncommitted-work check**: *Automaton uncommitted-work policy* and *Automaton uncommitted-work scope* |
| **REST API** | `PUT /api/config` with `{"autonomous.automation.uncommitted_work_policy": "warn"}`, or `PUT /api/autonomous/config` with the `automation` object; `GET /api/config` shows the value in effect |
| **Comm channel** | `configure autonomous.automation.uncommitted_work_policy=warn` |

The MCP tool `config_set` takes the same dotted key and value, and
`autonomous_config_set` takes `automation_uncommitted_work_policy` and
`automation_uncommitted_work_scope`. A value that is not allowed is refused with
an error naming the allowed words, and nothing is written. Both keys are read
live: a change applies to the next task that finishes, with no restart.

| Key | Meaning | Default | Allowed |
|---|---|---|---|
| `autonomous.automation.uncommitted_work_policy` | What happens when work is found uncommitted | `hold` | `hold`, `warn`, `off` |
| `autonomous.automation.uncommitted_work_scope` | Which files the per-task check counts | `task_files` | `task_files`, `worktree` |

## Base requirements

- A running daemon with the Automata engine enabled.
- An Automaton that commits its own work (git settings plus a worktree branch or
  declared write directories). The check before an Automaton is reported
  finished needs a local worktree: a work branch, a project directory and no
  cluster profile.
- `git` on the daemon's machine. If git cannot read the worktree's status (it
  is not a git repository, for example), the check is skipped and the task or
  Automaton goes on as before.
- For a question to be raised, the operator-question store must be available.

## Worked example

An Automaton has five tasks and works on `automaton/4f2a`. Task 3, *Add the
export route*, declared two files: `api/export.go` and `api/export_test.go`.

1. **The worker finishes.** It wrote both declared files and also touched
   `README.md` and `tmp-notes.txt`, but its session ended before it committed.
   Verification and the quality gate pass.
2. **The daemon commits the declared files.** It stages `api/export.go` and
   `api/export_test.go` by name and commits them as
   `task(4f2a): Add the export route`.
3. **The daemon looks again.** `README.md` and `tmp-notes.txt` are still
   uncommitted and are outside the task's declared files. With policy `hold`,
   the task is held: blocked, with the reason *files were still uncommitted
   after the commit*. A question is raised, and the task is not marked complete.
4. **You read it.**

   ```
   $ datawatch autonomous uncommitted-work
   Automaton uncommitted work (policy: hold, scope: task_files):
     1 held:
     - Automaton Export (4f2a), task Add the export route (t3)
         files: README.md, tmp-notes.txt
         outside the task's declared files: README.md, tmp-notes.txt
         reason: files were still uncommitted after the commit
         held since: 2031-03-02 10:14:00 UTC
         question: q-91 (answer commit, discard or retry)
   This is a status read only: answer the question to release a hold.
   ```

5. **You decide.** The question recommends **retry**, because both files are
   outside the declared files. You open them: the README edit is a stray change
   and the notes file is scratch. You answer **discard**. The daemon restores
   `README.md` to its last commit and deletes `tmp-notes.txt`; nothing else in
   the worktree is touched, and the task runs again on a clean tree with its
   attempt history kept.
6. **The Automaton finishes.** After task 5, the daemon checks the whole
   worktree once more. It is clean, so the Automaton becomes `completed` and
   can be listed as merge ready. Had a stray file still been there, the
   Automaton would have become `blocked` instead, with the file listed, and the
   branch would not have been offered for merging.

## What it never does

It never commits a file outside a task's declared files by itself, never uses
`git add -A` or `git add .`, never skips the repository's commit hooks, and
never pushes or merges. It never discards anything except the exact files in a
hold that you chose to discard. The status read never runs git and never
changes anything. In `off` it does not look at the worktree at all.

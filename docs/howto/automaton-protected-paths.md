---
docs:
  index: true
  topics: [autonomous, automation, automaton, protected paths, rules file, CODEOWNERS, workflows, hold, approve, reject, authorised paths, base commit, rules review, checkpoint, checkpoint entry revert, worker progress note, CHECKPOINT.md]
exec_params:
  - {name: automaton_id, required: false, description: "Automaton ID to narrow the read to, or to authorise paths for"}
  - {name: action, required: false, description: "Protected-paths action to set: hold, warn, block or off"}
exec_steps:
  - tool: autonomous_protected_paths
    description: Read the protected-paths action and rules source in effect, the protected list, every task held on protected paths and every Automaton that authorises paths in advance (read-only)
    args: {}
    read_only: true
  - tool: question_list
    description: List the open operator questions; a held task waits on one labelled protected_paths
    args: {state: open}
    read_only: true
  - tool: question_get
    description: Open one question to read the changed paths and the three answers
    args: {id: ""}
    read_only: true
  - tool: question_answer
    description: Answer the held question with approve, reject or change_spec; the answer is acted on at once
    args: {}
    read_only: false
  - tool: autonomous_prd_set_authorized_paths
    description: Replace the list of protected paths one Automaton may change without a decision (an empty list clears it)
    args:
      id: "{{params.automaton_id}}"
      paths: []
    read_only: false
  - tool: get_config
    description: Read the daemon settings, including the three autonomous.automation protected-paths keys
    args: {}
    read_only: true
  - tool: config_set
    description: Set the protected-paths action (hold, warn, block or off); it applies to the next task that finishes an attempt
    args:
      key: autonomous.automation.protected_paths_action
      value: "{{params.action}}"
    read_only: false
---

# Automaton protected paths

An Automaton hands its tasks to workers, and a worker can edit any file inside
the project. Some files must not change without you: the file that holds the
project's rules, the supervisor rules, the code owners file and the workflow
files. A worker that weakens the rules it is judged by, or that edits a
workflow so a check passes, makes everything after it untrustworthy, and
nothing in the history says who did it.

**Protected paths** close that gap. They are a list of files and directories
that an Automaton task may not change without a decision from you. Two parts
work together:

| Part | What it does |
|---|---|
| **The gate** | After a worker attempt finishes, the daemon compares the task's directory with the commit the work started from. If a protected path changed and the Automaton has not been authorised for it, the task is held, warned about or blocked, as you chose — with one narrow exception, a worker's checkpoint entry in the rules file, which the gate strips out itself ([the checkpoint-entry revert](#the-checkpoint-entry-revert)) |
| **The base-commit rules read** | When the rules review judges an Automaton, it reads the rules from the commit the work started from, not from the working tree the worker changed. A worker cannot rewrite the rubric it is graded by, or skip the review by deleting the rules file |

It works for any project you run Automata on. Nothing in it is specific to one
repository: the list is yours to change, and the defaults below are only a
starting point.

## The defaults

With no setting made, the protected list is:

| Entry | What it covers |
|---|---|
| The project rules file | The file named by `autonomous.rules_file`, `AGENT.md` when that is unset. A project that uses `RULES.md` sets `autonomous.rules_file: RULES.md` and the default list follows |
| `docs/plans/2026-10-17-supervisor-rules-and-lessons.md` | The supervisor rules |
| `CODEOWNERS` | The code owners file |
| `.github/workflows/` | Every workflow file, at any depth |

The action is **hold**: a task that changes one of these is parked and you are
asked. The rules review reads the rules from the **base commit**.

An unset key, an empty value and a word that is not one of the allowed words
read as these defaults. A value written through a command, the API or the PWA
that is not allowed is refused and nothing is saved.

## The keys

Three keys under `autonomous.automation` control it.

| Key | Meaning | Default | Allowed |
|---|---|---|---|
| `autonomous.automation.protected_paths` | The paths a task may not change without a decision | The default list above | A list of repository-relative paths, directories and globs; at most 100 entries of at most 256 bytes. An empty list reads the default |
| `autonomous.automation.protected_paths_action` | What happens when a task changes a protected path | `hold` | `hold`, `warn`, `block`, `off` |
| `autonomous.automation.rules_review_source` | Where the rules review reads the project rules from | `base_commit` | `base_commit`, `worktree` |

An explicit `protected_paths` list **replaces** the default list entirely. If
you want the defaults and one more path, write all of them. To go back to the
defaults, clear the list.

### The words

| Key | Word | What it does |
|---|---|---|
| `protected_paths_action` | `hold` (default) | The task is parked as blocked with the changed paths, and one question is raised. Nothing changes until you answer |
| | `warn` | The finding is recorded as a decision, an audit event and a warning alert. The task goes on, and no question is raised |
| | `block` | The task is parked as blocked with the changed paths and **no question**. You authorise the path for the Automaton or edit the task, then reset it |
| | `off` | No check at all |
| `rules_review_source` | `base_commit` (default) | The rules review is given the text of the rules files as they were at the base commit |
| | `worktree` | The rules review is told to read the rules files from the task's working directory, as it did before this check existed |

`hold`, `warn` and `block` are the three actions; `off` switches the gate
off. In every action, a path the Automaton is authorised for passes without a
question (see *Authorising paths for one Automaton*).

## Changing the settings

| Method | How |
|--------|-----|
| **YAML** | `~/.datawatch/config.yaml` → `autonomous:` → `automation:` → `protected_paths: [AGENT.md, ops/]`, `protected_paths_action: hold` and `rules_review_source: base_commit` |
| **CLI** | `datawatch config set autonomous.automation.protected_paths "AGENT.md,ops/"`, `datawatch config set autonomous.automation.protected_paths_action warn` and `datawatch config set autonomous.automation.rules_review_source worktree` |
| **Web UI** | Settings → General → Automata settings → **Protected paths**: *Automaton protected paths*, *Automaton protected-paths action* and the rules review source; the group's help link opens the definitions |
| **REST API** | `PUT /api/config` with `{"autonomous.automation.protected_paths_action": "warn"}` (the list takes a JSON array or one comma-separated string), or `PUT /api/autonomous/config` with the `automation` object; `GET /api/config` shows the value in effect |
| **Comm channel** | `configure autonomous.automation.protected_paths_action=warn` |
| **MCP** | `config_set` with the same dotted key and value |

A value that is not allowed is refused with an error that names the allowed
words or the offending entry, and nothing is written. All three keys are read
live: a change applies to the next task that finishes an attempt, with no
restart.

## What counts as a change

The gate asks one question: *did this task change a protected path?* It
answers from git alone, read-only, with no model involved. It compares the
task's directory with the **base commit**, so it sees:

- files the task committed;
- edits to tracked files that are not committed yet;
- new files that are not tracked yet;
- renames and deletions, which count for both the old and the new path.

A file that was already modified or untracked **before** the task started is
your own earlier work and is left out, unless a commit made since the base
touched it.

The rules file has one more exception, and it applies everywhere the daemon
reasons about a tree. When a session starts, the daemon writes its own marked
sections into the rules file — the session guardrails header, the memory,
RTK, cross-session and platform context sections. Those sections are the
daemon's work, not the worker's, and they are not changes: the uncommitted
work check, the read-only task check, this protected-paths gate, the merge
preflight before a task branch is folded back, the dirty-tree start guard, the
await probe that decides when the tree may be taken again, and the dirty check
when a worktree is cleaned up all compare the file with those marked sections
stripped on both sides, so a protected rules file counts as changed only when
its content changes outside them. In the merge preflight the same rule means a
rules-file commit that carries only daemon sections is stripped out before the
merge, while a rules file that also carries a real edit is rewritten without
the daemon's sections and the real edit is kept. A real edit to the rules is
still held and asked, exactly as before, and there is no new setting behind
any of this: the same protected list and the same action apply.

Beside those, the rules file carries one narrower, visible exception: if a
task's only change against the base is a worker checkpoint entry, the gate
strips that entry out of the file before any hold, warn or block is decided
([the checkpoint-entry revert](#the-checkpoint-entry-revert)) — so a
checkpoint append is neither a change the gate holds on nor a daemon section
it strips silently.

One more consequence of the marked sections: platform context text that was
ever committed inside `AGENT.md` or `CLAUDE.md` is stale by construction,
because the daemon owns the current text and re-delivers it at every session
start. At daemon start — and periodically afterwards — that marked platform
context block is removed from the working copy of those files, for the
projects of your stored Automata. The removal is never committed by the
daemon; the block simply leaves the commit you next make for the file.

The check runs in direct mode and in worktree mode. It is not skipped for an
Automaton that works on its own branch.

### The three pattern forms

Each entry of the list is a repository-relative path in one of three forms,
matched case-sensitively:

| Form | Example | Matches |
|---|---|---|
| **File** | `CODEOWNERS` | Only that path |
| **Directory** | `ops/` or `ops/**` | Every path below that directory, at any depth |
| **Glob** | `docs/plans/*.md` | A pattern using `*`, `?` and `[ ]`, matched against the whole path. A `*` never crosses a slash, so `src/*.go` matches `src/a.go` and not `src/sub/a.go` |

Absolute paths and any entry with a `..` segment are refused when you write
them. A leading `./` is dropped, and duplicates are removed.

## The question and its three answers

With the action `hold`, a held task raises one question, labelled
`protected_paths`. It names the Automaton, the task and the changed protected
paths (at most the first 20, with a count of the rest) and says what each
answer does. It carries no recommendation: the gate has no basis to prefer one
answer. Its default is **hold**, so until you answer, nothing changes. A
question that is withdrawn or expires keeps the hold, and so does an answer to
a question that no longer matches the hold.

| Option | Label you see | What it does |
|---|---|---|
| `approve` | **Approve this change** | The task keeps the change and completes. The approval covers only these paths, for this task |
| `reject` | **Reject it** | The listed paths are put back to their state at the base commit, in a **new** commit, and the task runs again |
| `change_spec` | **Change the spec** | The task stays blocked so you can edit its instructions; then you reset it |

### What the three answers do

- **Approve this change** adds the paths to the task's approved paths and marks
  the task complete. Another task of the same Automaton that changes one of
  these paths is held again: to cover the whole Automaton, authorise the paths
  for it instead. Approving never changes the Automaton's authorised paths. An
  Automaton that went blocked on the hold goes back to running.
- **Reject it** puts every listed path back to its content at the base commit
  and commits that as a new commit with the message `task(<short id>): restore
  protected paths`. A path the task added is removed. History is never
  rewritten, nothing is pushed, and the task's attempt history is kept. The
  task then runs again with a note telling the worker the paths were put back
  and must not be changed. The rejection uses none of the task's automatic
  retry budget. If any listed path would lie outside the task directory, the
  restore refuses and touches nothing at all.
- **Change the spec** touches no file. The task stays blocked, marked as
  waiting for its instructions to change. Edit the task (the MCP tool
  `autonomous_prd_edit_task`, or the task editor in the PWA), then reset it
  (`autonomous_prd_reset_task`, or the reset task action). The reset clears
  the hold and the gate runs again on the next attempt.

If a restore cannot be done (a failing hook, a git error, a refused path), the
hold stays, the reason is recorded on it, a warning alert *Protected paths not
restored* is raised and a **fresh question** is raised, so you always have a
way to answer again.

A held task consumes none of its automatic-retry budget and records no failed
attempt: nothing about the work failed; it is waiting for you.

Answer it like any operator question: the question queue in the PWA, `datawatch
question answer`, the MCP tool `question_answer`, `POST
/api/questions/{id}/answer` or the comm verb `question answer`. See
[Answering operator questions](operator-questions-answering.md).

### The warn and block actions

With `warn`, the task is not held: the gate records a `protected_paths_warned`
decision, writes an audit event and raises a warning alert *Protected paths
warning*, then lets the task go on. With `block`, the task is parked as blocked
with the error `protected_path_blocked: <paths>` and no question is raised;
authorise the path for the Automaton, or edit the task, then reset it. In
neither case does the gate touch the change — the one thing it reverts on its
own is a worker's checkpoint entry in a rules file, and only when that is the
file's entire change against the base ([the checkpoint-entry
revert](#the-checkpoint-entry-revert)); apart from that, the change stays
where the worker put it.

## Reading the state

Every surface reads the same status and none of them changes anything.

| Surface | How to read it |
|---|---|
| REST | `GET /api/autonomous/protected-paths`, optionally `?automaton=<id>` (only GET; 503 while Automata are disabled; a federated peer needs the autonomous read capability) |
| MCP | `autonomous_protected_paths` (read-only), with the optional `automaton` argument |
| CLI | `datawatch autonomous protected-paths`; add `--automaton <id>` to narrow it, `--json` for the raw body |
| Comm channel | `autonomous protected-paths [automaton-id]` |
| PWA | Automata tab → Automation view → **Overview** sub-tab → the **Automata protected paths** card, refreshed every 30 seconds; a **Protected paths (N)** chip on a held task; a banner on a held Automaton's detail page |
| Mobile and wear | The same REST contract, described in `docs/api/openapi.yaml` |

The status is built from what the daemon has stored. It runs no git command, so
reading it is cheap and cannot disturb a running worktree. It holds:

| Field | Meaning |
|---|---|
| `action` | The protected-paths action in effect |
| `rules_source` | Where the rules review reads the rules from |
| `paths` | The effective protected list: yours, or the defaults |
| `held` | One entry per held task (an empty list when nothing is held) |
| `authorized` | One entry per Automaton that authorises paths in advance (an empty list when none) |

A held entry has `automaton_id`, `automaton_title`, `task_id`, `task_title`,
`paths`, `action`, `reason`, `question_id`, `since`, `base_commit` and
`awaiting_spec_change`. The same hold is carried as `protected_path_hold` on
the held task in the Automaton's task list, and the paths you approved are
`approved_protected_paths` on the task.

## Authorising paths for one Automaton

Sometimes an Automaton is *meant* to edit a protected path: a rules update, a
workflow change. Rather than answer a question for every task, authorise the
paths for that Automaton in advance. Its tasks may then change them without a
decision. Every other Automaton is still held.

| Method | How |
|---|---|
| **REST API** | `POST /api/autonomous/prds/{id}/set_authorized_paths` with `{"paths": ["RULES.md", "ops/"]}`; an empty list clears it. The answer is the updated Automaton, which carries `authorized_paths` when any are set |
| **MCP** | `autonomous_prd_set_authorized_paths` with `id` and `paths` (the argument must be present; an empty array clears the list) |
| **CLI** | `datawatch autonomous authorize-paths <automaton-id> --path RULES.md --path ops/`, or `--clear` |
| **Comm channel** | `autonomous authorize-paths <automaton-id> <path> [<path>...]`, or `autonomous authorize-paths <automaton-id> clear` |
| **Web UI** | The Automaton's detail page → the **Authorised paths** card: a comma-separated field and **Save authorised paths**; an empty field clears the list |

The list you send **replaces** the current one. Each entry follows the same
three pattern forms. An absolute entry, an entry with a `..` segment, an entry
over 256 bytes and a list over 100 entries are refused with a message naming
the entry. The change is recorded as a `protected_paths_authorised_set`
decision and audited. The first time a task changes a path under the list, the
Automaton records a `protected_paths_authorised` decision, so you can see the
authorisation was used.

Authorising paths does not answer a task that is already held. Answer its
question, or reset the task so the gate runs again.

## Worked example

A project keeps its rules in `RULES.md` and its deployment files in an `ops/`
directory. You want both protected, plus the code owners file.

1. **Point the rules file at `RULES.md` and set the list.**

   ```
   $ datawatch config set autonomous.rules_file RULES.md
   $ datawatch config set autonomous.automation.protected_paths "RULES.md,ops/,CODEOWNERS"
   ```

   The explicit list replaces the defaults, so the supervisor rules and the
   workflow directory are no longer protected here. Add
   `.github/workflows/` if you want it too.

2. **A task goes beyond its job.** An Automaton adds a health endpoint. Task 2,
   *Add the health route*, declared `api/health.go`. The worker also edits
   `ops/deploy.yaml` and loosens a rule in `RULES.md`. Verification passes.

3. **The gate holds it.** It compares the task's directory with the base commit
   and finds `ops/deploy.yaml` and `RULES.md`. The Automaton is authorised for
   neither, so the task is held, and one question is raised.

4. **You read it.**

   ```
   $ datawatch autonomous protected-paths
   Automaton protected paths (action: hold, rules source: base_commit):
     Protected list (3): RULES.md, ops/, CODEOWNERS
     Held (1):
     - Automaton Health (7c1e), task Add the health route (t2)
         paths: RULES.md, ops/deploy.yaml
         action: hold
         reason: the task's commit changed protected paths
         held since: 2031-03-02 10:14:00 UTC
         question: q-44 (answer approve, reject or change the spec)
     Authorised in advance: no Automaton authorises protected paths.
   This is a status read only: answer the question in the question queue to release a hold.
   ```

5. **You decide.** The `ops/deploy.yaml` edit is a fix you wanted. The `RULES.md`
   edit is not. You answer **Reject it**: the daemon puts both paths back to
   their base-commit content in one new commit and runs the task again, with a
   note that those paths must not change. Had you wanted only the deploy edit,
   you would answer **Change the spec** and tighten the task's instructions.

6. **A later Automaton is meant to edit the rules.** For an Automaton whose job
   is to update `RULES.md`, authorise it once, and its tasks pass:

   ```
   $ datawatch autonomous authorize-paths 9d02 --path RULES.md
   Automaton Rules update (9d02) authorises 1 protected path(s): RULES.md
   ```

   The rules review for that Automaton still reads `RULES.md` as it was at the
   base commit, so the rubric it is judged by cannot change under it.

## The daemon's own sections in the rules file

When a session starts, the session tracker writes its own marked sections into
the project rules file by itself — the session guardrails header, the memory,
RTK, cross-session and platform context sections. They are recognised
everywhere as the daemon's work, not the worker's: they never count as a
change in any of the checks above, and they never reach the held paths. What
a hold names is a change outside those sections — a real edit to the rules, by
the worker or by hand — and it is answered exactly like any other protected
path: **Approve this change** if you want it, or authorise the rules file for
the Automaton so its tasks pass.

## Worker progress notes no longer touch the rules file

The rules file is protected by default, and for a while the gate's most
frequent hit was a routine one: a worker appending a progress note to the
Checkpoints section of the rules file (this repository used to hold its
operating notes in `CLAUDE.md`), so almost every task ended in a
protected-path question over a note, not a rule change.

Two things now keep that case from reaching you. The first closes the source.
Every worker prompt that gets its rules and context block also gets a short,
fixed rule (see
[the no-checkpoints rule](automata-rules-and-context.md#the-no-checkpoints-rule)):
the Checkpoints and progress-note sections of `AGENT.md`, `CLAUDE.md` and any
configured rules file are written only by the human operator and by the
supervisor, and a worker that needs a progress note writes it to
`CHECKPOINT.md` at the root of its working directory instead. `CHECKPOINT.md`
is one of the daemon's own marker files, not work: new worktrees ignore it and
the uncommitted-work checks skip it (see
[the completion marker file](automaton-uncommitted-work.md)).

The second covers the rule's one failure mode — a worker that appends its
entry to the rules file anyway. The gate now handles exactly that case itself,
before any question is decided:

### The checkpoint-entry revert

When a hit names one of the rules files — `AGENT.md`, `CLAUDE.md` or your
configured rules file — the gate first asks whether the file's change against
the base commit is **only** a worker checkpoint entry. If so, it strips the
entry out of the file and lets the hit go: no question, no alert, the task
goes on as if the note had never been written. If the change is anything
beyond the entry, the hit is kept untouched and the normal hold / warn /
block action decides, exactly as before.

What counts as an entry the gate may strip:

- the added lines must form checkpoint entry blocks — a `## YYYY-MM-DD`
  heading (optionally with ` HH:MM` and a short tail) running to the next
  non-dated `## ` heading or the end of the file;
- the blank lines that separate the entries;
- at most one added `## Checkpoints` heading, and only when the base file did
  not already carry one.

Nothing else. The base version must be an ordered subsequence of the working
copy — the worker may have added lines, but not changed or removed one — and
a non-dated new section, body text outside an entry block, or a second
`## Checkpoints` heading keeps the hit and puts the file back in the normal
hold / warn / block path. The revert is line-level and deterministic: no model
is involved and no git write is issued. The daemon's own marked sections in
the rules file are never part of what is stripped — an entry written next to
them is still stripped, and the sections are preserved byte for byte.

It fails closed. A base version that cannot be read, a symlink or other
non-regular file, a failed write, or a stripped copy that does not line up
with the base leaves the working copy exactly as found and keeps the hit —
none of these ever ends in a file the gate half-rewrote.

The revert is recorded, once per task and file, as the
`protected_paths_checkpoint_reverted` decision on the Automaton (its note
reads *task <id>: removed a worker checkpoint entry from <path>*), so you can
see in the decision log that the entry was stripped and that the note's
content still lives in `CHECKPOINT.md` and the session record. There is no
new setting behind it: it runs under `hold`, `warn` and `block`, it is
idempotent (a second gate run over an already-stripped file reverts nothing),
and a task that changes any other protected path is still held for that path.

What this changes for you:

- a routine worker progress note no longer reaches the gate — the note lives
  in `CHECKPOINT.md`, which is not on the protected list, and even a note a
  worker appends to the rules file anyway is stripped by the
  [checkpoint-entry revert](#the-checkpoint-entry-revert);
- the Checkpoints sections of `AGENT.md` and `CLAUDE.md` are yours and the
  supervisor's. Workers that add, edit or remove entries there are disobeying
  their prompt, and the daemon writes only what it strips back out again;
- a **real** change to a protected file is still held exactly as before, and
  the rules review still reads the rules from the base commit. A change that
  mixes the worker's entry with any other edit is not stripped — it is held
  like one.

**Still to review (operator):** the checkpoint-entry revert is verified by
its tests (entry-only strips, entry-plus-real-edit holds, fail-closed and
second-run cases), but a live run on a daemon in use — a worker's checkpoint
append to the rules file being stripped, the
`protected_paths_checkpoint_reverted` decision recorded, and no
protected-path question raised — is not yet confirmed. If a task is held on a
rules file after this, read the held paths: the change went beyond a plain
entry, so **Reject it** (the entry is removed with it), **Approve this
change** if you want both, or authorise the rules file for the Automaton as
before.

## How the judge reads the rules

The rules review is opt-in: it runs when `rules-review` is in the Automaton's
guardrail list. With `rules_review_source: base_commit` (the default) the
daemon first finds the **base commit** for the whole Automaton:

- for an Automaton on its own branch, the merge-base of its worktree and the
  Automaton's base branch;
- for a direct-mode Automaton, the commit before its first task started.

It then reads the rules file (`autonomous.rules_file`) and the context file
(`autonomous.context_file`, `CONTEXT.md` when unset) **as they were at that
commit** and gives their text to the judge, in delimited blocks, with a
statement that the working-tree copy may have been changed by the work being
judged. If the working-tree copy differs from the text given, the judge is
told to report the difference as a finding. A file that did not exist at the
base is left out, so deleting the rules file cannot skip the review. A file
longer than 262144 bytes is given in part, and the text says so.

### When the base commit cannot be found

If no base commit can be resolved and verified, or a git read fails, the
rules review does not stop. It falls back, for that run, to reading the rules
files from the working directory as before, and records one
`rules_review_source_fallback` decision on the Automaton with the reason. The
protected-paths gate has its own fallback: when it cannot resolve the base
commit or cannot read the changed files, it records a
`protected_paths_check_skipped` decision, raises a warning alert *Protected
paths check skipped* and lets the task continue, so a check that did not run is
never read as a pass.

The base commit for a task comes from, in order: the commit before its first
attempt, the commit before its latest attempt, then the merge-base of its
directory and the Automaton's base branch. Each must be a real commit and an
ancestor of the current head, or the next one is tried.

## Where to see

| Location | What you see |
|---|---|
| **Web UI** | Automation view → Overview sub-tab → **Automata protected paths** card; a chip on a held task row; a banner on a held Automaton; the **Authorised paths** card on the Automaton detail |
| **REST API** | `GET /api/autonomous/protected-paths` → `action`, `rules_source`, `paths`, `held[]`, `authorized[]` |
| **MCP** | `autonomous_protected_paths` |
| **CLI** | `datawatch autonomous protected-paths` |
| **Comm channel** | `autonomous protected-paths` reply |
| **Audit log** | Events `autonomous_protected_paths_held`, `autonomous_protected_paths_warned`, `autonomous_protected_paths_blocked`, `autonomous_protected_paths_answered` and `autonomous_protected_paths_authorised_set` |
| **Automaton decision log** | Decisions `protected_paths_held`, `protected_paths_warned`, `protected_paths_blocked`, `protected_paths_authorised`, `protected_paths_authorised_set`, `protected_paths_approved`, `protected_paths_rejected`, `protected_paths_spec_change_requested`, `protected_paths_check_skipped`, `protected_paths_checkpoint_reverted` and `rules_review_source_fallback` |
| **Alerts** | Warning alerts *Protected paths held*, *Protected paths warning*, *Protected paths blocked*, *Protected paths check skipped* and *Protected paths not restored* |

## A note on spelling

Prose says **authorised**. Identifiers, JSON fields, route names and command
names say **authorized**: `authorized_paths`, `set_authorized_paths`,
`authorize-paths`, `autonomous_prd_set_authorized_paths`. They name the same
list. When a search for one finds nothing, try the other.

## Known limits

- **Direct mode shares the tree.** In direct mode the task works in the project
  directory itself. A change you make by hand, or one a task running in
  parallel makes, in the same tree during the task can be attributed to it.
  Use worktree mode for an Automaton that must be held to its own changes.
- **Files that were dirty before the task are not attributed.** A file that
  was already modified or untracked when the task started is not counted,
  unless a commit made since the base touched it. An uncommitted edit to a
  protected file that you made before the task is therefore not a hit.
- **The base commit can fall back.** When the base cannot be found, the gate
  skips with a warning and the rules review reads the working tree for that
  run (see *When the base commit cannot be found*). Both are recorded; neither
  is silent.
- **The gate looks at paths, not meaning.** A task that changes a protected
  file in a harmless way is held like one that weakens it. Read the paths and
  decide. The one exception is the
  [checkpoint-entry revert](#the-checkpoint-entry-revert): a worker that defies
  the no-checkpoints rule by appending its entry is not held when the entry is
  the file's entire change against the base — the gate strips it out and
  records `protected_paths_checkpoint_reverted` — and is held when it changed
  anything else in the file.
- **Nothing here is a permission wall.** The gate acts after a worker
  attempt and — apart from the
  [checkpoint-entry revert](#the-checkpoint-entry-revert), which only strips a
  worker's own checkpoint entry out of a rules file when that is the file's
  entire change against the base — never reverts, resets or rewrites by
  itself. A real change to a protected file is put back only when you answer
  Reject it.

## Troubleshooting

| Symptom | Likely cause and fix |
|---|---|
| A task is held and you did not expect it | Read the held paths with `datawatch autonomous protected-paths`. If the path is on the list by mistake, change the list; if the Automaton is meant to edit it, authorise the path for it |
| A hold names the rules file and it looks like a progress note | The change went beyond the worker's checkpoint entry — a plain entry append is now stripped by the gate (`protected_paths_checkpoint_reverted` in the decision log, no hold). For a hold that did fire, **Reject it** to put the file back (the entry is removed with it); the note's content is still in `CHECKPOINT.md` and the session record |
| A path you listed is not protected | An explicit list replaces the defaults, and an entry is matched whole: `docs/plans/*.md` does not match `docs/plans/sub/a.md`, and a `*` never crosses a slash. Use the directory form `docs/plans/` for everything below it |
| Nothing is ever held | The action may be `off` or `warn`; read `action` in the status. Check the Automaton has a task directory, and look for a `protected_paths_check_skipped` decision |
| A task is blocked and no question was raised | The action is `block`. Authorise the path or edit the task, then reset it |
| The question never appears | The operator-question store may be unavailable. The hold still stands and the status shows no question; reset the task once you have dealt with the paths yourself |
| Reject it did not put the paths back | Read the warning alert *Protected paths not restored* and the reason on the hold; a fresh question was raised, so answer again |
| `PUT /api/config` is refused | A word is not allowed or an entry is absolute, has a `..` segment, is over 256 bytes or the list is over 100 entries. The error names it |
| Authorising paths did not release a held task | It never does: answer the question, or reset the task so the gate runs again |
| The rules review ignores an edit to the rules file | That is the base-commit read at work. Use `rules_review_source: worktree` only if you want the working-tree copy |

## What it never does

The one exception is the [checkpoint-entry revert](#the-checkpoint-entry-revert):
a worker's own checkpoint entry, stripped out of a rules file, and only when
it is that file's entire change against the base. Apart from that, nothing
here reverts, resets, amends or rewrites a change by itself, and nothing
here pushes or merges. It never changes the list, the action or an Automaton's
authorised paths on its own. Rejecting a change is the one case where files are
put back, and then only the exact paths in the hold, in a new commit. The
status read never runs git and never changes anything. With the action `off`
it does not look at the tree at all.

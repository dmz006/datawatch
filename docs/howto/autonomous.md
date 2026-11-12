---
docs:
  index: true
  topics: [autonomous, automaton, snapshot, staging, git, commit, write scope, never-stage, skipped paths]
exec_params:
  - {name: patterns, required: false, description: "Never-stage patterns to set (e.g. '.env, node_modules/'); omit to read the current list"}
exec_steps:
  - tool: autonomous_config_get
    description: Read the autonomous settings, including the effective `snapshot_never_stage` list
    args: {}
    read_only: true
  - tool: autonomous_config_set
    description: Set the never-stage patterns for the autonomous snapshot (each surface sends its own form; an explicit empty list clears the list)
    args:
      snapshot_never_stage: "{{params.patterns}}"
    read_only: false
  - tool: autonomous_prd_get
    description: Read an Automaton and its tasks; each task shows `skipped_snapshot_paths` when the staging guard left files unstaged (path + reason per file)
    args:
      id: "{{params.id}}"
    read_only: true
---

# How-to: Autonomous snapshot staging (scoped automatic commits)

When a daemon automaton runs task sessions, the daemon is the only thing
that ever commits that work. Every time it commits, the staging is
**scoped**: the commit is built from a short list of candidate file
paths, checked against a set of protection rules, and staged one path at
a time. This howto covers the three pieces an operator normally needs to
know together:

1. **The scope** — what a staged automatic commit may and may contain
   (task write scope, per-run bounding for unscoped sessions, the
   protection checks).
2. **`autonomous.snapshot_never_stage`** — the setting that keeps whole
   path patterns out of staging (secret files, out-of-scope vendor
   trees, scratch areas), including its built-in defaults and how to set
   it on every surface.
3. **`skipped_snapshot_paths`** — the record on the task result that
   tells you which files the staging pass refused to stage, and why.
   The record is written by the normal completion commit, the
   stall-recovery commit and the attempt snapshot path, and the PWA,
   the CLI and the comm channel all show it.

If a file the task needed shows up in `skipped_snapshot_paths`, the
usual fix is to put that path back in scope for that file (widen the
task's write directories) or adjust the one protection rule that refused
it — not to weaken the guard wholesale.

## Baseline

- A daemon with autonomous automations enabled
  (`autonomous.enabled: true`) running task sessions in a directory that
  is (or is under) a git repository, with git enabled on the session.
- The settings and the task result have the same fields on every
  surface; the tables below use the daemon on `localhost:23456` as
  `$BASE` (e.g. `curl -s $BASE/api/autonomous/config` shows the effective
  `snapshot_never_stage` list).

## What gets staged, and how the scope works

The daemon makes the automatic commits for its task sessions; a worker
session can never commit (a `git commit` run by a worker fails with a
message saying the harness commits after validation). The automatic
commits that stage task work are the **pre-session snapshot** (the tree
state at launch, so the session's work can always be told apart from
what was already there), made for automaton task sessions, and the
**completion commit**, made after the session's validation and the task's
verification have passed.

There is a third, preservation-only commit: the **attempt snapshot**,
taken before a stalled worker is killed (when `autonomous.preserve_attempt_work`
is on), which records the attempt's working state under
`refs/datawatch/attempts/<task>/<attempt>` so it can be reviewed and
restored on retry. It is a ref, not a commit in your branch history, and
it has a different job, so it runs through the common staging guard only:
the per-file size cap, ignored cache/build directories, model-weight
patterns and the file-count refusal, with no whole-tree `git add` — but
without the task write-scope filter and the
`autonomous.snapshot_never_stage` checks.

The two session commits — the ones whose output becomes the task's
committed work — follow this staging discipline:

1. **Candidates are enumerated, never swept.** The commit considers a
   short list of paths: tracked modifications, already-staged changes
   and untracked files — never `git add -A`, never a directory tree.
   An automatic commit that stages nothing is never made.
2. **The task's write scope is the primary bound.** When the automaton
   task has declared write directories, the candidate list is filtered
   to those at or under one of them, and the filter applies to all
   three sources — a tracked file outside the scope that is dirty in
   the tree stays out of the commit. Everything refused this way is
   reported with reason `outside-plan`.
3. **Unscoped sessions are bounded to the run.** A session without a
   write scope stages only the files changed during *this* run, and the
   run's own log names the paths it left out.
4. **Protection checks run before staging.** Each candidate is then
   checked against:
   - the **never-stage patterns** from `autonomous.snapshot_never_stage`
     (reason `never-stage`) — see the next section;
   - **the repository's ignore rules**: a path git reports as ignored is
     never *newly added* (reason `ignored`). A file that is already
     tracked and was modified is the task's own work and is not
     affected — the rule keeps `node_modules/`, build output, caches and
     such from being pulled in.
5. **The staging guard still applies** to whatever survives: per-file
   size cap (reason `size`), ignored cache/build directories and
   model-weight patterns (reasons `ignore-dir`, `pattern`), candidates
   outside the repository (reason `outside-root`), a capped file count
   (an over-sized commit is refused as a whole and nothing is staged),
   and a time-bounded `git add`. See the *Automatic commit staging
   guard* section of `docs/implementation.md` for the full guard rules.
6. **The skip is reported, not papered over.** Every refused file is
   logged with its reason (the first 50 are shown, the rest counted as
   "and N more skipped") with a per-reason summary (for example
   `ignored=1, never-stage=2`), and the pass is recorded on the task
   result — the normal completion commit, the stall-recovery commit and
   the attempt snapshot path all set the record. See
   `skipped_snapshot_paths` below.

In short: a file reaches an automatic commit only if it is within the
task's declared scope (or the run's own changes), it is not in your
never-stage list, git does not report it as ignored for a new add, and
the staging guard accepts it.

## `autonomous.snapshot_never_stage` — patterns the snapshot must never stage

`autonomous.snapshot_never_stage` is a list of path patterns the
automatic commits must never `git add`. It is the operator's tool for
files that must never enter a snapshot even though they are inside a
task's write directory: secret files, out-of-scope vendor trees, scratch
areas with heavy churn.

### The built-in defaults

When the key is **unset** (absent from the config), the built-in
default list applies:

| Pattern | Meaning |
|---|---|
| `scratch/` | the `scratch` directory at the repository root, and everything under it |
| `tmp/` | the `tmp` directory at the repository root, and everything under it |
| `temp/` | the `temp` directory at the repository root, and everything under it |
| `.tmp/` | the `.tmp` directory at the repository root, and everything under it |
| `data/` | the `data` directory at the repository root, and everything under it |
| `.data/` | the `.data` directory at the repository root, and everything under it |
| `*.tmp` | any file named `*.tmp`, at any depth (a file-name glob) |

### Pattern syntax

- Patterns are **root-anchored**: they are matched against the
  candidate's **repo-relative** path, starting at the repository root —
  not at any component halfway down the path. A pattern names a
  location *at the root*; it does not "float" to wherever the same
  directory happens to sit deeper in the tree.
- An entry with a **trailing `/`** (e.g. `vendor/`, `node_modules/`)
  names a root directory: `node_modules/` refuses `node_modules` itself
  and `node_modules/anything` — but *not* `lib/node_modules` or
  `pkg/vendor/node_modules`. Protect those by listing them explicitly
  (`lib/node_modules/`, `pkg/vendor/node_modules/`).
- An entry **without a trailing `/`** is matched as a shell-style glob
  against the relative path, and also against the candidate's file name
  alone. A **slash-free** glob therefore catches the name at *any*
  depth: `*.tmp` refuses any `*.tmp` file wherever it sits. A glob with
  a slash is root-anchored: `secrets/key.*` refuses
  `secrets/key.<anything>` at the repository root only.
- An exact entry (no `/` glob characters, no trailing slash) names that
  exact relative path — or a root-level directory and everything under
  it: `secrets/credentials.yaml` refuses exactly that path, not
  `vendor/secrets/credentials.yaml` deeper down.
- Absolute patterns never match (and are inert). Use repo-relative
  patterns, anchored at the repository root.

### Unset vs. explicit empty — and validation

- **Unset** → the built-in defaults above apply.
- **Explicit empty list** (`[]`) → the list is empty: **nothing is
  protected**. This is a distinct state, and it round-trips: saving the
  effective config back on any surface does not silently re-default an
  explicit empty.
- **Blank entries** (empty strings, whitespace-only) are **rejected** on
  load and on every write surface — the daemon refuses the write
  ("patterns must be non-empty") rather than silently dropping them.

### Setting it — all surfaces

The key accepts either a JSON array or a comma-separated string (the
comm channel sends CSV).

| Surface | How |
|---|---|
| **YAML config** | `autonomous: snapshot_never_stage: [".env", "keys/", "out/"]` in `~/.datawatch/config.yaml`; daemon must be restarted to pick up YAML changes |
| **CLI** | `datawatch config set autonomous.snapshot_never_stage '.env,keys/,out/'` (write; it is a `config set <key> <value>` command that goes through the running daemon), read back with `datawatch config get autonomous.snapshot_never_stage` |
| **REST** | `PUT /api/config` with body `{"autonomous.snapshot_never_stage": [".env","keys/"]}` (or the comma-separated form); or the list-only `autonomous` surface: `PUT /api/autonomous/config` with `{"snapshot_never_stage": [".env","keys/"]}` — on that surface `null` means no change (the patch merges onto the current settings), not "reset to unset"; read with `GET` on the same endpoints, which always render the **effective** list (defaults when unset) |
| **MCP** | `autonomous_config_set` with `snapshot_never_stage: [".env", "keys/"]`; read with `autonomous_config_get` |
| **Chat / comm** | `configure autonomous.snapshot_never_stage=.env,keys/,out/` |
| **PWA** | Settings → Autonomous — "patterns the snapshot must never stage (comma-separated)" field |

All write surfaces validate: a blank pattern or a non-list value is
refused with a `400` (or the surface's error reply) and nothing is
changed.

## `skipped_snapshot_paths` — the skip record on the task result

When a staging pass refused one or more candidates, the task result
carries it. The record is written by the normal completion commit, the
stall-recovery commit and the attempt snapshot path — and the PWA, the
CLI and the comm channel all show it alongside MCP and REST. Each
automaton task in the `autonomous_prd_get` /
`/api/autonomous/prds/<id>` record shows, when it has a last staging
pass with skips:

```json
{
  "id": "t-3",
  "status": "completed",
  "files": ["internal/fetch/client.go", "internal/fetch/client_test.go"],
  "skipped_snapshot_paths": [
    { "path": "internal/fetch/.env.local", "reason": "never-stage" },
    { "path": "scratch/notes", "reason": "never-stage" },
    { "path": "vendor/dep/main.c", "reason": "outside-plan" }
  ]
}
```

- **`reason`** is one of: `never-stage` (your
  `autonomous.snapshot_never_stage` list), `ignored` (the repository's
  ignore rules), `outside-plan` (outside the task's declared write
  scope), plus the staging guard's reasons `size`, `ignore-dir`,
  `pattern`, `outside-root` when a guard rule did the refusing.
- **Semantics:** it is *advisory evidence* — the list names the files
  that were left unstaged and why, so you can decide whether they matter
  (a skipped scratch file is normal; a skipped source file is a signal
  the scope is wrong). It is **not** an action: the files are left
  untouched in the working tree, and a commit with fewer files is still
  a valid commit. A given path appears either in the staged/committed
  files or in this record — never in both.
- **Cap and ordering:** at most 50 entries are kept (oldest dropped), and
  when the same task ran more than one staging pass the **last pass
  wins** — the record always describes the most recent pass.
- **Read it** with `autonomous_prd_get` (MCP), `GET /api/autonomous/prds/<id>` (REST), and `datawatch autonomous prd-get <id>` (CLI) — all three return the same task record. The PWA shows the record on the task, and the comm channel renders an "left unstaged" line per task that has skips. The daemon log also has one line per skipped file with the reason.

## Troubleshooting

**A file the task needed never reached the commit.** Read the task's
`skipped_snapshot_paths`. `outside-plan` → the path is outside the
task's write scope: widen the task's write directories. `ignored` / `size`
/ `pattern` → the repository's ignore rules, the per-file size cap or a
model-weight pattern refused it: fix the ignore rule, the file size or
the name. `never-stage` → it is in your `snapshot_never_stage` list;
remove or narrow that pattern only if it was not meant to protect that
path (remember patterns are root-anchored: `out/` protects the `out`
directory at the repository root and its contents — a nested `out/`
needs its own entry, e.g. `build/out/`).

**The guard refused the whole commit** (`auto-commit refused: …` in the
daemon log, the task failing or stalling on the commit): too many files
were staged at once — narrow the task's write scope rather than raising
the guard's limits. See `docs/operations.md` → *Auto-commit refused*.

**You changed the setting but staging still refuses a path.** Confirm
what is actually in force with `autonomous_config_get` (or
`GET /api/config`): an unset key runs the built-in defaults, an explicit
empty list protects nothing, and YAML changes need a restart while the
runtime surfaces apply immediately to the next commit.

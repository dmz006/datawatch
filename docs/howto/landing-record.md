---
docs:
  index: true
  topics: [autonomous, automation, automaton, landing, work branch, merge, keep, drop, pending, landed, archive, default branch, migration, worktree]
exec_params: []
exec_steps:
  - tool: autonomous_prd_get
    description: Read one Automaton — its landing field is the landing record (state, commit, actor, at, reason)
    args: {id: ""}
    read_only: true
  - tool: autonomous_landing_keep
    description: Record that the Automaton's work branch is kept unmerged on purpose (needs a reason)
    args: {id: "", reason: ""}
    read_only: false
  - tool: autonomous_landing_drop
    description: Record that the Automaton's work branch and its work will not be merged (needs a reason)
    args: {id: "", reason: ""}
    read_only: false
  - tool: autonomous_prd_merge_selection
    description: Read which branches datawatch may merge and why each other one is left out
    args: {id: ""}
    read_only: true
  - tool: autonomous_prd_accept_for_merge
    description: Accept one exact commit of an Automaton's branch for merging (needs the full commit id and a reason)
    args: {id: "", commit: "", reason: ""}
    read_only: false
---
# The landing record

An Automaton that runs in worktree mode does its work on its own branch
(`automaton/<id>`). Some of that work will never be merged — it is superseded,
kept for reference, or abandoned. The **landing record** is where the Automaton
records that: its own account of whether its work branch's work reached the
project's default branch, who decided what, when, and why.

The record is the field `landing` on the Automaton, with five parts: `state`,
`commit`, `actor`, `at`, `reason`. It is its own thing — separate from the
[Unlanded work review](automaton-worktree-cleanup.md) (which
lists archived *branches* by their git state) and separate from the
[safe-to-delete report](automaton-worktree-cleanup.md). Here the record is kept
on the Automaton, one per Automaton, and archive is refused
while it is still `pending` (see [below](#what-the-record-gates-archive)).

The definition lives with the other Automaton terms in
[datawatch-definitions](../datawatch-definitions.md).

## The four states

| State | Set by |
|---|---|
| `pending` | the daemon, when the Automaton's work branch is created: the actor is `autonomous`, the reason is the fixed note "branch created", and there is no merge commit yet. This is the default: an Automaton that carries a work branch starts `pending`. |
| `landed` | the daemon, after a merge run's push step, once the remote's default branch is confirmed to contain the merge commit (see [What gets recorded](#what-gets-recorded-after-a-push)). It names the merge commit in `commit`. |
| `kept` | you — an operator decision with an actor and a non-empty reason. Use it when the branch is kept on purpose and will not be merged. |
| `dropped` | you — an operator decision with an actor and a non-empty reason. Use it when the work is abandoned and will not be merged. |

`pending` to `landed` is the daemon's business; `pending` to `kept` or
`dropped` is yours. Every state carries the actor who set it, the time, and
the reason (a `landed` record's reason is the fixed note "merged on the
default branch").

A `pending` landing is also treated as `landed` when the Automaton's branch
tip is already an ancestor of the default branch (git's
`merge-base --is-ancestor`) and, when the repository has a remote, the merge
commit is on the remote's copy of that branch too. A merge that exists only on
the local default branch has not landed. It does not matter how the merge
happened. The next archive check finds it, passes, and updates the record to
`landed` with the merge commit.

## What datawatch may merge

One rule picks the work datawatch may merge. The supervisor loop's merge job,
a merge run request, the shadow merge plan, the selection read and the
finished-work findings all use it. A branch is **selected** when either:

- the Automaton is `completed` and its completion review **passed on exactly
  the commit the branch points at**; or
- the operator or the supervisor **accepted that exact commit**, with a
  reason. This covers a review that is pending or timed out, a branch that
  moved past the reviewed commit, and an Automaton that is `needs_review`.

A branch is **never** selected, accepted or not, when the Automaton is failed,
blocked, cancelled, archived or rejected, or its review is blocked. A block is
kept for each check on each unit (the completion review, each story, each
task): it stays until the **same check passes on that same unit**. A pass
anywhere else does not hide it, and a later re-judge that only timed out does
not remove it. An acceptance does not lift it.

These also leave a branch out, and each answer lists every reason that
applies: the work is not finished; an operator hold or pause (on the
Automaton, its project or everything); uncommitted work held in its worktree;
a code task that is not completed, or no finished task other than gate tasks
(a finished task counts whatever its title says; an open task that is a gate,
or whose title reads as documentation, does not hold the branch back); a
landing already
`landed`, `kept` or `dropped`; the answer "I will land it" (accepting the
commit afterwards hands it back); a shipped record; a closed outcome; a branch
that is not in the merge repository; a task branch with commits that are not on
the Automaton branch.

**An acceptance names one commit.** It must be the full commit id of the
branch's tip. When the branch moves, the acceptance no longer covers it. A
merge run holds the branch to the selected commit: if it moves during the run,
the run stops. The pipeline's own rebase step rewrites the branch, so a run
that stops after a rebase leaves a new commit that needs a new review or a new
acceptance.

The acceptance records the actor, the way keep and drop do, writes a
`merge_accept` decision and a `merge_accept` audit entry (refusals too). It
does not prove who the actor is. A caller the daemon knows to be a session or a
federation peer is recorded as that, whatever it names. A session that is doing
Automaton work cannot accept anything.

| Surface | See what is selected | Accept a commit |
|---|---|---|
| PWA | **May datawatch merge it?** under the landing block | **Accept commit … for merge**, shown with the reasons; it asks why |
| REST | `GET /api/autonomous/merge?selection=true`, or `?automaton=<id>` | `POST /api/autonomous/prds/{id}/landing/accept`, body `{"actor": "<who>", "commit": "<full id>", "reason": "<why>"}` |
| CLI | `datawatch autonomous merge-selection [<id>]` | `datawatch autonomous prd-landing-accept <id> --commit <full id> --reason "<why>" [--actor <who>]` |
| MCP | `autonomous_prd_merge_selection` | `autonomous_prd_accept_for_merge` with `id`, `commit`, `reason` |
| chat / comm | `autonomous landing selection [<id>]` | `autonomous landing accept <id> <full commit id> <reason words…> [actor=<who>]` |

From a federated peer, the read needs the autonomous read capability and the
acceptance the autonomous write capability. Nothing here merges or pushes;
merge runs stay off until
`autonomous.merge_deploy_run_enabled` is on.

A merge run request names exactly one Automaton branch. A refused request
still answers with a recorded run whose result is `failed` and whose reason
says why; nothing is merged. A branch that no Automaton owns is refused.

## What gets recorded after a push

After a merge run's push step, the daemon asks the remote whether its default
branch contains the merge commit, and checks that the commit contains the
Automaton's branch. Only then does it write, in one step: landing `landed`,
status `completed` (also from `needs_review`), and the shipped record (commit,
release version, source `merge_step`), with a `merge_landed` decision. The run
shows this as the step `record-landing`. A push that was held, skipped or
failed, or a remote that cannot confirm it, leaves everything as it was.

A shipped record you set by hand with source `merge_step`, for a branch in the
merge repository that has a remote, is refused unless the remote contains the
commit; when it does, the landing becomes `landed` too. It never changes the
Automaton's status.

**A `backfill` record is stored as given, with no check**, and `backfill` is
what the CLI, MCP and chat send unless you choose `merge_step`. So such a
record can mark work that was never pushed as shipped. The landing stays
`pending`, and the selection rule then leaves the work out as already shipped.
Record a shipped record only after the push, or choose `merge_step` to have the
daemon check it.

## Reading the record

The record rides on **every Automaton read**, so you never fetch it
separately — you read the Automaton:

- **PWA** — Automata tab → open the Automaton's detail page → **Overview**
  sub-tab: the landing block shows a colored state badge plus the commit,
  actor, time and reason rows when they are filled in.
- **REST** — `GET /api/autonomous/prds/{id}`: the `landing` field.
- **CLI** — `datawatch autonomous prd-get <id>`: the record prints as a
  single landing line.
- **MCP** — `autonomous_prd_get`: the same `landing` field in the read.
- **chat / comm** — `autonomous landing <id>`: just the landing line, or a
  note that the Automaton has no landing record.

The line format is shared by the CLI and the chat channel:

```
landing: <state>[ @ <commit>][ by <actor>][ (YYYY-MM-DD HH:MM)][ — reason]
```

for example

```
landing: pending by autonomous (2026-11-09 07:00) — branch created
landing: landed @ 3f9c1a2 by operator (2026-11-08 18:40) — merged on the default branch
landing: dropped by operator (2026-11-07 11:05) — superseded by a later pass
```

An Automaton with **no** landing record — no work branch at all, or an old
record that was never migrated (see below) — simply has no `landing` field;
the read, the landing line and the PWA block all say so instead of guessing.

## Keep and drop

Both decisions move a `pending` record: **keep** says the branch is kept on
purpose and its work will not be merged; **drop** says the work is abandoned.
They change the record only — they never touch the branch, the worktree or
the status.

The rules every surface applies the same way:

- An **actor** is required. On REST and the CLI it defaults to `operator`
  when you omit it; the MCP actor argument and the comm `actor=<who>` token
  are optional with the same default; the PWA record has no actor field —
  the default applies.
- A **non-empty reason** is required; each surface refuses it without naming
  the requirement.
- An Automaton with **no branch** is refused — there is nothing to keep or drop.
- A record that is already `landed` is refused — a landed landing is never
  rewritten.
- The record keeps the branch tip (`tip`, a commit id) at the moment of the
  decision, read from the base repository. The unlanded work list leaves a
  kept or dropped branch out only while the branch still points at that tip;
  once new commits land on it, it is listed again with the reason "new commits
  after the decision". When the tip could not be read (no base repository),
  the decision is recorded without a tip and keeps hiding the branch.
- A record that is already `kept` can be re-decided with `dropped`, and vice
  versa: the record takes the newest decision, and each decision writes one
  more entry on the record's decision log (`landing_keep` or `landing_drop`,
  naming the branch, the new state and the reason), so the history of the
  calls stays visible.

Keep, on every surface:

| Surface | Command / action |
|---|---|
| PWA | the **Keep (branch not merged)** button in the landing block (shown while the state is `pending`): it asks for the reason and records the decision |
| REST | `POST /api/autonomous/prds/{id}/landing/keep`, body `{"actor": "<who>", "reason": "<why>"}` — needs the autonomous write capability in federation; the reply is the updated Automaton |
| CLI | `datawatch autonomous prd-landing-keep <id> --reason "<why>" [--actor <who>]` (`--actor` defaults to `operator`) |
| MCP | `autonomous_landing_keep` with `id` and `reason` (`actor` optional); the reply is the updated Automaton |
| chat / comm | `autonomous landing keep <id> <reason words…> [actor=<who>]` — and the other order, `autonomous landing <id> keep <reason words…>`, works too |

Drop is the same five rows with **Drop (work not merged)** /
`landing/drop` / `prd-landing-drop` / `autonomous_landing_drop` /
`autonomous landing drop <id> <reason words…>`.

A REST keep, end to end:

```
$ curl -s -X POST http://127.0.0.1:8443/api/autonomous/prds/ab12cd34/landing/keep \
    -H 'content-type: application/json' \
    -d '{"reason": "the fix landed separately in a later pass"}'
{ "landing": { "state": "kept", "actor": "operator", "at": "2026-11-09T08:12:04Z", "reason": "the fix landed separately in a later pass" }, … }
```

Each settled decision is also written as an audit entry
(`landing_keep` / `landing_drop` on the Automaton), and the reply, the
landing line and the PWA block all show the record with the new state.

## What the record gates: archive

Archiving the Automaton — by hand on any surface, or the daemon's own close
of a finished record — is **refused while its landing is still `pending`**,
with the branch named:

> `automaton "ab12cd34" cannot be archived: its branch automaton/ab12cd34 has not landed on the default branch; merge it, or record keep or drop for its landing`

Before it refuses, the check asks git whether the Automaton's branch tip is an
ancestor of the default branch (`merge-base --is-ancestor`). If it is, the
branch has landed — whether the merge came from the pipeline, the shipped
record call or a hand merge — so the archive check passes and the record is
updated to `landed`, naming the merge commit. A branch that still has commits
that are not in the default branch stays not landed, and archive is still
refused.

Once the record is `landed`, `kept` or `dropped` the archive goes through as
before. An Automaton with no landing record at all archives as it always
did. The point of the gate: no unlanded work branch can silently disappear
behind an archived record — the record is exactly the account that keeps it
in view.

## The one-time migration, and its count

Automata that already existed when the record was introduced have no record
yet: they archive as they always did. To bring them into the scheme,
the daemon runs a **one-time landing migration** (the daemon API
`MigrateLanding`). It scans every archived Automaton and, for each one that
**has a work branch, no landing record yet, and whose branch read-only git
cannot prove was merged on the default branch**, writes a `pending` record —
actor `landing_migration`, a reason naming the proof it found.

It deliberately skips, and reports, rather than guesses:

- records that already carry a landing record;
- records with no branch;
- records that are not archived;
- branches whose merge the git evidence proves to be on the default branch;
- branches with no commit ahead of the default branch (a fast-forward or
  merge-commit landing puts the branch tip on the default branch, so
  nothing is left to land);
- any Automaton whose default branch cannot be established.

Two properties make it safe to run: it is **idempotent** (a second run finds
nothing to mark) and it **changes no status and writes no decision** — it
only adds the record, so an Automaton it marks is the one that will now
refuse archive until you merge the branch or record keep or drop.

The migration's **count** is the number of records it wrote: it returns the
count and reports it in its log line

```
landing migration: marked 3 archived Automata pending (branch not on the default branch)
```

with the ones it refused to classify (default branch unknown) named. A count
of zero on a run means everything the migration looked at already carried a
record or a provable merge — run it again and it will still say zero.

**When it runs.** The daemon runs the migration once at start, in the
background, so the read-only git checks never hold up startup. When the
count is above zero it also raises one operator alert, *Archived Automata
with unlanded branches*, naming the count — read it in the PWA's alerts, on
`GET /api/alerts`, or through your messaging channel. After a pass with no
errors it writes a done marker, `landing_migration.done`, in the autonomous
data directory (`~/.datawatch/autonomous/` by default), and later starts
skip the migration. A pass that hit an error (for example an Automaton whose
default branch is unknown) writes no marker, so the next start tries again.
To run it again on purpose, stop the daemon, remove the marker and start it.

There is no operator command for it — no REST route, CLI command, MCP tool,
chat verb or PWA button; the daemon start is its only caller.

## Related

- [Automaton worktree cleanup](automaton-worktree-cleanup.md) — the
  *The landing record* section, and the Unlanded work review the record
  works next to.
- [Post-ship hooks](post-ship-hooks.md) — the `mark_completed` hook that
  records `landed`.
- [Automaton uncommitted-work check](automaton-uncommitted-work.md) — the
  other guard on the worktree.

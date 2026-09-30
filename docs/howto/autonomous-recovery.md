---
docs:
  index: true
  topics: [autonomous, prd, recovery, dependencies]
exec_params:
  - {name: prd_id, required: true, description: "Automaton ID (8-char hex)"}
  - {name: task_id, required: false, description: "Task ID to reset/cancel, when acting on a single task"}
exec_steps:
  - tool: autonomous_prd_get
    description: Read the Automaton record — status, per-story/per-task status, DependsOn
    args: {id: "{{params.prd_id}}"}
    read_only: true
  - tool: autonomous_prd_repair_depends_on
    description: Resolve any DependsOn entries still stuck as raw titles instead of real IDs
    args: {id: "{{params.prd_id}}"}
    read_only: false
  - tool: autonomous_prd_reset_task
    description: Reset a failed/blocked task to pending, or force-requeue a completed/cancelled one
    args: {id: "{{params.prd_id}}", task_id: "{{params.task_id}}", force: true}
    read_only: false
---
# How-to: Recover a stuck, failed, or incorrectly-terminal Automaton

[`autonomous-planning.md`](autonomous-planning.md) covers the normal
spawn → plan → run → complete loop. This howto covers the unhappy paths:
a task fails or gets stuck, a story gets cancelled and never restarted,
or an Automaton reaches `completed` while a story inside it is still
`cancelled`. It also covers `repair_depends_on`, a one-time fix for
Automata whose stories/tasks were created before v8.36.5.

## What it is

Three operator-facing repair tools, all scoped to a single story or task
so you never have to discard a partially-completed run:

- **`reset_task`** — puts one task back to pending so the run picks it
  up again. Without `force`, only `failed`/`blocked` tasks qualify. With
  `force: true`, it also requeues a `completed` or `cancelled` task —
  and, as of v8.36.6, this works even when the *Automaton* itself has
  already reached `completed`, and correctly reopens the task's story if
  that story was `cancelled` (previously only `completed`/`failed`
  stories reopened — a cancelled one had no recovery path at all).
- **`cancel_story`** / **`cancel_task`** — stop an individual story or
  task without cancelling the whole Automaton. The most common cause of
  the "completed Automaton with a cancelled story inside it" scenario
  below is cancelling a story mid-run to fix something, then forgetting
  to `reset_task` it back — the run then proceeds around the gap.
- **`repair_depends_on`** — resolves `depends_on` entries against real
  story/task IDs. Decompose can only reference a sibling by its title
  (IDs don't exist until stories are saved), and versions before v8.36.5
  never translated those titles to IDs afterward — so on an Automaton
  planned before that fix, `depends_on` still holds raw titles and was
  never actually enforced. Safe to call on any Automaton; a no-op if
  everything already resolves.

## Base requirements

- `autonomous.enabled: true`
- An Automaton that has been decomposed (has stories/tasks) for
  `reset_task`/`cancel_story`/`cancel_task`; `repair_depends_on` works
  even pre-decompose (no-op).

## Recognizing the "completed but a dependency was skipped" scenario

Symptoms in `autonomous_prd_get`:

- Automaton `status: completed`
- One story shows `status: cancelled` with all its tasks `cancelled`
- A sibling story shows `status: completed`, and — before v8.36.5 — its
  tasks' `depends_on` *titles* actually named tasks in the cancelled
  story. Those tasks ran anyway, without their stated prerequisites.

## Fixing it

`reset_task`, `cancel_story`, `cancel_task`, and `repair_depends_on` are
REST + MCP only — there's no `datawatch autonomous` CLI subcommand for
any of them yet (the CLI has `prd-get` and whole-Automaton `prd-cancel`,
but nothing per-story/per-task). Use `datawatch autonomous prd-get <id>`
to confirm the diagnosis, then the REST or MCP calls below to fix it:

1. Confirm the diagnosis — look for a cancelled story next to a
   completed one that depends on it (`prd-get`, or the REST/MCP reads
   below).
2. If this Automaton predates v8.36.5, resolve `depends_on` titles to
   real IDs first, so dependency ordering is actually enforced on the
   retry below (no-op if already resolved).
3. Force-requeue the cancelled story's tasks, in dependency order
   (oldest task ID first is a safe proxy for creation order if you'd
   rather not read the `depends_on` graph by hand).
4. Once the skipped story's tasks complete for real, force-requeue any
   downstream tasks that ran without them, so they redo their work
   using the now-real prerequisite output.

Each `reset_task` call restores the Automaton to `running` and reopens
its story to `pending` automatically; the daemon's executor picks the
newly-pending task back up without any separate "resume" step.

### REST

```bash
curl -sk -X POST https://HOST:PORT/api/autonomous/prds/<prd_id>/repair_depends_on \
  -H 'Content-Type: application/json' -d '{"actor":"operator"}'

curl -sk -X POST https://HOST:PORT/api/autonomous/prds/<prd_id>/reset_task \
  -H 'Content-Type: application/json' \
  -d '{"task_id":"<task_id>","actor":"operator","force":true}'
```

### MCP

```
autonomous_prd_repair_depends_on(id="<prd_id>")
autonomous_prd_reset_task(id="<prd_id>", task_id="<task_id>", force=true)
autonomous_prd_cancel_story(id="<prd_id>", story_id="<story_id>", reason="...")
autonomous_prd_cancel_task(id="<prd_id>", task_id="<task_id>", reason="...")
```

## Common pitfalls

- **Cancelling a story mid-run and not restarting it.** The rest of the
  Automaton keeps going — a sibling story that `depends_on` the cancelled
  one will still run against missing prerequisites unless you also fix
  `depends_on` (see above) or manually sequence the retries.
- **Forgetting the downstream redo.** Fixing the skipped story alone
  isn't enough — anything that already ran off its absence produced
  output without the real prerequisite content and needs `force`-reset
  too.
- **Reject vs Cancel vs reset_task.** Reject = the Automaton shouldn't
  exist. Cancel (story/task) = stop this piece; the Automaton is fine.
  `reset_task force=true` = bring a stopped or already-finished piece
  back to pending for a genuine retry.

## See also

- [howto/autonomous-planning](autonomous-planning.md)
- [howto/autonomous-review-approve](autonomous-review-approve.md)
- [howto/automata-orchestrator](automata-orchestrator.md)
- [datawatch-definitions](../datawatch-definitions.md)

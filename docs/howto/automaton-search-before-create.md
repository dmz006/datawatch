---
docs:
  index: true
  topics: [automaton, overlap warning, search before create, duplicate, shadow mode, threshold, audit, create]
exec_steps:
  - tool: autonomous_overlap_search
    description: Search for existing Automata, registry rows and unmerged branches that already cover a plan
    args:
      title: "{{params.title}}"
      spec: "{{params.spec}}"
    read_only: true
  - tool: autonomous_config_get
    description: Read the autonomous settings, including the create search mode and threshold
    args: {}
    read_only: true
---

# Search before create

When you create an Automaton, the daemon can first check whether the same work
is already covered somewhere. This is the **create-time check**. If it finds a
match it produces an **overlap warning**. A warning is advice. It never blocks
the create, and it never changes anything it points at.

## Base requirements

- A running daemon with `autonomous.enabled: true`.
- For the registry and branch checks, a project directory that is a git
  repository with a plans registry at `docs/plans/README.md`. When the
  Automaton names no project directory, the daemon uses the configured
  worktree base repository.

## What an overlap warning is

An overlap warning says: "something that already exists looks like the plan you
are creating." It names the thing and gives a score, so you can go and read it
before you start new work.

The check compares the new title and spec with three places:

1. every **Automaton** in the store, whatever its status (draft, running,
   finished, failed, cancelled);
2. the **registry rows** in `docs/plans/README.md`;
3. the **unmerged Automaton branches**, meaning local branches under
   `automaton/` whose work has not reached `main`. Each branch is compared by
   its name and its commit subjects.

The comparison counts shared words. It lower-cases the text, drops short and
very common words, and then divides the number of shared words by the size of
the shorter text. A pair only counts as a match when it shares **at least
three** words and its score reaches the threshold. No model is called, and
nothing is written to any of the three places.

## How to read each kind of warning

Every warning has a `kind`, a `score` from 0 to 1, up to five `shared_terms`,
a one-sentence `reason`, and a `read_hint` that tells you how to look at the
thing.

| Kind | What it means | What to do |
|---|---|---|
| `automaton` | An Automaton already in the store describes this plan. The warning gives its `id` and its `status`. | Read it with `datawatch autonomous get <id>`. If it is finished and did the work, you may not need a new one. If it failed or was cancelled, decide whether to pick that one up again instead of starting over. If it is still running or waiting, a second one would duplicate it. |
| `registry` | A row in the plans registry already covers this. The warning gives the row number. | Open `docs/plans/README.md` at that row and read the plan it points to. |
| `branch` | An unmerged Automaton branch already carries this work. The warning gives the branch name. | Run `git log main..<branch>` to see what it holds. The work may be waiting for review or a merge. |
| `unavailable` | One of the three places could not be read this time, so it was **not checked**. There is no score. | Treat the check as incomplete. The create still succeeded. Fix the cause (for example a missing registry file or a repository that will not open) and run the search again. |

How to judge the score:

- A score near **1.0** means the shorter text is almost entirely inside the
  other. That is very likely a real duplicate.
- A score just above the threshold means the texts share a lot of vocabulary.
  That is often a related plan, and sometimes only a plan in the same area.
  Read it before you decide.
- A high score does not prove a duplicate, and a missing warning does not prove
  there is none. The check counts words. It does not understand them.

The warning list is sorted best match first.

## The three modes

The mode is `autonomous.automation.create_search_mode`. Its default is
`shadow`.

| Mode | What happens when you create an Automaton |
|---|---|
| `off` | The check does not run. The create reply is exactly what it was before this feature. |
| `shadow` (default) | The check runs and records what it found in the audit log. **The create reply does not change** and no one is shown a warning. |
| `on` | The check runs and the warnings are added to the create reply as an `overlap_warnings` list (empty when there are none). The create still succeeds. |

In every mode the check runs after the Automaton is stored, so a failure in the
check can never stop a create. If the search fails or crashes, the daemon logs
one line and replies as if the mode were `off`.

The read-only overlap search (see the access table below) works in **every**
mode, including `off`. It reports the mode in force next to the warnings, so
you can tell whether a create would have shown them.

Changing the mode takes effect on the next create. No restart is needed.

### What shadow mode records

In shadow mode, each create writes one entry to the operator audit log:

| Field | Value |
|---|---|
| `action` | `automaton_create_search` |
| `subject` | the id of the Automaton that was just created |
| `actor` | who made the create |
| `details.mode` | `shadow` |
| `details.warnings` | the warnings the check **would have** shown, or an empty list when it found none |

An entry with an empty `warnings` list is still written. That is how you know
the check ran and found nothing, as opposed to not running at all.

### How to compare shadow mode with what the supervisor did

Shadow mode exists so you can test the matching rules on real work before the
warnings are shown to anyone. Run it for a day or two, then:

1. List the shadow entries:

   ```bash
   datawatch audit --action automaton_create_search --limit 200
   ```

2. Pick the entries whose `details.warnings` is not empty. For each one, note
   the new Automaton (the `subject`) and the Automaton, row or branch the
   warning named.
3. Look at what the supervisor actually did with that new Automaton. Did they
   cancel it as a duplicate? Merge it into the older one? Run it as it was?
   Check with `datawatch autonomous get <id>` for both Automata.
4. Sort each case into one of these:
   - **Right warning**: the supervisor treated the new Automaton as a duplicate
     or as closely related. The check agreed with them.
   - **Noise**: the supervisor ran the new Automaton as separate work. The
     warning would have been a false alarm.
   - **Missed**: the supervisor cancelled or merged a duplicate and the
     entry has no matching warning. The check missed it.
5. If there is too much noise, raise the threshold. If there are too many
   misses, lower it. Then repeat for another day. When the noise and the misses
   are both low enough for you, switch the mode to `on`.

Also read the `unavailable` warnings in the shadow entries. A source that is
often unavailable gives a comparison you cannot trust.

## The threshold

The threshold is `autonomous.automation.create_search_threshold`. It is a
number above 0 and at most 1. An unset key reads **0.5**.

- A **higher** threshold means fewer warnings and only the closest matches.
  Use it when you see noise.
- A **lower** threshold means more warnings and looser matches. Use it when the
  check is missing duplicates.
- A value of zero, a negative value, a value above 1 or a value that is not a
  number is refused and nothing is written.
- Whatever the threshold, a match still needs at least three shared words.

The threshold is read on every create and every search, so a change applies at
once.

## Where to read the warnings

Every way to ask is the same read. Each one reports the warnings, the
threshold in force and the mode in force.

| Access method | How |
|---|---|
| YAML config key | Set the behaviour in the config file under `autonomous.automation`: `create_search_mode` (`off`, `shadow` or `on`) and `create_search_threshold` (above 0, at most 1). The warnings themselves are not stored in the config. |
| CLI command | `datawatch autonomous overlap --title "<title>" --spec "<spec>"`. Either one alone is enough, and the spec may be given as the argument. Add `--json` for the raw reply. Shadow entries: `datawatch audit --action automaton_create_search`. |
| Web UI path | After you create an Automaton with the mode set to `on`, a read-only **Possible overlap with existing work** block appears at the bottom of the page. Each line shows the kind, what it points to and the score; the one control closes the block. Set the mode and threshold under **Settings → General → Autonomous**. Shadow entries are in the **Observer** tab, **Audit log** card. |
| REST endpoint | `GET /api/autonomous/overlap?title=<title>&spec=<spec>&limit=<n>` (see the example below). The create route, `POST /api/autonomous/prds`, also returns `overlap_warnings` when the mode is `on`. |
| Comm verb | `autonomous overlap <text>`. The text is used as the spec. The reply has one line per warning. |

The read is also available to agents as the MCP tool `autonomous_overlap_search`
with the same `title`, `spec` and `limit` arguments.

### REST example

```bash
curl -s -G "https://localhost:8443/api/autonomous/overlap" \
  --data-urlencode "title=Add search before create" \
  --data-urlencode "spec=Warn when a new Automaton overlaps an existing one" \
  --data-urlencode "limit=5"
```

The reply:

```json
{
  "warnings": [
    {
      "kind": "automaton",
      "id": "3fa9c2d1",
      "status": "completed",
      "score": 0.72,
      "shared_terms": ["automaton", "existing", "overlaps", "warn"],
      "reason": "Automaton 3fa9c2d1 already describes this plan.",
      "read_hint": "datawatch autonomous get 3fa9c2d1"
    }
  ],
  "threshold": 0.5,
  "mode": "shadow"
}
```

Rules for the read:

- Give a `title`, a `spec`, or both. Both blank is a 400.
- `limit` is a whole number from 1 to 50. The default is 10. Anything else is a
  400.
- It writes nothing, and it needs only read access.
- If the search itself fails, the reply is a 500. It is never an empty list
  that looks like "no overlaps".

The CLI and the comm verb print one line per warning, for example:

```
automaton 3fa9c2d1 status completed score 0.72: Automaton 3fa9c2d1 already describes this plan.
registry row 41 score 0.61: An existing Automaton plan is already on the roadmap (row 41).
branch automaton/ab12cd34 score 0.55: An unmerged Automaton branch already carries this work (automaton/ab12cd34).
```

When nothing matches they print `No overlaps found.`

## What it never does

- It never blocks, delays or refuses a create.
- It never edits, cancels or merges any Automaton, registry row or branch.
- It never sends an alert or a notification.
- It does not read the meaning of a plan. It counts shared words, so a rewrite
  in different words can slip past it.

## See also

- [Search before create data flow](../flow/automaton-create-overlap-flow.md)
- [Operations: the create-time check and shadow mode](../operations.md)
- [Lane fill](automation-lane-fill.md), which uses the same shadow-first idea

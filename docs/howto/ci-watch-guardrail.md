---
docs:
  index: true
  topics: [ci, guardrails, autonomous, automata]
exec_params:
  - {name: automaton_id, required: true, description: "Automaton ID to turn ci-watch on for and read the CI result of"}
  - {name: completion_guardrails_json, required: false, default: '["ci-watch"]', description: "JSON array of Automaton-completion guardrails to set, e.g. [\"rules-review\",\"ci-watch\"]"}
  - {name: guardrail_profile, required: false, default: "", description: "Guardrail profile to keep assigned (the set call replaces the profile and every list, so pass the current one if the Automaton has one)"}
exec_steps:
  - tool: guardrail_library_list
    description: Confirm ci-watch is registered in the guardrail library
    args: {}
  - tool: autonomous_config_get
    description: Read the global per_prd_guardrails list and ci_watch_timeout_seconds
    args: {}
  - tool: per_automaton_guardrails_set
    description: Turn ci-watch on for this one Automaton (explicit completion list beats profile and global config)
    args: {id: "{{params.automaton_id}}", guardrail_profile: "{{params.guardrail_profile}}", per_prd_guardrails_json: "{{params.completion_guardrails_json}}"}
  - tool: autonomous_prd_get
    description: Read the Automaton, including its ci_watch result once it has completed
    args: {id: "{{params.automaton_id}}"}
---
# How-to: CI Watch Guardrail

`ci-watch` is an opt-in completion guardrail. When an Automaton
finishes and the commit it produced has already been pushed to
GitHub, ci-watch waits for that commit's GitHub Actions runs before
the Automaton is called done. If CI fails, an LLM judge reads your
project's own rules file and the real failure logs and decides what
happens next.

## What it does and why

Without ci-watch, "completed" means the local work finished: the
tasks ran, verification passed, and any other completion checks were
happy with the local diff. None of that tells you whether the build
on the remote agreed. An Automaton whose last task pushes a release
commit can show **completed** while the CI run for that exact commit
is still running, and nothing notices when that run fails a few
minutes later.

With ci-watch on, completion also means "the CI for what this
Automaton pushed is green", or else you get a clear record of why it
isn't:

- **All runs green**: the Automaton completes as normal.
- **Runs still going at the timeout**: the Automaton completes anyway
  (slow CI never holds it hostage), marked `pending`, and one
  follow-up check later records how CI ended.
- **A run failed**: the judge reads your rules/context file (for
  example `AGENT.md`) and the failure logs, then picks one of these:
  - **An operator must decide.** Your rules say this kind of failure
    needs a human, for example "dependency or vulnerability findings
    go to the operator". The Automaton is **Blocked** and nothing is
    fixed automatically.
  - **A genuine, fixable problem in this Automaton's own work.** A
    corrective task is added to the **same** Automaton (when content
    auto-fix is enabled, which it is by default). When that task
    finishes and pushes, ci-watch watches the new commit's CI.
  - **Acceptable or unrelated.** The judge may pass or warn instead
    of blocking. Nothing is invented.
- **CI machinery broke** (runs cancelled, timed out, or never
  started): the failed runs are re-run automatically within the
  Automaton's `infra` retry budget (see
  [retry-policy.md](retry-policy.md)).

ci-watch never pushes anything and doesn't care how the commit got to
the remote: a task's own `git push`, a cluster push, or a release
step all count the same.

### When it skips (not an error)

ci-watch records `skipped` and lets the Automaton complete normally
when there's nothing it can watch:

- Nothing was pushed: the final commit is not on any remote branch.
- The `origin` remote is not GitHub. Only GitHub Actions is watched.
- The `gh` CLI is not installed on the daemon host (it must be on
  `$PATH` and authenticated for the repository).
- The project directory is missing or is not a git repository.

**Worktree / auto-PR Automata skip today.** Those Automata push their
branch and open the PR *after* completion, so at the moment ci-watch
runs, their final commit hasn't been pushed yet and it records a
skip. Review their CI as the PR's own checks on the pull request.

## Base requirements

- A running daemon with the autonomous (Automata) loop enabled.
- `gh` installed on the daemon host and authenticated (`gh auth
  status`) with read access to Actions for the repo, plus permission
  to re-run workflows if you want infra re-runs to work.
- A guardrail judge configured: the same LLM used for verification
  and `rules-review` (`autonomous.verification_backend` /
  per-Automaton guardrail backend). With no judge configured, a
  failed run is recorded as `error` and does not block.
- Optional but strongly recommended: a rules/context file in the
  project root (`AGENT.md`, `CLAUDE.md`, ...) that says how CI
  failures should be handled. The judge works without one, but then
  it can only judge each failure on its own merits.

## Turning it on

The completion guardrail list is resolved in the same priority order
as every other guardrail list ([guardrail-library.md](guardrail-library.md)):

1. **The Automaton's own completion list** (`per_prd_guardrails` on
   the Automaton)
2. **Its guardrail profile** (`guardrail_profile`)
3. **Global config** (`autonomous.per_prd_guardrails`), empty by
   default

`ci-watch` sits alongside `rules-review`. Both are off unless listed,
and you can list either one or both.

> **How profiles apply.** A guardrail profile has one list, and it's
> used at **every** level (task, story and completion) where the
> Automaton doesn't have its own list. So a profile containing
> `ci-watch` turns ci-watch on at completion as intended, but it also
> sends each task and story to the guardrail judge under that name
> (one extra LLM review per unit). If you only want the completion
> behaviour, use the Automaton's own completion list (`per_prd_guardrails`,
> next section) or the global list, or give the Automaton its own
> task and story lists so the profile isn't used at those levels.

### For one Automaton

> The per-Automaton set call **replaces** the profile and all three
> lists (task, story, completion) in one go. Pass whatever the
> Automaton already has (read it first with `autonomous_prd_get`), or
> those settings will be cleared.

MCP:

```json
{
  "tool": "per_automaton_guardrails_set",
  "args": {
    "id": "a1b2c3d4",
    "per_prd_guardrails_json": "[\"rules-review\",\"ci-watch\"]"
  }
}
```

REST:

```sh
BASE=https://localhost:8443
curl -sk -X PUT -H "Content-Type: application/json" \
  -d '{"per_prd_guardrails":["rules-review","ci-watch"]}' \
  $BASE/api/autonomous/prds/a1b2c3d4/guardrails
```

CLI:

```sh
datawatch guardrail automaton set a1b2c3d4 --per-prd rules-review,ci-watch
```

Comm channel: the per-Automaton command accepts a profile but has no
completion-list flag, so use a profile that contains `ci-watch` (next
section):

```
You: guardrail automaton set a1b2c3d4 profile=release
Bot: {"id":"a1b2c3d4","guardrail_profile":"release",...}
```

YAML: per-Automaton settings live in the Automaton record, not in
`config.yaml`. Use any surface above.

PWA: the Automaton detail view shows the CI watch result (below).
Per-Automaton guardrail lists are set from the CLI, REST, MCP or a
profile.

### For a guardrail profile

Make a profile once, then assign it to every Automaton that ships
something. It's the only per-Automaton route from a comm channel.
Read the profile note above first: the profile's list also applies
at task and story level.

MCP:

```json
{
  "tool": "guardrail_profile_create",
  "args": {
    "name": "release",
    "description": "Release Automata: review the diff, then wait for CI",
    "guardrails_json": "[\"rules-review\",\"ci-watch\"]"
  }
}
```

then:

```json
{ "tool": "per_automaton_guardrails_set", "args": { "id": "a1b2c3d4", "guardrail_profile": "release" } }
```

REST:

```sh
curl -sk -X POST -H "Content-Type: application/json" \
  -d '{"name":"release","description":"Release Automata: review the diff, then wait for CI","guardrails":["rules-review","ci-watch"]}' \
  $BASE/api/autonomous/guardrail_profiles

curl -sk -X PUT -H "Content-Type: application/json" \
  -d '{"guardrail_profile":"release"}' \
  $BASE/api/autonomous/prds/a1b2c3d4/guardrails
```

CLI:

```sh
datawatch guardrail profile create --name release \
  --guardrails rules-review,ci-watch
datawatch guardrail automaton set a1b2c3d4 --profile release
```

Comm channel:

```
You: guardrail profile create release rules-review,ci-watch
Bot: {"id":"p9z8","name":"release","guardrails":["rules-review","ci-watch"]}

You: guardrail automaton set a1b2c3d4 profile=release
Bot: {"id":"a1b2c3d4","guardrail_profile":"release",...}
```

PWA: Settings → **Automate** → **Guardrail Profiles** → **New
Profile**, guardrails `rules-review, ci-watch`.

### Globally (every Automaton)

MCP:

```json
{ "tool": "autonomous_config_set", "args": { "per_prd_guardrails": ["rules-review", "ci-watch"] } }
```

REST:

```sh
curl -sk -X PUT -H "Content-Type: application/json" \
  -d '{"key":"autonomous.per_prd_guardrails","value":["rules-review","ci-watch"]}' \
  $BASE/api/config
```

CLI:

```sh
datawatch config set autonomous.per_prd_guardrails rules-review,ci-watch
```

Comm channel:

```
You: configure autonomous.per_prd_guardrails=rules-review,ci-watch
```

PWA: Settings → **Automate** → **Automaton completion guardrails**,
enter `rules-review, ci-watch`.

YAML (`~/.datawatch/config.yaml`):

```yaml
autonomous:
  per_prd_guardrails:
    - rules-review
    - ci-watch
```

Turning it on globally is safe for Automata that never push: they
just record `skipped`.

## Tuning the wait: `ci_watch_timeout_seconds`

How long ci-watch waits for the pushed commit's runs to finish.
Default **1500** (25 minutes); `0` means "use the default". It's a
daemon-wide setting.

| Surface | Example |
|---|---|
| MCP | `autonomous_config_set` `{"ci_watch_timeout_seconds": 2700}` |
| REST | `PUT /api/config` `{"key":"autonomous.ci_watch_timeout_seconds","value":2700}` |
| CLI | `datawatch config set autonomous.ci_watch_timeout_seconds 2700` |
| Comm channel | `configure autonomous.ci_watch_timeout_seconds=2700` |
| PWA | Settings → **Automate** → **CI watch timeout** |
| YAML | `autonomous:` → `ci_watch_timeout_seconds: 2700` |

Set it a little above how long your slowest workflow normally takes
for a release commit. If it's too short, Automata complete as
`pending` and only the single follow-up check sees the result.

## Reading the result

Once ci-watch has run, `autonomous_prd_get` (REST `GET
/api/autonomous/prds/<id>`) includes a `ci_watch` object. Every
outcome is also written to the Automaton's decision timeline (kind
`ci_watch`) with the run URLs, and the PWA Automaton detail view shows
a **CI watch** card with the status, the commit, and a link to each
run.

| `status` | Meaning |
|---|---|
| `skipped` | Nothing to watch; `summary` says why |
| `pass` | Every run for the pushed commit succeeded |
| `warn` | A run failed, but the judge flagged it as non-blocking |
| `pending` | Runs were still going when the timeout elapsed; CI is unconfirmed |
| `failed_policy` | A run failed and your rules say an operator decides. Automaton is **Blocked** |
| `failed_content` | A run failed with a fixable finding. A corrective task was added (or, when auto-fix is off or out of budget, the Automaton is **Blocked**) |
| `failed_infra` | Runs were cancelled, timed out or never started, and the infra retry budget is spent. Automaton is **Blocked** |
| `error` | A run failed but no judge was configured; not blocking |

### Pass

```json
"ci_watch": {
  "status": "pass",
  "sha": "4e1f9c2a7b",
  "repo": "acme/widgets",
  "runs": [
    {"id": 11850231, "name": "CI", "status": "completed", "conclusion": "success",
     "url": "https://github.com/acme/widgets/actions/runs/11850231"}
  ],
  "summary": "all 1 CI run(s) passed",
  "checked_at": "2026-10-13T14:02:11Z"
}
```

**Next:** nothing. The Automaton is completed.

### Failed, operator decides (`failed_policy`)

```json
"ci_watch": {
  "status": "failed_policy",
  "sha": "9b07d5e1c3",
  "repo": "acme/widgets",
  "runs": [
    {"id": 11850977, "name": "Release", "status": "completed", "conclusion": "failure",
     "url": "https://github.com/acme/widgets/actions/runs/11850977"}
  ],
  "summary": "Container scan found a HIGH vulnerability in the base image; AGENT.md says dependency/vulnerability findings are operator decisions.",
  "category": "policy",
  "issues": [
    "Trivy: CVE-2026-41234 (HIGH) in libexpat 2.6.2 from the base image; fixed in 2.6.4",
    "Recommendation: bump the base image to the patched tag, or record an accepted risk with a justification if no fixed image is available yet"
  ],
  "checked_at": "2026-10-13T14:31:40Z"
}
```

The Automaton's status is **blocked**.

**Next:** read `issues` and open the run URL, then make the call
yourself. See the worked example below for how to resume.

### Failed, auto-fix in progress (`failed_content`)

```json
"ci_watch": {
  "status": "failed_content",
  "sha": "c2d4e6f8a0",
  "repo": "acme/widgets",
  "runs": [
    {"id": 11851102, "name": "CI", "status": "completed", "conclusion": "failure",
     "url": "https://github.com/acme/widgets/actions/runs/11851102"}
  ],
  "summary": "Unit test TestParseConfig fails: the new 'timeout' field is not defaulted.",
  "issues": [
    "internal/config/parse_test.go:88 — expected Timeout=30, got 0",
    "Default Timeout in applyDefaults() alongside the other new fields"
  ],
  "checked_at": "2026-10-13T15:05:02Z"
}
```

The Automaton has a new corrective story and task built from
`issues`, and it keeps running. It skips per-story approval when
`auto_fix_skip_approval` is on (the default).

**Next:** usually nothing. When the corrective task pushes its fix,
the completion checks run again and ci-watch watches the new commit's
CI. If content auto-fix is turned off or its retry budget is used up,
the same status comes with a **Blocked** Automaton instead. Fix it
yourself, or raise the budget ([retry-policy.md](retry-policy.md)) and
resume as below.

### Still running at the timeout (`pending`)

```json
"ci_watch": {
  "status": "pending",
  "sha": "71aa03be5d",
  "repo": "acme/widgets",
  "runs": [
    {"id": 11851340, "name": "Release", "status": "in_progress",
     "url": "https://github.com/acme/widgets/actions/runs/11851340"}
  ],
  "summary": "CI for 71aa03be5d had not finished after 25m0s (1 run(s) seen); completing without waiting — one follow-up check in 25m0s",
  "category": "infra",
  "issues": ["category=infra source=ci-watch retryable=false: CI for 71aa03be5d had not finished ..."],
  "checked_at": "2026-10-13T16:40:00Z"
}
```

The Automaton is **completed**, but CI is unconfirmed.

**Next:** check again after the follow-up. The follow-up rewrites
`ci_watch` once, to `pass`, `failed_content`, `failed_infra`, or
`pending` again if CI still hasn't finished. It can't reopen a
completed Automaton, so a failure seen there is recorded (with a
learning, see `autonomous_learnings`) for you to act on. If this
happens often, raise `ci_watch_timeout_seconds`.

## Worked example: a vulnerability scan fails after a release tag

Setup: the project's `AGENT.md` contains a rule like

> CVE / dependency-vulnerability findings in CI are operator decisions:
> never auto-upgrade or suppress them; report the finding and a
> recommendation and stop.

The Automaton's job is to cut release `v2.4.0`. Its guardrail profile
is `release` (`rules-review`, `ci-watch`).

1. The last task bumps the version, commits, tags `v2.4.0`, and runs
   `git push --follow-tags`. The tag push starts the `Release`
   workflow for that commit.
2. All tasks finish and `rules-review` passes the diff. Then ci-watch
   sees the final commit on `origin/main`, finds the repo is
   `acme/widgets` on GitHub, and waits for that commit's runs.
3. `CI` passes, but `Release` fails in its container vulnerability
   scan step.
4. The judge is handed the failed run list, pointed at `AGENT.md`, and
   told to fetch the real logs (`gh run view <id> --log-failed`). It
   finds a HIGH CVE in the base image, reads the rule above, and
   returns a `block` with `"category":"policy"` plus its
   recommendation.
5. Because it's a policy block, nothing is auto-fixed. The Automaton
   goes to **blocked** with `ci_watch.status: "failed_policy"`, the run
   URL in `runs[]` and in the `ci_watch` decision, and the judge's
   finding and recommendation in `issues` (see the sample above). A
   learning is recorded so later Automata on this project can recall
   it.
6. You decide. For example:
   - **Upgrade**: bump the base image to the patched tag.
   - **Accept the risk**: no fixed image exists yet, so record an
     accepted-risk entry with a justification and a review date, in
     whatever form your project uses (for example a scanner ignore
     file).
7. Resume. A Blocked Automaton doesn't accept new stories or tasks,
   so the usual path is to put your decision into the release task
   and run it again:

   ```
   autonomous_prd_get        {"id":"a1b2c3d4"}                       → find the release task's id
   autonomous_prd_reset_task {"id":"a1b2c3d4","task_id":"t-rel","force":true}
   autonomous_prd_run        {"id":"a1b2c3d4"}
   ```

   (REST: `POST /api/autonomous/prds/<id>/reset_task` with
   `{"task_id":"t-rel","force":true}`, then `POST
   /api/autonomous/prds/<id>/run`.) Before running it, land your
   decision so the re-run picks it up: commit the base-image bump or
   accepted-risk entry yourself, or put it in the task's spec.
   `force: true` re-runs a completed task, which puts the Automaton
   back in the running state.
   Make sure the release task copes with being run twice (for example,
   it shouldn't re-create an existing tag). When it pushes again,
   ci-watch watches the **new** commit's CI.

   If you finished the release by hand instead, leave the Automaton
   blocked as the record of what happened, or cancel it
   (`autonomous_prd_cancel`).

## Using with templates

Templates (`autonomous_template_create` / `autonomous_template_instantiate`,
and Automata saved as templates with `autonomous_prd_clone_to_template` /
`autonomous_prd_instantiate`) carry the spec only: title, spec, type,
tags and variables. They don't carry guardrail settings. So pair the
instantiated Automaton with a guardrail profile that contains
`ci-watch`:

```json
{ "tool": "autonomous_template_instantiate",
  "args": { "id": "tmpl-release", "project_dir": "/srv/acme/widgets",
            "vars_json": "{\"version\":\"v2.4.0\"}" } }
```

→ returns the new Automaton's `id` (say `a1b2c3d4`), then:

```json
{ "tool": "per_automaton_guardrails_set",
  "args": { "id": "a1b2c3d4", "guardrail_profile": "release" } }
```

Do this before the Automaton completes; ci-watch only fires at
completion, so any time before you approve and run it works. Because
a profile also applies at task and story level (see "How profiles
apply" above), you can set only the completion list as the second
call instead:

```json
{ "tool": "per_automaton_guardrails_set",
  "args": { "id": "a1b2c3d4", "per_prd_guardrails_json": "[\"rules-review\",\"ci-watch\"]" } }
```

Or turn `ci-watch` on globally (above) so every instantiated
Automaton gets it without a second call.

## See also

- [guardrail-library.md](guardrail-library.md): guardrail resolution, profiles, `rules-review`
- [retry-policy.md](retry-policy.md): `infra` / `content` / `policy` categories, budgets, content auto-fix
- [autonomous-recovery.md](autonomous-recovery.md): resetting and re-running tasks on a Blocked Automaton
- [autonomous-planning.md](autonomous-planning.md): Automaton lifecycle
- [mcp.md](../mcp.md): MCP tool reference

# Project automation policy — datawatch-app parity issue

> Filed on `dmz006/datawatch-app` on merge of the PRD. Status: **draft — do not post until the PRD merges.**

**What changed in datawatch:** a new per-project tier sits between the daemon defaults and each Automaton's own settings. The daemon already resolves every
Automaton setting through that tier, and the new surfaces expose the tier as a first-class object:

- **Stored policy** on each project. `GET /api/projects` entries now carry an optional
  `policy` object (absent or null = the project sets nothing and inherits the daemon's values
  unchanged), and `POST /api/projects` accepts the same object, validated per key, to set or
  clear it. The keys are the plan checks (`plan_coverage_check` off/warn/block,
  `plan_coverage_min_ratio` in `(0,1]`, `plan_review_mode` off/warn/block, `scope_lint`
  off/warn/enforce), retry & escalation (`retry_policy` map of `infra`/`content`/`scope`/`policy`
  to whole attempt counts `>= 0`, `retry_ceiling_total`, `auto_fix_retries`,
  `infra_retry_escalate_after`, `failure_escalation` tier ladder), git defaults
  (`default_auto_pr`, `parallel_read_only_tasks`, `parallel_worktree_per_task`), lane limits
  (`max_concurrent_tasks` `>= 1`, `plan_tier_max_inflight` per plan tier, `lane_fill_gpu_lanes`
  / `lane_fill_claude_lanes` with `0` = no preference) and the uncommitted-work pair
  (`uncommitted_work_policy` hold/ask/discard, `uncommitted_work_scope` commit/worktree) plus
  `burn_guard_ask_on_change` (confirmation question vs silent write for burn-guard changes to
  this project's tasks).
- **Effective policy.** `GET /api/projects/{name}/policy/effective` returns one object for every
  setting — `{value, source}` where `source` is `automaton`, `project` or `daemon`, i.e. the tier
  that supplied the value after the daemon resolved the three tiers. `?automaton=<id>` adds the
  top tier to the resolution (it reads that Automaton's existing `policy_overrides`, it does not
  write them). The response envelope:

```json
{
  "project": "core-api",
  "automaton": "acme-orchestrator",
  "policy": {
    "plan_coverage_check": { "value": "warn", "source": "project" },
    "plan_coverage_min_ratio": { "value": 0.85, "source": "project" },
    "plan_review_mode": { "value": "off", "source": "daemon" },
    "max_concurrent_tasks": { "value": 4, "source": "daemon" },
    "burn_guard_ask_on_change": { "value": false, "source": "daemon" }
  }
}
```

  The app should render the rows with the `source` tier, not re-resolve them — the daemon is the
  single resolver, and the row's badge is data the daemon already sent.

Everything else the feature ships on — the CLI verbs `datawatch projects policy show <name>
[--effective] [--automaton <id>]`, `set <name> <key> <value>` and `unset <name> <key>`, the MCP
tools `project_upsert` (with the `policy` argument) and `project_policy_effective` (name, optional
automaton), the `project policy <name> [automaton]` comm verb, and the PWA Settings → Automata →
**Project policy** panel — reads and writes the same objects over the same endpoints, so the
app-side work is purely to render and edit the policy, with no new daemon API.

**Where it is documented:**
- [datawatch howto/project-automation-policy.md](../../howto/project-automation-policy.md)
- [datawatch datawatch-definitions.md § Project automation policy](../../datawatch-definitions.md#project-automation-policy)
- [datawatch config-reference.yaml `projects.` block](../../config-reference.yaml)
- [datawatch operations.md § Project automation policy](../../operations.md)
- [datawatch api-mcp-mapping.md](../../api-mcp-mapping.md) (the `GET /api/projects`, `POST /api/projects`, `project_policy_effective` rows)
- [datawatch parity-status.md](../../parity-status.md) "Parity surface — project automation policy"

**Acceptance (the issue's checklist for the app):** the app reads the stored policy on
`GET /api/projects` entries and renders it on each project view; reads
`GET /api/projects/{name}/policy/effective` (with an optional automaton) and renders the settings
with their value and source tier per setting; and offers set / clear of a key through
`POST /api/projects` with the `policy` object (send the whole block — the upsert replaces the
object), with bad values surfacing the daemon's per-key refusal reason rather than half-writing.
No new alerts or queue questions ride on this feature: the existing alert and question surfaces
are unchanged.

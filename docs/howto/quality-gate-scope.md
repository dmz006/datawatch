---
docs:
  index: true
  topics: [autonomous, quality-gate, testing]
exec_steps:
  - tool: autonomous_config_get
    description: Read the global gate settings, including default_quality_gates.scope and smoke_packages
    args: {}
    read_only: true
  - tool: autonomous_prd_get
    description: Read one Automaton's quality_gates override (scope, smoke_packages)
    args: {id: "prd_demo"}
    read_only: true
  - tool: autonomous_status
    description: List the Automata and their tasks so you can open one and read its quality_gate_result.scope_plan
    args: {}
    read_only: true
  - tool: audit_query
    description: List recent gate decisions for the Automaton
    args: {action: "gate_*", limit: 20}
    read_only: true
---

# Quality gate scope: run only the tests that can break

An Automaton's quality gate re-runs its test command after each task and
compares the failing test names against a baseline. That command is often
the full suite, and every task pays the full-suite cost. The **gate scope**
steers how much the gate runs on ordinary tasks without changing what a
regression looks like when it does show up.

Set it per Automaton (`quality_gates.scope` + `quality_gates.smoke_packages`)
or once for every Automaton (`autonomous.default_quality_gates.scope` +
`autonomous.default_quality_gates.smoke_packages`). Unset scope reads as the
built-in default, `full`, so the feature is opt-in and nothing changes until
you choose a scoped value.

## What the three scopes do

| Scope | What a gate task runs |
|---|---|
| `full` (default) | Exactly the configured test command, unchanged. |
| `touched-plus-smoke` | The touched packages, plus the one-level importers of each, plus your smoke set. |
| `touched` | The touched packages, plus the one-level importers of each. No smoke set. |

- **Touched packages.** Every package that has a file the task changed.
- **One-level importers.** Packages that import a touched package directly.
  Importers of importers are *not* walked — the extra level is cheap and a
  second level grows fast.
- **Smoke set.** Your list of package patterns that always run under
  `touched-plus-smoke`, because they touch shared surfaces (config, server,
  MCP, observers). When you have not set one, a built-in list applies:
  `./internal/autonomous/...`, `./internal/config`, `./internal/pipeline`,
  `./internal/server/...`, `./internal/mcp/...`, `./internal/observer/...`,
  `./internal/audit`, `./internal/metrics`.

The gate rewrites a `go test` command of the form `go test <flags> ./...`
into `go test <flags> <the package list>`. Compound or non-Go commands
cannot be rewritten, and the task then runs the full command instead.

## Two guarantees you should know

- **The final gate task always runs the full suite.** The last non-terminal
  task of an Automaton (or any task tagged as the final gate) runs the
  configured command unchanged, whatever the scope is. Scoping trims the
  per-task runs; the run that closes the Automaton does not get trimmed.
- **Failures that cannot be attributed are not reported as regressions.**
  The baseline and the current run are both restricted to the same package
  set, so a pre-existing failure in a package that did not run does not
  appear as a new failure. `go vet` runs over the same package set and its
  new findings count as regressions; a `go vet` finding that was already
  there does not. A run that cannot be started, times out, names no tests,
  or otherwise cannot be classified behaves exactly as before: the gate is
  recorded invalid and it only warns.

**When you are in doubt, stay on `full`.** A scoped run that misses a
breakage is worse than a slow full run, so anything the gate cannot compute
safely — a file outside the repository, a changed module-level file, no
packages found, or the mapping itself failing — falls back to the full
suite for that run, and the reason is recorded on the task.

## Override order

```
per-Automaton quality_gates.scope       (set on the Automaton)
        ↓ if unset
autonomous.default_quality_gates.scope  (global default)
        ↓ if unset
full                                      (built-in default)
```

The same order applies to `smoke_packages`, with the built-in list above
standing in for an unset global list. Which level won is recorded on the
task (see below), so you can always tell where a scoped run came from.

## Setting the scope, per surface

Global default (`autonomous.default_quality_gates.scope`):

| Surface | How |
|---|---|
| YAML | `autonomous.default_quality_gates.scope: touched-plus-smoke` and, if you want a different smoke set, `autonomous.default_quality_gates.smoke_packages: ["internal/...", ...]` in `~/.datawatch/config.yaml` |
| REST | `PUT /api/config` with `{"autonomous.default_quality_gates": {"scope": "touched-plus-smoke", "smoke_packages": ["./internal/pipeline"]}}`; read it back from `GET /api/config` |
| MCP | `autonomous_config_set` with `quality_gates_scope` and `quality_gates_smoke_packages` (comma-separated string or JSON array); `autonomous_config_get` returns `default_quality_gates` with both keys |
| CLI | `datawatch autonomous config-set '{"default_quality_gates":{"scope":"touched-plus-smoke","smoke_packages":["internal/pipeline"]}}'`; `config-get` shows both keys |
| Comm | `configure autonomous.default_quality_gates.scope=touched-plus-smoke` and `configure autonomous.default_quality_gates.smoke_packages=a/b,c/d` (comma list); `configure help` / `list` name both keys |
| PWA | Settings → General → the autonomous quality-gate card: a **Quality gate scope** selector (empty = inherit the built-in default `full`) and a **Smoke packages** box (comma-separated, saved as a list) |

Per-Automaton override (`quality_gates.scope` + `quality_gates.smoke_packages`):

| Surface | How |
|---|---|
| REST | `POST /api/autonomous/prds/{id}/set_quality_gates` with `{"enabled": true, "test_command": "...", "scope": "touched", "smoke_packages": ["./cmd/..."]}` — invalid scopes are a 400; `GET /api/autonomous/prds/{id}` returns the `quality_gates` override |
| MCP | `autonomous_prd_set_quality_gates` with `scope` and `smoke_packages` (or `quality_gates_smoke_packages`); `autonomous_prd_get` returns the override |
| Comm | `autonomous set-quality-gates <id> [scope=full|touched-plus-smoke|touched] [smoke_packages=a,b]` (see `autonomous help`) |

An empty scope on an Automaton inherits the global default, and an empty
smoke list inherits the global list, then the built-in list.

## Reading which run a gate task made (`scope_plan`)

Every gate run — scoped or full — records its decision on the task in
`quality_gate_result.scope_plan` (present on `GET /api/autonomous/prds/{id}`
tasks and in the task JSON everywhere tasks are returned). The PWA renders
it under each task as a collapsed "Quality gate scope" block; the locale
keys are `prd_task_gate_scope` and `prd_task_gate_scope_full`.

```jsonc
"quality_gate_result": {
  "status": "pass",
  "scope_plan": {
    "scope": "touched-plus-smoke",
    "scope_source": "automaton",           // automaton | global | default
    "full": false,
    "packages": ["./internal/pipeline", "./internal/config", "…"],
    "reasons": [
      {"package": "./internal/pipeline", "reason": "changed files"},
      {"package": "./internal/config",   "reason": "smoke set"}
    ],
    "changed_files_total": 14,
    "warnings": []
  }
}
```

A full-suite plan instead shows `"full": true` with a `full_reason` (for
example `final gate task` or a fallback explanation plus
`fallback_reason`). The log line for a gate run carries the same suffix, for example
`scope touched-plus-smoke: 3 package(s) (1 touched, 1 importers, 1 smoke)`
or, for a full run, `scope full: full test command run (final gate task)`.

## The built-in smoke list, and confirming yours

The built-in list above is the default smoke set for this repository; it is
the proposed list in `docs/plans/2026-10-23-quality-gate-scope.md`. It is
deliberately conservative (core packages only) because a smoke package that
is expensive to build slows every scoped run.

- **Confirm or replace it** before turning the scope on for an Automaton:
  read the plan doc, check the list against your project, then set
  `autonomous.default_quality_gates.smoke_packages` (or per Automaton) to
  the list you want. Keep the scope at `full` until the smoke list is the
  one you would defend.
- **In the datawatch source tree** the default stays `full` — the scoped
  scopes exist, are fully surfaced and tested, but nothing is scoped by
  default until the operator confirms the smoke list.
- **On another project** pick the packages that hold your shared surfaces
  (config, server, protocol, core pipeline) and leave the rest to the
  touched-package mapping.

## Troubleshooting

- **The gate ran the full suite but I set a scoped scope.** Read the task's
  `scope_plan` — `full_reason` / `fallback_reason` say why: the task was
  the final gate, a module-level file changed, no packages mapped, the
  test command cannot be scoped, or the package mapping failed. All of
  these are safe fallbacks, not errors.
- **A regression I expected is not reported.** Check that the package with
  the new failure was in `packages` (or is an importer of one). If it was
  not run, it is deliberately not attributed as a regression.
- **`go vet` complains but tests pass.** New vet findings on the packages
  that ran count as regressions (a compile-level break the tests never
  reach); pre-existing vet findings do not.
- **The scope value was rejected.** Only `full`, `touched-plus-smoke` and
  `touched` are accepted, everywhere (REST is a 400, MCP and comm an error
  reply, a hand-edited YAML rejected on load). An unrecognized stored value
  resolves to `full`, never to a scoped run.

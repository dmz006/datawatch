# Weekly dependency update

- **Date:** 2026-11-11
- **Scope:** one weekly datawatch Automation that updates every dependency together (Go modules, container base image tags, GitHub Actions pins), the Dependabot configuration limited to security updates, the report in Events and the alert rule, and the three settings with their surfaces. Operator how-to: [docs/howto/automation-dependency-update.md](../howto/automation-dependency-update.md).

## Design record

Dependabot used to open one version-update pull request per package. Each one rebased on every push to main and started the same CI jobs, so a burst of them used up the shared API rate limit and overlapped with each other and with the operator's own work. One weekly Automaton replaces them: one branch, one change, one set of checks, with each major update isolated in its own task so a single bad dependency can be dropped without blocking the rest.

Decision, quoted: "security updates only plus weekly automation". Dependabot stays on for security updates so each advisory still gets its own reviewable pull request; every version update comes from the weekly run.

Edits to workflow files raise the protected paths question and wait for the answer; the run never authorises those paths in advance.

## Phases

1. **Classifier and outdated check** — Done. A script lists outdated dependencies in the three places and classifies each as security, major, minor or patch.
2. **Built-in Automation template** — Done. Find, update minor and patch together, one task per major (capped by `max_majors`), fix breakage, full checks, write results.
3. **Change description** — Done. Summary table, "Needs your attention" section, test result per dependency, dropped updates.
4. **Settings and schedule** — Done. `enabled`, `schedule` (default `monday 05:00`) and `max_majors` (default 5, 0 to 20), validated on every write path.
5. **Dependabot security-only** — Done. `open-pull-requests-limit: 0` on every ecosystem, held by a lint check.
6. **Events row and alert** — Done. One `check` row per run; an alert only when the run needs the operator.
7. **Status tool and comm configure** — Done. `dependency_update_status` and the chat `configure` keys.
8. **Documentation** — Done. How-to, definitions entry, how-to index and this plan.
9. **Mobile parity** — Planned. Tracked in the datawatch-app parity issue.

## Parity surface

| Surface | Status | Detail / reason |
|---|---|---|
| REST | Touched | The three settings read and write through `GET`/`PUT /api/config`. |
| CLI | Touched | `datawatch config set` and `get` on the three dotted keys. |
| Comm channel | Touched | `configure autonomous.automation.dependency_update.<key>=<value>`, validated before anything is written. |
| MCP | Touched | The read-only `dependency_update_status` tool reports the settings in effect, the next run and the last run; `config_set` changes the keys. |
| PWA Settings | Touched | Dependency update toggle, schedule and major-limit fields in the Automata section. |
| YAML/config | Touched | The `autonomous.automation.dependency_update` block in `config.yaml` with `enabled`, `schedule` and `max_majors`. |
| Android | Excluded from this change, tracked | No Android code lives in this repository; tracked in the datawatch-app parity issue. |
| iPhone | Excluded from this change, tracked | Same as Android; tracked in the datawatch-app parity issue. |

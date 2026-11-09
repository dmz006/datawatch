# Post-ship hooks

**Date:** 2026-11-02
**Version:** v9.67.0 (planning version, from the `Version` variable in `cmd/datawatch/main.go`)
**Design record:** `docs/plans/2026-10-30-supervisor-duties-generic-and-project-split.md` — the "Post-merge extras" row: filing a parity issue in the companion app repository and updating the plans registry are "hooks a project turns on", not core steps. Tracker row 267 in `docs/plans/README.md`.
**Related plan:** `docs/plans/historical-plans/2026-11-01-merge-pipeline-and-onboarding-report.md` (the pipeline these steps attach to).

## Goal

Give every project a configurable list of post-ship hooks — small steps that run after the ship's deploy step to finish what a project does by hand today (updating its plans registry row, filing a parity issue in its companion repository, tagging the release where the project wants it tagged, notifying people). The mechanism is generic core; the targets (which registry file, which row pattern, which companion repository, which title or message templates) are all configuration, so nothing in `internal/mergepipeline` or the daemon names this repository, its companion or its operator's message text. A project that lists no hooks, or leaves them disabled, ships exactly as it does today.

## Scope

- `internal/mergepipeline/definition.go` — the new hook built-in action names join `BuiltinActions()`; existing step/`post_ship_hook` kind unchanged.
- `internal/mergepipeline/run.go` — hook steps run after the deploy step, non-blocking, in order.
- `internal/config/merge_pipeline.go` — the hook list and the built-in hook targets in the `autonomous.merge_pipeline` block, with the existing global / per-project override shape, plus validation.
- `internal/autonomous/` — the merge run record carries each hook's result as a step, and when the run belongs to an Automaton the same result is also written to that Automaton's triage note.
- REST, MCP, CLI, comm channel, YAML/config, PWA surfaces for the hook list and targets (see Parity surface); the mobile clients receive the PWA card through one datawatch-app parity issue.
- Docs: `docs/howto/post-ship-hooks.md`, `docs/config-reference.yaml`, `docs/datawatch-definitions.md`, `docs/operations.md`, `README.md` / `docs/README.md` index, `docs/testing-tracker.md`, `CHANGELOG.md`.
- Not in scope: changing any existing pipeline step, the conflict policy, or anything before the deploy step; making hooks mandatory or ordered relative to the tag/push/deploy steps; mobile clients as configuration-input surfaces (see Parity surface).

## Design

### The hook list is configuration

- The hook list lives in configuration, in the project's `autonomous.merge_pipeline` block, not in code. Hook steps appear in the pipeline (file, record, or the built-in default) as steps of the existing `post_ship_hook` kind; the built-in default pipeline lists none.
- Each entry is one of two shapes, exactly like a pipeline step: a **named built-in** or a **shell command**. The built-ins are `registry_row`, `parity_issue`, `tag` and `notify`. A command entry is any shell line the project wants, subject to the existing step rules (relative `dir`, no unscoped tmux kill, timeout).

### Off by default

- Hooks are off by default. A hook that is listed but not enabled does nothing: the runner records it as a skipped step (the step record shows the reason "disabled") and moves on. Enabling is per hook and per project, through the same config surfaces as every other `autonomous.merge_pipeline` key — nothing turns on by being mentioned.

### Built-in targets are settings, never hard-coded

- Every target a built-in touches comes from configuration, with the same global / per-project override shape as the rest of the block:
  - `registry_row` — the registry file path pattern and the registry row pattern (the row's identifying shape) are settings.
  - `parity_issue` — the companion repository and the issue title template are settings.
  - `tag` — the tag name pattern and the repository it applies to are settings.
  - `notify` — the notify template (the message body with its placeholders) is a setting.
- The core code formats and delivers; it contributes no file path, repository name, row text or message body of its own. A hook whose target settings are missing records a skipped step naming the missing setting; it never guesses one.

### Ordering and failure

- A hook runs **after the ship's deploy step**, hooks in listed order, each after the previous hook finishes (failure or not).
- A failed hook **never blocks or fails the ship**. After tag, push and deploy have succeeded the ship is done: a hook failure is recorded on that hook's step record with its output tail, and the run continues to the next hook and ends `ok`. Hook steps carry `on_failure: continue` semantics by construction; a project cannot configure a hook to stop the run.

### Recording

- Every hook result — ran ok, failed, or skipped because disabled or missing-target — is recorded as a **step in the merge run record** (`autonomous.MergePipelineRun`), which is what the merge status already shows as the pipeline runs: step name, kind `post_ship_hook`, start and end, result and the outcome line.
- When the run **belongs to an Automaton**, the same per-hook results are additionally appended to that Automaton's **triage note**, so whoever is looking at the Automaton sees which hooks ran, which failed and what they reported, without hunting the pipeline record.

## Phases

Each phase's checklist includes the AGENT.md **Phase Completion Checklist** in full (all 8 items of the general documentation checklist, conditional docs, mobile parity, localization, version handling and the plan status line), each item checked or marked N/A with a one-line reason — a phase's functional items alone do not make it done.

### Phase 1 — Hook list and targets in configuration

Status: Planned

- The `autonomous.merge_pipeline` block gains the hook list (per hook: kind, built-in action name or command, enabled flag, dir, timeout) and the built-in target keys (registry file path pattern, registry row pattern, companion repository, issue title template, notify template), each overridable per project with the existing `Effective*` accessor shape.
- `Validate()` accepts and rejects: unknown action names, a hook with both an action and a command, absolute or `..`-escaping file path patterns, empty templates; unset keys stay valid and read as "no hooks, all disabled".

Checklist (in addition to the Phase Completion Checklist):

- [ ] Config block, accessors and validation implemented per the description above.
- [ ] Unit tests cover the valid and rejected shapes above and the unset-defaults case.
- [ ] `go test ./...` green.

### Phase 2 — The four built-in hook actions

Status: Planned

- `registry_row`, `parity_issue`, `tag` and `notify` implemented as built-in actions against their configured targets only, each returning a short outcome line for the run record and recording a skipped step when a required target is unset.
- No action carries a repository name, file path, row text or message body that is not a setting.

Checklist (in addition to the Phase Completion Checklist):

- [ ] The four actions work per their target settings; each missing-target case skips with the setting named.
- [ ] Fake-ship-runner tests exercise every action and every skip, in the spirit of the existing `run_test.go` hook step — no test reaches a real remote or repository.
- [ ] `go test ./...` green.

### Phase 3 — Runner wiring: ordering, non-blocking, recording

Status: Planned

- Hook steps run after the deploy step, in listed order; a disabled hook is recorded skipped; a failed hook is recorded failed and the run continues and ends ok.
- Each hook result lands as a step in the merge run record, and when the run's Automaton ID is set, the results are also appended to that Automaton's triage note.

Checklist (in addition to the Phase Completion Checklist):

- [ ] Ordering, skip, continue-on-failure and both recording paths behave per the Design section.
- [ ] Tests for the fail-then-continue sequence and for the with/without-Automaton recording branches.
- [ ] Live/smoke item from the Phase Completion Checklist: one real local run with an enabled `notify`-shaped command hook against a throwaway project confirms the record and triage note shape.
- [ ] `go test ./...` green.

### Phase 4 — Operator surfaces: REST, MCP, CLI, comm

Status: Planned

- Read and write of the hook list and the built-in targets through every operator configuration surface, alongside the existing `autonomous merge-pipeline` surfaces; hook results readable wherever merge run records already are.

Checklist (in addition to the Phase Completion Checklist):

- [ ] Round-trip verified per the Configuration Accessibility Rule: write the hook list and targets on one surface, read them back on another.
- [ ] Endpoint contract changes recorded in `docs/testing-tracker.md` with the section name in the commit message.
- [ ] `go test ./...` green.

### Phase 5 — PWA

Status: Planned

- A Post-ship hooks card in the existing Merge pipeline settings area: the hook list with per-hook enable toggles plus the target inputs for the four built-ins, and hook results shown in the merge run / Automaton views where pipeline runs already render.
- New locale keys added to all five bundles with high-visibility keys in the locale guard test.

Checklist (in addition to the Phase Completion Checklist):

- [ ] Card and results view render per the description; `node --test internal/server/web/*.test.js` green.
- [ ] Localization Rule fully followed, including the datawatch-app key/translation issue for the new strings.
- [ ] Full `go test ./...` green.

### Phase 6 — Documentation

Status: Planned

- `docs/howto/post-ship-hooks.md` (the operator workflow: list a hook, set its targets, enable it, read its result), entries in `docs/config-reference.yaml`, `docs/datawatch-definitions.md` and `docs/operations.md`, the doc index updates, the testing-tracker section, the CHANGELOG entry and this plan's own status lines.
- No tracker IDs in the operator-facing howto or definitions entries.

Checklist (in addition to the Phase Completion Checklist):

- [ ] All eight general documentation checklist items present or N/A with a reason.
- [ ] Conditional docs item: the definitions and howto above are the conditional docs for this concept, checked as present.
- [ ] `make lint` green on the tree.

### Phase 7 — Mobile parity

Status: Planned

- The single datawatch-app parity issue covering the Post-ship hooks PWA card and its results views, filed with the parent version and the new locale keys, per the Mobile-Parity and Localization rules.
- Translations of the new keys into DE, ES, FR and JA land through the mobile pipeline, replacing the English placeholders in the PWA bundles.

Checklist (in addition to the Phase Completion Checklist):

- [ ] Mobile-Parity Rule item checked explicitly: issue filed or the existing one commented, linked from this phase.
- [ ] Plan status line for this phase updated to reflect the filed issue.
- [ ] Full `go test ./...` and `node --test internal/server/web/*.test.js` green.

## Parity surface

Every channel in the full parity set, with the planned change or the reason for exclusion:

| Surface | Plan |
|---|---|
| REST | Read and write the hook list and the built-in hook targets on the existing `autonomous merge-pipeline` config routes; hook results readable on the merge run record route, alongside the existing step records. |
| MCP | The existing merge-pipeline get/set/validate tools carry the hook list and targets; run records expose the hook steps as with the other steps. |
| CLI | `datawatch autonomous merge-pipeline show` and `set-record` include the hook list and targets; no new top-level command. |
| Comm channel | The `autonomous merge-pipeline` verbs and verb set cover the hook list and targets like every other `autonomous.merge_pipeline` key. |
| YAML/config | New keys under `autonomous.merge_pipeline` (hook list entries and the four targets' settings) with per-project overrides, commented in the config template per the Configuration Accessibility Rule. |
| PWA | Post-ship hooks card in the Merge pipeline settings area (enable toggles + target inputs) and hook results in the merge run / Automaton views. |
| Android | Excluded as a configuration-input surface — mobile config-input is out of scope per the Configuration Accessibility Rule; the PWA card is mirrored by a datawatch-app parity issue filed with this work. |
| iPhone/iOS | Excluded as a configuration-input surface — mobile config-input is out of scope per the Configuration Accessibility Rule; the PWA card is mirrored by a datawatch-app parity issue filed with this work. |

## Status

All phases **Planned** as of 2026-11-02. This is a plan document: its own commit does not bump the version.

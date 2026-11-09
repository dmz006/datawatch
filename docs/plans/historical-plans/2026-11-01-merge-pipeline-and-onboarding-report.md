# Merge pipeline and onboarding report: plan

**Date:** 2026-11-01
**Version:** set at merge time. The merge helper (`scripts/merge-branches-bump-version.sh`) sets the next patch above main; this plan names no number and adds no versioned changelog heading.
**Design record:** `docs/plans/2026-10-29-pre-10-0-design-proposals-index.md` (the questions and the operator's words are recorded there)
**Parity issue:** `docs/plans/parity-issues/merge-pipeline.md`

## What this delivers

1. **A merge pipeline for every project.** The ordered steps that take finished branches to shipped (checks, merge, version bump, changelog, tag, push, deploy) are data, not code tied to one repository. A project defines them in a file in its own repository, in a record the daemon holds, or not at all and uses the built-in pipeline.
2. **Merge conflict handling the operator chooses.** A conflict policy and a selectable model decide what the daemon does when a merge conflicts.
3. **An onboarding report.** One read that says what is configured, what is missing and what to try next, so a new install does not need a hunt through settings. It is part of the tutorial and onboarding material.

## The operator's answers (2026-11-01)

| Question | Answer | Operator's words |
|---|---|---|
| Where a project's merge pipeline is defined | Both a repository file and a daemon record, and when both exist the file wins | "When we make tutorial and onboarding stuff this should be part of that,  set up a report for datawatch automation,  help users use datawatch" |
| What the daemon does at a merge conflict | Options: stop and ask; resolve additive conflicts mechanically; mechanical first and then a model proposal that must pass the tests. The model is selectable | "This should be an option for others and be able to select model of they code what i did" |
| Where the onboarding report belongs | Part of the tutorial and onboarding work, reporting on Automation, to help users use datawatch | the same words as the first row |

Consequences:

- The source order is a setting whose default is the file first, then the record, then the built-in pipeline. A bad file falls back by another setting (default: the record), and the problem is always reported.
- The conflict policy and the conflict model are settings. The default policy is the mechanical step followed by a model; the model names an entry in the operator's LLM registry. A policy that needs a model degrades to stop-and-ask when none is set or reachable. The settings never add, edit or remove a registry entry.
- A model proposal is never applied to the project directly: it is built and tested in a throwaway scratch worktree, and the project moves only if the pipeline's test step passes there.
- The report is read-only, every part of it is a setting (on by default), and nothing in it is tied to this repository's name, paths or people.

## Parity surface

Every layer carries the same data from the same code.

| Layer | Merge pipeline | Onboarding report |
|---|---|---|
| REST | `GET /api/autonomous/merge-pipeline`, `PUT` and `DELETE /api/autonomous/merge-pipeline/record`, `POST /api/autonomous/merge-pipeline/validate` | `GET /api/automation/onboarding-report` (`?format=text`) |
| MCP | `autonomous_merge_pipeline_get`, `autonomous_merge_pipeline_validate`, `autonomous_merge_pipeline_set_record` | `onboarding_report` |
| CLI | `datawatch autonomous merge-pipeline show`, `validate`, `set-record`, `clear-record` | `datawatch automation onboarding-report` |
| Comm channel | `autonomous merge-pipeline` and its sub-verbs | `onboarding-report` |
| PWA | Settings > Automation > Merge pipeline card and settings | Onboarding report card and settings |
| Mobile apps | the parity issue file above; filed by the supervisor at the merge | same |
| Settings | `autonomous.merge_pipeline.*` with per-project overrides | `onboarding_report.*` |
| Docs | how-to `docs/howto/merge-pipeline.md`, how-to `docs/howto/onboarding-report.md`, the definitions, the config reference, operations, the testing tracker | same |

## Phases

| Phase | What | Status |
|---|---|---|
| 1 | Pipeline definition, validation, the built-in pipeline, and the file and record sources with the resolver | done |
| 2 | Conflict handling: the mechanical additive step, the model proposal tested in a scratch worktree, the policy and model settings | done |
| 3 | The merge run driven by the resolved pipeline, with the config block and its checked write | done |
| 4 | REST routes, MCP tools, CLI commands and comm verbs for the pipeline | done |
| 5 | The onboarding report builder, its settings and its REST route | done |
| 6 | The onboarding report MCP tool, CLI command and comm verb | done |
| 7 | PWA cards, settings, help links and locale keys for both | done |
| 8 | Documentation: the two how-tos, definitions, config reference, operations, testing tracker | done |
| 9 | This plan and the mobile parity issue file | done in this change |
| 10 | The supervisor files the mobile app parity issue and replaces the first line of the issue file with the reference | pending, at the merge |
| 11 | Translations of the 80 new locale keys into DE, ES, FR and JA, which replace the English placeholders in the PWA bundles | pending, from the mobile pipeline |

## Version

No version is chosen here. The merge helper sets the next patch above main when the branch is merged, and the changelog heading is written then.

# Supervisor duties: what is generic datawatch and what is one project's habit

**Date:** 2026-10-30
**Version:** none (planning document; plan-document commits are exempt from the version bump)
**Tracker:** rows 265 to 269 in `docs/plans/README.md`

Confirmed: no — the split below is the supervisor's reading. Nothing is built from the open choices until the matching question in the queue is answered.

## Why this exists

Operator, 2026-10-30, quoted: "How much of what you listed is specific to datawatch building vs generic datawatch for other users doing their own thing". The list was the inventory in `2026-10-17-supervisor-rules-and-lessons.md` ("What the supervisor still does by hand"). Operator rule from the same day, quoted: "most options need to be configurable since others may use datawatch in their own way, my answers are default and our use case".

## The split

Every duty on the inventory has a **mechanism** that any user of Automata on their own repository needs, and **inputs** that are this project's habits. The mechanism goes in core. The inputs are configuration with this project's answers as the shipped defaults, so nothing in core names this repository.

| Duty | Generic mechanism (core) | This project's inputs (configuration, not code) |
|---|---|---|
| Merge a finished Automaton | trial merge in a throwaway tree, conflict handling, test gate, ship | the test and lint commands, the version and changelog rule, the files that hold a version, the push rule |
| Cancel records after a ship | close the finished records | none |
| Deploy | run a configured deploy command after a ship and check it came up | the build, install and restart commands, the post-deploy health check |
| Hourly check | the daemon watch | which findings are reported and how often |
| Plan coverage | gate on missing coverage, ignore files a task creates | none |
| Move a stuck task, release a hung verification | observe progress, route to another backend | the backend order |
| Retry after infrastructure failure | retry transient failures, recover dependents | the retry budget |
| Uncommitted work | refuse to complete a task whose work is not committed | none |
| Specs and follow-up work from an answer | draft from the quoted answer, validate, queue | the spec rules (no tracker IDs, no absolute paths) |
| Record and apply answers | store the quote, apply the decision | where the decision records live |
| Cleanup | worktree and temp cleanup (shipped) | retention |
| CI | watch, classify, rerun a hung run once (shipped) | the workflows that count as security |
| Budget and lanes | calculator advises, then moves the lane count inside the operator's setting | the cap, the weekly reset, the special-item rule |
| Next step of a chain | start the next member | whether to file the next step automatically |
| Post-merge extras | run configured hooks | filing a parity issue in the companion app repository, updating the plans registry |

The last row is the clearest project-only habit. Parity issues and registry rows are not core steps; they are hooks a project turns on.

## What needs a model

- **No model:** closing records, deploy, the watch, retry, cleanup, answer recording, the lane-count move, starting a chain step, the merge up to the point of a conflict.
- **A small local model is enough, behind a validator that re-asks on failure:** drafting a spec from a quoted answer, first-pass CI log classification (infrastructure, flake, real), picking a backend from a routing rule.
- **A stronger model:** a merge conflict the mechanical resolvers cannot settle, diagnosing and fixing a real CI failure, judging whether a plan covers its spec.
- **Escalation shape:** a step that fails twice goes to the question queue with an attempt summary (the existing failure-escalation rule), not to a bigger model silently.

Not yet tested: whether the small local model meets the bar on spec drafting and log classification. That is a short eval against past supervisor outputs and it belongs with the existing eval work.

## What is filed

- Row 265: deploy as a configured post-ship step.
- Row 266: lane count moves by itself inside the operator's setting.
- Row 267: post-merge hooks (registry row, companion-repository parity issue) so these are not core steps.
- Row 268: the pipeline as configuration (test, bump, changelog, ship, tag, deploy per project).
- Row 269: CodeQL advanced workflow conflicts with the repository's default setup.

## Open choices (each is a question in the queue)

1. Where the per-project pipeline lives: a file in the repository, a record in the daemon, or both with the file winning.
2. Who runs the supervisor loop: plain code with narrow validated model calls and escalation, or a continuous Claude session as today.
3. How far the daemon may go on its own at a merge conflict.
4. The CodeQL setup conflict: answered 2026-10-30, quoted: "Keep gh, remove the local,  unless we can align them and test locally and find issues before ci failure". Default setup stays; the PRD `36db0001` removes the analyze jobs, keeps the alert gate and investigates a local run.

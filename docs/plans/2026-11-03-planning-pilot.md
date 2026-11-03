# Planning pilot: local model against the cloud planner

Date: 2026-11-03. Status: running. Decision owner: operator; the supervisor runs it.

## Why

Every decompose runs on the global planner, which is the cloud model, and the supervisor then rewrites tasks by hand. Operator direction (2026-11-03): if the local model can plan, let it, and judge it by how few rewrites the supervisor makes. If it works better or cleaner after changing decompose lint, verify scripts or other tools, that is a win. Planning must be routable and configurable for other users (see the planning routing Automaton).

## Success measure

Fewer supervisor rewrites per decompose than the cloud planner on comparable drafts. Recorded per decompose:

| Field | Meaning |
|---|---|
| planner | backend and model that planned |
| tasks | number of tasks produced |
| lint findings | decompose lint findings on the first pass (once the lint ships; until then the supervisor's pattern scan) |
| rewrites | tasks the supervisor edited, cancelled or re-flagged before approval |
| missing verify | tasks with no verification script |
| attempts | planning runs needed |
| minutes | wall time |
| outcome | approved as is, touched up, retried, escalated |

## Ladder under test

1. Plan on the local model.
2. Short review: a quick touch-up when the problems are few and local; a full retry with the findings as feedback when they are structural.
3. After two failed attempts, plan on the cloud model.

Heavy design and security-sensitive drafts that need a larger view start on the cloud model; this stays configurable (the planning routing Automaton makes the rules data).

## Samples

Chosen to cover the kinds of work: a UI layout change (first sample, the Automation sub-tab layout), a config and surfaces feature, a docs-only task set, a defect with tests, a security-sensitive draft. The cloud-planned decomposes from 2026-11-02 and 2026-11-03 are the control group.

## Tooling changes allowed

When the local model fails in a repeatable way, the fix may be a decompose lint rule, a task template, a verify script, the guidance block or a prompt change. Each such change is recorded here with the failure that caused it.

## Log

(one line per decompose, newest last)

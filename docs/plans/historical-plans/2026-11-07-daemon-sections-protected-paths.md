# Daemon-owned sections and the Automaton protected paths

**Date:** 2026-11-07
**Scope:** the protected-paths gate versus the daemon's own sections of the
rules file, plus the operator-facing documentation. No new setting.

## The problem

When a session starts, the daemon writes its own sections into the project
rules file — the session guardrails header, the memory section, the RTK
section, the cross-session section, the platform context section. The rules
file (`AGENT.md` or `CLAUDE.md`) is a default protected path. So a task that
starts right after a session start can be held on the one rule that only the
daemon itself changed: a false positive.

## The chosen way, and why

- **The sections stay in the tracked rules file.** The worker's LLM reads the
  rules file for the project's rules every session; moving the daemon's
  sections out of it would break that contract, so they stay where the worker
  finds them.
- **The guard compares with the sections stripped on both sides.** When it
  compares the base content with the task's content, the daemon's marked
  sections are removed from the base side and from the task side first. The
  daemon's own write can never register as a change, and any content that
  changes outside the marked sections still registers — a real edit to the
  rules is still held and asked.
- **The daemon's own commits and the task merge strip the sections** in the
  same way, so the daemon's commits and the merge of a task's work do not drag
  the sections into the diff.

Rejected alternatives:

- **`skip-worktree`.** It hides real local edits from git — an operator's own
  edit to the rules would silently disappear from `git status` and `git
  diff`. That hides genuine changes instead of exempting one writer.
- **`git exclude`.** Excludes (`.git/info/exclude`, `core.excludesFile`) do
  not apply to files that are already tracked, so they cannot exempt a
  tracked rules file at all.

## Spotting the false positives before the sweep ran

The supervisor identifies a task blocked by the false positive when all of
the following hold:

1. the task is in `blocked` status, with an error that starts
   `protected_path_blocked: ` or a protected_paths hold;
2. its only held path is `AGENT.md` or `CLAUDE.md`;
3. its open question is labelled `protected_paths`
   (`autonomous_held_questions`); and
4. `git diff <base> -- AGENT.md` (or the matching rules file) shows only the
   daemon's sections.

## Releasing them

At start, the daemon's sweep finds those tasks and releases them without a
question — no operator answer is raised or needed — and records a
`protected_paths_false_positive_released` decision.

## Operator note

Once this ships, the platform context setting can be switched back on. The
operator does that; it is not a task.

## Parity surface

A documentation and gate-behaviour note. No new surface in `REST`, `MCP`,
`CLI`, `comm channel`, `YAML/config`, `PWA`, `Android` or `iPhone/iOS` — no
new endpoint, tool, command, setting or client affordance. The sweep and its
`protected_paths_false_positive_released` decision ride on the existing
Automaton decision-log surface already listed in the howto.

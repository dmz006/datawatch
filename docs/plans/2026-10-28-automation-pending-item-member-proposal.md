# Automation members for work that has no PRD yet: proposal

**Date:** 2026-10-28
**Version:** none (planning document; plan-document commits are exempt from the version bump)
**Tracker:** BL644 (draft PRD `900d0001`)

Confirmed: no — the option is not chosen. Operator, 2026-10-28, quoted: "file gap 1 as a PRD first", in answer to the gap list in `docs/plans/2026-10-28-ten-oh-arc-automation-draft.md`. This file records the problem and the options; nothing is decided and no implementation starts until a `Confirmed: yes` line replaces this one.

## Problem

An Automation member must be an existing PRD (`validateAutomationOwnership`: "member prd ... not found"). The 10.0.0 arc has about ten registry rows with no PRD (BL620, BL556, BL412, BL414, BL422 follow-up, BL473, BL483, upstream monitoring, BL609 steps 9 to 11, BL610 steps 2 to 5). They cannot be members, so the arc Automation `0ff316f1` would move from "open work" to the final stage while they are still open. The ordering then lives in the supervisor's head again.

## Options (not chosen)

**A. A new member kind, "pending item".** A member carries a registry row id and a title and no PRD. The coordinator never starts it; it holds the cursor as "waiting for the operator" until a PRD is attached to it (then it behaves as a prd member) or the operator skips or removes it. Successors stay blocked. It shows in the pipeline strip and counts as open work in progress and ETA.

**B. File a draft PRD from the registry row.** One action creates a held draft PRD whose spec is the row text, quoted, and the row then becomes an ordinary prd member. No new member kind. Cost: a PRD per row with a prose-only spec, and the HOLD is again text (a draft member blocks the Automation with "next member is draft").

**C. Both.** B for rows that are ready to be specified, A for rows that are still waiting on a design decision.

## Answers so far

Q1, operator, 2026-10-28, quoted: "C and it's things are held or blocked how do w get them moving and closed?" — option C (both). Q2 and Q3 are still open, so `Confirmed: no` stays until they are answered.

## Open questions for the operator

1. A, B or C?
2. Should an unattached pending item hold the Automation (default in this draft) or be skippable by a timer?
3. Where do registry rows come from: the repository's `docs/plans/README.md` parsed by the daemon, or the operator typing a title and a free-text reference?

## Parity surface (if implemented)

Data model and validation (`internal/autonomous`), REST, MCP, CLI, comm channel, PWA pipeline strip and the create form, locale keys, openapi, howto, and a datawatch-app parity issue for the PWA change.

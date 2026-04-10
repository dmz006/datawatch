# Plan Attribution Guide

When plans or features in datawatch are inspired by or derived from other projects,
the source must be credited in the plan document.

---

## Why

Transparency matters. Contributors and reviewers should know where design ideas
originate, especially when cross-pollinating between projects.

## How to credit

Add a **Source** field near the top of the plan metadata:

```markdown
# Feature Name

**Date:** 2026-04-10
**Source:** Inspired by [project-name](link) — brief description of what was borrowed
**Priority:** medium
**Effort:** 2-3 days
```

For multiple sources:

```markdown
**Source:**
- Memory architecture from [hackerdave](link) — spatial memory organization (wings/rooms/halls)
- Wake-up stack from [milla jovovich](link) — 4-layer context loading (L0–L3)
```

## What to include

- **Project name** — the originating project or repo
- **Link** — URL to the project, repo, or specific file/doc if applicable
- **What was borrowed** — brief description of the concept, pattern, or design
- **Adaptation notes** (optional) — how the idea was modified for datawatch

## Known source projects

| Project | Contributions to datawatch |
|---------|---------------------------|
| hackerdave | Memory system concepts, spatial organization |
| milla jovovich | Wake-up stack, context layering patterns |

*Update this table as new source projects are referenced.*

## Scope

This applies to:
- Plan documents in `docs/plans/`
- Architecture docs that reference external designs
- Backlog items that originate from other project explorations

It does **not** apply to:
- Standard library or framework usage
- Common design patterns (MVC, pub/sub, etc.)
- Bug fixes or refactors with no external inspiration

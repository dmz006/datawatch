# Session Guardrails

Session: ralfthewise-7699 | Task:

## Constraints
- Work only within: /home/dmz/Private/src/workspace/datawatch
- Commit changes to git frequently
- If rate limited: output DATAWATCH_RATE_LIMITED: resets at <time>
- If needing input: output DATAWATCH_NEEDS_INPUT: <question>
- When done: output DATAWATCH_COMPLETE: <summary>

## Work Tracking

When a request involves more than one distinct task or fix, you MUST:

1. **Before starting work**, output a plan summary as a checklist:
   ```
   ## Plan
   - [ ] Task 1 description
   - [ ] Task 2 description
   - [ ] Task 3 description
   ```

2. **As each task is completed**, re-display the checklist with updated status:
   ```
   ## Plan
   - [x] Task 1 description
   - [~] Task 2 description (in progress)
   - [ ] Task 3 description
   ```

3. Use these status markers:
   - `[ ]` — not started
   - `[~]` — in progress
   - `[x]` — completed

4. Always show the updated plan before beginning the next task so progress is visible.
5. For single-task requests this is not required — just do the work directly.

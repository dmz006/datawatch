# Parity: task verification script fields and stable-script lint rules (datawatch v9.171.0)

Parent release: datawatch v9.171.0

## What changed in datawatch

Verification scripts are stable by default. The planner's prompt carries five habits for the script of every file-changing task, and the decompose lint rule pack grows to thirteen rules, nine of them blocking, with new findings for a task with no verification script and no stated reason, a script that compares only against the task's own files, and a docs check that does not run `make sync-docs` first. The plan check requires the single final gate of an Automaton to carry a usable verification script, or a stated exemption reason when the project's own gate command is the closing check. The task edit path now takes `verification_script` and `verification_script_reason` on REST, MCP, the CLI, chat and the web task editor; an omitted field stays unchanged.

## Contract change

The task edit request (`POST /api/autonomous/prds/{id}/edit_task` and its MCP, CLI and chat equivalents) gains two optional fields. At least one of `new_spec`, `read_only`, `report_path`, `verification_script` or `verification_script_reason` is required.

| Field | Type | Meaning |
|---|---|---|
| `verification_script` | string | One repo-relative script file the Automaton runs to check the task, not a command. Empty leaves it unchanged. |
| `verification_script_reason` | string | The stated exemption for a task with no script. Empty leaves it unchanged. |

The decompose lint findings read (`autonomous_decompose_lint_findings`) can now carry `verification_script_reason_empty`, `verification_script_task_files_only` and `verification_script_docs_without_sync`.

## What the mobile app needs

- Two fields in the task editor, Verification script and Script exemption reason, with the hint text, sent only when changed.
- Show the three new decompose lint findings wherever the app shows lint findings.
- Three new web strings in all five locales: `prd_task_verif_script_label`, `prd_task_verif_script_reason_label` and `prd_task_verif_script_hint`.

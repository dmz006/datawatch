# datawatch v5.26.8 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.7 → v5.26.8
**Patch release** (no binaries — operator directive: every release until v6.0 is a patch).
**Closed:** PRD delete cascade + clearer error, dynamic model dropdown across all LLM modals, tab hide-when-disabled, mic + CSV-expand affordances, `auto` badge removed.

## What's new

### PRD delete — cascade-aware confirm + clearer error

Operator: *"I get an error when trying to delete an autonomous task, and deleting a task with children should delete the children."*

Two improvements:

1. **`confirmPRDDelete`** in the PWA now pre-fetches `/api/autonomous/prds/{id}/children` before showing the confirm dialog. If the PRD has spawned children:
   - Confirm message names the cascade count: *"…and 3 child PRD(s) under it…"*.
   - If any child is running: *"…(one of which is running — daemon will refuse until you Cancel it first)…"*.
2. **Failure toast** strips the leading `Error:` prefix and surfaces the daemon's verbatim message — pre-v5.26.8 the toast read *"PRD delete failed: Error: prd ... is running ..."* with the actionable bit buried under double-prefix noise.

3. **`Manager.DeletePRD` cascade-aware running guard** (Go-side). Previously only the top-level PRD's status was checked, and `Store.DeletePRD` happily deleted a running child along with the parent — leaving the executor goroutine writing to a now-deleted PRD. v5.26.8 walks descendants and refuses with: *"descendant prd %q is running; cancel it before deleting parent %q"*. Two new tests cover the running-descendant + cancelled-descendant matrix.

### Dynamic model dropdown across every LLM modal

Operator: *"In autonomous selecting an llm would refresh a list of available models to select in a drop down, if model selection isn't available that should be hidden."*

`openPRDCreateModal` (New PRD) had the dynamic-dropdown pattern since v5.27.0. v5.26.8 generalizes it into two reusable helpers:

- `ensureLLMModelLists()` — fetch `/api/ollama/models` + `/api/openwebui/models` once, cache in `state._availableModels`.
- `refreshLLMModelField(wrapId, innerId, backendId, currentValue)` — populate or hide the Model field based on the selected backend. Operator-pinned custom models survive backend toggles via a "(custom) <name>" option.

Wired into:

- `openPRDEditTaskModal` (per-task LLM override).
- `openPRDSetLLMModal` (per-PRD LLM override).
- `openPRDCreateModal` (already had it; refactored to use the shared helpers).

Each modal now opens with the dropdown pre-populated for the current backend; switching the backend refreshes the list; selecting a backend with no known model list hides the field entirely.

### Mic + CSV-expand for large config editing

Two operator messages bundled together:

> *"Any configurations that are large input of text like comma separated lists of guardrails should have a dialog open so editing is easier and any large editing dialog should have mic input also"* — and *"Mic should only be displayed if whisper is configured."*

Two new helpers:

1. **`micButtonHTML(targetId)`** — emits a 🎙 button next to a textarea. Click → record → POST `/api/voice/transcribe` → append transcript to the named field. Reuses the existing voice infrastructure; **gated on `state._whisperEnabled`** (populated on boot from `/api/config` — if whisper isn't configured the button isn't emitted at all).
2. **`csvExpandButtonHTML(targetId, label)`** — emits a ✎ button next to a CSV-list input. Opens a modal with a textarea (one item per line) plus a mic button. Save normalizes back to comma-separated and dispatches `change` so existing autosave handlers fire.

Wired:

- Mic button → spec textareas in New PRD + Edit Task + Edit PRD modals; the new CSV-edit modal textarea.
- CSV-expand button → `autonomous.per_task_guardrails`, `autonomous.per_story_guardrails`. Field-metadata gains a `csv: true` flag, so extending to `fallback_chain` and other CSV lists is one-line.

### Autonomous tab hidden when feature disabled

Operator: *"Anyway if it is not enabled the tab should not show."*

`navBtnAutonomous` starts `display:none` in `index.html`; JS unhides only when `/api/autonomous/config` returns `enabled: true`. Failed fetches leave the tab hidden — better invisible than showing a tab that 503s on click. Pattern is reusable for any future feature-gated tab.

### Auto badge removed

Operator: *"Why is there an auto indicator on autonomous page?"*

The `● auto / ● offline` indicator added in v5.26.6 / v5.26.7 was clutter — header status dot already shows WS state. Removed entirely. Toolbar now only carries `+ New PRD`, the status filter, and the templates checkbox.

## Configuration parity

No new config knob.

## Tests

1397 passing (1395 baseline + 2 new descendant-running tests). 56 in `internal/autonomous/`.

## Known follow-ups

- Validate claude-code can run an autonomous PRD end-to-end (operator-asked next). Different-LLM API path verified — set_llm + decisions log + task LLM resolution all confirmed via REST round-trip. End-to-end run with claude-code as the worker is the next validation target.
- v6.0 packaging items unchanged.

## Upgrade path

```bash
git pull
# Hard-refresh PWA tab once (Ctrl+Shift+R) to pick up the new
# CACHE_NAME and v5.26.8 app.js. The Autonomous toolbar will be just
# "+ New PRD" and the filters; click any LLM modal to see the model
# dropdown refresh as you switch backends; the per-task-guardrails
# inputs in Settings → General → Autonomous get a ✎ button next to
# them; if whisper.enabled=true, every spec textarea gets a 🎙 button.
```

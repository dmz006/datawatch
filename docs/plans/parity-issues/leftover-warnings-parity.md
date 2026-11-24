# [sync] Files left behind: leftover_warnings in the Automaton detail

Draft only. No issue has been filed on any tracker.

## Affected area

The Automaton detail screen in the datawatch-app mobile clients (Android and iOS). The daemon already returns the field and the PWA now shows it.

## What the daemon does

When an Automaton reaches a final outcome (done or failed), the end-of-run cleanup removes the harness scratch files it can and looks for files the run left in the project folder: scratch files tracked by git and unexpected untracked files. Each one is recorded as a plain sentence, `leftover file: <path>`, in the Automaton's `leftover_warnings` list. Repeated finishes do not add the same sentence twice. The list is warn-only; it never blocks or changes the Automaton. It is returned by `GET /api/autonomous/prds/{id}` (and the `autonomous_prd_get` tool) and is absent when empty. The daemon's own launch scratch files (`worker-mcp.json` and `worker-starting-context.md`) are never staged into a task commit, so they are not part of any run's work.

## What the PWA does

The Automaton detail overview shows a bordered panel titled "Files left behind (N)" after the CI status and before the landing state. It carries one sentence of help and the entries as a list of escaped code spans. Nothing is shown when the list is empty. Locale keys `prd_leftover_warnings_title` and `prd_leftover_warnings_body`, present in all five locales (en, de, es, fr, ja).

## Expected behavior in the apps

1. Automaton detail: show the same panel when `leftover_warnings` is non-empty, with the count in the title. Render each entry as plain text, never as markup.
2. Reuse the two locale strings above.
3. No new setting and no action: the panel is informational.

## Compatibility

No migration is needed. Older apps ignore the field.

## Tests

Daemon side: the existing leftover warning and launcher scratch file tests. PWA side: `app-leftover-warnings.test.js` covers the render helper, escaping, the empty case, the detail call and the locale keys.

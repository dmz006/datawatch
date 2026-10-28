# Parity note: session progress strip removal

Parent: dmz006/datawatch-app issue 292 (feat(i18n): pi worker progress block and settings parity (datawatch v9.30.0)).

## Comment posted to the app repo

Parity note: the Live progress strip was removed from the PWA session detail page. The progress and looping_warning task data and the locale keys the task card uses are unchanged, so the mobile app should not build the strip for the session screen; keep any rendering on the automaton task card only.

Comment URL: https://github.com/dmz006/datawatch-app/issues/292#issuecomment-6483767524

## Filing record

Posted: 2026-10-28, via `gh issue comment 292 -R dmz006/datawatch-app --body-file <scratch file>`. No earlier identical comment existed on issue 292; posted exactly once.

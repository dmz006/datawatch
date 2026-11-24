# datawatch-app parity draft: web app update loop (file only, no issue filed)

Date: 2026-11-24. One draft for the web app fix that stops the update bar and the reload loop on an unchanged release.

## What changed in the web app

- On an installed web app with no release change, the page used to show the bottom bar "Update available - tap to
  reload" about one second after load and reload itself about 30 seconds later, repeatedly. It now registers the
  versioned worker script at once, shows the bar only when the release the page was loaded with differs from the
  release the daemon reports, takes the idle reload once per browser tab session, and a worker activation no longer
  waits on its own window navigation.
- No new endpoint, setting, tool, locale key or auth change. `/api/health` `version` is unchanged.

- A launch splash whose minimum hold was cut short (the web page was navigated away from) is shown again and held
  in full on the next load; a hold that finishes, or that the operator dismisses, counts as shown.

## Mobile client

- If the app records "splash shown" when the splash appears, record it as unfinished until the hold ends, so a
  restart during the hold does not skip the next one.

- The native apps have no service worker and no page reload, so nothing to build. Check only that the app does not
  treat a repeated `/api/health` `version` equal to its own as an update prompt, and that an update prompt, where the
  app has one, appears once per real release change rather than on a timer.
- Acceptance: with the daemon release unchanged for 2 minutes, the app shows no update prompt and does not restart
  its connection on its own.

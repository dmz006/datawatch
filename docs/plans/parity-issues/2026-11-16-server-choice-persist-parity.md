# fix(parity): the header server choice is no longer overwritten by the settle timeout (datawatch v9.181.0)

Parent release: datawatch v9.181.0

## What changed

The web app remembers the server picked in the header dropdown, per browser. A bug in the restore let a pending settle timeout overwrite the saved choice with "all" after a validated restore had already settled, or over a choice the user made in the meantime. The restore now cancels that timeout when it settles, and only the stale or unvalidatable case saves the "all" fallback.

## Settings to add to the mobile app

None.

## REST contract

None.

## New locale keys with English values

None.

## Screens

- Header server dropdown (web only): the selection is kept per browser and survives a reload.

## For the mobile app

No change is required for this release. Please check that the app's own server selection is kept across restarts and is never replaced by a fallback value after a restore has already settled or after the user has chosen a server.

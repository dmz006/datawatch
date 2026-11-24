# [sync] Capacity read: adopted workers over a pool or node limit are flagged

Draft only. No issue has been filed on any tracker.

## Affected area

Any client that renders the capacity read (`GET /api/capacity`, the `leases` list): the PWA (already updated), the Android and iOS apps (datawatch-app), the CLI, MCP and the comm channel text view.

## What changed in the daemon

After a restart the daemon can re-adopt more sessions than a pool or node limit allows. The sessions are real and stay recorded; new work waits. Each such adopted worker lease in `leases` now carries:

- `adopted` (bool): the lease was recorded from work that survived a restart.
- `over_limit` (bool), `over_limit_code` (`adopted_over_limit`) and `over_limit_reason` (readable text naming the pool or node, what it holds and its limit).

All are omitted when the lease is not over a limit, are computed on each read, and clear when the pool or node is back at its limit. A worker admitted through the queue is never flagged, and operator sessions started over a cap keep their `overage` records instead.

## What the PWA already does

The capacity card marks a flagged holder with a chip beside its id and shows `over_limit_reason` as the tooltip. New locale key, in en, de, es, fr and ja: `capacity_over_limit` ("over the limit").

## Expected behavior in the apps

If an app lists lease holders from the capacity read, show a warning chip for a lease with `over_limit` true and the reason on tap or long press. Reuse `capacity_over_limit`. Treat unknown `over_limit_code` values as a generic over-limit marker.

## Compatibility

Older apps ignore the new fields. Older daemons omit them, so an app must treat a missing `over_limit` as false.

## Tests

Daemon: ledger snapshot tests (adopted flagged, admitted not flagged, operator overage not flagging workers, node overflow) and a REST read test. PWA: a node test for the chip, escaping and the locale key.

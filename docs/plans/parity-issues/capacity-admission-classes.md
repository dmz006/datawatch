# The admission class settings and the capacity overage and buffer notes are missing from the Android and iPhone apps

**Draft:** file as a datawatch-app issue at the merge that ships this change. The operator checklist in
`docs/howto/automata-capacity.md` (section "Who is admitted: worker, supervisor and operator starts") lists
the mobile apps as open until the issue ships.

## Affected area

Five settings under `autonomous`, all read live:

- `autonomous.capacity_account_claude_limit` — whole number, cap on the whole Claude account (default 2,
  `0` = unlimited).
- `autonomous.capacity_supervisor_reserve_claude` — whole number, Claude slots above the cap only supervisor
  sessions may use (default 1).
- `autonomous.capacity_supervisor_reserve_pi` — pi slots above the cap (default 1).
- `autonomous.capacity_supervisor_reserve_host` — host session slots above `session.max_sessions` (default 4).
- `autonomous.capacity_operator_reserve_host` — whole number, host session slots above `session.max_sessions`
  only operator starts may use (default 5, `0` = none). Locale key `settings_capacity_operator_reserve_host`.
  `GET /api/capacity` carries `session_cap` (`max`, `active`, `operator_slots`, `supervisor_slots`, `over`) and a
  session record carries `over_session_cap`; the apps may show "N over the session cap" on a session started
  into the allowance.
- `autonomous.capacity_operator_overage_alert` — switch; raise an alert each time an operator session starts
  over a pool's limit (default off).

And the capacity read, `GET /api/capacity`, which gained fields:

- on a pool: `reserve` (the supervisor buffer above `limit`) and `over` (how far held plus external is above
  `limit`); both are absent when zero;
- on a lease: `class` (`worker`, `supervisor`, `operator`) and, on an operator lease granted over a limit,
  `overage` (a list of `pool`, `limit`, `over`);
- a top-level `federation` object (only when a peer is opted in, in a `peer_window` or matched): `hosts`
  (each host at its own pools: `name`, `self`, `reachable`, `pools`, `llm_limit`, `llm_held`, `node_limit`,
  `node_held`, `shared_account` self | confirmed | opted_in | pending | none, `counted_held`, `note`), `cluster`
  sums, `account` (`limit`, `local_held`, `peer_held`, `total`) and `pending_shared_accounts` (matches awaiting
  confirmation: they count nothing). Locale keys `capacity_fed_*` in every locale file; renderer
  `capacityFederationHtml` in internal/server/web/app.js. The apps show the cluster line, one line per host with
  its own sizes, and the pending note "possible shared account, awaiting your confirmation".
- a top-level `session_cap` and a top-level `unmapped` list: subscription backends a start named that resolve to no LLM entry.

Where they live:

- Config: `AutonomousConfig` in internal/config/config.go and internal/config/capacity_admission.go.
- Locale keys: `settings_capacity_account_claude_limit`, `settings_capacity_supervisor_reserve_claude`,
  `settings_capacity_supervisor_reserve_pi`, `settings_capacity_supervisor_reserve_host`,
  `settings_capacity_operator_overage_alert`, and the capacity view notes `capacity_pool_account`,
  `capacity_pool_over`, `capacity_pool_reserve`, in every file under internal/server/web/locales.
- Settings page rows: the capacity rows of the autonomous settings list in internal/server/web/app.js.
- Capacity view: `capacityPoolLabel` in internal/server/web/app.js.

## Current behavior

The daemon and the PWA carry all of it. The apps' Settings has none of the five rows, and their capacity view
shows a pool as used over limit only: an operator session admitted over a limit reads as a pool above 100%
with no explanation, the supervisor buffer is not shown, and the `account:claude` pool has no name.

## Expected behavior

Capability parity with the PWA; each app uses its own idiomatic controls.

- Settings shows the four number fields and the switch, with labels from the locale keys above and the
  defaults shown when unset; a number field accepts whole numbers of zero or more.
- A save sends the dotted key to `PUT /api/config` and shows the daemon's 400 text unchanged.
- The capacity view names the `account:` pool, and a pool line carries "N over the limit: operator" when
  `over` is above zero and "+N reserved for supervisors" when `reserve` is above zero, using the
  `capacity_pool_over` and `capacity_pool_reserve` strings (`{0}` is the count).
- A backend listed under `unmapped` is shown as a warning line.

## Supervisor credential (no app change)

The supervisor credential (`autonomous.capacity_supervisor_credential_enabled`, default true, plus the CLI and
REST endpoint `/api/capacity/supervisor-credential`) is operator tooling for an external supervisor and a
removable stopgap. The apps need neither a setting row for it nor a way to mint it: the token is a secret that
is not meant to pass through a phone. The one setting is in the PWA Settings row list and the locale key
`settings_capacity_supervisor_credential_enabled`; the apps may show it as a switch with the other capacity
rows (optional).

## Restarted workers (no app change)

A restarted worker session (rate-limit fallback, exit hook) keeps the worker class and queues at the head, ahead
of other waiting workers, still respecting the cap. This is daemon behaviour only: it adds no field and no
setting. The apps need nothing beyond showing the wait reason of a waiting task as they already do.

## Acceptance

- [ ] Settings has the five rows, labels from the locale keys in every shipped language.
- [ ] Each number row reads and writes through `GET` and `PUT /api/config`; unset shows the default.
- [ ] A refused value shows the daemon's 400 text unchanged.
- [ ] The capacity view shows the over and reserve notes and the account pool name.
- [ ] `unmapped` backends show as a warning.
- [ ] The cluster view shows each host at its own limits, the Claude account total and a pending shared account
      note; a peer that is not reachable shows as such.

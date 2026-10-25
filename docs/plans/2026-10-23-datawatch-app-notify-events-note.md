# datawatch-app — notify set (`notify_events`) — app-side note

**Date:** 2026-10-23
**Repo:** `dmz006/datawatch-app` (Android + iPhone/iOS)
**Daemon counterpart:** this repo — the `notify_events` feature (Unreleased; see the resolved open questions 1 and 2 in [`plans/2026-10-13-prd-lifecycle-events.md`](2026-10-13-prd-lifecycle-events.md))
**Status:** **file a GitHub issue on `dmz006/datawatch-app` when this daemon build merges.** The daemon surface below lands with this build; the app UI is not built here (per this repo's parity policy — the app repo owns its UI).

## Context

The daemon now answers "which Automaton lifecycle event kinds produce notifications?" with **three tiers**: the Automaton's own `notify_events` list, its guardrail profile's `notify_events` list, and the global `autonomous.notify.default_events`. The first non-empty tier wins. Two behaviour changes land with this build that the app must be able to explain and, where possible, drive:

1. **The global default is now the five-kind baseline** — `prd.needs_review`, `prd.blocked`, `prd.failed`, `prd.completed`, `prd.cancelled` — where before the global tier contributed nothing (the effective default came from elsewhere / nothing fired). **Opt-out** is an *explicit empty list* at the global tier (`default_events: []`) — not a null and not an absent key (those mean "use the baseline").
2. **Opt-in kinds are gated**: `task.session_started` / `task.session_cleared` (and every other task-level kind) fire only when the Automaton's resolved set includes them. A subscription on such a kind for an Automaton that does not list it is inert until the kind is added.

Every existing surface already carries the new field; this note is the **app-side management UI** on top of the REST surface below. The PWA "Notification events" panel (Automaton detail) and the Guardrail Profiles editor (settings) are the behavioural references.

## Kinds (closed set)

- Automaton-level: `prd.active`, `prd.approved`, `prd.archived`, `prd.blocked`, `prd.cancelled`, `prd.completed`, `prd.draft`, `prd.failed`, `prd.needs_review`, `prd.paused`, `prd.planning`, `prd.rejected`, `prd.revisions_asked`, `prd.running`
- Task-level: `task.awaiting_approval`, `task.blocked`, `task.cancelled`, `task.completed`, `task.failed`, `task.in_progress`, `task.pending`, `task.queued`, `task.running_tests`, `task.session_cleared`, `task.session_started`, `task.verifying`, `task.waiting_capacity`

`prd.updated` is an event kind but is **rejected** by `set_notify_events` — "always notify or never". The daemon's validation error text carries the full valid list; render it verbatim in an inline alert rather than re-implementing the list client-side.

## Resolution semantics (what the UI must show)

- `nil` / absent / empty at the Automaton tier **and** at the profile tier means **inherit**, not "off". Empty means "off" only at the global tier.
- The daemon answers with two fields on the Automaton JSON:
  - `notify_events` — the Automaton's own list (absent when inherited);
  - `notify_events_effective` — `{ "events": [ … ], "source": "automaton" | "profile" | "global" }`, the list that actually applies and where it came from.
- Guardrail profile JSON carries its own `notify_events` (absent when unset).
- A subscription's kind filter applies **after** the gate: narrowing a subscription can only remove kinds, never add them.

**App-side recommendation:** surface the `source` value as a small badge / caption on every screen that shows the notify list ("Automaton override", "From guardrail profile", "Global default"), so an operator changing nothing at the Automaton tier can still see *why* those kinds fire without opening settings. This is the difference between a settings field and an operator feature.

## REST surface (this build — Unreleased)

| Method & path | Auth | Body / response |
|---|---|---|
| `POST /api/autonomous/prds/{id}/set_notify_events` | `autonomous:write` (federation cap `autonomous:write`) | body `{ "notify_events": [ … ] }` → `200` + the updated Automaton (including `notify_events` and the computed `notify_events_effective` with `source`). Missing key, `null` or `[]` = clear the Automaton tier (inherit from the profile tier, then the global). `400` on blank or unknown kinds — the error message contains the valid kind list. |
| `POST /api/autonomous/guardrail_profiles` | `autonomous:write` | create — body may include `"notify_events": [ … ]` (or omit it; an omitted key means unset). |
| `PUT /api/autonomous/guardrail_profiles/{id}` | `autonomous:write` | update — same field; `"notify_events": []` clears the profile tier back to the global default. |
| `GET /api/autonomous/prds/{id}` | `autonomous:read` | the Automaton JSON with both `notify_events` and `notify_events_effective` — read these for the resolved preview, do not compute tiers client-side. |

Audit: every set or clear is logged as `autonomous_notify_events_changed` (CEF name `NotifyEventsChanged`), with the old and new list and, for the Automaton tier, the `source` after the change. The app's audit feed (if it renders CEF) should treat id 45 / severity 3 like every other config-change action.

The global tier (`autonomous.notify.default_events`) is config, not per-Automaton data: it is set through the app's existing config UI (`autonomous.get_config` / `set_config` or the config YAML). When surfacing it, show the same tri-state meaning — absent = five-kind baseline, explicit empty list = nothing fires from the global tier.

## MCP (for reference — the app talks REST, not MCP)

`autonomous_prd_set_notify_events(id, notify_events)` — `id` is the Automaton ID; an omitted or empty `notify_events` array means inherit. On the guardrail-profile tools (`guardrail_profile_create` / `guardrail_profile_update`) the field is the `notify_events_json` argument — a JSON array of kind strings omitted to leave the tier unset, `"[]"` to clear it. `autonomous_prd_get(id)` returns the `notify_events_effective` shape above.

## CLI (reference)

```
datawatch autonomous prd-set-notify-events <prd-id> <kind,kind,...|inherit>
datawatch guardrail profile create --name <name> [--guardrails name,name,...] --notify-events kind,kind,...
datawatch guardrail profile update <id> --notify-events kind,kind,...
datawatch autonomous prd-get <prd-id>   # full JSON, notify_events + notify_events_effective in it
```

## Comm (reference)

`autonomous set-notify-events <prd-id> <kind1,kind2,...|inherit>` — `inherit` (or a blank value) clears the Automaton tier back to the profile/global default.
`guardrail profile notify-events <id> <kind1,kind2,...|inherit>` — sets the profile tier; `inherit` clears it back to the global default. The list argument is always present; the `inherit` sentinel is the one that clears.

## Android build guidance

- Surface: **Automaton detail screen** → a "Notifications" section (new) showing the resolved kind list with the `source` badge (above), a "Choose kinds…" affordance (multi-select over the valid list, fetched from a failed validation or shipped with the app — prefer the former, it is always current), a "Save" / "Clear (inherit)" pair, and a "Guardrail profile: manage…" link.
- **Manage mode:** a second entry under Settings → Guardrail Profiles (or the equivalent existing group) with the same edit form bound to the profile endpoints. When an Automaton's `source` badge reads "profile", the Automaton screen should show a disabled-style list with a pointer to the profile editor rather than an editable copy — editing the Automaton's own field there would shadow the profile for that one Automaton, which operators will not expect.
- Validation is server-side and total: no client-side kind whitelist needed; on `400`, display the daemon's message (it carries the valid list) and keep the form's current state.
- The **five-kind default** should never be hard-coded into the app UI as "the default" — read `notify_events_effective` (its `source` = "global" case is exactly the baseline the daemon currently ships with). The baseline set is operator-changeable and will likely change; any copy in the app that says "the default is X" must be dynamic or say "the daemon default".
- Offline: same pattern as the existing offline-queue — enqueue set/clear and flush on reconnect; the daemon is authoritative and the write is idempotent (last write wins), so queued writes reorder safely.
- i18n: new strings go through the app's locale pipeline; no English-only UI. The PWA's new `notify_events_*` keys in the five locale bundles are a good source of wording.

## iOS build guidance

- Mirror the Android surface in the existing Automaton detail + profile editor.
- Same offline-queue note: writes are idempotent and the daemon re-resolves on every event, so flushed writes are safe.
- The `source` badge must be visible without opening the list, or operators will not know a kind came from a profile.

## Parity surface for this note (app repo)

| Surface | Status |
|---|---|
| Resolved notify list + `source` badge on the Automaton screen | ❌ — the work item for this note |
| Edit / clear (inherit) the Automaton's `notify_events` | ❌ — part of the UI work item |
| Guardrail-profile `notify_events` editor (create / update) | ❌ — part of the UI work item |
| Validation errors displayed verbatim (daemon owns the valid-kind list) | ❌ — part of the UI work item |
| Global tier (`autonomous.notify.default_events`) in the config UI with the tri-state meaning (baseline / explicit-off) | ❌ — extend the existing config surface |
| Event *delivery* for kinds in the resolved set | ✅ already flows via the v9.3.0 `PublishToTopic` → UnifiedPush / APNs path — no change, but note the new gate only fires for kinds in the resolved set |

## Open questions (app-repo)

1. Where does "Guardrail profile: manage…" live — a link on the Automaton screen, a section in the existing settings group, or both?
2. Should the Automaton screen offer "inherit from profile" as a visible button when `source` = "profile", or is the badge + disabled list enough?
3. On the resolved list, do we want a per-kind "why does this fire?" affordance that expands to the tier that supplied it (Automaton override / profile / global)?

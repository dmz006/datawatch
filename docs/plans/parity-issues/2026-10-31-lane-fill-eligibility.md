# [sync] feat(lane-fill): eligibility reasons, lane counts and per-lane-kind mode (datawatch next minor release)

Parent release: datawatch next minor release

## What changed in datawatch

Lane fill now says why it refuses a queued Automaton, says how many lanes it is really using, and can be switched on or off for one lane kind at a time. The default mode stays `shadow`: nothing starts work on its own unless the operator sets a mode to `on`.

- **Eligibility reasons.** Before lane fill takes a queued Automaton it checks that the Automaton is fit to run. An Automaton that fails a check is refused, the refusal is recorded as a `blocked` decision, and the decision carries a stable code in `eligibility_reason` next to the readable `reason` text. A refused Automaton does not count as eligible work anywhere: not in the lane's `eligible_waiting`, not in `queued_eligible`, and not in the supervisor watch idle lane finding.
- **Lane counts.** The number of lanes now follows the in-flight caps of the enabled LLM entries for the lane kind's backend, unless the operator turns that off. The status shows, per lane kind, the configured count, the count the last poll used, and where the used count came from.
- **Per-lane-kind mode.** The GPU lanes and the Claude lanes each have their own mode. An empty setting inherits the base `mode`. The status shows the mode in effect for each kind.
- **Surfaces.** The same data appears on the REST read, the MCP tool `autonomous_lane_fill_status`, the CLI (`datawatch autonomous lane-fill`), the comm channel (`autonomous lane-fill`) and the PWA lane fill card. A decision line on the CLI and comm channel reads `[<eligibility_reason>] <reason>`.

## Changed REST contract

`GET /api/autonomous/lane-fill`. Every other field of the status is unchanged. Unknown fields are ignored, as before.

### Decision field (each entry of `decisions[]`)

| Field | Type | Notes |
|---|---|---|
| `eligibility_reason` | string | The refusal code. **The key is absent when the decision is not a refusal** (it is omitted, never sent as `""`). A client must treat an absent key or `null` as none. Only decisions with `action` `blocked` for a refused Automaton carry it. |

The seven allowed values:

| Value | Meaning |
|---|---|
| `stale` | The queued Automaton has not been touched for longer than the stale limit (probes and old drafts). |
| `held` | A HOLD line, the words "do not run" or "sequenced last" in the spec, or a judge that timed out while the Automaton needs review. |
| `sequenced_after_unshipped` | The spec says it is sequenced after another Automaton that is not completed, or that resolves to no Automaton at all. |
| `proposal_not_confirmed` | An implementation Automaton whose named proposal file is readable but carries no `Confirmed: yes` line. |
| `proposal_missing` | An implementation Automaton whose spec names no proposal file, or none of the named files can be read. |
| `main_carries_work` | The base branch already carries what the Automaton asks for (a merge or task commit for it, or its branch is entirely on the base branch). |
| `worktree_invalid` | The Automaton has a recorded worktree that is gone, has no `.git` entry, or is not in the registered worktree list of its repository. |

The `reason` text beside the code is readable English and may change wording; the code is the stable part.

### Status field `lane_counts`

An object with two members, `gpu` and `claude`. Each member has these fields:

| Field | Type | Notes |
|---|---|---|
| `configured` | integer | The count from the config (`gpu_lanes` or `claude_lanes`). |
| `effective` | integer | The count the last poll planned with. |
| `source` | string | Where the effective count came from: `config` (the configured count) or `llm_caps` (the sum of the in-flight caps of the enabled LLM entries for that lane kind's backend). Treat any other value as the raw text. |
| `reason` | string | **Absent key when not set.** When `source` is `config` it says why the caps were not used (for example the cap-following setting is off, the lane kind is configured for none, no cap source, no enabled entry for the backend, or an entry is unlimited). It is absent when `source` is `llm_caps`. |

Example: `"lane_counts": {"gpu": {"configured": 2, "effective": 2, "source": "config", "reason": "no enabled gpu entry for backend pi-prompt"}, "claude": {"configured": 3, "effective": 2, "source": "llm_caps"}}`.

### Status field `modes`

An object with two members, `gpu` and `claude`. Each is a string: `off`, `shadow` or `on`, the mode in effect for that lane kind (the kind's own setting when it has one, otherwise the base `mode`). The existing `mode` field stays and is the base mode. An unknown or empty value is read as `shadow`.

### Config keys (`GET` / `PUT /api/config`, under `autonomous.lane_fill`)

| Key | Type | Default | Meaning |
|---|---|---|---|
| `stale_after_hours` | integer | `72` | An Automaton queued this many hours is stale and is not started automatically. An explicit `0` turns the stale rule off. Absent key means 72. |
| `require_confirmed_proposal` | boolean | `true` | An Automaton created from a proposal is eligible only once the operator has confirmed that proposal. |
| `check_main_carries` | boolean | `true` | Check whether the project default branch already carries an Automaton's work before it counts as eligible. |
| `require_worktree` | boolean | `true` | An Automaton must have its own worktree to be eligible. |
| `lane_counts_from_caps` | boolean | `true` | The lane counts follow the in-flight caps; when `false` only `gpu_lanes` and `claude_lanes` apply. |
| `gpu_mode` | string | `""` (inherits `mode`) | Mode for the GPU lanes: empty, `off`, `shadow` or `on`. |
| `claude_mode` | string | `""` (inherits `mode`) | Mode for the Claude lanes: empty, `off`, `shadow` or `on`. |

The four booleans and `stale_after_hours` are omitted by the daemon when unset, so an absent key means the default and an explicit `false` or `0` survives a save. The same keys are set through the CLI (`datawatch config set autonomous.lane_fill.<key> <value>`), the comm channel (`configure autonomous.lane_fill.<key>=<value>`) and the existing config tools; no new endpoint or tool is added.

## Where to look in datawatch

- `internal/autonomous/lane_fill.go`: the decision type with `eligibility_reason`.
- `internal/autonomous/lane_fill_eligibility.go`: the seven refusal codes.
- `internal/autonomous/lane_fill_caps.go`: `lane_counts` and how the counts are derived.
- `internal/autonomous/lane_fill_coordinator.go`: the status object with `modes` and `lane_counts`.
- `internal/config/config.go`: the `autonomous.lane_fill` keys and defaults.
- `internal/server/web/app.js` and `internal/server/web/locales/{en,de,es,fr,ja}.json`: the PWA lane fill card, the eligibility view and the settings rows.

## Acceptance (mobile)

Android and iOS both:

- The lane fill status view shows the refusal code beside a `blocked` decision when `eligibility_reason` is present, together with the readable `reason`.
- An unknown `eligibility_reason` value renders as the raw code and never crashes or drops the decision; the seven known values may be shown with a localized label.
- An absent `eligibility_reason` key, and a `null`, both mean none: the decision renders exactly as today.
- The view shows effective versus configured lane counts per lane kind (for example "Claude: 2 of 3 configured, from LLM caps"), and shows the `reason` text when it is present and the source is `config`. Absent `lane_counts` or `modes` (an older daemon) is tolerated and the view renders as today.
- The view shows the mode in effect for each lane kind from `modes`, alongside the base `mode`.
- The Automata settings block has the seven `autonomous.lane_fill.*` keys above with the listed types and defaults; an absent key shows the default; `gpu_mode` and `claude_mode` offer empty (inherit), `off`, `shadow` and `on`.
- All new labels are localized in EN, DE, ES, FR and JA, using the app's own word for Automaton, with the English text and the translations taken from the PWA bundles.
- Nothing in the app starts or approves an Automaton as a result of these fields; they are read-only status plus the settings.

## Filing record

Checked for an open issue first: `gh issue list -R dmz006/datawatch-app --state all --search '"lane fill" in:title'` on 2026-11-01 returned only #301, the verifier-wait lane view issue, which covers a different change. A new issue was filed for this work, once, with the text from "Parent release" through "Acceptance (mobile)" as its body.

Filed: https://github.com/dmz006/datawatch-app/issues/327 (2026-11-01, via `gh issue create -R dmz006/datawatch-app --title "[sync] feat(lane-fill): eligibility reasons, lane counts and per-lane-kind mode (datawatch next minor release)" --body-file <scratch draft>`).

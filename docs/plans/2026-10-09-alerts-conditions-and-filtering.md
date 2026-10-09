# Plan: Alerts — conditions, filtering, and SIEM parity

- **Date**: 2026-10-09
- **Version at planning**: v8.78.2
- **Status**: Planned — not started. Operator-requested design
  discussion only; no implementation in this pass.

## Context

Immediately after shipping `create_alert` (a one-shot "raise an alert"
action with no conditions, no filters, and no on/off state — just
`level`/`title`/`body`), the operator raised a real design concern
while reviewing the documentation for it:

> "alerts on every event is really just a log event queue and I think
> we have those output (SIEM) integrations, that i hope these alerts
> are also streaming to. we need a plan to have alerts be tied to
> filtered/richer conditions and configurations."

Both halves of that concern are confirmed, not hypothetical — checked
directly against the code before writing this plan, not assumed:

1. **Alerts are not uniformly SIEM-integrated.** `internal/alerts/
   store.go` has zero references to `audit`/CEF — it's a fully
   separate persistence mechanism (`alerts.json`) from the operator
   audit trail (`audit.log`/`access.log`, which *does* CEF-mirror to a
   SIEM when `audit.cef_mirror_enabled` is set — see
   `docs/howto/audit-logging.md`). Of the 10 call sites across the
   codebase that create an alert (`cmd/datawatch/main.go` ×8,
   `internal/server/peer_health.go` ×2), exactly **one** — the new
   REST `handleAlertCreate` — incidentally also writes an `audit.log`
   entry, purely because that handler happens to call the same
   `s.audit(...)` helper every other Phase 4 write path uses. That
   was never a deliberate "alerts stream to the SIEM" design; it's a
   side effect of which package the handler lives in. The MCP
   `create_alert` tool and every other alert source (session events,
   peer-health transitions, alert-rule firings) produce **no**
   audit/CEF trace at all today.
2. **`create_alert` genuinely is "just a log event queue."** It has
   no condition, no filter, no priority/category field, no rate
   limiting, no routing, and no way to say "don't notify me about
   this again for an hour" — every call unconditionally creates one
   row. By contrast, the *separate*, pre-existing alert-rules system
   (`internal/alertrules`, `docs/howto/alert-rules.md`) already has a
   real condition (metric/operator/threshold), a source filter, a
   window, a cooldown, and an enable/disable toggle — but only for
   *rule-fired* alerts, not for `create_alert`, not for the 8
   `main.go`-internal `Add`/`AddSystem` call sites, and not for
   anything arriving from an external monitor.

So today there are, in effect, **three unconnected tiers** of
"alert," each with a different amount of configurability:

| Tier | Example | Conditions/filters? | SIEM? |
|---|---|---|---|
| Alert rules | CPU > 85% for 60s | Full (condition, source filter, window, cooldown, enable/disable) | No |
| Internal `Add`/`AddSystem` call sites | session start, peer went stale | None — hardcoded per call site | No |
| `create_alert` (REST/MCP) | external monitor raises one | None | Incidental (REST only, not MCP) |

## Goals (for whenever this is actually built)

1. A single, consistent way for **any** alert — regardless of which
   of the three tiers it came from — to be filtered/suppressed before
   it's created, not just queried after the fact (today's `source=`/
   `session_id=` filters on `GET /api/alerts` only filter the *read*
   side).
2. A single, consistent way for **any** alert to reach a SIEM, not
   just the one REST path that happens to also hit `s.audit(...)`.
3. Keep `create_alert` cheap and simple for the common case (a quick
   one-shot alert) while making the richer path opt-in, not a
   required body field every caller must now supply.

## Open design questions (operator decides when this is scheduled — not decided here)

### A. Where does filtering live?

- **Option A1 — extend alert rules to also gate `create_alert`/`Add`/
  `AddSystem`.** Every alert, regardless of source, is evaluated
  against the existing `internal/alertrules` condition engine before
  being persisted — e.g. a rule with no metric condition but a
  `source_filter`/`level` match could act as a pure suppression rule.
  Reuses an existing, already-tested engine; but that engine was
  designed to *evaluate observer metrics on a timer*, not to gate a
  point-in-time `Add()` call synchronously — needs investigation into
  whether `alertrules.Store` can cheaply support a synchronous
  "would this ALREADY-ASSEMBLED alert be allowed?" check, separate
  from its existing "evaluate metrics every 30s" loop.
- **Option A2 — a new, smaller "alert policy" concept**, independent
  of alert rules: a short allow/deny list keyed on
  `level`/`source`/title-substring, checked by `alerts.Store.Add`/
  `AddSystem` directly (in-package, no `internal/alertrules` import
  needed — avoids a new cross-package dependency). Simpler to reason
  about, but is a second, parallel filtering concept alongside alert
  rules' existing one — risks the same "three unconnected tiers"
  problem this plan is trying to close, just with two tiers instead
  of three.
- **Option A3 — do nothing architecturally; just add the missing
  fields** (category/priority, trimmed from `create_alert`'s original
  ask in GH#201 Phase 5 since nothing consumed them at the time) and
  let operators filter client-side (PWA Alerts tab already has filter
  chips) or via `source=`/`session_id=` on the read side. Cheapest,
  but doesn't address "alerts on every event" at the point they're
  *created* — a noisy source still writes 500 rows to `alerts.json`
  even if every client-side view hides them.

### B. SIEM/CEF parity

- Does `alerts.Store` need its **own** CEF mirror (mirroring
  `internal/audit.Log.EnableCEFMirror`'s pattern, e.g.
  `alerts.cef_mirror_enabled`), or should every `Add`/`AddSystem` call
  site **also** emit a real `audit.log` entry (reusing the existing
  CEF pipeline, with a new `action` value like `"alert_fired"`) so
  there's only one audit/SIEM pipeline in the codebase, not two?
  The latter keeps one pipeline but conflates two different kinds of
  record (audit = "someone/something *did* X", alert = "something
  happened the operator should *notice*") — worth deciding
  deliberately, not defaulting into it the way the one incidental
  REST-path mirror happened this session.
- If alerts get their own CEF signature mapping (mirroring
  `internal/audit/cef.go`'s `cefSignature` table), `Level`
  (info/warn/error) maps naturally to CEF severity; `Source` maps to
  a custom string field, matching the pattern `internal/agents/
  audit.go`'s CEF formatter already uses for its own event family.

### C. Fields worth adding (only if B or A1/A2 need them)

- `category`/`priority` — explicitly asked for in `create_alert`'s
  original design (plan doc `2026-10-09-gh201-audit-completeness.md`,
  Phase 5 section) and explicitly trimmed because nothing consumed
  them. A richer filtering system is exactly the consumer that would
  make them worth adding — but only alongside whichever of A1/A2/A3
  is chosen, not standalone (a field nothing filters on is the same
  mistake Phase 5 avoided).
- A `dedupe_key`/coalesce window — the PWA already visually coalesces
  identical repeated alerts (`×N` badge, see
  `docs/howto/alerts-and-notifications.md`); that's client-side only
  today. Server-side dedup/rate-limiting (e.g. "don't create a 501st
  identical row within 5 minutes") would need a real design pass of
  its own — flagged, not sized here.

## Explicitly out of scope for this plan doc

- No code changes. This is a design-options doc for a future
  implementation pass, per the operator's own framing ("later
  improvement but it needs to be planned").
- Not re-litigating alert *rules*' existing design (condition/filter/
  cooldown/enable-disable) — that system already works and is
  documented; this plan is about extending similar rigor to the other
  two tiers, not replacing it.
- Not deciding between A1/A2/A3, or the B question — those are the
  operator's calls when this is actually scheduled, via the usual
  one-question-at-a-time process, not pre-decided here.

## Parity surface (once scoped and actually built)

Whichever option is chosen, the Documentation/Parity-surface
discipline this session's GH#201 work followed applies in full:
REST + MCP + CLI + comm + YAML + PWA all need an explicit per-surface
decision (not an assumption), and `docs/howto/alert-rules.md` /
`alerts-and-notifications.md` / `audit-logging.md` all need a
revisit once real behavior changes — not just this plan doc.

## Verification (once scoped and actually built)

- Unit tests for whichever filtering mechanism is chosen, following
  the same "confirmed-fails-without-fix" pattern used throughout the
  GH#201 work.
- A test proving the *previously-silent* gap this plan documents is
  closed: an alert created through each of the three tiers (rule
  firing, an internal `Add`/`AddSystem` call site, `create_alert`)
  produces the same SIEM-visible trace, not just the one REST path
  that happens to today.
- `docs/testing-tracker.md` entry, same convention as every other
  phase this session.

## See also

- [`docs/howto/alerts-and-notifications.md`](../howto/alerts-and-notifications.md)
- [`docs/howto/alert-rules.md`](../howto/alert-rules.md)
- [`docs/howto/audit-logging.md`](../howto/audit-logging.md)
- [`docs/plans/2026-10-09-gh201-audit-completeness.md`](2026-10-09-gh201-audit-completeness.md) — Phase 5's own investigation findings this plan builds on

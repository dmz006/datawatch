# PWA parity sweep review: what the operator has and has not approved

**Date:** 2026-10-28
**Version:** none (planning document; plan-document commits are exempt from the version bump)
**Tracker:** BL647, BL648

Operator, 2026-10-28, about the terminal search and copy buttons: "The search and copy can be removed, the rest look ok for now, I will review when we document everything for 10.0.0". The two earlier removal requests are in `docs/plans/README.md` rows 237 (the live progress strip, BL647) and 238 (terminal search and copy, BL648).

## Why this review exists

The operator asked what the 🔍 button and the "Last event" strip were, and said both were design decisions he had not been asked about. Both came from parity work: the progress strip from BL492 (9.30.0), the terminal search and copy from item D69 of the 2026-10 parity sweep (issue 172 on `dmz006/datawatch`, "parity: PWA changes from the 2026-10-04 three-way parity decisions", filed under the operator's account). The sweep plan is `docs/plans/historical-plans/2026-10-05-pwa-parity-sweep.md`.

## Decision

- Remove: terminal search and copy (BL648); the session-view live progress strip (BL647).
- Keep for now: the rest of D59 to D83 (splash extras, skeleton list, watch filter, tap-to-mute, voice reply, council badge, agent and Chrome badges, quick-reply chips, recent firings, parent link, inline reject and revise, memory promote field, approve with note, permission mode edit, repair depends_on, memory UI, Observer cards, config viewer and editor, subsystem reload and MCP cards, saved-command library, resume previous session, inline file viewer).
- Review point: the operator reviews the whole PWA surface when everything is documented for 10.0.0 (the BL424 integration guide stage). Until then none of these is removed.

## Standing rule (from this review)

A PWA change that adds visible UI the operator did not name is a design decision: list it in the PRD, and say in the merge report what screen it changes. Parity with the apps is required, but the operator decides what the PWA shows.

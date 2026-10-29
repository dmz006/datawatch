# The Automation tab is getting busy: navigation options

**Date:** 2026-10-29
**Version:** none (planning document; plan-document commits are exempt from the version bump)
**Tracker:** row 248 in `docs/plans/README.md`

Confirmed: no — no option is chosen. This file records the request and the options; nothing is built until the operator picks one and a `Confirmed: yes` line replaces this one.

Operator, 2026-10-29, quoted: "Also the automation tab is getting very busy, we may need sub tabs or some way to make it easier to navigate". This is a PWA layout change, so it is a design decision for the operator (see the standing rule on PWA changes in `docs/plans/2026-10-29-pwa-parity-sweep-review.md`).

## What is on the screen today (from `docs/howto/automation-tab.md`; the screen itself was not inspected live for this note)

The Automata tab has three sub-tabs on one bar: Automation, Automata and Templates. The bar's action buttons (select, filter, history, watch, new) change with the sub-tab. The Automation sub-tab stacks, top to bottom: the Progress card, the Capacity and queue card (pools, usage back-off, budgets, Automata waiting for capacity), a status badge row (Draft, Active, Paused, Blocked, Completed, Cancelled), an optional search bar, then one card per Automation. Tapping a card opens the pipeline strip with the control buttons (pause, resume, cancel, skip, insert, remove, edit edges), pending inserts, and, when the question channel is on, the questions area (REST only today; Step 3 adds a PWA queue). The Automata sub-tab carries its own copy of the Capacity card, so the card appears on two sub-tabs.

## Options (none chosen)

**A. More sub-tabs by purpose.** Split the Automation sub-tab into Overview (Progress and Capacity cards), Chains (the list and the pipeline strip), and later Questions (Step 3's queue). Cost: one small PRD; the bar already supports sub-tabs, but the width on a phone needs checking (three become five).

**B. Collapse by default.** Keep one page, but the Progress and Capacity cards start collapsed to one summary line each (state remembered per session), and the Automation cards show only name, status and the running member until tapped. Cost: one small PRD; no new navigation, fewer taps for the list.

**C. Filter-first.** Keep the layout, make the status badges a persistent bar that defaults to Active and Blocked only, move Draft, Completed and Cancelled behind the history toggle, and pin pending inserts and questions at the top as a banner. Cost: one small PRD; changes defaults, not structure.

**D. A mix.** B for the two cards plus C for the list; A only when Questions arrives.

## Recommendation

D: collapse the two summary cards, default the list to what needs attention (Active, Blocked, Paused, pending inserts), and add a Questions sub-tab only when Step 3 lands. It uses the controls already built and avoids a wider sub-tab bar on a phone.

## Open questions for the operator

1. Which option: A, B, C or D?
2. Which part feels busiest today: the two summary cards, the list, or the open Automation's pipeline strip? A screenshot of the busy state would settle it.
3. Should the remembered sub-tab survive a page reload (the how-to says storing it across loads is switched off until the operator decides)?

## Parity surface

PWA only for layout; locale keys for any new sub-tab or collapse label in all five bundles; a datawatch-app issue only if the mobile app mirrors the Automation screen layout.

## Operator direction 2026-10-29 (second message)

Operator, quoted: "In automation there is a new automation button in the header that could be removed qnd sub tabs made there, we have a create button at bottom right still". Verified in the code: the Automation sub-tab has three ways to create an Automation: the **New Automation** button in the sub-tab bar header (`automationNewBtn`, shown only on the Automation sub-tab), the round create button at the bottom right (the same form, same label), and the empty-state **Create Automation** button that appears only when no Automation exists. The header button is therefore a duplicate of the round button. The direction is: remove the header button and use sub-tabs to organise the Automation view. This points to option A (sub-tabs by purpose) with the header button removed; which sub-tabs, and what moves into each, is not yet decided.

Question for the operator, one at a time: remove the header **New Automation** button and make the Automation view's sub-tabs Overview (the Progress and Capacity cards), Chains (the list and the open Automation) and, when step 3 lands, Questions: yes, or name different sub-tabs?

## Decision 2026-10-29

Operator, quoted: "Yes, do those sub-tabs and remove the header button". Decided: remove the header New Automation button; give the Automation view the sub-tabs Overview (Progress and Capacity cards) and Chains (the list and the open Automation), with Questions added when step 3 of the decision channel lands. Filed as draft PRD `e6d40001` (registry row 249). Option A with the header button removed.

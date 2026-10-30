# Dashboard: what it is for and how to make it useful

**Date:** 2026-10-30
**Version:** none (planning document; plan-document commits are exempt from the version bump)
**Tracker:** row 273 in `docs/plans/README.md`

Confirmed: no — no option is chosen. This file records the problem and the options; nothing is decided and no implementation PRD is filed until a `Confirmed: yes` line is added after the operator answers the question in the queue.

## Problem

Operator, 2026-10-30, quoted: "i was just reviewing the dashboard page and it's mostly useless, some data is still empty, cards have lays with nothing useful, not sure what the purpose of it is. We need to iterate on making value of it".

The dashboard is a grid of cards that grew one at a time, each added for the feature of that month. Nobody wrote down who the page is for or which question it answers, so cards cannot be judged as useful or not.

## What is on it today

The cards (from the card list in the web app): Integration status, Automata tree, Network (the orbital map), Live Events, Sessions (sparklines), Timeline (6 hours), 30-Day Activity (heatmap), Guardrails, Multi-EKG with a burn rate, Smoke Run, Memory Scopes, Search Usage. The stored layout also lists the Automata tree twice. Most were built for one feature: Smoke Run for the end-to-end test runs, Memory Scopes and Search Usage for those subsystems, Guardrails for the guardrail feature.

What the cards do not show, although you ask these questions every day: which lanes are busy and with what, how far Claude usage is from the pace line, which questions wait for you, which Automata are held, blocked or waiting for a merge, whether CI is green, and what shipped today.

## Options for the purpose

1. **Operator console.** One screen that answers "do I need to do anything and is anything stuck?": questions waiting, held and blocked work, lanes and their load, usage against the pace line, CI and the latest release, the last decisions. Few cards, each with a number and a link to act. Everything else moves out of the default view.
2. **System health.** CPU, memory, GPU, nodes, models loaded, sessions, errors. Close to what exists, with the empty cards fixed or removed. Answers "is the machine fine?", not "what should I do?".
3. **Per-project view.** The same cards filtered to one project (a worker view for someone running datawatch on their own repository). Needs a project picker and is the most useful for other users, but the most work.
4. **Configurable boards.** Keep the card library, ship two or three ready-made boards (console, health, project) and let each user pick or build their own. Most flexible, and in line with the rule that others use datawatch their own way. The default board is the operator's answer.

## Proposed first step whichever purpose is chosen

- Audit every card: record its data source, whether it is empty on this installation and why, and who would act on it. Remove or hide a card that has no data source on a normal installation; a card with nothing to show says why in one line and links to the setting that fills it.
- Fix the duplicate Automata card in the stored layout.
- Cards are tied to features, so a card shows only when its feature is switched on.
- Give every card a "?" help entry, as every new card needs.

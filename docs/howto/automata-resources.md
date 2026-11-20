---
docs:
  index: true
  topics: [automata, resources, lanes, usage, capacity, ui]
---

# Automata resources: lanes and the usage card

The **Resources** sub-tab of the Automation view shows what your Automata are
running on and how much of the weekly allowance is left. It is read-only:
nothing on it starts, stops or re-prioritises a lane. Each card has a **?**
button with a short explanation and a link to the matching guide.

## What is on it

| Card | What it tells you |
|---|---|
| **Usage calculator** (one card at the top) | **Weekly allowance**, **Pace line** and **Burn per day**: one card — a verdict sentence over three graphs of a fixed size. The same three graphs are on the usage page and can be added to a dashboard. See [usage-calculator.md](usage-calculator.md#the-usage-page-one-card-with-a-verdict-and-three-graphs). |
| **Burn guard** (follows the usage card) | A read-only status line: the guard's enabled state and last check, the rung in force, the projection against the pace line and a **Run now** button. It has no settings controls; a **Burn guard settings** link opens the guard's fields in Settings. See [burn-guard.md](burn-guard.md). |
| **Lane fill** | One row per lane. Each row shows the lane's **node** (or *Node: not reported*), its state and what it runs. The bar is full when the lane works, half when it waits on a verifier and empty when it is idle. See [automation-lane-fill.md](automation-lane-fill.md). |
| **Capacity and queue** | The pools and their usage, any usage back-off, budgets and the Automata waiting for capacity. See [automata-capacity.md](automata-capacity.md). |
| **Automata ship status** | A line saying what shipping is, the push, tag and CI watch steps as tiles with their mode words, the tag format, remote and CI workflow names, and the outcomes. See [automata-checks-and-ship.md](automata-checks-and-ship.md). |

The infrastructure retries and the Automata checks cards are on the
[Events tab](automata-events.md), not here.

## Reading a lane row

- **Node** names the machine the lane runs on. A lane whose node is unknown
  says *Node: not reported* rather than leaving the line out.
- **State** is working, waiting on a verifier, or idle.
- A lane that waits on a verifier counts as available for lane fill; see
  [automation-lane-fill.md](automation-lane-fill.md).

## Settings

This tab has no settings of its own. In **Settings → General → Automata**, the
lane fill keys under `autonomous.lane_fill.*` are in the **Autonomous Automata
planning** section, the usage keys under `usage.calculator.*` are in the
**Usage calculator** section, and the burn guard keys under `usage.burn_guard.*`
are in the **Usage burn guard** section. The cards changed only where they are
drawn and what they show — no new REST, MCP, CLI, comm or chat surface was
added, and the existing endpoints and verbs are unchanged.

## Operator review after the restyle

The Resources, Progress and costs and Housekeeping sub-tabs of Settings →
Automata were restyled to one card style. Before you rely on them, look at
each sub-tab by eye and check the points below. Nothing here changes a setting.

**Resources**

- The usage calculator is one card at the top, followed by the burn guard card.
- The burn guard card is read-only: it shows the status line and **Run now**,
  and has no checkboxes or number inputs.
- The **Burn guard settings** link opens the burn guard section of Settings
  on the Automata tab, and the link returns you to the same place.
- Lane fill, Capacity and queue, and Automata ship status each sit in their own
  card with the same edge, corner and spacing as the usage card.

**Progress and costs**

- The four cost cards (cost, tools, failures and report) use the same card
  style and top spacing as the Resources cards.
- Card text says Automaton and Automata, never PRD.

**Housekeeping**

- Each repair group shows its three states (off, report, on) and its hint text.
- Cards keep their layout with the group hints and help buttons visible.

**Locales (de, es, fr, ja)**

- Switch the web app to each language in turn and check the card titles, the
  **Burn guard settings** link, the **?** help text and the Housekeeping hints.
- Every label is translated. A raw key such as `burn_guard_settings_title`
  showing on screen means a missing translation, and it is a defect to report.
- Wording and card order match English; only the language changes.

## See also

- [Usage calculator](usage-calculator.md)
- [Automata progress](automata-progress.md) and [Automata cost](automata-cost.md), the cards on the Progress and cost sub-tab

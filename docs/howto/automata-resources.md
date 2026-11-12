---
docs:
  index: true
  topics: [automata, resources, lanes, usage, capacity, ui]
---

# Automata resources: lanes and the usage cards

The **Resources** sub-tab of the Automation view shows what your Automata are
running on and how much of the weekly allowance is left. It is read-only:
nothing on it starts, stops or re-prioritises a lane. Each card has a **?**
button with a short explanation and a link to the matching guide.

## What is on it

| Card | What it tells you |
|---|---|
| **Usage cards** (a row at the top) | **Weekly allowance**, **Pace line** and **Burn per day**: three small cards of a fixed size. The same cards are on the usage page and can be added to a dashboard. See [usage-calculator.md](usage-calculator.md#the-usage-page-a-verdict-and-three-cards). |
| **Lane fill** | One row per lane. Each row shows the lane's **node** (or *Node: not reported*), its state and what it runs. The bar is full when the lane works, half when it waits on a verifier and empty when it is idle. See [automation-lane-fill.md](automation-lane-fill.md). |
| **Capacity and queue** | The pools and their usage, any usage back-off, budgets and the Automata waiting for capacity. See [automata-capacity.md](automata-capacity.md). |
| **Automata ship status** | The push, tag and release steps. See [automata-checks-and-ship.md](automata-checks-and-ship.md). |

The infrastructure retries and the Automata checks cards are on the
[Events tab](automata-events.md), not here.

## Reading a lane row

- **Node** names the machine the lane runs on. A lane whose node is unknown
  says *Node: not reported* rather than leaving the line out.
- **State** is working, waiting on a verifier, or idle.
- A lane that waits on a verifier counts as available for lane fill; see
  [automation-lane-fill.md](automation-lane-fill.md).

## Settings

This tab has no settings of its own. The lane fill keys are under
`autonomous.lane_fill.*` and the usage keys under `usage.calculator.*`; both
are in **Settings → General → Automata**.

## See also

- [Usage calculator](usage-calculator.md)
- [Automata progress](automata-progress.md) and [Automata cost](automata-cost.md), the cards on the Progress and cost sub-tab

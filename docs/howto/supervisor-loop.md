---
docs:
  index: true
  topics: [autonomous, automation, supervisor loop, escalation, jobs, runs, settings]
---

# Supervisor loop

The **supervisor loop** keeps every Automaton moving. Each pass runs a set of
mechanical steps with no model call, and a few model jobs (drafting a
specification, classifying a failed CI run, picking a backend). Every run is
recorded. See the [Supervisor loop entry](../datawatch-definitions.md#supervisor-loop)
for the definitions of each setting.

## See what it is doing

Open **Automation** and find the **Supervisor loop** card. It lists the jobs
with their name, style, backend, model, last outcome and escalation count, and
the most recent runs underneath.

## Change it

Open **Settings → Automata → Supervisor loop**. Every field has a help link to
its own entry in the definitions. You can turn the whole loop or any single
step off, choose the style, backend and model for each model job, and set the
retry and escalation limits and the lane count bounds.

## When a job escalates

A model job whose output fails its check is retried (`validator_retries`).
After `escalate_after_failures` failed runs in a row you are asked. A job with no model configured
escalates at once; it never falls back to a hosted model on its own.

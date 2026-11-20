---
docs:
  index: true
  topics: [plan-records, registry-rows, migration, plans]
exec_params: []
exec_steps:
  - tool: plan_record_list
    description: List the plan records before and after a plans migration
    args: {}
    read_only: true
---
# How-to: the plans migration and its six summary kinds

The plans migration builds plans from the older project arcs and registry
rows. It previews by default and writes only when you apply. See
[plan-records.md](plan-records.md#building-plans-from-arcs-and-rows) for the
full picture of plans, rows and titles.

```
datawatch plans migrate            # preview, writes nothing
datawatch plans migrate --apply    # write
```

## What each registry row becomes

| Row | What the migration does |
|---|---|
| Shipped, done or closed, names an Automaton | Never a new plan. The row is linked, as a registry row, to the plan that already holds that Automaton (the release item). |
| Shipped, done or closed, names none | Never a plan. It is listed under *history without an Automaton*, with its title, in a preview and after an apply alike. |
| Open, names an Automaton or Automation | One plan linked to it. |
| Open, names none | One backlog plan, ordered by the old row number. |
| Open, and no name can be derived from the row or the item it names | Listed under *needs review*; no plan is created. |

If the Automaton a closed row names is not yet on a release plan item, the
row is skipped with the reason `history: the Automaton is not on a release plan
item yet; no plan was made for the closed row`; no plan is made for it. Run the release migration first, then the
plans migration again, to link it.

## Review before you apply

- Read the *needs review* and *conflicts* lists in the preview; neither is
  written, and conflicts name what holds the item.
- Read the *history without an Automaton* list: those closed rows stay history
  and are never turned into plans.

Text in a status column, such as a date and a version (`2026-11-10 v9.5.1
shipped`), is never used as a title.

## The six summary kinds

Every preview and apply starts with the server summary line, printed verbatim:
created (would create in a preview), linked, already present, history without
an Automaton, needs review and conflicts. A preview says what would be done; an
apply says what was done.

`datawatch plans migrate` then lists each kind, one line per item. The two
kinds that never become plans get their own sections: the existing
`History without an Automaton (N, not made into plans):` section and the
`Needs review (N, not made into plans):` section, each naming its rows with
their source. `--json` is unchanged and carries `needs_review`.

1. **created** (would create in a preview): plans made.
2. **linked** (would link): Automata, Automations or closed rows linked to a plan.
3. **already present**: items a plan already holds, left where they are.
4. **history without an Automaton**: closed rows that name none; listed only.
5. **needs review**: open rows with no derivable name; not created.
6. **conflicts**: items held by something else, such as a taken title. Each
   names what already holds it and what the migration would have changed;
   nothing of it is written.

## Safety

A second apply changes nothing, because every plan the migration makes carries
a `migrated-from` link. The migration never deletes an arc, row, plan or
setting.

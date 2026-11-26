# Plan roll-up, rename and edit: merge review

**Date:** 2026-11-20

**Recovered:** 2026-11-26. A dated review: its checks, commit ids and line-level statements describe
the tree of 2026-11-20 and were not run again. Later differences seen on 2026-11-26: plan answers built with `planBlock` (the helper named `planBlockWithHand` below is gone) include `set_by_hand` only when the status was set by hand; the plan's tasks read still always includes it. Check each *Operator must
review* item against main before acting on it.

**Scope:** the merge of the plan roll-up, rename and edit feature branch into the
integration branch, the resolution of its conflicts, what the main branch
behaviour is after the merge, and what an operator must review before release.
Files affected: the plan records store and rules (`internal/planrecords/`), the
REST, MCP, CLI, chat and PWA surfaces that expose plans, the federation capability
map, the generated MCP tool-name golden file, the locale bundles, and the plan
documentation (`docs/howto/plan-records.md` and the reference docs it links to).

**Status:** Review complete. Two items need an operator decision before the next
release (see *Operator must review*, items 1 and 2). Nothing in this record
changes behaviour.

## What was merged

| Step | Commit | What it brought in |
|---|---|---|
| Feature branch merged into the integration branch | `57a48468f` | Plan roll-up (REST, MCP, CLI, chat), plan rename and goal edit (REST, MCP, CLI, chat, PWA), the `set_by_hand` marker on plans, and the plan documentation set. 20 files conflicted. |
| Integration merge recorded as an Automaton task | `6069dced6` | The merge above, carried onto the integration branch. No conflicts of its own. |
| Main branch merged back into the integration branch | `84bf3ccdc` | Everything that landed on main since the feature branch forked: decompose lint, defect queue, the no-PWA how-to, supervisor watch, scan and fix-loop changes, the release-smoke and pre-tag scripts, and a large CHANGELOG and operations update. Clean merge; no conflicts recorded. |
| Guard tests added as an Automaton task | `eb3c6031c`, merged as `3450167e1` | `internal/planrecords/plan_feature_presence_test.go`: a guard that fails if the roll-up or the rename and edit change is lost. |

The feature branch's own commits are the source of the roll-up, rename and edit
behaviour. They are not repeated here; the plan records how-to is the operator
reference for them.

### What the feature adds, at a glance

- **Roll-up.** A plan's status is derived from the Automata and Automations
  linked to it. Preview is `GET /api/plans/rollup?project=&dry_run=true`; apply is
  `POST /api/plans/rollup`. MCP `plan_rollup`, CLI `datawatch plans rollup`, chat
  `autonomous plan rollup`. A plan whose status was set by hand is kept and
  reported with `set_by_hand_kept`.
- **Rename and edit.** `PATCH /api/plans/{ref}` takes `title`, `goal` or both.
  MCP `plan_rename` and `plan_edit`, CLI `datawatch plans rename` and
  `datawatch plans edit --goal`, chat `autonomous plan rename` and
  `autonomous plan edit`. The PWA has Rename and Edit goal on an open plan.
- **Marker.** A plan renamed by hand is marked `title_set_by_hand`, so a second
  plans migration keeps the name. A plan whose status is written through the
  store is marked `set_by_hand`.

## Conflicts and how they were resolved

The table covers the 20 files the merge recorded as conflicting. The resolution
was checked by reading the combined diff of the merge commit (`git show --cc
57a48468f`) and, where stated, by re-checking the result against the code.

| File | Conflict | Resolution |
|---|---|---|
| `CHANGELOG.md` | The main side added its own `[Unreleased]` entries. The feature side added its roll-up, rename and edit entries under the same heading. | Main's text was kept and the feature entries were **not** carried over. See operator item 1. |
| `cmd/datawatch/plans_cli.go` | Both sides added plan subcommands. | Union. Main's subcommands kept; `rename`, `edit` and `rollup` added with the feature's flags (`--goal`, `--dry-run`, `--project`, `--json`). |
| `docs/api-mcp-mapping.md` | Both sides added mapping rows. | Union of rows. |
| `docs/howto/plan-records.md` | Both sides edited the same how-to. Main's release-plan and migration sections and the feature's roll-up, rename and edit sections. | Union. Re-checked against the code in this review; see *Plan records how-to*. |
| `docs/mcp.md`, `docs/operations.md` | Both sides added lines to the same reference sections. | Union. The operations entries cover the roll-up and rename and edit. |
| `internal/federation/mcp_tool_caps.go` | Both sides added plan tool capability entries in the same block, with different alignment. | Union. Main's release-plan entries kept. `plan_rollup`, `plan_rename` and `plan_edit` added as `CapAutonomousWrite`, and listed in `nonConfigWriteTools`. |
| `internal/mcp/testdata/registered_tool_names.golden` | Both sides added tool names. | Sorted union. The golden test passes. |
| `internal/planrecords/record.go` | Main added release-plan kind, themes and release label, with their limits. The feature added the one-line title limit, the `set_by_hand` and `title_set_by_hand` fields, and the `Patch` fields for them. | Union. The plan-only check covers the release fields and `title_set_by_hand`. |
| `internal/planrecords/store.go` | Main added the plan-kind and release patch handling in `Update`. The feature added the `set_by_hand` decision in `Update`. | Both blocks kept, in order: plan kind, release, then `set_by_hand`. The `set_by_hand` block runs only for plans. A status written through `Patch.State` sets the marker unless `Patch.SetByHand` says otherwise. |
| `internal/router/plan_records_comm.go` | Both sides added chat verbs and the list output. | Union. Main's release verbs kept; the feature's `rollup`, `rename` and `edit` verbs added. The `(by hand)` mark is printed after a status set by hand. |
| `internal/router/sx2_parity.go` | Both sides edited the `autonomous` help text. | Union of the verbs. Compared with the main branch's help text, every entry is kept; the only additions are `plan rename <plan> <new title...>`, `plan edit <plan> goal <text...>` and `plan rollup [dry-run] [project=P]`. |
| `internal/server/plans_read_api.go` | One side built the plan header block with `planBlock`; the other built it inline with `set_by_hand`. | The header uses a new helper, `planBlockWithHand`, which is `planBlock` plus `set_by_hand`. For an ordinary, unarchived plan the block carries the same `title`, `id`, `state` and `set_by_hand` fields as the inline version. `planBlock` also adds `plan_kind`, `release` and `archived` when they are set; those keys are new on this route. |
| `internal/server/server.go` | Main had `/api/plans/migrate` with no method. The feature registered it as `POST /api/plans/migrate`. Main's `row-plans` route was kept. | Feature's `POST` registration kept. See operator item 2. |
| `internal/server/web/app.js` | Both sides changed the plans browser: main's release-plan controls and the feature's Rename, Edit goal and the set-by-hand badge. | Union. The rename and edit editors are in `app-plan-rename.test.js` (12 checks, passing). |
| `internal/server/web/locales/{en,de,es,fr,ja}.json` | Both sides added keys. | Union of keys. The editor strings exist in all five bundles and keep their placeholders (checked by the PWA test). |

The feature also changed files that merged without a conflict, among them
`docs/commands.md`, `docs/datawatch-definitions.md`, `docs/cursor-mcp.md`,
`docs/testing-tracker.md`, the planrecords router and REST files, and the
guard test. Those merged as the feature wrote them.

## Main behaviour kept

The following main-branch behaviour is unchanged by the merge:

- **Release plans and migration.** Release plan kind, themes, archive and restore,
  the unattributed list, row-plan conversion and the `mode: release` migration keep
  their routes, limits and tool names.
- **Migration.** `POST /api/plans/migrate` is still dry-run by default and applies
  only with `apply: true`. The handler still refuses any method other than POST.
- **Plan-record limits.** Existing record limits (`MaxGoalRunes`, `MaxLinkedItems`,
  `MaxOrder`) are unchanged.
- **Plan header shape.** The `plan` block on the Automata and tasks reads keeps
  the `title`, `id`, `state` and `set_by_hand` fields. The release keys described
  under *Conflicts* are added only when a plan sets them.
- **Automaton help and verbs.** Every verb that was on main's `autonomous` help text
  is still there; the help text gains only the three new plan verbs.
- **Federation caps.** Main's read and write split for the release-plan tools is
  unchanged; the feature's three new tools follow the same write split.

## Checks run in this review

| Check | Result |
|---|---|
| `go test ./internal/planrecords/ -run TestPlanFeaturePresence -count=1 -v` (guard test; its commit recorded that verification was not run) | Passes; four checks ran. |
| `go test ./internal/planrecords/ ./internal/federation/ ./internal/mcp/ -count=1` | Passes (planrecords, federation, mcp golden and tool tests). |
| `go test ./internal/server/ ./internal/router/ -run 'Plan\|plan\|Locale' -count=1` | Passes. |
| `node --test internal/server/web/app-plan-rename.test.js` | 12 of 12 pass. |
| Scan for leftover conflict markers in Go, Markdown, JavaScript, JSON and YAML | None found. |
| `make sync-docs` | Ran clean; no tracked file changed. |

The Go test commands were run under `env -u TMUX -u TMUX_TMPDIR`, as the project rules require.
`make pre-tag` was not run; it is a separate release gate.

## Plan records how-to

`docs/howto/plan-records.md` was checked against the code after the merge. It
describes the roll-up, rename and edit on every surface: the surfaces table, the
*Rolling plan status up from the work* section, and the *Renaming a plan and
editing its goal* section. The routes, MCP tool names, CLI commands, chat verbs,
audit names (`plan_renamed`, `plan_goal_edited`, `plan_record_rolled_up`) and the
100-character title and 2000-character goal limits match the code. The claim that
no surface writes a plan's status after creation is also correct: `plan-record
update` routes accept only registry rows and findings (checked on the REST routes).

No edit to the how-to was needed. Its *Still for the operator to review* section
lists the open decisions (clearing `set_by_hand`, older titles, a rename that does
not rewrite text, a status edit surface, and the roll-up running while plan records
are off).

## Operator must review

1. **Roll-up, rename and edit entries are missing from the CHANGELOG.** The
   feature branch had its entries under `[Unreleased]` (a *Added* group and a
   *Documentation* group). The merge kept main's CHANGELOG and did not carry them
   over. The result has no entry for the roll-up, the rename and edit, or the
   `set_by_hand` marker. Restore the entries under `[Unreleased]` before the next
   release; the project rules require a CHANGELOG entry for behaviour that ships.
2. **Non-POST requests to `/api/plans/migrate` now get a different response.** The
   feature registered the route as `POST /api/plans/migrate`. Main had no method on
   the route and the handler answered any other method with a JSON 405 error. With
   the method on the route, the Go mux answers first with its own plain-text 405
   and an `Allow: POST` header. The handler still refuses other methods. Check any
   client that parses the JSON 405 body. Keeping the handler's JSON response would
   mean removing the method from the route.
3. **The roll-up writes by default.** `datawatch plans rollup` and `POST
   /api/plans/rollup` apply the changes unless you pass `--dry-run` or
   `dry_run=true`. The daily run rolls up in the background as well. Confirm that
   the default is the one you want for the CLI and chat forms.
4. **Mobile parity.** The PWA gained Rename, Edit goal, the roll-up and the
   `set_by_hand` badge. The project's mobile-parity rule needs a `datawatch-app`
   issue for that change. This review did not check whether one was filed. The
   plan records how-to states that Android and iPhone do not show plan records yet.
5. **Open decisions in the how-to.** The how-to's *Still for the operator to
   review* section lists decisions that are the operator's to make. Nothing in this
   merge settles them.
6. **Guard test.** The guard test's commit said verification had not been run. It
   passes now (see *Checks run in this review*). Keep it in the suite; it is the
   only check that fails if the roll-up or rename change is lost in a later merge.

## Documentation status after this review

The operator-facing reference for the behaviour described above is
[the plan records how-to](../howto/plan-records.md), with the matching entries in
[the definitions glossary](../datawatch-definitions.md#plan-records). The glossary
gained a *Set-by-hand plan* entry in this pass; the how-to gained a pointer back to
the *Operator must review* list here. Item 1 above (the missing CHANGELOG entries)
was re-checked: `CHANGELOG.md` still has no roll-up, rename, edit or `set_by_hand`
entry under `[Unreleased]`. That file is outside this documentation pass and is
left for the operator.

## Parity surface

| Surface | Status | Reason |
|---|---|---|
| REST | Touched | `GET`/`POST /api/plans/rollup`, `PATCH /api/plans/{ref}`, `POST /api/plans/migrate` (method on the route; see item 2). |
| MCP | Touched | `plan_rollup`, `plan_rename`, `plan_edit`, with the golden tool-name file updated. |
| CLI | Touched | `datawatch plans rollup`, `rename`, `edit --goal`. |
| Comm channel | Touched | `autonomous plan rollup`, `plan rename`, `plan edit`. |
| YAML/config | Excluded | No new configuration key. The roll-up follows the existing `autonomous.plan_records.enabled` switch. |
| PWA | Touched | Rename, Edit goal, the roll-up and the set-by-hand badge in the Plans browser. |
| Android | Excluded | Not changed in this merge; plan records are not shown on Android yet (operator item 4). |
| iPhone | Excluded | Not changed in this merge; plan records are not shown on iPhone yet (operator item 4). |

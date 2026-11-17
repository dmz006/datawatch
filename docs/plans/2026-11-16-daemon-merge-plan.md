# Plan: the daemon does the merging

Status: agreed direction, 2026-11-16. Operator, quoted: "yes just daemon merge but shadow it and make sure it has no bugs".

The supervisor merges finished Automata by hand with the chain scripts today. The daemon has its own merge pipeline and a standing loop job, `run_merge_until_conflict`, that would do the same. This plan moves the work to the daemon in shadow first, with exit criteria, and keeps the supervisor as the reviewer.

## Where we stand (evidence from the stored shadow plans, 85 of them)

| Finding | Count | What it means |
|---|---:|---|
| Release size `none` and version unset | every plan | the plan cannot say which bump the merge would make |
| `changelog_missing` | 85 of 85 | the plan takes changelog text only from an exact entry shape and never finds one |
| `conflict_unresolved` | 168 | conflicts the daemon did not resolve; the supervisor resolves these by hand today |
| `gate_fail` | 40 | gates that failed in the throwaway trial merge |
| shadow comparisons or defect candidates written | 0 | the daemon never compared its plan with a real merge, so nothing proves it matches |

Other facts that shape the plan:

- The loop job `run_merge_until_conflict` is a real merge job, on by default. It failed every minute with "working tree is not clean" because of a stray root-owned folder in the main checkout, so it never merged. It is now **off**, and the folder is locally ignored. Do not turn it back on until the exit criteria below hold.
- The built-in pipeline has steps the supervisor's process does not use or must not use: `tag-release`, `push-release` and `deploy-latest`. Development builds are not tagged (a tag is an explicit release decision), and CI, not a manual step, makes releases.
- The supervisor's process runs lint, build, the full Go tests, the web tests and the merge gates before it ships, ships by fast-forward only, never tags, and records a ship only when the branch is on main. The built-in pipeline has none of those test gates.
- Many finished Automata carry unmerged commits (the board lists them). Landing the clean ones through the supervisor's chain is the first source of comparison data.

## Phases

### Phase 0 — safe state (done 2026-11-16)

Real merge job off. Stray folder ignored locally. Shadow plans keep being written when an Automaton finishes. The supervisor's chain records each real merge.

### Phase 1 — make the plan complete

Defects to fix on pi, each verified by the supervisor, in this order:

1. **Release size and version.** Find out why every plan reads `none`: whether the Automaton's kind or release size is unset on those records, or the plan step never reads them. A plan must name the bump (a bug is a patch, a feature a minor) and the next version.
2. **Changelog.** Have the plan read the Automaton's own changelog draft (`docs/plans/drafts/<id>-changelog.md`), the same source the supervisor's chain uses, or make the changelog task emit the exact shape the plan wants. Either way a plan carries real text.
3. **Comparison written.** After a merge lands and the shipped record is written, store the comparison of plan against landed state so it can be read.
4. **Conflicts.** Resolve conflicts through the configured policy (`additive_then_model`, the Sonnet conflict model) only when Claude is open; otherwise record `conflict_unresolved` and leave the merge to the supervisor.

Exit: a plan for a freshly finished Automaton names its size, version and changelog text, and a comparison exists after each real merge.

### Phase 2 — make the pipeline match our process

Record a pipeline for this project (`merge-pipeline set-record`) that matches the supervisor's process:

- no `tag-release` (a release tag stays an explicit decision), and no push of anything but main;
- lint, build, the full Go tests, the web tests and the merge gates before the ship step;
- the AGENT.md revert step stays, and fast-forward-only shipping;
- `deploy-latest` stays separate, under the existing deploy-after-ship job.

Exit: the pipeline shown by `merge-pipeline show` equals the supervisor's steps, and `validate` passes.

### Phase 3 — shadow period

The supervisor keeps merging by hand. For every merge the daemon's plan and comparison are read.

Exit criteria, all of them:

- ten merges in a row with no **major** difference (a wrong version, a missing changelog, a README or registry row landing nowhere, a gate absent from the manual record);
- no `stray_root_file`, `unmerged_task_branch` or `review_stale` finding that the supervisor had to correct;
- every difference graded and either fixed or accepted with a written reason.

### Phase 4 — promote in steps

1. Turn the job on for **patch-size** Automata only, with the first five conflict resolutions confirmed by a person (`conflict_confirm_first_n`).
2. After five clean daemon merges, turn it on for minor features.
3. The supervisor reviews each result for another ten merges, then stops reviewing every one.

Stop and fall back to manual merging at the first failed gate, wrong version, tag or push that was not asked for.

## What the stale unlanded work does for this plan

The board lists the finished Automata whose commits never reached main. The operator said, quoted, "yes on the stale ones": land the ones that merge cleanly and bring only the real conflicts. Each clean one lands through the supervisor's chain and produces a plan-versus-landed comparison, so landing the backlog and proving the daemon's merge are the same work.

## Roles

- The daemon: writes the plan, resolves what its policy allows, merges when promoted.
- The supervisor: merges by hand until Phase 4, reads every comparison, fixes plan defects, owns the rollback.
- The operator: accepts this plan, decides the promotion, and records keep or drop for work that will not land.

# [sync] fix(merge): release size and approval modes not reachable from any config surface (Part of finishing review and merge by the daemon)

**Filed 2026-11-09 as dmz006/datawatch-app#421, one consolidated issue for the merge lock, validator and merge run drafts** (shipped in v9.118.0).

Parent work: "Part of finishing review and merge by the daemon (Phase 4 of the
autonomy handover plan). Operator, 2026-11-07, quoted:". This draft captures the
parity work that is left open, or closed, by the release-version step of the daemon
merge pipeline: the release size and next-version function, the version step in the
built-in pipeline, and the run record that carries the shipped version. This draft is
a file only: nothing has been filed on any tracker, and no hosting CLI is run for it.

## Affected area

The daemon-side merge pipeline step that stamps a release version onto a finished
Automaton's merged work, and everywhere that version is supposed to be seen or
decided:

- `internal/mergepipeline/release_version.go` and
  `internal/mergepipeline/release_version_test.go` — the release size
  (patch, minor, major) and the next-version function that turns main's version
  into the version the run releases.
- `internal/mergepipeline/definition.go` — the step kinds and built-in action
  names, including `set_release_version`.
- `internal/mergepipeline/default.go` and `internal/mergepipeline/default_test.go`
  — the built-in pipeline, where the `set-release-version` step sits between
  `merge-branches` and `insert-changelog-heading`.
- `internal/mergepipeline/run.go` and the version tests there — the Runner's
  `setReleaseVersion` step: it reads main's version after the rebase and merge,
  computes the next version, writes it to every version file, and stores it once on
  the run; the later steps (changelog, commit, tag, push) read it back from the run.
- `internal/autonomous/merge_pipeline_run.go` — the run record, which carries the
  shipped version as `release_version` and is returned by `GET /api/autonomous/merge`.

## Current behavior

- **The version is computed at merge time, after the rebase and the merge.** The
  `set-release-version` step reads main's version as it stands at that moment,
  picks the next version, and writes it to every version file the project named in
  `autonomous.merge_pipeline.version_files`. With no version files named, that step
  fails instead of guessing at a path. The step runs exactly once per run; running
  it again for the same run is refused. A pipeline with no such step has no run
  version, and the later steps read main's version as it stands, as before.
- **The size is always a patch, and there is no way to say otherwise.** The Runner
  carries a release size, and the next-version function understands patch, minor
  and major, but the size is never set anywhere: there is no config key in
  `autonomous.merge_pipeline` or anywhere else, no field on the run request, and no
  per-project form. Every daemon-run release therefore raises the patch number. The
  size types and the minor/major paths of the function exist, are unit-tested, and
  are unreachable from an operator.
- **The shipped version is read-only on REST and invisible on the text surfaces.**
  `GET /api/autonomous/merge` returns `release_version` per run in
  `pipeline_runs`. The shared text renderer used by the CLI, the MCP
  `autonomous_merge` read and the comm channel renders the merge-pending list, the
  held lock and the shadow decisions, but not the run list at all, so the shipped
  version is not stated by those surfaces. No PWA control reads `release_version`
  either (checked by search of the PWA code, not by a live check). The howto and
  the definitions text do state where the version comes from, which is correct for
  what ships.
- **The branch side of the rule is closed by this work.** A branch that set a
  version of its own is refused before anything merges, release tasks may not name
  a fixed version number, and a push that would rewrite the remote main is refused
  — the release version is taken at merge time as the next patch above main, in
  every case the daemon can reach today.

## Expected behavior

- **The release size should be settable per the handover plan, or the gap recorded
  as an exclusion with a reason.** Phase 4 of the autonomy handover plan resolves
  the release size in layers, most specific first: an explicit size on the
  Automaton, its arc when the arc is a milestone, the project's kind-to-size map,
  then the built-in default — with a separate approval-mode setting per size
  (patch, minor, major) that keeps today's behaviour as its defaults. None of those
  layers is reachable yet; the only size in force is the patched-in default. Until
  the layer work ships, the parity position is "patch only, by design of this
  first step", and that should be said where the version is documented rather than
  implied.
- **Every surface that shows a merge run should show the version the run shipped, in
  the same words.** `GET /api/autonomous/merge` has it; the CLI
  `datawatch autonomous merge`, the MCP `autonomous_merge` read, the comm verb and
  the PWA view of the run history should render each run with its
  `release_version` when present, so an operator reading any one of them sees the
  number that actually shipped.
- **The size vocabulary and the default should be stated identically on the
  surfaces that do talk about versioning.** The howto, the definitions entry, the
  config reference and the step's own result text all say "the next patch above
  main" right now, which is true of the shipped behaviour and should stay true —
  the rule to keep is that wherever the default is named, it names the same
  default the Runner actually uses, and nowhere claims a minor or major release is
  settable by the operator before that is actually true.

## Parity surface

| Surface | Status | Note |
|---|---|---|
| REST | Included | `GET /api/autonomous/merge` returns `release_version` per run. `POST /api/autonomous/merge/run` accepts no size. |
| MCP | Partly included | `autonomous_merge` reads the same object, whose description mentions the run list but not the shipped version; no tool sets a size. |
| CLI | Partly included | `datawatch autonomous merge` renders state through the shared text renderer, which does not render the run list or the shipped version; no verb sets a size. |
| comm channel | Partly included | `autonomous merge` uses the same renderer; no verb sets a size. |
| YAML/config | Partly included | `autonomous.merge_pipeline.version_files` is settable on every settings channel; the release size is not a key anywhere. |
| PWA | Partly included | The Merge pipeline card documents the steps and the version source; no run panel or size control was found in the PWA code. To be confirmed live before filing. |
| Android | Not verified | Not checked in this draft. The datawatch-app issue must state its own status. |
| iPhone | Not verified | Not checked in this draft. Same as Android. |

## Suggested next step

File the datawatch-app parity issue from this draft, asking for two things: a
run-history presentation on mobile and PWA that shows each run's shipped version
with the same wording the REST field carries, and the Android and iPhone rows above
completed with an included/excluded verdict per surface. On the datawatch side, the
decision that belongs to the operator is which of the handover plan's release-size
layers and approval modes ship next, in what order — the code already carries the
size types and the next-version function for all three sizes, so that work is a
settings and resolution-layer addition, not a rewrite. Until that decision lands,
every surface that names the default should keep naming "the next patch above main"
as it does, and the text renderer should gain the run list with the shipped version
so the CLI, MCP and comm channels stop silently omitting the one number an operator
most wants to see after a merge.

Verification still to do before this issue is filed: a live check of the PWA
Automation view for any run panel and of one completed daemon merge run reading
`pipeline_runs` on `GET /api/autonomous/merge`, both against a sandbox daemon run
with `env -u TMUX -u TMUX_TMPDIR`. The unit tests in the files above cover the
next-version function and the once-per-run storage of the version, but not any of
the rendering gaps named here.

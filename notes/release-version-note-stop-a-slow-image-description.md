# Release version note — operator's records

**Task (template `version_bump_left_to_merge`):** record the release version of
"Stop a slow image description from costing the operator an extra Enter. Found
2026-11-08 right after the vision describe."

## The note

The release version of this change is the **next patch above main, resolved at
merge time**. It is **never a fixed number written into the tree**: this note
performs no edit and leaves the version fields exactly as they are.

Concretely:

- The branch does **not** change the version. `cmd/datawatch/main.go`
  (`var Version = "9.112.3"` at the time of writing) and
  `internal/server/api.go` (`var Version = "9.112.3"`) stay untouched, and no
  `## vX.Y.Z` heading is added to `CHANGELOG.md` on the branch.
- When the branch reaches main, `scripts/merge-branches-bump-version.sh`
  performs the version step: it picks the next patch above the then-current
  main version and records it in a single bump commit covering every branch
  merged in that run (`scripts/set-release-version.sh` is the writer).
  `scripts/check-merge-version.sh` guards the expected next patch before each
  merge, and names the expected version on refusal.
- Therefore the concrete number is deliberately left open here: it is defined
  at merge time by what main is at, not by this branch. If main is `9.112.3`
  when this merges, the resolved version is `9.112.4` — but that is a
  projection, not a commitment; any higher main moves the bump.

**Classification:** this is a note, not an edit — no file other than this note
was written, and no version field was changed.

*Recorded 2026-11-08.*

# Release version note (operator's records)

Task: **Fix the built-in task templates so a decomposed Automaton's final gate and version note tasks pass the verifier instead** (template `version_bump_left_to_merge`)

**Note:** The release version of this task is **the next patch above main, resolved at merge time**. It is **never a fixed number written into the tree**.

That is how this project's branch-versioning works: the branch version on a feature branch is deliberately left to merge — `scripts/merge-branches-bump-version.sh` computes the next patch above `main` at merge time and `scripts/check-merge-version.sh` gates it. Nothing in this task's tree records a release number, and none should.

This entry is a **note, not an edit**: it writes no version fields and the version fields in the tree (`cmd/datawatch/main.go`, `internal/server/api.go`) are left exactly as they are (v9.112.1 as of head `bc5b868`, matching the top released entry in `CHANGELOG.md`).

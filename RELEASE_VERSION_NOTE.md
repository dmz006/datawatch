# Release Version Note

**Story:** Show each system tag once in the Sessions tag menu. Found 2026-11-08 from the operator's phone screenshot: the Tags menu

**Recorded:** 2026-11-08

## Policy (template: version_bump_left_to_merge)

The release version for this story is **the next patch above main, resolved at merge time**.

- It is never a fixed number written into the tree.
- This is a note, not an edit: no version field in any file is modified by this record.
- The concrete version number (e.g. vX.Y.Z) is determined when the merge commit lands on `main`, following the repository's standard version-bump procedure.

## Scope of this note

- Operator's records only.
- Does not create, modify, or delete any file other than this note.
- Leaves all version fields (`cmd/datawatch/main.go`, `internal/server/api.go`, `CHANGELOG.md`, `README.md`, etc.) exactly as they are.

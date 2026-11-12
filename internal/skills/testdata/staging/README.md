# Staging

Author staging for the community registry. Skills written here are drafts
waiting to be published to the community registry.

This folder is not embedded in the binary and not pinned to any registry
revision. It may be empty.

Each subfolder is one skill and must hold:

- `SKILL.md` with frontmatter naming the skill (equal to the folder name),
  a description and a version
- `USAGE.md` with plain usage notes
- `tests/cases.yaml` with at least three cases

The staging tests in `internal/skills/staging_skills_test.go` check the
format and scan every file for content that must not leave the project.

Currently staged: `session-lifecycle` and `durable-session-notes`. The gap
ranking report that would name further skills is not in the repository, so no
other skills have been added; the notes are recorded in `session-lifecycle/USAGE.md`.

# Automaton templates

The standard-tasks pack ships the built-in Automaton templates that a fresh
story starts with. Operators reference templates by id; an operator template
with the same id replaces the built-in at its original position.

| Template id | Title | Notes |
|---|---|---|
| `changelog_entry` | Changelog entry | Writes one entry into the changelog. |
| `parity_issue_draft` | Parity issue draft | Writes an issue-draft file under `docs/plans/parity-issues/`; never runs a hosting CLI. |
| `docs_set` | Docs set | Brings the affected howto, operations and glossary entries into shape. |
| `bounded_load_step` | Bounded load-step rule | Posture note: `nice -n 19`, a quarter of the cores, a hard timeout, a trap on EXIT INT TERM; writes no files. |
| `final_gate` | Final gate | Read-only verdict; its only write is the report file under `.datawatch/reports/`. |
| `version_bump_left_to_merge` | Release version note | Note only: the release version is the next patch above main, resolved at merge time; never edits a version field. |

This pack folder is a fixture for tests only: the real standard-tasks pack
is published in the community registry repository (operator decision,
2026-11-09), and nothing under it is embedded into the binary.

# Publishing staged skills

Staged skills are drafts in `internal/skills/testdata/staging/`. Publishing one means copying its
folder into the community registry. This is a **separate step, approved by an operator**. An
Automaton never publishes a skill, never pushes to the registry and never opens a pull request
for one. Automata stop at the staged folder.

## Before you publish

1. Run `go test ./internal/skills/ -run TestStagingSkills`. It must pass, which means each folder
   has `SKILL.md`, `USAGE.md` and `tests/cases.yaml` with at least three cases, and no file
   contains content that must not leave the project.
2. Read each file yourself. The test is a floor, not a review.
3. Run `bash scripts/skills-coverage-check.sh` and note which matrix rows name the skill.

## Publish

1. Clone the community registry and create a branch.
2. Copy one staged folder into the registry as `<name>/`. Copy `SKILL.md`, `USAGE.md` and
   `tests/`. Keep the folder name equal to the `name` in the frontmatter.
3. Open a pull request on the registry and wait for it to merge.

## After it is published

1. Run `skills_registry_available` (or open the Skills page) and confirm the skill is listed.
2. Replace the names in `docs/skills/registry-skills.txt` with the registry's current list.
3. Update the matching rows in `docs/skills/coverage-matrix.yaml` (skills, status, note).
4. Remove the staged folder, or leave it if the registry copy is still being revised. Update
   `internal/skills/testdata/staging/README.md` so the list of staged skills stays true.
5. Run `bash scripts/skills-coverage-check.sh`.

See [`../howto/skills-find-install-write.md`](../howto/skills-find-install-write.md) for writing a
skill and reading the matrix.

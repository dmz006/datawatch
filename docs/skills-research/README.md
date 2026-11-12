# Skills research

This folder is meant to hold the skills research documents: the skills catalog, skill specs, plugin proposals and integration notes.

## Source unreachable

The research input folder, the sibling working folder next to this repository at `../datawatch-skills/docs/catalog`, could not be read from this sandbox. It does not exist at that path. Nothing was invented or reconstructed.

## Missing files

All four documents are missing, so each file below holds only a heading and a pointer to this README:

- `catalog.md`: would hold the skills catalog.
- `skill-specs.md`: would hold the skill specs.
- `plugin-proposals.md`: would hold the plugin proposals.
- `integration-notes.md`: would hold the integration notes.

## Left out

Nothing was left out for privacy reasons, because no source content was read.

## Next step

Make the source folder readable from the sandbox, then rerun this task to copy, clean and leak-check the real content.

## Skills coverage data

The mapping of every datawatch feature to the community skills that cover it lives in three files, separate from the research documents above:

- `docs/skills/inventory.yaml`: the feature inventory. Each entry has an `id`, an `area`, a `summary`, and the `tools`, `howtos`, `routes`, `commands` and `settings` it covers, plus its `sources`.
- `docs/skills/coverage-matrix.yaml`: one row per inventory entry with the covering `skills`, a `status` (`none` or `partial`) and a `note`. Most areas currently have no covering skill; the rows with `partial` status are sessions, automata-core, federation, messaging, schedules, web-app and rest-and-mcp.
- `scripts/skills-coverage-extract.sh`: prints the current surface names so the inventory can be checked against code and docs. Run `scripts/skills-coverage-extract.sh <mcp-tools|howtos|routes|commands|settings>`. Set `ROOT` to point it at another checkout.

## For the operator to review

- The four research documents are still placeholders, so the skills catalog, skill specs, plugin proposals and integration notes are not available. The matrix was built from the inventory alone, not from them.
- Skill names in the matrix come from the registry as read when the matrix was written. Re-check them before relying on a `partial` or `none` status.

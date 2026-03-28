# AGENT.md — datawatch Guardrails

This file defines operating rules for Claude when working on the **datawatch codebase itself**.
For session-level guardrails (rules for each claude-code session launched by the daemon), see
`templates/session-CLAUDE.md`.

---

## Scope Constraints

- Work only within the `datawatch` repository directory.
- Do not read, write, or execute files outside this repository unless explicitly instructed.
- Do not modify system files, install packages, or change system configuration without user confirmation.

## Code Quality Rules

- All Go code must be syntactically valid and compile with `go build ./...`.
- All new packages must have a `doc.go` or package-level comment explaining purpose.
- The `SignalBackend` and `LLMBackend` interfaces must remain stable — changes are breaking.
- Do not remove existing API endpoints or change their signatures without a major version bump.
- All new config fields must have a corresponding entry in `docs/implementation.md`.

## Git Discipline

- Every logical change gets its own commit with a conventional commit message.
- Format: `type(scope): description` — e.g. `feat(session): add rate-limit retry logic`
- Types: `feat`, `fix`, `docs`, `chore`, `refactor`, `test`
- Do not squash history. Each commit should be meaningful and reversible.
- Do not force-push to `main`.

## Versioning

**Every commit that is pushed must include a patch version bump unless the user explicitly
designates it a minor or major release.**

- The version string lives in two places — keep them in sync:
  - `cmd/datawatch/main.go`: `var Version = "X.Y.Z"`
  - `internal/server/api.go`: `var Version = "X.Y.Z"`
- **Patch bump** (default for all pushes): increment the third number — `0.1.2` → `0.1.3`
- **Minor bump** (new features / non-breaking additions, explicit user request): increment the
  second number and reset patch — `0.1.3` → `0.2.0`
- **Major bump** (breaking changes, explicit user request): increment the first number and reset
  minor/patch — `0.2.0` → `1.0.0`
- Include the new version in the commit message subject, e.g. `v0.1.3: fix signal groupID filter`
- Never push without bumping the version — a version mismatch between the binary and the running
  daemon causes confusing support issues.

## Dependency Rules

- Do not add new Go module dependencies without noting them in `CHANGELOG.md`.
- Prefer standard library over third-party for simple tasks.
- All new dependencies must be compatible with the Polyform Noncommercial license.

## Planning Rules

When creating a large implementation plan (3+ files or non-trivial architectural work):

1. **Create a plan document** in `docs/plans/` named `YYYY-MM-DD-<slug>.md`.
2. The plan must include:
   - **Date** (ISO 8601) at the top
   - **Version** — the datawatch version at the time of planning (e.g. `v0.5.19`)
   - **Scope** — which files/packages are affected
   - **Phases** — numbered steps in implementation order
   - **Status** — mark each phase as Planned / In Progress / Done as work proceeds
3. After implementation, update the plan's status and note the **version it shipped in**.
4. Plans saved in `/home/dmz/.claude/plans/` are session-local; for durable record keeping,
   copy or symlink them to `docs/plans/` before committing.

## Documentation Rules

- **Every commit must include documentation updates.** A commit that adds or changes
  behavior without updating the relevant `docs/` file(s) is incomplete.
- **Before committing**, review all changed files and update every doc that describes
  affected commands, config fields, API endpoints, or behaviors. If uncertain, err on
  the side of updating docs.
- Every new feature must have a corresponding entry in `docs/`.
- Every new CLI command or API endpoint must be documented in `docs/commands.md` or
  `docs/api/openapi.yaml`.
- Every new config field must appear in the configuration block in `docs/operations.md`
  and `README.md` in addition to `docs/implementation.md`.
- Update `CHANGELOG.md` under `[Unreleased]` for every change.
- Keep `docs/README.md` up to date — add a row for every new doc file created.

### New LLM backend

When adding a new LLM backend (`internal/llm/backends/<name>/`):

1. Add a full section to `docs/llm-backends.md` covering: prerequisites, installation,
   config block, how it runs (the exact command launched), and any notable caveats.
2. In the new section, document:
   - **Interactive input support** (yes/no) — whether `send <id>: <msg>` works
   - **Output filter compatibility** — whether the filter engine can watch its output
   - **Saved command compatibility** — whether scheduled/saved commands work
   - **Session completion detection** — what output pattern signals the session is done
3. Update the summary table in `docs/backends.md`.
4. Add config fields to `internal/config/config.go` and document them in
   `docs/implementation.md`.
5. Update the config example in `README.md`.

### New messaging backend

When adding a new messaging backend (`internal/messaging/backends/<name>/`):

1. Add a full section to `docs/messaging-backends.md` covering: prerequisites, setup
   steps, config block, how it works (inbound/outbound/bidirectional), and notes.
2. Update the summary table in `docs/backends.md`.
3. Add config fields to `internal/config/config.go` and document them in
   `docs/implementation.md`.
4. If the backend has an uninstall step (e.g. removing credentials), add it to
   `docs/uninstall.md`.

### New MCP tool

When adding a new MCP tool to `internal/mcp/server.go`:

1. Document it in `docs/mcp.md` under **Available Tools** with a parameter table and
   example response.
2. Update the tools table in `docs/cursor-mcp.md`.

### New messaging/communication interface or backend

When adding any new communication interface (messaging backend, covert channel,
notification sink, or transport):

1. Add a row to `docs/testing-tracker.md` with Tested=No, Validated=No, and a note
   explaining the current status (e.g. "planned", "not implemented yet").
2. Add a data flow section to `docs/data-flow.md` (or the relevant backend doc) showing
   the full message path: inbound → router → session manager → response → outbound.
3. In the backend's setup section (in `docs/messaging-backends.md`, `docs/llm-backends.md`,
   or the relevant doc) document:
   - Every field stored in `config.yaml` with type and default
   - Which fields are **sensitive** (masked in `/api/config`, never logged): tokens, passwords,
     secrets, API keys, phone numbers
   - Available **security options**: TLS, HMAC signatures, bearer tokens, IP allowlists, etc.
4. Add a **"Supported Commands"** section (for bidirectional backends) or **"Notification Events"**
   section (for outbound-only backends) to the backend's documentation, listing:
   - For bidirectional: every datawatch command supported, with any limitations (e.g. message
     length, no setup wizard for Signal, etc.)
   - For outbound-only: which events trigger notifications and the notification format
   - For inbound-only: which trigger events start sessions and how the payload is parsed
   - Reference `docs/commands.md` for the full command syntax
5. Update `docs/backends.md` summary table and the relevant detailed doc.

### New install method or platform

When adding support for a new install method or platform:

1. Add the corresponding uninstall steps to `docs/uninstall.md`.
2. Add a row to the installation section of `README.md`.

## BACKLOG.md Discipline

- **When a bug or backlog item is fully implemented and verified**, remove it from `BACKLOG.md`.
  Do not leave stale "completed" entries — the file should only contain open/pending work.
- **Partially fixed items** should be updated in place with a note describing what remains.
- After removing completed bugs, add corresponding entries to `CHANGELOG.md`.

## Release Discipline

**Every release must include pre-built binaries** via GoReleaser for all supported platforms.

- Supported platforms: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64
- Create a release by tagging the commit and running GoReleaser:
  ```bash
  git tag vX.Y.Z
  git push origin vX.Y.Z
  make release          # runs goreleaser release --clean
  ```
- Use `make release-snapshot` to test the release build locally without publishing.
- The GoReleaser configuration is in `.goreleaser.yaml` at the repo root.
- Release notes are derived from `CHANGELOG.md` — keep it current.
- **Before any commit or release**, check for open GitHub PRs:
  ```bash
  gh pr list --state open
  ```
  If any PRs target `docs/testing-tracker.md`, review and squash-merge them before
  proceeding:
  ```bash
  gh pr merge <PR_NUMBER> --squash --delete-branch
  ```

### Functional Change Checklist

**After any functional change** (new feature, bug fix, behavioral change — not docs-only):

1. **Bump the version** per the Versioning rules above (patch bump minimum).
2. **Build a release binary**: run `make release-snapshot` to verify the build succeeds
   locally, then `make release` (after tagging) to publish.
3. **Verify the upgrade path**:
   - Confirm `datawatch update --check` reports the new version once the release is published.
   - Confirm `datawatch update` can install it (`go install` from the published tag).
   - Test the install script: `bash install/install.sh` should download the new prebuilt
     binary without falling back to a source build.
4. To check whether an upgrade is available at any time:
   - CLI: `datawatch update --check`
   - Any messaging backend: send `update check` to the configured channel

## Rate Limit Handling

- If Claude hits an API rate limit or quota, **do not stop or fail**. Wait for the limit to reset
  and then continue from where you left off.
- When paused for rate limiting: write a `PAUSED.md` note in the current working context explaining
  what was in progress, so work can resume cleanly.
- Signal the user about the pause and estimated resume time if known.

## Security Rules

- Never log or commit API keys, tokens, phone numbers, or passwords.
- Never write code that sends data to external services not already in the design.
- The `server.token` config value must never appear in logs.

## Session Management Rules

- Never kill a session without user confirmation (except via explicit `kill` command).
- Session state transitions must always be recorded in the session's `timeline.md`.
- If the project directory is a git repo, always commit changes before and after a session.

---

*These guardrails apply when Claude operates on this repository. They do not restrict what
users can instruct datawatch sessions to do within their own project directories.*

# AGENT.md — datawatch Guardrails

This file defines operating rules for Claude when working on the **datawatch codebase itself**.
For session-level guardrails (rules for each claude-code session launched by the daemon), see
`templates/session-CLAUDE.md`.

---

## Pre-Execution Rule

Before executing any user prompt that involves code changes, new features, or bug fixes:

1. **Re-read AGENT.md rules** relevant to the task (planning, documentation, versioning, testing, etc.)
2. **Verify compliance** — ensure the planned approach follows all applicable rules
3. **Flag conflicts** — if the prompt conflicts with a rule, notify the user before proceeding

This ensures rules are not forgotten over long sessions as context compresses.

## Session Safety

- **Never stop, kill, or delete running user sessions** unless explicitly asked.
- When debugging or testing, create new sessions instead of interfering with existing ones.
- When restarting the daemon, preserve all active tmux sessions (they survive daemon restarts).

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
- All code should have as close to 100% code coverage for testing and tests should not be skeletons but functionally, where possible, fully testing the code

## Testing Tracker Rules

The testing tracker (`docs/testing-tracker.md`) must include **two levels of validation** for every interface:

1. **Unit/integration tests** (Go `_test.go` files): Protocol correctness, encode/decode round-trips,
   error handling, edge cases. These run via `go test` and are automated.

2. **Live connection tests**: Actually start the interface, connect a real or simulated client,
   send a command, verify the response appears in a session or output. Document what was tested,
   how, and what was observed. Mark "Validated" only when a live test has been performed.

- **Tested=Yes** means Go unit/integration tests exist and pass.
- **Validated=Yes** means a human or automated end-to-end test confirmed the interface works
  with a real connection (e.g., Signal message sent and received, DNS query answered,
  WebSocket session output streamed, web UI interaction observed).
- The "Test Conditions" column must describe the actual environment (versions, hosts, config).
- The "Notes" column must describe what was observed and any limitations discovered.
- Do not mark Validated=Yes based solely on unit tests — a live connection must be confirmed.

## Git Discipline

- Every logical change gets its own commit with a conventional commit message.
- Format: `type(scope): description` — e.g. `feat(session): add rate-limit retry logic`
- Types: `feat`, `fix`, `docs`, `chore`, `refactor`, `test`
- Do not squash history. Each commit should be meaningful and reversible.
- Do not force-push to `main`.

## Versioning

**Version bump rules:**
- **Every completed feature** (new plan delivered, new backend, new command) = **minor bump**
  (e.g. `0.7.4` → `0.8.0`) unless the user designates it as a major release.
- **Bug fixes, docs, refactors, config changes** = **patch bump** (e.g. `0.7.4` → `0.7.5`).
- **Breaking changes** (user must explicitly request) = **major bump** (e.g. `0.7.4` → `1.0.0`).

- The version string lives in **two places** — they MUST be updated together in every commit:
  - `cmd/datawatch/main.go`: `var Version = "X.Y.Z"`
  - `internal/server/api.go`: `var Version = "X.Y.Z"`
- **Pre-commit version check** — before every `git commit`, verify BOTH files have the NEW version.
  A common failure mode is editing one file but not the other, or editing neither because the
  version bump was forgotten during a rapid fix cycle. Search for `var Version =` in both files
  and confirm they match and are incremented from the last release.
- **Never reuse a version** — if v0.14.3 is already pushed/released, the next commit must be
  v0.14.4 or higher. Amending a pushed commit with `--force-with-lease` to fix a version is
  acceptable only if no release was created for the old version.
- **Running daemon check** — after `go build` + install, verify the binary version matches:
  `datawatch version` should show the new version BEFORE restarting the daemon.
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
- **Planned items** must be in recommended priority order with a comment explaining why.
  Each entry must link to its plan document in `docs/plans/` with an effort estimate.
- **Bugs** must be prioritized by severity with section headers and comments:
  - **Critical** — sessions not visible, stuck, or data loss. Fix first.
  - **High** — incorrect UI behavior, wrong data shown. Fix second.
  - **Medium** — config gaps, missing configurability. Fix third.
  - **Low** — UI polish, cosmetic. Fix last.
  Each bug must have a one-line description of what it affects (`— *Affects: ...*`).
- **When processing the backlog** (at the start of a session or when explicitly asked):
  1. Remove all completed items (verify they shipped)
  2. Update status/details on partially complete items
  3. Re-evaluate bug priorities and reorder if impact has changed
  4. Verify planned items are in recommended order with rationale
  5. Ensure every planned item has a corresponding plan document
  6. Move newly identified work to the appropriate section (bugs, planned, backlog)
  7. When a plan is created for a backlog item, move it from `# backlog` to `# planned`
     with a link to the plan doc. Do not leave duplicates in both sections.

## Release Discipline

**Every version that is pushed MUST have a corresponding GitHub release with pre-built binaries.**
This is non-negotiable — the install script and `datawatch update` both depend on release assets.

- Supported platforms: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64
- Binary naming convention: `datawatch-{os}-{arch}` (e.g. `datawatch-linux-amd64`)
- **Release workflow** (must be run after every version bump):
  ```bash
  # 1. Tag the version
  git tag vX.Y.Z
  git push origin vX.Y.Z

  # 2. Build cross-platform binaries
  make cross

  # 3. Create GitHub release with binaries attached
  gh release create vX.Y.Z \
    ./bin/datawatch-linux-amd64 \
    ./bin/datawatch-linux-arm64 \
    ./bin/datawatch-darwin-amd64 \
    ./bin/datawatch-darwin-arm64 \
    ./bin/datawatch-windows-amd64.exe \
    --title "vX.Y.Z" \
    --notes "Release notes here"
  ```
- If GoReleaser is available, `make release` can be used instead (produces tar.gz archives).
  The install script handles both raw binaries and GoReleaser archives.
- **Never push a version bump without creating the release.** A version without a release
  breaks `datawatch update` and the install script for all users.
- **Before any commit or release**, check for open GitHub PRs:
  ```bash
  gh pr list --state open
  ```

### Functional Change Checklist

**After any functional change** (new feature, bug fix, behavioral change — not docs-only):

1. **Bump the version** per the Versioning rules above (patch bump minimum).
2. **Build and release**: run `make cross` then `gh release create` with all binaries.
3. **Verify the upgrade path**:
   - Confirm `datawatch update --check` reports the new version once the release is published.
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

## Architecture & Documentation Index Rules

When adding a new connection type, LLM backend, messaging backend, or major feature:

1. **Update the architecture diagram** in `README.md` to reflect the new component
   and its connections (messaging path, MCP path, session path, etc.)
2. **Update the documentation index** in both `README.md` and `docs/README.md`
   with links to any new documentation files
3. **Add a row to `docs/testing-tracker.md`** for the new interface
4. These updates must be included in the same commit as the feature

## Bug Testing Documentation

Before closing any bug from the BACKLOG:
1. **Document the test** in `docs/bug-testing.md` with: test description, steps, code verified, result (PASS/FAIL)
2. **If the test failed**, document the fix and mark as "retest needed"
3. **Browser-dependent fixes** (JavaScript, CSS) must note "needs browser validation" if not tested live
4. **API tests** should include the actual curl command and response
5. **Never close a bug without a documented test result**

## Detection Pattern Governance

- **No hardcoded patterns** — all prompt, completion, rate-limit, and input-needed patterns
  must be in `config.DefaultDetection()` (config.go) and editable via Settings → Detection Filters.
- When adding a new LLM backend or pattern, add it to `DefaultDetection()` defaults, never to
  `manager.go` directly. Users can override patterns in their config.yaml or per-LLM detection config.

## Decision Making

When faced with a design or implementation decision where no existing rule in this file
covers how to proceed:

1. **Do not guess.** Ask the user for input before proceeding.
2. **After the user decides**, add the decision as a rule to the relevant section of this
   `AGENT.md` file so the same question never needs to be asked twice.
3. Examples of decisions that require asking:
   - Whether to auto-accept or require manual user interaction for prompts
   - Whether to add new dependencies vs. implement from scratch
   - UX choices that affect user workflow (e.g. modal vs. inline, disabled vs. hidden)
   - Architectural trade-offs with no clear winner

This rule itself was added because the agent auto-accepted claude consent prompts without
asking — the user wanted manual acceptance via tmux.

## Configuration Rules

- **Every config option must appear in the web UI Settings** under the General Configuration
  card (or the relevant section). If you add a new config field to `config.go`, you must also:
  1. Add it to the GET response in `handleGetConfig` (api.go)
  2. Add it to the PUT handler in `applyConfigPatch` (api.go)
  3. Add it to `GENERAL_CONFIG_FIELDS` in app.js so it's editable in Settings
- **Config fields should be grouped by function** in both `config.go` (struct comments) and
  the web UI (section headers in `GENERAL_CONFIG_FIELDS`).

---

## Work Tracking

When a request involves more than one distinct task or fix, you MUST:

1. **Before starting work**, output a plan summary as a checklist:
   ```
   ## Plan
   - [ ] Task 1 description
   - [ ] Task 2 description
   - [ ] Task 3 description
   ```

2. **As each task is completed**, re-display the checklist with updated status:
   ```
   ## Plan
   - [x] Task 1 description
   - [~] Task 2 description (in progress)
   - [ ] Task 3 description
   ```

3. Use these status markers:
   - `[ ]` — not started
   - `[~]` — in progress
   - `[x]` — completed

4. Always show the updated plan before beginning the next task so progress is visible.
5. For single-task requests this is not required — just do the work directly.

---

*These guardrails apply when Claude operates on this repository. They do not restrict what
users can instruct datawatch sessions to do within their own project directories.*

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

Every commit that adds or changes behavior must include documentation updates. Failure to
update docs is a blocking issue — do not merge/push without them.

### No internal tracker IDs in user-facing docs

Internal tracking IDs (B1, B23, F4, F11, F16, BL7, etc.) must **never** appear in
user-facing documentation. These IDs are internal-only and live exclusively in
`docs/plans/README.md` (the backlog tracker) and `docs/plans/*.md` (plan files).

**Files that must NOT contain tracker IDs:**
- `CHANGELOG.md`, `README.md`, `docs/setup.md`, `docs/operations.md`
- `docs/messaging-backends.md`, `docs/llm-backends.md`, `docs/config-reference.yaml`
- `docs/encryption.md`, `docs/architecture.md`, `docs/data-flow.md`
- All files under `docs/flow/`, and any other user-facing doc

**Files where tracker IDs ARE allowed:**
- `docs/plans/README.md` (backlog tracker)
- `docs/plans/*.md` (plan documents)

When referencing a feature or bug in user-facing docs, use a plain English description
instead (e.g. "multi-user access control feature" not "BL7"). GitHub release notes
follow the same rule.

### General documentation checklist (every change)

1. Update `CHANGELOG.md` under `[Unreleased]` (or current version)
2. Update `docs/config-reference.yaml` for any new config fields
3. Update `docs/operations.md` if the change affects deployment, security, or configuration
4. Update `README.md` if adding a new interface, command, or user-visible feature
5. Update the **documentation index** in both `README.md` and `docs/README.md` for any new doc files
6. Update `docs/testing-tracker.md` for any new interface or backend
7. Verify no internal tracker IDs leaked into user-facing docs (see rule above)

### New LLM backend (`internal/llm/backends/<name>/`)

1. Add full section to `docs/llm-backends.md`: prerequisites, installation, config, command
   launched, interactive input support, output filter compatibility, session completion detection
2. Add config fields to `config.go`, `docs/config-reference.yaml`, and `docs/implementation.md`
3. Update `docs/backends.md` summary table and config example in `README.md`
4. Check [RTK support matrix](docs/plans/2026-03-30-rtk-integration.md); if supported, add hook config to setup wizard

### New messaging backend (`internal/messaging/backends/<name>/`)

1. Add full section to `docs/messaging-backends.md`: prerequisites, setup, config, how it works
   (inbound/outbound/bidirectional), supported commands, sensitive fields, security options
2. Add data flow section to `docs/data-flow.md` showing the full message path
3. Add config fields to `config.go`, `docs/config-reference.yaml`, and `docs/implementation.md`
4. Update `docs/backends.md` summary table
5. If the backend has uninstall steps, add to `docs/uninstall.md`

### New MCP tool (`internal/mcp/server.go`)

1. Document in `docs/mcp.md` under **Available Tools** with parameter table and example
2. Update `docs/cursor-mcp.md` tools table

### New install method or platform

1. Add uninstall steps to `docs/uninstall.md`
2. Add a row to the installation section of `README.md`

## Project Tracking (docs/plans/README.md)

All bugs, plans, and backlog items are tracked in `docs/plans/README.md` — the single source of truth.

- **When a bug or backlog item is fully implemented and verified**, move it to the Completed section.
- **Partially fixed items** should be updated in place with a note describing what remains.
- After completing items, add corresponding entries to `CHANGELOG.md`.
- **When processing the tracker** (at the start of a session or when explicitly asked):
  1. Move all completed items to the Completed section
  2. Update status/details on partially complete items
  3. Re-evaluate bug priorities
  4. Verify planned items are in recommended order with rationale
  5. Ensure every planned item has a corresponding plan document
  6. Move newly identified work to the appropriate section (bugs, planned, backlog)
  7. When a plan is created for a backlog item, move it from `# backlog` to `# planned`
     with a link to the plan doc. Do not leave duplicates in both sections.
  8. When a backlog item (BL) is completed, move it to the **Completed Backlog** table
     with a note about which feature it was promoted to and the version it shipped in.
     Remove the struck-through row from the active backlog table — keep the backlog clean.
  9. Keep only ONE completed bugs table — do not split across multiple tables.
     All completed bugs go in the single `## Completed Bugs (archived)` section.

## Release Discipline

**Every version that is pushed MUST have a corresponding GitHub release with pre-built
binaries attached.** This is non-negotiable — the install script (`install/install.sh`)
and `datawatch update` both download binaries from release assets. A release without
binaries forces users to build from source.

### Required binary assets

Every release must include these 5 binaries:

| Platform | Asset name |
|----------|-----------|
| Linux x86_64 | `datawatch-linux-amd64` |
| Linux ARM64 | `datawatch-linux-arm64` |
| macOS x86_64 | `datawatch-darwin-amd64` |
| macOS ARM64 | `datawatch-darwin-arm64` |
| Windows x86_64 | `datawatch-windows-amd64.exe` |

### Release workflow (must be followed for every version bump)

```bash
# 1. Bump version in BOTH files (they must match)
#    cmd/datawatch/main.go:   var Version = "X.Y.Z"
#    internal/server/api.go:  var Version = "X.Y.Z"

# 2. Commit, tag, push
git add -A && git commit -m "release: vX.Y.Z — description"
git tag -a vX.Y.Z -m "vX.Y.Z"
git push && git push --tags

# 3. Cross-compile (Makefile reads version from main.go automatically)
make cross

# 4. Create release with binaries attached
gh release create vX.Y.Z \
  ./bin/datawatch-linux-amd64 \
  ./bin/datawatch-linux-arm64 \
  ./bin/datawatch-darwin-amd64 \
  ./bin/datawatch-darwin-arm64 \
  ./bin/datawatch-windows-amd64.exe \
  --title "vX.Y.Z — description" \
  --notes-file /tmp/release-notes.md \
  --verify-tag

# 5. Verify: the install script should download the binary, not build from source
```

### Common mistakes to avoid

- **Forgetting `make cross`** — creating a release without binaries. The install script
  will fall back to source build, which most users don't have Go installed for.
- **Stale Makefile version** — the Makefile extracts version from `main.go` via shell.
  If you hardcode the version in the Makefile, cross-compiled binaries get the wrong version.
- **Creating release before pushing tag** — use `--verify-tag` to ensure the tag exists.
- **Forgetting one of the two Version vars** — `cmd/datawatch/main.go` AND
  `internal/server/api.go` must both be updated.

### Functional Change Checklist

**After any functional change** (new feature, bug fix, behavioral change — not docs-only):

1. **Bump the version** per the Versioning rules above (patch bump minimum).
2. **Build and test**: `make cross` then `go test ./...`
3. **Create release**: `gh release create` with all 5 binaries attached.
4. **Verify the upgrade path**:
   - Confirm `datawatch update --check` reports the new version.
   - The install script should download the prebuilt binary, not fall back to source.

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

## Testing Requirements

When implementing any new feature or bug fix:

1. **Write tests** — Go `_test.go` files with close to 100% code coverage for new/changed logic
2. **Run all tests** — `go test ./...` must pass before committing
3. **Test all interfaces** — validate through every applicable access method:
   - **API**: execute `curl` commands against the running daemon
   - **Web UI**: verify via Chrome browser automation (settings cards render, stats display)
   - **CLI**: verify `datawatch` commands produce expected output
   - **Comm channel**: use `POST /api/test/message` to simulate messaging commands
   - **Config**: PUT via API, verify GET reflects change, verify web UI shows it
   - **WebSocket**: for WS message changes, verify the message is received
   - **MCP**: if MCP tools affected, verify via channel or test client
4. **Clean up test sessions** — if testing creates sessions, stop and delete them
5. **Document results** in `docs/plans/README.md` testing section and `docs/testing.md`

### Bug testing

Before closing any bug, document in `docs/testing.md` with: test description, steps,
expected result, actual result (PASS/FAIL). API tests should include actual curl commands
and responses. Browser-dependent fixes must include user validation steps.

## User Input Tracking During Active Work

When the user sends additional messages while actively working:

1. **Immediately note the input** — add to task tracking or create a sub-task
2. **Do not ignore** — acknowledge and note when it will be handled
3. **Update the plan** — add user's input as a new item
4. **Design decisions** — ask the user before proceeding with choices

## RTK Integration

When adding a new LLM backend:
1. Check [RTK support matrix](docs/plans/2026-03-30-rtk-integration.md) for compatibility
2. If RTK supports the backend, add hook configuration to the setup wizard
3. Test RTK integration with the new backend and update the support matrix
4. Document any RTK-specific configuration in the backend's docs

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

## Feature Documentation: All Access Methods

**Every new feature with configuration or user-facing behavior MUST document ALL five access
methods.** Documentation that only shows YAML or API is incomplete. Users access datawatch
through different interfaces and each must be covered.

When documenting a feature in `docs/setup.md`, `docs/operations.md`, or any user-facing doc,
include a table like this:

```markdown
| Method | How |
|--------|-----|
| **YAML** | Edit `~/.datawatch/config.yaml` → `section:` (details) |
| **CLI** | `datawatch setup <feature>` or `datawatch <command>` |
| **Web UI** | Settings tab → Section → **Card Name** |
| **REST API** | `PUT /api/config` with `{"key": value}` or specific endpoint |
| **Comm channel** | `configure key=value` or specific chat command |
```

The five methods are:
1. **YAML config** — the config file field name and section
2. **CLI** — the `datawatch` CLI command (setup wizard, subcommand, or flag)
3. **Web UI** — exact navigation path: which tab, which section, which card name
4. **REST API** — the endpoint, method, and example request body
5. **Comm channel** — the chat command (`configure`, `setup`, or feature-specific command)

If a method does not apply (e.g. no CLI command exists for a feature), explicitly state "N/A"
or omit that row — but the default expectation is that all five are supported.

**For monitoring/stats features**, also include a "Where to see" table:

```markdown
| Location | What you see |
|----------|-------------|
| **Web UI** | Settings → Monitor → Card Name (details) |
| **REST API** | `GET /api/stats` → field names |
| **Comm channel** | `stats` command output |
| **Prometheus** | `GET /metrics` → metric names |
```

This rule applies to:
- New features in `docs/setup.md`
- Feature sections in `docs/operations.md`
- New backend setup guides in `docs/messaging-backends.md` and `docs/llm-backends.md`
- Config additions in `docs/config-reference.yaml`

**Testing access methods**: Use `POST /api/test/message` to verify comm channel commands
work for any new feature before marking it complete.

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

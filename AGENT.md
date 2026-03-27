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

## Dependency Rules

- Do not add new Go module dependencies without noting them in `CHANGELOG.md`.
- Prefer standard library over third-party for simple tasks.
- All new dependencies must be compatible with the Polyform Noncommercial license.

## Documentation Rules

- **Every PR must include documentation updates.** A PR that adds or changes behavior
  without updating the relevant `docs/` file(s) must not be merged.
- Every new feature must have a corresponding entry in `docs/`.
- Every new CLI command or API endpoint must be documented in `docs/commands.md` or
  `docs/api/openapi.yaml`.
- Update `CHANGELOG.md` under `[Unreleased]` for every change.
- Keep `docs/README.md` up to date — add a row for every new doc file created.

### New LLM backend

When adding a new LLM backend (`internal/llm/backends/<name>/`):

1. Add a full section to `docs/llm-backends.md` covering: prerequisites, installation,
   config block, how it runs (the exact command launched), and any notable caveats.
2. Update the summary table in `docs/backends.md`.
3. Add config fields to `internal/config/config.go` and document them in
   `docs/implementation.md`.
4. Update the config example in `README.md`.

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

### New install method or platform

When adding support for a new install method or platform:

1. Add the corresponding uninstall steps to `docs/uninstall.md`.
2. Add a row to the installation section of `README.md`.

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

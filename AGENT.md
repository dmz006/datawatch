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
4. Plans saved in `~/.claude/plans/` are session-local; for durable record keeping,
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

- **Never reuse bug (B#), backlog (BL#), or feature (F#) numbers.** Each number is permanent,
  even after completion. Always increment to the next unused number. Before creating a new item,
  check the highest existing number in that category.
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

## Release vs Patch Discipline

**User terminology determines the action:**

- **"release"** or **"gh release"** = full GitHub release: run all tests, bump version,
  update CHANGELOG with ALL changes since last GH release, cross-compile binaries (`make cross`),
  create GH release with comprehensive notes + binaries, build + restart daemon.
- **"patch"**, **"commit"**, **"push"**, or **no explicit keyword** = commit and push only.
  Bump version, commit, push to main. Do NOT create a GH release or cross-compile binaries.
- GH release notes must cover ALL changes since the previous GH release tag (not just the latest commit).
  Check `gh release list --limit 1` for the last release tag before writing notes.

### Binary-build cadence (patch vs minor / major)

- **Patch releases (X.Y.Z where Z > 0)** — still build the **host-arch binary**
  (`make build`) so the local daemon can be installed + restarted with the new
  code; do **not** run `make cross` and do **not** attach cross-compiled binary
  assets to the GitHub release.
- **Minor releases (X.Y.0)** and **Major releases (X.0.0)** — full `make cross`,
  all five binary assets attached to the GitHub release.
- This keeps patch ship-time tight (the cross build adds ~2-3 minutes) and avoids
  GitHub release-asset upload churn for cosmetic / docs-only patches, while
  still letting `datawatch restart` pick up the patch on the dev workstation.

### Release-discipline rules (referenced from `docs/plans/README.md`)

These rules apply to **every release commit** (patch or minor/major), and live here
so they survive the backlog file's refactors:

- **README.md must reflect the current release.** Every release commit updates the
  `**Current release: vX.Y.Z (DATE).**` line at the top of `/README.md` and refreshes
  the "Highlights since vN.0.0" bullets if anything notable shipped. The marquee is a
  project signpost; staleness here is a worse impression than staleness in the backlog.
- **Backlog refactor each release.** Every release commit also touches
  `docs/plans/README.md`: clear `## Unclassified` into BL### entries, mark just-shipped
  items as `✅ Closed in vX.Y.Z` and move them under the closed section, and confirm
  the open table only has actually-open work.
- **Embedded docs must be current at binary build time** (added v5.23.0). The
  embedded PWA docs viewer reads from `internal/server/web/docs/` which is mirrored
  from the canonical `docs/` tree by `make sync-docs`. The Makefile's `build` and
  `cross` targets depend on `sync-docs` so this is automatic for `make cross` /
  `make build`. The release rule: never `go build ./cmd/datawatch/` directly when
  preparing a release binary — always go through `make cross` (cross-arch) or
  `make build` (host-arch) so the embedded docs match the shipped binary's
  release-notes references.
- **Asset retention** (refined v5.25.0). To keep the GH releases page navigable +
  GHCR storage bounded, the keep-set is:
  1. Every **major** release (X.0.0) — kept indefinitely.
  2. The **latest minor** (highest X.Y.0 with Y >= 1) — kept until superseded.
  3. The **latest patch on the latest minor** (highest X.Y.Z with Z > 0 where X.Y
     matches the latest minor) — kept until superseded.

  Everything else gets release-page binary attachments deleted; release notes
  themselves stay forever. Container images on GHCR follow the same keep-set
  pattern; cleanup needs a separate token with `read:packages + delete:packages`
  scope. The script `scripts/delete-past-minor-assets.sh` is idempotent — re-run
  on every release as part of the post-`gh release create` step.

  Operators upgrading from a deleted-asset release fall through to the
  current-release binary via `datawatch update`.

### Major release alias refresh (v5.27.5)

Every **major** release (X.0.0) must refresh the hardcoded LLM alias /
model lists against the upstream provider's current set:

- `internal/server/api.go` `handleClaudeModels` — refresh the
  `aliases` and `full_names` slices to match Anthropic's current
  alias map (currently `opus` / `sonnet` / `haiku` plus full
  names like `claude-opus-4-7` / `claude-sonnet-4-6` / `claude-haiku-4-5-…`).
  Same drill for any future provider that lands a similar
  hardcoded-list endpoint.

The Anthropic `/v1/models` query was deferred (BL206 frozen, operator
decision 2026-04-29). Major-release refresh is the forcing function
that keeps the hardcoded list current. Mid-cycle minor / patch
releases do not need to refresh — the operator always has the
"custom..." free-text input as escape hatch in the PWA + can pass
any full model name on the CLI.

Add an entry to the major release notes' *Container images* section
(or a new *LLM aliases* section) listing what changed.

### Container maintenance

Every release must audit the container product surface (Dockerfiles in
`docker/dockerfiles/` + the Helm chart in `charts/datawatch/`) and decide per-image
whether a rebuild/retag is needed. Daemon-behavior changes require rebuilding
`parent-full`. Agent/validator image changes require rebuilding the relevant
`agent-*` or `validator` image. Helm chart changes require bumping `Chart.yaml`
`version` (chart SemVer) AND `appVersion` (datawatch tag). Document the image-delta
per release in the release notes under a `## Container images` section. No silent
image drift allowed.

**GitHub release requirements (when explicitly requested):**

### Required binary assets

Every release must include these 5 binaries:

| Platform | Asset name |
|----------|-----------|
| Linux x86_64 | `datawatch-linux-amd64` |
| Linux ARM64 | `datawatch-linux-arm64` |
| macOS x86_64 | `datawatch-darwin-amd64` |
| macOS ARM64 | `datawatch-darwin-arm64` |
| Windows x86_64 | `datawatch-windows-amd64.exe` |

### Pre-release dependency audit

Before every release, run a dependency audit:

```bash
# 1. List outdated dependencies
go list -m -u all 2>/dev/null | grep '\[' | head -30

# 2. For each outdated module, check its release date:
#    - Only upgrade if the new version has been available for >= 72 hours
#    - This avoids being an early adopter of broken releases
#    - Exception: if the user explicitly requests an upgrade, do it immediately
#    - Exception: security patches (CVEs) may be upgraded immediately

# 3. Upgrade stable dependencies
go get -u <module>@<version>

# 4. Tidy and verify
go mod tidy
go test ./...
```

**Rules:**
- Do NOT upgrade a dependency that was released less than 72 hours ago unless
  the user specifically asks for it or it fixes a known CVE
- Run `go mod tidy` after any upgrade to clean up go.sum
- If an upgrade breaks tests, revert it and note the incompatibility
- Document dependency upgrades in the commit message

### Pre-release security scan (gosec)

Before every release, run a security scan:

```bash
# Install gosec if not present
go install github.com/securego/gosec/v2/cmd/gosec@latest

# Run scan (reads exclusions from .gosec-exclude)
EXCLUDE=$(grep -v '^#' .gosec-exclude | tr '\n' ',' | sed 's/,$//')
~/go/bin/gosec -exclude="$EXCLUDE" -fmt text -quiet ./... 2>&1 | tail -20

# Or JSON for parsing
EXCLUDE=$(grep -v '^#' .gosec-exclude | tr '\n' ',' | sed 's/,$//')
~/go/bin/gosec -exclude="$EXCLUDE" -fmt json -quiet ./... 2>/dev/null | python3 -c "
import sys,json; d=json.load(sys.stdin)
issues = d.get('Issues',[])
from collections import Counter
rules = Counter()
for i in issues:
    rules[i.get('rule_id','?') + ': ' + i.get('details','')[:50]] += 1
for rule, count in rules.most_common():
    print(f'  {count:3d}x {rule}')
"
```

**Global suppressions** are in `.gosec-exclude` (one rule ID per line, not inline annotations):
- **G104 (unhandled errors)** — suppressed globally. Most are fire-and-forget tmux
  send-keys, log writes, and `//nolint:errcheck` already covers the intent. Edit
  `.gosec-exclude` to add/remove global exclusions.

**Per-finding rules:**
- **HIGH severity findings** must be reviewed and either fixed or documented with justification
- **G204 (subprocess with variable)** — expected for tmux, signal-cli, whisper, LLM backends
- **G704 (SSRF)** — expected for proxy mode (forwarding to configured remote servers)
- **G702 (command injection)** — expected for daemon restart and tmux session management
- **G304 (file inclusion)** — expected for config/data file operations on admin-configured paths
- **G112 (Slowloris)** — must fix: add `ReadHeaderTimeout` to all HTTP servers
- **G401/G505 (weak crypto)** — SHA1 for non-security ID generation is acceptable; document justification
- New findings in code you wrote MUST be addressed before release

## Configuration Accessibility Rule

**No configuration may EVER be hard-coded.** Every configurable value MUST be settable
at runtime without editing code or restarting. Every feature with configurable options
MUST have its configuration accessible through ALL of these channels:

1. **YAML** — field in `config.yaml` with annotated comment in config template
2. **Web UI** — toggle/field in the appropriate Settings tab (General/LLM/Comms)
3. **REST API** — readable via `GET /api/config`, writable via `PUT /api/config`
4. **Comm channel** — `configure <key>=<value>` from Signal/Telegram/Slack/etc.
5. **MCP** — if the feature has a stats or status tool, expose via MCP
6. **CLI** — `datawatch config set <key> <value>` if applicable

The `handleGetConfig` map in `api.go` and the `handlePutConfig` switch in `api.go`
MUST include all new config fields. The web UI `GENERAL_CONFIG_FIELDS`,
`LLM_CONFIG_FIELDS`, or `COMMS_CONFIG_FIELDS` arrays MUST include the field.

**Before marking a feature complete**, verify the config value round-trips:
```
PUT /api/config {"key":"feature.setting","value":true}
GET /api/config → verify feature.setting = true
configure feature.setting=true → verify response
```

## Localization Rule (BL214, v5.28.0)

**Every user-facing string MUST be added to localization, and every localization
addition MUST be mirrored to datawatch-app.** The PWA loads `/locales/<lang>.json`
bundles sourced 1:1 from the datawatch-app Compose Multiplatform Android client.
Drift between PWA and Android translations is a parity violation.

**When adding/changing user-facing strings (PWA, alerts, error messages, button
labels, modal titles, menu items, etc.):**

1. **Add a key + English value to `internal/server/web/locales/en.json`** — use
   the same naming convention as Android (`nav_*`, `action_*`, `settings_*`,
   `alerts_*`, `sessions_*`, etc.). Keep keys snake_case, descriptive, and
   stable — never rename a shipped key.
2. **Add the same key to `de.json`, `es.json`, `fr.json`, `ja.json`** — when
   the Android translations don't yet have the key, ship the EN value as a
   placeholder + open a datawatch-app issue (see step 4). When Android already
   has the key, mirror its translations.
3. **Wire the string through `t(key)` or `data-i18n="<key>"`** at the call site
   in `app.js` / `index.html`. Never hardcode user-facing English in markup or
   JS string literals.
4. **File a datawatch-app issue** titled `feat(i18n): add <key>(s) for <feature>
   parity` with: (a) the new key(s) + EN values + context (which screen/UI),
   (b) parent release that introduced them, (c) request for DE/ES/FR/JA
   translations through the same Compose-Multiplatform pipeline. Link the
   issue from the parent release notes.
5. **Update the locale-parity tests** if the key list materially expands —
   `internal/server/v5280_locales_test.go::TestLocales_CommonNavKeysPresent`
   guards specific keys; add to that list when shipping new high-visibility
   strings (nav, primary actions, settings tabs).

**Iterative coverage extension** — wiring existing English-only literals through
`t()` is always safe and welcome; do it whenever you touch a section.
Strings not yet keyed render in English (the harness returns the raw key on
miss; English fallback bundle catches it). Coverage gaps are visible, not
crashing.

**Why "always file a datawatch-app issue"** — the Android client is the
source-of-truth for translations because Compose Multiplatform routes them
through real-user UX feedback. Machine-translating ad hoc on the PWA side
would diverge wording across clients. Mirror direction is parent ← mobile
for translation values, parent → mobile for new key requests.

## Skills-Awareness Rule (BL255, v6.7.0)

Skills are a first-class cross-cutting concern in datawatch. Whenever you
add or modify a session/PRD/agent/comm-channel/plugin path, ask **"does
this need a skill hook?"** before shipping.

- **Skills are extensible.** The v1 manifest has 6 datawatch-specific
  fields (`compatible_with`, `requires`, `applies_to`,
  `cost_hint`/`disk_mb`, `verify`, `provides_mcp_tools`) on top of PAI's
  base format (`name`, `description`, `version`, `tags`, `entrypoint`).
  More fields will land — the manifest parser must tolerate unknown
  fields, the registry sync must round-trip them untouched, and PWA/CLI
  surfaces must render unknown fields as `<key>: <raw value>` instead of
  hiding them.
- **PAI compatibility is non-negotiable.** Manifests authored without any
  datawatch extensions must parse cleanly. We extend the format, we don't
  fork it. If a field name conflicts with a future PAI standard, rename
  ours.
- **Cross-cutting touch-points** the skills layer must consider every time
  a feature lands:
  1. **Session spawn** — does the new session type need to resolve `Skills`
     into prompt context (option c) and/or MCP tool registration (option d)?
  2. **PRD/automaton** — does PRD planning need to surface skill availability
     in the LLM prompt (so it can recommend `set_skills` calls)?
  3. **Comm verbs** — does the new comm-channel surface need to expose
     `skills` so operators can list/sync from chat?
  4. **Plugin manifests** (BL244 v2.1) — can plugins ship skills as part of
     their manifest? When a plugin lands, the skills layer should pick up
     its declared skills automatically.
  5. **Agent containers** (F10) — does the agent image need the synced
     skill files mounted? Currently `internal/skills/resolution.go`
     handles this for local sessions; cluster/k8s spawn needs a parallel
     path.
- **Adding a new manifest field** is allowed any time and is a
  non-breaking change as long as the parser stays tolerant. Document new
  fields in `docs/skills.md` with a worked example. PAI compatibility
  audit before each minor release: clone PAI mainline and verify our
  parser still accepts every pack manifest it ships.
- **Built-in default registry** is `pai` pointing at
  `https://github.com/danielmiessler/Personal_AI_Infrastructure`. Operator
  can delete it; the `skills registry add-default` verb (every surface)
  re-creates it idempotently. Don't hard-code PAI as a special case in
  resolution code — it's just a registry that ships preconfigured.

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

### Secrets-Store Rule (BL241 design discussion 2026-05-04, project-wide)

All credential-bearing config fields (access tokens, API keys, passwords,
signing secrets, recovery keys, webhook tokens, OAuth client secrets,
etc.) must accept and prefer `${secret:name}` references resolved from
the BL242 secrets manager.

- **New backends ship secrets-store-only from day one.** The first
  backend under this rule is BL241 Matrix (v6.7.0): `matrix.access_token`,
  `matrix.application_service.as_token`, `matrix.application_service.hs_token`,
  and `matrix.recovery_key` are all `${secret:...}`-required; YAML plaintext
  is rejected at config load with a clear remediation message.
- **Existing backends** (Signal/Telegram/Slack/Discord/Ntfy/Twilio/GitHub/
  SMTP/etc.) retain their current YAML-plaintext token paths until each is
  next opened for substantive work, at which point the plaintext path is
  removed and a deprecation notice ships in the release notes for that
  backend's next minor.
- A separate backlog item (filed alongside this rule) tracks the audit +
  retroactive sweep across already-shipped backends so progress is visible
  even when no backend is being edited.
- The `${secret:name}` resolution path is the existing one in BL242 Phase 4
  (`internal/config/secrets_resolver.go`); no new resolution surface needed.
- Operator UX: `datawatch setup <backend>` for a secrets-store-only field
  prompts for the secret value, calls `secrets_set` to store it, and writes
  `${secret:<backend>-<field>}` into the config — operator never sees the
  raw token in YAML.

This rule lives here (not just in the BL241 design doc) so it survives
the design conversation and applies project-wide.

### No local-environment leaks in git

Anything tied to one operator's machine (private hostnames, internal IP
addresses, personal email, absolute home-dir paths, internal registry
URLs, kubectl context names, organisation-internal CA fingerprints,
specific Wi-Fi SSIDs, etc.) must NOT land in tracked files. This applies
to docs, plan files, config samples, Helm charts, deployment manifests,
test fixtures, and Go source comments alike.

When a value is needed to make a feature work, choose one of:

1. **Documentation example** — use IANA-reserved ranges so the example
   is obviously not someone's real network: `192.0.2.0/24`,
   `198.51.100.0/24`, `203.0.113.0/24` for IPs; `example.com` /
   `*.example.com` for hostnames; `noreply@example.com` for emails.
2. **Operator config** — the value lives in `~/.datawatch/config.yaml`
   (gitignored) or a config file the operator names; the repo only
   ships defaults / placeholders.
3. **Environment variable** — for build-time values (registries,
   credentials), define an env var read by `Makefile` or
   `cmd/datawatch/main.go`; document the override path; ship a
   `.env.<thing>.example` template, never the real `.env.<thing>`.
4. **Cluster Secret / SealedSecret / CSI** — for cluster-resolved
   values (kubeconfig, git tokens, image-pull creds), the Helm chart
   reads from a Secret the operator pre-creates; the chart itself
   never carries the values.

`.gitignore` must continue to cover `.env`, `.env.build`, `config.yaml`,
`*.kubeconfig`, and any Secret-bearing file. Before committing, grep
the diff for personal markers (your hostname, your IP range, your
email) — finding any means stop, replace, and re-check.

## Session Management Rules

- Never kill a session without user confirmation (except via explicit `kill` command).
- Session state transitions must always be recorded in the session's `timeline.md`.
- If the project directory is a git repo, always commit changes before and after a session.

## Background Shell Cleanup Rule

After every build+test+smoke cycle is complete (i.e., all background tasks have resolved and
results have been read), kill any lingering poll-watcher bash processes before finishing:

```bash
pgrep -u "$USER" bash | grep -v -E "^($$|PPID_OF_INTERACTIVE_SHELLS)$" | xargs kill 2>/dev/null || true
```

Practical steps:
1. After reading the last background task result, run `pgrep -a -u "$USER" bash` to see what's alive.
2. Kill every bash process that is a Claude Code `until`-loop watcher (identifiable by the
   `/home/.../.claude/shell-snapshots/snapshot-bash-` prefix in its command line).
3. Keep only interactive login shells (`-bash` or `bash` without a snapshot source line).
4. Run the cleanup as a single `xargs kill` — do not loop or sleep.

This prevents accumulation of dozens of stale poll processes across a long conversation.

## Memory Use Rule

datawatch ships its own episodic memory + temporal knowledge graph
+ 4-layer wake-up stack — and an MCP surface (`memory_recall`,
`memory_remember`, `kg_query`, `kg_add`, `research_sessions`,
`copy_response`, `get_prompt`) that AI sessions running ON the
project must use, not just write to. Treat memory as a first-class
collaborator: passive logging is the bare minimum; active recall +
KG cross-reference is the standard.

**At the start of every working session:**

1. `memory_recall` for the area you're about to touch (e.g.
   "F10 spawn token broker", "session reconnect bug", "memory
   federation modes"). Look for prior decisions, abandoned
   approaches, and known gotchas before re-deriving them.
2. `kg_query` for the entities involved (people, modules,
   profiles, clusters). Check existing relationships before
   declaring new ones.
3. For multi-session work, `research_sessions` for cross-session
   context that wouldn't surface in the current project's
   memory alone.

**During work, write back the non-obvious:**

- `memory_remember` for: irreversible decisions, traded-off
  approaches and *why* the trade fell that way, surprising
  failure modes, performance characteristics that aren't in the
  code's comments. Skip the trivial — comments + commit messages
  cover what the code does; memory is for what the code *would
  have done* if not for X.
- `kg_add` for: subj-pred-obj triples that capture relationships
  worth querying later (e.g. `BL96 — depends_on — Sprint7-orchestrator`,
  `auth.TokenBroker — owns — audit.jsonl`). Anchor with a
  `valid_from` date so temporal queries work.
- Both are also reachable via every comm channel (`remember:` /
  `recall:` / `kg add` / `kg query`) so they participate in
  human-in-the-loop workflows, not just AI sessions.

**Built-in memory hooks the daemon emits — use rather than
re-implement:**

| Hook | Code path | Fires when |
|------|-----------|-----------|
| auto-save every N exchanges | `internal/session/manager.go` (Claude Code hook) | every Nth assistant turn — captures running context for free |
| pre-compact save | same | before Claude's context-compact, so summary windows preserve detail |
| session-end summary | `internal/memory/retriever.go SaveSessionSummary` | terminal-state callback registered in main.go |
| session-output chunking | BL52 — `internal/memory.SaveOutputChunks` | tail of session output indexed for granular search |
| F10 worker memory federation | `internal/agents/client.go` BootstrapMemory | per-Project-Profile namespace + sync-back / shared / ephemeral mode |
| wake-up stack | `internal/memory/layers.go` | every session start — L0 identity + L1 critical facts loaded automatically |

When extending datawatch, **wire into the existing hook surface**
before adding a new sink. The four-layer wake-up + dedup + WAL +
encryption + namespace + cross-project tunnels are all in place;
new features should slot into them, not parallel them. F10
follow-ups BL96 (wake-up extension for recursive agents), BL97
(per-agent diaries), BL98 (contradiction detection), BL99
(closets/drawers) all extend rather than replace.

**After context compaction or history compaction:** when an AI
session has its prompt history compressed (Claude Code's `/compact`,
a model-side context-window summarization, or a post-compaction
resume), the first action on the next turn must be a memory pass —
*not* "guess from the summary":

1. `memory_recall` for the in-flight feature, the file paths most
   recently touched, and any backlog ID currently in progress; the
   pre-compact auto-save hook already wrote the running context to
   memory, so it's there to pull back.
2. Walk the wake-up stack as needed (L0 identity → L1 critical
   facts → L2 recent decisions → L3 current-task summary; once
   BL96 lands, also L4 parent-context + L5 peer-agent visibility
   for spawned workers). One layer at a time, stop as soon as you
   have enough to continue without inventing — extra layers are
   only worth the tokens when the question still feels under-
   specified.
3. Re-check `kg_query` for the entities you'll mutate so you don't
   re-derive a relationship the KG already knows.
4. If anything you remembered conflicts with what you observe now,
   trust the current code/state — and update the memory rather
   than acting on stale recall (per the "Before recommending from
   memory" pattern).

This applies whether the compaction came from the model, from
`/compact`, from a session resume, or from a new session that the
operator told to "continue where the last one stopped".

**Test requirement:** every memory-emitting code path must:
1. Use `memory_remember` (not direct SQL) so dedup + WAL + KG
   auto-population fire.
2. Tag with `wing` (project) + `room` (topic) + `hall` (one of
   facts / events / discoveries / preferences / advice) — empty
   metadata makes the spatial-search +34pp accuracy gain useless.
3. For F10 worker writes, set `namespace` to the worker's
   Project Profile namespace (per S6.2's federation contract).
4. Round-trip-test that writes survive Save → Load. Apply the
   same Audit-Logging-Rule dual-output requirement when the
   memory event also has a security-relevant audit dimension.

Reference: [docs/memory.md](docs/memory.md), [docs/memory-usage-guide.md](docs/memory-usage-guide.md),
inspirations + comparisons in [docs/plan-attribution.md](docs/plan-attribution.md)
(mempalace + nightwire).

## Audit Logging Rule

Every audit-style event (security-relevant lifecycle change: spawn,
terminate, token mint/revoke, validation result, secret rotation,
auth event, config write, session-write, etc.) must be emittable in
**both** of these formats so operators can choose between in-house
pipelines and SIEM forwarding:

1. **JSON-lines** — one JSON object per line, jq-friendly. The
   default; preferred for the datawatch web UI's audit query, the
   project's own `audit.jsonl` files, and any in-house log shipper
   (Loki, OpenSearch, ELK).
2. **CEF** — ArcSight Common Event Format. Single-line, syslog-
   friendly, parsed out-of-the-box by every major SIEM (Splunk,
   QRadar, ArcSight, Sentinel, Chronicle). Required when forwarding
   to a SOC.

Reference impl: `internal/agents/audit.go` (`FileAuditor` +
`FormatCEFLine` — used by the F10 agent audit trail S8.4) +
`internal/auth/token_broker.go` (token broker's audit, also JSON-
lines; CEF mirror tracked as backlog).

CEF mapping rules:
- Header escapes: `|` and `\` only (per spec)
- Extension escapes: `=`, `\`, `\n`, `\r` (pipes are OK in extension)
- Use standard CEF keys when they fit (`rt` for timestamp, `duser`
  for actor identity, `msg` for free-form note, `dvchost` for host,
  `src` for source IP, etc.); fall back to `deviceCustomString[1-6]`
  with their `Label` companion for datawatch-specific fields
- SignatureID + Name + Severity per event class — see the inline
  `cefSignature` mapping for the canonical assignments
- Severity scale: 0-3 = informational, 4-6 = low/medium, 7-8 =
  high, 9-10 = critical

**Test requirement:** every new audit-event-emitting code path must
add (a) a JSON-lines round-trip test asserting valid JSON output and
(b) a CEF format test asserting header pipe-escaping + extension
equals/newline-escaping + the correct (signatureID, name, severity)
triple. Bad escapes break SIEM parsing and can let an attacker
inject synthetic events; treat escape-coverage as security-critical.

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

### Release testing — full functional, not just unit tests

**Operator directive 2026-04-28 (revises 2026-04-27):** smoke runs are **required on minor and major releases**, plus on the **first patch that introduces a new feature** (initial-feature testing). Subsequent patches inside the same minor that DON'T add new features can ship without a full smoke pass — a targeted run via `SMOKE_ONLY=<sections>` is appropriate when only specific sections matter. The full smoke is mandatory at every minor/major boundary so regression coverage doesn't drift across the patch window.

When in doubt, run smoke. Cost is low; coverage is the point. The autonomous decompose path silently broke in v3.10.0 because unit tests covered the manager + REST handler in isolation but never exercised the loopback together — full-smoke at the v3.11.0 cut would have caught it.

**Required for every minor/major release + first patch of a new feature:**

Run `./scripts/release-smoke.sh` against the running daemon. The script exercises:

- `/api/health` + version
- `/api/backends` shape
- `/api/stats?v=2` observer roll-up
- `/api/diagnose` battery
- `/api/channel/history` (v5.26.1)
- Autonomous PRD CRUD + cascade-aware delete
- Autonomous decompose loopback (the v3.10.0-introduced bug — explicit regression check)
- Observer peer register + push + cross-host aggregator (BL173 path)
- Memory recall
- Voice transcribe availability
- Orchestrator graph CRUD

Every release tag must include a smoke-pass note. PRs that add new operator-facing surface MUST extend `release-smoke.sh` to cover the new endpoint or the new code path before merge.

**Additional requirements for a major release (cumulative — patches inherit these too if cluster is available):**

1. **Single-host smoke** — `tests/integration/spawn_docker.sh` end-
   to-end (profile create → spawn → bootstrap → terminate → cleanup);
   bonus pass with `RUN_BOOTSTRAP=1` against a real worker image.
2. **Kubernetes smoke** — `tests/integration/spawn_k8s.sh` against a
   reachable kubectl context (operator's testing cluster, kind, k3d,
   or any cluster the maintainer has admin on). Validates: Helm
   chart installs cleanly, parent reaches `/readyz`, child Pod
   spawns + bootstraps, audit events land, terminate cleanup leaves
   no orphaned Pods. Run with the same image tag the release will
   ship.
3. **Cross-feature flows** — at least one path that exercises
   multiple newly-shipped pieces together (e.g. spawn → audit query
   shows the event → memory_recall finds the session summary →
   peer broker delivers a message between two workers). One real
   flow per sprint's worth of changes.
4. **UI smoke** — log in via web UI, walk Settings → Profiles →
   Agents cards, verify the new feature surfaces (settings, alerts,
   timeline). Browser automation OK; manual click-through OK; the
   point is "an operator could find + use this".
5. **Config-channel parity audit** — for every new config knob,
   verify it round-trips through YAML, REST `PUT/GET /api/config`,
   MCP `config_set`, comm `configure …`, and CLI flag (where one
   exists).

**Document each pass:** `docs/testing.md` gets a release-checkpoint
section per major release with: what cluster (kind/k3d/real),
chart version, observed behaviour, screenshots/log snippets where
relevant, and PASS/FAIL per feature. Failures block the release.

**Use what's available:** the operator/maintainer typically has a
real cluster in scope (`kubectl config get-contexts` will list it).
Prefer that over CI-only smoke when validating a release — real
networks, real CNIs, and real storage classes surface bugs unit
tests can't see.

## Monitoring & Observability Rule

Every new feature MUST include monitoring and observability support:

1. **Stats metrics** — add measurable fields to `SystemStats` in `internal/stats/collector.go`
   for any new subsystem (counts, sizes, status flags, durations). These appear automatically
   in the Monitor tab's real-time dashboard via the WS stats broadcast.

2. **API endpoint** — expose subsystem stats via a dedicated `GET /api/<subsystem>/stats`
   endpoint returning JSON. Add to `openapi.yaml`.

3. **MCP tool** — add a `<subsystem>_stats` MCP tool that returns the same data as the
   API endpoint, so IDE clients and automation can query it.

4. **Web UI card** — add a stats card to the Monitor tab in `renderStatsData()` showing
   key metrics (counts, status, sizes). Use the real-time WS data for live updates.

5. **Comm channel command** — if the subsystem has user-facing commands, ensure a
   `stats`/`status` variant is accessible from messaging channels.

6. **Prometheus metrics** — if the feature has numeric counters or gauges, add them to
   `internal/metrics/metrics.go` and populate in the `SetOnCollect` callback.

This ensures every feature is observable from day one — no blind spots in production.

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
  4. Add it to MCP
  5. Add it to all comms
  6. Add it to cli
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


<!-- rtk-instructions -->
# RTK (Rust Token Killer) - Token-Optimized Commands

**Always prefix commands with `rtk`**. If RTK has a dedicated filter, it uses it.
If not, it passes through unchanged. This means RTK is always safe to use.

```bash
# Always use rtk prefix, even in chains:
rtk go build && rtk go test ./...
rtk cargo build
rtk git status && rtk git diff
rtk git log
```

**Key savings:** Build 80-90%, Test 90-99%, Git 59-80%, Files 60-75%.
Run `rtk gain` to view token savings statistics.
<!-- /rtk-instructions -->
# Release Notes — v6.7.0 (BL255 Skill Registries)

Released: 2026-05-04
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.7.0
Smoke: 95 pass / 0 fail / 6 skip

## Summary

v6.7.0 is a minor feature release closing **BL255 — Skill Registries**: a new subsystem for managing skill registries (catalogs of reusable AI-session skills from external git repos), with built-in PAI default, full CRUD across all 7 operator surfaces, a connect → browse → sync flow that lets operators select individual skills before downloading, and session-spawn resolution that drops synced skills into the working directory + exposes them via a new MCP tool.

This release also lands **three new project-wide rules** in AGENT.md:
- **Skills-Awareness Rule** — skills are cross-cutting; manifest is extensible; PAI compatibility is non-negotiable.
- **GH-runner status check rule** — every release commit must check `gh run list` for failures, investigate, fix, and delete the failed run record so the runner list stays a useful signal channel.
- **Security-failure exception** — old (>72h since CVE publication) dependency security failures that are currently suppressed go to the operator for decision; CI doesn't auto-handle them.

A fourth rule, **Secrets-Store Rule** (filed in the BL241 design discussion), becomes load-bearing: BL255's `auth_secret_ref` for private-repo access rejects plaintext in YAML.

---

## BL255 — Skill Registries

### Concepts

A **skill** is a self-contained markdown-and-scripts package that influences how an AI session does its work. Skills come from **registries** (built-in PAI by default; operator can add others), get **synced** selectively into `~/.datawatch/skills/`, and **resolve** at session spawn either as files in the project working directory (option C) or via the new `skill_load` MCP tool (option D).

The format follows PAI's `SKILL.md` + YAML frontmatter convention, with **6 datawatch-specific extensions** layered on top:

| Extension | Field | Purpose |
|---|---|---|
| (a) Compatibility hints | `compatible_with: [datawatch>=6.7.0]` | Operator-visible warnings when a skill targets a newer datawatch |
| (b) Dependency declarations | `requires: [other-skill]` | Syncing one skill auto-pulls its dependencies (BL255-followup) |
| (c) Routing / applicability | `applies_to: {agents, session_types, comm_channels}` | Auto-attach only where the skill makes sense |
| (d) Resource hints | `cost_hint: low\|medium\|high` + `disk_mb: N` | PRD planning + selection UX |
| (e) Verification command | `verify: ./verify.sh` | Catches a bad sync immediately |
| (f) Built-in MCP-tool decls | `provides_mcp_tools: [tool_a, tool_b]` | When skill is synced, declared MCP tools become available |

The parser is **tolerant of unknown fields** (per the new Skills-Awareness Rule): future extensions land in an Extra map and round-trip through sync without loss. PWA / CLI surfaces render unknown fields as raw key/value rather than hiding them.

### Architecture

New `internal/skills/` package:

```
internal/skills/
  manifest.go      # YAML frontmatter parser + 6 extension fields
  store.go         # Registry list + synced index (JSON-on-disk)
  git_registry.go  # Shallow clone via git CLI; manifest discovery
  manager.go       # Connect/Browse/Sync/Unsync + PAI default
  resolution.go    # InjectSkills/EnsureSkillsIgnored/CleanupSessionSkills
                   # (BL219-aligned lifecycle hooks)
```

Storage uses **JSON-on-disk** (`~/.datawatch/skills.json`) per the project convention — only memory uses SQLite. Cache (shallow clones) at `~/.datawatch/.skills-cache/<reg>/`. Synced content at `~/.datawatch/skills/<reg>/<name>/`.

### Built-in default registry

`pai` → `https://github.com/danielmiessler/Personal_AI_Infrastructure` is preconfigured. The `add-default` verb on every surface lets the operator create / recreate it idempotently — no tombstone state.

### Resolution at session spawn (options C + D both default-on)

When a session has `Skills: ["foo", "bar"]`:

- **(C) File injection** — copy each synced skill's directory into `<projectDir>/.datawatch/skills/<name>/`. Auto-add `.datawatch/` to `.gitignore` (and `.cfignore` / `.dockerignore` when present). On session end, remove the injection if `cfg.Session.CleanupArtifactsOnEnd` is true. Mirrors the BL219 backend-artifact lifecycle.
- **(D) `skill_load` MCP tool** — agent calls `skill_load <name>` and gets the markdown returned as text. Avoids prompt bloat when many skills are configured but only one is needed at a time. Always available regardless of (C).

### Surface parity (Configuration Accessibility Rule)

Every knob is reachable from every operator surface:

| Surface | Path / Verb |
|---|---|
| YAML | `skills:` block — `registries: [...]`, `add_default_on_start`, `auto_ignore_on_session_start` |
| REST | `GET/POST/PUT/DELETE /api/skills/registries[/{name}]`, `POST /api/skills/registries/{name}/{connect,sync,unsync}`, `POST /api/skills/registries/add-default`, `GET /api/skills`, `GET /api/skills/{name}[/content]` |
| MCP | 13 tools — `skills_registry_list/get/create/update/delete/add_default/connect/available/sync/unsync`, `skills_list/get`, `skill_load` |
| CLI | `datawatch skills [registry [list/get/add/update/delete/add-default/connect/browse/sync/unsync]] [list/get/load]` |
| Comm | `skills [registry [list/get/add/update/delete/add-default/connect/browse/sync/unsync]] [get/load]` |
| PWA | **Settings → Automata → Skill Registries** card — full CRUD, per-row Connect/Browse/Edit/Delete, browse modal with checkbox selection, "+ Add default (PAI)" button, synced-skills summary list |
| Locale | 45 new `skills_*` keys × 5 bundles (en/de/es/fr/ja) with inline translations |

### Connect → Browse → Sync flow

1. **Connect** — daemon performs `git clone --depth=1` of the registry into `~/.datawatch/.skills-cache/<reg>/` and walks the tree for `SKILL.md` files. Each becomes an *available* skill.
2. **Browse** — operator inspects what's available before committing disk space. PWA shows a checkbox table; CLI: `datawatch skills registry browse <name>`.
3. **Sync** — operator picks names. Selected skills get copied into `~/.datawatch/skills/<reg>/<name>/`. Only synced skills consume real disk space + appear in resolution.

### `Session.Skills []string` field

Added to the session struct so per-session resolution works for both PRD-spawned sessions (inherits `PRD.Skills`) and operator-spawned sessions (set on `/api/sessions/start`).

---

## Project rule additions (AGENT.md)

### Skills-Awareness Rule

Skills are a first-class cross-cutting concern. Whenever a session/PRD/agent/comm-channel/plugin path is added or modified, ask "does this need a skill hook?" before shipping.

- **Skills are extensible.** Manifest parser tolerates unknown fields; registry sync round-trips them; PWA/CLI render unknown fields as raw rather than hide.
- **PAI compatibility is non-negotiable.** Manifests authored without datawatch extensions parse cleanly. We extend, we don't fork.
- **Cross-cutting touch-points** documented for each subsystem (session spawn, PRD/automaton, comm verbs, plugin manifests, agent containers).
- **Built-in default registry** is `pai`; operator can delete it; `skills registry add-default` re-creates idempotently.

### Secrets-Store Rule (load-bearing for v6.7.0)

Filed in the BL241 design discussion (2026-05-04, Round 2). Now binds: BL255's `auth_secret_ref` for private-repo access rejects plaintext in YAML — must be `${secret:name}` resolved via the BL242 secrets manager.

Implications for already-shipped backends are tracked in **BL254** (Secrets-Store Rule retroactive sweep) — operator-driven cadence; not v6.7.0-blocking.

### GH-runner status check rule

Every release commit (patch / minor / major) must:
1. Run `gh run list --limit 20` after `gh release create` lands.
2. For any failure, `gh run view <id> --log-failed` to investigate.
3. Fix the underlying cause OR document as known-environmental flake.
4. **Delete the failed run** (`gh run delete <id>`) so the runner list stays a useful signal channel.

Exception: dependency security failures from `security-scan.yaml` / `dependency-review.yaml` / `owasp-zap.yaml` / `secret-scan.yaml` get a different path when **all three** are true: failure is a real dependency CVE, the advisory is older than 72 hours, the project is currently suppressing it. Then surface to operator with name + CVE/advisory ID + publication date + suppression status. Operator owns the suppression decision; CI doesn't auto-delete.

### Cross-compilation on a GH runner — open question

Logged in AGENT.md as a question raised during this release: should `make cross` move to a GH Actions runner instead of running on the operator dev box? Tradeoffs documented (parallel compile + provenance attestation vs CI-step-in-release-path + UPX setup + macOS notarisation pain when relevant). No decision; local `make cross` remains canonical until a follow-up BL is filed.

---

## BL254 — Secrets-Store Rule retroactive sweep (filed)

Tracking BL filed in `docs/plans/README.md` for the project-wide audit + retroactive sweep across already-shipped backends (Signal, Telegram, Slack, Discord, Ntfy, Twilio, GitHub webhook, etc.) to migrate plaintext credential fields to `${secret:...}` references. Operator-driven cadence; doesn't block any release. Output deliverable: `docs/secrets-store-sweep.md` with a per-field state matrix.

---

## Documentation

- **`docs/skills.md`** — full architecture doc with mermaid diagram covering registry → cache → synced → resolution flow. Includes manifest schema with all 6 extensions documented.
- **`docs/howto/skills-sync.md`** — operator-facing first-time setup walkthrough: add default, connect, browse, sync, use in a session. Plus authoring a new skill + private-repo auth via secrets store.
- **`docs/plans/2026-05-04-bl241-matrix-design.md`** (committed earlier in the v6.7.0 dev window) — Matrix design discussion finalized through Round 3; Plan II ready for P1 implementation when operator gives the green light. Not v6.7.0 implementation but tracked in this release window.

---

## Testing

- **Unit:** new `internal/skills/` package compiles cleanly; reuses existing test patterns from `internal/profile/` JSON store tests.
- **Smoke:** new `§12. v6.7.0 BL255 — Skills registry CRUD + add-default + sync flow` section in `scripts/release-smoke.sh`. Exercises: registries endpoint reachable, `add-default` is idempotent (200), `pai` registry present after add-default, synced-list endpoint reachable. **Smoke total: 95 pass / 0 fail / 6 skip** (was 91/0/6 in v6.6.x — +4 from BL255 section).

---

## Container images

No image changes in this release — skills are a daemon-side feature; agent containers are unchanged. Helm chart `appVersion` should be bumped to `v6.7.0` when the operator next opens it (queued for the next chart-touching change).

---

## Mobile parity

datawatch-app issue **#50** filed at release cut. Lists the 45 new `skills_*` locale keys + the Settings → Automata → Skill Registries surface for mirroring on the Compose Multiplatform Android client.

---

## What's next

- **BL241 Matrix communication channel** — design doc finalized at `docs/plans/2026-05-04-bl241-matrix-design.md`. Plan II (E2EE from day one, hybrid AS+bot, single-room v1) ready for P1 implementation. ~17 working days estimated. Pending operator green-light.
- **BL254 Secrets-Store Rule retroactive sweep** — operator-driven audit + per-backend migration as each is next opened for substantive work.
- **BL246 items 1/5/6** — closed v6.6.0; if any usability issues surface, follow-ups filed as new BLs.

---

## Release-discipline retrospective (this cut)

- **Missed:** dedicated `docs/plans/RELEASE-NOTES-vX.Y.Z.md` files for v6.3.0 through v6.6.1 — the project convention has been silently dropped since v6.2.0. This file is the v6.7.0 entry; the older missing files (v6.3.0 / v6.4.0–v6.4.7 / v6.5.0–v6.5.7 / v6.6.0 / v6.6.1) can be backfilled from CHANGELOG.md if/when the operator wants. CHANGELOG.md itself is current.
- **Caught:** the post-release GH-runner-status check (now a rule in AGENT.md) immediately after the v6.7.0 tag — `containers` workflow ran clean (success), 0 failures across last 10 runs, no run records to delete.

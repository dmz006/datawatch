# How-to: Sync skills from PAI (or any git registry)

Skills are reusable markdown packages that influence how an AI session
does its work. The default registry is PAI
([danielmiessler/Personal_AI_Infrastructure](https://github.com/danielmiessler/Personal_AI_Infrastructure))
— datawatch ships preconfigured to consume it. Add your own registries
to share team / company skills across hosts.

## What it is

A **skill** is a directory with:

- `manifest.yaml` (PAI format + 6 datawatch extensions).
- One or more markdown files (`SKILL.md`, examples, sub-skills).

A **registry** is a git repo containing many skills. `datawatch
skills` syncs configured registries; resolved skills are referenced
by name from sessions, profiles, and Automatons. At session spawn
time the daemon copies the synced files into
`<projectDir>/.datawatch/skills/<name>/` so the LLM can read them.

## Base requirements

- `datawatch start` — daemon up.
- `git` on PATH (registries are git repos).
- (Optional) Network access if your registries are remote; local
  registries (file:// path) work offline.

## Setup

```sh
# PAI registry ships preconfigured + auto-added on first start.
datawatch skills registries
#  → pai     https://github.com/danielmiessler/Personal_AI_Infrastructure   (last sync: never)

# Add a custom registry.
datawatch skills registry-add \
  --name team \
  --url https://github.com/your-org/datawatch-skills \
  --branch main
datawatch reload
```

## Two happy paths

### 4a. Happy path — CLI

```sh
# 1. Sync all configured registries.
datawatch skills registry-sync
#  → syncing pai...   42 skills (3 new, 39 unchanged)
#    syncing team...  8 skills (8 new)

# 2. List synced skills.
datawatch skills list
#  → go-style                pai     code-style
#    test-first               pai     methodology
#    rtk-cli-aware           team    code-style
#    secrets-scan            pai     security
#    sast                    team    security

# 3. Inspect a skill.
datawatch skills get test-first
#  → name: test-first
#    registry: pai
#    description: Write the test before the implementation
#    instructions: |
#      ...

# 4. Use a skill in a session at spawn.
datawatch sessions start \
  --backend claude-code \
  --skills test-first,rtk-cli-aware,secrets-scan \
  --task "Add a new endpoint to /api/sessions"
# Skills get copied into <project_dir>/.datawatch/skills/<name>/
# at spawn; the LLM can read them with the same tooling it uses for
# other context.

# 5. Reference skills in a Project Profile (so every spawn against
#    that profile gets them).
$EDITOR ~/.datawatch/profiles/projects/datawatch-dev.yaml
# Add:
#   skills: [test-first, rtk-cli-aware, go-style]
```

### 4b. Happy path — PWA

1. Settings → Automate → **Skill Registries** card.
2. The PAI registry is pre-listed. Click **+ Connect** to add another.
   Modal asks for name + git URL + branch.
3. Click **Sync** next to a registry. Status spinner; on completion
   the count of synced skills updates.
4. Below the registry list, **Synced skills** lists every skill from
   every connected registry, with `tag`, `description`, and an
   inspect ▾ expander.
5. To use a skill, spawn a session via Sessions → + FAB → wizard:
   - **Advanced (collapsed)** → **Skills** → multi-select chip
     picker shows synced skills.
   - Select; **Start**. The chosen skills get installed at spawn.
6. Or attach skills to a Project Profile via Settings → Agents →
   Project Profiles → edit a profile → Skills field.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Settings → Automate → Skill Registries card. Sync flow + inspect ▾
parity. Skill picker in the new-session wizard.

### 5b. REST

```sh
# Registries.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/skills/registries
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"team","url":"https://github.com/your-org/datawatch-skills","branch":"main"}' \
  $BASE/api/skills/registries

# Sync a registry.
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  $BASE/api/skills/registries/pai/sync

# List synced skills.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/skills

# Get a skill.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/skills/test-first
```

### 5c. MCP

Tools: `skills_list`, `skills_registry_add`, `skills_registry_sync`,
`skills_get`, `skill_load`.

`skill_load` is operator-confirmed before it copies a synced skill
into a non-default workspace — useful for an LLM that wants to
opportunistically pull a skill mid-session ("I should grade this
output; let me load the grader skill").

### 5d. Comm channel

| Verb | Example |
|---|---|
| `skills list` | Returns all synced skills. |
| `skills sync` | Triggers registry sync. |
| `skill use <name>` | Sets the chat-default for next spawn. |

### 5e. YAML

Registry config at `~/.datawatch/skills-registries.yaml`:

```yaml
registries:
  - name: pai
    url: https://github.com/danielmiessler/Personal_AI_Infrastructure
    branch: main
    sync_on_start: true
    sync_interval: 24h
  - name: team
    url: https://github.com/your-org/datawatch-skills
    branch: main
    auth: ${secret:GITHUB_TOKEN}      # for private repos
```

Synced skills land at `~/.datawatch/skills/<name>/` (operator-readable
copies). Don't edit them — re-sync would overwrite. Fork the registry
or add to your team registry for permanent edits.

Per-skill manifest:

```yaml
name: test-first
description: Write the test before the implementation
tags: [methodology, testing]
instructions: |
  Before writing implementation code:
  ...

# datawatch extensions:
backends: [claude-code, opencode-acp]   # restrict to backends that benefit
prerequisites: []                        # other skills required
session_inject: true                     # auto-inject into session prompt
```

## Diagram

```
   ┌──────────────────────┐         ┌──────────────────────┐
   │ Registry (git repo)  │   ...   │ Registry (git repo)  │
   │  - skill-1/          │         │  - skill-7/          │
   │  - skill-2/          │         │  - skill-8/          │
   └──────────┬───────────┘         └──────────┬───────────┘
              │ datawatch skills registry-sync │
              └──────────────┬─────────────────┘
                             ▼
                ┌──────────────────────────┐
                │ ~/.datawatch/skills/     │ ← synced copies
                └──────────┬───────────────┘
                           │ session start --skills A,B,C
                           ▼
              ┌────────────────────────────────┐
              │ <project_dir>/.datawatch/skills/│
              │   A/  B/  C/                    │ ← per-session install
              └────────────────────────────────┘
                           │
                           ▼
                       LLM reads
```

## Common pitfalls

- **Skill not appearing in the picker after registry-add.** You forgot
  to sync. `datawatch skills registry-sync`.
- **Private registry sync fails with 401.** Set `auth: ${secret:GITHUB_TOKEN}`
  in the registry config; ensure the token has read access.
- **Skill referenced in profile but not synced.** Spawn fails with
  "skill not found". Sync first; confirm with `skills list`.
- **Editing the synced copy.** Lost on next sync. Fork the registry
  or maintain your team registry instead.
- **Skill manifest schema mismatch.** Older PAI skills predate the
  `session_inject` extension; daemon defaults to `true`. Inspect with
  `skills get <name>` to see the merged manifest.

## Linked references

- See also: [`profiles.md`](profiles.md) — Project Profiles attach skills.
- See also: [`autonomous-planning.md`](autonomous-planning.md) — PRDs reference skills per task.
- See also: [`secrets-manager.md`](secrets-manager.md) — private registry auth.
- Plan attribution: PAI Skill Registries in `../plan-attribution.md`.

## Screenshots needed (operator weekend pass)

- [ ] Settings → Automate → Skill Registries card with PAI + a custom registry
- [ ] Connect Registry modal
- [ ] Synced skills list with inspect ▾ expanded
- [ ] New-session wizard with Skills multi-select chip picker
- [ ] CLI `datawatch skills list` output

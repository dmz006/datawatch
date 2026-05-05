# How-to: Sync skills from PAI (or any git registry)

Skills are reusable markdown packages that influence how an AI session does its work. The default registry is PAI ([danielmiessler/Personal_AI_Infrastructure](https://github.com/danielmiessler/Personal_AI_Infrastructure)) — datawatch ships preconfigured to consume it.

This walkthrough takes you from "no skills configured" to "session spawned with a skill" in five steps.

## When to use this

- You want to reuse a curated body of practice across sessions (review checklists, framework-specific patterns, project boilerplate).
- You have a team-internal git repo of skill markdown files and want sessions to pick from it.
- You want to try PAI's pack catalog on a datawatch session.

## Concepts

A **registry** is a git repo containing one or more skills. A **skill** is a directory with a `SKILL.md` (or `skill.md`/`skill.yaml`) holding YAML frontmatter + a markdown body. Sync downloads a skill's content locally; PRD/session `Skills: [name]` references the synced skills.

Background: [docs/skills.md](../skills.md).

## 1. Add the default registry

The PAI registry doesn't auto-create — operator opts in. From any surface:

```bash
datawatch skills registry add-default
```

Or in a comm channel: `skills registry add-default`. Or click **+ Add default (PAI)** on the empty-state in **Settings → Automata → Skill Registries**.

Idempotent — safe to re-run any time.

## 2. Connect to the registry

Connecting performs a shallow git clone into `~/.datawatch/.skills-cache/pai/` and discovers the skills inside:

```bash
datawatch skills registry connect pai
```

Output reports how many skill manifests were discovered.

## 3. Browse what's available

```bash
datawatch skills registry browse pai
```

Returns a JSON array of available skills with `name`, `description`, `tags`, dependencies, etc. The PWA browse modal renders the same list with checkboxes.

## 4. Sync the ones you want

Pick by name:

```bash
datawatch skills registry sync pai summarize-session review-changes
```

Or all of them:

```bash
datawatch skills registry sync pai --all
```

Each sync'd skill lands at `~/.datawatch/skills/pai/<name>/`.

## 5. Use a synced skill in a session

Set on a PRD via the Settings modal in PWA, or directly:

```bash
datawatch automata prd-set-skills <prd-id> summarize-session
```

When the PRD spawns a worker session, the daemon copies the skill's files into `<projectDir>/.datawatch/skills/<name>/` (option C). The agent can also call the `skill_load <name>` MCP tool to read the markdown on demand without it being in every prompt (option D).

`.datawatch/` is auto-added to `.gitignore` so the operator's repo stays clean. When the session ends and `session.cleanup_artifacts_on_end` is true, the injected files are removed.

## Adding your own registry

```bash
datawatch skills registry add my-team https://gitea.example.com/team/skills
datawatch skills registry connect my-team
datawatch skills registry browse my-team
datawatch skills registry sync my-team my-skill
```

For private repos:

```bash
datawatch secrets set gitea-skills-token <token>
datawatch skills registry update my-team --auth-secret-ref '${secret:gitea-skills-token}'
```

Per the **Secrets-Store Rule** ([AGENT.md](../../AGENT.md#secrets-store-rule-bl241-design-discussion-2026-05-04-project-wide)), plaintext auth tokens in YAML are rejected at config load.

## Troubleshooting

| Symptom | Fix |
|---|---|
| `connect` fails with `git clone: ...` | Verify the URL works manually with `git clone --depth=1 <url>`. For private repos check the secret is set: `datawatch secrets get <name>`. |
| `browse` returns 0 available | The repo doesn't have any `SKILL.md`/`skill.md`/`skill.yaml` files at depth ≤ 6 from the root. Check the registry repo structure. |
| `sync` says "no skills matched" | Browse first to refresh the cache: `datawatch skills registry connect <n>`. Or use `--all`. |
| Synced skill doesn't appear in a session | Confirm the skill name matches `name:` in its `SKILL.md` frontmatter. The session must have `cfg.Skills.AutoIgnoreOnSessionStart=true` (default true) for `.datawatch/` to land in `.gitignore`. |

## Authoring a new skill

Create a directory with a `SKILL.md`:

```yaml
---
name: my-skill
description: One-line description.
version: 0.1.0
tags: [scope-tag]
applies_to:
  agents: [claude-code]
---

# My Skill

Markdown body — the actual instructions, examples, references the agent reads when this skill is loaded.
```

Commit to a git repo, push, then in datawatch:

```bash
datawatch skills registry add my-stuff <git-url>
datawatch skills registry connect my-stuff
datawatch skills registry sync my-stuff my-skill
```

See [docs/skills.md](../skills.md) for the full manifest schema (all 6 datawatch extensions are optional).

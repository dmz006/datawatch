---
docs:
  index: true
  topics: [sessions, memory, federation, security, claude-code]
exec_params: []
exec_steps: []
---
# How-to: Cross-Session Communication Rule

A short policy section datawatch injects into spawned claude-code
sessions' `CLAUDE.md`, steering the LLM toward datawatch's own
audited memory/discussion/reply tools instead of Claude Code's native
session-to-session messaging, for anything that should be audited or
might cross a host/container boundary.

## What it is

Claude Code's own native cross-session messaging
(`SendMessage`/`ListAgents`) only works between sessions sharing one
Claude Code install on one machine, and nothing it sends is recorded
anywhere datawatch's own audit trail can see. A future session
reading the policy may be on a different host or in a different
container than the session that wrote it — this rule exists so the
LLM reaches for datawatch's federation-aware, audited tools first for
anything durable or sensitive, while leaving ephemeral same-host
chatter alone.

The injected section covers five tools:

| Tool | Purpose |
|------|---------|
| `memory_remember` / `memory_recall` | Save/retrieve durable facts and decisions |
| `memory_discussion_write` / `memory_discussion_recall` | A shared, cross-host discussion scope, synced to registered peers |
| `discussion_subscribe` | Get new discussion entries pushed to this session instead of polling |
| `reply_to_parent` | Reply to the session that spawned this one |
| `memory_handoff` | Pass a task-completion summary to the next task in the same story |

It is **not** an absolute ban on native messaging — the policy text
itself says ephemeral, same-host coordination nobody needs to audit
or retrieve later is still fine without going through datawatch.

## Base requirements

- `datawatch` daemon running.
- Applies to **claude-code sessions only** — the policy merges into
  `CLAUDE.md` specifically; other backends get `AGENT.md` with no
  equivalent section (there's no native cross-session messaging
  concept to steer away from on those backends).

## Setup

On by default (`cross_session.enabled: true`). Disable it if you'd
rather spawned sessions not receive this guidance:

```yaml
# ~/.datawatch/datawatch.yaml
cross_session:
  enabled: false
```

Same key works from `PUT /api/config`, the MCP `config_set` tool, and
Settings → Cross-Session Communication Rule in the PWA.

## How injection works

Mirrors the existing Memory Use Rule / RTK instructions pattern
exactly — same merge logic, same file:

1. A new claude-code session starts in a project directory.
2. If that directory's `CLAUDE.md` (or `AGENT.md`, preferred if it
   exists) doesn't exist yet, the full session-guardrails template is
   written fresh — the policy section is appended if
   `cross_session.enabled` is true.
3. If the file already exists (an earlier session in the same
   project wrote it), the policy section is appended **only if not
   already present** — never duplicated on a later session's spawn.

```sh
# Confirm the policy landed in a project's CLAUDE.md.
grep -A2 "Cross-Session Communication Rule" <project_dir>/CLAUDE.md
```

## Diagram

```
  New claude-code session spawns in <project_dir>
         │
         ▼
  WriteSessionGuardrails(templatePath, sess, opts)
         │
         ├─ <project_dir>/CLAUDE.md doesn't exist yet?
         │     → write full template (memory + RTK + this policy,
         │       each included only if its own config flag is on)
         │
         └─ <project_dir>/CLAUDE.md already exists?
               → merge only what's MISSING:
                   memory section    (if memory.enabled && memory.session_awareness)
                   RTK section       (if rtk.enabled)
                   this policy       (if cross_session.enabled)
               → refresh the "# Session Guardrails" header block
                 (session ID / task) unconditionally, every write —
                 a stale header from an earlier session in the same
                 project_dir is a known, separately-fixed bug
```

## Common pitfalls

- **Policy doesn't appear in CLAUDE.md.** Confirm the session's
  backend is actually `claude-code` — every other backend writes
  `AGENT.md` with no equivalent merge step. Also confirm
  `cross_session.enabled` wasn't turned off.
- **I turned it off but it's still in an existing CLAUDE.md.**
  Disabling the config flag stops *future* sessions from adding the
  section if it's missing — it does not retroactively remove a
  section a prior session already wrote. Edit the file directly if
  you want it gone from an existing project.
- **A session ignored the policy and used native `SendMessage`
  anyway.** This is guidance text injected into the LLM's own context,
  not an enforced technical control — there's no code path that blocks
  native cross-session messaging. If a session's task genuinely needs
  cross-host coordination and it reached for the wrong tool, that's a
  model-following-instructions issue, not a config bug.

## See also

- [howto/cross-agent-memory](cross-agent-memory.md) — the Memory Use
  Rule section this mirrors.
- [howto/audit-logging](audit-logging.md) — where datawatch's own
  tool calls (not the LLM's own messages) actually land in an audit
  trail.
- [datawatch-definitions](../datawatch-definitions.md)

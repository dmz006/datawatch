# How-to: Operator identity & Telos

Tell datawatch who you are, what you're trying to accomplish, and how
you make trade-offs. Once. Every spawned session inherits the context
through the L0 layer of the wake-up stack — the LLM has your role,
goals, current focus, and constraints in front of it before it sees the
first prompt you type.

This is the single highest-leverage piece of one-time configuration in
datawatch. A 5-minute identity setup pays for itself across every
session you'll ever spawn.

## What it is

A structured operator self-description loaded from
`~/.datawatch/identity.yaml`. The schema is intentionally narrow — six
fields, each with a defined purpose:

| Field | Purpose | Example |
|---|---|---|
| `role` | What you do, day-to-day | "Platform SRE running a 3-cluster lab + 2 prod clusters for a 50-person eng org" |
| `north_star_goals` | 1–3 outcomes you want over the next quarter | "Cut on-call pages 50%; ship k8s 1.31 cluster-by-cluster; no incidents from automation" |
| `current_projects` | Active work; each can carry its own context block | "BL266 state engine debugging; v6.13.0 docs walk; cluster lifecycle review" |
| `values` | What you care about — drives trade-off resolution | "Simplicity over cleverness; audit over speed; low-toil over feature breadth" |
| `current_focus` | What the LLM should default-prioritize today | "BL266 state engine — finish the structured-event path" |
| `context_notes` | Anything else (env quirks, team norms, vocabulary) | "We use 'eq' for k8s as a verb. Our cluster names are pet-themed. Default to Go for new tools." |

The Telos additions (PAI's contribution) are the `north_star_goals` +
`values` fields. The pairing matters: goals tell the LLM what to chase,
values tell it how to choose when goals conflict.

## When to use it

- **The first time you set up datawatch.** Before you spawn your first
  serious session.
- **When projects shift** — current_focus + current_projects rot
  fastest; revisit weekly or whenever you start a new chunk of work.
- **When values drift** — annual or quarterly. Rare.
- **Never on a per-session basis** — that's what session-level
  Profiles are for. Identity is operator-scoped, not work-scoped.

## 1. Initial setup via the wizard

The PWA wizard is the easiest path:

1. PWA → Settings → Automate → **Identity** card → **Configure**.
2. (Or, from anywhere on the Automata page: click the 🤖 robot icon in
   the header.)
3. The wizard walks each field with placeholder examples and
   field-specific prompts ("What outcomes do you want over the next
   quarter?"). Each field accepts free-form text; multi-line is fine.
4. **Save**. The daemon writes `~/.datawatch/identity.yaml` and
   immediately re-injects on the next session spawn.

The wizard is also reachable from anywhere via:

```sh
datawatch identity configure
```

which runs the same prompts in the terminal.

## 2. Initial setup via YAML

If you already know what you want to write, edit the file directly:

```sh
$EDITOR ~/.datawatch/identity.yaml
```

Schema:

```yaml
role: |
  Platform SRE running a 3-cluster lab plus 2 prod clusters for a
  50-person eng org. Strong preference for boring tech.

north_star_goals:
  - Cut on-call pages 50% by end of Q3
  - Ship k8s 1.31 cluster-by-cluster, zero downtime
  - No customer-visible incidents from automation

current_projects:
  - name: BL266
    context: Channel-driven state engine for datawatch sessions.
  - name: cluster-lifecycle-review
    context: Documenting + automating the create/upgrade/decommission flow.

values:
  - Simplicity over cleverness
  - Audit over speed
  - Low-toil over feature breadth

current_focus: |
  BL266 — finish the structured-event path for opencode-acp; verify
  the gap-watcher 30s warm-up is enough.

context_notes: |
  - We use "eq" for kubectl as a verb.
  - Our clusters are pet-themed (kona / luna / pico).
  - Default new tooling to Go unless there's a strong reason not to.
```

YAML is the source of truth. Wizard edits round-trip through it.

## 3. Verify the injection actually happens

Spawn a fresh session, then check the wake-up log:

```sh
tail -50 ~/.datawatch/sessions/<session-id>/wakeup.log | grep -A40 'L0:'
```

You should see your role / goals / focus rendered into the L0 block.

Or — easier — start a session and ask the LLM "what do you know about
me?" The first response should mirror your identity fields back.

## 4. Update mid-session

Identity changes don't propagate to existing sessions (their L0 was
captured at spawn). They DO apply to every new session. To force an
existing session to pick up the new identity:

- Quick: kill it and restart with the same task spec.
- Surgical: send a chat message in the session: "use my updated identity
  context: <paste the relevant fields>". Less clean but doesn't lose
  conversation history.

## CLI reference

```sh
datawatch identity get                   # print current identity (audit-logged)
datawatch identity set --role "..." \
                       --focus "..."     # patch one or more fields
datawatch identity edit                  # opens $EDITOR on the YAML
datawatch identity configure             # interactive wizard in terminal
```

`datawatch identity set` accepts `--role`, `--focus`, `--values`
(comma-separated), `--goals` (comma-separated), `--context` (free
text). Append-only flags: `--add-project name=context`,
`--remove-project name`.

## REST / MCP

- `GET /api/identity` — returns the full identity object.
- `PATCH /api/identity` — partial update; missing fields preserved.
- `PUT /api/identity` — full replacement; fields not in payload are cleared.
- MCP tool: `identity_get`, `identity_set`, `identity_patch`.

All three surfaces are audit-logged.

## Common pitfalls

- **Too long.** A 2000-word context_notes block dilutes the L0 layer
  and pushes other content (recent memory, current task) further down
  the prompt. Aim for tight, declarative bullets. Edit ruthlessly.
- **Mixing identity with task spec.** Identity is operator-scoped. If
  you find yourself writing project-specific context that only matters
  for one work area, move it to a Project Profile instead.
- **Stale focus.** `current_focus` is the field that decays fastest.
  Set a calendar reminder to revisit weekly, or update it at the top
  of every session.
- **No goals at all.** Without goals the LLM has nothing to defer to
  when you give it ambiguous direction. Even "ship cleaner code" is
  better than empty.

## Linked references

- Plan attribution: PAI Identity / Telos in `plan-attribution.md`
- Architecture: `architecture-overview.md` § Wake-up stack
- API: `/api/identity` (Swagger UI under `/api/docs`)
- Mempalace inheritance: how identity flows into the L0 layer
- See also: `profiles.md` for per-project context

## All channels reference

Every datawatch feature is reachable through 7 surfaces. For Identity:

| Channel | How |
|---|---|
| **PWA** | Settings → Automate → Identity card → Configure (or 🤖 robot icon in Automata page header). |
| **Mobile** | Settings → Automate → Identity card → Configure (Compose Multiplatform; same flow). |
| **REST** | `GET/PUT/PATCH /api/identity` (Bearer auth). Examples in `docs/api/identity.md`. |
| **MCP** | Tools `identity_get`, `identity_set`, `identity_patch`. Hosts: claude-code MCP, Cursor, opencode-acp. |
| **CLI** | `datawatch identity {get,set,configure,edit}`. Same fields, same effect. |
| **Comm** | From any configured chat channel: `identity get`, `identity focus "..."` etc. |
| **YAML** | Edit `~/.datawatch/identity.yaml` directly; daemon picks up changes on next session spawn. |

A typical flow: configure once via PWA, edit incrementally via CLI or YAML.

## Screenshots needed (operator weekend pass)

- [ ] PWA Identity card (Settings → Automate)
- [ ] Identity Wizard modal — first step (role)
- [ ] Identity Wizard modal — completed example
- [ ] 🤖 robot icon location in Automata page header
- [ ] CLI `datawatch identity get` example output

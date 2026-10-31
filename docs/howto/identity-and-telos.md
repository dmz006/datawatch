---
docs:
  index: true
  topics: [identity, telos, onboarding]
exec_params:
  - name: role
    description: Your operator role (e.g. "platform engineer")
    required: true
  - name: north_star_goals
    description: Comma-separated north-star goals
    required: false
    default: ""
  - name: current_focus
    description: Current focus area (one sentence)
    required: false
    default: ""
exec_steps:
  - tool: get_identity
    description: Read current operator identity (no-op if unset)
    args: {}
    read_only: true
  - tool: set_identity
    description: Set role + goals + focus
    args:
      role: "{{params.role}}"
      north_star_goals: "{{params.north_star_goals}}"
      current_focus: "{{params.current_focus}}"
    read_only: false
  - tool: get_identity
    description: Confirm the identity was saved
    args: {}
    read_only: true
  - tool: identity_backups
    description: List the saved backups of the identity (read-only)
    args: {}
    read_only: true
---
# How-to: Operator identity & Telos

Tell datawatch who you are, what you're trying to accomplish, and how
you make trade-offs. Once. Every spawned session inherits the context
through the L0 layer of the wake-up stack — the LLM has your role,
goals, current focus, and constraints in front of it before it sees
the first prompt you type.

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
| `current_projects` | Active work; each can carry its own context block | "state engine debugging; docs walk; cluster lifecycle review" |
| `values` | What you care about — drives trade-off resolution | "Simplicity over cleverness; audit over speed; low-toil over feature breadth" |
| `current_focus` | What the LLM should default-prioritize today | "state engine — finish the structured-event path" |
| `context_notes` | Anything else (env quirks, team norms, vocabulary) | "We use 'eq' for k8s as a verb. Cluster names are pet-themed. Default new tools to Go." |

The Telos additions (PAI's contribution) are the `north_star_goals` +
`values` fields. The pairing matters: goals tell the LLM what to chase,
values tell it how to choose when goals conflict.

## Base requirements

- `datawatch start` — daemon running and reachable.
- An LLM backend with at least one model configured (claude-code,
  ollama, openai, etc. — any will work). See
  [`llm-registry.md`](llm-registry.md) for setup and verification.
- No additional services. Identity lives entirely in
  `~/.datawatch/identity.yaml` on the daemon's host.

## Setup

Create the file (or let the wizard create it). The command below only
writes the file when it does not exist yet, so it can never replace an
identity you have already saved:

```sh
[ -e ~/.datawatch/identity.yaml ] || cat > ~/.datawatch/identity.yaml <<'EOF'
role: ""
north_star_goals: []
current_projects: []
values: []
current_focus: ""
context_notes: ""
EOF
```

No daemon restart needed — identity is re-read on every session spawn.

## Two happy paths

### 4a. Happy path — CLI

End-to-end via the terminal. Best when you're SSH'd into the host or
have a notes file you're translating into structured fields.

```sh
# 1. See what's there now (audit-logged read).
datawatch identity get
#  → role: ""
#    north_star_goals: []
#    current_projects: []
#    values: []
#    current_focus: ""
#    context_notes: ""

# 2. Set the role (one field per call; the others are preserved).
datawatch identity set --field role \
  --value "Platform SRE running 3 lab + 2 prod k8s clusters for a 50-person eng org"

# 3. Set the current focus.
datawatch identity set --field current_focus \
  --value "State engine — finish the structured-event path"

# 4. Set 3 north-star goals (list fields take comma-separated values).
datawatch identity set --field north_star_goals \
  --value "Cut on-call pages 50% by end of Q3,Ship k8s 1.31 cluster-by-cluster,Zero incidents from automation"

# 5. Set 3 values.
datawatch identity set --field values \
  --value "Simplicity over cleverness,Audit over speed,Low-toil over feature breadth"

# 6. Set the current projects.
datawatch identity set --field current_projects \
  --value "Channel-driven state engine for datawatch sessions"

# 6b. Set free-form context notes (newlines preserved).
datawatch identity set --field context_notes --value "$(cat <<'EOF'
- We use "eq" for kubectl as a verb.
- Cluster names are pet-themed (kona / luna / pico).
- Default new tooling to Go unless there's a strong reason not to.
EOF
)"

# 7. Verify the YAML reads back the way you expect.
datawatch identity get
```

Verify injection on a fresh session:

```sh
SID=$(datawatch sessions start --llm claude-code --task "Hello" --project-dir /tmp 2>&1 \
  | grep -oP 'session \K[a-z0-9-]+')
sleep 2
grep -A30 'L0:' ~/.datawatch/sessions/$SID/wakeup.log
#  → L0: identity
#    role: Platform SRE running 3 lab + 2 prod k8s clusters …
#    north_star_goals:
#      - Cut on-call pages 50% by end of Q3
#      - Ship k8s 1.31 cluster-by-cluster
#      - Zero incidents from automation
#    …
datawatch sessions kill $SID
```

### 4b. Happy path — PWA

End-to-end via the browser. Best the first time, when you want to see
field examples and structure suggestions inline.

1. Open the PWA → bottom nav **Settings** → top sub-tab strip
   **Automata**.
2. Scroll to the **Identity** card. Click **Configure** (or click the
   🤖 robot icon in the page header — same destination).
3. The wizard opens as a modal with six steps, one per field. Each
   step has placeholder examples and a short "what is this for?"
   blurb.
4. Step 1 — **Role**. Type one or two sentences describing what you
   do day-to-day. Tab forward.
5. Step 2 — **North-star goals**. Click **+ Add goal** for each (up
   to 3). Each is a short outcome statement, not a project.
6. Step 3 — **Current projects**. Click **+ Add project**. Each row
   takes a `name` + `context`. Add anywhere from 0 to ~5; this is
   the field that rotates fastest.
7. Step 4 — **Values**. **+ Add value** for each. Phrase as
   "X over Y" trade-offs where it helps.
8. Step 5 — **Current focus**. One-paragraph "what should the LLM
   default-prioritize this week".
9. Step 6 — **Context notes**. Free-form. Team vocabulary, env
   quirks, default-language preferences, etc.
10. **Save**. The wizard writes `~/.datawatch/identity.yaml` and
    closes. The Identity card now shows a summary block with
    timestamps.
11. To verify the injection works, spawn a fresh session: PWA → bottom
    nav **Sessions** → **+** FAB → backend `claude-code` → task
    `What do you know about me?` → **Start**. The first response
    should mirror your role / focus back.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Same surface as the PWA: Settings → Automata → Identity card →
Configure. The wizard renders as a Material-style stepped form.
Read-write parity is full. The 🤖 robot-icon shortcut isn't on the
mobile header — open from Settings only.

### 5b. REST

```sh
TOKEN=$(cat ~/.datawatch/token)
BASE=https://localhost:8443

# GET — read current identity (audit-logged).
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/identity
#  → {"role":"...","north_star_goals":[...],"current_projects":[...],
#     "values":[...],"current_focus":"...","context_notes":"..."}

# PATCH — partial update; missing fields preserved.
curl -sk -X PATCH -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"current_focus":"State engine — finish structured-event path"}' \
  $BASE/api/identity

# PUT — full replacement; missing fields are CLEARED. A body with no content
# at all over a saved identity is refused with HTTP 409 (see "Backups, restore
# and the empty-write guard" below).
curl -sk -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @- $BASE/api/identity <<'EOF'
{
  "role": "Platform SRE running 3 lab + 2 prod k8s clusters",
  "north_star_goals": ["Cut on-call pages 50%","Ship k8s 1.31"],
  "current_projects": [{"name":"state-engine","context":"State engine work"}],
  "values": ["Simplicity over cleverness","Audit over speed"],
  "current_focus": "State engine structured-event path",
  "context_notes": "Cluster names pet-themed. Default to Go."
}
EOF
```

Both PATCH and PUT return the post-update identity object as JSON.

### 5c. MCP

Tools exposed through the daemon's MCP server. From any MCP host
(claude-code MCP, Cursor, opencode-acp), the operator's AI can call:

| Tool | Args | Returns |
|---|---|---|
| `get_identity` | `{}` | Full identity object. |
| `update_identity` | `{role?, north_star_goals?[], current_projects?[], values?[], current_focus?, context_notes?}` | Merges the non-empty fields; returns the updated object. |
| `set_identity` | Same fields | Replaces the whole identity (PUT semantics). A call with no content over a saved identity is refused with `identity_wipe_refused`. |
| `identity_backups`, `restore_identity`, `reset_identity` | see "Backups, restore and the empty-write guard" below | List, restore and explicitly clear. |

Example invocation from a claude-code session:

> *Operator:* "Update my current focus to debugging the state-engine bug."
>
> *Claude:* (calls `update_identity {"current_focus": "Debugging the state-engine bug"}`)
>
> *Daemon:* returns the patched identity object.
>
> *Claude:* "Done. Your current focus is now: Debugging the state-engine bug."

### 5d. Comm channel

From any configured chat channel (Signal, Telegram, Slack, etc.):

| Verb | Example |
|---|---|
| `identity get` | Returns the current identity as a chat message. |
| `identity get [field]` | Returns one field of the identity. |
| `identity set <field> <value>` | Patches one field; list fields take comma-separated values. |
| `identity backups` | Lists the saved copies, newest first. |
| `identity restore <name\|latest>` | Brings a saved copy back. |

The full edit surface is intentionally narrower in chat — chat is for
quick adjustments, not full edits. Reach for CLI/PWA for substantive
changes.

### 5e. YAML

`~/.datawatch/identity.yaml` is the source of truth. All other
surfaces read and write through it. Schema:

```yaml
role: |
  Platform SRE running a 3-cluster lab plus 2 prod clusters for a
  50-person eng org. Strong preference for boring tech.

north_star_goals:
  - Cut on-call pages 50% by end of Q3
  - Ship k8s 1.31 cluster-by-cluster, zero downtime
  - No customer-visible incidents from automation

current_projects:
  - name: state-engine
    context: Channel-driven state engine for datawatch sessions.
  - name: cluster-lifecycle-review
    context: Documenting + automating the create/upgrade/decommission flow.

values:
  - Simplicity over cleverness
  - Audit over speed
  - Low-toil over feature breadth

current_focus: |
  State engine — finish the structured-event path for opencode-acp; verify
  the gap-watcher 30s warm-up is enough.

context_notes: |
  - We use "eq" for kubectl as a verb.
  - Cluster names are pet-themed (kona / luna / pico).
  - Default new tooling to Go unless there's a strong reason not to.
```

No daemon restart needed; identity re-reads on every session spawn.
For an existing session to pick up an updated identity, kill + restart
that session.

## Backups, restore and the empty-write guard

The identity is the one file the daemon injects into every session, so the
daemon keeps copies of it and will not let an ordinary save empty it. This
section explains what is kept, what is refused, and how to get an identity back
on every surface. The settings page version of this explanation is
[Identity backups and restore](../datawatch-definitions.md#identity-backups-and-restore).

### What the daemon does for you

- **A backup before every write.** Before the daemon changes
  `identity.yaml` it copies the existing file to
  `identity-backups/identity-<UTC timestamp>.yaml`, in the `identity-backups`
  folder next to `identity.yaml` in the data directory (by default
  `~/.datawatch/identity-backups/`). The timestamp is the UTC time to the
  millisecond, for example `identity-20261029T101500.000Z.yaml`. Saving from
  the PWA, the CLI, MCP, chat or the REST API all count, and so do Reset and
  Restore.
- **Nothing pointless is kept.** No backup is made when the current file is
  missing or has no content, or when it is the same as the newest backup apart
  from its `updated_at` line. Saving the same identity twice does not use up two
  slots.
- **Only the newest few are kept.** The daemon keeps the newest
  `identity.backups_to_keep` backups and removes the older ones. The default is
  10, the minimum is 1, and a value below 1 or a non-numeric one is refused when
  you set it. The change applies at the next write with no restart. Set it in
  Settings → Automata → Identity (*Identity backups to keep*), with
  `datawatch config set identity.backups_to_keep 20`, with
  `PUT /api/config` and `{"identity.backups_to_keep": 20}`, with the MCP
  `config_set` tool, or from chat with `configure identity.backups_to_keep=20`.
- **A failed backup stops the write.** If the daemon cannot make the backup, it
  does not write the new identity and the save fails with an error. The file you
  had is left as it was.
- **A save that would empty the identity is refused.** A `PUT /api/identity`
  (or `set_identity`) whose fields are all empty, sent over a saved identity, is
  answered with HTTP 409 and the code `identity_wipe_refused`; nothing is
  written. The body names the fields that were kept:

  ```json
  {"error": "refusing to replace the saved identity with an empty one; to clear it on purpose use POST /api/identity/reset",
   "code": "identity_wipe_refused",
   "fields_kept": ["role", "values"]}
  ```

  The refusal is recorded in the audit log. A partial save is not refused:
  `PATCH` (and `POST`, its alias) only merges the fields it carries, so an
  empty patch changes nothing, and a `PUT` that carries at least one field
  replaces the identity with that, as before, after taking a backup. Writing an
  empty identity over an empty one is allowed.
- **Clearing on purpose is a separate, explicit action.** Reset empties the
  identity, always works, and keeps a backup of what it cleared.

### Restore from a backup, on each surface

A restore brings a saved copy back. It first backs up the identity it is about
to replace, so a restore can be undone with another restore. The name `latest`
stands for the newest backup that has content, not simply the newest file, so a
backup made just before a reset is found even when later backups are empty.

| Surface | List the backups | Restore | Clear on purpose |
|---|---|---|---|
| PWA | Settings → Automata → Identity → **Restore** lists them newest first, each with its time in your local time, its role and how many fields it filled in | Choose a backup in the list and confirm | **Reset**, after a confirmation that says a backup is kept |
| CLI | `datawatch identity backups` (add `--json` for the raw reply) | `datawatch identity restore` (newest) or `datawatch identity restore --backup <name>` | `datawatch identity reset` (type `RESET` when asked, or pass `--yes`; without a terminal `--yes` is required) |
| MCP | `identity_backups` (read-only) | `restore_identity` with `backup` set to a name from the list, or `latest` (the default) | `reset_identity` |
| REST | `GET /api/identity/backups` returns `{"backups": [...], "keep": N}` | `POST /api/identity/restore` with `{"backup": "<name or latest>"}` | `POST /api/identity/reset` |
| Chat | `identity backups` | `identity restore <name>` or `identity restore latest` | Not available from chat; the reply points at the CLI, PWA or MCP so one mistyped line cannot empty the identity |

Restore answers with the restored identity. An unknown or malformed name is
HTTP 404, a backup with no content is HTTP 422, and a missing `backup` field is
HTTP 400.

### Recover an identity that was emptied

1. List what the daemon kept, newest first:

   ```sh
   datawatch identity backups
   #  NAME                                  TIME                 ROLE               FIELDS
   #  identity-20261029T101500.000Z.yaml    2026-10-29 06:15:00  platform engineer  5
   #  identity-20261028T080000.000Z.yaml    2026-10-28 04:00:00  platform engineer  3
   ```

2. Restore the newest backup that has content, or a specific one:

   ```sh
   datawatch identity restore
   datawatch identity restore --backup identity-20261028T080000.000Z.yaml
   ```

3. Read it back with `datawatch identity get` and confirm the role and goals are
   yours. If it is not the one you wanted, run `datawatch identity backups`
   again: the identity you just replaced is now the newest backup, so restoring
   `latest` goes back.
4. Existing sessions keep the identity they started with; kill and restart a
   session to give it the restored one.

The same recovery from the PWA: Settings → Automata → Identity → **Restore**,
pick the backup by its time and role, confirm.

### Restore by hand

Use this when the daemon will not start, or when you prefer to work on the file
yourself. Stop the daemon first, so it cannot write while you copy.

```sh
systemctl --user stop datawatch        # or: datawatch stop
ls -1 ~/.datawatch/identity-backups/    # names sort oldest to newest
cp ~/.datawatch/identity.yaml ~/identity.yaml.before-restore   # optional, keep what is there now
cp ~/.datawatch/identity-backups/identity-20261028T080000.000Z.yaml ~/.datawatch/identity.yaml
chmod 600 ~/.datawatch/identity.yaml
systemctl --user start datawatch       # or: datawatch start
datawatch identity get
```

If the daemon runs with a different data directory, use that directory in place
of `~/.datawatch`. A backup is an ordinary copy of `identity.yaml`, so you can
also open it in an editor to see what it holds before you copy it.

### Troubleshooting

- **`HTTP 409 identity_wipe_refused` when saving.** The save you sent had no
  content and the daemon holds a saved identity. Send at least one field. To
  clear the identity on purpose use Reset (`datawatch identity reset`, the PWA
  Reset button, `reset_identity` or `POST /api/identity/reset`). A script that
  empties the identity "to start clean" should call the reset route, not send an
  empty `PUT`.
- **`identity backups` says "no backups yet".** No write has replaced a saved
  identity since the daemon gained this feature, or the data directory is
  different from the one you are looking at. Check the `identity-backups`
  folder next to `identity.yaml` in the directory the daemon runs with.
- **The oldest backup you wanted is gone.** Only the newest
  `identity.backups_to_keep` are kept. Raise it for a longer trail; it does not
  bring back backups already removed, so copy a backup you want to keep
  elsewhere.
- **Restore says the backup was not found (404).** The name must be one from
  `identity backups` (or `latest`), with no folder part. A backup removed by the
  retention limit since you listed them also answers 404.
- **Restore says the backup has no content (422).** That backup was made from an
  empty identity. Choose another one, or `latest`, which skips such backups.
- **Restore of `latest` says no backup has content.** The folder holds no
  backups, or every backup in it is empty. Nothing is changed; restore by hand
  from a copy you made yourself.
- **A save fails with a backup error.** The daemon could not write into
  `identity-backups`, for example because the disk is full or the folder is not
  writable by the daemon's user. The identity file was not changed. Fix the
  folder and save again.
- **You edited `identity.yaml` by hand while the daemon ran.** The daemon reads
  the file from disk on each save, so a hand edit needs no restart. A `PATCH` or
  `POST` merges its fields onto your edit; a `PUT` replaces the whole identity
  with its body, so your edit is overwritten by it. Either way the edited file is
  backed up first, so the edit can be restored.

## Diagram

How identity flows into a spawned session's wake-up stack:

```
  ┌──────────────────────────────────────────┐
  │ ~/.datawatch/identity.yaml               │
  └──────────┬───────────────────────────────┘
             │   read on every spawn
             ▼
  ┌──────────────────────────────────────────┐
  │ Daemon: render L0 layer                  │
  │   role / goals / projects / values /     │
  │   current_focus / context_notes          │
  └──────────┬───────────────────────────────┘
             │   prepended to system prompt
             ▼
  ┌──────────────────────────────────────────┐
  │ LLM session (claude-code, ollama, ...)   │
  │   sees identity BEFORE the first user    │
  │   message                                │
  └──────────────────────────────────────────┘
```

For the full wake-up stack (L0 identity + L1 critical facts + L2 room
recall + L3 deep search), see `architecture-overview.md` § Wake-up.

## Common pitfalls

- **Too long.** A 2000-word `context_notes` block dilutes L0 and pushes
  other content (recent memory, current task) further down the prompt.
  Aim for tight, declarative bullets. Edit ruthlessly.
- **Mixing identity with task spec.** Identity is operator-scoped. If
  you find yourself writing project-specific context that only matters
  for one work area, move it to a Project Profile instead
  (`profiles.md`).
- **Stale focus.** `current_focus` is the field that decays fastest.
  Set a calendar reminder to revisit weekly, or update it at the top
  of every fresh session.
- **No goals at all.** Without goals the LLM has nothing to defer to
  when you give it ambiguous direction. Even "ship cleaner code" is
  better than empty.
- **Forgetting that existing sessions don't pick up changes.** Identity
  is captured at spawn time. To force an existing session to pick up
  edits, kill and restart it (or paste the new fields into the chat).

## Linked references

- Architecture: `../architecture-overview.md` § Wake-up stack
- Plan attribution: PAI Identity / Telos in `../plan-attribution.md`
- API: full Swagger UI under `/api/docs`
- Profiles (project-scoped, not operator-scoped): `profiles.md`
- Memory subsystem (L1–L3 layers that surround L0): `cross-agent-memory.md`

## Screenshots needed (operator weekend pass)

- [ ] PWA Identity card (Settings → Automata)
- [ ] Identity Wizard modal — Role step (with placeholder text visible)
- [ ] Identity Wizard modal — Current projects step with 2 entries
- [ ] Identity Wizard modal — completed example, Save button highlighted
- [ ] 🤖 robot icon location in Automata page header
- [ ] CLI `datawatch identity get` example output
- [ ] Verification — fresh session's L0 wake-up log block

---

## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [howto/algorithm-mode](algorithm-mode.md)
- [howto/council-mode](council-mode.md)
- [howto/autonomous-planning](autonomous-planning.md)

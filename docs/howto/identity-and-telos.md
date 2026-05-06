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
| `current_projects` | Active work; each can carry its own context block | "BL266 state engine debugging; v6.13.0 docs walk; cluster lifecycle review" |
| `values` | What you care about — drives trade-off resolution | "Simplicity over cleverness; audit over speed; low-toil over feature breadth" |
| `current_focus` | What the LLM should default-prioritize today | "BL266 state engine — finish the structured-event path" |
| `context_notes` | Anything else (env quirks, team norms, vocabulary) | "We use 'eq' for k8s as a verb. Cluster names are pet-themed. Default new tools to Go." |

The Telos additions (PAI's contribution) are the `north_star_goals` +
`values` fields. The pairing matters: goals tell the LLM what to chase,
values tell it how to choose when goals conflict.

## Base requirements

- `datawatch start` — daemon running and reachable.
- An LLM backend with at least one model configured (claude-code,
  ollama, openai, etc. — any will work).
- No additional services. Identity lives entirely in
  `~/.datawatch/identity.yaml` on the daemon's host.

## Setup

Create the file (or let the wizard create it):

```sh
cat > ~/.datawatch/identity.yaml <<'EOF'
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

# 2. Set the role + focus in one shot.
datawatch identity set \
  --role "Platform SRE running 3 lab + 2 prod k8s clusters for a 50-person eng org" \
  --focus "BL266 state engine — finish the structured-event path"

# 3. Add 3 north-star goals.
datawatch identity set \
  --goals "Cut on-call pages 50% by end of Q3,Ship k8s 1.31 cluster-by-cluster,Zero incidents from automation"

# 4. Add 3 values.
datawatch identity set \
  --values "Simplicity over cleverness,Audit over speed,Low-toil over feature breadth"

# 5. Add a current project with context.
datawatch identity set \
  --add-project "BL266=Channel-driven state engine for datawatch sessions"

# 6. Add free-form context notes (newlines preserved).
datawatch identity set --context "$(cat <<'EOF'
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
SID=$(datawatch sessions start --backend claude-code --task "Hello" --project-dir /tmp 2>&1 \
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
   **Automate**.
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

Same surface as the PWA: Settings → Automate → Identity card →
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
  -d '{"current_focus":"BL266 — finish structured-event path"}' \
  $BASE/api/identity

# PUT — full replacement; missing fields are CLEARED.
curl -sk -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @- $BASE/api/identity <<'EOF'
{
  "role": "Platform SRE running 3 lab + 2 prod k8s clusters",
  "north_star_goals": ["Cut on-call pages 50%","Ship k8s 1.31"],
  "current_projects": [{"name":"BL266","context":"State engine work"}],
  "values": ["Simplicity over cleverness","Audit over speed"],
  "current_focus": "BL266 structured-event path",
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
| `identity_get` | `{}` | Full identity object. |
| `identity_patch` | `{role?, focus?, goals?[], values?[], projects?[]}` | Updated object. |
| `identity_set` | Same as PATCH; alias for symmetry with the CLI. |

Example invocation from a claude-code session:

> *Operator:* "Update my current focus to debugging the state-engine bug."
>
> *Claude:* (calls `identity_patch {"focus": "Debugging the state-engine bug"}`)
>
> *Daemon:* returns the patched identity object.
>
> *Claude:* "Done. Your current focus is now: Debugging the state-engine bug."

### 5d. Comm channel

From any configured chat channel (Signal, Telegram, Slack, etc.):

| Verb | Example |
|---|---|
| `identity get` | Returns the current identity as a chat message. |
| `identity focus "<text>"` | Updates `current_focus` only. |
| `identity goal add "<text>"` | Appends to `north_star_goals`. |
| `identity goal remove <n>` | Removes by index (1-based, as listed in `identity get`). |

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
  - Cluster names are pet-themed (kona / luna / pico).
  - Default new tooling to Go unless there's a strong reason not to.
```

No daemon restart needed; identity re-reads on every session spawn.
For an existing session to pick up an updated identity, kill + restart
that session.

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

- [ ] PWA Identity card (Settings → Automate)
- [ ] Identity Wizard modal — Role step (with placeholder text visible)
- [ ] Identity Wizard modal — Current projects step with 2 entries
- [ ] Identity Wizard modal — completed example, Save button highlighted
- [ ] 🤖 robot icon location in Automata page header
- [ ] CLI `datawatch identity get` example output
- [ ] Verification — fresh session's L0 wake-up log block

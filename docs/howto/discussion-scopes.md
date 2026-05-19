# Discussion Scopes

Discussion scopes give you a per-discussion memory namespace that is durable, federated,
and conflict-aware. Use them when you want a shared scratchpad between two or more peers
that survives daemon restarts, can be recalled semantically, and needs a tamper-evident
write-ahead log.

---

## What discussion scopes are and when to use them

A discussion scope is a named memory partition keyed by a discussion ID (a short string
you choose). Each scope stores an ordered list of entries, a WAL at
`~/.datawatch/discussions/<id>/wal.jsonl`, and an optional set of participant peers that
automatically receive a copy of every new write.

Use discussion scopes when:
- You want multiple datawatch peers to share a memory thread about a topic or project.
- You need a durable, ordered log of decisions or observations attached to a conversation.
- You want conflict detection when two peers write concurrently without coordination.

Do not confuse discussion scopes with the other four memory scopes:
- **persona-global** and **project-shared** are for long-lived operator knowledge.
- **session-local** is ephemeral per-session scratch.
- **discussion** scopes are explicitly multi-peer and WAL-backed.

### Scope resolution

The `ScopeDiscussion` constant resolves to:

```
projectDir = ""
role       = "discussion/<id>"
sessionID  = ""
```

This means discussion entries do not belong to any project directory or session; they are
addressable only by their discussion ID.

---

## Base requirements

- datawatch daemon running (`datawatch start`)
- Bearer token for authentication
- For federated sync: at least two datawatch peers registered and reachable

---

## Creating a discussion scope and writing entries

Every write to a new discussion ID automatically creates the scope and the WAL file. There
is no explicit create step.

### REST

```sh
export DW_HOST=http://localhost:8080
export DW_TOKEN=<your-daemon-token>
alias dw="curl -sk -H 'Content-Type: application/json' -H 'Authorization: Bearer $DW_TOKEN'"

# Write the first entry — the scope is created on first write
dw -X POST $DW_HOST/api/memory/discussion/my-topic \
  -d '{"content": "We decided to use PostgreSQL for the vector store."}' | jq .
# {"ok":true,"seq":1}

# Write another entry
dw -X POST $DW_HOST/api/memory/discussion/my-topic \
  -d '{"content": "pgvector extension confirmed available on prod cluster."}' | jq .
# {"ok":true,"seq":2}

# List all known discussions
dw $DW_HOST/api/memory/discussion | jq .
# {"discussions":["my-topic"]}

# Read all entries for a discussion
dw $DW_HOST/api/memory/discussion/my-topic | jq .
# {"id":"my-topic","entries":[{"seq":1,"content":"...","ts":"..."},{"seq":2,"content":"...","ts":"..."}]}

# Delete a discussion (removes all entries and the WAL)
dw -X DELETE $DW_HOST/api/memory/discussion/my-topic | jq .
# {"ok":true}
```

### CLI

```sh
# Write an entry
datawatch memory discussion write my-topic "We decided to use PostgreSQL for the vector store."

# List discussions
datawatch memory discussion list

# Recall entries for a discussion
datawatch memory discussion recall my-topic

# Show the raw WAL
datawatch memory discussion wal my-topic
```

### MCP (via Claude Code)

```
memory_discussion_write(id="my-topic", content="We decided to use PostgreSQL for the vector store.")
# → {"ok":true,"seq":1}

memory_discussion_recall(id="my-topic")
# → {"id":"my-topic","entries":[...]}

memory_discussion_wal(id="my-topic")
# → {"id":"my-topic","entries":[...]}
```

### PWA

1. Open Settings → General.
2. Scroll to the **Discussion Scopes** card.
3. Use the **New Discussion** form to set an ID and create the scope.
4. Click a discussion row's **Recall** button to view entries.

---

## Setting up participant sync

Participant sync lets you fan-out every write to a list of registered federation peers.
When you add peers to a discussion's participant list, every future POST to that discussion
automatically triggers an asynchronous push to each participant.

### Prerequisites

Register the participant peers in your daemon before adding them to a discussion:

```sh
datawatch federation peer add \
  --name "peer-alpha" \
  --url "https://peer-alpha.internal:8080" \
  --token "<peer-alpha-bearer-token>"
```

### Set the participant list

**REST:**

```sh
dw -X PUT $DW_HOST/api/memory/discussion/my-topic/participants \
  -d '{"participants":["peer-alpha","peer-beta"]}' | jq .
# {"ok":true}

# Verify
dw $DW_HOST/api/memory/discussion/my-topic/participants | jq .
# {"id":"my-topic","participants":["peer-alpha","peer-beta"]}
```

**CLI:**

```sh
datawatch memory discussion participants my-topic --set peer-alpha,peer-beta
datawatch memory discussion participants my-topic
# peers: peer-alpha, peer-beta
```

**MCP:**

```
memory_discussion_participants(id="my-topic", participants=["peer-alpha","peer-beta"])
# → {"ok":true}
```

### What happens on write

After a successful POST, the daemon fans out the entry to each participant peer
asynchronously. The sync uses the peer's registered URL and token. Each entry in the WAL
records `origin_peer` and `origin_wal_seq` so the receiving peer can detect and skip
re-syncing entries it originally produced.

---

## Understanding the WAL and conflict resolution

### The WAL

Every write appends a record to `~/.datawatch/discussions/<id>/wal.jsonl`. Each record has:

| Field | Description |
|---|---|
| `seq` | Monotonically increasing sequence number within the discussion |
| `content` | The entry content |
| `ts` | RFC 3339 timestamp at write time |
| `origin_peer` | Peer that originally produced this entry (empty = local) |
| `origin_wal_seq` | Sequence number of the entry on the originating peer |

```sh
# View the WAL
datawatch memory discussion wal my-topic
# or via REST:
dw $DW_HOST/api/memory/discussion/my-topic/wal | jq .
```

### Conflicts

A conflict occurs when two peers write to the same discussion concurrently without
coordination, producing entries with overlapping sequence windows. Conflicts are detected
when the daemon receives a sync push from a peer whose entries overlap with locally
committed entries.

**List conflicts:**

```sh
dw $DW_HOST/api/memory/discussion/my-topic/conflicts | jq .
# {"id":"my-topic","conflicts":[{"seq_a":3,"seq_b":3,"ts":"..."}]}
```

**Resolve a conflict:**

Identify which entry should be the canonical winner, then call the resolve endpoint:

```sh
dw -X POST $DW_HOST/api/memory/discussion/my-topic/conflicts/resolve \
  -d '{"winner_seq":3,"loser_seq":3}' | jq .
# {"ok":true}
```

The loser entry is tombstoned in the WAL; it will not be returned by subsequent reads or
sync operations.

---

## The 60 ops/min throttle

To prevent sync storms, each federation peer is limited to **60 write operations per
minute** per discussion. This is a per-peer token bucket; local writes by the daemon
itself are not throttled.

When a peer exceeds the limit, the daemon returns `429 Too Many Requests`. The peer should
back off and retry after the bucket refills (approximately 1 second per token).

```sh
# Writing from a peer that exceeds 60/min:
# → HTTP 429 Too Many Requests
# Retry-After header indicates when the bucket refills
```

If you operate a high-throughput discussion (more than 60 writes per minute from a single
peer), consider batching entries into a single POST with a structured content block, or
splitting the workload across multiple discussion IDs.

---

## Accessing via MCP

The four MCP tools for discussion scopes are available in any MCP-connected client
(Claude Code, Claude Desktop, Cursor, etc.):

| Tool | Description |
|---|---|
| `memory_discussion_write` | Write an entry to a discussion scope |
| `memory_discussion_recall` | Read all entries for a discussion scope |
| `memory_discussion_wal` | Read the raw WAL for a discussion scope |
| `memory_discussion_participants` | Get or set the participant peer list |

All four tools require an active connection to the datawatch MCP server. Verify they appear
in the tool list:

```sh
curl -sk -H "Authorization: Bearer $DW_TOKEN" \
  "$DW_HOST/api/mcp/docs" | jq '[.tools[] | select(.name | startswith("memory_discussion"))] | map(.name)'
# ["memory_discussion_participants","memory_discussion_recall","memory_discussion_wal","memory_discussion_write"]
```

---

## Security model

Discussion scope endpoints are gated on the `comm:*` capability surface:

| Action | Required cap |
|---|---|
| List discussions, read entries, read WAL, read participants, read conflicts | `comm:read` |
| Write entry, delete discussion, set participants, resolve conflicts | `comm:write` |

**Builtin groups and their access:**

| Group | Can read | Can write |
|---|---|---|
| `comm-bridge` | Yes | Yes |
| `read-only` | Yes | No |
| `full-control` | Yes | Yes |
| `config-admin` | No | No |
| `federation-peer` (default) | No | No |

To give a peer write access to discussion scopes, grant it the `comm-bridge` group or
explicitly add `comm:write` to its capabilities:

```sh
datawatch federation peer update peer-alpha --capabilities "comm:read,comm:write"
# or add to a custom group:
datawatch federation group add discussion-writers --caps "comm:read,comm:write"
datawatch federation peer update peer-alpha --capabilities "discussion-writers"
```

See [federation-cbac.md](federation-cbac.md) for the full capability reference.

---

## See also

- [cross-agent-memory.md](cross-agent-memory.md) — episodic memory + knowledge graph + scope hierarchy
- [federation-cbac.md](federation-cbac.md) — capability-based access control for federation peers
- [file-service.md](file-service.md) — federated file storage under `peers/` and `discussions/` subdirs
- [channel-routing.md](channel-routing.md) — route inbound messages to federation peers

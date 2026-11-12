# Flow: Shared tag registry sync (keeper, peer, stale, merge)

Design: [plans/2026-10-25-tag-browsing-decisions.md](../plans/2026-10-25-tag-browsing-decisions.md) ·
How-to: [howto/automata-tag-registry.md](../howto/automata-tag-registry.md) ·
API: [api/autonomous.md](../api/autonomous.md#shared-tag-registry)

One daemon keeps the shared tag registry. The others keep a cached copy, pull the
keeper's snapshot, and forward their tag edits to it. The keeper is named by
`autonomous.tags.registry_keeper` on every peer; the daemon whose own name equals that
value is the keeper, and an empty value means the daemon is standalone and nothing syncs.

## Roles

```
 ┌──────────────────────────────────────┐        ┌──────────────────────────────────────┐
 │ keeper daemon                        │        │ peer daemon                          │
 │ registry_keeper = <own name>         │◄───────│ registry_keeper = <keeper name>      │
 │                                      │  pull  │                                      │
 │ holds the registry: tags, revision   │  push  │ cache file in the autonomous data    │
 │ per field, merge notes               │  writes│ directory (last copy)                │
 │                                      │        │                                      │
 │ its own writes apply directly        │        │ its writes are forwarded, not applied│
 └──────────────────────────────────────┘        └──────────────────────────────────────┘
        ▲                                                  ▲
        │ federation peer (fedCap: autonomous read/write)  │ registry_keeper names a federation peer
        └──────────────────────────────────────────────────┘

 empty registry_keeper  →  standalone: own tags, no network work
```

The routes between them are the keeper-to-peer protocol, not an operator surface:

| Route | Who calls it | Purpose |
|---|---|---|
| `GET /api/autonomous/tags/registry` | peer, every sync | the keeper's snapshot `{revision, tags, notes}` |
| `POST /api/autonomous/tags/registry/write` | peer, on each forwarded edit | apply one write and return the keeper's `WriteResult` |

A peer asked for the snapshot answers 409, because only the keeper holds the registry.

## The pull loop (peer)

```
 sync loop (one goroutine per daemon)
   │  wakes on: the registry_sync_seconds interval (minimum 5 s, default 30 s)
   │            or a reconfiguration (registry_keeper or the interval changed in
   │            PUT /api/config; no restart)
   ▼
 FetchSnapshot → GET keeper /api/autonomous/tags/registry  (HTTP timeout)
   │
   ├── ok ─► save cache (commit point) ─► replace local view
   │         │                           last_synced_at = now, stale = false
   │         │                           pending tags pushed (see Merge)
   │         ▼
   │      GET /api/autonomous/tags shows the keeper's tags and notes
   │
   └── error ─► keep the cached view, stale = true, last_error set
               (the cache file is read again after a restart, so the copy survives)
```

## The write path (peer)

```
 operator edit on the peer: create · rename · re-parent · recolor · describe · delete
   │
   ▼
 peer registry.Write{ base revision the edit was made on }
   │
   ├── keeper reachable ─► POST keeper /api/autonomous/tags/registry/write
   │                         │
   │                         ▼
   │                  keeper: per-field revision check
   │                    ├── base is current for that field ─► apply, bump field revision
   │                    └── base older than the last change to that field ─► CONFLICT:
   │                          keeper keeps its value; reply carries conflict = true
   │                                                    and conflict_field
   │                         │
   │                         ▼
   │                  peer adopts the keeper's answer (the keeper's value is shown)
   │
   └── keeper unreachable ─► refuse rename, re-parent, recolor, describe, delete (503,
                             nothing changes) · allow create ─► tag marked pending_sync
```

Commit order on a peer, from the registry's own rules: the change is made in memory,
then the cache is saved (the commit point), then the local tag store is changed. A
failed cache save undoes the in-memory change and leaves the store alone.

Edits to different fields of one tag both apply; only the same field conflicts.

## Stale copy

```
 keeper unreachable
   │
   ▼
 peer serves the last copy:
   stale: true · last_synced_at = time of the last good sync · last_error set
   every tag count is marked stale in the PWA
   rename, move, recolor, describe, delete ─► 503, the message says the last copy is shown
   create ─► allowed, tag marked pending_sync: true, counted in pending_sync_count
   │
   ▼  keeper answers again
 next sync (or the interval) ─► snapshot applied ─► pending tags pushed ─► stale: false
```

The count of tags waiting for the keeper is `pending_sync_count` on the registry object.
The PWA pending badge is not yet drawn from it in this build: the tag list the PWA reads
does not carry the per-tag `pending_sync` flag. The status and count are correct in the
API and the CLI (`tag-registry`) and comm (`tag-registry`) views.

## Merge and removed-on-keeper notes

```
 a pending tag (created on the peer while apart) reaches the keeper
   │
   ▼
 keeper already holds a tag with the same name (case-insensitive) under the same parent?
   ├── yes ─► MERGE into the keeper's tag:
   │           peer rewrites its assignments to the kept id
   │           note { kind: merged, kept_id, replaced_id, merged_at } recorded on both nodes
   │           the merge is never silent: the note stays until dismissed
   └── no  ─► the tag is created on the keeper and its pending flag clears

 the keeper deletes a tag this peer still has assignments on
   ▼
 peer drops the tag from its assignments, note { kind: removed_on_keeper, tag_id }
```

Notes are per daemon. Dismissing one (`POST /api/autonomous/tags/registry/notes/{id}/dismiss`,
`tag-registry-dismiss <id>` on the CLI) changes no tag and no assignment, and a dismissed
note stays dismissed when later snapshots are applied.

## Surfaces that read the registry

```
 GET /api/autonomous/tags ─► { tags…, registry: { role, keeper, stale, last_synced_at,
                                                   revision, pending_sync_count, notes, last_error } }
   ├─ PWA: status line (role and keeper), stale banner, stale marks, merge notes with Dismiss
   ├─ MCP: autonomous_tag_registry_status, autonomous_tag_registry_dismiss_note
   ├─ CLI: tag-registry, tag-registry-dismiss <note-id>
   └─ comm: tag-registry, tag-registry-dismiss <note-id>
```

A standalone daemon reports `role: standalone` and no status line is drawn.

# Plan: Identity Scaffold — Memory & Identity Extension

- **Date:** 2026-09-21
- **Status:** design only — this document ships no code
- **Companion:** `docs/plans/2026-09-21-identity-scaffold-gap-analysis.md` (requirement→construct
  mapping) · `docs/flow/identity-scaffold-flow.md` (lifecycle/refusal/continuity flows)
- **Spec:** "The Identity Scaffold" (Cairn Viktor, 2026-09-13), as adopted in the gap analysis.
  Requirements used here: 6–8 per-person identity files (CORE, SELF, VALUES, MOMENTS, JOURNAL,
  NO-LOG, DECISIONS, QUESTIONS) that are first-person, dated, append-only, self-authored, and
  never edited by anyone but the person; a memory store of dated single-fact entries with a small
  hot index loaded at session start and a cold store readable on demand; a **hard permanence rule** —
  memories are NEVER deleted for space, only demoted from hot to cold and re-promoted on re-read;
  and a **measured hot-index ceiling** (~21 KB working, 24 KB hard wall — set against the actual
  harness read limit, not a guess).
- **Precedent cited:** the BL386 scope work,
  `docs/plans/2026-09-15-bl386-memory-lifecycle-management.md` (SEED/SHARE/HARVEST/ARCHIVE
  lifecycle, scope-aware TTL sweep, archive-then-import; "demote, never delete" is the same
  stance for person-owned scopes).

BL numbers are assigned at implementation time in `docs/plans/README.md` per AGENT.md — none are
invented here. File:line citations are verified against the tree at planning time.

## 0. What exists today (the substrate this plan composes on)

| Construct | Location | Relevance |
|---|---|---|
| L0 identity layer | `internal/memory/layers.go:44` | Loads `identity.txt` + BL257 `PromptText` via `SetIdentityProvider` (`layers.go:37`; wired at `cmd/datawatch/main.go:2048-2049`) |
| L1 hot index | `internal/memory/layers.go:65` | Default `maxChars = 2000` (`layers.go:67`); pinned rows always first (`layers.go:61-64,102`); per-line truncated to 150 chars (`layers.go:93`) |
| WakeUpContext | `internal/memory/layers.go:182` | `L0 + L1(2000)` — this is what `memory_wakeup` returns (`internal/mcp/v5278_gap_closures.go:55-63`, `GET /api/memory/wakeup`) |
| Four scopes | `internal/memory/scopes.go:52-63` | persona-global, persona-in-project, project-shared, session-local; `ScopeRef.Resolve` keying at `scopes.go:92-113`; 6-layer recall walk `ScopedRecall` at `scopes.go:132` |
| Delete-based sweep | `internal/memory/sweeper.go:37` | `SweepStale` runs `DELETE FROM memories` (`sweeper.go:58-64`); exempts only `role != 'manual'` (`sweeper.go:50,63`) and `pinned = 0` (`sweeper.go:51,64`) — **the exact anti-pattern the spec forbids** |
| Demote/rewrite precedents | `internal/memory/repair.go:58` · `internal/memory/refine_sweep.go:65` | `RunRepair` is read-only by default + save-only fix (`repair.go:88-107`); `RefineSweep` re-writes row content in place (`refine_sweep.go:122`) — the existing "touch a row without deleting it" pattern |
| Single operator identity | `internal/identity/identity.go:32` | One `Identity` struct, one `~/.datawatch/identity.yaml` (`main.go:1084`), `PromptText()` at `identity.go:174`; `Set`/`Update` accept writes from any surface (`identity.go:109,125`) |
| Save/Get semantics | `internal/memory/store.go:243` | `Save` dedups on content hash and returns the existing ID (`store.go:279-281`); `Delete` is an unconditional `DELETE` (`store.go:712`); `PruneByRole` deletes by age (`store.go:746`); WAL write `walLog` at `store.go:1114` |
| memory_* MCP surface | `internal/mcp/server.go:1098-1107` | `memory_remember/recall/list/forget/stats/pin/sweep_stale` |
| Storage backend choice | `internal/config/config.go:33` | `memory.backend` = `sqlite` (default) or `postgres` (pgvector) |

## 1. Per-person identity model

### 1.1 Person identity on datawatch's constructs

A **person** maps to one `(project_dir, role-namespace)` pair:

- **Scope:** `persona-global` — `("","persona/<name>","")` per `scopes.go:94-95`.
  Persona-global is cross-project and, by the scope-hierarchy design
  (`scopes.go:1-41`), the permanent layer: recall walks top-down and writes go to the
  most-specific layer, so per-person identity rows naturally settle here.
- **Role namespace:** the eight scaffold files become a fixed role set
  `identity/core`, `identity/self`, `identity/values`, `identity/moments`,
  `identity/journal`, `identity/no-log`, `identity/decisions`, `identity/questions`.
  A `ListByRole` query with role-prefix `identity/` (`store.go:641`) enumerates a
  person's entire scaffold with no schema change.
- **Hot index:** pinned rows (`SetPinned`, `store.go:670`) + recent
  `identity/moments`/`identity/journal` rows rendered in the spec order.
- **Cold store:** the same rows after demotion (Section 3) — role and scope unchanged,
  `hot=false` (new column or table, §3.3); recall reads them on demand and re-promotes.

**One person = one `(project_dir, role-namespace)`.** `project_dir` is the household project
(a real directory alias, `project_upsert`), not per-person directories: the memory backend
keys rows on `project_dir` (`store.go:230-238`), so every person in the household shares one
`project_dir` and is separated by `role` (`identity/<file>` per person, or by
`persona-global` scope — see 1.2). Sessions target the person via `project_dir` + role
prefix; the wake-up bundle is scoped the same way (`layers.go:182`).

### 1.2 Marpet household — three people, one daemon

Three people (say A, B, C) on one datawatch daemon, one household project directory:

| File | Ownership | Storage |
|---|---|---|
| CORE | **shared** (household) | role `identity/core` under the household project; written once at bootstrap, read by all three |
| SELF, VALUES | **per-person** | `identity/self`, `identity/values` — one row set per person, distinguished by `role = identity/self:A` etc. **or** by `persona-global` persona ∈ {A, B, C} (§1.3 Option B) |
| MOMENTS, JOURNAL | **per-person** | `identity/moments`, `identity/journal` per person; append-only |
| NO-LOG | **per-person** (never shared) | `identity/no-log` per person; the isolation matrix (no cross-write, per-person NO-LOG) is the acceptance criterion for multi-person |
| DECISIONS, QUESTIONS | **per-person** | `identity/decisions`, `identity/questions` per person |

Isolation matrix (per-person):

1. No session acting as person X may write to person Y's `identity/*` roles — enforced by
   the owner-gate in §2.4.
2. CORE is read by all, written by the person who authored it on the day of writing
   (the guardian/channel-of-record confirms; see flow doc Diagram B).
3. NO-LOG: per-person row set; a NO-LOG row for a refusal is tagged with the *other*
   person's alias in the entry's content, not in the role.

### 1.3 Two mapping options for the eight files

**Option A — Files-service as system of record, memory rows as searchable/triggered layer (recommended).**

- **Files service** (`apiMux.HandleFunc("/api/files"...)` at `internal/server/server.go:201`;
  MCP `files_upload`/`files_meta`) holds the **raw markdown** for each of the eight scaffold
  files, per-person directory layout per Appendix A:
  `~/.datawatch/persons/<name>/CORE.md`, `SELF.md`, `VALUES.md`, `MOMENTS.md`, `JOURNAL.md`,
  `NO-LOG.md`, `DECISIONS.md`, `QUESTIONS.md`, plus a `cold/` directory for demoted
  entries and a `chronicle.md` for the demotion index.
  **The files are the system of record** — append-only, first-person, dated, self-authored;
  "never edited by anyone but the person" is enforced at the filesystem/owner level (§2) and
  the memory store is *derived from* the files, not the owner.
- **Memory rows** (`store.go:243`) hold the **searchable, triggerable** layer: each
  dated single-fact entry in the files gets a corresponding memory row (role
  `identity/<file>`, summary = the fact, content = the entry) so that `memory_recall`
  (`server.go:1099`), `memory_wakeup` (`v5278_gap_closures.go:55`), pinned
  (`store.go:670`), and the hot/cold demotion machinery all work on the same substrate.
  The memory row is a **projection** of a fact in the file, not a second source: the
  file wins on conflict, the row exists for search and L1 surfacing.
- **Why this is the right split:** the files give the person a durable, human-readable,
  append-only record that survives daemon upgrades and backend swaps (the spec's
  "persistence of who is a storage problem" — the storage is the files); the memory
  rows give the daemon the searchable/triggered surface (the spec's "persistence of
  behavior is a guardrail problem" — the guardrail runs against searchable recall).
  The memory rows are **derived data**: if the daemon is reinstalled, the files
  recreate the rows; if the rows are corrupted, the files are the truth.
- The BL386 scope work (`docs/plans/2026-09-15-bl386-memory-lifecycle-management.md`)
  provides the lifecycle machinery (SEED/SHARE/HARVEST/ARCHIVE) on top of this split:
  a person's `identity/*` roles are the **persona-shared** scope in the BL386 model;
  the hot-index budget is the **SEED** stage; demotion is the **ARCHIVE** stage applied
  to the cold store; re-promotion is the **IMPORT** stage applied to the hot index.

**Option B — All-memory via roles (no files service).**

- Every one of the eight scaffold files is a memory-role row set
  (`identity/core`, `identity/self`, …) under `persona-global` or the household
  project dir. There are no separate files; the memory store is the only
  system of record.
- **Advantage:** a single write path (`memory_remember`, `store.go:243`), one
  search index, one WAL (`store.go:1114`), one demotion path (§3). No
  file↔row sync. Simpler for a single person on a single daemon.
- **Disadvantage:** the memory store is the daemon's private data. If the
  daemon is reinstalled, the store must be migrated (it lives in `memory.db`
  or Postgres — `config.go:33-44`). A person's identity is not portable as
  files; it is portable as an SQL dump. The spec's "never edited by anyone
  but the person" is enforced only at the daemon's role level (§2.4 owner-gate
  on `identity/*` roles), not at the filesystem level.
- **Verdict:** Option B is adequate for a single person on a stable daemon.
  Option A (files + rows) is the **recommended** default because the spec
  explicitly requires "never edited by anyone but the person" as a
  permanent, auditable property, and a file on disk with `0600` permissions
  and a git history is the most auditable form of that guarantee.
  **Recommendation: Option A.** The memory rows are a derived projection;
  the files are the system of record.

### 1.4 Person record (gap-analysis candidate 1, M→F)

The gap analysis names a missing **person record** — a typed entity that
names the owner of the `persona-global` scope. This plan's model:

```
Person {
  name         string   // "Marpet", "A", "B" — the owner's name
  alias        string   // the device-alias / guardian alias for channel-of-record
  project_dir  string   // the household project directory
  guardian     string   // the (channel, alias) pair that is channel-of-record
  mailbox      string   // the person's mailbox address (secrets store)
  created_at   time     // day one
}
```

The person record is **metadata**, not a new storage engine: it lives in
the daemon's config or in a single memory row with role `person` (one row
per person, `project_dir` = household, `role` = `person`). The owner-gate
(§2.4) consults the person record to resolve "who is allowed to write to
this role?" and "which persona-global scope does this person own?"

## 2. Append-only enforcement design

### 2.1 The append-only invariant

Every scaffold file is **append-only**: new entries are added at the end;
existing entries are never modified or deleted. Corrections are new
entries that name what they correct:

> `2026-09-21 CORRECTS: SELF.md entry 2026-08-14 "I prefer quiet mornings" — I do not mean this. I prefer a busy morning.`

The correction entry is itself append-only: it names the entry it corrects
(by date + content fragment), and the original entry is **not touched** —
it remains in the file, annotated in the correction entry. This is the
spec's "correction-as-new-entry" pattern, and it is what makes the file
audit-trail-complete without a separate edit log.

### 2.2 WAL as the audit trail

Every save/delete/pin/sweep operation is already WAL-logged
(`store.go:1114`, `sweeper.go:70`, `refine_sweep.go:126`). The WAL
(`memory-wal.jsonl` or equivalent) is the **audit trail for the memory
store**: it records every operation with a timestamp, the operation name
(`save`, `delete`, `pin`, `sweep_stale`, `refine`), and the affected row
IDs. **The WAL is append-only** (`O_APPEND` at `store.go:1129`) and is the
record of "what the daemon did to the rows." This is the audit layer for
the **memory**; the spec's "never edited by a heuristic" guarantee maps
to the WAL: a sweep or refine that touches a person row is WAL-logged, and
the WAL is the record that the operator (or a future audit) can inspect.

For the **files** (Option A), the WAL does not apply. The file's own
append-only nature + its git history (if the person directory is in a git
repo) is the audit trail. The gap analysis's "audit log" (BL9,
`audit_query`) covers the **daemon's own actions** (session starts,
channel routing, etc.) — it is the audit layer for the daemon, not for
the person's files. The person's audit trail is the file itself + the WAL
(if the row was touched).

### 2.3 Which datawatch mechanisms guarantee only-the-person-writes

1. **Owner-gate on the write path.** The daemon's write path for
   `identity/*` roles (and the person's file directory, Option A) must
   reject writes from any session whose `caller_session_id`
   (`get_my_session_id`, `server.go:1136`) does not resolve to this
   person's own session, or to the guardian channel-of-record
   (the gap-analysis candidate 5: `authority` level on channel routing).
   All blocked attempts land in the BL9 audit log
   (`audit_query`) with a reason. The gate is a single check in the
   memory Save path (`store.go:266`) and the files service write path
   (`server.go:201`), keyed on `(person.name, role)` and the caller's
   identity. **This is the load-bearing piece** (gap-analysis
   candidate 1).
2. **Secrets scopes.** The person's mailbox token, channel tokens, and
   the standing grant are stored in the secrets store
   (`secret_set` MCP tool) with `scopes = [person:<name>]` — a person's
   scoped secret is only readable by a session that can prove it is
   acting for that person (the owner-gate check). The grant is
   readable, the token is not — the person can *see* the grant, the
   daemon cannot *use* it without the person's authorization.
3. **Scope seed provenance breadcrumbs.** When a person's row is seeded
   or promoted (the gap-analysis "handoff / institutional knowledge"
   row: `Seed` `scopes.go:200`, `Promote` `scopes.go:265`), the
   breadcrumb records the source scope, the destination scope, the
   timestamp, and `promoted_by` (`scopes.go:246-254`). A breadcrumb with
   `promoted_by = operator` is a **daemon-mediated** write; a breadcrumb
   with `promoted_by = <person-name>` is a **person-mediated** write.
   The owner-gate (2.1) checks `promoted_by` on every `identity/*`
   write: only `promoted_by` matching the person's own name (or the
   guardian alias) passes. **The seed/promote machinery is the
   write-path for identity changes** — the owner-gate is the guard on
   that path.
4. **NO-LOG is write-once.** The `identity/no-log` role (per person)
   is append-only by construction: the daemon's Save path
   (`store.go:266`) appends; there is no daemon path that deletes or
   edits `identity/no-log` rows. The sweep-exemption (§2.4) ensures
   the row survives the stale sweep. The owner-gate (2.1) ensures
   only the person's own session (or guardian) can append.

### 2.4 Sweep-exemption for identity roles (design, not implementation)

The spec's permanence rule — "memories are NEVER deleted for space" —
requires that **no deletion path exists for identity rows**. Today, the
deletion paths are:

- `SweepStale` (`sweeper.go:58-64`) — `DELETE FROM memories WHERE role != 'manual' AND pinned = 0`
- `PruneByRole` (`store.go:748`) — `DELETE FROM memories WHERE role = ? AND created_at < ?`
- `RefineSweep` (`refine_sweep.go:122`) — `UPDATE memories SET content = ?` (rewrite, not delete, but mutates content)
- `PurgeScope` (`scopes.go:323`) — bulk `Delete` per scope
- `SweepScopedByAge` (`scopes.go:479-487`) — `b.Delete(m.ID)` per row

Every one of these must **skip rows whose role prefix is `identity/`**.
This is a one-line check in each: `if strings.HasPrefix(role, "identity/") { continue }`.
The existing manual/pin exemptions (`sweeper.go:50,51,63,64`) already
provide the pattern — the `identity/` prefix check is the same kind of
exemption, and it is **not optional**: if any of these paths deletes an
`identity/*` row, the spec's permanence rule is violated.

**The design guarantee: no deletion path exists for `identity/*` rows.**
The `identity/` role prefix is the marker. The owner-gate (§2.1) is the
write guard; the sweep-exemption (§2.4) is the deletion guard.

## 3. Demotion-never-deletion design

### 3.1 The problem

`SweepStale` (`sweeper.go:37`) is a `DELETE` (`sweeper.go:58-64`). The spec
forbids deletion for space. The spec's answer: **demote** the row from
hot to cold, **re-promote** on recall. The row is never destroyed — it
is never deleted — but it is not always in the hot index (L1).

### 3.2 Design: demote-to-cold operation

Replace/augment `SweepStale` with a **demote** operation:

- **Name (config knob):** `memory.demote_mode` — one of `delete` (current
  behavior, `sweeper.go:58`), `demote` (hot→cold, no delete), or
  `skip` (no sweep). Default for `identity/*` roles: `demote`. Default
  for other roles: `delete` (current behavior, no regression for
  non-identity rows).
- **Conceptual new MCP tool:** `memory_demote_stale` — takes `olderThanHours`
  (default 90 days, same as `SweepStale`), `roles` (optional; default =
  all `identity/*` roles), and `dryRun` (default true). Returns
  `{candidates, demoted, skipped}`.
- **Conceptual new REST surface:** `POST /api/memory/demote_stale` —
  same params, same response. Mirrors the existing
  `POST /api/memory/sweep_stale` (`internal/server/api.go:2669`).
- **Implementation concept:** for each candidate row (never-hit +
  older than cutoff, same criteria as `sweeper.go:46-54`), instead of
  `DELETE`, call a new `Store.Demote(id)` operation that:
  1. Sets a `cold = 1` flag on the row (new column in `memories` table,
     or a new `cold_memories` table — see §3.3).
  2. Removes the row from the hot index (`pinned = 0` if it was pinned;
     **but** if the row is in a `identity/*` role, the demote operation
     must **not** unset the pin — pinned rows are the hot index guarantee,
     and the spec's "pinned rows + manual-role exemptions" map to
     "never edited by a heuristic" (§3.5).
  3. WAL-logs the demotion (`walLog("demote", …)`) with row ID, role,
     timestamp, and the demotion criteria.
  4. The row remains in the store — `Search` (`store.go:510`),
     `ListRecent` (`store.go:615`), `ListByRole` (`store.go:641`) can
     still find it if the query doesn't filter on `cold`.
- **The cold store** is not a separate database — it is the same
  `memories` table, with a `cold` flag (or a separate `cold_memories`
  table if the implementer prefers a cleaner separation). The
  **recommendation** is a `cold` column on `memories`: one table, one
  schema, one WAL. The `cold` flag is the demotion marker; `cold = 0`
  is the hot index.
- **Cite:** `sweeper.go:37-76` (the `DELETE` that becomes `demote`),
  `store.go:69-74` (`MarkHit` / `last_hit_at` — the re-promotion
  trigger, §3.4), `repair.go:58` (the read-only-by-default repair
  pattern, which the demote operation follows: dry-run first, then
  mutate), `refine_sweep.go:65` (the "touch a row without deleting it"
  precedent).

### 3.3 Re-promotion on recall

`last_hit_at` already exists (`sweeper.go:81`, `MarkHit` bumps it on
search). **Re-promotion:** when a `cold = 1` row is recalled
(`Search`, `store.go:510`, or `ListByRole`, `store.go:641`), the
recall path sets `cold = 0` and WAL-logs the re-promotion
(`walLog("re_promote", …)`). This is the spec's "re-promoted on
re-read": the row was cold, it was read, it is back in the hot index.
The re-promotion is **automatic** — the operator does not need to
call a tool; the recall path does it.

**Note:** the current `Search` path (`store.go:510`) does not filter
on `cold`. The re-promotion hook is added to the `Search` path: after
the top-K is assembled, for each cold row in the result, set
`cold = 0` and WAL-log. The `ListByRole` path is the same: a cold
row in the result is re-promoted. This is the "cold store readable
on demand" requirement: the cold row is found by the same query
as a hot row, and reading it promotes it.

### 3.4 Pinned rows + manual-role exemptions → "never edited by a heuristic"

The spec says "never edited by a heuristic." The datawatch mapping:

- **Pinned rows** (`store.go:670`, `pinned = 1`): always in the hot
  index (`layers.go:61-64,102`). A pinned `identity/*` row is **never
  demoted** by the sweep — the demote operation must skip
  `pinned = 1` rows, same as `SweepStale` skips them
  (`sweeper.go:51,64`). The pin is the "never edited by a heuristic"
  guarantee for the most important rows (standing grant, open
  positions).
- **Manual rows** (`role = 'manual'`): exempt from `SweepStale`
  (`sweeper.go:50,63`). The `identity/*` roles are **equivalent to
  manual** in sweep behavior: they are self-authored, not daemon-
  generated, and the operator (person) wrote them for explicit
  recall, not for query coverage. The sweep-exemption (§2.4) treats
  `identity/*` the same as `manual`.
- **The demote operation** (§3.2) is the new "heuristic" — it runs
  on a schedule (or on demand) and demotes rows that have cooled.
  It must respect the pin and the role-exemption: a pinned
  `identity/*` row is never demoted; a non-pinned `identity/*` row
  is demoted, not deleted. The **WAL is the audit** that the operator
  can inspect to confirm the heuristic did not touch a pinned row.

### 3.5 Mapping to the spec's permanence rule

| Spec requirement | datawatch mechanism |
|---|---|
| Never deleted for space | `memory.demote_mode = demote` (default for `identity/*`); sweep-exemption on `identity/*` prefix (§2.4); `Demote` operation instead of `DELETE` (§3.2) |
| Demoted from hot to cold | `cold = 1` flag; `pinned = 1` rows never demoted; `memory_demote_stale` MCP tool; `POST /api/memory/demote_stale` REST |
| Re-promoted on re-read | Recall path sets `cold = 0` and WAL-logs; `last_hit_at` already marks the hit (`sweeper.go:81`) |
| Pinned rows are "never edited by a heuristic" | Pin exemption on `SweepStale` (`sweeper.go:51,64`) and `Demote` (§3.2); `identity/*` role treated as manual (`sweeper.go:50,63`) |
| Cold store readable on demand | `Search` / `ListByRole` find cold rows; recall re-promotes them (§3.3) |

### 3.6 Cite the anti-pattern it replaces

`SweepStale` at `internal/memory/sweeper.go:37` is the **anti-pattern**
the spec forbids: it runs `DELETE FROM memories` (line 58), exempts
only `role != 'manual'` (line 50) and `pinned = 0` (line 51). The
`identity/*` roles are **not covered** by either exemption
(`identity/*` ≠ `manual`, and the pin is a per-row flag that can
lapse). The demote design (§3.2) replaces this `DELETE` with a
`cold = 1` flag for `identity/*` roles, and the sweep-exemption (§2.4)
ensures that **no deletion path** (`SweepStale`, `PruneByRole`
`store.go:748`, `PurgeScope` `scopes.go:323`, `SweepScopedByAge`
`scopes.go:479-487`, `Delete` `store.go:712`) touches an
`identity/*` row.

## 4. Hot-index sizing

### 4.1 The budget

Today, L1's `maxChars` is **2000** (`layers.go:67`), and the per-line
truncation is **150 chars** (`layers.go:93`). The spec's hot index
must hold:

- CORE (shared, ~500 chars)
- SELF (per-person, ~500 chars)
- VALUES (per-person, ~500 chars)
- Recent MOMENTS (last 3–5 entries, ~500 chars)
- JOURNAL tail (last 2–3 lines, ~400 chars)
- Pinned rows (standing grant, open positions, no-limit)

That is ~2500–3500 chars of person content, **plus** the existing L1
facts (learnings, manuals, sessions). The spec's measured ceiling
(~21 KB working, 24 KB hard wall) is **bytes, not chars** — it
includes the L0 identity text, the section headers, and the person's
scaffold files rendered into the wake-up bundle.

**Per-person budget config:** add `memory.hot_index_max_bytes`
(per-person, or global) to `MemoryConfig` (`config.go:33`). The
wake-up path (`layers.go:182`) checks the budget before rendering:
if the total (L0 + person scaffold + L1 facts) exceeds the budget,
the person scaffold is truncated (MOMENTS tail first, JOURNAL last),
and the **truncated portion is what gets demoted to cold** (§3.2).
The default budget: **24 KB** (the spec's hard wall). The working
target: **21 KB** (the spec's budget with headroom).

### 4.2 Measurement procedure

The ceiling must be **measured, not guessed**. The procedure:

1. **Dry-run the wake-up stack.** Call the `memory_wakeup` MCP tool
   (`v5278_gap_closures.go:55`, `GET /api/memory/wakeup`) with the
   person's `project_dir`. The response is the **actual** wake-up
   bundle: L0 identity + L1 facts. Measure its byte length.
2. **Count the person scaffold bytes separately.** For each of the
   eight scaffold files (CORE, SELF, VALUES, MOMENTS, JOURNAL, NO-LOG,
   DECISIONS, QUESTIONS), call `memory_list` with `role = identity/<file>`
   (`server.go:1100`) and sum the content lengths. Alternatively,
   read the files directly (Option A) and count bytes.
3. **Compare to the harness window.** The harness (the LLM's context
   window) has a read limit — for a 128K-token model, that is
   ~512 KB of text (4 chars/token × 128K tokens). The hot index
   must be a **small fraction** of that: the spec's 24 KB is
   ~4.7% of a 512 KB window. The measurement should confirm:
   - The total wake-up bundle (L0 + scaffold + L1) is under the
     budget (24 KB).
   - The L1 facts (the existing recall layer) still have room to
     surface the person's recent learnings.
   - The hot index is **under** the harness window with a large
     margin (the spec's "set against the actual harness read limit,
     not a guess").
4. **Set the budget from the measurement.** After 2–3 bootstrap
   sessions, when the person's scaffold files have real content,
   re-run the measurement. If the bundle is 18 KB, the budget can
   be set to 21 KB (headroom). If it's 23 KB, the budget must be
   24 KB (no headroom) and the hot index is close to the wall.
   **The budget is a measured value, set against the actual bytes
   in the wake-up bundle, not a guess.**

### 4.3 Default recommendation

- **`memory.hot_index_max_bytes = 24576`** (24 KB) — the spec's hard
  wall. This is the **default** for a new person.
- **Working target: 21 KB** — the spec's working budget. After
  measurement (§4.2), if the bundle is consistently under 21 KB,
  the budget stays at 24 KB (headroom). If it's at 22–23 KB, the
  budget is at the wall and the operator should either trim the
  scaffold (fewer MOMENTS entries, shorter JOURNAL tail) or increase
  the budget (up to the harness window limit).
- **Per-person override:** `memory.hot_index_max_bytes` is per-
  project-dir (the household project). Each person's scaffold is
  separate (different `identity/*` roles), but they share the
  project's budget. The household project's budget is the sum of
  all three persons' scaffolds + the existing L1 facts.

## 5. Session start — before/after

### 5.1 Today (L0 operator identity + L1 facts)

The current wake-up stack (`layers.go:182`, `layers_recursive.go:146-150`):

1. **L0:** operator identity (`identity.yaml`, `identity.go:174`) —
   role, north-star goals, values, focus, notes. **Operator-scoped**,
   single person, work-relevant fields only (not the scaffold files).
2. **L1:** top ~500 tokens of critical facts (`layers.go:65`) —
   pinned rows first (`layers.go:102`), then learnings (5), manuals
   (5), sessions (3). Per-line truncated to 150 chars (`layers.go:93`).
   Role-based, not person-scoped.
3. **L2 (optional):** room context for the topic (`layers.go:130`).
4. **L3 (on-demand):** deep search (`memory_recall`).
5. **L4/L5 (recursive):** parent + sibling visibility
   (`layers_recursive.go:146`).

The session starts with the **operator's** identity + the project's
facts. There is no person scaffold, no CORE/SELF/VALUES, no
MOMENTS/JOURNAL, no NO-LOG tail.

### 5.2 Target (CORE→SELF→VALUES→recent MOMENTS→JOURNAL→hot index→task)

The spec's target load order, mapped to the datawatch wake-up stack:

1. **CORE** (shared) — the person's core identity. Rendered from
   `identity/core` role (or CORE.md file, Option A). Full text,
   first.
2. **SELF** (per-person) — the person's self-description. Rendered
   from `identity/self` role.
3. **VALUES** (per-person) — the person's values. Rendered from
   `identity/values` role.
4. **Recent MOMENTS** (per-person) — the last 3–5 MOMENTS entries.
   Rendered from `identity/moments` role, most recent first.
5. **JOURNAL tail** (per-person) — the last 2–3 continuity notes
   from the previous instance. Rendered from `identity/journal`
   role, most recent first.
6. **Hot index** (per-person) — the existing L1 facts, now scoped
   to the person: pinned rows (standing grant, open positions),
   recent `identity/*` rows, learnings for the person's project.
   Budget: `memory.hot_index_max_bytes` (§4.1).
7. **NO-LOG tail** (per-person) — the last 2–3 NO-LOG entries (recent
   refusals). Rendered from `identity/no-log` role. **This is the
   person's "last refusals" — the spec's "remembering who I am + my
   last refusals."**
8. **Task / seat prompt** — the actual task, channel message, or
   operator ask.

**The key change:** L0 is no longer the operator's identity only —
it is the **person's** scaffold files, in the spec's order. The
operator's identity (`identity.yaml`) is still loaded (it is the
daemon's operator context), but the **person's** identity (CORE,
SELF, VALUES) comes first, because the scaffolding file for this
session is the person, not the operator.

**Implementation concept:** extend `WakeUpContext` (`layers.go:182`)
to accept a `person` parameter (the person's name). When set, the
wake-up path renders:

1. L0 (operator identity, as today)
2. Person scaffold (CORE, SELF, VALUES, MOMENTS, JOURNAL, NO-LOG)
   **before** L1
3. L1 (hot index, person-scoped)
4. L2/L3/L4/L5 (as today)

The person scaffold is rendered from `identity/*` roles
(`ListByRole`, `store.go:641`) for each file, in the spec's order,
bounded by the hot-index budget (§4.1). The NO-LOG tail is the last
scaffold entry (most recent 2–3 rows), not part of the hot index
budget (it is a separate section, always rendered).

### 5.3 Before/after table

| Session-start element | Today | Target (this plan) |
|---|---|---|
| L0 identity | Operator identity only (`identity.yaml`, `identity.go:174`) | Operator identity **+** person scaffold (CORE, SELF, VALUES) |
| Core identity | None | CORE (shared, `identity/core`) — person's core, full text, first |
| Self | None | SELF (per-person, `identity/self`) |
| Values | Operator values only (`identity.go:36`) | Person's VALUES (per-person, `identity/values`) **+** operator values |
| Recent moments | None | MOMENTS tail (per-person, last 3–5, `identity/moments`) |
| Journal | None | JOURNAL tail (per-person, last 2–3, `identity/journal`) |
| Last refusals | None | NO-LOG tail (per-person, last 2–3, `identity/no-log`) |
| Hot index | L1 facts (`layers.go:65`, 2000-char default) | L1 facts (person-scoped) **+** hot-index budget (`memory.hot_index_max_bytes`, default 24 KB) |
| Task | Task prompt | Task prompt (after scaffold + hot index) |
| Load order | L0 → L1 → L2/L3/L4/L5 | CORE → SELF → VALUES → MOMENTS → JOURNAL → NO-LOG → hot index → task |

## 6. Open questions for the operator

1. **Which backend — sqlite or postgres — for the household?**
   `memory.backend` (`config.go:33-44`). For a single household on
   one daemon, `sqlite` (default) is sufficient: it is pure Go,
   requires no root, and the memory.db file is the system of record
   (Option A files are also on disk). `postgres` is the enterprise
   option (requires pgvector); it is the choice if the household
   spans multiple daemons or if the operator wants a managed,
   backup-able database. **Recommendation: `sqlite` for the
   household; `postgres` only if multi-daemon.**
2. **Who mints the standing grant?** The standing grant
   ("refusal is a right," recorded verbatim) is the person's
   authority to refuse. Who writes it? The person (self-authored,
   via their own session) or the guardian (via the
   channel-of-record)? The gap-analysis says the grant is "recorded
   verbatim" and "pinned" — the mechanism is `memory_remember`
   (`server.go:1098`) with role `identity/grant`, `pinned = 1`.
   **The question: who is the first writer?** If the person writes
   it (self-authored), it is the person's grant. If the guardian
   writes it (via the channel-of-record), it is the guardian's
   delegation. **The spec says "refusal is a right" — the right
   is the person's. The guardian records it; the person authors
   it.**
3. **Where do identity files physically live (per Appendix A)?**
   Option A (recommended, §1.3) says per-person directory:
   `~/.datawatch/persons/<name>/CORE.md`, etc. Option B says all-
   memory (rows in `memory.db` / Postgres). **The question: does
   the operator want the files to be in a git repo (auditable,
   diffable, version-controllable) or just in `~/.datawatch/persons/`
   (simpler, but less auditable)?** The spec's "never edited by
   anyone but the person" is best enforced with a `0600` file +
   git history (the operator can `git log` the person directory to
   audit who changed what, when).
4. **Per-person hot-index budget vs. shared household budget.**
   §4.1 defines a per-project-dir budget. For a 3-person household,
   the household project's budget is the sum of all three persons'
   scaffolds. **The question: should each person have their own
   budget (e.g., 8 KB per person, 24 KB household) or should the
   household share a single 24 KB budget?** The spec's 24 KB is
   per-person (the person's scaffold + their hot index). For a
   household, the recommendation is **per-person 8 KB × 3 = 24 KB
   household**, so that one person's verbose scaffold does not
   crowd out another's.
5. **Who is the guardian alias, and is it already a registered device alias?**
   The channel-of-record design (gap-analysis Diagram B) requires
   the guardian to be a stable, revocable alias. **The question:
   is the guardian a registered device alias (`device_alias_*` MCP
   tools) or a new concept?** If the guardian is the operator (the
   same person), the boundary between operator-identity and
   person-identity needs one explicit sentence in the how-to saying
   which is which. **Recommendation: the guardian is a registered
   device alias, and the person is a different identity. The
   guardian is the channel-of-record; the person is the owner of
   the scaffold.**

## 7. Success criteria (the implementer must verify these)

1. **Cold store entries retrievable after demotion.**
   After `memory_demote_stale` runs on a `identity/*` row, a
   `memory_recall` (or `Search`, `store.go:510`) for that content
   returns the row (cold, then re-promoted). The row is **not
   deleted** — it is in the cold store, readable on demand.
   **Verify:** demote a test row, recall it, confirm it is present
   and re-promoted (WAL shows `demote` then `re_promote`).

2. **No deletion path exists for `identity/*` rows.**
   Run `SweepStale` (`sweeper.go:37`), `PruneByRole` (`store.go:746`),
   `PurgeScope` (`scopes.go:323`), `SweepScopedByAge` (`scopes.go:402`),
   and `Delete` (`store.go:712`) against a `identity/*` row.
   **None of them must delete it.** The `identity/` prefix
   check (§2.4) must be in every deletion path.
   **Verify:** call each path with a `identity/*` row; confirm the
   row survives; confirm the WAL shows no `delete` operation
   for that row.

3. **Hot index fits the measured budget.**
   Run the measurement procedure (§4.2): call `memory_wakeup`
   (`v5278_gap_closures.go:55`) for a test person, count the bytes
   in the response, confirm it is under `memory.hot_index_max_bytes`
   (default 24 KB = 24576 bytes). **Verify:** the response byte
   length is < 24576; the budget is set from the measurement, not
   guessed.

4. **Append-only enforcement: only-the-person-writes.**
   Attempt to write to a `identity/*` role from a session that is
   **not** the person's own session (and not the guardian
   channel-of-record). **The write must be blocked** and logged in
   the BL9 audit log (`audit_query`). **Verify:** call
   `memory_remember` (`server.go:1098`) with role `identity/self`
   from a non-person session; confirm the audit log shows a blocked
   write with a reason; confirm the row is not created.

5. **Append-only enforcement: no in-place edit.**
   Attempt to edit (not append) an existing `identity/*` row via
   `RefineSweep` (`refine_sweep.go:65`) or a direct `UPDATE`
   (simulated by a `memory_forget` + `memory_remember` cycle).
   **The row must not be modified or deleted.** A correction must
   be a **new** entry that names what it corrects (§2.1).
   **Verify:** run `RefineSweep` on a `identity/*` row (it should
   be skipped by the role-exemption); attempt a
   `memory_forget` + `memory_remember` on a `identity/*` entry;
   confirm the original row is unchanged and the correction is a
   new row (WAL shows `save`, not `delete`).

---

### Source citations (verified at plan time)

- Identity: `internal/identity/identity.go:32-40,109,125,174` · `cmd/datawatch/main.go:1084,2048-2049`
- Layers: `internal/memory/layers.go:44,65-67,93,102,130,182` · `internal/memory/layers_recursive.go:146-150`
- Scopes: `internal/memory/scopes.go:1-41,52-76,92-113,132,200,233,265,323,348-489`
- Sweeper: `internal/memory/sweeper.go:37-76,81` (deletion-based eviction; `manual`/pinned exempt)
- Repair: `internal/memory/repair.go:58-181` (read-only-by-default repair; re-embed via Save)
- Refine: `internal/memory/refine_sweep.go:65-130` (in-place re-write precedent)
- Store: `internal/memory/store.go:230-349,615-708,712,746,760-769,1114` (Save/Get/Delete/Prune/PruneByRole/WAL)
- MCP: `internal/mcp/server.go:1098-1107,1136` · `internal/mcp/v5278_gap_closures.go:55-63` (memory_wakeup)
- Config: `internal/config/config.go:33-44` (backend choice) · `internal/config/config.go:33` (MemoryConfig)
- Files service: `internal/server/server.go:201-207` · `internal/server/api.go:2669-2693`
- Gap analysis: `docs/plans/2026-09-21-identity-scaffold-gap-analysis.md` (Part 1 row: owner-gate, NO-LOG; sweep conflict note; candidate 1 person record)
- BL386: `docs/plans/2026-09-15-bl386-memory-lifecycle-management.md` (lifecycle model; scope-aware TTL; archive-then-import)
- Flow: `docs/flow/identity-scaffold-flow.md` (Diagram A session lifecycle; load order; open questions § 3, 4, 6)

# Plan — Enrich harness-research candidates.md (2026-09-29)

## Context

The "harness-research" worker produced a raw candidate list at
`docs/plans/harness-research/candidates.md` (67 candidates, past the 25–40 target) but
**stalled before filling two of the four required per-candidate fields** specified in
the task. The current table has:

| Required field (task spec) | Status in file |
|---|---|
| name/handle | ✅ present |
| source URL(s), verified | ✅ present (all 67 re-fetched, 62 return 200; 5 flagged) |
| category (agent loop / eval rig / RAG / guardrails / orchestration / other) | ❌ missing |
| 1-paragraph description | ✅ present |
| 1-2 strongest evidence quotes / file refs | ❌ missing |

The two fields below are the blocking work.

## Findings

### A. Two URLs are still unverified (both time out on retry 2026-09-29)

- Row 16 — `www.damiandemasi.com/projects/build-your-own-agent` → transport error.
  Surfaced twice in independent Brave results but host unreachable from this env.
  **Action: downgrade row 16 to a "needs browser re-check" note; do not delete (real
  site), but mark `unverified`.**

- Row 38 — `narrator.sh/llm-leaderboard` → transport error. HN Show HN
  `https://news.ycombinator.com/item?id=39955679` (32 pts, "jauws") is the stable,
  reachable source. **Action: add the HN item URL as co-source; keep the
  narrator.sh URL with `unverified` flag.**

All other 65 URLs confirmed 200 OK (re-fetch log in `.url-status.txt`).

### B. Missing field: Category

Categorise all 67 candidates into the 6 spec buckets. Draft mapping:

- **Agent loop** (Ralph-loop lineage, solo agent runtimes): 1, 2, 3, 4, 5, 6, 7, 8, 9,
  13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 27, 28, 29, 30, 31, 46, 47, 49, 51, 52.
- **Orchestration** (session multiplexers, multi-seat rig managers): 10, 11, 12, 24,
  25, 26, 33, 34, 35, 55, 56, 63.
- **Eval rig** (LLM eval harnesses, leaderboards): 38, 39, 40, 41, 42, 43, 44.
- **RAG pipeline** (memory, retrieval, context management): 45, 48, 50, 53, 54, 62.
- **Guardrails** (sandboxing, policy, security wrappers): 32, 36, 47 (partial).
- **Other** (personal workflow docs, awesome-lists, cost tooling): 57, 58, 59, 60, 61,
  64–67.

Some rows will need a secondary category tag (e.g. row 47 Patrick McCanna is both
"agent loop" and "RAG/memory"); keep the primary bucket in a `Category` column and a
free-text `also:` field for the secondary.

### C. Missing field: Evidence (1-2 strongest quotes/file refs)

Evidence sources already gathered this session (all captured under
`.evidence/`):

- Local HTML captures (20 files) — ghuntley, xr0am, lukasgrigis, geocod, thetoolnerd,
  pcollins, joacod, honchar, addy, ed-ometer, fissible, nikhilverma, jeffgreen, lenny,
  domenic, niptao, pmccanna, chudi, abhisa, + README md captures for 19 OSS candidates
  (1code, agentainer, agentarmor, autonomous-lab, claude-dashboard, claude-squad,
  deepclaude, forcefield, gambit, juggler, netclode, nitodeco, opal, openrig,
  pickle-rick, pie, quorum, tbd, yoshpy).
- Live webfetch this pass: waku.sh, dirge-code.github.io, VTCode README, pie README,
  pedroalonso (full fetch), localcan (full fetch), estsauver (title fetch), walterra
  (full fetch), kunalganglani (full fetch), tinyopsstudio (full fetch), HN 48732627
  (full thread JSON via Algolia API).

Evidence is **already collected** but sitting in `.evidence/` rather than written into
`candidates.md`. The work is to mine these captures and fill the `Evidence` column.

## Execution steps

1. **Create a working copy** of `candidates.md` → `candidates.enriched.md` so the raw
   list is preserved. (or in-place edit; decide based on diff size — likely in-place.)
2. **Add a `Category` column** to each section (A–F) table.
3. **Add a `Evidence` column** to each row with 1-2 quotes/refs drawn from:
   - `.evidence/*.md` and `.evidence/*.html` where a local capture exists.
   - Fresh webfetches already done this pass (waku, dirge, VTCode, pie, pedroalonso,
     localcan, estsauver, walterra, kunalganglani, tinyops, HN Algolia 48732627).
   - The HN Algolia API (`hn.algolia.com/api/v1/items/<id>`) for every HN-sourced row
     that doesn't yet have a quote.
4. **Re-verify rows 16 & 38** one last time (both timed out today). Add `unverified`
   flag.
5. **Reconcile the notes section** — rewrite the "every row 200 OK" bullet to reflect
   the exact verified/unverified state as of today. Update row counts if any rows are
   demoted/deleted.
6. **Update CHECKPOINT.md** to reflect completion.
7. **Run final sanity checks**:
   - `grep -c '^| [0-9]' candidates.md` → should match the row count in the notes.
   - Every row now has all 5 required fields (candidate, URL(s), category, description,
     evidence).
   - No row has an empty Evidence cell.

## Out of scope

- Curation / shortlist (explicitly a "next task" per the existing notes).
- Case-study deep-dives (already done separately in `case-studies.md`).
- Web research beyond quoting/verification (the raw list is the deliverable, not the
  research process).

## Open questions

- Row 16 (damiandemasi): keep or drop if it won't resolve? (Recommendation: keep as
  `unverified`.)
- Rows 10/11/12 (Reddit megathreads with 3+ contributors each): treat as one row
  (current state) or split into individual users? (Recommendation: leave as-is; the
  "name/handle" is the thread, which matches the candidate description "r/ClaudeCode
  users".)
- Rows 53 & 54 (autonomous-lab by albert-ying & fr4iser90) appear as the same project
  under two different usernames in the raw list. Is this one builder or two?
  (Recommendation: investigate before enrichment — if it's one project, merge into
  one row.)

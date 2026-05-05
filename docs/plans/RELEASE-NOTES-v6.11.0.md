# Release Notes — v6.11.0 (BL260 — Council Mode multi-agent debate)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.0
Smoke: 100/0/7

## Summary

BL260 — Council Mode shipped with full 7-surface parity. **Closes the BL257-BL260 PAI parity arc** that began with the 2026-05-05 audit ("I don't see the identity robot interview, what else was missed in setting up PAI features?").

PAI source: `docs/plans/2026-05-02-pai-comparison-analysis.md` §4 + Recommendation M2.

For v6.11.0 the LLM-driven persona responses are stubbed (deterministic placeholders that mention the persona + system prompt summary + proposal length). Real per-persona LLM inference and synthesizer wiring lands in a v6.11.x follow-up.

## Added

### `internal/council` package

- Types: `Persona`, `Round`, `Run`, `Orchestrator`, `Mode`.
- 6 default personas seeded to `~/.datawatch/council/personas/<name>.yaml` on first run:
  - `security-skeptic` — attack vectors, privacy, supply chain
  - `ux-advocate` — operator experience, error recovery
  - `perf-hawk` — latency, throughput, IO, unbounded growth
  - `simplicity-advocate` — minimalism, push back on complexity
  - `ops-realist` — deployment, observability, on-call burden
  - `contrarian` — devil's advocate, steel-man alternatives
- Operators can edit YAML files or drop new ones; `Orchestrator.Personas()` reflects on every read.
- Runs persisted to `~/.datawatch/council/runs/<id>.json` with full per-round per-persona response history.
- `LLMFn` injection point allows the daemon (or tests) to substitute real LLM inference.
- 11 unit tests pass.

### REST surface (4 endpoints)

- `GET /api/council/personas` — list personas (sorted).
- `POST /api/council/run` — body `{proposal, personas[], mode}`. Returns full Run.
- `GET /api/council/runs[?limit=N]` — list runs (most recent first).
- `GET /api/council/runs/{id}` — fetch one run.
- Audit-logged on run (`action=council_run`).

### MCP tools (4)

- `council_personas`, `council_run`, `council_list_runs`, `council_get_run`.

### CLI

- `datawatch council personas`
- `datawatch council run --proposal "..." [--personas a,b] [--mode debate|quick]`
- `datawatch council runs [--limit N]`
- `datawatch council get-run <id>`

### Comm verb

- `council` / `council personas` (list)
- `council run <mode> <proposal>` (mode = debate or quick)
- `council runs`
- `council get-run <id>`

### PWA

- Settings → Agents → Council Mode card.
- Persona checkbox list (all enabled by default).
- Proposal textarea + mode picker (Quick 1-round / Debate 3-round) + Run button.
- Recent runs (last 5) with mode badge, persona × round count, run-id, detail button.

### Locale

- 14 new keys × 5 bundles (`council_*`).

### Smoke

- New step "16. v6.11.0 BL260 — Council Mode: personas + quick run" — fetches personas, runs quick contrarian debate, asserts run id returned.

## PAI parity arc — CLOSED

This release closes the four PAI features that were retro-filed on 2026-05-05 after the audit:

| BL | Feature | Shipped | Notes |
|---|---|---|---|
| BL257 | Identity / Telos + interview robot-icon | v6.8.0 + v6.8.1 | Wake-up L0 injection live |
| BL258 | Algorithm Mode (7-phase per-session) | v6.9.0 | Operator-driven advance; auto-detect TBD |
| BL259 | Evals Framework | v6.10.0 + v6.10.1 | 4 graders; llm_rubric stubbed |
| BL260 | Council Mode | v6.11.0 (this) | 6 personas; LLM responses stubbed |
| BL261 | v6.7.6-followup padding bug | v6.7.7 | Pipeline / Orchestrator / Skills cards |

Plus BeCreative deferred (low value) and Daemon public profile frozen per operator decision.

## What didn't change

- No new go-mod dependencies.
- Existing surfaces untouched.

## Mobile parity

[`datawatch-app#56`](https://github.com/dmz006/datawatch-app/issues/56) updated with shipped scope.

## Sequence reminder

The PAI parity arc is now closed. v6.11.x follow-up work (real LLM responses for Council, real LLM grading for Evals, auto phase-detection for Algorithm Mode) will be filed as separate BLs when prioritized.

## See also

- CHANGELOG.md `[6.11.0]`
- `docs/plan-attribution.md` (BL260 row updated to ✅ v6.11.0)
- `docs/plans/2026-05-05-bl257-260-pai-parity-plan.md` (entire arc complete)

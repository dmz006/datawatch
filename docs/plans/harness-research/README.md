# harness-research — Directory Index

**Root:** `docs/plans/harness-research/`
**Last close-out:** 2026-09-30 (v8.37.4 baseline)

Two research passes share this directory, now fully reconciled:

- **Framework pass (Sept 7-11).** Catalogs 16 public vendor harnesses (`examples-catalog.md`) and derives the baseline gap analysis + first 5 proposals.
- **Practitioner pass (Sept 17-30).** Studies 20 individual builders (`case-studies.md`), extracts 19 recurring patterns (`usage-patterns.md`), maps each against v8.37.4 (`datawatch-mapping.md`), and produces the re-grounded proposals (`enhancement-proposals.md`).

The `synthesis.md` close-out executive summary, `diagrams.md` (pattern→package + top-recommendation data-flow), and this `README.md` index are the load-bearing documents — read those first.

---

## All files in `docs/plans/harness-research/`

| File | One-line description |
|---|---|
| `README.md` | This file — the index, one-line description per file, and the recommended reading order. |
| `synthesis.md` | **Close-out executive synthesis:** the 5 most common builder patterns with the case names behind them, the 3 most important datawatch gaps, the top 3 recommended enhancements with their why-them-first rationale, and the cross-pass reconciliation. **Read this first.** |
| `diagrams.md` | Two Mermaid diagrams: (1) each recurring builder pattern → the datawatch internal package(s) that implement it (or are the named gap), and (2) the top recommendation (P1 per-LLM-call span artifact) proposed data flow. Conceptual, not code. |
| `case-studies.md` | 20 curated individual builders (from 63 raw candidates), each with the 5 required fields: problem, harness architecture/data flow, recurring usage loop, tools/backends, what breaks / what they wish existed. |
| `candidates.md` | The over-collection pass: 63 raw individual builder candidates + 4 awesome-list curation surfaces, with URLs, category, description, and source. HTTP statuses spot-checked. |
| `usage-patterns.md` | The 19 recurring cross-builder patterns (each ≥2 builders), ranked by builder count, plus a one-off techniques appendix and the expected-areas verification table. |
| `datawatch-mapping.md` | The 19 patterns mapped against the v8.37.4 baseline: MATCH / PARTIAL / MISSING per pattern, evidence + deltas, and the 5 ranked gaps (MISSING: per-LLM-call artifact; PARTIAL: routing/verifier/tune/triage/debate). |
| `enhancement-proposals.md` | **v2 proposals re-grounded in the 19-pattern evidence:** P1 span artifact, P4 eval sweep + DAG gate, P2 stall-ladder, P5 VRAM lock, P3 live model flip, P6 instruction back-write, P7 debate-methods, plus the 3 explicit declines and the full sequenced recommendation. |
| `methodology.md` | The definition of "AI harness," the 6 target categories (eval, RAG, guardrail, orchestration, verification, session/state), and the inclusion criteria. |
| `examples-catalog.md` | 16 public vendor harnesses, each with stack, data flow, license/stars, and a "vs. DataWatch" comparison row. Stars/URLs re-verified 2026-09-10. |
| `feature-themes.md` | The 6 candidate enhancement themes the framework gap analysis produces (eval sweep, drift, provenance, red-team, RAG grounding, cost/quality routing) — needs + value, not concrete proposals. |
| `impact-matrix.md` | The effort×impact 4-quadrant the 6 themes map into, with the sequenced read. |
| `synthesis-v2.md` | Exec-summary of the practitioner pass: 5 most common patterns, 3 ranked gaps, top-3 enhancements with sequencing, and the framework⇄practitioner reconciliation. |
| `enhancements-v2.md` | The 9 original v2 proposals from the practitioner pass, differentiated against the 5 framework proposals, with 3-tier sequencing and a traceability diagram. (Superseded in detail by the close-out `enhancement-proposals.md` v2 — kept for provenance.) |
| `.evidence/` | Local HTML/MD captures of the primary-source pages behind the 20 case studies. |
| `.urls.txt`, `.url-status.txt`, `.verify-urls.sh` | URL collection, status-check results, and the re-verification script. |

---

## Recommended reading order

Shortest-to-longest, load-bearing documents first, evidence last:

1. `synthesis.md` — the close-out executive summary (patterns, gaps, top-3, why-them-first). **Start here.**
2. `diagrams.md` — the pattern→package map and the P1 data-flow, to see the shape of the work.
3. `datawatch-mapping.md` — the MATCH/PARTIAL/MISSING table and the 5 ranked gaps behind the synthesis's gaps.
4. `enhancement-proposals.md` — the full P1–P7 proposal set, the declines, and the sequenced recommendation.
5. `usage-patterns.md` — the 19 recurring patterns the mapping and proposals draw from.
6. `case-studies.md` — the 20 builder case studies behind the patterns.
7. `candidates.md` — the 63 raw candidates behind the 20 case studies.
8. `synthesis-v2.md` + `enhancements-v2.md` — the earlier practitioner-pass exec-summary and proposals (provenance for the v2 reconciliation).
9. `feature-themes.md` + `impact-matrix.md` — the framework-pass themes and their effort×impact positioning.
10. `examples-catalog.md` — the 16 vendor harnesses the framework pass cataloged.
11. `methodology.md` — the inclusion criteria (read if you are auditing a selection).

*(Optional: `.evidence/`, `.urls.txt`, `.verify-urls.sh` — the primary-source captures and URL-check trail.)*

---

*Index for the directory. All listed files exist as of the 2026-09-30 close-out; cross-references verified. No code was written for this documentation set.*

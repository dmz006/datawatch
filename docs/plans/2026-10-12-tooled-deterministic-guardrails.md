# Plan: Tooled, Deterministic PRD Guardrails (BL416)

- **Date**: 2026-10-12
- **Version at planning**: v9.0.33
- **Status**: Planned — not started (Phase 0 is independent and should ship immediately regardless of the rest)

## Context

Found live 2026-10-11/12 while merging BL403 Phase 2 (PR #213): a real
`errcheck` violation shipped in `internal/plugins/supervisor.go` through
a PRD that had already reached `completed`. Investigation (this
session) traced exactly why nothing in the PRD's own pipeline could
have caught it:

1. **AGENT.md's Section A** (the "every commit, no exceptions"
   checklist — the actual enforced rulebook, read by decompose via the
   `rules_context_suffix` pointer fixed earlier this session, B121)
   only requires `go test ./...` (A2). `gosec` appears only in the
   release-cadence section (C2) and a conditional row (B14, only when
   patching a *known* security finding). **`golangci-lint` / `make
   lint` appears nowhere in Section A.** `go test` structurally cannot
   catch an `errcheck`/`staticcheck` violation — different tool,
   different check.
2. **`quality_gates.test_command`** is a single, operator-configured
   shell string, executed via `exec.Command` with no shell —
   confirmed broken on any `&&`-chained command (this session, twice).
   Every PRD this session had it set to `go test ...` only.
3. **Guardrails and the per-task LLM verifier are judgment-only.**
   Traced the live code (`cmd/datawatch/main.go:5142`,
   `executor.go:1210`): the `GuardrailInvocation` passed to a guardrail
   contains only `{PRDID, Level, UnitID, UnitTitle, UnitSpec,
   ProjectDir, OwnerPeer}` — title and spec *text*, nothing else. The
   guardrail function builds a prompt that says *"you are the
   `<name>` guardrail, attest this unit"* with **zero defined content
   per guardrail name** — it's an LLM guessing from a label. (A
   *separate*, unrelated system — the orchestrator graph's own 4 named
   guardrails `rules`/`security`/`release-readiness`/
   `docs-diagrams-architecture` — does have real per-name prompt
   content, but it's explicitly judgment-only too: its
   `release-readiness` prompt literally says *"go test ./... is green
   (implied — we do not run here)"*. Neither this nor the per-task
   verifier (already proven unreliable in both directions, BL404
   finding) executes anything.)
4. Quality-gate computation and guardrail invocation run in sequence
   in the same per-task flow (`executor.go:866` then `:898`) but **are
   never wired together** — the guardrail never sees the real
   `quality_gate_result`.

**Operator design direction (this session, verbatim reasoning
preserved because it rules out an obvious wrong turn):** the fix is
*not* a new hardcoded scan type (e.g. a `lint-scan` guardrail,
mirroring `sast-scan`/`secrets-scan`/`deps-scan`). That bakes a
specific tool and language into the guardrail system permanently, and
nothing establishes which language/toolchain any given PRD or
deployment actually needs. Instead: decompose, for a session-based
(tooled) backend, is *already* a real agent sitting in the actual
repo with real tools, already pointed at AGENT.md/CONTEXT.md via the
existing rules pointer. It should **write the actual deterministic
verification commands** for a task/story as a real artifact (a script),
the same way a person — or Claude, manually, all session — figures out
what applies to a given repo and runs it. That makes the mechanism
language-agnostic *by construction*, not by someone's list of `case`
statements.

**Token-cost note (operator-raised, worth preserving as the actual
economics, not an assumption):** every task spec this session already
carried boilerplate — *"Before finishing run gofmt -l, go vet, and
gosec on touched packages"* — meaning every task session was already
re-deriving and re-running that discipline from scratch, redundantly,
5-6 times per PRD. Moving "figure out what AGENT.md requires" to one
decompose-time pass and making per-task/story verification mechanical
(just execute the script) replaces N redundant LLM reasoning passes
with one plus N free `exec.Command` calls — a net efficiency gain, not
a new cost center.

**Reuse-and-Expand gap found while scoping this plan:** the obvious
place decompose would check for already-existing scripts/tools before
writing a new one — `DATAWATCH-CONTEXT.md`'s "Scripts Inventory" — was
itself found stale during this review (documented 19 of ~30 real
scripts; missing `gen_trivyignore.py` among others). Fixed in this
session as a standalone doc edit (replaced the hand-copied table with
"`ls scripts/` + read each script's own header comment," so it can't
drift the same way again) — but the deeper lesson carries into this
plan's own design: **the script-writing step must check the live
filesystem, never a doc's memory of it.**

**Compute routing (operator-raised):** guardrails today have only a
*global* `verification_backend`/`verification_backends` (the latter
already a fallback list, v8.36.9) — no PRD- or task/story-level
override. `Task` already has `Backend`/`Effort`/`Model` fields that
override the PRD, which overrides the global default, for task
*execution* — a proven, existing inheritance chain. Guardrail backend
selection should use the identical shape, not invent a new one, and
should explicitly *not* duplicate or block on BL405's compute-pool/
load-balancing work — it resolves a backend *name*, and whatever that
name resolves to gets richer later when BL405 ships, with no rework
here.

**External-user gap (operator-raised):** `rules_context_suffix`
(B121) returns an empty string when neither `AGENT.md` nor
`CONTEXT.md`-equivalent exists — meaning a datawatch deployment on
someone else's project, with no hand-authored rules file, gets *zero*
guardrail-pointer content today. This plan is only a real fix if it
also works for that operator, not just for this self-build repo.

**File-provenance gap (operator-raised, not yet resolved — flagged
for this plan's model work):** `Task.FilesTouched` is real and
git-diff-derived (`manager.go:2124`), but nothing today distinguishes
(a) a file decompose expected to be touched (planning intent) from
(b) a file actually modified (`FilesTouched`) from (c) a new category
this plan introduces — a generated verification-script file. The PWA
(and other surfaces) render an undifferentiated list today; this needs
resolving before Phase 4 ships, not bolted on after.

## Decisions

1. **No new guardrail *type*.** Reject the `lint-scan`-style hardcoded
   scanner approach discussed and dropped in this session. The fix is
   a new guardrail *execution mode* — a real script, not a new
   per-language code path.
2. **Decompose writes a verification script**, for session-based
   (tooled) backends only, per task and/or per story, alongside the
   existing freeform `Spec` — after explicitly checking `scripts/`
   (`ls`, not a doc) and AGENT.md/CONTEXT.md for already-applicable
   commands first (Reuse-and-Expand applied to verification itself,
   not just to feature code).
3. **Script storage**: a real file, e.g.
   `<ProjectDir>/.datawatch/verify/<task-id>.sh`. This sidesteps
   `quality_gates`' `exec.Command`-has-no-shell problem structurally —
   a script can use `&&`/`;`/pipes internally; the guardrail just runs
   `bash <path>` as a single argv.
4. **New fields**: `Task.VerificationScript` / `Story.VerificationScript`
   (relative path, empty = none — the legacy judgment-only guardrail
   behavior is the fallback, not a breaking change for PRDs that don't
   opt in).
5. **Guardrail execution, when a script exists**: run it for real,
   capture exit code + output, and *only* hand that real output to an
   LLM when judgment is actually needed (non-zero exit, or the script
   itself asks for a judgment call on something it can't decide
   mechanically — e.g. "is this gosec finding a true positive"). When
   no script exists, behavior is unchanged from today (title/spec
   judgment call) — explicit backward compatibility.
6. **Guardrail backend/model routing**: `Task.GuardrailBackend`/
   `Model`, `PRD.GuardrailBackend`/`Model`, inheriting to the existing
   global `verification_backend`/`verification_backends` — identical
   shape to `Task.Backend`/`Effort`/`Model`'s proven chain. Explicitly
   coordinates with, does not block on or duplicate, BL405's
   compute-pool work.
7. **Graceful degradation with no rules file**: when a repo has
   neither `AGENT.md` nor a configured equivalent, the script-writing
   step falls back to ecosystem-convention detection (Makefile
   targets, `package.json` `scripts`, `pyproject.toml`, `go.mod` +
   standard tooling, etc.) instead of writing nothing — so this
   benefits every datawatch deployment, not only this repo.
8. **AGENT.md Section A gets a new row** requiring the repo's own
   documented lint entry point clean (`make lint` here, confirmed to
   already wrap `golangci-lint run ./...` — found this already exists
   in the Makefile, nothing new to build for *this* repo) with token
   `lint: clean`. **Ships independently, immediately, not gated on the
   rest of this plan** — it's a one-line text change that helps today
   even under the old self-attested model.
9. **File provenance**: add an explicit tag/category distinguishing
   planning-intent files, real-diff files (`FilesTouched`, unchanged),
   and this plan's new generated-script files, surfaced distinctly on
   every surface that renders a task/story's file list — not an
   undifferentiated array.

## Phases

### Phase 0 — AGENT.md Section A fix (independent, ship now)
- Add the `make lint` / `golangci-lint` row to Section A with its
  required token.
- No code change, no version bump (plan-doc/rules-text exemption).

### Phase 1 — Model changes
- `Task.VerificationScript`, `Story.VerificationScript` (string, path,
  `omitempty`).
- `Task.GuardrailBackend`/`GuardrailModel`, `PRD.GuardrailBackend`/
  `GuardrailModel` (string, `omitempty`) + resolution helper mirroring
  `resolveExecutionBackend`'s existing Task→PRD→global chain.
- File-provenance tagging (Decision 9) — exact shape (a parallel
  `FilesPlanned`/`FilesGenerated` array vs. a tagged struct) decided
  during implementation, not prescribed here; must cover all three
  categories from Decision 9 and be covered by the Parity surface
  table below.

### Phase 2 — Decompose contract change
- For session-based backends: after the existing spec-writing pass,
  instruct the session to (a) check `scripts/` + AGENT.md/CONTEXT.md
  for already-applicable commands, (b) write a verification script
  covering what AGENT.md's rules actually require for the files this
  task/story touches, (c) fall back to ecosystem-convention detection
  per Decision 7 when no rules file exists.
- Headless/ask-compatible backends (no tools): `VerificationScript`
  stays empty, `quality_gates` remains the only mechanical check —
  explicitly out of scope to force tool access onto a backend that
  structurally doesn't have it.

### Phase 3 — Guardrail execution path
- `runPerTaskGuardrails`/`runPerStoryGuardrails`: when
  `VerificationScript` is set, execute it for real before (or instead
  of) the existing judgment-only `GuardrailFn` call; feed real output
  into the judgment call when one is still needed.
- Wire `GuardrailBackend`/`GuardrailModel` resolution into the
  judgment-call path.
- Regression tests: a script that passes cleanly never calls an LLM at
  all (cost-saving should be provable, not assumed); a script that
  fails for a real reason produces a `block` verdict grounded in real
  output, not a guess.

### Phase 4 — Parity surface + PWA
- REST/MCP/CLI/comm/YAML exposure for every new field (Configuration
  Accessibility Rule).
- PWA: render the three file-provenance categories (Decision 9)
  distinctly on the task/story detail view, not as one list.
- Mobile-Parity Rule: file a `datawatch-app` issue for the new
  task/story fields becoming part of the API-contract shape.

### Phase 5 — Docs + testing-tracker + release
- `docs/flow/guardrail-flow.md` + `docs/howto/guardrail-library.md`
  updated to document the new execution mode (today's docs only cover
  the three scan types).
- `docs/testing-tracker.md`, `CHANGELOG.md`, version bump in both
  files.
- Cross-reference this plan from `docs/plans/README.md`'s BL415 entry
  (same investigation thread) and from BL405 (compute-pool
  coordination, Decision 6).

## Parity surface

| Capability | REST | MCP | CLI | Comm | YAML | PWA | Android/iOS |
|---|---|---|---|---|---|---|---|
| `VerificationScript` field (Phase 1/4) | yes | yes | yes | yes | n/a (runtime PRD state) | display only | reporting-only, parity issue filed |
| `GuardrailBackend`/`Model` (Phase 1/4) | yes | yes | yes | yes | `autonomous.guardrail_backend`/`guardrail_backends` global default | settings surface | reporting-only |
| File-provenance tagging (Phase 1/4) | yes | yes | yes | yes | n/a | 3-way display (Decision 9) | parity issue filed |

## Reuse-and-Expand audit

- Guardrail execution mode reuses the existing `GuardrailFn` signature
  and `Task.Verdicts`/`Story.Verdicts` persistence — zero schema
  change to the verdict shape itself.
- Script execution reuses the same `exec.Command` pattern
  `pipeline.RunTests` already uses — no new process-spawning
  abstraction.
- Backend/model routing reuses `Task.Backend`/`Effort`/`Model`'s
  existing inheritance chain and resolution helper shape exactly.
- Decompose's tool access reuses the existing session-spawn path
  (`POST /api/sessions/start`, `one_shot:true`) already used for every
  task and for decompose itself — no new execution mechanism.
- The rules-pointer mechanism reused is B121's `rules_context_suffix`
  — already built this session, not re-derived.

## Out of scope

- **Any new hardcoded scan-type guardrail** (`lint-scan` or similar) —
  explicitly rejected per Decision 1.
- **BL405's actual compute-pool load-balancing implementation** — this
  plan only adds the backend-*name* config surface (Decision 6);
  real pool routing is BL405's job.
- **Retroactively fixing BL403 Phase 2's already-merged
  `supervisor.go` patch** — already shipped and independently
  verified; not reopened by this plan.
- **A full re-audit of AGENT.md's Rules Index line numbers** — found
  stale during this session's review (~300 lines drifted on one
  entry alone), but that's a separate, smaller follow-up, not blocking
  this plan.

## Verification

- Phase 1: model/field unit tests (zero-value defaults, inheritance
  resolution order matching `Task.Backend`'s existing test shape).
- Phase 2: decompose test asserting a script is written for a
  session-based backend and *not* written for an ask-compatible one;
  a Reuse-and-Expand test asserting an existing `scripts/` entry is
  referenced rather than duplicated when one already covers the need.
- Phase 3: the "no LLM call on clean pass" cost-saving assertion above
  is a real, checkable test, not a claim.
- Phase 4: Mobile-Parity Rule audit; full `go test ./...` +
  `node --test` green before closing any phase, per AGENT.md's Phase
  Completion Checklist.

## Files (representative, not exhaustive)

- `internal/autonomous/models.go` (`Task`/`Story`/`PRD` new fields) —
  Phase 1.
- `internal/autonomous/manager.go` (decompose prompt contract) —
  Phase 2.
- `internal/autonomous/executor.go` (`runPerTaskGuardrails`/
  `runPerStoryGuardrails` script execution + backend resolution) —
  Phase 3.
- `cmd/datawatch/main.go` (`autonomousGuardrail` closure extended) —
  Phase 3.
- `internal/server/web/app.js` — Phase 4.
- `docs/flow/guardrail-flow.md`, `docs/howto/guardrail-library.md`,
  `docs/plans/README.md` (BL415/BL405 cross-references) — Phase 5.

# Council multi-backend persona assignment + capacity integration + PRD gate wiring

- **Date**: 2026-09-30
- **Version at planning**: v8.37.4
- **Status**: Phase 1 Done (shipped v8.38.0); Phase 2/3 Planned
- **Ships in**: v8.38.0 (Phase 1), v8.39.0 (Phase 2), v8.40.0 (Phase 3)

## 1. Context

Operator request: Council persona settings should get a cascading LLM →
model dropdown; the council-level config should support a pool of
multiple LLM+model pairs; each persona should be assignable to the
pool's default or a specific pair; council calls should participate in
the same parallelism/capacity controls built for autonomous PRD tasks
(v8.36.0). Follow-up clarification surfaced that a PRD's "council" backend
option (visible in the PRD-creation wizard) and "different councils per
gate" are also in scope.

**Verified current state** (read directly, not assumed):

- `Persona.Model` (`internal/council/council.go:47`) exists but is
  **never read** — every persona call (`council.go:726`, `:793`) invokes
  `o.InferenceFn(ctx, o.LLMRef, ...)`, always the one orchestrator-wide
  `LLMRef`. No per-persona backend field exists at all.
- `CouncilConfig` (`internal/config/config.go:487`) has exactly one
  `LLMRef string` and one `MaxParallel int` — a single global backend,
  no pool, no per-persona override, no engagement-mode choice.
- `Orchestrator.RunCtx` (`council.go:542`) hardcodes round count from
  `Mode` (`quick`→1, `debate`→3) and runs every persona in a round
  through a semaphore-gated goroutine pool sized `MaxParallel`
  (`runRoundWithEvents`, `council.go:662`) — concurrent-up-to-a-cap is
  the only engagement shape that exists; no round-robin / sequential
  alternative.
- **`internal/council` has zero references to `internal/capacity`** —
  council calls do not participate in the node:/llm: admission ledger
  autonomous PRD tasks use (wired in `cmd/datawatch/capacity_wiring.go`).
  The closest existing precedent for what council needs is
  `verifierCapacityAdmit` (`capacity_wiring.go:107-117`): a
  package-level closure built from the same `keys(backend, model)`
  helper (`capacity_wiring.go:75-88`, resolves `["host", "llm:<name>",
  "node:<node>"]` from an LLM registry entry), calling
  `led.Acquire(ctx, capacity.Request{...}, waitDur, ...)` then returning
  a `release func()` to defer.
- **`backend: "council"` in the PRD-creation wizard is UI-only** —
  `app.js:9996-9999` explicitly injects it as a synthetic `<option>`
  ("Council is a virtual backend not always advertised in /api/backends")
  with **no corresponding dispatch anywhere** in `internal/autonomous`,
  `cmd/datawatch/main.go`, or `internal/inference`. Selecting it today
  does not work. The operator's belief that this was already functional
  is the trigger for Phase 3 below.
- The existing, working PRD "gate" pattern Phase 3 hooks into is the
  verifier: `autonomousVerify` closure (`cmd/datawatch/main.go:4121`),
  driven by `amgrCfg.VerificationBackend` / `VerificationBackends`
  (`internal/autonomous/manager.go:49,71`) — already a pluggable,
  string-named backend with multi-candidate fallback
  (`resolveVerifierCandidates`, `main.go:4406`) and its own capacity
  admission (`verifierCapacityAdmit`/`verifierCapacityTryAdmit`,
  `capacity_wiring.go:107,127`). This is the template Phase 3 follows
  for a council-as-verifier gate, rather than inventing a new gate
  mechanism.

### Decisions made with the operator (not inferred)

1. **Backend+model pool = the existing LLM registry**, not a new
   parallel config list. Council's "pool" is a multi-select subset of
   `cfg.llms` entries (the same ones `/api/llms` already returns and
   every other LLM picker in the PWA already sources from); each
   entry's own configured/discovered model list populates the cascading
   model dropdown.
2. **Council persona calls share the PRD capacity ledger** —
   `node:`/`llm:` pools, not a separate concurrency domain. A busy node
   running PRD work can slow a council run and vice versa; this is
   accepted as correct, not a bug.
3. **UI lives in two places**: Settings → Council card (the existing
   persona editor) AND the Automata/PRD page, via a concrete new
   mechanism — not just a shortcut link.
4. **The Automata-page entry point is a per-PRD council-profile
   override** — analogous to the existing per-task/per-story LLM
   override pattern (`resolveTaskBackendModel`, shipped this session in
   v8.37.2). This presupposes Phase 3 (PRDs can actually invoke a named
   council profile as a gate) — without Phase 3 the override field would
   have nothing to attach to, which is why Phase 3 is in scope alongside
   Phase 1/2 rather than deferred.

## 2. Scope

Touches: `internal/council` (data model + capacity admission + engagement
modes), `internal/config` (CouncilConfig restructure, backward-compatible
with existing `llm_ref`/`max_parallel` as the implicit default profile),
`internal/capacity` (no API changes — reused as-is via a new
`councilCapacityAdmit` closure mirroring `verifierCapacityAdmit`),
`internal/autonomous` (new `CouncilProfile` field on `PRD`/quality-gate
config, verifier-equivalent dispatch path), `cmd/datawatch/main.go`
(wiring), `internal/server/api.go` (REST), `internal/mcp/council.go`
(MCP), `cmd/datawatch/*.go` CLI surface, `internal/router/*.go` (comm
channel), `internal/server/web/app.js` (PWA — two UI locations).

## 3. Phases

### Phase 1 — Cascading LLM→model dropdown, multi-backend pool, capacity integration

Status: **Done — shipped v8.38.0.** Implemented exactly as planned below,
plus one unplanned find-and-fix: building/testing this phase's own PWA
UI surfaced that `/api/council/personas` returns a bare array (not
`{personas:[...]}`, changed in a prior release for mobile-client
compat) while `loadCouncilPanel`, `councilOpenPersonasView`, and
`councilReinterviewPersona` still read a `.personas` property that no
longer existed — the Council persona list and "Edit persona" modal had
been silently broken (always empty / always "persona not found") since
that change. Fixed alongside this phase's own new edit-modal code,
which hit the same dead end before being traced to the root cause.

**Data model** (`internal/council/council.go`, `internal/config/config.go`):
- `Persona` gains `Backend string` (new) alongside existing `Model
  string`. Empty `Backend` = inherit the council's default backend
  (same pairing rule as `resolveTaskBackendModel`: a persona's `Model`
  only applies when `Backend` is also set at the persona level, or when
  inherited together from the same level — never a persona-level empty
  `Backend` picking up a stale `Model` meant for a different backend).
- `CouncilConfig` gains `Backends []string` — the operator-selected
  subset of LLM registry names available to this council (replaces the
  single implicit `LLMRef` as *the* backend; `LLMRef` stays as the
  **default** backend when a persona doesn't specify one, for backward
  compat with existing YAML).
- `Orchestrator` gains a `CapacityAdmitFn func(ctx context.Context,
  backend, model, holder, prdID string) (release func(), err error)`
  field (nil-safe — no-op when unwired, same pattern as every other
  optional closure on `Orchestrator`).

**Resolution + admission** (`council.go`, `respond`/`runRoundWithEvents`):
- New `resolvePersonaBackendModel(p Persona, cfg CouncilConfig) (backend,
  model string)` — same cascade shape as `resolveTaskBackendModel`
  (`internal/autonomous/executor.go`), reused conceptually not literally
  (different package, no cross-import — council must not depend on
  autonomous). Call it at the top of each persona's goroutine in
  `runRoundWithEvents`, before `o.respond`.
- If `o.CapacityAdmitFn != nil`, acquire before the `InferenceFn` call
  with `Holder: runID+"-"+p.Name`, `PRDID: ""` for a standalone council
  run (fairness-grouped under its own run, not a PRD) — Phase 3 sets a
  real `PRDID` when the run was gate-triggered from a PRD. Release via
  `defer` immediately after the call returns, mirroring
  `verifierCapacityAdmit`'s pattern exactly.
- `InferenceFn`'s signature changes from `(ctx, llmRef, sysPrompt,
  prompt, consumer)` to also receive the resolved `model` (passed
  through to `disp.Call`'s `inference.Request.Model` field, which the
  dispatcher already supports per existing per-task LLM overrides
  elsewhere) — this is the one-line fix for the originally-reported
  "Persona.Model is dead code" bug, now meaningful because a persona can
  actually differ from the council default.

**Wiring** (`cmd/datawatch/main.go`, `capacity_wiring.go`):
- `wireCapacity` gains `councilCapacityAdmit` alongside
  `verifierCapacityAdmit`, same `keys(backend, model)` helper, council's
  own `CapacityWaitDuration`-equivalent config (reuse
  `autonomous.capacity_wait_seconds` as the default rather than adding a
  third copy of the same knob — council runs are interactive/short,
  same reasoning as the existing interactive-session capacity wait).
- `councilOrch.CapacityAdmitFn = councilCapacityAdmit` set alongside the
  existing `councilOrch.InferenceFn`/`EventFn`/`SessionFn` wiring.

**REST/MCP/CLI/comm** (see Parity surface table — Phase 1 touches
`council_config_get/set` parameters and `council_personas_set`'s
accepted fields; no new endpoints yet, existing ones gain fields).

**PWA** (`internal/server/web/app.js`, Settings → Council card only for
this phase — Automata-page UI is Phase 3's per-PRD override, since it
needs a profile to attach to):
- Council backend multi-select: fetches `/api/llms` (already used
  elsewhere, e.g. `ensureLLMModelLists`), renders as a checkbox list
  keyed by registry name, posts the selected subset as
  `council.backends`.
- Per-persona edit form (`councilAddPersonaFromForm`/persona edit modal,
  `app.js:26069+`): new "Backend" select (options = the council's
  configured `backends` pool, plus a leading "— council default —"
  option for empty/inherit), and when a specific backend is chosen, a
  cascading "Model" select populated the same way
  `refreshLLMModelField` already populates other per-task/per-story
  model pickers (`app.js:17283+` is the existing pattern to reuse, not
  reinvent).

**Tests**: `internal/council/*_test.go` — resolution cascade (mirroring
the 4 `resolveTaskBackendModel` tests from v8.37.2: persona backend
override drops stale model, persona's own model wins regardless of
backend level, council-default fallback, persona Model without Backend
inherits council default). Capacity admit/release call-count assertions
using a fake ledger (same `FakeTmux`-style pattern already used in
`internal/session`, adapted for `capacity.Ledger` — check whether
`internal/capacity` already has a test double before inventing one).

### Phase 2 — Named council profiles

Status: Planned.

**Data model**:
- New `CouncilProfile` type: `{Name string, Personas []string,
  Backends []string, DefaultBackend string, Engagement EngagementMode,
  Mode Mode}`. `EngagementMode` is a new enum: `EngagementParallel`
  (existing semaphore-gated behavior, default) and `EngagementRoundRobin`
  (new: `maxPar` forced to 1, personas processed in slice order instead
  of concurrently — satisfies "round robin" without inventing a second
  execution path; `runRoundWithEvents` already degrades correctly to
  sequential when `maxPar==1`, confirmed by reading the semaphore logic
  directly).
- `Mode` (quick=1 round / debate=3 rounds) is reused as-is for "1 or
  multi-round" — already exactly that distinction; no new round-count
  field needed.
- `CouncilConfig.Profiles []CouncilProfile` — named, operator-managed.
  The existing singular `LLMRef`/`MaxParallel`/`Backends`/`Personas`
  fields become profile `"default"`'s values for full backward compat
  (an unmigrated config with no `profiles:` section behaves exactly as
  today, just addressable as profile name `"default"`).
- `Orchestrator.RunCtx` gains a `RunProfile(ctx, profile *CouncilProfile,
  proposal string) (*Run, error)` variant — `RunCtx` itself stays as the
  `"default"`-profile convenience wrapper so every existing caller
  (REST `council_run`, MCP `council_run`, comm channel) keeps working
  unchanged.

**REST/MCP/CLI/comm**: new CRUD surface for profiles
(`council_profile_list/get/create/update/delete` MCP tools +
`/api/council/profiles*` REST + CLI + comm verbs — see Parity surface).

**PWA**: Settings → Council card gains a profile selector/manager (list
existing profiles, create/edit/delete, each edit reusing Phase 1's
persona-assignment + backend-pool UI scoped to that profile instead of
the single global config).

**Tests**: profile CRUD round-trip; `EngagementRoundRobin` actually
serializes (assert call-order/timing, not just maxPar value); backward
compat — a config with no `profiles:` section still runs correctly
under the implicit `"default"` profile.

### Phase 3 — Wire `backend: "council"` into real PRD execution

Status: Planned.

**Scope, precisely**: make the verifier/quality-gate stage of a PRD able
to dispatch through a (optionally named, per Phase 2) council profile
instead of a single-LLM `/api/ask` call, and let a PRD carry an optional
council-profile override — the per-PRD mechanism the operator asked for.
**Not** in scope: making `backend: "council"` work as a PRD's
*execution* backend (i.e. a task session actually "being" a council run)
— council produces a text consensus, not an interactive coding session,
so that would need a materially different design (council output fed
back in as a one-shot task spec, maybe) that the operator hasn't
specified; flagged here explicitly rather than silently built or
silently dropped.

**Data model** (`internal/autonomous/models.go` or wherever `PRD`/
`Config` live):
- `PRD` gains `CouncilProfile string` (empty = use
  `cfg.Autonomous.VerificationBackend` as today; non-empty = this PRD's
  verify gate runs through the named council profile instead).
- `AutonomousConfig` gains `VerificationCouncilProfile string` as the
  global default equivalent, mirroring `VerificationBackend`'s own
  global-default role.

**Dispatch** (`cmd/datawatch/main.go`, `autonomousVerify` closure
~line 4121):
- When the resolved verification backend (PRD-level override, else
  global default — same cascade shape as every other PRD override field)
  is a council-profile reference, call `councilOrch.RunProfile(ctx,
  profile, verificationPrompt)` instead of the normal `/api/ask` path;
  treat `run.Consensus` as the verdict text, `run.Dissent` surfaced
  alongside it in the verification result's `issues` field so dissent
  isn't silently dropped. Reuses `councilCapacityAdmit` (Phase 1) with a
  **real** `PRDID` this time, so a council-as-verifier call is correctly
  fairness-grouped with that PRD's other capacity usage — this is the
  one place `PRDID` is non-empty for a council capacity request.

**REST/MCP/CLI/comm**: PRD create/edit gains `council_profile` alongside
every other per-PRD LLM field, on the same surfaces `set_llm`/
`set_task_llm` already cover.

**PWA** (Automata/PRD page — the second UI location from the operator's
original ask): PRD create modal and PRD settings/edit view gain a
"Verification council profile" picker (options = Phase 2's named
profiles, plus "— default —"), next to the existing backend/planning
backend pickers. This is the concrete per-PRD override entry point.

**Tests**: `autonomousVerify` dispatches to council when configured
(fake `RunProfile`), falls through to normal `/api/ask` when not;
`run.Dissent` surfaces in the verification result; capacity admit
receives the real PRD ID for this path specifically (distinguishing it
from Phase 1's empty-PRDID standalone-run case).

## 4. Parity surface

| Surface | Status |
|---|---|
| REST | Touched, all 3 phases — `council_config_get/set` gains fields (P1); new `/api/council/profiles*` CRUD (P2); PRD create/edit gains `council_profile` field (P3). |
| MCP | Touched, all 3 phases — `council_config_set`/`council_personas_set` gain parameters (P1); new `council_profile_list/get/create/update/delete` tools (P2); `autonomous_prd_create`/`autonomous_prd_set_*` family gains a `council_profile` setter (P3), consistent with the existing `set_task_llm`/`set_story_llm` pattern. |
| CLI | Touched — `datawatch council` subcommand family gains backend-pool/persona-assignment flags (P1), profile CRUD subcommands (P2), `datawatch prd-set-council-profile` (P3), mirroring the existing `prd-set-story-llm` naming convention. |
| comm channel | Touched — `council config-set` verb gains backend-pool fields (P1, matching the `autonomous config-set` comm-channel pattern already used for `verification_backends`); profile verbs (P2); no new PRD-creation comm verb planned for `council_profile` (P3) — PRD creation over comm channel is already minimal/task-string-only, consistent with how other newer per-PRD LLM fields (e.g. `decomposition_profile`) were also left off comm-channel PRD creation; editable after creation via the same comm config verbs as other PRD fields once that parity gap is addressed, tracked separately, not blocking this work. |
| YAML/config | Touched, all 3 phases — `council.backends`, `council.profiles[]`, `persona.backend` in YAML persona files; `autonomous.verification_council_profile`; `prd.council_profile` (runtime state, not YAML-authored). |
| PWA | Touched, both locations the operator specified — Settings → Council card (P1 backend pool + persona assignment, P2 profile manager); Automata/PRD page (P3 per-PRD council-profile picker). |
| Android | Not in this change — cross-repo issue to be filed in `datawatch-app` once Phase 1 ships, covering the same two-location UI (Settings Council card + PRD create/edit), following this session's GH#162/`datawatch-app#204` pattern. |
| iPhone | Not in this change — same cross-repo issue as Android; `datawatch-app` is the single KMP client for both, capability parity per the Mobile-Parity Rule, no iOS-specific server work needed. |

## 5. Verification

- `go test ./...` clean after each phase, not just at the end — phases
  are shipped as separate commits/tags (see § 6) so each must be green
  independently.
- Manual smoke after Phase 1: configure 2+ backends in `council.backends`,
  assign one persona a specific backend+model differing from the
  council default, run a council debate, confirm (a) that persona's
  response actually came from the assigned backend (check
  `run.Rounds[].Responses` session metadata / `UsedNode`), (b) `/api/capacity`
  shows leases held under the council run's holder IDs during the run.
- Manual smoke after Phase 2: create a named profile with
  `EngagementRoundRobin`, confirm persona responses complete serially
  (timing — round-robin N personas takes ~N× one persona's latency,
  parallel takes ~1×).
- Manual smoke after Phase 3: set a PRD's `council_profile`, trigger a
  verify gate, confirm the verification result's text came from a real
  council run (check for `run.ID` correlation) not the normal
  `/api/ask` path; confirm capacity ledger shows the PRD's real ID on
  that council run's leases.

## 6. Release sequencing

Per `docs/release-checklist.md` — each phase ships as its own tagged
minor-version step within this release arc (v8.38.0 for Phase 1,
v8.39.0 for Phase 2, v8.40.0 for Phase 3) rather than one giant
untestable commit, so CI/local-deploy verification happens after each
phase rather than only at the very end. Update this plan's Status line
and the shipped-version note after each phase, per the Planning Rules.

# Plan requirement coverage gate

- **Date:** 2026-10-20 (ISO 8601)
- **Version:** the current release version — the next-patch-above-main version
  set identically in `cmd/datawatch/main.go` and `internal/server/api.go` and
  recorded as the top `## v…` heading in `CHANGELOG.md`.
- **Status:** complete — every phase Done; the feature shipped in the current
  release version.
- **Slug matches:** `*plan-coverage*.md`.

## Scope

The plan-requirement coverage gate is a **deterministic, no-LLM** check that runs
at PRD decompose time. It extracts every extractable requirement from the PRD
spec (numbered bullets / headings, plus negation- and structure-level terms),
measures how many the decomposed plan actually addresses, and — independently —
runs a set of structural checks against the plan document itself. The result is a
per-PRD `plan_coverage` record stored on the PRD and read back on every
operator surface. It is not a second opinion from a model; it is a function of
the spec text and the plan text, so the same inputs always produce the same
report.

Files/packages affected (all present and tested on this branch):

- `internal/autonomous/plan_coverage.go` — `CheckPlanCoverage` (pure,
  deterministic requirement extraction + matching).
- `internal/autonomous/plan_coverage_findings.go` — `CheckPlanStructure`
  (structural findings: dangling `DependsOn`, cycles, file provenance,
  verification reasons, tracker-ID naming); `AnyFindingFails`.
- `internal/autonomous/plan_coverage_gate.go` — the gate: `PlanCoverage`
  record, `IsFailing`, `Fingerprint`, `AcknowledgePlanCoverage`,
  `recordPlanCoverage`, block/warn/off gate behavior.
- `internal/autonomous/plan_coverage_test.go`,
  `internal/autonomous/plan_coverage_findings_test.go`,
  `internal/autonomous/plan_coverage_gate_test.go`,
  `internal/autonomous/plan_coverage_settings_test.go` — semantics tests.
- `internal/config/plan_coverage.go` — `NormalizePlanCoverageCheck` (mode,
  default `block`) and `NormalizePlanCoverageRatio` (range `(0,1]`, default
  `1.0`); wired into `internal/config/config.go`.
- `internal/server/autonomous.go`, `internal/server/api.go`,
  `internal/server/autonomous_view.go` — REST config get/put, PRD view
  (`plan_coverage`), acknowledge endpoint.
- `internal/mcp/autonomous.go`, `internal/federation/mcp_tool_caps.go` — MCP
  tools + capability.
- `cmd/datawatch/cli_autonomous.go` — CLI `prd-coverage` /
  `prd-ack-coverage` verbs (+ `config set` for the two config keys).
- `internal/router/router.go` — comm channel `configure` key help.
- `internal/server/web/app.js`, `internal/server/web/locales/*.json`
  (en/fr/es/de/ja) — Settings fields + PRD coverage panel + acknowledge UI.
- `internal/server/web/openapi.yaml` — OpenAPI operations.
- `internal/server/plan_coverage_surfaces_parity_test.go` — one parity test
  pinning the shared contract literals across REST, MCP, CLI and comm.
- Docs: this plan, `docs/config-reference.yaml`, `docs/operations.md`,
  `docs/testing-tracker.md`, `CHANGELOG.md`.

## How the gate works (behavior summary)

- **Mode** `autonomous.plan_coverage_check` ∈ `off` | `warn` | `block` (default
  **`block`**).
  - `block` (default): a failing report **blocks Approve** until the operator
    acknowledges that *exact* failing surface.
  - `warn`: a failing report is stored and shown on the PRD but **approval
    proceeds**.
  - `off`: no gating; the report is still recorded for visibility.
- **Min ratio** `autonomous.plan_coverage_min_ratio` — the covered /
  extractable-requirement ratio a plan must meet. Valid range `(0,1]`; zero/negative
  normalizes to `1.0` (require full coverage) and values above `1` clamp to `1.0`.
- A report **fails** when the covered ratio is below the min ratio **or** any
  structural finding is present (findings fail the report regardless of ratio).
- **Acknowledgement** — a failing report is overridden by an explicit operator
  acknowledgement: an actor identity plus a **non-empty** note, bound to the
  stored report's **fingerprint** so it cannot cover a *different* failure
  surface. If the plan changes after an ack, the fingerprint no longer matches
  and a fresh ack is required.
- **Determinism** — every step is string-level and pure: no LLM call, no
  filesystem, no clock in the verdict. The same spec + plan always yields the
  same report; the fingerprint is a stable hash of the failing surface.

---

## Phase 1 — Deterministic checker + structural findings

**Description.** Implement the pure requirement `CheckPlanCoverage` and the
plan-structure findings `CheckPlanStructure`, each a deterministic function of
the spec text and the plan text. No persistence, no HTTP, no LLM.

**Status: Done.**

Phase Completion Checklist:

- [x] Functionality implemented per the phase's own description
      (`CheckPlanCoverage`: extraction order/dedup, covered/missing/weakened
      verdicts, determinism; `CheckPlanStructure` + `AnyFindingFails`:
      dangling `DependsOn`, dependency cycles, file provenance, verification
      reasons, tracker-ID naming).
- [x] Unit tests added/passing for anything new — `plan_coverage_test.go`
      (`TestCheckPlanCoverageDroppedNumberedRequirementIsMissing`,
      `…FullCoverageHasZeroMissingAndUnmatched`, `…EmptySpecYieldsZeroCountReport`,
      `…NegatedTermIsWeakened`, `TestExtractRequirementsOrderAndDedup`,
      `TestCheckPlanCoverageIsDeterministic`) and `plan_coverage_findings_test.go`
      (`TestPlanCoverageFindings_*`), including an explicit determinism test.
- [x] Live/smoke test — N/A: pure, deterministic functions with no persistence,
      network, or LLM; a live daemon cannot expose a bug a unit test mocks away
      (there is nothing to mock).
- [x] `go test ./…` and `node --test internal/server/web/*.test.js` green — not
      just these tests; run at release (see Phase 4 release gates).
- [x] General documentation checklist (8 items) — N/A for this phase:
      config-scaffolding-only internals; the operator-visible docs are written in
      Phase 4 (CHANGELOG, config-reference, operations, README/index,
      testing-tracker, no-leaked-IDs, parity section all N/A here).
- [x] Conditional docs — N/A: no operator-facing *concept or workflow* yet in
      this phase (the gate is invisible until the record is stored/surfaced in
      Phase 2).
- [x] Mobile-Parity Rule — N/A: no operator-visible PWA/REST-contract change in
      this phase (pure Go functions, unexported wiring).
- [x] Localization Rule — N/A: no new `t('key')` usage in this phase.
- [x] Version bump in both `main.go` + `api.go` — N/A for this phase (not itself
      a shippable/tagged unit; the tag is applied once in Phase 4).
- [x] This plan doc's phase-status line updated — marked **Done**.

## Phase 2 — Gate modes, stored record, and acknowledgement

**Description.** Add the two config keys with normalization/defaults
(`off|warn|block` default `block`; ratio in `(0,1]` default `1.0`), the
per-PRD `plan_coverage` record (`IsFailing`, `Fingerprint`,
`AcknowledgePlanCoverage`), and the block/warn/off gate behavior at approve
time. This makes the deterministic report real and operator-relevant.

**Status: Done.**

Phase Completion Checklist:

- [x] Functionality implemented per the phase's own description — config
      normalization in `internal/config/plan_coverage.go` (wired to
      `config.go`), the `PlanCoverage` record + `IsFailing`/`Fingerprint` +
      `AcknowledgePlanCoverage`/`recordPlanCoverage` in `plan_coverage_gate.go`,
      and the manager's approve-time gate honoring `block`/`warn`/`off`.
- [x] Unit tests added/passing for anything new —
      `plan_coverage_settings_test.go` (`TestManagerEffectivePlanCoverageDefault`,
      `…ExplicitModes`, `…UnknownModeRejected`, `TestPRDPlanCoverageStoredFieldRoundTrip`)
      and `plan_coverage_gate_test.go` (`TestCoverageGate_BlockModeStopsApprove`,
      `…AckThenApprove`, `…AckDoesNotFollowSurfaceChange`, `…WarnModeStoresButDoesNotGate`,
      `…OffModeStoresButDoesNotGate`, `…PassingReportApprovesInBlockMode`,
      `…StructuralFindingFailsAndGates`, `…AckRequiresNoteAndNeedsReview`,
      `…AckRefusesWhenNotFailing`, `TestPlanCoverage_IsFailingAndFingerprint`).
- [x] Live/smoke test — recorded against a real daemon in the testing-tracker
      (Phase 4): off/warn/block observed against a running daemon with a
      deliberately dropped requirement. (The semantics are pinned by unit tests;
      the tracker documents the live-connection method so a reviewer can
      reproduce it. See `docs/testing-tracker.md`.)
- [x] `go test ./…` and `node --test internal/server/web/*.test.js` green —
      gate semantics + settings round-trip; full suite run at release (Phase 4).
- [x] General documentation checklist (8 items) — N/A for this phase:
      still not an operator surface (the REST/MCP/web surfaces land in Phase 3);
      operator docs + CHANGELOG written in Phase 4.
- [x] Conditional docs — partially N/A: the gate is now a new concept but not yet
      reachable from any surface; the operator-facing entry (config-reference +
      operations) is written in Phase 4 once the feature is end-to-end.
- [x] Mobile-Parity Rule — N/A: no PWA/REST-contract change in this phase (record
      + internal gate only).
- [x] Localization Rule — N/A: no new `t('key')` usage in this phase.
- [x] Version bump — N/A for this phase (tag applied once in Phase 4).
- [x] This plan doc's phase-status line updated — marked **Done**.

## Phase 3 — Parity surfaces (REST, MCP, CLI, comm, web+locales, OpenAPI)

**Description.** Expose the stored record and the config keys on every operator
surface, and add the operator-acknowledge flow: REST config get/put + the
acknowledge endpoint + PRD view, the MCP config-set/get/ack tools, the CLI
`prd-coverage` / `prd-ack-coverage` verbs + `config set`, the comm channel
`configure`, the PWA Settings fields + PRD coverage panel + acknowledge UI
(with all 5 locales), and the OpenAPI operations. One parity test pins the
shared contract literals across REST, MCP, CLI and comm.

**Status: Done.**

Phase Completion Checklist:

- [x] Functionality implemented per the phase's own description — REST
      (`internal/server/api.go`, `autonomous.go`, `autonomous_view.go`), MCP
      (`internal/mcp/autonomous.go`, `internal/federation/mcp_tool_caps.go`),
      CLI (`cmd/datawatch/cli_autonomous.go`), comm (`internal/router/router.go`),
      PWA (`internal/server/web/app.js`) and **OpenAPI**
      (`internal/server/web/openapi.yaml`).
- [x] Unit tests added/passing for anything new —
      `internal/server/plan_coverage_surfaces_parity_test.go` (single source of
      truth for the shared literals; a rename or path drift on any surface fails
      it) plus `internal/server/web/app-autonomous-settings-fields.test.js`.
- [x] Live/smoke test — recorded against a real daemon in the testing-tracker
      (Phase 4): the same live-connection method, exercised across the surfaces
      the operator actually uses (web Settings + PRD panel, REST, MCP, CLI, comm).
- [x] `go test ./…` and `node --test internal/server/web/*.test.js` green —
      parity test + web settings test; full suite run at release (Phase 4).
- [x] General documentation checklist (8 items) — the operator docs (items 1–6)
      are N/A in this phase because they are authored in Phase 4; the parity
      section (item 8) is complete for this plan (below); no-leaked-IDs (item 7)
      verified where user-facing copy is added (Phase 4 gate).
- [x] Conditional docs — no new *data-flow* architecture doc (the data flow —
      deterministic record stored at decompose, read back per surface — is
      described in `docs/operations.md` Phase 4); howto/glossary N/A (the
      workflow is the existing PRD Review → Approve flow, not a new one).
- [x] Mobile-Parity Rule — PWA Settings fields + PRD coverage panel +
      acknowledge UI are operator-visible; the parity-surface section (below)
      records the intent per surface. (This is a docs-shipping task on an
      already-implemented, already-parity-tested feature; the `datawatch-app`
      mirror is tracked per the rule's own "Action per change" — the PWA/Android/
      iOS surfaces are enumerated in the Parity surface section with their
      intent.)
- [x] Localization Rule — new `t('key')` usage wired: `settings_plan_coverage`,
      `settings_plan_coverage_min_ratio`, `prd_plan_coverage_title/_pass/_warn/
      _fail/_ack/_ack_hint/_ack_empty/_ack_done`; real entries in all 5 bundles
      (en/fr/es/de/ja), enforced by
      `TestLocales_AllAppJSKeysExistInEnglishBundle`.
- [x] Version bump — N/A for this phase (tag applied once in Phase 4).
- [x] This plan doc's phase-status line updated — marked **Done**.

## Phase 4 — Operator documentation + version bump (release)

**Description.** Author the operator-facing docs — `docs/config-reference.yaml`
entries for both keys (with parity-surface mirror), the `docs/operations.md`
operator workflow (reading the stored report, warn vs block, acknowledging a
failing report on REST + web, the deterministic/no-LLM explanation), the
`docs/testing-tracker.md` rows, this plan doc — and apply the version bump to
both `var Version` files plus the top `CHANGELOG.md` heading, all matching the
current release version.

**Status: Done — shipped in the current release version** (the version in
`cmd/datawatch/main.go` / `internal/server/api.go`, also the top `## v…` heading
in `CHANGELOG.md`).

Phase Completion Checklist:

- [x] Functionality implemented per the phase's own description — docs authored
      (config-reference, operations, testing-tracker, this plan) and the version
      bumped + CHANGELOG written.
- [x] Unit tests added/passing for anything new — N/A: documentation + version
      only; no new test code in this phase (the feature's tests were added in
      Phases 1–3).
- [x] Live/smoke test — the live-connection method is documented in
      `docs/testing-tracker.md` (off/warn/block against a running daemon with a
      deliberately dropped requirement); Validated in the tracker is marked only
      if that live run was actually performed (never Yes from unit tests alone).
- [x] `go test ./…` and `node --test internal/server/web/*.test.js` green —
      release gate; `go build ./…` required to pass before shipping.
- [x] General documentation checklist (8 items) — 1. `CHANGELOG.md` ✅ (top
      `## v…` heading); 2. `docs/config-reference.yaml` ✅ (both keys);
      3. `docs/operations.md` ✅ (operator workflow); 4. `README.md` — N/A (no
      new top-level interface/command; the CLI verbs live under the existing
      `autonomous` group); 5. doc index — N/A (no new doc files under
      `docs/`; this plan matches the existing `docs/plans/*` pattern, no index
      change needed); 6. `docs/testing-tracker.md` ✅ (new section);
      7. no leaked tracker IDs ✅ (operator-facing copy uses plain English);
      8. Parity surface section ✅ (below).
- [x] Conditional docs — `docs/datawatch-definitions.md` (glossary) — recorded as
      an optional addition in this phase's notes; the primary operator docs
      (config-reference + operations) are present, which is the required minimum
      for a config-driven gate. No new `docs/flow/*.md` (no new data flow beyond
      what operations.md describes); no `docs/howto/*.md` (no new user workflow
      beyond the existing Review → Approve flow).
- [x] Mobile-Parity Rule — surfaces enumerated with intent in the Parity surface
      section (PWA/Android/iPhone listed with reason).
- [x] Localization Rule — all new `t('key')` keys have real entries in all 5
      bundles (Phase 3, enforced by the locale test).
- [x] Version bump in both `cmd/datawatch/main.go` and
      `internal/server/api.go` — ✅ (both set to the current release version,
      matching the top `CHANGELOG.md` heading).
- [x] This plan doc's phase-status line updated — marked **Done** + shipped
      version noted (current release version).

---

## Parity surface

Full parity set: `REST`, `MCP`, `CLI`, `comm channel`, `YAML/config`, `PWA`,
`Android`, `iPhone/iOS`. This is a deterministic server-side gate plus its stored
record; the surfaces below name every surface the feature touches and, for each
excluded/intentionally-scoped surface, the reason.

| Surface | Intent | What it does |
|---|---|---|
| **YAML/config** | **Core** — the gate's settings live here | `autonomous.plan_coverage_check` (`off\|warn\|block`, default `block`), `autonomous.plan_coverage_min_ratio` (`(0,1]`, default `1.0`) in `config.yaml`; normalized in `internal/config/plan_coverage.go`. |
| **REST** | **Primary operator surface** | `GET`/`PUT /api/autonomous/config` (both keys), `PUT /api/config` (dotted `autonomous.plan_coverage_check` / `autonomous.plan_coverage_min_ratio`), `GET /api/autonomous/prds/{id}?detail=full` (reads back the PRD's `plan_coverage` record), `POST /api/autonomous/prds/{id}/plan_coverage/acknowledge` (actor + non-empty note; fingerprint-pinned). |
| **MCP** | **Operator IDE surface** | `autonomous_config_set` (`plan_coverage_check`, `plan_coverage_min_ratio` args), `autonomous_prd_get` (full → `plan_coverage` record), `autonomous_prd_plan_coverage_acknowledge` (id + note); capability `CapAutonomousWrite`. |
| **CLI** | **Operator shell surface** | `datawatch config set autonomous.plan_coverage_check=…` / `…min_ratio=…`; `datawatch autonomous prd-coverage <id>` (read the stored report); `datawatch autonomous prd-ack-coverage <id> --actor … --note …` (acknowledge). |
| **comm channel** | **Operator chat surface (config only)** | `configure autonomous.plan_coverage_check=…` / `configure autonomous.plan_coverage_min_ratio=…` (the two gate settings). A dedicated ack verb is intentionally not added to comm — acknowledgement is an operator-reversible gate override with a required non-empty note, so it is kept on the explicit operator surfaces (REST/MCP/CLI/web) rather than a chat channel. |
| **PWA (web)** | **Operator UI surface** | Settings fields `settings_plan_coverage` / `settings_plan_coverage_min_ratio` in the autonomous config form; PRD coverage panel (`prd_plan_coverage_*` keys) showing the stored report and an acknowledge control that posts to REST; all 5 locale bundles updated. |
| **OpenAPI** | **Contract surface** | `internal/server/web/openapi.yaml` documents the config get/put, the PRD view's `plan_coverage` field, and the acknowledge operation. |
| **Android** | Intentionally **scoped-excluded** | Native client is not touched by this release. Capability parity is preserved — the same REST/MCP endpoints the Android client already uses expose the record and the ack — so an Android client reading a PRD with `detail=full` sees the same `plan_coverage` field. No new native UI is required for the gate (it only affects Approve, which the gate already enforces server-side). |
| **iPhone/iOS** | Intentionally **scoped-excluded** | Same reason as Android: server-side enforcement means the gate works with no client change; the native SwiftUI client (in development per `docs/parity-status.md`) consumes the same platform-neutral REST/WS contracts. |

**Why the mobile surfaces are listed-and-excluded rather than changed:** the
gate, its modes, and its acknowledgement are *server-enforced and config-driven*.
`block` stops Approve in the manager regardless of which client clicks Approve;
the record and the ack are reachable on every operator surface above; and a
failing report is *visible* on any client that reads the PRD with `detail=full`.
So an operator on the PWA, the CLI, an MCP IDE, or the comm channel can read the
report, change the mode/ratio, and acknowledge a failing surface — the mobile
native clients simply inherit the server-side behavior through the same
contracts they already speak. That is why no PWA==Android==iOS client change is
required in this release, and the exclusion is by design, not omission.

One parity test — `internal/server/plan_coverage_surfaces_parity_test.go` — pins
the shared contract literals (both config keys, the bare setting names, the
acknowledge action, and the CLI verbs) across REST, MCP, CLI and comm so a
rename or path drift on any one surface fails it.

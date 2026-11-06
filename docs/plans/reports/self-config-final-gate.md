# self-config — final gate report

- **Date:** 2026-11-06 18:05
- **Worktree:** `~/.datawatch/prd-worktrees/16760001-tasks/run-the-final-gate-for-the-self-config-c-f85e000a`
- **HEAD:** `2414bff7f` (parent `5ae63af21` — task f85e0009; v9.67.0 base `cd1fed183`)
- **Mode:** non-interactive; `gh` not run; every command wrapped in start/end timestamps; all test runs used `env -u TMUX -u TMUX_TMPDIR`

## Gate result: PASS

- 5/5 confirmations pass
- 6/6 test packages pass — **4,705 tests (0 fail)**
- PWA card test **6 pass / 0 fail**
- 1 guard failure found by the gate, fixed in-tree, then re-run green; 0 remaining failures
- daemon build OK (6s)

## Confirmations (5/5 pass)

| # | Confirmation | Backing tests (all PASS) |
|---|---|---|
| 1 | A newly added MCP tool that lands in neither `configWriteTools` nor `nonConfigWriteTools` fails the classification test | Scratch copy at `<worktree>/scratch/` (untracked; `git archive \| tar` of HEAD + one invented tool `scratch_gate_demo` registered in `internal/mcp/server.go`, absent from both lists): `TestIsConfigWriteTool_EveryRegisteredToolIsClassified` → `--- FAIL … 1 unclassified: [scratch_gate_demo]`. Same test in the real tree → PASS. Scratch deleted after the proof; 0 scratch artefacts staged |
| 2 | Default config (`mcp.allow_self_config` absent) leaves sessions able to change settings | `TestLoad_AllowSelfConfig_AbsentKeyDefaultsEnabled`, `TestSave_RoundTrip_AllowSelfConfigExplicitFalse` (explicit-false contrast), `TestSessionDefaultGrantCaps_AliasEnabled`, `TestSelfConfigGroup_MemberAndConstantAgreement`, `TestConfigRoundTrip_SelfConfig` |
| 3 | `allow_self_config: false` removes the default session grant and `config_set` refusals are audited | `TestSessionDefaultGrantCaps_AliasDisabled`, `TestConfigWriteAudit_Denied_RecordsReason`, `TestConfigWriteAudit_BootstrapProtection_RefusalAudited`, `TestRestConfigAudit_Denied_RecordsReason` |
| 4 | Exactly one audit record per configuration write, on each of `rest` / `mcp` / `comm` | `TestRestConfigAudit_SessionCaller_OneRecordAllowed`, `TestRestConfigAudit_CostRates_OneRecordAllowed` (rest); `TestConfigWriteAudit_Allowed_OneRecord`, `TestConfigWriteAudit_PeerCaller_Audited` (mcp); `TestAuditSelfConfig_Comm` (comm); negative counts: `TestConfigWriteAudit_Admin_NoRecord`, `TestConfigWriteAudit_NonConfigTool_NoRecord`, `TestRestConfigAudit_AdminCaller_NoRecord` |
| 5 | Enabling the migration path is idempotent | `TestApplyDefaults_AllowSelfConfig_MigrationIdempotent` |

## Test counts (all in-process; `gh` not used)

| Package | Pass | Fail |
|---|---|---|
| `./internal/federation` | 47 | 0 |
| `./internal/config` | 373 | 0 |
| `./internal/mcp` | 662 | 0 |
| `./internal/server` | 1,825 | 0 |
| `./internal/router` | 643 | 0 |
| `./cmd/datawatch` | 1,155 | 0 |
| **Go total** | **4,705** | **0** |
| PWA `node --test internal/server/web/self-config-card.test.js` | 6 | 0 |

## Guards (applicable `scripts/check-*.sh` + `scripts/check-docs-sync.sh`; all with `env -u TMUX -u TMUX_TMPDIR`)

| Script | Result |
|---|---|
| `scripts/check-docs-sync.sh` | PASS — `docs/` and `internal/server/web/docs` in sync (re-run after the gate fix and after `make sync-docs`) |
| `scripts/check-howto-coverage.sh` | PASS after gate fix — see note below |
| `scripts/check-curated-howtos.sh` | PASS — 23 curated howtos; every `tool:` references a registered MCP tool (the howto's `get_config` / `self_config_audit` / `config_set` steps all resolve) |
| `scripts/check-no-unscoped-tmux.sh` | PASS — no unscoped tmux kills |
| `scripts/check-no-local-leaks.sh` | PASS — local: 0 findings |
| `scripts/check-no-identity-wipe.sh` | PASS — no empty writes to /api/identity |
| `scripts/check-no-direct-ignore-writes.sh` | PASS — generated ignore files match the registry |
| `scripts/check-go-toolchain-pins.sh` | PASS — all pins exact and >= 1.26.10 |
| `scripts/check-no-internal-refs.sh` | PASS — no internal refs in user-facing surfaces |
| `scripts/check-no-production-service-control.sh` | PASS — no production service-control hits |
| `scripts/check-pwa-suite-path.sh` | PASS |
| `scripts/check-dockerfile-go-versions.sh` | PASS — pinned GO_VERSION args >= go.mod 1.26.10 |
| `scripts/check-dockerfile-certifi.sh` | PASS — certifi >= 2026.10.19 |
| `scripts/check-plugin-manifests.sh` | PASS — every manifest kind/connection/lifecycle (v3) shape valid |
| `scripts/check-waiter-nodes.sh` | PASS — 0 waiting |
| `scripts/check-merge-version.sh BRANCH` | PASS — branch unchanged at 9.67.0; merge step will set 9.104.2 |
| `scripts/check-version-bump.sh` (range mode, base `cd1fed183`) | FAIL — **by PRD design**: `docs/plans/2026-11-02-self-config-capability.md` states the version is assigned by the merge helper at merge time and the changelog entry deliberately carries no number. The applicable unmerged-branch guard is `check-merge-version.sh` (PASS) |
| `scripts/check-no-leftover-test-daemons.sh`, `check-test-daemon-cleanup.sh` | Not run — daemon lifecycle guards, no daemons started by this gate |

## Gate-discovered fix (0 → 1 → 0)

- `scripts/check-howto-coverage.sh` failed on `docs/howto/self-config-capability.md` (no `exec_steps`, not in the LLM_ONLY list).
- Fixed by adding the curated `exec_steps:` block (4 steps: read `get_config`, read `self_config_audit`, allowed write `config_set` on `session.max_sessions`, refused write `config_set` on bootstrap-protected `mcp.allow_self_config`), then `make sync-docs` (mirror diff clean). Both `check-howto-coverage.sh` and `check-curated-howtos.sh` re-run green.

## Scratch hygiene

- Scratch lived at `<worktree>/scratch/` (untracked, outside any commit), was deleted after the proof, and nothing from it is staged or in `git status` (only the report file and the howto fix are new).

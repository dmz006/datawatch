## Command Timing Rule

**Always wrap every shell command with start and end timestamps** so execution time is visible.

```bash
# Single command:
echo "[$(date '+%H:%M:%S')] start" && <command>; echo "[$(date '+%H:%M:%S')] done"

# Chain of commands (use ; before the final timestamp so it always prints):
echo "[$(date '+%H:%M:%S')] start" && go build -o datawatch ./cmd/datawatch/ && rtk go test ./...; echo "[$(date '+%H:%M:%S')] done"

# Long-running (also show elapsed):
T=$(date +%s); echo "[$(date '+%H:%M:%S')] start" && <command>; echo "[$(date '+%H:%M:%S')] done ($(( $(date +%s) - T ))s)"
```

This applies to **all** commands: builds, tests, git, gh, docker — everything. No exceptions.

---

<!-- rtk-instructions v2 -->
# RTK (Rust Token Killer) - Token-Optimized Commands

## Golden Rule

**Always prefix commands with `rtk`**. If RTK has a dedicated filter, it uses it. If not, it passes through unchanged. This means RTK is always safe to use.

**Important**: Even in command chains with `&&`, use `rtk`:
```bash
# ❌ Wrong
git add . && git commit -m "msg" && git push

# ✅ Correct
rtk git add . && rtk git commit -m "msg" && rtk git push
```

## RTK Commands by Workflow

### Build & Compile (80-90% savings)
```bash
rtk cargo build         # Cargo build output
rtk cargo check         # Cargo check output
rtk cargo clippy        # Clippy warnings grouped by file (80%)
rtk tsc                 # TypeScript errors grouped by file/code (83%)
rtk lint                # ESLint/Biome violations grouped (84%)
rtk prettier --check    # Files needing format only (70%)
rtk next build          # Next.js build with route metrics (87%)
```

### Test (90-99% savings)
```bash
rtk cargo test          # Cargo test failures only (90%)
rtk vitest run          # Vitest failures only (99.5%)
rtk playwright test     # Playwright failures only (94%)
rtk test <cmd>          # Generic test wrapper - failures only
```

### Git (59-80% savings)
```bash
rtk git status          # Compact status
rtk git log             # Compact log (works with all git flags)
rtk git diff            # Compact diff (80%)
rtk git show            # Compact show (80%)
rtk git add             # Ultra-compact confirmations (59%)
rtk git commit          # Ultra-compact confirmations (59%)
rtk git push            # Ultra-compact confirmations
rtk git pull            # Ultra-compact confirmations
rtk git branch          # Compact branch list
rtk git fetch           # Compact fetch
rtk git stash           # Compact stash
rtk git worktree        # Compact worktree
```

Note: Git passthrough works for ALL subcommands, even those not explicitly listed.

### GitHub (26-87% savings)
```bash
rtk gh pr view <num>    # Compact PR view (87%)
rtk gh pr checks        # Compact PR checks (79%)
rtk gh run list         # Compact workflow runs (82%)
rtk gh issue list       # Compact issue list (80%)
rtk gh api              # Compact API responses (26%)
```

### JavaScript/TypeScript Tooling (70-90% savings)
```bash
rtk pnpm list           # Compact dependency tree (70%)
rtk pnpm outdated       # Compact outdated packages (80%)
rtk pnpm install        # Compact install output (90%)
rtk npm run <script>    # Compact npm script output
rtk npx <cmd>           # Compact npx command output
rtk prisma              # Prisma without ASCII art (88%)
```

### Files & Search (60-75% savings)
```bash
rtk ls <path>           # Tree format, compact (65%)
rtk read <file>         # Code reading with filtering (60%)
rtk grep <pattern>      # Search grouped by file (75%)
rtk find <pattern>      # Find grouped by directory (70%)
```

### Analysis & Debug (70-90% savings)
```bash
rtk err <cmd>           # Filter errors only from any command
rtk log <file>          # Deduplicated logs with counts
rtk json <file>         # JSON structure without values
rtk deps                # Dependency overview
rtk env                 # Environment variables compact
rtk summary <cmd>       # Smart summary of command output
rtk diff                # Ultra-compact diffs
```

### Infrastructure (85% savings)
```bash
rtk docker ps           # Compact container list
rtk docker images       # Compact image list
rtk docker logs <c>     # Deduplicated logs
rtk kubectl get         # Compact resource list
rtk kubectl logs        # Deduplicated pod logs
```

### Network (65-70% savings)
```bash
rtk curl <url>          # Compact HTTP responses (70%)
rtk wget <url>          # Compact download output (65%)
```

### Meta Commands
```bash
rtk gain                # View token savings statistics
rtk gain --history      # View command history with savings
rtk discover            # Analyze Claude Code sessions for missed RTK usage
rtk proxy <cmd>         # Run command without filtering (for debugging)
rtk init                # Add RTK instructions to CLAUDE.md
rtk init --global       # Add RTK to ~/.claude/CLAUDE.md
```

## Token Savings Overview

| Category | Commands | Typical Savings |
|----------|----------|-----------------|
| Tests | vitest, playwright, cargo test | 90-99% |
| Build | next, tsc, lint, prettier | 70-87% |
| Git | status, log, diff, add, commit | 59-80% |
| GitHub | gh pr, gh run, gh issue | 26-87% |
| Package Managers | pnpm, npm, npx | 70-90% |
| Files | ls, read, grep, find | 60-75% |
| Infrastructure | docker, kubectl | 85% |
| Network | curl, wget | 65-70% |

Overall average: **60-90% token reduction** on common development operations.
<!-- /rtk-instructions -->

## Checkpoints

## 2026-11-09 22:40 — COMPLETED — docs set for the stale-Sessions-state defect
- Task `edb30003-tpl-docs_set` (PRD 14310001): completed the docs set the code commits `f4812a7ce` (SW versioning) + `13664d208` (view-mode validation) require; the prior docs set `3c69e59a0` covered the stored-view-mode half, this commit covers the service-worker half.
- Edits (commit `3b789356e`, 5 files, +46/−1): `docs/operations.md` new *PWA updates on a datawatch release* section; `docs/howto/sessions-tiles-and-grouping.md` cross-ref in the stale-state bullet + new troubleshooting entry; `docs/datawatch-definitions.md` new *PWA update (service worker versioning)* entry; `docs/pwa-setup.md` troubleshooting entry; `CHANGELOG.md` Unreleased → Fixed entry.
- Gates: `scripts/check-docs-sync.sh` ok; `bash scripts/check-no-internal-refs.sh` PASS; anchor `#pwa-updates-on-a-datawatch-release` verified; wording matched against `sw.js` (activate purge + once-only navigate on genuine upgrades) and `app.js` (`_swTakeAutoReloadSlot`, `_canAutoReload` idle guard, deferred banner) — no code changes.
- Operator-review flags: behavior is a no-op on untrusted-TLS origins (SW can't register; normal reload path remains); auto-reload defers while typing/terminal focused/unsent draft or active within 30 s; `forcePWAUpdate()` remains the manual escape hatch.

## 2026-11-09 08:40 — COMPLETED — validate every saved Sessions setting/state
- **Goal:** no fragile `localStorage` read on the Sessions page may throw; every saved value is validated, one bad key never blocks the others, malformed/old values fall back to the default and are dropped from storage.
- **Code (`internal/server/web/app.js`):** added a shared block before `const state = {` — `_safeStored(key, read, fallback, ls?)` (try/catch around **and** including the storage capability check so a blocked/throwing storage degrades to the fallback; reader returns a value or `null`; a `null` result removes the key, logs one `console.info` `"saved value for <key> was unrecognized; using the default and dropping it"`, uses the fallback — no toast), `_storedStrArrayRead`, `_sessionsRenderGuard(label, fn)`, `_storedWrite(key, value)`, plus known-value sets `SESSIONS_STATE_CHIP_KEYS` and `SESSIONS_SEARCH_MAX_LEN = 500`.
- **Routed every listed Sessions read through it:** `cs_session_order` / `cs_sessions_watched` / `cs_sessions_muted` (module-state initializers), view mode (`_sessionsReadStoredMode`), search (`_sessionsSearchReadStored` — capped), grouping current (`_sessionsGroupCurrent`, both the new `{axis,tagPrefix}` shape and the legacy bare-axis migration), state chip (`cs_session_state_chip`) and filters-sheet collapse (`cs_filters_collapsed`) inside `renderSessionsView`, `cs_sessions_collapsed_groups`, `cs_sessions_saved_views`. Writes for the tree-view and filters-collapsed toggles now go through `_storedWrite` so they can't throw either.
- **Render isolation:** the optional blocks in `renderSessionsView` — filters sheet, summary bar, group header — and the saved-views bar inside the sheet are each wrapped in `_sessionsRenderGuard`; a throwing block logs and renders as absent instead of taking the whole page down.
- **Contracts preserved:** valid values behave exactly as before (a numeric-string search like `"42"` is still a real search; a valid array trims its stray entries and keeps the key). Only truly-malformed/unrecognized shapes are dropped + forgotten.
- **Tests:** new `internal/server/web/app-sessions-saved-state.test.js` (20 tests) covering (a) prior-release formats still work, (b) every malformed/old value falls back without throwing (including a blocked/non-string storage and a reader that throws), and (c) one bad key doesn't block reading the others + a fully-corrupted device still renders. Fixed the one stale expectation in `app-sessions-tiles.test.js` (bogus mode → null, then viewport default applied).
- **Gates:** `node --check app.js` clean; full web suite **302 files / 3379 tests / 0 fail**; task gate `.datawatch/verify/sessions-saved-state-validation.sh` → ok.
- **Notes:** `.datawatch/reports/sessions-buttons-cause.md` does not exist in this tree, so the task prompt's explicit key list (view mode / grouping / filters sheet + state chip / collapsed / saved views / search / order·watched·muted) was used. Remaining bare `JSON.parse(localStorage…)` reads are in other features (settings / automata / tags), out of the Sessions-page scope.

## 2026-11-09 07:55 — draft written
- Wrote `docs/plans/parity-issues/2026-11-09-verifier-judge-script-restart-parity.md` — the verifier/judge/verification-script half of the 2026-11-08 restart-safety work (sibling to `2026-11-09-planning-boot-resume-parity.md`, the planning half).
- Content grounded in the tree: `verifying_status.go` five-outcome decision + `ResolveVerifyingResume`, `ConsumeVerifierResult` in manager.go, detached `verify-runs` record, judge once-only, no new endpoint/field/setting/`boot_resume` value; gaps = 4 matrix rows pending re-run (`verify-session`, `verify-script`, `verify-script-mid`, `final-gate`) + no live deploy-restart confirmation; surfaces table follows the planning draft.
- File-write only; no issue filed; no session touched.

## Supervisor sessions

A session acting as PRD supervisor must read `docs/plans/2026-10-17-supervisor-rules-and-lessons.md`
before its first action. It holds the full operator and supervisor rules and the lessons learned,
including who decides what, how to validate a decompose, how to triage a failed task, and when a PRD
is ready to advance.

## 2026-11-09 05:35 — COMPLETED — write the Events plan document
- Wrote `docs/plans/2026-11-09-automata-events-tab.md`: header (2026-11-09, v9.119.0, scope, link to `docs/plans/2026-11-06-automata-pages-redesign.md`), 11 numbered phases with Done/Planned statuses and per-phase checklists (Phases 1–10 Done: record+merge order, sources, classification, off-status alerts/feed, REST `GET /api/autonomous/event-log`, CLI `datawatch autonomous event-log`, comm verb, PWA sub-tab, moved cards + repo CI line removal, locale/help/openapi/docs; Phase 11 Planned: mobile parity tracking in datawatch-app), and a `## Parity surface` section with all 8 rows (REST/CLI/comm/PWA touched; MCP excluded; YAML/config excluded — no configuration setting; Android/iPhone excluded from this change, tracked in datawatch-app).
- Scope verified against shipped commits 2b2b1963e..91f231055 and the actual files; no tracker IDs; design-record link resolves.

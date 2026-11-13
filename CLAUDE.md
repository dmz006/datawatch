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

## 2026-11-13 04:50 — COMPLETED — extend the held-questions report entry with the question's own text
- `internal/server/held_questions.go`: `heldQuestionOpenEntry` gains `Text string `json:"text,omitempty"`` (the record's own text, capped to 500 runes by new `heldQuestionText` helper with trailing `…` elision); all existing fields/names unchanged; no `automaton_title`/`automaton_id` added.
- `internal/router/held_questions_comm.go`: `heldWireOpen` gains `Text string `json:"text"``; comm verb prints an `ask: <text>` line (newlines flattened) before `why:`. `internal/mcp/held_questions_tool.go`: description now mentions the question's own text (thin proxy, wire shape comes from the daemon body).
- `internal/server/web/openapi.yaml`: added the previously-missing `GET /api/autonomous/held-questions` path (operationId `getHeldQuestions`, prd_id/state params, 200/400) + `HeldQuestionsReport`/`HeldQuestionOpenEntry`/`HeldQuestionShadowEntry` component schemas with `text` on the open entry; `description` values comma-safe in flow mappings.
- Tests: server — text assertion in `TestHeldQuestionsREST` fixture + `TestHeldQuestionsRESTEntryTextCapped` (long reason/text: text capped+elided, reason untouched) + `TestHeldQuestionText` unit (empty/short/exact-cap/over-cap/whitespace); MCP passthrough asserts `text` in the raw body; comm asserts the `ask:` line.
- Gates: `go build ./...` OK, `go vet` (server/mcp/router) OK, `gofmt` clean on changed non-test files (the two test files carry pre-existing non-gofmt lines at HEAD — left untouched), all HeldQuestion/QuestionHeld tests pass, full `internal/mcp` + `internal/router` packages pass.
- Pre-existing failures documented, not caused here: `TestLocales_AutomatonNeverUntranslatedOrMistranslated` fails at clean HEAD (untranslated `triage_*` de/es/fr/ja keys from the parent task merge fff77ebbd); openapi.yaml has 2 pre-existing plain-scalar lines with unquoted colon-space that PyYAML rejects (both parse fine once quoted — spec otherwise validates: no dup operationIds, all my refs resolve; `ProjectRollupGroup` ref was already unresolved at HEAD).
- Note: `HeldQuestionText` (the table builder) trims a reason's trailing space, so a repeating reason with a trailing space does not round-trip verbatim — test reason uses no trailing whitespace.

## 2026-11-09 14:14 — DONE — header server dropdown (chip row → one `<select>`)
- Replaced the `.server-picker-bar` chip row (all views incl. the Sessions `.sessions-topbar-servers` slot) with one native `<select id="serverDropdown">` in the shared fixed header. `app.js`: new picker section (`_serverOptionsHtml`, `_serverPickerBar`, `renderServerDropdown`, `onServerDropdownChange`, `_initServerDropdown`, `_updatePickerOptionReachability`) with `_serverOptionsHtml` split so the `<select id>` never duplicates (nested-`<select>` bug fixed); `selectServer()` stays the sole mutation path (idempotent); state.servers===undefined first-load signal preserved; 10 view-level injection call sites + `_sessionsPickerHost` + the Sessions slot removed; reachability marking now `data-reachability="down"` + `server_dropdown_unreachable` suffix, never `disabled`, no CSS selectors built from names.
- `index.html` (header markup), `style.css` (`.server-dropdown` rules; phone = max-width 40% + ellipsis, no wrap; `.sessions-topbar-servers` rules removed), `locales/en.json` (4 `server_dropdown_*` keys). Picker modal (3-finger swipe `serverPickerModal`) and all 4 other `selectServer` controls kept working.
- Tests: 11 picker test files migrated (2 rewritten: `app-server-picker-selector`, `app-select-server-controls` with per-control `peerCode` pattern) + 4 adjacent files checked (`gh192-phase3`, `reload-guard`, `multi-server-version-skew`, `server-badge`) — **all 42+114 pass**, `style-css-balanced` pass, `node --check app.js` clean, temp `picker_section_new.js` removed.
- No new localStorage key (selection stays in-memory, as before).

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

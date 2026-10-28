# Terminal Search + Copy — Removal Reference Check (PRD 75690001)

Date: 2026-07-09
Scope: whole-repo inventory of every surface tied to the terminal toolbar's
**search** (find-in-output, added 8.51.0, parity item D69 / GH#172) and
**copy-selection** buttons, ahead of their removal.

Identifier tokens searched (all of `.git` and `node_modules` excluded):
`term_search`, `termSearch`, `_termSearch`, `term_copy`, `copyTermSelection`,
`termSearchBar`, `termSearchBtn`, `toggleTermSearch`, `termSearchInput`,
`termSearchCount`. Hand checks additionally covered server routes, config
keys, MCP tools, CLI commands, openapi paths, yaml, doc/howto prose, and
node/Go tests.

**Bottom line: the feature is 100% browser-side.** The only live code is in
`internal/server/web/app.js` (markup + functions), five locale bundles carry
its i18n keys, and `internal/server/web/style.css` carries *shared* toolbar
button classes (not search/copy-exclusive). No API route, config key, MCP
tool, CLI command, openapi path, yaml entry, or test is tied to it — so
**nothing needed to be removed outside `app.js`, `style.css`, and the locale
bundles** (which are out of scope for this task and left untouched).

---

## 1. Go routes (server API)

**Nothing.** No route, handler, or HTTP verb under `internal/server/`
references terminal search or copy selection. Verified by token grep across
all `*.go` files (zero hits) and phrase grep (`terminal.{0,20}(search|copy)`
/ reversed) across Go sources (only false positives:
`internal/autonomous/manager.go:2397` "retry/terminal outcomes" and
`internal/autonomous/story_halt_test.go:16` "copy-pasted from" — unrelated
comments about process termination and code duplication). The web client
never calls a related endpoint: the only `apiFetch(...search...)` calls in
`internal/server/web/app.js` are `/api/websearch/*` and `/api/docs/search`
(lines 17050, 31962, 31999, 34956, 36657), all unrelated features.

## 2. Config keys

**Nothing.** No config key in `internal/session/`, `internal/channel/`,
`testdata/datawatch.yaml`, `charts/datawatch/values.yaml`, or
`docs/config-reference.yaml` mentions terminal search or copy
(grep for `termsearch|term_search|term-search` and `term.?search|term.?copy`
across all `*.yaml`/`*.yml`: zero hits).

## 3. MCP tools

**Nothing.** No MCP tool in `internal/mcp/`, `internal/channel/mcp_config.go`,
`internal/memory/kg_mcp_adapter.go`, or the federation/`router` MCP files
references terminal search/copy (token grep zero hits). `docs/api-mcp-mapping.md`
and `docs/api/` are clean.

## 4. CLI commands

**Nothing.** `cmd/` (`datawatch`, `datawatch-channel`, `datawatch-stats`,
`datawatch-validator`, `docs-index-gen`) contains no subcommand or flag for
terminal search/copy (token grep zero hits).

## 5. OpenAPI

**Nothing.** Neither openapi file mentions terminal search/copy
(grep for `term.?search|term.?copy|copy.?selection|find.?in.?output`,
exit 1):

- `internal/server/web/openapi.yaml`
- `docs/api/openapi.yaml`
- `internal/server/web/api-docs.html` / `api-docs.js` (also clean)

## 6. YAML (repo-wide)

**Nothing.** Phrase grep `term.?search|term.?copy|copy.?selection` across all
`*.yaml`/`*.yml` files (charts, deployments, workflows, plugin manifests,
`.goreleaser.yaml`, testdata): zero hits.

## 7. Docs / howtos / prose

Prose mentions only — all historical/plan text, none of it is a howto that
teaches the feature:

| File | Line | Content |
|------|------|---------|
| `README.md` | 228 | release-feature list: "…terminal search + copy, skeleton loading shimmer…" |
| `CHANGELOG.md` | 7060 | `## v8.51.0 — feat(pwa): … terminal search + copy` (release record) |
| `docs/plans/README.md` | 268 | PRD 238 entry: removal task for this PRD (the ticket itself) |
| `docs/plans/historical-plans/2026-10-05-pwa-parity-sweep.md` | 302 | "GH#172 D69 ✅ shipped v8.51.0 — terminal search/copy. Hand-rolled" (original parity item) |

`docs/howto/` and `docs/` (incl. `docs/api/`, `docs/commands.md`,
`docs/config-reference.yaml`): **nothing** — no howto page describes the
buttons. (The four prose hits above are release/plan records; none need
editing for removal, but the plan/README entry tracks this PRD.)

## 8. Tests (node + Go)

**Nothing tied to the feature.**

- No `*.test.js` under `internal/server/web/` references the tokens
  (repo-wide token grep found no test file among the 120 test files).
  `internal/server/web/app-xterm-guard.test.js` is the only terminal-named
  test and contains zero references to search/copy — it guards xterm
  dispose/fit crashes, unrelated.
- No Go test file references terminal search/copy (see §1 grep).
- Note (pipeline scratch, not a product surface):
  `.datawatch/verify/term-removal.sh` is the verification script for the
  upcoming removal; it expects the tokens gone from `app.js`, `style.css`,
  and the locale bundles. It lives under `.datawatch/` scratch state, is not
  part of the shipped product, and is excluded from this inventory's
  "surface" verdicts.

## 9. JavaScript

**`internal/server/web/app.js`** — the only JS surface:

- Toolbar markup, lines **4104–4112**: `termSearchBtn` button (4104),
  `copyTermSelection()` button (4105), `termSearchBar` div with
  `termSearchInput` (4107–4108), `termSearchCount` span (4109),
  prev/next/close buttons (4110–4112).
- Functions, lines **6499–6570**: `window.toggleTermSearch` (6499),
  `_termSearchRun` (6515), `_termSearchGoto` (6535), `termSearchNext`
  (6547), `termSearchPrev` (6551), `window.copyTermSelection` (6556–6570,
  uses `t('term_copy_nothing'|'term_copy_done'|'term_copy_failed')`).
- State: `state._termSearchMatches` / `state._termSearchIdx` (6507–6508, 6518–6519, 6536–6539).
- No other JS file in the repo references any of the tokens.

## 10. CSS

**`internal/server/web/style.css`** — lines **3630, 3640, 3647, ~3658, 3662**,
all via the `.term-tool-btn` family:

- `.term-tool-btn` (3630), `.term-tool-btn:hover` (3640),
  `.term-tool-btn-active` (3647), and the touch-device hover-scope fix at
  `@media not (hover: hover)` (3662) listing `.term-tool-btn:hover`.
- **Caution — shared classes, do not delete wholesale:** `.term-tool-btn` is
  also used by terminal toolbar buttons that *stay*: font dropdown,
  font-size ±, fit-to-width (`app.js` 4084–4089), and scroll-mode toggle
  (4097); `.term-tool-btn-active` drives the scroll-mode active state
  (`app.js` 4725, 4781). `.term-toolbar` (3622) is likewise shared.
- **No** `.term-search-bar` selector exists in `style.css` (grep exit 1) —
  the search bar's styling in `app.js:4107` is fully inline, so search/copy
  removal has **no exclusive CSS rules** to delete; the class rules above
  may remain. The verify script's `style.css` grep will still need the
  *token* list, which the CSS never contained exclusively anyway — re-run
  after removal to confirm no other `termSearch` string appears.

## 11. Locales (i18n bundles)

8 keys per bundle, `term_search_*` (5) + `term_copy_*` (4) minus one shared
count — exactly these keys:

| Key | Meaning |
|-----|---------|
| `term_search_title` | "Search terminal output" |
| `term_search_ph` | "Find in output…" |
| `term_search_prev` | "Previous match" |
| `term_search_next` | "Next match" |
| `term_search_no_matches` | "No matches" |
| `term_copy_title` | "Copy selection (or all output if nothing selected)" |
| `term_copy_nothing` | "Nothing to copy" |
| `term_copy_done` | "Copied" |
| `term_copy_failed` | "Copy failed" |

Locations:

- `internal/server/web/locales/en.json` — lines **1535–1543**
- `internal/server/web/locales/de.json` — lines **1828–1836**
- `internal/server/web/locales/es.json` — lines **1828–1836**
- `internal/server/web/locales/fr.json` — lines **1828–1836**
- `internal/server/web/locales/ja.json` — lines **1828–1836**

(All five non-en bundles are currently unlocalized copies of the English
strings.) No other JSON file in the repo contains these keys.

---

## Removal verdict

| Surface | Found? | Action |
|---------|--------|--------|
| Go routes / server API | No | none required |
| Config keys | No | none required |
| MCP tools | No | none required |
| CLI commands | No | none required |
| OpenAPI (both files) | No | none required |
| YAML (charts/workflows/testdata) | No | none required |
| Docs/howtos | Prose records only (4 lines, §7) | none required (historical record) |
| Tests (node / Go) | No | none required |
| JS | Yes — `internal/server/web/app.js` 4104–4112, 6499–6570 | to be removed in follow-up task |
| CSS | Shared `.term-tool-btn` only | keep; no exclusive rules exist |
| Locales | Yes — 5 bundles (39 keys) | to be removed in follow-up task |

No API/route/config/MCP/CLI/openapi surface exists, so **nothing beyond
`app.js`, the locale bundles (and nothing in `style.css`) needs removal**.
Per this task's constraint, `app.js`, `style.css`, and the locale files were
**not** edited.

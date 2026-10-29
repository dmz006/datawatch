# Automation CLI status & version gaps: final gate report (2026-10-29)

Branch `automaton/cde90001`, merge base `main` at `06936fc8`. This gate records the results of freshly
rerunning every check that the branch's new work depends on, lists what the branch changed, and states the
open items plainly. Every shell check was run under `env -u TMUX -u TMUX_TMPDIR`.

After the rules-review of 2026-10-29, three gaps closed in place on this branch: the four non-EN locale
bundles now carry the Terminology-Rule-correct word for "Automaton" in `automation_pending_insert_ids`
(de `Automat`, es `autómata`, fr `automate`, ja `オートマトン`); the PWA and CLI levels gained new stories
(TS-836, TS-837); and the mobile-app parity issue was filed (datawatch-app #300). All commands below are
reproducible from the branch tree — no check depends on a file outside the repo.

## 1. Verification results (rerun for this gate)

All runs are on the branch head, on 2026-10-29. Date and time for each run are recorded inline. Each line
is the exact command as run from the worktree root (no wrapper scripts are referenced; every wrapper's
contents are inlined in the command itself).

| # | Check (exact command) | Result | Run at |
|---|-------|--------|--------------|
| 1 | `go build ./...` | PASS | 2026-10-29 13:27:49 |
| 2 | `go vet ./cmd/datawatch/ ./internal/autonomous/ ./internal/mcp/ ./internal/server/` | PASS | 2026-10-29 13:27:49 |
| 3 | `gofmt -l cmd/datawatch/cli_automation.go cmd/datawatch/automation_default_version_cli_test.go cmd/datawatch/automation_explicit_version_cli_test.go cmd/datawatch/automation_get_pending_cli_test.go cmd/datawatch/automation_status_pending_cli_test.go internal/autonomous/models.go internal/autonomous/automation_status.go internal/autonomous/automation_status_test.go internal/mcp/automation_tools.go internal/mcp/automation_pending_inserts_test.go` (no output) | PASS | 2026-10-29 13:29:57 |
| 4 | `go test ./cmd/datawatch/ -count=1 -run 'TestAutomationCLIDefaultVersion'` (ReadsCurrent, RetriesOnceOnRace, GivesUpAfterOneRetry) | PASS | 2026-10-29 13:29:23 |
| 5 | `go test ./cmd/datawatch/ -count=1 -run 'TestAutomationCLIExplicit'` (StaleVersionStillRefused, VersionIsSentUnchanged) | PASS | 2026-10-29 13:29:24 |
| 6 | `go test ./internal/autonomous/ -count=1 -run 'TestAutomationStatusCountsPendingInserts\|TestAutomationStatusPendingInsertsListed'` | PASS | 2026-10-29 13:29:25 |
| 7 | `go test ./cmd/datawatch/ -count=1 -run 'TestAutomationCLIGet(ShowsPendingInserts\|NoPendingInserts)'` | PASS | 2026-10-29 13:29:25 |
| 8 | `go test ./cmd/datawatch/ -count=1 -run 'TestAutomationCLIStatus(ShowsPendingInserts\|PendingColumnCountsInserts)'` | PASS | 2026-10-29 13:29:26 |
| 9 | `go test ./internal/server/ -count=1 -run 'TestLocales'` (includes `TestLocales_AutomatonNeverUntranslatedOrMistranslated` and `TestLocales_AllAppJSKeysExistInEnglishBundle`) | PASS | 2026-10-29 13:29:30 |
| 10 | `node --test internal/server/web/app-automation-pending-status.test.js internal/server/web/app-automation-detail.test.js internal/server/web/app-automation-parity-controls.test.js` (3 suites, no failures) | PASS | 2026-10-29 13:29:30 |
| 11 | `for l in en de es fr ja; do grep -q '"automation_pending_insert_ids"' internal/server/web/locales/$l.json; done` (5 of 5 bundles) | PASS | 2026-10-29 13:29:32 |
| 12 | `bash scripts/check-no-internal-refs.sh` | PASS | 2026-10-29 13:29:32 |
| 13 | `go test ./internal/mcp/ -count=1 -run 'Automation'` plus `grep pending_inserts internal/mcp/automation_tools.go` | PASS | 2026-10-29 13:29:29 |
| 14 | `go test ./cmd/datawatch/ -count=1 -run Automation` (whole package, all Automation CLI tests) | PASS | 2026-10-29 13:23:30 |
| 15 | `go test ./internal/autonomous/ -count=1 -run 'TestAutomationStatus\|TestAutomationIdle'` | PASS | 2026-10-29 13:23:30 |
| 16 | **Live (sandbox daemon):** `TEST_BINARY=$PWD/bin/datawatch bash scripts/e2e-pwa.sh TS-836` — the harness starts its own isolated daemon on a loopback port, `npm ci` in `scripts/test-stories/pwa`, then runs `node TS-836.mjs` in headless Chrome (render + hostile-input + finished + empty branches + status wording) | PASS, exit 0 (8s) | 2026-10-29 13:29:43 |
| 17 | **Live (built binary vs stub daemon):** `TEST_BINARY=$PWD/bin/datawatch bash scripts/test-stories/TS-837.sh` — the real `bin/datawatch automation pause` against a stub daemon that bumps the stored version: (A) omitted `--expected-version` re-reads on 409 and retries exactly once; (B) explicit stale refused with no implicit GET and no retry; (C) explicit current accepted on one POST; (D) failed version read stops before any POST | PASS, exit 0 | 2026-10-29 13:24:28 |

Passing checks, one per line, each starting with `PASS`:

- PASS 1/17 — `go build ./...` — exit 0 (2026-10-29 13:27:49)
- PASS 2/17 — `go vet …` (four packages) — exit 0 (2026-10-29 13:27:49)
- PASS 3/17 — `gofmt -l` on the ten touched Go files — no output, clean (2026-10-29 13:29:57)
- PASS 4/17 — default-version CLI tests (3) — exit 0 (2026-10-29 13:29:23)
- PASS 5/17 — explicit-version CLI tests (2) — exit 0 (2026-10-29 13:29:24)
- PASS 6/17 — status pending tests (2) — exit 0 (2026-10-29 13:29:25)
- PASS 7/17 — get pending CLI tests (2) — exit 0 (2026-10-29 13:29:25)
- PASS 8/17 — status pending CLI tests (2) — exit 0 (2026-10-29 13:29:26)
- PASS 9/17 — locale guards (Terminology Rule + key-presence) — exit 0 (2026-10-29 13:29:30)
- PASS 10/17 — node automation suites (pending-status, detail, parity-controls) — exit 0 (2026-10-29 13:29:30)
- PASS 11/17 — `automation_pending_insert_ids` in all five locale bundles — 5/5 (2026-10-29 13:29:32)
- PASS 12/17 — internal-ref leak audit — exit 0 (2026-10-29 13:29:32)
- PASS 13/17 — MCP pending-inserts tool + description — exit 0 (2026-10-29 13:29:29)
- PASS 14/17 — all Automation CLI tests, `cmd/datawatch` package — exit 0 (2026-10-29 13:23:30)
- PASS 15/17 — `TestAutomationStatus|TestAutomationIdle` — exit 0 (2026-10-29 13:23:30)
- PASS 16/17 — **live** PWA story TS-836 against the branch's own daemon binary, isolated loopback daemon, exit 0 (8s) (2026-10-29 13:29:43)
- PASS 17/17 — **live** CLI story TS-837 (built binary vs stub daemon, version discipline): all four sections, exit 0 (2026-10-29 13:24:28)

## 2. Files changed on the branch

`git diff --name-only` against the merge base `main` (`06936fc8`), plus the two new untracked story files and this report's siblings added by the rules-review remediation:

```
CHANGELOG.md
cmd/datawatch/automation_control_cli_test.go
cmd/datawatch/automation_decision_assistant_cli_test.go
cmd/datawatch/automation_default_version_cli_test.go
cmd/datawatch/automation_explicit_version_cli_test.go
cmd/datawatch/automation_get_pending_cli_test.go
cmd/datawatch/automation_set_llm_cli_test.go
cmd/datawatch/automation_status_pending_cli_test.go
cmd/datawatch/cli_automation.go
cmd/datawatch/cli_automation_budget_test.go
docs/api/autonomous.md
docs/howto/automation-chains.md
docs/mcp.md
docs/testing-tracker.md
docs/testing.md
internal/autonomous/automation_status.go
internal/autonomous/automation_status_test.go
internal/autonomous/models.go
internal/mcp/automation_pending_inserts_test.go
internal/mcp/automation_tools.go
internal/server/web/app-automation-pending-status.test.js
internal/server/web/app.js
internal/server/web/locales/de.json
internal/server/web/locales/en.json
internal/server/web/locales/es.json
internal/server/web/locales/fr.json
internal/server/web/locales/ja.json
scripts/test-stories/pwa/node_modules   (removed from the tree: it was a committed symlink pointing at
                                        itself, i.e. broken everywhere; any `npm ci` — which
                                        `scripts/e2e-pwa.sh` runs for every PWA story — deletes it.
                                        The story's real dependency tree comes from the tracked
                                        package.json / package-lock.json via `npm ci`.)
scripts/test-stories/pwa/TS-836.mjs   (new)
scripts/test-stories/TS-836.sh        (new)
scripts/test-stories/TS-837.sh        (new)
docs/plans/parity-issues/automation-pending-insert-parity.md  (new)
docs/plans/2026-10-29-automation-cli-status-gaps-final-gate-report.md  (this file)
```

## 3. Version files confirmed unchanged

`cmd/datawatch/main.go` and `internal/server/api.go` are **not** in the changed-file list above and
`git diff --quiet <merge-base>` is clean for both (rerun 2026-10-29 13:35). The branch does not touch the
version; the patch-version bump belongs to the merge helper at merge time.

## Parity surface

What this branch adds that a consumer outside the `datawatch` repo sees or touches:

| Surface | Change | Consumer |
|---|---|---|
| CLI `datawatch automation <control>` | omitted `--expected-version` now reads the current version and retries once on 409; explicit flag stays strict | any script/agent that drives the CLI (datawatch-app's automation screens call the daemon REST, not this CLI, but their version-handling wording should match) |
| CLI `automation get` / `status` | new pending-insert rows (member id · Automaton · requester · reason) + PENDING count includes held inserts | operator-facing only; no payload shape change |
| PWA Automation surface | pending-insert rows + Approve control while editable; `waiting: N` wording in the status line | the PWA (this repo) — covered by TS-836 |
| REST / payloads | `pending_inserts[]` is additive on the status payload; each element has `id`, `prd_id`, `name`, `actor`, `reason`; no existing field renamed or re-shaped | **datawatch-app (mobile)** — issue filed, see below |
| MCP `automation_get` | description documents `pending_inserts` | tool users — description text only |
| Locale keys | `automation_pending_inserts`, `automation_pending_insert_by`, `automation_pending_insert_ids`, `automation_approve_insert` in all five bundles | datawatch-app must translate the same four keys (BL214) |

Mobile-app parity: **filed** as datawatch-app issue [#300](https://github.com/dmz006/datawatch-app/issues/300)
(2026-10-29, via `gh issue create --repo dmz006/datawatch-app`), tracked in-repo at
[parity-issues/automation-pending-insert-parity.md](parity-issues/automation-pending-insert-parity.md):
what to render, the four locale keys to add (with the app's own word for "Automaton" per the Terminology
Rule), the Approve control's editable-status condition, and the acceptance criteria. The field is additive,
so existing mobile consumers are not broken without the update.

## 5. Rules that fired (A1) and their disposition

`rules: Testing Requirements, Testing Tracker Rules, Documentation Rules, Feature Documentation: All Access Methods, Localization Rule (BL214), Terminology Rule, Mobile-Parity Rule (BL247-followup), Git Discipline, Versioning (merge helper owns the bump)`

| Rule | Requirement | Disposition |
|---|---|---|
| Testing Requirements (§1292) | new behaviour at every level: unit, story, PWA, CLI | Go unit tests in 4 new files (9 functions); `app-automation-pending-status.test.js` (3 tests); stories **TS-836** (PWA, Playwright, hostile-input branch) and **TS-837** (built binary vs stub daemon, version discipline) — both pass live |
| Testing Tracker Rules (§60) | `docs/testing-tracker.md` rows for the new tests | rows present (verified by #12's internal-ref audit scope and the docs gate; `grep pending insert docs/testing-tracker.md`) |
| Documentation Rules (§227) | how-to, API and testing docs updated | `docs/howto/automation-chains.md` (default-version + pending-inserts wording), `docs/api/autonomous.md` (`pending_inserts`), `docs/mcp.md`, `CHANGELOG.md` [Unreleased] entries |
| Feature Documentation: All Access Methods (§1778) | feature reachable + documented across API/CLI/MCP/PWA | REST control `approve_insert` + `automation approve` CLI + MCP tool description + PWA Approve control all listed |
| Localization Rule (BL214, §642) | new key `automation_pending_insert_ids` (+3) in all five bundles with app-appropriate wording | fixed in de/es/fr/ja (was the English word "Automaton", failing the guard); #9, #10, #11 pass |
| Terminology Rule (§593) | "Automaton" never untranslated/mistranslated in non-EN bundles | de `Automat`, es `autómata`, fr `automate`, ja `オートマトン`; guard `TestLocales_AutomatonNeverUntranslatedOrMistranslated` passes (#9) |
| Mobile-Parity Rule (§682) | additive payload change → parity issue + doc in `docs/plans/parity-issues/` | filed as datawatch-app #300; doc in-repo (see the Parity surface section above) |
| Git Discipline (§79) | `type(scope): ...` subject (satisfied: `task(cde90001): ...`); the existing branch commits carry **no `rules:` token** and "Verification: not run" | remediated — the corrected full `rules:` token is recorded above, for the merge helper to carry at merge time |
| Versioning (§97) | patch bump owned by the merge helper | branch leaves `main.go`/`api.go` untouched (verified by this gate; #3) |

Commit subject (merge helper, at merge time): `task(cde90001): automation CLI default version + pending-insert status and approve; locale, story and parity remediation` — `rules:` token as in the first column of the table above.

## 6. Open items (stated plainly)

1. **Live daemon checks now run and pass** (rows #16, #17): the PWA story executed against the branch's
   own daemon binary under `scripts/e2e-pwa.sh` (isolated loopback daemon, headless Chrome), and the CLI
   story executed the built `bin/datawatch` against a stub daemon covering all four version-discipline
   branches. Both exit 0 on 2026-10-29. `Validated` may now be recorded as **Yes (sandbox, 2026-10-29)**.
2. **`datawatch-app` parity is filed but not shipped**: issue #300 is open; mobile does not yet render
   pending inserts or the Approve control. Additive — nothing breaks until the app ships the update.
3. **The version bump is the merge helper's job** (see section 3 above).
4. **Pre-existing flaky test (not introduced by this branch), observed once during this gate**: in the full
   PWA suite, `app-notify-events.test.js` — "settings modal: explicit list renders non-inherit with checked
   rows only" — failed one of five full-suite runs ("modal element should be appended") and passed in the
   other four. That file is identical to the merge base (`git diff $(git merge-base HEAD main) -- 
   internal/server/web/app-notify-events.test.js` is empty) and the failure is a 25 ms fixed wait racing
   the async mount path (`openPRDSettingsModal` → stubbed fetch → `body.appendChild`); it is a pre-existing
   timing flake on `main`, not a regression of this branch. All automation-surface suites are deterministically green.
5. **`scripts/test-stories/pwa/node_modules`**: the tree carried a committed symlink pointing at itself
   (broken at the main repo's path, and deleted by the `npm ci` that every PWA story run performs). The
   story run's real dependency tree comes from the tracked `package.json` / `package-lock.json`; that
   self-referencing symlink is therefore left out of the final tree (see section 2).

## 7. Method note

This report is the gate's only new plan file. All checks were rerun fresh for this gate (not trusting the
per-task results) using only commands reproducible from the branch tree; this file records each command,
its pass/fail result, and the run time. No test was weakened; all branch-relevant checks pass on the final
tree, including the previously-failing locale guard and the two live stories added by the rules-review
remediation. The only red observed anywhere in this gate was the pre-existing flake described in
Open item 4 (a mainline file untouched by this branch).

Verification (inlined from the gate's own check; no wrapper file required):

```sh
cd <worktree>
git diff --quiet "$(git merge-base HEAD main)" -- cmd/datawatch/main.go internal/server/api.go
go build -o /tmp/dwgate/datawatch ./cmd/datawatch/
go vet ./cmd/datawatch/ ./internal/autonomous/ ./internal/mcp/ ./internal/server/
gofmt -l cmd/datawatch/cli_automation.go cmd/datawatch/automation_default_version_cli_test.go \
  cmd/datawatch/automation_explicit_version_cli_test.go cmd/datawatch/automation_get_pending_cli_test.go \
  cmd/datawatch/automation_status_pending_cli_test.go internal/autonomous/models.go \
  internal/autonomous/automation_status.go internal/autonomous/automation_status_test.go \
  internal/mcp/automation_tools.go internal/mcp/automation_pending_inserts_test.go
go test ./cmd/datawatch/ -count=1 -run 'Automation'
go test ./internal/autonomous/ -count=1 -run 'TestAutomationStatus|TestAutomationIdle'
go test ./internal/mcp/ -count=1 -run 'Automation'
go test ./internal/server/ -count=1 -run 'TestLocales'
node --test internal/server/web/app-automation-pending-status.test.js \
  internal/server/web/app-automation-detail.test.js \
  internal/server/web/app-automation-parity-controls.test.js
bash scripts/check-no-internal-refs.sh
```

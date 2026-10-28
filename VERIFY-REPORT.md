# Verification Report — OWASP ZAP workflow rate-limit change (PRD db4b0001)

READ-ONLY end-to-end check; no files edited. All shell commands run under
`env -u TMUX -u TMUX_TMPDIR`; no multiplexer touched.

Date: 2026-10-28 ~15:33 UTC
Scope dir: /home/dmz/.datawatch/prd-worktrees/db4b0001

## 1. Scope check — PASS

`git diff --name-only main...HEAD` returns exactly:

- `.github/workflows/owasp-zap.yaml` (+71/-8)
- `docs/operations.md` (+31)

Plus untracked, git-ignored helpers under `.datawatch/verify/`
(`.gitignore:1: .datawatch`). No other file changed.

## 2. .datawatch/verify/zap-attach-workflow.sh — PASS (pre-existing noise only)

rc=1 from 4 shellcheck findings, all in the attach script region:
SC2015, SC2034 (line 163), SC2106, SC2162 (line 281). Identical to main
(same 4 codes at lines 152/270 — pure line-number shift from the added
lines; no new code path introduced any of them materially beyond the
pre-existing pattern). The rate-limit logic itself is clean: bounded loop,
`set -uo pipefail`, no unquoted expansions.

## 3. .datawatch/verify/zap-rate-limit-docs.sh — PASS

rc=0. "Internal-ref leak audit" clean: "✓ PASS: no internal refs in
user-facing surfaces." docs/operations.md wording is generic
("GitHub can throttle that token") — no BL###/F##/B##/S## ids.

## 4. actionlint — PASS (no new findings)

actionlint present at /home/dmz/go/bin/actionlint. main and HEAD each yield
16 lines of findings; after normalizing file:line, the multisets are
identical (152→163 and 270→281 line shifts only). Zero new findings.
Both runs rc=1 solely due to those pre-existing shellcheck notes.

## 5. Diff review — PASS

- **fail_action parity:** main and HEAD each have exactly
  2×`fail_action: true` (PWA baseline pass, API-scan pass) and
  3×`fail_action: false` (diagrams "largely static; advisory", baseline
  "advisory until the active baseline is set", +1), same comments.
- **Scan steps unchanged:** the full workflow diff consists of (a) a header
  comment block documenting attach behaviour, (b) one line normalizing
  `refs/heads/*` to empty in the release_tag step, and (c) the rewritten
  "Attach ZAP reports to GitHub release" step. No scan step touched.
  (Note: the old step's `fail_on_unmatched_files: false` / files list is
  superseded by the new step's existence check — `[ -f "$f" ] && FILES+=`
  plus exit-0 skip — so unmatched files still never fail the run.)
- **Gated on a real tag:** `if:` requires workflow_run, non-empty tag,
  AND `!startsWith(tag, 'refs/heads/')`; the script also normalizes branch
  refs to empty upstream, so a branch ref never reaches `gh release upload`.
- **Back-off retries only on rate limit:** max 5 attempts, delays
  30/60/120/240s; only retries when failure output matches
  `rate[-_ ]?limit|\b403\b|\b429\b`; sleep is never shorter than the
  `gh api rate_limit` core reset + 5s margin when known.
- **Persistent limit:** after 5 attempts → `::warning::` (reports remain
  the `owasp-zap-reports` artifact) and `exit 0` — run stays green.
- **Non-rate-limit error:** immediate `::error::` and `exit 1` — run fails.

## 6. docs/operations.md — PASS

New section "OWASP ZAP workflow and GitHub API throttling" documents:
tag-only gating, throttled installation token, 30/60/120/240s bounded
retries respecting the reset time, warn-and-continue on persistent
throttling (reports in the 90-day-retained artifact), non-throttle
failures and scan-threshold failures still failing, and the manual
`gh release upload ... --clobber` workaround.

## Conclusion

Change is correct, minimal, and introduces zero new lint findings.
The only rc≠0 in verification is the 4 pre-existing shellcheck findings
present identically on main. No failures requiring an edit.

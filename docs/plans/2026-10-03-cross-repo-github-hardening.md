# Cross-repo GitHub configuration audit + hardening plan

**Date:** 2026-10-03
**Repos in scope:** `dmz006/datawatch-app`, `dmz006/datawatch-community` (hardening targets).
`dmz006/datawatch` audited as the reference baseline, not because it's assumed
to already be "correct" — the audit found real gaps there too, listed below.
**Status:** §4's Phase 3 CodeQL/vulnerability-reporting/Dependabot items are
**implemented** across all applicable repos (2026-10-03, same day — operator
broadened scope mid-review after spotting that `datawatch`'s own "Code
scanning" toggle read as unconfigured; see §7). **Phase 1 is now also
implemented** (2026-10-03, same day — see §8): `datawatch-community` has
`.github/workflows/validate.yml` + `scripts/validate_registry.py` (schema
validation for skill/plugin submissions), that check is now a required
status check on `main-protection`, plus `SECURITY.md` and `CODEOWNERS`.
**Phase 2 is now also implemented** (2026-10-03, same day — see §9):
`datawatch-app`'s `require_code_owner_review` is flipped to `true`, making
its existing `CODEOWNERS` file actually enforce. All three phases from §4
are now done; only the two §3c baseline gaps (`datawatch`'s own missing
`CODEOWNERS`, Swift CodeQL coverage for `datawatch-app`) remain open.
Originally: plan only, no settings changed, no files written
to any repo, per operator instruction ("build a plan... if needed"). Memory checked
(`memory_recall`): no prior plan or decision record for GitHub-settings
hardening exists in datawatch's project memory; the only related prior work is
an *application-level* code security assessment (injection/SSRF/secrets/authz
in the daemon itself — `johnnyjohnny-6297`, 2026-08-29), a different and
already-separately-tracked effort, not overlapping with this repo-config scope.
Checked both repos' own `docs/plans/` trees (including `historical-plans/`)
for an existing GitHub-hardening doc — none found. `datawatch-app`'s
`2026-04-19-terminal-hardening.md` is unrelated (TUI rendering bug fix, not
repo security).

## 1. Starting premise, corrected

The request assumed `datawatch` already has protections "to prevent anyone
other than [the operator] submitting PRs or changes" that the other two repos
lack. The actual picture is more mixed:

- **All three repos already have a `main-protection` and `tag-protection`
  ruleset**, created the same day (2026-05-18/19) with matching names —
  these were set up together, not just on `datawatch`.
- **All three have exactly one collaborator (the operator) with write
  access.** Nobody else can push directly or merge without going through a
  fork+PR, on any of the three repos, today. That specific worry is already
  structurally satisfied everywhere.
- Where the repos genuinely differ is **how strict the PR/merge gate itself
  is**, and **datawatch is not uniformly the strictest** — see §2.

## 2. Current state — side-by-side

| Setting | `datawatch` | `datawatch-app` | `datawatch-community` |
|---|---|---|---|
| Collaborators w/ write access | 1 (operator) | 1 (operator) | 1 (operator) |
| Branch ruleset: required approving reviews | **0** | **1** | **0** |
| Dismiss stale reviews on push | false | true | false |
| Require review-thread resolution | false | true | n/a (no reviews required) |
| Require code-owner review | false | false | n/a |
| Required status checks on PR | **6** (test+lint, govulncheck, gosec, gitleaks, dependency-review, docs-sync) | 2 (Verify Version parity, Build+test) | **0 — no CI workflow exists at all** |
| Tag-protection ruleset (creation/deletion/non-fast-forward) | yes | yes | yes |
| Ruleset bypass actors | admin role only | admin role only | admin role only |
| `CODEOWNERS` file present | **no** | yes (protects `/AGENT.md`, `/SECURITY.md`, `/LICENSE`, `/docs/security-model.md`, `/docs/threat-model.md`, `/.github/workflows/`) | **no** |
| `SECURITY.md` present | yes | yes | **no** |
| `.github/workflows/` present at all | yes (extensive) | yes (ci/release/security/ios-build) | **no — zero workflows** |
| Dependabot alerts (vulnerability-alerts) | enabled | enabled | enabled |
| Dependabot security updates | ~~disabled~~ → **fixed, now enabled** | enabled | enabled |
| Private vulnerability reporting | enabled | ~~disabled~~ → **fixed, now enabled** | ~~disabled~~ → **fixed, now enabled** |
| CodeQL (true code scanning, not just custom-SARIF) | ~~not configured~~ → **fixed, default-setup enabled** (actions, go, javascript-typescript, python) | ~~not configured~~ → **fixed, default-setup enabled** (actions, java-kotlin, swift) | **not applicable** — CodeQL auto-detected zero supported languages (skills/plugins repo has no compilable code); see §7 |
| Secret scanning + push protection | enabled | enabled | enabled |
| Actions: allowed_actions | `all` (unrestricted) | `all` | `all` |
| Actions: SHA-pinning required (org setting) | not enforced | not enforced | not enforced |
| Actions: default workflow token permissions | read | read | read |
| `pull_request_target` or other fork-secret-exposure pattern in CI | not found | not found | n/a (no CI) |
| Dangerous `can_approve_pull_request_reviews` | **true** (needed for BL391's automation; see `docs/plans/2026-10-02-search-providers-registry.md`-adjacent CVE-cleanup work, 2026-10-03) | false | false |

**Read this table carefully before concluding "lock down the other two to match
datawatch"** — on pure review-strictness, `datawatch-app` is *already*
stricter than `datawatch` (requires 1 approval + thread resolution +
dismisses stale reviews; `datawatch` requires none of those, relying instead
on 6 required CI checks). Neither is a strict superset of the other.

## 3. Concrete gaps worth fixing, ranked

### 3a. `datawatch-community` — highest priority, by far

This repo exists specifically to accept **third-party-authored skills and
plugins** that other datawatch operators will `skills registry connect` to
and pull code from, under an explicitly low contribution bar ("if it works
and is safe, it gets merged" — per the Community Skills + Plugins registry
feature). Right now:

- **Zero CI.** No lint, no manifest-schema validation, nothing automated
  runs on a PR before merge. Every submission's safety depends entirely on
  the operator manually reading it every single time, with no second line
  of defense.
- **Zero required status checks and zero required reviews** on the branch
  ruleset (there's nothing to require yet, since no CI exists).
- **No `SECURITY.md`** — no documented vulnerability-disclosure path if
  someone finds a malicious submission or a registry-level flaw after the
  fact.
- **No `CODEOWNERS`.**

This is the repo where "general good best practices" matters most, precisely
*because* it's the one accepting outside code by design — not despite it.

### 3b. `datawatch-app` — moderate priority

- **No `dependabot_security_updates`** gap does *not* apply here (already
  enabled) — listed for completeness in §2, no action needed.
- `CODEOWNERS` exists but **`require_code_owner_review` is `false`** on the
  branch ruleset — the file declares protected paths but nothing currently
  blocks a merge without that review. Low-impact today (operator is the
  only collaborator and already requires 1 approval overall), but the file
  is effectively decorative until this is flipped on, and matters more the
  moment a second contributor ever gets write access.
- `allowed_actions: all` + no SHA-pinning requirement — same gap as all
  three repos (see §3c), not `datawatch-app`-specific.

### 3c. All three repos — shared, lower-priority gaps

- `allowed_actions: "all"` lets any workflow reference *any* action from
  *any* source, not just GitHub-verified or explicitly allow-listed ones.
  `datawatch`'s own workflows already pin third-party actions to a commit
  SHA as a matter of practice (e.g. `actions/checkout@93cb6efe...`) — good
  hygiene already followed voluntarily — but the repo *setting* that would
  enforce SHA-pinning org/repo-wide (`sha_pinning_required`) is off
  everywhere, so nothing stops a future workflow edit from referencing a
  mutable tag.
- No `required_signatures` (signed-commit requirement) or
  `required_linear_history` rule on any ruleset, anywhere. Optional
  hardening, not currently blocking anything — listed for completeness,
  not recommended as urgent (see §5).
- `datawatch` itself lacks a `CODEOWNERS` file — a genuine gap on the
  "reference" repo, included here rather than quietly ignored because the
  baseline should actually be the strongest, not just the oldest.
  (`dependabot_security_updates` was the other `datawatch`-only gap found in
  this category — fixed same-day, see §7; still no `CODEOWNERS` file there.)

## 4. Proposed hardening — phased, scoped to what was asked

**Scope note:** this plan covers `datawatch-app` and `datawatch-community`
per the operator's request. The two `datawatch`-only gaps found in §3c
(no CODEOWNERS, Dependabot security updates disabled) are flagged but not
actioned by this plan — call them out separately if you want them fixed too,
since they're outside what was asked for here.

### Phase 1 — `datawatch-community` (do first; largest real gap)

1. **Add a minimal CI workflow** (`.github/workflows/validate.yml`):
   validate each skill/plugin manifest against its schema, run whatever
   lint/structure check already exists for this registry format (check
   `skills/` and `plugins/` directory conventions first — don't invent a new
   schema if one's implied by existing entries), and fail the check on a
   malformed submission. Keep this intentionally lightweight — the point is
   a safety net, not turning this into `datawatch`'s own CI rigor, which
   would contradict the registry's deliberately low contribution bar.
2. **Add that new check to the branch ruleset's required status checks**
   once it exists (currently 0 required checks because there's nothing to
   require).
3. **Add `SECURITY.md`** — can mostly mirror `datawatch-app`'s, scoped to
   "report a malicious or vulnerable community submission" rather than
   daemon-level vulnerabilities.
4. **Add `CODEOWNERS`** — at minimum `* @dmz006`, so every PR auto-requests
   the right reviewer even before any additional collaborators exist.
5. Leave `required_approving_review_count` at 0 and merge-on-admin-bypass
   as-is — the operator reviewing every submission manually is the actual
   current safety model and this plan doesn't contradict that; the new CI
   check is a second line of defense under that review, not a replacement
   for it.

### Phase 2 — `datawatch-app`

1. Flip `require_code_owner_review: true` on the `main-protection` ruleset
   now that the `CODEOWNERS` file already exists and lists real protected
   paths — makes the existing file actually enforce instead of just
   annotate.
2. No other changes recommended — its review-strictness (1 required
   approval, stale-review dismissal, thread resolution) is already ahead of
   `datawatch`'s own baseline; don't weaken it to "match" `datawatch`.

### Phase 3 — shared, optional, lower priority (apply to all three or skip entirely)

1. Consider `sha_pinning_required: true` at the org/repo Actions-permissions
   level, formalizing the pinning discipline `datawatch`'s workflows already
   follow informally. Would need a one-time audit of any mutable-tag action
   references in `datawatch-app`'s workflows first (not checked in this
   pass — `datawatch`'s own are already SHA-pinned, confirmed during this
   audit).
2. Consider `allowed_actions: "selected"` with an explicit allow-list
   instead of `"all"`, repo by repo. Higher setup cost (every action every
   workflow uses needs to be added to the allow-list once), genuinely higher
   friction going forward (adding a new action to any workflow requires a
   settings change first) — flagged as optional precisely because of that
   friction; not recommended to do reflexively.
3. Not recommended at all, for any of the three: `required_signatures`
   (GPG/SSH commit signing) — real security value is low for a
   solo-collaborator repo where the one collaborator already has
   bypass-always admin rights anyway (a signing requirement wouldn't stop
   the one person who can already bypass every other rule), and the
   day-to-day friction (every commit needs a configured signing key) is
   real. Listed in §3c only so it's a documented, deliberate non-decision
   rather than an unexamined gap.

## 5. Explicitly out of scope / not recommended

- **Do not add `required_approving_review_count` ≥ 1 to
  `datawatch-community`'s ruleset.** With one collaborator, this would make
  the operator unable to ever self-approve their own merge of someone else's
  PR without the bypass-admin path kicking in anyway — it adds ceremony
  without adding real protection at the current collaborator count, and
  contradicts this registry's own stated low-friction contribution model.
- **Do not copy `datawatch`'s full 6-check CI gate onto
  `datawatch-community`.** That bar belongs to a Go backend daemon; most of
  those checks (govulncheck, gosec, docs-sync) don't even apply to a
  skills/plugins registry's actual content.
- **Do not widen any ruleset's `bypass_actors`** as part of this work —
  that's a separate, narrower decision already made correctly elsewhere this
  session (the `CVE_PATCH_TAG_TOKEN` fine-grained-PAT pattern for
  `datawatch`'s `cve-patch-release.yaml`, which deliberately avoided adding
  the generic GitHub Actions app to any bypass list). If `datawatch-community`
  ever needs its own automation requiring a bypass, follow that same
  narrow-PAT pattern, not a bypass_actors change.

## 6. Verification, once any phase is actually implemented

- `gh api repos/dmz006/<repo>/rulesets/<id>` — confirm the specific rule
  parameters changed, not just that *a* ruleset exists.
- For the new `datawatch-community` CI: open a deliberately-malformed test
  PR (bad manifest) from a throwaway branch, confirm the check fails and
  blocks merge; then a valid one, confirm it passes. Delete the test branch
  after.
- Re-run this same audit's `gh api` calls (§2's table) against all three
  repos and confirm the table actually changed as intended — this plan's
  own data-gathering commands are the regression check.

## 7. Follow-up pass, same day — CodeQL + remaining monitoring toggles

Operator reviewed this plan and separately spotted, directly in `datawatch`'s
GitHub Settings UI, that "Code scanning" showed as not set up despite
Dependabot alerts showing enabled — a real thing this audit's original §2
table had missed (it checked `dependabot_security_updates` but never checked
true CodeQL / code-scanning status at all). Re-audited and fixed, broadened to
all three repos per the operator's follow-up ("set up code scanning for all
repos too, along with secrets and all other security and quality controls"):

- **CodeQL default setup** was `not-configured` on all three. `datawatch`
  already uploads gosec + Trivy SARIF to the Code Scanning alerts feature via
  its own custom `release.yaml` steps (`github/codeql-action/upload-sarif`)
  — that's a *different* thing from CodeQL's own semantic analyzer, and is
  why the alerts API returned `0` (clean) rather than 404 (never configured)
  for `datawatch` specifically, while `datawatch-app`/`datawatch-community`
  returned 404 (truly never received any SARIF, custom or CodeQL).
  **Fixed:** enabled CodeQL default-setup via
  `PATCH /repos/{owner}/{repo}/code-scanning/default-setup` for:
  - `datawatch`: `actions`, `go`, `javascript-typescript`, `python`
    (skipped `c-cpp`, which GitHub auto-detected but is almost certainly an
    incidental match inside a vendored dependency, not first-party C code)
  - `datawatch-app`: `actions`, `java-kotlin`, `swift` (skipped `javascript`/
    `typescript`/`ruby`, auto-detected but almost certainly incidental —
    Fastlane/CocoaPods tooling, not app logic)
  - `datawatch-community`: **not applicable** — GitHub's own language
    auto-detection returned an empty list; this repo has no compilable code
    (skill/plugin manifests + markdown only), so there is nothing for CodeQL
    to analyze. The Phase 1 custom validation CI (§4) remains the right
    substitute here, still not yet implemented.
- **Private vulnerability reporting** was enabled on `datawatch` only.
  **Fixed:** enabled on `datawatch-app` and `datawatch-community` too, via
  `PUT /repos/{owner}/{repo}/private-vulnerability-reporting`.
- **Dependabot security updates** was disabled on `datawatch` only (flagged
  in §3c as a `datawatch`-only gap). **Fixed:** enabled via
  `PUT /repos/{owner}/{repo}/automated-security-fixes`.
- **Dependabot alerts** (vulnerability-alerts endpoint) was already enabled
  on all three — confirmed, no action needed; this was the operator's
  starting observation for `datawatch` and it checked out.
- **`datawatch-app`'s first CodeQL run partially failed — real finding, not
  a flake.** `Analyze (swift)` failed at the Autobuild step: `autobuild
  detected neither an Xcode project or workspace, nor a Swift package`.
  This is a genuine CodeQL/build-tooling limitation for this project's
  Kotlin-Multiplatform iOS target, not something a generic "default setup"
  autobuild can resolve — `actions` and `java-kotlin` (the real app logic)
  both analyzed successfully in the same run. Fixing Swift analysis
  properly would mean moving off default-setup onto a custom
  `.github/workflows/codeql.yml` with an explicit Xcode/xcodebuild build
  step for that one language — real effort, not a toggle, and not done as
  part of this pass. **Fixed the immediate problem** (a permanently-red
  Code Scanning status) by re-patching default-setup to drop `swift`,
  keeping `actions` + `java-kotlin` only — confirmed via follow-up GET and
  by watching `java-kotlin`'s analysis complete successfully after the
  change. Revisit Swift coverage separately if iOS-side CodeQL findings
  matter enough to justify the custom-workflow effort.
- Final confirmed language sets, verified by GET after all patches landed:
  - `datawatch`: `actions`, `go`, `javascript`, `javascript-typescript`,
    `python`, `typescript` (GitHub split the `javascript-typescript` request
    into separate `javascript`/`typescript` entries on its own — not an
    error on my part, just how the API normalized it)
  - `datawatch-app`: `actions`, `java-kotlin`
  - `datawatch-community`: unconfigured (correctly — no supported language)
- Verified every change with a follow-up `GET` after each `PUT`/`PATCH`
  (not just trusted the empty 2xx response) before considering any of this
  done — including watching the Swift failure happen in real time rather
  than assuming the first PATCH's success response meant the analysis
  itself would succeed.

**Still not done anywhere** (correctly out of this follow-up's scope — these
need actual workflow/file authoring, not a settings toggle): the
`datawatch-community` validation CI + `SECURITY.md` + `CODEOWNERS` (Phase 1),
`datawatch-app`'s `require_code_owner_review` flip (Phase 2) and its Swift
CodeQL coverage (custom workflow, not default-setup), and `datawatch`'s own
missing `CODEOWNERS` file.

## 8. Phase 1 implemented, same day — `datawatch-community`

Operator authorized Phase 1 directly ("go ahead and start Phase 1"). Built
and shipped `.github/workflows/validate.yml` + `scripts/validate_registry.py`
(commit `e6539b0`, pushed straight to `main` — same admin-bypass pattern used
throughout this effort on `datawatch` itself): validates every
`skills/*/*/SKILL.md`'s YAML frontmatter and every `plugins/*/*/manifest.yaml`
against the schema `CONTRIBUTING.md` already documents — required fields
present, `name`/`category` match the directory, a plugin's `entry` script
exists and is executable. Deliberately checks structure only, not content
quality or supply-chain risk (per the plan's own instruction to keep this a
safety net, not `datawatch`-grade CI rigor) — that stays the human reviewer's
job per `CONTRIBUTING.md`'s existing review criteria.

**Before trusting it as a required check, ran it locally against the real
repo and found three genuine pre-existing schema violations** (not
synthetic test cases):

1. `skills/vision/image-reviewer/SKILL.md` had no YAML frontmatter at all —
   violates `CONTRIBUTING.md`'s own "at minimum a SKILL.md with YAML
   frontmatter" spec. Added frontmatter matching the pattern used by the
   repo's other skills.
2. Two plugins' `run.sh` (`workspace-git-sync`, `workspace-rsync-sync`)
   were not executable in the git tree (`100644`, confirmed via
   `git ls-files -s` — not a local-checkout artifact) — violates
   `CONTRIBUTING.md`'s own PR checklist item. Fixed with `chmod +x`.
3. `skills/ops/workspace-nfs-mount` was actually a **plugin** (an
   executable `entry: mount.sh` with a `pre_session_start` hook/trigger)
   mis-filed under `skills/` using a non-standard `skill.yaml` instead of
   `SKILL.md`. Moved to `plugins/ops/workspace-nfs-mount/`, renamed
   `skill.yaml` → `manifest.yaml`, renamed its `triggers` key to `hooks`
   (the schema plugins actually use) and added the required `mode: oneshot`
   field it was missing. Added a `plugins/ops/README.md` (the `ops` plugin
   category didn't have one yet) and fixed `CONTRIBUTING.md`'s category
   table, which was missing `vision` (skills) and `sync`/`ops` (plugins) —
   all three already existed on disk, just undocumented.

Pushed, watched the resulting Actions run complete (`success`,
`databaseId: 37139225571`), then confirmed the check's exact context name
via `GET /repos/{owner}/{repo}/commits/{sha}/check-runs` (`"Validate registry
entries"`, the job's `name:`, not its workflow filename) before wiring it in
— added a `required_status_checks` rule for that exact context to the
existing `main-protection` ruleset via `PUT .../rulesets/16600318`, alongside
the ruleset's pre-existing `deletion`/`non_fast_forward`/`pull_request`
rules (replaced the whole `rules` array in the PUT, since the rulesets API
takes the full set, not a diff — verified the existing three rules first so
none were silently dropped).

## 9. Phase 2 implemented, same day — `datawatch-app`

Operator authorized Phase 2 directly ("go ahead and start Phase 2"). Exactly
what §4 specified: flipped `require_code_owner_review` from `false` to
`true` on `datawatch-app`'s `main-protection` ruleset (`id: 16600320`) via
`PUT .../rulesets/16600320`, preserving every other rule/parameter verbatim
(fetched the full current ruleset first, changed only that one field, and
kept the existing `deletion`/`non_fast_forward` rules and the
`required_status_checks` rule — `Verify Version parity` + `Build + test` —
untouched, same "replace needs the full set" caution as §8). Verified with a
follow-up `GET` that the field actually took, and confirmed `CODEOWNERS`
(`* @dmz006` plus path-specific entries for `AGENT.md`, `SECURITY.md`,
`LICENSE`, the security/threat-model docs, and `.github/workflows/`) is
still in place — the file was already correct, it just wasn't being
enforced until this flip. No other changes made, per the plan's own note
not to weaken this repo's already-stricter review settings to "match"
`datawatch`.

`SECURITY.md` was scoped per the plan to "report a malicious or vulnerable
community submission" rather than daemon-level vulnerabilities, with an
explicit note that a passing CI check is schema validation only, not a
safety guarantee. `CODEOWNERS` is `* @dmz006`, matching the plan's minimum.
`required_approving_review_count` stays at `0` as the plan specified — the
operator reviewing every submission manually remains the actual safety
model; this CI check is a second line of defense under that review, not a
replacement for it.

**Not done, correctly out of Phase 1's scope:** Phase 2 (`datawatch-app`'s
`require_code_owner_review` flip), `datawatch`'s own missing `CODEOWNERS`,
and Swift CodeQL coverage for `datawatch-app` (§7).

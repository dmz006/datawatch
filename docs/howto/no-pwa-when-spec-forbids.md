# no-PWA-when-spec-forbids guard (`scripts/check-no-pwa-when-spec-forbids.sh`)

The repository now has a guard for one recurring class of violation: **a
branch that changes the PWA even though its task spec forbade any PWA change.**
That happened on a real Automaton branch (e19b0001, daemon-side priority
stall wake), where `internal/server/web/app.js` was modified although the
spec's own wording forbade a PWA change.
`scripts/check-no-pwa-when-spec-forbids.sh` makes that violation fail fast,
and `tests/test-no-pwa-when-spec-forbids.sh` pins the guard's four behaviours.

This is repository development tooling — a script, a `make test` entry and a
self-test. It has no daemon surface: no setting, and no REST, MCP, CLI, chat
or PWA surface.

## What the check does

`scripts/check-no-pwa-when-spec-forbids.sh <spec-file> [base-ref]`:

1. **Reads the spec and checks it for a PWA prohibition.** The file contents
   are lowercased and matched as substrings against a closed list of five
   phrases:

   - `no pwa change`
   - `no pwa part`
   - `forbade any pwa change`
   - `forbids any pwa change`
   - `must not change the pwa`

   If none of the five phrases occurs, the guard does not apply: it exits `0`
   without even running git. Matching is case-insensitive and is a plain
   substring test, not a parse of the spec's grammar.

2. **When the spec forbids the PWA, diffs the change set against the base
   ref.** The base ref defaults to the merge base of `HEAD` and `main`
   (`git merge-base HEAD main`); an explicit second argument overrides it. The
   diff is `git diff --name-only <base-ref>` from the working tree, so it
   covers both committed changes and uncommitted working-tree changes.

3. **Fails on the exact PWA file.** The guard fails only if
   `internal/server/web/app.js` itself appears in that file list (exact path
   match, one of the diff lines). Any other path — including other PWA files
   such as `style.css`, `index.html` or the locale bundles — does not trip it.

Exit codes:

| Exit | Meaning |
|------|---------|
| `0` | Spec does not forbid a PWA change, or `app.js` is untouched |
| `1` | Spec forbids a PWA change and `internal/server/web/app.js` was changed; prints exactly `forbidden PWA change: internal/server/web/app.js` on stderr |
| `2` | Wrong argument count, spec file missing, the default base ref could not be determined, or `git diff` failed |

The guard is read-only: it reads the spec, runs `git merge-base` and
`git diff`, and prints. It never reverts, restages or edits anything.

## Running it

| How | Command |
|-----|---------|
| Standalone | `bash scripts/check-no-pwa-when-spec-forbids.sh <spec-file> [base-ref]` |
| Self-test | `bash tests/test-no-pwa-when-spec-forbids.sh` — builds a throwaway git repo in an `mktemp -d` directory (removed on exit), commits a base containing `internal/server/web/app.js` and runs the four cases: (1) spec forbids + `app.js` edited → exit 1 with the exact stderr line, (2) spec forbids + only another file changed → exit 0, (3) spec does not forbid + `app.js` edited → exit 0, (4) missing spec file → exit 2. No fixed dates, no daemons. |
| As part of the test target | `make test` — the `test:` target in the `Makefile` runs `bash tests/test-no-pwa-when-spec-forbids.sh` alongside the other `tests/test-*.sh` entries |

## Where it does not run

- It is not part of `make lint`, the local pre-tag run, or the CI workflow:
  no `.github/workflows/` job calls the guard or its self-test. The `make
  test` line only pins the guard's own behaviour; it does not check any
  branch's diff, because the self-test works in a throwaway repo.
- It touches no daemon setting and no REST, MCP, CLI, chat or PWA surface.
  The thing it guards (the PWA) is the one surface it can never change.

## For the operator to review

- **Coverage of the PWA.** The guard only fails on the exact path
  `internal/server/web/app.js`. The rest of the PWA (`style.css`,
  `index.html`, `locales/*.json`, `sw.js`) is out of scope today; extend the
  `grep -qx` match list in the script if the intent is "no PWA file at all".
- **The phrase list is closed.** Only the five phrases above count as a
  prohibition. If specs start expressing the same ban differently (e.g.
  "no changes to the web app"), add the phrase list in the script and a
  matching case in the self-test. Substring matching also means a spec that
  mentions one of the phrases while *denying* the prohibition in the same
  sentence would trip the guard — a guard that errs on the loud side.
- **Nothing enforces it automatically.** The guard is opt-in per branch:
  there is no `make lint` step, pre-tag step or CI step that runs it against
  a real branch's spec and diff. Until it is wired into a decomposition,
  verification or merge-step, the discipline that this guard exists to
  backstop is still carried by humans reading the diff. Operator decision
  needed: where (decompose lint, verifier, merge helper) it should run, and
  whether the `app.js`-only scope is the intended bar.
- **The branch that triggered it still carries the violation.**
  `automaton/e19b0001` still changes `internal/server/web/app.js` (the
  `'priority_stall'` line in the Events lifecycle filter), although its own
  spec says `no PWA change`. The PRD that added this guard did not remove
  that line and does not merge that branch — keeping or removing it is the
  operator's call at merge. Running the guard against that branch's spec
  fails (exit 1) until the line is resolved; that is the guard working, not
  a defect.

## See also

- [operations.md](../operations.md#no-pwa-when-spec-forbids-guard) — the guard and where it runs, in the operations guide.
- [datawatch-definitions.md](../datawatch-definitions.md) — the "no-PWA-when-spec-forbids guard" glossary entry.
- [9c8f0001 final gate](../plans/reports/archive/2026-11/9c8f0001-final-gate.md) — the final-gate report for this guard and the priority-stalls smoke script, with the live gate evidence.
- [local-environment leak check](local-env-leak-check.md) — a sibling repository guard with the same "script + gates, no daemon surface" shape.
- [gofmt format check](gofmt-check.md) — the closest sibling: a `scripts/check-*.sh` guard with a self-test and a Makefile target.

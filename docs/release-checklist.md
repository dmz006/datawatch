# Release Checklist

Referenced from `AGENT.md` § Release vs Patch Discipline. Run through this
in order for every release commit (patch, minor, or major) — it exists
because every item on it was missed at least once in practice before being
added here. If you add a new failure mode to this list, say so in the PR/
commit that adds it, same as any other rule change.

## 1. Before writing code

- [ ] If this is 3+ files or architecturally non-trivial: write the plan
  doc first (`docs/plans/YYYY-MM-DD-<slug>.md`), per the Planning Rules.
  Include the `## Parity surface` table — all 8 channels, each either
  "touched" with specifics or "excluded" with a stated reason.
- [ ] If any part of the design has more than one reasonable approach,
  stop and ask the operator rather than picking one silently. Verify
  claims about "already supported" behavior against the actual code
  before relying on them — a UI option existing is not the same as a
  backend implementing it.

## 2. Implementation

- [ ] Tests for every new code path, not just the happy path. Match the
  existing test file's pattern in that package rather than inventing a
  new one.
- [ ] `go build ./...` clean.
- [ ] `go test ./...` clean — the **full** suite, not just the touched
  package. A change in a shared resolution/cascade function can break
  an unrelated caller.
- [ ] If a test fails in a way that looks unrelated to this change,
  don't assume flake — rerun it in isolation 3-5x locally first. Only
  write it off as a pre-existing flake if it passes consistently
  locally and the failing CI job's duration/pattern matches a known
  timing-sensitive helper (e.g. this repo's `runToTerminal` family).
  Otherwise treat it as a real regression and find the cause.

## 3. Docs (every commit that adds or changes behavior)

- [ ] `CHANGELOG.md` entry under the new version heading.
- [ ] `docs/config-reference.yaml` for any new config fields.
- [ ] `docs/operations.md` if deployment/security/config behavior changed.
- [ ] `README.md`:
  - [ ] **The `**Current release: vX.Y.Z (DATE).**` marquee line at the
    top — every release commit updates this, not just minor/major ones.
    Staleness here is worse than staleness in the backlog.
  - [ ] Documentation index, if new doc files were added.
  - [ ] New interface/command/user-visible feature gets a summary bullet.
- [ ] `docs/testing-tracker.md` for any new interface, backend, or
  **changed endpoint contract** (status code, new/renamed field, renamed
  route) — record the section name in the commit message as
  `tracker: <section name>`.
- [ ] No internal tracker IDs (B#, BL#, F#) leaked into user-facing docs
  (CHANGELOG, README, docs/ outside `docs/plans/`).
- [ ] `docs/plans/README.md` backlog refactor: clear `## Unclassified`
  into BL### entries, mark just-shipped items `✅ Closed in vX.Y.Z` and
  move them to the closed section, confirm the open table only lists
  actually-open work.
- [ ] Plan doc (if one exists for this change): update its Status to
  Done and note the version it shipped in.

## 4. Version + build

- [ ] Bump `var Version = "X.Y.Z"` in **both** `cmd/datawatch/main.go`
  and `internal/server/api.go` — **before** the build, not after. (A
  build run before the bump silently ships a binary whose self-reported
  `/api/info` version lags the actual code by one release — it still
  has the real fix, just reports the wrong number. Confirm the bumped
  string with `grep -n "^var Version" cmd/datawatch/main.go
  internal/server/api.go` right before building, not from memory.)
- [ ] **Never `go build ./cmd/datawatch/` directly for anything that will
  be installed or shipped.** Always go through:
  - `make build` — host-arch binary to `./dist/` (verification builds).
  - `make install` — host-arch binary straight to `~/.local/bin/datawatch`
    (local dev-deploy; this is also what sidesteps the ETXTBSY "text file
    busy" problem a manual `cp` into a running daemon's binary path hits —
    Go's own `-o` write is an atomic rename under the hood, `cp` is not).
  - `make cross` — all 5 platform binaries, minor/major releases only
    (CI's `release.yaml` already does this for the GitHub release
    itself; `make cross` locally is only needed if reproducing that).
  All three depend on `sync-docs docs-index`, which mirrors `docs/` into
  the embedded `internal/server/web/docs/` the PWA docs viewer reads —
  skipping this ships a binary whose in-app docs are stale relative to
  what was just written. (CI's own `release.yaml`/`ci.yaml` already run
  `make sync-docs docs-index` before building, so a GitHub-released
  binary is never affected by this — only ad hoc local builds are.)
- [ ] Binary-build cadence: patch releases (`X.Y.Z`, Z>0) only need the
  host-arch binary; minor (`X.Y.0`) and major (`X.0.0`) need the full
  cross-compiled set with all 5 assets attached to the GitHub release.

## 5. Git

- [ ] Stage files **explicitly by name** — never `git add -A` / `git add
  .`. Check `git status` first for any concurrent, unrelated work
  already sitting in the tree (another live session, another PRD's
  worker output) and leave it alone.
- [ ] Commit message ends with the required attribution lines (see the
  session's own `Co-Authored-By`/`Claude-Session` convention).
- [ ] Push `main`, then tag `vX.Y.Z`, then push the tag. Tag only after
  the commit is confirmed pushed.

## 6. CI — watch it properly, not just the overall conclusion

- [ ] Find the triggered run: `gh run list --branch vX.Y.Z --limit 3`.
- [ ] Watch it to completion (`gh run watch <id> --exit-status`), but
  **do not stop at the overall `conclusion` field** — an unrelated job
  (e.g. a pre-existing container-build dependency conflict) can fail
  while every job relevant to this change is green. Always break down
  by job:
  `gh api /repos/<owner>/<repo>/actions/runs/<id>/jobs --jq '.jobs[] |
  {name, conclusion}'`
- [ ] For any failing job, read its actual log
  (`gh api .../actions/jobs/<job_id>/logs`) before deciding it's
  unrelated. Confirm by checking whether the **same job** failed the
  **same way** on the immediately preceding release's run, with a commit
  that didn't touch anything related — only then is it safe to call it
  pre-existing and move on without fixing it in this release.
- [ ] If a job fails and it genuinely is caused by this change (e.g. a
  lint violation in new code), fix it and **re-tag**: delete the broken
  tag/release (`gh release delete vX.Y.Z --yes`, `git push origin
  :refs/tags/vX.Y.Z`, local `git tag -d`), commit the fix, re-tag, push,
  and re-watch from step 6. This repo's convention is to retag the same
  version rather than bump a new patch number for a same-day lint-only
  respin that never had a working release published.

## 7. Local deploy (if redeploying the dev daemon)

- [ ] Check for in-flight work before restarting:
  `GET /api/capacity` for held leases, and running sessions
  (`GET /api/sessions?state=running`). A daemon restart does not kill a
  tmux-backed session, but it does reset in-memory capacity-ledger
  state and briefly interrupt anything mid-call.
- [ ] If a lease is actively held by autonomous/PRD work, prefer to wait
  for it to clear rather than restart through it — unless the operator
  has explicitly said the interruption cost is acceptable (e.g. "local
  LLM, no cloud cost, just time").
- [ ] Use `make install` (see § 4) — not a manual `cp` — for the binary
  swap, so there's no ETXTBSY race against this daemon's own long-lived
  `datawatch mcp`/`datawatch mcp-search` subprocess helpers (which any
  active session, including the one doing the deploy, keeps running).
- [ ] Stop, then start: `datawatch stop`, confirm the process is
  actually gone (`ps aux | grep "datawatch start"`) before restarting —
  don't assume `stop`'s exit code alone means the old process released
  its file descriptors.
- [ ] After `datawatch start`, give it real time to finish booting
  before concluding it's stuck — a full boot (memory/postgres connect
  attempt, signal-cli, docs index, observer, etc.) can take 30-60s
  depending on network conditions (e.g. a slow-timeout vs. instant
  connection-refused on an unreachable postgres host). Don't restart
  again out of impatience; check `tail` of `daemon.log` for forward
  progress first.
- [ ] Confirm `/api/info` reports the **new** version string (if it
  still shows the old one, the binary swap didn't actually take —
  re-check which binary is actually running, e.g. `lsof` the path or
  `ss -tlnp | grep <port>` to find the real PID, not just `ps aux`).
- [ ] Confirm boot-resume: `daemon.log` should show
  `boot-resume: re-launched executor for prd=<id>` for anything that was
  `running`, and `tmux list-panes -a` should show every session that was
  alive before the restart still alive after.

## 8. Wrap-up

- [ ] Report the final state to the operator: what shipped, in which
  version(s), CI status (with the per-job breakdown if anything looked
  off), and whether the local daemon is running the new code.

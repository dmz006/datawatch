# ci-watch completion guardrail — live verification

**Date:** 2026-10-13
**Version:** v9.0.62
**Scope:** docs/howto/ci-watch-guardrail.md, docs/mcp.md, docs/api-mcp-mapping.md, plus the live verification of the `ci-watch` completion guardrail (sections added by other tasks).

## Template fit

**Conclusion: templates cannot enable ci-watch today without a new mechanism, so none was built; the howto documents pairing an instantiated Automaton with a guardrail profile (or an explicit completion list) instead.** The reason: `Template` (internal/autonomous/models.go) carries only `title`/`description`/`spec`/`type`/`tags`/`vars`. `TemplateStore.Instantiate` + `Manager.InstantiateFromTemplateStore` (`autonomous_template_instantiate`) return only a substituted spec and call `CreatePRD(spec, projectDir, backend, "", effort)`, with no profile or guardrail argument. The PRD-as-template path (`Manager.InstantiateTemplate`, `autonomous_prd_instantiate`) copies spec, title, project dir, backend/model/effort and stories, but not `GuardrailProfile` or `PerPRDGuardrails`. There are no built-in release templates in internal/autonomous. The documented recipe ("Using with templates" in the howto) is therefore two calls: instantiate, then `per_automaton_guardrails_set` with `guardrail_profile` (or `per_prd_guardrails_json`) before the Automaton completes. Or list `ci-watch` in the global `autonomous.per_prd_guardrails`.

A caveat found while checking this, which also affects the profile recipe: `resolveGuardrails` (internal/autonomous/guardrail_registry.go) returns a profile's single `Guardrails` list at **every** level (task, story, prd) when the Automaton has no explicit list for that level. A profile containing `ci-watch`/`rules-review` therefore also sends each task and story to the guardrail judge under those names (one extra LLM review per unit; task/story loops in executor.go don't skip judgment-type completion-only names). `rules-review` in a profile already behaves this way. The howto states it and offers the explicit `per_prd_guardrails` list as the completion-only alternative. It is not changed here and is left for an operator decision.

## Release

**Version:** v9.0.63 · **Commit:** `33864705` (tag `v9.0.63`, pushed 2026-10-14T01:28Z) · **Deployed:** local daemon restarted via `systemctl --user restart datawatch`; running daemon reports 9.0.63.

Release published 2026-10-14T01:40Z with 15 assets (goreleaser binaries, stats tarballs, checksums).

| Workflow | Result | Run |
|---|---|---|
| CI | success | https://github.com/dmz006/datawatch/actions/runs/38495885477 |
| security-scan | success | https://github.com/dmz006/datawatch/actions/runs/38495885478 |
| CodeQL | success | https://github.com/dmz006/datawatch/actions/runs/38495884946 |
| Matrix Integration | success | https://github.com/dmz006/datawatch/actions/runs/38495885474 |
| secret-scan | success | https://github.com/dmz006/datawatch/actions/runs/38495885462 |
| docs-sync | success | https://github.com/dmz006/datawatch/actions/runs/38495885400 |
| Release CI Summary (×4) | success | https://github.com/dmz006/datawatch/actions/runs/38496433905 (latest) |
| Release (tag v9.0.63) | **failure** (2 jobs) | https://github.com/dmz006/datawatch/actions/runs/38495887540 |

Release jobs: go test + lint, gosec baseline-diff, govulncheck, goreleaser, containers/validator, containers/stats-cluster all succeeded. Two failed. Neither is caused by this release's code, and both are **escalated to the operator, not fixed here**:

1. **containers / agent-base: blocking Trivy SARIF scan.** Seven HIGH findings in the agent-base image:
   - openssh-client 1:9.2p1-2+deb12u10: CVE-2026-106553, CVE-2026-106555, CVE-2026-106583 (Debian status `fix_deferred`, no fixed version).
   - golang.org/x/net v0.58.0: CVE-2026-78669 (fixed in 0.60.0).
   - Go stdlib v1.27.1: CVE-2026-78667, CVE-2026-78669, CVE-2026-97031 (fixed in 1.27.2).

   This is the same container-vulnerability class that prompted the ci-watch guardrail. Fixing it is explicitly out of scope for this release. The v9.0.62 Release run (https://github.com/dmz006/datawatch/actions/runs/38484929685) failed on the same agent-base job, so the failure predates this release.
2. **attach stats-cluster tarball: upload step exhausted its retries.** Every attempt from 01:46Z to 02:10Z got `release not found` with API quota at 5000 (not rate-limited). The release itself had been published at 01:40Z, which rules out the step's "read-path lag" assumption. The step's lookup of the release looks broken, not slow. The four `datawatch-stats_9.0.63_*` goreleaser tarballs are attached. Only the `datawatch-stats-cluster-9.0.63-linux-amd64.tar.gz` container-image tarball is missing from the release. In v9.0.62 the same job succeeded within 30s.

## Live probe

- 2026-10-13: ci-watch live probe commit pushed by an automaton.
- 2026-10-13: ci-watch live probe commit pushed by an automaton (v9.0.65 re-run).

## Live verification against a real GitHub Actions run

Run 2026-10-13 22:17–22:45 local (2026-10-14 02:17–02:45Z) by an automated session. Everything below was observed live; nothing here is inferred from code or unit tests.

### 1. Deployed version

`get_version` (MCP) → `Current version: 9.0.63`, matching `var Version = "9.0.63"` in `cmd/datawatch/main.go` at the time (`95fea209`). The binary wasn't stale, so the run went ahead.

### 2. Operator config change

This is a config change made through the API, not a code default and not a hand-edited file. `autonomous_config_get` showed `per_prd_guardrails: ["rules-review"]`. `autonomous_config_set {per_prd_guardrails: ["rules-review","ci-watch"]}` returned `{"status":"ok"}`. Reading it back with `autonomous_config_get` gave `"per_prd_guardrails":["rules-review","ci-watch"]` and `"ci_watch_timeout_seconds":1500`.

### 3–4. Probe Automaton

- **ID:** `9477ebcc` ("ci-watch guardrail live probe commit"). Project dir: this repository. Execution, planning and guardrail backends: `claude-code`.
- **Guardrails:** `per_automaton_guardrails_set {id: 9477ebcc, per_prd_guardrails_json: ["ci-watch"]}` was called before approval. The explicit list wins over the global one, so `rules-review` did not run on this Automaton.
- **Plan:** decomposed into one story (`8227e174`) with one task (`8227e6e5`): append the `## Live probe` line, stage only this file, commit, then `git push origin HEAD:main`. Approved at 22:19:23 and run at 22:19:25.
- **Pushed commit:** `029f8cd784b63aac2c9baaa64720a402b3ed3fa0` ("docs(plans): ci-watch live probe line"), pushed 02:20:16Z. The commit is docs-only. `ci.yaml` ignores `docs/**`, but `docs-sync`, `secret-scan`, `security-scan` and GitHub's dynamic "Push on main" do run on any push to main, so the commit had real runs to watch and no path-filter retry was needed.
- **Final status:** `completed` at 22:36:33. ci-watch waited about 16 minutes, mostly for the `owasp-zap` run to finish.
- **`ci_watch` (excerpt from `autonomous_prd_get`):**

```json
"ci_watch": {
  "status": "pass",
  "sha": "029f8cd784b63aac2c9baaa64720a402b3ed3fa0",
  "repo": "dmz006/datawatch",
  "runs": [
    {"id": 38501546824, "name": "Release CI Summary", "status": "completed", "conclusion": "success", "url": "https://github.com/dmz006/datawatch/actions/runs/38501546824"},
    {"id": 38500632308, "name": "Release CI Summary", "status": "completed", "conclusion": "success", "url": "https://github.com/dmz006/datawatch/actions/runs/38500632308"},
    {"id": 38500590367, "name": "Release CI Summary", "status": "completed", "conclusion": "success", "url": "https://github.com/dmz006/datawatch/actions/runs/38500590367"},
    {"id": 38500590421, "name": "owasp-zap", "status": "completed", "conclusion": "success", "url": "https://github.com/dmz006/datawatch/actions/runs/38500590421"},
    {"id": 38500532890, "name": "Release CI Summary", "status": "completed", "conclusion": "success", "url": "https://github.com/dmz006/datawatch/actions/runs/38500532890"},
    {"id": 38500512943, "name": "Release CI Summary", "status": "completed", "conclusion": "success", "url": "https://github.com/dmz006/datawatch/actions/runs/38500512943"},
    {"id": 38500494958, "name": "docs-sync", "status": "completed", "conclusion": "success", "url": "https://github.com/dmz006/datawatch/actions/runs/38500494958"},
    {"id": 38500495026, "name": "secret-scan", "status": "completed", "conclusion": "success", "url": "https://github.com/dmz006/datawatch/actions/runs/38500495026"},
    {"id": 38500495040, "name": "security-scan", "status": "completed", "conclusion": "success", "url": "https://github.com/dmz006/datawatch/actions/runs/38500495040"},
    {"id": 38500495120, "name": "Push on main", "status": "completed", "conclusion": "success", "url": "https://github.com/dmz006/datawatch/actions/runs/38500495120"}
  ],
  "summary": "all 10 CI run(s) passed",
  "checked_at": "2026-10-13T22:36:33.040844677-04:00"
}
```

- **Decision entry (kind `ci_watch`):** `{"at":"2026-10-13T22:36:33.04-04:00","kind":"ci_watch","verdict_outcome":"pass","actor":"autonomous","note":"status=pass sha=029f8cd7… runs=<the 10 URLs above> — all 10 CI run(s) passed"}`.
- **Checked against GitHub:** `gh run list --commit 029f8cd784b63aac2c9baaa64720a402b3ed3fa0 --json databaseId,name,conclusion,url` returned the same 10 run IDs, all `success`, and no others. The recorded outcome matches reality. The pass is real.

**Observations (recorded only, not acted on):**

- **Runs ci-watch counts that the push didn't start.** Seven of the 10 runs were `workflow_run`-triggered (`owasp-zap` and `Release CI Summary`) and were created 1–12 minutes after the push. GitHub gives `workflow_run` runs the default branch's HEAD SHA, so `owasp-zap` 38500590421 reports `headSha: 029f8cd7…` even though the probe's push did not trigger it. ci-watch correctly treats every run on the SHA as CI for that commit, and in this case that made the wait longer. It also means a failing downstream workflow that happens to land on the Automaton's SHA would be judged against it. Whether that is desired is an operator call. The judge exists to sort out exactly this.
- **Full SHA required.** `gh run list --commit` returns `[]` when given a short SHA. ci-watch uses the full SHA, so this only matters for anyone checking by hand.

### 5. Can an assistant work out ci-watch from the MCP tool descriptions alone?

**Method:** a fresh sub-agent with no source access and an instruction to use no tools. It was given only the live descriptions (from the MCP tool listing) of `autonomous_config_get`, `autonomous_config_set`, `autonomous_prd_get`, `guardrail_library_list` and `per_automaton_guardrails_set`, plus a sample `autonomous_prd_get` response with `status: "blocked"` and `ci_watch.status: "failed_policy"` (Release run failed on a no-fix HIGH CVE). It was asked three things: how to enable ci-watch for one Automaton, how to enable it globally, and what the blocked result means and what to do.

**Round 1 (v9.0.63 descriptions), its answer, condensed:**

1. *One Automaton:* `per_automaton_guardrails_set(id=…, per_prd_guardrails_json="[\"ci-watch\"]")`. The explicit list beats profile and global, so include `rules-review` if you still want it. Check with `autonomous_prd_get`. The timeout is global only.
2. *Globally:* read with `autonomous_config_get`, then `autonomous_config_set(per_prd_guardrails=[<existing>…, "ci-watch"])`, e.g. `["rules-review","ci-watch"]`. Pitfall: the list is probably replaced, not appended ("the text doesn't actually say"), so sending only `["ci-watch"]` could turn rules-review off. Automata with their own list or profile won't pick it up. It is skipped without GitHub or `gh`.
3. *Meaning:* the Automaton pushed; CI passed and Release failed; the judge ruled the failure policy (CVE suppression needs operator sign-off); nothing was auto-fixed. *What to do:* open run 1002, decide whether to suppress with a justification and review date or to upgrade or remove the package, then commit and push. Then: "**None of the tools described here can do that.** There's no tool shown for approving a `failed_policy` result, re-running ci-watch, or clearing `blocked`."
4. *Gaps it listed:* whether arrays replace or merge (global and per-Automaton); whether the override must be set before completion; **how to get out of `blocked`**; how each status maps to the Automaton's status; profile tools; which LLM and which rules file is used; the infra retry budget; which runs count; and that the timeout is global only.

**Round 1 assessment:** enabling ci-watch, per Automaton and globally, was **correct**, including the advice to read first and send the full list. The meaning of `failed_policy` was **correct**. The "what should I do" answer was **incomplete in a way that blocks the operator**: the descriptions never said how to resume a Blocked Automaton. The real path, documented in `docs/howto/ci-watch-guardrail.md`, is commit the decision, then `autonomous_prd_reset_task` with `force: true` on the pushing task, then `autonomous_prd_run`, or `autonomous_prd_cancel`. The answer also flagged two description gaps that `docs/mcp.md` already covers: setters replace rather than merge, and `per_automaton_guardrails_set` replaces the profile **and all three lists**, so a call carrying only `per_prd_guardrails_json` clears any per-task or per-story override and profile. It also couldn't say which statuses block.

**Fix:** v9.0.64 (`de9f79f8`) changed descriptions only, in `internal/mcp/autonomous.go` and `internal/mcp/guardrail_tools.go`:
- `autonomous_prd_get` now says which statuses block and gives the resume path.
- `per_automaton_guardrails_set` now says each call replaces the profile and all three lists, and that completion guardrails must be set before completion.
- `autonomous_config_set.per_prd_guardrails` now says it replaces the list, so read first.

`make build` → `bin/datawatch` v9.0.64. `go test ./internal/mcp/`: 201 passed. Round 2's descriptions were taken from `tools/list` served by the rebuilt binary (`bin/datawatch mcp` over stdio), not copied from source.

**Deployment note:** the user systemd unit runs `ExecStart=$HOME/.local/bin/datawatch`, a separate copy outside this repository, and `make build` writes only `./bin/datawatch`. This session's write scope was limited to this repository, so it did **not** install the binary. Running `systemctl --user restart datawatch` would only have restarted 9.0.63, so the restart was not done either. The running daemon is still 9.0.63 with the old descriptions until an operator runs `make install` (or copies `bin/datawatch`) and restarts. Round 2 therefore exercised the rebuilt binary's own MCP tool listing, not the long-running daemon's.

**Round 2 (v9.0.64 descriptions, same sample, fresh sub-agent), its answer, condensed:**

1. *One Automaton:* read the current profile and lists with `autonomous_prd_get`, then `per_automaton_guardrails_set` with `per_prd_guardrails_json: "[\"ci-watch\"]"` (or `["rules-review","ci-watch"]`), passing the current profile and per-story/per-task lists back. Pitfalls: omitted values are wiped; set it before completion; explicit beats profile beats global; the timeout is daemon-wide only; it is skipped without GitHub or `gh`.
2. *Globally:* `autonomous_config_get`, then `autonomous_config_set` with `per_prd_guardrails: [<existing>…, "ci-watch"]`. The list is replaced, not merged. Per-Automaton settings win. The setter takes an array while the per-Automaton tool takes a JSON string. A short timeout gives `pending` with the Automaton `completed`.
3. *Blocked result:* correctly explained, then the next steps: decide whether to accept the risk or fix and upgrade; commit the decision; `autonomous_prd_reset_task` with `force=true` on `t-rel`; `autonomous_prd_run`. ci-watch then watches the new commit. If finished by hand, leave it blocked or `autonomous_prd_cancel`.
4. *Remaining gaps it listed:* exact parameter names of `reset_task`/`run`/`cancel` (not in the five descriptions it was given; a real client sees them in the full listing); whether `autonomous_prd_get` shows the profile and per-task/per-story lists; whether the decision commit must be pushed; whether a tag task can run twice (the howto covers this: "make sure the release task copes with being run twice"); which rules file and which judge LLM; the infra budget; how to clear a profile.

**Round 2 assessment: correct.** All three questions were answered correctly and actionably, and the round-1 gap (resuming from blocked) is closed. The remaining gaps are minor or are covered by other tools' descriptions and `docs/howto/ci-watch-guardrail.md`. One worth a later look, not done here: the descriptions say "the project's rules file" without naming its default (`AGENT.md`, set by the rules/context file name config). It wasn't changed here, to keep this pass small.

### 6. Howto coverage

`scripts/check-howto-coverage.sh` → `✓ PASS: every howto under docs/howto/ has exec_steps or is LLM-only.` (exit 0). `scripts/check-no-internal-refs.sh` → `✓ PASS: no internal refs in user-facing surfaces.` (exit 0).

## Closing summary

- **Deployed version under test:** 9.0.63. `get_version` matched `cmd/datawatch/main.go`.
- **Config change:** `autonomous.per_prd_guardrails` changed from `["rules-review"]` to `["rules-review","ci-watch"]` via `autonomous_config_set` and confirmed by reading it back. It is an operator config change, not a code default.
- **Probe Automaton `9477ebcc`:** explicit per-Automaton guardrails `["ci-watch"]`, claude-code throughout. It pushed `029f8cd7` to `origin/main`. ci-watch waited about 16 minutes for 10 real runs, recorded `status: "pass"` with every run URL, and wrote a `ci_watch` decision. The Automaton ended **Completed**. `gh run list --commit <sha>` returned the same 10 runs, all success. The probe record is left in place as evidence.
- **Real run URLs:** https://github.com/dmz006/datawatch/actions/runs/38500494958 (docs-sync), https://github.com/dmz006/datawatch/actions/runs/38500495026 (secret-scan), https://github.com/dmz006/datawatch/actions/runs/38500495040 (security-scan), https://github.com/dmz006/datawatch/actions/runs/38500495120 (Push on main), https://github.com/dmz006/datawatch/actions/runs/38500590421 (owasp-zap), plus five Release CI Summary runs listed in the excerpt above.
- **Discoverability:** round 1 enabled ci-watch correctly but could not resume a Blocked Automaton. The descriptions were fixed in v9.0.64 (`de9f79f8`, description-only, built and tested, not tagged). Round 2 answered correctly. **The running daemon still serves the 9.0.63 descriptions** until an operator installs `bin/datawatch` to the systemd unit's binary path and restarts. That path is outside this session's write scope.
- **Template fit:** templates cannot enable ci-watch today without a new mechanism, so none was built. Pair an instantiated Automaton with `per_automaton_guardrails_set` (a guardrail profile or an explicit `per_prd_guardrails_json`) before it completes, or list `ci-watch` in the global `autonomous.per_prd_guardrails`. See the "Template fit" section above for the code-level reasons and the profile-applies-at-every-level caveat.
- **Untouched:** an uncommitted change to `docker/dockerfiles/Dockerfile.agent-base` was present in the working tree during this run. It belongs to someone else's in-progress work, and it was neither staged nor reverted.

### Out of scope

- Fixing the specific container vulnerabilities in agent-base (openssh-client, `golang.org/x/net`, Go stdlib findings) and the stats-cluster tarball attach failure recorded in "Release".
- Non-GitHub CI providers.
- Backfilling ci-watch onto existing Automata.
- Independent or continuous CI polling outside an Automaton's completion.
- A new template system, or making templates carry guardrail settings.
- Changing how `workflow_run`-triggered runs attached to the Automaton's SHA are counted (observation above).
- Changing `per_automaton_guardrails_set`'s replace-all semantics. Only its description was clarified.

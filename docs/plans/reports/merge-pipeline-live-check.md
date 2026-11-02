# Merge pipeline and onboarding report — live check

Date: 2026-11-02 UTC (the run was at 2026-11-01 20:13 local time, UTC-5; the evidence timestamps such as 2026-11-02T01:13Z are UTC, so both name the same moment)

Result: FAILED — see the checks marked FAIL below.

## Environment

- A throwaway daemon built from this tree ran with its own HOME, data directory and config under <scratch>, started with TMUX and TMUX_TMPDIR unset.
- The spec asks for TLS port 18443. Port 18443 was already held by a process that is not a datawatch test daemon (it listens on all interfaces and was still listening when this report was finalised; scripts/check-no-leftover-test-daemons.sh reports none of ours). That process is not this task's to stop, so the deviation is not resolved: a free loopback port was used instead; the production daemon port 8443 was never used. All listeners were loopback only.
- The repository for the checks was a temporary git repository at <scratch>/smokeproj with branch main and two branches (branch-a, branch-b) that each append a different line after the same base line (an additive conflict). It is registered as the project smokeproj and as the worktree base repository of the sandbox daemon.
- The daemon lifecycle used scripts/test-daemon-lib.sh: the daemon pid was recorded and ended by that pid on every exit path.
- Host names and home paths are replaced by placeholders: <scratch>, <repo>, <home>.

## Checks

1. no file, no record -> source default — PASS — HTTP 200, source=default, steps=9, reason='no file .datawatch/merge-pipeline.yaml in the project; no daemon record for the project; using the built-in pipeline'

2. file wins over record; record applies once the file is removed — PASS — record PUT HTTP 200; with both present source=file steps=file-step-one,file-step-two; after removing the file source=record steps=record-step

3. additive conflict under each of the three policies — FAIL — policy=stop_and_ask (config PUT HTTP 200, in effect=stop_and_ask): POST /api/autonomous/merge/run answered HTTP 503 'Automaton merge unavailable', run result='' error=''; branch-b merged=no; list.txt=alpha|beta|; working tree changes=0; expected=stopped -> MISMATCH. policy=mechanical (config PUT HTTP 200, in effect=mechanical): POST /api/autonomous/merge/run answered HTTP 503 'Automaton merge unavailable', run result='' error=''; branch-b merged=no; list.txt=alpha|beta|; working tree changes=0; expected=both -> MISMATCH. policy=mechanical_and_model (config PUT HTTP 200, in effect=stop_and_ask): POST /api/autonomous/merge/run answered HTTP 503 'Automaton merge unavailable', run result='' error=''; branch-b merged=no; list.txt=alpha|beta|; working tree changes=0; expected=stopped -> MISMATCH. 

4. onboarding report: REST, CLI and MCP agree — PASS — REST json HTTP 200; merge_pipeline check status REST=ok CLI=ok; text digests REST=f926ab21fc8b CLI=f926ab21fc8b MCP=f926ab21fc8b (CLI rc=0, text lines REST=18 CLI=17 MCP=17)

5. PWA merge pipeline card renders the source badge — PASS — rendering the live view (source=record) through the app's own card function in a headless script runtime gave: Daemon record (class merge-pipeline-source-record); this is a script-runtime render of the page code, not a visual browser screenshot

## Finding from check 3

POST /api/autonomous/merge/run answers 503 'Automaton merge unavailable' on the live daemon. The route (internal/server/merge_run.go) asserts that the server's autonomous handle implements MergeBaseRepo, MergeQuestionStore and RecordMergeRun. Those methods exist on the autonomous Manager (internal/autonomous/merge_pipeline_run.go), but the daemon hands the server the autonomous API wrapper (autonomous.NewAPI), which does not expose them, so the assertion fails at run time. Whether the route's own tests use a stand-in that does implement them was not checked here. Nothing was changed here; the conflict policies could not be exercised end to end through the daemon. The evidence log ends with the supplementary unit-test result for the policy logic, which is not a live check.

## Closing steps

- Test and daemon commands ran under env -u TMUX -u TMUX_TMPDIR (the daemon was also started with TMUX unset). The leftover-daemon check was run again at the end under the same prefix: scripts/check-no-leftover-test-daemons.sh printed 'OK: no leftover test daemons' and exited 0.
- Check 4 line counts: the three plain-text forms hash to the same digest (f926ab21fc8b), and the text shown in the evidence below is 17 lines. The 18 reported for REST is a count taken from the saved response body, which carries one extra trailing newline; it is a counting difference in the smoke script, not a content difference. The script was not kept, so this explanation was checked against the evidence text only.
- The supplementary unit-test line (ok, internal/mergepipeline, 0.236s) and the route assertion lines are the last lines of the evidence log and are shown in full below; they are not a live check.

## Limits

- The model policy (mechanical_and_model) is run with no model registry wired into the merge route, so it degrades to stop_and_ask; no model proposal was exercised.
- Check 5 renders the PWA card function from app.js in a headless script runtime against the live API body; it does not drive a real browser.
- Check 4 compares the plain-text forms (REST, CLI, MCP) and the merge_pipeline check status between the REST and CLI JSON forms.
- The built-in default pipeline's other steps (tests, version bump, ship) were not run; the merge checks used a record holding only the merge-branches step.

## Evidence (ordered request/response log)

```
GET /api/autonomous/merge-pipeline?project=smokeproj  -> HTTP 200 {"project":"smokeproj","definition":{"steps":[{"name":"check-branch-versions","kind":"custom","action":"check_branch_versions","on_failure":"stop","timeout":120000000000},{"name":"check-internal-refs","kind":"lint","command":"make check-internal-refs","on_failure":"stop","requires_make_target":"check-internal-refs","timeout":600000000000},{"name":"merge-branches","kind":"ship","action":"merge_branches","on_failure":"stop","timeout":300000000000},{"name":"set-release-version","kind":"version_bump","action":"set_release_version","on_failure":"stop","timeout":60000000000},{"name":"insert-changelog-heading","kind":"changelog","action":"insert_changelog_heading","on_failure":"stop","timeout":60000000000},{"name":"commit-release-bump","kind":"ship","action":"commit_release_bump","on_failure":"stop","timeout":60000000000},{"name":"tag-release","kind":"tag","action":"tag_release","on_failure":"stop","timeout":60000000000},{"name":"push-release","kind":"ship","action":"push_release","on_failure":"stop","timeout":300000000000},{"name":"deploy-latest","kind":"deploy","action":"deploy_latest","on_failure":"continue","timeout":600000000000}]},"source":"default","reason":"no file .datawatch/merge-pipeline.yaml in the project; no daemon record for the project; using the built-in pipeline","settings":{"file_name":".datawatch/merge-pipeline.yaml","source_order":["file","record"],"fallback_on_bad_file":"record","version_files":[]},"conflict_policy":"mechanical_and_model","conflict_policy_in_ef
PUT /api/autonomous/merge-pipeline/record?project=smokeproj {"project":"smokeproj","yaml":"steps:\n  - name: record-step\n    kind: custom\n    command: \"true\"\n"} -> HTTP 200 {"project":"smokeproj","definition":{"steps":[{"name":"record-step","kind":"custom","command":"true"}]},"updated_at":"2026-11-02T01:13:19.880639698Z"}
GET /api/autonomous/merge-pipeline?project=smokeproj  -> HTTP 200 {"project":"smokeproj","definition":{"steps":[{"name":"file-step-one","kind":"custom","command":"true"},{"name":"file-step-two","kind":"custom","command":"true"}]},"source":"file","reason":"the project's own file .datawatch/merge-pipeline.yaml is present and valid","settings":{"file_name":".datawatch/merge-pipeline.yaml","source_order":["file","record"],"fallback_on_bad_file":"record","version_files":[]},"conflict_policy":"mechanical_and_model","conflict_policy_in_effect":"stop_and_ask","conflict_model":"claude-opus-5-5","conflict_model_in_effect":"","conflict_degraded_reason":"the conflict model \"claude-opus-5-5\" is not available, so conflicts stop and ask","overrides":null}
GET /api/autonomous/merge-pipeline?project=smokeproj  -> HTTP 200 {"project":"smokeproj","definition":{"steps":[{"name":"record-step","kind":"custom","command":"true"}]},"source":"record","reason":"no file .datawatch/merge-pipeline.yaml in the project; the daemon's record for the project is in effect","settings":{"file_name":".datawatch/merge-pipeline.yaml","source_order":["file","record"],"fallback_on_bad_file":"record","version_files":[]},"conflict_policy":"mechanical_and_model","conflict_policy_in_effect":"stop_and_ask","conflict_model":"claude-opus-5-5","conflict_model_in_effect":"","conflict_degraded_reason":"the conflict model \"claude-opus-5-5\" is not available, so conflicts stop and ask","overrides":null,"record_updated_at":"2026-11-02T01:13:19Z"}
PUT /api/autonomous/merge-pipeline/record?project=smokeproj {"project":"smokeproj","yaml":"steps:\n  - name: merge-branches\n    kind: ship\n    action: merge_branches\n"} -> HTTP 200 {"project":"smokeproj","definition":{"steps":[{"name":"merge-branches","kind":"ship","action":"merge_branches"}]},"updated_at":"2026-11-02T01:13:19.922441686Z"}
PUT /api/config {"autonomous.merge_pipeline.conflict_policy":"stop_and_ask"} -> HTTP 200 {"status":"ok"}
GET /api/autonomous/merge-pipeline?project=smokeproj  -> HTTP 200 {"project":"smokeproj","definition":{"steps":[{"name":"merge-branches","kind":"ship","action":"merge_branches"}]},"source":"record","reason":"no file .datawatch/merge-pipeline.yaml in the project; the daemon's record for the project is in effect","settings":{"file_name":".datawatch/merge-pipeline.yaml","source_order":["file","record"],"fallback_on_bad_file":"record","version_files":[]},"conflict_policy":"stop_and_ask","conflict_policy_in_effect":"stop_and_ask","conflict_model":"claude-opus-5-5","conflict_model_in_effect":"","overrides":null,"record_updated_at":"2026-11-02T01:13:19Z"}
POST /api/autonomous/merge/run {"project":"smokeproj","title":"smoke","branches":["branch-a","branch-b"]} -> HTTP 503 Automaton merge unavailable
PUT /api/config {"autonomous.merge_pipeline.conflict_policy":"mechanical"} -> HTTP 200 {"status":"ok"}
GET /api/autonomous/merge-pipeline?project=smokeproj  -> HTTP 200 {"project":"smokeproj","definition":{"steps":[{"name":"merge-branches","kind":"ship","action":"merge_branches"}]},"source":"record","reason":"no file .datawatch/merge-pipeline.yaml in the project; the daemon's record for the project is in effect","settings":{"file_name":".datawatch/merge-pipeline.yaml","source_order":["file","record"],"fallback_on_bad_file":"record","version_files":[]},"conflict_policy":"mechanical","conflict_policy_in_effect":"mechanical","conflict_model":"claude-opus-5-5","conflict_model_in_effect":"","overrides":null,"record_updated_at":"2026-11-02T01:13:19Z"}
POST /api/autonomous/merge/run {"project":"smokeproj","title":"smoke","branches":["branch-a","branch-b"]} -> HTTP 503 Automaton merge unavailable
PUT /api/config {"autonomous.merge_pipeline.conflict_policy":"mechanical_and_model"} -> HTTP 200 {"status":"ok"}
GET /api/autonomous/merge-pipeline?project=smokeproj  -> HTTP 200 {"project":"smokeproj","definition":{"steps":[{"name":"merge-branches","kind":"ship","action":"merge_branches"}]},"source":"record","reason":"no file .datawatch/merge-pipeline.yaml in the project; the daemon's record for the project is in effect","settings":{"file_name":".datawatch/merge-pipeline.yaml","source_order":["file","record"],"fallback_on_bad_file":"record","version_files":[]},"conflict_policy":"mechanical_and_model","conflict_policy_in_effect":"stop_and_ask","conflict_model":"claude-opus-5-5","conflict_model_in_effect":"","conflict_degraded_reason":"the conflict model \"claude-opus-5-5\" is not available, so conflicts stop and ask","overrides":null,"record_updated_at":"2026-11-02T01:13:19Z"}
POST /api/autonomous/merge/run {"project":"smokeproj","title":"smoke","branches":["branch-a","branch-b"]} -> HTTP 503 Automaton merge unavailable
PUT /api/config {"autonomous.merge_pipeline.conflict_policy":"stop_and_ask"} -> HTTP 200 {"status":"ok"}
supplementary unit test: ok  	github.com/dmz006/datawatch/internal/mergepipeline	0.236s
route assertion / manager method / mentions in autonomous.API: 116:	mr, ok := s.autonomousMgr.(mergeRunner)
202:func (m *Manager) MergeBaseRepo() string { return m.Config().WorktreeBaseRepo }
0
GET /api/automation/onboarding-report?format=json  -> HTTP 200 {"enabled":true,"configured":{"backends":["claude-code","goose","ollama","opencode","openwebui"],"models":["qwen3:1.7b"],"projects":[{"name":"smokeproj","dir":"<scratch>/smokeproj"}],"channels":["ntfy","webhook"],"memory":true,"merge_pipeline":[{"project":"smokeproj","source":"record"}]},"checks":[{"id":"backends","title":"An LLM backend is available","status":"ok","reason":"5 backend(s) are enabled."},{"id":"models","title":"A model is available to run","status":"ok","reason":"1 model(s) can be chosen."},{"id":"projects","title":"A project directory is registered","status":"ok","reason":"1 project(s) are registered."},{"id":"channels","title":"A comm channel is enabled","status":"ok","reason":"2 channel(s) are enabled."},{"id":"memory","title":"Memory is enabled","status":"ok","reason":"Memory is on."},{"id":"merge_pipeline","title":"Projects use their own merge pipeline","status":"ok","reason":"Every project has its own merge pipeline."}],"next_steps":[]}
GET /api/automation/onboarding-report?format=text  -> HTTP 200 What is configured
  Backends: claude-code, goose, ollama, opencode, openwebui
  Models: qwen3:1.7b
  Projects:
    smokeproj (<scratch>/smokeproj)
  Comm channels: ntfy, webhook
  Memory: on
  Merge pipeline:
    smokeproj: record

What is missing
  [ok] An LLM backend is available: 5 backend(s) are enabled. (backends)
  [ok] A model is available to run: 1 model(s) can be chosen. (models)
  [ok] A project directory is registered: 1 project(s) are registered. (projects)
  [ok] A comm channel is enabled: 2 channel(s) are enabled. (channels)
  [ok] Memory is enabled: Memory is on. (memory)
  [ok] Projects use their own merge pipeline: Every project has its own merge pipeline. (merge_pipeline)
CLI rc=0 cli.err=2026/11/01 20:13:20 mcp/stdio: listener daemon started
2026/11/01 20:13:20 mcp/stdio: listener daemon stopped — stdin closed/EOF (client disconnected or shutting down)
MCP raw={"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"prompts":{},"resources":{},"sampling":{},"tools":{"listChanged":true}},"serverInfo":{"name":"datawatch","version":"1.0.0"}}}
{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"What is configured\n  Backends: claude-code, goose, ollama, opencode, openwebui\n  Models: qwen3:1.7b\n  Projects:\n    smokeproj (<scratch>/smokeproj)\n  Comm channels: ntfy, webhook\n  Memory: on\n  Merge pipeline:\n    smokeproj: record\n\nWhat is missing\n  [ok] An LLM backend is available: 5 backend(s) are enabled. (backends)\n  [ok] A model is available to run: 1 model(s) can be chosen. (models)\n  [ok] A project directory is registered: 1 project(s) are registered. (projects)\n  [ok] A comm channel is enabled: 2 channel(s) are enabled. (channels)\n  [ok] Memory is enabled: Memory is on. (memory)\n  [ok] Projects use their own merge pipeline: Every project has its own merge pipeline. (merge_pipeline)\n"}]}}
GET /api/autonomous/merge-pipeline?project=smokeproj  -> HTTP 200 {"project":"smokeproj","definition":{"steps":[{"name":"merge-branches","kind":"ship","action":"merge_branches"}]},"source":"record","reason":"no file .datawatch/merge-pipeline.yaml in the project; the daemon's record for the project is in effect","settings":{"file_name":".datawatch/merge-pipeline.yaml","source_order":["file","record"],"fallback_on_bad_file":"record","version_files":[]},"conflict_policy":"stop_and_ask","conflict_policy_in_effect":"stop_and_ask","conflict_model":"claude-opus-5-5","conflict_model_in_effect":"","overrides":null,"record_updated_at":"2026-11-02T01:13:19Z"}
```

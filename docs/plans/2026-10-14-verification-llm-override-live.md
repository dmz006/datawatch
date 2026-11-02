# Verification LLM override — live end-to-end proof (v9.0.73)

Date: 2026-10-14. Feature: per-task, per-story and per-Automaton
verification LLM override (v9.0.67–v9.0.73, consolidated in the
v9.0.73 CHANGELOG entry).

## Result

**Pass.** On a real daemon run, the overridden task was judged by the
override backend and recorded `verifier_source: "task"`. Its sibling
task, which had no override, was judged by the fallback and recorded
`verifier_source: "global"`.

```
  PASS  [TS-796] task 03126167 judged by override ollama-localhost (verifier_source=task); sibling 042ab911 verifier_source global/pool (v9.0.72)
RESULT=pass
```

## Deployed version

- Production: **`datawatch v9.0.73`**. It was built with `make install`
  and restarted with `systemctl --user restart datawatch` (not
  `datawatch restart`). `datawatch version` → `datawatch v9.0.73`; the
  running daemon's `GET /api/health` → `"version":"9.0.73"`.
- History: production was on v9.0.65, was first deployed at v9.0.72,
  then went to v9.0.73. CI's version-bump check (against `HEAD^`)
  requires the release-bookkeeping commit to carry its own patch number.
  v9.0.73 differs from v9.0.72 only in the version string, the
  CHANGELOG/README and this story, with no Go code change. So the
  sandbox run below, done on the v9.0.72 build, exercises the same
  code.

## Sandbox

- A separate sandbox daemon built from the same `bin/datawatch`
  (v9.0.72), listening on `https://127.0.0.1:18443` with the lib.sh test
  token. Its config was generated from `testdata/datawatch.yaml` the
  same way `scripts/run-tests.sh`'s `start_test_daemon` does it. Its
  data dir, tmux socket dir and `TMPDIR` were under the gitignored
  `.datawatch/sandbox-vllm/` in the repo checkout, and it ran with
  `TMUX` unset. The production daemon on 8443 was not used for the
  test.
- Sandbox global autonomous config: `verification_backend` empty,
  `verification_backends` unset. Execution backend of the Automaton:
  `claude-code` (effort `low`).
- Override chosen: `ollama-localhost` / `qwen3:1.7b`, the testdata
  registry entry. It differs from both the global verifier (empty) and
  the execution backend (`claude-code`). testdata's `llms:` block isn't
  loaded into the inference registry, so the story registered it
  inline: a compute node at `http://localhost:11434` plus the
  `ollama-localhost` LLM entry. It confirmed `/api/ask` answered
  through it (`{"backend":"ollama","model":"qwen3:1.7b","answer":"OK","duration_ms":1362}`)
  and removed both afterwards.

## TS-796 run

The story was run by sourcing `scripts/test-stories/lib.sh` and
`scripts/test-stories/TS-796.sh` directly against the sandbox
(`TEST_BASE=https://127.0.0.1:18443`). `run-tests.sh` always creates its
working directory beside the repo checkout, which was outside this
task's allowed write scope. The story body is identical either way.

- Automaton: `e0ef099c`. It ran in a fresh temp git repo containing
  `README.md`, and decompose ran on ollama from a file-free placeholder
  spec to reach needs_review. The planned story was then replaced by
  story `00b3eb15` with two tasks:
  - `03126167` "hello": "Append the line hello to NOTES.md (create the
    file if missing). Do nothing else." — **override set**
  - `042ab911` "world": "Append the line world to WORLD.md (create the
    file if missing). Do nothing else." — no override
- The exact override request (HTTP 200):

```
POST /api/autonomous/prds/e0ef099c/set_task_verification_llm
{"task_id":"03126167","verification_backend":"ollama-localhost","verification_model":"qwen3:1.7b"}
```

- After approve + run, the Automaton reached `completed` and both tasks
  reached `completed`, in about 1.5 min. The run started at about
  02:07:45 local time; the verify timestamps are below.
- `verification` of task `03126167`, verbatim from
  `GET /api/autonomous/prds/e0ef099c`:

```json
{
  "ok": true,
  "severity": "info",
  "summary": "The diff correctly appends 'hello' to NOTES.md as specified.",
  "verified_at": "2026-10-14T02:08:42.02447006-04:00",
  "verifier_backend": "ollama-localhost",
  "verifier_model": "qwen3:1.7b",
  "verifier_source": "task"
}
```

- `verification` of task `042ab911`, verbatim:

```json
{
  "ok": true,
  "severity": "info",
  "summary": "WORLD.md was created containing exactly the line \"world\" (6 bytes, newline-terminated); no tracked files modified.",
  "verified_at": "2026-10-14T02:09:40.552880285-04:00",
  "verifier_backend": "claude-code",
  "verifier_source": "global"
}
```

  With no override and an empty global `verification_backend`, the
  verifier fell back to the Automaton's own backend (`claude-code`,
  session-routed because the Automaton has a project directory).
  `verifier_model` is omitted because no model was configured at any
  level.

## Findings from getting here (no code changed)

- Decompose on an Automaton whose plan names files is rejected unless
  the plan carries at least one `verification_script`. qwen3:1.7b
  didn't produce one for the "append hello to NOTES.md" spec. The story
  therefore decomposes a file-free placeholder spec and then replaces
  the stories. This is an existing guard and works as designed.
- An ollama execution backend has no file tools. A task that changes
  no files is rejected by the "no changes detected" check before the
  LLM-judgment stage, and that result carries no `verifier_*` fields
  because nothing was judged. The story therefore executes on
  claude-code.
- An earlier manual probe of the same flow (Automaton `26df0420`, tasks
  `da94e5d9` override / `daf0c5ce` sibling) gave the same result:
  `ollama-localhost` / `task` and `claude-code` / `global`.

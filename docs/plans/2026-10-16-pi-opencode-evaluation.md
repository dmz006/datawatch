# BL466 — Evaluate switching from opencode to pi for open-model PRD execution

**Status:** needs_review · **Date:** 2026-10-16 · **Tracker:** BL466
**Scope:** v11.0.0 — not required for v10.0.0, no code changes here.

## Why this is filed

Tonight's PRD execution on opencode (the `opencode-acp` backend driving
`qwen3.8:27b` via Ollama) hit two real, confirmed problems: a health-check
bug and a completion-detection bug, both now fixed and shipped (v9.1.19,
v9.1.20) — but a third, still-unfixed bug in opencode's own code
(`AI_APICallError: no user query found in messages`, see
`project_opencode_no_user_query_bug.md`) repeatedly stalled one PRD's
execution, surviving a full re-decompose. Opencode can complete individual
tasks reliably now, but cannot be trusted to finish a PRD unattended.

A same-night research pass (live-tested against our real `qwen3.8:27b` on
`johnnyjohnny`, not a smaller model) found two real alternative harnesses:

- **pi** (`earendil-works/pi`, MIT, 115K stars): minimal harness, calls the
  official OpenAI/Anthropic/Google SDKs directly rather than the
  `@ai-sdk/openai-compatible` layer where opencode's bug lives. Two live
  tests (a 3-tool-call task and a heavier 4-tool-call edit+write+test+commit
  task) both completed cleanly with real correct commits. Has a real
  `--print --mode json` headless mode with a flat JSON-lines event stream
  (tool calls, thinking, cost) — clearer to parse than opencode's SSE.
- **Oh My Pi / omp** (`can1357/oh-my-pi`, MIT, 35K stars, requires Bun): a
  more complete harness (LSP, subagents, plan mode) built on top of the
  same space. One live test completed cleanly. No documented flag to make
  the CLI process exit on its own after its JSON stream reports
  `isTerminal: true` — any integration must watch that event directly and
  kill the process on our own timeout, not trust process exit.

**Neither is bug-free.** Real research (not assumption) found an open pi
issue, #9216, describing Ollama + `qwen3.8:27b` stream-termination errors
under heavy tool use — our exact model, a known 0.84→0.85 regression, a
documented downgrade workaround. A second open pi issue describes a
different trigger in the same structural bug family as opencode's crash
(an aborted turn leaving unmatched tool-call state that the next turn gets
rejected over).

## What this item covers

1. **Decide** whether switching is worth it, based on a real pilot, not
   just this one night's smoke test — run pi against a currently-parked
   PRD task as a direct A/B against opencode on the same model/endpoint.
2. **Get pi running** as an integrated datawatch backend — an
   `acpbackend.go`-equivalent: session lifecycle, event-stream parsing,
   completion detection keyed off pi's own JSON terminal event (not
   process exit — datawatch's own retry/stall machinery, like
   `stale_task_seconds`, should still apply as a safety net regardless of
   which harness is driving).
3. **Test what we actually need**: reliability across many tasks and a
   full PRD (not just 1-2 smoke tasks), tool-call visibility surfaced into
   datawatch's own session view, and whether pi's own open issues (#9216
   and the aborted-turn one) are hit in real multi-task PRD runs the way
   opencode's bug was.
4. **Get PRD execution reliably working again** — the actual goal. If pi
   holds up across a real pilot, make it the default open-model execution
   backend; if not, the opencode fixes already shipped plus whatever's
   still open (`project_opencode_no_user_query_bug.md`'s 281-stale-session
   lead) remain the path forward instead.

`omp` is a second candidate if pi doesn't pan out — more capable
out of the box, but needs its own ACP-protocol integration work (the
`omp acp` stdio server mode, not yet tested) rather than the simpler
`--print` CLI mode validated tonight.

## Not yet done

No code. No backend integration. No full-PRD pilot run. This is the
research base for a future implementation PRD, not the PRD itself.

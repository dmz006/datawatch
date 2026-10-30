# BL466 — Evaluate switching from opencode to pi for open-model PRD execution

**Status:** active, backend built and shipped (v9.1.21) · **Date:** 2026-10-16 · **Tracker:** BL466
**Scope:** moved from "v11.0.0 research" to **active now** — a hard operator
constraint ("no PRD task execution via claude-code until Sunday") made local-
model execution via opencode or pi the only path to real PRD throughput, so
getting pi running stopped being deferrable research and became tonight's
priority. The deeper opencode reliability work (the still-open
"no user query" bug, the 281-stale-session `opencode.db` lead) is what
moved to v11.0.0 instead — see the corrected framing below.

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
`local-node`, not a smaller model) found two real alternative harnesses:

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

## What got built tonight (v9.1.21)

Per the priority correction above, item 2 ("get pi running as an integrated
datawatch backend") was done tonight, not deferred. Built as the simplest
viable shape instead of the `acpbackend.go`-equivalent originally sketched —
see "Architecture decision" below for why:

- `internal/llm/backends/pi/` — two backends: `pi` (interactive TUI,
  resumable via pi's own `--session-id`) and `pi-prompt` (one-shot, blocking
  `pi --print`). 24 unit tests.
- Full `Config`/`inference.Kind` wiring, REST `GET`/`PUT /api/config`
  round-trip, comm channel (no new code needed — generic config-patch path),
  CLI setup wizard (`datawatch setup llm pi` / `pi-prompt`), PWA (picker,
  config form, install hints, docs links).
- Docs: `docs/llm-backends.md#pi`, `docs/config-reference.yaml`,
  `docs/backends.md`, `docs/testing-tracker.md`, `README.md`, `CHANGELOG.md`.

### Architecture decision: blocking one-shot / plain TUI, not an ACP-equivalent

The original sketch (item 2 above) called for an `acpbackend.go`-equivalent:
async event-stream parsing, completion detection keyed off pi's own JSON
terminal event. That's exactly the shape that produced tonight's two real
opencode-acp bugs (a health-check race, v9.1.19; a completion-detection
race, v9.1.20) — both async-signal races. Building the same shape for pi
would start the new backend with the identical risk class already paid down
twice tonight. Built instead: `pi-prompt` blocks on a single CLI call with
unconditional shell-level completion-echo (no async signal to race against,
same shape as `aider`'s backend), and `pi` runs pi's plain interactive TUI
in tmux (same shape as `claude-code`/`goose` interactive sessions, whose
completion detection is datawatch's existing generic pane-scan, not a
backend-specific mechanism). A `pi --mode rpc` variant analogous to
opencode-acp remains possible later (see "Not yet done" below) once there's
a reason to accept that risk class again — not needed to get PRD execution
working tonight.

### Parity surface (AGENT.md plan requirement)

| Surface | Status |
|---|---|
| YAML (`pi`, `pi_prompt`) | Done — `internal/config/config.go`, `docs/config-reference.yaml` |
| REST (`GET`/`PUT /api/config`) | Done — `internal/server/api.go` |
| MCP | No new tool needed — generic `llm_list`/`llm_get`/`backends_list`/`get_config` MCP tools already reflect any registered backend dynamically, same pattern as aider/goose (no backend-specific MCP tool exists for those either) |
| Comm channel (`configure pi.binary=...`) | Done — no new code; routes through the existing `/api/config` PUT path (verified: `router.go`'s `configureFn` is a thin HTTP client over that same endpoint) |
| CLI | Done — `datawatch setup llm pi` / `pi-prompt` wizard steps (`internal/wizard/defs.go`); generic `datawatch config set`/`session new --llm pi` already work via the registry, no backend-specific CLI code needed |
| PWA | Done — backend icon, filter badge, New-LLM dropdown, config form (binary+model+console+git fields), docs link, SaaS-kind set (no compute-node picker, matches aider/goose), install hint |
| Android / iPhone | No client-code change needed — both apps consume the generic backend-picker/config surfaces (REST `GET /api/config`, session-start backend selection) that already enumerate whatever backends the daemon has registered; neither app hardcodes a backend list. Reasoned exclusion, not an oversight — same as every other backend added without a native-app PR. |
| Localization | No new user-facing string added — `pi`/`pi-prompt` are backend identifiers (like `aider`, `goose`), not translated UI copy; PWA labels reuse existing generic field labels ("Binary path", "Model", etc.) already covered by the existing locale keys. |

## Not yet done

No real pilot run yet: no full multi-task PRD executed end-to-end through
the new `pi`/`pi-prompt` backend inside datawatch's own session manager (the
live tool-use tests above were pi's own CLI run directly/standalone, not
through datawatch). No A/B against opencode on the same parked PRD task. No
RPC-protocol (`pi --mode rpc`) variant — see "Architecture decision" above
for why that's deliberate, not an oversight. The 281-stale-session
`opencode.db` lead and opencode's own "no user query" bug remain unfixed —
moved to the v11.0.0 opencode-reliability track per the priority correction
above.

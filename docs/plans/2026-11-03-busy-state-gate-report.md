# Busy-state gate audit: every backend family checked for the silent-pane "waiting for input" mistake

**Date:** 2026-11-03
**Status:** Audit report. No code was changed by this task; classification only.
**Scope:** the same class of mistake found in `pi-prompt` — a session that is
still working gets flipped to `WaitingInput` by the gap watcher because the
pane is silent mid-turn. The gap watcher (15 s window over
`LastChannelEventAt`, `internal/session/state_engine.go`) is the only generic
gate; the busy probe (`internal/session/busy_probe.go`) deliberately scopes to
the `claude-code` family, and the new pure classifier
(`internal/session/backend_liveness.go`, rules a–f) is the intended fix for
print-mode and interactive families alike.

Reference vocabulary (from `internal/session/backend_liveness.go`):

- **print-mode** — one-turn, non-interactive launches that are pane-silent (or
  pane-quiet) while the turn is in flight; classified alive by activity window
  (rule a: within window → running; rule b: beyond window → stalled) instead of
  by a prompt in the pane.
- **interactive with a real prompt** — the pane shows the backend's own input
  prompt when idle (rule d → waiting_input; rule e → running when active;
  rule f → no-opinion, existing gap watcher keeps its behaviour unless the
  caller opts in to stalling others).
- **busy probe already used** — the family is `claude-code`; not one of the
  nine audited here.
- **not applicable** — the family already reports idle structurally, so the
  gap watcher is a fallback guard band, not the working-state signal.

Family → adapter locations (repo-relative): `internal/llm/backends/pi/`,
`internal/llm/backends/opencode/`, `internal/llm/backends/goose/`,
`internal/llm/backends/gemini/`, `internal/llm/backends/aider/`,
`internal/llm/backends/ollama/`, `internal/llm/backends/openwebui/`.
Prompt-vs-interactive split rule: `internal/session/backend_family.go`.

## Audit

| Family | How working state is observed | Classification | Rule that applies |
|---|---|---|---|
| `pi-prompt` | One-shot `pi --print --mode text … \| tee <output scratch>` in the pane; the turn is pane-quiet and its liveness is carried by the output-file mtime / exit status and the terminal outcome line. No input prompt appears while running; the pane returns to the shell only after the turn completes (`internal/llm/backends/pi/backend.go`). The flip is **wrong** for it: this is the reported class of bug. | Needs the liveness rule as print-mode | (a)/(b) print-mode rules in `internal/session/backend_liveness.go`; `IsPromptBackend` is true (`internal/session/backend_family.go`) |
| `pi` | Interactive TUI (`pi --approve --model …`, task as first turn); the TUI streams turns into the pane and shows its own input prompt when idle, so an idle flip is there only when the prompt is visible (`internal/llm/backends/pi/backend.go`). Long quiet mid-turn pauses can still mis-flip, but idle ≠ silent-for-us. | Interactive with a real prompt | (d)/(e), with (f) as the guard band |
| `opencode` | Interactive TUI; the TUI runs on the alternate screen, which hides plain-pane output, so mid-turn frame updates are the main pane signal and the idle input prompt ("Ask anything" footer, recognized in `internal/session/manager.go`) marks a true idle. Completion is detected via terminal shell-prompt + outcome markers (`internal/session/manager.go`). | Interactive with a real prompt | (d)/(e), with (f) as the guard band |
| `opencode-acp` | `opencode serve` + REST/SSE; the pane only hosts server logs. Working state is observed structurally: every SSE event maps to a channel event (`OnACPEvent` in `internal/llm/backends/opencode/acpbackend.go` → `MarkChannelEvent` in `internal/session/state_engine.go`), and the backend reports idle itself (`[opencode-acp] awaiting input` / `ready` markers, recognized in `internal/session/manager.go`). The gap watcher already gets a dedicated 5-minute fallback window (`acpSilenceGap`, `internal/session/state_engine.go`) purely to guard a dropped event stream. The flip is **not wrong** for this family. | Not applicable | (f) no-opinion: existing gap watcher unchanged as guard band only |
| `goose` | Interactive TUI (`goose session`, forced plain theme); the TUI streams turns and shows goose's input prompt (or an approval dialog) when idle (`internal/llm/backends/goose/backend.go`). The one-shot cousin `goose run --text` is the print-mode variant of the same family, but the audited row is interactive. | Interactive with a real prompt | (d)/(e), with (f) as the guard band |
| `gemini` | Go-mediated one-shot: `gemini -p --output-format json` runs as a blocking child of the daemon; the pane prints only the task echo, and the response text is written to the pane after the turn returns (`printLines`). The pane is silent for the whole turn, so the flip is **wrong** for it: same mistake class as `pi-prompt`. | Needs the liveness rule as print-mode | (a)/(b); liveness must come from process-alive + activity (rule (c) for the dead case) |
| `aider` | One-shot `aider --yes --message '<task>'`; aider streams its chat/diff output to the pane once it starts, and reports the outcome line after the blocking call returns (`internal/llm/backends/aider/backend.go`). Before the first token there is a silent stretch with no input prompt in the pane (only the shell), so the flip is **wrong** for that window. | Needs the liveness rule as print-mode | (a)/(b) once the print-mode input bit is set for one-shot launches (`PromptRequired`, `SupportsInteractiveInput=false`) |
| `ollama` | Default mode is the interactive REPL: `ollama run <model>`, which shows its own `>>>` input prompt when ready (recognized in `internal/session/manager.go`) and streams model replies into the pane; the chat-mode variant streams API chunks into the pane from Go (`internal/llm/backends/ollama/backend.go`, `internal/llm/backends/ollama/conversation.go`). Idle = real `>>>` prompt. | Interactive with a real prompt | (d)/(e), with (f) as the guard band |
| `openwebui` | Go-mediated conversations over the chat-completions API: each turn's SSE chunks are written into the pane as they arrive, and input is routed through the Go conversation manager rather than typed into the pane (`internal/llm/backends/openwebui/backend.go`, `internal/llm/backends/openwebui/conversation.go`). No family-owned input prompt ever appears in the pane, so a pre-first-token silence mis-flips — the working state has to come from the streaming activity itself. | Needs the liveness rule as print-mode | (a)/(b) with activity = chunk stream; (c) covers the dead case |

## Counts

Counts: 9 4 4 1

(orders: families audited = 9; families needing the print-mode rule = 4
— `pi-prompt`, `gemini`, `aider`, `openwebui`; families interactive with a
real prompt = 4 — `pi`, `opencode`, `goose`, `ollama`; families not
applicable = 1 — `opencode-acp`; busy-probe-already-used = 0, that gate
belongs to `claude-code` and is not one of the audited families.)

## Liveness config surfaces gate

- Packages: 5 passed, 0 failed
- Go tests: 6446 passed, 0 failed
- Node tests: 1982 passed, 0 failed
- Checks (docs-sync, no-internal-refs, TS-844.sh syntax, TS-844.mjs syntax, build): 5 passed, 0 failed
- YAML: round-trip test present yes
- REST: round-trip test present yes
- MCP: round-trip test present yes
- CLI: round-trip test present yes
- comm: round-trip test present yes
- web: round-trip test present no

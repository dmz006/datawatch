# Task

Wire the liveness rule (`internal/session/backend_liveness.go`, `ClassifyLiveness`) into the busy probe (`internal/session/busy_probe.go`) and the gap watcher (`internal/session/state_engine.go`, `runChannelStateWatcherTick`) so they govern **all** backend families, not just claude-code. 1. `applyBusyProbe` and the gap tick must call `ClassifyLiveness` first for every backend; print-mode backends use process-alive + transcript/output mtime; alive+writing stays Running with `LastChannelEventAt` refreshed. 2. A session whose process is gone while the record is open → `StateStalled` (never `WaitingInput`). 3. Idle timeout resolved per-backend via config accessors (`LivenessInputIdleTimeoutEffective`); print-mode backends default to **off** (0); explicit overrides still apply. 4. Claude-code behaviour unchanged; existing tests must pass. 5. Extend `busy_probe_test.go`; add `state_engine_liveness_test.go` with `TestStateEngineLiveness_*` covering pi-prompt and ≥1 interactive backend across: alive+writing, alive+silent, dead, at real prompt. 6. Use `NewFakeTmux` + `paneChildFn` fake; no real tmux, no daemon, no version bump, no `gh`. 7. Checkpoint protocol every 5–10 min; finish with `DATAWATCH_COMPLETE: <summary>`.

# Context

- Branch `task/2026-11-01-wire-liveness-rules-into-the-busy-probe` at `/home/dmz/.datawatch/prd-worktrees/c6be0001-tasks/wire-liveness-rules-into-the-busy-probe-7a850007`; parent repo `/home/dmz/.datawatch/dev`; base `master` `8d96b1b`.
- Prior tasks already added the pieces this one wires:
  - `internal/session/backend_liveness.go` — pure classifier `ClassifyLiveness(LivenessInput) LivenessVerdict`. Verdicts: `LivenessNoOpinion` (keep gap-watcher behaviour), `LivenessRunning`, `LivenessWaitingInput`, `LivenessStalled`. Input fields: `BackendFamily`, `PrintMode`, `ProcessAlive`, `LastActivity`, `Now`, `Window` (≤0 → `DefaultLivenessWindow`), `AtPrompt`, `StallOthers`.
  - Rules in priority order: (c) dead → Stalled, never WaitingInput; (a) print-mode alive + activity within window → Running; (b) print-mode alive + silent → Stalled; (d) interactive alive + at real prompt → WaitingInput; (e) interactive alive + not at prompt + within window → Running; (f) interactive alive + silent → Running for claude-code (preserves busy-probe), Stalled for other families only when `StallOthers` set, else NoOpinion.
  - `internal/session/store.go` — `StateStalled` state added (non-terminal).
  - `internal/config/backend_liveness.go` — `LivenessWindowSecondsEffective()` (0→`DefaultLivenessWindowSeconds`), `LivenessInputIdleTimeoutEffective(backendName)` (override wins, then print-mode→0, else global `input_idle_timeout` default 10s).
- `Manager.paneChildFn` (busy_probe.go `paneBusy`) is the injectable process-alive seam: `m.paneChildFn`, default `paneHasChildProcess`. `newTestManagerWithFake(t)` gives a Manager with `NewFakeTmux`. `FakeTmux` has a `Pane map[string]string` for content.
- Existing busy probe tests: `TestBusyProbe_*` in `busy_probe_test.go`. Existing state-engine tests: `TestChannelStateWatcher_*`, `TestBusyProbe_WatcherGapDoesNotFlipBusySession`, `TestBusyProbe_WatcherRecoversWaitingOneShot`, `TestBusyProbe_WatcherLeavesOtherBackendsAlone`.
- `session.IsPromptBackend(backendName)` (backend_family.go) = `strings.HasSuffix(name, "-prompt")`. `config.isPromptFamily` mirrors it.
- `applyBusyProbe` is called both from the recovery pass (WaitingInput OneShot delivered) and the gap pass (before flip). Both call sites in `runChannelStateWatcherTick`.
- Tests must not start real tmux/daemon; no version bump; no `gh`.
- Existing `busyProbeApplies` guards `sess.BackendFamily == busyProbeBackend && sess.TmuxSession != ""` — this hard-codes the claude-code gate. `paneBusy` reads the pane and the pane's child processes.
- `runChannelStateWatcherTick` currently: recovery pass → gap pass. Gap pass: `if now.Sub(sess.LastChannelEventAt) < gap` → continue; then `applyBusyProbe` (which only acts on claude-code via `busyProbeApplies`), then flip to WaitingInput. **It has no notion of StateStalled, no notion of process-alive for non-claude families, and no config accessor calls.**
- `LivenessInput.LastActivity` is the caller-supplied last-activity time (transcript/output-file mtime, or `LastChannelEventAt`).
- `LivenessInput.Window` is the liveness silence window; ≤0 selects `DefaultLivenessWindow`.
- `LivenessInput.StallOthers` opts in to stalling non-claude-code interactive backends when they are silent beyond the window; when false the classifier returns `LivenessNoOpinion` for that case.
- `StateStalled` is a non-terminal lifecycle state in `store.go`; a stalled session resumes to Running when activity returns.

# Plan / Next

- [x] Read task prompt + CLAUDE.md + all key source files (backend_liveness.go, busy_probe.go, state_engine.go, backend_family.go, config/backend_liveness.go, manager.go, busy_probe_test.go, store_stalled_test.go)
- [x] Ground-truth check of test helpers (`newTestManagerWithFake`, `busyProbeSession`, `NewFakeTmux`)
- [ ] Implement liveness-rule integration in `busy_probe.go` (`applyBusyProbe` for non-claude families via `ClassifyLiveness`)
- [ ] Implement liveness-rule integration in `state_engine.go` (`runChannelStateWatcherTick` gap pass: call `ClassifyLiveness` first; Stalled → `StateStalled`; NoOpinion → existing behaviour)
- [ ] Wire idle-timeout resolution in `state_engine.go` gap path using `cfg.LivenessInputIdleTimeoutEffective(sess.BackendFamily)`
- [ ] Extend `busy_probe_test.go` for non-claude-code families (alive+writing → Running, alive+silent → Stalled, dead → Stalled, at prompt → WaitingInput)
- [ ] Create `state_engine_liveness_test.go` with `TestStateEngineLiveness_*` covering pi-prompt + interactive backend across the four scenarios
- [ ] Run `go test ./internal/session/... -count=1` — all pass, including existing tests
- [ ] Checkpoint; report; finish with `DATAWATCH_COMPLETE`

# Key Decisions

- **`ClassifyLiveness` is the first decision for every backend in both paths** — `applyBusyProbe` and the gap tick. Claude-code is one of the families governed, not a special case: its `LivenessRunning` (from rule f, silent-but-busy) must map to the existing keep-Running behaviour, not a flip.
- The existing `busyProbeApplies` claude-only gate must be **removed or generalised** so the probe path also runs for non-claude-code families.
- `LivenessStalled` → set `StateStalled` in both the recovery and gap paths. The recovery path should not flip a Stalled session back to Running on its own; the state stays sticky until real output resumes it.
- `LivenessNoOpinion` → the existing gap-watcher behaviour applies (this is the `StallOthers=false` case for non-claude-code interactive backends, which keeps the current 15s-flip behaviour).
- **Print-mode (`-prompt` suffix) backends**: idle timeout resolves to 0 (off) via `LivenessInputIdleTimeoutEffective`; alive+writing stays Running; alive+silent → Stalled; dead → Stalled.
- **Interactive non-claude-code backends** (pi, goose, opencode, opencode-acp, gemini, aider, ollama, openwebui): alive+writing → Running; at real prompt → WaitingInput; alive+silent → Stalled **only when `StallOthers=true`**, else NoOpinion→existing gap-watcher behaviour (flip to WaitingInput after 15s).
- `LivenessInput.ProcessAlive` in the gap/recovery paths comes from `m.paneChildFn` via the existing `paneBusy` plumbing — but the probe must now run for all families, not just claude-code.
- `LivenessInput.LastActivity` for print-mode backends = transcript/output-file mtime (or `LastChannelEventAt` if no file mtime is available); for interactive = `LastChannelEventAt`.
- `LivenessInput.Window` = `cfg.LivenessWindowSecondsEffective()` (resolved once per session check), falling back to `DefaultLivenessWindow` when ≤0.
- No tmux/daemon in tests; use `newTestManagerWithFake(t)` and `FakeTmux.Pane`.
- No version bump, no `gh`.

# Findings — ground truth

- `newTestManagerWithFake(t)` (bl89_fake_tmux_test.go:11): creates `newTestManager(t)` then `m.tmux = NewFakeTmux()`; `FakeTmux.Pane` is `map[string]string` for content.
- `busyProbeSession(t, mgr, suffix, state, family)` (busy_probe_test.go:23): saves a session with `TmuxSession: "cs-<suffix>"`, `BackendFamily: family`, `OneShot: true`.
- `newBusyProbeManager(t)` (busy_probe_test.go:45): creates `newTestManagerWithFake`, sets `mgr.paneChildFn = func(string) bool { return false }`.
- `paneBusy` (busy_probe.go): reads `m.tmux.CapturePaneLiveTail(sess.TmuxSession)`, calls `paneLooksBusy(capture, childFn(sess.TmuxSession))`.
- `runChannelStateWatcherTick` (state_engine.go): recovery pass iterates `m.store.List()`, gates on `sess.State == StateWaitingInput && sess.OneShot && m.TaskDelivered(sess.FullID)`, calls `m.applyBusyProbe(sess.FullID, now)`. Gap pass: iterates Running sessions, checks `now.Sub(sess.LastChannelEventAt) < sessGap`, calls `m.applyBusyProbe(sess.FullID, now)`, then flips to `StateWaitingInput`.
- `applyBusyProbe` (busy_probe.go): calls `m.paneBusy(sess)` which calls `busyProbeApplies(sess)` → `sess.BackendFamily == busyProbeBackend` (claude-code only). **This is the hard gate to remove/generalise.**
- `StateStalled` exists in store.go; `store_stalled_test.go` pins it as non-terminal.
- `config.LivenessWindowSecondsEffective()` returns `DefaultLivenessWindowSeconds` (120s) when `Session.LivenessWindowSeconds` ≤ 0.
- `config.LivenessInputIdleTimeoutEffective(backend)`: override wins; then print-mode → 0; else global `input_idle_timeout` default 10s.
- `session.IsPromptBackend(name)` = `strings.HasSuffix(name, "-prompt")` (backend_family.go:15).
- `paneChildFn` is `func(string) bool` on Manager (busy_probe.go:340).
- `m.cfg` is `*config.Config` on Manager (config.go:555); tests use `newTestManager` which likely sets a default config — needs verification.
- `LivenessInput` needs `BackendFamily`, `PrintMode`, `ProcessAlive`, `LastActivity`, `Now`, `Window`, `AtPrompt`, `StallOthers`. For the gap/recovery paths: `PrintMode = IsPromptBackend(family)`, `ProcessAlive = paneChildFn result`, `LastActivity = LCE or output-file mtime`, `Now = tick time`, `Window = cfg.LivenessWindowSecondsEffective()`, `AtPrompt = false` (not determinable without pane parsing — the classifier handles this), `StallOthers = false` (preserve current behaviour for non-claude-code interactive until explicitly opted in).
- The `AtPrompt` field is set by the caller, not derived. For the gap/recovery paths we do not have an "at prompt" signal for interactive backends (that is the pane-parse path in `paneBusy`). Setting `AtPrompt=false` means interactive backends always fall to rule (e) or (f), never (d). This is correct for the gap path (which fires on silence, not on prompt detection). The busy probe already detects "at prompt" via `paneIdlePromptVisible` — but that is the claude-code idle prompt, not a generic one. For non-claude-code interactive backends, rule (d) is unreachable in the gap path, which is fine: they fall to (f) → NoOpinion → existing gap-watcher behaviour.

# Errors

(none yet)


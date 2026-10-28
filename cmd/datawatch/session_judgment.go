package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	alertspkg "github.com/dmz006/datawatch/internal/alerts"
	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/session"
)

// decomposeSessionName is the sessionName the PRD decompose one-shot
// session runs under. runOneShotJSONSessionWithEffort keys its
// decompose-only waiting_input handling off it.
const decomposeSessionName = "autonomous:decompose"

// resolveAskCompatibleKind maps a raw backend string to (askKind, model,
// err): if raw is a named LLM in the registry, its kind is returned,
// otherwise raw itself is returned as the kind. If the resolved kind is
// not ask-compatible, an error is returned — but kind is still the REAL
// resolved kind, never "", so a caller that needs to know what it
// actually was can still see it even on the error path.
//
// B124 (2026-10-12) — found live: this function was a closure inline in
// decomposeFn's setup, originally returning ("", "", err) on the
// not-ask-compatible branch, which was fine for its one original
// caller (decomposeFn, which only ever checks err). When verify/
// guardrail routing (needsSessionJudgment) started reusing this same
// closure to learn whether a configured backend had real tool access,
// discarding err (as both call sites already did) silently turned into
// "the kind was always empty," which made isInferenceSessionKind("")
// always false — so every verify/guardrail call configured for a
// tool-capable backend silently fell through to the old ollama
// default, with no error and no log line, for the entire time this bug
// was live. Returning the real kind alongside the error fixes every
// existing caller's actual need (decomposeFn and resolveVerifierCandidates
// both already check err explicitly) while finally giving
// needsSessionJudgment what it always needed. Extracted to a package-
// level function, with reg and askCompatible passed in rather than
// closed over, specifically so this exact contract is independently
// testable — the inline closure version never was, which is why a unit
// test using an idealized fake (one that happened to always return the
// real kind) passed while the real closure's bug shipped anyway.
func resolveAskCompatibleKind(reg *inference.Registry, raw string, askCompatible func(string) bool) (kind, model string, err error) {
	if reg != nil {
		if llm, regErr := reg.Get(raw); regErr == nil && !llm.Disabled {
			kind = string(llm.Kind)
			model = llm.Model
			if !askCompatible(kind) {
				return kind, model, fmt.Errorf("backend %q has kind %q which does not support headless calls via /api/ask", raw, kind)
			}
			return kind, model, nil
		}
	}
	// Raw adapter type string (not in named registry).
	return raw, "", nil
}

// needsSessionJudgment (B124 — filed debugging why BL416's verifier never
// got real tool access despite being pointed at a tool-capable backend)
// reports whether a verify/guardrail call for the resolved backend kind
// should run as a real one-shot tool-using session instead of a
// text-only /api/ask call. True only when the kind has no ask adapter
// (inference.IsSessionBackendKind — claude-code, opencode, goose, aider,
// gemini, shell) AND there is a real project directory to run it in: a
// session-only kind with no project_dir (a headless PRD, no worktree)
// has nowhere to spawn into and must still fall back to the text-only
// path, same as before this change.
//
// kind and isSessionKind are passed in rather than closed over so this
// decision is a pure, independently testable function — same rationale
// resolveVerifierBackendModel's own doc comment already gives.
func needsSessionJudgment(kind, projectDir string, isSessionKind func(string) bool) bool {
	return projectDir != "" && isSessionKind(kind)
}

// isInferenceSessionKind reports whether kind names a backend with real
// tool/file access — fully data-driven off llm.ToolCapable (see that
// interface's doc comment for why this replaced a hand-maintained
// switch of kind-name literals, B124). Any kind not registered in the
// llm package at all (ask-compatible chat kinds like ollama/openwebui,
// or the cloud "claude" API kind) is never tool-capable by definition —
// they have no llm.Backend entry to query.
func isInferenceSessionKind(kind string) bool {
	b, err := llm.Get(kind)
	if err != nil {
		return false
	}
	tc, ok := b.(llm.ToolCapable)
	return ok && tc.HasToolAccess()
}

// sessionJudgmentTask (B124) wraps an existing /api/ask-style judgment
// prompt — one that already ends in an inline "reply with JSON" request
// — for delivery to a one-shot tool-using session instead: tells the
// session it has real file/tool access in projectDir and to write that
// same JSON to outputFile rather than reply inline. The judgment prompt
// text itself (what to verify, the diff, the security preamble) is
// reused byte-for-byte from the existing /api/ask path — only the
// delivery and result-capture mechanism changes.
func sessionJudgmentTask(prompt, projectDir, outputFile string) string {
	return prompt + "\n\n" +
		"You are running as a one-shot coding session inside " + projectDir +
		" with real file and tool access — use it. Read the files the diff" +
		" touched, and run the project's own build/lint/test commands if" +
		" that would ground your verdict, before deciding. Do not take the" +
		" diff's or spec's own claims at face value when you can check them.\n\n" +
		"Do NOT reply with the JSON directly — this is not a chat. Instead" +
		" write EXACTLY that JSON object (no markdown fences, no extra" +
		" prose, no explanation) to this file:\n" + outputFile
}

// runOneShotJSONSession (B124) spawns a one-shot coding session to carry
// out sessionTask — which must itself instruct the session to write its
// JSON result to outputFile — and returns that file's content once
// ready. Extracted from decomposeFnSession (Option B, v8.21.0), which
// proved this exact spawn / poll-output-file-first / kill-on-return
// shape for decompose; this generalizes it so the verify/guardrail
// session path (see needsSessionJudgment) can reuse it rather than
// duplicate it.
//
// scratchRelocateName is the stable basename this session's output is
// relocated to under the PRD's scratch dir (see
// autonomouspkg.RelocateProjectFile/ScratchDir) once written — callers
// MUST use a name unique to their own call site (e.g.
// "decompose-output.json", "verify-<taskID>.json") so concurrent
// sessions for the same PRD never collide on the same relocated file.
func runOneShotJSONSession(cfg *config.Config, prdID, projectDir, backend, model, sessionName, sessionTask, outputFile, scratchRelocateName string, timeout time.Duration) (string, error) {
	return runOneShotJSONSessionWithEffort(cfg, prdID, projectDir, backend, model, "", sessionName, sessionTask, outputFile, scratchRelocateName, timeout)
}

// runOneShotJSONSessionWithEffort is runOneShotJSONSession plus the
// session's effort (a /api/sessions/start effort value: quick / normal /
// thorough). Empty effort omits the field, so the request body is
// identical to runOneShotJSONSession's and the daemon's
// session.default_effort applies.
// sessionOneShotTimeoutMessage is the operator-visible text for a one-shot
// judgment session that exceeded its limit. Operators acting on it need the
// backend, the exact model and the limit to debug a local-model timeout, so
// all four are named. An empty model means the backend default was used and
// prints as "model=" — that is the truth, so no placeholder is substituted.
func sessionOneShotTimeoutMessage(sessionName, sessionID, backend, model string, timeout time.Duration) string {
	return fmt.Sprintf("%s session %s (backend=%s, model=%s) timed out after %v", sessionName, sessionID, backend, model, timeout)
}

func runOneShotJSONSessionWithEffort(cfg *config.Config, prdID, projectDir, backend, model, effort, sessionName, sessionTask, outputFile, scratchRelocateName string, timeout time.Duration) (string, error) {
	scratchDir, scratchErr := autonomouspkg.ScratchDir(expandHome(cfg.DataDir), prdID)
	if scratchErr != nil {
		fmt.Printf("[%s] scratch dir for prd=%s: %v\n", sessionName, prdID, scratchErr)
	}
	relocate := func() (string, bool) {
		return autonomouspkg.RelocateProjectFile(projectDir, filepath.Base(outputFile), scratchDir, scratchRelocateName)
	}
	// Restart-recovery: reuse output written by a previous attempt of
	// the exact same call (same outputFile name, derived by the caller
	// from its own content hash).
	if existingContent, ok := relocate(); ok {
		fmt.Printf("[%s] reusing prior output file %s (%d bytes)\n", sessionName, outputFile, len(existingContent))
		return existingContent, nil
	}
	_ = os.Remove(outputFile) // clear stale/empty output from an incomplete prior attempt

	startReq := map[string]any{
		"task":        sessionTask,
		"project_dir": projectDir,
		"backend":     backend,
		"model":       model,
		"name":        sessionName,
		"one_shot":    true,
		"prd_id":      prdID,
	}
	if effort != "" {
		startReq["effort"] = effort
	}
	body, _ := json.Marshal(startReq)
	spawnedAt := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// A full session cap is a wait, not a failure: retry until a slot
	// frees or the timeout expires.
	var rb []byte
	for {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			loopbackBaseURL(cfg)+"/api/sessions/start",
			bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if cfg.Server.Token != "" {
			httpReq.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
		}
		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			return "", fmt.Errorf("%s session start: %w", sessionName, err)
		}
		rb, _ = io.ReadAll(resp.Body)
		resp.Body.Close() //nolint:errcheck
		if resp.StatusCode == http.StatusOK {
			break
		}
		if !strings.Contains(string(rb), "max sessions") {
			return "", fmt.Errorf("%s session start: %s — %s", sessionName, resp.Status, string(rb))
		}
		fmt.Printf("[%s] waiting for a free session slot: %s\n", sessionName, strings.TrimSpace(string(rb)))
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("%s session start: %w while waiting for capacity", sessionName, ctx.Err())
		case <-time.After(15 * time.Second):
		}
	}
	var startOut struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rb, &startOut)
	if startOut.ID == "" {
		return "", fmt.Errorf("%s session start: no id in response: %s", sessionName, string(rb))
	}
	fmt.Printf("[%s] spawned %s (backend=%s, output=%s)\n", sessionName, startOut.ID, backend, outputFile)

	// Best-effort: kill the session when this function returns (handles
	// TUI sessions that land in waiting_input after writing the output).
	sessionID := startOut.ID
	defer func() {
		killCtx, killCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer killCancel()
		killBody, _ := json.Marshal(map[string]string{"id": sessionID})
		killReq, err := http.NewRequestWithContext(killCtx, http.MethodPost,
			loopbackBaseURL(cfg)+"/api/sessions/kill", bytes.NewReader(killBody))
		if err != nil {
			return
		}
		killReq.Header.Set("Content-Type", "application/json")
		if cfg.Server.Token != "" {
			killReq.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
		}
		if r, err := http.DefaultClient.Do(killReq); err == nil {
			_ = r.Body.Close()
		}
		fmt.Printf("[%s] killed %s\n", sessionName, sessionID)
	}()

	// role names this session's job in the no-output error family, e.g.
	// "decompose" for the PRD planning session.
	role := strings.TrimPrefix(sessionName, "autonomous:")

	// Poll for session completion.
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	waitingInputTicks := 0
	// Session idle detector (autonomous.session_idle_timeout_seconds):
	// idleSince is when the session was first seen parked in waiting_input in
	// the current unbroken run.
	idleLimit := time.Duration(config.NormalizeSessionIdleTimeoutSeconds(cfg.Autonomous.SessionIdleTimeoutSeconds)) * time.Second
	var idleSince time.Time
	for {
		select {
		case <-ctx.Done():
			return "", &autonomouspkg.OneShotNoOutputError{Msg: sessionOneShotTimeoutMessage(sessionName, startOut.ID, backend, model, timeout)}
		case <-tick.C:
			pollReq, err := http.NewRequestWithContext(ctx, http.MethodGet,
				loopbackBaseURL(cfg)+"/api/sessions/"+startOut.ID, nil)
			if err != nil {
				continue
			}
			if cfg.Server.Token != "" {
				pollReq.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
			}
			pollResp, err := http.DefaultClient.Do(pollReq)
			if err != nil {
				continue
			}
			pollBody, _ := io.ReadAll(pollResp.Body)
			pollResp.Body.Close() //nolint:errcheck

			var s struct {
				State       string `json:"state"`
				TmuxSession string `json:"tmux_session"`
			}
			if err := json.Unmarshal(pollBody, &s); err != nil {
				continue
			}
			// Output-file-first: whenever the file exists and has
			// content, treat the call as done regardless of session
			// state — see decomposeFnSession's identical reasoning.
			if content, ok := relocate(); ok {
				fmt.Printf("[%s] %s state=%q, output file ready (%d bytes) — treating as complete\n", sessionName, startOut.ID, s.State, len(content))
				return content, nil
			}
			// waiting_input debounce, mirroring the task executor's
			// stall check (see waitingInputTick). A one-shot decompose
			// session is never answered by a human, so a sustained
			// waiting_input is a stuck session: kill it (the deferred kill
			// above) and report the no-output family rather than polling
			// to the timeout. Scoped to claude-code like the executor:
			// other backends show a persistent prompt-looking status line
			// that would be misread as waiting_input.
			var stalled bool
			if sessionName == decomposeSessionName && backend == "claude-code" {
				waitingInputTicks, stalled = waitingInputTick(waitingInputTicks, session.State(s.State))
			}
			if stalled {
				fmt.Printf("[%s] %s state=waiting_input for %d ticks — killing; one-shot session cannot answer\n", sessionName, startOut.ID, waitingInputTicks)
				return "", &autonomouspkg.OneShotNoOutputError{Msg: fmt.Sprintf("one-shot %s session %s ended with state waiting_input without producing output (backend=%s, model=%q)", role, startOut.ID, backend, model)}
			}
			// Session idle detector: a one-shot verifier parked in
			// waiting_input with no live runner child past the budget
			// will never write its output. Returning here fires the
			// deferred kill above, and the infra error feeds the retry budget.
			if s.State != "waiting_input" {
				idleSince = time.Time{}
			} else {
				if idleSince.IsZero() {
					idleSince = time.Now()
				}
				if idleFor := time.Since(idleSince); idleFor > idleLimit {
					_, outputAge, _ := staleWorkerCheck(outputFile, idleLimit, time.Now())
					idleObs := autonomouspkg.SessionIdleObservation{
						SessionID:           startOut.ID,
						Backend:             backend,
						Model:               model,
						State:               s.State,
						IdleFor:             idleFor,
						OutputAge:           outputAge,
						LogLastEventAge:     piLogLastEventAge(backend, piSessionsRoot(), projectDir, spawnedAt, time.Now()),
						HasLiveChildProcess: hasLiveChildProcess(s.TmuxSession),
						Limit:               idleLimit,
					}
					if idleStalled, idleReason := idleObs.Stalled(); idleStalled {
						alertspkg.EmitSystem(alertspkg.LevelError, "Verifier session idle without completion",
							fmt.Sprintf("prd=%s session=%s backend=%s model=%s: %s", prdID, startOut.ID, backend, model, idleReason))
						return "", autonomouspkg.SessionIdleError(idleObs)
					}
				}
			}
			switch s.State {
			case "complete":
				return "", fmt.Errorf("%s session %s completed but output file %s not found (backend=%s, model=%q)", sessionName, startOut.ID, outputFile, backend, model)
			case "failed", "killed", "cancelled":
				// Give the output file one last chance: the session may
				// have written it in the same instant it was killed.
				time.Sleep(3 * time.Second)
				if content, ok := relocate(); ok {
					fmt.Printf("[%s] %s state=%q, output file appeared after kill (%d bytes)\n", sessionName, startOut.ID, s.State, len(content))
					return content, nil
				}
				return "", &autonomouspkg.OneShotNoOutputError{Msg: fmt.Sprintf("one-shot %s session %s ended with state %s without producing output (backend=%s, model=%q)", role, startOut.ID, s.State, backend, model)}
			}
		}
	}
}

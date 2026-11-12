package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
)

// supervisorLoopSessionTimeout bounds a session-style model job.
const supervisorLoopSessionTimeout = 30 * time.Minute

// supervisorLoopAskFn is the oneshot model call for the supervisor loop. It
// resolves backend through the daemon's LLM registry (a named LLM becomes its
// adapter kind and, when model is empty, its configured model) and calls the
// same /api/ask path decompose uses. The manager's decision-assistant call
// path wraps it with DefaultDecisionModelTimeout.
func supervisorLoopAskFn(cfg *config.Config, reg *inference.Registry) func(ctx context.Context, backend, model, prompt string) (string, error) {
	askCompatible := func(b string) bool { return b == "ollama" || b == "openwebui" }
	return func(ctx context.Context, backend, model, prompt string) (string, error) {
		kind, regModel, err := resolveAskCompatibleKind(reg, backend, askCompatible)
		if err != nil {
			return "", err
		}
		if !askCompatible(kind) {
			return "", fmt.Errorf("backend %q cannot answer a oneshot call; use style session", backend)
		}
		if model == "" {
			model = regModel
		}
		body := map[string]any{"question": prompt, "backend": kind}
		if backend != kind {
			body["llm"] = backend
		}
		if model != "" {
			body["model"] = model
		}
		raw, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, loopbackBaseURL(cfg)+"/api/ask", bytes.NewReader(raw))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		if cfg.Server.Token != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.Server.Token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("supervisor loop ask: %w", err)
		}
		defer resp.Body.Close() //nolint:errcheck
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("supervisor loop ask: %s — %s", resp.Status, string(b))
		}
		var ans struct {
			Answer string `json:"answer"`
		}
		if err := json.Unmarshal(b, &ans); err != nil {
			return string(b), nil
		}
		return ans.Answer, nil
	}
}

// supervisorLoopSessionFn starts a long-running session through the existing
// one-shot session start path (/api/sessions/start via runOneShotJSONSession),
// in a scratch directory under the data directory, and returns what the
// session wrote to its output file.
func supervisorLoopSessionFn(cfg *config.Config) autonomouspkg.SupervisorSessionFn {
	return func(ctx context.Context, backend, model, prompt string) (string, error) {
		dir := filepath.Join(expandHome(cfg.DataDir), "supervisor-loop-sessions")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
		sum := sha256.Sum256([]byte(backend + "\x00" + model + "\x00" + prompt))
		id := hex.EncodeToString(sum[:8])
		outputFile := filepath.Join(dir, "answer-"+id+".txt")
		task := prompt + "\n\nDo NOT reply in chat. Write ONLY your final answer (no fences, no commentary) to this file:\n" + outputFile
		type result struct {
			out string
			err error
		}
		ch := make(chan result, 1)
		go func() {
			out, err := runOneShotJSONSession(cfg, "supervisor-loop", dir, backend, model,
				"supervisor-loop-"+id, task, outputFile, "supervisor-loop-"+id+".txt", supervisorLoopSessionTimeout,
				launchTagsForSupervisorLoop())
			ch <- result{out, err}
		}()
		select {
		case r := <-ch:
			return r.out, r.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

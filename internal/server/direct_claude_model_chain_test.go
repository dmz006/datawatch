package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/llm"
)

// fakeClaudeBackend reports the claude-code family but never launches a CLI.
type fakeClaudeBackend struct{}

func (fakeClaudeBackend) Name() string { return "claude-code" }
func (fakeClaudeBackend) Launch(_ context.Context, _, _, _, _ string) error {
	return nil
}
func (fakeClaudeBackend) SupportsInteractiveInput() bool { return true }
func (fakeClaudeBackend) Version() string                { return "fake" }
func (fakeClaudeBackend) SetModel(string)                {}

// postDirectStart posts body to handleStartSession and returns the recorder.
func postDirectStart(t *testing.T, srv *Server, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/start", bytes.NewReader(raw))
	w := httptest.NewRecorder()
	srv.handleStartSession(w, req)
	return w
}

func startedModel(t *testing.T, srv *Server, w *httptest.ResponseRecorder) string {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body: %s", w.Code, w.Body.String())
	}
	cleanupStartedSession(t, srv, w)
	var resp struct {
		FullID string `json:"full_id"`
		ID     string `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	id := resp.FullID
	if id == "" {
		id = resp.ID
	}
	sess, ok := srv.manager.GetSession(id)
	if !ok || sess == nil {
		t.Fatalf("session %q not found", id)
	}
	return sess.Model
}

func claudeRowServer(t *testing.T, row *inference.LLM) *Server {
	t.Helper()
	srv := newLLMServer(t)
	// Replace the registered claude-code backend with a fake: no real launch.
	llm.Register(fakeClaudeBackend{})
	if err := srv.inferenceReg.Add(row); err != nil {
		t.Fatalf("Add: %v", err)
	}
	return srv
}

func TestDirectClaudeStartModelBodyWinsOverRow(t *testing.T) {
	srv := claudeRowServer(t, &inference.LLM{Name: "cc-row", Kind: inference.KindClaudeCode, Model: "haiku"})
	w := postDirectStart(t, srv, map[string]any{
		"task": "hi", "llm": "cc-row", "model": "sonnet", "one_shot": true, "project_dir": t.TempDir(),
	})
	if got := startedModel(t, srv, w); got != "sonnet" {
		t.Fatalf("Model = %q, want sonnet (body wins)", got)
	}
}

func TestDirectClaudeStartModelFromRow(t *testing.T) {
	srv := claudeRowServer(t, &inference.LLM{
		Name: "cc-row", Kind: inference.KindClaudeCode,
		Models: []inference.EnabledModel{{Model: ""}, {Model: "haiku"}},
	})
	w := postDirectStart(t, srv, map[string]any{
		"task": "hi", "llm": "cc-row", "one_shot": true, "project_dir": t.TempDir(),
	})
	if got := startedModel(t, srv, w); got != "haiku" {
		t.Fatalf("Model = %q, want haiku from the row", got)
	}
}

func TestDirectClaudeStartRefusalNamesEverySurface(t *testing.T) {
	srv := claudeRowServer(t, &inference.LLM{Name: "cc-row", Kind: inference.KindClaudeCode})
	w := postDirectStart(t, srv, map[string]any{
		"task": "hi", "llm": "cc-row", "one_shot": true, "project_dir": t.TempDir(),
	})
	if w.Code == http.StatusOK {
		cleanupStartedSession(t, srv, w)
		t.Fatalf("start with no model anywhere must be refused; got 200")
	}
	body := w.Body.String()
	for _, want := range []string{
		"refused", "datawatch session new --model", "POST /api/sessions/start",
		"start_session", "new: model=<name>", "datawatch llm models add <llm> --model <name>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("refusal must name %q; got: %s", want, body)
		}
	}
}

func TestDirectNonClaudeStartModelUnchanged(t *testing.T) {
	srv := claudeRowServer(t, &inference.LLM{Name: "pi-row", Kind: inference.KindPiPrompt})
	w := postDirectStart(t, srv, map[string]any{
		"task": "hi", "llm": "pi-row", "one_shot": true, "project_dir": t.TempDir(),
	})
	if got := startedModel(t, srv, w); got != "" {
		t.Fatalf("non-claude-code Model = %q, want empty", got)
	}
}

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/federation"
)

func TestQuestionRaiseTradeoffs_MCP(t *testing.T) {
	var gotBody map[string]any
	var gotPath, gotMethod string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"question":{"id":"q1"},"warnings":[{"code":"short_text","message":"too short"}]}`))
	})

	tool := s.toolQuestionRaise()
	if _, ok := tool.InputSchema.Properties["tradeoffs"]; !ok {
		t.Fatal("question_raise has no tradeoffs parameter")
	}
	for _, want := range []string{"warn", "refuse", "warnings", "422"} {
		if !strings.Contains(tool.Description, want) {
			t.Errorf("question_raise description does not mention %q: %s", want, tool.Description)
		}
	}

	res, err := s.handleQuestionRaise(context.Background(), callReq(map[string]any{
		"text":      "Ship the change now, or hold it for the next release?",
		"options":   "ship,hold",
		"context":   "none",
		"tradeoffs": map[string]any{"ship": "faster, skips the soak", "hold": "slower, safer"},
	}))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/questions" {
		t.Errorf("proxied %s %s, want POST /api/questions", gotMethod, gotPath)
	}
	tradeoffs, ok := gotBody["tradeoffs"].(map[string]any)
	if !ok {
		t.Fatalf("body carries no tradeoffs object: %v", gotBody)
	}
	if tradeoffs["ship"] != "faster, skips the soak" || tradeoffs["hold"] != "slower, safer" {
		t.Errorf("tradeoffs = %v", tradeoffs)
	}
	if text := resultText(t, res, err); !strings.Contains(text, "warnings") {
		t.Errorf("response should carry the warnings back, got %s", text)
	}

	// Without tradeoffs the body does not carry the key.
	gotBody = nil
	if _, err := s.handleQuestionRaise(context.Background(), callReq(map[string]any{
		"text": "Ship it?", "options": "ship,hold", "context": "none",
	})); err != nil {
		t.Fatal(err)
	}
	if _, present := gotBody["tradeoffs"]; present {
		t.Errorf("tradeoffs sent when none were given: %v", gotBody)
	}
}

func TestQuestionQuality_MCP(t *testing.T) {
	var gotPath, gotMethod string
	s := capacityTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_, _ = w.Write([]byte(`{"mode":"warn","min_text_chars":40,"tracker_id_pattern":"x","queue_preview_lines":3}`))
	})

	tool := s.toolQuestionQuality()
	if tool.Name != "question_quality" {
		t.Fatalf("name = %q", tool.Name)
	}
	if tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Error("question_quality must be marked read-only")
	}
	if got := federation.MCPToolCap["question_quality"]; got != federation.CapAutonomousRead {
		t.Errorf("capability = %q, want %q", got, federation.CapAutonomousRead)
	}

	res, err := s.handleQuestionQuality(context.Background(), callReq(nil))
	if err != nil || res == nil || res.IsError {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/questions/quality" {
		t.Errorf("proxied %s %s, want GET /api/questions/quality", gotMethod, gotPath)
	}
	text := resultText(t, res, err)
	for _, want := range []string{`"mode":"warn"`, `"min_text_chars":40`, `"queue_preview_lines":3`} {
		if !strings.Contains(text, want) {
			t.Errorf("policy missing %s: %s", want, text)
		}
	}

	answer := s.toolQuestionAnswer()
	for _, want := range []string{
		"optional when you pick an option",
		"saved exactly as written, with your choice, as the record of your decision",
		"the asker reads it as your answer",
	} {
		if !strings.Contains(answer.Description, want) {
			t.Errorf("question_answer description missing %q", want)
		}
		quote, _ := answer.InputSchema.Properties["quote"].(map[string]any)
		desc, _ := quote["description"].(string)
		if !strings.Contains(desc, want) {
			t.Errorf("quote description missing %q: %s", want, desc)
		}
	}
}

// B98 — tests for the OpenWebUI per-turn usage-reporting callback.
// The SSE fixture shape below was captured from a real OpenWebUI 0.11.4
// instance (routed to a local Ollama model) via a live /api/chat/completions
// call, not guessed — see cmd/datawatch/main.go's B98 openwebui wiring
// comment for the live-verification context.

package openwebui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const realOpenWebUISSEFixture = `data: {"id": "chatcmpl-1", "created": 1, "model": "qwen3:1.7b", "choices": [{"index": 0, "logprobs": null, "finish_reason": null, "delta": {"content": "Hi"}}], "object": "chat.completion.chunk"}

data: {"id": "chatcmpl-1", "created": 1, "model": "qwen3:1.7b", "choices": [{"index": 0, "logprobs": null, "finish_reason": "stop", "delta": {}}], "object": "chat.completion.chunk", "usage": {"input_tokens": 15, "output_tokens": 182, "total_tokens": 197, "prompt_tokens": 15, "completion_tokens": 182}}

data: [DONE]

`

func TestSendAndStream_ReportsUsageFromFinalChunk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, realOpenWebUISSEFixture)
	}))
	defer srv.Close()

	var gotTmux string
	var gotIn, gotOut int
	calls := 0
	usageFn = func(tmuxSession string, tokensIn, tokensOut int) {
		calls++
		gotTmux, gotIn, gotOut = tmuxSession, tokensIn, tokensOut
	}
	defer func() { usageFn = nil }()

	b := &InteractiveBackend{baseURL: srv.URL, model: "qwen3:1.7b"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.sendAndStream(ctx, "cs-usage-test", "say hi", true); err != nil {
		t.Fatalf("sendAndStream: %v", err)
	}

	if calls != 1 {
		t.Fatalf("usageFn called %d times, want 1", calls)
	}
	if gotTmux != "cs-usage-test" || gotIn != 15 || gotOut != 182 {
		t.Fatalf("got tmux=%q in=%d out=%d, want cs-usage-test/15/182", gotTmux, gotIn, gotOut)
	}
}

func TestSendAndStream_NoUsageChunkNeverCallsUsageFn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\": \"chatcmpl-1\", \"choices\": [{\"delta\": {\"content\": \"Hi\"}}], \"object\": \"chat.completion.chunk\"}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	called := false
	usageFn = func(string, int, int) { called = true }
	defer func() { usageFn = nil }()

	b := &InteractiveBackend{baseURL: srv.URL, model: "qwen3:1.7b"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := b.sendAndStream(ctx, "cs-no-usage-test", "say hi", true); err != nil {
		t.Fatalf("sendAndStream: %v", err)
	}
	if called {
		t.Error("usageFn must not be called when no chunk carries usage")
	}
}

func TestSetUsageFn(t *testing.T) {
	usageFn = nil
	defer func() { usageFn = nil }()
	called := false
	SetUsageFn(func(string, int, int) { called = true })
	if usageFn == nil {
		t.Fatal("SetUsageFn did not register the callback")
	}
	usageFn("x", 1, 1)
	if !called {
		t.Error("registered callback was not invoked")
	}
}

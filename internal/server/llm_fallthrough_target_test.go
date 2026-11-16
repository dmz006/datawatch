// REST round-trip coverage for the LLM registry entry's fallthrough_target
// mark (an allowed capacity fall-through target).

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fallthroughTargetEntry struct {
	Name              string `json:"name"`
	FallthroughTarget bool   `json:"fallthrough_target"`
}

// TestLLMFallthroughTargetRESTRoundTrip: creation carries the mark, an update
// that omits the field keeps it, an update that sets it false clears it, and
// an entry created without it is unmarked.
func TestLLMFallthroughTargetRESTRoundTrip(t *testing.T) {
	srv := newLLMServer(t)
	send := func(method, path string, payload map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.URL.Path = path
		w := httptest.NewRecorder()
		srv.handleLLMs(w, req)
		return w
	}
	get := func(name string) fallthroughTargetEntry {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/llms/"+name, nil)
		req.URL.Path = "/api/llms/" + name
		w := httptest.NewRecorder()
		srv.handleLLMs(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("get %s: status %d body: %s", name, w.Code, w.Body.String())
		}
		var got fallthroughTargetEntry
		if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return got
	}

	if w := send(http.MethodPost, "/api/llms", map[string]any{"name": "ft-marked", "kind": "claude-code", "fallthrough_target": true}); w.Code != http.StatusOK {
		t.Fatalf("create marked: status %d body: %s", w.Code, w.Body.String())
	}
	if w := send(http.MethodPost, "/api/llms", map[string]any{"name": "ft-plain", "kind": "claude-code"}); w.Code != http.StatusOK {
		t.Fatalf("create plain: status %d body: %s", w.Code, w.Body.String())
	}
	if !get("ft-marked").FallthroughTarget {
		t.Error("create: fallthrough_target not stored")
	}
	if get("ft-plain").FallthroughTarget {
		t.Error("an entry created without the field must not be a target")
	}

	// Update without the field keeps the stored mark.
	if w := send(http.MethodPut, "/api/llms/ft-marked", map[string]any{"name": "ft-marked", "kind": "claude-code", "model": "sonnet"}); w.Code != http.StatusOK {
		t.Fatalf("update: status %d body: %s", w.Code, w.Body.String())
	}
	if !get("ft-marked").FallthroughTarget {
		t.Error("update without fallthrough_target must keep the stored mark")
	}

	// Explicit false clears it; explicit true sets it on the plain entry.
	if w := send(http.MethodPut, "/api/llms/ft-marked", map[string]any{"name": "ft-marked", "kind": "claude-code", "fallthrough_target": false}); w.Code != http.StatusOK {
		t.Fatalf("clear: status %d body: %s", w.Code, w.Body.String())
	}
	if get("ft-marked").FallthroughTarget {
		t.Error("explicit false must clear the mark")
	}
	if w := send(http.MethodPut, "/api/llms/ft-plain", map[string]any{"name": "ft-plain", "kind": "claude-code", "fallthrough_target": true}); w.Code != http.StatusOK {
		t.Fatalf("set: status %d body: %s", w.Code, w.Body.String())
	}
	if !get("ft-plain").FallthroughTarget {
		t.Error("explicit true must set the mark")
	}
}

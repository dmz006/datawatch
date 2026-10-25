package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// startFakeModelServer stands up an Ollama-shaped fake: /api/tags and
// /api/ps. loaded lists the model names /api/ps reports as loaded;
// failWhen true makes every endpoint answer 500 (a broken server).
func startFakeModelServer(t *testing.T, loaded []string, failWhen bool) *httptest.Server {
	t.Helper()
	body := `{"models":[`
	if len(loaded) > 0 {
		body += `{"name":"` + loaded[0] + `"}`
	}
	body += `]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failWhen {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tags", "/api/ps":
			_, _ = fmt.Fprint(w, body)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A probe against a model server that errors must report unknown — never
// active, never idle-by-error. The watchdog treats unknown as "no
// evidence either way"; it can never end a session on its own.
func TestProbeModelServerRequestErrorReportsUnknown(t *testing.T) {
	srv := startFakeModelServer(t, nil, true)
	if got := probeModelServerRequest(srv.URL, "ollama/qwen3.8:27b"); got != modelServerRequestUnknown {
		t.Fatalf("probe against an erroring server must report unknown, got %v", got)
	}

	// A server that refuses the connection is unknown as well.
	refused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := refused.URL
	refused.Close()
	if got := probeModelServerRequest(url, "ollama/qwen3.8:27b"); got != modelServerRequestUnknown {
		t.Fatalf("probe against a refused connection must report unknown, got %v", got)
	}

	// An unset host is unknown (no probe at all).
	if got := probeModelServerRequest("", "ollama/qwen3.8:27b"); got != modelServerRequestUnknown {
		t.Fatalf("probe with an unset host must report unknown, got %v", got)
	}
}

// The session's model loaded and serving on the host -> active request.
// The model string may carry the "ollama/" prefix or not.
func TestProbeModelServerRequestActiveWhenModelLoaded(t *testing.T) {
	srv := startFakeModelServer(t, []string{"glm-5:755b"}, false)
	for _, model := range []string{"ollama/glm-5:755b", "glm-5:755b"} {
		if got := probeModelServerRequest(srv.URL, model); got != modelServerRequestActive {
			t.Fatalf("model %q loaded and serving must report active, got %v", model, got)
		}
	}
}

// Model loaded but not this session's model -> known idle (the server
// answered, the list was readable, the match simply did not hold).
func TestProbeModelServerRequestIdleWhenThisModelNotLoaded(t *testing.T) {
	srv := startFakeModelServer(t, []string{"some-other-model"}, false)
	if got := probeModelServerRequest(srv.URL, "ollama/glm-5:755b"); got != modelServerRequestIdle {
		t.Fatalf("a different loaded model must report idle for this session's model, got %v", got)
	}

	// No models loaded at all -> known idle.
	empty := startFakeModelServer(t, nil, false)
	if got := probeModelServerRequest(empty.URL, "ollama/glm-5:755b"); got != modelServerRequestIdle {
		t.Fatalf("an empty loaded-model list must report idle, got %v", got)
	}
}

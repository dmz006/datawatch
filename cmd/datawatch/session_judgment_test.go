// B124 — the verify/guardrail judgment pipeline was pointed at
// tool-capable backends (claude-code/opencode/goose) but never actually
// gave them tool access: resolveVerifierBackendModel's own doc comment
// documents the pre-existing stopgap ("session-only kinds ... fall
// through to the ollama default") as deliberate, but no one ever built
// the session-based path decompose already proved. These tests cover
// the new shared mechanism (runOneShotJSONSession, extracted from
// decomposeFnSession) and the decision function that routes to it.
package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/llm"
	"github.com/dmz006/datawatch/internal/llm/backends/aider"
	"github.com/dmz006/datawatch/internal/llm/backends/gemini"
	"github.com/dmz006/datawatch/internal/llm/backends/opencode"
	"github.com/dmz006/datawatch/internal/llm/backends/shell"
)

// registerAllLLMBackendsOnce mirrors main()'s unconditional backend
// registration (cmd/datawatch/main.go, "Always register configured
// backends regardless of Enabled flag") so isInferenceSessionKind tests
// see the same registry a real daemon would have. claude-code and goose
// self-register via their packages' own init(), which already ran by
// the time any test runs; the rest only register from inside main()'s
// setup, which these tests never call.
var registerAllLLMBackendsOnce sync.Once

func registerAllLLMBackendsForTest() {
	registerAllLLMBackendsOnce.Do(func() {
		llm.Register(aider.New(""))
		llm.Register(gemini.New(""))
		llm.Register(opencode.New(""))
		llm.Register(opencode.NewPrompt(""))
		llm.Register(opencode.NewACPWithTimeouts("", 0, 0, 0))
		llm.Register(shell.New(""))
	})
}

// B124 — the actual root cause of why nothing above ever fired live:
// resolveAskCompatibleKind must return the REAL resolved kind even when
// that kind isn't ask-compatible (the whole point of needsSessionJudgment
// needing it), not "" the way the original inline closure did. A fake
// resolver that always returns the real kind (like fakeAskResolver in
// verifier_diff_test.go) would never have caught this — this test
// exercises the real function against a real *inference.Registry.
func TestResolveAskCompatibleKind_ReturnsRealKindEvenWhenNotAskCompatible(t *testing.T) {
	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{Name: "claude-code", Kind: inference.KindClaudeCode}); err != nil {
		t.Fatalf("reg.Add: %v", err)
	}
	askCompatible := func(k string) bool { return k == "ollama" || k == "openwebui" }

	kind, _, err := resolveAskCompatibleKind(reg, "claude-code", askCompatible)
	if err == nil {
		t.Fatal("want an error for a non-ask-compatible kind")
	}
	if kind != "claude-code" {
		t.Fatalf("kind = %q, want the real resolved kind %q even on the error path — this is exactly the bug that made every verify/guardrail call configured for a tool-capable backend silently fall through to ollama with no error and no log line", kind, "claude-code")
	}
}

func TestResolveAskCompatibleKind_AskCompatiblePassesThrough(t *testing.T) {
	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{Name: "ollama-datawatch", Kind: inference.KindOllama, Model: "qwen3:1.7b"}); err != nil {
		t.Fatalf("reg.Add: %v", err)
	}
	askCompatible := func(k string) bool { return k == "ollama" || k == "openwebui" }

	kind, model, err := resolveAskCompatibleKind(reg, "ollama-datawatch", askCompatible)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kind != "ollama" || model != "qwen3:1.7b" {
		t.Fatalf("kind=%q model=%q, want ollama/qwen3:1.7b", kind, model)
	}
}

func TestResolveAskCompatibleKind_UnregisteredNamePassesThroughAsBareKind(t *testing.T) {
	reg := inference.NewRegistry()
	askCompatible := func(k string) bool { return k == "ollama" || k == "openwebui" }

	kind, model, err := resolveAskCompatibleKind(reg, "ollama", askCompatible)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if kind != "ollama" || model != "" {
		t.Fatalf("kind=%q model=%q, want the bare string passed through", kind, model)
	}
}

func TestNeedsSessionJudgment(t *testing.T) {
	isSession := func(k string) bool { return k == "claude-code" || k == "opencode" }
	cases := []struct {
		name       string
		kind       string
		projectDir string
		want       bool
	}{
		{"session-kind with project dir", "claude-code", "/tmp/worktree", true},
		{"session-kind without project dir falls back", "claude-code", "", false},
		{"ask-compatible kind never needs a session", "ollama", "/tmp/worktree", false},
		{"ask-compatible kind without project dir", "ollama", "", false},
		{"unknown kind without project dir", "mystery", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := needsSessionJudgment(c.kind, c.projectDir, isSession); got != c.want {
				t.Errorf("needsSessionJudgment(%q, %q) = %v, want %v", c.kind, c.projectDir, got, c.want)
			}
		})
	}
}

func TestIsInferenceSessionKind(t *testing.T) {
	registerAllLLMBackendsForTest()
	cases := map[string]bool{
		"claude-code":                  true,
		"opencode-acp":                 true,
		"opencode-prompt":              true,
		"goose":                        true,
		"aider":                        true,
		"gemini":                       true,
		"shell":                        true,
		"opencode":                     true, // the real opencode CLI tmux backend — a tool-using agent
		"ollama":                       false,
		"openwebui":                    false,
		"claude":                       false, // cloud API kind, ask-compatible via its own adapter
		"unregistered-mystery-backend": false,
	}
	for kind, want := range cases {
		if got := isInferenceSessionKind(kind); got != want {
			t.Errorf("isInferenceSessionKind(%q) = %v, want %v", kind, got, want)
		}
	}
}

func TestSessionJudgmentTask_WrapsPromptWithFileInstruction(t *testing.T) {
	task := sessionJudgmentTask("REPLY WITH JSON: {\"ok\":true}", "/work/proj", "/work/proj/.verify-abc.json")
	if !strings.Contains(task, "REPLY WITH JSON: {\"ok\":true}") {
		t.Error("session task dropped the original judgment prompt")
	}
	if !strings.Contains(task, "/work/proj") {
		t.Error("session task doesn't tell the session its own project dir")
	}
	if !strings.Contains(task, "/work/proj/.verify-abc.json") {
		t.Error("session task doesn't name the output file to write")
	}
	if !strings.Contains(strings.ToLower(task), "do not reply with the json directly") {
		t.Error("session task doesn't redirect the model away from an inline chat reply")
	}
}

// fakeSessionServer builds an httptest server standing in for the
// daemon's own /api/sessions/{start,kill,:id} endpoints, letting
// runOneShotJSONSession's spawn/poll/kill contract be tested without a
// real session.Manager. writeOutputOnPoll, when true, writes
// outputPath with outputContent the first time the poll endpoint is
// hit, simulating a session that finishes between two ticks.
func fakeSessionServer(t *testing.T, state string, outputPath, outputContent string, writeOnPoll bool) (*httptest.Server, *int) {
	t.Helper()
	polls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions/start", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "sess-test-1"})
	})
	mux.HandleFunc("/api/sessions/sess-test-1", func(w http.ResponseWriter, r *http.Request) {
		polls++
		if writeOnPoll && polls == 1 {
			_ = os.WriteFile(outputPath, []byte(outputContent), 0o600)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"state": state})
	})
	mux.HandleFunc("/api/sessions/kill", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &polls
}

// withFakeLoopback points loopbackBaseURL at srv for the duration of
// the test, restoring the real value on cleanup — same serverURL
// override convention daemon_api_url_test.go already uses.
func withFakeLoopback(t *testing.T, srv *httptest.Server) {
	t.Helper()
	origURL := serverURL
	t.Cleanup(func() { serverURL = origURL })
	serverURL = srv.URL
}

func TestRunOneShotJSONSession_SpawnPollRelocate(t *testing.T) {
	projectDir := t.TempDir()
	dataDir := t.TempDir()
	outputFile := filepath.Join(projectDir, ".test-output-abc.json")
	const want = `{"ok":true,"severity":"info","summary":"looks good"}`

	srv, polls := fakeSessionServer(t, "running", outputFile, want, true)
	withFakeLoopback(t, srv)

	cfg := &config.Config{}
	cfg.DataDir = dataDir

	got, err := runOneShotJSONSession(cfg, "prd-1", projectDir, "claude-code", "sonnet",
		"autonomous:test", "do the thing, write JSON to "+outputFile, outputFile, "test-output.json",
		10*time.Second)
	if err != nil {
		t.Fatalf("runOneShotJSONSession: %v", err)
	}
	if got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
	if *polls == 0 {
		t.Error("expected at least one poll of the session before the output file was found")
	}
	// Relocated out of projectDir — the worker's own directory must not
	// keep a copy (B124 follows the same relocation contract decompose
	// already used; see scratch.go).
	if _, statErr := os.Stat(outputFile); !os.IsNotExist(statErr) {
		t.Errorf("output file %s should have been relocated out of projectDir", outputFile)
	}
	if _, statErr := os.Stat(filepath.Join(dataDir, "autonomous", "scratch", "prd-1", "test-output.json")); statErr != nil {
		t.Errorf("expected relocated copy under scratch dir: %v", statErr)
	}
}

func TestRunOneShotJSONSession_ReusesExistingOutputOnRestart(t *testing.T) {
	projectDir := t.TempDir()
	dataDir := t.TempDir()
	outputFile := filepath.Join(projectDir, ".test-output-restart.json")
	const want = `{"ok":true,"severity":"info","summary":"from a prior attempt"}`
	if err := os.WriteFile(outputFile, []byte(want), 0o600); err != nil {
		t.Fatalf("seed output file: %v", err)
	}

	// No /api/sessions/start handler registered — a session spawn here
	// would fail the test with a connection/404 error, proving restart
	// recovery short-circuits before ever spawning.
	srv := httptest.NewServer(http.NewServeMux())
	t.Cleanup(srv.Close)
	withFakeLoopback(t, srv)

	cfg := &config.Config{}
	cfg.DataDir = dataDir

	got, err := runOneShotJSONSession(cfg, "prd-2", projectDir, "claude-code", "",
		"autonomous:test", "unused", outputFile, "test-output-restart.json", 10*time.Second)
	if err != nil {
		t.Fatalf("runOneShotJSONSession: %v", err)
	}
	if got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func TestRunOneShotJSONSession_FailedStateWithNoOutputIsAnError(t *testing.T) {
	projectDir := t.TempDir()
	dataDir := t.TempDir()
	outputFile := filepath.Join(projectDir, ".test-output-failed.json")

	srv, _ := fakeSessionServer(t, "failed", outputFile, "", false)
	withFakeLoopback(t, srv)

	cfg := &config.Config{}
	cfg.DataDir = dataDir

	_, err := runOneShotJSONSession(cfg, "prd-3", projectDir, "claude-code", "sonnet",
		"autonomous:test", "unused", outputFile, "test-output-failed.json", 10*time.Second)
	if err == nil {
		t.Fatal("expected an error when the session ends failed with no output file")
	}
	if !strings.Contains(err.Error(), "ended with state") {
		t.Errorf("error = %v, want it to name the terminal state", err)
	}
	assertErrorNamesBackendAndModel(t, err, "claude-code", "sonnet")
}

// TestRunOneShotJSONSession_CompleteStateWithNoOutputIsAnError covers the
// "complete" terminal branch: the session reports done but never wrote the
// output file, which must surface as an error naming the launch settings.
func TestRunOneShotJSONSession_CompleteStateWithNoOutputIsAnError(t *testing.T) {
	projectDir := t.TempDir()
	dataDir := t.TempDir()
	outputFile := filepath.Join(projectDir, ".test-output-complete.json")

	srv, _ := fakeSessionServer(t, "complete", outputFile, "", false)
	withFakeLoopback(t, srv)

	cfg := &config.Config{}
	cfg.DataDir = dataDir

	_, err := runOneShotJSONSession(cfg, "prd-5", projectDir, "pi-prompt", "prov/model:64k",
		"autonomous:test", "unused", outputFile, "test-output-complete.json", 10*time.Second)
	if err == nil {
		t.Fatal("expected an error when the session completes with no output file")
	}
	if !strings.Contains(err.Error(), "completed but output file") {
		t.Errorf("error = %v, want it to say the session completed without the output file", err)
	}
	assertErrorNamesBackendAndModel(t, err, "pi-prompt", "prov/model:64k")
}

// TestRunOneShotJSONSessionCaptureID_ReturnsSpawnedSessionID proves the
// spawned session id comes back alongside the content on the success path and
// alongside the error on the failed-state path, so a caller can name the
// session even when the judgment failed. fakeSessionServer always issues
// "sess-test-1" from its start endpoint.
func TestRunOneShotJSONSessionCaptureID_ReturnsSpawnedSessionID(t *testing.T) {
	const wantID = "sess-test-1"

	t.Run("success", func(t *testing.T) {
		projectDir := t.TempDir()
		dataDir := t.TempDir()
		outputFile := filepath.Join(projectDir, ".test-output-capture-ok.json")
		const want = `{"ok":true,"severity":"info","summary":"looks good"}`

		srv, _ := fakeSessionServer(t, "running", outputFile, want, true)
		withFakeLoopback(t, srv)

		cfg := &config.Config{}
		cfg.DataDir = dataDir

		got, id, err := runOneShotJSONSessionCaptureID(cfg, "prd-capture-ok", projectDir, "claude-code", "sonnet", "",
			"autonomous:test", "do the thing, write JSON to "+outputFile, outputFile, "test-output-capture-ok.json",
			10*time.Second)
		if err != nil {
			t.Fatalf("runOneShotJSONSessionCaptureID: %v", err)
		}
		if got != want {
			t.Errorf("content = %q, want %q", got, want)
		}
		if id != wantID {
			t.Errorf("sessionID = %q, want %q", id, wantID)
		}
	})

	t.Run("failed", func(t *testing.T) {
		projectDir := t.TempDir()
		dataDir := t.TempDir()
		outputFile := filepath.Join(projectDir, ".test-output-capture-failed.json")

		srv, _ := fakeSessionServer(t, "failed", outputFile, "", false)
		withFakeLoopback(t, srv)

		cfg := &config.Config{}
		cfg.DataDir = dataDir

		_, id, err := runOneShotJSONSessionCaptureID(cfg, "prd-capture-failed", projectDir, "claude-code", "sonnet", "",
			"autonomous:test", "unused", outputFile, "test-output-capture-failed.json",
			10*time.Second)
		if err == nil {
			t.Fatal("expected an error when the session ends failed with no output file")
		}
		if id != wantID {
			t.Errorf("sessionID = %q on failed path, want %q", id, wantID)
		}
	})
}

// assertErrorNamesBackendAndModel checks that a no-output error reports the
// backend and model the session start request carried, so an operator can
// tell from the recorded summary which selection produced it.
func assertErrorNamesBackendAndModel(t *testing.T, err error, backend, model string) {
	t.Helper()
	if !strings.Contains(err.Error(), "backend="+backend) {
		t.Errorf("error = %v, want it to name backend=%s", err, backend)
	}
	if !strings.Contains(err.Error(), "model="+`"`+model+`"`) {
		t.Errorf("error = %v, want it to name model=%q", err, model)
	}
}

// TestRunOneShotJSONSession_PiPromptStartBodyCarriesModelVerbatim proves
// the verifier selection's model reaches the session start request
// unchanged. The start handler records the decoded body; the pi --model
// flag itself is rendered from that field by the pi backend tests.
func TestRunOneShotJSONSession_PiPromptStartBodyCarriesModelVerbatim(t *testing.T) {
	projectDir := t.TempDir()
	dataDir := t.TempDir()
	outputFile := filepath.Join(projectDir, ".test-output-pi-model.json")
	const want = `{"ok":true,"severity":"info","summary":"verified"}`
	const model = "prov/model:64k"

	var startBody map[string]any
	polls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions/start", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&startBody); err != nil {
			t.Errorf("decode start body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "sess-test-1"})
	})
	mux.HandleFunc("/api/sessions/sess-test-1", func(w http.ResponseWriter, r *http.Request) {
		polls++
		if polls == 1 {
			_ = os.WriteFile(outputFile, []byte(want), 0o600)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"state": "running"})
	})
	mux.HandleFunc("/api/sessions/kill", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	withFakeLoopback(t, srv)

	cfg := &config.Config{}
	cfg.DataDir = dataDir

	got, err := runOneShotJSONSessionWithEffort(cfg, "prd-4", projectDir, "pi-prompt", model, "",
		"autonomous:verify", "verify the thing", outputFile, "verify-pi.json", 10*time.Second, "")
	if err != nil {
		t.Fatalf("runOneShotJSONSessionWithEffort: %v", err)
	}
	if got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
	if startBody == nil {
		t.Fatal("start handler never received a request body")
	}
	if startBody["backend"] != "pi-prompt" {
		t.Errorf("start body backend = %v, want \"pi-prompt\"", startBody["backend"])
	}
	if startBody["model"] != model {
		t.Errorf("start body model = %v, want %q verbatim", startBody["model"], model)
	}
}

// The one-shot timeout error is what operators see after the
// verifier(session)/guardrail(session) prefix, so it must name the backend,
// the exact model and the limit — not just "timed out after 10m0s".
func TestSessionOneShotTimeoutMessage(t *testing.T) {
	msg := sessionOneShotTimeoutMessage("autonomous:verify:task-1", "sess-42", "ollama", "qwen2.5:14b", 30*time.Minute)
	for _, want := range []string{
		"autonomous:verify:task-1",
		"sess-42",
		"backend=ollama",
		"model=qwen2.5:14b",
		"30m0s",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("sessionOneShotTimeoutMessage() = %q, missing %q", msg, want)
		}
	}
}

// TestRunOneShotJSONSession_DecomposeWaitingInputIsKilledBeforeTimeout covers
// the decompose one-shot waiting_input debounce: a planning session that
// sits in waiting_input (asking a question no one will answer) is killed and
// reported in the no-output family after the debounce window, long before
// its timeout, instead of polling until the timeout.
func TestRunOneShotJSONSession_DecomposeWaitingInputIsKilledBeforeTimeout(t *testing.T) {
	projectDir := t.TempDir()
	dataDir := t.TempDir()
	outputFile := filepath.Join(projectDir, ".test-output-waiting.json")

	var mu sync.Mutex
	kills := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions/start", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "sess-test-1"})
	})
	mux.HandleFunc("/api/sessions/sess-test-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"state": "waiting_input"})
	})
	mux.HandleFunc("/api/sessions/kill", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		kills++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	withFakeLoopback(t, srv)

	cfg := &config.Config{}
	cfg.DataDir = dataDir
	// Shrink the task-delivery grace (decomposeWaitingInputTick exempts the
	// stall check for the delivery window; the default budget is ~195 s and
	// would outlast the timeout below): window=1 s, attempts=1 gives a 1 s
	// grace, so the debounce still fires after a few 3 s ticks.
	cfg.Session.TaskDeliveryWindowSeconds = 1
	cfg.Session.TaskDeliveryAttempts = 1

	const timeout = 60 * time.Second
	start := time.Now()
	_, err := runOneShotJSONSession(cfg, "prd-wait", projectDir, "claude-code", "sonnet",
		decomposeSessionName, "unused", outputFile, "decompose-output.json", timeout)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error when a decompose session stays in waiting_input")
	}
	var noOutput *autonomouspkg.OneShotNoOutputError
	if !errors.As(err, &noOutput) {
		t.Fatalf("error = %v (%T), want *autonomous.OneShotNoOutputError", err, err)
	}
	if !strings.Contains(err.Error(), "ended with state waiting_input without producing output") {
		t.Errorf("error = %v, want the waiting_input no-output message", err)
	}
	assertErrorNamesBackendAndModel(t, err, "claude-code", "sonnet")
	if elapsed >= timeout/2 {
		t.Errorf("waiting_input session polled for %v, want the debounce to stop it well before the %v timeout", elapsed, timeout)
	}
	mu.Lock()
	defer mu.Unlock()
	if kills != 1 {
		t.Errorf("kill calls = %d, want 1 (stuck session must be killed)", kills)
	}
}

// A guardrail judgment reads its output with PeekProjectFile, so the
// project-directory copy must survive the call. The content is still copied to
// the scratch dir so the durable copy exists.
func TestRunOneShotJudgeSession_KeepsProjectOutputAndCopiesToScratch(t *testing.T) {
	projectDir := t.TempDir()
	dataDir := t.TempDir()
	outputFile := filepath.Join(projectDir, ".guardrail-output-judge.json")
	const want = `{"outcome":"pass","severity":"info","summary":"looks good"}`

	srv, polls := fakeSessionServer(t, "running", outputFile, want, true)
	withFakeLoopback(t, srv)

	cfg := &config.Config{}
	cfg.DataDir = dataDir

	got, id, err := runOneShotJudgeSessionCaptureID(cfg, "prd-judge", projectDir, "claude-code", "sonnet", "",
		"autonomous:guardrail:unit-1", "judge it, write JSON to "+outputFile, outputFile, "guardrail-unit-1-sec.json",
		10*time.Second, "")
	if err != nil {
		t.Fatalf("runOneShotJudgeSessionCaptureID: %v", err)
	}
	if got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
	if id != "sess-test-1" {
		t.Errorf("session id = %q, want sess-test-1", id)
	}
	if *polls == 0 {
		t.Error("expected at least one poll of the session before the output file was found")
	}
	if _, statErr := os.Stat(outputFile); statErr != nil {
		t.Errorf("project-dir output %s must survive the judge read: %v", outputFile, statErr)
	}
	scratchCopy := filepath.Join(dataDir, "autonomous", "scratch", "prd-judge", "guardrail-unit-1-sec.json")
	copied, statErr := os.ReadFile(scratchCopy)
	if statErr != nil {
		t.Fatalf("expected scratch copy at %s: %v", scratchCopy, statErr)
	}
	if string(copied) != want {
		t.Errorf("scratch copy = %q, want %q", string(copied), want)
	}
}

// A judged output already on disk (restart recovery, or a retry after a failed
// decision save) must be returned without starting a new judgment session, and
// it must stay on disk for the next call.
func TestRunOneShotJudgeSession_ReusesPresentOutputWithoutStarting(t *testing.T) {
	projectDir := t.TempDir()
	dataDir := t.TempDir()
	outputFile := filepath.Join(projectDir, ".guardrail-output-judged.json")
	const want = `{"outcome":"pass","severity":"info","summary":"judged earlier"}`
	if err := os.WriteFile(outputFile, []byte(want), 0o600); err != nil {
		t.Fatalf("seed output file: %v", err)
	}

	var mu sync.Mutex
	starts := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions/start", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		starts++
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	withFakeLoopback(t, srv)

	cfg := &config.Config{}
	cfg.DataDir = dataDir

	for call := 1; call <= 2; call++ {
		got, id, err := runOneShotJudgeSessionCaptureID(cfg, "prd-judged", projectDir, "claude-code", "sonnet", "",
			"autonomous:guardrail:unit-2", "unused", outputFile, "guardrail-unit-2-sec.json",
			10*time.Second, "")
		if err != nil {
			t.Fatalf("call %d: runOneShotJudgeSessionCaptureID: %v", call, err)
		}
		if got != want {
			t.Errorf("call %d: content = %q, want %q", call, got, want)
		}
		if id != "" {
			t.Errorf("call %d: session id = %q, want empty (no session was started)", call, id)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if starts != 0 {
		t.Errorf("start requests = %d, want 0 (a judged output on disk must be reused, not re-judged)", starts)
	}
	if _, statErr := os.Stat(outputFile); statErr != nil {
		t.Errorf("project-dir output %s must still exist after reuse: %v", outputFile, statErr)
	}
}

// fakeSessionServerCaptureStart is fakeSessionServer plus capture of the
// /api/sessions/start request body, so a test can assert exactly what the
// daemon was asked to start with.
func fakeSessionServerCaptureStart(t *testing.T, state string, outputPath, outputContent string, writeOnPoll bool) (*httptest.Server, map[string]any) {
	t.Helper()
	var mu sync.Mutex
	captured := map[string]any{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions/start", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Errorf("decoding /api/sessions/start body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "sess-test-1"})
	})
	polls := 0
	mux.HandleFunc("/api/sessions/sess-test-1", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		polls++
		if writeOnPoll && polls == 1 {
			_ = os.WriteFile(outputPath, []byte(outputContent), 0o600)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"state": state})
	})
	mux.HandleFunc("/api/sessions/kill", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, captured
}

// assertPlanningStartBody checks the fields a planning/judgment start
// request must carry: skip_snapshot=true (no pre-session snapshot commit and
// no completion commit in the PRD's repo — the session's own output files
// would otherwise get staged and committed), plus the one-shot/PRD linkage
// the daemon's snapshot gate keys on.
func assertPlanningStartBody(t *testing.T, startBody map[string]any, prdID string) {
	t.Helper()
	if v, ok := startBody["skip_snapshot"].(bool); !ok || !v {
		t.Errorf("start request skip_snapshot = %v (%T), want true — without it the daemon snapshot-commits the planning session's own output", startBody["skip_snapshot"], startBody["skip_snapshot"])
	}
	if v, ok := startBody["one_shot"].(bool); !ok || !v {
		t.Errorf("start request one_shot = %v (%T), want true", startBody["one_shot"], startBody["one_shot"])
	}
	if v, ok := startBody["prd_id"].(string); !ok || v != prdID {
		t.Errorf("start request prd_id = %v, want %q", startBody["prd_id"], prdID)
	}
}

// The start request built for the decompose and judge one-shots must carry
// skip_snapshot=true (via the same runOneShotJSONSessionWithReader seam
// both go through), so the daemon never stages or commits a planning
// session's working tree.
func TestPlanningSessionStartRequestSkipsSnapshot(t *testing.T) {
	t.Run("decompose", func(t *testing.T) {
		projectDir := t.TempDir()
		dataDir := t.TempDir()
		outputFile := filepath.Join(projectDir, ".decompose-output-abc.json")

		srv, startBody := fakeSessionServerCaptureStart(t, "running", outputFile, `{"tasks":[]}`, true)
		withFakeLoopback(t, srv)

		cfg := &config.Config{}
		cfg.DataDir = dataDir

		got, id, err := runOneShotJSONSessionCaptureID(cfg, "prd-decomp-skip", projectDir, "claude-code", "sonnet", "",
			decomposeSessionName, "decompose it, write JSON to "+outputFile, outputFile, "decompose-output.json",
			10*time.Second)
		if err != nil {
			t.Fatalf("runOneShotJSONSessionCaptureID: %v", err)
		}
		if got == "" || id != "sess-test-1" {
			t.Fatalf("content=%q id=%q, want content and the spawned id", got, id)
		}
		assertPlanningStartBody(t, startBody, "prd-decomp-skip")
	})

	t.Run("judge", func(t *testing.T) {
		projectDir := t.TempDir()
		dataDir := t.TempDir()
		outputFile := filepath.Join(projectDir, ".guardrail-output-skip.json")
		const want = `{"outcome":"pass","severity":"info","summary":"ok"}`

		srv, startBody := fakeSessionServerCaptureStart(t, "running", outputFile, want, true)
		withFakeLoopback(t, srv)

		cfg := &config.Config{}
		cfg.DataDir = dataDir

		got, id, err := runOneShotJudgeSessionCaptureID(cfg, "prd-judge-skip", projectDir, "claude-code", "sonnet", "",
			"autonomous:guardrail:skip-unit", "judge it, write JSON to "+outputFile, outputFile, "guardrail-skip-unit-sec.json",
			10*time.Second, "")
		if err != nil {
			t.Fatalf("runOneShotJudgeSessionCaptureID: %v", err)
		}
		if got != want || id != "sess-test-1" {
			t.Fatalf("content=%q id=%q, want %q and the spawned id", got, id, want)
		}
		assertPlanningStartBody(t, startBody, "prd-judge-skip")
	})
}

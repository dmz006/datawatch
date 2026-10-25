package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// noOutputReader stands in for the output-file reader: there is no output
// file, so it always reports absent.
func noOutputReader(_, _, _, _ string) (string, bool) { return "", false }

// startFakeDaemonForOneShot stands up the daemon endpoints the one-shot
// wait loop reads: /api/sessions/start (returns a fixed id),
// /api/sessions/{id} (the session stays in the configured state and never
// writes an output file), /api/sessions/kill (the deferred kill), and
// /api/output (a pane tail that never changes — a constant pane marker).
func startFakeDaemonForOneShot(t *testing.T, state string) *httptest.Server {
	t.Helper()
	const sessionID = "st-fake"
	mux := http.NewServeMux()
	mux.HandleFunc("/api/sessions/start", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":%q}`, sessionID)
	})
	mux.HandleFunc("/api/sessions/"+sessionID, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":%q,"state":%q}`, sessionID, state)
	})
	mux.HandleFunc("/api/sessions/kill", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/output", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, "a static pane tail that never changes")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// pointLoopbackAt redirects loopback requests made during the test (the
// one-shot loop's daemon base URL) to baseURL.
func pointLoopbackAt(t *testing.T, baseURL string) {
	t.Helper()
	prev := serverURL
	serverURL = baseURL
	t.Cleanup(func() { serverURL = prev })
}

// verifierLoopConfig builds a config with a short no-progress limit whose
// watchdog would otherwise (and does, in the silent case) trip well before
// the test's session cap; the model-server host is whatever the probe
// needs to say.
func verifierLoopConfig(modelServerHost string) *config.Config {
	return &config.Config{
		Ollama:     config.OllamaConfig{Host: modelServerHost},
		Autonomous: config.AutonomousConfig{VerifierNoProgressTimeoutSeconds: 2},
	}
}

// A silent verifier session (no output file, no pane change, no live
// runner process) whose model server cannot be reached must end at the
// no-progress limit: the unknown probe must not spare the session, must
// not crash the loop, and the error must be the no-output family naming
// the watchdog.
func TestVerifierWaitLoopSilentEndsAtLimitWhenProbeUnknown(t *testing.T) {
	daemon := startFakeDaemonForOneShot(t, "running")
	pointLoopbackAt(t, daemon.URL)

	// Closed immediately: connection refused → the probe reports unknown.
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusInternalServerError)
	}))
	dead.Close()

	projectDir := t.TempDir()
	outputFile := filepath.Join(projectDir, "never-written.json")
	cfg := verifierLoopConfig(dead.URL)

	start := time.Now()
	_, _, err := runOneShotJSONSessionWithReader(cfg, "prd-1", projectDir, "claude-code", "glm-5:755b", "high",
		"autonomous:verify:task-1", "verify", outputFile, "judge-1.json", 30*time.Second, noOutputReader, "")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("a silent verifier session must end at the no-progress limit, got success")
	}
	noOutput, ok := err.(*autonomous.OneShotNoOutputError)
	if !ok {
		t.Fatalf("want *autonomous.OneShotNoOutputError, got %T (%v)", err, err)
	}
	want := autonomous.VerifierWatchdogMessage(2 * time.Second)
	if noOutput.Msg != want {
		t.Fatalf("want the watchdog message %q, got %q", want, noOutput.Msg)
	}
	if elapsed > 20*time.Second {
		t.Fatalf("the watchdog should end the session at the limit (2s), not the session cap (30s): took %v", elapsed)
	}
}

// A silent verifier session whose model server reports its model loaded
// and serving (i.e. an active request for the session's model) must keep
// living past the no-progress limit: the confirmed active request counts
// as progress. The session ends only on the session cap, and the error
// must not name the watchdog.
func TestVerifierWaitLoopActiveRequestKeepsSilentSessionAlive(t *testing.T) {
	daemon := startFakeDaemonForOneShot(t, "running")
	pointLoopbackAt(t, daemon.URL)

	// Ollama-shaped: the session's model is loaded and serving.
	modelSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tags", "/api/ps":
			_, _ = fmt.Fprint(w, `{"models":[{"name":"glm-5:755b"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(modelSrv.Close)

	projectDir := t.TempDir()
	outputFile := filepath.Join(projectDir, "never-written.json")
	cfg := verifierLoopConfig(modelSrv.URL)

	_, _, err := runOneShotJSONSessionWithReader(cfg, "prd-1", projectDir, "claude-code", "ollama/glm-5:755b", "high",
		"autonomous:verify:task-2", "verify", outputFile, "judge-2.json", 8*time.Second, noOutputReader, "")
	if err == nil {
		t.Fatal("the session never wrote output; the loop must not report success")
	}
	noOutput, ok := err.(*autonomous.OneShotNoOutputError)
	if !ok {
		t.Fatalf("want *autonomous.OneShotNoOutputError, got %T (%v)", err, err)
	}
	if strings.Contains(noOutput.Msg, "no-progress watchdog") {
		t.Fatalf("a confirmed active model-server request must keep a silent session alive, but the watchdog ended it: %v", noOutput.Msg)
	}
}

// The no-progress watchdog is verifier-scoped: decompose and worker
// sessions keep polling to their session cap — it must not end them
// early, and the error must be the session-cap timeout, not a watchdog
// verdict.
func TestVerifierWaitLoopLeavesDecomposeAndWorkerSessionsAlone(t *testing.T) {
	for _, sessionName := range []string{"autonomous:decompose:prd-1", "autonomous:worker:task-9"} {
		t.Run(sessionName, func(t *testing.T) {
			daemon := startFakeDaemonForOneShot(t, "running")
			pointLoopbackAt(t, daemon.URL)

			projectDir := t.TempDir()
			outputFile := filepath.Join(projectDir, "never-written.json")
			cfg := verifierLoopConfig("") // no model server host: probe is unknown everywhere

			_, _, err := runOneShotJSONSessionWithReader(cfg, "prd-1", projectDir, "claude-code", "glm-5:755b", "high",
				sessionName, "run", outputFile, "judge-3.json", 6*time.Second, noOutputReader, "")
			if err == nil {
				t.Fatal("the session never wrote output; the loop must not report success")
			}
			noOutput, ok := err.(*autonomous.OneShotNoOutputError)
			if !ok {
				t.Fatalf("want *autonomous.OneShotNoOutputError, got %T (%v)", err, err)
			}
			if strings.Contains(noOutput.Msg, "no-progress watchdog") {
				t.Fatalf("the no-progress watchdog must not end a %s session: %v", sessionName, noOutput.Msg)
			}
		})
	}
}

package main

// `datawatch config set` for the attempt-cost keys: a valid value is stored by
// the daemon, and an invalid one is refused with the daemon's message and a
// non-nil error (the root command exits 1 on any error). The fake daemon
// applies the same shared validator the real PUT /api/config handler runs, so
// the refusal text here is the text a user sees.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// attemptCostFakeDaemon is a stateful stand-in for PUT /api/config that keeps
// the attempt-cost settings and refuses a write the shared validator rejects.
type attemptCostFakeDaemon struct {
	t        *testing.T
	fa       map[string]config.FirstAttemptChoice
	escalate int
	retry    int
	puts     int
	lastBody string
}

func newAttemptCostFakeDaemon(t *testing.T) (*attemptCostFakeDaemon, *httptest.Server) {
	t.Helper()
	d := &attemptCostFakeDaemon{t: t, fa: map[string]config.FirstAttemptChoice{},
		escalate: config.DefaultFirstAttemptEscalateAfterFailures,
		retry:    config.DefaultRetryResumeMaxContextTokens}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/config" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		d.puts++
		d.lastBody = string(raw)
		var patch map[string]any
		if err := json.Unmarshal(raw, &patch); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		next := config.AutonomousConfig{FirstAttempt: map[string]config.FirstAttemptChoice{}}
		for k, v := range d.fa {
			next.FirstAttempt[k] = v
		}
		next.FirstAttemptEscalateAfterFailures = d.escalate
		next.RetryResumeMaxContextTokens = d.retry
		for k, v := range patch {
			switch {
			case strings.HasPrefix(k, "autonomous.first_attempt."):
				parts := strings.Split(strings.TrimPrefix(k, "autonomous.first_attempt."), ".")
				s, _ := v.(string)
				c := next.FirstAttempt[parts[0]]
				if parts[1] == "backend" {
					c.Backend = s
				} else {
					c.Model = s
				}
				next.FirstAttempt[parts[0]] = c
			case k == "autonomous.first_attempt_escalate_after_failures" || k == "autonomous.retry_resume_max_context_tokens":
				f, isNum := v.(float64)
				if !isNum {
					http.Error(w, k+": expected a whole number", http.StatusBadRequest)
					return
				}
				if k == "autonomous.first_attempt_escalate_after_failures" {
					next.FirstAttemptEscalateAfterFailures = int(f)
				} else {
					next.RetryResumeMaxContextTokens = int(f)
				}
			}
		}
		if err := config.ValidateAttemptCostSettings(next); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		d.fa = next.FirstAttempt
		d.escalate = next.FirstAttemptEscalateAfterFailures
		d.retry = next.RetryResumeMaxContextTokens
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	old := serverURL
	serverURL = srv.URL
	t.Cleanup(func() { serverURL = old })
	return d, srv
}

func TestAttemptCostConfigSetAcceptsValidValues(t *testing.T) {
	d, _ := newAttemptCostFakeDaemon(t)
	cfg := &config.Config{}
	// pi-prompt needs a model, so the model is set before the backend.
	if err := runConfigSet(cfg, "autonomous.first_attempt.docs.model", "qwen3.8:27b"); err != nil {
		t.Fatalf("model set: %v", err)
	}
	if err := runConfigSet(cfg, "autonomous.first_attempt.docs.backend", "pi-prompt"); err != nil {
		t.Fatalf("backend set: %v", err)
	}
	if got := d.fa["docs"]; got.Backend != "pi-prompt" || got.Model != "qwen3.8:27b" {
		t.Errorf("stored docs choice = %+v, want pi-prompt / qwen3.8:27b", got)
	}
	if err := runConfigSet(cfg, "autonomous.first_attempt_escalate_after_failures", "2"); err != nil {
		t.Fatalf("escalate set: %v", err)
	}
	if err := runConfigSet(cfg, "autonomous.retry_resume_max_context_tokens", "80000"); err != nil {
		t.Fatalf("retry set: %v", err)
	}
	if d.escalate != 2 || d.retry != 80000 {
		t.Errorf("stored escalate=%d retry=%d, want 2 and 80000", d.escalate, d.retry)
	}
}

func TestAttemptCostConfigSetRefusesInvalidValues(t *testing.T) {
	cases := []struct {
		name    string
		key     string
		value   string
		wantMsg string
	}{
		{"unknown kind", "autonomous.first_attempt.bogus.backend", "pi", `unknown task kind "bogus"`},
		{"model-needing backend without model", "autonomous.first_attempt.docs.backend", "pi-prompt", "autonomous.first_attempt.docs.model: backend \"pi-prompt\" needs a model"},
		{"escalate above range", "autonomous.first_attempt_escalate_after_failures", "25", "autonomous.first_attempt_escalate_after_failures: 25 is invalid"},
		{"escalate not a number", "autonomous.first_attempt_escalate_after_failures", "many", "expected a whole number"},
		{"retry negative", "autonomous.retry_resume_max_context_tokens", "-1", "autonomous.retry_resume_max_context_tokens: -1 is invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, _ := newAttemptCostFakeDaemon(t)
			err := runConfigSet(&config.Config{}, tc.key, tc.value)
			if err == nil {
				t.Fatalf("runConfigSet(%s, %s) = nil, want refusal", tc.key, tc.value)
			}
			if !strings.Contains(err.Error(), "HTTP 400") || !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("err = %v, want HTTP 400 carrying %q", err, tc.wantMsg)
			}
			if d.escalate != config.DefaultFirstAttemptEscalateAfterFailures || len(d.fa) != 0 {
				t.Errorf("refused write changed stored state: escalate=%d fa=%v", d.escalate, d.fa)
			}
		})
	}
}

func TestAttemptCostConfigSetRefusedScalarKeepsDaemonMessageWithoutRetry(t *testing.T) {
	d, _ := newAttemptCostFakeDaemon(t)
	err := runConfigSet(&config.Config{}, "autonomous.retry_resume_max_context_tokens", "3000000")
	if err == nil || !strings.Contains(err.Error(), "3000000 is invalid") {
		t.Fatalf("err = %v, want the daemon's range message", err)
	}
	if d.puts != 1 {
		t.Errorf("PUT count = %d, want 1 (a valid-JSON refusal is not re-quoted and retried)", d.puts)
	}
	if !strings.Contains(d.lastBody, strconv.Itoa(3000000)) {
		t.Errorf("last body = %s", d.lastBody)
	}
}

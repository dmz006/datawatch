// BL103 — validator check-logic tests.

package validator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeParent serves a tiny REST surface mimicking the daemon's
// /api/agents/{id} + /api/agents/audit endpoints.
type fakeParent struct {
	mu          sync.Mutex
	agent       map[string]any
	auditEvents []map[string]any
	reportErr   bool
	reported    map[string]any
}

func (f *fakeParent) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/agents/audit"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"path":   "/tmp/audit.jsonl",
				"events": f.auditEvents,
			})
		case strings.HasSuffix(r.URL.Path, "/result"):
			if f.reportErr {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			_ = json.NewDecoder(r.Body).Decode(&f.reported)
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(r.URL.Path, "/api/agents/"):
			_ = json.NewEncoder(w).Encode(f.agent)
		default:
			http.NotFound(w, r)
		}
	})
}

func TestValidate_PassWhenAllChecksGreen(t *testing.T) {
	fp := &fakeParent{
		agent: map[string]any{
			"id":    "w1",
			"state": "stopped",
			"task":  "build the thing",
			"result": map[string]any{
				"status":  "ok",
				"summary": "built",
			},
		},
		auditEvents: []map[string]any{
			{"event": "spawn", "agent_id": "w1"},
			{"event": "memory_save", "agent_id": "w1"},
			{"event": "terminate", "agent_id": "w1", "state": "stopped"},
		},
	}
	srv := httptest.NewServer(fp.handler(t))
	defer srv.Close()

	res, err := Validate(context.Background(), Config{
		ParentURL: srv.URL, WorkerID: "w1", HTTP: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict != VerdictPass {
		t.Errorf("verdict=%s reasons=%v", res.Verdict, res.Reasons)
	}
}

func TestValidate_FailWhenNoResult(t *testing.T) {
	fp := &fakeParent{
		agent: map[string]any{
			"id":    "w1",
			"state": "stopped",
			"task":  "build",
		},
		auditEvents: []map[string]any{},
	}
	srv := httptest.NewServer(fp.handler(t))
	defer srv.Close()
	res, _ := Validate(context.Background(), Config{
		ParentURL: srv.URL, WorkerID: "w1", HTTP: srv.Client(),
	})
	if res.Verdict != VerdictFail {
		t.Errorf("verdict=%s want fail; reasons=%v", res.Verdict, res.Reasons)
	}
}

func TestValidate_InconclusiveWhenSomeSignalsMissing(t *testing.T) {
	// Result is OK but no audit events for memory writes.
	fp := &fakeParent{
		agent: map[string]any{
			"id":    "w1",
			"state": "stopped",
			"task":  "scan only",
			"result": map[string]any{"status": "ok"},
		},
		auditEvents: []map[string]any{
			{"event": "spawn", "agent_id": "w1"},
			{"event": "terminate", "agent_id": "w1", "state": "stopped"},
		},
	}
	srv := httptest.NewServer(fp.handler(t))
	defer srv.Close()
	res, _ := Validate(context.Background(), Config{
		ParentURL: srv.URL, WorkerID: "w1", HTTP: srv.Client(),
	})
	if res.Verdict != VerdictInconclusive {
		t.Errorf("verdict=%s want inconclusive; reasons=%v", res.Verdict, res.Reasons)
	}
}

func TestValidate_InconclusiveOnAgentFetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	res, _ := Validate(context.Background(), Config{
		ParentURL: srv.URL, WorkerID: "w1", HTTP: srv.Client(),
	})
	if res.Verdict != VerdictInconclusive {
		t.Errorf("verdict=%s want inconclusive on fetch error", res.Verdict)
	}
}

func TestValidate_RequiresWorkerID(t *testing.T) {
	if _, err := Validate(context.Background(), Config{ParentURL: "http://x"}); err == nil {
		t.Error("expected error for missing worker_id")
	}
}

func TestValidate_RequiresParentURL(t *testing.T) {
	if _, err := Validate(context.Background(), Config{WorkerID: "w"}); err == nil {
		t.Error("expected error for missing parent_url")
	}
}

func TestReport_PostsToCorrectEndpoint(t *testing.T) {
	fp := &fakeParent{}
	srv := httptest.NewServer(fp.handler(t))
	defer srv.Close()
	res := &Result{
		Verdict:  VerdictPass,
		Reasons:  []string{"all checks ok"},
		WorkerID: "w1",
	}
	err := res.Report(context.Background(),
		Config{ParentURL: srv.URL, HTTP: srv.Client()},
		"validator-agent-id")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := fp.reported["status"].(string); got != "pass" {
		t.Errorf("reported status=%v want pass", fp.reported["status"])
	}
}

func TestReport_RequiresValidatorAgentID(t *testing.T) {
	res := &Result{}
	if err := res.Report(context.Background(),
		Config{ParentURL: "http://x"}, ""); err == nil {
		t.Error("expected error for missing validator_agent_id")
	}
}

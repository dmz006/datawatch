// Model/backend refusal over REST. The handlers run against a real
// autonomous.Manager, so a refused pair reaches the handler as the
// setter's own error and the status comes from modelBackendErrStatus.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func TestModelBackendRefusalRESTReturns400(t *testing.T) {
	m, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("refusal spec", t.TempDir(), "claude-code", "claude-opus-5-5", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	prd.Story = []autonomous.Story{{ID: "s1", Title: "story", Tasks: []autonomous.Task{{ID: "t1", Title: "task"}}}}
	prd.Status = autonomous.PRDNeedsReview
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	srv := &Server{autonomousMgr: newTestAPI(m)}

	createBody, _ := json.Marshal(map[string]string{
		"spec":        "refusal spec",
		"project_dir": t.TempDir(),
		"backend":     "pi",
		"model":       "claude-haiku-5-5",
	})
	base := "/api/autonomous/prds/" + prd.ID + "/"
	cases := []struct {
		name, path, body string
	}{
		{"create", "/api/autonomous/prds", string(createBody)},
		{"set_llm", base + "set_llm", `{"backend":"pi","model":"claude-haiku-5-5"}`},
		{"set_story_llm", base + "set_story_llm", `{"story_id":"s1","backend":"pi","model":"claude-haiku-5-5"}`},
		{"set_task_llm", base + "set_task_llm", `{"task_id":"t1","backend":"pi","model":"claude-haiku-5-5"}`},
		{"set_task_verification_llm", base + "set_task_verification_llm",
			`{"task_id":"t1","verification_backend":"pi","verification_model":"claude-haiku-5-5"}`},
		{"set_story_verification_llm", base + "set_story_verification_llm",
			`{"story_id":"s1","verification_backend":"pi","verification_model":"claude-haiku-5-5"}`},
		{"set_verification_llm", base + "set_verification_llm",
			`{"verification_backend":"pi","verification_model":"claude-haiku-5-5"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			method := http.MethodPost
			req := httptest.NewRequest(method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			srv.handleAutonomousPRDs(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("%s: status %d, want 400; body %s", tc.name, rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), autonomous.ErrModelBackendMismatch.Error()) {
				t.Fatalf("%s: body %q does not carry the refusal text", tc.name, rr.Body.String())
			}
		})
	}

	// The refused calls left the PRD's worker pair untouched.
	stored, _ := m.Store().GetPRD(prd.ID)
	if stored.Backend != "claude-code" || stored.Model != "claude-opus-5-5" {
		t.Fatalf("refused REST calls changed the PRD pair to %q/%q", stored.Backend, stored.Model)
	}

	// Control: a pair the backend can serve is accepted with 200.
	ok := httptest.NewRequest(http.MethodPost, base+"set_task_llm",
		strings.NewReader(`{"task_id":"t1","backend":"claude-code","model":"claude-haiku-5-5"}`))
	ok.Header.Set("Content-Type", "application/json")
	okRR := httptest.NewRecorder()
	srv.handleAutonomousPRDs(okRR, ok)
	if okRR.Code != http.StatusOK {
		t.Fatalf("valid set_task_llm: status %d, want 200; body %s", okRR.Code, okRR.Body.String())
	}
}

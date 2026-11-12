// Model-pull consent gate — server-level tests for the two existing pull
// paths (compute-node model pull and the marketplace pull task it runs),
// the consent write surfaces (PUT /api/config and PUT /api/autonomous/config),
// and the audit trail every pull attempt and consent change leaves.
//
// Fakes only: the Ollama node is an httptest server that records hits and
// streams a canned success; nothing is ever actually pulled.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
)

// newFakeOllama stands in for a compute node's Ollama server. It records hit
// counts (a refused pull must produce zero hits) and accepts only the pull
// POST its node is said to serve.
func newFakeOllama(t *testing.T, hits *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		if r.Method != http.MethodPost || r.URL.Path != "/api/pull" {
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = fmt.Fprintf(w, `{"status":"pulling manifest","total":100,"completed":10}
{"status":"success"}
`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newModelPullServer wires one fresh server: a compute registry, an audit
// log, and a fake Ollama node registered under the name "gpu-a".
func newModelPullServer(t *testing.T, consent config.ModelPullConsent) (*Server, *audit.Log, *int) {
	t.Helper()
	reg, err := compute.NewRegistry(filepath.Join(t.TempDir(), "nodes.json"))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	al, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { _ = al.Close() })

	hits := 0
	fake := newFakeOllama(t, &hits)

	s := &Server{
		cfg:        config.DefaultConfig(),
		computeReg: reg,
		auditLog:   al,
	}
	s.cfg.Autonomous.ModelPullConsent = consent

	body, _ := json.Marshal(map[string]any{
		"name":    "gpu-a",
		"kind":    "remote",
		"address": fake.URL,
	})
	w := httptest.NewRecorder()
	s.handleComputeNodes(w, httptest.NewRequest(http.MethodPost, "/api/compute/nodes?probe=skip", strings.NewReader(string(body))))
	if w.Code != http.StatusOK {
		t.Fatalf("add compute node: %d %s", w.Code, w.Body.String())
	}
	return s, al, &hits
}

func pullAuditByAction(t *testing.T, al *audit.Log, action string) []audit.Entry {
	t.Helper()
	got, err := al.Read(audit.QueryFilter{Action: action})
	if err != nil {
		t.Fatalf("audit read: %v", err)
	}
	return got
}

// waitPullTask polls the package-level pull task registry for a terminal
// status instead of racing the worker goroutine with sleeps.
func waitPullTask(t *testing.T, id string) *PullTask {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if tt, ok := globalPullTasks.get(id); ok && (tt.Status == "done" || tt.Status == "failed") {
			return tt
		}
		time.Sleep(20 * time.Millisecond)
	}
	if tt, ok := globalPullTasks.get(id); ok {
		t.Fatalf("task %s stuck in %q (error: %q), wait timed out", tt.ID, tt.Status, tt.Error)
	}
	t.Fatalf("task %s never appeared", id)
	return nil
}

func TestModelPullRefusedWithoutConsent(t *testing.T) {
	s, al, hits := newModelPullServer(t, config.ModelPullConsent{})
	w := httptest.NewRecorder()
	s.handleComputeNodeModelPull(w, httptest.NewRequest(http.MethodPost, "/api/compute/nodes/gpu-a/models/pull",
		strings.NewReader(`{"model":"gpt-oss:20b"}`)), "gpu-a")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "autonomous.model_pull_consent") {
		t.Fatalf("refusal must name the consent key to set: %q", body)
	}
	if *hits != 0 {
		t.Fatalf("a refused pull hit the Ollama node %d times", *hits)
	}
	denied := pullAuditByAction(t, al, audit.ActionModelPullDenied)
	if len(denied) != 1 {
		t.Fatalf("model_pull_denied entries = %d, want 1", len(denied))
	}
	if denied[0].Actor != "operator" {
		t.Fatalf("denied entry actor = %q, want operator", denied[0].Actor)
	}
	if got := denied[0].Details["model"]; got != "gpt-oss:20b" {
		t.Fatalf("denied entry model = %v, want gpt-oss:20b", got)
	}
	if got := denied[0].Details["origin"]; got != "compute_node_model_pull" {
		t.Fatalf("origin = %v, want the pull path", got)
	}
	if got, _ := denied[0].Details["reason"].(string); !strings.Contains(got, "autonomous.model_pull_consent") {
		t.Fatalf("reason = %q", got)
	}
}

func TestModelPullAllowedByModelEntry(t *testing.T) {
	s, al, hits := newModelPullServer(t, config.ModelPullConsent{
		Models: []config.ModelConsentEntry{{Name: "gpt-oss", Publisher: "openai", Allowed: true}},
	})
	w := httptest.NewRecorder()
	s.handleComputeNodeModelPull(w, httptest.NewRequest(http.MethodPost, "/api/compute/nodes/gpu-a/models/pull",
		strings.NewReader(`{"model":"gpt-oss:20b"}`)), "gpu-a")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var task PullTask
	if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	tt := waitPullTask(t, task.ID)
	if tt.Status != "done" {
		t.Fatalf("task = %q (error %q)", tt.Status, tt.Error)
	}
	if *hits != 1 {
		t.Fatalf("Ollama pull hits = %d, want 1", *hits)
	}
	allowed := pullAuditByAction(t, al, audit.ActionModelPullAllowed)
	if len(allowed) != 1 {
		t.Fatalf("model_pull_allowed entries = %d, want 1", len(allowed))
	}
	if allowed[0].Details["via"] != "model" || allowed[0].Details["publisher"] != "openai" {
		t.Fatalf("allow details = %v", allowed[0].Details)
	}
}

func TestModelPullAllowedByPublisher(t *testing.T) {
	s, al, hits := newModelPullServer(t, config.ModelPullConsent{
		Publishers: []string{"ORG"},
	})
	w := httptest.NewRecorder()
	s.handleComputeNodeModelPull(w, httptest.NewRequest(http.MethodPost, "/api/compute/nodes/gpu-a/models/pull",
		strings.NewReader(`{"model":"org/deepseek-r1"}`)), "gpu-a")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var task PullTask
	if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	if tt := waitPullTask(t, task.ID); tt.Status != "done" {
		t.Fatalf("task = %q (error %q)", tt.Status, tt.Error)
	}
	if *hits != 1 {
		t.Fatalf("Ollama pull hits = %d, want 1", *hits)
	}
	allowed := pullAuditByAction(t, al, audit.ActionModelPullAllowed)
	if len(allowed) != 1 || allowed[0].Details["via"] != "publisher" || allowed[0].Details["publisher"] != "org" {
		t.Fatalf("allow entries = %+v", allowed)
	}
}

func TestModelPullRefusalCarriesOperatorNote(t *testing.T) {
	s, _, hits := newModelPullServer(t, config.ModelPullConsent{
		Models: []config.ModelConsentEntry{{Name: "phi3", Allowed: false, Note: "licensed only — pull from the internal registry"}},
	})
	w := httptest.NewRecorder()
	s.handleComputeNodeModelPull(w, httptest.NewRequest(http.MethodPost, "/api/compute/nodes/gpu-a/models/pull",
		strings.NewReader(`{"model":"phi3:mini"}`)), "gpu-a")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"listed in the model-pull consent with allowed:false", "operator note:", "licensed only"} {
		if !strings.Contains(body, want) {
			t.Fatalf("refusal %q must include %q", body, want)
		}
	}
	if *hits != 0 {
		t.Fatalf("a refused pull hit the Ollama node %d times", *hits)
	}
}

// TestModelPullWorkerRechecksConsent — the worker re-runs the gate at run
// time: it honors the consent in force when the pull actually starts, so a
// pull requested earlier (or one whose consent was revoked meanwhile) is
// refused here, the task failed, and the refusal audited against the task.
func TestModelPullWorkerRechecksConsent(t *testing.T) {
	s, al, hits := newModelPullServer(t, config.ModelPullConsent{})
	id := "pull-test-worker-recheck"
	now := time.Now().UTC()
	globalPullTasks.add(&PullTask{
		ID: id, NodeName: "gpu-a", Model: "gpt-oss:20b", Status: "pending", StartedAt: now, UpdatedAt: now,
	})
	s.runOllamaPull(id, &compute.Node{Name: "gpu-a", Address: "http://127.0.0.1:0"}, "gpt-oss:20b") // unreachable fake node — must not be reached
	if *hits != 0 {
		t.Fatalf("a refused worker hit the Ollama node %d times", *hits)
	}
	tt := waitPullTask(t, id)
	if tt.Status != "failed" {
		t.Fatalf("task = %q (error %q), want failed", tt.Status, tt.Error)
	}
	if !strings.Contains(tt.Error, "autonomous.model_pull_consent") {
		t.Fatalf("task error = %q, want the consent key", tt.Error)
	}
	denied := pullAuditByAction(t, al, audit.ActionModelPullDenied)
	if len(denied) != 1 {
		t.Fatalf("model_pull_denied entries = %d, want 1", len(denied))
	}
	if got := denied[0].Details["origin"]; got != "pull_task:"+id {
		t.Fatalf("origin = %v, want the pull task", got)
	}
}

// --- consent write surfaces -------------------------------------------------

func TestPutConfigModelPullConsentRoundTrip(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	resp := putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.model_pull_consent": map[string]any{
			"models": []any{
				map[string]any{"name": "gpt-oss:20b", "publisher": "openai", "allowed": true},
				map[string]any{"name": "phi3", "allowed": false, "note": "licensed only"},
			},
			"publishers": []any{"org"},
		},
	})
	if _, ok := resp["deprecated_fields"]; ok {
		t.Errorf("deprecated_fields present for a non-deprecated key: %v", resp)
	}
	// The daemon config holds it…
	got := s.cfg.Autonomous.ModelPullConsent
	if len(got.Models) != 2 || len(got.Publishers) != 1 {
		t.Fatalf("cfg consent = %+v", got)
	}
	if got.Models[0].Name != "gpt-oss:20b" || !got.Models[0].Allowed || got.Models[1].Allowed || got.Models[1].Note != "licensed only" || got.Publishers[0] != "org" {
		t.Fatalf("cfg consent entries = %+v", got.Models)
	}
	// …the live autonomous manager holds the same block (so the gate and the
	// setup report both read it)…
	if got := mgr.Config().ModelPullConsent; len(got.Models) != 2 || got.Publishers[0] != "org" {
		t.Fatalf("live manager consent = %+v", got)
	}
	// …and GET /api/config serves it back.
	aut := getAutonomousConfigMap(t, s)
	if string(aut["model_pull_consent"]) == "" {
		t.Fatalf("GET /api/config autonomous.model_pull_consent missing: %v", aut)
	}
}

func TestPutConfigModelPullConsentRejectsMalformed(t *testing.T) {
	malformed := []any{
		// bad entries
		map[string]any{"autonomous.model_pull_consent": map[string]any{
			"models": []any{map[string]any{"name": "  ", "allowed": true}},
		}},
		map[string]any{"autonomous.model_pull_consent": map[string]any{
			"models": []any{
				map[string]any{"name": "gpt-oss", "allowed": true},
				map[string]any{"name": "GPT-OSS:20b", "allowed": false},
			},
		}},
		map[string]any{"autonomous.model_pull_consent": map[string]any{
			"publishers": []any{"org", ""},
		}},
		// wrong shapes
		map[string]any{"autonomous.model_pull_consent": `{"models":[]}`},
		map[string]any{"autonomous.model_pull_consent": []any{map[string]any{"name": "gpt-oss", "allowed": true}}},
		map[string]any{"autonomous.model_pull_consent": "gpt-oss"},
	}
	for i, body := range malformed {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			s, _, _ := newMaxConcurrentTasksServer(t)
			b, _ := json.Marshal(body)
			rr := httptest.NewRecorder()
			s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("malformed consent %s: want 400, got %d: %s", b, rr.Code, rr.Body.String())
			}
			if got := s.cfg.Autonomous.ModelPullConsent; len(got.Models) != 0 || len(got.Publishers) != 0 {
				t.Fatalf("a rejected PUT must not store consent: %+v", got)
			}
		})
	}
}

// TestPutConfigModelPullConsentRejectsWholePUT — one bad consent block
// rejects the whole PUT, even with other valid keys in it, and nothing is
// stored.
func TestPutConfigModelPullConsentRejectsWholePUT(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	b, _ := json.Marshal(map[string]any{
		"autonomous.max_concurrent_tasks": 3, // valid key included
		"autonomous.model_pull_consent": map[string]any{
			"models": []any{map[string]any{"name": "", "allowed": true}},
		},
	})
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("one bad consent block must reject the whole PUT, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := s.cfg.Autonomous.ModelPullConsent; len(got.Models) != 0 {
		t.Fatalf("consent stored by a rejected PUT: %+v", got)
	}
	if got := s.cfg.Autonomous.MaxConcurrentTasks; got != 1 {
		t.Fatalf("max_concurrent_tasks changed by a rejected PUT: %d", got)
	}
	if got := mgr.Config().MaxConcurrentTasks; got != 1 {
		t.Fatalf("live manager max_concurrent_tasks changed by a rejected PUT: %d", got)
	}
}

func TestPutAutonomousConfigModelPullConsentAuditsChange(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	al, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { _ = al.Close() })
	s.auditLog = al

	resp := putJSON(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{
		"model_pull_consent": map[string]any{
			"models":     []any{map[string]any{"name": "gpt-oss", "allowed": true}},
			"publishers": []any{"org"},
		},
	})
	if string(resp["status"]) != `"ok"` {
		t.Fatalf("status = %s", resp["status"])
	}
	if got := mgr.Config().ModelPullConsent; len(got.Models) != 1 || got.Models[0].Name != "gpt-oss" || got.Publishers[0] != "org" {
		t.Fatalf("live manager consent = %+v", got)
	}
	// …and it round-trips back onto the daemon config (GH#180 sync-back).
	if got := s.cfg.Autonomous.ModelPullConsent; len(got.Models) != 1 {
		t.Fatalf("cfg consent = %+v", got)
	}
	// The consent change is audited with the consent itself, not just the
	// generic autonomous_config update.
	changed := pullAuditByAction(t, al, audit.ActionModelPullConsentChanged)
	if len(changed) != 1 {
		t.Fatalf("model_pull_consent_changed entries = %d, want 1", len(changed))
	}
	details := changed[0].Details
	// The audit log round-trips through JSON, so slices read back as []any.
	models, _ := details["models"].([]any)
	if len(models) != 1 || models[0] != "gpt-oss=pull_allowed" {
		t.Fatalf("models detail = %v", details["models"])
	}
	pubs, _ := details["publishers"].([]any)
	if len(pubs) != 1 || pubs[0] != "org" {
		t.Fatalf("publishers detail = %v", details["publishers"])
	}
}

func TestPutAutonomousConfigModelPullConsentRejectsMalformed(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	b, _ := json.Marshal(map[string]any{"model_pull_consent": map[string]any{
		"models": []any{map[string]any{"name": "gpt-oss", "allowed": true}, map[string]any{"name": "gpt-oss", "allowed": false}},
	}})
	rr := httptest.NewRecorder()
	s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", strings.NewReader(string(b))))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("duplicate model entry: want 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := mgr.Config().ModelPullConsent; len(got.Models) != 0 {
		t.Fatalf("rejected PUT stored consent: %+v", got)
	}
}

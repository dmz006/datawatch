// REST write/read surfaces for the autonomous.requirements min-Ollama
// overrides: GET /api/config reads the block back (stored min_ollama map +
// the known capability list), and PUT /api/config accepts either the whole
// "autonomous.requirements" object or one per-capability
// "autonomous.requirements.min_ollama.<capability>" key. Every shape is
// checked by the one shared rule set, and a rejected PUT stores nothing —
// in the daemon config or the live autonomous manager.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// reqMinOllamaFromGET decodes the autonomous.requirements block served by
// GET /api/config.
func reqMinOllamaFromGET(t *testing.T, s *Server) map[string]any {
	t.Helper()
	aut := getAutonomousConfigMap(t, s)
	raw, ok := aut["requirements"]
	if !ok {
		t.Fatalf("GET /api/config missing autonomous.requirements: %v", aut)
	}
	var block struct {
		MinOllama    map[string]string `json:"min_ollama"`
		Capabilities []string          `json:"capabilities"`
	}
	if err := json.Unmarshal(raw, &block); err != nil {
		t.Fatalf("decode requirements block: %v", err)
	}
	if len(block.MinOllama) == 0 && len(block.Capabilities) == 0 {
		t.Fatalf("requirements block empty: %s", raw)
	}
	got := map[string]any{"min_ollama": block.MinOllama, "capabilities": block.Capabilities}
	return got
}

func TestRequirementsMinOllama_GetServesKnownCapabilities(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	got := reqMinOllamaFromGET(t, s)
	caps := got["capabilities"].([]string)
	found := false
	for _, c := range caps {
		if c == "classification_verdict" {
			found = true
		}
	}
	if !found {
		t.Fatalf("capabilities = %v, want to include classification_verdict", caps)
	}
	// No override stored → an empty min_ollama map (the in-code floors
	// apply), not a null or missing field.
	if m, ok := got["min_ollama"].(map[string]string); !ok || len(m) != 0 {
		t.Fatalf("default min_ollama = %v, want empty object", got["min_ollama"])
	}
}

func TestRequirementsMinOllama_PutWholeBlockRoundTrip(t *testing.T) {
	s, mgr, cfgPath := newMaxConcurrentTasksServer(t)
	resp := putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.requirements": map[string]any{
			"min_ollama": map[string]any{"classification_verdict": "0.9.1"},
		},
	})
	if _, ok := resp["deprecated_fields"]; ok {
		t.Errorf("deprecated_fields present for a non-deprecated key: %v", resp)
	}
	// The daemon config holds it…
	if got := s.cfg.Autonomous.Requirements.MinOllama["classification_verdict"]; got != "0.9.1" {
		t.Fatalf("cfg min_ollama = %v, want classification_verdict=0.9.1", s.cfg.Autonomous.Requirements.MinOllama)
	}
	// …the live autonomous manager holds the same block…
	if got := mgr.Config().Requirements.MinOllama["classification_verdict"]; got != "0.9.1" {
		t.Fatalf("live manager min_ollama = %v, want classification_verdict=0.9.1", mgr.Config().Requirements.MinOllama)
	}
	// …and GET /api/config serves it back.
	got := reqMinOllamaFromGET(t, s)
	m, _ := got["min_ollama"].(map[string]string)
	if m["classification_verdict"] != "0.9.1" {
		t.Fatalf("GET min_ollama = %v, want classification_verdict=0.9.1", got)
	}
	raw, err := readConfigYaml(t, cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if !strings.Contains(raw, "classification_verdict") || !strings.Contains(raw, "0.9.1") {
		t.Fatalf("config.yaml lacks the min_ollama override:\n%s", raw)
	}
}

func TestRequirementsMinOllama_PutPerCapabilityKeyRoundTrip(t *testing.T) {
	s, mgr, _ := newMaxConcurrentTasksServer(t)
	putJSON(t, s.handlePutConfig, "/api/config",
		map[string]any{"autonomous.requirements.min_ollama.classification_verdict": "0.9.1"})
	if got := s.cfg.Autonomous.Requirements.MinOllama["classification_verdict"]; got != "0.9.1" {
		t.Fatalf("cfg min_ollama = %v, want classification_verdict=0.9.1", s.cfg.Autonomous.Requirements.MinOllama)
	}
	if got := mgr.Config().Requirements.MinOllama["classification_verdict"]; got != "0.9.1" {
		t.Fatalf("live manager min_ollama = %v, want classification_verdict=0.9.1", mgr.Config().Requirements.MinOllama)
	}
	got := reqMinOllamaFromGET(t, s)
	m, _ := got["min_ollama"].(map[string]string)
	if m["classification_verdict"] != "0.9.1" {
		t.Fatalf("GET min_ollama = %v, want classification_verdict=0.9.1", got)
	}

	// The empty string clears one capability's floor: stored as empty,
	// hidden from the GET surface's min_ollama map (the in-code floor
	// applies again).
	putJSON(t, s.handlePutConfig, "/api/config",
		map[string]any{"autonomous.requirements.min_ollama.classification_verdict": ""})
	if v, ok := s.cfg.Autonomous.Requirements.MinOllama["classification_verdict"]; !ok || v != "" {
		t.Fatalf("clear = %q (%v), want empty-string entry", v, ok)
	}
	got = reqMinOllamaFromGET(t, s)
	m, _ = got["min_ollama"].(map[string]string)
	if _, present := m["classification_verdict"]; present {
		t.Fatalf("cleared floor still shown in GET min_ollama: %v", got)
	}
}

func TestRequirementsMinOllama_PutRejectsMalformed(t *testing.T) {
	bodies := []map[string]any{
		// unknown capability
		{"autonomous.requirements": map[string]any{"min_ollama": map[string]any{"not_a_capability": "0.35.0"}}},
		// bad version
		{"autonomous.requirements": map[string]any{"min_ollama": map[string]any{"classification_verdict": "abc"}}},
		// non-string value
		{"autonomous.requirements": map[string]any{"min_ollama": map[string]any{"classification_verdict": 3}}},
		// string instead of object
		{"autonomous.requirements": `{"min_ollama":{}}`},
		// array instead of object
		{"autonomous.requirements": []any{"classification_verdict"}},
		// per-capability key with a non-string value
		{"autonomous.requirements.min_ollama.classification_verdict": 0.91},
		// per-capability key with an empty capability name
		{"autonomous.requirements.min_ollama.": "0.9.1"},
	}
	for _, body := range bodies {
		t.Run(strings.ReplaceAll(plainKey(body), " ", "_"), func(t *testing.T) {
			s, mgr, _ := newMaxConcurrentTasksServer(t)
			b, _ := json.Marshal(body)
			rr := httptest.NewRecorder()
			s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("malformed %s: want 400, got %d: %s", b, rr.Code, rr.Body.String())
			}
			if got := s.cfg.Autonomous.Requirements.MinOllama; len(got) != 0 {
				t.Fatalf("a rejected PUT must not store overrides: %v", got)
			}
			if got := mgr.Config().Requirements.MinOllama; len(got) != 0 {
				t.Fatalf("a rejected PUT must not reach the live manager: %v", got)
			}
		})
	}
}

func TestRequirementsMinOllama_PutRejectsWholePUT(t *testing.T) {
	s, _, _ := newMaxConcurrentTasksServer(t)
	b, _ := json.Marshal(map[string]any{
		"autonomous.max_concurrent_tasks": 3, // valid key included
		"autonomous.requirements": map[string]any{
			"min_ollama": map[string]any{"not_a_capability": "0.35.0"},
		},
	})
	rr := httptest.NewRecorder()
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(string(b))))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("one bad requirements block must reject the whole PUT, got %d: %s", rr.Code, rr.Body.String())
	}
	if got := s.cfg.Autonomous.MaxConcurrentTasks; got != 1 {
		t.Fatalf("valid sibling key stored by a rejected PUT: max_concurrent_tasks = %d, want 1", got)
	}
}

// plainKey names the first top-level key of a PATCH for a subtest id.
func plainKey(m map[string]any) string {
	for k := range m {
		return k
	}
	return "empty"
}

// readConfigYaml reads the persisted config for assertions.
func readConfigYaml(t *testing.T, path string) (string, error) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

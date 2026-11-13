// Project policy over REST: policy round-trips on upsert/list/get, invalid
// values are refused with the offending field named, and the effective
// endpoint resolves each setting to its value and supplying tier.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type policyEffFake struct {
	fakeOrchAutonomous
	prds map[string]any
}

func (f *policyEffFake) GetPRD(id string) (any, bool) {
	v, ok := f.prds[id]
	return v, ok
}

func (s *Server) projectsEffective(t *testing.T, name, automaton string) int {
	t.Helper()
	url := "/api/projects/" + name + "/policy/effective"
	if automaton != "" {
		url += "?automaton=" + automaton
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rr := httptest.NewRecorder()
	s.handleProjects(rr, req)
	return rr.Code
}

func (s *Server) projectsUpsertPolicy(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.handleProjects(rr, req)
	return rr
}

func TestProjectsPolicy_UpsertGetRoundTrip(t *testing.T) {
	s := bl90Server(t)
	body := `{"name":"acme","dir":"/home/<user>/acme","policy":{"plan_review_mode":"block","retry_ceiling_total":12,"max_concurrent_tasks":3}}`
	rr := s.projectsUpsertPolicy(t, body)
	if rr.Code != http.StatusOK {
		t.Fatalf("upsert status=%d body=%s", rr.Code, rr.Body.String())
	}

	// Get carries the policy block.
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/projects/acme", nil)
	s.handleProjects(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get status=%d", rr.Code)
	}
	var got map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&got)
	policy, _ := got["policy"].(map[string]any)
	if policy == nil {
		t.Fatalf("no policy in get body: %s", rr.Body.String())
	}
	if policy["plan_review_mode"] != "block" {
		t.Errorf("plan_review_mode = %v", policy["plan_review_mode"])
	}
	if v, ok := policy["retry_ceiling_total"].(float64); !ok || int(v) != 12 {
		t.Errorf("retry_ceiling_total = %v", policy["retry_ceiling_total"])
	}

	// The list carries it on the entry too.
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	s.handleProjects(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list status=%d", rr.Code)
	}
	var list []map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&list)
	if len(list) != 1 {
		t.Fatalf("list = %s", rr.Body.String())
	}
	if _, ok := list[0]["policy"].(map[string]any); !ok {
		t.Errorf("list entry missing policy: %s", rr.Body.String())
	}
}

func TestProjectsPolicy_UpsertRejectsInvalidFieldNames(t *testing.T) {
	s := bl90Server(t)
	cases := map[string]string{
		"plan_review_mode": `{"name":"acme","dir":"/home/<user>/acme","policy":{"plan_review_mode":"banana"}}`,
		"retry_policy":     `{"name":"acme","dir":"/home/<user>/acme","policy":{"retry_policy":{"nope":1}}}`,
		"max_concurrent":   `{"name":"acme","dir":"/home/<user>/acme","policy":{"max_concurrent_tasks":0}}`,
	}
	for field, body := range cases {
		rr := s.projectsUpsertPolicy(t, body)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d, want 400 (%s)", field, rr.Code, rr.Body.String())
			continue
		}
		if !strings.Contains(rr.Body.String(), field) {
			t.Errorf("%s: body does not name the field: %s", field, rr.Body.String())
		}
	}
}

func TestProjectsPolicy_EffectiveDaemonTier(t *testing.T) {
	s := bl90Server(t)
	rr := s.projectsUpsertPolicy(t, `{"name":"acme","dir":"/home/<user>/acme"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("upsert status=%d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/projects/acme/policy/effective", nil)
	s.handleProjects(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("effective status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Project string `json:"project"`
		Policy  map[string]struct {
			Value  json.RawMessage `json:"value"`
			Source string          `json:"source"`
		} `json:"policy"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v (%s)", err, rr.Body.String())
	}
	if got.Project != "acme" {
		t.Errorf("project = %q", got.Project)
	}
	expectDaemon := map[string]string{
		"max_concurrent_tasks": "1",
		"plan_review_mode":     `"off"`,
		"default_auto_pr":      `false`,
	}
	for key, rawValue := range expectDaemon {
		f, ok := got.Policy[key]
		if !ok {
			t.Errorf("missing key %q", key)
			continue
		}
		if f.Source != "daemon" {
			t.Errorf("%s source = %s, want daemon", key, f.Source)
		}
		if strings.TrimSpace(string(f.Value)) != rawValue {
			t.Errorf("%s value = %s, want %s", key, f.Value, rawValue)
		}
	}
}

func TestProjectsPolicy_EffectiveProjectTier(t *testing.T) {
	s := bl90Server(t)
	rr := s.projectsUpsertPolicy(t, `{"name":"acme","dir":"/home/<user>/acme","policy":{"plan_review_mode":"block","max_concurrent_tasks":7}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("upsert status=%d", rr.Code)
	}
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/projects/acme/policy/effective", nil)
	s.handleProjects(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("effective status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Policy map[string]struct {
			Value  json.RawMessage `json:"value"`
			Source string          `json:"source"`
		} `json:"policy"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.Policy["plan_review_mode"].Source != "project" || strings.TrimSpace(string(got.Policy["plan_review_mode"].Value)) != `"block"` {
		t.Errorf("plan_review_mode = %+v, want {\"block\" project}", got.Policy["plan_review_mode"])
	}
	if got.Policy["max_concurrent_tasks"].Source != "project" || strings.TrimSpace(string(got.Policy["max_concurrent_tasks"].Value)) != "7" {
		t.Errorf("max_concurrent_tasks = %+v, want 7 project", got.Policy["max_concurrent_tasks"])
	}
	// A field the project does not set stays on the daemon tier.
	if got.Policy["plan_coverage_check"].Source != "daemon" {
		t.Errorf("plan_coverage_check source = %s, want daemon", got.Policy["plan_coverage_check"].Source)
	}
}

func TestProjectsPolicy_EffectiveAutomatonTierWins(t *testing.T) {
	s := bl90Server(t)
	rr := s.projectsUpsertPolicy(t, `{"name":"acme","dir":"/home/<user>/acme","policy":{"max_concurrent_tasks":7}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("upsert status=%d", rr.Code)
	}
	s.autonomousMgr = &policyEffFake{prds: map[string]any{
		"prd-1": map[string]any{"id": "prd-1", "max_concurrent_tasks": 9, "retry_policy": map[string]any{"infra": 3}},
	}}
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/projects/acme/policy/effective?automaton=prd-1", nil)
	s.handleProjects(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("effective status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Automaton string `json:"automaton"`
		Policy    map[string]struct {
			Value  json.RawMessage `json:"value"`
			Source string          `json:"source"`
		} `json:"policy"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&got)
	if got.Automaton != "prd-1" {
		t.Errorf("automaton = %q", got.Automaton)
	}
	if got.Policy["max_concurrent_tasks"].Source != "automaton" || strings.TrimSpace(string(got.Policy["max_concurrent_tasks"].Value)) != "9" {
		t.Errorf("max_concurrent_tasks = %+v, want 9 automaton (overrides the project's 7)", got.Policy["max_concurrent_tasks"])
	}
	if !strings.Contains(string(got.Policy["retry_policy"].Value), "infra") || got.Policy["retry_policy"].Source != "automaton" {
		t.Errorf("retry_policy = %+v, want {infra:3} automaton", got.Policy["retry_policy"])
	}
}

func TestProjectsPolicy_EffectiveMissingProjectAndAutomaton(t *testing.T) {
	s := bl90Server(t)
	code := s.projectsEffective(t, "ghost", "")
	if code != http.StatusNotFound {
		t.Errorf("missing project: %d, want 404", code)
	}
	rr := s.projectsUpsertPolicy(t, `{"name":"acme","dir":"/home/<user>/acme"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("upsert status=%d", rr.Code)
	}
	// No autonomous manager wired: 503, not a panic or 200.
	code = s.projectsEffective(t, "acme", "prd-1")
	if code != http.StatusServiceUnavailable {
		t.Errorf("no mgr: %d, want 503", code)
	}
	// Manager wired but unknown id: 404.
	s.autonomousMgr = &policyEffFake{}
	code = s.projectsEffective(t, "acme", "prd-1")
	if code != http.StatusNotFound {
		t.Errorf("unknown automaton: %d, want 404", code)
	}
}

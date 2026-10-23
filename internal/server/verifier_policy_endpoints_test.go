// Verifier policy over REST: the autonomous.verifier_policy.* config keys on
// both config surfaces (validation and live apply) and the presence-aware
// set_verifier_policy / set_task_verifier_policy actions.

package server

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/inference"
)

// verifierPolicyVals stores per-unit overrides so the fake manager can
// emulate presence-aware persistence.
type verifierPolicyVals struct {
	mode     string
	backends []string
}

type verifierPolicyTestOrch struct {
	fakeOrchAutonomous
	calls int
	units map[string]*verifierPolicyVals
}

func (o *verifierPolicyTestOrch) apply(key string, mode *string, backends *[]string) (any, error) {
	o.calls++
	if o.units == nil {
		o.units = map[string]*verifierPolicyVals{}
	}
	v := o.units[key]
	if v == nil {
		v = &verifierPolicyVals{}
		o.units[key] = v
	}
	if mode != nil {
		v.mode = *mode
	}
	if backends != nil {
		v.backends = *backends
	}
	return &autonomous.PRD{ID: key}, nil
}

func (o *verifierPolicyTestOrch) SetTaskVerifierPolicy(prdID, taskID string, mode *string, backends *[]string, _ string) (any, error) {
	return o.apply(prdID+"/task/"+taskID, mode, backends)
}
func (o *verifierPolicyTestOrch) SetPRDVerifierPolicy(prdID string, mode *string, backends *[]string, _ string) (any, error) {
	return o.apply(prdID, mode, backends)
}

type verifierPolicyCase struct {
	action, unitJSON, key string
}

var verifierPolicyCases = []verifierPolicyCase{
	{"set_task_verifier_policy", `"task_id":"t1",`, "p1/task/t1"},
	{"set_verifier_policy", ``, "p1"},
}

func verifierPolicyServer(t *testing.T, orch *verifierPolicyTestOrch) (*Server, *audit.Log) {
	t.Helper()
	reg := inference.NewRegistry()
	_ = reg.Add(&inference.LLM{Name: "good-llm", Kind: inference.KindOllama})
	_ = reg.Add(&inference.LLM{Name: "other-llm", Kind: inference.KindOllama})
	srv := &Server{autonomousMgr: orch}
	srv.inferenceReg = reg
	al, err := audit.NewAt(t.TempDir() + "/audit.log")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = al.Close() })
	srv.auditLog = al
	return srv, al
}

func verifierPolicyDo(srv *Server, method, prdID, action, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/autonomous/prds/"+prdID+"/"+action, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.handleAutonomousPRDs(rr, req)
	return rr
}

func TestVerifierPolicyEndpoints_SetAndClear(t *testing.T) {
	for _, tc := range verifierPolicyCases {
		orch := &verifierPolicyTestOrch{}
		srv, _ := verifierPolicyServer(t, orch)

		rr := verifierPolicyDo(srv, http.MethodPost, "p1", tc.action, `{`+tc.unitJSON+`"mode":"prefer-free","backends":["good-llm","other-llm"]}`)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: want 200, got %d: %s", tc.action, rr.Code, rr.Body.String())
		}
		want := verifierPolicyVals{"prefer-free", []string{"good-llm", "other-llm"}}
		if got := *orch.units[tc.key]; !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: persisted %+v", tc.action, got)
		}

		// Absent keys leave fields unchanged: only the mode is cleared.
		rr = verifierPolicyDo(srv, http.MethodPost, "p1", tc.action, `{`+tc.unitJSON+`"mode":""}`)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s clear mode: want 200, got %d: %s", tc.action, rr.Code, rr.Body.String())
		}
		want = verifierPolicyVals{"", []string{"good-llm", "other-llm"}}
		if got := *orch.units[tc.key]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: after clearing mode got %+v", tc.action, got)
		}

		// An empty list clears the backends without registry validation.
		rr = verifierPolicyDo(srv, http.MethodPost, "p1", tc.action, `{`+tc.unitJSON+`"backends":[]}`)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s clear backends: want 200, got %d: %s", tc.action, rr.Code, rr.Body.String())
		}
		if got := *orch.units[tc.key]; got.mode != "" || len(got.backends) != 0 {
			t.Errorf("%s: after clearing both got %+v", tc.action, got)
		}
	}
}

func TestVerifierPolicyEndpoints_InvalidInput400(t *testing.T) {
	for _, tc := range verifierPolicyCases {
		for name, body := range map[string]string{
			"unknown mode":           `{` + tc.unitJSON + `"mode":"round-robin"}`,
			"empty backend entry":    `{` + tc.unitJSON + `"backends":["good-llm",""]}`,
			"blank backend entry":    `{` + tc.unitJSON + `"backends":["  "]}`,
			"unknown backend":        `{` + tc.unitJSON + `"backends":["bad-llm"]}`,
			"backends not an array":  `{` + tc.unitJSON + `"backends":"good-llm"}`,
			"mode not a string":      `{` + tc.unitJSON + `"mode":3}`,
			"malformed json":         `{`,
			"valid mode bad backend": `{` + tc.unitJSON + `"mode":"balance","backends":["bad-llm"]}`,
		} {
			orch := &verifierPolicyTestOrch{}
			srv, _ := verifierPolicyServer(t, orch)
			rr := verifierPolicyDo(srv, http.MethodPost, "p1", tc.action, body)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("%s/%s: want 400, got %d: %s", tc.action, name, rr.Code, rr.Body.String())
			}
			if orch.calls != 0 {
				t.Errorf("%s/%s: manager must not run on validation failure", tc.action, name)
			}
		}
	}
}

func TestVerifierPolicyEndpoints_InvalidModeMessage(t *testing.T) {
	orch := &verifierPolicyTestOrch{}
	srv, _ := verifierPolicyServer(t, orch)
	rr := verifierPolicyDo(srv, http.MethodPost, "p1", "set_verifier_policy", `{"mode":"round-robin"}`)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "prefer-free") {
		t.Fatalf("want 400 naming the valid modes, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestVerifierPolicyEndpoints_GET405(t *testing.T) {
	for _, tc := range verifierPolicyCases {
		orch := &verifierPolicyTestOrch{}
		srv, _ := verifierPolicyServer(t, orch)
		if rr := verifierPolicyDo(srv, http.MethodGet, "p1", tc.action, ``); rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: want 405, got %d", tc.action, rr.Code)
		}
	}
}

func TestVerifierPolicyEndpoints_TaskIDRequired(t *testing.T) {
	orch := &verifierPolicyTestOrch{}
	srv, _ := verifierPolicyServer(t, orch)
	rr := verifierPolicyDo(srv, http.MethodPost, "p1", "set_task_verifier_policy", `{"mode":"balance"}`)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "task_id required") {
		t.Errorf("want 400 task_id required, got %d %s", rr.Code, rr.Body.String())
	}
}

func TestVerifierPolicyEndpoints_Audited(t *testing.T) {
	for _, tc := range verifierPolicyCases {
		orch := &verifierPolicyTestOrch{}
		srv, al := verifierPolicyServer(t, orch)
		if rr := verifierPolicyDo(srv, http.MethodPost, "p1", tc.action, `{`+tc.unitJSON+`"mode":"balance"}`); rr.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", tc.action, rr.Code, rr.Body.String())
		}
		entries, err := al.Read(audit.QueryFilter{})
		if err != nil || len(entries) == 0 {
			t.Fatalf("%s: no audit entry (err=%v)", tc.action, err)
		}
		e := entries[0]
		if e.Action != tc.action || e.Details["resource_type"] != "automaton" || e.Details["resource_id"] != "p1" {
			t.Errorf("%s: audit entry %+v", tc.action, e)
		}
		if e.Details["mode"] != "balance" {
			t.Errorf("%s: audit detail missing mode: %+v", tc.action, e.Details)
		}
		if _, ok := e.Details["backends"]; ok {
			t.Errorf("%s: absent backends should not appear in audit detail", tc.action)
		}
	}
}

// realVerifierPolicyServer builds a server over a real manager holding one
// Automaton with one task, pinned to the given status.
func realVerifierPolicyServer(t *testing.T, status autonomous.PRDStatus) (*Server, *autonomous.Manager, *autonomous.PRD) {
	t.Helper()
	srv, mgr, _ := newPlanCoverageServer(t, "block", 1.0, pcGateFailingPlan)
	prd, err := mgr.CreatePRD("spec", t.TempDir(), "opencode", "", autonomous.EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := mgr.Store().SetStories(prd.ID, []autonomous.Story{{
		Title: "Story",
		Tasks: []autonomous.Task{{Title: "Task 1", Spec: "do task 1"}},
	}}); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, _ = mgr.Store().GetPRD(prd.ID)
	prd.Status = status
	if err := mgr.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	prd, _ = mgr.Store().GetPRD(prd.ID)
	return srv, mgr, prd
}

func TestVerifierPolicyEndpoints_LockedPRD(t *testing.T) {
	srv, mgr, prd := realVerifierPolicyServer(t, autonomous.PRDRunning)
	taskID := prd.Story[0].Tasks[0].ID

	for _, tc := range []struct{ action, body string }{
		{"set_verifier_policy", `{"mode":"balance"}`},
		{"set_task_verifier_policy", `{"task_id":"` + taskID + `","mode":"balance"}`},
	} {
		rr := verifierPolicyDo(srv, http.MethodPost, prd.ID, tc.action, tc.body)
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "is locked") {
			t.Errorf("%s on a running PRD: want 400 locked, got %d: %s", tc.action, rr.Code, rr.Body.String())
		}
	}
	got, _ := mgr.Store().GetPRD(prd.ID)
	if got.VerifierPolicy != nil || got.Story[0].Tasks[0].VerifierPolicy != nil {
		t.Fatalf("a refused override must change nothing: %+v / %+v", got.VerifierPolicy, got.Story[0].Tasks[0].VerifierPolicy)
	}
}

func TestVerifierPolicyEndpoints_RealManagerSetAndClear(t *testing.T) {
	srv, mgr, prd := realVerifierPolicyServer(t, autonomous.PRDNeedsReview)
	taskID := prd.Story[0].Tasks[0].ID

	rr := verifierPolicyDo(srv, http.MethodPost, prd.ID, "set_verifier_policy", `{"mode":"Prefer-Free","backends":[" a ","b"]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("set automaton policy: %d %s", rr.Code, rr.Body.String())
	}
	rr = verifierPolicyDo(srv, http.MethodPost, prd.ID, "set_task_verifier_policy", `{"task_id":"`+taskID+`","mode":"claude-only"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("set task policy: %d %s", rr.Code, rr.Body.String())
	}
	got, _ := mgr.Store().GetPRD(prd.ID)
	if p := got.VerifierPolicy; p == nil || p.Mode != "prefer-free" || !reflect.DeepEqual(p.Backends, []string{"a", "b"}) {
		t.Fatalf("automaton policy = %+v", p)
	}
	if p := got.Story[0].Tasks[0].VerifierPolicy; p == nil || p.Mode != "claude-only" || len(p.Backends) != 0 {
		t.Fatalf("task policy = %+v", p)
	}
	kinds := map[string]bool{}
	for _, d := range got.Decisions {
		kinds[d.Kind] = true
	}
	if !kinds["set_prd_verifier_policy"] || !kinds["set_task_verifier_policy"] {
		t.Errorf("decision log lacks the override decisions: %v", kinds)
	}

	// Clearing every field returns the level to inheriting.
	rr = verifierPolicyDo(srv, http.MethodPost, prd.ID, "set_verifier_policy", `{"mode":"","backends":[]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("clear automaton policy: %d %s", rr.Code, rr.Body.String())
	}
	got, _ = mgr.Store().GetPRD(prd.ID)
	if got.VerifierPolicy != nil {
		t.Fatalf("cleared automaton policy must be nil, got %+v", got.VerifierPolicy)
	}
}

func TestVerifierPolicyConfig_LiveApplyBothSurfaces(t *testing.T) {
	s, mgr, _ := newPlanCoverageServer(t, "block", 1.0, pcGateFailingPlan)

	// PUT /api/config in the CLI / comm shape: dotted keys, string values.
	pcPutOK(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.verifier_policy.mode":                      "Balance",
		"autonomous.verifier_policy.backends":                  "ollama-a, ollama-b",
		"autonomous.verifier_policy.claude_session_cap":        "4",
		"autonomous.verifier_policy.claude_cap_window_minutes": 30,
		"autonomous.verifier_policy.force_stronger_on_retry":   true,
		"autonomous.verifier_policy.final_gate_stronger":       "true",
		"autonomous.verifier_policy.stronger_backend":          "claude-code",
		"autonomous.verifier_policy.stronger_model":            "claude-opus-5-5",
	})
	vp := mgr.Config().VerifierPolicy
	if vp.Mode != "balance" || !reflect.DeepEqual(vp.Backends, []string{"ollama-a", "ollama-b"}) ||
		vp.ClaudeSessionCap != 4 || vp.ClaudeCapWindowMinutes != 30 || !vp.ForceStrongerOnRetry ||
		!vp.FinalGateStronger || vp.StrongerBackend != "claude-code" || vp.StrongerModel != "claude-opus-5-5" {
		t.Fatalf("live manager policy after PUT /api/config = %+v", vp)
	}
	if got := s.cfg.Autonomous.VerifierPolicy; !reflect.DeepEqual(got, vp) {
		t.Fatalf("daemon config policy = %+v, live = %+v", got, vp)
	}
	if raw := getAutonomousConfigMap(t, s)["verifier_policy"]; !strings.Contains(string(raw), `"mode":"balance"`) {
		t.Fatalf("GET /api/config verifier_policy = %s", raw)
	}

	// One key leaves the others alone.
	pcPutOK(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.verifier_policy.claude_session_cap": 0})
	vp = mgr.Config().VerifierPolicy
	if vp.ClaudeSessionCap != 0 || vp.Mode != "balance" || len(vp.Backends) != 2 {
		t.Fatalf("a single key changed more than itself: %+v", vp)
	}

	// PUT /api/autonomous/config in the autonomous_config_set shape: one
	// nested object, merged onto the current policy field by field.
	pcPutOK(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{
		"verifier_policy": map[string]any{"mode": "gpu-only", "backends": []string{"ollama-c"}},
	})
	vp = mgr.Config().VerifierPolicy
	if vp.Mode != "gpu-only" || !reflect.DeepEqual(vp.Backends, []string{"ollama-c"}) || vp.ClaudeCapWindowMinutes != 30 || !vp.FinalGateStronger {
		t.Fatalf("live manager policy after PUT /api/autonomous/config = %+v", vp)
	}
	if got := s.cfg.Autonomous.VerifierPolicy; !reflect.DeepEqual(got, vp) {
		t.Fatalf("daemon config policy = %+v, live = %+v", got, vp)
	}

	// An empty list clears the backends.
	pcPutOK(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"verifier_policy": map[string]any{"backends": []string{}}})
	if vp = mgr.Config().VerifierPolicy; len(vp.Backends) != 0 || vp.Mode != "gpu-only" {
		t.Fatalf("clearing backends: %+v", vp)
	}
}

func TestVerifierPolicyConfig_InvalidRejectedOnBothSurfaces(t *testing.T) {
	s, mgr, _ := newPlanCoverageServer(t, "block", 1.0, pcGateFailingPlan)
	pcPutOK(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{
		"verifier_policy": map[string]any{"mode": "balance", "backends": []string{"a"}, "claude_session_cap": 2},
	})
	before := mgr.Config().VerifierPolicy

	for _, bad := range []map[string]any{
		{"autonomous.verifier_policy.mode": "round-robin"},
		{"autonomous.verifier_policy.mode": 3},
		{"autonomous.verifier_policy.backends": "a,,b"},
		{"autonomous.verifier_policy.backends": []string{"a", ""}},
		{"autonomous.verifier_policy.claude_session_cap": -1},
		{"autonomous.verifier_policy.claude_session_cap": 1.5},
		{"autonomous.verifier_policy.claude_session_cap": "many"},
		{"autonomous.verifier_policy.claude_cap_window_minutes": -5},
		{"autonomous.verifier_policy.force_stronger_on_retry": "maybe"},
		{"autonomous.verifier_policy.stronger_model": 7},
		{"autonomous.verifier_policy.bogus": "x"},
		// A bad value rejects the whole PUT, including the valid key beside it.
		{"autonomous.verifier_policy.mode": "gpu-only", "autonomous.verifier_policy.claude_session_cap": -1},
	} {
		pcPutBad(t, s.handlePutConfig, "/api/config", bad)
	}
	for _, bad := range []map[string]any{
		{"verifier_policy": map[string]any{"mode": "round-robin"}},
		{"verifier_policy": map[string]any{"mode": 3}},
		{"verifier_policy": map[string]any{"backends": []string{"a", ""}}},
		{"verifier_policy": map[string]any{"backends": "a,b"}},
		{"verifier_policy": map[string]any{"claude_session_cap": -1}},
		{"verifier_policy": map[string]any{"claude_cap_window_minutes": -1}},
		{"verifier_policy": map[string]any{"claude_session_cap": 2.5}},
		{"verifier_policy": map[string]any{"force_stronger_on_retry": "yes"}},
		{"verifier_policy": map[string]any{"bogus": 1}},
		{"verifier_policy": "balance"},
	} {
		pcPutBad(t, s.handleAutonomousConfig, "/api/autonomous/config", bad)
	}
	if after := mgr.Config().VerifierPolicy; !reflect.DeepEqual(after, before) {
		t.Fatalf("a rejected PUT changed the live policy: before %+v, after %+v", before, after)
	}
	if got := s.cfg.Autonomous.VerifierPolicy; !reflect.DeepEqual(got, before) {
		t.Fatalf("a rejected PUT changed the daemon config: before %+v, after %+v", before, got)
	}
}

func TestVerifierPolicyConfig_NullObjectIsAbsent(t *testing.T) {
	s, mgr, _ := newPlanCoverageServer(t, "block", 1.0, pcGateFailingPlan)
	pcPutOK(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"verifier_policy": map[string]any{"mode": "balance"}})
	pcPutOK(t, s.handleAutonomousConfig, "/api/autonomous/config", map[string]any{"verifier_policy": nil})
	if vp := mgr.Config().VerifierPolicy; vp.Mode != "balance" {
		t.Fatalf("null verifier_policy must leave the policy alone, got %+v", vp)
	}
}

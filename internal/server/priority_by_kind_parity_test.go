// priority_by_kind + defect_intake_automation over REST: both settings are
// readable and writable on /api/autonomous/config and via PUT /api/config
// (autonomous.* keys), with the same integer-only validation on both
// surfaces, a valid empty-table clear that leaves the daemon defaults in
// effect, and the GET side always showing the effective table.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func priorityServer(t *testing.T) (*Server, *autonomous.Manager) {
	t.Helper()
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.Config{}, func(autonomous.DecomposeRequest) (string, error) { return "", nil })
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: newTestAPI(mgr),
	}, mgr
}

func priorityPut(t *testing.T, s *Server, body string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodPut, "/api/autonomous/config", bytes.NewReader([]byte(body))))
	return rr, rr.Body.String()
}

func getEffectiveTable(t *testing.T, s *Server) map[string]int {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET config = %d: %s", rr.Code, rr.Body.String())
	}
	var got struct {
		PriorityByKind         map[string]int `json:"priority_by_kind"`
		DefectIntakeAutomation string         `json:"defect_intake_automation"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	return got.PriorityByKind
}

var defaultKindTable = map[string]int{"bug": 80, "maintenance": 70, "feature": 20, "other": 0}

func TestConfigPriorityByKind_PutGet_Parity(t *testing.T) {
	s, mgr := priorityServer(t)

	// Object form: operator entries land on the table.
	rr, body := priorityPut(t, s, `{"priority_by_kind":{"bug":90,"epic":50}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT object = %d: %s", rr.Code, body)
	}
	c := mgr.Config()
	if c.PriorityByKind["bug"] != 90 || c.PriorityByKind["epic"] != 50 {
		t.Fatalf("stored table = %v, want bug=90 epic=50", c.PriorityByKind)
	}
	if c.PriorityByKind["feature"] != 20 {
		t.Fatalf("untouched feature row = %d, want the daemon default 20", c.PriorityByKind["feature"])
	}

	// GET always shows the effective table — operator entries over defaults.
	if got := getEffectiveTable(t, s); !reflect.DeepEqual(got, map[string]int{"bug": 90, "maintenance": 70, "feature": 20, "other": 0, "epic": 50}) {
		t.Fatalf("GET effective table = %v, want bug=90 epic=50 over defaults", got)
	}

	// String form (CLI/comm quote their values): a PUT writes that entry
	// plus the daemon-default layer over the shipped rows (bug went back to
	// the default 80), while rows for custom kinds the PUT did not mention
	// (epic) keep their stored values.
	rr, body = priorityPut(t, s, `{"priority_by_kind":"maintenance=55"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT string = %d: %s", rr.Code, body)
	}
	got := getEffectiveTable(t, s)
	want := map[string]int{"bug": 80, "maintenance": 55, "feature": 20, "other": 0, "epic": 50}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GET effective table = %v, want %v", got, want)
	}
}

func TestConfigPriorityByKind_ClearRestoresDaemonDefaults(t *testing.T) {
	s, mgr := priorityServer(t)
	rr, body := priorityPut(t, s, `{"priority_by_kind":{"bug":3}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", rr.Code, body)
	}
	rr, body = priorityPut(t, s, `{"priority_by_kind":{}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("clear PUT = %d: %s", rr.Code, body)
	}
	if got := mgr.Config().PriorityByKind["bug"]; got != 80 {
		t.Fatalf("cleared bug row = %d, want the daemon default 80 (a cleared table must not read 0 at runtime)", got)
	}
	if got := getEffectiveTable(t, s); !reflect.DeepEqual(got, defaultKindTable) {
		t.Fatalf("GET after clear = %v, want the daemon defaults %v", got, defaultKindTable)
	}
}

func TestConfigPriorityByKind_PutRejectsNonInteger(t *testing.T) {
	s, mgr := priorityServer(t)
	mustReject := func(name, body, frag string) {
		t.Helper()
		rr, got := priorityPut(t, s, body)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", name, rr.Code, got)
		}
		if !strings.Contains(strings.ToLower(got), frag) {
			t.Errorf("%s: body %q should say %q", name, got, frag)
		}
	}
	mustReject("fraction", `{"priority_by_kind":{"bug":1.5}}`, "integer")
	mustReject("word", `{"priority_by_kind":{"bug":"high"}}`, "integer")
	mustReject("boolean", `{"priority_by_kind":{"bug":true}}`, "integer")
	mustReject("negative", `{"priority_by_kind":{"bug":-5}}`, "integer")
	mustReject("empty kind", `{"priority_by_kind":{" ":5}}`, "empty kind name")
	mustReject("array", `{"priority_by_kind":[1,2]}`, "object")

	// Rejected edits change nothing.
	priorityPut(t, s, `{"priority_by_kind":{"bug":77}}`)
	if got := mgr.Config().PriorityByKind["bug"]; got != 77 {
		t.Fatalf("bug row = %d after rejected edits, want the 77 set before them", got)
	}
}

func TestConfigDefectIntakeAutomation_PutGet_Parity(t *testing.T) {
	s, mgr := priorityServer(t)
	rr, body := priorityPut(t, s, `{"defect_intake_automation":"my-auto"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT = %d: %s", rr.Code, body)
	}
	if got := mgr.Config().DefectIntakeAutomation; got != "my-auto" {
		t.Fatalf("stored = %q, want my-auto", got)
	}
	// GET: present with its value.
	rr = httptest.NewRecorder()
	s.handleAutonomousConfig(rr, httptest.NewRequest(http.MethodGet, "/api/autonomous/config", nil))
	if !strings.Contains(rr.Body.String(), "\"defect_intake_automation\":\"my-auto\"") {
		t.Fatalf("GET lacks defect_intake_automation: %s", rr.Body.String())
	}
	// The empty string clears it back to the unset state.
	rr, body = priorityPut(t, s, `{"defect_intake_automation":""}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("clear PUT = %d: %s", rr.Code, body)
	}
	if got := mgr.Config().DefectIntakeAutomation; got != "" {
		t.Fatalf("cleared = %q, want empty", got)
	}
	// A present non-string is a 400.
	rr, body = priorityPut(t, s, `{"defect_intake_automation":123}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("non-string = %d, want 400: %s", rr.Code, body)
	}
}

// PUT /api/config (the autonomous.* keys) carries the same rules: the live
// manager receives exactly the touched field, the value is validated first,
// and the GET side shows the merged result.
func TestPutConfig_AutonomousPriorityByKindAndDefectIntake(t *testing.T) {
	spy := &autonomousConfigPropagationSpy{}
	s := &Server{cfg: &config.Config{}, cfgPath: t.TempDir() + "/config.yaml", autonomousMgr: spy}

	rr := httptest.NewRecorder()
	patch, _ := json.Marshal(map[string]any{"autonomous.priority_by_kind": map[string]int{"feature": 0, "epic": 60}, "autonomous.defect_intake_automation": "my-auto"})
	s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(patch)))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT /api/config = %d: %s", rr.Code, rr.Body.String())
	}
	var forwarded map[string]json.RawMessage
	if err := json.Unmarshal(spy.lastSetConfig, &forwarded); err != nil {
		t.Fatalf("decode propagated payload: %v", err)
	}
	if len(forwarded) != 2 {
		t.Fatalf("forwarded fields = %v, want exactly priority_by_kind + defect_intake_automation", forwarded)
	}
	var table map[string]int
	if err := json.Unmarshal(forwarded["priority_by_kind"], &table); err != nil || table["feature"] != 0 || table["epic"] != 60 {
		t.Fatalf("forwarded priority_by_kind = %v (err %v), want feature=0 epic=60", table, err)
	}
	if string(forwarded["defect_intake_automation"]) != `"my-auto"` {
		t.Fatalf("forwarded defect_intake_automation = %s", forwarded["defect_intake_automation"])
	}

	// The daemon-side config holds the operator layer.
	if s.cfg.Autonomous.PriorityByKind["epic"] != 60 || s.cfg.Autonomous.DefectIntakeAutomation != "my-auto" {
		t.Fatalf("daemon config = %+v %+v", s.cfg.Autonomous.PriorityByKind, s.cfg.Autonomous.DefectIntakeAutomation)
	}

	// GET /api/config shows the EFFECTIVE table (defaults + the layer).
	grr := httptest.NewRecorder()
	s.handleGetConfig(grr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if grr.Code != http.StatusOK {
		t.Fatalf("GET /api/config = %d: %s", grr.Code, grr.Body.String())
	}
	var gotCfg struct {
		Autonomous struct {
			PriorityByKind         map[string]int `json:"priority_by_kind"`
			DefectIntakeAutomation string         `json:"defect_intake_automation"`
		} `json:"autonomous"`
	}
	if err := json.NewDecoder(grr.Body).Decode(&gotCfg); err != nil {
		t.Fatalf("decode GET /api/config: %v", err)
	}
	want := map[string]int{"bug": 80, "maintenance": 70, "feature": 0, "other": 0, "epic": 60}
	if !reflect.DeepEqual(gotCfg.Autonomous.PriorityByKind, want) {
		t.Fatalf("GET /api/config priority_by_kind = %v, want %v", gotCfg.Autonomous.PriorityByKind, want)
	}
	if gotCfg.Autonomous.DefectIntakeAutomation != "my-auto" {
		t.Fatalf("GET /api/config defect_intake_automation = %q", gotCfg.Autonomous.DefectIntakeAutomation)
	}

	// Same validation on the autonomous.* key: bad values are a 400 and
	// propagate nothing.
	spy.calls = 0
	for name, raw := range map[string]string{
		"non-integer": `{"autonomous.priority_by_kind":{"bug":"high"}}`,
		"fraction":    `{"autonomous.priority_by_kind":{"bug":2.5}}`,
		"non-string":  `{"autonomous.defect_intake_automation":7}`,
	} {
		spy.calls = 0
		br := httptest.NewRecorder()
		s.handlePutConfig(br, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader([]byte(raw))))
		if br.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", name, br.Code, br.Body.String())
		}
		if spy.calls != 0 {
			t.Errorf("%s: SetConfig was called after a rejected value", name)
		}
	}
}

// Plan record config keys (autonomous.plan_records.*) over REST. GET
// /api/config returns the effective values, an explicit false survives a PUT
// and reaches config.yaml, and a bad value is a 400 that names the key,
// applies nothing and writes nothing.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
)

type planRecordSurfaceView struct {
	Enabled              bool   `json:"enabled"`
	WriteThrough         bool   `json:"write_through"`
	ImportConflictPolicy string `json:"import_conflict_policy"`
}

func getPlanRecordSurface(t *testing.T, s *Server) planRecordSurfaceView {
	t.Helper()
	raw, ok := getAutonomousConfigMap(t, s)["plan_records"]
	if !ok {
		t.Fatal("GET /api/config autonomous map lacks plan_records")
	}
	var got planRecordSurfaceView
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode plan_records: %v", err)
	}
	return got
}

func TestPlanRecordConfigSurface_PutGet(t *testing.T) {
	s, _, cfgPath := newMaxConcurrentTasksServer(t)

	got := getPlanRecordSurface(t, s)
	want := planRecordSurfaceView{Enabled: true, WriteThrough: false, ImportConflictPolicy: "suffix"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GET defaults = %+v, want %+v", got, want)
	}
	if p := s.cfg.Autonomous.PlanRecords; p.Enabled != nil || p.WriteThrough != nil || p.ImportConflictPolicy != nil {
		t.Fatalf("stored values must be absent by default, got %+v", p)
	}

	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.plan_records.enabled":                false,
		"autonomous.plan_records.write_through":          true,
		"autonomous.plan_records.import_conflict_policy": "skip",
	})
	got = getPlanRecordSurface(t, s)
	want = planRecordSurfaceView{Enabled: false, WriteThrough: true, ImportConflictPolicy: "skip"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GET after PUT = %+v, want %+v", got, want)
	}
	if p := s.cfg.Autonomous.PlanRecords; p.Enabled == nil || *p.Enabled {
		t.Fatalf("an explicit false must be stored as such, got %+v", p)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	for _, frag := range []string{"plan_records:", "enabled: false", "write_through: true", "import_conflict_policy: skip"} {
		if !strings.Contains(string(raw), frag) {
			t.Errorf("config.yaml lacks %q:\n%s", frag, raw)
		}
	}

	// The switch is live: with enabled false the routes answer 404.
	if rec := prCall(t, s, http.MethodGet, prBase+"/findings", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("enabled=false via PUT must turn the routes off, got %d", rec.Code)
	}

	// An explicit false for write_through survives and reads back false.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.plan_records.write_through": false})
	if got = getPlanRecordSurface(t, s); got.WriteThrough {
		t.Fatalf("write_through false must read back false: %+v", got)
	}
}

func TestPlanRecordConfigSurface_PutRefusesBad(t *testing.T) {
	cases := []struct{ key, value, named string }{
		{"autonomous.plan_records.enabled", `"yes"`, "boolean"},
		{"autonomous.plan_records.write_through", `1`, "boolean"},
		{"autonomous.plan_records.import_conflict_policy", `"overwrite"`, "skip"},
		{"autonomous.plan_records.import_conflict_policy", `true`, "skip"},
	}
	for _, c := range cases {
		s, _, cfgPath := newMaxConcurrentTasksServer(t)
		body := `{"autonomous.plan_records.write_through": true, "` + c.key + `": ` + c.value + `}`
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(body)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("PUT %s=%s: want 400, got %d (%s)", c.key, c.value, rr.Code, rr.Body.String())
		}
		if msg := rr.Body.String(); !strings.Contains(msg, c.key) || !strings.Contains(msg, c.named) {
			t.Errorf("400 for %s=%s should name the key and %q; got %q", c.key, c.value, c.named, msg)
		}
		if p := s.cfg.Autonomous.PlanRecords; p.Enabled != nil || p.WriteThrough != nil || p.ImportConflictPolicy != nil {
			t.Fatalf("a rejected PUT (%s=%s) changed the stored config: %+v", c.key, c.value, p)
		}
		if _, err := os.Stat(cfgPath); err == nil {
			t.Fatalf("a rejected PUT (%s=%s) wrote config.yaml", c.key, c.value)
		}
	}
}

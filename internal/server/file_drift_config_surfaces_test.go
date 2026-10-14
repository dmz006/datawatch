// Foreign-file drift — Configuration Accessibility round-trip:
// PUT /api/config then GET /api/config must show
// autonomous.block_on_file_drift, the live manager must receive it, and
// an Automaton's task JSON must carry foreign_file_drift for the PWA /
// MCP autonomous_prd_get readers.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func TestConfigRoundTrip_BlockOnFileDrift(t *testing.T) {
	spy := &autonomousConfigPropagationSpy{}
	s := &Server{
		cfg:           &config.Config{},
		cfgPath:       t.TempDir() + "/config.yaml",
		autonomousMgr: spy,
	}
	get := func() bool {
		t.Helper()
		rr := httptest.NewRecorder()
		s.handleGetConfig(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET: want 200, got %d", rr.Code)
		}
		var got struct {
			Autonomous map[string]json.RawMessage `json:"autonomous"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode GET: %v", err)
		}
		raw, ok := got.Autonomous["block_on_file_drift"]
		if !ok {
			t.Fatalf("GET /api/config autonomous map lacks block_on_file_drift")
		}
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("decode block_on_file_drift: %v", err)
		}
		return v
	}
	put := func(v bool) {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"autonomous.block_on_file_drift": v})
		rr := httptest.NewRecorder()
		s.handlePutConfig(rr, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(b)))
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT %v: want 200, got %d body=%s", v, rr.Code, rr.Body.String())
		}
	}

	if get() {
		t.Fatal("block_on_file_drift default must be false (warn-only)")
	}
	put(true)
	if !get() {
		t.Fatal("GET block_on_file_drift = false after PUT true")
	}
	var fwd map[string]json.RawMessage
	if err := json.Unmarshal(spy.lastSetConfig, &fwd); err != nil {
		t.Fatalf("decode propagated payload: %v", err)
	}
	if string(fwd["block_on_file_drift"]) != "true" {
		t.Errorf("block_on_file_drift not propagated to manager: %s", spy.lastSetConfig)
	}

	// Clearing back to false must reach the live manager too.
	put(false)
	if get() {
		t.Fatal("GET block_on_file_drift = true after PUT false")
	}
	fwd = nil
	if err := json.Unmarshal(spy.lastSetConfig, &fwd); err != nil {
		t.Fatalf("decode propagated payload: %v", err)
	}
	if string(fwd["block_on_file_drift"]) != "false" {
		t.Errorf("block_on_file_drift=false not propagated to manager: %s", spy.lastSetConfig)
	}
}

func TestAutomatonTaskJSON_CarriesForeignFileDrift(t *testing.T) {
	prd := autonomous.PRD{
		ID: "a1",
		Story: []autonomous.Story{{
			ID: "s1",
			Tasks: []autonomous.Task{
				{ID: "t1", ForeignFileDrift: []string{"docs/notes.md", "internal/x.go"}},
				{ID: "t2"},
			},
		}},
	}
	b, err := json.Marshal(prd)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Stories []struct {
			Tasks []map[string]json.RawMessage `json:"tasks"`
		} `json:"stories"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	tasks := got.Stories[0].Tasks
	if string(tasks[0]["foreign_file_drift"]) != `["docs/notes.md","internal/x.go"]` {
		t.Errorf("task foreign_file_drift = %s", tasks[0]["foreign_file_drift"])
	}
	if _, ok := tasks[1]["foreign_file_drift"]; ok {
		t.Errorf("task without drift must omit foreign_file_drift: %v", tasks[1])
	}
}

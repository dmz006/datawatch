package mcp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestArchivedUnmergedToolDefinitions(t *testing.T) {
	s := &Server{}
	list := s.toolArchivedUnmergedList()
	if list.Name != "archived_unmerged_list" || !strings.Contains(list.Description, "/api/autonomous/archived-unmerged") || !strings.Contains(list.Description, "Read-only") {
		t.Errorf("list tool = %+v", list)
	}
	carry := s.toolArchivedUnmergedCarryForward()
	if carry.Name != "archived_unmerged_carry_forward" || !strings.Contains(carry.Description, "carry-forward") {
		t.Errorf("carry tool = %+v", carry)
	}
	if len(carry.InputSchema.Required) != 1 || carry.InputSchema.Required[0] != "id" {
		t.Errorf("required = %v", carry.InputSchema.Required)
	}
}

func TestArchivedUnmergedListProxiesCompletedRecord(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`{"archived_unmerged":[{"id":"done-1","title":"Parser rewrite","former_status":"completed","ahead":3}],"total":1}`))
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}
	res, err := s.handleArchivedUnmergedList(t.Context(), callReq(nil))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if gotMethod != "GET" || gotPath != "/api/autonomous/archived-unmerged" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if out := resContentText(t, res); !strings.Contains(out, `"former_status":"completed"`) {
		t.Errorf("result = %s", out)
	}
}

func TestArchivedUnmergedCarryForwardProxiesAndRefuses(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"id":"draft-1","title":"Parser rewrite","status":"draft"}`))
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv)}

	res, err := s.handleArchivedUnmergedCarryForward(t.Context(), callReq(map[string]any{"id": " done-1 ", "force": true}))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if gotMethod != "POST" || gotPath != "/api/autonomous/archived-unmerged/done-1/carry-forward" || !strings.Contains(gotBody, `"force":true`) {
		t.Errorf("request = %s %s %s", gotMethod, gotPath, gotBody)
	}
	if out := resContentText(t, res); !strings.Contains(out, "draft-1") {
		t.Errorf("result = %s", out)
	}

	gotPath = ""
	res, _ = s.handleArchivedUnmergedCarryForward(t.Context(), callReq(map[string]any{}))
	if !res.IsError || gotPath != "" {
		t.Errorf("missing id must be refused without a call (path %q)", gotPath)
	}
	status = http.StatusConflict
	res, _ = s.handleArchivedUnmergedCarryForward(t.Context(), callReq(map[string]any{"id": "done-1"}))
	if !res.IsError {
		t.Errorf("daemon 409 must be an error result")
	}
}

func TestArchivedUnmergedKeepSettingViaConfigSetAndAutonomousSet(t *testing.T) {
	const key = "autonomous.archive_keep_unmerged"
	if desc := (&Server{}).toolConfigSet().Description; !strings.Contains(desc, key) {
		t.Errorf("config_set description does not mention %s", key)
	}
	if _, ok := (&Server{}).toolAutonomousConfigSet().InputSchema.Properties["archive_keep_unmerged"]; !ok {
		t.Errorf("autonomous_config_set lacks archive_keep_unmerged")
	}
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv), cfg: &config.MCPConfig{AllowSelfConfig: config.BoolPtr(true)}}
	res, err := s.handleConfigSet(t.Context(), callReq(map[string]any{"key": key, "value": "false"}))
	if err != nil || res.IsError {
		t.Fatalf("err=%v res=%v", err, res)
	}
	if !strings.Contains(gotBody, key) {
		t.Errorf("daemon got %q", gotBody)
	}
}

func TestArchivedUnmergedKeepSettingBadValueRefusedByDaemon(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "autonomous.archive_keep_unmerged: must be true or false, got maybe", http.StatusBadRequest)
	}))
	defer srv.Close()
	s := &Server{webPort: bl427Port(t, srv), cfg: &config.MCPConfig{AllowSelfConfig: config.BoolPtr(true)}}
	res, err := s.handleConfigSet(t.Context(), callReq(map[string]any{"key": "autonomous.archive_keep_unmerged", "value": "maybe"}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(automationText(t, res), "archive_keep_unmerged") {
		t.Errorf("bad value must come back as the daemon's refusal: %+v", res)
	}
}

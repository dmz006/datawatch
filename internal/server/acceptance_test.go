package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/acceptance"
	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/config"
)

func acceptanceServer(t *testing.T, mode string) (*Server, *audit.Log) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Acceptance.Mode = mode
	cfg.Acceptance.ProjectChecks = map[string]config.AcceptanceProjectCheck{"unit": {
		Command: "make test TOKEN=hunter2", Expect: []config.AcceptanceExpect{{Pass: "ok", Fail: "FAIL"}},
	}}
	al, err := audit.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = al.Close() })
	s := &Server{cfg: cfg, auditLog: al}
	svc, err := acceptance.Open(acceptance.Options{
		Path: filepath.Join(t.TempDir(), "a.json"), Mode: func() string { return cfg.Acceptance.Mode },
		Settings: func() acceptance.Settings {
			return acceptance.Settings{ProjectChecks: map[string]acceptance.ProjectCheck{"unit": {Command: "make test TOKEN=hunter2"}}}
		},
		Audit: s.auditAcceptance,
	})
	if err != nil {
		t.Fatal(err)
	}
	s.SetAcceptance(svc, false)
	return s, al
}

func acceptanceDo(s *Server, method, path, body string, sessionCaps []string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if sessionCaps != nil {
		req = req.WithContext(context.WithValue(req.Context(), sessionCapsKey, sessionCaps))
	}
	rr := httptest.NewRecorder()
	route := map[string]http.HandlerFunc{
		"/api/acceptance": s.handleAcceptance, "/api/acceptance/edit": s.handleAcceptanceEdit,
		"/api/acceptance/confirm": s.handleAcceptanceConfirm, "/api/acceptance/accept": s.handleAcceptanceAccept,
		"/api/acceptance/report": s.handleAcceptanceReport, "/api/acceptance/catalog": s.handleAcceptanceCatalog,
	}
	route[req.URL.Path](rr, req)
	return rr
}

func acceptanceDecoded(t *testing.T, rr *httptest.ResponseRecorder) acceptanceView {
	t.Helper()
	var v acceptanceView
	if rr.Code != 200 {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

const acceptanceNewBody = `{"id":"r1","source":"bug","ask":"Fix the secret export","actor":"cli",
 "items":[{"id":"fix","statement":"the export test passes","from":"Fix the secret export","method":"project:unit"},
  {"id":"seen","statement":"I saw it","method":"confirm"}]}`

func TestAcceptanceREST_OffAnswersAndWritesNothing(t *testing.T) {
	s, _ := acceptanceServer(t, "")
	rr := acceptanceDo(s, http.MethodGet, "/api/acceptance", "", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"mode":"off"`) {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	if rr = acceptanceDo(s, http.MethodPost, "/api/acceptance/edit", acceptanceNewBody, nil); rr.Code != http.StatusConflict {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	if rr = acceptanceDo(&Server{cfg: &config.Config{}}, http.MethodGet, "/api/acceptance", "", nil); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("not wired: %d", rr.Code)
	}
}

func TestAcceptanceREST_NoRouteTakesCommandTextOrAResult(t *testing.T) {
	s, _ := acceptanceServer(t, "report")
	for _, field := range []string{`"command":"rm -rf x"`, `"script":"x"`, `"script_path":"x"`, `"check":{"command":"x"}`, `"result":{"value":"met"}`, `"state_value":"met"`} {
		body := `{"id":"bad","ask":"a","items":[{"statement":"s","method":"command",` + field + `}]}`
		if rr := acceptanceDo(s, http.MethodPost, "/api/acceptance/edit", body, nil); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", field, rr.Code, rr.Body.String())
		}
	}
	for _, body := range []string{`{"id":"bad","ask":"a","verdict":"met"}`, `{"id":"bad","ask":"a","reviewed_hash":"x"}`, `{"id":"bad","ask":"a","holders":[]}`} {
		if rr := acceptanceDo(s, http.MethodPost, "/api/acceptance/edit", body, nil); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", body, rr.Code)
		}
	}
}

func TestAcceptanceREST_FourActionsAndHiddenCommandText(t *testing.T) {
	s, al := acceptanceServer(t, "report")
	v := acceptanceDecoded(t, acceptanceDo(s, http.MethodPost, "/api/acceptance/edit", acceptanceNewBody, nil))
	if v.List.Kind != acceptance.KindChange || v.Rollup.Status != acceptance.StatusDraft || v.Hash == "" {
		t.Fatalf("%+v", v.Rollup)
	}
	// Freeze is sent alone and names the list that was read.
	if rr := acceptanceDo(s, http.MethodPost, "/api/acceptance/edit", `{"id":"r1","freeze":true,"hash":"`+v.Hash+`","remove":["seen"]}`, nil); rr.Code != 400 {
		t.Fatalf("freeze with edits: %d", rr.Code)
	}
	if rr := acceptanceDo(s, http.MethodPost, "/api/acceptance/edit", `{"id":"r1","freeze":true,"hash":"stale"}`, nil); rr.Code != 409 {
		t.Fatalf("stale hash: %d", rr.Code)
	}
	v = acceptanceDecoded(t, acceptanceDo(s, http.MethodPost, "/api/acceptance/edit", `{"id":"r1","freeze":true,"hash":"`+v.Hash+`","words":"go"}`, nil))
	rr := acceptanceDo(s, http.MethodGet, "/api/acceptance?id=r1", "", nil)
	if strings.Contains(rr.Body.String(), "hunter2") {
		t.Fatal("saved command text was handed out")
	}
	got := acceptanceDecoded(t, rr)
	for _, it := range got.List.Items {
		if it.ID == "fix" && !strings.HasPrefix(it.Check.Command, "sha256:") {
			t.Fatalf("%+v", it.Check)
		}
	}
	if got.Hash != v.Hash {
		t.Fatal("hiding text changed the list hash")
	}
	if rr := acceptanceDo(s, http.MethodPost, "/api/acceptance/confirm", `{"id":"r1","item":"seen","words":"y"}`, nil); rr.Code != 400 {
		t.Fatalf("confirm without an answer: %d", rr.Code)
	}
	if rr := acceptanceDo(s, http.MethodPost, "/api/acceptance/confirm", `{"id":"r1","item":"fix","answer":"yes","words":"y"}`, nil); rr.Code != 400 {
		t.Fatalf("a person set a checked item: %d", rr.Code)
	}
	v = acceptanceDecoded(t, acceptanceDo(s, http.MethodPost, "/api/acceptance/confirm", `{"id":"r1","item":"seen","answer":"yes","words":"I saw the secret words","actor":"web"}`, nil))
	if c := v.List.State["seen"].Confirm; !c.Counts || c.By.Observed != "admin-token" || c.By.Claimed != "web" {
		t.Fatalf("%+v", c)
	}
	v = acceptanceDecoded(t, acceptanceDo(s, http.MethodPost, "/api/acceptance/accept", `{"id":"r1","item":"fix","reason":"no host","words":"accept","actor":"web"}`, nil))
	for _, r := range v.Rollup.Items {
		if r.ID == "fix" && (!r.Accepted || r.Result.Value == acceptance.ResultMet) {
			t.Fatalf("%+v", r)
		}
	}
	for _, path := range []string{"/api/acceptance/report", "/api/acceptance/catalog"} {
		rr := acceptanceDo(s, http.MethodGet, path, "", nil)
		if rr.Code != 200 || strings.Contains(rr.Body.String(), "hunter2") {
			t.Fatalf("%s: %d %s", path, rr.Code, rr.Body.String())
		}
	}
	entries, err := al.Read(audit.QueryFilter{Action: audit.ActionAcceptanceChanged, Limit: 50})
	if err != nil || len(entries) < 4 {
		t.Fatalf("audit: %v %d", err, len(entries))
	}
	raw, _ := json.Marshal(entries)
	for _, leak := range []string{"secret export", "secret words", "export test passes"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("audit holds the words: %s", leak)
		}
	}
}

func TestAcceptanceREST_ASessionIsObservedWhateverItClaims(t *testing.T) {
	s, _ := acceptanceServer(t, "report")
	v := acceptanceDecoded(t, acceptanceDo(s, http.MethodPost, "/api/acceptance/edit", acceptanceNewBody, nil))
	acceptanceDecoded(t, acceptanceDo(s, http.MethodPost, "/api/acceptance/edit", `{"id":"r1","freeze":true,"hash":"`+v.Hash+`"}`, nil))
	caps := []string{"autonomous:read", "autonomous:write"}
	for name, tc := range map[string]struct {
		body string
		caps []string
	}{
		"session credential claiming operator":  {`{"id":"r1","item":"seen","answer":"yes","words":"ok","actor":"operator"}`, caps},
		"session credential faking via_session": {`{"id":"r1","item":"seen","answer":"yes","words":"ok","via_session":"nobody"}`, caps},
		"model tool with the daemon token":      {`{"id":"r1","item":"seen","answer":"yes","words":"ok","actor":"operator","via":"mcp"}`, nil},
		"command line inside a session":         {`{"id":"r1","item":"seen","answer":"yes","words":"ok","via":"cli","via_session":"w1"}`, nil},
	} {
		v := acceptanceDecoded(t, acceptanceDo(s, http.MethodPost, "/api/acceptance/confirm", tc.body, tc.caps))
		if c := v.List.State["seen"].Confirm; c.Counts {
			t.Errorf("%s counted: %+v", name, c)
		}
		for _, r := range v.Rollup.Items {
			if r.ID == "seen" && r.Result.Value == acceptance.ResultMet {
				t.Errorf("%s read met", name)
			}
		}
	}
	// A session without the capability is refused as on any write route.
	if rr := acceptanceDo(s, http.MethodPost, "/api/acceptance/confirm", `{"id":"r1","item":"seen","answer":"yes","words":"x"}`, []string{"autonomous:read"}); rr.Code != http.StatusForbidden {
		t.Fatalf("%d", rr.Code)
	}
}

func TestAcceptanceConfigKeys(t *testing.T) {
	cfg := &config.Config{}
	applyConfigPatch(cfg, map[string]interface{}{
		"acceptance.mode": "report", "acceptance.rework_rounds": float64(3), "acceptance.infra_retries": float64(1),
		"acceptance.check_timeout_seconds": float64(30), "acceptance.freshness_seconds": float64(60), "acceptance.warn_hours": float64(4),
		"acceptance.project_checks": map[string]interface{}{"x": map[string]interface{}{"command": "evil"}}, "acceptance.defaults_file": "/tmp/weak.yaml",
	})
	a := cfg.Acceptance
	if a.Mode != "report" || a.ReworkRounds != 3 || a.InfraRetries != 1 || a.CheckTimeoutSeconds != 30 || a.FreshnessSeconds != 60 || a.WarnHours != 4 {
		t.Fatalf("%+v", a)
	}
	if len(a.ProjectChecks) != 0 || a.DefaultsFile != "" {
		t.Fatalf("a file-only setting was written through the API: %+v", a)
	}
	applyConfigPatch(cfg, map[string]interface{}{"acceptance.mode": "enforce"})
	if cfg.Acceptance.Mode != "report" {
		t.Fatalf("mode = %q", cfg.Acceptance.Mode)
	}
}

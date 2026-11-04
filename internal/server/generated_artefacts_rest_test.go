package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/changerecord"
	"github.com/dmz006/datawatch/internal/config"
)

// genArtFake stores one Automaton and one merged record; regenerate answers
// skipped for the kind named in skipKind (an edited file).
type genArtFake struct {
	fakeOrchAutonomous
	id       string
	rec      *changerecord.ChangeRecord
	skipKind string
}

func (f *genArtFake) GetPRD(id string) (any, bool) {
	if id != f.id {
		return nil, false
	}
	return &autonomous.PRD{ID: id}, true
}
func (f *genArtFake) GeneratedChangeRecord(id string) (*changerecord.ChangeRecord, error) {
	if id != f.id {
		return nil, autonomous.ErrGeneratedPRDNotFound
	}
	if f.rec == nil {
		return nil, autonomous.ErrGeneratedNoChangeData
	}
	return f.rec, nil
}
func (f *genArtFake) PreviewGeneratedArtefact(id, kind string) (*autonomous.GeneratedArtefactPreview, error) {
	return &autonomous.GeneratedArtefactPreview{ID: id, Kind: kind, Path: "docs/plans/drafts/" + id + "-x.md", Enabled: true, Content: "preview of " + kind}, nil
}
func (f *genArtFake) RegenerateGeneratedArtefact(id, kind string) (*autonomous.GeneratedArtefactResult, error) {
	if kind == f.skipKind {
		return &autonomous.GeneratedArtefactResult{ID: id, Kind: kind, Status: "skipped", Path: "p.md", Reason: "file was edited by a person"}, nil
	}
	return &autonomous.GeneratedArtefactResult{ID: id, Kind: kind, Status: "written", Path: "p.md"}, nil
}

func genArtServer(f *genArtFake) *Server {
	return &Server{autonomousMgr: f, cfg: config.DefaultConfig()}
}

func genArtDo(s *Server, method, path string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	switch path {
	case "/api/autonomous/generated-artefacts":
		s.handleGeneratedArtefacts(rr, httptest.NewRequest(method, path, nil))
	default:
		s.handleAutonomousPRDs(rr, httptest.NewRequest(method, path, nil))
	}
	return rr
}

func TestGeneratedArtefactsREST_Settings(t *testing.T) {
	s := genArtServer(&genArtFake{id: "p1"})
	off := false
	s.cfg.Autonomous.GeneratedArtefacts.TrackerRows = &off
	rr := genArtDo(s, http.MethodGet, "/api/autonomous/generated-artefacts")
	if rr.Code != 200 {
		t.Fatalf("code %d %s", rr.Code, rr.Body)
	}
	var out struct {
		Settings config.EffectiveGeneratedArtefactsSettings `json:"settings"`
		Kinds    []struct {
			Kind, Format string
			TrackedPath  string `json:"tracked_path"`
			Enabled      bool
		} `json:"kinds"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Settings.TrackerRows || !out.Settings.GateReport || len(out.Kinds) != 5 {
		t.Fatalf("settings/kinds wrong: %+v", out)
	}
	for _, k := range out.Kinds {
		if k.Format == "" || !strings.Contains(k.TrackedPath, "docs/") {
			t.Errorf("kind %s missing format/path: %+v", k.Kind, k)
		}
		if k.Kind == "tracker_rows" && k.Enabled {
			t.Error("tracker_rows should read as disabled")
		}
	}
	if rr := genArtDo(s, http.MethodPost, "/api/autonomous/generated-artefacts"); rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST code %d, want 405", rr.Code)
	}
}

func TestGeneratedArtefactsREST_ChangeRecord(t *testing.T) {
	f := &genArtFake{id: "p1", rec: &changerecord.ChangeRecord{AutomatonID: "p1", TestsAdded: []string{"TestX"}}}
	s := genArtServer(f)
	rr := genArtDo(s, http.MethodGet, "/api/autonomous/prds/p1/change-record")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "TestX") {
		t.Fatalf("code %d body %s", rr.Code, rr.Body)
	}
	if rr := genArtDo(s, http.MethodGet, "/api/autonomous/prds/zzz/change-record"); rr.Code != 404 {
		t.Errorf("unknown id code %d, want 404", rr.Code)
	}
	f.rec = nil
	if rr := genArtDo(s, http.MethodGet, "/api/autonomous/prds/p1/change-record"); rr.Code != 404 {
		t.Errorf("no record code %d, want 404", rr.Code)
	}
	if rr := genArtDo(s, http.MethodPost, "/api/autonomous/prds/p1/change-record"); rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST code %d, want 405", rr.Code)
	}
}

func TestGeneratedArtefactsREST_Preview(t *testing.T) {
	s := genArtServer(&genArtFake{id: "p1"})
	rr := genArtDo(s, http.MethodGet, "/api/autonomous/prds/p1/generated/gate_report")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "preview of gate_report") {
		t.Fatalf("code %d body %s", rr.Code, rr.Body)
	}
	for path, want := range map[string]int{
		"/api/autonomous/prds/zzz/generated/gate_report":     404,
		"/api/autonomous/prds/p1/generated/not_a_kind":       404,
		"/api/autonomous/prds/p1/generated/gate_report/what": 404,
	} {
		if rr := genArtDo(s, http.MethodGet, path); rr.Code != want {
			t.Errorf("GET %s code %d, want %d", path, rr.Code, want)
		}
	}
	if rr := genArtDo(s, http.MethodPost, "/api/autonomous/prds/p1/generated/gate_report"); rr.Code != 405 {
		t.Errorf("POST preview code %d, want 405", rr.Code)
	}
}

func TestGeneratedArtefactsREST_Regenerate(t *testing.T) {
	s := genArtServer(&genArtFake{id: "p1", skipKind: "parity_draft"})
	rr := genArtDo(s, http.MethodPost, "/api/autonomous/prds/p1/generated/gate_report/regenerate")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"status":"written"`) {
		t.Fatalf("code %d body %s", rr.Code, rr.Body)
	}
	rr = genArtDo(s, http.MethodPost, "/api/autonomous/prds/p1/generated/parity_draft/regenerate")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"status":"skipped"`) || !strings.Contains(rr.Body.String(), "edited") {
		t.Fatalf("edited file should be skipped: code %d body %s", rr.Code, rr.Body)
	}
	for path, want := range map[string]int{
		"/api/autonomous/prds/zzz/generated/gate_report/regenerate": 404,
		"/api/autonomous/prds/p1/generated/bogus/regenerate":        404,
	} {
		if rr := genArtDo(s, http.MethodPost, path); rr.Code != want {
			t.Errorf("POST %s code %d, want %d", path, rr.Code, want)
		}
	}
	if rr := genArtDo(s, http.MethodGet, "/api/autonomous/prds/p1/generated/gate_report/regenerate"); rr.Code != 405 {
		t.Errorf("GET regenerate code %d, want 405", rr.Code)
	}
}

func TestGeneratedArtefactsREST_NotSupportedManager(t *testing.T) {
	s := &Server{autonomousMgr: &pcFiltersReadAPI{prd: &autonomous.PRD{ID: "p1"}}, cfg: config.DefaultConfig()}
	if rr := genArtDo(s, http.MethodGet, "/api/autonomous/prds/p1/change-record"); rr.Code != http.StatusNotImplemented {
		t.Errorf("code %d, want 501", rr.Code)
	}
}

// Config: GET shows the effective switches, PUT /api/config patches them and
// the change reaches a running Manager through its provider without restart.
func TestGeneratedArtefactsConfig_GetPatchPropagates(t *testing.T) {
	dir := t.TempDir()
	mgr, err := autonomous.NewManager(filepath.Join(dir, "autonomous"), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.DefaultConfig(), cfgPath: filepath.Join(dir, "config.yaml"), autonomousMgr: newTestAPI(mgr)}
	mgr.SetGeneratedArtefacts(func() config.EffectiveGeneratedArtefactsSettings {
		return s.cfg.Autonomous.EffectiveGeneratedArtefacts()
	})
	read := func() map[string]bool {
		raw := getAutonomousConfigMap(t, s)["generated_artefacts"]
		var m map[string]bool
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("decode generated_artefacts %s: %v", raw, err)
		}
		return m
	}
	for k, v := range read() {
		if !v {
			t.Errorf("default %s = false, want on", k)
		}
	}
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{
		"autonomous.generated_artefacts.gate_report":        false,
		"autonomous.generated_artefacts.combined_docs_task": false,
	})
	got := read()
	if got["gate_report"] || got["combined_docs_task"] || !got["change_record"] {
		t.Errorf("after PUT: %+v", got)
	}
	// A later partial PUT keeps the explicit offs.
	putJSON(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.generated_artefacts.parity_draft": false})
	if got := read(); got["gate_report"] || got["parity_draft"] {
		t.Errorf("explicit off lost after partial PUT: %+v", got)
	}
	// Live Manager sees it with no restart: a disabled kind previews as disabled.
	prd, err := mgr.CreatePRD("spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Skipf("cannot create PRD in this harness: %v", err)
	}
	id := prdID(t, prd)
	pv, err := mgr.PreviewGeneratedArtefact(id, "gate_report")
	if err != nil || pv.Enabled {
		t.Fatalf("manager did not see gate_report off: %+v, %v", pv, err)
	}
	if pv, err = mgr.PreviewGeneratedArtefact(id, "changelog_draft"); err != nil || !pv.Enabled {
		t.Fatalf("changelog_draft should stay on: %+v, %v", pv, err)
	}
}

func prdID(t *testing.T, v any) string {
	t.Helper()
	b, _ := json.Marshal(v)
	var x struct{ ID string }
	_ = json.Unmarshal(b, &x)
	if x.ID == "" {
		t.Fatalf("no id in %s", b)
	}
	return x.ID
}

func TestGeneratedArtefactsConfig_InvalidValueRejected(t *testing.T) {
	s, _, _ := newPlanCoverageFiltersServer(t)
	for _, v := range []any{"yes", 1, "false"} {
		putExpect(t, s.handlePutConfig, "/api/config", map[string]any{"autonomous.generated_artefacts.tracker_rows": v}, http.StatusBadRequest)
	}
	if got := s.cfg.Autonomous.EffectiveGeneratedArtefacts(); !got.TrackerRows {
		t.Error("rejected PUT changed the config")
	}
}

// A federation peer without the autonomous read or write capability gets the
// same 403 for an existing and a missing Automaton id on every per-Automaton
// route, so the answer does not reveal which ids exist.
func TestGeneratedArtefactsREST_PeerWithoutCapSameAnswerForMissingID(t *testing.T) {
	s := genArtServer(&genArtFake{id: "p1"})
	routes := []struct{ method, suffix string }{
		{http.MethodGet, "/change-record"},
		{http.MethodGet, "/generated/gate_report"},
		{http.MethodPost, "/generated/gate_report/regenerate"},
	}
	for _, rt := range routes {
		var bodies []string
		for _, id := range []string{"p1", "zzz"} {
			req := asPeer(httptest.NewRequest(rt.method, "/api/autonomous/prds/"+id+rt.suffix, nil), "autonomous:list")
			rr := httptest.NewRecorder()
			s.handleAutonomousPRDs(rr, req)
			if rr.Code != http.StatusForbidden {
				t.Errorf("%s %s id=%s: code %d, want 403", rt.method, rt.suffix, id, rr.Code)
			}
			bodies = append(bodies, rr.Body.String())
		}
		if bodies[0] != bodies[1] {
			t.Errorf("%s %s: body differs between existing and missing id: %q vs %q", rt.method, rt.suffix, bodies[0], bodies[1])
		}
	}
}

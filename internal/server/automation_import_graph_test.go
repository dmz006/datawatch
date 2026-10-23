package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/orchestrator"
)

// readOnlyOrchestrator exposes only GetGraph of a real orchestrator store. The
// embedded nil interface panics on any other method, so a handler that tried to
// save or cancel the source graph would fail the test.
type readOnlyOrchestrator struct {
	OrchestratorAPI
	st *orchestrator.Store
}

func (o readOnlyOrchestrator) GetGraph(id string) (any, bool) {
	g, ok := o.st.GetGraph(id)
	if !ok {
		return nil, false
	}
	return g, true
}

type importFixture struct {
	s    *Server
	mgr  *autonomous.Manager
	al   *audit.Log
	st   *orchestrator.Store
	dir  string
	prds []string
}

func newImportFixture(t *testing.T) *importFixture {
	t.Helper()
	s, mgr, al := newAutomationAuditServer(t)
	dir := t.TempDir()
	st, err := orchestrator.NewStore(dir)
	if err != nil {
		t.Fatalf("orchestrator.NewStore: %v", err)
	}
	s.SetOrchestratorAPI(readOnlyOrchestrator{st: st})
	f := &importFixture{s: s, mgr: mgr, al: al, st: st, dir: dir}
	for i := 0; i < 4; i++ {
		p, err := mgr.CreatePRD("spec", t.TempDir(), "", "", "")
		if err != nil {
			t.Fatalf("CreatePRD: %v", err)
		}
		f.prds = append(f.prds, p.ID)
	}
	return f
}

// graph stores a graph whose nodes are {id, prd index, depends_on...}.
func (f *importFixture) graph(t *testing.T, nodes ...orchestrator.Node) string {
	t.Helper()
	g, err := f.st.CreateGraph("g", "", f.prds)
	if err != nil {
		t.Fatalf("CreateGraph: %v", err)
	}
	for i := range nodes {
		nodes[i].GraphID = g.ID
		nodes[i].Kind = orchestrator.NodeKindPRD
		nodes[i].Status = orchestrator.NodePending
	}
	g.Nodes = nodes
	if err := f.st.SaveGraph(g); err != nil {
		t.Fatalf("SaveGraph: %v", err)
	}
	return g.ID
}

func (f *importFixture) diamond(t *testing.T) string {
	return f.graph(t,
		orchestrator.Node{ID: "a", PRDID: f.prds[0]},
		orchestrator.Node{ID: "b", PRDID: f.prds[1], DependsOn: []string{"a"}},
		orchestrator.Node{ID: "c", PRDID: f.prds[2], DependsOn: []string{"a"}},
		orchestrator.Node{ID: "d", PRDID: f.prds[3], DependsOn: []string{"b", "c"}},
	)
}

// disconnected is a graph an Automation cannot express.
func (f *importFixture) disconnected(t *testing.T) string {
	return f.graph(t,
		orchestrator.Node{ID: "a", PRDID: f.prds[0]},
		orchestrator.Node{ID: "b", PRDID: f.prds[1]},
	)
}

func (f *importFixture) post(graphID string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/automations/import-graph/"+graphID, bytes.NewReader(b))
	rr := httptest.NewRecorder()
	f.s.handleAutomations(rr, req)
	return rr
}

func (f *importFixture) importEntries(t *testing.T) []audit.Entry {
	t.Helper()
	es, err := f.al.Read(audit.QueryFilter{Action: audit.ActionAutomationImportGraph})
	if err != nil {
		t.Fatalf("audit read: %v", err)
	}
	return es
}

func TestAutomationImportGraphREST(t *testing.T) {
	f := newImportFixture(t)
	gid := f.diamond(t)
	rr := f.post(gid, map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var a struct {
		ID      string           `json:"id"`
		Members []map[string]any `json:"members"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &a); err != nil || a.ID == "" || len(a.Members) == 0 {
		t.Fatalf("bad Automation body (err=%v): %s", err, rr.Body.String())
	}
	if _, ok := f.mgr.GetAutomation(a.ID); !ok {
		t.Fatalf("Automation %s was not stored", a.ID)
	}
	es := f.importEntries(t)
	if len(es) != 1 {
		t.Fatalf("want 1 audit entry, got %d", len(es))
	}
	d := es[0].Details
	if d["graph_id"] != gid || d["automation_id"] != a.ID || d["member_count"] != float64(len(a.Members)) {
		t.Errorf("audit details = %v", d)
	}
}

func TestAutomationImportGraphRESTDryRun(t *testing.T) {
	f := newImportFixture(t)
	rr := f.post(f.diamond(t), map[string]any{"dry_run": true})
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var res struct {
		Input  map[string]any `json:"input"`
		DryRun bool           `json:"dry_run"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil || !res.DryRun || res.Input == nil {
		t.Fatalf("bad dry run body (err=%v): %s", err, rr.Body.String())
	}
	if n := len(f.mgr.ListAutomations()); n != 0 {
		t.Errorf("dry run stored %d Automations", n)
	}
	if n := len(f.importEntries(t)); n != 0 {
		t.Errorf("dry run wrote %d audit entries", n)
	}
}

func TestAutomationImportGraphRESTRefusal(t *testing.T) {
	f := newImportFixture(t)
	rr := f.post(f.disconnected(t), map[string]any{})
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "disconnected") {
		t.Errorf("refusal carries no reason: %q", rr.Body.String())
	}
	if n := len(f.mgr.ListAutomations()); n != 0 {
		t.Errorf("refusal stored %d Automations", n)
	}
	if n := len(f.importEntries(t)); n != 0 {
		t.Errorf("refusal wrote %d audit entries", n)
	}
	if rr := f.post("nosuchgraph", map[string]any{}); rr.Code != http.StatusNotFound {
		t.Errorf("unknown graph: want 404, got %d", rr.Code)
	}
}

func TestAutomationImportGraphRESTUnavailable(t *testing.T) {
	f := newImportFixture(t)
	f.s.orchestratorAPI = nil
	if rr := f.post("x", map[string]any{}); rr.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", rr.Code)
	}
}

func TestAutomationImportGraphRESTOwnershipConflict(t *testing.T) {
	f := newImportFixture(t)
	gid := f.diamond(t)
	if rr := f.post(gid, map[string]any{}); rr.Code != http.StatusOK {
		t.Fatalf("first import: %d %s", rr.Code, rr.Body.String())
	}
	if rr := f.post(gid, map[string]any{}); rr.Code != http.StatusConflict {
		t.Errorf("second import of the same members: want 409, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestAutomationImportGraphSourceUntouched(t *testing.T) {
	f := newImportFixture(t)
	storeFile := filepath.Join(f.dir, "orchestrator", "graphs.jsonl")
	snapshot := func(id string) (file, graph []byte) {
		t.Helper()
		file, err := os.ReadFile(storeFile)
		if err != nil {
			t.Fatalf("read store file: %v", err)
		}
		g, ok := f.st.GetGraph(id)
		if !ok {
			t.Fatalf("graph %s vanished", id)
		}
		graph, _ = json.Marshal(g)
		return file, graph
	}
	diamond, bad := f.diamond(t), f.disconnected(t)
	for _, c := range []struct {
		name string
		id   string
		body map[string]any
		want int
	}{
		{"dry run", diamond, map[string]any{"dry_run": true}, http.StatusOK},
		{"refusal", bad, map[string]any{}, http.StatusUnprocessableEntity},
		{"import", diamond, map[string]any{}, http.StatusOK},
	} {
		beforeFile, beforeGraph := snapshot(c.id)
		if rr := f.post(c.id, c.body); rr.Code != c.want {
			t.Fatalf("%s: want %d, got %d: %s", c.name, c.want, rr.Code, rr.Body.String())
		}
		afterFile, afterGraph := snapshot(c.id)
		if !bytes.Equal(beforeFile, afterFile) {
			t.Errorf("%s changed the orchestrator store file", c.name)
		}
		if !bytes.Equal(beforeGraph, afterGraph) {
			t.Errorf("%s changed the source graph", c.name)
		}
	}
}

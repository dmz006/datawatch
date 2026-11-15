// plan_records_test.go — the plan record routes under /api/plan-records. The
// real store stands behind the handler over temp files, with the operator
// audit log wired the way the daemon wires it.

package server

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/planrecords"
)

const prBase = "/api/plan-records"

type planRecordsRig struct {
	s       *Server
	store   *planrecords.Store
	audit   *audit.Log
	project string // registered alias "datawatch"
	dir     string // its directory
}

func newPlanRecordsRig(t *testing.T) *planRecordsRig {
	t.Helper()
	root := t.TempDir()
	projDir := filepath.Join(root, "proj")
	copyPlanFixture(t, filepath.Join("..", "planrecords", "testdata", "project"), projDir)
	cfg := config.DefaultConfig()
	cfg.Projects = map[string]config.ProjectConfigEntry{"datawatch": {Dir: projDir}, "other": {Dir: filepath.Join(root, "other")}}
	cfg.Server.AllowedProjectRoots = []string{root}
	al, err := audit.New(filepath.Join(root, "audit"))
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	t.Cleanup(func() { _ = al.Close() })
	st, err := planrecords.Open(filepath.Join(root, "data"), nil, planrecords.Options{
		ImportConflictPolicy: planrecords.PolicySuffix,
		Audit:                PlanRecordsAuditFunc(al),
	})
	if err != nil {
		t.Fatalf("planrecords.Open: %v", err)
	}
	s := &Server{cfg: cfg}
	s.SetAuditLog(al)
	s.SetPlanRecords(st)
	return &planRecordsRig{s: s, store: st, audit: al, project: "datawatch", dir: projDir}
}

func copyPlanFixture(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
}

func prCall(t *testing.T, s *Server, method, path string, body any, caps ...string) *httptest.ResponseRecorder {
	t.Helper()
	var rd *bytes.Reader
	switch b := body.(type) {
	case nil:
		rd = bytes.NewReader(nil)
	case string:
		rd = bytes.NewReader([]byte(b))
	default:
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rd)
	if len(caps) > 0 {
		req = asPeer(req, caps...)
	}
	rec := httptest.NewRecorder()
	s.handlePlanRecords(rec, req)
	return rec
}

func prMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, rec.Body.String())
	}
	return m
}

func prRecord(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	r, ok := prMap(t, rec)["record"].(map[string]any)
	if !ok {
		t.Fatalf("no record in %s", rec.Body.String())
	}
	return r
}

func (r *planRecordsRig) create(t *testing.T, coll string, body map[string]any) map[string]any {
	t.Helper()
	if _, ok := body["project"]; !ok {
		body["project"] = r.project
	}
	rec := prCall(t, r.s, http.MethodPost, prBase+"/"+coll, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %v: want 201, got %d %s", body, rec.Code, rec.Body.String())
	}
	return prRecord(t, rec)
}

func prAuditActions(t *testing.T, l *audit.Log) []audit.Entry {
	t.Helper()
	es, err := l.Read(audit.QueryFilter{Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	var out []audit.Entry
	// Read answers newest first; the tests read oldest first.
	for i := len(es) - 1; i >= 0; i-- {
		if strings.HasPrefix(es[i].Action, "plan_record_") {
			out = append(out, es[i])
		}
	}
	return out
}

func TestPlanRecordCreateGetUpdateListSearch(t *testing.T) {
	r := newPlanRecordsRig(t)
	row := r.create(t, "registry-rows", map[string]any{"title": "Move plans into the daemon", "body": "Keep the registry as records.",
		"state": "planned", "legacy_number": 412})
	if row["kind"] != "registry_row" || row["state"] != "planned" || row["id"] == "" || row["uid"] == "" {
		t.Fatalf("bad created row: %v", row)
	}
	if row["id"] == "412" || strings.Contains(row["id"].(string), "412") {
		t.Fatalf("a new record must not take the row number as its id: %v", row["id"])
	}
	fnd := r.create(t, "findings", map[string]any{"title": "Import drops a heading", "raised_by": "ab12cd34"})
	if fnd["kind"] != "finding" || fnd["state"] != "open" {
		t.Fatalf("bad created finding: %v", fnd)
	}

	// Title is the first key of the body and the id the second.
	rec := prCall(t, r.s, http.MethodGet, prBase+"/registry-rows/"+row["id"].(string), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get by id: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if ti, ii := strings.Index(body, `"title"`), strings.Index(body, `"id"`); ti < 0 || ii < 0 || ti > ii {
		t.Fatalf("title must come before id: %s", body)
	}

	// Every kind of reference resolves.
	for _, ref := range []string{row["uid"].(string), "row-412", "Move plans into the daemon", "move plans", "MOVE PLANS INTO THE DAEMON"} {
		rec := prCall(t, r.s, http.MethodGet, prBase+"/registry-rows/"+strings.ReplaceAll(ref, " ", "%20"), nil)
		if rec.Code != http.StatusOK || prRecord(t, rec)["id"] != row["id"] {
			t.Fatalf("ref %q: %d %s", ref, rec.Code, rec.Body.String())
		}
	}

	// Update: state closes the row; a rename keeps the old title as an alias.
	rec = prCall(t, r.s, http.MethodPut, prBase+"/registry-rows/row-412",
		map[string]any{"state": "shipped", "title": "Plans live in the daemon", "shipped_version": "free text"})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	u := prRecord(t, rec)
	if u["state"] != "shipped" || u["closed_at"] == nil || u["title"] != "Plans live in the daemon" {
		t.Fatalf("bad updated row: %v", u)
	}
	if al, _ := u["aliases"].([]any); len(al) != 1 || al[0] != "Move plans into the daemon" {
		t.Fatalf("rename must keep the old title as an alias: %v", u["aliases"])
	}
	if rec := prCall(t, r.s, http.MethodGet, prBase+"/registry-rows/Move%20plans%20into%20the%20daemon", nil); rec.Code != http.StatusOK {
		t.Fatalf("old title must still resolve: %d", rec.Code)
	}

	// List: paging, filters and the total.
	rec = prCall(t, r.s, http.MethodGet, prBase+"/registry-rows?project=datawatch&state=shipped&limit=1", nil)
	m := prMap(t, rec)
	if rec.Code != http.StatusOK || m["total"].(float64) != 1 || len(m["records"].([]any)) != 1 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	rec = prCall(t, r.s, http.MethodGet, prBase+"/registry-rows?legacy_number=412", nil)
	if m := prMap(t, rec); m["total"].(float64) != 1 {
		t.Fatalf("legacy_number filter: %s", rec.Body.String())
	}
	rec = prCall(t, r.s, http.MethodGet, prBase+"/findings?automaton=ab12cd34", nil)
	if m := prMap(t, rec); m["total"].(float64) != 1 {
		t.Fatalf("automaton filter: %s", rec.Body.String())
	}
	rec = prCall(t, r.s, http.MethodGet, prBase+"/findings?q=heading", nil)
	if m := prMap(t, rec); m["total"].(float64) != 1 {
		t.Fatalf("list q filter: %s", rec.Body.String())
	}
	// Records of the other kind never show up in a collection.
	rec = prCall(t, r.s, http.MethodGet, prBase+"/findings", nil)
	if m := prMap(t, rec); m["total"].(float64) != 1 {
		t.Fatalf("findings list must hold only findings: %s", rec.Body.String())
	}

	// Search spans both kinds, best first, with the title first.
	rec = prCall(t, r.s, http.MethodGet, prBase+"/search?q=daemon", nil)
	m = prMap(t, rec)
	hits := m["hits"].([]any)
	if rec.Code != http.StatusOK || len(hits) < 1 {
		t.Fatalf("search: %d %s", rec.Code, rec.Body.String())
	}
	if hits[0].(map[string]any)["record"].(map[string]any)["id"] != row["id"] {
		t.Fatalf("search should find the renamed row first: %s", rec.Body.String())
	}
	if rec := prCall(t, r.s, http.MethodGet, prBase+"/search", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("search without q: want 400, got %d", rec.Code)
	}
}

func TestPlanRecordErrors409And404(t *testing.T) {
	r := newPlanRecordsRig(t)
	a := r.create(t, "registry-rows", map[string]any{"title": "Alpha plan one", "legacy_number": 7})
	b := r.create(t, "registry-rows", map[string]any{"title": "Alpha plan two"})
	r.create(t, "findings", map[string]any{"title": "A finding"})

	// Create with a taken title (case and spaces ignored): 409 naming the holder.
	rec := prCall(t, r.s, http.MethodPost, prBase+"/registry-rows", map[string]any{"project": "datawatch", "title": "  ALPHA   plan one "})
	if rec.Code != http.StatusConflict {
		t.Fatalf("title conflict on create: want 409, got %d %s", rec.Code, rec.Body.String())
	}
	m := prMap(t, rec)
	if !strings.Contains(m["error"].(string), "Alpha plan one") || m["existing"].(map[string]any)["id"] != a["id"] {
		t.Fatalf("409 must name the existing record: %s", rec.Body.String())
	}
	// The same title in another project is fine.
	if rec := prCall(t, r.s, http.MethodPost, prBase+"/registry-rows", map[string]any{"project": "other", "title": "Alpha plan one"}); rec.Code != http.StatusCreated {
		t.Fatalf("same title in another project: want 201, got %d %s", rec.Code, rec.Body.String())
	}
	// Taken row number.
	rec = prCall(t, r.s, http.MethodPost, prBase+"/registry-rows", map[string]any{"project": "datawatch", "title": "Other", "legacy_number": 7})
	if rec.Code != http.StatusConflict {
		t.Fatalf("row number conflict: want 409, got %d", rec.Code)
	}
	// Rename onto a taken title.
	rec = prCall(t, r.s, http.MethodPut, prBase+"/registry-rows/"+b["id"].(string), map[string]any{"title": "alpha plan one"})
	if rec.Code != http.StatusConflict || prMap(t, rec)["existing"] == nil {
		t.Fatalf("title conflict on rename: want 409 with existing, got %d %s", rec.Code, rec.Body.String())
	}
	// Ambiguous prefix: 409 with the candidates.
	rec = prCall(t, r.s, http.MethodGet, prBase+"/registry-rows/alpha", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("ambiguous title: want 409, got %d %s", rec.Code, rec.Body.String())
	}
	if c, _ := prMap(t, rec)["candidates"].([]any); len(c) < 2 {
		t.Fatalf("ambiguous answer must list candidates: %s", rec.Body.String())
	}
	rec = prCall(t, r.s, http.MethodPut, prBase+"/registry-rows/alpha", map[string]any{"body": "x"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("ambiguous update: want 409, got %d", rec.Code)
	}
	// 404s: unknown ref, a finding asked for through registry-rows, unknown path.
	for _, p := range []string{"/registry-rows/nothing-like-it", "/registry-rows/row-999", "/findings/" + a["id"].(string), "/other", "/registry-rows/x/y"} {
		if rec := prCall(t, r.s, http.MethodGet, prBase+p, nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: want 404, got %d", p, rec.Code)
		}
	}
	if rec := prCall(t, r.s, http.MethodPut, prBase+"/registry-rows/nothing-like-it", map[string]any{"body": "x"}); rec.Code != http.StatusNotFound {
		t.Errorf("PUT unknown: want 404, got %d", rec.Code)
	}
	// There is no delete route.
	if rec := prCall(t, r.s, http.MethodDelete, prBase+"/registry-rows/"+a["id"].(string), nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE: want 405, got %d", rec.Code)
	}
	// Bad input: 400.
	for name, c := range map[string]struct {
		method, path string
		body         any
	}{
		"bad state on create": {http.MethodPost, "/findings", map[string]any{"project": "datawatch", "title": "T", "state": "shipped"}},
		"unknown field":       {http.MethodPost, "/findings", map[string]any{"project": "datawatch", "title": "T", "kind": "finding"}},
		"empty title":         {http.MethodPost, "/findings", map[string]any{"project": "datawatch", "title": " "}},
		"not json":            {http.MethodPost, "/findings", "{"},
		"bad state on update": {http.MethodPut, "/registry-rows/" + a["id"].(string), map[string]any{"state": "fixed"}},
		"project in update":   {http.MethodPut, "/registry-rows/" + a["id"].(string), map[string]any{"project": "other"}},
		"bad limit":           {http.MethodGet, "/registry-rows?limit=abc", nil},
		"bad state filter":    {http.MethodGet, "/findings?state=nope", nil},
		"kind mismatch":       {http.MethodGet, "/findings?kind=registry_row", nil},
	} {
		if rec := prCall(t, r.s, c.method, prBase+c.path, c.body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d %s", name, rec.Code, rec.Body.String())
		}
	}
}

func TestPlanRecordProjectNotRegistered(t *testing.T) {
	r := newPlanRecordsRig(t)
	for _, proj := range []string{"nope", ""} {
		rec := prCall(t, r.s, http.MethodPost, prBase+"/registry-rows", map[string]any{"project": proj, "title": "X"})
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "project") {
			t.Errorf("project %q: want 400 naming project, got %d %s", proj, rec.Code, rec.Body.String())
		}
	}
	rec := prCall(t, r.s, http.MethodPost, prBase+"/import", map[string]any{"project": "nope"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("import into an unregistered project: want 400, got %d", rec.Code)
	}
	if r.store.Len() != 0 {
		t.Fatalf("nothing may be stored, have %d", r.store.Len())
	}
}

func TestPlanRecordCapabilitiesReadVersusWrite(t *testing.T) {
	r := newPlanRecordsRig(t)
	created := r.create(t, "findings", map[string]any{"title": "Seen by readers"})
	id := created["id"].(string)

	readOnly := []string{"autonomous:read"}
	for _, c := range []struct {
		method, path string
		body         any
		want         int
	}{
		{http.MethodGet, "/findings", nil, http.StatusOK},
		{http.MethodGet, "/findings/" + id, nil, http.StatusOK},
		{http.MethodGet, "/search?q=seen", nil, http.StatusOK},
		{http.MethodPost, "/findings", map[string]any{"project": "datawatch", "title": "W"}, http.StatusForbidden},
		{http.MethodPut, "/findings/" + id, map[string]any{"body": "w"}, http.StatusForbidden},
		{http.MethodPost, "/import?dry_run=true", map[string]any{}, http.StatusForbidden},
	} {
		if rec := prCall(t, r.s, c.method, prBase+c.path, c.body, readOnly...); rec.Code != c.want {
			t.Errorf("read-only peer %s %s: want %d, got %d", c.method, c.path, c.want, rec.Code)
		}
	}
	writeOnly := []string{"autonomous:write"}
	if rec := prCall(t, r.s, http.MethodGet, prBase+"/findings", nil, writeOnly...); rec.Code != http.StatusForbidden {
		t.Errorf("write-only peer read: want 403, got %d", rec.Code)
	}
	if rec := prCall(t, r.s, http.MethodPost, prBase+"/findings", map[string]any{"project": "datawatch", "title": "By writer"}, writeOnly...); rec.Code != http.StatusCreated {
		t.Errorf("write peer create: want 201, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := prCall(t, r.s, http.MethodGet, prBase+"/findings", nil, "sessions:read"); rec.Code != http.StatusForbidden {
		t.Errorf("unrelated cap: want 403, got %d", rec.Code)
	}
}

func TestPlanRecordDisabledSwitch(t *testing.T) {
	r := newPlanRecordsRig(t)
	off := false
	r.s.cfg.Autonomous.PlanRecords.Enabled = &off
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/registry-rows"}, {http.MethodPost, "/findings"}, {http.MethodGet, "/search?q=a"},
		{http.MethodPost, "/import"}, {http.MethodGet, "/findings/x"}, {http.MethodPut, "/registry-rows/x"},
	} {
		rec := prCall(t, r.s, c.method, prBase+c.path, map[string]any{})
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: want 404 while off, got %d", c.method, c.path, rec.Code)
		}
		if body := strings.TrimSpace(rec.Body.String()); body == "" || len(body) > 120 || strings.HasPrefix(body, "{") {
			t.Errorf("%s %s: want a short plain message, got %q", c.method, c.path, body)
		}
	}
	on := true
	r.s.cfg.Autonomous.PlanRecords.Enabled = &on
	if rec := prCall(t, r.s, http.MethodGet, prBase+"/registry-rows", nil); rec.Code != http.StatusOK {
		t.Fatalf("back on: want 200, got %d", rec.Code)
	}
	// Not wired at all: 503.
	r.s.SetPlanRecords(nil)
	if rec := prCall(t, r.s, http.MethodGet, prBase+"/registry-rows", nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no store: want 503, got %d", rec.Code)
	}
}

func TestPlanRecordImportDryRunVersusReal(t *testing.T) {
	r := newPlanRecordsRig(t)
	rec := prCall(t, r.s, http.MethodPost, prBase+"/import?dry_run=true", map[string]any{"project": "datawatch", "source": "all"})
	if rec.Code != http.StatusOK {
		t.Fatalf("dry run: %d %s", rec.Code, rec.Body.String())
	}
	dry := prMap(t, rec)["result"].(map[string]any)
	dryCreate := dry["counts"].(map[string]any)["create"].(float64)
	if dry["dry_run"] != true || dryCreate < 1 {
		t.Fatalf("dry run should preview creations: %s", rec.Body.String())
	}
	if r.store.Len() != 0 || len(prAuditActions(t, r.audit)) != 0 {
		t.Fatalf("a dry run must write nothing and audit nothing (records %d)", r.store.Len())
	}

	rec = prCall(t, r.s, http.MethodPost, prBase+"/import", map[string]any{"project": "datawatch", "source": "all"})
	if rec.Code != http.StatusOK {
		t.Fatalf("real run: %d %s", rec.Code, rec.Body.String())
	}
	real := prMap(t, rec)["result"].(map[string]any)
	if real["dry_run"] != false || real["counts"].(map[string]any)["create"].(float64) != dryCreate {
		t.Fatalf("real run must create what the dry run promised (%v): %s", dryCreate, rec.Body.String())
	}
	if float64(r.store.Len()) != dryCreate {
		t.Fatalf("store holds %d records, want %v", r.store.Len(), dryCreate)
	}
	// A second run changes nothing.
	rec = prCall(t, r.s, http.MethodPost, prBase+"/import", map[string]any{"project": "datawatch"})
	again := prMap(t, rec)["result"].(map[string]any)["counts"].(map[string]any)
	if again["create"].(float64) != 0 || float64(r.store.Len()) != dryCreate {
		t.Fatalf("a second import must be idempotent: %v", again)
	}
	// The import summary is audited once per real run.
	var imports int
	for _, e := range prAuditActions(t, r.audit) {
		if e.Action == audit.ActionPlanRecordImported {
			imports++
		}
	}
	if imports != 2 {
		t.Fatalf("want 2 import audit events (two real runs), got %d", imports)
	}
	// Source limits what is read.
	r2 := newPlanRecordsRig(t)
	rec = prCall(t, r2.s, http.MethodPost, prBase+"/import", map[string]any{"project": "datawatch", "source": "findings"})
	if rec.Code != http.StatusOK {
		t.Fatalf("findings only: %d %s", rec.Code, rec.Body.String())
	}
	recs, _ := r2.store.List(planrecords.Filter{Kind: planrecords.KindRegistryRow})
	if len(recs) != 0 {
		t.Fatalf("source=findings must not import registry rows, got %d", len(recs))
	}
	recs, _ = r2.store.List(planrecords.Filter{Kind: planrecords.KindFinding})
	if len(recs) == 0 {
		t.Fatalf("source=findings imported no findings")
	}
	for name, body := range map[string]any{"bad source": map[string]any{"source": "docs"}, "bad policy": map[string]any{"conflict_policy": "overwrite"}} {
		if rec := prCall(t, r2.s, http.MethodPost, prBase+"/import", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", name, rec.Code)
		}
	}
	if rec := prCall(t, r2.s, http.MethodPost, prBase+"/import?dry_run=maybe", map[string]any{}); rec.Code != http.StatusBadRequest {
		t.Errorf("bad dry_run: want 400, got %d", rec.Code)
	}
}

func TestPlanRecordAuditEventPerWrite(t *testing.T) {
	r := newPlanRecordsRig(t)
	rec := r.create(t, "findings", map[string]any{"title": "Audited"})
	if got := prAuditActions(t, r.audit); len(got) != 1 || got[0].Action != audit.ActionPlanRecordCreated || got[0].Subject != "Audited" {
		t.Fatalf("create must emit one created event with the title as subject: %+v", got)
	}
	if got := prAuditActions(t, r.audit); got[0].Details["id"] != rec["id"] || got[0].Actor != "operator" {
		t.Fatalf("created event details: %+v", got[0])
	}
	prCall(t, r.s, http.MethodGet, prBase+"/findings", nil)
	prCall(t, r.s, http.MethodGet, prBase+"/search?q=audited", nil)
	if n := len(prAuditActions(t, r.audit)); n != 1 {
		t.Fatalf("reads must not emit plan record events, have %d", n)
	}
	if res := prCall(t, r.s, http.MethodPut, prBase+"/findings/Audited", map[string]any{"state": "triaged"}); res.Code != http.StatusOK {
		t.Fatalf("update: %d", res.Code)
	}
	got := prAuditActions(t, r.audit)
	if len(got) != 2 || got[1].Action != audit.ActionPlanRecordUpdated {
		t.Fatalf("update must emit one updated event: %+v", got)
	}
	// A refused write emits nothing.
	prCall(t, r.s, http.MethodPost, prBase+"/findings", map[string]any{"project": "datawatch", "title": "audited"})
	prCall(t, r.s, http.MethodPut, prBase+"/findings/Audited", map[string]any{"state": "nope"})
	if n := len(prAuditActions(t, r.audit)); n != 2 {
		t.Fatalf("refused writes must not be audited, have %d", n)
	}
	// A peer is named as the actor.
	req := httptest.NewRequest(http.MethodPost, prBase+"/findings", strings.NewReader(`{"project":"datawatch","title":"From a peer"}`))
	req = asPeer(req, "autonomous:write")
	w := httptest.NewRecorder()
	r.s.handlePlanRecords(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("peer create: %d %s", w.Code, w.Body.String())
	}
	got = prAuditActions(t, r.audit)
	if last := got[len(got)-1]; !strings.HasPrefix(last.Actor, "peer:") {
		t.Fatalf("a peer write must name the peer, got actor %q", last.Actor)
	}
}

// A request only reads and writes files: no tmux, git or any other program is
// started. A fake tmux and git on PATH leave a marker file if one is run.
func TestPlanRecordRequestStartsNoProcess(t *testing.T) {
	r := newPlanRecordsRig(t)
	bin := t.TempDir()
	marker := filepath.Join(bin, "ran")
	for _, name := range []string{"tmux", "git", "sh", "bash", "systemctl"} {
		script := "#!/bin/sh\necho " + name + " >> " + marker + "\n"
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)

	row := r.create(t, "registry-rows", map[string]any{"title": "No process", "state": "planned"})
	prCall(t, r.s, http.MethodPut, prBase+"/registry-rows/"+row["id"].(string), map[string]any{"state": "done"})
	prCall(t, r.s, http.MethodGet, prBase+"/registry-rows", nil)
	prCall(t, r.s, http.MethodGet, prBase+"/search?q=process", nil)
	prCall(t, r.s, http.MethodPost, prBase+"/import?dry_run=true", map[string]any{"project": "datawatch"})
	prCall(t, r.s, http.MethodPost, prBase+"/import", map[string]any{"project": "datawatch"})
	if _, err := os.Stat(marker); err == nil {
		b, _ := os.ReadFile(marker)
		t.Fatalf("a plan-records request started a process: %s", b)
	}
}

func TestPlanRecordResponsesCarryNoTrackerIDs(t *testing.T) {
	r := newPlanRecordsRig(t)
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/import?dry_run=true", map[string]any{"project": "datawatch"}},
		{http.MethodPost, "/import", map[string]any{"project": "datawatch"}},
		{http.MethodPost, "/registry-rows", map[string]any{"project": "datawatch", "title": "Dup"}},
		{http.MethodPost, "/registry-rows", map[string]any{"project": "datawatch", "title": "Dup"}},
		{http.MethodGet, "/registry-rows/zzz", nil},
	} {
		rec := prCall(t, r.s, c.method, prBase+c.path, c.body)
		m := prMap(t, rec)
		for _, k := range []string{"note", "error"} {
			if v, ok := m[k].(string); ok && trackerIDRe.MatchString(v) {
				t.Errorf("%s %s: %s carries a tracker id: %q", c.method, c.path, k, v)
			}
		}
	}
}

var trackerIDRe = regexp.MustCompile(`\b(BL|B|GH)[0-9]{2,}\b`)

func TestPlanRecordArchiveAndRestoreAuditActions(t *testing.T) {
	r := newPlanRecordsRig(t)
	p, err := r.store.Create(planrecords.Record{Kind: planrecords.KindPlan, Project: "datawatch", Title: "Audited plan"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.store.Archive(p.ID, "tidy", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.store.Archive(p.ID, "tidy", "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.store.Restore(p.ID, "operator"); err != nil {
		t.Fatal(err)
	}
	got := prAuditActions(t, r.audit)
	if len(got) != 3 || got[1].Action != audit.ActionPlanRecordArchived || got[2].Action != audit.ActionPlanRecordRestored {
		t.Fatalf("want created, archived, restored once each: %+v", got)
	}
	if got[1].Actor != "operator" || got[1].Subject != "Audited plan" || got[1].Details["reason"] != "tidy" {
		t.Fatalf("archived entry: %+v", got[1])
	}
}

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func newTagTestServer(t *testing.T) (*Server, *autonomous.Manager) {
	t.Helper()
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("autonomous.NewManager: %v", err)
	}
	return &Server{autonomousMgr: newTestAPI(mgr)}, mgr
}

// tagCall issues a request through the matching handler and returns the
// recorder.
func tagCall(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	if strings.Contains(path, "/tags/sweep") {
		s.handleTagSweep(rec, req)
		return rec
	}
	if strings.HasPrefix(path, "/api/autonomous/tags") {
		s.handleAutonomousTags(rec, req)
	} else {
		s.handleAutonomousPRDs(rec, req)
	}
	return rec
}

type tagJSON struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parent_id"`
	Color    string `json:"color"`
	Desc     string `json:"description"`
	Count    int    `json:"count"`
	System   bool   `json:"system"`
}

func listTags(t *testing.T, s *Server, query string) map[string]tagJSON {
	t.Helper()
	rec := tagCall(t, s, http.MethodGet, "/api/autonomous/tags"+query, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list tags: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Tags []tagJSON `json:"tags"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	m := map[string]tagJSON{}
	for _, tg := range out.Tags {
		m[tg.ID] = tg
	}
	return m
}

func createTag(t *testing.T, s *Server, body string) tagJSON {
	t.Helper()
	rec := tagCall(t, s, http.MethodPost, "/api/autonomous/tags", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("create tag %s: %d %s", body, rec.Code, rec.Body.String())
	}
	var tg tagJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &tg); err != nil {
		t.Fatalf("decode tag: %v", err)
	}
	return tg
}

func TestTagCreateListUpdateRoundTrip(t *testing.T) {
	s, _ := newTagTestServer(t)
	parent := createTag(t, s, `{"name":"infra","color":"#112233"}`)
	child := createTag(t, s, `{"name":"net","parent_id":"`+parent.ID+`","description":"networking"}`)
	if len(parent.ID) != 8 {
		t.Errorf("tag id %q: want 8 hex chars", parent.ID)
	}

	got := listTags(t, s, "")
	if got[parent.ID].Name != "infra" || got[parent.ID].Color != "#112233" {
		t.Errorf("parent after create = %+v", got[parent.ID])
	}
	if got[child.ID].ParentID != parent.ID || got[child.ID].Desc != "networking" {
		t.Errorf("child after create = %+v", got[child.ID])
	}

	rec := tagCall(t, s, http.MethodPatch, "/api/autonomous/tags/"+child.ID,
		`{"name":"network","color":"#ff0000","description":"net stuff","parent_id":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	got = listTags(t, s, "")
	c := got[child.ID]
	if c.Name != "network" || c.Color != "#ff0000" || c.Desc != "net stuff" || c.ParentID != "" {
		t.Errorf("child after patch = %+v", c)
	}

	if rec := tagCall(t, s, http.MethodPost, "/api/autonomous/tags", `{"name":"  "}`); rec.Code != http.StatusBadRequest {
		t.Errorf("blank name: %d, want 400", rec.Code)
	}
	if rec := tagCall(t, s, http.MethodPatch, "/api/autonomous/tags/nosuchid", `{"name":"x"}`); rec.Code != http.StatusNotFound {
		t.Errorf("patch unknown: %d, want 404", rec.Code)
	}
}

func TestTagPatchCycleRefused(t *testing.T) {
	s, _ := newTagTestServer(t)
	a := createTag(t, s, `{"name":"a"}`)
	b := createTag(t, s, `{"name":"b","parent_id":"`+a.ID+`"}`)
	c := createTag(t, s, `{"name":"c","parent_id":"`+b.ID+`"}`)

	for _, id := range []string{a.ID, b.ID} {
		rec := tagCall(t, s, http.MethodPatch, "/api/autonomous/tags/"+id, `{"parent_id":"`+c.ID+`"}`)
		if rec.Code != http.StatusConflict {
			t.Fatalf("reparent %s under descendant: %d %s, want 409", id, rec.Code, rec.Body.String())
		}
	}
	if rec := tagCall(t, s, http.MethodPatch, "/api/autonomous/tags/"+a.ID, `{"parent_id":"`+a.ID+`"}`); rec.Code != http.StatusConflict {
		t.Errorf("self-parent: %d, want 409", rec.Code)
	}
	if got := listTags(t, s, ""); got[a.ID].ParentID != "" {
		t.Errorf("refused reparent changed the tag: %+v", got[a.ID])
	}
}

func TestTagDeleteRefusedWithChildren(t *testing.T) {
	s, _ := newTagTestServer(t)
	parent := createTag(t, s, `{"name":"parent"}`)
	child := createTag(t, s, `{"name":"child","parent_id":"`+parent.ID+`"}`)

	rec := tagCall(t, s, http.MethodDelete, "/api/autonomous/tags/"+parent.ID, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete parent with child: %d %s, want 409", rec.Code, rec.Body.String())
	}
	if got := listTags(t, s, ""); len(got) != 2 {
		t.Fatalf("tags after refused delete = %d, want 2", len(got))
	}

	if rec := tagCall(t, s, http.MethodDelete, "/api/autonomous/tags/"+child.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("delete leaf: %d %s", rec.Code, rec.Body.String())
	}
	if rec := tagCall(t, s, http.MethodDelete, "/api/autonomous/tags/"+parent.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("delete parent after child gone: %d %s", rec.Code, rec.Body.String())
	}
	if got := listTags(t, s, ""); len(got) != 0 {
		t.Errorf("tags after deletes = %d, want 0", len(got))
	}
}

func TestTagPatchSystemRefusedRenameReparentDelete(t *testing.T) {
	s, m := newTagTestServer(t)
	on := true
	m.SetAutoTagsDaemon(func() config.AutoTagsConfig { return config.AutoTagsConfig{Enabled: &on} })
	prd, err := m.CreatePRD("Ship the dark mode toggle", "test-project", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if _, err := m.CreateAutomation(autonomous.AutomationInput{
		Name:    "Alpha",
		Members: []autonomous.AutomationMember{{PRDID: prd.ID, Stage: "draft"}},
	}, "operator"); err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	var sys string
	for _, tg := range m.ListTags() {
		if tg.Name == "stage:draft" {
			sys = tg.ID
		}
	}
	if sys == "" {
		t.Fatal("the engine did not create the system tag stage:draft")
	}
	// A second operator tag to reparent under.
	other := createTag(t, s, `{"name":"operator-note"}`)

	// The list payload carries the system flag.
	if got := listTags(t, s, ""); !got[sys].System {
		t.Fatalf("list: the engine tag %q must carry system=true: %+v", sys, got[sys])
	}

	// Rename, reparent and delete are all 409 with the system-tag refusal.
	for _, c := range []struct {
		label  string
		method string
		body   string
	}{
		{"rename", http.MethodPatch, `{"name":"renamed"}`},
		{"reparent", http.MethodPatch, `{"parent_id":"` + other.ID + `"}`},
		{"delete", http.MethodDelete, ""},
	} {
		rec := tagCall(t, s, c.method, "/api/autonomous/tags/"+sys, c.body)
		if rec.Code != http.StatusConflict {
			t.Fatalf("%s: %d %s, want 409", c.label, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "system tag") {
			t.Errorf("%s: body %q does not name the system-tag refusal", c.label, rec.Body.String())
		}
	}

	// The record survives every refused attempt, still named and at the root.
	if got := listTags(t, s, ""); got[sys].Name != "stage:draft" || got[sys].ParentID != "" {
		t.Fatalf("refused attempts changed the record: %+v", got[sys])
	}

	// Color and description stay editable on a system tag.
	rec := tagCall(t, s, http.MethodPatch, "/api/autonomous/tags/"+sys, `{"color":"#123456","description":"engine tag"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("color/description on a system tag: %d %s, want 200", rec.Code, rec.Body.String())
	}
	if got := listTags(t, s, ""); got[sys].Color != "#123456" || got[sys].Desc != "engine tag" || !got[sys].System {
		t.Fatalf("after patch: %+v", got[sys])
	}
}

func TestTagDeleteRefusedWhileAssignedReportsCount(t *testing.T) {
	s, mgr := newTagTestServer(t)
	tag := createTag(t, s, `{"name":"shared"}`)
	var prdIDs []string
	for i := 0; i < 2; i++ {
		prd, err := mgr.CreatePRD("spec", t.TempDir(), "", "", autonomous.EffortNormal)
		if err != nil {
			t.Fatalf("CreatePRD: %v", err)
		}
		prdIDs = append(prdIDs, prd.ID)
		rec := tagCall(t, s, http.MethodPost, "/api/autonomous/prds/"+prd.ID+"/set_tags", `{"tag_ids":["`+tag.ID+`"]}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("set_tags: %d %s", rec.Code, rec.Body.String())
		}
	}

	rec := tagCall(t, s, http.MethodDelete, "/api/autonomous/tags/"+tag.ID, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete assigned tag: %d %s, want 409", rec.Code, rec.Body.String())
	}
	var body struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Count != 2 {
		t.Fatalf("409 body = %s (count %d, err %v), want count 2", rec.Body.String(), body.Count, err)
	}
	if got := listTags(t, s, ""); got[tag.ID].Count != 2 {
		t.Errorf("list count = %d, want 2", got[tag.ID].Count)
	}

	for _, id := range prdIDs {
		if rec := tagCall(t, s, http.MethodPost, "/api/autonomous/prds/"+id+"/set_tags", `{"tag_ids":[]}`); rec.Code != http.StatusOK {
			t.Fatalf("clear tags: %d %s", rec.Code, rec.Body.String())
		}
	}
	if rec := tagCall(t, s, http.MethodDelete, "/api/autonomous/tags/"+tag.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("delete after unassign: %d %s", rec.Code, rec.Body.String())
	}
}

func TestTagSetTagsAndListFilter(t *testing.T) {
	s, mgr := newTagTestServer(t)
	parent := createTag(t, s, `{"name":"parent"}`)
	child := createTag(t, s, `{"name":"child","parent_id":"`+parent.ID+`"}`)
	other := createTag(t, s, `{"name":"other"}`)

	pChild, _ := mgr.CreatePRD("spec", t.TempDir(), "", "", autonomous.EffortNormal)
	pParent, _ := mgr.CreatePRD("spec", t.TempDir(), "", "", autonomous.EffortNormal)
	pNone, _ := mgr.CreatePRD("spec", t.TempDir(), "", "", autonomous.EffortNormal)

	set := func(id string, tags ...string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]any{"tag_ids": tags})
		return tagCall(t, s, http.MethodPost, "/api/autonomous/prds/"+id+"/set_tags", string(b))
	}
	if rec := set(pChild.ID, child.ID, child.ID, other.ID); rec.Code != http.StatusOK {
		t.Fatalf("set_tags: %d %s", rec.Code, rec.Body.String())
	}
	if rec := set(pParent.ID, parent.ID); rec.Code != http.StatusOK {
		t.Fatalf("set_tags: %d %s", rec.Code, rec.Body.String())
	}
	if rec := set(pNone.ID, "deadbeef"); rec.Code != http.StatusNotFound {
		t.Errorf("set_tags unknown tag: %d, want 404", rec.Code)
	}

	// tag_ids appears on the PRD JSON and duplicates are collapsed.
	rec := tagCall(t, s, http.MethodGet, "/api/autonomous/prds?full=1", "")
	var all struct {
		PRDs []struct {
			ID     string   `json:"id"`
			TagIDs []string `json:"tag_ids"`
		} `json:"prds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatalf("decode prds: %v", err)
	}
	for _, p := range all.PRDs {
		if p.ID == pChild.ID && len(p.TagIDs) != 2 {
			t.Errorf("child PRD tag_ids = %v, want 2 unique", p.TagIDs)
		}
	}

	ids := func(query string) map[string]bool {
		rec := tagCall(t, s, http.MethodGet, "/api/autonomous/prds"+query, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("list %s: %d %s", query, rec.Code, rec.Body.String())
		}
		var out struct {
			PRDs []struct {
				ID string `json:"id"`
			} `json:"prds"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		m := map[string]bool{}
		for _, p := range out.PRDs {
			m[p.ID] = true
		}
		return m
	}
	if got := ids("?tag=" + parent.ID); len(got) != 2 || !got[pChild.ID] || !got[pParent.ID] {
		t.Errorf("tag=parent (include_sub default) = %v, want child+parent PRDs", got)
	}
	if got := ids("?tag=" + parent.ID + "&include_sub=false"); len(got) != 1 || !got[pParent.ID] {
		t.Errorf("tag=parent include_sub=false = %v, want only parent PRD", got)
	}
	if rec := tagCall(t, s, http.MethodGet, "/api/autonomous/prds?tag=nosuchid", ""); rec.Code != http.StatusNotFound {
		t.Errorf("filter by unknown tag: %d, want 404", rec.Code)
	}

	// Counts: own vs rolled up.
	if got := listTags(t, s, ""); got[parent.ID].Count != 1 || got[child.ID].Count != 1 {
		t.Errorf("own counts parent=%d child=%d, want 1/1", got[parent.ID].Count, got[child.ID].Count)
	}
	if got := listTags(t, s, "?include_sub=true"); got[parent.ID].Count != 2 {
		t.Errorf("rolled-up parent count = %d, want 2", got[parent.ID].Count)
	}
}

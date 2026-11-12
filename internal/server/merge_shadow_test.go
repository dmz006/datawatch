package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/mergeshadow"
)

type shadowResp struct {
	Projects []struct {
		Project     string                           `json:"project"`
		Plans       []json.RawMessage                `json:"plans"`
		Comparisons []json.RawMessage                `json:"comparisons"`
		Counts      map[string]mergeShadowSizeCounts `json:"counts"`
	} `json:"projects"`
}

func shadowGet(t *testing.T, s *Server, method string) (*httptest.ResponseRecorder, shadowResp) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleAutonomousMergeShadow(rec, httptest.NewRequest(method, "/api/autonomous/merge/shadow", nil))
	var out shadowResp
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("bad JSON: %v: %s", err, rec.Body.String())
		}
	}
	return rec, out
}

func shadowSeed(t *testing.T, s *Server, id, project string, size mergeshadow.ReleaseSize, different int) {
	t.Helper()
	dir := s.statePath()
	plan := &mergeshadow.MergePlan{AutomatonID: id, Project: project, ReleaseSize: size, CreatedAt: time.Now()}
	if err := mergeshadow.Save(dir, id, plan); err != nil {
		t.Fatal(err)
	}
	c := &mergeshadow.Comparison{AutomatonID: id, ReleaseSize: size, Matched: 3, Different: different, CreatedAt: time.Now()}
	if err := mergeshadow.SaveComparison(dir, id, c); err != nil {
		t.Fatal(err)
	}
}

func TestMergeShadow_Empty(t *testing.T) {
	s := &Server{dataDir: t.TempDir()}
	rec, out := shadowGet(t, s, http.MethodGet)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", rec.Code, rec.Body.String())
	}
	if out.Projects == nil || len(out.Projects) != 0 {
		t.Fatalf("projects = %v, want empty list", out.Projects)
	}
}

func TestMergeShadow_MethodNotAllowed(t *testing.T) {
	s := &Server{dataDir: t.TempDir()}
	if rec, _ := shadowGet(t, s, http.MethodPost); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestMergeShadow_OneMatched(t *testing.T) {
	s := &Server{dataDir: t.TempDir()}
	shadowSeed(t, s, "a1", "proj", mergeshadow.ReleaseSizePatch, 0)
	_, out := shadowGet(t, s, http.MethodGet)
	if len(out.Projects) != 1 {
		t.Fatalf("projects = %d", len(out.Projects))
	}
	p := out.Projects[0]
	if p.Project != "proj" || len(p.Plans) != 1 || len(p.Comparisons) != 1 {
		t.Fatalf("unexpected: %+v", p)
	}
	if got := p.Counts["patch"]; got.Matched != 1 || got.Different != 0 {
		t.Fatalf("patch counts = %+v", got)
	}
}

func TestMergeShadow_OneDifferent(t *testing.T) {
	s := &Server{dataDir: t.TempDir()}
	shadowSeed(t, s, "a2", "proj", mergeshadow.ReleaseSizeMinor, 2)
	_, out := shadowGet(t, s, http.MethodGet)
	p := out.Projects[0]
	if got := p.Counts["minor"]; got.Matched != 0 || got.Different != 1 {
		t.Fatalf("minor counts = %+v", got)
	}
}

func TestMergeShadow_PerSizeCounts(t *testing.T) {
	s := &Server{dataDir: t.TempDir()}
	shadowSeed(t, s, "p1", "proj", mergeshadow.ReleaseSizePatch, 0)
	shadowSeed(t, s, "p2", "proj", mergeshadow.ReleaseSizePatch, 1)
	shadowSeed(t, s, "m1", "proj", mergeshadow.ReleaseSizeMinor, 0)
	shadowSeed(t, s, "j1", "proj", mergeshadow.ReleaseSizeMajor, 4)
	shadowSeed(t, s, "n1", "other", mergeshadow.ReleaseSizeNone, 0)
	_, out := shadowGet(t, s, http.MethodGet)
	if len(out.Projects) != 2 || out.Projects[0].Project != "other" || out.Projects[1].Project != "proj" {
		t.Fatalf("projects = %+v", out.Projects)
	}
	want := map[string]mergeShadowSizeCounts{
		"patch": {1, 1}, "minor": {1, 0}, "major": {0, 1}, "none": {0, 0},
	}
	for k, w := range want {
		if got := out.Projects[1].Counts[k]; got != w {
			t.Errorf("proj %s = %+v, want %+v", k, got, w)
		}
	}
	if got := out.Projects[0].Counts["none"]; got.Matched != 1 {
		t.Errorf("other none = %+v", got)
	}
}

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// listItem is the subset of an Automata list item these tests read.
type listItem struct {
	ID               string         `json:"id"`
	Tags             []string       `json:"tags"`
	PrimaryTagID     string         `json:"primary_tag_id"`
	TagIDs           []string       `json:"tag_ids"`
	TaskCount        int            `json:"task_count"`
	StoryCount       int            `json:"story_count"`
	TaskStatusCounts map[string]int `json:"task_status_counts"`
}

// listPRDItems fetches the Automata list (summary form, or full when query
// carries full=1) and returns its items keyed by ID.
func listPRDItems(t *testing.T, s *Server, query string) map[string]listItem {
	t.Helper()
	rec := tagCall(t, s, http.MethodGet, "/api/autonomous/prds"+query, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list prds%s: %d %s", query, rec.Code, rec.Body.String())
	}
	var out struct {
		PRDs []listItem `json:"prds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode prds: %v", err)
	}
	m := map[string]listItem{}
	for _, p := range out.PRDs {
		m[p.ID] = p
	}
	return m
}

func TestSummaryViewListItemCountsAndTags(t *testing.T) {
	s, mgr := newTagTestServer(t)
	tag, err := mgr.CreateTag("work", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prd := &autonomous.PRD{
		ID:           "p1",
		TagIDs:       []string{tag.ID},
		PrimaryTagID: tag.ID,
		Story: []autonomous.Story{{ID: "s1", Tasks: []autonomous.Task{
			{ID: "t1", Status: autonomous.TaskPending},
			{ID: "t2", Status: autonomous.TaskInProgress},
		}}},
	}
	if err := mgr.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}

	item, ok := listPRDItems(t, s, "")["p1"]
	if !ok {
		t.Fatal("summary list has no p1 item")
	}
	if item.TaskCount != 2 || item.StoryCount != 1 {
		t.Errorf("task_count=%d story_count=%d, want 2 and 1", item.TaskCount, item.StoryCount)
	}
	wantCounts := map[string]int{"pending": 1, "in_progress": 1}
	if !reflect.DeepEqual(item.TaskStatusCounts, wantCounts) {
		t.Errorf("task_status_counts = %v, want %v", item.TaskStatusCounts, wantCounts)
	}
	if !reflect.DeepEqual(item.Tags, []string{tag.ID}) {
		t.Errorf("summary tags = %v, want [%s]", item.Tags, tag.ID)
	}
	if item.PrimaryTagID != tag.ID {
		t.Errorf("summary primary_tag_id = %q, want %q", item.PrimaryTagID, tag.ID)
	}

	full, ok := listPRDItems(t, s, "?full=1")["p1"]
	if !ok {
		t.Fatal("full list has no p1 item")
	}
	if !reflect.DeepEqual(full.TagIDs, []string{tag.ID}) {
		t.Errorf("full list tag_ids = %v, want [%s]", full.TagIDs, tag.ID)
	}
	if full.PrimaryTagID != tag.ID {
		t.Errorf("full list primary_tag_id = %q, want %q", full.PrimaryTagID, tag.ID)
	}
}

func TestTagCountsOverREST(t *testing.T) {
	s, mgr := newTagTestServer(t)
	parent, _ := mgr.CreateTag("parent", "", "", "")
	child, _ := mgr.CreateTag("child", parent.ID, "", "")
	for _, p := range []*autonomous.PRD{
		{ID: "p1", TagIDs: []string{child.ID}},
		{ID: "p2", TagIDs: []string{parent.ID}},
		{ID: "p3"},
	} {
		if err := mgr.Store().SavePRD(p); err != nil {
			t.Fatal(err)
		}
	}

	rec := tagCall(t, s, http.MethodGet, "/api/autonomous/tags?counts=true", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("counts: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Tags []struct {
			ID                  string `json:"id"`
			Count               int    `json:"count"`
			SubtreeCount        int    `json:"subtree_count"`
			DirectCount         int    `json:"direct_count"`
			SessionSubtreeCount int    `json:"session_subtree_count"`
			SessionDirectCount  int    `json:"session_direct_count"`
		} `json:"tags"`
		Untagged struct {
			Count        int `json:"count"`
			SessionCount int `json:"session_count"`
		} `json:"untagged"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode counts: %v", err)
	}
	byID := map[string]int{}
	for i, tg := range out.Tags {
		byID[tg.ID] = i
	}
	p := out.Tags[byID[parent.ID]]
	if p.SubtreeCount != 2 || p.DirectCount != 1 || p.Count != 1 {
		t.Errorf("parent subtree=%d direct=%d count=%d, want 2, 1, 1", p.SubtreeCount, p.DirectCount, p.Count)
	}
	c := out.Tags[byID[child.ID]]
	if c.SubtreeCount != 1 || c.DirectCount != 1 {
		t.Errorf("child subtree=%d direct=%d, want 1 and 1", c.SubtreeCount, c.DirectCount)
	}
	if out.Untagged.Count != 1 || out.Untagged.SessionCount != 0 {
		t.Errorf("untagged = %+v, want count 1 and session_count 0", out.Untagged)
	}

	// The plain list keeps its shape: no subtree fields and no untagged object.
	plain := tagCall(t, s, http.MethodGet, "/api/autonomous/tags?include_sub=true", "")
	if strings.Contains(plain.Body.String(), "subtree_count") || strings.Contains(plain.Body.String(), "untagged") {
		t.Errorf("plain tag list carries counts fields: %s", plain.Body.String())
	}
}

func TestPrimaryTagSetOverREST(t *testing.T) {
	s, mgr := newTagTestServer(t)
	a, _ := mgr.CreateTag("a", "", "", "")
	b, _ := mgr.CreateTag("b", "", "", "")
	if err := mgr.Store().SavePRD(&autonomous.PRD{ID: "p1"}); err != nil {
		t.Fatal(err)
	}
	setTags := func(body string) *httptest.ResponseRecorder {
		return tagCall(t, s, http.MethodPost, "/api/autonomous/prds/p1/set_tags", body)
	}

	// A primary outside the tag list is refused with 400.
	if rec := setTags(`{"tag_ids":["` + a.ID + `"],"primary_tag_id":"` + b.ID + `"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("primary outside tags: %d %s, want 400", rec.Code, rec.Body.String())
	}
	if got := listPRDItems(t, s, "")["p1"].PrimaryTagID; got != "" {
		t.Fatalf("refused call set primary to %q", got)
	}

	if rec := setTags(`{"tag_ids":["` + a.ID + `","` + b.ID + `"],"primary_tag_id":"` + a.ID + `"}`); rec.Code != http.StatusOK {
		t.Fatalf("set primary: %d %s", rec.Code, rec.Body.String())
	}
	if got := listPRDItems(t, s, "")["p1"].PrimaryTagID; got != a.ID {
		t.Fatalf("primary after set = %q, want %q", got, a.ID)
	}

	// Dropping the primary tag from the Automaton clears the primary.
	if rec := setTags(`{"tag_ids":["` + b.ID + `"]}`); rec.Code != http.StatusOK {
		t.Fatalf("remove primary tag: %d %s", rec.Code, rec.Body.String())
	}
	if got := listPRDItems(t, s, "")["p1"].PrimaryTagID; got != "" {
		t.Errorf("primary after removing its tag = %q, want none", got)
	}
}

func TestTagAttentionEndpointReviewAndCustomStatuses(t *testing.T) {
	s, mgr := newTagTestServer(t)
	for _, p := range []*autonomous.PRD{
		{ID: "p-blocked", Status: "blocked", BlockedReason: "no quota"},
		{ID: "p-running", Status: autonomous.PRDRunning},
		{ID: "p-review", Status: autonomous.PRDRunning, Story: []autonomous.Story{{ID: "s1",
			Tasks: []autonomous.Task{{ID: "t1", Status: autonomous.TaskReviewNeeded}}}}},
	} {
		if err := mgr.Store().SavePRD(p); err != nil {
			t.Fatal(err)
		}
	}

	type attention struct {
		Statuses []string `json:"statuses"`
		Items    []struct {
			ID            string `json:"id"`
			BlockedReason string `json:"blocked_reason"`
		} `json:"items"`
	}
	get := func() (attention, *httptest.ResponseRecorder) {
		req := httptest.NewRequest(http.MethodGet, "/api/autonomous/attention", nil)
		rec := httptest.NewRecorder()
		s.handleAutonomousAttention(rec, req)
		var out attention
		if rec.Code == http.StatusOK {
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatalf("decode attention: %v", err)
			}
		}
		return out, rec
	}
	ids := func(a attention) []string {
		var out []string
		for _, it := range a.Items {
			out = append(out, it.ID)
		}
		sort.Strings(out)
		return out
	}

	def, rec := get()
	if rec.Code != http.StatusOK {
		t.Fatalf("attention: %d %s", rec.Code, rec.Body.String())
	}
	if want := []string{"blocked", "needs_review", "paused"}; !reflect.DeepEqual(def.Statuses, want) {
		t.Errorf("default statuses = %v, want %v", def.Statuses, want)
	}
	if want := []string{"p-blocked", "p-review"}; !reflect.DeepEqual(ids(def), want) {
		t.Errorf("default items = %v, want %v", ids(def), want)
	}
	for _, it := range def.Items {
		if it.ID == "p-blocked" && it.BlockedReason != "no quota" {
			t.Errorf("blocked item blocked_reason = %q, want no quota", it.BlockedReason)
		}
	}

	// A custom attention setting replaces the default statuses; a review-held
	// Automaton still appears.
	s.autonomousSettings = func(key string) ([]string, bool) {
		if key == autonomous.AttentionStatusesKey {
			return []string{"running"}, true
		}
		return nil, false
	}
	custom, _ := get()
	if want := []string{"running"}; !reflect.DeepEqual(custom.Statuses, want) {
		t.Errorf("custom statuses = %v, want %v", custom.Statuses, want)
	}
	if want := []string{"p-review", "p-running"}; !reflect.DeepEqual(ids(custom), want) {
		t.Errorf("custom items = %v, want %v", ids(custom), want)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/attention", nil)
	post := httptest.NewRecorder()
	s.handleAutonomousAttention(post, req)
	if post.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST attention = %d, want 405", post.Code)
	}
}

// TestTagPrimaryOnlyKeepsTags covers a set_tags body with primary_tag_id and
// no tag_ids: the Automaton keeps its tag set and only the primary changes.
// A primary outside that set is still refused.
func TestTagPrimaryOnlyKeepsTags(t *testing.T) {
	s, mgr := newTagTestServer(t)
	a, _ := mgr.CreateTag("a", "", "", "")
	b, _ := mgr.CreateTag("b", "", "", "")
	if err := mgr.Store().SavePRD(&autonomous.PRD{ID: "p1"}); err != nil {
		t.Fatal(err)
	}
	setTags := func(body string) *httptest.ResponseRecorder {
		return tagCall(t, s, http.MethodPost, "/api/autonomous/prds/p1/set_tags", body)
	}
	if rec := setTags(`{"tag_ids":["` + a.ID + `","` + b.ID + `"],"primary_tag_id":"` + a.ID + `"}`); rec.Code != http.StatusOK {
		t.Fatalf("seed: %d %s", rec.Code, rec.Body.String())
	}

	if rec := setTags(`{"primary_tag_id":"` + b.ID + `"}`); rec.Code != http.StatusOK {
		t.Fatalf("primary only: %d %s, want 200", rec.Code, rec.Body.String())
	}
	item := listPRDItems(t, s, "")["p1"]
	if item.PrimaryTagID != b.ID {
		t.Errorf("primary = %q, want %q", item.PrimaryTagID, b.ID)
	}
	if len(item.Tags) != 2 {
		t.Errorf("tags = %v, want both tags kept", item.Tags)
	}

	if rec := setTags(`{"primary_tag_id":"not-a-member"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("primary outside tags: %d %s, want 400", rec.Code, rec.Body.String())
	}
	if got := listPRDItems(t, s, "")["p1"]; got.PrimaryTagID != b.ID || len(got.Tags) != 2 {
		t.Errorf("refused call changed the Automaton: primary %q, tags %v", got.PrimaryTagID, got.Tags)
	}
}

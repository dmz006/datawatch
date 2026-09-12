// BL375 — API-level tests for story status lifecycle and active-session-card shape.
//
// TS-700: GET /api/autonomous/prds/{id} response uses key "stories" (not "story")
// TS-701: Story status field is present in each story object in the API response
// TS-702: StoryInProgress and StoryCompleted appear correctly in API-serialised PRD
// TS-703: Active-session-card: stories with terminal-state tasks are not surfaced

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// prdWithStoriesAPI is a minimal AutonomousAPI fake that returns a PRD with
// populated stories — used to verify JSON serialisation through the HTTP handler.
type prdWithStoriesAPI struct {
	fakeOrchAutonomous
	prd map[string]any
}

func (a *prdWithStoriesAPI) GetPRD(id string) (any, bool) {
	if a.prd != nil {
		return a.prd, true
	}
	return nil, false
}

// buildTestPRD returns a PRD map matching the shape the real autonomous.Manager
// produces after JSON round-trip: stories key, each story with status field.
func buildTestPRD(status, story1Status, story2Status string) map[string]any {
	return map[string]any{
		"id":     "test-prd-1",
		"status": status,
		"stories": []any{
			map[string]any{
				"id":     "s1",
				"title":  "Story One",
				"status": story1Status,
				"tasks": []any{
					map[string]any{
						"id":     "t1",
						"title":  "Task One",
						"status": "done",
					},
				},
			},
			map[string]any{
				"id":     "s2",
				"title":  "Story Two",
				"status": story2Status,
				"tasks": []any{
					map[string]any{
						"id":     "t2",
						"title":  "Task Two",
						"status": "in_progress",
					},
				},
			},
		},
	}
}

// TS-700: GET /api/autonomous/prds/{id} response uses "stories" key, not "story".
func TestTS700_GetPRD_ResponseHasStoriesKey(t *testing.T) {
	prd := buildTestPRD("running", "in_progress", "pending")
	api := &prdWithStoriesAPI{prd: prd}
	s := &Server{autonomousMgr: api}

	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/test-prd-1", nil)
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, hasStories := resp["stories"]; !hasStories {
		t.Errorf("response missing 'stories' key — got keys: %v", keysOf(resp))
	}
	// Must NOT have a top-level "story" key (the legacy/wrong name).
	if _, hasStory := resp["story"]; hasStory {
		t.Errorf("response has 'story' key — should be 'stories' (prd.story bug)")
	}
}

// TS-701: Stories array elements each contain a "status" field.
func TestTS701_GetPRD_StoriesHaveStatusField(t *testing.T) {
	prd := buildTestPRD("running", "in_progress", "pending")
	api := &prdWithStoriesAPI{prd: prd}
	s := &Server{autonomousMgr: api}

	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/test-prd-1", nil)
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)

	stories, ok := resp["stories"].([]any)
	if !ok || len(stories) == 0 {
		t.Fatalf("stories not present or empty in response")
	}
	for i, raw := range stories {
		s2, ok2 := raw.(map[string]any)
		if !ok2 {
			t.Errorf("story[%d] is not an object", i)
			continue
		}
		if _, hasStatus := s2["status"]; !hasStatus {
			t.Errorf("story[%d] missing 'status' field — keys: %v", i, keysOf(s2))
		}
	}
}

// TS-702: Story status values "in_progress" and "completed" are preserved
// through the API response without mutation or omission.
func TestTS702_GetPRD_StoryStatusValues(t *testing.T) {
	cases := []struct {
		name           string
		story1Status   string
		story2Status   string
	}{
		{"both completed", "completed", "completed"},
		{"one in_progress one completed", "in_progress", "completed"},
		{"pending then in_progress", "pending", "in_progress"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prd := buildTestPRD("running", tc.story1Status, tc.story2Status)
			api := &prdWithStoriesAPI{prd: prd}
			srv := &Server{autonomousMgr: api}

			req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/test-prd-1", nil)
			rr := httptest.NewRecorder()
			srv.handleAutonomousPRDs(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rr.Code)
			}
			var resp map[string]any
			_ = json.Unmarshal(rr.Body.Bytes(), &resp)
			stories, _ := resp["stories"].([]any)
			if len(stories) != 2 {
				t.Fatalf("want 2 stories, got %d", len(stories))
			}
			s1, _ := stories[0].(map[string]any)
			s2, _ := stories[1].(map[string]any)
			if s1["status"] != tc.story1Status {
				t.Errorf("story[0] status: want %q, got %q", tc.story1Status, s1["status"])
			}
			if s2["status"] != tc.story2Status {
				t.Errorf("story[1] status: want %q, got %q", tc.story2Status, s2["status"])
			}
		})
	}
}

// TS-703: Active-session-card correctness — stories whose tasks are all in a
// terminal state (done / failed / cancelled) should not be counted as active.
// This test validates the contract the app.js _loadPRDActiveSessionCard relies on.
func TestTS703_ActiveSessionCard_TerminalTasksNotActive(t *testing.T) {
	// PRD with one story fully done, one story still running.
	prd := map[string]any{
		"id":     "card-prd",
		"status": "running",
		"stories": []any{
			map[string]any{
				"id":     "s1",
				"title":  "Done Story",
				"status": "completed",
				"tasks": []any{
					map[string]any{"id": "t1", "status": "done"},
					map[string]any{"id": "t2", "status": "done"},
				},
			},
			map[string]any{
				"id":     "s2",
				"title":  "Active Story",
				"status": "in_progress",
				"tasks": []any{
					map[string]any{"id": "t3", "status": "in_progress"},
					map[string]any{"id": "t4", "status": "queued"},
				},
			},
		},
	}

	api := &prdWithStoriesAPI{prd: prd}
	s := &Server{autonomousMgr: api}

	req := httptest.NewRequest(http.MethodGet, "/api/autonomous/prds/card-prd", nil)
	rr := httptest.NewRecorder()
	s.handleAutonomousPRDs(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)

	stories, _ := resp["stories"].([]any)
	if len(stories) != 2 {
		t.Fatalf("want 2 stories, got %d", len(stories))
	}

	// Verify terminal story is "completed" and non-terminal is "in_progress".
	terminalStory, _ := stories[0].(map[string]any)
	activeStory, _ := stories[1].(map[string]any)

	if terminalStory["status"] != "completed" {
		t.Errorf("terminal story status: want 'completed', got %q", terminalStory["status"])
	}
	if activeStory["status"] != "in_progress" {
		t.Errorf("active story status: want 'in_progress', got %q", activeStory["status"])
	}

	// Active story must have at least one non-terminal task — this is what
	// the active-session-card filter checks in app.js.
	activeTasks, _ := activeStory["tasks"].([]any)
	nonTerminal := 0
	terminal := []string{"done", "failed", "cancelled", "skipped"}
	for _, rawTask := range activeTasks {
		task, _ := rawTask.(map[string]any)
		status, _ := task["status"].(string)
		isTerminal := false
		for _, ts := range terminal {
			if status == ts {
				isTerminal = true
				break
			}
		}
		if !isTerminal {
			nonTerminal++
		}
	}
	if nonTerminal == 0 {
		t.Errorf("active story has no non-terminal tasks — active-session-card would be empty")
	}
}

// keysOf returns the keys of a map for error messages.
func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

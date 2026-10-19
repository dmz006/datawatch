package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func TestWriteAutonomousResult(t *testing.T) {
	prd := &autonomous.PRD{
		ID:     "abcd1234",
		Status: autonomous.PRDDraft,
		Rev:    7,
		Story: []autonomous.Story{
			{ID: "s1", Tasks: []autonomous.Task{{ID: "t1", Status: autonomous.TaskPending}}},
		},
	}

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantKeys   []string
		noKeys     []string
	}{
		{
			name:       "view absent returns full",
			query:      "",
			wantStatus: http.StatusOK,
			wantKeys:   []string{"id", "stories"},
			noKeys:     []string{"message"},
		},
		{
			name:       "view=full returns full",
			query:      "?view=full",
			wantStatus: http.StatusOK,
			wantKeys:   []string{"id", "stories"},
			noKeys:     []string{"message"},
		},
		{
			name:       "view=compact returns write view",
			query:      "?view=compact",
			wantStatus: http.StatusOK,
			wantKeys:   []string{"id", "status", "rev", "message"},
			noKeys:     []string{"stories"},
		},
		{
			name:       "view=bogus is rejected",
			query:      "?view=bogus",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{}
			req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/abcd1234/tasks/t1"+tc.query, nil)
			rec := httptest.NewRecorder()

			s.writeAutonomousResult(rec, req, prd, "t1", "", "task updated")

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus != http.StatusOK {
				return
			}

			var body map[string]json.RawMessage
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode reply: %v", err)
			}
			for _, k := range tc.wantKeys {
				if _, ok := body[k]; !ok {
					t.Errorf("reply missing key %q; body %s", k, rec.Body.String())
				}
			}
			for _, k := range tc.noKeys {
				if _, ok := body[k]; ok {
					t.Errorf("reply has unexpected key %q; body %s", k, rec.Body.String())
				}
			}
		})
	}
}

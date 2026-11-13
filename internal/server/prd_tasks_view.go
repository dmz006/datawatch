package server

// GET /api/autonomous/prds/{id}/tasks serves one page of one Automaton's task
// rows. It reads only that Automaton, so the Gantt expand and the Automaton
// detail can ask for the tasks of the row they opened without the list ever
// carrying them.
//
// Query: limit (default 50, max 500), cursor (opaque, from next_cursor),
// story=<story id>, status=<comma list of task statuses>.
// Response: {"prd_id", "tasks": [...], "total", "next_cursor"}. total counts
// the tasks that match the filters, ignoring the cursor.

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

const (
	prdTasksDefaultLimit = 50
	prdTasksMaxLimit     = 500
	prdTasksErrorRunes   = 240
)

// prdTaskRow is one task in a task page: identity, state, timing, the sessions
// that ran it and a short error. No spec, history or verification text.
type prdTaskRow struct {
	ID                string     `json:"id"`
	Title             string     `json:"title"`
	Status            string     `json:"status"`
	Story             string     `json:"story"`
	StoryTitle        string     `json:"story_title,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	SessionID         string     `json:"session_id,omitempty"`
	VerifierSessionID string     `json:"verifier_session_id,omitempty"`
	JudgeSessionIDs   []string   `json:"judge_session_ids,omitempty"`
	Error             string     `json:"error,omitempty"`
}

type prdTasksResponse struct {
	PRDID      string       `json:"prd_id"`
	Tasks      []prdTaskRow `json:"tasks"`
	Total      int          `json:"total"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

func encodePRDTasksCursor(lastID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(lastID))
}

func decodePRDTasksCursor(s string) (string, bool) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) == 0 {
		return "", false
	}
	return string(b), true
}

func shortTaskError(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= prdTasksErrorRunes {
		return s
	}
	return string(r[:prdTasksErrorRunes]) + "…"
}

func buildPRDTaskRow(story *autonomous.Story, t *autonomous.Task) prdTaskRow {
	row := prdTaskRow{
		ID: t.ID, Title: t.Title, Status: string(t.Status), Story: story.ID, StoryTitle: story.Title,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, StartedAt: t.StartedAt, CompletedAt: t.CompletedAt,
		SessionID: t.SessionID, VerifierSessionID: t.VerifierSessionID, Error: shortTaskError(t.Error),
	}
	seen := map[string]bool{}
	for _, v := range t.Verdicts {
		if v.JudgeSessionID != "" && !seen[v.JudgeSessionID] {
			seen[v.JudgeSessionID] = true
			row.JudgeSessionIDs = append(row.JudgeSessionIDs, v.JudgeSessionID)
		}
	}
	return row
}

// handlePRDTasks serves GET /api/autonomous/prds/{id}/tasks.
func (s *Server) handlePRDTasks(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		rest := strings.TrimPrefix(r.URL.Path, "/api/autonomous/prds/")
		id = strings.TrimSuffix(rest, "/tasks")
	}
	q := r.URL.Query()
	limit := prdTasksDefaultLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		limit = n
	}
	if limit > prdTasksMaxLimit {
		limit = prdTasksMaxLimit
	}
	after := ""
	if v := q.Get("cursor"); v != "" {
		c, ok := decodePRDTasksCursor(v)
		if !ok {
			http.Error(w, "invalid cursor", http.StatusBadRequest)
			return
		}
		after = c
	}
	statuses := map[string]bool{}
	for _, st := range strings.Split(q.Get("status"), ",") {
		if st = strings.ToLower(strings.TrimSpace(st)); st != "" {
			statuses[st] = true
		}
	}
	storyWanted := strings.TrimSpace(q.Get("story"))

	raw, ok := s.autonomousMgr.GetPRD(id)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	prd, err := toPRD(raw)
	if err != nil || prd == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var rows []prdTaskRow
	for i := range prd.Story {
		st := &prd.Story[i]
		if storyWanted != "" && st.ID != storyWanted {
			continue
		}
		for j := range st.Tasks {
			t := &st.Tasks[j]
			if len(statuses) > 0 && !statuses[strings.ToLower(string(t.Status))] {
				continue
			}
			rows = append(rows, buildPRDTaskRow(st, t))
		}
	}
	start := 0
	if after != "" {
		start = -1
		for i := range rows {
			if rows[i].ID == after {
				start = i + 1
				break
			}
		}
		if start < 0 {
			http.Error(w, "invalid cursor", http.StatusBadRequest)
			return
		}
	}
	end := start + limit
	if end > len(rows) {
		end = len(rows)
	}
	resp := prdTasksResponse{PRDID: prd.ID, Tasks: rows[start:end], Total: len(rows)}
	if resp.Tasks == nil {
		resp.Tasks = []prdTaskRow{}
	}
	if end < len(rows) && end > start {
		resp.NextCursor = encodePRDTasksCursor(rows[end-1].ID)
	}
	body, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

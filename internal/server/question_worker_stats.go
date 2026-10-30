package server

import (
	"net/http"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// questionStatsResponse is the GET /api/questions/stats body: the worker
// question marker's counters since daemon start, and the two questions open
// right now that came from a marker line.
type questionStatsResponse struct {
	// QuestionsEnabled and WorkerMarkerEnabled are the two live switches.
	QuestionsEnabled    bool `json:"questions_enabled"`
	WorkerMarkerEnabled bool `json:"worker_marker_enabled"`
	// OpenMarkerQuestions and OpenBlockingQuestions count the open questions
	// that arrived by a marker line, and the open ones that hold a task.
	OpenMarkerQuestions   int `json:"open_marker_questions"`
	OpenBlockingQuestions int `json:"open_blocking_questions"`
	autonomous.WorkerQuestionCounts
}

// handleQuestionStats implements GET /api/questions/stats. It needs the
// autonomous read capability and answers 200 whether or not the channel is on,
// with the switches in the body, so a monitor can tell "off" from "idle".
func (s *Server) handleQuestionStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	out := questionStatsResponse{WorkerQuestionCounts: autonomous.WorkerQuestionCountsSnapshot()}
	if s.cfg != nil {
		out.QuestionsEnabled = s.cfg.Autonomous.Questions.Enabled
		out.WorkerMarkerEnabled = s.cfg.Autonomous.Questions.EffectiveWorkerMarker()
	}
	if s.questionStore != nil {
		for _, q := range s.questionStore.List(autonomous.QuestionFilter{State: autonomous.QuestionOpen}) {
			if q.Source == autonomous.QuestionSourceMarker {
				out.OpenMarkerQuestions++
			}
			if q.Blocking {
				out.OpenBlockingQuestions++
			}
		}
	}
	writeJSONOK(w, out)
}

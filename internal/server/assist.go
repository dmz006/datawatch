// BL42 — quick-response assistant.
//
// POST /api/assist body: {"question": "..."}
// Wraps /api/ask with a dedicated backend + model + system prompt
// configured under session.assistant_*. Lighter than starting a full
// session for one-off questions like "what does GOMAXPROCS default
// to?".

package server

import (
	"encoding/json"
	"net/http"
	"strings"
)

// AssistRequest is the wire form of POST /api/assist.
type AssistRequest struct {
	Question string `json:"question"`
}

// AssistResponse mirrors AskResponse plus the resolved backend/model.
type AssistResponse struct {
	Backend     string `json:"backend"`
	Model       string `json:"model,omitempty"`
	Answer      string `json:"answer"`
	DurationMs  int64  `json:"duration_ms"`
	UsedAssist  bool   `json:"used_assist"`
}

func (s *Server) handleAssist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req AssistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	question := strings.TrimSpace(req.Question)
	if question == "" {
		http.Error(w, "question required", http.StatusBadRequest)
		return
	}

	backend := "ollama"
	model := ""
	if s.cfg != nil {
		if s.cfg.Session.AssistantBackend != "" {
			backend = s.cfg.Session.AssistantBackend
		}
		if s.cfg.Session.AssistantModel != "" {
			model = s.cfg.Session.AssistantModel
		}
		if sys := strings.TrimSpace(s.cfg.Session.AssistantSystemPrompt); sys != "" {
			question = sys + "\n\n" + question
		}
	}

	// Reuse the AskRequest path for the actual call.
	askReq := AskRequest{Question: question, Backend: backend, Model: model}
	var (
		answer string
		err    error
	)
	switch backend {
	case "ollama":
		answer, err = askOllama(s, askReq)
	case "openwebui":
		answer, err = askOpenWebUI(s, askReq)
	default:
		http.Error(w, "unsupported assistant backend: "+backend, http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(AssistResponse{
		Backend:    backend,
		Model:      model,
		Answer:     answer,
		UsedAssist: true,
	})
}

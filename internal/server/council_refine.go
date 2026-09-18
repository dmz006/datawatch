// POST /api/council/personas/refine-step — single-turn LLM call for the
// Council persona wizard "Refine with AI" button (GH#159).
//
// Request:  {"step":"focus","current_answer":"...","instruction":"..."}
// Response: {"refined":"..."}
//
// Routes through the inference dispatcher (same path as /api/ask).
// Returns 503 when no LLM is reachable, 400 on invalid input.

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/inference"
)

var validPersonaSteps = map[string]string{
	"focus":    "area of expertise / domain focus",
	"stance":   "intellectual stance and reasoning style",
	"tone":     "communication tone and style",
	"pushback": "pushback patterns and anti-patterns to avoid",
	"examples": "example phrases or responses",
}

type refineStepRequest struct {
	Step          string `json:"step"`
	CurrentAnswer string `json:"current_answer"`
	Instruction   string `json:"instruction"`
	LLM           string `json:"llm,omitempty"`
}

type refineStepResponse struct {
	Refined    string `json:"refined"`
	DurationMs int64  `json:"duration_ms"`
}

func (s *Server) handleCouncilPersonaRefineStep(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}

	var req refineStepRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "bad request: "+err.Error())
		return
	}
	req.Step = strings.TrimSpace(strings.ToLower(req.Step))
	req.CurrentAnswer = strings.TrimSpace(req.CurrentAnswer)
	req.Instruction = strings.TrimSpace(req.Instruction)

	stepLabel, ok := validPersonaSteps[req.Step]
	if !ok {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("unknown step %q — valid steps: focus, stance, tone, pushback, examples", req.Step))
		return
	}
	if req.CurrentAnswer == "" {
		jsonError(w, http.StatusBadRequest, "current_answer required")
		return
	}
	if req.Instruction == "" {
		jsonError(w, http.StatusBadRequest, "instruction required")
		return
	}

	prompt := fmt.Sprintf(`You are helping a user refine a council AI persona definition.

The current answer for the "%s" step (%s) is:
%s

The user wants to refine it with this instruction: %s

Return only the improved text for that step — no explanation, no preamble, no quotes. Just the refined answer.`,
		req.Step, stepLabel, req.CurrentAnswer, req.Instruction)

	start := time.Now()

	if s.inferenceDisp == nil {
		jsonError(w, http.StatusServiceUnavailable, "no LLM backend configured")
		return
	}

	llmName := req.LLM
	if llmName == "" {
		// Pick the first available non-disabled LLM entry.
		if s.inferenceReg != nil {
			for _, e := range s.inferenceReg.List() {
				if !e.Disabled {
					llmName = e.Name
					break
				}
			}
		}
	}
	if llmName == "" {
		jsonError(w, http.StatusServiceUnavailable, "no LLM backend available")
		return
	}

	resp, err := s.inferenceDisp.Call(r.Context(), llmName, inference.Request{
		Prompt:   prompt,
		Consumer: "council-refine",
	})
	if err != nil {
		jsonError(w, http.StatusServiceUnavailable, "LLM unavailable: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(refineStepResponse{
		Refined:    strings.TrimSpace(resp.Text),
		DurationMs: time.Since(start).Milliseconds(),
	})
}

// Failure escalation REST operations on one Automaton: set or clear its failure
// escalation override (or a task's), and the operator's decision on a task held
// for review. The routes are cases of handleAutonomousPRDs in autonomous.go;
// this file holds their request decoding, existence checks and error mapping.
package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// failureEscalationAPI is the optional part of the Automaton manager that the
// failure escalation routes need. autonomous.API implements it; a manager
// without it answers 501, so AutonomousAPI test doubles need no change.
type failureEscalationAPI interface {
	SetFailureEscalation(prdID, taskID string, policy *autonomous.FailureEscalationPolicy, actor string) (any, error)
	ResolveTaskReview(prdID, taskID string, req autonomous.ReviewResolution, actor string) (any, error)
}

// setFailureEscalationReq is the body of set_failure_escalation. An absent or
// empty task_id addresses the Automaton. Policy is kept raw so that an absent
// key (an error) can be told apart from an explicit null (clear the override).
type setFailureEscalationReq struct {
	TaskID string          `json:"task_id"`
	Policy json.RawMessage `json:"policy"`
	Actor  string          `json:"actor"`
}

// resolveTaskReviewReq is the body of resolve_task_review. Action is "retry" or
// "cancel"; the other fields apply to a retry only.
type resolveTaskReviewReq struct {
	TaskID  string   `json:"task_id"`
	Action  string   `json:"action"`
	Spec    string   `json:"spec"`
	Backend string   `json:"backend"`
	Model   string   `json:"model"`
	Effort  string   `json:"effort"`
	Files   []string `json:"files"`
	Note    string   `json:"note"`
	Actor   string   `json:"actor"`
}

// handleSetFailureEscalation serves POST set_failure_escalation. The caller has
// already checked the method and the federation capability.
func (s *Server) handleSetFailureEscalation(w http.ResponseWriter, r *http.Request, id string) {
	var req setFailureEscalationReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Policy) == 0 {
		http.Error(w, `policy required: send an object with enabled, max_failures and steps, or null to clear the override`, http.StatusBadRequest)
		return
	}
	var policy *autonomous.FailureEscalationPolicy
	if string(req.Policy) != "null" {
		policy = &autonomous.FailureEscalationPolicy{}
		dec := json.NewDecoder(bytes.NewReader(req.Policy))
		dec.DisallowUnknownFields()
		if err := dec.Decode(policy); err != nil {
			http.Error(w, "policy: "+err.Error()+" (valid keys: enabled, max_failures, steps)", http.StatusBadRequest)
			return
		}
	}
	if req.Actor == "" {
		req.Actor = "operator"
	}
	if !s.writeMissingAutomatonOrTask(w, id, req.TaskID) {
		return
	}
	api, ok := s.autonomousMgr.(failureEscalationAPI)
	if !ok {
		http.Error(w, errFailureEscalationNotSupported.Error(), http.StatusNotImplemented)
		return
	}
	updated, err := api.SetFailureEscalation(id, req.TaskID, policy, req.Actor)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.audit(r.Context(), "set_failure_escalation", "automaton", id, map[string]any{
		"task_id": req.TaskID,
		"cleared": policy == nil,
	})
	prd, _ := updated.(*autonomous.PRD)
	s.writeAutonomousResult(w, r, prd, req.TaskID, "", "")
}

// handleResolveTaskReview serves POST resolve_task_review. It works while the
// Automaton is running and other tasks are live. A task that is not held for
// review answers 409; an unknown Automaton or task answers 404.
func (s *Server) handleResolveTaskReview(w http.ResponseWriter, r *http.Request, id string) {
	var req resolveTaskReviewReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.TaskID == "" {
		http.Error(w, "task_id required", http.StatusBadRequest)
		return
	}
	if req.Actor == "" {
		req.Actor = "operator"
	}
	if !s.writeMissingAutomatonOrTask(w, id, req.TaskID) {
		return
	}
	api, ok := s.autonomousMgr.(failureEscalationAPI)
	if !ok {
		http.Error(w, errFailureEscalationNotSupported.Error(), http.StatusNotImplemented)
		return
	}
	res := autonomous.ReviewResolution{
		Action:  req.Action,
		Spec:    req.Spec,
		Backend: req.Backend,
		Model:   req.Model,
		Effort:  autonomous.Effort(req.Effort),
		Files:   req.Files,
		Note:    req.Note,
	}
	updated, err := api.ResolveTaskReview(id, req.TaskID, res, req.Actor)
	if err != nil {
		if errors.Is(err, autonomous.ErrTaskNotInReview) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.audit(r.Context(), "resolve_task_review", "automaton", id, map[string]any{
		"task_id":        req.TaskID,
		"action":         req.Action,
		"spec_changed":   req.Spec != "",
		"worker_changed": req.Backend != "" || req.Model != "" || req.Effort != "",
		"files_narrowed": len(req.Files) > 0,
		"note":           req.Note,
	})
	prd, _ := updated.(*autonomous.PRD)
	s.writeAutonomousResult(w, r, prd, req.TaskID, "", "")
}

// writeMissingAutomatonOrTask answers 404 and reports false when the Automaton
// id does not exist, or when taskID is set and no task of that Automaton has
// it. It reports true when both exist.
func (s *Server) writeMissingAutomatonOrTask(w http.ResponseWriter, id, taskID string) bool {
	raw, ok := s.autonomousMgr.GetPRD(id)
	if !ok || isNilPRD(raw) {
		http.Error(w, fmt.Sprintf("automaton %q not found", id), http.StatusNotFound)
		return false
	}
	if taskID == "" {
		return true
	}
	prd, err := toPRD(raw)
	if err != nil {
		http.Error(w, "automaton: "+err.Error(), http.StatusInternalServerError)
		return false
	}
	for i := range prd.Story {
		for j := range prd.Story[i].Tasks {
			if prd.Story[i].Tasks[j].ID == taskID {
				return true
			}
		}
	}
	http.Error(w, fmt.Sprintf("task %q not found in automaton %q", taskID, id), http.StatusNotFound)
	return false
}

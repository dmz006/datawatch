package server

import "net/http"

const safeguardShippedForceRun = "shipped_work_force_run"

type safeguardEvent struct {
	Action, Target, Actor, Reason string
}

func (s *Server) noteSafeguardAction(r *http.Request, ev safeguardEvent) {}

func isWebAppCall(r *http.Request) bool { return false }

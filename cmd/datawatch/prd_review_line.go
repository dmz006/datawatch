package main

import "encoding/json"

// prdReviewLine renders the one-line review-state status for prd-get from the
// PRD JSON. A judge timeout reads "timed out" with its retry or reroute action
// (a timeout is not a block); a block reads "blocked" with its fix action.
// Passed and pending states, an absent review_state and a non-object body all
// say nothing, so the line is empty and the caller prints nothing.
func prdReviewLine(prdJSON []byte) string {
	var v struct {
		ReviewState struct {
			State  string `json:"state"`
			Cause  string `json:"cause"`
			Action string `json:"action"`
		} `json:"review_state"`
	}
	if err := json.Unmarshal(prdJSON, &v); err != nil {
		return ""
	}
	r := v.ReviewState
	switch r.State {
	case "timed_out":
		line := "review timed out: " + r.Cause
		if r.Action != "" {
			line += " -> " + r.Action
		}
		return line
	case "blocked":
		line := "review blocked: " + r.Cause
		if r.Action != "" {
			line += " -> " + r.Action
		}
		return line
	default:
		return ""
	}
}

// Comm-channel one-line rendering of a PRD's computed review state, so a judge
// timeout reads differently from a judge block.

package router

import (
	"encoding/json"
)

// reviewStateStatusLine renders the review_state of a PRD GET reply into the
// one status line comm channels append after the JSON body: a judge timeout
// says "timed out" with its retry or reroute action (a timeout is not a
// block), a block says "blocked" with its fix action. Passed and pending
// states, an absent review_state and a non-object body all say nothing, so
// the line is empty and the caller prints nothing.
func reviewStateStatusLine(body string) string {
	var v struct {
		ReviewState struct {
			State  string `json:"state"`
			Cause  string `json:"cause"`
			Action string `json:"action"`
			Text   string `json:"judge_outcome_text"`
		} `json:"review_state"`
	}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return ""
	}
	r := v.ReviewState
	suffix := ""
	if r.Text != "" {
		suffix = " (" + r.Text + ")"
	}
	switch r.State {
	case "timed_out":
		line := "review timed out: " + r.Cause
		if r.Action != "" {
			line += " -> " + r.Action
		}
		return line + suffix
	case "blocked":
		line := "review blocked: " + r.Cause
		if r.Action != "" {
			line += " -> " + r.Action
		}
		return line + suffix
	default:
		if r.Text != "" {
			return "review: " + r.Text
		}
		return ""
	}
}

package autonomous

import "encoding/json"

// RenderLanding renders one landing record as a single line for CLI and
// chat-channel output:
//
//	landing: pending by operator (2026-11-09 07:00) — reason
//
// The state is always present; the commit, actor, time and reason are
// appended when filled in. It returns "" when l is the zero value.
func RenderLanding(l Landing) string {
	if l.State == "" {
		return ""
	}
	line := "landing: " + string(l.State)
	if l.Commit != "" {
		line += " @ " + l.Commit
	}
	if l.Actor != "" {
		line += " by " + l.Actor
	}
	if !l.At.IsZero() {
		line += " (" + l.At.Format("2006-01-02 15:04") + ")"
	}
	if l.Reason != "" {
		line += " — " + l.Reason
	}
	return line
}

// LandingLineFromPRD renders the landing record of a PRD JSON body (its
// "landing" field) as a single line, or "" when the PRD carries no landing
// record. Shared by the CLI's `autonomous prd-get` and the chat channel's
// `autonomous landing` verb, so every surface renders the record the same
// way.
func LandingLineFromPRD(prdJSON []byte) string {
	var v struct {
		Landing *Landing `json:"landing"`
	}
	if json.Unmarshal(prdJSON, &v) != nil || v.Landing == nil {
		return ""
	}
	return RenderLanding(*v.Landing)
}

// LandingStateFromPRD returns the landing state of a PRD JSON body (or ""
// when the PRD carries no landing record).
func LandingStateFromPRD(prdJSON []byte) LandingState {
	var v struct {
		Landing *Landing `json:"landing"`
	}
	if json.Unmarshal(prdJSON, &v) != nil || v.Landing == nil {
		return ""
	}
	return v.Landing.State
}

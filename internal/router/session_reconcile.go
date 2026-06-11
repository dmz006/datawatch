// BL93/BL94 — CmdSession handler for chat-side reconcile + import.

package router

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// handleSessionCmd dispatches a parsed CmdSession.
func (r *Router) handleSessionCmd(cmd Command) {
	if r.manager == nil {
		r.send(fmt.Sprintf("[%s] session manager not configured", r.hostname))
		return
	}
	if cmd.Text != "" {
		r.send(sessionHelpText(r.hostname, cmd.Text))
		return
	}
	switch cmd.SessionVerb {
	case SessionVerbReconcile:
		r.sessionReconcile(cmd.SessionArg == "apply")
	case SessionVerbImport:
		r.sessionImport(cmd.SessionArg)
	case SessionVerbSummarize:
		r.sessionSummarize(cmd.SessionArg)
	case SessionVerbSelf:
		r.sessionSelf()
	case SessionVerbOrphaned:
		r.sessionOrphaned()
	default:
		r.send(sessionHelpText(r.hostname, "unknown verb: "+cmd.SessionVerb))
	}
}

func (r *Router) sessionReconcile(apply bool) {
	res, err := r.manager.ReconcileSessions(apply)
	if err != nil {
		r.send(fmt.Sprintf("[%s] reconcile: %v", r.hostname, err))
		return
	}
	if len(res.Imported) == 0 && len(res.Orphaned) == 0 && len(res.Errors) == 0 {
		r.send(fmt.Sprintf("[%s] reconcile: registry matches disk", r.hostname))
		return
	}
	lines := []string{fmt.Sprintf("[%s] reconcile result:", r.hostname)}
	if len(res.Imported) > 0 {
		lines = append(lines, fmt.Sprintf("  imported (%d):", len(res.Imported)))
		for _, id := range res.Imported {
			lines = append(lines, "    + "+id)
		}
	}
	if len(res.Orphaned) > 0 {
		lines = append(lines, fmt.Sprintf("  orphaned (%d) — re-send `session reconcile apply` to import:", len(res.Orphaned)))
		for _, id := range res.Orphaned {
			lines = append(lines, "    ? "+id)
		}
	}
	if len(res.Errors) > 0 {
		lines = append(lines, fmt.Sprintf("  errors (%d):", len(res.Errors)))
		for _, e := range res.Errors {
			lines = append(lines, "    ! "+e)
		}
	}
	r.send(strings.Join(lines, "\n"))
}

func (r *Router) sessionImport(dirOrID string) {
	if dirOrID == "" {
		r.send(sessionHelpText(r.hostname, "import requires a session dir or id"))
		return
	}
	dir := dirOrID
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(r.manager.DataDir(), "sessions", dir)
	}
	sess, imported, err := r.manager.ImportSessionDir(dir)
	if err != nil {
		r.send(fmt.Sprintf("[%s] import %s: %v", r.hostname, dirOrID, err))
		return
	}
	if imported {
		r.send(fmt.Sprintf("[%s] imported %s (state=%s)", r.hostname, sess.FullID, sess.State))
	} else {
		r.send(fmt.Sprintf("[%s] already in registry: %s", r.hostname, sess.FullID))
	}
}

func (r *Router) sessionSummarize(id string) {
	if id == "" {
		r.send(sessionHelpText(r.hostname, "summarize requires a session id"))
		return
	}
	body, err := r.commJSON("POST", fmt.Sprintf("/api/sessions/%s/summarize", id), `{"lines":200}`)
	if err != nil {
		r.send(fmt.Sprintf("[%s] summarize error: %v", r.hostname, err))
		return
	}
	var v struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		r.send(fmt.Sprintf("[%s] summarize result: %s", r.hostname, strings.TrimSpace(body)))
		return
	}
	if v.Summary == "" {
		r.send(fmt.Sprintf("[%s] summarize: no summary generated (summarizer may not be configured)", r.hostname))
		return
	}
	r.send(fmt.Sprintf("[%s] summary: %s", r.hostname, v.Summary))
}

func (r *Router) sessionSelf() {
	body, err := r.commJSON("GET", "/api/sessions/self", "")
	if err != nil {
		r.send(fmt.Sprintf("[%s] session self: %v", r.hostname, err))
		return
	}
	var v struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		r.send(fmt.Sprintf("[%s] session self: %s", r.hostname, strings.TrimSpace(body)))
		return
	}
	out := fmt.Sprintf("[%s] self: id=%s", r.hostname, v.ID)
	if v.Name != "" {
		out += " name=" + v.Name
	}
	r.send(out)
}

func (r *Router) sessionOrphaned() {
	body, err := r.commJSON("GET", "/api/sessions/orphaned", "")
	if err != nil {
		r.send(fmt.Sprintf("[%s] session orphaned: %v", r.hostname, err))
		return
	}
	var sessions []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		State    string `json:"state"`
		ParentID string `json:"parent_id"`
	}
	if err := json.Unmarshal([]byte(body), &sessions); err != nil {
		r.send(fmt.Sprintf("[%s] session orphaned: %s", r.hostname, strings.TrimSpace(body)))
		return
	}
	if len(sessions) == 0 {
		r.send(fmt.Sprintf("[%s] no orphaned sessions", r.hostname))
		return
	}
	lines := []string{fmt.Sprintf("[%s] orphaned sessions (%d):", r.hostname, len(sessions))}
	for _, s := range sessions {
		line := fmt.Sprintf("  %s state=%s parent=%s", s.ID, s.State, s.ParentID)
		if s.Name != "" {
			line = fmt.Sprintf("  %s (%s) state=%s parent=%s", s.ID, s.Name, s.State, s.ParentID)
		}
		lines = append(lines, line)
	}
	r.send(strings.Join(lines, "\n"))
}

func sessionHelpText(hostname, note string) string {
	out := []string{fmt.Sprintf("[%s] session usage:", hostname)}
	if note != "" {
		out = append(out, "  "+note)
	}
	out = append(out,
		"  session reconcile         — list orphan session dirs",
		"  session reconcile apply   — import every orphan",
		"  session import <dir|id>   — import a single session dir",
		"  session summarize <id>    — summarize last session output",
		"  session self              — show the current channel-ready session",
		"  session orphaned          — list sessions with a missing parent",
	)
	return strings.Join(out, "\n")
}

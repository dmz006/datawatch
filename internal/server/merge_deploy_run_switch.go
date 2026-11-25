// The merge run and deploy run request switch
// (autonomous.merge_deploy_run_enabled, off by default).
//
// Both run routes ask it first, so every surface that sends one of those
// requests (REST, MCP, CLI, chat, web) gets the same refusal. The daemon's own
// ways to start a merge or a deploy ask it too: the supervisor loop jobs
// deploy_after_ship and run_merge_until_conflict, and the retry of a deferred
// deploy at daemon start. Finishing a deploy a restart interrupted does not.

package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/config"
)

// Audit subjects of a refused merge or deploy.
const (
	mergeRunSubject  = "merge_run"
	deployRunSubject = "deploy_run"
)

// mergeDeployRunEnabled reads the switch from the live config. No config
// reads as off.
func (s *Server) mergeDeployRunEnabled() bool {
	return s.cfg != nil && s.cfg.Autonomous.MergeDeployRunEnabled
}

// refuseMergeDeployRun answers and audits a run request while the switch is
// off. It reports true when it has answered, and the caller then returns.
func (s *Server) refuseMergeDeployRun(w http.ResponseWriter, r *http.Request, subject string) bool {
	if s.mergeDeployRunEnabled() {
		return false
	}
	s.writeRunRefusal(s.auditActor(r.Context()), subject, map[string]any{
		"setting": config.MergeDeployRunKey,
		"route":   r.URL.Path,
	})
	http.Error(w, config.MergeDeployRunRefusal, http.StatusForbidden)
	return true
}

// writeRunRefusal writes the audit entry of a refused merge or deploy.
func (s *Server) writeRunRefusal(actor, subject string, details map[string]any) {
	if s.auditLog == nil {
		return
	}
	details["outcome"] = "refused"
	_ = s.auditLog.Write(audit.Entry{
		Actor:   actor,
		Action:  audit.ActionMergeDeployRunRefused,
		Subject: subject,
		Details: details,
	})
}

// mergeDeployRunValue reads a patch value for the switch: a JSON boolean, or
// the text form the CLI, MCP and chat send.
func mergeDeployRunValue(v any) (on, ok bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(x))
		return b, err == nil
	}
	return false, false
}

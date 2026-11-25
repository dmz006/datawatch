// The merge run and deploy run request switch
// (autonomous.merge_deploy_run_enabled, off by default).
//
// Both run routes ask it first, so every surface that sends one of those
// requests (REST, MCP, CLI, chat, web) gets the same refusal. It holds
// requests only: the daemon's supervisor loop steps and the deploy resume at
// start do not pass through these routes.

package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/config"
)

// Audit subjects of a refused request.
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
	if s.auditLog != nil {
		_ = s.auditLog.Write(audit.Entry{
			Actor:   s.auditActor(r.Context()),
			Action:  audit.ActionMergeDeployRunRefused,
			Subject: subject,
			Details: map[string]any{
				"setting": config.MergeDeployRunKey,
				"route":   r.URL.Path,
				"outcome": "refused",
			},
		})
	}
	http.Error(w, config.MergeDeployRunRefusal, http.StatusForbidden)
	return true
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

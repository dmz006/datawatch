// GET /api/autonomous/standard-tasks — the read-only listing of every
// standard task the decomposer can add to a story, resolved for one
// project. Rows come from autonomous.ListStandardTasks; this file only
// adds the HTTP method and capability gate and the response envelope.

package server

import (
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
)

// standardTasksResponse is the GET /api/autonomous/standard-tasks body.
// Enabled is the global autonomous.task_templates.enabled; Tasks holds one
// row per effective template for the requested project.
type standardTasksResponse struct {
	Enabled bool                      `json:"enabled"`
	Tasks   []autonomous.StandardTask `json:"tasks"`
}

// handleAutonomousStandardTasks serves GET /api/autonomous/standard-tasks.
// The optional project query parameter names a project directory or alias;
// it selects the per-project override the same way the decomposer does. With
// no project, every row reports the global enabled value.
func (s *Server) handleAutonomousStandardTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	project := strings.TrimSpace(r.URL.Query().Get("project"))
	cfg := s.cfg
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	resp := standardTasksResponse{
		Enabled: cfg.Autonomous.TaskTemplates.Enabled,
		Tasks:   autonomous.ListStandardTasks(cfg, project),
	}
	writeJSONOK(w, resp)
}

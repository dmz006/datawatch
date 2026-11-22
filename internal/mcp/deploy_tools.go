package mcp

import (
	"context"
	"net/http"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// The deploy tools proxy the /api/autonomous/deploy routes:
//
//	autonomous_deploy_get   GET  /api/autonomous/deploy
//	autonomous_deploy_run   POST /api/autonomous/deploy/run
//
// Neither takes arguments: the deploy settings are changed through
// autonomous_config_set. Each returns the daemon's JSON body untouched.

const (
	deployPath    = "/api/autonomous/deploy"
	deployRunPath = deployPath + "/run"
)

// DeployToolNames lists the deploy tools, for the parity tests.
func DeployToolNames() []string {
	return []string{"autonomous_deploy_get", "autonomous_deploy_run"}
}

func (s *Server) toolAutonomousDeployGet() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_deploy_get",
		mcpsdk.WithDescription("Read the deploy state: the effective deploy settings, whether a merge or deploy is running, the last deploy phase and outcome, any deferral and its reason, and the snapshot time and SHA-256. Read-only; the same object as GET /api/autonomous/deploy. Takes no arguments."),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) toolAutonomousDeployRun() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_deploy_run",
		mcpsdk.WithDescription("Start the deploy sequence for the current build now. Refused with a conflict while a merge or deploy is already running. Same as POST /api/autonomous/deploy/run. Takes no arguments."),
	)
}

func (s *Server) handleAutonomousDeployGet(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyGet(deployPath, nil)
	if err != nil {
		return toolError("autonomous_deploy_get", err), nil
	}
	return textOK(string(out)), nil
}

func (s *Server) handleAutonomousDeployRun(_ context.Context, _ mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	out, err := s.proxyJSON(http.MethodPost, deployRunPath, nil)
	if err != nil {
		return toolError("autonomous_deploy_run", err), nil
	}
	return textOK(string(out)), nil
}

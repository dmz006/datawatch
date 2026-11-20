package mcp

// The remote branch reaper tools proxy the /api/autonomous/remote-reap
// routes:
//
//	autonomous_remote_reap          GET    .../remote-reap            report on origin: what would be reaped, what is held, the backup refs that exist
//	autonomous_remote_reap_run      POST   .../remote-reap/run        perform one pass: each qualifying branch is backed up, the backup is verified, then deleted on origin
//	autonomous_remote_reap_restore  POST   .../remote-reap/restore    recreate one reaped branch from its newest backup ref
//
// The report is read-only and touches nothing on origin. The run deletes
// nothing while autonomous.remote_reap_enabled is false or unset (the
// default): the same evaluation runs and the report says report_only. By
// default each answers in plain language (internal/autonomous
// remote_reap_text.go) and format "json" returns the daemon's body
// untouched.

import (
	"context"
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// RemoteReapToolNames lists the remote branch reaper tools, for the parity
// test.
func RemoteReapToolNames() []string {
	return []string{"autonomous_remote_reap", "autonomous_remote_reap_run", "autonomous_remote_reap_restore"}
}

const (
	remoteReapPath    = "/api/autonomous/remote-reap"
	remoteReapRunPath = remoteReapPath + "/run"
	remoteReapRestore = remoteReapPath + "/restore"
)

func (s *Server) toolAutonomousRemoteReap() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_remote_reap",
		mcpsdk.WithDescription("Read the remote branch reaper report on origin (the read-side pass): \"reaped\" lists the Automaton work branches that would be deleted from origin (fully merged into the project's default branch and at least the retention window old, whole days), \"held\" lists the ones left alone with the reason (not merged, still inside the retention window, or no remote ref), and \"backups\" lists every backup ref under refs/datawatch/remote-reap-backup/ with the branch it came from and the tip it holds; \"retention_days\" is the effective autonomous.remote_reap_retention_days (0 or unset reads 30). Read-only: it writes no ref and deletes nothing on origin. The same object as GET /api/autonomous/remote-reap. It reuses the merge base repository (autonomous.worktrees.base_repo). format is text (the default) or json."),
		mcpsdk.WithString("format", mcpsdk.Enum("text", "json"), mcpsdk.Description("Optional — text (default) or json")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) toolAutonomousRemoteReapRun() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_remote_reap_run",
		mcpsdk.WithDescription("Perform one remote branch reap pass in the base repository. This writes: each Automaton work branch that is fully merged into the project's default branch and at least the retention window old is backed up to refs/datawatch/remote-reap-backup/<branch>/<unix-ts>, the backup ref is verified to point at the branch tip, and only then is the branch deleted from origin (git push --delete). While autonomous.remote_reap_enabled is false or unset (the default) the same evaluation runs but nothing is deleted and the report says report_only; the held list then says \"would be reaped\". Same as POST /api/autonomous/remote-reap/run; it needs the base repository (autonomous.worktrees.base_repo). A pass in progress answers a conflict, not a silent skip. A branch whose backup could not be verified is held, never deleted."),
		mcpsdk.WithString("format", mcpsdk.Enum("text", "json"), mcpsdk.Description("Optional — text (default) or json")),
	)
}

func (s *Server) toolAutonomousRemoteReapRestore() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_remote_reap_restore",
		mcpsdk.WithDescription("Recreate one branch a previous remote reap pass deleted, from its newest backup ref (refs/datawatch/remote-reap-backup/<branch>/<unix-ts>). The local branch is re-pointed at the newest tip it was backed up at. branch is the Automaton work branch name from the report (e.g. automaton/p1). Same as POST /api/autonomous/remote-reap/restore. A branch that was never remotely reaped (no backup ref) answers not found."),
		mcpsdk.WithString("branch", mcpsdk.Required(), mcpsdk.Description("The work branch to restore, e.g. automaton/p1")),
		mcpsdk.WithString("format", mcpsdk.Enum("text", "json"), mcpsdk.Description("Optional — text (default) or json")),
	)
}

func (s *Server) handleAutonomousRemoteReap(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "autonomous_remote_reap"
	asJSON, err := mergePipelineWantsJSON(req.GetArguments())
	if err != nil {
		return toolError(op, err), nil
	}
	out, err := s.proxyGet(remoteReapPath, url.Values{})
	if err != nil {
		return toolError(op, err), nil
	}
	if asJSON {
		return textOK(string(out)), nil
	}
	rep, err := autonomous.ParseRemoteReapSurfaceReport(out)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(autonomous.RenderRemoteReapReport(rep)), nil
}

func (s *Server) handleAutonomousRemoteReapRun(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "autonomous_remote_reap_run"
	asJSON, err := mergePipelineWantsJSON(req.GetArguments())
	if err != nil {
		return toolError(op, err), nil
	}
	out, err := s.proxyJSON("POST", remoteReapRunPath, nil)
	if err != nil {
		return toolError(op, err), nil
	}
	if asJSON {
		return textOK(string(out)), nil
	}
	rep, err := autonomous.ParseRemoteReapReport(out)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(autonomous.RenderRemoteReapRun(rep)), nil
}

func (s *Server) handleAutonomousRemoteReapRestore(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "autonomous_remote_reap_restore"
	args := req.GetArguments()
	asJSON, err := mergePipelineWantsJSON(args)
	if err != nil {
		return toolError(op, err), nil
	}
	branch := ""
	if v, ok := args["branch"].(string); ok {
		branch = strings.TrimSpace(v)
	}
	if branch == "" {
		return toolErrorf(op, "branch is required and must be the work branch to restore"), nil
	}
	out, err := s.proxyJSON("POST", remoteReapRestore, map[string]any{"branch": branch})
	if err != nil {
		return toolError(op, err), nil
	}
	if asJSON {
		return textOK(string(out)), nil
	}
	res, err := autonomous.ParseRemoteReapRestoreResult(out)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(autonomous.RenderRemoteReapRestore(res)), nil
}

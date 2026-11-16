package mcp

// The branch reaper tools proxy the /api/autonomous/branch-reap routes:
//
//	autonomous_branch_reap          GET    .../branch-reap            report: what would be reaped, what is held, the backup refs that exist
//	autonomous_branch_reap_run      POST   .../branch-reap/run        perform one pass: each listed branch is backed up first and then deleted
//	autonomous_branch_reap_restore  POST   .../branch-reap/restore    recreate one reaped branch from its newest backup ref
//
// The report is read-only and deletes nothing; the run and the restore are
// the two operator-approved writes. By default each answers in plain
// language (internal/autonomous reap_text.go) and format "json" returns the
// daemon's body untouched.

import (
	"context"
	"net/url"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// ReapToolNames lists the branch reaper tools, for the parity test.
func ReapToolNames() []string {
	return []string{"autonomous_branch_reap", "autonomous_branch_reap_run", "autonomous_branch_reap_restore"}
}

const (
	branchReapPath    = "/api/autonomous/branch-reap"
	branchReapRunPath = branchReapPath + "/run"
	branchReapRestore = branchReapPath + "/restore"
)

func (s *Server) toolAutonomousBranchReap() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_branch_reap",
		mcpsdk.WithDescription("Read the branch reaper report (the read-side pass): \"reaped\" lists the task sub-branches that would be reaped (their landing state has settled, every commit is on the base, and for any of them that an earlier pass already backed up, its backup ref), \"waiting\" lists the ones held (still landing, a commit not contained in the base, or a merge in progress), and \"backups\" lists every backup ref under refs/datawatch/reap-backup/ with the branch it came from and the tip it holds; the retention of those refs is \"backup_days\" in whole days (0 or unset reads 30). Read-only; the same object as GET /api/autonomous/branch-reap. It reuses the merge base repository (autonomous.worktrees.base_repo); if that is unset, the report errors because there is nothing to work on. format is text (the default) or json."),
		mcpsdk.WithString("format", mcpsdk.Enum("text", "json"), mcpsdk.Description("Optional — text (default) or json")),
		mcpsdk.WithReadOnlyHintAnnotation(true),
	)
}

func (s *Server) toolAutonomousBranchReapRun() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_branch_reap_run",
		mcpsdk.WithDescription("Perform one branch reap pass. This writes: for every Automaton whose landing state has settled, each task sub-branch whose commits are all on the base is backed up to refs/datawatch/reap-backup/ first and then deleted with git branch -D. Automata still landing, branches with uncontained commits, and a merge in progress are held, and the held ones are listed. Same as POST /api/autonomous/branch-reap/run; it needs the base repository (autonomous.worktrees.base_repo). The preview (autonomous_branch_reap) already computed what would happen. A reap in progress answers a conflict, not a silent skip."),
		mcpsdk.WithString("format", mcpsdk.Enum("text", "json"), mcpsdk.Description("Optional — text (default) or json")),
	)
}

func (s *Server) toolAutonomousBranchReapRestore() mcpsdk.Tool {
	return mcpsdk.NewTool("autonomous_branch_reap_restore",
		mcpsdk.WithDescription("Recreate one branch a previous pass reaped, from its newest backup ref (refs/datawatch/reap-backup/<branch>/<unix-ts>). The branch is re-pointed at the newest tip it was backed up at. branch is the task sub-branch name from the reap report. Same as POST /api/autonomous/branch-reap/restore. A branch that was never reaped (no backup ref) answers not found."),
		mcpsdk.WithString("branch", mcpsdk.Required(), mcpsdk.Description("The task sub-branch to restore, e.g. automaton/p1--tasks-fix-t1")),
		mcpsdk.WithString("format", mcpsdk.Enum("text", "json"), mcpsdk.Description("Optional — text (default) or json")),
	)
}

func (s *Server) handleAutonomousBranchReap(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "autonomous_branch_reap"
	asJSON, err := mergePipelineWantsJSON(req.GetArguments())
	if err != nil {
		return toolError(op, err), nil
	}
	out, err := s.proxyGet(branchReapPath, url.Values{})
	if err != nil {
		return toolError(op, err), nil
	}
	if asJSON {
		return textOK(string(out)), nil
	}
	rep, err := autonomous.ParseReapSurfaceReport(out)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(autonomous.RenderReapReport(rep)), nil
}

func (s *Server) handleAutonomousBranchReapRun(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "autonomous_branch_reap_run"
	asJSON, err := mergePipelineWantsJSON(req.GetArguments())
	if err != nil {
		return toolError(op, err), nil
	}
	out, err := s.proxyJSON("POST", branchReapRunPath, nil)
	if err != nil {
		return toolError(op, err), nil
	}
	if asJSON {
		return textOK(string(out)), nil
	}
	rep, err := autonomous.ParseReapReport(out)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(autonomous.RenderReapRun(rep)), nil
}

func (s *Server) handleAutonomousBranchReapRestore(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	const op = "autonomous_branch_reap_restore"
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
		return toolErrorf(op, "branch is required and must be the task sub-branch to restore"), nil
	}
	out, err := s.proxyJSON("POST", branchReapRestore, map[string]any{"branch": branch})
	if err != nil {
		return toolError(op, err), nil
	}
	if asJSON {
		return textOK(string(out)), nil
	}
	res, err := autonomous.ParseReapRestoreResult(out)
	if err != nil {
		return toolError(op, err), nil
	}
	return textOK(autonomous.RenderReapRestore(res)), nil
}

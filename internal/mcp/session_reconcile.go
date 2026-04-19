package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
)

// toolSessionReconcile (BL93) exposes the orphan-session scanner.
func (s *Server) toolSessionReconcile() mcpsdk.Tool {
	return mcpsdk.NewTool("session_reconcile",
		mcpsdk.WithDescription("List session directories on disk that are missing from the registry. Pass auto_import=true to import them all."),
		mcpsdk.WithBoolean("auto_import",
			mcpsdk.Description("If true, import every orphan into the registry (write-through). Default: false (dry-run)."),
		),
	)
}

func (s *Server) handleSessionReconcile(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	auto := req.GetBool("auto_import", false)
	res, err := s.manager.ReconcileSessions(auto)
	if err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error: %v", err)), nil
	}
	body, _ := json.MarshalIndent(res, "", "  ")
	return mcpsdk.NewToolResultText(string(body)), nil
}

// toolSessionImport (BL94) exposes the single-dir import path.
func (s *Server) toolSessionImport() mcpsdk.Tool {
	return mcpsdk.NewTool("session_import",
		mcpsdk.WithDescription("Import a single session directory into the registry. dir may be an absolute path or a session ID resolved under <data_dir>/sessions/."),
		mcpsdk.WithString("dir",
			mcpsdk.Required(),
			mcpsdk.Description("Session directory or short ID"),
		),
	)
}

func (s *Server) handleSessionImport(_ context.Context, req mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	dir := req.GetString("dir", "")
	if dir == "" {
		return mcpsdk.NewToolResultText("Error: dir is required"), nil
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(s.manager.DataDir(), "sessions", dir)
	}
	sess, imported, err := s.manager.ImportSessionDir(dir)
	if err != nil {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Error: %v", err)), nil
	}
	if imported {
		return mcpsdk.NewToolResultText(fmt.Sprintf("Imported %s (state=%s)", sess.FullID, sess.State)), nil
	}
	return mcpsdk.NewToolResultText(fmt.Sprintf("Already in registry: %s", sess.FullID)), nil
}

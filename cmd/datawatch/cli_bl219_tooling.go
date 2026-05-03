// BL219 — CLI surface for tooling artifact lifecycle.

package main

import (
	"github.com/spf13/cobra"
)

func newToolingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tooling",
		Short: "Manage LLM backend artifact lifecycle (gitignore hygiene + cleanup)",
		Long: `Inspect and manage file-system artifacts left by LLM backends
(aider cache dirs, goose session JSONLs, etc.) in a project directory.

Subcommands:
  status   [backend]   — show which artifacts are present and whether they are gitignored
  gitignore <backend>  — append backend artifact patterns to .gitignore
  cleanup  <backend>   — remove ephemeral backend artifacts from the project directory`,
	}
	cmd.AddCommand(
		newToolingStatusCmd(),
		newToolingGitignoreCmd(),
		newToolingCleanupCmd(),
	)
	return cmd
}

func newToolingStatusCmd() *cobra.Command {
	var projectDir, backend string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show artifact presence + gitignore state for LLM backends",
		RunE: func(*cobra.Command, []string) error {
			path := "/api/tooling/status"
			sep := "?"
			if projectDir != "" {
				path += sep + "project_dir=" + projectDir
				sep = "&"
			}
			if backend != "" {
				path += sep + "backend=" + backend
			}
			return daemonGet(path)
		},
	}
	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Project directory to inspect (default: daemon default_project_dir)")
	cmd.Flags().StringVar(&backend, "backend", "", "Specific backend to query (omit for all)")
	return cmd
}

func newToolingGitignoreCmd() *cobra.Command {
	var projectDir, backend string
	cmd := &cobra.Command{
		Use:   "gitignore",
		Short: "Append a backend's artifact patterns to .gitignore",
		RunE: func(*cobra.Command, []string) error {
			body := map[string]any{"backend": backend}
			if projectDir != "" {
				body["project_dir"] = projectDir
			}
			return daemonJSON("POST", "/api/tooling/gitignore", body)
		},
	}
	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Project directory (default: daemon default_project_dir)")
	cmd.Flags().StringVar(&backend, "backend", "", "Backend to gitignore: claude-code, opencode, aider, goose, gemini")
	_ = cmd.MarkFlagRequired("backend")
	return cmd
}

func newToolingCleanupCmd() *cobra.Command {
	var projectDir, backend string
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Remove ephemeral backend artifact files from project directory",
		RunE: func(*cobra.Command, []string) error {
			body := map[string]any{"backend": backend}
			if projectDir != "" {
				body["project_dir"] = projectDir
			}
			return daemonJSON("POST", "/api/tooling/cleanup", body)
		},
	}
	cmd.Flags().StringVar(&projectDir, "project-dir", "", "Project directory (default: daemon default_project_dir)")
	cmd.Flags().StringVar(&backend, "backend", "", "Backend whose artifacts to remove: aider, goose, gemini, opencode, claude-code")
	_ = cmd.MarkFlagRequired("backend")
	return cmd
}

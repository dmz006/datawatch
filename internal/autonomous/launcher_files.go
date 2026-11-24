// Exclusion set for the launcher files a worker session start writes.
//
// The launcher writes exactly two files per session, now into a private
// per-session folder outside the project directory (claudecode newLaunchDir);
// older versions wrote them at the project root, so copies there are still
// excluded by name. Neither is operator work: the per-session MCP config,
// channel.WorkerMCPConfigFileName ("worker-mcp.json",
// internal/channel/worker_mcp_config.go), and the starting-context
// document, claudecode.WorkerStartingContextFileName
// ("worker-starting-context.md", internal/llm/claudecode/backend.go).
// internal/autonomous cannot import the writer packages (claudecode already
// imports autonomous), so the names are carried here as literals and pinned
// to the writer constants by the sync test in internal/llm/claudecode
// (launcher_file_names_test.go).
package autonomous

import (
	"path"
	"path/filepath"
)

const (
	// launcherMCPConfigFileName is the launcher-written MCP config file
	// name at the project root; must equal channel.WorkerMCPConfigFileName.
	launcherMCPConfigFileName = "worker-mcp.json"
	// launcherStartingContextFileName is the launcher-written starting
	// context file name at the project root; must equal
	// claudecode.WorkerStartingContextFileName.
	launcherStartingContextFileName = "worker-starting-context.md"
)

// launcherToolingFiles returns the two launcher file names at the project
// root, in a stable order.
func launcherToolingFiles() []string {
	return []string{launcherMCPConfigFileName, launcherStartingContextFileName}
}

// LauncherToolingFileNames exposes launcherToolingFiles for the writer
// package (claudecode), whose test pins this set to the writer constants so
// the two literals can never drift. The returned slice is safe to mutate.
func LauncherToolingFileNames() []string {
	return launcherToolingFiles()
}

// isLauncherToolingFile reports whether p is one of the launcher file names
// at the project directory root, matched exactly (slash-normalized and
// cleaned). A same-named file in a subfolder (e.g. sub/worker-mcp.json) is
// not a launcher file and is not treated as one, and neither is an unsafe
// path: a false answer is harmless because every caller fails closed by
// keeping the path in the dirt.
func isLauncherToolingFile(p string) bool {
	clean := path.Clean(filepath.ToSlash(p))
	return clean == launcherMCPConfigFileName || clean == launcherStartingContextFileName
}

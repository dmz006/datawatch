// Per-session worker MCP config writer.
//
// Worker sessions (automata task workers, subtask sessions, etc.) start from
// a workerprofile.WorkerProfile whose MCP selection decides which MCP servers
// that session may use. This file writes the session-scoped `worker-mcp.json`
// so backends pick up exactly that server set, without touching the
// project-level or instance .mcp.json files in mcp_config.go.

package channel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/dmz006/datawatch/internal/workerprofile"
)

// WorkerMCPConfigFileName is the session-scoped MCP config file written into
// a worker session's directory. It uses the same "mcpServers" shape as
// `.mcp.json` (see MCPProjectConfig in mcp_config.go).
const WorkerMCPConfigFileName = "worker-mcp.json"

// WriteWorkerMCPConfig writes dir/worker-mcp.json selecting MCP servers for a
// worker session according to sel:
//
//   - workerprofile.MCPNone      — the config's mcpServers object is empty.
//   - workerprofile.MCPDatawatch — only the entry named "datawatch"
//     (ProjectMCPEntryName) is kept; all others are dropped.
//   - workerprofile.MCPList      — exactly the servers named in
//     sel.Servers are kept; if any named server is missing from servers the
//     call fails and no file is written, with an error naming every missing
//     server.
//
// The file is written with mode 0600: it can carry server env, including
// tokens, in plaintext (HLLM-001). The write is atomic — a temporary file in
// dir is written and renamed into place, so a reader never observes a partial
// file. Output is deterministic: the same input always produces the same
// bytes. Returns the path of the written file.
func WriteWorkerMCPConfig(dir string, sel workerprofile.MCPSelection, servers map[string]MCPServerSpec) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("WriteWorkerMCPConfig: dir is required")
	}

	kept := map[string]MCPServerSpec{}
	switch sel.Mode {
	case workerprofile.MCPNone:
		// empty mcpServers object
	case workerprofile.MCPDatawatch:
		if spec, ok := servers[ProjectMCPEntryName]; ok {
			kept[ProjectMCPEntryName] = spec
		}
	case workerprofile.MCPList:
		var missing []string
		for _, name := range sel.Servers {
			spec, ok := servers[name]
			if !ok {
				missing = append(missing, name)
				continue
			}
			kept[name] = spec
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return "", fmt.Errorf("WriteWorkerMCPConfig: mcp servers not found: %v", missing)
		}
	default:
		return "", fmt.Errorf("WriteWorkerMCPConfig: unknown mcp mode %q", sel.Mode)
	}

	cfg := MCPProjectConfig{MCPServers: kept}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("WriteWorkerMCPConfig: marshal: %w", err)
	}
	out = append(out, '\n')

	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("WriteWorkerMCPConfig: mkdir %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "."+WorkerMCPConfigFileName+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("WriteWorkerMCPConfig: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Best-effort cleanup; ignored after a successful rename.
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("WriteWorkerMCPConfig: %w", err)
	}
	// 0600 before the rename so the live path never appears group/world
	// readable, even for the brief moment after it exists.
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("WriteWorkerMCPConfig: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("WriteWorkerMCPConfig: %w", err)
	}
	path := filepath.Join(dir, WorkerMCPConfigFileName)
	if err := os.Rename(tmpName, path); err != nil {
		return "", fmt.Errorf("WriteWorkerMCPConfig: rename: %w", err)
	}
	return path, nil
}

package channel

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// requiredSettings are merged into CLAUDE_CONFIG_DIR/settings.json at daemon
// startup. They prevent Claude Code from blocking on interactive prompts when
// running inside a datawatch-managed tmux session.
var requiredSettings = map[string]any{
	// Suppress the "⚠ Bypass permissions mode" confirmation dialog that blocks
	// automated sessions launched with --permission-mode bypassPermissions.
	"skipDangerousModePermissionPrompt": true,
	// Suppress the auto-permission approval prompt.
	"skipAutoPermissionPrompt": true,
	// Trust all MCP servers declared in .mcp.json files in the project tree.
	"enableAllProjectMcpServers": true,
	// Allowlist the datawatch MCP server by name (matches WriteProjectMCPConfig).
	"enabledMcpjsonServers": []any{"datawatch"},
}

// EnsureClaudeSettings merges requiredSettings into dir/settings.json,
// creating the file and directory as needed. Existing values not in
// requiredSettings are preserved; values that are already correct are left
// untouched (idempotent). Call once at daemon startup after SetClaudeConfigDir.
func EnsureClaudeSettings(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "settings.json")

	existing := make(map[string]any)
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &existing)
	}

	changed := false
	for k, want := range requiredSettings {
		// Simple equality check; sufficient for booleans and string slices.
		cur, ok := existing[k]
		if !ok || !settingEqual(cur, want) {
			existing[k] = want
			changed = true
		}
	}
	if !changed {
		return nil
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// settingEqual does a JSON-round-trip comparison so []any{"datawatch"} from
// a freshly-constructed map matches the same value decoded from JSON.
func settingEqual(a, b any) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}

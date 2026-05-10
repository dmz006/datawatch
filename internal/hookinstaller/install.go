// Package hookinstaller — alpha.34a #202.
//
// Auto-installs Claude Code hook scripts into a session's project_dir
// at session spawn. The hooks POST Stop / PostToolUse / UserPromptSubmit
// events to the daemon's /api/sessions/<id>/hook-event endpoint so the
// PWA Status sub-tab can render live state.
//
// Layout written:
//
//	<project_dir>/.claude/settings.json   — operator-mergeable hook decls
//	<project_dir>/.claude/sprint/post-event.sh — POST helper
//	<project_dir>/.claude/sprint/.dw-env  — daemon URL + session ID + token (chmod 600)
//
// IDEMPOTENT: existing settings.json is left intact if it already
// declares hooks. Existing post-event.sh is overwritten (script is
// deterministic). .dw-env is rewritten per session.

package hookinstaller

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Install writes the .claude/sprint/* files into projectDir. Returns
// nil even if individual writes fail (best-effort; hook-installer
// failures must not block session spawn).
//
// CHAINABLE: if settings.json already declares hooks, the daemon's
// hook entry (the one whose command path ends in
// `.claude/sprint/post-event.sh`) is APPENDED to each Stop /
// PostToolUse / UserPromptSubmit / SubagentStop array. Operator's
// existing entries are preserved + run alongside ours. We detect our
// own entry by path so re-installs are idempotent (no duplicates).
//
// VALIDATED EVERY SPAWN: this function is called from OnSessionStart
// for every claude-code session. It reads the current settings.json,
// merges, and writes back. If the operator has removed/edited hooks
// between sessions, we re-add ours.
func Install(projectDir, sessionFullID, daemonURL, token string) error {
	if projectDir == "" || sessionFullID == "" {
		return fmt.Errorf("hookinstaller: project_dir + session_id required")
	}
	claudeDir := filepath.Join(projectDir, ".claude")
	sprintDir := filepath.Join(claudeDir, "sprint")
	if err := os.MkdirAll(sprintDir, 0o755); err != nil {
		return fmt.Errorf("hookinstaller: mkdir %s: %w", sprintDir, err)
	}

	// settings.json — chain into existing hooks dict.
	settingsPath := filepath.Join(claudeDir, "settings.json")
	hookCmdBase := filepath.Join(sprintDir, "post-event.sh")
	doc := map[string]any{}
	if existing, err := os.ReadFile(settingsPath); err == nil && len(existing) > 0 {
		_ = json.Unmarshal(existing, &doc)
	}
	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	// Per-event entry we want present.
	wantEntry := func(cmd string) map[string]any {
		return map[string]any{"type": "command", "command": cmd}
	}
	addIfMissing := func(eventName, cmd string) {
		// Find the event's existing array.
		var arr []any
		switch v := hooks[eventName].(type) {
		case []any:
			arr = v
		case []map[string]any:
			for _, m := range v {
				arr = append(arr, m)
			}
		}
		// Idempotent: skip if any entry's command starts with hookCmdBase.
		for _, item := range arr {
			if m, ok := item.(map[string]any); ok {
				if c, ok := m["command"].(string); ok && len(c) >= len(hookCmdBase) && c[:len(hookCmdBase)] == hookCmdBase {
					return // ours already present
				}
			}
		}
		arr = append(arr, wantEntry(cmd))
		hooks[eventName] = arr
	}
	addIfMissing("Stop", hookCmdBase+" Stop")
	addIfMissing("PostToolUse", hookCmdBase+" PostToolUse $TOOL_NAME")
	addIfMissing("UserPromptSubmit", hookCmdBase+" UserPromptSubmit")
	addIfMissing("SubagentStop", hookCmdBase+" SubagentStop")
	doc["hooks"] = hooks
	body, _ := json.MarshalIndent(doc, "", "  ")
	_ = os.WriteFile(settingsPath, body, 0o644)

	// post-event.sh — deterministic; always rewrite.
	scriptBody := `#!/usr/bin/env bash
# datawatch hook event POST — auto-installed (alpha.34a #202).
# Args: $1 = event name (Stop/PostToolUse/UserPromptSubmit/SubagentStop)
#       $2 = optional tool name (PostToolUse only)
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
[[ -f "$SCRIPT_DIR/.dw-env" ]] && source "$SCRIPT_DIR/.dw-env"
EVENT="${1:-}"
TOOL="${2:-}"
[[ -z "$EVENT" ]] && exit 0
PAYLOAD='{}'
if [[ -f "$SCRIPT_DIR/state.json" ]]; then
  PAYLOAD=$(cat "$SCRIPT_DIR/state.json" 2>/dev/null || echo '{}')
fi
curl -ks -X POST "${DAEMON_URL:-https://localhost:8443}/api/sessions/${SESSION_ID}/hook-event" \
  -H "Content-Type: application/json" \
  ${TOKEN:+-H "Authorization: Bearer ${TOKEN}"} \
  -d "{\"event\":\"${EVENT}\",\"tool\":\"${TOOL}\",\"payload\":${PAYLOAD}}" \
  >/dev/null 2>&1 || true
`
	scriptPath := filepath.Join(sprintDir, "post-event.sh")
	if err := os.WriteFile(scriptPath, []byte(scriptBody), 0o755); err != nil {
		return fmt.Errorf("hookinstaller: write post-event.sh: %w", err)
	}

	// .dw-env — rewritten per session (chmod 600 — contains bearer token).
	envBody := fmt.Sprintf("DAEMON_URL=%s\nSESSION_ID=%s\nTOKEN=%s\n", daemonURL, sessionFullID, token)
	envPath := filepath.Join(sprintDir, ".dw-env")
	if err := os.WriteFile(envPath, []byte(envBody), 0o600); err != nil {
		return fmt.Errorf("hookinstaller: write .dw-env: %w", err)
	}

	return nil
}

// Cleanup removes the .claude/sprint/.dw-env file written by Install.
// Best-effort. Leaves settings.json + post-event.sh intact so the
// operator's project state is preserved between sessions.
func Cleanup(projectDir string) error {
	if projectDir == "" {
		return nil
	}
	envPath := filepath.Join(projectDir, ".claude", "sprint", ".dw-env")
	_ = os.Remove(envPath)
	return nil
}

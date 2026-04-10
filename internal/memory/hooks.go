package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// InstallClaudeHooks writes datawatch memory hooks to the project's
// .claude/settings.local.json so Claude Code auto-saves to memory.
// Merges with existing settings (preserves user config).
// hooksDir is the path to the datawatch hooks/ directory containing the scripts.
func InstallClaudeHooks(projectDir, hooksDir string, saveInterval int) error {
	if saveInterval <= 0 {
		saveInterval = 15
	}

	claudeDir := filepath.Join(projectDir, ".claude")
	settingsPath := filepath.Join(claudeDir, "settings.local.json")

	saveHookPath := filepath.Join(hooksDir, "datawatch_save_hook.sh")
	precompactHookPath := filepath.Join(hooksDir, "datawatch_precompact_hook.sh")

	// Verify hook scripts exist
	if _, err := os.Stat(saveHookPath); err != nil {
		return fmt.Errorf("save hook not found: %s", saveHookPath)
	}
	if _, err := os.Stat(precompactHookPath); err != nil {
		return fmt.Errorf("pre-compact hook not found: %s", precompactHookPath)
	}

	// Read existing settings
	var settings map[string]interface{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		json.Unmarshal(data, &settings) //nolint:errcheck
	}
	if settings == nil {
		settings = make(map[string]interface{})
	}

	// Build hook entries
	saveHook := map[string]interface{}{
		"matcher": "*",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": fmt.Sprintf("DATAWATCH_HOOK_INTERVAL=%d %s", saveInterval, saveHookPath),
				"timeout": 10,
			},
		},
	}
	precompactHook := map[string]interface{}{
		"matcher": "*",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": precompactHookPath,
				"timeout": 10,
			},
		},
	}

	// Merge hooks into settings
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = make(map[string]interface{})
	}

	// Only add if not already configured (don't overwrite user customization)
	if _, exists := hooks["Stop"]; !exists {
		hooks["Stop"] = []interface{}{saveHook}
	}
	if _, exists := hooks["PreCompact"]; !exists {
		hooks["PreCompact"] = []interface{}{precompactHook}
	}

	settings["hooks"] = hooks

	// Write settings
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("create .claude dir: %w", err)
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, append(data, '\n'), 0644)
}

// EnsureHookScripts writes the hook shell scripts to the given directory
// if they don't already exist. Called on daemon startup.
func EnsureHookScripts(hooksDir string) {
	os.MkdirAll(hooksDir, 0755) //nolint:errcheck

	saveScript := filepath.Join(hooksDir, "datawatch_save_hook.sh")
	if _, err := os.Stat(saveScript); err != nil {
		os.WriteFile(saveScript, []byte(saveHookScript), 0755) //nolint:errcheck
	}

	precompactScript := filepath.Join(hooksDir, "datawatch_precompact_hook.sh")
	if _, err := os.Stat(precompactScript); err != nil {
		os.WriteFile(precompactScript, []byte(precompactHookScript), 0755) //nolint:errcheck
	}
}

const saveHookScript = `#!/bin/bash
# DATAWATCH MEMORY SAVE HOOK — Auto-save every N exchanges
SAVE_INTERVAL=${DATAWATCH_HOOK_INTERVAL:-15}
DATAWATCH_URL=${DATAWATCH_URL:-http://localhost:8080}
STATE_DIR="$HOME/.datawatch/hook_state"
mkdir -p "$STATE_DIR"
INPUT=$(cat)
eval $(echo "$INPUT" | python3 -c "
import sys, json, re
data = json.load(sys.stdin)
safe = lambda s: re.sub(r'[^a-zA-Z0-9_/.\-~]', '', str(s))
print(f'SESSION_ID=\"{safe(data.get(\"session_id\", \"unknown\"))}\"')
print(f'STOP_HOOK_ACTIVE=\"{data.get(\"stop_hook_active\", False)}\"')
print(f'TRANSCRIPT_PATH=\"{safe(data.get(\"transcript_path\", \"\"))}\"')
" 2>/dev/null)
TRANSCRIPT_PATH="${TRANSCRIPT_PATH/#\~/$HOME}"
if [ "$STOP_HOOK_ACTIVE" = "True" ] || [ "$STOP_HOOK_ACTIVE" = "true" ]; then echo "{}"; exit 0; fi
EXCHANGE_COUNT=0
if [ -f "$TRANSCRIPT_PATH" ]; then
  EXCHANGE_COUNT=$(python3 - "$TRANSCRIPT_PATH" <<'PYEOF'
import json, sys
count = 0
with open(sys.argv[1]) as f:
    for line in f:
        try:
            entry = json.loads(line)
            msg = entry.get('message', {})
            if isinstance(msg, dict) and msg.get('role') == 'user':
                content = msg.get('content', '')
                if isinstance(content, str) and '<command-message>' in content: continue
                count += 1
        except: pass
print(count)
PYEOF
2>/dev/null)
fi
LAST_SAVE_FILE="$STATE_DIR/${SESSION_ID}_last_save"
LAST_SAVE=0; [ -f "$LAST_SAVE_FILE" ] && LAST_SAVE=$(cat "$LAST_SAVE_FILE")
SINCE_LAST=$((EXCHANGE_COUNT - LAST_SAVE))
if [ "$SINCE_LAST" -ge "$SAVE_INTERVAL" ] && [ "$EXCHANGE_COUNT" -gt 0 ]; then
  echo "$EXCHANGE_COUNT" > "$LAST_SAVE_FILE"
  if [ -f "$TRANSCRIPT_PATH" ]; then
    SUMMARY=$(python3 - "$TRANSCRIPT_PATH" <<'PYEOF'
import json, sys
msgs = []
with open(sys.argv[1]) as f:
    for line in f:
        try:
            entry = json.loads(line)
            msg = entry.get('message', {})
            if isinstance(msg, dict) and msg.get('role') in ('user','assistant'):
                content = msg.get('content','')
                if isinstance(content, list):
                    content = ' '.join(str(c.get('text','')) if isinstance(c,dict) else str(c) for c in content)
                if content and '<command-message>' not in str(content):
                    msgs.append({'role': msg['role'], 'content': content[:200]})
        except: pass
recent = msgs[-4:] if len(msgs) >= 4 else msgs
print(' | '.join(f"{m['role']}: {m['content'][:100]}" for m in recent))
PYEOF
2>/dev/null)
    [ -n "$SUMMARY" ] && curl -s -X POST "$DATAWATCH_URL/api/test/message" \
      -H "Content-Type: application/json" \
      -d "{\"text\":\"remember: [auto-save] $SUMMARY\"}" > /dev/null 2>&1
  fi
fi
echo "{}"
`

const precompactHookScript = `#!/bin/bash
# DATAWATCH PRE-COMPACT HOOK — Save before context compression
DATAWATCH_URL=${DATAWATCH_URL:-http://localhost:8080}
INPUT=$(cat)
TRANSCRIPT_PATH=$(echo "$INPUT" | python3 -c "
import sys, json, re
data = json.load(sys.stdin)
print(re.sub(r'[^a-zA-Z0-9_/.\-~]', '', str(data.get('transcript_path', ''))))
" 2>/dev/null)
TRANSCRIPT_PATH="${TRANSCRIPT_PATH/#\~/$HOME}"
if [ -f "$TRANSCRIPT_PATH" ]; then
  SUMMARY=$(python3 - "$TRANSCRIPT_PATH" <<'PYEOF'
import json, sys
topics = set()
with open(sys.argv[1]) as f:
    for line in f:
        try:
            entry = json.loads(line)
            msg = entry.get('message', {})
            if isinstance(msg, dict) and msg.get('role') == 'user':
                content = str(msg.get('content', ''))
                if '<command-message>' not in content:
                    topics.add(' '.join(content.split()[:10]))
        except: pass
print('[pre-compact] Topics: ' + '; '.join(list(topics)[:5]))
PYEOF
2>/dev/null)
  [ -n "$SUMMARY" ] && curl -s -X POST "$DATAWATCH_URL/api/test/message" \
    -H "Content-Type: application/json" \
    -d "{\"text\":\"remember: $SUMMARY\"}" > /dev/null 2>&1
fi
echo "{}"
`

// HooksInstalled checks if datawatch hooks are already configured in the project.
func HooksInstalled(projectDir string) bool {
	settingsPath := filepath.Join(projectDir, ".claude", "settings.local.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return false
	}
	var settings map[string]interface{}
	if json.Unmarshal(data, &settings) != nil {
		return false
	}
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		return false
	}
	_, hasStop := hooks["Stop"]
	_, hasPreCompact := hooks["PreCompact"]
	return hasStop && hasPreCompact
}

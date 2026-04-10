#!/bin/bash
# DATAWATCH MEMORY SAVE HOOK — Auto-save to memory every N exchanges
#
# Claude Code "Stop" hook. After every assistant response:
# 1. Counts human messages in the session transcript
# 2. Every SAVE_INTERVAL messages, sends a remember command to datawatch
# 3. Extracts the last user+assistant exchange and saves to memory
#
# === INSTALL ===
# Add to .claude/settings.local.json:
#   "hooks": {
#     "Stop": [{
#       "matcher": "*",
#       "hooks": [{
#         "type": "command",
#         "command": "/path/to/datawatch_save_hook.sh",
#         "timeout": 10
#       }]
#     }]
#   }
#
# === CONFIGURATION ===

SAVE_INTERVAL=${DATAWATCH_HOOK_INTERVAL:-15}  # Save every N human messages
DATAWATCH_URL=${DATAWATCH_URL:-http://localhost:8080}
DATAWATCH_TOKEN=${DATAWATCH_TOKEN:-}
STATE_DIR="$HOME/.datawatch/hook_state"
mkdir -p "$STATE_DIR"

# Read JSON input from stdin
INPUT=$(cat)

# Parse fields
eval $(echo "$INPUT" | python3 -c "
import sys, json, re
data = json.load(sys.stdin)
sid = data.get('session_id', 'unknown')
sha = data.get('stop_hook_active', False)
tp = data.get('transcript_path', '')
safe = lambda s: re.sub(r'[^a-zA-Z0-9_/.\-~]', '', str(s))
print(f'SESSION_ID=\"{safe(sid)}\"')
print(f'STOP_HOOK_ACTIVE=\"{sha}\"')
print(f'TRANSCRIPT_PATH=\"{safe(tp)}\"')
" 2>/dev/null)

TRANSCRIPT_PATH="${TRANSCRIPT_PATH/#\~/$HOME}"

# If already in a save cycle, let through
if [ "$STOP_HOOK_ACTIVE" = "True" ] || [ "$STOP_HOOK_ACTIVE" = "true" ]; then
    echo "{}"
    exit 0
fi

# Count human messages
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
                if isinstance(content, str) and '<command-message>' in content:
                    continue
                count += 1
        except:
            pass
print(count)
PYEOF
2>/dev/null)
fi

# Track last save
LAST_SAVE_FILE="$STATE_DIR/${SESSION_ID}_last_save"
LAST_SAVE=0
[ -f "$LAST_SAVE_FILE" ] && LAST_SAVE=$(cat "$LAST_SAVE_FILE")
SINCE_LAST=$((EXCHANGE_COUNT - LAST_SAVE))

# Time to save?
if [ "$SINCE_LAST" -ge "$SAVE_INTERVAL" ] && [ "$EXCHANGE_COUNT" -gt 0 ]; then
    echo "$EXCHANGE_COUNT" > "$LAST_SAVE_FILE"

    # Extract last exchange and save to datawatch memory
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
        except:
            pass
# Last 2 exchanges (4 messages)
recent = msgs[-4:] if len(msgs) >= 4 else msgs
print(' | '.join(f"{m['role']}: {m['content'][:100]}" for m in recent))
PYEOF
2>/dev/null)

        if [ -n "$SUMMARY" ]; then
            AUTH_HEADER=""
            [ -n "$DATAWATCH_TOKEN" ] && AUTH_HEADER="-H 'Authorization: Bearer $DATAWATCH_TOKEN'"
            curl -s -X POST "$DATAWATCH_URL/api/test/message" \
                -H "Content-Type: application/json" \
                $AUTH_HEADER \
                -d "{\"text\":\"remember: [auto-save] $SUMMARY\"}" > /dev/null 2>&1
        fi
    fi
fi

echo "{}"

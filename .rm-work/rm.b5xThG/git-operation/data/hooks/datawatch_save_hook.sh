#!/bin/bash
# DATAWATCH MEMORY SAVE HOOK — Auto-save every N exchanges
SAVE_INTERVAL=${DATAWATCH_HOOK_INTERVAL:-15}
DATAWATCH_URL=${DATAWATCH_URL:-https://localhost:8443}
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
    [ -n "$SUMMARY" ] && curl -sk -X POST "$DATAWATCH_URL/api/test/message" \
      -H "Content-Type: application/json" \
      -d "{\"text\":\"remember: [auto-save] $SUMMARY\"}" > /dev/null 2>&1
  fi
fi
echo "{}"

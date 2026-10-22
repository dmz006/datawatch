#!/bin/bash
# DATAWATCH PRE-COMPACT HOOK — Save before context compression
DATAWATCH_URL=${DATAWATCH_URL:-https://localhost:8443}
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
  [ -n "$SUMMARY" ] && curl -sk -X POST "$DATAWATCH_URL/api/test/message" \
    -H "Content-Type: application/json" \
    -d "{\"text\":\"remember: $SUMMARY\"}" > /dev/null 2>&1
fi
echo "{}"

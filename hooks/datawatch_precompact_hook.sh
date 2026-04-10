#!/bin/bash
# DATAWATCH PRE-COMPACT HOOK — Save context before Claude Code context compression
#
# Claude Code "PreCompact" hook. Fires before context window shrinkage.
# Saves the current conversation summary to datawatch memory.
#
# === INSTALL ===
# Add to .claude/settings.local.json:
#   "hooks": {
#     "PreCompact": [{
#       "matcher": "*",
#       "hooks": [{
#         "type": "command",
#         "command": "/path/to/datawatch_precompact_hook.sh",
#         "timeout": 10
#       }]
#     }]
#   }

DATAWATCH_URL=${DATAWATCH_URL:-http://localhost:8080}
DATAWATCH_TOKEN=${DATAWATCH_TOKEN:-}

INPUT=$(cat)

TRANSCRIPT_PATH=$(echo "$INPUT" | python3 -c "
import sys, json, re
data = json.load(sys.stdin)
safe = lambda s: re.sub(r'[^a-zA-Z0-9_/.\-~]', '', str(s))
print(safe(data.get('transcript_path', '')))
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
                    words = content.split()[:10]
                    topics.add(' '.join(words))
        except:
            pass
print('[pre-compact] Topics: ' + '; '.join(list(topics)[:5]))
PYEOF
2>/dev/null)

    if [ -n "$SUMMARY" ]; then
        AUTH_HEADER=""
        [ -n "$DATAWATCH_TOKEN" ] && AUTH_HEADER="-H 'Authorization: Bearer $DATAWATCH_TOKEN'"
        curl -s -X POST "$DATAWATCH_URL/api/test/message" \
            -H "Content-Type: application/json" \
            $AUTH_HEADER \
            -d "{\"text\":\"remember: $SUMMARY\"}" > /dev/null 2>&1
    fi
fi

echo "{}"

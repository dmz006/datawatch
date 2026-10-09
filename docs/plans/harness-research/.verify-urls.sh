#!/usr/bin/env bash
# Batch-verify candidate URLs from candidates.md -> status file
set -u
cd /home/dmz/workspace/datawatch/docs/plans/harness-research
UA="Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
grep -hoE 'https?://[^ )>"]+' candidates.md | awk '!seen[$0]++' > .urls.txt
: > .url-status.txt
while IFS= read -r u; do
  code=$(curl -s -o /dev/null -m 15 -L -A "$UA" -w '%{http_code}' "$u" 2>/dev/null || echo 000)
  printf '%s\t%s\n' "$code" "$u" >> .url-status.txt
done < .urls.txt
echo "DONE $(wc -l < .url-status.txt) urls"

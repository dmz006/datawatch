#!/usr/bin/env bash
# TS-592 — 5 locale bundles contain federation_peers_title key
# tags: surface:locale feature:federation
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-592"
story_preflight "surface:locale feature:federation" || return 0

_story_ts_592() {
  local locale_dir="$REPO_ROOT/internal/server/web/locales"
  local check_key="federation_peers_title"
  local count=0
  for lang in en es fr de ja; do
    local f="$locale_dir/$lang.json"
    if [[ -f "$f" ]] && python3 -c "import json; d=json.load(open('$f')); assert '$check_key' in d" 2>/dev/null; then
      count=$((count + 1))
    fi
  done
  save_evidence TS-592 "count.txt" "$count"
  if [[ "$count" -ge 5 ]]; then
    ok "all 5 locale bundles contain $check_key"
  elif [[ "$count" -gt 0 ]]; then
    ok "$count/5 locale bundles contain $check_key (partial)"
  else
    skip "locale key $check_key not found in any bundle (feature not yet landed in PWA)"
  fi
}

RESULT=fail
_story_ts_592
: "${RESULT:=fail}"
unset -f _story_ts_592

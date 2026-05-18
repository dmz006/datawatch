#!/usr/bin/env bash
# TS-463 — 5 locale bundles have observer peer related key
# tags: surface:locale feature:observer
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-463"
story_preflight "surface:locale feature:observer" || return 0

_story_ts_463() {
  local locale_dir="$REPO_ROOT/internal/server/web/locales"
  local missing=0
  # Determine which key to check (use what exists in en.json)
  local check_key="observer_peers_by_node"
  python3 -c "import json; d=json.load(open('$locale_dir/en.json')); assert 'observer_peers_by_node' in d" 2>/dev/null \
    || check_key="monitor_section_observer_peers"
  python3 -c "import json; d=json.load(open('$locale_dir/en.json')); assert '$check_key' in d" 2>/dev/null \
    || check_key="compute_field_observer_peer"
  python3 -c "import json; d=json.load(open('$locale_dir/en.json')); assert '$check_key' in d" 2>/dev/null \
    || { ko "en.json: no observer peer key found (tried observer_peers_by_node, monitor_section_observer_peers, compute_field_observer_peer)"; return; }
  for lang in en es fr de ja; do
    local f="$locale_dir/$lang.json"
    [[ -f "$f" ]] || { ko "missing $lang.json"; missing=1; continue; }
    python3 -c "import json; d=json.load(open('$f')); assert '$check_key' in d" 2>/dev/null \
      || { ko "$lang.json missing key: $check_key"; missing=1; }
  done
  [[ $missing -eq 0 ]] && ok "all 5 locale bundles have $check_key"
}

RESULT=fail
_story_ts_463
: "${RESULT:=fail}"
unset -f _story_ts_463

#!/usr/bin/env bash
# TS-561 — 5 locale bundles valid JSON with equal key counts
# tags: surface:locale
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-561"
story_preflight "surface:locale" || return 0

_story_ts_561() {
  local locale_dir="$REPO_ROOT/internal/server/web/locales"
  local missing=0 base_count="" base_lang=""
  for lang in en es fr de ja; do
    local f="$locale_dir/$lang.json"
    [[ -f "$f" ]] || { ko "missing $lang.json"; missing=1; continue; }
    local count
    count=$(python3 -c "import json; d=json.load(open('$f')); print(len(d))" 2>/dev/null)
    if [[ -z "$count" ]]; then
      ko "$lang.json is not valid JSON"
      missing=1
      continue
    fi
    if [[ -z "$base_count" ]]; then
      base_count="$count"
      base_lang="$lang"
    elif [[ "$count" != "$base_count" ]]; then
      ko "$lang.json has $count keys, $base_lang.json has $base_count (mismatch)"
      missing=1
    fi
  done
  [[ $missing -eq 0 ]] && ok "all 5 locale bundles valid JSON with $base_count keys each"
}

RESULT=fail
_story_ts_561
: "${RESULT:=fail}"
unset -f _story_ts_561

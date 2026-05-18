#!/usr/bin/env bash
# TS-185 — Locale completeness: 5 locale files have identical key sets
# tags: surface:parity feature:locale
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-185"
story_preflight "surface:parity feature:locale" || return 0

_story_ts_185() {
    echo ""; echo "  >> TS-185: Locale completeness: 5 locale files have identical key sets"
    locale_dir="$REPO_ROOT/internal/server/web/locales"
    if [[ -d "$locale_dir" ]]; then
      en_keys=$(python3 -c "import json; d=json.load(open('$locale_dir/en.json')); print(sorted(d.keys()))" 2>/dev/null)
      all_match=true
      for lang in es fr de ja; do
        if [[ -f "$locale_dir/$lang.json" ]]; then
          keys=$(python3 -c "import json; d=json.load(open('$locale_dir/$lang.json')); print(sorted(d.keys()))" 2>/dev/null)
          if [[ "$keys" != "$en_keys" ]]; then
            ko "Locale $lang key set differs from en"
            all_match=false
          fi
        else
          ko "Locale $lang.json missing"
          all_match=false
        fi
      done
      [[ "$all_match" == "true" ]] && ok "All 5 locale files have identical key sets"
      save_evidence "TS-185" "en_keys.txt" "$en_keys"
    else
      skip "Locale dir not found at $locale_dir"
    fi

}

RESULT=fail
_story_ts_185
: "${RESULT:=fail}"
unset -f _story_ts_185

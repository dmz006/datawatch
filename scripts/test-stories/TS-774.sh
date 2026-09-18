#!/usr/bin/env bash
# TS-774 — Goose config parity: API config matches testdata/datawatch.yaml
# tags: surface:api feature:goose parallel:ok
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-774"

story_preflight "surface:api feature:goose" || exit 0

# GET config from daemon.
cfg_raw=$(curl "${curl_args[@]}" "$TEST_TLS/api/config" 2>/dev/null)
goose_enabled=$(echo "$cfg_raw" | python3 -c "import sys,json; c=json.load(sys.stdin); print(c.get('goose',{}).get('enabled','?'))" 2>/dev/null)
goose_binary=$(echo "$cfg_raw" | python3 -c "import sys,json; c=json.load(sys.stdin); print(c.get('goose',{}).get('binary','?'))" 2>/dev/null)
echo "  [TS-774] daemon config: goose.enabled=$goose_enabled goose.binary=$goose_binary"
save_evidence "$CURRENT_STORY" "config.json" "$cfg_raw"

if [[ "$goose_enabled" == "?" ]]; then
  ko "GET /api/config has no goose section"
  exit 0
fi

# Check testdata/datawatch.yaml as the canonical source.
testdata_yaml="$REPO_ROOT/testdata/datawatch.yaml"
if [[ ! -f "$testdata_yaml" ]]; then
  ko "testdata/datawatch.yaml not found at $testdata_yaml"
  exit 0
fi
td_enabled=$(python3 -c "
import sys
try:
    import yaml
    with open('$testdata_yaml') as f:
        d = yaml.safe_load(f)
    print(str(d.get('goose',{}).get('enabled',False)).lower())
except ImportError:
    import re
    content=open('$testdata_yaml').read()
    m=re.search(r'goose:\s*\n\s+enabled:\s*(\S+)', content)
    print(m.group(1).lower() if m else '?')
" 2>/dev/null)
td_binary=$(python3 -c "
import sys
try:
    import yaml
    with open('$testdata_yaml') as f:
        d = yaml.safe_load(f)
    print(d.get('goose',{}).get('binary','?'))
except ImportError:
    import re
    content=open('$testdata_yaml').read()
    m=re.search(r'binary:\s*(\S+)', content)
    print(m.group(1) if m else '?')
" 2>/dev/null)
echo "  [TS-774] testdata: goose.enabled=$td_enabled goose.binary=$td_binary"
save_evidence "$CURRENT_STORY" "testdata.txt" "enabled=$td_enabled binary=$td_binary"

# Verify binary actually exists.
binary_exists=false
if [[ -x "$goose_binary" ]]; then
  binary_exists=true
  goose_ver=$("$goose_binary" --version 2>/dev/null | head -1 || echo "unknown")
  echo "  [TS-774] goose binary exists: $goose_binary version=$goose_ver"
fi

if [[ "${goose_enabled,,}" != "true" ]]; then
  ko "goose.enabled is not true in daemon config (got $goose_enabled) — check testdata/datawatch.yaml"
  exit 0
fi
if [[ -z "$goose_binary" || "$goose_binary" == "?" ]]; then
  ko "goose.binary not set in daemon config"
  exit 0
fi
if [[ "$binary_exists" != "true" ]]; then
  ko "goose binary $goose_binary not executable — install required"
  exit 0
fi
if [[ "${td_enabled}" != "true" ]]; then
  ko "testdata/datawatch.yaml has goose.enabled=$td_enabled (expected true) — config drift"
  exit 0
fi

ok "goose config parity: enabled=$goose_enabled binary=$goose_binary testdata_enabled=$td_enabled binary_exists=$binary_exists"

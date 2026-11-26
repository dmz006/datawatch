#!/usr/bin/env bash
# The release smoke must test the daemon of this tree. With no DATAWATCH_BIN and
# no binary in the tree it builds one with "make build"; it never falls back to a
# datawatch found on PATH, which may be an older or the operator's own build.
# Offline: runs only the binary selection block of scripts/release-smoke.sh with
# a fake make and a fake datawatch. No daemon is started.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM

block="$TMP/select.sh"
awk '/^  # Find the datawatch binary/{p=1} /^  echo "Smoke run ID : \$RUN_ID"/{p=0} p' \
  "$ROOT/scripts/release-smoke.sh" >"$block"
[[ -s "$block" ]] || { echo "FAIL: binary selection block not found in release-smoke.sh"; exit 1; }
# The block ends in a "[[ ... ]] && {...}" line; do not let its false test end
# the shell that sources it.
echo true >>"$block"

mkdir -p "$TMP/path"
printf '#!/bin/sh\nexit 0\n' >"$TMP/path/datawatch"
# Fake "make build": records the call and, unless MAKE_FAILS, writes bin/datawatch.
cat >"$TMP/path/make" <<'MK'
#!/bin/sh
echo "$*" >>"$PWD/make.calls"
[ -n "$MAKE_FAILS" ] && exit 1
mkdir -p "$PWD/bin" && printf '#!/bin/sh\nexit 0\n' >"$PWD/bin/datawatch" && chmod +x "$PWD/bin/datawatch"
MK
chmod +x "$TMP/path/datawatch" "$TMP/path/make"

# run <repo dir> [DATAWATCH_BIN] [MAKE_FAILS]: prints the chosen binary last.
run() {
  env -i PATH="$TMP/path:/usr/bin:/bin" REPO_DIR="$1" DATAWATCH_BIN="${2:-}" MAKE_FAILS="${3:-}" \
    bash -c 'set -euo pipefail; source "$1"; printf "BIN=%s\n" "$_DW_BIN"' _ "$block" 2>&1
}

fails=0
check() { # check <label> <expected> <actual>
  if [[ "$2" == "$3" ]]; then echo "ok: $1"; else echo "FAIL: $1: want '$2', got '$3'"; fails=$((fails + 1)); fi
}

mkdir -p "$TMP/empty" "$TMP/built" "$TMP/override" "$TMP/broken"
out="$(run "$TMP/empty" || true)"
check "empty tree uses the binary built from it" "BIN=$TMP/empty/bin/datawatch" "$(tail -n1 <<<"$out")"
check "empty tree runs make build" "build" "$(cat "$TMP/empty/make.calls" 2>/dev/null || true)"

printf '#!/bin/sh\nexit 0\n' >"$TMP/built/datawatch"; chmod +x "$TMP/built/datawatch"
out="$(run "$TMP/built" || true)"
check "a binary in the tree is used as it is" "BIN=$TMP/built/datawatch" "$(tail -n1 <<<"$out")"
check "a binary in the tree is not rebuilt" "" "$(cat "$TMP/built/make.calls" 2>/dev/null || true)"

out="$(run "$TMP/override" /opt/some/datawatch || true)"
check "DATAWATCH_BIN wins" "BIN=/opt/some/datawatch" "$(tail -n1 <<<"$out")"
check "DATAWATCH_BIN skips the build" "" "$(cat "$TMP/override/make.calls" 2>/dev/null || true)"

if out="$(run "$TMP/broken" "" 1)"; then rc=0; else rc=$?; fi
check "a failed build stops the smoke" "1" "$rc"
check "a failed build never picks the PATH binary" "" "$(grep -c "BIN=$TMP/path" <<<"$out" | grep -v '^0$' || true)"

[[ "$fails" -eq 0 ]] || exit 1
echo "PASS: smoke binary selection"

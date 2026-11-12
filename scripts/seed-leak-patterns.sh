#!/usr/bin/env bash
# scripts/seed-leak-patterns.sh — seed the operator's local leak patterns
# file with the names the local datawatch daemon knows (federation peers,
# compute nodes, observer peers, pool hosts) so the whole-tree leak check
# (make check-leaks) catches them as 'local' class findings.
#
# Usage:
#   scripts/seed-leak-patterns.sh [target-file]
#
#   target-file defaults to .leak-patterns.local at the repository root
#   (resolved against the current directory, matching the make target).
#
# Behaviour:
#   * When the target is absent it is created from
#     scripts/leak-patterns.local.template. An existing target is NEVER
#     overwritten; only names missing from it are appended.
#   * Appended names go after a '# seeded peer names' comment line (added
#     once per file), one name per line. Names are trimmed; blank lines and
#     lines starting with '#' in the source list are skipped; duplicates are
#     removed case-insensitively against the existing file content.
#   * A candidate name is NEVER seeded when it is shorter than 5 characters
#     or when it matches a whole word in the tracked generic-name stoplist
#     scripts/leak-patterns.skip (case-insensitive). Such names would match
#     thousands of ordinary code lines; the skipped count is printed as
#     'skipped N generic names' (count only, never a name).
#   * Output is counts only: 'seeded N names'. A name is never printed.
#     When the name source yields no names at all, a one-line notice saying
#     nothing was seeded is printed instead of a silent empty run.
#   * When the daemon is not reachable, nothing is seeded, a line saying so
#     is printed, and the script exits 0.
#   * Test hook: when LEAK_SEED_NAMES_CMD is set, it is the command whose
#     stdout supplies the names (one per line) instead of the daemon; used by
#     tests/test-seed-leak-patterns.sh.
#
# Daemon discovery (read at run time): DW_BASE (with DW_BASE_TOKEN) overrides
# everything; otherwise the local config (DW_CONFIG, default
# ~/.datawatch/config.yaml) selects 127.0.0.1 with server.port (default
# 8080) or, when server.tls_enabled, server.tls_port (default 8443), and
# server.token is sent as the bearer token — the same resolution the
# datawatch CLI uses. Sources:
#   GET /api/federation/peers  — name of every federation peer
#   GET /api/compute/nodes     — name of every compute node
#   GET /api/observer/peers    — name of every observer peer
#   GET /api/capacity          — pool hosts (the part after 'node:'/'llm:')
#
# JSON is parsed with jq when available, otherwise python3 -I; when neither
# is present the runs seed nothing (graceful — no parsing, no guessing).

set -u
export LC_ALL=C

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE="$SCRIPT_DIR/leak-patterns.local.template"
TARGET="${1:-.leak-patterns.local}"

die2() {
    echo "seed-leak-patterns: $*" >&2
    exit 2
}

# ----------------------------------------------- target file (template)
if [ ! -e "$TARGET" ]; then
    [ -f "$TEMPLATE" ] || die2 "template not found: $TEMPLATE"
    cp "$TEMPLATE" "$TARGET" 2>/dev/null || die2 "cannot create target from template: $TARGET (exit 2)"
fi
[ -f "$TARGET" ] || die2 "target is not a regular file: $TARGET (exit 2)"

# ---------------------------------------------------------------- json tool
JSON_TOOL=""
if command -v jq >/dev/null 2>&1; then
    JSON_TOOL=jq
elif command -v python3 >/dev/null 2>&1; then
    JSON_TOOL=python3
fi

# names_from_json FILE JQ_FILTER SEL_PATH STRIP(0|1)
# SEL_PATH is the dot path of the entry array inside the JSON document
# ("" = the document itself is the array); STRIP=1 drops everything up to
# and including the first ':' in each name (pool id → pool host).
names_from_json() {
    local file="$1" jqf="$2" sel="$3" strip="${4:-0}"
    case "$JSON_TOOL" in
    jq)
        jq -r "$jqf" "$file" 2>/dev/null || true
        ;;
    python3)
        SEL="$sel" STRIP="$strip" python3 -I - "$file" <<'PY' 2>/dev/null || true
import json, os, sys
try:
    with open(sys.argv[1], "r", encoding="utf-8", errors="replace") as f:
        d = json.load(f)
except Exception:
    sys.exit(0)
for part in [p for p in os.environ.get("SEL", "").split(".") if p]:
    d = d.get(part) if isinstance(d, dict) else None
if not isinstance(d, list):
    sys.exit(0)
strip = os.environ.get("STRIP", "0") == "1"
for e in d:
    if isinstance(e, dict) and isinstance(e.get("name"), str):
        v = e["name"].strip()
        if strip:
            # Only node: pools name a machine; llm: pools name an LLM entry
            # (claude-code, pi-prompt, ...), which is product vocabulary.
            if not v.startswith("node:"):
                continue
            v = v.split(":", 1)[1].strip()
        if v:
            print(v)
PY
        ;;
    esac
}

# Emit the JSON array (or "") for PATH against the daemon; "" = no JSON tool,
# a fetch failure, or an unparseable body — all non-fatal for one source.
fetch_json() {
    local path="$1" body
    [ -n "$JSON_TOOL" ] || return 0
    body="$(curl "${DW_CURL_ARGS[@]}" "$DW_BASE_URL$path" 2>/dev/null)" || true
    printf '%s' "$body"
    return 0
}

emit_names() { # BODY JQ_FILTER SEL_PATH STRIP
    printf '%s' "$1" >"$NAMES_TMP" 2>/dev/null || return 0
    [ -s "$NAMES_TMP" ] || return 0
    names_from_json "$NAMES_TMP" "$2" "$3" "${4:-0}"
    return 0
}

# ------------------------------------------------------------ name sources
# Emits raw name lines (one per line) from whichever source applies.
# resolve_daemon sets DW_BASE_URL / DW_TLS / DW_CURL_ARGS in this shell.
resolve_daemon() {
    local base="" token=""
    local cfg="${DW_CONFIG:-$HOME/.datawatch/config.yaml}"
    if [ -n "${DW_BASE:-}" ]; then
        base="${DW_BASE%/}"
        token="${DW_BASE_TOKEN:-}"
    else
        [ -f "$cfg" ] || base="http://127.0.0.1:8080"
    fi
    case "$base" in
    https://*) DW_TLS=1 ;;
    *)
        DW_TLS=0
        if [ -z "$base" ] && [ -f "$cfg" ]; then
            _cfg_get() {
                awk -v key="$1" '
                    /^server:[ \t]*(#.*)?[ \t]*$/ { inblk = 1; next }
                    inblk && /^[^ \t#]/ { inblk = 0 }
                    inblk && $0 ~ "^[ \t]*" key ":" {
                        v = $0
                        sub(/^[ \t]*[^:]*:[ \t]*/, "", v)
                        sub(/[ \t]+(#.*)?$/, "", v)
                        sub(/^["'\'']/, "", v); sub(/["'\'']$/, "", v)
                        print v
                        exit
                    }' "$cfg" 2>/dev/null
            }
            local port tlsen tlsp tok
            port="$(_cfg_get port)"
            tlsen="$(_cfg_get tls_enabled)"
            tlsp="$(_cfg_get tls_port)"
            tok="$(_cfg_get token)"
            [ -n "$tok" ] && token="$tok"
            case "$tlsen" in
            true | True | TRUE) base="https://127.0.0.1:${tlsp:-8443}"; DW_TLS=1 ;;
            *) base="http://127.0.0.1:${port:-8080}" ;;
            esac
        fi
        [ -n "$base" ] || base="http://127.0.0.1:8080"
        ;;
    esac

    # One curl argument list for the probe and for each source fetch. Kept
    # in a single array so the bearer token is never re-expanded inline
    # (keeps it out of any error message).
    DW_CURL_ARGS=(-s --max-time 8)
    [ "$DW_TLS" = 1 ] && DW_CURL_ARGS+=(-k)
    [ -n "$token" ] && DW_CURL_ARGS+=(-H "Authorization: Bearer $token")
    DW_BASE_URL="$base"
    return 0
}

# The four daemon sources. A failing source contributes no names; the run
# goes on with the rest.
fetch_all_names() {
    local body
    body="$(fetch_json /api/federation/peers)"
    [ -n "$body" ] && emit_names "$body" \
        '.[]? | .name? | select(type == "string") | select(length > 0)' '' 0
    body="$(fetch_json /api/compute/nodes)"
    [ -n "$body" ] && emit_names "$body" \
        '.nodes[]? | .name? | select(type == "string") | select(length > 0)' nodes 0
    body="$(fetch_json /api/observer/peers)"
    [ -n "$body" ] && emit_names "$body" \
        '.peers[]? | .name? | select(type == "string") | select(length > 0)' peers 0
    body="$(fetch_json /api/capacity)"
    [ -n "$body" ] && emit_names "$body" \
        '.pools[]? | .name? | select(type == "string") | select(startswith("node:")) | sub("^[^:]*:"; "") | select(length > 0)' pools 1
    return 0
}

# ------------------------------------------------- filter, dedup, append
NAMES_TMP="$(mktemp)" || die2 "mktemp failed (exit 2)"
trap 'rm -f "$NAMES_TMP"' EXIT

# Generic-name stoplist (tracked): a candidate equal to any of these words
# (case-insensitive, whole name) is refused. Absent file = empty stoplist.
STOPFILE="$SCRIPT_DIR/leak-patterns.skip"
declare -A STOP=()
if [ -f "$STOPFILE" ]; then
    while IFS= read -r w; do
        w="$(printf '%s' "$w" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
        [ -n "$w" ] || continue
        case "$w" in '#'*) continue ;; esac
        STOP["$(tr '[:upper:]' '[:lower:]' <<<"$w")"]=1
    done < "$STOPFILE"
fi

if [ -n "${LEAK_SEED_NAMES_CMD:-}" ]; then
    # Test hook: the command's stdout IS the name list.
    raw="$(bash -c "$LEAK_SEED_NAMES_CMD" 2>/dev/null)" || raw=""
    if [ -z "$raw" ]; then
        echo "seed-leak-patterns: source gave no names — nothing seeded"
    fi
elif [ -z "$JSON_TOOL" ]; then
    # Neither jq nor python3: nothing can be parsed, so nothing is seeded.
    raw=""
else
    resolve_daemon
    PROBE=(-s -o /dev/null -w '%{http_code}' --max-time 8)
    [ "${DW_TLS:-0}" = "1" ] && PROBE+=(-k)
    code="$(curl "${PROBE[@]}" "${DW_BASE_URL}/api/health" 2>/dev/null)" || code=000
    if ! printf '%s' "$code" | grep -qE '^2'; then
        echo "seed-leak-patterns: daemon not reachable — nothing seeded"
        exit 0
    fi
    raw="$(fetch_all_names)"
fi

# Trim, drop blanks and comments, keep first occurrence per name
# (case-insensitive).
cleaned="$(printf '%s\n' "$raw" | awk '
    { sub(/\r$/, "") }
    { gsub(/^[ \t]+/, ""); gsub(/[ \t]+$/, "") }
    length($0) == 0 { next }
    /^#/ { next }
    { k = tolower($0); if (!(k in seen)) { seen[k] = 1; print } }
')"

# Case-insensitive set of the target's existing non-comment lines.
declare -A have=()
while IFS= read -r line; do
    [ -n "$line" ] || continue
    have["$(tr '[:upper:]' '[:lower:]' <<<"$line")"]=1
done < <(grep -vE '^[[:space:]]*(#|$)' "$TARGET" 2>/dev/null)

added=""
count=0
skipped=0
while IFS= read -r name; do
    [ -n "$name" ] || continue
    key="$(tr '[:upper:]' '[:lower:]' <<<"$name")"
    [ -n "${have[$key]:-}" ] && continue
    if [ "${#name}" -lt 5 ] || [ -n "${STOP[$key]:-}" ]; then
        skipped=$((skipped + 1))
        continue
    fi
    have["$key"]=1
    added+="$name"$'\n'
    count=$((count + 1))
done <<<"$cleaned"

if [ "$count" -gt 0 ]; then
    # Keep the file line-oriented: ensure a trailing newline before appending.
    if [ -s "$TARGET" ] && [ "$(tail -c 1 "$TARGET" | wc -l)" = "0" ]; then
        printf '\n' >>"$TARGET"
    fi
    {
        if ! grep -qE '^[[:space:]]*#[[:space:]]*seeded peer names[[:space:]]*$' "$TARGET" 2>/dev/null; then
            printf '\n# seeded peer names\n'
        fi
        printf '%s' "$added"
    } >>"$TARGET" || die2 "cannot append names to $TARGET (exit 2)"
fi

echo "seeded $count names"
if [ "$skipped" -gt 0 ]; then
    echo "skipped $skipped generic names"
fi
exit 0

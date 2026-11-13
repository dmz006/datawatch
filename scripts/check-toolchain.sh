#!/usr/bin/env bash
# scripts/check-toolchain.sh — compare the installed development tools with the newest
# stable releases listed in toolchain.yaml. Read-only: runs only each tool's version
# command, makes HTTPS reads, and writes nothing outside a mktemp dir removed on exit.
#
# Output: a table of  tool | installed | newest | status, then a note per UNREACHABLE source.
# Statuses:
#   CURRENT       installed is the newest (or newer)
#   PATCH-BEHIND  non-Go tool, same major.minor, older patch (informational only)
#   BEHIND        non-Go tool, a minor or major behind
#   GO-BEHIND     Go is behind the newest stable patch of the go.mod minor line
#   MISSING       tool not on PATH (reported, never fatal)
#   UNREACHABLE   the newest-version source (or the installed version) could not be read/parsed
#
# Exit codes: 1 when Go is GO-BEHIND or any tool is BEHIND; 0 in every other case,
# including UNREACHABLE and MISSING. A network error never causes a non-zero exit.
#
# Environment:
#   TOOLCHAIN_FILE         path of the tool list (default: toolchain.yaml at the repo root)
#   TOOLCHAIN_FIXTURE_DIR  test hook. When set the network is never touched. The newest
#                          version of each tool is read from a file in this directory:
#                            <tool name>   containing the version string (e.g. 2.22.1 or v2.22.1)
#                            go.json       a copy of the go.dev/dl JSON shape (for tool "go")
#                          An absent or unparsable fixture file means that source is UNREACHABLE.
#                          Installed versions still come from running the version command, so
#                          tests can put fake tool scripts first on PATH.
#   GITHUB_TOKEN           optional; sent only to api.github.com to lift the rate limit.
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TOOLCHAIN_FILE="${TOOLCHAIN_FILE:-$REPO_ROOT/toolchain.yaml}"
FIXTURE_DIR="${TOOLCHAIN_FIXTURE_DIR:-}"

TMPD="$(mktemp -d)" || { echo "error: cannot create a temp dir" >&2; exit 0; }
trap 'rm -rf "$TMPD"' EXIT

if [[ ! -f "$TOOLCHAIN_FILE" ]]; then
    echo "warning: tool list not found: $TOOLCHAIN_FILE" >&2
    exit 0
fi

# ---- parse the line-oriented tool list into parallel arrays ----
t_name=(); t_cmd=(); t_sed=(); t_target=(); t_kind=(); t_url=()
n=-1
while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%$'\r'}"
    [[ "$line" =~ ^[[:space:]]*(#|$) ]] && continue
    if [[ "$line" =~ ^(-[[:space:]]+)?[[:space:]]*([a-z_]+):[[:space:]]*(.*)$ ]]; then
        starts="${BASH_REMATCH[1]}"; key="${BASH_REMATCH[2]}"; val="${BASH_REMATCH[3]}"
        val="${val%"${val##*[![:space:]]}"}"
        if [[ "$val" == \'*\' && ${#val} -ge 2 ]]; then val="${val:1:${#val}-2}"; fi
        if [[ -n "$starts" ]]; then
            n=$((n + 1))
            t_name[n]=""; t_cmd[n]=""; t_sed[n]=""; t_target[n]=""; t_kind[n]=""; t_url[n]=""
        fi
        (( n < 0 )) && continue
        case "$key" in
            name) t_name[n]="$val" ;;
            version_cmd) t_cmd[n]="$val" ;;
            version_sed) t_sed[n]="$val" ;;
            target_file) t_target[n]="$val" ;;
            newest_kind) t_kind[n]="$val" ;;
            newest_url) t_url[n]="$val" ;;
        esac
    fi
done < "$TOOLCHAIN_FILE"

# ---- helpers ----
# fetch URL -> stdout; HTTPS only, short timeouts. Fails (non-zero) when it cannot read.
fetch() {
    local url="$1"
    if command -v curl >/dev/null 2>&1; then
        local -a args=(-fsSL --proto '=https' --proto-redir '=https' --connect-timeout 5 --max-time 15)
        if [[ -n "${GITHUB_TOKEN:-}" && "$url" == https://api.github.com/* ]]; then
            args+=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
        fi
        curl "${args[@]}" "$url" 2>/dev/null
    elif command -v wget >/dev/null 2>&1; then
        wget -q -O - --timeout=15 --tries=1 "$url" 2>/dev/null
    else
        return 1
    fi
}

# source_desc index -> names the source read for that tool (for UNREACHABLE notes).
source_desc() {
    if [[ -n "$FIXTURE_DIR" ]]; then
        if [[ "${t_kind[$1]}" == "go-dl" ]]; then echo "fixture $FIXTURE_DIR/go.json"
        else echo "fixture $FIXTURE_DIR/${t_name[$1]}"; fi
    else
        echo "${t_url[$1]:-<no newest_url set>}"
    fi
}

# source_text index -> prints the raw source body.
source_text() {
    local i="$1"
    if [[ -n "$FIXTURE_DIR" ]]; then
        local f="$FIXTURE_DIR/${t_name[$i]}"
        [[ "${t_kind[$i]}" == "go-dl" ]] && f="$FIXTURE_DIR/go.json"
        [[ -r "$f" ]] && cat "$f"
        return
    fi
    [[ -n "${t_url[$i]}" ]] || return 1
    fetch "${t_url[$i]}"
}

semver_or_empty() { # first x.y[.z] in input, normalised to x.y.z; empty when none
    local v
    v="$(sed -nE 's/^[^0-9]*([0-9]+\.[0-9]+(\.[0-9]+)?).*/\1/p' <<<"$1" | head -1)"
    [[ -z "$v" ]] && return 0
    [[ "$v" =~ ^[0-9]+\.[0-9]+$ ]] && v="$v.0"
    echo "$v"
}

version_lt() { # a < b (x.y.z)
    [[ "$1" == "$2" ]] && return 1
    [[ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -1)" == "$1" ]]
}

# newest_version index -> prints the newest version; returns 1 when unreadable/unparsable.
newest_version() {
    local i="$1" body v line
    body="$(source_text "$i")" || return 1
    [[ -n "$body" ]] || return 1
    case "${t_kind[$i]}" in
        go-dl)
            local minor="$GO_LINE"
            [[ -n "$minor" ]] || return 1
            # pair each top-level "version" with the "stable" flag that follows it
            v="$(tr ',{' '\n\n' <<<"$body" | awk -v m="$minor" '
                /"version"[[:space:]]*:/ { s=$0; sub(/.*"version"[[:space:]]*:[[:space:]]*"go/, "", s); sub(/".*/, "", s); cur=s; next }
                /"stable"[[:space:]]*:/ { if ($0 ~ /true/ && cur != "") print cur; cur="" }
            ' | awk -v m="$minor" '
                { x=$0; if (x == m) x = m ".0"
                  if (x ~ "^" m "\\.[0-9]+$") print x }
            ' | sort -V | tail -1)"
            ;;
        github)
            v="$(sed -nE 's/.*"tag_name"[[:space:]]*:[[:space:]]*"v?([0-9]+\.[0-9]+\.[0-9]+)".*/\1/p' <<<"$body" | head -1)"
            ;;
        goproxy)
            v="$(sed -nE 's/.*"Version"[[:space:]]*:[[:space:]]*"v?([0-9]+\.[0-9]+\.[0-9]+)".*/\1/p' <<<"$body" | head -1)"
            if [[ -z "$v" && -n "$FIXTURE_DIR" ]]; then v="$(semver_or_empty "$body")"; fi
            ;;
        *)
            v=""
            ;;
    esac
    # fixtures for non-go tools may hold just the version string
    if [[ -z "$v" && -n "$FIXTURE_DIR" && "${t_kind[$i]}" == "github" ]]; then v="$(semver_or_empty "$body")"; fi
    [[ -n "$v" ]] || return 1
    echo "$v"
}

# ---- Go target line from go.mod (major.minor), read at runtime ----
GO_LINE=""
for ((i = 0; i <= n; i++)); do
    if [[ "${t_name[$i]}" == "go" ]]; then
        tf="${t_target[$i]:-go.mod}"
        [[ "$tf" == /* ]] || tf="$REPO_ROOT/$tf"
        [[ -r "$tf" ]] && GO_LINE="$(awk '/^go[[:space:]]+[0-9]/ { split($2, a, "."); print a[1] "." a[2]; exit }' "$tf")"
    fi
done

# ---- run ----
rows=(); notes=(); rc=0
add_row() { rows+=("$1|$2|$3|$4"); }

for ((i = 0; i <= n; i++)); do
    name="${t_name[$i]}"
    [[ -n "$name" ]] || continue
    read -r -a cmd <<<"${t_cmd[$i]}"
    if [[ ${#cmd[@]} -eq 0 ]] || ! command -v "${cmd[0]}" >/dev/null 2>&1; then
        add_row "$name" "-" "-" "MISSING"
        continue
    fi
    if command -v timeout >/dev/null 2>&1; then
        out="$(timeout 20 "${cmd[@]}" 2>&1)" || true
    else
        out="$("${cmd[@]}" 2>&1)" || true
    fi
    installed="$(sed -nE "${t_sed[$i]}" <<<"$out" 2>/dev/null | head -1)"
    installed="$(semver_or_empty "$installed")"
    if [[ -z "$installed" ]]; then
        add_row "$name" "unknown" "-" "UNREACHABLE"
        notes+=("$name: could not parse the installed version from: ${t_cmd[$i]}")
        continue
    fi
    if ! newest="$(newest_version "$i")"; then
        add_row "$name" "$installed" "-" "UNREACHABLE"
        notes+=("$name: could not read or parse the newest version from: $(source_desc "$i")")
        continue
    fi
    status="CURRENT"
    if version_lt "$installed" "$newest"; then
        if [[ "${t_kind[$i]}" == "go-dl" ]]; then
            status="GO-BEHIND"; rc=1
        elif [[ "${installed%.*}" != "${newest%.*}" ]]; then
            status="BEHIND"; rc=1
        else
            status="PATCH-BEHIND"
        fi
    fi
    add_row "$name" "$installed" "$newest" "$status"
done

printf '%-15s %-12s %-12s %s\n' "TOOL" "INSTALLED" "NEWEST" "STATUS"
for r in "${rows[@]+"${rows[@]}"}"; do
    IFS='|' read -r a b c d <<<"$r"
    printf '%-15s %-12s %-12s %s\n' "$a" "$b" "$c" "$d"
done
for note in "${notes[@]+"${notes[@]}"}"; do
    echo "note: $note" >&2
done
exit "$rc"

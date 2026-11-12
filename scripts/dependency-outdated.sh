#!/usr/bin/env bash
# scripts/dependency-outdated.sh — find outdated dependency pins. Never edits any file.
#
#   --list-pins   print every current pin as one JSON object per line (no network):
#                 Go modules (go.mod direct requires), container base images (FROM lines
#                 in docker/dockerfiles/Dockerfile.* and Dockerfile.dev) and GitHub Actions
#                 (every 'uses:' in .github/workflows).
#   --check       (default) add the newest available version for each pin and pipe the
#                 result through scripts/dependency-update-classify.py.
#
# Exit codes: 0 ok, 1 a pin is malformed (e.g. an action without a version comment) or a
# lookup failed, 2 bad usage.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"
TIMEOUT=20
mode="--check"
[[ $# -gt 0 ]] && mode="$1"
case "$mode" in
    --list-pins|--check) ;;
    *) echo "usage: $0 [--list-pins|--check]" >&2; exit 2 ;;
esac

pin_errors=0

list_gomod() {
    awk '
        /^require \(/ { inb=1; next }
        inb && /^\)/ { inb=0; next }
        inb && !/\/\/ indirect/ && NF>=2 { print $1, $2; next }
        /^require [^(]/ && !/\/\/ indirect/ { print $2, $3 }
    ' go.mod | while read -r name ver; do
        jq -nc --arg n "$name" --arg v "$ver" '{ecosystem:"gomod",name:$n,version:$v}'
    done
}

list_images() {
    local f
    for f in docker/dockerfiles/Dockerfile.* Dockerfile.dev; do
        [[ -f "$f" ]] || continue
        awk -v file="$f" '
            toupper($1) == "FROM" {
                i = 2
                while ($i ~ /^--/) i++
                ref = $i
                if (ref in stages) { for (j = i; j <= NF; j++) if (toupper($j) == "AS") stages[$(j+1)] = 1; next }
                for (j = i; j <= NF; j++) if (toupper($j) == "AS") stages[$(j+1)] = 1
                print file "\t" ref
            }
        ' "$f"
    done | while IFS=$'\t' read -r file ref; do
        image="$ref"; tag="latest"
        last="${ref##*/}"
        if [[ "$last" == *:* ]]; then
            tag="${last#*:}"
            image="${ref%:*}"
        fi
        if [[ "$ref" == *'$'* ]]; then
            jq -nc --arg i "$image" --arg t "$tag" --arg f "$file" --arg r "$ref" \
                '{ecosystem:"docker",image:$i,tag:$t,file:$f,ref:$r,unresolved_arg:true}'
        else
            jq -nc --arg i "$image" --arg t "$tag" --arg f "$file" \
                '{ecosystem:"docker",image:$i,tag:$t,file:$f}'
        fi
    done
}

list_actions() {
    local line file n rest ref action hash ver
    while IFS= read -r line; do
        file="${line%%:*}"; rest="${line#*:}"; n="${rest%%:*}"; rest="${rest#*:}"
        ref="$(sed -E 's/^[[:space:]]*(-[[:space:]]*)?uses:[[:space:]]*//' <<<"$rest")"
        ref="${ref%%#*}"; ref="$(tr -d ' \t\r"'"'" <<<"$ref")"
        [[ "$ref" == ./* || "$ref" == docker://* ]] && continue
        action="${ref%@*}"; hash="${ref#*@}"
        ver=""
        if [[ "$rest" =~ \#[[:space:]]*(v[0-9][0-9A-Za-z.+_-]*) ]]; then ver="${BASH_REMATCH[1]}"; fi
        if [[ ! "$hash" =~ ^[0-9a-f]{40}$ || -z "$ver" ]]; then
            echo "error: ${file}:${n}: '${ref}' needs a full 40-character commit hash and a trailing '# vN' version comment" >&2
            echo 1 >> "$ERRFILE"
            continue
        fi
        jq -nc --arg a "$action" --arg h "$hash" --arg v "$ver" --arg f "$file" \
            '{ecosystem:"github-actions",action:$a,hash:$h,version:$v,file:$f}'
    done < <(grep -rn --include='*.yaml' --include='*.yml' -E '^[[:space:]]*(-[[:space:]]*)?uses:' .github/workflows | sort)
}

ERRFILE="$(mktemp)"
trap 'rm -f "$ERRFILE"' EXIT

all_pins() {
    list_gomod
    list_images
    list_actions
}

if [[ "$mode" == "--list-pins" ]]; then
    all_pins
    [[ -s "$ERRFILE" ]] && exit 1
    exit 0
fi

# ---- --check ----
for tool in jq curl go python3; do
    command -v "$tool" >/dev/null 2>&1 || { echo "error: required tool '$tool' not found" >&2; exit 1; }
done

fail_note() { echo "warning: $*" >&2; echo 1 >> "$ERRFILE"; }

latest_modules() {
    local names
    names="$(go list -m -f '{{if not .Indirect}}{{.Path}}{{end}}' all 2>/dev/null | tr '\n' ' ')" || true
    # shellcheck disable=SC2086
    if ! timeout "$TIMEOUT" go list -m -u -json $names 2>/dev/null | jq -c 'select(.Update != null) | {name:.Path, new:.Update.Version}'; then
        fail_note "go list -m -u failed or timed out; the module proxy may be unreachable"
    fi
}

registry_tags() { # image -> one tag per line
    local image="$1" first="${1%%/*}" repo json
    if [[ "$image" != */* ]]; then repo="library/$image"; first="docker.io"
    elif [[ "$first" != *.* && "$first" != *:* ]]; then repo="$image"; first="docker.io"
    else repo="${image#*/}"; fi
    if [[ "$first" == "docker.io" ]]; then
        json="$(curl -fsS --max-time "$TIMEOUT" "https://hub.docker.com/v2/repositories/${repo}/tags?page_size=100&ordering=last_updated")" || return 1
        jq -r '.results[].name' <<<"$json"
    else
        json="$(curl -fsS --max-time "$TIMEOUT" "https://${first}/v2/${repo}/tags/list")" || return 1
        jq -r '.tags[]' <<<"$json"
    fi
}

latest_image_tag() { # image current-tag -> newest tag of the same shape
    local image="$1" cur="$2" suffix pat tags
    [[ "$cur" =~ ^v?[0-9][0-9.]*(-.+)?$ ]] || { echo "$cur"; return 0; }
    suffix="$(sed -E 's/^v?[0-9][0-9.]*//' <<<"$cur")"
    pat="^$(sed -E 's/^(v?)[0-9][0-9.]*.*/\1/' <<<"$cur")[0-9][0-9.]*$(sed 's/[.]/\\./g' <<<"$suffix")\$"
    tags="$(registry_tags "$image")" || return 1
    { grep -E "$pat" <<<"$tags" || true; echo "$cur"; } | sort -V | tail -1
}

check_pins() {
    local mods pin eco
    mods="$(latest_modules)"
    all_pins | while IFS= read -r pin; do
        eco="$(jq -r .ecosystem <<<"$pin")"
        case "$eco" in
        gomod)
            jq -c --argjson pin "$pin" --slurpfile m <(echo "$mods" | jq -c .) -n \
                '($m | map(select(.name==$pin.name)) | first // null) as $u
                 | {name:$pin.name, ecosystem:"gomod", old:$pin.version, new:($u.new // $pin.version)}' ;;
        docker)
            if [[ "$(jq -r '.unresolved_arg // false' <<<"$pin")" == "true" ]]; then continue; fi
            local img tag new
            img="$(jq -r .image <<<"$pin")"; tag="$(jq -r .tag <<<"$pin")"
            if ! new="$(latest_image_tag "$img" "$tag")"; then
                fail_note "registry tags API unavailable for ${img}; network or API may be down"
                continue
            fi
            jq -nc --arg n "$img" --arg o "$tag" --arg w "$new" '{name:$n,ecosystem:"docker",old:$o,new:$w}' ;;
        github-actions)
            local act ver rel new
            act="$(jq -r .action <<<"$pin")"; ver="$(jq -r .version <<<"$pin")"
            if ! rel="$(curl -fsS --max-time "$TIMEOUT" -H 'Accept: application/vnd.github+json' \
                "https://api.github.com/repos/$(cut -d/ -f1,2 <<<"$act")/releases/latest")"; then
                fail_note "GitHub releases API unavailable for ${act}; network, rate limit or API may be down"
                continue
            fi
            new="$(jq -r '.tag_name // empty' <<<"$rel")"
            jq -nc --arg n "$act" --arg o "$ver" --arg w "${new:-$ver}" '{name:$n,ecosystem:"github-actions",old:$o,new:$w}' ;;
        esac
    done
}

check_pins | jq -sc '.' | python3 scripts/dependency-update-classify.py
[[ -s "$ERRFILE" ]] && exit 1
exit 0

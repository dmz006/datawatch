#!/usr/bin/env bash
# scripts/check-plugin-manifests.sh — enforces the "Plugin-Manifest
# Validation" rule (AGENT.md, BL274 closure).
#
# Two rule sets run against every manifest.yaml found:
#
# 1. docs: references. Per BL274 Q9: a plugin manifest may declare a
#    `docs:` block. When present, `docs.files:` is REQUIRED (a list of
#    operator-readable doc paths relative to the plugin dir) and every
#    listed file must exist. `docs.howtos:` is optional metadata; if
#    present, every entry's `file` must also exist.
#
# 2. Manifest v3 shape (kind / connection / lifecycle). Mirrors
#    Manifest.Validate in internal/plugins/plugins.go:
#    - `kind:` absent (or empty) means automation — a v2.1 manifest,
#      unchanged. Present, it must be `automation` or `service`.
#    - `kind: service` requires a `connection:` block whose `base_url:`
#      is an http:// or https:// URL. When `connection.auth:` is set,
#      `auth.type:` (if given) must be `bearer` and `auth.token:` must be
#      exactly one ${secret:NAME} reference. Only the reference SHAPE is
#      checked — the secret is never looked up and the token is never
#      printed.
#    - `lifecycle:` is only allowed on service manifests. Its
#      `supported_modes:` (block or flow list) may contain only
#      `attach_only` / `managed`; `managed` requires `lifecycle.entry:`;
#      `restart_policy:` must be on-failure|always|never;
#      `stop_timeout_ms:` must be a non-negative integer.
#    - Unknown keys anywhere are ignored (forward compatible).
#
# Scan roots: examples/plugins/ and ~/.datawatch/plugins/ by default.
# Set PLUGIN_MANIFEST_ROOTS (colon-separated dirs) to scan exactly those
# dirs instead — used to run the check against test fixtures.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

ERRORS=0
report() {
    echo "  ✗ $1"
    ERRORS=$((ERRORS + 1))
}

declare -a ROOTS=()
if [[ -n "${PLUGIN_MANIFEST_ROOTS:-}" ]]; then
    IFS=':' read -r -a OVERRIDE_ROOTS <<< "$PLUGIN_MANIFEST_ROOTS"
    for r in "${OVERRIDE_ROOTS[@]}"; do
        [[ -n "$r" ]] || continue
        if [[ -d "$r" ]]; then
            ROOTS+=("$r")
        else
            report "PLUGIN_MANIFEST_ROOTS entry is not a directory: $r"
        fi
    done
else
    # Validate every manifest.yaml under examples/plugins/ + ~/.datawatch/plugins/.
    [[ -d examples/plugins ]] && ROOTS+=("examples/plugins")
    [[ -d "$HOME/.datawatch/plugins" ]] && ROOTS+=("$HOME/.datawatch/plugins")
fi

if [[ ${#ROOTS[@]} -eq 0 && $ERRORS -eq 0 ]]; then
    echo "==> Plugin manifest audit (no plugin roots found — skipping)"
    exit 0
fi

echo "==> Plugin manifest audit (roots: ${ROOTS[*]:-none})"

# check_docs MANIFEST — rule set 1 (docs: references).
check_docs() {
    local manifest="$1" plugin_dir f full
    plugin_dir="$(dirname "$manifest")"
    # Only act if the manifest declares docs: at top level.
    if ! grep -q '^docs:' "$manifest"; then
        return 0
    fi
    # Pull every `docs.files:` and `docs.howtos[].file:` value.
    local -a FILES
    mapfile -t FILES < <(awk '
        /^docs:/{indocs=1; next}
        indocs && /^[a-z]/ && !/^docs:/ {indocs=0}
        indocs && /^[[:space:]]+files:/{infiles=1; next}
        indocs && /^[[:space:]]+[a-z]/{infiles=0}
        infiles && /^[[:space:]]+- /{
            sub(/^[[:space:]]+-[[:space:]]*/, "")
            gsub(/"/, "")
            print
        }
        indocs && /^[[:space:]]+- file:/{
            sub(/^[[:space:]]+-[[:space:]]*file:[[:space:]]*/, "")
            gsub(/"/, "")
            print
        }
    ' "$manifest")
    if [[ ${#FILES[@]} -eq 0 ]]; then
        report "$manifest declares docs: but no files: list (Q9 requires files)"
        return 0
    fi
    for f in "${FILES[@]}"; do
        full="$plugin_dir/$f"
        if [[ ! -f "$full" ]]; then
            report "$manifest references non-existent doc: $f (looked in $full)"
        fi
    done
}

# yaml_flatten MANIFEST — prints one "dotted.path<TAB>value" line per
# mapping key and one "dotted.path[]<TAB>item" line per list item (block
# or flow style). Values are unquoted with inline comments stripped.
# Nesting follows indentation; block scalar (| / >) bodies are skipped.
yaml_flatten() {
    awk '
        function clean(v,   q, i) {
            sub(/^[ \t]+/, "", v)
            q = substr(v, 1, 1)
            if (q == "\"" || q == "\x27") {
                i = index(substr(v, 2), q)
                return (i > 0) ? substr(v, 2, i - 1) : substr(v, 2)
            }
            sub(/[ \t]+#.*$/, "", v)
            sub(/[ \t\r]+$/, "", v)
            return v
        }
        function path(n,   p, i) {
            p = keys[1]
            for (i = 2; i <= n; i++) p = p "." keys[i]
            return p
        }
        function emit_flow(p, v,   n, parts, i, item) {
            v = substr(v, 2)
            sub(/\][^]]*$/, "", v)
            n = split(v, parts, ",")
            for (i = 1; i <= n; i++) {
                item = clean(parts[i])
                if (item != "") print p "[]\t" item
            }
        }
        {
            sub(/\r$/, "")
            if ($0 ~ /^[ \t]*(#.*)?$/) next
            match($0, /^ */)
            ind = RLENGTH
            if (scalar_ind >= 0) {
                if (ind > scalar_ind) next
                scalar_ind = -1
            }
            line = substr($0, ind + 1)
            if (line ~ /^-([ \t]|$)/) {
                # List item: attach to the deepest key indented <= item.
                while (depth > 0 && inds[depth] > ind) depth--
                if (depth == 0) next
                item = clean(substr(line, 2))
                print path(depth) "[]\t" item
                next
            }
            if (match(line, /^[A-Za-z0-9_.-]+[ \t]*:([ \t]|$)/) == 0) next
            key = line
            sub(/[ \t]*:.*$/, "", key)
            raw = substr(line, RLENGTH + 1)
            while (depth > 0 && inds[depth] >= ind) depth--
            depth++
            keys[depth] = key
            inds[depth] = ind
            p = path(depth)
            sub(/^[ \t]+/, "", raw)
            if (raw ~ /^\[/) {
                print p "\t"
                emit_flow(p, raw)
                next
            }
            v = clean(raw)
            if (v ~ /^[|>]/) {
                scalar_ind = ind
                v = ""
            }
            print p "\t" v
        }
        BEGIN { depth = 0; scalar_ind = -1 }
    ' "$1"
}

# check_v3 MANIFEST — rule set 2 (kind / connection / lifecycle).
check_v3() {
    local manifest="$1" flat kind base_url auth_type token mode
    local restart stop_timeout entry
    flat="$(yaml_flatten "$manifest")"

    has()  { printf '%s\n' "$flat" | awk -F'\t' -v k="$1" '$1==k{f=1} END{exit !f}'; }
    val()  { printf '%s\n' "$flat" | awk -F'\t' -v k="$1" '$1==k{print $2; exit}'; }
    items() { printf '%s\n' "$flat" | awk -F'\t' -v k="$1[]" '$1==k{print $2}'; }

    kind="$(val kind | tr '[:upper:]' '[:lower:]')"
    [[ -n "$kind" ]] || kind="automation"
    case "$kind" in
        automation)
            if has lifecycle; then
                report "$manifest declares lifecycle: but kind is automation (lifecycle requires kind: service)"
            fi
            return 0
            ;;
        service) ;;
        *)
            report "$manifest has invalid kind: \"$kind\" (must be automation or service)"
            return 0
            ;;
    esac

    # kind: service — connection block.
    if ! has connection; then
        report "$manifest is kind: service but has no connection: block"
    else
        base_url="$(val connection.base_url)"
        if [[ -z "$base_url" ]]; then
            report "$manifest is kind: service but connection.base_url is missing or empty"
        elif ! [[ "$base_url" =~ ^https?://[^/[:space:]]+ ]]; then
            report "$manifest connection.base_url \"$base_url\" must be an absolute http:// or https:// URL"
        fi
        if has connection.auth; then
            auth_type="$(val connection.auth.type | tr '[:upper:]' '[:lower:]')"
            if [[ -n "$auth_type" && "$auth_type" != "bearer" ]]; then
                report "$manifest connection.auth.type \"$auth_type\" is unsupported (only bearer)"
            fi
            token="$(val connection.auth.token)"
            # Never echo the token — it may be a literal credential.
            if [[ -z "$token" ]]; then
                report "$manifest connection.auth is set but connection.auth.token is missing"
            elif ! [[ "$token" =~ ^\$\{secret:[A-Za-z0-9_.-]+\}$ ]]; then
                report "$manifest connection.auth.token must be a single \${secret:NAME} reference, not a literal credential"
            fi
        fi
    fi

    # kind: service — optional lifecycle block.
    has lifecycle || return 0
    local -a MODES
    mapfile -t MODES < <(items lifecycle.supported_modes)
    if [[ ${#MODES[@]} -eq 0 ]]; then
        report "$manifest lifecycle.supported_modes is missing or empty"
    fi
    local managed=0 seen=" "
    for mode in "${MODES[@]}"; do
        case "$mode" in
            attach_only|managed) ;;
            *)
                report "$manifest lifecycle.supported_modes has unknown mode \"$mode\" (must be attach_only or managed)"
                continue
                ;;
        esac
        if [[ "$seen" == *" $mode "* ]]; then
            report "$manifest lifecycle.supported_modes lists \"$mode\" more than once"
        fi
        seen+="$mode "
        [[ "$mode" == "managed" ]] && managed=1
    done
    entry="$(val lifecycle.entry)"
    if [[ $managed -eq 1 && -z "$entry" ]]; then
        report "$manifest lifecycle.supported_modes contains managed but lifecycle.entry is missing"
    fi
    restart="$(val lifecycle.restart_policy)"
    case "$restart" in
        ""|on-failure|always|never) ;;
        *) report "$manifest lifecycle.restart_policy \"$restart\" is invalid (must be on-failure, always or never)" ;;
    esac
    stop_timeout="$(val lifecycle.stop_timeout_ms)"
    if [[ -n "$stop_timeout" ]] && ! [[ "$stop_timeout" =~ ^[0-9]+$ ]]; then
        report "$manifest lifecycle.stop_timeout_ms \"$stop_timeout\" must be a non-negative integer"
    fi
}

while IFS= read -r manifest; do
    # Skip non-yaml manifests + nested vendor dirs.
    [[ -f "$manifest" ]] || continue
    check_docs "$manifest"
    check_v3 "$manifest"
done < <( ((${#ROOTS[@]})) && find "${ROOTS[@]}" -name 'manifest.yaml' 2>/dev/null)

if [[ $ERRORS -gt 0 ]]; then
    echo
    echo "✗ FAIL: $ERRORS plugin manifest issue(s)."
    echo "  Rule: AGENT.md §Plugin-Manifest Validation (BL274 Q9) — every"
    echo "  plugin manifest's docs:files: must list files that exist, and"
    echo "  every manifest must have a valid v3 shape: kind automation|service,"
    echo "  service manifests need connection.base_url (http/https) and a"
    echo "  \${secret:NAME} auth token, lifecycle only on service manifests"
    echo "  with valid supported_modes/entry/restart_policy."
    exit 1
fi

echo "✓ PASS: every plugin manifest's docs:files: + howtos[].file references existing files,"
echo "  and every manifest's kind/connection/lifecycle (v3) shape is valid."
exit 0

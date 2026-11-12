#!/usr/bin/env bash
# scripts/check-no-config-copies.sh — configuration-copy leak check over the
# changed files.
#
# Modelled on scripts/check-no-local-leaks.sh, but instead of scanning file
# contents across the whole tree it looks at path names of the files that
# changed and fails when any path looks like a copy of operator-local
# configuration or credentials.
#
# What is flagged (first matching class wins):
#   backup   any *.bak or *.orig backup copy (of a config, env, key or any
#            other file — backup artifacts never belong in the repo)
#   env      .env files: a base name of .env, .env.* or *.env
#   config   copies of datawatch.yaml|datawatch.yml|config.yaml|config.yml
#            (those exact base names, or the same with any suffix appended,
#            e.g. datawatch.yaml copy saved as datawatch.yaml.save)
#   dir      files under a copied .datawatch or home config directory, i.e.
#            any path with a nested .datawatch/ component (the repository's
#            own top-level .datawatch/ tooling directory is not matched) or
#            a .config/datawatch/ component
#   secret   secret or key files: id_rsa*, id_dsa*, id_ecdsa*, id_ed25519*,
#            *.pem, *.p12, *.pfx, *.key, *.jks, *.keystore, *.kdbx, *.gpg,
#            *.token, *.secret, secrets.json|secrets.yaml|secrets.yml,
#            credentials*
#
# Documented templates and testdata fixtures that legitimately look like a
# leak go in the ALLOW_LIST at the top of this script; allowlist entries are
# exact repository-relative paths or glob patterns.
#
# Usage:
#   scripts/check-no-config-copies.sh [base-ref]
#
#   base-ref  optional git ref; when given, the change set is the diff from
#             that ref to HEAD (base-ref..HEAD), plus all untracked
#             non-ignored files. When omitted, the change set is the diff
#             against HEAD (staged and working-tree changes), plus all
#             untracked non-ignored files.
#
# Changed files are read from `git diff --name-status` and
# `git ls-files --others --exclude-standard`; files only deleted in the
# change set are not flagged (removing a stray copy must not be blocked).
#
# Output: one line per finding, `path: class`, then a single findings-count
# line. When the change set is clean, a single OK line is printed.
# Exit codes: 0 clean, 1 findings reported, 2 usage or configuration error.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# ---------------------------------------------------------------------------
# Allow list: documented templates and testdata fixtures that look like a
# configuration copy but are meant to be in the repository. Exact
# repository-relative paths or glob patterns; first match passes the file.
# ---------------------------------------------------------------------------
ALLOW_LIST=(
    # testdata fixture used by the datawatch.yaml tests
    "testdata/datawatch.yaml"
    # documented build environment template
    ".env.build.example"
)

usage() {
    echo "usage: $(basename "$0") [base-ref]" >&2
}

die2() {
    echo "check-no-config-copies: $*" >&2
    exit 2
}

BASE_REF=""
if (( $# == 1 )); then
    case "$1" in
        -h | --help) usage; exit 2 ;;
    esac
    BASE_REF="$1"
elif (( $# > 1 )); then
    usage
    exit 2
fi

git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1 \
    || die2 "repository not found (run from inside a git work tree)"

if [[ -n "$BASE_REF" ]]; then
    git -C "$ROOT" rev-parse --verify --quiet "${BASE_REF}^{commit}" >/dev/null \
        || die2 "base ref '$BASE_REF' does not resolve to a commit"
    RANGE="$BASE_REF..HEAD"
else
    RANGE="HEAD"
fi

# ---------------------------------------------------------------------------
# Collect the changed file set (deduplicated), skipping deletions.
# ---------------------------------------------------------------------------
declare -A CHANGED=()

add_changed() { # path
    [[ -n "$1" ]] || return 0
    CHANGED["$1"]=1
}

# git diff --name-status emits "STATUS\tPATH" records (renames carry the new
# path as the last tab-separated field); deletions are skipped.
if mapfile -t DS < <(git -C "$ROOT" diff --name-status "$RANGE" 2>/dev/null); then
    for rec in "${DS[@]}"; do
        [[ -n "$rec" ]] || continue
        status="${rec%%$'\t'*}"
        path="${rec#*$'\t'}"
        case "$status" in
            A | M | T) add_changed "$path" ;;
            R*)  path="${rec##*$'\t'}"; add_changed "$path" ;;
            *)   ;; # D (and anything else): removing a stray copy is allowed
        esac
    done
fi

while IFS= read -r -d '' u; do
    add_changed "$u"
done < <(git -C "$ROOT" ls-files --others --exclude-standard -z)

FILES=()
if (( ${#CHANGED[@]} > 0 )); then
    while IFS= read -r -d '' p; do
        FILES+=("$p")
    done < <(printf '%s\0' "${!CHANGED[@]}" | LC_ALL=C sort -z)
fi

# ---------------------------------------------------------------------------
# Classification
# ---------------------------------------------------------------------------
is_allowed() { # path
    local pat
    for pat in "${ALLOW_LIST[@]}"; do
        # shellcheck disable=SC2053  # intentional glob match
        if [[ "$1" == $pat ]]; then
            return 0
        fi
    done
    return 1
}

classify() { # path -> prints one of backup|env|config|dir|secret,
            # or prints nothing when the path is not a configuration copy
    local p="$1"
    local base="${1##*/}"
    case "$p" in
        *.bak | *.orig) echo backup; return 0 ;;
    esac
    case "$base" in
        .env | .env.* | *.env) echo env; return 0 ;;
    esac
    case "$base" in
        datawatch.yaml | datawatch.yml | config.yaml | config.yml)
            echo config; return 0 ;;
        datawatch.yaml.* | datawatch.yml.* | config.yaml.* | config.yml.*)
            echo config; return 0 ;;
    esac
    case "$p" in
        */.datawatch/* | */.config/datawatch*) echo dir; return 0 ;;
    esac
    case "$base" in
        id_rsa* | id_dsa* | id_ecdsa* | id_ed25519*) echo secret; return 0 ;;
        *.pem | *.p12 | *.pfx | *.key | *.jks | *.keystore | *.kdbx | *.gpg)
            echo secret; return 0 ;;
        *.token | *.secret | secrets.json | secrets.yaml | secrets.yml)
            echo secret; return 0 ;;
        credentials | credentials.*) echo secret; return 0 ;;
    esac
    return 0
}

# ---------------------------------------------------------------------------
# Report
# ---------------------------------------------------------------------------
FINDINGS=()
for p in "${FILES[@]}"; do
    if is_allowed "$p"; then
        continue
    fi
    cls="$(classify "$p")"
    if [[ -n "$cls" ]]; then
        FINDINGS+=("$p: $cls")
    fi
done

if (( ${#FINDINGS[@]} > 0 )); then
    for line in "${FINDINGS[@]}"; do
        printf '%s\n' "$line"
    done
    printf 'config-copy findings: %d\n' "${#FINDINGS[@]}"
    exit 1
fi

printf 'OK: no configuration copies in the changed files (%d file(s) checked)\n' \
    "${#FILES[@]}"
exit 0

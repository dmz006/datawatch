#!/usr/bin/env bash
# F10 S1.8 — container-upgrade.
#
# Reads the current "latest" version of every pinned tool used in
# docker/dockerfiles/ and rewrites the ARG <NAME>=<VERSION> default
# in-place. By default this is a dry-run (prints what would change);
# pass --apply to actually rewrite the files.
#
# Usage:
#   scripts/container-upgrade.sh           # dry-run
#   scripts/container-upgrade.sh --apply   # rewrite files
#
# Sources used:
#   GitHub releases API for: claude-code, aider, rtk, signal-cli,
#       gradle, kotlin, go (mirror), rust toolchain, golangci-lint,
#       gopls, delve, bun
#   npm registry for: pnpm, typescript, tsx, eslint, opencode-ai,
#       @anthropic-ai/claude-code, @google/gemini-cli
#   astral.sh for: uv
#   PyPI for: poetry, ruff, pyright

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DOCKERFILES="$REPO_ROOT/docker/dockerfiles"
APPLY=0
[[ "${1:-}" == "--apply" ]] && APPLY=1

# ── Helpers ───────────────────────────────────────────────────────────
gh_latest() {
    # gh_latest <owner/repo> [strip_v]
    # Returns latest release tag. Strips leading "v" if second arg is "1".
    local repo="$1" strip="${2:-0}"
    local tag
    tag=$(curl -fsS --max-time 10 "https://api.github.com/repos/$repo/releases/latest" \
          | python3 -c 'import json,sys; print(json.load(sys.stdin).get("tag_name",""))')
    if [[ "$strip" == "1" ]]; then
        tag="${tag#v}"
    fi
    echo "$tag"
}

npm_latest() {
    # npm_latest <package>
    curl -fsS --max-time 10 "https://registry.npmjs.org/$1/latest" \
        | python3 -c 'import json,sys; print(json.load(sys.stdin).get("version",""))'
}

pypi_latest() {
    curl -fsS --max-time 10 "https://pypi.org/pypi/$1/json" \
        | python3 -c 'import json,sys; print(json.load(sys.stdin).get("info",{}).get("version",""))'
}

# Resolve every version we care about. Failures (network, rate-limit) leave
# the variable empty and that bump is skipped.
echo "→ resolving upstream versions…"
NEW_CLAUDE_CODE=$(npm_latest "@anthropic-ai/claude-code" || true)
NEW_OPENCODE=$(npm_latest "opencode-ai" || true)
NEW_GEMINI_CLI=$(npm_latest "@google/gemini-cli" || true)
NEW_AIDER=$(pypi_latest "aider-chat" || true)
NEW_RTK=$(gh_latest "rtk-ai/rtk" 1 || true)
NEW_SIGNAL_CLI=$(gh_latest "AsamK/signal-cli" 1 || true)
NEW_PNPM=$(npm_latest "pnpm" || true)
NEW_TYPESCRIPT=$(npm_latest "typescript" || true)
NEW_TSX=$(npm_latest "tsx" || true)
NEW_ESLINT=$(npm_latest "eslint" || true)
NEW_BUN=$(gh_latest "oven-sh/bun" 1 | sed 's/^bun-v//' || true)
NEW_UV=$(gh_latest "astral-sh/uv" 1 || true)
NEW_POETRY=$(pypi_latest "poetry" || true)
NEW_RUFF=$(pypi_latest "ruff" || true)
NEW_PYRIGHT=$(pypi_latest "pyright" || true)
NEW_GOPLS=$(gh_latest "golang/tools" 0 | sed 's@^gopls/@@' || true)  # tags are gopls/vX
NEW_GOLANGCI_LINT=$(gh_latest "golangci/golangci-lint" 0 || true)
NEW_DELVE=$(gh_latest "go-delve/delve" 0 || true)
NEW_KOTLIN=$(gh_latest "JetBrains/kotlin" 1 || true)
NEW_GRADLE=$(gh_latest "gradle/gradle" 1 | sed 's/^v//' || true)

# Map: ARG_NAME → resolved-version. Empty value = skip.
declare -A BUMPS=(
    [CLAUDE_CODE_VERSION]="$NEW_CLAUDE_CODE"
    [OPENCODE_VERSION]="$NEW_OPENCODE"
    [GEMINI_CLI_VERSION]="$NEW_GEMINI_CLI"
    [AIDER_VERSION]="$NEW_AIDER"
    [RTK_VERSION]="$NEW_RTK"
    [SIGNAL_CLI_VERSION]="$NEW_SIGNAL_CLI"
    [PNPM_VERSION]="$NEW_PNPM"
    [TYPESCRIPT_VERSION]="$NEW_TYPESCRIPT"
    [TSX_VERSION]="$NEW_TSX"
    [ESLINT_VERSION]="$NEW_ESLINT"
    [BUN_VERSION]="$NEW_BUN"
    [UV_VERSION]="$NEW_UV"
    [POETRY_VERSION]="$NEW_POETRY"
    [RUFF_VERSION]="$NEW_RUFF"
    [PYRIGHT_VERSION]="$NEW_PYRIGHT"
    [GOPLS_VERSION]="$NEW_GOPLS"
    [GOLANGCI_LINT_VERSION]="$NEW_GOLANGCI_LINT"
    [DELVE_VERSION]="$NEW_DELVE"
    [KOTLIN_VERSION]="$NEW_KOTLIN"
    [GRADLE_VERSION]="$NEW_GRADLE"
)

CHANGED=0
for arg in "${!BUMPS[@]}"; do
    new="${BUMPS[$arg]}"
    if [[ -z "$new" ]]; then
        printf "  %-26s [skipped — no value resolved]\n" "$arg"
        continue
    fi
    # Find current value(s) of ARG <name>=<version> across all dockerfiles
    while IFS= read -r file; do
        cur=$(grep -E "^ARG[[:space:]]+${arg}=" "$file" | head -1 | sed -E "s/^ARG[[:space:]]+${arg}=//")
        if [[ -z "$cur" ]]; then continue; fi
        if [[ "$cur" == "$new" ]]; then
            printf "  %-26s %-12s = %s\n" "$arg" "$cur" "(no change)"
        else
            printf "  %-26s %-12s → %s   %s\n" "$arg" "$cur" "$new" "$(basename "$file")"
            CHANGED=$((CHANGED+1))
            if [[ "$APPLY" == "1" ]]; then
                # In-place rewrite, only the ARG default at top of file
                sed -i -E "s/^ARG[[:space:]]+${arg}=.*/ARG ${arg}=${new}/" "$file"
            fi
        fi
    done < <(grep -lE "^ARG[[:space:]]+${arg}=" "$DOCKERFILES"/Dockerfile.* 2>/dev/null || true)
done

echo
if [[ "$APPLY" == "1" ]]; then
    echo "→ applied $CHANGED change(s); review with 'git diff' then commit."
    echo "  Commit suggestion: 'chore(F10): bump pinned container tool versions'"
else
    if [[ "$CHANGED" -gt 0 ]]; then
        echo "→ $CHANGED change(s) available. Re-run with --apply to write them."
    else
        echo "→ all pinned versions are current."
    fi
fi

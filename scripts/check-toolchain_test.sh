#!/usr/bin/env bash
# scripts/check-toolchain_test.sh — shell tests for scripts/check-toolchain.sh.
#
# Runs the script only through its fixture hook (TOOLCHAIN_FIXTURE_DIR), so the network is never
# touched. Everything the test creates lives under one mktemp directory that a trap removes on
# EXIT:
#   fakebin/   fake tool scripts that print a chosen version (the only thing on PATH besides
#              symlinks to the handful of system utilities the script needs — no curl, no wget,
#              and none of the operator's real tools, so "tool missing" is deterministic)
#   fixture/   the "newest version" files the fixture hook reads (go.json + one file per tool)
#   tmp/       TMPDIR for the script under test; it must be empty again after every run
# The real toolchain.yaml is used as the tool list; only its go target_file is rewritten to an
# absolute path of a fixture go.mod so the Go line (1.26) never depends on the repo's go.mod.
# No formatter is run, nothing is written to the repository, no sleeps, no load.
#
# Scenarios (each asserts the printed status row and the exit code):
#   a  Go one patch behind the newest stable of its go.mod line   -> GO-BEHIND, exit 1
#   b  installed equals newest for every tool                     -> all CURRENT, exit 0
#   c  fixture files absent / unreadable                          -> UNREACHABLE + notes, exit 0
#   d  one tool absent from PATH                                  -> MISSING, exit 0
#   e  a non-Go tool a minor (or major) behind                    -> BEHIND, exit 1
#   f  a non-Go tool only a patch behind                          -> PATCH-BEHIND, exit 0
#
# Run independently: bash scripts/check-toolchain_test.sh
# Exit codes: 0 all checks passed, 1 at least one check failed.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
S="$REPO_ROOT/scripts/check-toolchain.sh"
REAL_YAML="$REPO_ROOT/toolchain.yaml"
BASH_BIN="$(command -v bash)"

fails=0
checks=0
ok()  { checks=$((checks + 1)); echo "PASS: $1"; }
bad() { checks=$((checks + 1)); echo "FAIL: $1" >&2; fails=$((fails + 1)); }

[[ -f "$S" ]] || { echo "script under test is missing: $S" >&2; exit 1; }
[[ -f "$REAL_YAML" ]] || { echo "tool list is missing: $REAL_YAML" >&2; exit 1; }
bash -n "$S" || { echo "syntax error in $S" >&2; exit 1; }

T="$(mktemp -d)" || { echo "cannot create a temp dir" >&2; exit 1; }
trap 'rm -rf "$T"' EXIT
FAKEBIN="$T/fakebin"; FIX="$T/fixture"; SYSBIN="$T/sysbin"; RUNTMP="$T/tmp"
mkdir -p "$SYSBIN" "$RUNTMP" "$T/home"

# System utilities the script under test needs. Anything else (curl, wget, real go, trivy, ...)
# is deliberately not reachable from the script's PATH.
for u in sed awk sort head tail tr cat mktemp rm timeout dirname; do
    p="$(command -v "$u" 2>/dev/null || true)"
    [[ -n "$p" && -x "$p" ]] && ln -s "$p" "$SYSBIN/$u"
done
for u in sed awk sort head tail tr cat mktemp rm dirname; do
    [[ -e "$SYSBIN/$u" ]] || { echo "required system utility not found: $u" >&2; exit 1; }
done
for u in curl wget; do
    [[ -e "$SYSBIN/$u" ]] && { echo "internal error: $u reachable from the test PATH" >&2; exit 1; }
done

# The Go target line comes from this fixture go.mod (line 1.26), never from the repo's go.mod.
printf 'module example.test/fixture\n\ngo 1.26.11\n' > "$T/go.mod"
TF="$T/toolchain.yaml"
sed -E "s|^([[:space:]]*target_file:).*|\1 $T/go.mod|" "$REAL_YAML" > "$TF"
grep -qF "target_file: $T/go.mod" "$TF" || { echo "could not point toolchain.yaml at the fixture go.mod" >&2; exit 1; }

mapfile -t TOOLS < <(sed -nE 's/^-[[:space:]]+name:[[:space:]]*(.*)$/\1/p' "$REAL_YAML")
NTOOLS=${#TOOLS[@]}
[[ $NTOOLS -gt 0 ]] || { echo "no tools found in $REAL_YAML" >&2; exit 1; }

# Baseline versions: installed == newest for every tool.
declare -A BASE=(
    [go]=1.26.11 [gosec]=2.22.1 [govulncheck]=1.1.4 [gitleaks]=8.24.2
    [golangci-lint]=2.1.6 [trivy]=0.62.1 [goreleaser]=2.9.0
)

# What each tool's version command prints, in its real format.
fake_text() { # tool version
    case "$1" in
        go)            printf 'go version go%s linux/amd64\n' "$2" ;;
        gosec)         printf 'Version: %s\nGit tag: v%s\nBuild date: 2026-01-01T00:00:00Z\n' "$2" "$2" ;;
        govulncheck)   printf 'Go: go1.26.11\nScanner: govulncheck@v%s\nDB: https://vuln.go.dev\nDB updated: 2026-01-01 00:00:00 +0000 UTC\n\nNo vulnerabilities found.\n' "$2" ;;
        gitleaks)      printf '%s\n' "$2" ;;
        golangci-lint) printf 'golangci-lint has version %s built with go1.26.11 from abc1234 on 2026-01-01T00:00:00Z\n' "$2" ;;
        trivy)         printf 'Version: %s\nVulnerability DB:\n  Version: 2\n' "$2" ;;
        goreleaser)    printf 'GitVersion:    %s\nGitCommit:     abc1234\n' "$2" ;;
        *)             return 1 ;;
    esac
}

# install_fake_raw tool text -> an executable on the fake PATH that prints text (bash builtins only).
install_fake_raw() {
    {
        printf '#!%s\n' "$BASH_BIN"
        printf 'printf %%s %q\n' "$2"
    } > "$FAKEBIN/$1"
    chmod +x "$FAKEBIN/$1"
}
inst() { # tool version -> installed version
    local text
    text="$(fake_text "$1" "$2")" || { bad "no fake output format for tool '$1' — extend fake_text in $0"; return 1; }
    install_fake_raw "$1" "$text"$'\n'
}

go_json() { # newest stable patch of the 1.26 line -> go.dev/dl-shaped feed with decoys
    local n="$1"
    files() { printf '"files":[{"filename":"go%s.linux-amd64.tar.gz","os":"linux","arch":"amd64","version":"go%s","sha256":"00","size":1,"kind":"archive"}]' "$1" "$1"; }
    {
        printf '[\n'
        # another line, stable: must be ignored
        printf '{"version":"go1.27.1","stable":true,%s},\n' "$(files 1.27.1)"
        # same line but not stable: must be ignored
        printf '{"version":"go1.26.99","stable":false,%s},\n' "$(files 1.26.99)"
        printf '{"version":"go%s","stable":true,%s},\n' "$n" "$(files "$n")"
        printf '{"version":"go1.26.9","stable":true,%s},\n' "$(files 1.26.9)"
        printf '{"version":"go1.25.9","stable":true,%s}\n' "$(files 1.25.9)"
        printf ']\n'
    }
}

newest() { # tool version -> newest version in that tool's source fixture
    case "$1" in
        go)          go_json "$2" > "$FIX/go.json" ;;
        govulncheck) printf '{"Version":"v%s","Time":"2026-01-01T00:00:00Z"}\n' "$2" > "$FIX/govulncheck" ;;
        *)           printf '{"url":"https://api.github.com/example","tag_name":"v%s","name":"v%s"}\n' "$2" "$2" > "$FIX/$1" ;;
    esac
}

drop_installed() { rm -f "$FAKEBIN/$1"; }
drop_newest() { if [[ "$1" == go ]]; then rm -f "$FIX/go.json"; else rm -f "$FIX/$1"; fi; }

# begin_case: fresh fake bin + fixture dir with installed == newest for every tool.
begin_case() {
    rm -rf "$FAKEBIN" "$FIX"; mkdir -p "$FAKEBIN" "$FIX"
    local t
    for t in "${TOOLS[@]}"; do
        if [[ -z "${BASE[$t]:-}" ]]; then bad "no baseline version for tool '$t' — extend BASE in $0"; continue; fi
        inst "$t" "${BASE[$t]}"
        newest "$t" "${BASE[$t]}"
    done
}

OUT=""; ERR=""; RC=0
run() {
    env -i PATH="$FAKEBIN:$SYSBIN" LC_ALL=C TMPDIR="$RUNTMP" HOME="$T/home" \
        TOOLCHAIN_FILE="$TF" TOOLCHAIN_FIXTURE_DIR="$FIX" \
        "$BASH_BIN" "$S" > "$T/out" 2> "$T/err"
    RC=$?
    OUT="$(<"$T/out")"; ERR="$(<"$T/err")"
}

# ---- assertions ----
row() { awk -v t="$1" 'NR > 1 && $1 == t { print $2 " " $3 " " $4; exit }' <<<"$OUT"; }
expect_row() { # label tool installed newest status
    local got want="$3 $4 $5"
    got="$(row "$2")"
    if [[ "$got" == "$want" ]]; then ok "$1: $2 row is '$want'"
    else bad "$1: $2 row want '$want' got '${got:-<none>}'"; fi
}
expect_rc() { # label want
    if [[ "$RC" -eq "$2" ]]; then ok "$1: exit code $2"
    else bad "$1: exit code want $2 got $RC (stdout: $OUT; stderr: $ERR)"; fi
}
count_status() { awk -v s="$1" 'NR > 1 && $4 == s { c++ } END { print c + 0 }' <<<"$OUT"; }
count_rows() { awk 'NR > 1 && NF > 0 { c++ } END { print c + 0 }' <<<"$OUT"; }
expect_status_count() { # label status want
    local got; got="$(count_status "$2")"
    if [[ "$got" -eq "$3" ]]; then ok "$1: $3 row(s) with status $2"
    else bad "$1: want $3 row(s) with status $2, got $got (stdout: $OUT)"; fi
}
expect_rows_total() { # label want
    local got; got="$(count_rows)"
    if [[ "$got" -eq "$2" ]]; then ok "$1: table has $2 row(s)"
    else bad "$1: table want $2 row(s), got $got"; fi
}
expect_stderr_has() { # label text
    if grep -qF -- "$2" <<<"$ERR"; then ok "$1: stderr mentions '$2'"
    else bad "$1: stderr lacks '$2' (stderr: $ERR)"; fi
}
expect_stderr_empty() { # label
    if [[ -z "$ERR" ]]; then ok "$1: stderr is empty"
    else bad "$1: unexpected stderr: $ERR"; fi
}
expect_header() { # label
    if [[ "$(head -1 <<<"$OUT" | awk '{ print $1 " " $2 " " $3 " " $4 }')" == "TOOL INSTALLED NEWEST STATUS" ]]; then ok "$1: table header printed"
    else bad "$1: missing table header (stdout: $OUT)"; fi
}
expect_clean_tmp() { # label — the script removes its own mktemp dir
    if [[ -z "$(ls -A "$RUNTMP")" ]]; then ok "$1: script left nothing in its TMPDIR"
    else bad "$1: script left files in its TMPDIR: $(ls -A "$RUNTMP")"; rm -rf "${RUNTMP:?}"/* "${RUNTMP:?}"/.[!.]* 2>/dev/null; fi
}

before_git=""
if command -v git >/dev/null 2>&1 && git -C "$REPO_ROOT" rev-parse --git-dir >/dev/null 2>&1; then
    before_git="$(git -C "$REPO_ROOT" status --porcelain)"
fi

echo "== tool list: ${TOOLS[*]} (Go line from fixture go.mod: 1.26) =="

# ---- (a) Go one patch behind the newest stable of its go.mod line ----
begin_case
inst go 1.26.11
newest go 1.26.12
run
L="a go-behind"
expect_header "$L"
expect_row "$L" go 1.26.11 1.26.12 GO-BEHIND
expect_status_count "$L" GO-BEHIND 1
expect_status_count "$L" CURRENT $((NTOOLS - 1))
expect_rc "$L" 1
expect_clean_tmp "$L"

# the newest stable of the *line* wins: not the newer 1.27.x, not the unstable 1.26.99
L="a go-behind (decoys ignored)"
expect_row "$L" go 1.26.11 1.26.12 GO-BEHIND

# ---- (b) installed equals newest for every tool ----
begin_case
run
L="b all-current"
expect_header "$L"
for t in "${TOOLS[@]}"; do expect_row "$L" "$t" "${BASE[$t]}" "${BASE[$t]}" CURRENT; done
expect_status_count "$L" CURRENT "$NTOOLS"
expect_rows_total "$L" "$NTOOLS"
expect_rc "$L" 0
expect_stderr_empty "$L"
expect_clean_tmp "$L"

# installed newer than the newest listed is still CURRENT
begin_case
inst gosec 2.23.0
run
L="b installed-ahead"
expect_row "$L" gosec 2.23.0 2.22.1 CURRENT
expect_rc "$L" 0

# a Go release without a patch component (go1.26) is read as 1.26.0
begin_case
printf 'module example.test/fixture\n\ngo 1.26\n' > "$T/go.mod"
inst go 1.26
printf '[{"version":"go1.26","stable":true,"files":[{"version":"go1.26"}]},\n{"version":"go1.25.9","stable":true}]\n' > "$FIX/go.json"
run
L="b go-minor-only"
expect_row "$L" go 1.26.0 1.26.0 CURRENT
expect_rc "$L" 0
printf 'module example.test/fixture\n\ngo 1.26.11\n' > "$T/go.mod"

# a bare version string is accepted as a fixture for a github-kind tool
begin_case
printf 'v%s\n' "${BASE[gosec]}" > "$FIX/gosec"
run
L="b plain-string-fixture"
expect_row "$L" gosec "${BASE[gosec]}" "${BASE[gosec]}" CURRENT
expect_rc "$L" 0

# ---- (c) unreachable: fixture files absent ----
begin_case
rm -f "$FIX"/*
run
L="c unreachable (no fixtures)"
expect_header "$L"
for t in "${TOOLS[@]}"; do expect_row "$L" "$t" "${BASE[$t]}" - UNREACHABLE; done
expect_status_count "$L" UNREACHABLE "$NTOOLS"
expect_rc "$L" 0
expect_stderr_has "$L" "could not read or parse the newest version"
expect_stderr_has "$L" "fixture $FIX/go.json"
expect_stderr_has "$L" "fixture $FIX/gosec"
notes="$(grep -c '^note: ' <<<"$ERR")"
[[ "$notes" -eq "$NTOOLS" ]] && ok "$L: one note per tool ($NTOOLS)" || bad "$L: want $NTOOLS notes, got $notes"
expect_clean_tmp "$L"

# only one source absent: that tool is UNREACHABLE, the rest stay CURRENT, still exit 0
begin_case
drop_newest gosec
run
L="c one-source-absent"
expect_row "$L" gosec "${BASE[gosec]}" - UNREACHABLE
expect_status_count "$L" CURRENT $((NTOOLS - 1))
expect_rc "$L" 0
expect_stderr_has "$L" "gosec: could not read or parse the newest version"

# the Go feed absent: Go is UNREACHABLE (never GO-BEHIND), exit 0
begin_case
drop_newest go
run
L="c go-feed-absent"
expect_row "$L" go "${BASE[go]}" - UNREACHABLE
expect_rc "$L" 0

# a fixture that holds no version at all is unparsable, therefore UNREACHABLE
begin_case
printf 'rate limit exceeded\n' > "$FIX/govulncheck"
printf '<html>not json</html>\n' > "$FIX/go.json"
run
L="c unparsable-fixture"
expect_row "$L" govulncheck "${BASE[govulncheck]}" - UNREACHABLE
expect_row "$L" go "${BASE[go]}" - UNREACHABLE
expect_rc "$L" 0

# a Go feed with no stable release of the target line is UNREACHABLE, not a crash
begin_case
printf '[{"version":"go1.27.1","stable":true},{"version":"go1.25.9","stable":true}]\n' > "$FIX/go.json"
run
L="c go-line-absent-from-feed"
expect_row "$L" go "${BASE[go]}" - UNREACHABLE
expect_rc "$L" 0

# unreachable never masks a real BEHIND elsewhere
begin_case
drop_newest gosec
inst trivy 0.61.0
run
L="c unreachable-plus-behind"
expect_row "$L" gosec "${BASE[gosec]}" - UNREACHABLE
expect_row "$L" trivy 0.61.0 "${BASE[trivy]}" BEHIND
expect_rc "$L" 1

# an installed version that cannot be parsed is UNREACHABLE too (note names the version command)
begin_case
install_fake_raw gitleaks $'no version here\n'
run
L="c installed-unparsable"
expect_row "$L" gitleaks unknown - UNREACHABLE
expect_stderr_has "$L" "gitleaks: could not parse the installed version"
expect_rc "$L" 0

# ---- (d) tool missing from PATH ----
begin_case
drop_installed gosec
run
L="d missing"
expect_row "$L" gosec - - MISSING
expect_status_count "$L" MISSING 1
expect_status_count "$L" CURRENT $((NTOOLS - 1))
expect_rows_total "$L" "$NTOOLS"
expect_rc "$L" 0
expect_clean_tmp "$L"

# several missing tools, nothing behind: still exit 0
begin_case
drop_installed gosec; drop_installed trivy; drop_installed goreleaser
run
L="d several-missing"
expect_status_count "$L" MISSING 3
expect_rc "$L" 0

# missing does not hide a BEHIND tool
begin_case
drop_installed gosec
inst govulncheck 1.0.9
run
L="d missing-plus-behind"
expect_row "$L" gosec - - MISSING
expect_row "$L" govulncheck 1.0.9 "${BASE[govulncheck]}" BEHIND
expect_rc "$L" 1

# nothing installed at all (the real toolchain.yaml tool list): every tool MISSING, exit 0
begin_case
rm -f "$FAKEBIN"/*
run
L="d all-missing"
expect_status_count "$L" MISSING "$NTOOLS"
expect_rows_total "$L" "$NTOOLS"
expect_rc "$L" 0

# ---- (e) a non-Go tool one minor (or one major) behind ----
begin_case
inst gosec 2.21.4
run
L="e minor-behind"
expect_row "$L" gosec 2.21.4 2.22.1 BEHIND
expect_status_count "$L" BEHIND 1
expect_status_count "$L" GO-BEHIND 0
expect_rc "$L" 1
expect_clean_tmp "$L"

begin_case
inst trivy 0.61.9
run
L="e minor-behind (patch higher than newest)"
expect_row "$L" trivy 0.61.9 "${BASE[trivy]}" BEHIND
expect_rc "$L" 1

begin_case
newest trivy 1.0.0
run
L="e major-behind"
expect_row "$L" trivy "${BASE[trivy]}" 1.0.0 BEHIND
expect_rc "$L" 1

# the goproxy-kind and the plain-output tools take the same path
begin_case
inst govulncheck 1.0.4
inst gitleaks 8.23.0
run
L="e other-kinds-behind"
expect_row "$L" govulncheck 1.0.4 "${BASE[govulncheck]}" BEHIND
expect_row "$L" gitleaks 8.23.0 "${BASE[gitleaks]}" BEHIND
expect_status_count "$L" BEHIND 2
expect_rc "$L" 1

# ---- (f) a non-Go tool only a patch behind ----
begin_case
inst gosec 2.22.0
run
L="f patch-behind"
expect_header "$L"
expect_row "$L" gosec 2.22.0 2.22.1 PATCH-BEHIND
expect_status_count "$L" PATCH-BEHIND 1
expect_status_count "$L" BEHIND 0
expect_status_count "$L" CURRENT $((NTOOLS - 1))
expect_rc "$L" 0
expect_stderr_empty "$L"
expect_clean_tmp "$L"

# several tools a patch behind: informational only, exit 0 (Go itself stays CURRENT)
begin_case
inst gosec 2.22.0
inst golangci-lint 2.1.0
newest goreleaser 2.9.3
run
L="f several-patch-behind"
expect_status_count "$L" PATCH-BEHIND 3
expect_row "$L" go "${BASE[go]}" "${BASE[go]}" CURRENT
expect_rc "$L" 0

# ---- the repository was never written ----
if [[ -n "${before_git+x}" ]] && command -v git >/dev/null 2>&1 && git -C "$REPO_ROOT" rev-parse --git-dir >/dev/null 2>&1; then
    after_git="$(git -C "$REPO_ROOT" status --porcelain)"
    [[ "$after_git" == "$before_git" ]] && ok "read-only: git status unchanged" || bad "git status changed during the run"
fi

echo "$checks checks, $fails failed"
[[ $fails -eq 0 ]] && echo "all passed" || { echo "$fails failed" >&2; exit 1; }

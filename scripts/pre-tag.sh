#!/usr/bin/env bash
# pre-tag.sh — run every pre-tag gate locally, in CI order, stop at the first failure.
#
# Usage:  scripts/pre-tag.sh          (or: make pre-tag)
#   PRE_TAG_DRY_RUN=1      print every step id in order, run nothing, write no record
#   PRE_TAG_RECORD_FILE=p  record file (default .datawatch/pre-tag-record)
#
# Each step prints "[step-id] ..." and its elapsed seconds. A step whose tool is
# missing prints "SKIP [step-id]: <tool> not found" and does not fail.
# On finish (pass or fail) one line is appended to the record file:
#   <sha> <iso-date> steps=<n> result=<pass|fail> seconds=<total>
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 2

RECORD="${PRE_TAG_RECORD_FILE:-.datawatch/pre-tag-record}"

STEP_IDS=(fmt-check govet-check golangci-lint build mod-tidy go-test pwa-tests docs-sync
  howto-coverage internal-refs local-leaks version-bump tmux-kill dockerfile-go-version
  toolchain-pin trivy-single certifi accepted-risks trivyignore-regen govulncheck gosec
  gitleaks goreleaser-snapshot slow-opt-in docker-agent-base)

if [ "${PRE_TAG_DRY_RUN:-}" = "1" ]; then
  for id in "${STEP_IDS[@]}"; do echo "[$id] dry-run"; done
  exit 0
fi

# ---- step bodies (each returns 0 on success) ---------------------------------
s_fmt_check() {
  local out
  out="$(git ls-files '*.go' | xargs -r gofmt -l)" || return 1
  [ -z "$out" ] || { echo "gofmt would change:"; echo "$out"; return 1; }
}
s_govet_check() { go vet ./...; }
s_golangci_lint() { golangci-lint run; }
s_build() { go build ./...; }
s_mod_tidy() { go mod tidy && git diff --exit-code go.mod go.sum; }
s_go_test() { env -u TMUX -u TMUX_TMPDIR go test ./...; }
s_pwa_tests() { node --test internal/server/web/*.test.js; }
s_docs_sync() {
  make sync-docs || return 1
  [ -z "$(git status --porcelain -- internal/server/web/docs)" ] || {
    echo "docs embed out of sync"; git status --porcelain -- internal/server/web/docs; return 1; }
}
s_howto_coverage() { bash scripts/check-howto-coverage.sh; }
s_internal_refs() { bash scripts/check-no-internal-refs.sh; }
s_local_leaks() { bash scripts/check-no-local-leaks.sh .; }
s_version_bump() { bash scripts/check-version-bump.sh; }
s_tmux_kill() { bash scripts/check-no-unscoped-tmux.sh; }
s_dockerfile_go_version() { bash scripts/check-dockerfile-go-versions.sh; }
s_toolchain_pin() {
  bash scripts/check-go-toolchain-pins.sh && bash scripts/check-go-toolchain-pins.sh --self-test
}
s_trivy_single() {
  bash scripts/check-trivy-version-single.sh && bash scripts/tests/check-trivy-version-single_test.sh
}
s_certifi() { bash scripts/check-dockerfile-certifi.sh && bash scripts/check-dockerfile-certifi.sh --self-test; }
s_accepted_risks() { python3 scripts/check_accepted_risks.py; }
s_trivyignore_regen() {
  python3 scripts/gen_trivyignore.py || return 1
  git diff --exit-code .trivyignore docs/security-review.md
}
# Stubbed to the skip path in this task; the next task completes them.
s_govulncheck() { return 99; }
s_gosec() { return 99; }
s_gitleaks() { return 99; }
s_goreleaser_snapshot() { return 99; }
s_slow_opt_in() {
  local rc=0 t
  for t in test-tmp-guard test-cross-build perf-rollup-check; do
    if grep -qE "^${t}:" Makefile 2>/dev/null; then
      echo "[slow-opt-in] make $t"; make "$t" || rc=1
    fi
  done
  for t in scripts/check-no-busy-loop.sh scripts/check-busy-loop-pattern.sh; do
    [ -f "$t" ] && { echo "[slow-opt-in] $t"; bash "$t" || rc=1; }
  done
  return $rc
}
s_docker_agent_base() { return 99; }

# step id -> required tool (empty: none beyond bash)
tool_for() {
  case "$1" in
    fmt-check) echo gofmt ;;
    govet-check|build|mod-tidy|go-test) echo go ;;
    golangci-lint) echo golangci-lint ;;
    pwa-tests) echo node ;;
    docs-sync) echo make ;;
    accepted-risks|trivyignore-regen) echo python3 ;;
    govulncheck) echo govulncheck ;;
    gosec) echo gosec ;;
    gitleaks) echo gitleaks ;;
    goreleaser-snapshot) echo goreleaser ;;
    docker-agent-base) echo docker ;;
    *) echo "" ;;
  esac
}

START=$SECONDS
RUN=0
RESULT=pass

write_record() {
  local sha total
  sha="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
  total=$((SECONDS - START))
  mkdir -p "$(dirname "$RECORD")"
  printf '%s %s steps=%d result=%s seconds=%d\n' \
    "$sha" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$RUN" "$RESULT" "$total" >> "$RECORD"
}
trap write_record EXIT

for id in "${STEP_IDS[@]}"; do
  tool="$(tool_for "$id")"
  if [ -n "$tool" ] && ! command -v "$tool" >/dev/null 2>&1; then
    echo "SKIP [$id]: $tool not found"
    continue
  fi
  fn="s_${id//-/_}"
  echo "[$id] running"
  t0=$SECONDS
  "$fn"
  rc=$?
  el=$((SECONDS - t0))
  if [ "$rc" -eq 99 ]; then
    echo "SKIP [$id]: not implemented yet (${el}s)"
    continue
  fi
  RUN=$((RUN + 1))
  if [ "$rc" -ne 0 ]; then
    echo "[$id] FAIL (${el}s)"
    RESULT=fail
    exit 1
  fi
  echo "[$id] ok (${el}s)"
done
echo "pre-tag: all steps passed"

#!/usr/bin/env bash
# pre-tag.sh — run every pre-tag gate locally, in CI order, stop at the first failure.
#
# Usage:  scripts/pre-tag.sh          (or: make pre-tag)
#   PRE_TAG_DRY_RUN=1      print every step id in order, run nothing, write no record
#   PRE_TAG_RECORD_FILE=p  record file (default .datawatch/pre-tag-record)
#   PRE_TAG_ONLY=a,b       run only the listed step ids (others are not run, not skipped)
#   PRE_TAG_SLOW_TIMEOUT=s hard timeout in seconds for each slow-opt-in command (default 21600)
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
s_accepted_risks() {
  # CI passes --base <PR base sha>; locally the base is the last tag (none: no --base).
  local tag
  if tag="$(git describe --tags --abbrev=0 2>/dev/null)" && [ -n "$tag" ]; then
    python3 scripts/check_accepted_risks.py --base "$tag"
  else
    python3 scripts/check_accepted_risks.py
  fi
}
s_trivyignore_regen() {
  python3 scripts/gen_trivyignore.py || return 1
  git diff --exit-code .trivyignore docs/security-review.md
}
# Security / release-build steps mirror the CI invocations exactly:
#   govulncheck, gosec (+ .gosec-exclude, .gosec-baseline.json): .github/workflows/security-scan.yaml
#   gitleaks: .github/workflows/secret-scan.yaml
#   goreleaser snapshot: Makefile release-snapshot (release.yaml runs the real release)
s_govulncheck() { govulncheck ./...; }
s_gosec() {
  local out rc
  out="$(mktemp)" || return 1
  local exclude
  exclude="$(grep -v '^#' .gosec-exclude | tr '\n' ',' | sed 's/,$//')"
  gosec -exclude="$exclude" -exclude-dir=.claude -severity=high -confidence=medium \
    -fmt=json -quiet ./... > "$out"
  rc=$?
  if [ ! -s "$out" ] || ! python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$out" 2>/dev/null; then
    echo "gosec itself failed to produce valid JSON output (exit code $rc)"
    cat "$out"; rm -f "$out"; return 1
  fi
  python3 - "$out" <<'PY'
import json, sys
from collections import Counter
live = json.load(open(sys.argv[1]))
base = json.load(open(".gosec-baseline.json"))
live_issues = live.get("Issues", [])
live_n = len(live_issues)
base_n = base["total"]
live_by = Counter(i["rule_id"] for i in live_issues)
base_by = base.get("by_rule", {})
print(f"gosec: live={live_n}  baseline={base_n}")
for rule in sorted(set(live_by) | set(base_by)):
    l = live_by.get(rule, 0)
    b = base_by.get(rule, 0)
    flag = "" if l == b else (f"  (+{l-b})" if l > b else f"  (-{b-l})")
    print(f"  {rule:6s}  live={l:3d}  baseline={b:3d}{flag}")
if live_n > base_n:
    print(f"gosec: {live_n - base_n} net-new findings exceed baseline {base_n}")
    sys.exit(1)
if live_n < base_n:
    print(f"gosec: live count {live_n} below baseline {base_n}; consider lowering the baseline")
PY
  rc=$?
  rm -f "$out"
  return $rc
}
s_gitleaks() { gitleaks detect --source . --redact --exit-code 1; }
s_goreleaser_snapshot() { goreleaser release --snapshot --clean; }

# ---- slow-opt-in -------------------------------------------------------------
# Every command here is niced, capped at max(1, nproc/4) workers, hard-timed-out,
# and killed by the EXIT/INT/TERM trap if this script goes away.
SLOW_PIDS=()
kill_slow() {
  local p
  for p in "${SLOW_PIDS[@]:-}"; do
    [ -n "$p" ] || continue
    kill -TERM "$p" 2>/dev/null
  done
  SLOW_PIDS=()
}
slow_workers() {
  local n
  n="$(( $(nproc 2>/dev/null || echo 4) / 4 ))"
  [ "$n" -ge 1 ] || n=1
  echo "$n"
}
# slow_run <cmd...>: run niced + capped + timed out, wait, return its status.
slow_run() {
  local w t rc
  w="$(slow_workers)"
  t="${PRE_TAG_SLOW_TIMEOUT:-21600}"
  GOMAXPROCS="$w" GOFLAGS="-p=$w" MAKEFLAGS="-j$w" \
    nice -n 19 timeout -k 30 "$t" "$@" &
  SLOW_PIDS+=("$!")
  wait "$!"
  rc=$?
  SLOW_PIDS=()
  return $rc
}
s_slow_opt_in() {
  local rc=0 t
  echo "[slow-opt-in] make test-tmp-guard"
  slow_run make test-tmp-guard || rc=1
  echo "[slow-opt-in] make test-cross-build"
  DW_RUN_CROSS_BUILD=1 slow_run make test-cross-build || rc=1
  if grep -qE '^perf-rollup-check:' Makefile 2>/dev/null; then
    echo "[slow-opt-in] make perf-rollup-check"
    slow_run make perf-rollup-check || rc=1
  elif [ -f scripts/perf-rollup-check.sh ]; then
    echo "[slow-opt-in] scripts/perf-rollup-check.sh"
    slow_run bash scripts/perf-rollup-check.sh || rc=1
  else
    echo "[slow-opt-in] perf-rollup-check: no target or script, not run"
  fi
  for t in scripts/check-no-busy-loop.sh scripts/check-busy-loop-pattern.sh; do
    if [ -f "$t" ]; then
      echo "[slow-opt-in] $t"
      slow_run bash "$t" || rc=1
    fi
  done
  return $rc
}

# ---- docker-agent-base -------------------------------------------------------
# Only when a Dockerfile, the container workflow or security/accepted-risks.yml
# differs from the last tag (always when no tag exists). Returns 98 after
# printing its own SKIP line when nothing changed.
s_docker_agent_base() {
  local tag changed
  if tag="$(git describe --tags --abbrev=0 2>/dev/null)" && [ -n "$tag" ]; then
    changed="$(git diff --name-only "$tag" -- ':(glob)**/Dockerfile*' \
      .github/workflows/containers.yaml security/accepted-risks.yml)"
    if [ -z "$changed" ]; then
      echo "SKIP [docker-agent-base]: no Dockerfile, containers workflow or accepted-risks change since $tag"
      return 98
    fi
    echo "[docker-agent-base] changed since $tag:"; echo "$changed"
  else
    echo "[docker-agent-base] no tag found, running"
  fi
  docker build -f docker/dockerfiles/Dockerfile.agent-base --build-arg VERSION=pre-tag \
    -t datawatch-agent-base:pre-tag . || return 1
  if command -v trivy >/dev/null 2>&1; then
    trivy image --severity HIGH,CRITICAL --ignorefile .trivyignore --exit-code 1 \
      --format table datawatch-agent-base:pre-tag
  else
    echo "SKIP [docker-agent-base] trivy scan: trivy not found"
  fi
}

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
    slow-opt-in) echo make ;;
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
trap 'kill_slow; write_record' EXIT
trap 'kill_slow; exit 130' INT
trap 'kill_slow; exit 143' TERM

for id in "${STEP_IDS[@]}"; do
  if [ -n "${PRE_TAG_ONLY:-}" ] && [[ ",${PRE_TAG_ONLY}," != *",$id,"* ]]; then continue; fi
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
  if [ "$rc" -eq 98 ]; then continue; fi   # step printed its own SKIP line
  RUN=$((RUN + 1))
  if [ "$rc" -ne 0 ]; then
    echo "[$id] FAIL (${el}s)"
    RESULT=fail
    exit 1
  fi
  echo "[$id] ok (${el}s)"
done
echo "pre-tag: all steps passed"

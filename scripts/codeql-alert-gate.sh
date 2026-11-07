#!/usr/bin/env bash
# codeql-alert-gate.sh — helper for the CodeQL alert-gate workflow
# (.github/workflows/codeql.yaml).
#
# wait-analyses
#   Waits until GitHub default setup has uploaded an analysis for every
#   expected category at the commit under check, then reports success.
#   It checks once immediately, then waits: the first wait is
#   WAIT_INITIAL_SECONDS, the interval doubles after each wait, capped at
#   WAIT_MAX_INTERVAL_SECONDS. Every wait, including rate-limit waits,
#   counts against the total budget of WAIT_MAX_SECONDS.
#
#   Exit statuses:
#     0  every expected category has an analysis with commit_sha == SHA
#     1  the wait budget is used up with categories still missing. When
#        the last failure was a rate limit, the reset time is reported
#        and the line says it is "not a code problem, rerun the check
#        after the reset time" — never "No analysis for"
#     2  bad usage or missing required inputs
#
#   A rate-limited API failure is not treated as a missing analysis:
#   print a '::notice title=Rate limited::' line, make one extra call to
#   the rate-limit endpoint to learn the core reset epoch, wait until it
#   (at least 1 second, never longer than the remaining budget; the
#   current interval when no reset is reported) and retry. Any other
#   API failure keeps the old behaviour: nothing seen yet, retried on
#   the normal schedule.
#
#   Inputs (environment):
#     REPO                 owner/name of the repository (required)
#     REF                  ref to query analyses for (required unless
#                          PR_NUMBER is set)
#     SHA                  commit sha whose analyses are expected
#                          (required unless PR_NUMBER is set)
#     PR_NUMBER            when set, REF becomes refs/pull/N/head and
#                          SHA becomes PR_HEAD_SHA
#     PR_HEAD_SHA          see PR_NUMBER (required when PR_NUMBER is set)
#     EXPECTED_CATEGORIES  space-separated categories, default
#                          '/language:actions /language:go
#                          /language:javascript-typescript
#                          /language:python'
#
#   Test seams and tuning (environment):
#     CODEQL_GATE_API_COMMAND     command used for API calls through its
#                                 `api` subcommand (default: gh); tests
#                                 can substitute a stub
#     CODEQL_GATE_SLEEP_COMMAND   command used to wait (default: sleep)
#     WAIT_INITIAL_SECONDS        first wait after the immediate check
#                                 (default 60)
#     WAIT_MAX_INTERVAL_SECONDS   interval cap (default 300)
#     WAIT_MAX_SECONDS            total budget, incl. rate-limit waits
#                                 (default 1500)
#
# Usage: codeql-alert-gate.sh wait-analyses
set -euo pipefail

readonly PROG="codeql-alert-gate"

usage() {
  cat <<'EOF'
Usage: codeql-alert-gate.sh wait-analyses

wait-analyses  Wait until every category in EXPECTED_CATEGORIES has an
               analysis with commit_sha == SHA on REF.

Exit statuses:
  0  all expected analyses are present for SHA
  1  wait budget used up with categories still missing; when the last
     failure was a rate limit, its reset time is reported and the
     situation is flagged as not a code problem
  2  bad usage or missing required inputs

Inputs and timing are configured through the environment; see the
header comment of this script for the full contract.
EOF
}

require_int() {
  local name="$1"
  local value="$2"
  case "$value" in
    ''|*[!0-9]*)
      printf '%s: %s must be a non-negative integer, got %q\n' "$PROG" "$name" "$value" >&2
      return 2
      ;;
  esac
  return 0
}

# require_env <name> <value>
require_env() {
  local name="$1"
  local value="$2"
  if [ -z "$value" ]; then
    printf '%s: %s is required\n' "$PROG" "$name" >&2
    return 2
  fi
  return 0
}

# Bump the next interval: double, capped.
bump_interval() {
  interval=$((interval * 2))
  if [ "$interval" -gt "$wait_max_interval" ]; then
    interval="$wait_max_interval"
  fi
}

# Record a wait of <secs> seconds (clamped to the remaining budget) and
# sleep through CODEQL_GATE_SLEEP_COMMAND.
record_wait() {
  local secs="$1"
  local remaining
  remaining=$((wait_max - waited))
  if [ "$secs" -gt "$remaining" ]; then
    secs="$remaining"
  fi
  if [ "$secs" -lt 1 ]; then
    # Rate-limit waits must be at least a second, so a reset that already
    # passed cannot spin the loop.
    secs=1
  fi
  "$sleep_command" "$secs"
  waited=$((waited + secs))
  bump_interval
}

wait_analyses() {
  local api_command="${CODEQL_GATE_API_COMMAND:-gh}"
  local sleep_command="${CODEQL_GATE_SLEEP_COMMAND:-sleep}"
  local wait_initial="${WAIT_INITIAL_SECONDS:-60}"
  local wait_max_interval="${WAIT_MAX_INTERVAL_SECONDS:-300}"
  local wait_max="${WAIT_MAX_SECONDS:-1500}"
  require_int WAIT_INITIAL_SECONDS "$wait_initial"
  require_int WAIT_MAX_INTERVAL_SECONDS "$wait_max_interval"
  require_int WAIT_MAX_SECONDS "$wait_max"
  if [ "$wait_max" -eq 0 ]; then
    printf '%s: WAIT_MAX_SECONDS must be > 0\n' "$PROG" >&2
    return 2
  fi

  local expected="${EXPECTED_CATEGORIES:-/language:actions /language:go /language:javascript-typescript /language:python}"
  local repo="${REPO:-}"
  local ref sha=""
  if [ -n "${PR_NUMBER:-}" ]; then
    # Default setup records pull request analyses under refs/pull/N/head
    # at the PR head commit, not under refs/pull/N/merge at the merge commit.
    ref="refs/pull/${PR_NUMBER}/head"
    sha="${PR_HEAD_SHA:-}"
    require_env PR_HEAD_SHA "$sha"
  else
    ref="${REF:-}"
    sha="${SHA:-}"
    require_env REF "$ref"
    require_env SHA "$sha"
  fi
  require_env REPO "$repo"
  local analyses_url="repos/${repo}/code-scanning/analyses?ref=${ref}&per_page=100"

  # The analyses endpoint ignores a sha query parameter, so filter on
  # commit_sha here.
  local analyses_jq='.[] | select(.commit_sha == env.SHA) | .category'
  local reset_jq='.resources.core.reset'

  local interval="$wait_initial"
  local waited=0
  local missing=""
  local last_rate_limited=0
  local last_reset=""

  while true; do
    local have=""
    local rc=0
    local errfile
    errfile="$(mktemp)"
    local err=""
    have="$("${api_command}" api "$analyses_url" --jq "$analyses_jq" 2>"$errfile")" || rc=$?
    err="$(<"$errfile")"
    rm -f "$errfile"

    if [ "$rc" -eq 0 ]; then
      missing=""
      local c
      for c in $expected; do
        if ! printf '%s\n' "$have" | grep -qxF -- "$c"; then
          missing="${missing:+${missing} }${c}"
        fi
      done
      if [ -z "$missing" ]; then
        echo "All default setup analyses are present for ${sha} on ${ref}."
        return 0
      fi
      if [ "$waited" -ge "$wait_max" ]; then
        echo "::error title=Default setup analysis missing::No analysis for ${missing} on ${ref} at ${sha} after ${waited}s."
        return 1
      fi
      last_rate_limited=0
      last_reset=""
      echo "Waiting for: ${missing} (${waited}s of ${wait_max}s)"
      record_wait "$interval"
      continue
    fi

    local rate_limited=0
    if printf '%s\n' "$err" | grep -qiE 'rate limit|HTTP 429'; then
      rate_limited=1
    fi

    if [ "$rate_limited" -eq 1 ]; then
      last_rate_limited=1
      local reset=""
      # One extra call to learn the core rate-limit reset epoch.
      reset="$("${api_command}" api rate_limit --jq "$reset_jq" 2>"$errfile")" || reset=""
      rm -f "$errfile"
      case "$reset" in
        ''|*[!0-9]*)
          reset=""
          ;;
        *)
          last_reset="$reset"
          ;;
      esac
    else
      last_rate_limited=0
      last_reset=""
    fi

    if [ "$waited" -ge "$wait_max" ]; then
      if [ "$last_rate_limited" -eq 1 ]; then
        echo "::error title=Rate limited::Waited ${waited}s while the API kept returning rate limits (core reset epoch: ${last_reset:-unknown}). This is not a code problem, rerun the check after the reset time."
      else
        echo "::error title=Default setup analysis missing::No analysis for ${expected} on ${ref} at ${sha} after ${waited}s."
      fi
      return 1
    fi

    if [ "$rate_limited" -eq 1 ]; then
      echo "::notice title=Rate limited::API rate limit hit while checking analyses; retrying within the budget."
      if [ -n "$reset" ]; then
        local now
        now="$(date +%s)"
        record_wait $((reset - now))
      else
        record_wait "$interval"
      fi
      continue
    fi

    echo "Waiting for: ${expected} (${waited}s of ${wait_max}s)"
    record_wait "$interval"
  done
}

main() {
  local cmd="${1:-}"
  case "$cmd" in
    wait-analyses)
      wait_analyses
      ;;
    help|-h|--help)
      usage
      ;;
    *)
      printf '%s: expected the wait-analyses subcommand\n' "$PROG" >&2
      usage >&2
      return 2
      ;;
  esac
}

main "$@"

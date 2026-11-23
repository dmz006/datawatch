#!/usr/bin/env bash
# check-no-stray-daemons.sh — flag sandbox `datawatch` daemons that were not
# reaped after a test run.
#
# Usage:
#   bash scripts/check-no-stray-daemons.sh            # fail if any sandbox daemon lingers
#   bash scripts/check-no-stray-daemons.sh --list     # always exit 0, just report
#
# WHAT IT DOES
#   * Lists every running `datawatch start ...` process (read-only; it
#     NEVER kills anything — teardown is always the owner script's job via
#     its own recorded PID).
#   * A process is classified STRAY (sandbox) when its start args reference
#     a config/data path living in a known sandbox pattern (mktemp dir,
#     /tmp/dw-*, datawatch-smoke-*, *-e2e*, *.datawatch-test*, dw-docker-sim*,
#     dw-fed-peer*, or a repo-relative test config). Lingers there after a
#     test run = leaked sandbox.
#   * A process that looks like the production service is listed as
#     INFO/production (not failed on). Allow further PIDs or config-path
#     regexes via DATAWATCH_DAEMON_ALLOW (colon-separated) — e.g.
#     DATAWATCH_DAEMON_ALLOW="$(cat pidfile)" for a kept sandbox daemon.
#   * Exits non-zero if any STRAY process is reported.
#
# WHY READ-ONLY
#   Operator rule (2026-10-17 session): "NEVER kill a process you did not
#   start" and "never `pkill datawatch` as a catch-all." A checker script
#   running in an arbitrary shell cannot know which daemon a live run owns;
#   killing anything here risks SIGKILLing a healthy production service.
#   This script therefore only NAMES the leaker (pid, age, config path);
#   the operator (or the owning script's recorded-PID trap) does the stop.
set -u

ARG="${1:-}"

# ── known sandbox markers in the start args ──────────────────────────────────
STRAY_PATTERNS=(
  '/tmp/dw-'
  '/tmp/datawatch-'
  'datawatch-smoke-'
  '-e2e'
  '\.datawatch-test'
  'docker-sim'
  'dw-docker-sim'
  'dw-fed-peer'
  'dw-secure-smoke'
  'matrix-test'
  '[Tt]est-config\.yaml'
  '/synapse\.'
  'ts7[0-9]{2}-'
)
STRAY_REGEX="$(printf '%s|' "${STRAY_PATTERNS[@]}")"
STRAY_REGEX="(${STRAY_REGEX%\|})"

# best-effort: a daemon's own DATAWATCH_DATA_DIR (when ps args don't show it,
# e.g. a daemon started with env only). Same-user only; harmless otherwise.
proc_datadir() {
  local pid="$1"
  [[ -r "/proc/${pid}/environ" ]] || return 0
  tr '\0' '\n' < "/proc/${pid}/environ" 2>/dev/null \
    | awk -F= '/^DATAWATCH_DATA_DIR=/{print $2; exit}'
}

# ── allowlist (colon-separated PIDs or config-path regexes) ─────────────────
ALLOW="${DATAWATCH_DAEMON_ALLOW:-}"

is_allowed() {
  local pid="$1" args="$2" pat
  [[ -n "$ALLOW" ]] || return 1
  local IFS=':'
  for pat in $ALLOW; do
    [[ -z "$pat" ]] && continue
    if [[ "$pid" == "$pat" ]]; then return 0; fi
    if [[ "$pat" =~ ^[0-9]+$ ]]; then continue; fi
    if [[ "$args" =~ $pat ]]; then return 0; fi
  done
  return 1
}

# ── scan: one line per datawatch start process: pid, age_s, args ─────────────
PROCS="$(ps -eo pid=,etimes=,args= 2>/dev/null \
  | awk 'NF >= 3 { pid=$1; age=$2; sub(/^[0-9]+[ \t]+/,""); \
            if ($0 ~ /(^|[ \t\/])datawatch([ \t].*)*\bstart\b/) print pid "\t" age "\t" $0 }')"

STRAY_FOUND=0
LISTED=0

print_proc() {
  local pid="$1" age="$2" args="$3" cls="$4"
  # extract --config / --data-dir style arg value for reporting (regex kept
  # in a variable — a bare `[= ]+` inside [[ ... ]] trips the conditional parser)
  local cfg="" cfg_re='--config[= ]+([^ ]+)'
  if [[ "$args" =~ $cfg_re ]]; then cfg="${BASH_REMATCH[1]}"; fi
  LISTED=1
  printf '%s pid=%s age=%ss config=%s cmd=%s\n' \
    "$cls" "$pid" "$age" "${cfg:-?}" "$(echo "$args" | cut -c1-200)"
}

while IFS=$'\t' read -r pid age args; do
  [[ -n "${pid:-}" ]] || continue
  if is_allowed "$pid" "$args"; then
    print_proc "$pid" "$age" "$args" "ALLOWED"
    continue
  fi
  # strip pid+age, keep the command string
  cmd="${args}"
  datadir="$(proc_datadir "$pid" 2>/dev/null || true)"
  datadir_stray=0
  [[ -n "${datadir:-}" && ( "$datadir" == /tmp/* || "$datadir" =~ $STRAY_REGEX ) ]] && datadir_stray=1
  if [[ "${datadir_stray}" -eq 1 ]] || [[ "$cmd" =~ $STRAY_REGEX ]]; then
    [[ -n "${datadir:-}" ]] && printf 'INFO  pid=%s age=%ss DATAWATCH_DATA_DIR=%s\n' "$pid" "$age" "$datadir"
    print_proc "$pid" "$age" "$cmd" "STRAY"
    STRAY_FOUND=1
  else
    print_proc "$pid" "$age" "$cmd" "INFO "
  fi
done <<< "$PROCS"

echo ""
if [[ "$STRAY_FOUND" -eq 1 ]]; then
  echo "check-no-stray-daemons: FAIL — ${STRAY_FOUND:-0} sandbox daemon(s) still running (stray). "
  echo "  Stop each by its RECORDED PID (the test script owns its own daemon; see its logs/pidfile) —"
  echo "  this checker never kills on its own terms, and never signals a process by name."
  if [[ "$ARG" == "--list" ]]; then exit 0; fi
  exit 1
fi

if [[ "$LISTED" -eq 0 ]]; then
  echo "check-no-stray-daemons: OK — no lingering datawatch daemon processes found."
else
  echo "check-no-stray-daemons: OK — no STRAY (sandbox) datawatch daemons running."
fi
exit 0

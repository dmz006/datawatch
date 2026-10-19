#!/usr/bin/env bash
# scripts/check-unsafe-tmux-calls.sh — fail-closed scan for unsafe tmux calls.
#
# A safe tmux call in this repo always scopes itself to an explicit dedicated
# socket (-S / --socket) AND to a specific session target, so it can only
# affect sessions on that socket. The patterns below kill EVERY tmux
# session/server the user can reach (default socket), wiping unrelated live
# sessions including running datawatch session panes:
#
#   * a server-wide kill (kills all sessions on the socket) done without
#     an explicit dedicated -S / --socket on the same line
#   * killing ALL sessions: the kill-session form with the -a flag
#   * pkill of the tmux server process
#   * killall of the tmux server process
#
# Usage:
#   check-unsafe-tmux-calls.sh [SCAN_TARGET]
#   # or
#   SCAN_TARGET=scripts scripts/check-unsafe-tmux-calls.sh
#
# SCAN_TARGET (optional; positional $1 takes precedence over the SCAN_TARGET
# variable) is a file, or a directory scanned recursively. With no target
# it scans the repository code tree: scripts/, tests/, cmd/, internal/.
#
# Scoping notes (why this is precise rather than pattern-noisy, while still
# failing closed):
#   * In directory mode only shell scripts (*.sh *.bash *.ksh *.zsh) are
#     scanned: the flagged constructs are shell command lines. Markdown/JSON
#     documentation or data that merely describes the command is not
#     executable and is not shell code. A file passed explicitly as a target
#     is always scanned regardless of extension (caller's explicit choice).
#   * Comment-only lines (first non-whitespace char is #) are skipped, so
#     documentation of this rule does not self-trigger. This script also
#     excludes itself from the scan set (realpath comparison).
#   * If the resolved scan scope contains zero scannable files, the check
#     FAILS instead of passing on an empty scope.
#
# Output on violation (one per line):  <file>:<line>: <reason>
# Exit codes: 0 = clean scan, 1 = violations found (or empty scope),
#             2 = usage error (bad/missing target).
#
set -u

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
self="$script_dir/$(basename "${BASH_SOURCE[0]}")"
repo_root="$(cd "$script_dir/.." && pwd)"

usage() { echo "usage: $(basename "$0") [SCAN_TARGET]" >&2; }

# ---- resolve scan targets -------------------------------------------------
targets=()
if [ "$#" -ge 1 ]; then
  if [ -z "${1:-}" ]; then usage; exit 2; fi
  targets+=("$1")
elif [ -n "${SCAN_TARGET:-}" ]; then
  # shellcheck disable=SC2086  # word-splitting of SCAN_TARGET is intended
  for t in $SCAN_TARGET; do targets+=("$t"); done
else
  for d in scripts tests cmd internal; do
    [ -d "$repo_root/$d" ] && targets+=("$repo_root/$d")
  done
fi

if [ "${#targets[@]}" -eq 0 ]; then
  echo "check-unsafe-tmux-calls: FAIL (fail-closed): no scan targets resolved" >&2
  exit 1
fi

# ---- collect scannable files ----------------------------------------------
declare -A seen=()
files=()
for t in "${targets[@]}"; do
  case "$t" in -*) continue ;; esac
  if [ -f "$t" ]; then
    rp="$(realpath "$t" 2>/dev/null || echo "$t")"
    # Scanning this script itself would re-match its own rule text; it
    # cannot be the object of the check.
    [ "$rp" = "$self" ] && continue
    [ -n "${seen[$rp]:-}" ] && continue
    seen[$rp]=1
    files+=("$t")
  elif [ -d "$t" ]; then
    # Exclusions are anchored to the scan target ($t) so they only prune
    # that target's own scratch/VCS dirs — the checkout path itself may
    # legitimately contain components like .datawatch or target.
    while IFS= read -r -d '' f; do
      rp="$(realpath "$f" 2>/dev/null || echo "$f")"
      [ "$rp" = "$self" ] && continue
      [ -n "${seen[$rp]:-}" ] && continue
      seen[$rp]=1
      files+=("$f")
    done < <(find "$t" -type f \
               \( -name '*.sh' -o -name '*.bash' -o -name '*.ksh' -o -name '*.zsh' \) \
               -not -path "$t/.git/*" \
               -not -path "$t/.datawatch/*" \
               -not -path "*/node_modules/*" \
               -not -path "*/vendor/*" \
               -not -path "*/build/*" \
               -print0 | sort -z)
  else
    echo "check-unsafe-tmux-calls: FAIL (usage): scan target not found: $t" >&2
    exit 2
  fi
done

if [ "${#files[@]}" -eq 0 ]; then
  echo "check-unsafe-tmux-calls: FAIL (fail-closed): scan scope resolved to zero scannable shell files" >&2
  exit 1
fi

# ---- patterns (POSIX ERE, evaluated with bash [[ =~ ]]) --------------------
# Rule 1: a server-wide kill is only safe when the line scopes it to a
#         dedicated socket (-S or --socket).
re_kill_server='(^|[;|&`&(])[[:space:]]*tmux[[:space:]]+kill-server([^a-zA-Z0-9_-]|$)'
re_has_socket='(^|[[:space:]])--?(socket|-S)([=[:space:]]|$)'
# Rule 2: killing ALL sessions (kill-session with the -a flag, standalone,
#         not part of a longer flag/token).
re_kill_session='(^|[;|&`&(])[[:space:]]*tmux[[:space:]]+kill-session([^a-zA-Z0-9_-]|$)'
re_flag_a='(^|[[:space:]])-a([^a-zA-Z0-9_-]|$)'
# Rule 3/4: process-group murder of the tmux server (any pkill/killall that
#           names tmux, including option forms like 'pkill -9 tmux').
re_pkill_tmux='(^|[;|&`&(])[[:space:]]*pkill[[:space:]]+(-[A-Za-z0-9]+[[:space:]]+)*tmux([^a-zA-Z0-9_-]|$)'
re_killall_tmux='(^|[;|&`&(])[[:space:]]*killall[[:space:]]+tmux([^a-zA-Z0-9_-]|$)'

# ---- scan ------------------------------------------------------------------
violations=0
for f in "${files[@]}"; do
  [ -r "$f" ] || { echo "check-unsafe-tmux-calls: cannot read (skipping): $f" >&2; continue; }
  lineno=0
  while IFS= read -r line || [ -n "$line" ]; do
    lineno=$((lineno + 1))
    line="${line%$'\r'}"
    lead="${line%%[![:space:]]*}"
    [ "$lead" = "$line" ] && continue              # blank line
    stripped="${line#"$lead"}"
    [[ $stripped == \#* ]] && continue             # comment-only line

    # Rule 1: server-wide kill without an explicit dedicated socket.
    if [[ $line =~ $re_kill_server ]] && ! [[ $line =~ $re_has_socket ]]; then
      echo "$f:$lineno: unsafe tmux call: server-wide kill with no explicit dedicated -S/--socket kills every session reachable on the default socket; scope tmux to a dedicated socket and a specific session target"
      violations=$((violations + 1))
      continue
    fi
    # Rule 2: kill ALL sessions.
    if [[ $line =~ $re_kill_session && $line =~ $re_flag_a ]]; then
      echo "$f:$lineno: unsafe tmux call: kill-session with -a kills every session on the socket, not just this one; target a specific session (-t) on an explicit dedicated socket instead"
      violations=$((violations + 1))
      continue
    fi
    # Rule 3: pkill the tmux server process.
    if [[ $line =~ $re_pkill_tmux ]]; then
      echo "$f:$lineno: unsafe tmux call: pkill of the tmux server process drops every live tmux session; kill only this dedicated socket's session by name instead"
      violations=$((violations + 1))
      continue
    fi
    # Rule 4: killall the tmux server process.
    if [[ $line =~ $re_killall_tmux ]]; then
      echo "$f:$lineno: unsafe tmux call: killall of the tmux process drops every live tmux session; kill only this dedicated socket's session by name instead"
      violations=$((violations + 1))
    fi
  done < "$f"
done

if [ "$violations" -gt 0 ]; then
  echo "check-unsafe-tmux-calls: FAIL: $violations unsafe tmux call(s) found across ${#files[@]} file(s)"
  exit 1
fi

echo "check-unsafe-tmux-calls: OK: no unsafe tmux calls found in ${#files[@]} shell file(s)"
exit 0

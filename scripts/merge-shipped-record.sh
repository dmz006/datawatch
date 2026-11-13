# scripts/merge-shipped-record.sh — the shipped-record step of the merge
# helper (merge-branches-bump-version.sh).
#
# This file is SOURCED, never executed: it defines the functions the merge
# helper calls after the release bump commit, and it is sourced on its own
# by scripts/tests/merge-helper-shipped-record.sh.
#
# For every merged branch named automaton/<automaton id> it records the
# shipped state through the daemon CLI, best effort:
#
#   datawatch autonomous shipped record <automaton id> \
#       --release <version> --commit <merge sha> --at <ISO time> \
#       --source merge_step
#
# and, when that Automaton belongs to an Automation whose members are all
# shipped (every member Automaton already carries a merge commit), the
# matching Automation call:
#
#   datawatch autonomous shipped record <automation id> --automation \
#       --release <version> --commit <merge sha> --at <ISO time> \
#       --source merge_step
#
# After each successful shipped record it also marks the record Completed
# (best effort), through the single-record mode of the migration CLI:
#
#   datawatch autonomous completed-migrate --id <automaton id>
#   datawatch autonomous completed-migrate --id <automation id>   # all members shipped
#
# so the Automaton ends Completed with the merge time. The merge helper needs
# no extra call site: record_shipped_for_branch makes these calls itself.
#
# Contract:
#   * autonomous.shipped.enabled is honoured when it can be read
#     (`datawatch config get`); unset, missing or an unreadable/lost daemon
#     mean ON (the setting's default).
#   * Every datawatch failure — daemon unreachable, command error, unknown
#     id — prints a warning and returns 0. A failed record must never fail
#     a merge that already succeeded.
#   * No version number is produced or hard-coded here; the caller hands
#     over the release it computed from the repository itself.
#   * No tmux, no service control, no writes outside the daemon's own API.
#   * No cancel command is ever run from this file.
#   * The completed mark follows the same setting as the shipped record and
#     only runs after the shipped record for that id succeeded.
#
# Functions:
#   shipped_enabled                      0 when recording is on (the default);
#                                        1 only when the readable setting is off
#   automation_if_all_shipped PRD_ID     prints the PRD's automation id when the
#                                        PRD sits in an Automation and every
#                                        member of it is shipped; prints nothing
#                                        and returns 1 otherwise
#   completed_mark ID                    best-effort `completed-migrate --id ID`;
#                                        a failure warns; always returns 0
#   record_shipped_for_branch BRANCH RELEASE SHA AT
#                                        records the shipped state for one
#                                        merged branch; always returns 0

# shipped_enabled — honour autonomous.shipped.enabled when it is readable,
# defaulting to on. A value of false/0 (any case) turns the step off; a
# missing key, a daemon that is not up, or anything unparsable all read as
# enabled, which matches the setting's default.
shipped_enabled() {
    local v=""
    v=$(datawatch config get autonomous.shipped.enabled 2>/dev/null) || v=""
    v="${v//[!A-Za-z0-9]/}"
    case "$v" in
        false | False | FALSE | 0) return 1 ;;
    esac
    return 0
}

# automation_if_all_shipped PRD_ID — prints the automation id of the PRD
# when the PRD is a member of one AND every member of that Automation
# already carries a merge commit (the daemon's shipped definition). Any
# daemon or parse failure, a standalone PRD, or one unshipped member means
# no output and a non-zero return.
automation_if_all_shipped() {
    local prd_id="$1" prds auto_id
    prds=$(datawatch autonomous prd-list 2>/dev/null) || return 1
    prds=$(jq -c '.' <<<"$prds" 2>/dev/null) || return 1
    auto_id=$(jq -r --arg id "$prd_id" \
        '[.[]? | select(.id == $id) | (.automation_id // "")] | map(select(length > 0)) | .[0] // empty' \
        <<<"$prds" 2>/dev/null) || return 1
    [[ -n "$auto_id" ]] || return 1
    if jq -e --arg a "$auto_id" '
        [.[]? | select((.automation_id // "") == $a)] as $members
        | ($members | length) > 0
          and all($members[]; ((.merge_commit // "") | length) > 0)
    ' <<<"$prds" >/dev/null 2>&1; then
        printf '%s\n' "$auto_id"
        return 0
    fi
    return 1
}

# completed_mark ID — marks one Automaton or Automation Completed through the
# single-record mode of the migration CLI. Best effort: any failure prints a
# warning and the function still returns 0.
completed_mark() {
    local id="${1:-}"
    [[ -n "$id" ]] || return 0
    if datawatch autonomous completed-migrate --id "$id"; then
        echo "==> completed marked: $id"
    else
        echo "warning: completed mark for $id failed; the merge already succeeded and continues" >&2
    fi
    return 0
}

# record_shipped_for_branch BRANCH RELEASE SHA AT — the per-branch record
# step. BRANCH is the merged branch name, RELEASE the release the merge
# bumped to (for example v9.54.1), SHA the merge commit and AT the merge
# time (RFC 3339). Always returns 0: failures are warnings, never errors.
record_shipped_for_branch() {
    local branch="${1:-}" release="${2:-}" sha="${3:-}" at="${4:-}"
    local prd_id auto_id

    case "$branch" in
        automaton/*) prd_id="${branch#automaton/}" ;;
        *) echo "==> shipped record skipped: branch '$branch' is not named automaton/<automaton id>"
           return 0 ;;
    esac
    if [[ -z "$prd_id" ]]; then
        echo "==> shipped record skipped: no id after 'automaton/' in branch '$branch'"
        return 0
    fi

    if ! shipped_enabled; then
        echo "==> shipped record skipped: autonomous.shipped.enabled is off"
        return 0
    fi

    if datawatch autonomous shipped record "$prd_id" \
        --release "$release" --commit "$sha" --at "$at" --source merge_step; then
        echo "==> shipped recorded: automaton $prd_id shipped in $release at $sha"
        completed_mark "$prd_id"
    else
        echo "warning: shipped record for automaton $prd_id failed; the merge already succeeded and continues" >&2
        return 0
    fi

    auto_id=$(automation_if_all_shipped "$prd_id") || return 0
    if datawatch autonomous shipped record "$auto_id" --automation \
        --release "$release" --commit "$sha" --at "$at" --source merge_step; then
        echo "==> shipped recorded: automation $auto_id (all members shipped) shipped in $release at $sha"
        completed_mark "$auto_id"
    else
        echo "warning: shipped record for automation $auto_id failed; the merge already succeeded and continues" >&2
    fi
    return 0
}

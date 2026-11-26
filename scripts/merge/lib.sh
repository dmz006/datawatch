# shellcheck shell=bash
# Shared by the checks in scripts/merge. Source it; do not run it.
#
# datawatch runs each check inside the scratch worktree that holds the exact
# commit it is about to push, with an empty HOME, no credentials and these
# facts about the merge:
#   MERGE_PROJECT_DIR      the project's own checkout (files never committed)
#   MERGE_BASE_COMMIT      the commit the merge is built on
#   MERGE_RELEASE_SIZE     patch, minor, major or none
#   MERGE_RELEASE_VERSION  the version this merge carries
#   MERGE_SHADOW           1 in a shadow run
#   MERGE_PROPOSAL         1 while judging a model's conflict proposal

cd "$(git rev-parse --show-toplevel)" || exit 2

# No check may reach a terminal server it was started from. datawatch also
# gives each check a private TMUX_TMPDIR.
unset TMUX

merge_die() {
    echo "merge check: $*" >&2
    exit 2
}

merge_need_var() {
    [[ -n "${!1:-}" ]] || merge_die "$1 is not set (these checks run as merge pipeline steps)"
}

# A missing tool fails the check: a check that did not run never passes.
merge_need_tool() {
    command -v "$1" >/dev/null 2>&1 || merge_die "the tool $1 is not installed, so this check cannot run"
}

# How many packages or workers a check may use at once: a quarter of the
# cores, at least one and at most four.
merge_workers() {
    local n=$(($(nproc 2>/dev/null || echo 4) / 4))
    ((n < 1)) && n=1
    ((n > 4)) && n=4
    echo "$n"
}

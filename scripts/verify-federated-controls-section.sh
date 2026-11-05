#!/usr/bin/env bash
# Tracked wrapper for the PRD 396a0001 (federated peer placement design
# doc) story-check script.
#
# The canonical story-check lives at
#   .datawatch/verify/peer-placement-doc-complete.sh
# (that is the path the PRD gate spec names; .datawatch/ is git-ignored,
# so this tracked wrapper is the in-repo record of the check's location
# and keeps it discoverable after a fresh clone of the PRD worktree).
#
# AGENT.md line 1367: a design-document task without a script fails plan
# validation. Run either path; the wrapper execs the canonical script so
# there is exactly one implementation to maintain.

set -euo pipefail
cd "$(dirname "$0")/.."
exec bash .datawatch/verify/peer-placement-doc-complete.sh

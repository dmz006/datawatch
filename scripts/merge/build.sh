#!/usr/bin/env bash
# The project's own build.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
merge_need_tool go
make build

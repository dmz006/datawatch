#!/usr/bin/env bash
# The web app: a syntax check and its tests.
set -euo pipefail
source "$(dirname "$0")/lib.sh"
merge_need_tool node
node --check internal/server/web/app.js
nice -n 10 node --test --test-concurrency="$(merge_workers)" internal/server/web/*.test.js

#!/usr/bin/env bash
# TS-824 — federated tag registry on a peer daemon: keeper tags shown, rename reaches the peer, stale banner and disabled edits when the keeper stops, pending sync on an offline create, merge note and dismiss when the keeper returns with a same-name tag
# tags: surface:pwa feature:tag-registry conflict:pwa
# pwa-script: pwa/TS-824.mjs
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-824"
story_preflight "surface:pwa feature:tag-registry conflict:pwa" || return 0

run_pwa_story "TS-824"

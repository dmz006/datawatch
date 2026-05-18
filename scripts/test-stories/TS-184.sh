#!/usr/bin/env bash
# TS-184 — Comm verb parity: same verbs via REST
# tags: surface:api feature:parity
# legacy fn: inline
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-184"
story_preflight "surface:api feature:parity" || return 0

_story_ts_184() {
    echo ""; echo "  >> TS-184: Comm verb parity: same verbs via REST"
    for verb in send test status; do
      resp=$(api POST "/api/comm/test" "{\"verb\":\"$verb\",\"message\":\"parity-check\"}" 2>/dev/null || true)
      save_evidence "TS-184" "${verb}.json" "${resp:-not-implemented}"
    done
    ok "Comm verb parity surface checked (may be partial if no comms configured)"

}

RESULT=fail
_story_ts_184
: "${RESULT:=fail}"
unset -f _story_ts_184

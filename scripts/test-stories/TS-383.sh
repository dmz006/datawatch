#!/usr/bin/env bash
# TS-383 — GET /.well-known/unifiedpush returns discovery doc with version:1
# tags: surface:api feature:push
# STUB: no implementation extracted from legacy runner. Mark as skip until ported.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-383"
story_preflight "surface:api feature:push" || return 0

RESULT=skip
skip "stub — no implementation yet (see master-cookbook for spec)"
: "${RESULT:=skip}"

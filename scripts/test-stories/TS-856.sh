#!/usr/bin/env bash
# TS-856 — no raw locale key is ever on screen: with the locale bundle delayed 3 s, every visible string, the title, aria-label and placeholder attributes of the header, the bottom navigation, the pause window and every main view and sub-tab are read at 0.5 s, at 2 s and after the bundle loads, and none is a locale key
# tags: surface:pwa feature:i18n-raw-keys
# pwa-script: pwa/raw-key-scan.mjs
# See pwa/raw-key-scan.mjs header; the classifier is pwa/raw-key-scan-lib.mjs (node --test).
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
CURRENT_STORY="TS-856"
story_preflight "surface:pwa feature:i18n-raw-keys" || return 0

run_pwa_story "raw-key-scan"

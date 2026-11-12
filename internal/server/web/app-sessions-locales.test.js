'use strict';
// Session UI locale guard (tile mode, grouping, sub-task nesting, saved views).
//
// The sessions tile, grouping, nesting and saved-view code in app.js asks the
// i18n layer for literal sessions- prefixed keys (_stx('key', 'fallback')).
// This test extracts exactly those literal key names from app.js as written,
// then asserts each one exists in all five locale bundles (en, de, es, fr,
// ja) with a non-empty value, that the five bundles carry an identical key
// set, and that the non-English bundles really translate the new values
// rather than copying English (short product terms are exempt).
//
// Run: node --test internal/server/web/app-sessions-locales.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');

const WEB_DIR = __dirname;
const LOCALES_DIR = path.join(WEB_DIR, 'locales');
const APP_JS = path.join(WEB_DIR, 'app.js');
const LANGS = ['en', 'de', 'es', 'fr', 'ja'];

const bundles = {};
for (const lang of LANGS) {
  bundles[lang] = JSON.parse(
    fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));
}

// ── key extraction from app.js ───────────────────────────────────────────
// The sessions UI strings are looked up by literal key name, e.g.
// _stx('sessions_view_list', 'List'). Capture every sessions- prefixed key
// passed as the first string literal to _stx( or t( in app.js.
function extractSessionsKeys() {
  const source = fs.readFileSync(APP_JS, 'utf8');
  const pattern = /\b(?:_stx|t)\(\s*(['"])([A-Za-z0-9_]*sessions_[A-Za-z0-9_]*)\1/g;
  const keys = new Set();
  let m;
  while ((m = pattern.exec(source)) !== null) keys.add(m[2]);
  return [...keys].sort();
}

const SESSIONS_KEYS = extractSessionsKeys();

// The tile / grouping / nesting / saved-view strings this feature owns: all
// of them must be referenced in app.js (that is the contract), and the set
// is pinned so a rename in app.js cannot silently shrink the coverage.
const REQUIRED_KEYS = [
  'sessions_show_more',
  'sessions_tile_delete',
  'sessions_tile_restart',
  'sessions_tile_stop',
  'sessions_tile_summary',
  'sessions_view_compact',
  'sessions_view_list',
  'sessions_view_tiles',
  'sessions_view_toggle',
  'sessions_group_by',
  'sessions_group_none',
  'sessions_group_state',
  'sessions_group_node',
  'sessions_group_project',
  'sessions_group_tag',
  'sessions_group_tag_prefixes',
  'sessions_group_automaton',
  'sessions_group_model',
  'sessions_runs_on',
  'sessions_model_on',
  'sessions_runs_on_tip',
  'sessions_model_on_tip',
  'sessions_group_ungrouped',
  'sessions_group_empty',
  'sessions_group_none_view',
  'sessions_group_show_more',
  'sessions_auto_ungrouped',
  'sessions_auto_story_root',
  'sessions_view_title',
  'sessions_view_apply',
  'sessions_view_rename',
  'sessions_view_delete',
  'sessions_view_new',
  'sessions_view_ok',
  'sessions_view_name_ph',
  'sessions_search_placeholder',
  'sessions_search_label',
  'sessions_search_tip',
  'sessions_search_clear',
  'sessions_search_count',
  'sessions_filters_btn',
  'sessions_filters_tip',
  'sessions_filters_title',
  'sessions_filters_close',
  'sessions_filters_status',
  'sessions_filters_backend',
  'sessions_filters_tags',
  'sessions_fold_all',
  'sessions_unfold_all',
  'sessions_fold_all_tip',
  'sessions_unfold_all_tip',
  'sessions_tags_less',
  'sessions_tags_collapse_tip',
  'sessions_tags_expand_tip',
].sort();

test('extraction finds the sessions UI keys used in app.js', () => {
  assert.ok(
    SESSIONS_KEYS.length >= REQUIRED_KEYS.length,
    'expected at least ' + REQUIRED_KEYS.length
      + ' sessions keys in app.js, found ' + SESSIONS_KEYS.length);
  for (const k of REQUIRED_KEYS) {
    assert.ok(SESSIONS_KEYS.includes(k), 'app.js must reference key ' + k);
  }
});

for (const lang of LANGS) {
  test(lang + '.json: every sessions UI key is present and non-empty', () => {
    const missing = REQUIRED_KEYS.filter(
      (k) => typeof bundles[lang][k] !== 'string' || bundles[lang][k].trim() === '');
    assert.deepEqual(missing, [],
      lang + '.json is missing or empty for: ' + missing.join(', '));
  });
}

test('every literal sessions key app.js asks for resolves in all five bundles', () => {
  for (const lang of LANGS) {
    const unresolved = SESSIONS_KEYS.filter(
      (k) => typeof bundles[lang][k] !== 'string' || bundles[lang][k].trim() === '');
    assert.deepEqual(unresolved,
      [], lang + '.json does not resolve app.js sessions keys: ' + unresolved.join(', '));
  }
});

test('the five locale bundles have identical key sets', () => {
  const enKeys = Object.keys(bundles.en).sort();
  for (const lang of LANGS) {
    if (lang === 'en') continue;
    assert.deepEqual(Object.keys(bundles[lang]).sort(), enKeys,
      lang + '.json key set differs from en.json');
  }
});

// Short product/UI terms that are acceptable unchanged outside English.
const PRODUCT_TERMS = new Set(['OK', 'Compact', 'sessions', 'Tag', 'Tags', 'Status', 'Backend']);

for (const lang of LANGS) {
  if (lang === 'en') continue;
  test(lang + '.json translates the sessions keys (no English copies)', () => {
    const untranslated = REQUIRED_KEYS.filter((k) =>
      bundles[lang][k] === bundles.en[k] && !PRODUCT_TERMS.has(bundles.en[k]));
    assert.deepEqual(untranslated,
      [], lang + '.json still English: ' + untranslated.join(', '));
  });
}

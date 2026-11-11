// The three small usage cards are built-in dashboard card types that share one
// renderer with the Resources row. Source-level checks on app.js plus the five
// locale bundles on disk.
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');

const src = fs.readFileSync(path.join(__dirname, 'app.js'), 'utf8');
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const TYPES = ['usage-allowance', 'usage-pace', 'usage-burn'];
const NEW_KEYS = ['dash_card_usage_allowance', 'dash_card_usage_pace', 'dash_card_usage_burn'];
const HELP_KEYS = ['usage_page_allowance_help', 'usage_page_pace_help', 'usage_page_burn_help'];

test('three card types are registered in the dashboard card defs', () => {
  for (const id of TYPES) {
    assert.ok(new RegExp(`\\{ id: '${id}', label: '[^']+', labelKey: 'dash_card_[a-z_]+'`).test(src), id);
  }
});

test('the cards share one renderer table and the Resources row uses it', () => {
  assert.match(src, /const USAGE_CARD_RENDERERS = \{\s*'usage-allowance': _usageCalcAllowanceCard,\s*'usage-pace': _usageCalcPaceCard,\s*'usage-burn': _usageCalcBurnCard,/);
  assert.match(src, /window\.usageCalcCardForType = function/);
  assert.match(src, /window\.usageCalcCardForType\(el\.getAttribute\('data-usage-card'\), advice\)/);
  // Every card, wherever it appears, goes through the one card html builder.
  assert.equal((src.match(/_usageCalcCardHtml\('/g) || []).length, 3);
});

test('Resources renders the three cards in a single wrapping row', () => {
  assert.match(src, /function _usageCalcCardsRowHtml\(a\) \{\s*const cards = _usageCalcAllowanceCard\(a\) \+ _usageCalcPaceCard\(a\) \+ _usageCalcBurnCard\(a\);\s*return cards \? `<div class="usage-cards">/);
  assert.match(src, /const row = _usageCalcCardsRowHtml\(a\);/);
  const css = fs.readFileSync(path.join(__dirname, 'style.css'), 'utf8');
  assert.match(css, /\.usage-cards \{ display: flex; flex-wrap: wrap;/);
  assert.match(src, /id="automationUsageCard"/);
});

test('each card has a ? help entry with a docs link', () => {
  assert.match(src, /usage-help-docs" href="\$\{USAGE_CALC_DOCS_HREF\}"/);
  for (const k of HELP_KEYS) assert.ok(src.includes(`'${k}'`), k);
});

test('every new locale key exists in all five bundles', () => {
  for (const l of LOCALES) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));
    for (const k of NEW_KEYS.concat(HELP_KEYS)) {
      assert.equal(typeof b[k], 'string', `${l}:${k}`);
      assert.ok(b[k].length > 0, `${l}:${k}`);
    }
  }
});

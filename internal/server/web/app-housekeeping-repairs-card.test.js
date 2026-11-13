const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const src = fs.readFileSync(__dirname + '/app.js', 'utf8');
const locales = ['en', 'de', 'es', 'fr', 'ja'].map(l => [l, JSON.parse(fs.readFileSync(`${__dirname}/locales/${l}.json`, 'utf8'))]);

test('repairs card and slot are wired to the housekeeping sub-tab', () => {
  for (const s of ['window.housekeepingRepairsCardHtml', "'/api/autonomous/housekeeping-repairs'",
    "registerAutomationCardSlot('housekeeping'", "id: 'automationHousekeepingRepairsCard'"]) assert.ok(src.includes(s), s);
});

test('settings carry the four group mode selectors with off, report, on', () => {
  for (const g of ['task_chain', 'advance_judge', 'lane_fill', 'leftovers']) {
    const line = src.split('\n').find(l => l.includes(`key: 'autonomous.housekeeping_repairs.${g}'`));
    assert.ok(line, g);
    assert.ok(line.includes("type: 'select'") && line.includes("options: ['off', 'report', 'on']"), g);
    assert.ok(line.includes('helpKey:'), g + ' help');
  }
});

test('every key the card and settings use exists in every locale', () => {
  const keys = new Set();
  for (const m of src.matchAll(/'((?:settings_housekeeping_|housekeeping_repairs_)[a-z_]+)'/g)) { if (!m[1].endsWith('_') && m[1] !== 'housekeeping_repairs_card') keys.add(m[1]); }
  for (const g of ['task_chain', 'advance_judge', 'lane_fill', 'leftovers']) keys.add('housekeeping_repairs_group_' + g);
  for (const m of ['off', 'report', 'on']) keys.add('housekeeping_repairs_mode_' + m);
  assert.ok(keys.size > 20);
  for (const [l, j] of locales) for (const k of keys) assert.ok(j[k], `${l}: ${k}`);
});

test('no tracker ids in the new strings', () => {
  for (const [l, j] of locales) for (const [k, v] of Object.entries(j)) {
    if (/^(settings_housekeeping_|housekeeping_repairs_)/.test(k)) assert.ok(!/\b(BL|GH)\d+/.test(v), `${l}: ${k}`);
  }
});

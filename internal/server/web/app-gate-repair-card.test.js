const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const src = fs.readFileSync(__dirname + '/app.js', 'utf8');
const locales = ['en', 'de', 'es', 'fr', 'ja'].map(l => [l, JSON.parse(fs.readFileSync(`${__dirname}/locales/${l}.json`, 'utf8'))]);

test('gate repair card and slot are wired to the housekeeping sub-tab', () => {
  for (const s of [
    'window.gateRepairCardHtml',
    "'/api/autonomous/gate-repair'",
    "method: 'POST'",
    "registerAutomationCardSlot('housekeeping'",
    "id: 'automationGateRepairCard'",
    "url: '/api/autonomous/gate-repair'",
    "key: 'automation_gate_repair'",
  ]) assert.ok(src.includes(s), s);
});

test('mode selector writes autonomous.gate_repair.mode with report/on options', () => {
  // The mode write targets the shared config endpoint with the exact key.
  assert.ok(src.includes("'autonomous.gate_repair.mode'"), 'config key');
  assert.ok(src.includes("id=\"gateRepairModeSelect\""), 'select element');
  const sel = src.slice(src.indexOf('id="gateRepairModeSelect"'), src.indexOf('</select>', src.indexOf('id="gateRepairModeSelect"')));
  assert.ok(sel.includes('value="report"') && sel.includes('value="on"'), 'report/on options');
  assert.ok(!sel.includes('value="off"'), 'selector stays report/on only');
  // The same setting exists in the Settings group with off, report, on.
  const line = src.split('\n').find(l => l.includes(`key: 'autonomous.gate_repair.mode'`));
  assert.ok(line, 'settings entry');
  assert.ok(line.includes("type: 'select'") && line.includes("options: ['off', 'report', 'on']"), 'settings select');
  assert.ok(line.includes('helpKey:'), 'settings help');
});

test('run-now button posts to the run route and refreshes', () => {
  assert.ok(src.includes('window.runGateRepairNow(this)'), 'button handler');
  const fn = src.slice(src.indexOf('window.runGateRepairNow'));
  assert.ok(fn.includes("'/api/autonomous/gate-repair/run'"), 'run route');
  assert.ok(fn.includes('_gateRepairRefresh'), 'refresh after run');
});

test('empty record renders a localized "nothing yet" line', () => {
  assert.ok(src.includes('t(\'gate_repair_nothing\')'), 'renderer empty fallback');
  assert.ok(src.includes('findings.join(\'\') || empty'), 'empty line substitution');
  for (const [l, j] of locales) assert.ok(j['gate_repair_nothing'], `${l}: gate_repair_nothing`);
});

test('failed fetch renders a localized failure line via the shared loader', () => {
  const l = src.slice(src.indexOf('_automationGateRepairLoader'));
  assert.ok(l.includes('failedText: msg => t(\'gate_repair_failed\', [msg])'), 'failedText');
  for (const [lang, j] of locales) assert.ok(j['gate_repair_failed'], `${lang}: gate_repair_failed`);
  // Findings, budget and mode labels are present too.
  for (const piece of ['finding_cause', 'finding_repair', 'finding_before', 'finding_after',
    'budget_label', 'budget_spent', 'budget_empty', 'run_now', 'run_running', 'run_failed',
    'mode_save_failed', 'card_title', 'purpose', 'findings_label']) {
    for (const [lang, j] of locales) assert.ok(j['gate_repair_' + piece], `${lang}: gate_repair_${piece}`);
  }
});

test('every key the card uses exists in every locale', () => {
  const keys = new Set();
  for (const m of src.matchAll(/'gate_repair_[a-z_0-9]+(?:_[a-z0-9]+)*'/g)) {
    const k = m[0].slice(1, -1);
    if (k.endsWith('_')) continue; // dynamic 'gate_repair_mode_' / 'gate_repair_kind_' prefixes
    keys.add(k);
  }
  for (const m of ['off', 'report', 'on']) keys.add('gate_repair_mode_' + m);
  for (const k of ['would', 'did', 'failed', 'skipped_unsafe']) keys.add('gate_repair_kind_' + k);
  assert.ok(keys.size > 20);
  for (const [l, j] of locales) for (const k of keys) assert.ok(j[k], `${l}: ${k}`);
});

test('no tracker ids or PRD wording in the gate repair strings', () => {
  for (const [l, j] of locales) for (const [k, v] of Object.entries(j)) {
    if (k.startsWith('gate_repair_')) {
      assert.ok(!/\b(BL|GH)\d+/.test(v), `${l}: ${k}`);
      assert.ok(!/PRD/i.test(v), `${l}: ${k}`);
    }
  }
});

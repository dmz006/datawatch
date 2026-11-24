// Burn guard card — the pace policy block. The status carries `pace`: the mode, the hold
// the policy keeps (or, in shadow, the hold it would keep) with its rule, and the recent
// decisions with the evidence they were made on. The card only draws what the status says;
// it computes nothing. Every string goes through the five locale bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadApp(lang) {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', (lang || 'en') + '.json'), 'utf8'));
  return sandbox;
}

function card(s, status) {
  s._st = status;
  return vm.runInContext('window.burnGuardCardHtml(_st, {}, {})', s);
}

const BASE = { enabled: true, state: 'idle', level: 0, applied: [] };
const DECISION = {
  at: '2026-10-30T20:00:00Z', entry: 'claude-code', mode: 'on', action: 'hold', rule: 'window_pause_line',
  reason: 'window estimate 88.0% is at or over the 85% pause line', window_estimated: 88, window_stop_percent: 85,
};
const ON = Object.assign({}, BASE, {
  pace: {
    mode: 'on', enforcing: true, held: true,
    entries: [{ llm: 'claude-code', held: true, would_hold: false, rule: 'window_pause_line', since: '2026-10-30T20:00:00Z', last: DECISION }],
    log: [DECISION],
  },
});
const SHADOW = Object.assign({}, BASE, {
  pace: {
    mode: 'shadow', enforcing: false, held: false,
    entries: [{ llm: 'claude-code', held: false, would_hold: true, rule: 'weekly_pace_line',
      last: Object.assign({}, DECISION, { mode: 'shadow', action: 'would_hold', rule: 'weekly_pace_line', reason: 'week estimate 97.0% is over the pace line' }) }],
    log: [Object.assign({}, DECISION, { mode: 'shadow', action: 'would_hold', rule: 'weekly_pace_line', reason: 'week estimate 97.0% is over the pace line' })],
  },
});

test('enforcing: the card says new work is held, on which rule, and why', () => {
  const html = card(loadApp(), ON);
  assert.ok(html.includes('data-burn-guard="pace"'));
  assert.ok(html.includes('data-pace-mode="on"'));
  assert.ok(html.includes('Pace policy: on (enforcing)'));
  assert.ok(html.includes('claude-code: new work is held (rule: window pause line, since'));
  assert.ok(html.includes('window estimate 88.0% is at or over the 85% pause line'));
  assert.ok(html.includes('data-burn-guard="pace-log"'));
  assert.ok(html.includes('held'));
});

test('shadow: the card says what it would hold and that nothing is held', () => {
  const html = card(loadApp(), SHADOW);
  assert.ok(html.includes('data-pace-mode="shadow"'));
  assert.ok(html.includes('Pace policy: shadow (records only)'));
  assert.ok(html.includes('would hold new work (rule: weekly pace line; shadow, nothing is held)'));
  assert.ok(!html.includes('new work is held ('));
});

test('nothing held, an old daemon with no pace block, and escaping', () => {
  const none = card(loadApp(), Object.assign({}, BASE, { pace: { mode: 'on', enforcing: true, held: false, entries: [], log: [] } }));
  assert.ok(none.includes('No new work is held.'));
  assert.ok(!card(loadApp(), BASE).includes('data-burn-guard="pace"'));
  const evil = JSON.parse(JSON.stringify(ON));
  evil.pace.entries[0].last.reason = '<b>x</b>';
  evil.pace.entries[0].llm = '<i>y</i>';
  const html = card(loadApp(), evil);
  assert.ok(!html.includes('<b>x</b>') && !html.includes('<i>y</i>'));
});

test('the log shows at most the five newest decisions', () => {
  const st = JSON.parse(JSON.stringify(ON));
  st.pace.log = Array.from({ length: 9 }, (_, i) => Object.assign({}, DECISION, { reason: 'decision-number-' + i }));
  const html = card(loadApp(), st);
  assert.equal((html.match(/decision-number-/g) || []).length, 5);
});

const KEYS = [
  'burn_guard_pace_line', 'burn_guard_pace_mode_off', 'burn_guard_pace_mode_shadow', 'burn_guard_pace_mode_on',
  'burn_guard_pace_none', 'burn_guard_pace_held', 'burn_guard_pace_would_hold',
  'burn_guard_pace_rule_window_pause_line', 'burn_guard_pace_rule_weekly_pace_line',
  'burn_guard_pace_log_title', 'burn_guard_pace_log_line',
  'burn_guard_pace_action_hold', 'burn_guard_pace_action_release', 'burn_guard_pace_action_would_hold',
  'burn_guard_pace_action_would_release', 'burn_guard_pace_action_none',
];

test('every pace key is translated in en, de, es, fr and ja', () => {
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    for (const k of KEYS) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, `${lang} lacks ${k}`);
      if (lang !== 'en') assert.notEqual(b[k], en[k], `${lang} ${k} is the English text`);
    }
  }
});

test('the card draws in German with the translated strings', () => {
  const html = card(loadApp('de'), ON);
  assert.ok(!html.includes('Pace policy'));
  assert.ok(html.includes('data-burn-guard="pace"'));
});

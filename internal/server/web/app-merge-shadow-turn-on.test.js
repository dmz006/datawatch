// The turn-on report on the Merge shadow card: the matched hand merges against
// the target, whether merging is on, and the report text, escaped. It shows
// once a hand merge has been shadowed, also with no earlier comparison.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const CARD_JS = path.join(__dirname, 'app-merge-shadow-card.js');
const LOCALES_DIR = path.join(__dirname, 'locales');
const KEYS = [
  'merge_shadow_turn_on_streak', 'merge_shadow_turn_on_merging_on',
  'merge_shadow_turn_on_merging_off', 'merge_shadow_turn_on_report',
];

const bundle = (lang) => JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, lang + '.json'), 'utf8'));

function render(view) {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  loadScript(CARD_JS, sandbox);
  sandbox._i18n.bundle = bundle('en');
  sandbox._view = view;
  return vm.runInContext('window.mergeShadowCardHtml(_view)', sandbox);
}

const turnOn = (over) => Object.assign({
  merging_on: false, target: 5, met: false, streak: { length: 2, total: 3 },
  merges: [{ automaton_id: 'a1' }], lines: ['Turn-on report for demo.', 'line <b>two</b>'],
}, over);

test('a shadowed hand merge shows the run against the target and the report', () => {
  const html = render({ enabled: true, projects: [], turn_on: turnOn() });
  assert.ok(html.includes('merge-shadow-card'));
  assert.ok(html.includes('2 of 5'), html);
  assert.ok(html.includes(bundle('en').merge_shadow_turn_on_merging_off));
  assert.ok(html.includes('merge-shadow-turn-on-report'));
  assert.ok(html.includes('Turn-on report for demo.'));
  assert.ok(!html.includes('<b>two</b>'), 'report text is escaped');
  assert.ok(!html.includes('merge-shadow-table'), 'no empty table of earlier comparisons');
});

test('merging on is said so', () => {
  const html = render({ enabled: true, projects: [], turn_on: turnOn({ merging_on: true }) });
  assert.ok(html.includes(bundle('en').merge_shadow_turn_on_merging_on));
});

test('hidden with no shadowed hand merge, or with the shadow off', () => {
  assert.equal(render({ enabled: true, projects: [], turn_on: turnOn({ merges: [] }) }), '');
  assert.equal(render({ enabled: false, projects: [], turn_on: turnOn() }), '');
});

test('the card reads the report from the shadow route', async () => {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  loadScript(CARD_JS, sandbox);
  sandbox.apiFetch = (u) => Promise.resolve(u === '/api/config'
    ? { autonomous: { merge_shadow: { enabled: true } } }
    : { projects: [], turn_on: turnOn() });
  const view = await vm.runInContext('window.mergeShadowFetch()', sandbox);
  assert.equal(view.turn_on.target, 5);
});

test('locale bundles carry the turn-on labels in all five languages', () => {
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = bundle(lang);
    for (const k of KEYS) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, lang + ' ' + k);
    }
  }
});

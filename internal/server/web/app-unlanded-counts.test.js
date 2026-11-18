// Unlanded work counts block on the Automata Housekeeping card: counts per
// landing state and age band, the oldest pending branch, and the marker when
// the pending count is above the daily alert threshold.
const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function load() {
  const sb = buildSandbox();
  vm.createContext(sb);
  loadScript(path.join(__dirname, 'app.js'), sb);
  assert.equal(typeof sb.unlandedCountsHtml, 'function');
  return sb;
}
const counts = {
  total: 6,
  by_state: { landed: 1, pending: 4, live: 0, unlanded: 1 },
  by_age: { under_one_day: 2, under_one_week: 3, older: 1 },
  oldest_pending: { branch: 'automaton/abc-x', title: 'Old work', age: '9d 2h' },
};

test('counts block shows every state and age band chip and the oldest pending', () => {
  const sb = load();
  const h = sb.unlandedCountsHtml(counts, { enabled: true, threshold: 10 });
  for (const k of ['landed', 'pending', 'live', 'unlanded', 'under_one_day', 'under_one_week', 'older']) {
    assert.match(h, new RegExp(`data-key="${k}"`));
  }
  assert.match(h, /unlanded-counts-oldest/);
  assert.match(h, /automaton\/abc-x/);
  assert.doesNotMatch(h, /unlanded-counts-over/);
});

test('marker appears only when pending is above an enabled threshold', () => {
  const sb = load();
  assert.match(sb.unlandedCountsHtml(counts, { enabled: true, threshold: 3 }), /unlanded-counts-over/);
  assert.doesNotMatch(sb.unlandedCountsHtml(counts, { enabled: true, threshold: 4 }), /unlanded-counts-over/);
  assert.doesNotMatch(sb.unlandedCountsHtml(counts, { enabled: false, threshold: 0 }), /unlanded-counts-over/);
  assert.doesNotMatch(sb.unlandedCountsHtml(counts, null), /unlanded-counts-over/);
});

test('missing counts draw nothing; card embeds the block', () => {
  const sb = load();
  assert.equal(sb.unlandedCountsHtml(null), '');
  const card = sb.unlandedWorkCardHtml({ counts, branches: [{ branch: 'b1', title: 'T' }] });
  assert.match(card, /class="unlanded-counts"/);
});

test('every shipped locale has the counts keys', () => {
  const dir = path.join(__dirname, 'locales');
  const en = JSON.parse(fs.readFileSync(path.join(dir, 'en.json'), 'utf8'));
  const keys = Object.keys(en).filter(k => k.startsWith('unlanded_counts_'));
  assert.equal(keys.length, 11);
  for (const f of fs.readdirSync(dir).filter(f => f.endsWith('.json'))) {
    const d = JSON.parse(fs.readFileSync(path.join(dir, f), 'utf8'));
    for (const k of keys) assert.ok(d[k], `${f} missing ${k}`);
  }
});

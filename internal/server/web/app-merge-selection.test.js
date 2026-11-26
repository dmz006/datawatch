// The merge selection block on the Automaton detail overview: it asks the
// daemon's one selection rule (GET /api/autonomous/merge?automaton=ID), shows
// why a branch is left out, and offers to accept the exact commit shown.

const nodeTest = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const test = (title, fn) => nodeTest('TestMergeSelection: ' + title, fn);
const APP_JS = path.join(__dirname, 'app.js');
const bundle = lang => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
const EN = bundle('en');
const KEYS = ['merge_select_check_btn', 'merge_select_review_passed', 'merge_select_accepted',
  'merge_select_excluded', 'merge_select_accept_btn', 'merge_select_accept_prompt'];
const TIP = 'a'.repeat(40);

function boot() {
  const sandbox = buildSandbox({});
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  vm.runInContext('Object.assign(window._i18n.bundle, ' + JSON.stringify(EN) + ')', sandbox);
  sandbox.showToast = () => {};
  sandbox._refreshAutomataOrPRD = () => {};
  return sandbox;
}
const run = (sandbox, code) => vm.runInContext(code, sandbox);

test('the block is offered only for unlanded finished work', () => {
  const s = boot();
  const html = p => run(s, 'renderMergeSelectionBlock(' + JSON.stringify(p) + ')');
  assert.ok(html({ id: 'P1', status: 'needs_review', landing: { state: 'pending' } }).includes(EN.merge_select_check_btn));
  assert.equal(html({ id: 'P1', status: 'failed', landing: { state: 'pending' } }), '');
  assert.equal(html({ id: 'P1', status: 'completed', landing: { state: 'landed' } }), '');
  assert.equal(html({ id: 'P1', status: 'completed' }), '');
});

test('an excluded answer shows every reason and offers to accept the exact commit', () => {
  const s = boot();
  const html = run(s, 'mergeSelectionHtml(' + JSON.stringify({ automaton_id: 'P1', eligible: false, tip: TIP, reasons: ['its review is pending', '<b>x</b>'] }) + ')');
  assert.ok(html.includes('its review is pending'));
  assert.ok(html.includes('&lt;b&gt;x'), 'reasons are escaped');
  assert.ok(html.includes(TIP), 'the exact commit is shown');
  assert.ok(html.includes('mergeAcceptPrompt('));
});

test('an eligible answer says why and has no accept button', () => {
  const s = boot();
  for (const basis of ['review_passed', 'accepted']) {
    const html = run(s, 'mergeSelectionHtml(' + JSON.stringify({ automaton_id: 'P1', eligible: true, basis, tip: TIP, reasons: [] }) + ')');
    assert.ok(html.includes(TIP));
    assert.ok(!html.includes('mergeAcceptPrompt('));
  }
});

test('accepting posts the exact commit and the reason; an empty prompt posts nothing', async () => {
  const s = boot();
  const calls = [];
  s.fetch = (url, opts = {}) => {
    calls.push({ url: String(url), method: opts.method || 'GET', body: opts.body ? JSON.parse(opts.body) : null });
    return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({}), text: () => Promise.resolve('{}') });
  };
  s.prompt = () => null;
  run(s, 'mergeAcceptPrompt("P1", "' + TIP + '")');
  s.prompt = () => '   ';
  run(s, 'mergeAcceptPrompt("P1", "' + TIP + '")');
  await new Promise(r => setTimeout(r, 0));
  assert.equal(calls.length, 0);
  s.prompt = () => 'checked the diff';
  run(s, 'mergeAcceptPrompt("P1", "' + TIP + '")');
  await new Promise(r => setTimeout(r, 0));
  assert.equal(calls[0].url, '/api/autonomous/prds/P1/landing/accept');
  assert.deepEqual(calls[0].body, { actor: 'operator', commit: TIP, reason: 'checked the diff' });
});

test('locales: all five bundles carry the keys', () => {
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = bundle(lang);
    for (const k of KEYS) assert.ok(typeof b[k] === 'string' && b[k].trim(), lang + ' has ' + k);
  }
});

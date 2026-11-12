// The Settings -> Detection Filters card must show what is actually active.
// For the rate-limit patterns the shipped default is the daemon's built-in
// per-backend table, not a flat list, so with no operator list the card
// shows no phrases, says the built-in table applies, and never offers the
// old flat phrases as displayed defaults. An operator list is shown as is.

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, makeStubElement, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES = path.join(__dirname, 'locales');

function renderCard(detection) {
  let html = '';
  const el = makeStubElement(v => { html = v; });
  const sandbox = buildSandbox({ byId: { detectionFiltersList: el } });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  sandbox.apiFetch = () => Promise.resolve({ detection });
  vm.runInContext('loadDetectionFilters()', sandbox);
  return new Promise(resolve => setImmediate(() => resolve(html)));
}

function rateLimitSection(html) {
  const start = html.indexOf('Rate Limit Patterns');
  assert.ok(start >= 0, 'rate-limit section missing');
  const next = html.indexOf('Input Needed', start);
  return html.slice(start, next > start ? next : undefined);
}

test('with no operator list the rate-limit section shows no phrases and the built-in hint', async () => {
  const section = rateLimitSection(await renderCard({}));
  assert.ok(section.includes('det-builtin-hint'), 'built-in table hint missing');
  assert.ok(!section.includes('det-item'), 'no phrase rows may be shown when nothing is configured');
  assert.ok(!section.includes('removeDetPattern'), 'nothing to remove when nothing is configured');
  assert.ok(!section.includes('defaults'), 'the "N defaults" count must not appear for the built-in table');
});

test('the old flat phrases are not offered as displayed defaults', async () => {
  const html = await renderCard({});
  for (const old of ['rate limit exceeded', 'quota exceeded']) {
    assert.ok(!html.includes(old), 'stale displayed default: ' + old);
  }
  const src = fs.readFileSync(APP_JS, 'utf8');
  assert.ok(!src.includes("'DATAWATCH_RATE_LIMITED:', \"You've hit your limit\""), 'old default list is still in app.js');
});

test('an operator list is shown as custom entries without the built-in hint', async () => {
  const section = rateLimitSection(await renderCard({ rate_limit_patterns: ['my custom phrase'] }));
  assert.ok(section.includes('my custom phrase'));
  assert.ok(section.includes('1 custom'));
  assert.ok(section.includes("removeDetPattern('rate_limit_patterns',0)"));
  assert.ok(!section.includes('det-builtin-hint'), 'hint must go away once an operator list is set');
});

test('the other pattern sections still show their built-in defaults', async () => {
  const html = await renderCard({});
  assert.ok(html.includes('DATAWATCH_COMPLETE:'));
  assert.ok(html.includes('DATAWATCH_NEEDS_INPUT:'));
});

test('the new detection keys exist in all five locale bundles with a real value', () => {
  const keys = ['settings_detection_rate_limit_builtin', 'settings_detection_rate_limit_builtin_hint'];
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(LOCALES, lang + '.json'), 'utf8'));
    for (const k of keys) {
      assert.equal(typeof bundle[k], 'string', lang + ' missing ' + k);
      assert.ok(bundle[k].length > 0 && bundle[k] !== k, lang + ' has an empty value for ' + k);
    }
  }
});

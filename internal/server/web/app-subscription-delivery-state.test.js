// A subscription row says when its one-time message has not been typed, or
// needs a person to check the session. The labels exist in all five locale
// bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const KEYS = ['lifecycle_sub_delivery_waiting', 'lifecycle_sub_delivery_review'];

function bundle(locale) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
}

function row(locale, delivery) {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  vm.runInContext('window._i18n = ' + JSON.stringify({ bundle: bundle(locale), fallback: bundle('en') }) + ';', sandbox);
  sandbox.__sub = { id: 's1', filter: { prd_id: 'p1', kinds: ['task.session_started'] }, sink: { type: 'session', target: 'ops' }, one_shot: true };
  if (delivery) sandbox.__sub.delivery = delivery;
  return vm.runInContext("_renderLifecycleSubRow('p1', __sub)", sandbox);
}

test('a waiting or unsure one-time message is named on its row', () => {
  const en = bundle('en');
  for (const state of ['waiting', 'sending', 'not_typed']) {
    const html = row('en', { state, target: 'ops', text: 'x' });
    assert.ok(en.lifecycle_sub_delivery_waiting && html.includes(en.lifecycle_sub_delivery_waiting), state);
    assert.ok(!html.includes(en.lifecycle_sub_delivery_review), state);
  }
  const review = row('en', { state: 'review_needed', target: 'ops', text: 'x' });
  assert.ok(en.lifecycle_sub_delivery_review && review.includes(en.lifecycle_sub_delivery_review));
});

test('a row with nothing waiting shows neither label', () => {
  const en = bundle('en');
  const html = row('en', null);
  for (const k of KEYS) assert.ok(en[k] && !html.includes(en[k]), k);
});

test('both labels exist, translated, in all five locale bundles', () => {
  const en = bundle('en');
  for (const lg of LOCALES) {
    for (const k of KEYS) {
      const v = bundle(lg)[k];
      assert.ok(typeof v === 'string' && v.trim(), lg + ' has ' + k);
      if (lg !== 'en') assert.notEqual(v, en[k], lg + ' ' + k + ' is translated');
    }
    assert.ok(row(lg, { state: 'review_needed' }).includes(bundle(lg).lifecycle_sub_delivery_review), lg);
  }
});

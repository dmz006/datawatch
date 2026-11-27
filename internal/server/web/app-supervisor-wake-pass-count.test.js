// The supervisor wake status line shows how many supervisor passes event wakes
// ran, apart from the count of event wakes whose line was typed, and the label
// exists in all five locale bundles.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const KEY = 'supervisor_wake_status_passes';

function bundle(locale) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', locale + '.json'), 'utf8'));
}

function statusText(locale, view) {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  vm.runInContext('window._i18n = ' + JSON.stringify({ bundle: bundle(locale), fallback: bundle('en') }) + ';', sandbox);
  sandbox.__v = view;
  return vm.runInContext('supervisorWakeStatusText(__v)', sandbox);
}

const view = {
  last_event: '0001-01-01T00:00:00Z', event_count: 0, pass_count: 3,
  last_scheduled_check: '0001-01-01T00:00:00Z', scheduled_count: 0,
};

test('the status line shows the typed count and the pass count apart', () => {
  const text = statusText('en', view);
  assert.match(text, /Last event wake: not yet \(0 total\)/);
  assert.match(text, /Event wake passes run: 3\./);
});

test('the pass count label exists, translated, in all five locale bundles', () => {
  const en = bundle('en')[KEY];
  assert.ok(en, 'en has ' + KEY);
  for (const lg of LOCALES) {
    const label = bundle(lg)[KEY];
    assert.ok(typeof label === 'string' && label.includes('%1$d'), lg + ' has the label with its number');
    if (lg !== 'en') assert.notEqual(label, en, lg + ' is translated');
    assert.ok(statusText(lg, view).includes(label.replace('%1$d', '3')), lg + ' status shows it');
  }
});

test('the help says what each count means, in all five locale bundles', () => {
  assert.match(bundle('en').supervisor_wake_status_help, /typed/);
  assert.match(bundle('en').supervisor_wake_status_help, /passes/);
});

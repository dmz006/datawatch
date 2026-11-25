// A command schedule waiting for a session with its name says so in the
// schedule list, in all five locales.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const KEY = 'schedule_rearm_waiting_session';
const bundle = (l) => JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));

test('the waiting-for-a-session string is translated in all five locales', () => {
  for (const l of ['en', 'de', 'es', 'fr', 'ja']) {
    assert.ok((bundle(l)[KEY] || '').includes('%1$s'), `${l} lacks ${KEY} with the name`);
    if (l !== 'en') assert.notEqual(bundle(l)[KEY], bundle('en')[KEY], `${l} is a copy of English`);
  }
});

test('a schedule row waiting for a session names it', () => {
  const sb = buildSandbox();
  vm.createContext(sb);
  loadScript(path.join(__dirname, 'app.js'), sb);
  const en = bundle('en');
  vm.runInContext('window._i18n = ' + JSON.stringify({ bundle: en, fallback: en }) + ';', sb);
  const out = vm.runInContext(`_scheduleRearmSummary(${JSON.stringify({
    state: 'pending', session_name: 'supervisor', rearm_state: 'waiting_session',
  })})`, sb);
  assert.match(out, /Waiting for a session named supervisor/);
});

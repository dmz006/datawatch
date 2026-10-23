// Session card — capacity-queue priority badge.
//
// A priority of 0 (or none) shows no badge; a non-zero priority shows the
// localized label with the value. Output is escaped, and all five locale
// bundles carry the label.

const test = require('node:test');
const assert = require('node:assert/strict');
const path = require('path');
const fs = require('fs');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  return sandbox;
}

function sess(extra) {
  return Object.assign({
    id: 'ab12', full_id: 'host-ab12', state: 'running', task: 'build it',
    updated_at: '2026-10-22T12:00:00Z',
  }, extra || {});
}

test('priority 0 shows no badge', () => {
  const sb = loadApp();
  assert.doesNotMatch(sb.sessionCard(sess({ priority: 0 }), 0, 1), /priority-badge/);
});

test('a session without a priority shows no badge', () => {
  const sb = loadApp();
  assert.doesNotMatch(sb.sessionCard(sess(), 0, 1), /priority-badge/);
});

test('a non-zero priority shows the localized label and value', () => {
  const sb = loadApp();
  const html = sb.sessionCard(sess({ priority: 7 }), 0, 1);
  assert.match(html, /class="priority-badge"/);
  assert.match(html, />Priority 7</);
});

test('a negative priority is shown', () => {
  const sb = loadApp();
  assert.match(sb.sessionCard(sess({ priority: -3 }), 0, 1), />Priority -3</);
});

test('a non-numeric priority never reaches the markup', () => {
  const sb = loadApp();
  const html = sb.sessionCard(sess({ priority: '<img src=x onerror=alert(1)>' }), 0, 1);
  assert.doesNotMatch(html, /priority-badge/);
  assert.doesNotMatch(html, /<img src=x/);
});

test('the label is escaped', () => {
  const sb = loadApp();
  sb._i18n.bundle.session_priority_badge = '<b>P</b> %1$d';
  const html = sb.sessionCard(sess({ priority: 2 }), 0, 1);
  assert.match(html, /&lt;b&gt;P&lt;\/b&gt; 2/);
  assert.doesNotMatch(html, /<b>P<\/b>/);
});

test('every locale bundle carries the priority label', () => {
  for (const lang of ['en', 'de', 'es', 'fr', 'ja']) {
    const b = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
    assert.match(b.session_priority_badge, /%1\$d/, lang);
    assert.ok(b.session_priority_badge_tip, lang);
  }
});

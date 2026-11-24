'use strict';
// Over-limit workers on the capacity card — PWA side.
//
// A holder whose lease the daemon flags over_limit gets a chip beside its id,
// with the daemon's reason as the tooltip. Holders not flagged, and bodies from
// an older daemon without the field, render exactly as before.
//
//   env -u TMUX -u TMUX_TMPDIR node --test internal/server/web/app-capacity-over-limit.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];
const bundles = {};
for (const l of LOCALES) bundles[l] = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', l + '.json'), 'utf8'));

function loadApp() {
  const sandbox = buildSandbox();
  vm.createContext(sandbox);
  loadScript(path.join(__dirname, 'app.js'), sandbox);
  sandbox._i18n.bundle = bundles.en;
  return sandbox;
}

function render(s, cap) {
  s._cap = cap;
  return vm.runInContext('window.capacityCardHtml(_cap)', s);
}

const H1 = 'abcdef1234567890';
const H2 = '12345678ffff0000';
const REASON = 'adopted at restart over the limit: pool llm:pi holds 2, limit 1; new work waits until it is back under the limit';

function body(leases) {
  return {
    pools: [{ name: 'llm:pi', limit: 1, held: 2, external: 0, holder: H1 + ',' + H2, holders: [H1, H2] }],
    leases, waiting: [], backoff: [], budgets: [],
  };
}

test('a lease flagged over_limit is marked beside its holder with the reason as tooltip', () => {
  const s = loadApp();
  const html = render(s, body([
    { holder: H1, pools: ['llm:pi'], over_limit: true, over_limit_code: 'adopted_over_limit', over_limit_reason: REASON },
    { holder: H2, pools: ['llm:pi'] },
  ]));
  assert.ok(html.includes('abcdef12 <span class="cap-over-limit"'), 'flagged holder is marked: ' + html);
  assert.ok(html.includes('over the limit</span>'), 'chip text: ' + html);
  assert.ok(html.includes('title="' + REASON + '"'), 'reason is the tooltip: ' + html);
  assert.equal(html.split('cap-over-limit').length - 1, 1, 'only the flagged lease is marked: ' + html);
});

test('a body without over_limit renders no chip', () => {
  const s = loadApp();
  const html = render(s, body([{ holder: H1, pools: ['llm:pi'] }, { holder: H2, pools: ['llm:pi'] }]));
  assert.ok(!html.includes('cap-over-limit'), html);
});

test('the reason is escaped', () => {
  const s = loadApp();
  const html = render(s, body([{ holder: H1, pools: ['llm:pi'], over_limit: true, over_limit_reason: '<img src=x onerror=1>' }]));
  assert.ok(!html.includes('<img'), 'unescaped reason leaked: ' + html);
});

test('every locale carries the over-limit chip text', () => {
  for (const l of LOCALES) {
    assert.ok(typeof bundles[l].capacity_over_limit === 'string' && bundles[l].capacity_over_limit.length > 0, l + ' lacks capacity_over_limit');
  }
});

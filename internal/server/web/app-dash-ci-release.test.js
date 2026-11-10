// Operator console CI & release card (id 'console-ci-release').
//
// The card shows the real release and ship state from the console payload's
// ci_release.release detail. When the repo CI watch is off it says exactly
// "repo CI watch off" with the setting as the next action, and still shows the
// release and ship state. Failing or hung runs list only while the watch is on,
// and run links are rendered only for https URLs. The card is dropped from the
// grid through its DASH_CARD_FEATURES active() check until the release or ship
// state has been reported.

'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { buildSandbox, loadScript, vm, makeStubElement } = require('./testutil_browser_stub.js');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALE_DIR = path.join(__dirname, 'locales');
const EN = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, 'en.json'), 'utf8'));

const NEW_KEYS = [
  'dash_console_ci_latest', 'dash_console_ci_running', 'dash_console_ci_check_failed',
  'dash_console_ci_not_wired', 'dash_console_ci_ship', 'dash_console_ci_ship_note',
  'dash_console_ci_watch_off', 'dash_console_ci_watch_link', 'dash_console_ci_none_failing',
  'dash_console_ci_failing', 'dash_console_ci_run_link',
  'dash_card_off_ci_release_reason', 'dash_card_off_ci_release_link',
];

function makeEnv() {
  const byId = {};
  for (const id of ['console-questions', 'console-held', 'console-lanes', 'console-usage', 'console-ci-release']) {
    byId['dashConsoleCard_' + id] = makeStubElement();
  }
  byId['dashCardGrid'] = makeStubElement();
  const sandbox = vm.createContext(buildSandbox({ byId }));
  loadScript(APP_JS, sandbox);
  // Load the English bundle so t() returns real text, not keys.
  sandbox.__en = EN;
  vm.runInContext('window._i18n = { bundle: __en, fallback: __en };', sandbox);
  return { sandbox, byId };
}

function evalIn(sandbox, code) {
  return vm.runInContext(code, sandbox);
}

function setPayload(sandbox, ciRelease) {
  sandbox.__ci = ciRelease;
  vm.runInContext(`_dash._console = { ci_release: __ci };`, sandbox);
}

function releaseSection(over) {
  return Object.assign({
    available: true, count: 3, summary: '3 CI run(s) under watch', link: '#releases',
    release: {
      current_version: '1.2.3', latest_release: '1.2.4', release_state: 'update_available',
      watch_mode: 'off', watch_off: true, watch_link: '#settings',
      ship_push: 'auto', ship_tag: 'auto', ship_ci_watch: 'on', ship_last_note: '',
      failing_runs: [],
    },
  }, over || {});
}

test('watch off: says exactly "repo CI watch off" with a settings next action, and keeps release and ship state', () => {
  const { sandbox, byId } = makeEnv();
  setPayload(sandbox, releaseSection());
  evalIn(sandbox, '_dashRenderConsole();');
  const html = byId['dashConsoleCard_console-ci-release'].innerHTML;
  assert.ok(html.includes('repo CI watch off'), 'exact off line');
  assert.ok(html.includes('href="#settings"'), 'next action links to settings');
  assert.ok(html.includes('Latest release v1.2.4'), 'latest release shown');
  assert.ok(html.includes('push auto'), 'ship policy shown');
  assert.ok(!html.includes('failing or hung'), 'no failing-run list while off');
});

test('watch on: lists failing runs, links only https run URLs, escapes labels', () => {
  const { sandbox, byId } = makeEnv();
  setPayload(sandbox, releaseSection({
    release: Object.assign({}, releaseSection().release, {
      watch_mode: 'shadow', watch_off: false,
      failing_runs: [
        { repo: 'dmz006/datawatch', workflow: 'ci.yml', run_id: '1', status: 'failing', run_url: 'https://ci.example/runs/1' },
        { repo: '<img src=x>', workflow: 'w', run_id: '2', status: 'hung', run_url: 'javascript:alert(1)' },
      ],
    }),
  }));
  evalIn(sandbox, '_dashRenderConsole();');
  const html = byId['dashConsoleCard_console-ci-release'].innerHTML;
  assert.ok(!html.includes('repo CI watch off'), 'no off line while watch is on');
  assert.ok(html.includes('2 failing or hung CI run(s)'), 'count line');
  assert.ok(html.includes('href="https://ci.example/runs/1"'), 'https run link kept');
  assert.ok(!html.includes('javascript:'), 'non-https run URL never linked');
  assert.ok(!html.includes('<img'), 'label escaped');
});

test('no release or ship state: unavailable section falls back to the reason', () => {
  const { sandbox, byId } = makeEnv();
  setPayload(sandbox, { available: false, count: 0, reason: 'the autonomous manager is not wired in', link: '#settings' });
  evalIn(sandbox, '_dashRenderConsole();');
  const html = byId['dashConsoleCard_console-ci-release'].innerHTML;
  assert.ok(html.includes('the autonomous manager is not wired in'));
});

test('DASH_CARD_FEATURES active(): off until release or ship state arrives, then on even with the watch off', () => {
  const { sandbox } = makeEnv();
  assert.equal(evalIn(sandbox, "DASH_CARD_FEATURES['console-ci-release'].active()"), false, 'no payload');
  setPayload(sandbox, { available: false, reason: 'x', link: '#settings' });
  assert.equal(evalIn(sandbox, "DASH_CARD_FEATURES['console-ci-release'].active()"), false, 'unavailable');
  setPayload(sandbox, releaseSection());
  assert.equal(evalIn(sandbox, "DASH_CARD_FEATURES['console-ci-release'].active()"), true, 'watch off still active');
});

test('grid drops the card while inactive and shows it once the state is reported', () => {
  const { sandbox, byId } = makeEnv();
  const layout = [{ id: 'console-ci-release', cs: 3, rs: 1 }, { id: 'tree', cs: 2 }];
  sandbox.__layout = layout;
  vm.runInContext('_dashBuildGrid(__layout)', sandbox);
  assert.ok(!byId['dashCardGrid'].innerHTML.includes('console-ci-release'), 'hidden while inactive');

  setPayload(sandbox, releaseSection());
  vm.runInContext('_dashBuildGrid(__layout)', sandbox);
  assert.ok(byId['dashCardGrid'].innerHTML.includes('console-ci-release'), 'shown once active');
});

test('every new string has a value in all five locale bundles', () => {
  for (const loc of ['en', 'de', 'es', 'fr', 'ja']) {
    const bundle = JSON.parse(fs.readFileSync(path.join(LOCALE_DIR, loc + '.json'), 'utf8'));
    for (const k of NEW_KEYS) {
      assert.ok(bundle[k] && String(bundle[k]).length > 0, `${loc}/${k} missing or empty`);
    }
  }
  assert.equal(EN.dash_console_ci_watch_off, 'repo CI watch off');
});

// Supervisor watch card (Automaton view):
//
//   - one card per read view, derived from GET /api/autonomous/supervisor-watch
//     (mode, last poll age, findings with kind and text, comparison agreement)
//   - the card is hidden when the endpoint is missing rather than
//     showing a stale state
//   - every string is t()-wired in all five bundles and the supervisor_watch
//     config keys sit in GENERAL_CONFIG_FIELDS
//
//   node --test app-supervisor-watch.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');
const LOCALES = ['en', 'de', 'es', 'fr', 'ja'];

function readBundle(lang) {
  return JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', lang + '.json'), 'utf8'));
}

// One sandbox with the real app.js loaded, the English bundle active, and
// the card element present. `apiFetchImpl` (optional) stands in for the
// network read of the supervisor watch endpoint.
function loadApp({ byId = {}, apiFetchImpl = null } = {}) {
  byId.automationSupervisorWatchCard = { style: {}, innerHTML: '' };
  const sandbox = buildSandbox({ byId, localStorage: new Map() });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  const en = readBundle('en');
  sandbox.window._i18n.fallback = en;
  sandbox.window._i18n.bundle = en;
  if (apiFetchImpl) sandbox.apiFetch = apiFetchImpl;
  return { sandbox, el: byId.automationSupervisorWatchCard };
}

// Fixed clock: last poll 5 minutes before `now`.
const NOW_MS = Date.parse('2025-01-01T00:10:00Z');
const POLL_AT = '2025-01-01T00:05:00Z';

function viewFixture(overrides = {}) {
  return Object.assign({
    mode: 'shadow',
    last_poll_at: POLL_AT,
    findings: [],
    comparison: { last: null, agreed_days: 0, only_watch: [], only_script: [] },
  }, overrides);
}

test('supervisor watch card — mode, last poll age, findings with kind and text', () => {
  const { sandbox } = loadApp();
  const html = sandbox.supervisorWatchCardHtml(viewFixture({
    findings: [
      { kind: 'ci_red', subject: 'repo', text: 'main is red', resolved: false },
      { kind: 'task_failed', subject: 't1', text: 'task t1 failed', resolved: false, would_alert: true },
      { kind: 'ci_stuck', subject: 'old', text: 'resolved one', resolved: true },
    ],
  }), NOW_MS);
  assert.match(html, /Supervisor watch/);
  assert.match(html, /Mode: shadow \(reports only, never acts\)/);
  assert.match(html, /Last poll 5 m\. ago/);
  assert.match(html, /Open findings: 2/);
  assert.match(html, /<code>ci_red<\/code> main is red/);
  assert.match(html, /<code>task_failed<\/code> task t1 failed \(would alert\)/);
  assert.doesNotMatch(html, /resolved one/);
});

test('supervisor watch card — empty state and never-polled', () => {
  const { sandbox } = loadApp();
  const html = sandbox.supervisorWatchCardHtml(viewFixture({ mode: 'off', last_poll_at: '0001-01-01T00:00:00Z' }), NOW_MS);
  assert.match(html, /Mode: off/);
  assert.match(html, /Last poll never/);
  assert.match(html, /No open findings/);
});

test('supervisor watch card — finding text is escaped', () => {
  const { sandbox } = loadApp();
  const html = sandbox.supervisorWatchCardHtml(viewFixture({
    findings: [{ kind: 'ci_red', text: '<img src=x onerror=alert(1)>' }],
  }), NOW_MS);
  assert.doesNotMatch(html, /<img/);
  assert.match(html, /&lt;img/);
});

test('supervisor watch card — comparison agreement line', () => {
  const { sandbox } = loadApp();
  assert.equal(sandbox.supervisorWatchComparisonText(null),
    'Comparison: no supervisor script record compared yet');
  assert.equal(
    sandbox.supervisorWatchComparisonText({ last: { agreed: true }, agreed_days: 3, only_watch: [], only_script: [] }),
    'Comparison: agrees with the supervisor script (3 days of full agreement)');
  assert.equal(
    sandbox.supervisorWatchComparisonText({ last: { agreed: false }, agreed_days: 1, only_watch: ['ci_red'], only_script: [] }),
    'Comparison: differs from the supervisor script. Only the watch: ci_red. Only the script: nothing. (1 days of full agreement)');
  const html = sandbox.supervisorWatchCardHtml(viewFixture({
    comparison: { last: { agreed: true }, agreed_days: 2, only_watch: [], only_script: [] },
  }), NOW_MS);
  assert.match(html, /agrees with the supervisor script \(2 days of full agreement\)/);
});

test('supervisor watch card — null/garbage views render nothing', () => {
  const { sandbox } = loadApp();
  assert.equal(sandbox.supervisorWatchCardHtml(null, NOW_MS), null);
  assert.equal(sandbox.supervisorWatchCardHtml(undefined, NOW_MS), null);
  assert.equal(sandbox.supervisorWatchCardHtml('shadow', NOW_MS), null);
  assert.equal(sandbox.supervisorWatchCardHtml([], NOW_MS), null);
});

test('supervisor watch card — hidden when the endpoint is missing, shown when it answers', async () => {
  const { sandbox, el } = loadApp({
    apiFetchImpl: () => Promise.reject(new Error('404 not found')),
  });
  // start with a stale-looking value to prove the renderer clears it
  el.innerHTML = '<div>stale</div>';
  el.style.display = '';
  await sandbox.supervisorWatchRefreshCard(NOW_MS);
  assert.equal(el.style.display, 'none');
  assert.equal(el.innerHTML, '');

  // the endpoint answers later (same interval tick after a restore)
  sandbox.apiFetch = () => Promise.resolve(viewFixture({ findings: [{ kind: 'ci_red', text: 'main is red' }] }));
  await sandbox.supervisorWatchRefreshCard(NOW_MS);
  assert.equal(el.style.display, '');
  assert.match(el.innerHTML, /<code>ci_red<\/code> main is red/);
});

test('supervisor watch card — polling lives on window._supervisorWatchInterval and never stacks', () => {
  const { sandbox } = loadApp({ apiFetchImpl: () => new Promise(() => {}) });
  const cleared = [];
  let nextId = 100;
  sandbox.setInterval = () => ++nextId;
  sandbox.clearInterval = id => cleared.push(id);
  sandbox.supervisorWatchStartPolling();
  const first = sandbox.window._supervisorWatchInterval;
  assert.ok(first, 'start sets window._supervisorWatchInterval');
  sandbox.supervisorWatchStartPolling();
  assert.ok(cleared.includes(first), 'a second start clears the first timer');
  assert.notEqual(sandbox.window._supervisorWatchInterval, first);
  sandbox.supervisorWatchStopPolling();
  assert.equal(sandbox.window._supervisorWatchInterval, null);
});

test('supervisor watch — the supervisor_watch config keys sit in GENERAL_CONFIG_FIELDS', () => {
  const sandbox = buildSandbox({ localStorage: new Map() });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  const fields = vm.runInContext('GENERAL_CONFIG_FIELDS', sandbox);
  const section = fields.find(s => s.id === 'autonomous');
  assert.ok(section, 'autonomous section must exist in GENERAL_CONFIG_FIELDS');
  const byKey = new Map(section.fields.map(f => [f.key, f]));
  const keys = ['mode', 'alert_minutes', 'silent_minutes', 'poll_seconds',
    'script_output_path', 'tmux_restart_baseline', 'main_checkout', 'working_model'];
  for (const k of keys) {
    const full = 'autonomous.automation.supervisor_watch.' + k;
    assert.ok(byKey.has(full), `GENERAL_CONFIG_FIELDS must contain ${full}`);
    assert.equal(byKey.get(full).labelKey, 'settings_supervisor_watch_' + k);
  }
  assert.equal(byKey.get('autonomous.automation.supervisor_watch.mode').default, 'shadow');
});

test('supervisor watch — every supervisor_watch key app.js uses is in all five bundles, worded Automaton, never PRD', () => {
  const src = fs.readFileSync(APP_JS, 'utf8');
  const used = new Set();
  for (const m of src.matchAll(/\bt\('((?:settings_)?supervisor_watch_[a-z_]+)'/g)) used.add(m[1]);
  for (const m of src.matchAll(/labelKey: '(settings_supervisor_watch_[a-z_]+)'/g)) used.add(m[1]);
  // the mode keys are built from a prefix in app.js
  used.delete('supervisor_watch_mode_');
  for (const mode of ['off', 'shadow', 'on']) used.add('supervisor_watch_mode_' + mode);
  assert.ok(used.size >= 25, 'expected the full set of supervisor_watch keys, got ' + used.size);
  const en = readBundle('en');
  for (const lang of LOCALES) {
    const b = readBundle(lang);
    for (const k of used) {
      assert.ok(typeof b[k] === 'string' && b[k].length > 0, `${lang}.json must define ${k}`);
      assert.equal(b[k], en[k], `${lang}.json ${k} carries the English text`);
      assert.doesNotMatch(b[k], /\bPRDs?\b/, `${lang}.json ${k} must not say PRD`);
    }
  }
});

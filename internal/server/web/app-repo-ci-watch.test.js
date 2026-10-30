// Repo CI watch status line (Automaton view):
//
//   - one line per read view, derived from GET /api/autonomous/repo-ci-watch
//     (mode, failing/pending record counts, last poll age)
//   - the line is hidden when the endpoint is missing rather than
//     showing a stale state
//   - every string is t()-wired and the eight repo_ci_watch config keys
//     sit in GENERAL_CONFIG_FIELDS
//
//   node --test app-repo-ci-watch.test.js

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('fs');
const path = require('path');
const { vm, buildSandbox, loadScript } = require('./testutil_browser_stub');

const APP_JS = path.join(__dirname, 'app.js');

// One sandbox with the real app.js loaded, the English bundle active, and
// the status-line element present. `apiFetchImpl` (optional) stands in for
// the network read of the repo CI watch endpoint.
function loadApp({ byId = {}, apiFetchImpl = null } = {}) {
  // A plain observable element: the shared stub's textContent is
  // read-only by design, and this line writes visible text into it.
  byId.repoCIWatchStatusLine = { style: {}, title: '', textContent: '' };
  const sandbox = buildSandbox({ byId, localStorage: new Map() });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  sandbox.window._i18n.fallback = en;
  sandbox.window._i18n.bundle = en;
  if (apiFetchImpl) sandbox.apiFetch = apiFetchImpl;
  return { sandbox, el: byId.repoCIWatchStatusLine };
}

function viewFixture(overrides = {}) {
  return Object.assign({
    mode: 'shadow',
    alert_minutes: 30,
    repos: ['composeApp'],
    records: [],
  }, overrides);
}

// Fixed clock: last poll 5 minutes before `now`.
const NOW_MS = Date.parse('2025-01-01T00:10:00Z');
const POLL_AT = '2025-01-01T00:05:00Z';

test('repo CI watch line — shadow mode with failing and pending runs', () => {
  const { sandbox } = loadApp();
  const view = viewFixture({
    last_poll_at: POLL_AT,
    records: [
      { repo: 'composeApp', status: 'failing' },
      { repo: 'composeApp', status: 'failing' },
      { repo: 'composeApp', status: 'pending' },
      { repo: 'composeApp', status: 'green' },
    ],
  });
  assert.equal(
    sandbox.repoCIWatchStatusText(view, NOW_MS),
    'repo CI: shadow, 2 failing, 1 pending, last poll 5 m. ago');
});

test('repo CI watch line — on mode, only failing runs', () => {
  const { sandbox } = loadApp();
  const view = viewFixture({
    mode: 'on',
    last_poll_at: POLL_AT,
    records: [{ repo: 'composeApp', status: 'failing' }],
  });
  assert.equal(
    sandbox.repoCIWatchStatusText(view, NOW_MS),
    'repo CI: on, 1 failing, last poll 5 m. ago');
});

test('repo CI watch line — off mode, empty state (no records)', () => {
  const { sandbox } = loadApp();
  assert.equal(
    sandbox.repoCIWatchStatusText(viewFixture({ mode: 'off' }), NOW_MS),
    'repo CI: off, no failing or pending runs, last poll never');
});

test('repo CI watch line — pending-only count', () => {
  const { sandbox } = loadApp();
  const view = viewFixture({
    last_poll_at: POLL_AT,
    records: [
      { repo: 'composeApp', status: 'pending' },
      { repo: 'composeApp', status: 'pending' },
    ],
  });
  assert.equal(
    sandbox.repoCIWatchStatusText(view, NOW_MS),
    'repo CI: shadow, 2 pending, last poll 5 m. ago');
});

test('repo CI watch line — last poll age buckets', () => {
  const { sandbox } = loadApp();
  const base = { records: [] };
  assert.equal(
    sandbox.repoCIWatchStatusText(Object.assign(base, { mode: 'off', last_poll_at: '2025-01-01T00:09:58Z' }), NOW_MS),
    'repo CI: off, no failing or pending runs, last poll just now');
  assert.equal(
    sandbox.repoCIWatchStatusText(Object.assign(base, { mode: 'off', last_poll_at: '2025-01-01T00:09:30Z' }), NOW_MS),
    'repo CI: off, no failing or pending runs, last poll 30 s. ago');
  assert.equal(
    sandbox.repoCIWatchStatusText(Object.assign(base, { mode: 'off', last_poll_at: '2025-01-01T00:04:00Z' }), NOW_MS),
    'repo CI: off, no failing or pending runs, last poll 6 m. ago');
  assert.equal(
    sandbox.repoCIWatchStatusText(Object.assign(base, { mode: 'off', last_poll_at: '2025-01-01T00:05:00Z' }), NOW_MS),
    'repo CI: off, no failing or pending runs, last poll 5 m. ago');
  // 3.5 hours before -> minutes bucket is capped at hours
  assert.equal(
    sandbox.repoCIWatchStatusText(Object.assign(base, { mode: 'off', last_poll_at: '2025-01-01T23:10:00Z' }), Date.parse('2025-01-02T02:40:00Z')),
    'repo CI: off, no failing or pending runs, last poll 3 h. ago');
  // 40 hours before -> days bucket
  assert.equal(
    sandbox.repoCIWatchStatusText(Object.assign(base, { mode: 'off', last_poll_at: '2025-01-01T00:00:00Z' }), Date.parse('2025-01-02T16:00:00Z')),
    'repo CI: off, no failing or pending runs, last poll 1 d. ago');
});

test('repo CI watch line — null/garbage views render nothing', () => {
  const { sandbox } = loadApp();
  assert.equal(sandbox.repoCIWatchStatusText(null, NOW_MS), null);
  assert.equal(sandbox.repoCIWatchStatusText(undefined, NOW_MS), null);
  assert.equal(sandbox.repoCIWatchStatusText('shadow', NOW_MS), null);
  // a record whose status is missing counts as neither
  assert.equal(
    sandbox.repoCIWatchStatusText(viewFixture({ records: [{ repo: 'composeApp' }] }), NOW_MS),
    'repo CI: shadow, no failing or pending runs, last poll never');
});

test('repo CI watch line — the element is populated with the localized text and title', () => {
  const { sandbox, el } = loadApp();
  sandbox.repoCIWatchRenderLine(viewFixture({ last_poll_at: POLL_AT, records: [{ status: 'failing' }] }), NOW_MS);
  assert.equal(el.textContent, 'repo CI: shadow, 1 failing, last poll 5 m. ago');
  assert.equal(el.title, 'Repo CI watch');
  assert.equal(el.style.display, '');
});

test('repo CI watch line — the line is hidden when the endpoint is missing, then shown when it answers', async () => {
  const { sandbox, el } = loadApp({
    apiFetchImpl: () => Promise.reject(new Error('404 not found')),
  });
  // start with a stale-looking value to prove the renderer clears it
  el.textContent = 'repo CI: on, 9 failing, last poll 1 d. ago';
  el.style.display = '';
  await sandbox.repoCIWatchRefreshStatusLine(NOW_MS);
  assert.equal(el.style.display, 'none');
  assert.equal(el.textContent, '');

  // the endpoint answers later (same interval tick after a restore)
  const view = viewFixture({ last_poll_at: POLL_AT, records: [{ status: 'pending' }] });
  sandbox.apiFetch = () => Promise.resolve(view);
  await sandbox.repoCIWatchRefreshStatusLine(NOW_MS);
  assert.equal(el.style.display, '');
  assert.equal(el.textContent, 'repo CI: shadow, 1 pending, last poll 5 m. ago');
});

test('repo CI watch — the eight repo_ci_watch config keys sit in GENERAL_CONFIG_FIELDS', () => {
  const sandbox = buildSandbox({ localStorage: new Map() });
  vm.createContext(sandbox);
  loadScript(APP_JS, sandbox);
  const fields = vm.runInContext('GENERAL_CONFIG_FIELDS', sandbox);
  const section = fields.find(s => s.id === 'autonomous');
  assert.ok(section, 'autonomous section must exist in GENERAL_CONFIG_FIELDS');
  const keys = new Set(section.fields.map(f => f.key));
  for (const k of [
    'autonomous.automation.repo_ci_watch.mode',
    'autonomous.automation.repo_ci_watch.repos',
    'autonomous.automation.repo_ci_watch.poll_seconds',
    'autonomous.automation.repo_ci_watch.alert_minutes',
    'autonomous.automation.repo_ci_watch.tag_count',
    'autonomous.automation.repo_ci_watch.hung_after_minutes',
    'autonomous.automation.repo_ci_watch.security_workflows',
    'autonomous.automation.repo_ci_watch.delete_superseded',
  ]) {
    assert.ok(keys.has(k), `GENERAL_CONFIG_FIELDS must contain ${k}`);
  }
});

test('repo CI watch — every repo_ci_watch_* key app.js uses is wired in the English bundle', () => {
  const en = JSON.parse(fs.readFileSync(path.join(__dirname, 'locales', 'en.json'), 'utf8'));
  const used = [
    'repo_ci_watch_status_title',
    'repo_ci_watch_prefix',
    'repo_ci_watch_mode_off',
    'repo_ci_watch_mode_shadow',
    'repo_ci_watch_mode_on',
    'repo_ci_watch_failing',
    'repo_ci_watch_pending',
    'repo_ci_watch_none_open',
    'repo_ci_watch_last_poll',
    'repo_ci_watch_last_poll_never',
    'repo_ci_watch_age_just_now',
    'repo_ci_watch_age_seconds',
    'repo_ci_watch_age_minutes',
    'repo_ci_watch_age_hours',
    'repo_ci_watch_age_days',
  ];
  for (const k of used) {
    assert.ok(typeof en[k] === 'string' && en[k].length > 0, `en.json must define ${k}`);
  }
});
